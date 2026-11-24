package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// docs/PRICING_REVIEW.md step 4: promotions on chosen dishes.

func discountsOf(o *app.Order) map[string]int64 {
	out := map[string]int64{}
	for _, d := range o.Discounts {
		out[d.Kind+":"+d.Name] += d.Amount
	}
	return out
}

func TestDishPromotions(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Name: "half", Kind: "dish_second", Value: 5000, ItemIDs: []int64{s.tea}})
	pct := s.promo(t, app.PromotionInput{Name: "pct", Kind: "dish_percent", Value: 2000, ItemIDs: []int64{s.chicken}})
	s.promo(t, app.PromotionInput{Name: "m5", Kind: "amount_off", Threshold: 30000, Value: 5000})
	// would be the better one on the dishes' full price (37600), not on 30770
	s.promo(t, app.PromotionInput{Name: "m9", Kind: "amount_off", Threshold: 35000, Value: 9000})

	lines := []app.QuoteLineInput{{ItemID: s.tea, Qty: 3}, {ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}}}
	q := s.quote(t, "pickup", lines)
	// tea: the third of three at half price, 3450; chicken 20% off 16900, 3380;
	// then 30770 reaches 300.00 off 50.00
	if q.Totals.PromotionDiscount != 5000 || q.Totals.Discount != 3450+3380+5000 || len(q.DishDiscounts) != 2 {
		t.Fatalf("quote %+v dish %+v", q.Totals, q.DishDiscounts)
	}
	o := s.place(t, "pickup", lines)
	if o.Totals.Discount != 11830 || o.Totals.MerchantDiscount != 11830 {
		t.Fatalf("order %+v", o.Totals)
	}
	got := discountsOf(o)
	if got["dish_promotion:half"] != 3450 || got["dish_promotion:pct"] != 3380 || got["promotion:m5"] != 5000 {
		t.Fatalf("rows %+v", got)
	}
	tea, chicken := itemByName(o, "tea"), itemByName(o, "chicken")
	if tea.DiscountShare+chicken.DiscountShare != 11830 || chicken.DiscountShare < 3380 {
		t.Fatalf("shares tea %d chicken %d", tea.DiscountShare, chicken.DiscountShare)
	}

	// The merchant changes the dish promotion: the order keeps what it was sold with.
	_, err := a.SavePromotion(ctx, s.branchID, pct, app.PromotionInput{Name: "pct", Kind: "dish_percent", Value: 5000,
		ItemIDs: []int64{s.chicken}, AppliesDelivery: true, AppliesPickup: true}, s.staff)
	noErr(t, err)
	o, err = a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	// The customer sends one tea back: two left, one still at half price;
	// 23870 no longer reaches the promotion on the whole order.
	r, err := a.VoidLine(ctx, tea.ID, app.VoidInput{Qty: 1, Reason: "x", ReasonKind: "customer"}, s.staff, true)
	noErr(t, err)
	got = discountsOf(r.Order)
	if got["dish_promotion:half"] != 3450 || got["dish_promotion:pct"] != 3380 || got["promotion:m5"] != 0 ||
		r.Order.Totals.Discount != 6830 {
		t.Fatalf("after one void %+v %+v", got, r.Order.Totals)
	}
	// and another: one tea, nothing off it
	r, err = a.VoidLine(ctx, tea.ID, app.VoidInput{Qty: 1, Reason: "x", ReasonKind: "customer"}, s.staff, true)
	noErr(t, err)
	got = discountsOf(r.Order)
	if _, ok := got["dish_promotion:half"]; ok || got["dish_promotion:pct"] != 3380 || r.Order.Totals.Discount != 3380 {
		t.Fatalf("after two voids %+v %+v", got, r.Order.Totals)
	}
}

// An add-on that brings a second one of a dish gets it at its price.
func TestDishPromotionAddon(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Name: "half", Kind: "dish_second", Value: 5000, ItemIDs: []int64{s.tea}})
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	if o.Totals.Discount != 0 {
		t.Fatalf("one tea %+v", o.Totals)
	}
	res, err := a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}}}, s.staff)
	noErr(t, err)
	if got := discountsOf(res.Order); got["dish_promotion:half"] != 3450 || res.Order.Totals.Discount != 3450 {
		t.Fatalf("after add-on %+v %+v", got, res.Order.Totals)
	}
}

func TestDishPromotionSettings(t *testing.T) {
	s := newStore(t, false)
	other := newStore(t, false)
	ctx, a := context.Background(), testApp
	in := app.PromotionInput{Name: "x", Kind: "dish_percent", Value: 2000, AppliesPickup: true}
	_, err := a.SavePromotion(ctx, s.branchID, 0, in, s.staff)
	wantCode(t, err, "VALIDATION")
	in.ItemIDs = []int64{other.tea}
	_, err = a.SavePromotion(ctx, s.branchID, 0, in, s.staff)
	wantCode(t, err, "VALIDATION")
	in.ItemIDs, in.Value = []int64{s.tea, s.tea}, 10000
	_, err = a.SavePromotion(ctx, s.branchID, 0, in, s.staff)
	wantCode(t, err, "VALIDATION")
	in.Kind = "dish_second"
	id, err := a.SavePromotion(ctx, s.branchID, 0, in, s.staff)
	noErr(t, err)
	ps, err := a.ListPromotions(ctx, s.branchID)
	noErr(t, err)
	if len(ps) != 1 || ps[0].ID != id || len(ps[0].ItemIDs) != 1 || ps[0].ItemIDs[0] != s.tea {
		t.Fatalf("saved %+v", ps)
	}
}
