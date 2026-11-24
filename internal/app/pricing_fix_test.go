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
	// the store's fault: the promotion keeps its amount, so all of the 30.00 and its service
	rq, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(water, 1), ReasonKind: "store", Dry: true}, s.staff)
	noErr(t, err)
	if rq.Suggested != 3150 {
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
	if rc.Refunds.Count != 1 || rc.Refunds.Cash != 3150 || rc.Cash.Refunded != 3150 ||
		rc.Cash.Expected != rc.Cash.Counter+rc.Cash.RiderReturned-3150 || rc.Net != rc.Total-3150 {
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
