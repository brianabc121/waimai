package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// placeTok places an order and also returns the customer's access token.
func (s *store) placeTok(t *testing.T, service string, lines []app.QuoteLineInput) (*app.Order, string) {
	t.Helper()
	q := s.quote(t, service, lines)
	p, err := testApp.PlaceOrder(context.Background(), s.slug, "+639170000001", uniq("k"),
		app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main St"})
	noErr(t, err)
	o, err := testApp.OrderByToken(context.Background(), p.AccessToken)
	noErr(t, err)
	return o, p.AccessToken
}

func countItems(o *app.Order, name string) int {
	n := 0
	for _, it := range o.Items {
		if it.Name == name && it.PrepStatus != "cancelled" {
			n += it.Qty
		}
	}
	return n
}

func TestStaffAddonDelivery(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	o := s.place(t, "delivery", s.cart())
	if o.Addon == nil || !o.Addon.Open || len(o.Batches) != 1 {
		t.Fatalf("new order addon state: %+v batches=%d", o.Addon, len(o.Batches))
	}
	before := o.Totals

	// a quote changes nothing and says what the order will cost
	q, err := a.StaffAddonQuote(ctx, o.ID, app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.tea, Qty: 2}}})
	noErr(t, err)
	if q.Subtotal != 13800 || q.After.Subtotal != before.Subtotal+13800 || q.After.DeliveryFee != before.DeliveryFee ||
		q.Extra != q.After.Total-before.Total || q.Extra <= 0 {
		t.Fatalf("quote: %+v before=%+v", q, before)
	}
	// a client that showed another total is told to look again
	wrong := q.After.Total + 1
	_, err = a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.tea, Qty: 2}},
		ExpectedTotal: &wrong}, s.staff)
	wantCode(t, err, "QUOTE_CHANGED")
	_, err = a.StaffAddon(ctx, o.ID, "", app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.tea, Qty: 2}}}, s.staff)
	wantCode(t, err, "VALIDATION")

	// before the store accepts: the dishes simply join the order
	key := uniq("k")
	in := app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.tea, Qty: 2}}, RoundTags: []string{"加急"},
		ExpectedTotal: &q.After.Total}
	o, err = a.StaffAddon(ctx, o.ID, key, in, s.staff)
	noErr(t, err)
	if countItems(o, "tea") != 3 || o.Totals.Total != q.After.Total || len(o.Batches) != 2 {
		t.Fatalf("after add-on: tea=%d total=%d/%d batches=%d", countItems(o, "tea"), o.Totals.Total, q.After.Total, len(o.Batches))
	}
	b := o.Batches[1]
	if b.Seq != 2 || b.Source != "staff" || b.Status != "accepted" || len(b.RoundTags) != 1 ||
		*b.Before != before.Total || *b.After != o.Totals.Total {
		t.Fatalf("batch: %+v", b)
	}
	// a double press adds nothing more
	o, err = a.StaffAddon(ctx, o.ID, key, in, s.staff)
	noErr(t, err)
	if countItems(o, "tea") != 3 || len(o.Batches) != 2 {
		t.Fatalf("replay added again: tea=%d", countItems(o, "tea"))
	}

	// accept, cook everything, pack, assign a rider
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
	if !o.Addon.Open || !o.Addon.Repack {
		t.Fatalf("packed order addon state: %+v", o.Addon)
	}
	packed := o.ManifestVersion

	// packed: one more water goes in, and the order is packed again
	o, err = a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.chicken, Qty: 1,
		OptionIDs: []int64{s.spiceMild}}}}, s.staff)
	noErr(t, err)
	if o.PackingStatus != "waiting_items" || o.FulfillmentStatus != "none" || o.ManifestVersion == packed {
		t.Fatalf("after add-on to a packed order: %s %s manifest %d", o.PackingStatus, o.FulfillmentStatus, o.ManifestVersion)
	}
	var added app.OrderItem
	for _, it := range o.Items {
		if it.BatchSeq == 3 {
			added = it
		}
	}
	if added.PrepStatus != "pending" {
		t.Fatalf("added chicken: %+v", added)
	}
	_, err = a.StaffHandoff(ctx, o.ID, s.staff)
	wantCode(t, err, "ITEMS_NOT_READY")
	o, err = a.SetItemStatus(ctx, o.ID, added.ID, "ready", s.staff)
	noErr(t, err)
	_, err = a.Pack(ctx, o.ID, 1, packed, s.staff)
	wantCode(t, err, "MANIFEST_CHANGED")
	o, err = a.Pack(ctx, o.ID, 2, o.ManifestVersion, s.staff)
	noErr(t, err)
	o, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider1)
	noErr(t, err)
	if o.Addon.Open || o.Addon.Why != "handed_off" {
		t.Fatalf("after handoff: %+v", o.Addon)
	}
	_, err = a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}}, s.staff)
	wantCode(t, err, "ADDON_CLOSED")
	// the rider collects the new total
	o, err = a.RiderDeliver(ctx, o.Delivery.ID, s.rider1, o.Totals.Total)
	noErr(t, err)
	if o.Cash.AmountCollected != o.Totals.Total || o.PaymentStatus != "paid" {
		t.Fatalf("cash: %+v", o.Cash)
	}
}

func TestAddonPromotionOnlyGoesUp(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	small := s.promo(t, app.PromotionInput{Kind: "amount_off", Threshold: 40000, Value: 5000})
	o := s.place(t, "pickup", s.cart()) // 43,700
	if o.Totals.PromotionID != small || o.Totals.Discount != 5000 {
		t.Fatalf("order promotion: %+v", o.Totals)
	}
	big := s.promo(t, app.PromotionInput{Kind: "amount_off", Threshold: 60000, Value: 10000})
	// an add-on that reaches the bigger tier switches to it
	o, err := a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.tea, Qty: 3}}}, s.staff)
	noErr(t, err)
	if o.Totals.PromotionID != big || o.Totals.Discount != 10000 || s.used(t, small) != 0 || s.used(t, big) != 1 {
		t.Fatalf("after add-on: %+v used small=%d big=%d", o.Totals, s.used(t, small), s.used(t, big))
	}
	// a promotion ended since never takes off less than the order had
	noErr(t, a.SetPromotionStatus(ctx, s.branchID, big, "ended", s.staff))
	o, err = a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}}, s.staff)
	noErr(t, err)
	if o.Totals.PromotionID != big || o.Totals.Discount != 10000 {
		t.Fatalf("after ended promotion: %+v", o.Totals)
	}
	var promoRows int64
	for _, d := range o.Discounts {
		if d.Kind == "promotion" {
			promoRows += d.Amount
		}
	}
	if promoRows != 10000 {
		t.Fatalf("discount rows: %+v", o.Discounts)
	}
}

func TestCustomerAddonNeedsTheStore(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	o, tok := s.placeTok(t, "delivery", s.cart())
	tea := []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}

	// before acceptance the customer's dishes join the order at once
	o, err := a.CustomerAddon(ctx, tok, uniq("k"), app.AddonInput{Items: tea})
	noErr(t, err)
	if countItems(o, "tea") != 2 || o.Batches[1].Source != "customer" || o.Addon.CustomerLeft != 2 {
		t.Fatalf("pending order add-on: tea=%d %+v", countItems(o, "tea"), o.Addon)
	}
	o, err = a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	total := o.Totals.Total

	// after acceptance it waits for the store, out of the kitchen and the bill
	q, err := a.CustomerAddonQuote(ctx, tok, app.AddonInput{Items: tea})
	noErr(t, err)
	o, err = a.CustomerAddon(ctx, tok, uniq("k"), app.AddonInput{Items: tea, ExpectedTotal: &q.After.Total})
	noErr(t, err)
	b := o.Batches[2]
	if b.Status != "pending" || len(b.Lines) != 1 || b.ExpiresAt == nil || countItems(o, "tea") != 2 ||
		o.Totals.Total != total || !o.Addon.Pending {
		t.Fatalf("waiting add-on: %+v tea=%d total=%d/%d", b, countItems(o, "tea"), o.Totals.Total, total)
	}
	_, err = a.CustomerAddon(ctx, tok, uniq("k"), app.AddonInput{Items: tea})
	wantCode(t, err, "ADDON_PENDING")

	// the store turns it down: nothing changes
	o, err = a.RejectAddon(ctx, o.ID, 3, "sold out", s.staff)
	noErr(t, err)
	if o.Batches[2].Status != "rejected" || o.Batches[2].Reason != "sold out" || o.Totals.Total != total {
		t.Fatalf("rejected: %+v", o.Batches[2])
	}
	_, err = a.AcceptAddon(ctx, o.ID, 3, s.staff)
	wantCode(t, err, "ADDON_DECIDED")

	// the next one is accepted: it goes to the kitchen and the bill
	o, err = a.CustomerAddon(ctx, tok, uniq("k"), app.AddonInput{Items: tea})
	noErr(t, err)
	o, err = a.AcceptAddon(ctx, o.ID, 4, s.staff)
	noErr(t, err)
	if o.Batches[3].Status != "accepted" || countItems(o, "tea") != 3 || o.Totals.Total <= total {
		t.Fatalf("accepted: %+v tea=%d", o.Batches[3], countItems(o, "tea"))
	}
	// the customer may withdraw one still waiting; three in all were used
	o, err = a.CustomerAddon(ctx, tok, uniq("k"), app.AddonInput{Items: tea})
	noErr(t, err)
	if o.Addon.CustomerLeft != 0 {
		t.Fatalf("left: %+v", o.Addon)
	}
	o, err = a.CancelCustomerAddon(ctx, tok, 5)
	noErr(t, err)
	if o.Batches[4].Status != "cancelled" || o.Addon.CustomerLeft != 1 {
		t.Fatalf("withdrawn: %+v %+v", o.Batches[4], o.Addon)
	}
	// one waiting too long expires
	o, err = a.CustomerAddon(ctx, tok, uniq("k"), app.AddonInput{Items: tea})
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE order_batches SET expires_at = now() - interval '1 minute'
		WHERE order_id=$1 AND status='pending'`, o.ID)
	noErr(t, err)
	n, err := a.ExpireAddons(ctx)
	noErr(t, err)
	o, err = a.GetOrder(ctx, o.ID)
	noErr(t, err)
	if n < 1 || o.Batches[5].Status != "expired" || o.Addon.CustomerLeft != 1 {
		t.Fatalf("expired: n=%d %+v %+v", n, o.Batches[5], o.Addon)
	}
	// turned down, withdrawn and expired ones do not count; the third does
	o, err = a.CustomerAddon(ctx, tok, uniq("k"), app.AddonInput{Items: tea})
	noErr(t, err)
	_, err = a.AcceptAddon(ctx, o.ID, 7, s.staff)
	noErr(t, err)
	_, err = a.CustomerAddon(ctx, tok, uniq("k"), app.AddonInput{Items: tea})
	wantCode(t, err, "ADDON_LIMIT")
}

func TestCustomerAddonEndsWithTheOrder(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	o, tok := s.placeTok(t, "pickup", s.cart())
	o, err := a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	o, err = a.CustomerAddon(ctx, tok, uniq("k"), app.AddonInput{Items: []app.QuoteLineInput{{ItemID: s.water, Qty: 1}}})
	noErr(t, err)
	o, err = a.StaffCancel(ctx, o.ID, 0, "closing", s.staff)
	noErr(t, err)
	if o.Batches[1].Status != "cancelled" || o.Addon.Open {
		t.Fatalf("after cancel: %+v %+v", o.Batches[1], o.Addon)
	}
	// a closed order takes no dishes
	d := s.place(t, "pickup", s.cart())
	_, err = a.DB.Exec(ctx, `UPDATE orders SET acceptance_status='rejected' WHERE id=$1`, d.ID)
	noErr(t, err)
	_, err = a.StaffAddonQuote(ctx, d.ID, app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}})
	wantCode(t, err, "ADDON_CLOSED")
}
