package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// docs/PRICING_CHECK.md §3 scenario 14 and 16, docs/ORDER_TYPES_DESIGN.md §9.5:
// the reconciliation page with a platform order next to a cash table. Hand
// numbers: the S9 order (Grab, 30% commission, 20% markup) is 442.00 of
// dishes, 50.00 store-funded, 20.00 platform-funded, 372.00 paid, 392.00 the
// platform owes, 117.60 commission; then another charge 5.00 and a deduction
// 36.00: payout 392.00 − 117.60 − 5.00 − 36.00 = 233.40. The platform's
// statement says 300.00: 66.60 more than expected. A second Grab order the
// platform cancelled counts nowhere, and nothing of a platform order is cash.
func TestPlatformReconciliation(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	grab := grabFor(t, s)
	b, err := a.BranchByID(ctx, s.branchID)
	noErr(t, err)
	loc, _ := time.LoadLocation(b.Timezone)
	today := time.Now().In(loc).Format("2006-01-02")

	// a table paid in cash
	tbl, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "P1", Active: true})
	noErr(t, err)
	bill, err := a.OpenTable(ctx, tbl, 1, nil, s.staff)
	noErr(t, err)
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: s.water, Qty: 2}), s.staff)
	noErr(t, err)
	bill = finishKitchen(t, bill)
	bill, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{PayType: "cash", AmountReceived: 1000000,
		ExpectedManifest: bill.Order.ManifestVersion}, s.staff)
	noErr(t, err)
	cash := bill.Order
	checked(t, "现金堂食", cash.ID)

	// the S9 Grab order, a charge and a deduction
	in := app.PlatformOrderInput{PlatformID: grab, PlatformNo: uniq("GF-"),
		Items:         []app.StaffLine{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.water, Qty: 1}},
		MerchantPromo: 5000, PlatformPromo: 2000}
	o, err := a.PlacePlatformOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	checked(t, "平台单", o.ID)
	_, err = a.AddPlatformFee(ctx, o.ID, 500, "广告费", s.staff)
	noErr(t, err)
	o, err = a.RecordPlatformDeduction(ctx, o.ID, 3600, "顾客投诉漏菜", s.staff)
	noErr(t, err)
	checked(t, "平台扣款", o.ID)
	if o.Platform.Payout != 23340 {
		t.Fatalf("order payout %d, want 233.40", o.Platform.Payout)
	}
	// cancelled on the platform: the platform pays nothing for it
	in.PlatformNo = uniq("GF-")
	o2, err := a.PlacePlatformOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	_, err = a.StaffCancel(ctx, o2.ID, 0, "平台已取消", s.staff)
	noErr(t, err)
	checked(t, "平台已取消", o2.ID)

	r, err := a.Reconciliation(ctx, s.branchID, nil, today, today)
	noErr(t, err)
	if r.Count != 2 || r.Total != cash.Totals.Total+39200 || len(r.Rows) != 2 {
		t.Fatalf("summary: count %d total %d rows %+v", r.Count, r.Total, r.Rows)
	}
	pr := r.Rows[1]
	if r.Rows[0].PayType != "cash" || pr.PayType != "platform" || pr.PlatformID != grab || pr.Name != "Grab" ||
		pr.Count != 1 || pr.Amount != 39200 {
		t.Fatalf("rows: %+v", r.Rows)
	}
	// none of it is cash; the deduction is money given back, not from the drawer
	if r.Cash.Counter != cash.Totals.Total || r.Cash.Expected != cash.Totals.Total || r.Refunds.Total != 3600 ||
		r.Refunds.Cash != 0 || r.Net != r.Total-3600 {
		t.Fatalf("cash %+v refunds %+v net %d", r.Cash, r.Refunds, r.Net)
	}
	d := r.Discounts
	if d.PlatformMerchant != 5000 || d.PlatformSubsidy != 2000 || d.Coupon != 0 || d.Promotion != 0 || d.Total != 5000 {
		t.Fatalf("discounts: %+v", d)
	}
	if len(r.Platforms) != 1 {
		t.Fatalf("platforms: %+v", r.Platforms)
	}
	p := r.Platforms[0]
	if p.PlatformID != grab || p.Count != 1 || p.Subtotal != 44200 || p.Merchant != 5000 || p.Subsidy != 2000 || p.Paid != 37200 ||
		p.Receivable != 39200 || p.Commission != 11760 || p.OtherFees != 500 || p.Deducted != 3600 || p.Payout != 23340 || p.Actual != nil {
		t.Fatalf("platform block: %+v", p)
	}
	// by order type: the table (by staff) and Grab, adding up to the total
	var sum int64
	for _, x := range r.Types {
		sum += x.Amount
	}
	if len(r.Types) != 2 || r.Types[0].Type != "dine_in" || r.Types[0].Source != "staff" || r.Types[0].Amount != cash.Totals.Total ||
		r.Types[1].Type != "platform" || r.Types[1].PlatformID != grab || r.Types[1].Amount != 39200 || sum != r.Total {
		t.Fatalf("types: %+v", r.Types)
	}
	l, err := a.PlatformReconOrders(ctx, s.branchID, nil, today, today, grab)
	noErr(t, err)
	if len(l) != 1 || l[0].OrderID != o.ID || l[0].Receivable != 39200 || l[0].Deducted != 3600 || l[0].Payout != 23340 {
		t.Fatalf("orders under Grab: %+v", l)
	}

	// the platform's statement for the day: 300.00, 66.60 more than expected
	amt := int64(30000)
	wantCode(t, a.SavePlatformPayout(ctx, s.branchID, app.PlatformPayoutInput{PlatformID: grab, From: today, To: today}, s.staff), "VALIDATION")
	noErr(t, a.SavePlatformPayout(ctx, s.branchID, app.PlatformPayoutInput{PlatformID: grab, From: today, To: today, Amount: &amt, Note: "周结"}, s.staff))
	r, err = a.Reconciliation(ctx, s.branchID, nil, today, today)
	noErr(t, err)
	p = r.Platforms[0]
	if p.Actual == nil || *p.Actual != 30000 || *p.Diff != 6660 || len(p.Payouts) != 1 || p.Payouts[0].Note != "周结" {
		t.Fatalf("actual payout: %+v", p)
	}
	// typed again for the same period: replaced, not added
	amt = 23340
	noErr(t, a.SavePlatformPayout(ctx, s.branchID, app.PlatformPayoutInput{PlatformID: grab, From: today, To: today, Amount: &amt}, s.staff))
	r, err = a.Reconciliation(ctx, s.branchID, nil, today, today)
	noErr(t, err)
	p = r.Platforms[0]
	if *p.Actual != 23340 || *p.Diff != 0 || len(p.Payouts) != 1 {
		t.Fatalf("statement typed again: %+v", p)
	}
	noErr(t, a.DeletePlatformPayout(ctx, s.branchID, p.Payouts[0].ID, s.staff))
	wantCode(t, a.DeletePlatformPayout(ctx, s.branchID, p.Payouts[0].ID, s.staff), "NOT_FOUND")
	r, err = a.Reconciliation(ctx, s.branchID, nil, today, today)
	noErr(t, err)
	if r.Platforms[0].Actual != nil {
		t.Fatalf("deleted statement still counted: %+v", r.Platforms[0])
	}
}
