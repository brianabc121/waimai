package app

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The store's reconciliation page (docs/CHECKOUT_DISCOUNT_DESIGN.md §8): what
// came in by payment method, the owner's check of each online payment, the
// cash counted in the drawer, and who gave which discounts. A day is the
// store's own day; an order counts on the day it was paid.

// payTypeSQL is how an order was paid; older orders without a recorded type
// were cash, unless they were paid online before they were placed.
const payTypeSQL = `COALESCE(NULLIF(o.pay_type, ''), CASE WHEN o.payment_method = 'prepaid' THEN 'online' ELSE 'cash' END)`

// A platform order the platform cancelled brings nothing: the platform does
// not pay for it (docs/ORDER_TYPES_DESIGN.md §7.4).
const paidInPeriod = `o.branch_id = $1 AND o.payment_status = 'paid' AND o.paid_at IS NOT NULL
	AND (o.paid_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date
	AND NOT (o.order_type = 'platform' AND o.acceptance_status IN ('cancelled', 'rejected'))`

// ReconRow is one payment method's takings.
type ReconRow struct {
	PayType   string `json:"pay_type"`
	ChannelID int64  `json:"channel_id"`
	// PlatformID is the delivery platform of a "platform" row, whose amount is
	// what the platform owes the store (receivableSQL).
	PlatformID int64             `json:"platform_id"`
	Name       string            `json:"name"`
	Names      map[string]string `json:"names"`
	Count      int               `json:"count"`
	Amount     int64             `json:"amount"`
	Dine       int64             `json:"dine"`
	Takeaway   int64             `json:"takeaway"` // delivery and pickup
	// online only: how many the owner found arrived, found wrong, or has not checked
	OK        int `json:"ok"`
	Problem   int `json:"problem"`
	Unchecked int `json:"unchecked"`
}

// ReconCash is where the cash of the period is.
type ReconCash struct {
	Counter        int64 `json:"counter"`         // paid in cash at the store: tables and pickups
	RiderCollected int64 `json:"rider_collected"` // riders took in cash at the door
	RiderReturned  int64 `json:"rider_returned"`  // of which already handed in and checked
	RiderPending   int64 `json:"rider_pending"`   // still with the riders or not checked
	// Refunded is cash given back for dishes in the period.
	Refunded int64 `json:"refunded"`
	// Expected is what the drawer should hold: cash at the store plus what
	// riders handed in, less cash refunds.
	Expected int64 `json:"expected"`
}

// ReconRefunds is money given back by dish in the period, whenever the
// order was paid (docs/PRICING_REVIEW.md, problem 7).
type ReconRefunds struct {
	Count int   `json:"count"`
	Total int64 `json:"total"`
	Cash  int64 `json:"cash"`
}

// CashCount is the cash counted in the drawer on one day.
type CashCount struct {
	Day       string    `json:"day"`
	Expected  int64     `json:"expected"`
	Counted   int64     `json:"counted"`
	Note      string    `json:"note"`
	CountedBy string    `json:"counted_by"`
	At        time.Time `json:"at"`
}

// ReconDiscounts is what the store gave away in the period.
type ReconDiscounts struct {
	Promotion int64 `json:"promotion"`
	Coupon    int64 `json:"coupon"`
	Staff     int64 `json:"staff"`     // percentage off at checkout
	RoundOff  int64 `json:"round_off"` // round-offs and typed amounts
	// PlatformMerchant is the store's own offers on delivery platforms;
	// PlatformSubsidy the platforms' discounts on them, which the platforms pay
	// and so are not part of Total.
	PlatformMerchant int64 `json:"platform_merchant"`
	PlatformSubsidy  int64 `json:"platform_subsidy"`
	Total            int64 `json:"total"`
}

type Reconcile struct {
	Timezone  string         `json:"timezone"`
	From      string         `json:"from"`
	To        string         `json:"to"`
	Currency  string         `json:"currency"`
	Count     int            `json:"count"`
	Total     int64          `json:"total"`
	Rows      []ReconRow     `json:"rows"`
	Cash      ReconCash      `json:"cash"`
	Discounts ReconDiscounts `json:"discounts"`
	Refunds   ReconRefunds   `json:"refunds"`
	// Net is Total less the refunds.
	Net int64 `json:"net"`
	// Problems counts online payments marked wrong, in any period.
	Problems int `json:"problems"`
	// CashCount is the day's count when the period is one day.
	CashCount *CashCount `json:"cash_count"`
	// Platforms is the delivery platforms' block; Types the takings by order type.
	Platforms []ReconPlatform `json:"platforms"`
	Types     []ReconType     `json:"types"`
}

// Reconciliation sums up a period's takings by payment method.
func (a *App) Reconciliation(ctx context.Context, branchID int64, from, to string) (*Reconcile, error) {
	if err := billDays(from, to); err != nil {
		return nil, err
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	out := &Reconcile{Timezone: b.Timezone, From: from, To: to, Currency: b.Currency, Rows: []ReconRow{}}
	rows, err := a.DB.Query(ctx, `SELECT `+payTypeSQL+` AS pt, COALESCE(o.pay_channel_id, 0),
			CASE WHEN `+payTypeSQL+` = 'platform' THEN COALESCE(o.platform_id, 0) ELSE 0 END,
			COALESCE(max(c.name), max(dp.name), max(o.pay_method), ''), COALESCE(max(c.names::text), max(dp.names::text), '{}'),
			count(*), COALESCE(sum(`+receivableSQL+`), 0),
			COALESCE(sum(`+receivableSQL+`) FILTER (WHERE o.service_type = 'dine_in'), 0),
			COALESCE(sum(`+receivableSQL+`) FILTER (WHERE o.service_type <> 'dine_in'), 0),
			count(*) FILTER (WHERE pc.status = 'ok'), count(*) FILTER (WHERE pc.status = 'problem'),
			count(*) FILTER (WHERE pc.status IS NULL)
		FROM orders o JOIN branches b ON b.id = o.branch_id
			LEFT JOIN pay_channels c ON c.id = o.pay_channel_id
			LEFT JOIN payment_checks pc ON pc.order_id = o.id
			LEFT JOIN delivery_platforms dp ON dp.id = o.platform_id
		WHERE `+paidInPeriod+`
		GROUP BY 1, 2, 3 ORDER BY CASE `+payTypeSQL+` WHEN 'cash' THEN 0 WHEN 'card' THEN 1 WHEN 'online' THEN 2 ELSE 3 END,
			min(COALESCE(c.sort, dp.sort, 999)), 2, 3`, branchID, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r ReconRow
		var names string
		if err := rows.Scan(&r.PayType, &r.ChannelID, &r.PlatformID, &r.Name, &names, &r.Count, &r.Amount, &r.Dine, &r.Takeaway,
			&r.OK, &r.Problem, &r.Unchecked); err != nil {
			rows.Close()
			return nil, err
		}
		r.Names = jsonNames(names)
		if r.PayType != "online" {
			r.OK, r.Problem, r.Unchecked = 0, 0, 0
			if r.PayType != "platform" {
				r.Name = ""
			}
		}
		out.Count += r.Count
		out.Total += r.Amount
		out.Rows = append(out.Rows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// cash: at the counter, and what riders took in and handed back
	if err := a.DB.QueryRow(ctx, `SELECT
			COALESCE(sum(o.total) FILTER (WHERE `+payTypeSQL+` = 'cash' AND cc.id IS NULL), 0),
			COALESCE(sum(cc.amount_collected), 0),
			COALESCE(sum(COALESCE(cc.amount_returned, cc.amount_collected)) FILTER (WHERE cc.reconcile_status <> 'pending'), 0),
			COALESCE(sum(cc.amount_collected) FILTER (WHERE cc.reconcile_status = 'pending'), 0),
			COALESCE(sum(GREATEST(0, o.discount - o.platform_subsidy - COALESCE((SELECT sum(d.amount) FROM order_discounts d
				WHERE d.order_id = o.id AND d.kind NOT IN ('promotion','dish_promotion') AND d.funded_by = 'store'), 0))), 0),
			COALESCE(sum(o.staff_discount), 0), COALESCE(sum(o.round_off), 0)
		FROM orders o JOIN branches b ON b.id = o.branch_id
			LEFT JOIN cash_collections cc ON cc.order_id = o.id
		WHERE `+paidInPeriod, branchID, from, to).Scan(&out.Cash.Counter, &out.Cash.RiderCollected, &out.Cash.RiderReturned,
		&out.Cash.RiderPending, &out.Discounts.Promotion, &out.Discounts.Staff, &out.Discounts.RoundOff); err != nil {
		return nil, err
	}
	if err := a.DB.QueryRow(ctx, `SELECT count(*), COALESCE(sum(r.amount), 0),
			COALESCE(sum(r.amount) FILTER (WHERE r.pay_type = 'cash'), 0)
		FROM order_refunds r JOIN branches b ON b.id = r.branch_id
		WHERE r.branch_id = $1 AND (r.created_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date`,
		branchID, from, to).Scan(&out.Refunds.Count, &out.Refunds.Total, &out.Refunds.Cash); err != nil {
		return nil, err
	}
	out.Cash.Refunded = out.Refunds.Cash
	out.Net = out.Total - out.Refunds.Total
	out.Cash.Expected = out.Cash.Counter + out.Cash.RiderReturned - out.Cash.Refunded
	// coupons the store paid for
	if err := a.DB.QueryRow(ctx, `SELECT COALESCE(sum(d.amount), 0) FROM order_discounts d
		JOIN orders o ON o.id = d.order_id JOIN branches b ON b.id = o.branch_id
		WHERE `+paidInPeriod+` AND d.kind NOT IN ('promotion','dish_promotion','platform_merchant_promo') AND d.funded_by = 'store'`,
		branchID, from, to).Scan(&out.Discounts.Coupon); err != nil {
		return nil, err
	}
	// the two discounts of platform orders
	if err := a.DB.QueryRow(ctx, `SELECT COALESCE(sum(d.amount) FILTER (WHERE d.kind = 'platform_merchant_promo'), 0),
			COALESCE(sum(d.amount) FILTER (WHERE d.kind = 'platform_promo'), 0)
		FROM order_discounts d JOIN orders o ON o.id = d.order_id JOIN branches b ON b.id = o.branch_id
		WHERE `+paidInPeriod+` AND o.order_type = 'platform'`, branchID, from, to).
		Scan(&out.Discounts.PlatformMerchant, &out.Discounts.PlatformSubsidy); err != nil {
		return nil, err
	}
	out.Discounts.Total = out.Discounts.Promotion + out.Discounts.Coupon + out.Discounts.Staff + out.Discounts.RoundOff +
		out.Discounts.PlatformMerchant
	if out.Platforms, err = platformRecon(ctx, a.DB, branchID, from, to); err != nil {
		return nil, err
	}
	if out.Types, err = typeRecon(ctx, a.DB, branchID, from, to); err != nil {
		return nil, err
	}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM payment_checks WHERE branch_id=$1 AND status='problem'`, branchID).
		Scan(&out.Problems); err != nil {
		return nil, err
	}
	if from == to {
		out.CashCount, err = a.cashCount(ctx, a.DB, branchID, from)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func jsonNames(s string) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func (a *App) cashCount(ctx context.Context, q Querier, branchID int64, day string) (*CashCount, error) {
	var c CashCount
	var at time.Time
	err := q.QueryRow(ctx, `SELECT to_char(k.day, 'YYYY-MM-DD'), k.expected, k.counted, k.note,
			COALESCE(u.display_name, ''), k.created_at
		FROM cash_counts k LEFT JOIN users u ON u.id = k.counted_by WHERE k.branch_id=$1 AND k.day=$2::date`, branchID, day).
		Scan(&c.Day, &c.Expected, &c.Counted, &c.Note, &c.CountedBy, &at)
	if isNoRows(err) {
		return nil, nil
	}
	c.At = at
	return &c, err
}

// SaveCashCount records the cash counted in the drawer on one day. What the
// drawer should hold is worked out again now, so a late payment is included.
func (a *App) SaveCashCount(ctx context.Context, branchID int64, day string, counted int64, note string, actor Actor) (*CashCount, error) {
	if err := billDays(day, day); err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	if counted < 0 || len([]rune(note)) > 200 {
		return nil, ErrValidation("invalid count")
	}
	r, err := a.Reconciliation(ctx, branchID, day, day)
	if err != nil {
		return nil, err
	}
	var by *int64
	if actor.ID != 0 {
		by = &actor.ID
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO cash_counts (branch_id, day, expected, counted, note, counted_by)
			VALUES ($1, $2::date, $3, $4, $5, $6)
			ON CONFLICT (branch_id, day) DO UPDATE SET expected=$3, counted=$4, note=$5, counted_by=$6, created_at=now()`,
			branchID, day, r.Cash.Expected, counted, note, by); err != nil {
			return err
		}
		return audit(ctx, tx, branchID, 0, actor, "cash.counted", map[string]any{"day": day, "expected": r.Cash.Expected,
			"counted": counted, "note": note})
	})
	if err != nil {
		return nil, err
	}
	return a.cashCount(ctx, a.DB, branchID, day)
}

// OnlinePayment is one order paid online, with the owner's check.
type OnlinePayment struct {
	OrderID     int64             `json:"order_id"`
	PublicNo    string            `json:"public_no"`
	PaidAt      time.Time         `json:"paid_at"`
	ServiceType string            `json:"service_type"`
	TableName   string            `json:"table_name"`
	ChannelID   int64             `json:"channel_id"`
	Method      string            `json:"method"`
	Names       map[string]string `json:"names"`
	Amount      int64             `json:"amount"`
	Staff       string            `json:"staff"`  // the rider, or who placed it
	Status      string            `json:"status"` // "", ok, problem
	Note        string            `json:"note"`
	CheckedBy   string            `json:"checked_by"`
	CheckedAt   *time.Time        `json:"checked_at"`
}

// OnlinePayments lists a period's online payments; channelID and status
// ("", ok, problem, unchecked) narrow it down.
func (a *App) OnlinePayments(ctx context.Context, branchID int64, from, to string, channelID int64, status string) ([]OnlinePayment, error) {
	if err := billDays(from, to); err != nil {
		return nil, err
	}
	cond := ""
	switch status {
	case "":
	case "ok", "problem":
		cond = ` AND pc.status = '` + status + `'`
	case "unchecked":
		cond = ` AND pc.status IS NULL`
	default:
		return nil, ErrValidation("unknown status")
	}
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.public_no, o.paid_at, o.service_type, o.table_name,
			COALESCE(o.pay_channel_id, 0), COALESCE(c.name, o.pay_method), COALESCE(c.names::text, '{}'), o.total,
			COALESCE((SELECT u.display_name FROM delivery_tasks t JOIN users u ON u.id = t.rider_id
				WHERE t.order_id = o.id AND t.status = 'delivered'), (SELECT u.display_name FROM users u WHERE u.id = o.placed_by), ''),
			COALESCE(pc.status, ''), COALESCE(pc.note, ''), COALESCE(cu.display_name, ''), pc.checked_at
		FROM orders o JOIN branches b ON b.id = o.branch_id
			LEFT JOIN pay_channels c ON c.id = o.pay_channel_id
			LEFT JOIN payment_checks pc ON pc.order_id = o.id
			LEFT JOIN users cu ON cu.id = pc.checked_by
		WHERE `+paidInPeriod+` AND `+payTypeSQL+` = 'online' AND ($4 = 0 OR o.pay_channel_id = $4)`+cond+`
		ORDER BY o.paid_at DESC, o.id DESC LIMIT 2000`, branchID, from, to, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OnlinePayment{}
	for rows.Next() {
		var p OnlinePayment
		var names string
		if err := rows.Scan(&p.OrderID, &p.PublicNo, &p.PaidAt, &p.ServiceType, &p.TableName, &p.ChannelID, &p.Method, &names,
			&p.Amount, &p.Staff, &p.Status, &p.Note, &p.CheckedBy, &p.CheckedAt); err != nil {
			return nil, err
		}
		p.Names = jsonNames(names)
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetPaymentChecks marks online payments as arrived ("ok"), wrong
// ("problem", with a note) or not checked yet ("").
func (a *App) SetPaymentChecks(ctx context.Context, branchID int64, orderIDs []int64, status, note string, actor Actor) error {
	note = strings.TrimSpace(note)
	switch {
	case len(orderIDs) == 0 || len(orderIDs) > 2000:
		return ErrValidation("choose the payments to mark")
	case status != "" && status != "ok" && status != "problem":
		return ErrValidation("status must be ok or problem")
	case status == "problem" && note == "":
		return ErrValidation("say what is wrong")
	case len([]rune(note)) > 200:
		return ErrValidation("the note is at most 200 characters")
	}
	if status != "problem" {
		note = ""
	}
	var by *int64
	if actor.ID != 0 {
		by = &actor.ID
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM orders o WHERE o.id = ANY($1) AND o.branch_id = $2
			AND o.payment_status = 'paid' AND `+payTypeSQL+` = 'online'`, orderIDs, branchID).Scan(&n); err != nil {
			return err
		}
		if n != len(orderIDs) {
			return ErrValidation("only this store's paid online payments can be checked")
		}
		if status == "" {
			if _, err := tx.Exec(ctx, `DELETE FROM payment_checks WHERE order_id = ANY($1)`, orderIDs); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `INSERT INTO payment_checks (order_id, branch_id, status, note, checked_by)
			SELECT unnest($1::bigint[]), $2, $3, $4, $5
			ON CONFLICT (order_id) DO UPDATE SET status=$3, note=$4, checked_by=$5, checked_at=now()`,
			orderIDs, branchID, status, note, by); err != nil {
			return err
		}
		for _, id := range orderIDs {
			if err := audit(ctx, tx, branchID, id, actor, "payment.checked", map[string]any{"status": status, "note": note}); err != nil {
				return err
			}
		}
		return notify(ctx, tx, Event{BranchID: branchID})
	})
}

// PaymentCheck is an order's check on the order page.
type PaymentCheck struct {
	Status    string    `json:"status"`
	Note      string    `json:"note"`
	CheckedBy string    `json:"checked_by"`
	CheckedAt time.Time `json:"checked_at"`
}

func (a *App) OrderPaymentCheck(ctx context.Context, orderID int64) (*PaymentCheck, error) {
	var p PaymentCheck
	err := a.DB.QueryRow(ctx, `SELECT pc.status, pc.note, COALESCE(u.display_name, ''), pc.checked_at
		FROM payment_checks pc LEFT JOIN users u ON u.id = pc.checked_by WHERE pc.order_id=$1`, orderID).
		Scan(&p.Status, &p.Note, &p.CheckedBy, &p.CheckedAt)
	if isNoRows(err) {
		return nil, nil
	}
	return &p, err
}

// DiscountEntry is one order's discount at checkout.
type DiscountEntry struct {
	OrderID     int64     `json:"order_id"`
	PublicNo    string    `json:"public_no"`
	At          time.Time `json:"at"`
	ServiceType string    `json:"service_type"`
	TableName   string    `json:"table_name"`
	PercentBP   int       `json:"percent_bp"`
	Mode        string    `json:"mode"`
	Staff       int64     `json:"staff"`
	RoundOff    int64     `json:"round_off"`
	Total       int64     `json:"total"`
	Reason      string    `json:"reason"`
	By          string    `json:"by"`
	ApprovedBy  string    `json:"approved_by"`
}

type DiscountByStaff struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Amount int64  `json:"amount"`
}

type DiscountByPromo struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	Scope  string `json:"scope"`
	Count  int    `json:"count"`
	Amount int64  `json:"amount"`
}

type DiscountLog struct {
	Entries []DiscountEntry   `json:"entries"`
	ByStaff []DiscountByStaff `json:"by_staff"`
	ByPromo []DiscountByPromo `json:"by_promo"`
}

// Discounts lists a period's discounts at checkout, totals them by who gave
// them, and totals the promotions (dine-in and delivery apart).
func (a *App) Discounts(ctx context.Context, branchID int64, from, to string) (*DiscountLog, error) {
	if err := billDays(from, to); err != nil {
		return nil, err
	}
	out := &DiscountLog{Entries: []DiscountEntry{}, ByStaff: []DiscountByStaff{}, ByPromo: []DiscountByPromo{}}
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.public_no, o.paid_at, o.service_type, o.table_name, o.adj_percent_bp,
			o.adj_mode, o.staff_discount, o.round_off, o.total, o.adj_reason,
			COALESCE((SELECT u.display_name FROM users u WHERE u.id = o.adj_by), ''),
			COALESCE((SELECT u.display_name FROM users u WHERE u.id = o.adj_approved_by), '')
		FROM orders o JOIN branches b ON b.id = o.branch_id
		WHERE `+paidInPeriod+` AND (o.adj_percent_bp > 0 OR o.adj_mode <> '')
		ORDER BY o.paid_at DESC, o.id DESC LIMIT 2000`, branchID, from, to)
	if err != nil {
		return nil, err
	}
	byStaff := map[string]*DiscountByStaff{}
	var order []string
	for rows.Next() {
		var e DiscountEntry
		if err := rows.Scan(&e.OrderID, &e.PublicNo, &e.At, &e.ServiceType, &e.TableName, &e.PercentBP, &e.Mode, &e.Staff,
			&e.RoundOff, &e.Total, &e.Reason, &e.By, &e.ApprovedBy); err != nil {
			rows.Close()
			return nil, err
		}
		out.Entries = append(out.Entries, e)
		s := byStaff[e.By]
		if s == nil {
			s = &DiscountByStaff{Name: e.By}
			byStaff[e.By] = s
			order = append(order, e.By)
		}
		s.Count++
		s.Amount += e.Staff + e.RoundOff
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, n := range order {
		out.ByStaff = append(out.ByStaff, *byStaff[n])
	}
	slices.SortStableFunc(out.ByStaff, func(x, y DiscountByStaff) int { return cmp.Compare(y.Amount, x.Amount) })
	prs, err := a.DB.Query(ctx, `SELECT p.id, p.name, p.name_en, p.scope, count(*),
			COALESCE(sum(GREATEST(0, o.discount - o.platform_subsidy - COALESCE((SELECT sum(d.amount) FROM order_discounts d
				WHERE d.order_id = o.id AND d.kind <> 'promotion' AND d.funded_by = 'store'), 0))), 0)
		FROM orders o JOIN branches b ON b.id = o.branch_id JOIN promotions p ON p.id = o.promotion_id
		WHERE `+paidInPeriod+`
		GROUP BY p.id ORDER BY p.scope, 6 DESC`, branchID, from, to)
	if err != nil {
		return nil, err
	}
	defer prs.Close()
	for prs.Next() {
		var p DiscountByPromo
		if err := prs.Scan(&p.ID, &p.Name, &p.NameEn, &p.Scope, &p.Count, &p.Amount); err != nil {
			return nil, err
		}
		out.ByPromo = append(out.ByPromo, p)
	}
	return out, prs.Err()
}
