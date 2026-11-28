package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// The customer's say-so for an order the staff entered (a table, a WeChat
// order): the store texts the customer a code, the customer reads it out, and
// only then may the staff spend their store balance on the order or refund
// into it (docs/REFUND_BALANCE_DESIGN.md §5.3, §6.3). These codes are kept
// apart from the login codes, so a code told to the staff logs nobody in.

// CreditVerify is a texted code: ID says which request, Code is what the
// customer read out.
type CreditVerify struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
}

// CreditCodeSent is what the staff see after asking for a code.
type CreditCodeSent struct {
	ID    int64  `json:"id"`
	Phone string `json:"phone"` // masked
	// Balance is what the customer holds at the store (spend only).
	Balance int64 `json:"balance"`
	// DevCode is the code itself, only with OTP_DEV_ECHO (development).
	DevCode string `json:"dev_code,omitempty"`
}

const creditCodesPerOrder = 5

func (a *App) creditCodeHash(id int64, phone, code string) []byte {
	m := hmac.New(sha256.New, a.Secret)
	m.Write([]byte(fmt.Sprintf("credit:%d:%s:%s", id, phone, code)))
	return m.Sum(nil)
}

// RequestCreditCode texts the customer of an order (or the phone given, for a
// table without one) a code to use or fill their store balance.
func (a *App) RequestCreditCode(ctx context.Context, orderID int64, phone, purpose string, actor Actor) (*CreditCodeSent, error) {
	if purpose != "spend" && purpose != "refund" {
		return nil, ErrValidation("purpose is spend or refund")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return nil, err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	out := &CreditCodeSent{}
	var storeName string
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		o, err := lockOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		oc, err := readOrderCredit(ctx, tx, o.ID)
		if err != nil {
			return err
		}
		if oc.Type == TypePlatform {
			return ErrInvalidState("a delivery platform's order is paid on the platform")
		}
		if phone == "" {
			if err := tx.QueryRow(ctx, `SELECT customer_phone FROM orders WHERE id=$1`, o.ID).Scan(&phone); err != nil {
				return err
			}
		}
		if phone, err = normalizeCustomerPhone(phone); err != nil {
			return Err(http.StatusUnprocessableEntity, "CREDIT_PHONE", "enter the customer's mobile number")
		}
		var recent bool
		var today int
		if err := tx.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM wallet_verifications WHERE order_id=$1 AND created_at > now() - make_interval(secs => $2)),
			(SELECT count(*) FROM wallet_verifications WHERE order_id=$1 AND created_at > now() - interval '24 hours')`,
			o.ID, otpResendInterval.Seconds()).Scan(&recent, &today); err != nil {
			return err
		}
		if recent {
			return Err(http.StatusTooManyRequests, "OTP_WAIT", "please wait a minute before requesting another code")
		}
		if today >= creditCodesPerOrder {
			return Err(http.StatusTooManyRequests, "OTP_DAILY_LIMIT", "too many codes for this order today")
		}
		if purpose == "spend" {
			// what the order can be paid with: the refund balance and the
			// stored value, as spending counts them (storeCreditOf)
			bal, err := storeCreditOf(ctx, tx, oc.BranchID, phone, oc.Currency)
			if err != nil {
				return err
			}
			if out.Balance = bal; out.Balance <= 0 {
				return Err(http.StatusConflict, "CREDIT_NONE", "this customer has no balance at the store")
			}
		}
		var by *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if err := tx.QueryRow(ctx, `INSERT INTO wallet_verifications (order_id, phone, purpose, code_hash, requested_by, expires_at)
			VALUES ($1,$2,$3,'\x00',$4,$5) RETURNING id`, o.ID, phone, purpose, by, time.Now().Add(otpTTL)).Scan(&out.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE wallet_verifications SET code_hash=$2 WHERE id=$1`, out.ID,
			a.creditCodeHash(out.ID, phone, code)); err != nil {
			return err
		}
		out.Phone = maskPhone(phone)
		return tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(o.brand_name, ''), b.name) FROM orders o JOIN branches b ON b.id = o.branch_id
			WHERE o.id=$1`, o.ID).Scan(&storeName)
	})
	if err != nil {
		return nil, err
	}
	what := map[string][2]string{"spend": {"用你的店铺余额付款", "pay from your store balance"},
		"refund": {"把退款存入你的店铺余额", "put a refund into your store balance"}}[purpose]
	msg := fmt.Sprintf("%s 要%s，验证码 %s，5 分钟内有效，只告诉本店店员。%s asks to %s: code %s, valid 5 minutes. Only tell the store's staff.",
		storeName, what[0], code, storeName, what[1], code)
	if err := a.smsSender().Send(ctx, phone, msg); err != nil {
		log.Printf("store credit code: send: %v", err)
		return nil, Err(http.StatusBadGateway, "SMS_FAILED", "the code could not be sent")
	}
	if a.OTPDevEcho {
		out.DevCode = code
	}
	return out, nil
}

// useCreditVerify checks a texted code for the order (the order row locked)
// and returns the customer whose balance it names. A refund opens the
// customer's account if they have none yet; spending needs one.
func (a *App) useCreditVerify(ctx context.Context, tx pgx.Tx, orderID int64, purpose string, v CreditVerify) (int64, error) {
	bad := Err(http.StatusUnprocessableEntity, "OTP_INVALID", "the code is wrong or expired")
	var phone string
	var hash []byte
	var attempts int
	err := tx.QueryRow(ctx, `SELECT phone, code_hash, attempts FROM wallet_verifications WHERE id=$1 AND order_id=$2
		AND purpose=$3 AND consumed_at IS NULL AND expires_at > now()`, v.ID, orderID, purpose).
		Scan(&phone, &hash, &attempts)
	if isNoRows(err) {
		return 0, bad
	} else if err != nil {
		return 0, err
	}
	if attempts >= otpMaxAttempts {
		return 0, bad
	}
	if !hmac.Equal(hash, a.creditCodeHash(v.ID, phone, v.Code)) {
		// The wrong try is counted even though the request fails: it is
		// written outside this transaction (which holds no lock on the row:
		// the order's lock keeps two uses of a code apart).
		if _, err := a.DB.Exec(ctx, `UPDATE wallet_verifications SET attempts = attempts + 1 WHERE id=$1`, v.ID); err != nil {
			return 0, err
		}
		return 0, bad
	}
	tag, err := tx.Exec(ctx, `UPDATE wallet_verifications SET consumed_at=now() WHERE id=$1 AND consumed_at IS NULL
		AND attempts < $2`, v.ID, otpMaxAttempts)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, bad
	}
	var cust int64
	if purpose == "refund" {
		err = tx.QueryRow(ctx, `INSERT INTO customers (phone) VALUES ($1)
			ON CONFLICT (phone) DO UPDATE SET phone = EXCLUDED.phone RETURNING id`, phone).Scan(&cust)
	} else {
		err = tx.QueryRow(ctx, `SELECT id FROM customers WHERE phone=$1`, phone).Scan(&cust)
		if isNoRows(err) {
			return 0, Err(http.StatusConflict, "CREDIT_NONE", "this customer has no balance at the store")
		}
	}
	return cust, err
}

// SpendCreditByCode pays an order the staff entered from the store balance
// of the customer whose texted code they typed in: as much as the balance
// holds, up to what is still to pay.
func (a *App) SpendCreditByCode(ctx context.Context, orderID int64, v CreditVerify, actor Actor) (*Order, error) {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		o, err := lockOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if o.Payment == "paid" || o.Acceptance == "rejected" || o.Acceptance == "cancelled" {
			return ErrInvalidState("the order is already paid or closed")
		}
		cust, err := a.useCreditVerify(ctx, tx, o.ID, "spend", v)
		if err != nil {
			return err
		}
		used, err := useCredit(ctx, tx, o.ID, cust, o.Total, actor)
		if err != nil {
			return err
		}
		if used == 0 {
			return Err(http.StatusConflict, "CREDIT_NONE", "this customer has no balance at the store")
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.credit_used", map[string]any{"amount": used}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
	if err != nil {
		return nil, err
	}
	return a.GetOrder(ctx, orderID)
}
