package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// TestBrandReconciliation: rule R (docs/MULTI_BRAND_DESIGN.md §11.2, §12,
// docs/PRICING_CHECK.md §3 scenario 18). A cash table of the main brand, the
// S10a Grab orders of both brands (一品湘 148.00, commission 44.40; 好吃党
// 126.00, commission 31.50), and 10.00 taken back by Grab on 好吃党's. The
// whole store and each brand are read; checked at every step.
func TestBrandReconciliation(t *testing.T) {
	ctx, a := context.Background(), testApp
	b := newBrandStore(t)
	br, err := a.BranchByID(ctx, b.branchID)
	noErr(t, err)
	loc, _ := time.LoadLocation(br.Timezone)
	today := time.Now().In(loc).Format("2006-01-02")
	var grab int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM delivery_platforms WHERE prefix='Grab'`).Scan(&grab))
	shop := func(brand int64, commission, markup int) {
		_, err := a.SaveBranchPlatforms(ctx, b.branchID, brand, []app.BranchPlatformInput{{PlatformID: grab, Enabled: true,
			OwnCommission: &commission, OwnMarkup: &markup, RoundStep: 100}})
		noErr(t, err)
	}
	shop(b.main, 3000, 2000)
	shop(b.hcd, 2500, 1500)
	in := func(brand int64) app.PlatformOrderInput {
		return app.PlatformOrderInput{BrandID: brand, PlatformID: grab, PlatformNo: uniq("GF-"), CustomerName: "Ann",
			Items: []app.StaffLine{{ItemID: b.pork, Qty: 2}, {ItemID: b.rice, Qty: 2}}}
	}

	// a table of the main brand, 小炒肉 58.00 paid in cash
	tbl, err := a.SaveTable(ctx, b.branchID, 0, app.TableInput{Name: "B1", Active: true})
	noErr(t, err)
	bill, err := a.OpenTable(ctx, tbl, 1, nil, b.staff)
	noErr(t, err)
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: b.pork, Qty: 1}), b.staff)
	noErr(t, err)
	bill = finishKitchen(t, bill)
	bill, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{PayType: "cash", AmountReceived: 1000000,
		ExpectedManifest: bill.Order.ManifestVersion}, b.staff)
	noErr(t, err)
	cash := bill.Order
	checked(t, "一品湘现金堂食", cash.ID)
	if cash.BrandID != b.main || cash.Totals.Total != 5800 {
		t.Fatalf("table: brand %d total %d", cash.BrandID, cash.Totals.Total)
	}

	y, err := a.PlacePlatformOrder(ctx, b.branchID, b.staff, uniq("k"), in(0))
	noErr(t, err)
	checked(t, "一品湘 Grab", y.ID)
	h, err := a.PlacePlatformOrder(ctx, b.branchID, b.staff, uniq("k"), in(b.hcd))
	noErr(t, err)
	checked(t, "好吃党 Grab", h.ID)
	h, err = a.RecordPlatformDeduction(ctx, h.ID, 1000, "漏米饭", b.staff)
	noErr(t, err)
	checked(t, "好吃党 Grab 扣款", h.ID)
	// 好吃党 Grab pays 126.00 − 31.50 − 10.00 = 84.50, typed in from its statement.
	amt := int64(8450)
	noErr(t, a.SavePlatformPayout(ctx, b.branchID, app.PlatformPayoutInput{BrandID: b.hcd, PlatformID: grab, From: today, To: today,
		Amount: &amt}, b.staff))

	// The whole store: 58.00 + 148.00 + 126.00 = 332.00 in three orders; 10.00 taken back.
	w, err := a.Reconciliation(ctx, b.branchID, nil, today, today)
	noErr(t, err)
	if !w.Whole || w.Count != 3 || w.Total != 33200 || w.Refunds.Total != 1000 || w.Net != 32200 || w.Cash.Counter != 5800 {
		t.Fatalf("whole: whole %v count %d total %d refunds %d net %d cash %d", w.Whole, w.Count, w.Total, w.Refunds.Total, w.Net, w.Cash.Counter)
	}
	// one row a brand's shop: 一品湘 Grab first, then 好吃党 Grab
	if len(w.Platforms) != 2 {
		t.Fatalf("platform rows %+v", w.Platforms)
	}
	py, ph := w.Platforms[0], w.Platforms[1]
	if py.BrandID != b.main || py.Receivable != 14800 || py.Commission != 4440 || py.Payout != 10360 || py.Actual != nil {
		t.Fatalf("一品湘 Grab row %+v", py)
	}
	if ph.BrandID != b.hcd || ph.BrandName != "好吃党" || ph.Receivable != 12600 || ph.Commission != 3150 || ph.Deducted != 1000 ||
		ph.Payout != 8450 || ph.Actual == nil || *ph.Actual != 8450 || *ph.Diff != 0 {
		t.Fatalf("好吃党 Grab row %+v", ph)
	}
	// by brand: 一品湘 58.00 + 148.00 = 206.00; 好吃党 126.00, 10.00 taken back
	if len(w.Brands) != 2 {
		t.Fatalf("brand rows %+v", w.Brands)
	}
	if x := w.Brands[0]; x.BrandID != b.main || x.Count != 2 || x.Amount != 20600 || x.Refunds != 0 || x.Platform != 14800 || x.Commission != 4440 {
		t.Fatalf("一品湘 %+v", x)
	}
	if x := w.Brands[1]; x.BrandID != b.hcd || x.Count != 1 || x.Amount != 12600 || x.Refunds != 1000 || x.Platform != 12600 || x.Commission != 3150 {
		t.Fatalf("好吃党 %+v", x)
	}

	// 好吃党 alone: its Grab order only, and no cash drawer.
	hv, err := a.Reconciliation(ctx, b.branchID, []int64{b.hcd}, today, today)
	noErr(t, err)
	if hv.Whole || hv.Count != 1 || hv.Total != 12600 || hv.Refunds.Total != 1000 || hv.Cash != (app.ReconCash{}) || hv.CashCount != nil ||
		len(hv.Platforms) != 1 || hv.Platforms[0].Payout != 8450 || len(hv.Brands) != 1 {
		t.Fatalf("好吃党 view: %+v", hv)
	}
	// 一品湘 alone: the table and its Grab order; the cash stays in the store's view.
	yv, err := a.Reconciliation(ctx, b.branchID, []int64{b.main}, today, today)
	noErr(t, err)
	if yv.Count != 2 || yv.Total != 20600 || yv.Cash.Counter != 0 || len(yv.Platforms) != 1 || yv.Platforms[0].Payout != 10360 {
		t.Fatalf("一品湘 view: count %d total %d cash %d platforms %+v", yv.Count, yv.Total, yv.Cash.Counter, yv.Platforms)
	}
	l, err := a.PlatformReconOrders(ctx, b.branchID, []int64{b.hcd}, today, today, 0)
	noErr(t, err)
	if len(l) != 1 || l[0].OrderID != h.ID || l[0].BrandID != b.hcd || l[0].Payout != 8450 {
		t.Fatalf("好吃党 platform orders %+v", l)
	}

	// The bill lists each brand's delivery orders.
	bl, err := a.Bills(ctx, b.branchID, []int64{b.hcd}, today, today, "")
	noErr(t, err)
	if bl.Count != 1 || bl.Orders[0].ID != h.ID || bl.Orders[0].BrandID != b.hcd {
		t.Fatalf("好吃党 bill: %+v", bl.Orders)
	}

	// Rule R: the brands add up to the whole store.
	bad, err := a.CheckBrandViews(ctx, b.branchID, today, today)
	noErr(t, err)
	if len(bad) > 0 {
		t.Fatalf("R: %v", bad)
	}
	// A statement typed against the wrong brand is the other shop's.
	wrong := int64(100)
	noErr(t, a.SavePlatformPayout(ctx, b.branchID, app.PlatformPayoutInput{PlatformID: grab, From: today, To: today, Amount: &wrong}, b.staff))
	w, err = a.Reconciliation(ctx, b.branchID, nil, today, today)
	noErr(t, err)
	if *w.Platforms[0].Actual != 100 || *w.Platforms[1].Actual != 8450 {
		t.Fatalf("statements by shop: %+v", w.Platforms)
	}
	wantCode(t, a.SavePlatformPayout(ctx, b.branchID, app.PlatformPayoutInput{BrandID: -1, PlatformID: grab, From: today, To: today,
		Amount: &wrong}, b.staff), "VALIDATION")
	checked(t, "对账后 一品湘 Grab", y.ID)
	checked(t, "对账后 好吃党 Grab", h.ID)
}
