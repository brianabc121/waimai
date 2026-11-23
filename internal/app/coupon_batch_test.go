package app_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

func ptr[T any](v T) *T { return &v }

func TestStoreClaimCoupons(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	// a threshold coupon must save less than its threshold
	_, err := a.SaveStoreBatch(ctx, s.branchID, 0, app.CouponBatchInput{Kind: app.CouponStoreThreshold, Value: 3000,
		Threshold: 2000, ValidDays: 7}, s.staff)
	wantCode(t, err, "VALIDATION")
	id, err := a.SaveStoreBatch(ctx, s.branchID, 0, app.CouponBatchInput{Name: "满100减15", Kind: app.CouponStoreThreshold,
		Value: 1500, Threshold: 10000, ValidDays: 7, TotalQuota: ptr(2)}, s.staff)
	noErr(t, err)

	cust, phone := customer(t)
	list, err := a.StoreCoupons(ctx, s.slug, 0)
	noErr(t, err)
	if len(list) != 1 || list[0].ID != id || list[0].Claimed || list[0].Gone {
		t.Fatalf("store coupons: %+v", list)
	}
	// two taps at once give one coupon (one per customer by default)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, errs[i] = a.ClaimCoupon(ctx, s.slug, id, cust) }(i)
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("claims: %v / %v", errs[0], errs[1])
	}
	for _, e := range errs {
		if e != nil {
			wantCode(t, e, "COUPON_UNAVAILABLE")
		}
	}
	list, _ = a.StoreCoupons(ctx, s.slug, cust)
	if !list[0].Claimed {
		t.Fatal("claimed not shown")
	}
	mine, err := a.MyCoupons(ctx, cust)
	noErr(t, err)
	if len(mine) != 1 || mine[0].Kind != app.CouponStoreThreshold || mine[0].Value != 1500 {
		t.Fatalf("my coupons: %+v", mine)
	}
	// it is used at checkout
	q := s.quoteAs(t, phone, "pickup", nil)
	if len(q.Coupons) != 1 || q.Coupons[0].ID != mine[0].ID || q.Coupons[0].Amount != 1500 {
		t.Fatalf("claimed coupon at checkout: %+v", q.Coupons)
	}

	// the quota runs out
	c2, _ := customer(t)
	_, err = a.ClaimCoupon(ctx, s.slug, id, c2)
	noErr(t, err)
	c3, _ := customer(t)
	_, err = a.ClaimCoupon(ctx, s.slug, id, c3)
	wantCode(t, err, "COUPON_UNAVAILABLE")
	list, _ = a.StoreCoupons(ctx, s.slug, c3)
	if !list[0].Gone {
		t.Fatal("gone not shown")
	}
	_, err = a.SaveStoreBatch(ctx, s.branchID, id, app.CouponBatchInput{Name: "x", Kind: app.CouponStoreThreshold,
		Value: 1500, Threshold: 10000, ValidDays: 7, TotalQuota: ptr(1)}, s.staff)
	wantCode(t, err, "VALIDATION")

	// paused: not on the page and cannot be claimed; coupons already given stay
	noErr(t, a.SetStoreBatchStatus(ctx, s.branchID, id, "paused", s.staff))
	list, _ = a.StoreCoupons(ctx, s.slug, 0)
	if len(list) != 0 {
		t.Fatalf("paused batch listed: %+v", list)
	}
	noErr(t, a.SetStoreBatchStatus(ctx, s.branchID, id, "ended", s.staff))
	wantCode(t, a.SetStoreBatchStatus(ctx, s.branchID, id, "active", s.staff), "INVALID_STATE")
	mine, _ = a.MyCoupons(ctx, cust)
	if mine[0].Status != "available" {
		t.Fatalf("coupon after the batch ended: %s", mine[0].Status)
	}
	rows, err := a.ListStoreBatches(ctx, s.branchID)
	noErr(t, err)
	if len(rows) != 1 || rows[0].Stats.Issued != 2 || rows[0].Stats.Open != 2 {
		t.Fatalf("batches: %+v", rows)
	}
	// another store's batch cannot be claimed through this page
	other := newStore(t, false)
	_, err = a.ClaimCoupon(ctx, other.slug, id, c3)
	wantCode(t, err, "NOT_FOUND")
}

func TestPlatformCoupons(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	admin := adminActor(t)
	// this test's store is the only one in its province
	prov := uniq("Province ")
	_, err := a.DB.Exec(ctx, `UPDATE branches SET country_code='PH', country='Philippines', province=$2 WHERE id=$1`,
		s.branchID, prov)
	noErr(t, err)
	_, err = a.SavePlatformBatch(ctx, admin, 0, app.CouponBatchInput{Name: "x", Value: 1000, ValidDays: 14,
		Source: "manual", CountryCode: "AQ"})
	wantCode(t, err, "VALIDATION") // no store there
	in := app.CouponBatchInput{Name: "新人礼", Value: 1000, ValidDays: 14, Source: "new_customer",
		CountryCode: "ph", Province: prov, Budget: ptr(int64(1500))}
	nc, err := a.SavePlatformBatch(ctx, admin, 0, in)
	noErr(t, err)
	defer func() { _ = a.SetPlatformBatchStatus(ctx, admin, nc, "ended") }()

	// a new sign-up gets it, once; the budget covers just one
	phone := fmt.Sprintf("+6392%08d", time.Now().UnixNano()%1e8)
	login := func(p string) {
		code, err := a.RequestOTP(ctx, p, "")
		noErr(t, err)
		_, _, err = a.VerifyOTP(ctx, p, code)
		noErr(t, err)
	}
	login(phone)
	var cust int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM customers WHERE phone=$1`, phone).Scan(&cust))
	mine, err := a.MyCoupons(ctx, cust)
	noErr(t, err)
	if len(mine) != 1 || mine[0].Kind != app.CouponPlatformFree || mine[0].Value != 1000 {
		t.Fatalf("new customer coupons: %+v", mine)
	}
	phone2 := fmt.Sprintf("+6393%08d", time.Now().UnixNano()%1e8)
	login(phone2)
	var c2 int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM customers WHERE phone=$1`, phone2).Scan(&c2))
	if m, _ := a.MyCoupons(ctx, c2); len(m) != 0 {
		t.Fatalf("budget ran out but got %+v", m)
	}
	// it is used at checkout, paid by the platform
	q := s.quoteAs(t, phone, "pickup", nil)
	if len(q.Coupons) != 1 || q.Coupons[0].Amount != 1000 || q.Totals.PlatformSubsidy != 1000 {
		t.Fatalf("quote: %+v / %+v", q.Coupons, q.Totals)
	}
	if mine[0].Country != "Philippines" || mine[0].Province != prov || mine[0].Currency != "PHP" {
		t.Fatalf("coupon region: %+v", mine[0])
	}
	// it does not work at a store elsewhere
	other := newStore(t, false)
	if q := other.quoteAs(t, phone, "pickup", nil); len(q.Coupons) != 0 {
		t.Fatalf("platform coupon outside its stores: %+v", q.Coupons)
	}

	// a manual batch, given by phone number
	man, err := a.SavePlatformBatch(ctx, admin, 0, app.CouponBatchInput{Name: "补偿券", Value: 500, ValidDays: 7,
		Source: "manual", CountryCode: "PH", Province: prov})
	noErr(t, err)
	defer func() { _ = a.SetPlatformBatchStatus(ctx, admin, man, "ended") }()
	res, err := a.IssuePlatform(ctx, admin, man, app.IssueInput{Phones: []string{phone, phone, phone2, "+639000000001"}})
	noErr(t, err)
	if res.Given != 2 || res.Skipped != 0 || len(res.NotFound) != 1 {
		t.Fatalf("issue: %+v", res)
	}
	res, err = a.IssuePlatform(ctx, admin, man, app.IssueInput{Phones: []string{phone}})
	noErr(t, err)
	if res.Given != 0 || res.Skipped != 1 {
		t.Fatalf("issue twice: %+v", res)
	}
	noErr(t, a.SetPlatformBatchStatus(ctx, admin, man, "paused"))
	_, err = a.IssuePlatform(ctx, admin, man, app.IssueInput{Phones: []string{phone}})
	wantCode(t, err, "INVALID_STATE")

	list, err := a.ListPlatformBatches(ctx)
	noErr(t, err)
	var found bool
	for _, b := range list {
		if b.ID == nc {
			found = true
			if b.Stats.Issued != 1 || b.Province != prov {
				t.Fatalf("batch row: %+v", b)
			}
		}
	}
	if !found {
		t.Fatal("batch not listed")
	}
	_, err = a.PlatformCouponSummary(ctx)
	noErr(t, err)
	cs, err := a.CouponCountries(ctx)
	noErr(t, err)
	var ph bool
	for _, c := range cs {
		ph = ph || c.CountryCode == "PH" && c.Currency == "PHP"
	}
	if !ph {
		t.Fatalf("countries: %+v", cs)
	}
}
