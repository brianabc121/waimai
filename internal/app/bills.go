package app

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Bills (docs/COUPON_DESIGN.md §8, §9): every delivery and pickup order of a
// period with what each discount took off, the sums per kind of discount, and
// the monthly statements of what the platform owes a store for its coupons.
// Days are the store's own days.

const billMaxDays = 366

// BillDiscount is one discount on one order, with where its coupon came from.
type BillDiscount struct {
	OrderDiscount
	Source string `json:"source"` // repurchase, claim, new_customer, manual; "" for a promotion
}

// BillOrder is one row of the bill. State is completed, cancelled, failed
// (delivery did not happen) or open; only completed orders count in the sums,
// and the others show 0 for what was paid.
type BillOrder struct {
	ID             int64          `json:"id"`
	PublicNo       string         `json:"public_no"`
	CreatedAt      time.Time      `json:"created_at"`
	ServiceType    string         `json:"service_type"`
	State          string         `json:"state"`
	Food           int64          `json:"food"` // the dishes at full price
	DeliveryFee    int64          `json:"delivery_fee"`
	Promotion      int64          `json:"promotion"`
	StoreFree      int64          `json:"store_free"`
	StoreThreshold int64          `json:"store_threshold"`
	StoreDelivery  int64          `json:"store_delivery"`
	PlatformFree   int64          `json:"platform_free"`
	Paid           int64          `json:"paid"`    // what the customer paid
	Takings        int64          `json:"takings"` // paid + the platform's coupon
	Discounts      []BillDiscount `json:"discounts"`
}

func (o *BillOrder) add(x *BillOrder) {
	o.Food += x.Food
	o.DeliveryFee += x.DeliveryFee
	o.Promotion += x.Promotion
	o.StoreFree += x.StoreFree
	o.StoreThreshold += x.StoreThreshold
	o.StoreDelivery += x.StoreDelivery
	o.PlatformFree += x.PlatformFree
	o.Paid += x.Paid
	o.Takings += x.Takings
}

type BillList struct {
	Timezone string      `json:"timezone"`
	From     string      `json:"from"`
	To       string      `json:"to"`
	Currency string      `json:"currency"`
	Orders   []BillOrder `json:"orders"`
	Sum      BillOrder   `json:"sum"` // completed orders only
	Count    int         `json:"count"`
	Shown    int         `json:"shown"` // rows are cut at 2000
}

const billState = `CASE WHEN o.fulfillment_status IN ('delivered','picked_up') THEN 'completed'
	WHEN o.acceptance_status IN ('rejected','cancelled') THEN 'cancelled'
	WHEN o.fulfillment_status = 'delivery_failed' THEN 'failed' ELSE 'open' END`

// billDays checks a period of the store's days.
func billDays(from, to string) error {
	f, err1 := time.Parse("2006-01-02", from)
	t, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil {
		return ErrValidation("from and to must be dates like 2026-11-21")
	}
	if t.Before(f) {
		return ErrValidation("to is before from")
	}
	if t.Sub(f) > billMaxDays*24*time.Hour {
		return ErrValidation("the period is too long")
	}
	return nil
}

// Bills lists the orders of a period. filter: "" all, "coupon" any coupon,
// "none" no discount at all, or one kind (promotion, store_free, ...).
func (a *App) Bills(ctx context.Context, branchID int64, from, to, filter string) (*BillList, error) {
	if err := billDays(from, to); err != nil {
		return nil, err
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	cond := ""
	switch filter {
	case "":
	case "coupon":
		cond = ` AND EXISTS (SELECT 1 FROM order_discounts d WHERE d.order_id = o.id AND d.kind <> 'promotion')`
	case "none":
		cond = ` AND NOT EXISTS (SELECT 1 FROM order_discounts d WHERE d.order_id = o.id)`
	case "promotion", CouponStoreFree, CouponStoreThreshold, CouponStoreDelivery, CouponPlatformFree:
		cond = ` AND EXISTS (SELECT 1 FROM order_discounts d WHERE d.order_id = o.id AND d.kind = '` + filter + `')`
	default:
		return nil, ErrValidation("unknown filter")
	}
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.public_no, o.created_at, o.service_type, `+billState+`,
			o.subtotal, o.delivery_fee, o.total, o.platform_subsidy
		FROM orders o JOIN branches b ON b.id = o.branch_id
		WHERE o.branch_id = $1 AND o.service_type IN ('delivery','pickup')
			AND (o.created_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date`+cond+`
		ORDER BY o.created_at DESC, o.id DESC`, branchID, from, to)
	if err != nil {
		return nil, err
	}
	out := &BillList{Timezone: b.Timezone, From: from, To: to, Currency: b.Currency, Orders: []BillOrder{}}
	byID := map[int64]*BillOrder{}
	var ids []int64
	for rows.Next() {
		var o BillOrder
		var total, subsidy int64
		if err := rows.Scan(&o.ID, &o.PublicNo, &o.CreatedAt, &o.ServiceType, &o.State, &o.Food, &o.DeliveryFee,
			&total, &subsidy); err != nil {
			rows.Close()
			return nil, err
		}
		out.Count++
		if len(out.Orders) >= 2000 {
			continue
		}
		o.Discounts = []BillDiscount{}
		if o.State == "completed" {
			o.Paid, o.Takings = total, total+subsidy
		}
		out.Orders = append(out.Orders, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out.Orders {
		byID[out.Orders[i].ID] = &out.Orders[i]
		ids = append(ids, out.Orders[i].ID)
	}
	out.Shown = len(out.Orders)
	if len(ids) > 0 {
		drows, err := a.DB.Query(ctx, `SELECT d.order_id, d.kind, d.funded_by, d.name, d.name_en, d.amount, d.coupon_id,
				COALESCE(c.source, '')
			FROM order_discounts d LEFT JOIN customer_coupons c ON c.id = d.coupon_id
			WHERE d.order_id = ANY($1) ORDER BY d.order_id,
				array_position(ARRAY['promotion','store_threshold','store_free','store_delivery','platform_free'], d.kind), d.id`, ids)
		if err != nil {
			return nil, err
		}
		defer drows.Close()
		for drows.Next() {
			var d BillDiscount
			var orderID int64
			if err := drows.Scan(&orderID, &d.Kind, &d.FundedBy, &d.Name, &d.NameEn, &d.Amount, &d.CouponID, &d.Source); err != nil {
				return nil, err
			}
			o := byID[orderID]
			o.Discounts = append(o.Discounts, d)
			if o.State != "completed" {
				continue // given back, or nothing was collected
			}
			switch d.Kind {
			case "promotion":
				o.Promotion += d.Amount
			case CouponStoreFree:
				o.StoreFree += d.Amount
			case CouponStoreThreshold:
				o.StoreThreshold += d.Amount
			case CouponStoreDelivery:
				o.StoreDelivery += d.Amount
			case CouponPlatformFree:
				o.PlatformFree += d.Amount
			}
		}
		if err := drows.Err(); err != nil {
			return nil, err
		}
	}
	for i := range out.Orders {
		if out.Orders[i].State == "completed" {
			out.Sum.add(&out.Orders[i])
		}
	}
	return out, nil
}

// ---- the summary ----

type BillKind struct {
	Kind     string `json:"kind"`
	FundedBy string `json:"funded_by"`
	Orders   int    `json:"orders"`
	Amount   int64  `json:"amount"`
}

type BillDay struct {
	Day      string `json:"day"`
	Orders   int    `json:"orders"`
	Food     int64  `json:"food"`
	Discount int64  `json:"discount"`
	Paid     int64  `json:"paid"`
	Takings  int64  `json:"takings"`
}

type BillSummary struct {
	From             string `json:"from"`
	To               string `json:"to"`
	Currency         string `json:"currency"`
	Orders           int    `json:"orders"` // completed
	Cancelled        int    `json:"cancelled"`
	Food             int64  `json:"food"`
	MerchantDiscount int64  `json:"merchant_discount"`
	PlatformDiscount int64  `json:"platform_discount"`
	Paid             int64  `json:"paid"`
	Takings          int64  `json:"takings"`
	// Refunded is money given back by dish on the period's completed orders.
	Refunded   int64      `json:"refunded"`
	Kinds      []BillKind `json:"kinds"` // every kind, in the order of the bill
	Repurchase struct {
		Issued int   `json:"issued"`
		Used   int   `json:"used"`
		Sales  int64 `json:"sales"`
	} `json:"repurchase"`
	Days        []BillDay    `json:"days"`
	Settlements []Settlement `json:"settlements"`
}

var billKinds = []string{"promotion", CouponStoreFree, CouponStoreThreshold, CouponStoreDelivery, CouponPlatformFree}

func (a *App) BillSummary(ctx context.Context, branchID int64, from, to string) (*BillSummary, error) {
	if err := billDays(from, to); err != nil {
		return nil, err
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	s := &BillSummary{From: from, To: to, Currency: b.Currency, Kinds: []BillKind{}, Days: []BillDay{}}
	in := `o.branch_id = $1 AND o.service_type IN ('delivery','pickup')
		AND (o.created_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date`
	done := ` AND o.fulfillment_status IN ('delivered','picked_up')`
	err = a.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE `+strings.TrimPrefix(done, " AND ")+`),
			count(*) FILTER (WHERE o.acceptance_status IN ('rejected','cancelled')),
			COALESCE(sum(o.subtotal) FILTER (WHERE `+strings.TrimPrefix(done, " AND ")+`), 0),
			COALESCE(sum(o.merchant_discount) FILTER (WHERE `+strings.TrimPrefix(done, " AND ")+`), 0),
			COALESCE(sum(o.platform_subsidy) FILTER (WHERE `+strings.TrimPrefix(done, " AND ")+`), 0),
			COALESCE(sum(o.total) FILTER (WHERE `+strings.TrimPrefix(done, " AND ")+`), 0)
		FROM orders o JOIN branches b ON b.id = o.branch_id WHERE `+in, branchID, from, to).
		Scan(&s.Orders, &s.Cancelled, &s.Food, &s.MerchantDiscount, &s.PlatformDiscount, &s.Paid)
	if err != nil {
		return nil, err
	}
	s.Takings = s.Paid + s.PlatformDiscount
	if err := a.DB.QueryRow(ctx, `SELECT COALESCE(sum(r.amount), 0) FROM order_refunds r
		JOIN orders o ON o.id = r.order_id JOIN branches b ON b.id = o.branch_id WHERE `+in+done, branchID, from, to).
		Scan(&s.Refunded); err != nil {
		return nil, err
	}

	got := map[string]BillKind{}
	rows, err := a.DB.Query(ctx, `SELECT d.kind, count(DISTINCT d.order_id), sum(d.amount)
		FROM order_discounts d JOIN orders o ON o.id = d.order_id JOIN branches b ON b.id = o.branch_id
		WHERE `+in+done+` GROUP BY d.kind`, branchID, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k BillKind
		if err := rows.Scan(&k.Kind, &k.Orders, &k.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		got[k.Kind] = k
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, k := range billKinds {
		x := got[k]
		x.Kind, x.FundedBy = k, fundedBy(k)
		if k == "promotion" {
			x.FundedBy = "store"
		}
		s.Kinds = append(s.Kinds, x)
	}

	// the welcome-back coupon: given in the period, and of those, used
	err = a.DB.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE c.status='used'),
			COALESCE(sum(o.subtotal) FILTER (WHERE c.status='used' AND o.fulfillment_status IN ('delivered','picked_up')), 0)
		FROM customer_coupons c JOIN branches b ON b.id = c.branch_id LEFT JOIN orders o ON o.id = c.order_id
		WHERE c.branch_id=$1 AND c.source='repurchase'
			AND (c.issued_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date`, branchID, from, to).
		Scan(&s.Repurchase.Issued, &s.Repurchase.Used, &s.Repurchase.Sales)
	if err != nil {
		return nil, err
	}

	rows, err = a.DB.Query(ctx, `SELECT to_char((o.created_at AT TIME ZONE b.timezone)::date, 'YYYY-MM-DD'), count(*),
			sum(o.subtotal), sum(o.discount), sum(o.total), sum(o.total + o.platform_subsidy)
		FROM orders o JOIN branches b ON b.id = o.branch_id WHERE `+in+done+` GROUP BY 1 ORDER BY 1`, branchID, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d BillDay
		if err := rows.Scan(&d.Day, &d.Orders, &d.Food, &d.Discount, &d.Paid, &d.Takings); err != nil {
			rows.Close()
			return nil, err
		}
		s.Days = append(s.Days, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.Settlements, err = listSettlements(ctx, a.DB, `s.branch_id=$1`, branchID)
	return s, err
}

// ---- what the platform owes for its coupons ----

type Settlement struct {
	ID          int64      `json:"id"`
	BranchID    int64      `json:"branch_id"`
	StoreName   string     `json:"store_name"`
	StoreSlug   string     `json:"store_slug"`
	Month       string     `json:"month"` // 2026-10
	Currency    string     `json:"currency"`
	Orders      int        `json:"orders"`
	Amount      int64      `json:"amount"`
	Status      string     `json:"status"` // pending, paid
	PaidOn      *string    `json:"paid_on"`
	TransferRef string     `json:"transfer_ref"`
	PaidAt      *time.Time `json:"paid_at"`
}

func listSettlements(ctx context.Context, q Querier, where string, args ...any) ([]Settlement, error) {
	rows, err := q.Query(ctx, `SELECT s.id, s.branch_id, b.name, b.slug, to_char(s.month, 'YYYY-MM'), s.currency, s.orders,
			s.amount, s.status, to_char(s.paid_on, 'YYYY-MM-DD'), s.transfer_ref, s.paid_at
		FROM platform_settlements s JOIN branches b ON b.id = s.branch_id
		WHERE `+where+` ORDER BY s.month DESC, s.status, b.name LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Settlement{}
	for rows.Next() {
		var s Settlement
		if err := rows.Scan(&s.ID, &s.BranchID, &s.StoreName, &s.StoreSlug, &s.Month, &s.Currency, &s.Orders, &s.Amount,
			&s.Status, &s.PaidOn, &s.TransferRef, &s.PaidAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MakeSettlements writes last month's statement for every store whose
// customers used platform coupons on completed orders, once the store's month
// is over. It is safe to run any number of times.
func (a *App) MakeSettlements(ctx context.Context) (int, error) {
	tag, err := a.DB.Exec(ctx, `INSERT INTO platform_settlements (branch_id, month, currency, orders, amount)
		SELECT b.id, (date_trunc('month', now() AT TIME ZONE b.timezone) - interval '1 month')::date, o.currency,
			count(*), sum(o.platform_subsidy)
		FROM orders o JOIN branches b ON b.id = o.branch_id
		WHERE o.platform_subsidy > 0 AND o.fulfillment_status IN ('delivered','picked_up')
			AND o.completed_at >= now() - interval '70 days'
			AND (o.completed_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone) - interval '1 month'
			AND (o.completed_at AT TIME ZONE b.timezone) < date_trunc('month', now() AT TIME ZONE b.timezone)
		GROUP BY b.id, b.timezone, o.currency
		ON CONFLICT (branch_id, month, currency) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// SettlementOrders lists the orders behind a statement. branchID 0 is the
// platform, which may look at any store's.
func (a *App) SettlementOrders(ctx context.Context, branchID, id int64) ([]BillOrder, error) {
	var bid int64
	var month time.Time
	var cur string
	err := a.DB.QueryRow(ctx, `SELECT branch_id, month, currency FROM platform_settlements WHERE id=$1`, id).Scan(&bid, &month, &cur)
	if isNoRows(err) || err == nil && branchID != 0 && bid != branchID {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.public_no, o.created_at, o.service_type, o.subtotal, o.delivery_fee,
			o.total, o.platform_subsidy
		FROM orders o JOIN branches b ON b.id = o.branch_id
		WHERE o.branch_id=$1 AND o.currency=$3 AND o.platform_subsidy > 0 AND o.fulfillment_status IN ('delivered','picked_up')
			AND (o.completed_at AT TIME ZONE b.timezone) >= $2::date
			AND (o.completed_at AT TIME ZONE b.timezone) < ($2::date + interval '1 month')
		ORDER BY o.completed_at`, bid, month, cur)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BillOrder{}
	for rows.Next() {
		o := BillOrder{State: "completed", Discounts: []BillDiscount{}}
		if err := rows.Scan(&o.ID, &o.PublicNo, &o.CreatedAt, &o.ServiceType, &o.Food, &o.DeliveryFee, &o.Paid,
			&o.PlatformFree); err != nil {
			return nil, err
		}
		o.Takings = o.Paid + o.PlatformFree
		out = append(out, o)
	}
	return out, rows.Err()
}

// ---- the platform's side ----

func (a *App) ListSettlements(ctx context.Context, status string) ([]Settlement, error) {
	switch status {
	case "":
		return listSettlements(ctx, a.DB, `true`)
	case "pending", "paid":
		return listSettlements(ctx, a.DB, `s.status=$1`, status)
	}
	return nil, ErrValidation("status must be pending or paid")
}

// MarkSettlementPaid records the platform's transfer to the store.
func (a *App) MarkSettlementPaid(ctx context.Context, actor AdminActor, id int64, paidOn, ref string) error {
	if _, err := time.Parse("2006-01-02", paidOn); err != nil {
		return ErrValidation("the date paid must be like 2026-12-05")
	}
	ref = strings.TrimSpace(ref)
	if ref == "" || len(ref) > 100 {
		return ErrValidation("give the transfer reference")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM platform_settlements WHERE id=$1 FOR UPDATE`, id).Scan(&status)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status == "paid" {
			return ErrInvalidState("this statement is already paid")
		}
		if _, err := tx.Exec(ctx, `UPDATE platform_settlements SET status='paid', paid_on=$2, transfer_ref=$3, paid_by=$4,
			paid_at=now() WHERE id=$1`, id, paidOn, ref, actor.ID); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "settlement.paid", "platform_settlement", id, map[string]any{"paid_on": paidOn, "ref": ref})
	})
}

// StoreBill is one store's line on the platform's bill page.
type StoreBill struct {
	BranchID         int64  `json:"branch_id"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	Currency         string `json:"currency"`
	Orders           int    `json:"orders"`
	Food             int64  `json:"food"`
	MerchantDiscount int64  `json:"merchant_discount"`
	PlatformDiscount int64  `json:"platform_discount"`
	Paid             int64  `json:"paid"`
}

// StoreBills sums each store's completed orders of a period (each store's own days).
func (a *App) StoreBills(ctx context.Context, from, to string) ([]StoreBill, error) {
	if err := billDays(from, to); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT b.id, b.name, b.slug, o.currency, count(*), sum(o.subtotal), sum(o.merchant_discount),
			sum(o.platform_subsidy), sum(o.total)
		FROM orders o JOIN branches b ON b.id = o.branch_id
		WHERE o.service_type IN ('delivery','pickup') AND o.fulfillment_status IN ('delivered','picked_up')
			AND o.created_at >= $1::date - interval '1 day' AND o.created_at < $2::date + interval '2 days'
			AND (o.created_at AT TIME ZONE b.timezone)::date BETWEEN $1::date AND $2::date
		GROUP BY b.id, b.name, b.slug, o.currency ORDER BY sum(o.total) DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreBill{}
	for rows.Next() {
		var s StoreBill
		if err := rows.Scan(&s.BranchID, &s.Name, &s.Slug, &s.Currency, &s.Orders, &s.Food, &s.MerchantDiscount,
			&s.PlatformDiscount, &s.Paid); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
