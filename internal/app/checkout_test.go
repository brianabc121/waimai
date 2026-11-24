package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// payPH puts the test store in the Philippines and returns its GCash method.
func payPH(t *testing.T, s *store) int64 {
	t.Helper()
	ctx := context.Background()
	_, err := testApp.DB.Exec(ctx, `UPDATE branches SET country_code='PH' WHERE id=$1`, s.branchID)
	noErr(t, err)
	opts, err := testApp.PayOptions(ctx, s.branchID)
	noErr(t, err)
	for _, c := range opts.Channels {
		if c.Name == "GCash" {
			return c.ID
		}
	}
	t.Fatalf("no GCash: %+v", opts.Channels)
	return 0
}

// Discounts at checkout and payment types at a table
// (docs/CHECKOUT_DISCOUNT_DESIGN.md §5, §6).
func TestDineCheckoutDiscount(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	gcash := payPH(t, s)
	s.promo(t, app.PromotionInput{Scope: "dine_in", Kind: "amount_off", Threshold: 30000, Value: 5000})

	tbl, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "C1", Active: true})
	noErr(t, err)
	bill, err := a.OpenTable(ctx, tbl, 2, nil, s.staff)
	noErr(t, err)
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"),
		batch(app.QuoteLineInput{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}), s.staff)
	noErr(t, err)
	bill = finishKitchen(t, bill)
	o := bill.Order
	if o.Totals.Discount != 5000 {
		t.Fatalf("promotion: %+v", o.Totals)
	}

	// 10% off what the dishes still cost after the promotion; service and tax follow
	pv, err := a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{PercentBP: 9000})
	noErr(t, err)
	goods := o.Totals.Subtotal - 5000
	staff := goods - goods*9/10
	want := app.ComputeTotals(o.Totals.Subtotal, 0, app.TaxConfig{TaxRateBP: o.Totals.TaxRateBP, TaxInclusive: true,
		ServiceChargeBP: 500}, app.Discount{Goods: 5000 + staff})
	if pv.StaffDiscount != staff || pv.Discount != 5000 || pv.Total != want.Total || pv.ServiceCharge != want.ServiceCharge {
		t.Fatalf("10%% off: %+v, want total %d", pv, want.Total)
	}
	// then a round-off on top
	round := pv.Total % 1000
	if round == 0 {
		round = 500
	}
	pv2, err := a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{PercentBP: 9000, Mode: "round", Amount: round})
	noErr(t, err)
	if pv2.RoundOff != round || pv2.Total != pv.Total-round || pv2.TaxAmount != pv.TaxAmount {
		t.Fatalf("round-off: %+v", pv2)
	}
	// typed amount: on its own, with a reason, never more than the bill
	_, err = a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{PercentBP: 9000, Mode: "set_total", Amount: 100, Reason: "x"})
	wantCode(t, err, "VALIDATION")
	_, err = a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{Mode: "set_total", Amount: 100})
	wantCode(t, err, "VALIDATION")
	_, err = a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{Mode: "set_total", Amount: o.Totals.Total + 1, Reason: "x"})
	wantCode(t, err, "VALIDATION")
	free, err := a.PreviewOrderAdjust(ctx, o.ID, app.Adjust{Mode: "set_total", Amount: 0, Reason: "员工餐"})
	noErr(t, err)
	if free.Total != 0 || free.RoundOff != o.Totals.Total {
		t.Fatalf("on the house: %+v", free)
	}

	// paying: the cashier saw another amount -> the bill changed
	stale := o.Totals.Total
	_, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: o.ManifestVersion, ExpectedTotal: &stale,
		Adjust: &app.Adjust{PercentBP: 9000, Mode: "round", Amount: round}, PayType: "online", PayChannelID: gcash}, s.staff)
	wantCode(t, err, "VERSION_CONFLICT")
	// a method the store does not take
	_, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: o.ManifestVersion,
		PayType: "online", PayChannelID: 999999}, s.staff)
	wantCode(t, err, "VALIDATION")
	approver := s.staff
	approver.ApprovedBy = s.staff.ID
	bill, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: o.ManifestVersion, ExpectedTotal: &pv2.Total,
		Adjust: &app.Adjust{PercentBP: 9000, Mode: "round", Amount: round, Reason: "熟客"}, PayType: "online", PayChannelID: gcash}, approver)
	noErr(t, err)
	o = bill.Order
	if o.Totals.Total != pv2.Total || o.Totals.StaffDiscount != staff || o.Totals.RoundOff != round || o.Totals.AdjPercentBP != 9000 ||
		o.PayType != "online" || o.PayChannelID != gcash || o.PayMethod != "GCash" || *o.AmountReceived != pv2.Total ||
		o.AdjReason != "熟客" || o.AdjBy == "" || o.AdjApprovedBy == "" || o.AdjAt == nil {
		t.Fatalf("closed: %+v %+v", o.Totals, o)
	}

	// cash: the change is what was received less the bill; a store can stop taking cash
	tbl2, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "C2", Active: true})
	noErr(t, err)
	b2, err := a.OpenTable(ctx, tbl2, 1, nil, s.staff)
	noErr(t, err)
	b2, err = a.AddBatch(ctx, b2.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: s.water, Qty: 1}), s.staff)
	noErr(t, err)
	noErr(t, a.SavePaySettings(ctx, s.branchID, app.PaySettingsInput{Cash: false, Card: true, Off: []int64{gcash}}, s.staff))
	opts, err := a.PayOptions(ctx, s.branchID)
	noErr(t, err)
	for _, c := range opts.Channels {
		if c.ID == gcash {
			t.Fatal("a method the store turned off is offered")
		}
	}
	_, err = a.CloseTable(ctx, b2.SessionID, app.CloseInput{ExpectedManifest: b2.Order.ManifestVersion,
		AmountReceived: 100000, PayType: "cash"}, s.staff)
	wantCode(t, err, "VALIDATION")
	noErr(t, a.SavePaySettings(ctx, s.branchID, app.PaySettingsInput{Cash: true, Card: true}, s.staff))
	b2, err = a.CloseTable(ctx, b2.SessionID, app.CloseInput{ExpectedManifest: b2.Order.ManifestVersion,
		AmountReceived: 100000, PayType: "cash"}, s.staff)
	noErr(t, err)
	if b2.Order.PayType != "cash" || *b2.Order.AmountReceived != 100000 || b2.Order.Totals.StaffDiscount != 0 {
		t.Fatalf("cash: %+v", b2.Order)
	}
}

// Delivery and pickup orders taken by staff: a discount at checkout, already
// paid online, changed on the order page, kept through an add-on.
func TestStaffOrderDiscountAndPrepaid(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	gcash := payPH(t, s)

	in := app.StaffOrderInput{
		Member:      app.MemberInput{Phone: "+639450003333", Source: "phone", AddressLine: "1 Ayala Ave"},
		ServiceType: "delivery", ZoneID: &s.zoneID,
		Items:  []app.StaffLine{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}},
		Adjust: &app.Adjust{PercentBP: 9000, Mode: "round", Amount: 100},
	}
	plain := in
	plain.Adjust = nil
	q0, err := a.StaffQuote(ctx, s.branchID, plain)
	noErr(t, err)
	q, err := a.StaffQuote(ctx, s.branchID, in)
	noErr(t, err)
	if q.Totals.StaffDiscount != q0.Totals.Subtotal/10 || q.Totals.RoundOff != 100 || q.Totals.Total >= q0.Totals.Total ||
		q.Totals.DeliveryFee != q0.Totals.DeliveryFee {
		t.Fatalf("staff quote: %+v vs %+v", q.Totals, q0.Totals)
	}
	in.Pay = &app.PayInput{Type: "online", ChannelID: gcash}
	in.ExpectedTotal = &q0.Totals.Total
	_, err = a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	wantCode(t, err, "QUOTE_CHANGED")
	in.ExpectedTotal = &q.Totals.Total
	out, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	o := out.Order
	if o.Totals.Total != q.Totals.Total || o.PaymentStatus != "paid" || o.PaymentMethod != "prepaid" || o.PayType != "online" ||
		o.PayMethod != "GCash" || o.Totals.StaffDiscount == 0 {
		t.Fatalf("prepaid order: %+v %+v", o, o.Totals)
	}
	// a paid order's discount cannot change any more
	_, err = a.SetOrderAdjust(ctx, o.ID, 0, app.Adjust{}, s.staff)
	wantCode(t, err, "INVALID_STATE")
	// the rider collects nothing
	o, err = a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	for _, it := range o.Items {
		o, err = a.SetItemStatus(ctx, o.ID, it.ID, "ready", s.staff)
		noErr(t, err)
	}
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	o, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider1)
	noErr(t, err)
	o, err = a.RiderDeliver(ctx, o.Delivery.ID, s.rider1, 0)
	noErr(t, err)
	if o.Cash != nil || o.PaymentStatus != "paid" {
		t.Fatalf("prepaid delivered: %+v", o.Cash)
	}

	// cash on delivery: discount given later on the order page, then an add-on
	in2 := in
	in2.Adjust, in2.Pay, in2.ExpectedTotal = nil, nil, nil
	out, err = a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in2)
	noErr(t, err)
	o = out.Order
	before := o.Totals
	o, err = a.SetOrderAdjust(ctx, o.ID, o.Version, app.Adjust{PercentBP: 8000, Mode: "round", Amount: 50}, s.staff)
	noErr(t, err)
	if o.Totals.StaffDiscount != before.Subtotal/5 || o.Totals.RoundOff != 50 || o.Totals.Discount != before.Discount {
		t.Fatalf("adjusted later: %+v", o.Totals)
	}
	o, err = staffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}}}, s.staff)
	noErr(t, err)
	if o.Totals.AdjPercentBP != 8000 || o.Totals.StaffDiscount != o.Totals.Subtotal/5 || o.Totals.RoundOff != 0 || o.Totals.AdjMode != "" {
		t.Fatalf("after add-on: %+v", o.Totals)
	}
	// taking the discount away gives the plain price back
	o, err = a.SetOrderAdjust(ctx, o.ID, o.Version, app.Adjust{}, s.staff)
	noErr(t, err)
	if o.Totals.StaffDiscount != 0 || o.AdjBy != "" || o.AdjAt != nil {
		t.Fatalf("cleared: %+v %+v", o.Totals, o)
	}
	// the customer paid the rider by GCash at the door
	o, err = a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	for _, it := range o.Items {
		if it.PrepStatus != "ready" {
			o, err = a.SetItemStatus(ctx, o.ID, it.ID, "ready", s.staff)
			noErr(t, err)
		}
	}
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	o, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider1)
	noErr(t, err)
	o, err = a.RiderDeliverPaid(ctx, o.Delivery.ID, s.rider1, 0, &app.PayInput{Type: "online", ChannelID: gcash})
	noErr(t, err)
	if o.Cash != nil || o.PaymentStatus != "paid" || o.PayType != "online" || o.PayMethod != "GCash" {
		t.Fatalf("paid at the door: %+v %+v", o.Cash, o)
	}
	// corrected to cash afterwards
	o, err = a.SetOrderPayment(ctx, o.ID, app.PayInput{Type: "card"}, s.staff)
	noErr(t, err)
	if o.PayType != "card" || o.PayMethod != "" {
		t.Fatalf("corrected: %+v", o)
	}
}
