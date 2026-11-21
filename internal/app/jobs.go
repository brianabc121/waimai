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
)

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

// customerMessages are the status texts sent to customers, Chinese then English.
var customerMessages = map[string][2]string{
	"accepted":         {"商家已接单，正在备餐", "The store accepted your order and is preparing it"},
	"out_for_delivery": {"骑手已取餐，正在配送", "Your order is on the way"},
	"ready_for_pickup": {"可以取餐了", "Your order is ready for pickup"},
	"rejected":         {"商家未能接单，订单已关闭", "The store could not take your order"},
	"cancelled":        {"订单已取消", "Your order was cancelled"},
}

// notifyCustomer enqueues a status SMS in the caller's transaction when the
// branch has customer notifications switched on. The message is only sent if
// the business change commits.
func (a *App) notifyCustomer(ctx context.Context, tx pgx.Tx, orderID int64, event string) error {
	text, ok := customerMessages[event]
	if !ok {
		return nil
	}
	var enabled bool
	var store, phone, publicNo, pickupCode string
	var nonce []byte
	if err := tx.QueryRow(ctx, `SELECT b.notify_sms, b.name, o.customer_phone, o.public_no, o.pickup_code, o.token_nonce
		FROM orders o JOIN branches b ON b.id = o.branch_id WHERE o.id=$1`, orderID).
		Scan(&enabled, &store, &phone, &publicNo, &pickupCode, &nonce); err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	msg := fmt.Sprintf("【%s】%s %s。%s.", store, publicNo, text[0], text[1])
	if event == "ready_for_pickup" {
		msg += fmt.Sprintf(" 取餐号 Pickup code: %s.", pickupCode)
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
		RETURNING id, kind, payload, attempts, lease_token::text`, jobLease.Seconds()).
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
		return a.SMS.Send(ctx, p.Phone, p.Message)
	default:
		return fmt.Errorf("unknown job kind %q", j.Kind)
	}
}

// RunJobs processes due jobs until none are left. External calls happen
// outside any database transaction.
func (a *App) RunJobs(ctx context.Context) error {
	for ctx.Err() == nil {
		j, err := a.claimJob(ctx)
		if err != nil {
			return err
		}
		if j == nil {
			return nil
		}
		runErr := a.runJob(ctx, j)
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
			return err
		}
	}
	return ctx.Err()
}
