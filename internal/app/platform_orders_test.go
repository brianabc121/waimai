package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// grabFor turns Grab on for the store with a 30% commission and a 20% markup
// rounded to a whole 1, and returns its id.
func grabFor(t *testing.T, s *store) int64 {
	t.Helper()
	ctx, a := context.Background(), testApp
	var grab int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM delivery_platforms WHERE prefix='Grab'`).Scan(&grab))
	c, m := 3000, 2000
	_, err := a.SaveBranchPlatforms(ctx, s.branchID, 0, []app.BranchPlatformInput{{PlatformID: grab, Enabled: true,
		OwnCommission: &c, OwnMarkup: &m, RoundStep: 100}})
	noErr(t, err)
	return grab
}

func platformFees(t *testing.T, o *app.Order) (commission, payout int64) {
	t.Helper()
	if o.Platform == nil {
		t.Fatalf("order %d has no platform block", o.ID)
	}
	return o.Platform.Commission, o.Platform.Payout
}

// docs/PRICING_CHECK.md §3 scenario 16 and sample S9 (docs/ORDER_TYPES_DESIGN.md §10.3):
// a Grab order at platform prices with the store's offer 50.00 and Grab's
// 20.00, tax included; then the store voids the water; checked at every step.
func TestPlatformOrder(t *testing.T) {
	s := newStore(t, false) // the store charges tax on top; platform orders include it all the same
	ctx, a := context.Background(), testApp
	grab := grabFor(t, s)
	// a store promotion must not reach a platform order (§7.3)
	s.promo(t, app.PromotionInput{Name: "m50", Kind: "amount_off", Threshold: 10000, Value: 5000,
		AppliesDelivery: true, AppliesPickup: true})
	in := app.PlatformOrderInput{PlatformID: grab, PlatformNo: uniq("GF-"), CustomerName: "Ann", Note: "no ice",
		Items:         []app.StaffLine{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.water, Qty: 1}},
		MerchantPromo: 5000, PlatformPromo: 2000}
	q, err := a.PlatformQuote(ctx, s.branchID, in)
	noErr(t, err)
	tt := q.Totals
	// S9: chicken 169.00 × 1.2 = 202.80 → 203.00, water 30.00 × 1.2 = 36.00; 442.00 of dishes
	if q.Lines[0].UnitPrice != 20300 || q.Lines[1].UnitPrice != 3600 || tt.Subtotal != 44200 {
		t.Fatalf("platform prices: %d %d, subtotal %d", q.Lines[0].UnitPrice, q.Lines[1].UnitPrice, tt.Subtotal)
	}
	if tt.MerchantDiscount != 5000 || tt.PlatformSubsidy != 2000 || tt.TaxAmount != 4200 || !tt.TaxInclusive ||
		tt.ServiceCharge != 0 || tt.PackagingFee != 0 || tt.DeliveryFee != 0 || tt.Total != 37200 {
		t.Fatalf("S9 totals: %+v", tt)
	}
	if q.Receivable != 39200 || q.Commission != 11760 || q.Payout != 27440 {
		t.Fatalf("S9 receivable %d commission %d payout %d", q.Receivable, q.Commission, q.Payout)
	}

	o, err := a.PlacePlatformOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	checked(t, "平台单下单", o.ID)
	if o.OrderType != app.TypePlatform || o.PublicNo != "Grab "+in.PlatformNo || o.AcceptanceStatus != "accepted" ||
		o.PayType != "platform" || o.PaymentStatus != "paid" || o.Totals.Total != 37200 || o.Totals.TaxAmount != 4200 {
		t.Fatalf("placed: type=%s no=%s acc=%s pay=%s/%s total=%d tax=%d", o.OrderType, o.PublicNo, o.AcceptanceStatus,
			o.PayType, o.PaymentStatus, o.Totals.Total, o.Totals.TaxAmount)
	}
	var chickenTax, waterTax, chickenDisc int64
	for _, it := range o.Items {
		if it.Name == "chicken" {
			chickenTax, chickenDisc = it.TaxShare, it.DiscountShare
		} else {
			waterTax = it.TaxShare
		}
	}
	if chickenDisc != 4593 || chickenTax != 3858 || waterTax != 342 {
		t.Fatalf("S9 shares: chicken discount %d tax %d, water tax %d", chickenDisc, chickenTax, waterTax)
	}
	if c, p := platformFees(t, o); c != 11760 || p != 27440 || o.Platform.Receivable != 39200 {
		t.Fatalf("S9 commission %d payout %d receivable %d", c, p, o.Platform.Receivable)
	}
	kinds := map[string]int64{}
	for _, d := range o.Discounts {
		kinds[d.Kind] += d.Amount
	}
	if len(kinds) != 2 || kinds[app.KindPlatformMerchantPromo] != 5000 || kinds[app.KindPlatformPromo] != 2000 {
		t.Fatalf("discount rows: %v", kinds)
	}

	// the same platform number twice in a day is refused
	in2 := in
	_, err = a.PlacePlatformOrder(ctx, s.branchID, s.staff, uniq("k"), in2)
	wantCode(t, err, "DUPLICATE")
	if q, err = a.PlatformQuote(ctx, s.branchID, in2); err != nil || q.Duplicate == nil || q.Duplicate.OrderID != o.ID {
		t.Fatalf("quote does not name the order entered: %+v %v", q.Duplicate, err)
	}

	// the store voids the water (its fault): the offer keeps the chicken's share
	var water int64
	for _, it := range o.Items {
		if it.Name == "water" {
			water = it.ID
		}
	}
	r, err := a.VoidLine(ctx, water, app.VoidInput{Reason: "打翻了", ReasonKind: "store"}, s.staff, true)
	noErr(t, err)
	o = r.Order
	checked(t, "店铺原因退一瓶水", o.ID)
	if o.Totals.Subtotal != 40600 || o.Totals.MerchantDiscount != 4593 || o.Totals.PlatformSubsidy != 2000 ||
		o.Totals.TaxAmount != 3858 || o.Totals.Total != 34007 {
		t.Fatalf("S9 after the void: %+v", o.Totals)
	}
	if c, p := platformFees(t, o); c != 10802 || p != 25205 {
		t.Fatalf("S9 after the void: commission %d payout %d", c, p)
	}
}

// docs/PRICING_CHECK.md §3 scenario 16, the rest: an add-on at the markup the
// order was placed with although the store changed it, a customer's void, a
// commission typed by staff, another charge, what the platform took back,
// and the platform cancelling.
func TestPlatformOrderChain(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	grab := grabFor(t, s)
	// tea set by hand on Grab at 80.00
	_, err := a.DB.Exec(ctx, `INSERT INTO item_platform_prices (item_id, platform_id, price) VALUES ($1, $2, 8000)`, s.tea, grab)
	noErr(t, err)
	in := app.PlatformOrderInput{PlatformID: grab, PlatformNo: uniq("GF-"), Pickup: "customer",
		Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}, {ItemID: s.water, Qty: 2}}, PackagingFee: 500, MerchantPromo: 3000}
	o, err := a.PlacePlatformOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	checked(t, "平台单：手设价 + 包装费", o.ID)
	// tea 80.00 + water 36.00 × 2 = 152.00, packing 5.00, less 30.00: 127.00, tax included
	if o.Totals.Subtotal != 15200 || o.Totals.PackagingFee != 500 || o.Totals.Total != 12700 {
		t.Fatalf("placed: %+v", o.Totals)
	}
	// 30% of the dishes after the store's offer: 122.00 → 36.60
	if c, _ := platformFees(t, o); c != 3660 {
		t.Fatalf("commission %d", c)
	}

	// the store raises its Grab markup to 50%; the order keeps its 20%
	c, m := 3000, 5000
	_, err = a.SaveBranchPlatforms(ctx, s.branchID, 0, []app.BranchPlatformInput{{PlatformID: grab, Enabled: true,
		OwnCommission: &c, OwnMarkup: &m, RoundStep: 100}})
	noErr(t, err)
	add, err := a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{
		{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}}}}, s.staff)
	noErr(t, err)
	o = add.Order
	checked(t, "加单按下单时的加价", o.ID)
	var chicken int64
	for _, it := range o.Items {
		if it.Name == "chicken" {
			chicken = it.ID
			if it.UnitPrice != 20300 {
				t.Fatalf("add-on chicken at %d, not 203.00", it.UnitPrice)
			}
		}
	}
	// 355.00 of dishes, the offer still 30.00 (it is not a store promotion: none is picked)
	if o.Totals.Subtotal != 35500 || o.Totals.MerchantDiscount != 3000 || o.Totals.Total != 33000 {
		t.Fatalf("after the add-on: %+v", o.Totals)
	}
	if c, _ := platformFees(t, o); c != 9750 { // 30% × 325.00
		t.Fatalf("commission after the add-on %d", c)
	}

	// the customer cancels the chicken on the platform: the offer is worked out again, still 30.00
	r, err := a.VoidLine(ctx, chicken, app.VoidInput{Reason: "顾客不要了", ReasonKind: "customer"}, s.staff, true)
	noErr(t, err)
	o = r.Order
	checked(t, "顾客原因退菜", o.ID)
	if o.Totals.Total != 12700 {
		t.Fatalf("after the customer's void: %+v", o.Totals)
	}

	// staff type the commission Grab charged, then another charge
	amt := int64(3500)
	o, err = a.SetPlatformCommission(ctx, o.ID, &amt, "合同价", s.staff)
	noErr(t, err)
	checked(t, "手改佣金", o.ID)
	o, err = a.AddPlatformFee(ctx, o.ID, 800, "广告费", s.staff)
	noErr(t, err)
	checked(t, "其他扣费", o.ID)
	if c, p := platformFees(t, o); c != 3500 || o.Platform.OtherFees != 800 || p != 12700-3500-800 {
		t.Fatalf("typed commission %d, other %d, payout %d", c, o.Platform.OtherFees, p)
	}
	big := int64(50000)
	if _, err := a.SetPlatformCommission(ctx, o.ID, &big, "错", s.staff); err == nil {
		t.Fatal("a commission above what the store gets was taken")
	}
	o, err = a.SetPlatformCommission(ctx, o.ID, nil, "", s.staff)
	noErr(t, err)
	checked(t, "佣金改回自动", o.ID)
	if c, _ := platformFees(t, o); c != 3660 {
		t.Fatalf("commission back to the rate: %d", c)
	}

	// what Grab took back for a complaint is a refund paid by the platform
	o, err = a.RecordPlatformDeduction(ctx, o.ID, 3600, "漏了一瓶水", s.staff)
	noErr(t, err)
	checked(t, "记录平台扣款", o.ID)
	if o.Refunded != 3600 || o.Platform.Deducted != 3600 || o.Platform.Payout != 12700-3600-3660-800 {
		t.Fatalf("deduction: refunded %d deducted %d payout %d", o.Refunded, o.Platform.Deducted, o.Platform.Payout)
	}
	if _, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: nil, ReasonKind: "store", Dry: true}, s.staff); err == nil {
		t.Fatal("a platform order was refunded by dish")
	}
	// paid on the platform: it cannot be recorded as paid in cash and land in the drawer
	if _, err := a.SetOrderPayment(ctx, o.ID, app.PayInput{Type: "cash"}, s.staff); err == nil {
		t.Fatal("a platform order's payment was changed to cash")
	}

	// a second order, which the platform cancels before it is collected
	in.PlatformNo, in.MerchantPromo, in.PackagingFee = uniq("GF-"), 0, 0
	o2, err := a.PlacePlatformOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	checked(t, "第二张平台单", o2.ID)
	o2, err = a.StaffCancel(ctx, o2.ID, 0, "平台已取消", s.staff)
	noErr(t, err)
	checked(t, "平台已取消", o2.ID)
	// and its number may be entered again
	_, err = a.PlacePlatformOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)

	// a platform the store does not sell on is refused
	var fp int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM delivery_platforms WHERE prefix='FP'`).Scan(&fp))
	in.PlatformID, in.PlatformNo = fp, uniq("FP-")
	_, err = a.PlacePlatformOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	wantCode(t, err, "PLATFORM_OFF")
}
