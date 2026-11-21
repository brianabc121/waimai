package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

// promo creates a promotion and publishes it.
func (s *store) promo(t *testing.T, in app.PromotionInput) int64 {
	t.Helper()
	if in.Name == "" {
		in.Name = "P"
	}
	if in.Kind != "free_delivery" && !in.AppliesDelivery && !in.AppliesPickup {
		in.AppliesDelivery, in.AppliesPickup = true, true
	}
	id, err := testApp.SavePromotion(context.Background(), s.branchID, 0, in, s.staff)
	noErr(t, err)
	noErr(t, testApp.SetPromotionStatus(context.Background(), s.branchID, id, "active", s.staff))
	return id
}

func (s *store) used(t *testing.T, id int64) int {
	t.Helper()
	ps, err := testApp.ListPromotions(context.Background(), s.branchID)
	noErr(t, err)
	for _, p := range ps {
		if p.ID == id {
			return p.UsedCount
		}
	}
	t.Fatalf("promotion %d not found", id)
	return 0
}

func TestDiscountTotals(t *testing.T) {
	cfg := app.TaxConfig{TaxRateBP: 1200, TaxInclusive: false, ServiceChargeBP: 500, PackagingFee: 1000}
	// 500.00 of food, 50.00 off: service charge and tax are computed on 450.00
	got := app.ComputeTotals(50000, 6900, cfg, app.Discount{Goods: 5000})
	if got.ServiceCharge != 2250 || got.TaxAmount != 5790 || got.Discount != 5000 ||
		got.Total != 45000+1000+2250+5790+6900 {
		t.Fatalf("goods discount: %+v", got)
	}
	// free delivery changes neither service charge nor tax
	base := app.ComputeTotals(50000, 6900, cfg, app.Discount{})
	free := app.ComputeTotals(50000, 6900, cfg, app.Discount{Delivery: 6900})
	if free.TaxAmount != base.TaxAmount || free.ServiceCharge != base.ServiceCharge || free.Total != base.Total-6900 {
		t.Fatalf("delivery discount: %+v vs %+v", free, base)
	}
	// a discount can never exceed what it applies to
	over := app.ComputeTotals(3000, 6900, cfg, app.Discount{Goods: 9999, Delivery: 9999})
	if over.Discount != 3000+6900 || over.Total != 1000+120 {
		t.Fatalf("capped discount: %+v", over)
	}
}

func TestPromotionValidationAndStatus(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	bad := []app.PromotionInput{
		{Name: "", Kind: "amount_off", Value: 100, Threshold: 100, AppliesPickup: true},
		{Name: "x", Kind: "amount_off", Value: 0, AppliesPickup: true},
		{Name: "x", Kind: "amount_off", Value: 5000, Threshold: 1000, AppliesPickup: true}, // off > spend
		{Name: "x", Kind: "percent_off", Value: 10000, AppliesPickup: true},
		{Name: "x", Kind: "percent_off", Value: 1000},           // no service type
		{Name: "x", Kind: "free_delivery", AppliesPickup: true}, // pickup only
		{Name: "x", Kind: "bogus", Value: 1, AppliesPickup: true},
		{Name: "x", Kind: "percent_off", Value: 1000, AppliesPickup: true, DailyStart: "25:00"},
		{Name: "x", Kind: "percent_off", Value: 1000, AppliesPickup: true, TotalQuota: intPtr(0)},
	}
	for i, in := range bad {
		if _, err := a.SavePromotion(ctx, s.branchID, 0, in, s.staff); err == nil {
			t.Fatalf("case %d should be rejected", i)
		}
	}
	id, err := a.SavePromotion(ctx, s.branchID, 0, app.PromotionInput{Name: "ten", Kind: "percent_off", Value: 1000,
		AppliesDelivery: true, AppliesPickup: true}, s.staff)
	noErr(t, err)
	// a draft gives nothing
	if q := s.quote(t, "pickup", s.cart()); q.Totals.Discount != 0 {
		t.Fatalf("draft promotion applied: %+v", q.Totals)
	}
	wantCode(t, a.SetPromotionStatus(ctx, s.branchID, id, "paused", s.staff), "INVALID_STATE")
	noErr(t, a.SetPromotionStatus(ctx, s.branchID, id, "active", s.staff))
	if q := s.quote(t, "pickup", s.cart()); q.Totals.Discount != 4370 || q.Totals.PromotionID != id {
		t.Fatalf("active promotion: %+v", q.Totals)
	}
	noErr(t, a.SetPromotionStatus(ctx, s.branchID, id, "paused", s.staff))
	if q := s.quote(t, "pickup", s.cart()); q.Totals.Discount != 0 {
		t.Fatal("paused promotion applied")
	}
	noErr(t, a.SetPromotionStatus(ctx, s.branchID, id, "ended", s.staff))
	wantCode(t, a.SetPromotionStatus(ctx, s.branchID, id, "active", s.staff), "INVALID_STATE")
	_, err = a.SavePromotion(ctx, s.branchID, id, app.PromotionInput{Name: "ten", Kind: "percent_off", Value: 2000,
		AppliesPickup: true}, s.staff)
	wantCode(t, err, "INVALID_STATE")
	// another store cannot touch it
	other := newStore(t, true)
	wantCode(t, a.SetPromotionStatus(ctx, other.branchID, id, "ended", s.staff), "NOT_FOUND")
}

func TestBestPromotionAndSnapshot(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	// cart subtotal is 437.00, delivery fee 69.00
	small := s.promo(t, app.PromotionInput{Name: "满300减30", Kind: "amount_off", Threshold: 30000, Value: 3000})
	big := s.promo(t, app.PromotionInput{Name: "满400减60", Kind: "amount_off", Threshold: 40000, Value: 6000})
	s.promo(t, app.PromotionInput{Name: "满1000减200", Kind: "amount_off", Threshold: 100000, Value: 20000})
	s.promo(t, app.PromotionInput{Name: "九折封顶20", Kind: "percent_off", Value: 1000, MaxDiscount: int64Ptr(2000)})
	freeDel := s.promo(t, app.PromotionInput{Name: "免配送费", Kind: "free_delivery", Threshold: 20000, AppliesDelivery: true})

	// delivery: free delivery (69) beats 60 off; one promotion only, no stacking
	q := s.quote(t, "delivery", s.cart())
	if q.Totals.PromotionID != freeDel || q.Totals.Discount != 6900 {
		t.Fatalf("delivery best: %+v", q.Totals)
	}
	// pickup: free delivery does not apply; 60 off wins over 30 off and the capped 10%
	q = s.quote(t, "pickup", s.cart())
	if q.Totals.PromotionID != big || q.Totals.Discount != 6000 {
		t.Fatalf("pickup best: %+v", q.Totals)
	}
	// 437 - 60 = 377 food, +5% service charge 18.85, +10 packaging; tax included
	if q.Totals.ServiceCharge != 1885 || q.Totals.Total != 37700+1885+1000 {
		t.Fatalf("pickup totals: %+v", q.Totals)
	}
	// below the bigger threshold only the small one applies: tea x5 = 345
	q = s.quote(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 5}})
	if q.Totals.PromotionID != small || q.Totals.Discount != 3000 {
		t.Fatalf("small cart: %+v", q.Totals)
	}

	// the storefront advertises live promotions without usage numbers
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Promotions) != 5 || sf.Promotions[0].TotalQuota != nil {
		t.Fatalf("storefront promotions: %+v", sf.Promotions)
	}

	// the order keeps its own copy of the promotion and amount
	o := s.place(t, "pickup", s.cart())
	if o.Totals.Discount != 6000 || o.Totals.PromotionName != "满400减60" || s.used(t, big) != 1 {
		t.Fatalf("order snapshot: %+v used=%d", o.Totals, s.used(t, big))
	}
	_, err = a.SavePromotion(ctx, s.branchID, big, app.PromotionInput{Name: "改名", Kind: "amount_off", Threshold: 40000,
		Value: 1000, AppliesDelivery: true, AppliesPickup: true}, s.staff)
	noErr(t, err)
	o, err = a.GetOrder(ctx, o.ID)
	noErr(t, err)
	if o.Totals.Discount != 6000 || o.Totals.PromotionName != "满400减60" || o.Totals.Total != 37700+1885+1000 {
		t.Fatalf("order changed after the promotion was edited: %+v", o.Totals)
	}

	// a promotion paused between quote and order needs a new confirmation
	q = s.quote(t, "delivery", s.cart())
	noErr(t, a.SetPromotionStatus(ctx, s.branchID, freeDel, "paused", s.staff))
	_, err = a.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main"})
	wantCode(t, err, "QUOTE_CHANGED")
}

func TestPromotionTimeWindow(t *testing.T) {
	s := newStore(t, true) // Asia/Manila, UTC+8
	ctx := context.Background()
	a := testApp
	manila := time.FixedZone("PHT", 8*3600)
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, manila)
	end := time.Date(2026, 3, 8, 0, 0, 0, 0, manila)
	// late-night offer, 22:00 to 02:00 store time, for one week
	id := s.promo(t, app.PromotionInput{Name: "夜宵", Kind: "amount_off", Threshold: 10000, Value: 2000,
		DailyStart: "22:00", DailyEnd: "02:00", StartsAt: &start, EndsAt: &end})
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 3, 2, 21, 59, 0, 0, manila), "outside_time"},
		{time.Date(2026, 3, 2, 22, 0, 0, 0, manila), ""},
		{time.Date(2026, 3, 2, 23, 59, 0, 0, manila), ""},
		{time.Date(2026, 3, 3, 0, 30, 0, 0, manila), ""}, // after midnight, still the same evening
		{time.Date(2026, 3, 3, 1, 59, 0, 0, manila), ""},
		{time.Date(2026, 3, 3, 2, 0, 0, 0, manila), "outside_time"},
		{time.Date(2026, 3, 3, 12, 0, 0, 0, manila), "outside_time"},
		// 14:30 UTC is 22:30 in Manila: the window follows the store's clock, not the server's
		{time.Date(2026, 3, 2, 14, 30, 0, 0, time.UTC), ""},
		{time.Date(2026, 3, 2, 22, 30, 0, 0, time.UTC), "outside_time"},
		{time.Date(2026, 2, 28, 23, 0, 0, 0, manila), "outside_time"}, // before the start date
		{time.Date(2026, 3, 7, 23, 59, 0, 0, manila), ""},
		{time.Date(2026, 3, 8, 0, 30, 0, 0, manila), "outside_time"}, // after the end date
	}
	for _, c := range cases {
		res, err := a.TrialPromotions(ctx, s.branchID, app.TrialInput{ServiceType: "pickup", Subtotal: 20000, At: &c.at})
		noErr(t, err)
		if len(res.Rows) != 1 || res.Rows[0].ID != id || res.Rows[0].Reason != c.want {
			t.Fatalf("at %s: want reason %q, got %+v", c.at, c.want, res.Rows)
		}
		wantDiscount := int64(0)
		if c.want == "" {
			wantDiscount = 2000
		}
		if res.Totals.Discount != wantDiscount || res.Rows[0].Wins != (c.want == "") {
			t.Fatalf("at %s: totals %+v", c.at, res.Totals)
		}
	}
	// below the threshold, and a draft is evaluated in a trial as if it were active
	draft, err := a.SavePromotion(ctx, s.branchID, 0, app.PromotionInput{Name: "draft", Kind: "percent_off", Value: 5000,
		AppliesPickup: true}, s.staff)
	noErr(t, err)
	at := time.Date(2026, 3, 2, 23, 0, 0, 0, manila)
	res, err := a.TrialPromotions(ctx, s.branchID, app.TrialInput{ServiceType: "pickup", Subtotal: 5000, At: &at})
	noErr(t, err)
	if res.Rows[0].Reason != "below_threshold" || res.Rows[1].ID != draft || !res.Rows[1].Wins || res.Totals.Discount != 2500 {
		t.Fatalf("trial with draft: %+v", res.Rows)
	}
	// ...but a real quote ignores the draft
	if q := s.quote(t, "pickup", s.cart()); q.Totals.PromotionID == draft {
		t.Fatal("draft applied to a real quote")
	}
}

func TestPromotionQuotaUnderConcurrency(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	const quota, buyers = 3, 12
	id := s.promo(t, app.PromotionInput{Name: "前3单", Kind: "amount_off", Threshold: 5000, Value: 2000, TotalQuota: intPtr(quota)})
	quotes := make([]*app.Quote, buyers)
	for i := range quotes {
		quotes[i] = s.quote(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
		if quotes[i].Totals.Discount != 2000 {
			t.Fatal("every buyer is quoted the discount while quota remains")
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, buyers)
	for i := range quotes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = a.PlaceOrder(ctx, s.slug, uniq("+6391"), uniq("k"),
				app.PlaceOrderInput{QuoteID: quotes[i].ID, CustomerName: "Ann"})
		}()
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		} else {
			wantCode(t, err, "QUOTE_CHANGED")
		}
	}
	if won != quota || s.used(t, id) != quota {
		t.Fatalf("want exactly %d discounted orders, got %d (used_count %d)", quota, won, s.used(t, id))
	}
	orders, err := a.ListOrders(ctx, s.branchID, "active")
	noErr(t, err)
	if len(orders) != quota {
		t.Fatalf("losers must not leave orders behind: %d", len(orders))
	}
	// once the quota is gone the next quote is at full price and can be ordered
	q := s.quote(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	if q.Totals.Discount != 0 {
		t.Fatalf("quota used up, yet quoted: %+v", q.Totals)
	}
	// the quota cannot be edited below what is used
	_, err = a.SavePromotion(ctx, s.branchID, id, app.PromotionInput{Name: "前3单", Kind: "amount_off", Threshold: 5000,
		Value: 2000, TotalQuota: intPtr(2), AppliesPickup: true}, s.staff)
	wantCode(t, err, "VALIDATION")
}

func TestPromotionPerCustomerLimitAndRelease(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	phone := "+639170000001"
	id := s.promo(t, app.PromotionInput{Name: "新客", Kind: "amount_off", Threshold: 5000, Value: 2000,
		PerPhoneLimit: intPtr(1), TotalQuota: intPtr(10)})
	tea := []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}

	first := s.place(t, "pickup", tea)
	if first.Totals.Discount != 2000 || s.used(t, id) != 1 {
		t.Fatalf("first order: %+v", first.Totals)
	}
	// the same customer is now quoted full price
	if q := s.quote(t, "pickup", tea); q.Totals.Discount != 0 {
		t.Fatalf("limit reached, yet quoted: %+v", q.Totals)
	}
	// a quote made before the phone was known still cannot be used to get it twice
	anon, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: tea}, "")
	noErr(t, err)
	if anon.Totals.Discount != 2000 {
		t.Fatal("anonymous quote should show the offer")
	}
	_, err = a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: anon.ID, CustomerName: "Ann"})
	wantCode(t, err, "QUOTE_CHANGED")
	// two simultaneous orders from one new customer: only one gets it
	q1, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: tea}, "")
	noErr(t, err)
	q2, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: tea}, "")
	noErr(t, err)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, q := range []*app.Quote{q1, q2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = a.PlaceOrder(ctx, s.slug, "+639170000777", uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Bo"})
		}()
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("exactly one of two simultaneous orders may use a once-per-customer offer: %v / %v", errs[0], errs[1])
	}
	if s.used(t, id) != 2 {
		t.Fatalf("used_count: %d", s.used(t, id))
	}

	// rejecting the order gives the use back, and the customer can use it again
	_, err = a.Reject(ctx, first.ID, 0, "sold out", s.staff)
	noErr(t, err)
	if s.used(t, id) != 1 {
		t.Fatalf("after reject used_count: %d", s.used(t, id))
	}
	second, tok := s.placeToken(t, tea)
	if second.Totals.Discount != 2000 {
		t.Fatal("the use should be available again after a reject")
	}
	// customer cancel and accept timeout release too; releasing twice does nothing
	_, err = a.CustomerCancel(ctx, tok)
	noErr(t, err)
	_, err = a.CustomerCancel(ctx, tok)
	noErr(t, err)
	third := s.place(t, "pickup", tea)
	_, err = a.DB.Exec(ctx, `UPDATE orders SET accept_deadline = now() - interval '1 minute' WHERE id=$1`, third.ID)
	noErr(t, err)
	_, err = a.ExpirePendingOrders(ctx)
	noErr(t, err)
	if s.used(t, id) != 1 {
		t.Fatalf("after cancel and timeout used_count: %d", s.used(t, id))
	}
	// a completed order keeps its use; the rejected one keeps its discount on record
	fourth := s.place(t, "pickup", tea)
	_, err = a.Accept(ctx, fourth.ID, 0, s.staff)
	noErr(t, err)
	if s.used(t, id) != 2 {
		t.Fatalf("accepted order used_count: %d", s.used(t, id))
	}
	old, err := a.GetOrder(ctx, first.ID)
	noErr(t, err)
	if old.Totals.Discount != 2000 || old.Totals.PromotionID != id {
		t.Fatalf("history lost the discount: %+v", old.Totals)
	}
}

// placeToken places an order and returns it with the customer's access token.
func (s *store) placeToken(t *testing.T, lines []app.QuoteLineInput) (*app.Order, string) {
	t.Helper()
	q := s.quote(t, "pickup", lines)
	p, err := testApp.PlaceOrder(context.Background(), s.slug, "+639170000001", uniq("k"),
		app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)
	o, err := testApp.OrderByToken(context.Background(), p.AccessToken)
	noErr(t, err)
	return o, p.AccessToken
}
