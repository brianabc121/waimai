package app_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// Mall orders and orders on the store's own page (PRICING_CHECK.md cell 17):
// the same cart costs the same to the cent; a platform coupon only works in
// the mall (docs/ORDER_TYPES_DESIGN.md §9.4, rule D12).
func TestMallVsWebOrder(t *testing.T) {
	s := newStore(t, true) // tax included; cart 437.00 of food, delivery 69.00
	ctx := context.Background()
	a := testApp

	quote := func(phone string, own bool, coupons *[]int64) *app.Quote {
		t.Helper()
		in := app.QuoteInput{ServiceType: "delivery", ZoneID: &s.zoneID, Items: s.cart(), CouponIDs: coupons, OwnSite: own}
		q, err := a.CreateQuote(ctx, s.slug, in, phone)
		noErr(t, err)
		return q
	}
	place := func(phone string, q *app.Quote, own bool) (*app.Order, error) {
		t.Helper()
		p, err := a.PlaceOrder(ctx, s.slug, phone, uniq("k"),
			app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main St", OwnSite: own})
		if err != nil {
			return nil, err
		}
		return a.OrderByToken(ctx, p.AccessToken)
	}

	// Without coupons the two are the same to the cent.
	_, plain := customer(t)
	mq, wq := quote(plain, false, nil), quote(plain, true, nil)
	if mq.Totals != wq.Totals {
		t.Fatalf("mall %+v, own page %+v", mq.Totals, wq.Totals)
	}

	// A store coupon and a platform coupon of 25.00.
	cust, phone := customer(t)
	store1 := s.give(t, cust, app.CouponStoreFree, 2000, 0, 10)
	store2 := s.give(t, cust, app.CouponStoreFree, 2000, 0, 10)
	platform := s.give(t, cust, app.CouponPlatformFree, 2500, 0, 10)

	mq = quote(phone, false, nil)
	wq = quote(phone, true, nil)
	// in the mall both apply; on the own page the platform one is not offered
	// and the customer pays exactly its 25.00 more
	if len(mq.Coupons) != 2 || mq.Totals.PlatformSubsidy != 2500 {
		t.Fatalf("mall coupons %+v, platform %d", mq.Coupons, mq.Totals.PlatformSubsidy)
	}
	if len(wq.Coupons) != 1 || wq.Totals.PlatformSubsidy != 0 || wq.Totals.Total != mq.Totals.Total+2500 {
		t.Fatalf("own page coupons %+v, totals %+v (mall %d)", wq.Coupons, wq.Totals, mq.Totals.Total)
	}
	for _, c := range wq.CouponChoices {
		if c.ID == platform {
			t.Fatalf("platform coupon offered on the own page: %+v", c)
		}
	}
	// asking for it by id on the own page does not use it either
	asked := quote(phone, true, &[]int64{store1, platform})
	if len(asked.Coupons) != 1 || asked.Totals.PlatformSubsidy != 0 {
		t.Fatalf("own page with the platform coupon asked for: %+v", asked.Coupons)
	}

	// A quote from the mall is not used on the own page.
	_, err := place(phone, mq, true)
	wantCode(t, err, "QUOTE_EXPIRED")

	web, err := place(phone, wq, true)
	noErr(t, err)
	if web.OrderType != app.TypeWeb || web.PublicNo != "Online-1" {
		t.Fatalf("own page order: %s %s", web.OrderType, web.PublicNo)
	}
	checked(t, "own page order", web.ID)

	mq = quote(phone, false, nil)
	mall, err := place(phone, mq, false)
	noErr(t, err)
	if mall.OrderType != app.TypeMall || mall.PublicNo != "Hippo-1" {
		t.Fatalf("mall order: %s %s", mall.OrderType, mall.PublicNo)
	}
	checked(t, "mall order with the platform coupon", mall.ID)
	if st, _ := couponStatus(t, platform); st != "used" {
		t.Fatalf("platform coupon: %s", st)
	}
	_ = store2

	// The own page lists the store's orders only.
	other := newStore(t, false)
	op, err := a.CreateQuote(ctx, other.slug, app.QuoteInput{ServiceType: "pickup", Items: other.cart()}, phone)
	noErr(t, err)
	_, err = a.PlaceOrder(ctx, other.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: op.ID, CustomerName: "Ann"})
	noErr(t, err)
	all, err := a.CustomerOrders(ctx, cust, "all", "", 0)
	noErr(t, err)
	here, err := a.CustomerOrdersAt(ctx, cust, s.branchID, 0, "all", "", 0)
	noErr(t, err)
	if len(all.Orders) != 3 || len(here.Orders) != 2 {
		t.Fatalf("orders: all %d, this store %d", len(all.Orders), len(here.Orders))
	}
}

// Which site a host serves, and the merchant's own domains.
func TestSiteAndDomains(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	oldDomain, oldLookup := a.SiteDomain, a.LookupCNAME
	t.Cleanup(func() { a.SiteDomain, a.LookupCNAME = oldDomain, oldLookup })
	a.SiteDomain = "abcfood.test"
	b, err := a.BranchByID(ctx, s.branchID)
	noErr(t, err)
	if b.StoreNo < 10001 {
		t.Fatalf("store number %d", b.StoreNo)
	}

	mode := func(host string) string {
		t.Helper()
		site, err := a.SiteFor(ctx, host)
		noErr(t, err)
		if site.Mode == app.SiteWeb {
			return site.Store.Slug
		}
		return site.Mode
	}
	for host, want := range map[string]string{
		fmt.Sprintf("%d.abcfood.test", b.StoreNo):      s.slug,
		fmt.Sprintf("%d.ABCfood.test:443", b.StoreNo):  s.slug,
		fmt.Sprintf("%d.localhost:5173", b.StoreNo):    s.slug,
		"order.abcfood.test":                           "mall",
		"abcfood.test":                                 "mall",
		"99999999.abcfood.test":                        "mall",
		fmt.Sprintf("0%d.abcfood.test", b.StoreNo):     "mall",
		fmt.Sprintf("%d.abcfood.test.evil", b.StoreNo): "mall",
		"": "mall",
	} {
		if got := mode(host); got != want {
			t.Errorf("%q serves %q, want %q", host, got, want)
		}
	}
	if u := a.StoreURL(b.StoreNo); u != fmt.Sprintf("https://%d.abcfood.test", b.StoreNo) {
		t.Fatalf("store url %s", u)
	}

	// The merchant's own domain is used once its CNAME points to the store.
	own := fmt.Sprintf("shop%d.example.test", b.StoreNo)
	_, err = a.AddDomain(ctx, s.branchID, app.DomainInput{Kind: "own", Domain: "not a domain"})
	wantCode(t, err, "VALIDATION")
	_, err = a.AddDomain(ctx, s.branchID, app.DomainInput{Kind: "own", Domain: "12.abcfood.test"})
	wantCode(t, err, "VALIDATION")
	d, err := a.AddDomain(ctx, s.branchID, app.DomainInput{Kind: "own", Domain: "https://" + own + "/"})
	noErr(t, err)
	if d.Domain != own || d.Status != "pending" || d.Target != fmt.Sprintf("%d.abcfood.test", b.StoreNo) {
		t.Fatalf("domain %+v", d)
	}
	other := newStore(t, false)
	_, err = a.AddDomain(ctx, other.branchID, app.DomainInput{Kind: "own", Domain: own})
	wantCode(t, err, "DOMAIN_TAKEN")
	if mode(own) != "mall" {
		t.Fatal("a domain not checked yet serves the store")
	}
	if ok, _ := a.TLSAllowed(ctx, own); ok {
		t.Fatal("certificate allowed before the check")
	}
	points := map[string]string{}
	a.LookupCNAME = func(_ context.Context, host string) (string, error) {
		if to, ok := points[host]; ok {
			return to + ".", nil
		}
		return host + ".", nil
	}
	points[own] = "somewhere.else.test"
	_, err = a.CheckDomains(ctx)
	noErr(t, err)
	if mode(own) != "mall" {
		t.Fatal("a domain pointing elsewhere serves the store")
	}
	points[own] = d.Target
	_, err = a.CheckDomains(ctx) // checked a minute ago: waits its turn
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE branch_domains SET checked_at = now() - interval '10 minutes' WHERE id=$1`, d.ID)
	noErr(t, err)
	_, err = a.CheckDomains(ctx)
	noErr(t, err)
	if mode(own) != s.slug {
		t.Fatal("the checked domain does not serve the store")
	}
	if ok, _ := a.TLSAllowed(ctx, own); !ok {
		t.Fatal("certificate not allowed for the checked domain")
	}

	// Asking the platform to buy a domain.
	_, err = a.AddDomain(ctx, s.branchID, app.DomainInput{Kind: "buy"})
	wantCode(t, err, "VALIDATION")
	req, err := a.AddDomain(ctx, s.branchID, app.DomainInput{Kind: "buy", Candidates: "yupinxuan.test, ypx.test", Note: "budget 30"})
	noErr(t, err)
	if req.Status != "requested" || req.Domain != "" {
		t.Fatalf("request %+v", req)
	}
	admin := adminActor(t)
	_, err = a.AdminUpdateDomain(ctx, admin, req.ID, app.AdminDomainInput{Status: "bought"})
	wantCode(t, err, "VALIDATION")
	_, err = a.AdminUpdateDomain(ctx, admin, req.ID, app.AdminDomainInput{Status: "pending"})
	wantCode(t, err, "VALIDATION")
	bought := fmt.Sprintf("ypx%d.test", b.StoreNo)
	up, err := a.AdminUpdateDomain(ctx, admin, req.ID, app.AdminDomainInput{Status: "active", Domain: bought, AdminNote: "paid"})
	noErr(t, err)
	if up.Status != "active" || up.VerifiedAt == nil || mode(bought) != s.slug {
		t.Fatalf("bought domain %+v", up)
	}
	open, err := a.AdminDomains(ctx, "")
	noErr(t, err)
	found := false
	for _, x := range open {
		found = found || x.ID == req.ID
	}
	if !found {
		t.Fatal("request not listed for the platform")
	}
	links, err := a.SiteLinks(ctx, s.branchID)
	noErr(t, err)
	if len(links.Domains) != 2 || links.URL == "" {
		t.Fatalf("links %+v", links)
	}
	noErr(t, a.DeleteDomain(ctx, s.branchID, d.ID))
	wantCode(t, a.DeleteDomain(ctx, other.branchID, req.ID), "NOT_FOUND")
	if mode(own) != "mall" {
		t.Fatal("a removed domain still serves the store")
	}
}
