package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/brianabc121/waimai/internal/pricing"
)

// An order's money, priced in one place (docs/PRICING_REVIEW.md §5).
//
// Every screen that shows or changes what an order costs goes through the
// same three steps:
//
//  1. loadMoney reads what the order was priced with: its own fee rates,
//     its live dish lines, its promotion (with the kind it had then), its
//     coupons and the discount given at checkout;
//  2. choosePromo decides which promotion applies now, by a stated policy
//     (an add-on never lowers it, the store's fault keeps it, ...);
//  3. priceMoney hands all of it to the pricing engine, and saveMoney
//     writes the result: the order's amounts and one discount row for the
//     promotion and each coupon, always adding up to the order's discount.

// orderCoupon is a coupon (or the promotion) an order used, as recorded.
type orderCoupon struct {
	CouponID *int64
	Kind     string
	FundedBy string
	Name     string
	NameEn   string
	// Amount is what it took off when last priced; a repricing uses it as
	// the most it can take off (a coupon never grows back).
	Amount int64
}

// moneyLine is one live dish line.
type moneyLine struct {
	ID     int64
	Amount int64
}

// orderMoney is what an order was priced with, read under the order lock.
type orderMoney struct {
	ID, BranchID       int64
	ServiceType, Phone string
	Created            time.Time
	// Opened is when the table was opened (dine-in), else Created.
	Opened time.Time
	Fees   TaxConfig
	// Subtotal and Total are as stored now.
	Subtotal, DeliveryFee, Total int64
	Lines                        []moneyLine
	// The promotion, as loaded now but with the kind it had when the order
	// took it, and what it took off; nil when none.
	Promo       *Promotion
	PromoAmount int64
	Coupons     []orderCoupon
	// The discount at checkout: the share still charged (0 = none), and a
	// round-off or typed amount (Mode "round" or "set_total").
	StaffBP  int
	AdjMode  string
	RoundOff int64
}

func loadMoney(ctx context.Context, q Querier, orderID int64) (*orderMoney, error) {
	m := &orderMoney{ID: orderID, Lines: []moneyLine{}}
	var promoID *int64
	var promoKind string
	var discount, platform int64
	if err := q.QueryRow(ctx, `SELECT o.branch_id, o.service_type, o.customer_phone, o.created_at,
		COALESCE((SELECT s.opened_at FROM dining_sessions s WHERE s.id = o.session_id), o.created_at),
		o.tax_rate_bp, o.tax_inclusive, o.service_charge_bp, o.packaging_fee, o.subtotal, o.delivery_fee, o.total,
		o.promotion_id, o.promotion_kind, o.discount, o.platform_subsidy, o.adj_percent_bp, o.adj_mode, o.round_off
		FROM orders o WHERE o.id=$1`, orderID).Scan(&m.BranchID, &m.ServiceType, &m.Phone, &m.Created, &m.Opened,
		&m.Fees.TaxRateBP, &m.Fees.TaxInclusive, &m.Fees.ServiceChargeBP, &m.Fees.PackagingFee, &m.Subtotal,
		&m.DeliveryFee, &m.Total, &promoID, &promoKind, &discount, &platform, &m.StaffBP, &m.AdjMode, &m.RoundOff); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT id, line_total FROM order_items WHERE order_id=$1 AND prep_status <> 'cancelled'
		ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l moneyLine
		if err := rows.Scan(&l.ID, &l.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		m.Lines = append(m.Lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT coupon_id, kind, funded_by, name, name_en, amount FROM order_discounts
		WHERE order_id=$1 ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	havePromoRow := false
	var others int64
	for rows.Next() {
		var c orderCoupon
		if err := rows.Scan(&c.CouponID, &c.Kind, &c.FundedBy, &c.Name, &c.NameEn, &c.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		if c.Kind == "promotion" {
			havePromoRow = true
			m.PromoAmount += c.Amount
			continue
		}
		if c.FundedBy == "store" {
			others += c.Amount
		}
		m.Coupons = append(m.Coupons, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if promoID != nil {
		ps, err := loadPromotions(ctx, q, m.BranchID, "p.id = "+strconv.FormatInt(*promoID, 10), "")
		if err != nil {
			return nil, err
		}
		if len(ps) == 1 {
			m.Promo = &ps[0]
			if promoKind != "" {
				m.Promo.Kind = promoKind
			}
		}
		// Orders from before every promotion had its row (table bills, orders
		// taken by staff): the store's part of the discount that is not a coupon.
		if !havePromoRow {
			m.PromoAmount = max(0, discount-platform-others)
		}
	}
	return m, nil
}

// sum is the live dishes' total.
func (m *orderMoney) sum() int64 {
	var s int64
	for _, l := range m.Lines {
		s += l.Amount
	}
	return s
}

// promoOn is what an amount the promotion takes off comes off: the delivery
// fee for free delivery, the dishes otherwise.
func promoOn(kind string, amount int64) Discount {
	if kind == "free_delivery" {
		return Discount{Delivery: amount}
	}
	return Discount{Goods: amount}
}

// promoPolicy says how an order's promotion is decided again.
type promoPolicy struct {
	// at is when the promotions' hours are checked.
	at time.Time
	// keep keeps the order's promotion at the amount it took off (the
	// store's fault); nothing else is considered.
	keep bool
	// current works the order's own promotion out again on the new amount
	// (if the dishes still reach its threshold).
	current bool
	// floor never takes off less than the order's promotion did (add-ons;
	// a table's void by the store's fault).
	floor bool
	// pick moves to another live promotion that saves more.
	pick bool
}

// choosePromo decides the promotion for sub of dishes, by pol. switched
// says it is another promotion than the order had.
func (m *orderMoney) choosePromo(ctx context.Context, q Querier, b *Branch, sub int64, pol promoPolicy) (*Promotion, Discount, bool, error) {
	var cur *Promotion
	var off Discount
	if m.Promo != nil {
		switch {
		case pol.keep:
			if m.PromoAmount > 0 {
				return m.Promo, promoOn(m.Promo.Kind, m.PromoAmount), false, nil
			}
			return nil, Discount{}, false, nil
		case pol.current && sub >= m.Promo.Threshold:
			cur, off = m.Promo, m.Promo.discount(sub, m.DeliveryFee)
		}
		if pol.floor && off.total() < m.PromoAmount {
			cur, off = m.Promo, promoOn(m.Promo.Kind, m.PromoAmount)
		}
	}
	if !pol.pick {
		return cur, off, false, nil
	}
	scope := "delivery"
	if m.ServiceType == "dine_in" {
		scope = "dine_in"
	}
	phone := m.Phone
	if scope == "dine_in" {
		phone = ""
	}
	promos, err := loadPromotions(ctx, q, b.ID, "p.status='active' AND p.scope="+scopeSQL(scope), phone)
	if err != nil {
		return nil, Discount{}, false, err
	}
	best, d := bestPromotion(promos, pol.at, b.location(), m.ServiceType, sub, m.DeliveryFee)
	if best != nil && (cur == nil || best.ID != cur.ID) && d.total() > off.total() {
		return best, d, m.Promo == nil || best.ID != m.Promo.ID, nil
	}
	return cur, off, false, nil
}

// moneyIn is everything priceMoney needs.
type moneyIn struct {
	Lines    []pricing.Line
	Fees     TaxConfig
	Delivery int64
	Promo    *Promotion
	PromoOff Discount
	// Coupons in the order they are taken off; Amount is the most each
	// takes off (a platform coupon's value).
	Coupons []orderCoupon
	StaffBP int
	Round   *Adjust // mode round or set_total; nil for none
}

// priced is an order priced by the engine.
type priced struct {
	Totals  Totals
	Promo   *Promotion
	Coupons []orderCoupon // with what each actually took off
	Result  pricing.Result
}

// couponStage is where a coupon kind is taken off.
func couponStage(kind string) pricing.Stage {
	switch kind {
	case CouponStoreDelivery:
		return pricing.StageDelivery
	case CouponPlatformFree:
		return pricing.StagePlatform
	}
	return pricing.StageOrder
}

func priceMoney(in moneyIn) (*priced, error) {
	var adj []pricing.Adjustment
	if in.Promo != nil {
		a := pricing.Adjustment{Kind: "promotion", Ref: in.Promo.ID, Name: in.Promo.Name, NameEn: in.Promo.NameEn,
			FundedBy: "store", Stage: pricing.StageOrder, Amount: in.PromoOff.Goods}
		if in.Promo.Kind == "free_delivery" || in.PromoOff.Delivery > 0 {
			a.Stage, a.Amount = pricing.StageDelivery, in.PromoOff.Delivery
		}
		adj = append(adj, a)
	}
	// coupons in their fixed order (coupons.go couponOrder)
	for _, kind := range couponOrder {
		for _, c := range in.Coupons {
			if c.Kind == kind {
				adj = append(adj, pricing.Adjustment{Kind: c.Kind, FundedBy: c.FundedBy, Name: c.Name, NameEn: c.NameEn,
					Stage: couponStage(c.Kind), Amount: c.Amount})
			}
		}
	}
	if in.StaffBP > 0 {
		adj = append(adj, pricing.Adjustment{Kind: "staff_percent", FundedBy: "store", Stage: pricing.StageStaff,
			PercentBP: in.StaffBP})
	}
	if in.Round != nil && in.Round.Mode != "" {
		adj = append(adj, pricing.Adjustment{Kind: map[string]string{"round": "round_off", "set_total": "set_total"}[in.Round.Mode],
			FundedBy: "store", Stage: pricing.StageRound, Amount: in.Round.Amount})
	}
	r, err := pricing.Price(pricing.Input{Lines: in.Lines, Adjustments: adj, Fees: pricing.Fees{TaxRateBP: in.Fees.TaxRateBP,
		TaxInclusive: in.Fees.TaxInclusive, ServiceChargeBP: in.Fees.ServiceChargeBP, Packaging: in.Fees.PackagingFee,
		Delivery: in.Delivery}})
	switch err {
	case pricing.ErrRoundTooBig, pricing.ErrSetTooBig:
		return nil, ErrValidation(err.Error())
	case nil:
	default:
		return nil, err
	}
	p := &priced{Result: r, Promo: in.Promo}
	tt := r.Totals
	t := Totals{Subtotal: tt.Subtotal, PackagingFee: tt.Packaging, DeliveryFee: tt.Delivery, ServiceCharge: tt.ServiceCharge,
		TaxAmount: tt.Tax, TaxInclusive: tt.TaxInclusive, TaxRateBP: tt.TaxRateBP,
		MerchantDiscount: tt.GoodsDiscount + tt.DeliveryDiscount, PlatformSubsidy: tt.PlatformDiscount,
		AdjPercentBP: in.StaffBP, StaffDiscount: tt.StaffDiscount, RoundOff: tt.RoundOff, Total: tt.Total}
	t.Discount = t.MerchantDiscount + t.PlatformSubsidy
	if in.Round != nil {
		t.AdjMode = in.Round.Mode
	}
	i := 0
	if in.Promo != nil {
		if off := r.Applied[0].Off; off > 0 {
			t.PromotionID, t.PromotionName, t.PromotionNameEn = in.Promo.ID, in.Promo.Name, in.Promo.NameEn
			t.PromotionDiscount = off
		} else {
			p.Promo = nil
		}
		i = 1
	}
	for _, kind := range couponOrder {
		for _, c := range in.Coupons {
			if c.Kind == kind {
				c.Amount = r.Applied[i].Off
				p.Coupons = append(p.Coupons, c)
				i++
			}
		}
	}
	p.Totals = t
	return p, nil
}

// input is the order's own money as engine input, on lines (nil for the
// order's live lines), with promo taking off promoOff.
func (m *orderMoney) input(lines []moneyLine, promo *Promotion, promoOff Discount) moneyIn {
	if lines == nil {
		lines = m.Lines
	}
	in := moneyIn{Fees: m.Fees, Delivery: m.DeliveryFee, Promo: promo, PromoOff: promoOff, StaffBP: m.StaffBP}
	for _, l := range lines {
		in.Lines = append(in.Lines, pricing.Line{Key: strconv.FormatInt(l.ID, 10), Amount: l.Amount})
	}
	in.Coupons = append(in.Coupons, m.Coupons...)
	return in
}

// reprice prices the order on lines (nil for its live lines) with its
// promotion decided by pol. The percentage off at checkout carries over; a
// round-off or typed amount does not (docs/CHECKOUT_DISCOUNT_DESIGN.md §5.4).
func (m *orderMoney) reprice(ctx context.Context, q Querier, b *Branch, lines []moneyLine, pol promoPolicy) (*priced, bool, error) {
	var sub int64
	if lines == nil {
		sub = m.sum()
	} else {
		for _, l := range lines {
			sub += l.Amount
		}
	}
	promo, off, switched, err := m.choosePromo(ctx, q, b, sub, pol)
	if err != nil {
		return nil, false, err
	}
	p, err := priceMoney(m.input(lines, promo, off))
	return p, switched, err
}

// without is the order's live lines less some dish money (line id → amount).
func (m *orderMoney) without(less map[int64]int64) []moneyLine {
	out := []moneyLine{}
	for _, l := range m.Lines {
		l.Amount -= less[l.ID]
		if l.Amount > 0 {
			out = append(out, l)
		}
	}
	return out
}

// withExtra is the order's live lines and extra dish money as one more line.
func (m *orderMoney) withExtra(extra int64) []moneyLine {
	out := append([]moneyLine{}, m.Lines...)
	if extra > 0 {
		out = append(out, moneyLine{ID: -1, Amount: extra})
	}
	return out
}

// saveMoney writes an order's priced amounts and its discount rows. The
// discount at checkout's who, why and when are left as they are.
func saveMoney(ctx context.Context, tx pgx.Tx, b *Branch, orderID int64, p *priced, manifest bool) error {
	t := p.Totals
	var promoID *int64
	if p.Promo != nil && t.PromotionDiscount > 0 {
		promoID = &p.Promo.ID
	}
	bump := ""
	if manifest {
		bump = ", manifest_version=manifest_version+1"
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET subtotal=$2, service_charge=$3, tax_amount=$4, tax_inclusive=$5,
		tax_rate_bp=$6, discount=$7, merchant_discount=$8, platform_subsidy=$9, total=$10, promotion_id=$11,
		promotion_name=$12, promotion_name_en=$13, adj_percent_bp=$14, staff_discount=$15, adj_mode=$16, round_off=$17,
		version=version+1`+bump+`, updated_at=now() WHERE id=$1`,
		orderID, t.Subtotal, t.ServiceCharge, t.TaxAmount, t.TaxInclusive, t.TaxRateBP, t.Discount, t.MerchantDiscount,
		t.PlatformSubsidy, t.Total, promoID, t.PromotionName, t.PromotionNameEn, t.AdjPercentBP, t.StaffDiscount,
		t.AdjMode, t.RoundOff); err != nil {
		return wrap("save order money", err)
	}
	return saveDiscountRows(ctx, tx, b.ID, orderID, p)
}

// saveDiscountRows replaces the order's discount rows: one for the
// promotion and one per coupon, as priced. A coupon that no longer takes
// anything off keeps its row at 0 (it stays used by the order).
func saveDiscountRows(ctx context.Context, tx pgx.Tx, branchID, orderID int64, p *priced) error {
	if _, err := tx.Exec(ctx, `DELETE FROM order_discounts WHERE order_id=$1`, orderID); err != nil {
		return err
	}
	t := p.Totals
	if p.Promo != nil && t.PromotionDiscount > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO order_discounts (order_id, branch_id, kind, funded_by, name, name_en,
			amount, promotion_id) VALUES ($1,$2,'promotion','store',$3,$4,$5,$6)`, orderID, branchID, t.PromotionName,
			t.PromotionNameEn, t.PromotionDiscount, p.Promo.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE promotion_redemptions SET amount=$2 WHERE order_id=$1 AND status='applied'`,
			orderID, t.PromotionDiscount); err != nil {
			return err
		}
	}
	for _, c := range p.Coupons {
		if _, err := tx.Exec(ctx, `INSERT INTO order_discounts (order_id, branch_id, kind, funded_by, name, name_en,
			amount, coupon_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, orderID, branchID, c.Kind, c.FundedBy, c.Name, c.NameEn,
			c.Amount, c.CouponID); err != nil {
			return fmt.Errorf("discount row: %w", err)
		}
	}
	return nil
}
