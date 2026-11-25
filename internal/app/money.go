package app

import (
	"context"
	"fmt"
	"slices"
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
	// share is how it was shared over the lines when priced.
	share pricing.Applied
}

// moneyLine is one live dish line: what it costs, which dish it is and what
// one of it costs (so a dish promotion can count how many are left).
type moneyLine struct {
	ID     int64
	Amount int64
	ItemID int64
	Unit   int64
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
	// Dish are the dish promotions the order used, as they were then, and
	// what they took off in all.
	Dish       []*Promotion
	DishAmount int64
	Coupons    []orderCoupon
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
	rows, err := q.Query(ctx, `SELECT id, line_total, item_id, CASE WHEN qty > 0 THEN line_total / qty ELSE 0 END
		FROM order_items WHERE order_id=$1 AND prep_status <> 'cancelled' ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l moneyLine
		if err := rows.Scan(&l.ID, &l.Amount, &l.ItemID, &l.Unit); err != nil {
			rows.Close()
			return nil, err
		}
		m.Lines = append(m.Lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT coupon_id, kind, funded_by, name, name_en, amount, promotion_id, rule
		FROM order_discounts WHERE order_id=$1 ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	havePromoRow := false
	var others int64
	for rows.Next() {
		var c orderCoupon
		var pid *int64
		var rule []byte
		if err := rows.Scan(&c.CouponID, &c.Kind, &c.FundedBy, &c.Name, &c.NameEn, &c.Amount, &pid, &rule); err != nil {
			rows.Close()
			return nil, err
		}
		if c.Kind == "dish_promotion" {
			m.DishAmount += c.Amount
			if pid != nil {
				if p, ok := promoFromRule(*pid, c.Name, c.NameEn, rule); ok {
					m.Dish = append(m.Dish, p)
				}
			}
			continue
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
	// prorate scales what keep and floor hold to the dishes left: a void
	// or refund by the store's fault leaves the promotion its share of
	// what is still bought, not its whole amount.
	prorate bool
}

// heldPromo is what the order's promotion holds under pol when the dishes
// (after dish promotions) now cost sub: its amount, or with pol.prorate its
// share of it in proportion to what the dishes cost when it was priced.
// Free delivery is not about the dishes and is held whole.
func (m *orderMoney) heldPromo(sub int64, pol promoPolicy) int64 {
	held := m.PromoAmount
	if !pol.prorate || m.Promo == nil || m.Promo.Kind == "free_delivery" {
		return held
	}
	if base := m.Subtotal - m.DishAmount; base > 0 && sub < base {
		held = mulDivRound(held, max(sub, 0), base)
	}
	return held
}

// choosePromo decides the promotion for sub of dishes, by pol. switched
// says it is another promotion than the order had.
func (m *orderMoney) choosePromo(ctx context.Context, q Querier, b *Branch, sub int64, pol promoPolicy) (*Promotion, Discount, bool, error) {
	var cur *Promotion
	var off Discount
	if m.Promo != nil {
		switch {
		case pol.keep:
			if held := m.heldPromo(sub, pol); held > 0 {
				return m.Promo, promoOn(m.Promo.Kind, held), false, nil
			}
			return nil, Discount{}, false, nil
		case pol.current && sub >= m.Promo.Threshold:
			cur, off = m.Promo, m.Promo.discount(sub, m.DeliveryFee)
		}
		if held := m.heldPromo(sub, pol); pol.floor && off.total() < held {
			cur, off = m.Promo, promoOn(m.Promo.Kind, held)
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
	// Dish are the dish promotions' adjustments (dishAdjustments), and the
	// promotions they come from.
	Dish       []pricing.Adjustment
	DishPromos []*Promotion
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
	// promoShare is how the promotion is shared over the lines.
	promoShare pricing.Applied
	// ids maps the engine's line keys to order_items ids, when they are not the ids.
	ids map[string]int64
	// dish are the dish promotions applied, each with what it took off per dish.
	dish []dishApplied
	// dishIn is what went in for them, to price the same dishes again.
	dishIn     []pricing.Adjustment
	dishPromos []*Promotion
}

// dishDiscounts is what each dish promotion took off, for showing.
func (p *priced) dishDiscounts() []DishDiscount {
	out := []DishDiscount{}
	for _, d := range p.dish {
		out = append(out, DishDiscount{PromotionID: d.Promo.ID, Name: d.Promo.Name, NameEn: d.Promo.NameEn,
			Kind: d.Promo.Kind, Amount: d.Off})
	}
	return out
}

// dishApplied is one dish promotion on an order: what it took off on each
// dish, all together.
type dishApplied struct {
	Promo *Promotion
	pricing.Applied
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

// engineInput is in as the pricing engine takes it: the dish promotions,
// the promotion, the coupons in their fixed order (coupons.go couponOrder),
// the percentage off at checkout and the round-off, in that order.
func engineInput(in moneyIn) pricing.Input {
	adj := append([]pricing.Adjustment{}, in.Dish...)
	if in.Promo != nil {
		a := pricing.Adjustment{Kind: "promotion", Ref: in.Promo.ID, Name: in.Promo.Name, NameEn: in.Promo.NameEn,
			FundedBy: "store", Stage: pricing.StageOrder, Amount: in.PromoOff.Goods}
		if in.Promo.Kind == "free_delivery" || in.PromoOff.Delivery > 0 {
			a.Stage, a.Amount = pricing.StageDelivery, in.PromoOff.Delivery
		}
		adj = append(adj, a)
	}
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
	return pricing.Input{Lines: in.Lines, Adjustments: adj, Fees: pricing.Fees{TaxRateBP: in.Fees.TaxRateBP,
		TaxInclusive: in.Fees.TaxInclusive, ServiceChargeBP: in.Fees.ServiceChargeBP, Packaging: in.Fees.PackagingFee,
		Delivery: in.Delivery}}
}

func priceMoney(in moneyIn) (*priced, error) {
	r, err := pricing.Price(engineInput(in))
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
	// the dish promotions, one entry each, in the order they first appear
	p.dishIn, p.dishPromos = in.Dish, in.DishPromos
	byRef := map[int64]int{}
	for _, a := range r.Applied[:len(in.Dish)] {
		if a.Off == 0 {
			continue
		}
		k, ok := byRef[a.Ref]
		if !ok {
			var promo *Promotion
			for _, dp := range in.DishPromos {
				if dp.ID == a.Ref {
					promo = dp
				}
			}
			if promo == nil {
				promo = &Promotion{ID: a.Ref, Name: a.Name, NameEn: a.NameEn}
			}
			k = len(p.dish)
			byRef[a.Ref] = k
			p.dish = append(p.dish, dishApplied{Promo: promo, Applied: pricing.Applied{Adjustment: a.Adjustment,
				PerLine: map[string]int64{}}})
		}
		d := &p.dish[k]
		d.Off += a.Off
		for key, v := range a.PerLine {
			d.PerLine[key] += v
		}
	}
	i := len(in.Dish)
	if in.Promo != nil {
		p.promoShare = r.Applied[i]
		if off := r.Applied[i].Off; off > 0 {
			t.PromotionID, t.PromotionName, t.PromotionNameEn = in.Promo.ID, in.Promo.Name, in.Promo.NameEn
			t.PromotionDiscount = off
		} else {
			p.Promo = nil
		}
		i++
	}
	for _, kind := range couponOrder {
		for _, c := range in.Coupons {
			if c.Kind == kind {
				c.Amount, c.share = r.Applied[i].Off, r.Applied[i]
				p.Coupons = append(p.Coupons, c)
				i++
			}
		}
	}
	p.Totals = t
	return p, nil
}

// input is the order's own money as engine input, on lines (nil for the
// order's live lines), with the dish promotions dish and promo taking off
// promoOff.
func (m *orderMoney) input(lines []moneyLine, dish []*Promotion, promo *Promotion, promoOff Discount) moneyIn {
	if lines == nil {
		lines = m.Lines
	}
	in := moneyIn{Fees: m.Fees, Delivery: m.DeliveryFee, Promo: promo, PromoOff: promoOff, StaffBP: m.StaffBP,
		Dish: dishAdjustments(dish, dishLinesOf(lines)), DishPromos: dish}
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
	if lines == nil {
		lines = m.Lines
	}
	// the dish promotions first: the order's own as they were, and, when
	// pol may move to a better offer, those running now on other dishes
	dish := append([]*Promotion{}, m.Dish...)
	if pol.pick {
		live, err := liveDishPromos(ctx, q, b, m.ServiceType, pol.at, sub)
		if err != nil {
			return nil, false, err
		}
		for _, p := range live {
			if !slices.ContainsFunc(dish, func(d *Promotion) bool { return d.ID == p.ID }) {
				dish = append(dish, p)
			}
		}
	}
	sub -= dishOff(dishAdjustments(dish, dishLinesOf(lines)))
	promo, off, switched, err := m.choosePromo(ctx, q, b, sub, pol)
	if err != nil {
		return nil, false, err
	}
	p, err := priceMoney(m.input(lines, dish, promo, off))
	return p, switched, err
}

// dishLinesOf is lines as the dish rules see them.
func dishLinesOf(lines []moneyLine) []dishLine {
	out := make([]dishLine, 0, len(lines))
	for _, l := range lines {
		q := 0
		if l.Unit > 0 {
			q = int(l.Amount / l.Unit)
		}
		out = append(out, dishLine{Key: keyOf(l.ID), ItemID: l.ItemID, Unit: l.Unit, Qty: q})
	}
	return out
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

// withExtra is the order's live lines and the lines of an add-on not yet placed.
func (m *orderMoney) withExtra(extra []QuoteLine) []moneyLine {
	out := append([]moneyLine{}, m.Lines...)
	for i, l := range extra {
		ml := moneyLine{ID: int64(-1 - i), Amount: l.LineTotal, ItemID: l.ItemID}
		if l.Qty > 0 {
			ml.Unit = l.LineTotal / int64(l.Qty)
		}
		out = append(out, ml)
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
		adj_reason = CASE WHEN $14 = 0 AND $16 = '' THEN '' ELSE adj_reason END,
		adj_by = CASE WHEN $14 = 0 AND $16 = '' THEN NULL ELSE adj_by END,
		adj_approved_by = CASE WHEN $14 = 0 AND $16 = '' THEN NULL ELSE adj_approved_by END,
		adj_at = CASE WHEN $14 = 0 AND $16 = '' THEN NULL ELSE adj_at END,
		version=version+1`+bump+`, updated_at=now() WHERE id=$1`,
		orderID, t.Subtotal, t.ServiceCharge, t.TaxAmount, t.TaxInclusive, t.TaxRateBP, t.Discount, t.MerchantDiscount,
		t.PlatformSubsidy, t.Total, promoID, t.PromotionName, t.PromotionNameEn, t.AdjPercentBP, t.StaffDiscount,
		t.AdjMode, t.RoundOff); err != nil {
		return wrap("save order money", err)
	}
	if err := saveLineShares(ctx, tx, orderID, p.Result, p.ids); err != nil {
		return err
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
			amount, promotion_id, per_line, created_at) VALUES ($1,$2,'promotion','store',$3,$4,$5,$6,$7,
			(SELECT created_at FROM orders WHERE id=$1))`, orderID, branchID, t.PromotionName,
			t.PromotionNameEn, t.PromotionDiscount, p.Promo.ID, perLine(p.promoShare, p.ids)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE promotion_redemptions SET amount=$2 WHERE order_id=$1 AND status='applied'`,
			orderID, t.PromotionDiscount); err != nil {
			return err
		}
	}
	for _, d := range p.dish {
		if _, err := tx.Exec(ctx, `INSERT INTO order_discounts (order_id, branch_id, kind, funded_by, name, name_en,
			amount, promotion_id, per_line, rule, created_at) VALUES ($1,$2,'dish_promotion','store',$3,$4,$5,$6,$7,$8,
			(SELECT created_at FROM orders WHERE id=$1))`, orderID, branchID, d.Promo.Name, d.Promo.NameEn, d.Off,
			d.Promo.ID, perLine(d.Applied, p.ids), ruleOf(d.Promo)); err != nil {
			return fmt.Errorf("dish promotion row: %w", err)
		}
	}
	for _, c := range p.Coupons {
		if _, err := tx.Exec(ctx, `INSERT INTO order_discounts (order_id, branch_id, kind, funded_by, name, name_en,
			amount, coupon_id, per_line, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,
			(SELECT created_at FROM orders WHERE id=$1))`, orderID, branchID, c.Kind, c.FundedBy, c.Name, c.NameEn,
			c.Amount, c.CouponID, perLine(c.share, p.ids)); err != nil {
			return fmt.Errorf("discount row: %w", err)
		}
	}
	return nil
}

// moneyEvent says why the order amounts written next in this transaction
// change, for the money ledger (order_money_events, written by the database).
func moneyEvent(ctx context.Context, tx pgx.Tx, kind string, ref int64, actor Actor) error {
	refS, idS := "", ""
	if ref != 0 {
		refS = strconv.FormatInt(ref, 10)
	}
	if actor.ID != 0 {
		idS = strconv.FormatInt(actor.ID, 10)
	}
	_, err := tx.Exec(ctx, `SELECT set_config('waimai.money_kind', $1, true), set_config('waimai.money_ref', $2, true),
		set_config('waimai.money_actor_type', $3, true), set_config('waimai.money_actor_id', $4, true)`,
		kind, refS, actor.Type, idS)
	return err
}

// saveLineShares writes each dish line's share of the discounts, service
// charge and tax; ids maps the engine's line keys to order_items ids (nil
// when the keys are the ids). Voided lines share nothing.
func saveLineShares(ctx context.Context, tx pgx.Tx, orderID int64, r pricing.Result, ids map[string]int64) error {
	var lineIDs, disc, svc, tax []int64
	for _, l := range r.Lines {
		id, ok := ids[l.Key]
		if ids == nil {
			v, err := strconv.ParseInt(l.Key, 10, 64)
			id, ok = v, err == nil && v > 0
		}
		if !ok {
			continue
		}
		lineIDs, disc, svc, tax = append(lineIDs, id), append(disc, l.Discount), append(svc, l.Service), append(tax, l.Tax)
	}
	if _, err := tx.Exec(ctx, `UPDATE order_items SET discount_share=0, service_share=0, tax_share=0
		WHERE order_id=$1 AND prep_status = 'cancelled' AND (discount_share <> 0 OR service_share <> 0 OR tax_share <> 0)`,
		orderID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE order_items i SET discount_share=s.d, service_share=s.s, tax_share=s.t
		FROM unnest($2::bigint[], $3::bigint[], $4::bigint[], $5::bigint[]) AS s(id, d, s, t)
		WHERE i.id = s.id AND i.order_id = $1`, orderID, lineIDs, disc, svc, tax)
	return err
}

// perLine is an applied adjustment's sharing as stored, keyed by line id.
func perLine(a pricing.Applied, ids map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for k, v := range a.PerLine {
		if ids != nil {
			id, ok := ids[k]
			if !ok {
				continue
			}
			k = strconv.FormatInt(id, 10)
		}
		out[k] = v
	}
	return out
}

// MoneyEvent is one row of an order's money ledger (docs/PRICING_REVIEW.md
// §5.3): the order's amounts after a change, and the change.
type MoneyEvent struct {
	ID               int64     `json:"id"`
	Kind             string    `json:"kind"`
	RefID            *int64    `json:"ref_id"`
	ActorType        string    `json:"actor_type"`
	ActorName        string    `json:"actor_name"`
	Subtotal         int64     `json:"subtotal"`
	Discount         int64     `json:"discount"`
	MerchantDiscount int64     `json:"merchant_discount"`
	PlatformSubsidy  int64     `json:"platform_subsidy"`
	StaffDiscount    int64     `json:"staff_discount"`
	RoundOff         int64     `json:"round_off"`
	ServiceCharge    int64     `json:"service_charge"`
	TaxAmount        int64     `json:"tax_amount"`
	Total            int64     `json:"total"`
	Refunded         int64     `json:"refunded"`
	DeltaTotal       int64     `json:"delta_total"`
	CreatedAt        time.Time `json:"created_at"`
}

// OrderMoneyEvents is the order's money ledger, oldest first. Changes made in
// one transaction (the order and then its discounts) are shown as one row:
// the last amounts and the sum of the changes.
func (a *App) OrderMoneyEvents(ctx context.Context, orderID int64) ([]MoneyEvent, error) {
	rows, err := a.DB.Query(ctx, `SELECT e.id, e.kind, e.ref_id, e.actor_type,
		COALESCE((SELECT u.display_name FROM users u WHERE u.id = e.actor_id AND e.actor_type = 'staff'), ''),
		e.subtotal, e.discount, e.merchant_discount, e.platform_subsidy, e.staff_discount, e.round_off,
		e.service_charge, e.tax_amount, e.total, e.refunded,
		SUM(e.delta_total) OVER (PARTITION BY e.tx, e.kind), e.created_at,
		e.id = MAX(e.id) OVER (PARTITION BY e.tx, e.kind)
		FROM order_money_events e WHERE e.order_id = $1 ORDER BY e.id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MoneyEvent{}
	for rows.Next() {
		var e MoneyEvent
		var last bool
		if err := rows.Scan(&e.ID, &e.Kind, &e.RefID, &e.ActorType, &e.ActorName, &e.Subtotal, &e.Discount,
			&e.MerchantDiscount, &e.PlatformSubsidy, &e.StaffDiscount, &e.RoundOff, &e.ServiceCharge, &e.TaxAmount,
			&e.Total, &e.Refunded, &e.DeltaTotal, &e.CreatedAt, &last); err != nil {
			return nil, err
		}
		if last {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}
