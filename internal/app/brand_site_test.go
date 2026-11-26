package app_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// TestBrandSites: each brand is a shop of its own to customers
// (docs/MULTI_BRAND_DESIGN.md §9): its page, its menu and prices at its
// address, its own running order numbers, reviews, favourites, claimable
// coupons and mall card; the money is S10's (docs/PRICING_CHECK.md §4).
func TestBrandSites(t *testing.T) {
	ctx, a := context.Background(), testApp
	b := newBrandStore(t)
	hcd, err := a.BrandOf(ctx, b.branchID, b.hcd)
	noErr(t, err)

	// The brand's page at its own address.
	sf, err := a.Storefront(ctx, hcd.Slug)
	noErr(t, err)
	if sf.Branch.Name != "好吃党" || sf.Branch.Slug != hcd.Slug || sf.Brand == nil || sf.Brand.ID != b.hcd {
		t.Fatalf("brand page: %+v %+v", sf.Branch.Name, sf.Brand)
	}
	var porkPrice int64
	for _, c := range sf.Categories {
		for _, it := range c.Items {
			if it.ID == b.pork {
				porkPrice = it.Price
				if it.Name != "辣椒炒肉" {
					t.Fatalf("brand dish name %q", it.Name)
				}
			}
		}
	}
	if porkPrice != 5200 || len(sf.Promotions) != 1 || sf.Promotions[0].ID != b.hPrmo {
		t.Fatalf("brand menu: pork %d, promotions %+v", porkPrice, sf.Promotions)
	}
	main, err := a.Storefront(ctx, b.slug)
	noErr(t, err)
	if main.Branch.Name != "一品湘" || main.Brand.ID != b.main {
		t.Fatalf("main page: %s", main.Branch.Name)
	}

	// Ordering at each address: the brand's prices and promotion, and each
	// brand counts its own mall orders from 1.
	buyer, phone := customer(t)
	lines := []app.QuoteLineInput{{ItemID: b.pork, Qty: 2}, {ItemID: b.rice, Qty: 2}}
	order := func(slug string) *app.Order {
		q, err := a.CreateQuote(ctx, slug, app.QuoteInput{ServiceType: "delivery", ZoneID: &b.zoneID, Items: lines}, phone)
		noErr(t, err)
		p, err := a.PlaceOrder(ctx, slug, phone, uniq("k"),
			app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main St"})
		noErr(t, err)
		o, err := a.OrderByToken(ctx, p.AccessToken)
		noErr(t, err)
		return o
	}
	h := order(hcd.Slug)
	checked(t, "好吃党页面下单", h.ID)
	if h.BrandID != b.hcd || h.PublicNo != "Hippo-1" || h.Totals.Total != 15000 || h.Totals.Discount != 1000 || h.Totals.TaxAmount != 1071 {
		t.Fatalf("brand order: brand %d %s %+v", h.BrandID, h.PublicNo, h.Totals)
	}
	y := order(b.slug)
	checked(t, "一品湘页面下单", y.ID)
	if y.BrandID != b.main || y.PublicNo != "Hippo-1" || y.Totals.Total != 15700 {
		t.Fatalf("main order: brand %d %s %+v", y.BrandID, y.PublicNo, y.Totals)
	}
	h2 := order(hcd.Slug)
	checked(t, "好吃党第二单", h2.ID)
	if h2.PublicNo != "Hippo-2" {
		t.Fatalf("brand numbering: %s", h2.PublicNo)
	}

	// A quote made on one brand's page is not good on another's.
	q, err := a.CreateQuote(ctx, hcd.Slug, app.QuoteInput{ServiceType: "delivery", ZoneID: &b.zoneID, Items: lines}, phone)
	noErr(t, err)
	_, err = a.PlaceOrder(ctx, b.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main St"})
	wantCode(t, err, "QUOTE_EXPIRED")

	// A paused brand takes no orders; the store's other brands still do.
	in := app.BrandInput{Name: hcd.Name, Code: hcd.Code, Delivery: true, MallVisible: true, Paused: true}
	_, err = a.UpdateBrand(ctx, b.branchID, b.hcd, b.staff, in)
	noErr(t, err)
	_, err = a.CreateQuote(ctx, hcd.Slug, app.QuoteInput{ServiceType: "delivery", ZoneID: &b.zoneID, Items: lines}, phone)
	wantCode(t, err, "STORE_CLOSED")
	_, err = a.CreateQuote(ctx, b.slug, app.QuoteInput{ServiceType: "delivery", ZoneID: &b.zoneID, Items: lines}, phone)
	noErr(t, err)
	in.Paused, in.LogoURL, in.Intro = false, "https://example.com/hcd.png", "湘味小炒"
	_, err = a.UpdateBrand(ctx, b.branchID, b.hcd, b.staff, in)
	noErr(t, err)

	// Reviews are the brand's.
	_, err = a.DB.Exec(ctx, `INSERT INTO reviews (branch_id, order_id, rating, taste, packaging, comment, display_name)
		VALUES ($1,$2,5,5,5,'好吃','A')`, b.branchID, h.ID)
	noErr(t, err)
	rv, err := a.StoreReviews(ctx, hcd.Slug, "all", 0)
	noErr(t, err)
	rm, err := a.StoreReviews(ctx, b.slug, "all", 0)
	noErr(t, err)
	if rv.Summary.Count != 1 || len(rv.Reviews) != 1 || rm.Summary.Count != 0 {
		t.Fatalf("reviews: brand %d, main %d", rv.Summary.Count, rm.Summary.Count)
	}

	// Favourites are kept per brand.
	cust, _ := customer(t)
	noErr(t, a.SetFavorite(ctx, cust, hcd.Slug, true))
	on, err := a.IsFavorite(ctx, cust, hcd.Slug)
	noErr(t, err)
	off, err := a.IsFavorite(ctx, cust, b.slug)
	noErr(t, err)
	if !on || off {
		t.Fatalf("favourites: brand %v, main %v", on, off)
	}
	mine, err := a.MyStores(ctx, cust, true)
	noErr(t, err)
	if len(mine) != 1 || mine[0].Slug != hcd.Slug || mine[0].Name != "好吃党" || mine[0].LogoURL != "https://example.com/hcd.png" {
		t.Fatalf("my stores: %+v", mine)
	}

	// Claimable coupons are the brand's.
	_, err = a.SaveStoreBatch(ctx, b.branchID, 0, app.CouponBatchInput{Name: "好吃党券", Kind: app.CouponStoreThreshold,
		Value: 500, Threshold: 5000, ValidDays: 7, BrandID: b.hcd}, b.staff)
	noErr(t, err)
	hc, err := a.StoreCoupons(ctx, hcd.Slug, 0)
	noErr(t, err)
	mc, err := a.StoreCoupons(ctx, b.slug, 0)
	noErr(t, err)
	if len(hc) != 1 || len(mc) != 0 {
		t.Fatalf("claimable coupons: brand %d, main %d", len(hc), len(mc))
	}
	_, err = a.ClaimCoupon(ctx, b.slug, hc[0].ID, cust)
	wantCode(t, err, "NOT_FOUND")
	_, err = a.ClaimCoupon(ctx, hcd.Slug, hc[0].ID, cust)
	noErr(t, err)

	// The brand's own page answers at its own number.
	site, err := a.SiteFor(ctx, strconv.Itoa(hcd.StoreNo)+".localhost")
	noErr(t, err)
	if !site.OwnSite() || site.Store.Slug != hcd.Slug || site.Store.BrandID != b.hcd || site.Store.Name != "好吃党" {
		t.Fatalf("site: %+v", site.Store)
	}

	// The mall shows a card for each brand.
	city := uniq("Brandcity")
	_, err = a.DB.Exec(ctx, `UPDATE branches SET mall_listed=true, lat=14.55, lng=121.02, city=$2, country_code='PH' WHERE id=$1`, b.branchID, city)
	noErr(t, err)
	list, err := a.MallStores(ctx, app.MallQuery{CountryCode: "PH", City: city})
	noErr(t, err)
	cards := map[string]app.MallStore{}
	for _, s := range list.Stores {
		cards[s.Slug] = s
	}
	hs, ms := cards[hcd.Slug], cards[b.slug]
	if len(list.Stores) != 2 || hs.Name != "好吃党" || ms.Name != "一品湘" || hs.Tagline != "湘味小炒" || hs.LogoURL != "https://example.com/hcd.png" {
		t.Fatalf("mall cards: %+v", list.Stores)
	}
	if len(hs.Promos) != 1 || hs.Promos[0].Value != 1000 || len(ms.Promos) != 1 || ms.Promos[0].Value != 1500 {
		t.Fatalf("mall promotions: %+v / %+v", hs.Promos, ms.Promos)
	}
	found, err := a.MallStores(ctx, app.MallQuery{CountryCode: "PH", City: city, Q: "辣椒"})
	noErr(t, err)
	if len(found.Stores) != 1 || found.Stores[0].Slug != hcd.Slug || found.Stores[0].Dishes[0].Price != 5200 {
		t.Fatalf("mall search: %+v", found.Stores)
	}
	// a brand hidden from the mall has no card
	in.MallVisible = false
	_, err = a.UpdateBrand(ctx, b.branchID, b.hcd, b.staff, in)
	noErr(t, err)
	list, err = a.MallStores(ctx, app.MallQuery{CountryCode: "PH", City: city})
	noErr(t, err)
	if len(list.Stores) != 1 || list.Stores[0].Slug != b.slug {
		t.Fatalf("hidden brand: %+v", list.Stores)
	}

	// The customer's orders on the brand's own page are the brand's.
	page, err := a.CustomerOrdersAt(ctx, buyer, b.branchID, b.hcd, "all", "", 0)
	noErr(t, err)
	for _, c := range page.Orders {
		if c.Store.Slug != hcd.Slug || c.Store.Name != "好吃党" {
			t.Fatalf("brand page orders: %+v", c.Store)
		}
	}
	if len(page.Orders) != 2 {
		t.Fatalf("brand page orders: %d", len(page.Orders))
	}
}
