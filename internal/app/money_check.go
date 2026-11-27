package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/brianabc121/waimai/internal/pricing"
)

// CheckOrderMoney checks one stored order against the rules of
// docs/PRICING_CHECK.md §2 and returns one line per broken rule (nil when
// all hold). It only reads.
//
// Besides the sums on the order (D1–D9, D12–D15), it replays the order (R1–R3): it
// prices the order again from nothing but what the order itself holds — its
// fee rates, its live dishes, the rules of its dish promotions, what its
// promotion and coupons took off, the discount at checkout — and the result
// must be what is stored, to the cent, line by line. A change that writes
// an amount the engine would not give back breaks it.
func (a *App) CheckOrderMoney(ctx context.Context, orderID int64) ([]string, error) {
	var bad []string
	fail := func(f string, v ...any) { bad = append(bad, fmt.Sprintf(f, v...)) }

	var o struct {
		status                                                  string
		sub, pack, ship, svc, tax, disc, merch, platf, staff    int64
		round, total, rows, perLine, ledger, refunded, negative int64
		incl, hasRows                                           bool
		otype                                                   string
		platfRows                                               int64
	}
	if err := a.DB.QueryRow(ctx, `SELECT o.acceptance_status, o.subtotal, o.packaging_fee, o.delivery_fee,
		o.service_charge, o.tax_amount, o.discount, o.merchant_discount, o.platform_subsidy, o.staff_discount,
		o.round_off, o.total, o.tax_inclusive,
		EXISTS (SELECT 1 FROM order_discounts d WHERE d.order_id = o.id),
		COALESCE((SELECT sum(amount) FROM order_discounts d WHERE d.order_id = o.id), 0),
		COALESCE((SELECT sum(v.value::bigint) FROM order_discounts d, jsonb_each_text(d.per_line) v
			WHERE d.order_id = o.id), 0),
		COALESCE((SELECT sum(e.delta_total) FROM order_money_events e WHERE e.order_id = o.id), 0),
		COALESCE((SELECT sum(r.amount) FROM order_refunds r WHERE r.order_id = o.id), 0),
		(SELECT count(*) FROM order_discounts d WHERE d.order_id = o.id AND d.amount < 0),
		o.order_type, (SELECT count(*) FROM order_discounts d WHERE d.order_id = o.id AND d.funded_by = 'platform')
		FROM orders o WHERE o.id = $1`, orderID).Scan(&o.status, &o.sub, &o.pack, &o.ship, &o.svc, &o.tax, &o.disc,
		&o.merch, &o.platf, &o.staff, &o.round, &o.total, &o.incl, &o.hasRows, &o.rows, &o.perLine, &o.ledger,
		&o.refunded, &o.negative, &o.otype, &o.platfRows); err != nil {
		return nil, err
	}
	// D1 no amount below 0
	for name, v := range map[string]int64{"subtotal": o.sub, "packaging": o.pack, "delivery": o.ship,
		"service": o.svc, "tax": o.tax, "discount": o.disc, "merchant": o.merch, "platform": o.platf,
		"staff": o.staff, "round_off": o.round, "total": o.total, "discount rows below 0": o.negative} {
		if v < 0 || (name == "discount rows below 0" && v > 0) {
			fail("D1 %s is %d", name, v)
		}
	}
	// D6 the ledger adds up to the total (it records rejected orders too)
	if o.ledger != o.total {
		fail("D6 money ledger adds up to %d, total %d", o.ledger, o.total)
	}
	// W4–W7 the order's store balance (docs/REFUND_BALANCE_DESIGN.md §11)
	for _, b := range checkOrderCredit(ctx, a.DB, orderID) {
		fail("%s", b)
	}
	// D12 platform coupons are for the mall: an order on the store's own page
	// has no platform-funded discount (docs/ORDER_TYPES_DESIGN.md §9.4)
	if o.otype == "web" && (o.platf != 0 || o.platfRows != 0) {
		fail("D12 order on the store's own page has platform discount %d (%d rows)", o.platf, o.platfRows)
	}
	// D15 a staff order and its member are of one brand, and its number ends
	// with the member's number (docs/MULTI_BRAND_DESIGN.md §11.2). Orders
	// numbered before store codes (docs/ORDER_TYPES_DESIGN.md) keep their old
	// numbers and are only held to the brand.
	if o.otype == TypeStaff {
		var sameBrand, numbered bool
		err := a.DB.QueryRow(ctx, `SELECT m.brand_id = o.brand_id,
			o.public_no !~ '^[A-Z0-9]{2,8}[0-9]{6}' OR o.public_no ~ ('^[A-Z0-9]{2,8}'
				|| CASE WHEN m.member_no < 1000000 THEN lpad(m.member_no::text, 6, '0') ELSE m.member_no::text END
				|| '(-[0-9]+)?$')
			FROM orders o JOIN store_members m ON m.id = o.member_id WHERE o.id = $1`, orderID).Scan(&sameBrand, &numbered)
		switch {
		case isNoRows(err): // no member on it
		case err != nil:
			return nil, err
		case !sameBrand:
			fail("D15 staff order's member is of another brand")
		case !numbered:
			fail("D15 staff order's number does not end with its member's number")
		}
	}
	// D13 every promotion the order took applied to its brand then, and
	// every store coupon is the brand's own (docs/MULTI_BRAND_DESIGN.md §11.2);
	// platform coupons and the platform's offers on platform orders are not
	// a brand's. Rows from before brands carry no brands and are left alone.
	var otherPromos, otherCoupons int
	if err := a.DB.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE cardinality(d.brand_ids) > 0 AND NOT o.brand_id = ANY(d.brand_ids)),
		count(*) FILTER (WHERE c.issuer = 'store' AND c.brand_id IS DISTINCT FROM o.brand_id)
		FROM order_discounts d JOIN orders o ON o.id = d.order_id
		LEFT JOIN customer_coupons c ON c.id = d.coupon_id
		WHERE d.order_id = $1`, orderID).Scan(&otherPromos, &otherCoupons); err != nil {
		return nil, err
	}
	if otherPromos > 0 || otherCoupons > 0 {
		fail("D13 %d promotions and %d store coupons of another brand", otherPromos, otherCoupons)
	}
	// D14 a platform order was taken by a shop its brand has on the platform,
	// and its commission is that platform's (the rate on the row is the
	// brand's shop's when placed, docs/MULTI_BRAND_DESIGN.md §11.2).
	var noShop bool
	var badRate int
	if err := a.DB.QueryRow(ctx, `SELECT o.platform_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM branch_platforms bp
			WHERE bp.brand_id = o.brand_id AND bp.platform_id = o.platform_id),
		(SELECT count(*) FROM order_platform_fees f WHERE f.order_id = o.id AND f.kind = 'commission'
			AND f.platform_id IS DISTINCT FROM o.platform_id)
		FROM orders o WHERE o.id = $1`, orderID).Scan(&noShop, &badRate); err != nil {
		return nil, err
	}
	if noShop {
		fail("D14 platform order of a brand without a shop on the platform")
	}
	if badRate > 0 {
		fail("D14 commission of another platform")
	}
	// D7 never more given back than was paid
	if o.refunded > o.total {
		fail("D7 refunded %d of %d", o.refunded, o.total)
	}
	if o.status == "rejected" || o.status == "cancelled" {
		return bad, nil // nothing is owed; their amounts are what they were
	}
	// D2 what is paid adds up from its parts
	parts := o.sub + o.pack + o.ship + o.svc - o.disc - o.staff - o.round
	if !o.incl {
		parts += o.tax
	}
	if parts != o.total {
		fail("D2 parts give %d, total %d", parts, o.total)
	}
	// D3 store + platform = discount; D4 the discount rows add up to it
	if o.merch+o.platf != o.disc {
		fail("D3 merchant %d + platform %d, discount %d", o.merch, o.platf, o.disc)
	}
	if o.hasRows && o.rows != o.disc {
		fail("D4 discount rows add up to %d, discount %d", o.rows, o.disc)
	}

	// D5, D8, D9 the dish lines' shares
	type line struct{ disc, svc, tax int64 }
	stored := map[int64]line{}
	var ldisc, lsvc, ltax, voided int64
	rows, err := a.DB.Query(ctx, `SELECT id, discount_share, service_share, tax_share, prep_status = 'cancelled'
		FROM order_items WHERE order_id = $1`, orderID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var l line
		var gone bool
		if err := rows.Scan(&id, &l.disc, &l.svc, &l.tax, &gone); err != nil {
			rows.Close()
			return nil, err
		}
		if gone {
			voided += l.disc + l.svc + l.tax
			continue
		}
		stored[id] = l
		ldisc, lsvc, ltax = ldisc+l.disc, lsvc+l.svc, ltax+l.tax
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if voided != 0 {
		fail("D5 voided dishes still carry %d of discount, service and tax", voided)
	}
	if lsvc != o.svc {
		fail("D8 dishes' service %d, service charge %d", lsvc, o.svc)
	}
	if ltax > o.tax || (o.pack == 0 && ltax != o.tax) {
		fail("D8 dishes' tax %d, tax %d (packaging %d)", ltax, o.tax, o.pack)
	}
	if ldisc != o.perLine+o.staff {
		fail("D9 dishes' discount %d, discount rows' shares %d + checkout discount %d", ldisc, o.perLine, o.staff)
	}

	if o.otype == TypePlatform {
		bad = append(bad, a.checkPlatformOrder(ctx, orderID, o.sub, o.svc, o.ship, o.staff, o.round, o.merch, o.platf, o.total)...)
	}

	// R1–R3 replay. An order without a single dish line (the demo sales
	// figures cmd/seed writes) has nothing to replay.
	if len(stored) == 0 && voided == 0 {
		var any bool
		if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM order_items WHERE order_id = $1)`, orderID).Scan(&any); err != nil {
			return nil, err
		}
		if !any {
			return bad, nil
		}
	}
	m, err := loadMoney(ctx, a.DB, orderID)
	if err != nil {
		return nil, err
	}
	var promo *Promotion
	var off Discount
	if m.Promo != nil && m.PromoAmount > 0 {
		promo, off = m.Promo, promoOn(m.Promo.Kind, m.PromoAmount)
	}
	in := m.input(nil, m.Dish, promo, off)
	if m.AdjMode != "" {
		in.Round = &Adjust{Mode: "round", Amount: m.RoundOff}
	}
	p, err := priceMoney(in)
	if err != nil {
		fail("R1 the order does not price again: %v", err)
		return bad, nil
	}
	// R1 the engine's own rules hold on the replay
	for _, s := range pricing.Check(engineInput(in), p.Result) {
		fail("R1 %s", s)
	}
	// R2 the replay gives the stored amounts
	t := p.Totals
	if t.Subtotal != o.sub || t.ServiceCharge != o.svc || t.TaxAmount != o.tax || t.Discount != o.disc ||
		t.MerchantDiscount != o.merch || t.PlatformSubsidy != o.platf || t.StaffDiscount != o.staff ||
		t.RoundOff != o.round || t.Total != o.total {
		fail("R2 replayed subtotal %d service %d tax %d discount %d (store %d platform %d) staff %d round %d total %d; "+
			"stored %d %d %d %d (%d %d) %d %d %d", t.Subtotal, t.ServiceCharge, t.TaxAmount, t.Discount,
			t.MerchantDiscount, t.PlatformSubsidy, t.StaffDiscount, t.RoundOff, t.Total, o.sub, o.svc, o.tax, o.disc,
			o.merch, o.platf, o.staff, o.round, o.total)
	}
	// R3 and gives every dish the share it has stored
	for _, l := range p.Result.Lines {
		id, _ := strconv.ParseInt(l.Key, 10, 64)
		if s := stored[id]; s.disc != l.Discount || s.svc != l.Service || s.tax != l.Tax {
			fail("R3 dish line %d replayed discount %d service %d tax %d; stored %d %d %d", id,
				l.Discount, l.Service, l.Tax, s.disc, s.svc, s.tax)
		}
	}
	return bad, nil
}

// checkPlatformOrder is D10 and D11 (docs/ORDER_TYPES_DESIGN.md §10.2): a
// platform order has no service charge, delivery fee or discount at checkout,
// and no discount but the platform's two; its commission is rate × base
// unless typed, and what the store is paid is not below 0.
func (a *App) checkPlatformOrder(ctx context.Context, orderID, sub, svc, ship, staff, round, merch, platf, total int64) []string {
	var bad []string
	fail := func(f string, v ...any) { bad = append(bad, fmt.Sprintf(f, v...)) }
	if svc != 0 || ship != 0 || staff != 0 || round != 0 {
		fail("D10 platform order has service %d, delivery %d, checkout discount %d, round-off %d", svc, ship, staff, round)
	}
	var others int64
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM order_discounts WHERE order_id=$1 AND kind NOT IN ($2, $3)`,
		orderID, KindPlatformMerchantPromo, KindPlatformPromo).Scan(&others); err != nil {
		return append(bad, err.Error())
	}
	if others != 0 {
		fail("D10 platform order has %d discount rows of the store's own offers or coupons", others)
	}
	rows, err := a.DB.Query(ctx, `SELECT kind, rate_bp, base_kind, base, amount, manual FROM order_platform_fees WHERE order_id=$1`, orderID)
	if err != nil {
		return append(bad, err.Error())
	}
	defer rows.Close()
	var charges int64
	commissions := 0
	for rows.Next() {
		var kind, baseKind string
		var rate int
		var base, amount int64
		var manual bool
		if err := rows.Scan(&kind, &rate, &baseKind, &base, &amount, &manual); err != nil {
			return append(bad, err.Error())
		}
		charges += amount
		if kind != "commission" {
			continue
		}
		commissions++
		want := commissionBase(baseKind, sub, merch)
		if base != want {
			fail("D11 commission base %d, the order gives %d", base, want)
		}
		if !manual && amount != pricing.Commission(want, rate) {
			fail("D11 commission %d, %d bp of %d is %d", amount, rate, want, pricing.Commission(want, rate))
		}
	}
	if commissions != 1 {
		fail("D11 platform order has %d commission rows", commissions)
	}
	if payout := total + platf - charges; payout < 0 {
		fail("D11 the store is paid %d (total %d + platform %d - charges %d)", payout, total, platf, charges)
	}
	return bad
}

// CheckAllOrderMoney runs CheckOrderMoney over the orders created since
// since (all orders for the zero time), and returns the broken ones by id.
func (a *App) CheckAllOrderMoney(ctx context.Context, since time.Time) (int, map[int64][]string, error) {
	rows, err := a.DB.Query(ctx, `SELECT id FROM orders WHERE created_at >= $1 ORDER BY id`, since)
	if err != nil {
		return 0, nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	out := map[int64][]string{}
	for _, id := range ids {
		bad, err := a.CheckOrderMoney(ctx, id)
		if err != nil {
			return 0, nil, fmt.Errorf("order %d: %w", id, err)
		}
		if len(bad) > 0 {
			out[id] = bad
		}
	}
	return len(ids), out, nil
}

// CheckBrandViews checks rule R of docs/PRICING_CHECK.md §2 for one store
// and period: read brand by brand, the reconciliation and the bill add up to
// the whole store's; every brand together, named one by one, is the whole
// store; and the by-brand block of the whole store adds up to its totals.
// It returns one line per broken sum (nil when all hold) and only reads.
func (a *App) CheckBrandViews(ctx context.Context, branchID int64, from, to string) ([]string, error) {
	var bad []string
	fail := func(f string, v ...any) { bad = append(bad, fmt.Sprintf(f, v...)) }
	whole, err := a.Reconciliation(ctx, branchID, nil, from, to)
	if err != nil {
		return nil, err
	}
	wholeBill, err := a.BillSummary(ctx, branchID, nil, from, to)
	if err != nil {
		return nil, err
	}
	var ids []int64
	if err := pgxCollectInt64(ctx, a.DB, &ids, `SELECT id FROM brands WHERE branch_id=$1 ORDER BY id`, branchID); err != nil {
		return nil, err
	}
	type sums struct{ count, total, refunds, platform, commission, payout int64 }
	recon := func(r *Reconcile) sums {
		s := sums{count: int64(r.Count), total: r.Total, refunds: r.Refunds.Total}
		for _, p := range r.Platforms {
			s.platform += p.Receivable
			s.commission += p.Commission
			s.payout += p.Payout
		}
		return s
	}
	type bill struct{ orders, food, paid, takings, refunded, merchant, platform int64 }
	bills := func(b *BillSummary) bill {
		return bill{int64(b.Orders), b.Food, b.Paid, b.Takings, b.Refunded, b.MerchantDiscount, b.PlatformDiscount}
	}
	var each sums
	var eachBill bill
	for _, id := range ids {
		r, err := a.Reconciliation(ctx, branchID, []int64{id}, from, to)
		if err != nil {
			return nil, err
		}
		if r.Cash != (ReconCash{}) || r.CashCount != nil {
			fail("R brand %d's reconciliation shows the cash drawer", id)
		}
		x := recon(r)
		each.count += x.count
		each.total += x.total
		each.refunds += x.refunds
		each.platform += x.platform
		each.commission += x.commission
		each.payout += x.payout
		b, err := a.BillSummary(ctx, branchID, []int64{id}, from, to)
		if err != nil {
			return nil, err
		}
		y := bills(b)
		eachBill.orders += y.orders
		eachBill.food += y.food
		eachBill.paid += y.paid
		eachBill.takings += y.takings
		eachBill.refunded += y.refunded
		eachBill.merchant += y.merchant
		eachBill.platform += y.platform
	}
	if w := recon(whole); each != w {
		fail("R reconciliation by brand %+v, whole store %+v", each, w)
	}
	if w := bills(wholeBill); eachBill != w {
		fail("R bill by brand %+v, whole store %+v", eachBill, w)
	}
	all, err := a.Reconciliation(ctx, branchID, ids, from, to)
	if err != nil {
		return nil, err
	}
	if recon(all) != recon(whole) {
		fail("R every brand together %+v, whole store %+v", recon(all), recon(whole))
	}
	var blk sums
	for _, b := range whole.Brands {
		blk.count += int64(b.Count)
		blk.total += b.Amount
		blk.refunds += b.Refunds
		blk.platform += b.Platform
		blk.commission += b.Commission
	}
	w := recon(whole)
	if blk.count != w.count || blk.total != w.total || blk.refunds != w.refunds || blk.platform != w.platform || blk.commission != w.commission {
		fail("R by-brand block %+v, whole store %+v", blk, w)
	}
	return bad, nil
}

func pgxCollectInt64(ctx context.Context, q Querier, out *[]int64, sql string, args ...any) error {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return err
		}
		*out = append(*out, v)
	}
	return rows.Err()
}

// CheckAllBrandViews checks rule R for every store with more than one brand
// over its last days (by its own clock, today included).
func (a *App) CheckAllBrandViews(ctx context.Context, days int) (int, map[int64][]string, error) {
	rows, err := a.DB.Query(ctx, `SELECT b.id, to_char((now() AT TIME ZONE b.timezone)::date - $1::int, 'YYYY-MM-DD'),
			to_char((now() AT TIME ZONE b.timezone)::date, 'YYYY-MM-DD')
		FROM branches b WHERE b.deleted_at IS NULL AND (SELECT count(*) FROM brands br WHERE br.branch_id = b.id) > 1 ORDER BY b.id`, days-1)
	if err != nil {
		return 0, nil, err
	}
	type span struct {
		id       int64
		from, to string
	}
	var list []span
	for rows.Next() {
		var s span
		if err := rows.Scan(&s.id, &s.from, &s.to); err != nil {
			rows.Close()
			return 0, nil, err
		}
		list = append(list, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	bad := map[int64][]string{}
	for _, s := range list {
		lines, err := a.CheckBrandViews(ctx, s.id, s.from, s.to)
		if err != nil {
			return 0, nil, err
		}
		if len(lines) > 0 {
			bad[s.id] = lines
		}
	}
	return len(list), bad, nil
}
