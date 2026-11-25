package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// checked fails the test when the order breaks a rule of docs/PRICING_CHECK.md §2.
func checked(t *testing.T, step string, orderID int64) {
	t.Helper()
	bad, err := testApp.CheckOrderMoney(context.Background(), orderID)
	noErr(t, err)
	if bad != nil {
		t.Fatalf("%s: order %d\n  %s", step, orderID, strings.Join(bad, "\n  "))
	}
}

// docs/PRICING_CHECK.md §3 scenario 12: one order through everything that
// changes its money, checked after every step.
func TestPricingCheckChain(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Name: "half", Kind: "dish_second", Value: 5000, ItemIDs: []int64{s.tea}})
	s.promo(t, app.PromotionInput{Name: "m50", Kind: "amount_off", Threshold: 30000, Value: 5000,
		AppliesDelivery: true, AppliesPickup: true})
	cust, phone := customer(t)
	s.give(t, cust, app.CouponStoreFree, 2000, 0, 7)
	s.give(t, cust, app.CouponPlatformFree, 1500, 0, 7)
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{
		{ItemID: s.tea, Qty: 2}, {ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.water, Qty: 1}}}, phone)
	noErr(t, err)
	p, err := a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)
	o, err := a.OrderByToken(ctx, p.AccessToken)
	noErr(t, err)
	if o.Totals.Total != q.Totals.Total {
		t.Fatalf("placed %d, quoted %d", o.Totals.Total, q.Totals.Total)
	}
	if len(o.Discounts) < 4 {
		t.Fatalf("expected the dish promotion, the promotion and two coupons: %+v", o.Discounts)
	}
	checked(t, "placed", o.ID)

	o, err = a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	o, err = a.SetOrderAdjust(ctx, o.ID, 0, app.Adjust{PercentBP: 9000, Reason: "熟客"}, s.staff)
	noErr(t, err)
	checked(t, "10% off at checkout", o.ID)

	res, err := a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}}, s.staff)
	noErr(t, err)
	o = res.Order
	checked(t, "add-on", o.ID)

	r, err := a.VoidLine(ctx, itemByName(o, "chicken").ID, app.VoidInput{Qty: 1, Reason: "沽清", ReasonKind: "store"}, s.staff, true)
	noErr(t, err)
	checked(t, "void, store's fault", r.Order.ID)
	r, err = a.VoidLine(ctx, itemByName(r.Order, "tea").ID, app.VoidInput{Qty: 1, Reason: "不要了", ReasonKind: "customer"}, s.staff, true)
	noErr(t, err)
	o = r.Order
	checked(t, "void, customer's fault", o.ID)

	o, err = a.SetOrderAdjust(ctx, o.ID, 0, app.Adjust{PercentBP: 9000, Mode: "round", Amount: o.Totals.Total % 100,
		Reason: "抹零"}, s.staff)
	noErr(t, err)
	if o.Totals.Total%100 != 0 {
		t.Fatalf("round-off left %d", o.Totals.Total)
	}
	checked(t, "round-off", o.ID)

	for _, it := range o.Items {
		if it.PrepStatus != "cancelled" {
			o, err = a.SetItemStatus(ctx, o.ID, it.ID, "ready", app.Actor{Type: "system"})
			noErr(t, err)
		}
	}
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	o, err = a.StaffHandoff(ctx, o.ID, s.staff)
	noErr(t, err)
	checked(t, "picked up", o.ID)

	// refund every dish left, one by one: never more back than was paid
	var back int64
	for _, it := range o.Items {
		if it.PrepStatus == "cancelled" {
			continue
		}
		for k := 0; k < it.Qty; k++ {
			rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(it.ID, 1), ReasonKind: "store", Dry: true}, s.staff)
			noErr(t, err)
			if rq.Suggested <= 0 {
				continue
			}
			_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(it.ID, 1), ReasonKind: "store",
				Method: "现金", Reason: "x"}, s.staff)
			noErr(t, err)
			back += rq.Suggested
			checked(t, "refund", o.ID)
		}
	}
	t.Logf("paid %d, refunded %d", o.Totals.Total, back)
	if back != o.Totals.Total { // all of it back is exactly what was paid, not a cent more or less
		t.Fatalf("refunded %d of %d", back, o.Totals.Total)
	}
}

// The check catches a stored amount nothing would give back.
func TestPricingCheckCatchesTampering(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	s.promo(t, app.PromotionInput{Name: "m50", Kind: "amount_off", Threshold: 30000, Value: 5000,
		AppliesDelivery: true, AppliesPickup: true})
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}},
		{ItemID: s.water, Qty: 1}})
	checked(t, "placed", o.ID)
	water, chicken := itemByName(o, "water").ID, itemByName(o, "chicken").ID
	// one cent of discount moved from the chicken to the water: every sum
	// still holds, only the replay sees it
	_, err := testApp.DB.Exec(ctx, `UPDATE order_items SET discount_share = discount_share + CASE id WHEN $1 THEN 1 ELSE -1 END
		WHERE id IN ($1, $2)`, water, chicken)
	noErr(t, err)
	bad, err := testApp.CheckOrderMoney(ctx, o.ID)
	noErr(t, err)
	if len(bad) == 0 || !strings.HasPrefix(bad[0], "R3") {
		t.Fatalf("moved share not caught: %v", bad)
	}
	_, err = testApp.DB.Exec(ctx, `UPDATE order_items SET discount_share = discount_share - CASE id WHEN $1 THEN 1 ELSE -1 END
		WHERE id IN ($1, $2)`, water, chicken)
	noErr(t, err)
	checked(t, "put back", o.ID)
}
