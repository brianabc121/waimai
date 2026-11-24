package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func voidOf(o *app.Order, itemID int64) *app.OrderVoid {
	for i := range o.Voids {
		if o.Voids[i].ItemID == itemID {
			return &o.Voids[i]
		}
	}
	return nil
}

// docs/VOID_ITEM_DESIGN.md §4 to §6 on a delivery order.
func TestVoidDelivery(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	small := s.promo(t, app.PromotionInput{Kind: "amount_off", Threshold: 40000, Value: 5000})
	big := s.promo(t, app.PromotionInput{Kind: "amount_off", Threshold: 60000, Value: 10000})
	// chicken 3 × 169 + tea 69 + water 30 = 606: the big promotion
	o := s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 3, OptionIDs: []int64{s.spiceMild}},
		{ItemID: s.tea, Qty: 1}, {ItemID: s.water, Qty: 1}})
	if o.Totals.PromotionID != big || !o.VoidOpen {
		t.Fatalf("placed: %+v open=%v", o.Totals, o.VoidOpen)
	}
	o, err := a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	tea, chicken, water := itemByName(o, "tea"), itemByName(o, "chicken"), itemByName(o, "water")
	before := o.Totals

	// a quote changes nothing: the customer's fault drops to the small
	// promotion, the store's keeps the big one at its share of what is left
	// (100.00 × 537/606)
	q, err := a.QuoteVoid(ctx, tea.ID, app.VoidInput{ReasonKind: "customer"}, false)
	noErr(t, err)
	if q.Wasted || q.NeedsApproval || q.After.PromotionID != small || !q.PromoDown || q.Less != before.Total-q.After.Total ||
		q.After.Subtotal != before.Subtotal-6900 || q.After.DeliveryFee != before.DeliveryFee {
		t.Fatalf("customer quote: %+v", q)
	}
	qs, err := a.QuoteVoid(ctx, tea.ID, app.VoidInput{ReasonKind: "store"}, false)
	noErr(t, err)
	if qs.After.PromotionID != big || qs.After.Discount != 8861 || !qs.PromoDown || qs.Less <= 5000 || qs.Version != o.Version {
		t.Fatalf("store quote: %+v", qs)
	}
	o2, err := a.GetOrder(ctx, o.ID)
	noErr(t, err)
	if o2.Totals != before || len(o2.Voids) != 0 {
		t.Fatalf("a quote changed the order: %+v", o2.Totals)
	}

	// a reason is needed; a stale version is refused
	_, err = a.VoidLine(ctx, tea.ID, app.VoidInput{ReasonKind: "customer"}, s.staff, false)
	wantCode(t, err, "VALIDATION")
	_, err = a.VoidLine(ctx, tea.ID, app.VoidInput{Reason: "x", ExpectedVersion: o.Version - 1}, s.staff, false)
	wantCode(t, err, "VERSION_CONFLICT")

	// the tea, not started, the customer's change of mind
	r, err := a.VoidLine(ctx, tea.ID, app.VoidInput{Reason: "顾客不要了", ReasonKind: "customer", ExpectedVersion: o.Version}, s.staff, false)
	noErr(t, err)
	o = r.Order
	v := voidOf(o, tea.ID)
	if v == nil || v.Wasted || v.NeedsAck || v.WasStatus != "pending" || v.Before != before.Total || v.After != o.Totals.Total ||
		o.Totals.PromotionID != small || o.Totals.Total != q.After.Total {
		t.Fatalf("after void: %+v void=%+v", o.Totals, v)
	}
	var promoLines int
	for _, d := range o.Discounts {
		if d.Kind == "promotion" {
			promoLines++
			if d.Amount != 5000 {
				t.Fatalf("promotion line: %+v", d)
			}
		}
	}
	if promoLines != 1 {
		t.Fatalf("discount lines: %+v", o.Discounts)
	}
	// voiding it again does nothing
	_, err = a.VoidLine(ctx, tea.ID, app.VoidInput{Reason: "again"}, s.staff, false)
	noErr(t, err)

	// a started chicken needs the permission or an approval; one of three goes
	o, err = a.SetItemStatus(ctx, o.ID, chicken.ID, "start", s.staff)
	noErr(t, err)
	_, err = a.VoidLine(ctx, chicken.ID, app.VoidInput{Qty: 1, Reason: "上太慢"}, s.staff, false)
	wantCode(t, err, "VOID_NEEDS_APPROVAL")
	q, err = a.QuoteVoid(ctx, chicken.ID, app.VoidInput{Qty: 1}, false)
	noErr(t, err)
	if !q.NeedsApproval || !q.Wasted || q.WasStatus != "preparing" || q.MaxQty != 3 {
		t.Fatalf("started quote: %+v", q)
	}
	mgr := s.staff
	mgr.ApprovedBy = 1
	r, err = a.VoidLine(ctx, chicken.ID, app.VoidInput{Qty: 1, Reason: "上太慢"}, mgr, true)
	noErr(t, err)
	o = r.Order
	if countItems(o, "chicken") != 2 || len(o.Voids) != 2 {
		t.Fatalf("split: chicken=%d voids=%d", countItems(o, "chicken"), len(o.Voids))
	}
	cv := o.Voids[1]
	if !cv.Wasted || !cv.NeedsAck || cv.AckAt != nil || cv.Qty != 1 || cv.Amount != 16900 || cv.ItemID == chicken.ID {
		t.Fatalf("chicken void: %+v", cv)
	}
	// the store's fault keeps the small promotion although the dishes are now under its threshold
	if o.Totals.Subtotal != 33800+3000 || o.Totals.PromotionID != small {
		t.Fatalf("after the chicken: %+v", o.Totals)
	}
	noErr(t, a.AckVoid(ctx, cv.ID, s.staff))
	o, err = a.GetOrder(ctx, o.ID)
	noErr(t, err)
	if o.Voids[1].AckAt == nil {
		t.Fatal("ack not recorded")
	}

	// water needs no cooking: ready but not lost, no approval needed
	q, err = a.QuoteVoid(ctx, water.ID, app.VoidInput{}, false)
	noErr(t, err)
	if q.Wasted || q.NeedsApproval || q.WasStatus != "ready" {
		t.Fatalf("water quote: %+v", q)
	}

	// the last dishes cannot go: that is cancelling the order
	chickenLeft := itemByName(o, "chicken")
	_, err = a.VoidLine(ctx, water.ID, app.VoidInput{Reason: "x"}, s.staff, true)
	noErr(t, err)
	_, err = a.VoidLine(ctx, chickenLeft.ID, app.VoidInput{Reason: "x"}, s.staff, true)
	wantCode(t, err, "LAST_DISH")

	// packed, then a void: packing is checked again and the old list refused
	o, err = a.SetItemStatus(ctx, o.ID, chickenLeft.ID, "ready", s.staff)
	noErr(t, err)
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	stale := o.ManifestVersion
	r, err = a.VoidLine(ctx, chickenLeft.ID, app.VoidInput{Qty: 1, Reason: "做错了"}, s.staff, true)
	noErr(t, err)
	o = r.Order
	if o.PackingStatus != "ready_to_pack" || o.FulfillmentStatus != "none" || o.Voids[len(o.Voids)-1].WasStatus != "packed" {
		t.Fatalf("after packed void: %s %s %+v", o.PackingStatus, o.FulfillmentStatus, o.Voids[len(o.Voids)-1])
	}
	_, err = a.Pack(ctx, o.ID, 1, stale, s.staff)
	wantCode(t, err, "MANIFEST_CHANGED")
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)

	// the rider takes it: no more voids
	task := o.Delivery.ID
	o, err = a.RiderPickup(ctx, task, s.rider1)
	noErr(t, err)
	if o.VoidOpen {
		t.Fatal("void still open after pickup")
	}
	_, err = a.VoidLine(ctx, chickenLeft.ID, app.VoidInput{Qty: 1, Reason: "x"}, s.staff, true)
	wantCode(t, err, "VOID_CLOSED")
	// a refund waits for the delivery
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(chickenLeft.ID, 1), Method: "现金", Reason: "凉了"}, s.staff)
	wantCode(t, err, "REFUND_NOT_YET")

	_, err = a.RiderArrive(ctx, task, s.rider1)
	noErr(t, err)
	o, err = a.RiderDeliver(ctx, task, s.rider1, o.Totals.Total)
	noErr(t, err)
	if !o.RefundOpen || o.Cash.AmountDue != o.Totals.Total {
		t.Fatalf("delivered: refund_open=%v cash=%+v total=%d", o.RefundOpen, o.Cash, o.Totals.Total)
	}

	// refund by dish: the suggested amount, never more
	rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(chickenLeft.ID, 1), ReasonKind: "store", Dry: true}, s.staff)
	noErr(t, err)
	if rq.Dishes != 16900 || rq.Suggested <= 0 || rq.Suggested > o.Totals.Total {
		t.Fatalf("refund quote: %+v", rq)
	}
	tooMuch := rq.Suggested + 1
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(chickenLeft.ID, 1), Amount: &tooMuch, Method: "现金", Reason: "凉了"}, s.staff)
	wantCode(t, err, "VALIDATION")
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(chickenLeft.ID, 2), Method: "现金", Reason: "凉了"}, s.staff)
	wantCode(t, err, "VALIDATION")
	_, o, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(chickenLeft.ID, 1), Method: "现金", Reason: "凉了"}, s.staff)
	noErr(t, err)
	if len(o.Refunds) != 1 || o.Refunded != rq.Suggested || o.Refunds[0].Lines[0].Qty != 1 {
		t.Fatalf("refunds: %+v", o.Refunds)
	}
	// that portion is refunded: no second time
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(chickenLeft.ID, 1), Method: "现金", Reason: "again"}, s.staff)
	wantCode(t, err, "VALIDATION")

	// the day's log
	log, err := a.VoidLog(ctx, s.branchID, "")
	noErr(t, err)
	if log.Voids != 4 || log.WastedAmount != 16900*2 || log.RefundAmount != rq.Suggested || log.RefundOrders != 1 ||
		len(log.Rows) != 5 || len(log.ByReason) == 0 {
		t.Fatalf("log: %+v", log)
	}
}

func refundLines(itemID int64, qty int) []struct {
	ItemID int64 `json:"item_id"`
	Qty    int   `json:"qty"`
} {
	return []struct {
		ItemID int64 `json:"item_id"`
		Qty    int   `json:"qty"`
	}{{ItemID: itemID, Qty: qty}}
}

// A void that leaves no promotion gives its use back; limited stock comes
// back only for a dish not started; cancelling later does not count it twice.
func TestVoidStockAndPromotionRelease(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	s.promo(t, app.PromotionInput{Kind: "amount_off", Threshold: 20000, Value: 2000, TotalQuota: intPtr(1)})
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.limited, Qty: 1}, {ItemID: s.tea, Qty: 2}})
	if o.Totals.Discount != 2000 {
		t.Fatalf("placed: %+v", o.Totals)
	}
	limited := itemByName(o, "limited")
	r, err := a.VoidLine(ctx, limited.ID, app.VoidInput{Reason: "点错了", ReasonKind: "customer"}, s.staff, false)
	noErr(t, err)
	if r.Order.Totals.PromotionID != 0 || r.Order.Totals.Discount != 0 {
		t.Fatalf("below threshold: %+v", r.Order.Totals)
	}
	// the stock and the promotion's one use are back for the next order
	o2 := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.limited, Qty: 1}, {ItemID: s.tea, Qty: 2}})
	if o2.Totals.Discount != 2000 {
		t.Fatalf("second order: %+v", o2.Totals)
	}
	// cancelling the first order does not return the voided portion again
	_, err = a.Reject(ctx, o.ID, 0, "x", s.staff)
	noErr(t, err)
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s.limited, Qty: 1}}}, "")
	wantCode(t, err, "ITEM_UNAVAILABLE")
}

// A table: the store's fault keeps the dine-in promotion (at its share of
// what is left, even below its threshold), the guests' does not; a dish
// already started needs the permission.
func TestVoidDineIn(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	s.promo(t, app.PromotionInput{Scope: "dine_in", Name: "堂食满减", Kind: "amount_off", Threshold: 30000, Value: 3000})
	tbl, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "V1", Active: true})
	noErr(t, err)
	bill, err := a.OpenTable(ctx, tbl, 2, nil, s.staff)
	noErr(t, err)
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"), batch(
		app.QuoteLineInput{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}, app.QuoteLineInput{ItemID: s.tea, Qty: 1}), s.staff)
	noErr(t, err)
	if bill.Order.Totals.Discount != 3000 {
		t.Fatalf("bill: %+v", bill.Order.Totals)
	}
	chicken := itemByName(bill.Order, "chicken")
	tea := itemByName(bill.Order, "tea")
	q, err := a.QuoteVoid(ctx, chicken.ID, app.VoidInput{Qty: 1, ReasonKind: "customer"}, false)
	noErr(t, err)
	if q.After.Discount != 0 || !q.PromoDown {
		t.Fatalf("guests' fault: %+v", q.After)
	}
	r, err := a.VoidLine(ctx, chicken.ID, app.VoidInput{Qty: 1, Reason: "沽清了", ReasonKind: "store"}, s.staff, false)
	noErr(t, err)
	// 30.00 × 238/407
	if r.Order.Totals.Discount != 1754 || r.Order.Totals.Subtotal != 16900+6900 || r.SessionID == nil {
		t.Fatalf("store's fault: %+v", r.Order.Totals)
	}
	_, err = a.SetItemStatus(ctx, bill.Order.ID, tea.ID, "start", s.staff)
	noErr(t, err)
	_, err = a.VoidLine(ctx, tea.ID, app.VoidInput{Reason: "上太慢"}, s.staff, false)
	wantCode(t, err, "VOID_NEEDS_APPROVAL")
}
