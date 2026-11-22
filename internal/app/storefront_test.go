package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// finishPickup places a pickup order for tea and takes it to "picked up".
func (s *store) finishPickup(t *testing.T) (*app.Order, string) {
	t.Helper()
	ctx := context.Background()
	a := testApp
	q := s.quote(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 2}})
	p, err := a.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)
	o, err := a.Accept(ctx, p.OrderID, 0, s.staff)
	noErr(t, err)
	o, err = a.SetItemStatus(ctx, o.ID, o.Items[0].ID, "ready", s.staff)
	noErr(t, err)
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	o, err = a.StaffHandoff(ctx, o.ID, s.staff)
	noErr(t, err)
	return o, p.AccessToken
}

func TestBasketsAreKeptOnTheOrder(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	lines := []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}, {ItemID: s.tea, Qty: 1, Basket: 2}, {ItemID: s.water, Qty: 1, Basket: 2}}
	o := s.place(t, "pickup", lines)
	got := []int{}
	for _, it := range o.Items {
		got = append(got, it.Basket)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 2 {
		t.Fatalf("baskets = %v", got)
	}
	_, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup",
		Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 1, Basket: 5}}}, "")
	wantCode(t, err, "VALIDATION")

	// A store that turned baskets off gets everything in one.
	off := false
	set := app.SettingsInput{Name: "T", AcceptTimeoutMinutes: 10, DeliveryEnabled: true, PickupEnabled: true,
		OpenTime: "00:00", CloseTime: "00:00", BasketsEnabled: &off}
	b, err := a.SaveSettings(ctx, s.branchID, set)
	noErr(t, err)
	if b.BasketsEnabled {
		t.Fatal("baskets should be off")
	}
	for _, it := range s.place(t, "pickup", lines).Items {
		if it.Basket != 1 {
			t.Fatalf("basket %d with baskets off", it.Basket)
		}
	}
	// Settings saved without the new fields keep them.
	set.BasketsEnabled = nil
	notice := "  今天送饮料  "
	set.Notice = &notice
	b, err = a.SaveSettings(ctx, s.branchID, set)
	noErr(t, err)
	if b.BasketsEnabled || b.Notice != "今天送饮料" {
		t.Fatalf("kept settings: %+v", b)
	}
}

func TestRequiredCategory(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	yes := true
	soup, err := a.SaveCategory(ctx, s.branchID, 0, app.CategoryInput{Name: "汤底", Sort: 1, Required: &yes})
	noErr(t, err)
	in := app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}}

	// Nothing on sale in the required category yet: it is not enforced.
	_, err = a.CreateQuote(ctx, s.slug, in, "")
	noErr(t, err)

	broth, err := a.SaveItem(ctx, s.branchID, 0, app.ItemInput{CategoryID: soup, Name: "骨汤", Price: 1000,
		Published: true, DeliveryAvailable: true, PickupAvailable: true})
	noErr(t, err)
	_, err = a.CreateQuote(ctx, s.slug, in, "")
	wantCode(t, err, "REQUIRED_MISSING")
	in.Items = append(in.Items, app.QuoteLineInput{ItemID: broth, Qty: 1})
	_, err = a.CreateQuote(ctx, s.slug, in, "")
	noErr(t, err)

	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Categories) != 2 || sf.Categories[0].Required || !sf.Categories[1].Required {
		t.Fatalf("required flags: %+v", sf.Categories)
	}
	// Renaming a category without the flag keeps it.
	_, err = a.SaveCategory(ctx, s.branchID, soup, app.CategoryInput{Name: "汤底选择", Sort: 1})
	noErr(t, err)
	in.Items = in.Items[:1]
	_, err = a.CreateQuote(ctx, s.slug, in, "")
	wantCode(t, err, "REQUIRED_MISSING")
}

func TestReviewsAndSales(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp

	// An order still in progress cannot be reviewed.
	open := s.quote(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	p, err := a.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), app.PlaceOrderInput{QuoteID: open.ID, CustomerName: "Ann"})
	noErr(t, err)
	_, err = a.SubmitReview(ctx, p.AccessToken, app.ReviewInput{Rating: 5, Taste: 5, Packaging: 5})
	wantCode(t, err, "NOT_REVIEWABLE")

	_, token := s.finishPickup(t)
	st, err := a.OrderReview(ctx, token)
	noErr(t, err)
	if !st.CanReview || st.Review != nil {
		t.Fatalf("before review: %+v", st)
	}
	_, err = a.SubmitReview(ctx, token, app.ReviewInput{Rating: 6, Taste: 5, Packaging: 5})
	wantCode(t, err, "VALIDATION")
	st, err = a.SubmitReview(ctx, token, app.ReviewInput{Rating: 5, Taste: 4, Packaging: 3, Comment: " 好喝 "})
	noErr(t, err)
	if st.CanReview || st.Review == nil || st.Review.Comment != "好喝" || st.Review.Delivery != nil {
		t.Fatalf("after review: %+v", st.Review)
	}
	_, err = a.SubmitReview(ctx, token, app.ReviewInput{Rating: 1, Taste: 1, Packaging: 1})
	wantCode(t, err, "ALREADY_REVIEWED")

	_, token2 := s.finishPickup(t)
	_, err = a.SubmitReview(ctx, token2, app.ReviewInput{Rating: 2, Taste: 2, Packaging: 5})
	noErr(t, err)

	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if sf.Rating.Count != 2 || sf.Rating.Average != 3.5 || sf.Rating.Taste != 3 || sf.Rating.Packaging != 4 || sf.Rating.Delivery != 0 {
		t.Fatalf("rating: %+v", sf.Rating)
	}
	// Two accepted orders of two teas each; the unanswered one does not count.
	if sf.Sales != 2 {
		t.Fatalf("sales = %d", sf.Sales)
	}
	for _, it := range sf.Categories[0].Items {
		want := 0
		if it.ID == s.tea {
			want = 4
		}
		if it.Sold != want {
			t.Fatalf("%s sold %d, want %d", it.Name, it.Sold, want)
		}
		if (it.ID == s.limited) != (it.StockQty != nil) {
			t.Fatalf("%s stock %v", it.Name, it.StockQty)
		}
	}

	page, err := a.StoreReviews(ctx, s.slug, "all", 0)
	noErr(t, err)
	if len(page.Reviews) != 2 || page.More || page.Reviews[0].Rating != 2 || len(page.Reviews[0].Items) != 1 ||
		page.Reviews[0].Items[0].Name != "tea" || page.Reviews[0].DisplayName != page.Reviews[1].DisplayName {
		t.Fatalf("all: %+v", page.Reviews)
	}
	for filter, want := range map[string]int{"good": 5, "bad": 2} {
		page, err = a.StoreReviews(ctx, s.slug, filter, 0)
		noErr(t, err)
		if len(page.Reviews) != 1 || page.Reviews[0].Rating != want {
			t.Fatalf("%s: %+v", filter, page.Reviews)
		}
	}
	_, err = a.StoreReviews(ctx, s.slug, "worst", 0)
	wantCode(t, err, "VALIDATION")
}

func TestReportStore(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	wantCode(t, a.ReportStore(ctx, s.slug, app.ReportInput{Reason: "nonsense"}), "VALIDATION")
	wantCode(t, a.ReportStore(ctx, s.slug, app.ReportInput{Reason: "other"}), "VALIDATION")
	noErr(t, a.ReportStore(ctx, s.slug, app.ReportInput{Reason: "food_safety", Detail: "头发", Contact: "0917"}))
	var merchantID int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT merchant_id FROM branches WHERE id=$1`, s.branchID).Scan(&merchantID))
	d, err := a.GetMerchant(ctx, merchantID)
	noErr(t, err)
	if len(d.Reports) != 1 || d.Reports[0].Reason != "food_safety" || d.Reports[0].Detail != "头发" {
		t.Fatalf("reports: %+v", d.Reports)
	}
}
