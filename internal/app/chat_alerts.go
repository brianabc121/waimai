package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Chat, third part (docs/CHAT_DESIGN.md sections 8 to 10): telling someone
// who is not looking at the page that a message came, blocks, reports, and
// what is thrown away when.

var errChatBlocked = Err(http.StatusForbidden, "CHAT_BLOCKED", "this store cannot be reached by chat right now, please call it")

const (
	// chatNudgeAfter is how long a message may stay unread before a push goes
	// out: someone who has the page open reads it sooner than that.
	chatNudgeAfter = 20 * time.Second
	// chatSMSAfter is how long before a customer with an open order is texted.
	chatSMSAfter = 3 * time.Minute

	chatKeepDays      = 180
	chatImageKeepDays = 90
	// chatReportKeepDays is how long a reported conversation is kept after
	// the platform dealt with it.
	chatReportKeepDays = 90
)

// ---- a customer's browsers ----

// AddCustomerDevice remembers a browser's web push subscription.
func (a *App) AddCustomerDevice(ctx context.Context, customerID int64, token string) (int64, error) {
	token = strings.TrimSpace(token)
	var sub struct {
		Endpoint string `json:"endpoint"`
	}
	if token == "" || len(token) > 4096 || json.Unmarshal([]byte(token), &sub) != nil || !strings.HasPrefix(sub.Endpoint, "https://") {
		return 0, ErrValidation("invalid push subscription")
	}
	var id int64
	err := a.DB.QueryRow(ctx, `INSERT INTO customer_devices (customer_id, token) VALUES ($1,$2)
		ON CONFLICT (token) DO UPDATE SET customer_id=$1 RETURNING id`, customerID, token).Scan(&id)
	return id, err
}

func (a *App) RemoveCustomerDevice(ctx context.Context, customerID, id int64) error {
	_, err := a.DB.Exec(ctx, `DELETE FROM customer_devices WHERE id=$1 AND customer_id=$2`, id, customerID)
	return err
}

// ---- reminders ----

// chatAlertJob is a reminder that is due a little after a message: by then
// the message may have been read, and nothing is sent.
type chatAlertJob struct {
	ThreadID int64  `json:"thread_id"`
	To       string `json:"to"` // store, customer (push) or sms (the customer's phone)
}

func enqueueAfter(ctx context.Context, tx pgx.Tx, kind string, payload any, after time.Duration) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs (kind, payload, run_at) VALUES ($1, $2, now() + make_interval(secs => $3))`, kind, b, after.Seconds())
	return err
}

// chatAlerts queues the reminders for a message that became the other side's
// first unread one. Later messages ride on the same reminder.
func chatAlerts(ctx context.Context, tx pgx.Tx, threadID int64, sender string) error {
	if sender == "customer" {
		return enqueueAfter(ctx, tx, "chat_alert", chatAlertJob{ThreadID: threadID, To: "store"}, chatNudgeAfter)
	}
	if err := enqueueAfter(ctx, tx, "chat_alert", chatAlertJob{ThreadID: threadID, To: "customer"}, chatNudgeAfter); err != nil {
		return err
	}
	return enqueueAfter(ctx, tx, "chat_alert", chatAlertJob{ThreadID: threadID, To: "sms"}, chatSMSAfter)
}

// runChatAlert sends a reminder if the message is still unread. Neither a
// push nor a text carries what was written.
func (a *App) runChatAlert(ctx context.Context, j chatAlertJob) error {
	var branchID, customerID int64
	var storeUnread, customerUnread int
	var quiet, notifySMS bool
	var store, slug string
	var texted *int64
	err := a.DB.QueryRow(ctx, `SELECT t.branch_id, t.customer_id, t.store_unread, t.customer_unread, t.customer_quiet, t.sms_order_id,
			b.name, b.slug, b.notify_sms
		FROM chat_threads t JOIN branches b ON b.id=t.branch_id WHERE t.id=$1`, j.ThreadID).
		Scan(&branchID, &customerID, &storeUnread, &customerUnread, &quiet, &texted, &store, &slug, &notifySMS)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	tag := fmt.Sprintf("chat-%d", j.ThreadID)
	switch j.To {
	case "store":
		if storeUnread == 0 {
			return nil
		}
		// everyone at the store's desk who asked for reminders on a device
		rows, err := a.DB.Query(ctx, `SELECT DISTINCT r.user_id FROM user_branch_roles r JOIN users u ON u.id=r.user_id
			WHERE r.branch_id=$1 AND u.active AND r.role IN ('owner','manager','cashier')
			  AND EXISTS (SELECT 1 FROM rider_devices d WHERE d.user_id=r.user_id)`, branchID)
		if err != nil {
			return err
		}
		users, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		m := PushMessage{TitleKey: "sys.push.chat.store_title", Body: store, Tag: tag,
			URL: fmt.Sprintf("/merchant/messages?c=%d", customerID)}
		for _, u := range users {
			if err := a.sendPush(ctx, pushJob{UserID: u, Message: m}); err != nil {
				log.Printf("chat: reminder to user %d: %v", u, err)
			}
		}
		return nil
	case "customer":
		if customerUnread == 0 || quiet {
			return nil
		}
		return a.pushCustomer(ctx, customerID, PushMessage{Title: store, BodyKey: "sys.push.chat.sent_you", Tag: tag, URL: "/chat/" + slug})
	case "sms":
		if customerUnread == 0 || quiet || !notifySMS {
			return nil
		}
		return a.chatSMS(ctx, j.ThreadID, branchID, customerID, store, slug)
	}
	return nil
}

// pushCustomer sends to every browser the customer switched reminders on in.
// A server without web push keys sends nothing.
func (a *App) pushCustomer(ctx context.Context, customerID int64, m PushMessage) error {
	sender := a.pushSender("web")
	if sender == nil {
		return nil
	}
	rows, err := a.DB.Query(ctx, `SELECT id, token, lang FROM customer_devices WHERE customer_id=$1 ORDER BY id`, customerID)
	if err != nil {
		return err
	}
	devices, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Device, error) {
		d := Device{Platform: "web"}
		return d, r.Scan(&d.ID, &d.Token, &d.Lang)
	})
	if err != nil {
		return err
	}
	var sent int
	var last error
	for _, d := range devices {
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := sender.Send(sendCtx, d, m.in(ctx, a, d.Lang))
		cancel()
		switch {
		case err == nil:
			sent++
		case errors.Is(err, ErrPushGone):
			if _, err := a.DB.Exec(ctx, `DELETE FROM customer_devices WHERE id=$1`, d.ID); err != nil {
				return err
			}
		default:
			last = err
		}
	}
	if sent == 0 {
		return last
	}
	return nil
}

// chatSMS texts a customer who has an order under way with the store, has no
// browser to push to, and has not been texted about this order before.
func (a *App) chatSMS(ctx context.Context, threadID, branchID, customerID int64, store, slug string) error {
	var orderID int64
	var phone, lang string
	err := a.DB.QueryRow(ctx, `SELECT o.id, o.customer_phone, o.customer_lang FROM orders o
		WHERE o.branch_id=$1 AND o.customer_id=$2 AND o.closed_at IS NULL AND o.channel <> 'dine_in'
		  AND o.acceptance_status IN ('pending','accepted')
		  AND NOT EXISTS (SELECT 1 FROM customer_devices d WHERE d.customer_id=o.customer_id)
		ORDER BY o.id DESC LIMIT 1`, branchID, customerID).Scan(&orderID, &phone, &lang)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// the UPDATE both decides and remembers: one text per order
	tag, err := a.DB.Exec(ctx, `UPDATE chat_threads SET sms_order_id=$2 WHERE id=$1 AND sms_order_id IS DISTINCT FROM $2`, threadID, orderID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	// one language only: the one the customer ordered in
	if names, err := storeNames(ctx, a.DB, lang, []int64{branchID}); err == nil && names[branchID] != "" {
		store = names[branchID]
	}
	msg := a.sysText(ctx, lang, "sys.sms.chat", "store", store)
	if a.PublicBaseURL != "" {
		msg += " " + strings.TrimRight(a.PublicBaseURL, "/") + "/chat/" + slug
	}
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := a.smsSender().Send(sendCtx, phone, msg); err != nil {
		// not sent: the retry may try this order again
		if _, e := a.DB.Exec(ctx, `UPDATE chat_threads SET sms_order_id=NULL WHERE id=$1 AND sms_order_id=$2`, threadID, orderID); e != nil {
			return e
		}
		return err
	}
	return nil
}

// ---- what a customer can do with a conversation ----

// SetChatQuiet turns reminders about one store's messages off or on again.
func (a *App) SetChatQuiet(ctx context.Context, customerID, branchID int64, quiet bool) error {
	_, err := a.DB.Exec(ctx, `UPDATE chat_threads SET customer_quiet=$3 WHERE customer_id=$1 AND branch_id=$2`, customerID, branchID, quiet)
	return err
}

// HideChat takes a conversation off the customer's list. The store and the
// platform still have it, and the next message brings it back.
func (a *App) HideChat(ctx context.Context, customerID, branchID int64) error {
	_, err := a.DB.Exec(ctx, `UPDATE chat_threads SET customer_hidden_at=now(), customer_unread=0, customer_read_at=now()
		WHERE customer_id=$1 AND branch_id=$2`, customerID, branchID)
	return err
}

// ---- blocks ----

// BlockChatCustomer stops a customer from writing to this store, or lets
// them again. Ordering is not affected.
func (a *App) BlockChatCustomer(ctx context.Context, branchID, customerID, staffID int64, blocked bool) error {
	if !blocked {
		_, err := a.DB.Exec(ctx, `DELETE FROM chat_blocks WHERE branch_id=$1 AND customer_id=$2`, branchID, customerID)
		return err
	}
	var known bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_threads WHERE branch_id=$1 AND customer_id=$2)
		OR EXISTS (SELECT 1 FROM orders WHERE branch_id=$1 AND customer_id=$2)`, branchID, customerID).Scan(&known); err != nil {
		return err
	}
	if !known {
		return ErrNotFound
	}
	_, err := a.DB.Exec(ctx, `INSERT INTO chat_blocks (customer_id, branch_id, staff_user_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
		customerID, branchID, staffID)
	return err
}

// ---- reports ----

var chatReportReasons = map[string]bool{"harassment": true, "spam": true, "fraud": true, "abuse": true, "other": true}

// ChatReport is one side's complaint about a conversation.
type ChatReport struct {
	ID         int64      `json:"id"`
	By         string     `json:"reported_by"`
	Staff      string     `json:"staff,omitempty"`
	Reason     string     `json:"reason"`
	Detail     string     `json:"detail"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
}

// ReportChat files a complaint for the platform. side is who complains;
// staffID is the member of staff when it is the store.
func (a *App) ReportChat(ctx context.Context, side string, branchID, customerID, staffID int64, reason, detail string) error {
	detail = strings.TrimSpace(detail)
	if !chatReportReasons[reason] {
		return ErrValidation("choose a reason")
	}
	if utf8.RuneCountInString(detail) > 500 {
		return ErrValidation("text too long")
	}
	if reason == "other" && detail == "" {
		return ErrValidation("describe the problem")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var threadID int64
		err := tx.QueryRow(ctx, `SELECT id FROM chat_threads WHERE branch_id=$1 AND customer_id=$2 FOR UPDATE`, branchID, customerID).Scan(&threadID)
		if isNoRows(err) {
			return ErrValidation("there is nothing to report yet")
		}
		if err != nil {
			return err
		}
		// one open complaint per side: a second one adds nothing for the platform
		var open bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_reports WHERE thread_id=$1 AND reported_by=$2 AND resolved_at IS NULL)`,
			threadID, side).Scan(&open); err != nil || open {
			return err
		}
		var staff *int64
		if side == "store" && staffID != 0 {
			staff = &staffID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_reports (thread_id, reported_by, staff_user_id, reason, detail) VALUES ($1,$2,$3,$4,$5)`,
			threadID, side, staff, reason, detail); err != nil {
			return err
		}
		if side != "customer" {
			return nil
		}
		// a customer's complaint about a store also joins the store's other reports
		storeReason := map[string]string{"fraud": "fraud", "other": "other"}[reason]
		if storeReason == "" {
			storeReason = "bad_service"
		}
		_, err = tx.Exec(ctx, `INSERT INTO store_reports (branch_id, reason, detail, contact) VALUES ($1,$2,$3,'')`,
			branchID, storeReason, strings.TrimSpace("[聊天 chat] "+detail))
		return err
	})
}

func chatReports(ctx context.Context, q Querier, threadID int64) ([]ChatReport, error) {
	rows, err := q.Query(ctx, `SELECT r.id, r.reported_by, COALESCE(NULLIF(u.display_name,''), u.username, ''), r.reason, r.detail,
			r.created_at, r.resolved_at
		FROM chat_reports r LEFT JOIN users u ON u.id=r.staff_user_id WHERE r.thread_id=$1 ORDER BY r.id DESC LIMIT 50`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatReport{}
	for rows.Next() {
		var r ChatReport
		if err := rows.Scan(&r.ID, &r.By, &r.Staff, &r.Reason, &r.Detail, &r.CreatedAt, &r.ResolvedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminResolveChatReports marks a conversation's open reports as dealt with.
func (a *App) AdminResolveChatReports(ctx context.Context, actor AdminActor, threadID int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE chat_reports SET resolved_at=now() WHERE thread_id=$1 AND resolved_at IS NULL`, threadID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return adminAudit(ctx, tx, actor, "chat.report_resolve", "chat", threadID, map[string]any{"reports": tag.RowsAffected()})
	})
}

// ---- what is thrown away ----

// chatCleanup is run daily with the other clean-ups. A reported conversation
// is kept until the platform dealt with it, and a while longer.
var chatCleanup = []string{
	fmt.Sprintf(`DELETE FROM chat_messages m WHERE m.sent_at < now() - interval '%d days'
		AND NOT EXISTS (SELECT 1 FROM chat_reports r WHERE r.thread_id=m.thread_id
			AND (r.resolved_at IS NULL OR r.resolved_at > now() - interval '%d days'))`, chatKeepDays, chatReportKeepDays),
	fmt.Sprintf(`DELETE FROM chat_images WHERE created_at < now() - interval '%d days'`, chatImageKeepDays),
	fmt.Sprintf(`DELETE FROM chat_threads t WHERE t.last_at < now() - interval '%d days'
		AND NOT EXISTS (SELECT 1 FROM chat_messages m WHERE m.thread_id=t.id)`, chatKeepDays),
}

// ---- health ----

// HealthChat is the chat server's card on the platform's front page.
type HealthChat struct {
	On    bool   `json:"on"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

func (a *App) chatHealth(ctx context.Context) HealthChat {
	if a.IM == nil {
		return HealthChat{State: "ok"}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := a.IM.Check(ctx); err != nil {
		return HealthChat{On: true, State: "bad", Error: err.Error()}
	}
	return HealthChat{On: true, State: "ok"}
}

// ChatBranch finds the store a customer's chat address names.
func (a *App) ChatBranch(ctx context.Context, slug string) (int64, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return 0, err
	}
	return b.ID, nil
}
