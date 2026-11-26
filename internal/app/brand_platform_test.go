package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// TestBrandPlatformOrder: S10a (docs/MULTI_BRAND_DESIGN.md §11.4,
// docs/PRICING_CHECK.md §3 scenario 18). Each brand has its own shop on Grab:
// 一品湘 Grab 30% commission and 20% markup, 好吃党 Grab 25% and 15%, both
// rounded to a whole 1. The same order, pork × 2 and rice × 2, is entered on
// each; checked at every step.
func TestBrandPlatformOrder(t *testing.T) {
	ctx, a := context.Background(), testApp
	b := newBrandStore(t)
	var grab int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM delivery_platforms WHERE prefix='Grab'`).Scan(&grab))
	shop := func(brand int64, commission, markup int) {
		_, err := a.SaveBranchPlatforms(ctx, b.branchID, brand, []app.BranchPlatformInput{{PlatformID: grab, Enabled: true,
			ShopRef: "shop-" + itoa(brand), OwnCommission: &commission, OwnMarkup: &markup, RoundStep: 100}})
		noErr(t, err)
	}
	in := func(brand int64) app.PlatformOrderInput {
		return app.PlatformOrderInput{BrandID: brand, PlatformID: grab, PlatformNo: uniq("GF-"), CustomerName: "Ann",
			Items: []app.StaffLine{{ItemID: b.pork, Qty: 2}, {ItemID: b.rice, Qty: 2}}}
	}

	// Only 一品湘 is on Grab so far: 好吃党 cannot take a Grab order.
	shop(b.main, 3000, 2000)
	_, err := a.PlatformQuote(ctx, b.branchID, in(b.hcd))
	wantCode(t, err, "PLATFORM_OFF")
	shop(b.hcd, 2500, 1500)
	list, err := a.BranchPlatforms(ctx, a.DB, b.branchID, b.hcd, true)
	noErr(t, err)
	if len(list) != 1 || list[0].ShopRef != "shop-"+itoa(b.hcd) || list[0].Commission != 2500 {
		t.Fatalf("好吃党's shops: %+v", list)
	}

	// 好吃党 Grab: 52.00 × 1.15 = 59.80 → 60.00, 3.00 × 1.15 = 3.45 → 3.00;
	// 126.00, commission 25% = 31.50, payout 94.50.
	h, err := a.PlacePlatformOrder(ctx, b.branchID, b.staff, uniq("k"), in(b.hcd))
	noErr(t, err)
	checked(t, "好吃党 Grab", h.ID)
	if h.BrandID != b.hcd || h.Items[0].UnitPrice != 6000 || h.Items[1].UnitPrice != 300 || h.Totals.Total != 12600 {
		t.Fatalf("好吃党 Grab: brand %d, %d %d, total %d", h.BrandID, h.Items[0].UnitPrice, h.Items[1].UnitPrice, h.Totals.Total)
	}
	if c, p := platformFees(t, h); c != 3150 || p != 9450 {
		t.Fatalf("好吃党 Grab commission %d payout %d", c, p)
	}
	if h.Items[0].Name != "辣椒炒肉" {
		t.Fatalf("好吃党 Grab sells its own dish name: %q", h.Items[0].Name)
	}

	// 一品湘 Grab: 58.00 × 1.2 = 69.60 → 70.00, 3.00 × 1.2 = 3.60 → 4.00;
	// 148.00, commission 30% = 44.40, payout 103.60.
	y, err := a.PlacePlatformOrder(ctx, b.branchID, b.staff, uniq("k"), in(0))
	noErr(t, err)
	checked(t, "一品湘 Grab", y.ID)
	if y.BrandID != b.main || y.Items[0].UnitPrice != 7000 || y.Items[1].UnitPrice != 400 || y.Totals.Total != 14800 {
		t.Fatalf("一品湘 Grab: brand %d, %d %d, total %d", y.BrandID, y.Items[0].UnitPrice, y.Items[1].UnitPrice, y.Totals.Total)
	}
	if c, p := platformFees(t, y); c != 4440 || p != 10360 {
		t.Fatalf("一品湘 Grab commission %d payout %d", c, p)
	}

	// A price set by hand on Grab is 一品湘's only.
	_, err = a.DB.Exec(ctx, `INSERT INTO item_platform_prices (item_id, platform_id, price) VALUES ($1,$2,7500)`, b.pork, grab)
	noErr(t, err)
	qy, err := a.PlatformQuote(ctx, b.branchID, in(0))
	noErr(t, err)
	qh, err := a.PlatformQuote(ctx, b.branchID, in(b.hcd))
	noErr(t, err)
	if qy.Lines[0].UnitPrice != 7500 || qh.Lines[0].UnitPrice != 6000 {
		t.Fatalf("hand-set Grab price: 一品湘 %d, 好吃党 %d", qy.Lines[0].UnitPrice, qh.Lines[0].UnitPrice)
	}

	// Changing 好吃党's terms later does not touch the order it took.
	shop(b.hcd, 2000, 3000)
	checked(t, "好吃党 Grab 改条款后", h.ID)

	// D14: an order of a brand without a shop on the platform is caught.
	_, err = a.DB.Exec(ctx, `DELETE FROM branch_platforms WHERE brand_id=$1`, b.hcd)
	noErr(t, err)
	bad, err := a.CheckOrderMoney(ctx, h.ID)
	noErr(t, err)
	if len(bad) != 1 || bad[0][:3] != "D14" {
		t.Fatalf("D14: %v", bad)
	}
	shop(b.hcd, 2500, 1500)
	checked(t, "好吃党 Grab 恢复", h.ID)
}
