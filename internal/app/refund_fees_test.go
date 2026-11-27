package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// Sample S11 (docs/PRICING_CHECK.md §4): the last dish refunded takes the
// packaging fee, its tax and the delivery fee with it (brian, 2026-11-27).
// Store: tax 12% added, service 5%, packaging 10.00, delivery 69.00 (not
// taxed). The numbers were worked out by hand.
func TestRefundLastDishTakesFees(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	refund := func(o *app.Order, item int64, amount *int64) *app.RefundQuote {
		t.Helper()
		rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(item, 1), ReasonKind: "store", Dry: true}, s.staff)
		noErr(t, err)
		_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(item, 1), ReasonKind: "store", Amount: amount,
			Method: "现金", Reason: "x"}, s.staff)
		noErr(t, err)
		checked(t, "refund", o.ID)
		return rq
	}
	tw := []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}, {ItemID: s.water, Qty: 1}}

	// A, pickup tea 69.00 + water 30.00: service 4.95, tax 12% of 113.95 =
	// 13.67, total 127.62. Water: 127.62 − 92.34 (tea alone) = 35.28. Tea,
	// the last dish: its share 127.62 − 46.48 (water alone) = 81.14, plus
	// packaging 10.00 and its tax 1.20 = 92.34. All of it comes back.
	o := finishPickup(t, s.place(t, "pickup", tw), s.staff)
	checked(t, "A picked up", o.ID)
	if o.Totals.Total != 12762 {
		t.Fatalf("A total %d", o.Totals.Total)
	}
	if rq := refund(o, itemByName(o, "water").ID, nil); rq.Suggested != 3528 {
		t.Fatalf("A water: suggested %d, want 3528", rq.Suggested)
	}
	if rq := refund(o, itemByName(o, "tea").ID, nil); rq.Suggested != 9234 || rq.Left != 9234 {
		t.Fatalf("A tea (last): suggested %d of %d left, want 9234", rq.Suggested, rq.Left)
	}

	// B, the same order with the water given back short at 30.00: the last
	// dish suggests all that is left, 127.62 − 30.00 = 97.62; the staff may
	// give back less, 92.34
	o = finishPickup(t, s.place(t, "pickup", tw), s.staff)
	short := int64(3000)
	refund(o, itemByName(o, "water").ID, &short)
	less := int64(9234)
	if rq := refund(o, itemByName(o, "tea").ID, &less); rq.Suggested != 9762 || rq.Left != 9762 {
		t.Fatalf("B tea (last): suggested %d of %d left, want 9762", rq.Suggested, rq.Left)
	}

	// C, delivery tea alone: 69.00 + service 3.45 + packaging 10.00 + tax
	// 12% of 82.45 = 9.89 + delivery 69.00 = 161.34. Refunding the tea
	// gives back all of it, not just its share 71.14.
	o = s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	if o.Totals.Total != 16134 {
		t.Fatalf("C total %d", o.Totals.Total)
	}
	o, err := a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	o, err = a.SetItemStatus(ctx, o.ID, o.Items[0].ID, "ready", app.Actor{Type: "system"})
	noErr(t, err)
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	task := o.Delivery.ID
	_, err = a.RiderPickup(ctx, task, s.rider1)
	noErr(t, err)
	_, err = a.RiderArrive(ctx, task, s.rider1)
	noErr(t, err)
	o, err = a.RiderDeliver(ctx, task, s.rider1, o.Totals.Total)
	noErr(t, err)
	checked(t, "C delivered", o.ID)
	if rq := refund(o, o.Items[0].ID, nil); rq.Suggested != 16134 {
		t.Fatalf("C tea (last): suggested %d, want 16134", rq.Suggested)
	}
}
