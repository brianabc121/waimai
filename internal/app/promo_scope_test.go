package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// Delivery and dine-in promotions never mix (docs/CHECKOUT_DISCOUNT_DESIGN.md §4).
func TestPromotionScopes(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp

	// dine-in promotions cannot be free delivery; the per-phone limit is dropped
	_, err := a.SavePromotion(ctx, s.branchID, 0, app.PromotionInput{Scope: "dine_in", Name: "x", Kind: "free_delivery"}, s.staff)
	wantCode(t, err, "VALIDATION")
	_, err = a.SavePromotion(ctx, s.branchID, 0, app.PromotionInput{Scope: "takeaway", Name: "x", Kind: "amount_off", Threshold: 100, Value: 10}, s.staff)
	wantCode(t, err, "VALIDATION")
	dine := s.promo(t, app.PromotionInput{Scope: "dine_in", Name: "堂食满减", Kind: "amount_off", Threshold: 20000, Value: 3000,
		PerPhoneLimit: intPtr(1), TotalQuota: intPtr(1)})
	deliv := s.promo(t, app.PromotionInput{Name: "外卖满减", Kind: "amount_off", Threshold: 10000, Value: 1000})
	ps, err := a.ListPromotions(ctx, s.branchID)
	noErr(t, err)
	for _, p := range ps {
		if p.ID == dine && (p.Scope != "dine_in" || p.PerPhoneLimit != nil) || p.ID == deliv && p.Scope != "delivery" {
			t.Fatalf("scopes: %+v", p)
		}
	}
	// a promotion cannot move to the other set
	_, err = a.SavePromotion(ctx, s.branchID, dine, app.PromotionInput{Name: "x", Kind: "amount_off", Threshold: 100, Value: 10,
		AppliesDelivery: true}, s.staff)
	wantCode(t, err, "VALIDATION")

	// the delivery quote only sees the delivery promotion
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup",
		Items: []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}}, "")
	noErr(t, err)
	if q.Totals.PromotionID != deliv || q.Totals.Discount != 1000 {
		t.Fatalf("delivery quote: %+v", q.Totals)
	}
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Promotions) != 1 || sf.Promotions[0].ID != deliv || len(sf.DinePromotions) != 1 || sf.DinePromotions[0].ID != dine {
		t.Fatalf("storefront: %+v / %+v", sf.Promotions, sf.DinePromotions)
	}

	// a table bill only sees the dine-in one, and it follows the dishes
	tbl, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "P1", Active: true})
	noErr(t, err)
	bill, err := a.OpenTable(ctx, tbl, 2, nil, s.staff)
	noErr(t, err)
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: s.tea, Qty: 1}), s.staff)
	noErr(t, err)
	if bill.Order.Totals.PromotionID != 0 || bill.Order.Totals.Discount != 0 {
		t.Fatalf("below the threshold: %+v", bill.Order.Totals)
	}
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"),
		batch(app.QuoteLineInput{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}}), s.staff)
	noErr(t, err)
	tt := bill.Order.Totals
	if tt.PromotionID != dine || tt.Discount != 3000 || tt.PromotionName != "堂食满减" {
		t.Fatalf("dine-in promotion: %+v", tt)
	}
	want := app.ComputeTotals(tt.Subtotal, 0, app.TaxConfig{TaxRateBP: tt.TaxRateBP, TaxInclusive: tt.TaxInclusive,
		ServiceChargeBP: serviceBP(tt)}, app.Discount{Goods: 3000})
	if tt.Total != want.Total {
		t.Fatalf("total %d, want %d", tt.Total, want.Total)
	}

	// a second table at the same time also sees it; the quota (1) is taken at payment
	tbl2, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "P2", Active: true})
	noErr(t, err)
	b2, err := a.OpenTable(ctx, tbl2, 2, nil, s.staff)
	noErr(t, err)
	b2, err = a.AddBatch(ctx, b2.SessionID, "staff", uniq("k"),
		batch(app.QuoteLineInput{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}), s.staff)
	noErr(t, err)
	if b2.Order.Totals.PromotionID != dine {
		t.Fatalf("second table: %+v", b2.Order.Totals)
	}
	if s.used(t, dine) != 0 {
		t.Fatal("an open table must not hold the quota")
	}
	bill = finishKitchen(t, bill)
	bill, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: bill.Order.ManifestVersion,
		AmountReceived: bill.Order.Totals.Total, PayMethod: "现金"}, s.staff)
	noErr(t, err)
	if s.used(t, dine) != 1 || bill.Order.Totals.Discount != 3000 {
		t.Fatalf("claimed at payment: %d %+v", s.used(t, dine), bill.Order.Totals)
	}
	// the second table pays after the quota ran out: priced again without it
	b2 = finishKitchen(t, b2)
	_, err = a.CloseTable(ctx, b2.SessionID, app.CloseInput{ExpectedManifest: b2.Order.ManifestVersion,
		AmountReceived: b2.Order.Totals.Total, PayMethod: "现金"}, s.staff)
	wantCode(t, err, "VERSION_CONFLICT")
	b2, err = a.GetBill(ctx, b2.SessionID)
	noErr(t, err)
	if b2.Order.Totals.PromotionID != 0 || b2.Order.Totals.Discount != 0 {
		t.Fatalf("repriced: %+v", b2.Order.Totals)
	}
	_, err = a.CloseTable(ctx, b2.SessionID, app.CloseInput{ExpectedManifest: b2.Order.ManifestVersion,
		AmountReceived: b2.Order.Totals.Total, PayMethod: "现金"}, s.staff)
	noErr(t, err)

	// the trial calculator works for dine-in too
	tr, err := a.TrialPromotions(ctx, s.branchID, app.TrialInput{ServiceType: "dine_in", Subtotal: 50000})
	noErr(t, err)
	if len(tr.Rows) != 1 || tr.Rows[0].ID != dine {
		t.Fatalf("trial: %+v", tr.Rows)
	}
}

// serviceBP recovers the branch's service charge rate from a bill with no discount on it.
func serviceBP(t app.Totals) int {
	goods := t.Subtotal - t.Discount
	if goods == 0 {
		return 0
	}
	return int((t.ServiceCharge*10000 + goods/2) / goods)
}

func finishKitchen(t *testing.T, bill *app.Bill) *app.Bill {
	t.Helper()
	ctx := context.Background()
	for _, it := range bill.Order.Items {
		if it.PrepStatus == "pending" || it.PrepStatus == "preparing" {
			_, err := testApp.SetItemStatus(ctx, bill.Order.ID, it.ID, "ready", app.Actor{Type: "system"})
			noErr(t, err)
		}
	}
	b, err := testApp.GetBill(ctx, bill.SessionID)
	noErr(t, err)
	return b
}
