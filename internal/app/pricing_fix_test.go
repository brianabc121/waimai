package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

func pickupDone(t *testing.T, o *app.Order) *app.Order {
	t.Helper()
	ctx, a, staff := context.Background(), testApp, app.Actor{Type: "staff", ID: 1}
	o, err := a.Accept(ctx, o.ID, 0, staff)
	noErr(t, err)
	for _, it := range o.Items {
		o, err = a.SetItemStatus(ctx, o.ID, it.ID, "ready", app.Actor{Type: "system"})
		noErr(t, err)
	}
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, staff)
	noErr(t, err)
	o, err = a.StaffHandoff(ctx, o.ID, staff)
	noErr(t, err)
	return o
}

// docs/PRICING_REVIEW.md 问题 1：堂食有活动时按菜退款
func TestPricingFixDineRefundPromo(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Scope: "dine_in", Kind: "percent_off", Value: 2000})
	tbl, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "R1", Active: true})
	noErr(t, err)
	bill, err := a.OpenTable(ctx, tbl, 2, nil, s.staff)
	noErr(t, err)
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"),
		batch(app.QuoteLineInput{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}},
			app.QuoteLineInput{ItemID: s.water, Qty: 1}), s.staff)
	noErr(t, err)
	bill = finishKitchen(t, bill)
	o := bill.Order
	bill, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: o.ManifestVersion,
		AmountReceived: o.Totals.Total, PayType: "cash"}, s.staff)
	noErr(t, err)
	o = bill.Order
	water := itemByName(o, "water").ID
	// the guests' fault: the 20% stays on what is left, so 30.00 less 20%, plus its 5% service
	rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(water, 1), ReasonKind: "customer", Dry: true}, s.staff)
	noErr(t, err)
	if o.Totals.Total != 30912 || rq.Suggested != 2520 {
		t.Fatalf("customer's fault: total=%d suggested=%d", o.Totals.Total, rq.Suggested)
	}
	// the store's fault: the promotion keeps its share of what is left, so
	// also 30.00 less its 20% and its service
	rq, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(water, 1), ReasonKind: "store", Dry: true}, s.staff)
	noErr(t, err)
	if rq.Suggested != 2520 {
		t.Fatalf("store's fault: suggested=%d", rq.Suggested)
	}
	// it is paid back in cash: the drawer expects that much less
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(water, 1), ReasonKind: "store", Method: "现金", Reason: "打翻了"}, s.staff)
	noErr(t, err)
	br, err := a.BranchByID(ctx, s.branchID)
	noErr(t, err)
	loc, err := time.LoadLocation(br.Timezone)
	noErr(t, err)
	day := time.Now().In(loc).Format("2006-01-02")
	rc, err := a.Reconciliation(ctx, s.branchID, day, day)
	noErr(t, err)
	if rc.Refunds.Count != 1 || rc.Refunds.Cash != 2520 || rc.Cash.Refunded != 2520 ||
		rc.Cash.Expected != rc.Cash.Counter+rc.Cash.RiderReturned-2520 || rc.Net != rc.Total-2520 {
		t.Fatalf("reconcile: %+v %+v net=%d", rc.Refunds, rc.Cash, rc.Net)
	}
}

// docs/PRICING_REVIEW.md 问题 2：结账打折后按菜退款
func TestPricingFixRefundStaffDiscount(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}},
		{ItemID: s.water, Qty: 1}})
	o, err := a.SetOrderAdjust(ctx, o.ID, 0, app.Adjust{PercentBP: 8000}, s.staff)
	noErr(t, err)
	o = pickupDone(t, o)
	rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "water").ID, 1),
		ReasonKind: "store", Dry: true}, s.staff)
	noErr(t, err)
	// 30.00 less 20%, plus its 5% service; the packaging fee stays
	if o.Totals.Total != 31912 || rq.Suggested != 2520 {
		t.Fatalf("total=%d suggested=%d", o.Totals.Total, rq.Suggested)
	}
}

// docs/PRICING_REVIEW.md 问题 3：店铺原因退菜后优惠明细之和 ≠ 总优惠
func TestPricingFixVoidDiscountLines(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Kind: "amount_off", Threshold: 30000, Value: 10000, AppliesDelivery: true, AppliesPickup: true})
	cust, phone := customer(t)
	s.give(t, cust, app.CouponStoreFree, 5000, 0, 7)
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{
		{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.water, Qty: 1}}}, phone)
	noErr(t, err)
	p, err := a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)
	o, err := a.OrderByToken(ctx, p.AccessToken)
	noErr(t, err)
	o, err = a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	r, err := a.VoidLine(ctx, itemByName(o, "chicken").ID, app.VoidInput{Reason: "沽清", ReasonKind: "store"}, s.staff, true)
	noErr(t, err)
	var lines int64
	for _, d := range r.Order.Discounts {
		lines += d.Amount
	}
	if r.Order.Totals.Discount != 3000 || lines != 3000 {
		t.Fatalf("discount=%d lines=%d %+v", r.Order.Totals.Discount, lines, r.Order.Discounts)
	}
}

// docs/PRICING_REVIEW.md 问题 4：服务费率改了以后加单
func TestPricingFixServiceChargeDrift(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}})
	if o.Totals.ServiceCharge != 1690 {
		t.Fatalf("placed svc=%d", o.Totals.ServiceCharge)
	}
	s.settings(t, func(in *app.SettingsInput) { in.TaxInclusive = false; in.ServiceChargeBP = 1000 })
	res, err := a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}}, s.staff)
	noErr(t, err)
	// still 5%, the rate the order was placed with
	if res.Order.Totals.ServiceCharge != 1840 {
		t.Fatalf("after add-on svc=%d", res.Order.Totals.ServiceCharge)
	}
}

// docs/PRICING_REVIEW.md 问题 5：活动类型被编辑后预览结账优惠
func TestPricingFixPromoKindDrift(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	id := s.promo(t, app.PromotionInput{Kind: "free_delivery", AppliesDelivery: true})
	o := s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}})
	pv, err := a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{Mode: "round", Amount: 1})
	noErr(t, err)
	before := pv.Total
	_, err = a.SavePromotion(ctx, s.branchID, id, app.PromotionInput{Name: "x", Kind: "amount_off", Threshold: 6900,
		Value: 6900, AppliesDelivery: true}, s.staff)
	noErr(t, err)
	pv, err = a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{Mode: "round", Amount: 1})
	noErr(t, err)
	if pv.Total != before || before != 40868 {
		t.Fatalf("before=%d after=%d", before, pv.Total)
	}
}

// An order taken by staff keeps its promotion's row, so a void by the
// store's fault keeps the promotion (before, it had no row and was dropped).
func TestPricingFixStaffOrderPromotion(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Kind: "amount_off", Threshold: 30000, Value: 5000, AppliesDelivery: true, AppliesPickup: true})
	out, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), app.StaffOrderInput{
		Member:      app.MemberInput{Phone: "+639450004444", Source: "phone"},
		ServiceType: "pickup",
		Items:       []app.StaffLine{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.water, Qty: 1}},
	})
	noErr(t, err)
	o := out.Order
	if len(o.Discounts) != 1 || o.Discounts[0].Kind != "promotion" || o.Discounts[0].Amount != 5000 ||
		o.Totals.MerchantDiscount != 5000 {
		t.Fatalf("staff order discounts: %+v %+v", o.Discounts, o.Totals)
	}
	o, err = a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	r, err := a.VoidLine(ctx, itemByName(o, "water").ID, app.VoidInput{Reason: "沽清", ReasonKind: "store"}, s.staff, true)
	noErr(t, err)
	// the promotion stays, at its share of what is left: 50.00 × 338/368
	if r.Order.Totals.Discount != 4592 || len(r.Order.Discounts) != 1 || r.Order.Discounts[0].Amount != 4592 {
		t.Fatalf("after a store void: %+v", r.Order.Totals)
	}
}

// Every order the tests made: its discount rows add up to its discount and
// what it pays adds up from its parts.
func TestPricingInvariantsOverAllOrders(t *testing.T) {
	need(t)
	rows, err := testApp.DB.Query(context.Background(), `SELECT o.id, o.discount,
		COALESCE((SELECT sum(amount) FROM order_discounts d WHERE d.order_id = o.id), 0),
		EXISTS (SELECT 1 FROM order_discounts d WHERE d.order_id = o.id),
		o.subtotal + o.packaging_fee + o.delivery_fee + o.service_charge - o.discount - o.staff_discount - o.round_off
			+ CASE WHEN o.tax_inclusive THEN 0 ELSE o.tax_amount END, o.total,
		o.merchant_discount + o.platform_subsidy
		FROM orders o WHERE o.acceptance_status NOT IN ('rejected','cancelled','expired')
			AND o.updated_at > now() - interval '10 minutes'`)
	noErr(t, err)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, discount, lines, parts, total, split int64
		var has bool
		noErr(t, rows.Scan(&id, &discount, &lines, &has, &parts, &total, &split))
		n++
		if has && lines != discount {
			t.Errorf("order %d: discount %d, rows %d", id, discount, lines)
		}
		if parts != total && total != 0 {
			t.Errorf("order %d: parts %d, total %d", id, parts, total)
		}
		if split != discount {
			t.Errorf("order %d: merchant+platform %d, discount %d", id, split, discount)
		}
	}
	noErr(t, rows.Err())
	t.Logf("%d orders checked", n)
}
