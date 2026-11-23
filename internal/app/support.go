package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Platform support (docs/CHAT_DESIGN.md section 17): a customer or a store
// writes to the platform, and the platform's administrators answer from one
// shared inbox, the OpenIM account p_1. Like a store's inbox, the other side
// sees only "platform support"; which administrator answered travels in the
// message's ex and shows on the platform's page alone.

// platformIM is the platform's one OpenIM account.
const platformIM = "p_1"

const platformNickname = "平台客服 Support"

// supportOrdersShown is how many of the other side's orders a support
// conversation knows: what an order card shows and what can be sent.
const supportOrdersShown = 10

var errSupportParty = Err(http.StatusNotFound, "NOT_FOUND", "no such customer or store")

// support reports whether a callback is about a support conversation.
func (h *ChatHook) support() bool {
	from, _ := imParty(h.SendID)
	to, _ := imParty(h.RecvID)
	return from == "platform" || to == "platform"
}

// supportMsg is a support callback's message once it is understood.
type supportMsg struct {
	party   string // the customer's or the store's account
	kind    string // customer or store: who the party is
	id      int64  // the customer's or the branch's id
	sender  string // customer, store or platform
	staffID int64
	msgKind string
	body    string
	orderID int64
}

func (h *ChatHook) readSupport() (*supportMsg, error) {
	if h.SessionType != 1 {
		return nil, ErrValidation("only one-to-one chat is supported")
	}
	fromKind, fromID := imParty(h.SendID)
	toKind, toID := imParty(h.RecvID)
	m := &supportMsg{sender: fromKind}
	switch {
	case h.SendID == platformIM && (toKind == "customer" || toKind == "store"):
		m.party, m.kind, m.id = h.RecvID, toKind, toID
	case h.RecvID == platformIM && (fromKind == "customer" || fromKind == "store"):
		m.party, m.kind, m.id = h.SendID, fromKind, fromID
	default:
		return nil, ErrValidation("support is between the platform and a customer or a store")
	}
	var err error
	if m.msgKind, m.body, m.orderID, err = h.content(); err != nil {
		return nil, err
	}
	if m.sender != "customer" {
		var ex struct {
			UID int64 `json:"uid"`
		}
		_ = json.Unmarshal([]byte(h.Ex), &ex)
		m.staffID = ex.UID
	}
	return m, nil
}

// supportParty reads c_<id> or b_<id> and checks that it exists.
func (a *App) supportParty(ctx context.Context, party string) (kind string, id int64, err error) {
	kind, id = imParty(party)
	var ok bool
	switch kind {
	case "customer":
		err = a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customers WHERE id=$1)`, id).Scan(&ok)
	case "store":
		err = a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM branches WHERE id=$1 AND deleted_at IS NULL)`, id).Scan(&ok)
	}
	if err != nil {
		return "", 0, err
	}
	if !ok {
		return "", 0, errSupportParty
	}
	return kind, id, nil
}

// supportBeforeSend answers "may this support message go out?".
func (a *App) supportBeforeSend(ctx context.Context, h ChatHook) error {
	m, err := h.readSupport()
	if err != nil {
		return err
	}
	if _, _, err := a.supportParty(ctx, m.party); err != nil {
		return err
	}
	// a muted customer, or a store whose chat is closed, can still reach the
	// platform: that is where they ask why
	var ok bool
	switch m.sender {
	case "store":
		err = a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_branch_roles r JOIN users u ON u.id=r.user_id
			WHERE r.branch_id=$1 AND r.user_id=$2 AND u.active AND r.role IN ('owner','manager','cashier'))`, m.id, m.staffID).Scan(&ok)
	case "platform":
		err = a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id=$1 AND active AND is_platform_admin)`, m.staffID).Scan(&ok)
	default:
		ok = true
	}
	if err != nil {
		return err
	}
	if !ok {
		return Err(http.StatusForbidden, "FORBIDDEN", "not allowed to write for this side")
	}
	switch m.msgKind {
	case "image":
		side := "party"
		if m.sender == "platform" {
			side = "platform"
		}
		err = a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM support_images WHERE token=$1 AND party=$2 AND sender=$3)`,
			m.body, m.party, side).Scan(&ok)
	case "order":
		col := "customer_id"
		if m.kind == "store" {
			col = "branch_id"
		}
		err = a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders WHERE id=$1 AND `+col+`=$2)`, m.orderID, m.id).Scan(&ok)
	}
	if err != nil {
		return err
	}
	if !ok {
		return ErrValidation("the picture or the order does not belong to this conversation")
	}
	var recent int
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM support_messages s JOIN support_threads t ON t.id=s.thread_id
		WHERE t.party=$1 AND s.sender=$2 AND s.sent_at > now() - interval '1 minute'`, m.party, m.sender).Scan(&recent); err != nil {
		return err
	}
	if recent >= chatRate {
		return Err(http.StatusTooManyRequests, "CHAT_TOO_FAST", "too many messages, wait a moment")
	}
	return nil
}

// supportAfterSend keeps this system's copy of a support message.
func (a *App) supportAfterSend(ctx context.Context, h ChatHook) error {
	m, err := h.readSupport()
	if err != nil {
		return nil // nothing this system keeps
	}
	if h.ServerMsgID == "" {
		return ErrValidation("serverMsgID is required")
	}
	at := time.Now()
	if h.SendTime > 0 {
		at = time.UnixMilli(h.SendTime)
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var customer, branch *int64
		if m.kind == "customer" {
			customer = &m.id
		} else {
			branch = &m.id
		}
		var threadID int64
		err := tx.QueryRow(ctx, `INSERT INTO support_threads (party, customer_id, branch_id, last_at) VALUES ($1,$2,$3,$4)
			ON CONFLICT (party) DO UPDATE SET party=EXCLUDED.party RETURNING id`, m.party, customer, branch, at).Scan(&threadID)
		if err != nil {
			return err
		}
		var staff, order *int64
		if m.staffID != 0 {
			staff = &m.staffID
		}
		if m.orderID != 0 {
			order = &m.orderID
		}
		tag, err := tx.Exec(ctx, `INSERT INTO support_messages (thread_id, sender, staff_user_id, kind, body, order_id, im_msg_id, sent_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (im_msg_id) DO NOTHING`, threadID, m.sender, staff, m.msgKind, m.body, order, h.ServerMsgID, at)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		// as in chat.go: the writer has read what came before, and the other
		// side may have read this message before the callback came
		unread := `platform_unread = platform_unread + CASE WHEN platform_read_at >= $5 THEN 0 ELSE 1 END,
			party_unread = 0, party_read_at = GREATEST(party_read_at, $5), resolved_at = NULL, resolved_by = NULL`
		if m.sender == "platform" {
			unread = `party_unread = party_unread + CASE WHEN party_read_at >= $5 THEN 0 ELSE 1 END,
				platform_unread = 0, platform_read_at = GREATEST(platform_read_at, $5)`
		}
		var partyUnread int
		if err := tx.QueryRow(ctx, `UPDATE support_threads SET last_kind=$2, last_text=$3, last_sender=$4, last_at=$5,
			msg_count = msg_count + 1, `+unread+` WHERE id=$1 RETURNING party_unread`,
			threadID, m.msgKind, preview(m.msgKind, m.body), m.sender, at).Scan(&partyUnread); err != nil {
			return err
		}
		if m.sender == "platform" && partyUnread == 1 {
			if err := enqueueAfter(ctx, tx, "support_alert", supportAlertJob{ThreadID: threadID}, chatNudgeAfter); err != nil {
				return err
			}
		}
		if m.kind == "store" {
			return notify(ctx, tx, Event{BranchID: m.id})
		}
		return nil
	})
}

// supportAlertJob reminds a customer or a store of an answer from the
// platform that is still unread a little after it came.
type supportAlertJob struct {
	ThreadID int64 `json:"thread_id"`
}

func (a *App) runSupportAlert(ctx context.Context, j supportAlertJob) error {
	var customer, branch *int64
	var unread int
	err := a.DB.QueryRow(ctx, `SELECT customer_id, branch_id, party_unread FROM support_threads WHERE id=$1`, j.ThreadID).
		Scan(&customer, &branch, &unread)
	if isNoRows(err) || unread == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	tag := fmt.Sprintf("support-%d", j.ThreadID)
	if customer != nil {
		return a.pushCustomer(ctx, *customer, PushMessage{TitleKey: "sys.push.support.title", BodyKey: "sys.push.support.answered", Tag: tag, URL: "/me/support/chat"})
	}
	rows, err := a.DB.Query(ctx, `SELECT DISTINCT r.user_id FROM user_branch_roles r JOIN users u ON u.id=r.user_id
		WHERE r.branch_id=$1 AND u.active AND r.role IN ('owner','manager','cashier')
		  AND EXISTS (SELECT 1 FROM rider_devices d WHERE d.user_id=r.user_id)`, *branch)
	if err != nil {
		return err
	}
	users, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	m := PushMessage{TitleKey: "sys.push.support.merchant_title", BodyKey: "sys.push.support.open", Tag: tag, URL: "/merchant/support"}
	for _, u := range users {
		if err := a.sendPush(ctx, pushJob{UserID: u, Message: m}); err != nil {
			log.Printf("support: reminder to user %d: %v", u, err)
		}
	}
	return nil
}

// ---- signing in ----

func (a *App) platformAccount(ctx context.Context) error {
	return a.chatAccount(ctx, platformIM, platformNickname)
}

// PlatformChatLogin signs an administrator's browser in to the platform's inbox.
func (a *App) PlatformChatLogin(ctx context.Context) (*ChatLogin, error) {
	if err := a.platformAccount(ctx); err != nil {
		return nil, err
	}
	return a.chatLogin(ctx, platformIM)
}

// ---- the customer's and the store's side ----

// SupportChat is what the customer's or the store's support page starts from.
type SupportChat struct {
	Enabled    bool        `json:"enabled"`
	Peer       string      `json:"peer"`
	ThreadID   int64       `json:"thread_id"`
	Unread     int         `json:"unread"`
	PeerReadAt *time.Time  `json:"peer_read_at"`
	Orders     []ChatOrder `json:"orders"`
}

// SupportOf opens a customer's (c_<id>) or a store's (b_<id>) conversation
// with the platform. withTokens gives a customer the links to their orders.
func (a *App) SupportOf(ctx context.Context, party string, withTokens bool) (*SupportChat, error) {
	out := &SupportChat{Enabled: a.IM != nil, Peer: platformIM, Orders: []ChatOrder{}}
	kind, id, err := a.supportParty(ctx, party)
	if err != nil {
		return nil, err
	}
	if out.Orders, err = supportOrders(ctx, a.DB, kind, id); err != nil {
		return nil, err
	}
	if withTokens {
		for i := range out.Orders {
			out.Orders[i].Token = a.orderToken(out.Orders[i].nonce)
		}
	}
	err = a.DB.QueryRow(ctx, `SELECT id, party_unread, platform_read_at FROM support_threads WHERE party=$1`, party).
		Scan(&out.ThreadID, &out.Unread, &out.PeerReadAt)
	if err != nil && !isNoRows(err) {
		return nil, err
	}
	if a.IM != nil {
		// the platform's account must exist to be written to
		if err := a.platformAccount(ctx); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func supportOrders(ctx context.Context, q Querier, kind string, id int64) ([]ChatOrder, error) {
	col := "o.customer_id"
	if kind == "store" {
		col = "o.branch_id"
	}
	rows, err := q.Query(ctx, `SELECT o.id, o.public_no, o.acceptance_status, o.fulfillment_status, o.service_type, o.total, o.currency, o.created_at,
			o.closed_at IS NULL AND o.acceptance_status IN ('pending','accepted'), o.token_nonce, b.name
		FROM orders o JOIN branches b ON b.id=o.branch_id WHERE `+col+`=$1 AND o.channel <> 'dine_in' ORDER BY o.id DESC LIMIT $2`, id, supportOrdersShown)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatOrder{}
	for rows.Next() {
		var o ChatOrder
		var acc, ful string
		if err := rows.Scan(&o.ID, &o.PublicNo, &acc, &ful, &o.Service, &o.Total, &o.Currency, &o.CreatedAt, &o.Open, &o.nonce, &o.StoreName); err != nil {
			return nil, err
		}
		o.Status = DisplayStatus(acc, ful)
		out = append(out, o)
	}
	return out, rows.Err()
}

// SupportUnread counts the platform's answers a customer or a store has not read.
func (a *App) SupportUnread(ctx context.Context, party string) (int, error) {
	var n int
	err := a.DB.QueryRow(ctx, `SELECT COALESCE(sum(party_unread),0) FROM support_threads WHERE party=$1`, party).Scan(&n)
	return n, err
}

// ReadSupport marks a support conversation read by the party or by the platform.
func (a *App) ReadSupport(ctx context.Context, party string, byPlatform bool) error {
	col := "party"
	if byPlatform {
		col = "platform"
	}
	var branch *int64
	err := a.DB.QueryRow(ctx, `UPDATE support_threads SET `+col+`_unread=0, `+col+`_read_at=now() WHERE party=$1 RETURNING branch_id`, party).Scan(&branch)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if a.IM != nil {
		me, peer := party, platformIM
		if byPlatform {
			me, peer = peer, me
		}
		if err := a.IM.MarkRead(ctx, me, peer); err != nil {
			log.Printf("support: mark read %s: %v", me, err)
		}
	}
	if branch != nil {
		return notify(ctx, a.DB, Event{BranchID: *branch})
	}
	return nil
}

// AddSupportImage keeps a picture about to be sent in a support conversation.
// byPlatform: the platform is sending it to party.
func (a *App) AddSupportImage(ctx context.Context, party string, byPlatform bool, image []byte) (string, error) {
	if !isJPEG(image, ImageMaxBytes) {
		return "", ErrValidation("the picture must be a JPEG of at most 1 MB")
	}
	if _, _, err := a.supportParty(ctx, party); err != nil {
		return "", err
	}
	side := "party"
	if byPlatform {
		side = "platform"
	}
	var n int
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM support_images WHERE party=$1 AND sender=$2 AND created_at > now() - interval '1 hour'`,
		party, side).Scan(&n); err != nil {
		return "", err
	}
	if n >= chatImagesPerHour {
		return "", Err(429, "CHAT_TOO_FAST", "too many pictures, wait a while")
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tok)
	_, err := a.DB.Exec(ctx, `INSERT INTO support_images (token, party, sender, content) VALUES ($1,$2,$3,$4)`, token, party, side, image)
	return token, err
}

// ---- the platform's inbox ----

// SupportRow is one conversation in the platform's support inbox.
type SupportRow struct {
	ID         int64      `json:"id"`
	Party      string     `json:"party"`
	Kind       string     `json:"kind"` // customer or store
	CustomerID int64      `json:"customer_id,omitempty"`
	BranchID   int64      `json:"branch_id,omitempty"`
	Name       string     `json:"name"`
	Phone      string     `json:"phone"`
	PhoneTail  string     `json:"phone_tail"`
	Merchant   string     `json:"merchant,omitempty"`
	LastKind   string     `json:"last_kind"`
	LastText   string     `json:"last_text"`
	LastSender string     `json:"last_sender"`
	LastAt     time.Time  `json:"last_at"`
	Unread     int        `json:"unread"`
	Count      int        `json:"count"`
	PeerReadAt *time.Time `json:"peer_read_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	Resolver   string     `json:"resolved_by,omitempty"`
}

type SupportList struct {
	Chats  []SupportRow `json:"chats"`
	Total  int          `json:"total"`
	Unread int          `json:"unread"` // conversations with something unread
	Open   int          `json:"open"`   // conversations not marked dealt with
}

// SupportFilter narrows the platform's inbox.
type SupportFilter struct {
	Q      string
	Who    string // customer or store
	Unread bool
	Open   bool
	Page
}

const supportName = `CASE WHEN t.customer_id IS NOT NULL THEN COALESCE(NULLIF(c.nickname,''), (SELECT o.customer_name FROM orders o
	WHERE o.customer_id=t.customer_id ORDER BY o.id DESC LIMIT 1), '') ELSE b.name END`

const supportCols = `t.id, t.party, COALESCE(t.customer_id,0), COALESCE(t.branch_id,0), ` + supportName + `,
	COALESCE(c.phone, b.phone, ''), COALESCE(m.name, ''), t.last_kind, t.last_text, t.last_sender, t.last_at,
	t.platform_unread, t.msg_count, t.party_read_at, t.resolved_at, COALESCE(NULLIF(ru.display_name,''), ru.username, '')`

const supportFrom = ` FROM support_threads t LEFT JOIN customers c ON c.id=t.customer_id LEFT JOIN branches b ON b.id=t.branch_id
	LEFT JOIN merchants m ON m.id=b.merchant_id LEFT JOIN users ru ON ru.id=t.resolved_by`

func scanSupport(row interface{ Scan(...any) error }) (SupportRow, error) {
	var r SupportRow
	err := row.Scan(&r.ID, &r.Party, &r.CustomerID, &r.BranchID, &r.Name, &r.Phone, &r.Merchant, &r.LastKind, &r.LastText,
		&r.LastSender, &r.LastAt, &r.Unread, &r.Count, &r.PeerReadAt, &r.ResolvedAt, &r.Resolver)
	r.Kind = "customer"
	if r.BranchID != 0 {
		r.Kind = "store"
	}
	if r.Phone != "" {
		r.PhoneTail = last4(r.Phone)
	}
	return r, err
}

// AdminSupport lists the platform's support conversations: the unread ones
// first, then the latest. q looks in names, phones and the messages.
func (a *App) AdminSupport(ctx context.Context, f SupportFilter) (*SupportList, error) {
	where, args := `true`, []any{}
	switch f.Who {
	case "customer":
		where += ` AND t.customer_id IS NOT NULL`
	case "store":
		where += ` AND t.branch_id IS NOT NULL`
	}
	if f.Unread {
		where += ` AND t.platform_unread > 0`
	}
	if f.Open {
		where += ` AND t.resolved_at IS NULL`
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		args = append(args, like(q))
		n := len(args)
		where += fmt.Sprintf(` AND (%s ILIKE $%d OR c.phone ILIKE $%d OR b.phone ILIKE $%d OR m.name ILIKE $%d
			OR EXISTS (SELECT 1 FROM support_messages s WHERE s.thread_id=t.id AND s.body ILIKE $%d))`, supportName, n, n, n, n, n)
	}
	out := &SupportList{Chats: []SupportRow{}}
	if err := a.DB.QueryRow(ctx, `SELECT count(*)`+supportFrom+` WHERE `+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE platform_unread > 0), count(*) FILTER (WHERE resolved_at IS NULL)
		FROM support_threads`).Scan(&out.Unread, &out.Open); err != nil {
		return nil, err
	}
	limit, offset := f.limitOffset()
	args = append(args, limit, offset)
	rows, err := a.DB.Query(ctx, `SELECT `+supportCols+supportFrom+` WHERE `+where+
		fmt.Sprintf(` ORDER BY (t.platform_unread > 0) DESC, t.last_at DESC, t.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanSupport(rows)
		if err != nil {
			return nil, err
		}
		out.Chats = append(out.Chats, r)
	}
	return out, rows.Err()
}

// AdminSupportUnread is the number on the platform's "support" entry.
func (a *App) AdminSupportUnread(ctx context.Context) (int, error) {
	var n int
	err := a.DB.QueryRow(ctx, `SELECT COALESCE(sum(platform_unread),0) FROM support_threads`).Scan(&n)
	return n, err
}

// SupportDetail is what the platform sees beside one support conversation.
// There may be no conversation yet: the platform can write first.
type SupportDetail struct {
	SupportRow
	Started  *time.Time  `json:"started_at"`
	Orders   []ChatOrder `json:"orders"`
	Slug     string      `json:"slug,omitempty"`
	Merchant int64       `json:"merchant_id,omitempty"`
	Muted    bool        `json:"muted"`
	Since    time.Time   `json:"since"` // the customer's account, or the store, was made
	Enabled  bool        `json:"chat_enabled"`
}

func (a *App) AdminSupportChat(ctx context.Context, party string) (*SupportDetail, error) {
	kind, id, err := a.supportParty(ctx, party)
	if err != nil {
		return nil, err
	}
	out := &SupportDetail{}
	row, err := scanSupport(a.DB.QueryRow(ctx, `SELECT `+supportCols+supportFrom+` WHERE t.party=$1`, party))
	switch {
	case err == nil:
		out.SupportRow = row
		var started time.Time
		if err := a.DB.QueryRow(ctx, `SELECT created_at FROM support_threads WHERE id=$1`, row.ID).Scan(&started); err != nil {
			return nil, err
		}
		out.Started = &started
	case isNoRows(err):
		out.Party, out.Kind = party, kind
	default:
		return nil, err
	}
	if kind == "customer" {
		out.CustomerID = id
		err = a.DB.QueryRow(ctx, `SELECT COALESCE(NULLIF(c.nickname,''), (SELECT o.customer_name FROM orders o WHERE o.customer_id=c.id ORDER BY o.id DESC LIMIT 1), ''),
			c.phone, c.chat_muted_at IS NOT NULL, c.created_at FROM customers c WHERE c.id=$1`, id).Scan(&out.Name, &out.Phone, &out.Muted, &out.Since)
	} else {
		out.BranchID = id
		err = a.DB.QueryRow(ctx, `SELECT b.name, b.phone, b.slug, m.name, m.id, b.chat_enabled, b.created_at
			FROM branches b JOIN merchants m ON m.id=b.merchant_id WHERE b.id=$1`, id).
			Scan(&out.Name, &out.Phone, &out.Slug, &out.SupportRow.Merchant, &out.Merchant, &out.Enabled, &out.Since)
	}
	if err != nil {
		return nil, err
	}
	if out.Phone != "" {
		out.PhoneTail = last4(out.Phone)
	}
	if out.Orders, err = supportOrders(ctx, a.DB, kind, id); err != nil {
		return nil, err
	}
	if a.IM != nil {
		// the other side's account must exist to be written to
		if kind == "customer" {
			err = a.customerAccount(ctx, id)
		} else {
			err = a.storeAccount(ctx, id)
		}
	}
	return out, err
}

// AdminResolveSupport marks a support conversation dealt with, or open again.
func (a *App) AdminResolveSupport(ctx context.Context, actor AdminActor, party string, resolved bool) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `UPDATE support_threads SET resolved_at = CASE WHEN $2 THEN now() END,
			resolved_by = CASE WHEN $2 THEN $3::bigint END WHERE party=$1 RETURNING id`, party, resolved, actor.ID).Scan(&id)
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, map[bool]string{true: "support.resolve", false: "support.reopen"}[resolved], "support", id, map[string]any{"party": party})
	})
}

// supportCleanup is run daily with the chat's clean-ups.
var supportCleanup = []string{
	fmt.Sprintf(`DELETE FROM support_messages WHERE sent_at < now() - interval '%d days'`, chatKeepDays),
	fmt.Sprintf(`DELETE FROM support_images WHERE created_at < now() - interval '%d days'`, chatImageKeepDays),
	fmt.Sprintf(`DELETE FROM support_threads t WHERE t.last_at < now() - interval '%d days'
		AND NOT EXISTS (SELECT 1 FROM support_messages s WHERE s.thread_id=t.id)`, chatKeepDays),
}

// SupportThreadOf is the party's conversation with the platform as a row of
// the customer's message list, or nil when there is none yet.
func (a *App) SupportThreadOf(ctx context.Context, party string) (*ChatThread, error) {
	t := &ChatThread{Peer: platformIM}
	err := a.DB.QueryRow(ctx, `SELECT id, last_kind, last_text, CASE WHEN last_sender='platform' THEN 'store' ELSE 'customer' END,
			last_at, party_unread, msg_count, platform_read_at FROM support_threads WHERE party=$1`, party).
		Scan(&t.ID, &t.LastKind, &t.LastText, &t.LastSender, &t.LastAt, &t.Unread, &t.Count, &t.PeerReadAt)
	if isNoRows(err) {
		return nil, nil
	}
	return t, err
}

// CustomerParty and StoreParty are the accounts a support conversation is with.
func CustomerParty(id int64) string { return customerIM(id) }
func StoreParty(id int64) string    { return branchIM(id) }
