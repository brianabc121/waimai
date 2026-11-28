package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	jobLease       = 60 * time.Second
	jobMaxAttempts = 5
	// A long job (a menu translation can take minutes) renews its lease this
	// often, so that no other worker takes it for abandoned and runs it again.
	jobRenew = 20 * time.Second
	// One turn of the job loop stops after this many jobs or this long and
	// lets the next turn carry on, so that the turn's outcome gets recorded.
	jobBatch     = 50
	jobBatchTime = 30 * time.Second
)

func (a *App) jobLease() time.Duration {
	if a.JobLease > 0 {
		return a.JobLease
	}
	return jobLease
}

// keepLease renews a job's lease until stop is called. A renewal that finds
// the lease gone (another worker has the job) cancels the job's context.
func (a *App) keepLease(ctx context.Context, j *claimedJob, every time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			tag, err := a.DB.Exec(ctx, `UPDATE jobs SET locked_until = now() + make_interval(secs => $3)
				WHERE id=$1 AND lease_token::text=$2 AND status='pending'`, j.ID, j.Token, a.jobLease().Seconds())
			if err == nil && tag.RowsAffected() == 0 {
				cancel()
				return
			}
		}
	}()
	return ctx, func() { cancel(); <-done }
}

type smsJob struct {
	Phone   string `json:"phone"`
	Message string `json:"message"`
}

func enqueue(ctx context.Context, tx pgx.Tx, kind string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs (kind, payload) VALUES ($1, $2)`, kind, b)
	return err
}

// notifyCustomer leaves a message in the customer's message box and, when the
// branch has customer notifications switched on, enqueues a status SMS in the
// language the customer ordered in (docs/I18N_DESIGN.md §10). The message is
// only sent if the business change commits.
func (a *App) notifyCustomer(ctx context.Context, tx pgx.Tx, orderID int64, event string) error {
	key := "sys.order." + event
	if _, ok := sysTexts[key]; !ok {
		return nil
	}
	var enabled bool
	var branchID int64
	var store, phone, publicNo, pickupCode, lang string
	var nonce []byte
	var customerID *int64
	if err := tx.QueryRow(ctx, `SELECT b.id, b.notify_sms, b.name, o.customer_phone, o.public_no, o.pickup_code, o.token_nonce,
		o.customer_id, o.customer_lang FROM orders o JOIN branches b ON b.id = o.branch_id WHERE o.id=$1`, orderID).
		Scan(&branchID, &enabled, &store, &phone, &publicNo, &pickupCode, &nonce, &customerID, &lang); err != nil {
		return err
	}
	// The customer's message box gets every update, whether or not the store texts.
	if customerID != nil {
		m := Message{Kind: "order", Title: sysTexts[key]["zh"], TitleEn: sysTexts[key]["en"], Body: store + " · " + publicNo,
			Link: "/o/" + a.orderToken(nonce), Key: "order." + event, Args: map[string]string{"store": store, "no": publicNo}}
		m.BodyEn = m.Body
		if event == "ready_for_pickup" {
			m.Body, m.BodyEn = m.Body+" · 取餐号 "+pickupCode, m.BodyEn+" · pickup code "+pickupCode
			m.Args["code"] = pickupCode
		}
		if err := tell(ctx, tx, *customerID, m); err != nil {
			return err
		}
	}
	if !enabled {
		return nil
	}
	if names, err := storeNames(ctx, tx, lang, []int64{branchID}); err == nil && names[branchID] != "" {
		store = names[branchID]
	}
	msg := a.sysTextIn(ctx, tx, lang, "sys.sms.order", "store", store, "no", publicNo, "status", a.sysTextIn(ctx, tx, lang, key))
	if event == "ready_for_pickup" {
		msg += " " + a.sysTextIn(ctx, tx, lang, "sys.sms.pickup", "code", pickupCode)
	}
	if a.PublicBaseURL != "" {
		msg += " " + strings.TrimRight(a.PublicBaseURL, "/") + "/o/" + a.orderToken(nonce)
	}
	return enqueue(ctx, tx, "sms", smsJob{Phone: phone, Message: msg})
}

type claimedJob struct {
	ID       int64
	Kind     string
	Payload  []byte
	Attempts int
	Token    string
}

// claimJob leases one due job in a short transaction. SKIP LOCKED lets several
// workers pull different jobs; the lease token guards completion so that a
// worker whose lease expired cannot finish a job another worker re-claimed.
func (a *App) claimJob(ctx context.Context) (*claimedJob, error) {
	var j claimedJob
	err := a.DB.QueryRow(ctx, `UPDATE jobs SET attempts = attempts + 1, lease_token = gen_random_uuid(),
		locked_until = now() + make_interval(secs => $1)
		WHERE id = (SELECT id FROM jobs WHERE status='pending' AND run_at <= now()
			AND (locked_until IS NULL OR locked_until < now()) ORDER BY run_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id, kind, payload, attempts, lease_token::text`, a.jobLease().Seconds()).
		Scan(&j.ID, &j.Kind, &j.Payload, &j.Attempts, &j.Token)
	if isNoRows(err) {
		return nil, nil
	}
	return &j, err
}

func (a *App) runJob(ctx context.Context, j *claimedJob) error {
	switch j.Kind {
	case "sms":
		var p smsJob
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return a.smsSender().Send(ctx, p.Phone, p.Message)
	case "push":
		var p pushJob
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			return err
		}
		return a.sendPush(ctx, p)
	case "chat_alert":
		var p chatAlertJob
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			return err
		}
		return a.runChatAlert(ctx, p)
	case "support_alert":
		var p supportAlertJob
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			return err
		}
		return a.runSupportAlert(ctx, p)
	case "menu_import":
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			return err
		}
		return a.parseMenuImport(ctx, p.ID)
	case "translate_menu":
		a.freshSettings(ctx)
		var p translateJob
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		return a.TranslateMenu(ctx, p.BranchID, p.Force)
	case "translate_platform":
		a.freshSettings(ctx)
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		return a.TranslatePlatform(ctx, false)
	default:
		return fmt.Errorf("unknown job kind %q", j.Kind)
	}
}

// RunJobs processes due jobs until none are left. External calls happen
// outside any database transaction.
func (a *App) RunJobs(ctx context.Context) error {
	for {
		n, err := a.runJobs(ctx)
		if err != nil || n == 0 {
			return err
		}
	}
}

// runJobs is RunJobs that also reports how many jobs it ran.
func (a *App) runJobs(ctx context.Context) (int, error) {
	n := 0
	start := time.Now()
	for ctx.Err() == nil && n < jobBatch && time.Since(start) < jobBatchTime {
		j, err := a.claimJob(ctx)
		if err != nil {
			return n, err
		}
		if j == nil {
			return n, nil
		}
		n++
		renew := a.JobRenewEvery
		if renew == 0 {
			renew = jobRenew
		}
		jctx, stop := a.keepLease(ctx, j, renew)
		runErr := a.runJob(jctx, j)
		stop()
		switch {
		case runErr == nil:
			_, err = a.DB.Exec(ctx, `UPDATE jobs SET status='done', finished_at=now(), locked_until=NULL, last_error=''
				WHERE id=$1 AND lease_token::text=$2`, j.ID, j.Token)
		case j.Attempts >= jobMaxAttempts:
			_, err = a.DB.Exec(ctx, `UPDATE jobs SET status='failed', finished_at=now(), locked_until=NULL, last_error=$3
				WHERE id=$1 AND lease_token::text=$2`, j.ID, j.Token, runErr.Error())
		default:
			// Exponential backoff: 30s, 60s, 120s, 240s.
			delay := 30 * time.Second << (j.Attempts - 1)
			_, err = a.DB.Exec(ctx, `UPDATE jobs SET locked_until=NULL, last_error=$3,
				run_at = now() + make_interval(secs => $4) WHERE id=$1 AND lease_token::text=$2`,
				j.ID, j.Token, runErr.Error(), delay.Seconds())
		}
		if err != nil {
			return n, err
		}
	}
	return n, ctx.Err()
}
