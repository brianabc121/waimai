package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// Item marketing: purchase limits (per order, per customer per day) and the
// second one at half price.

// limitErr checks that err is ITEM_LIMIT of kind on item, with left more allowed.
func limitErr(t *testing.T, err error, kind string, item int64, left int) {
	t.Helper()
	var ae *app.Error
	if !errors.As(err, &ae) || ae.Code != "ITEM_LIMIT" {
		t.Fatalf("want ITEM_LIMIT, got %v", err)
	}
	d, _ := ae.Details.(map[string]any)
	if d["kind"] != kind || d["item_id"] != item || d["left"] != left {
		t.Fatalf("limit details %+v, want %s item %d left %d", d, kind, item, left)
	}
}

func (s *store) tryQuote(phone, service string, lines []app.QuoteLineInput) error {
	in := app.QuoteInput{ServiceType: service, Items: lines}
	if service == "delivery" {
		in.ZoneID = &s.zoneID
	}
	_, err := testApp.CreateQuote(context.Background(), s.slug, in, phone)
	return err
}

func newPhone() string { return fmt.Sprintf("+63918%07d", seq.Add(1)+int64(1000000)) }

// docs/PRICING_CHECK.md sample S19: the second one at half price counts in
// pairs. Pickup, tax 12% on top, service 5%, packaging 10.00, water 30.00.
//
//	4 waters: 120.00 − 2 × 15.00 = 90.00, service 4.50, packaging 10.00, tax 104.50 × 12% = 12.54: 117.04
//	5 waters: 150.00 − 2 × 15.00 = 120.00, service 6.00, packaging 10.00, tax 136.00 × 12% = 16.32: 152.32
func TestPricingSampleS19_SecondHalfInPairs(t *testing.T) {
	s := newStore(t, false)
	s.promo(t, app.PromotionInput{Name: "half", Kind: "dish_second", Value: 5000, ItemIDs: []int64{s.water}})
	for _, c := range []struct {
		qty             int
		off, tax, total int64
	}{{4, 3000, 1254, 11704}, {5, 3000, 1632, 15232}, {3, 1500, 1065, 9940}} {
		o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: c.qty}})
		if o.Totals.Discount != c.off || o.Totals.TaxAmount != c.tax || o.Totals.Total != c.total {
			t.Fatalf("%d waters: %+v", c.qty, o.Totals)
		}
		checked(t, fmt.Sprintf("%d waters, second at half price", c.qty), o.ID)
	}
}

func TestItemLimitPerOrder(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Name: "half", Kind: "dish_second", Value: 5000, ItemIDs: []int64{s.tea}})
	lim := s.promo(t, app.PromotionInput{Name: "two", Kind: app.KindLimitOrder, Value: 2, ItemIDs: []int64{s.tea}})
	phone := "+639170000001"

	limitErr(t, s.tryQuote(phone, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 3}}), "order", s.tea, 2)
	// two lines of the same item count together
	limitErr(t, s.tryQuote(phone, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}, {ItemID: s.tea, Qty: 2, Note: "hot"}}), "order", s.tea, 2)
	// other items are free
	noErr(t, s.tryQuote(phone, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 2}, {ItemID: s.water, Qty: 9}}))

	// two teas, the second at half price: 138.00 − 34.50
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 2}})
	if o.Totals.Discount != 3450 {
		t.Fatalf("two teas %+v", o.Totals)
	}
	checked(t, "two teas at the limit", o.ID)
	// the limit is no offer: it is not on the order and not advertised
	if got := discountsOf(o); len(got) != 1 || got["dish_promotion:half"] != 3450 {
		t.Fatalf("rows %+v", got)
	}
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Promotions) != 1 || len(sf.ItemLimits) != 1 || sf.ItemLimits[0] != (app.ItemLimit{ItemID: s.tea, PerOrder: 2}) || len(sf.DineItemLimits) != 0 {
		t.Fatalf("storefront promotions %+v limits %+v dine %+v", sf.Promotions, sf.ItemLimits, sf.DineItemLimits)
	}

	// an add-on counts what the order already holds
	_, err = a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}}}, s.staff)
	limitErr(t, err, "order", s.tea, 0)
	checked(t, "refused add-on", o.ID)
	// one tea sent back makes room for one more
	o, err = a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	_, err = a.VoidLine(ctx, itemByName(o, "tea").ID, app.VoidInput{Qty: 1, Reason: "x", ReasonKind: "customer"}, s.staff, true)
	noErr(t, err)
	checked(t, "one tea voided", o.ID)
	res, err := a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}}}, s.staff)
	noErr(t, err)
	if res.Order.Totals.Discount != 3450 {
		t.Fatalf("after add-on %+v", res.Order.Totals)
	}
	checked(t, "add-on back to the limit", o.ID)

	// a paused limit holds nobody back
	noErr(t, a.SetPromotionStatus(ctx, s.branchID, lim, "paused", s.staff))
	noErr(t, s.tryQuote(phone, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 5}}))
}

func TestItemLimitPerDay(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Name: "day3", Kind: app.KindLimitDay, Value: 3, ItemIDs: []int64{s.water}})
	ann, bob := newPhone(), newPhone()
	water := func(n int) []app.QuoteLineInput { return []app.QuoteLineInput{{ItemID: s.water, Qty: n}} }

	limitErr(t, s.tryQuote(ann, "pickup", water(4)), "day", s.water, 3)
	first, err := s.placeAs(t, ann, "pickup", water(2), false, nil)
	noErr(t, err)
	checked(t, "first of the day", first.ID)
	limitErr(t, s.tryQuote(ann, "delivery", append(water(2), app.QuoteLineInput{ItemID: s.tea, Qty: 1})), "day", s.water, 1)
	second, err := s.placeAs(t, ann, "pickup", water(1), false, nil)
	noErr(t, err)
	checked(t, "second of the day", second.ID)
	limitErr(t, s.tryQuote(ann, "pickup", water(1)), "day", s.water, 0)
	// an add-on to today's order counts too
	_, err = a.StaffAddon(ctx, second.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}}, s.staff)
	limitErr(t, err, "day", s.water, 0)
	// each customer has their own day; nobody is known without a phone
	noErr(t, s.tryQuote(bob, "pickup", water(3)))
	// a rejected order gives its two back
	_, err = a.Reject(ctx, first.ID, 0, "sold out", s.staff)
	noErr(t, err)
	limitErr(t, s.tryQuote(ann, "pickup", water(3)), "day", s.water, 2)
	noErr(t, s.tryQuote(ann, "pickup", water(2)))
	// yesterday's do not count
	_, err = a.DB.Exec(ctx, `UPDATE order_batches SET created_at = created_at - interval '2 days' WHERE order_id=$1`, second.ID)
	noErr(t, err)
	noErr(t, s.tryQuote(ann, "pickup", water(3)))
}

func TestItemLimitDineIn(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	// delivery limits and table limits never mix
	s.promo(t, app.PromotionInput{Name: "d1", Kind: app.KindLimitOrder, Value: 1, ItemIDs: []int64{s.water}})
	s.promo(t, app.PromotionInput{Scope: "dine_in", Name: "t2", Kind: app.KindLimitOrder, Value: 2, ItemIDs: []int64{s.tea}})
	tableID, _ := s.table(t, "L1")
	bill, err := a.OpenTable(ctx, tableID, 2, nil, s.staff)
	noErr(t, err)
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"),
		batch(app.QuoteLineInput{ItemID: s.tea, Qty: 2}, app.QuoteLineInput{ItemID: s.water, Qty: 3}), s.staff)
	noErr(t, err)
	checked(t, "table at the limit", bill.Order.ID)
	_, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: s.tea, Qty: 1}), s.staff)
	limitErr(t, err, "order", s.tea, 0)
	checked(t, "refused batch", bill.Order.ID)
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.DineItemLimits) != 1 || sf.DineItemLimits[0] != (app.ItemLimit{ItemID: s.tea, PerOrder: 2}) || len(sf.ItemLimits) != 1 {
		t.Fatalf("limits %+v dine %+v", sf.ItemLimits, sf.DineItemLimits)
	}
}

func TestItemLimitSettings(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	in := app.PromotionInput{Name: "x", Kind: app.KindLimitOrder, Value: 2}
	_, err := a.SavePromotion(ctx, s.branchID, 0, in, s.staff)
	wantCode(t, err, "VALIDATION") // no items
	in.ItemIDs = []int64{s.tea}
	for _, v := range []int64{0, -1, 100} {
		in.Value = v
		_, err = a.SavePromotion(ctx, s.branchID, 0, in, s.staff)
		wantCode(t, err, "VALIDATION")
	}
	in.Value, in.Threshold, in.Kind = 5, 99900, app.KindLimitDay
	id, err := a.SavePromotion(ctx, s.branchID, 0, in, s.staff)
	noErr(t, err)
	ps, err := a.ListPromotions(ctx, s.branchID)
	noErr(t, err)
	if len(ps) != 1 || ps[0].ID != id || ps[0].Threshold != 0 || !ps[0].AppliesDelivery || !ps[0].AppliesPickup || len(ps[0].ItemIDs) != 1 {
		t.Fatalf("saved %+v", ps[0])
	}
}
