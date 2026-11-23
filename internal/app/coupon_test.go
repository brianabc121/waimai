package app_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// customer makes a customer account with a fresh phone number.
func customer(t *testing.T) (int64, string) {
	t.Helper()
	phone := fmt.Sprintf("+639%09d", (time.Now().UnixNano()/1000+seq.Add(1))%1e9)
	var id int64
	noErr(t, testApp.DB.QueryRow(context.Background(), `INSERT INTO customers (phone) VALUES ($1)
		ON CONFLICT (phone) DO UPDATE SET phone=EXCLUDED.phone RETURNING id`, phone).Scan(&id))
	return id, phone
}

// give hands a customer a coupon directly, as a later batch (store claim or
// platform coupons) would.
func (s *store) give(t *testing.T, customerID int64, kind string, value, threshold int64, days int) int64 {
	t.Helper()
	ctx := context.Background()
	issuer, branch := "store", &s.branchID
	if kind == app.CouponPlatformFree {
		issuer, branch = "platform", nil
	}
	var batch, id int64
	noErr(t, testApp.DB.QueryRow(ctx, `INSERT INTO coupon_batches (issuer, branch_id, kind, source, name, currency, value,
		threshold, valid_days) VALUES ($1,$2,$3,'manual',$3,(SELECT currency FROM branches WHERE id=$4),$5,$6,$7) RETURNING id`,
		issuer, branch, kind, s.branchID, value, threshold, max(days, 1)).Scan(&batch))
	noErr(t, testApp.DB.QueryRow(ctx, `INSERT INTO customer_coupons (batch_id, customer_id, issuer, kind, branch_id, currency,
		name, value, threshold, source, expires_at)
		SELECT id, $2, issuer, kind, branch_id, currency, name, value, threshold, source, now() + make_interval(days => $3)
		FROM coupon_batches WHERE id=$1 RETURNING id`, batch, customerID, days).Scan(&id))
	return id
}

func (s *store) quoteAs(t *testing.T, phone, service string, coupons *[]int64) *app.Quote {
	t.Helper()
	in := app.QuoteInput{ServiceType: service, Items: s.cart(), CouponIDs: coupons}
	if service == "delivery" {
		in.ZoneID = &s.zoneID
	}
	q, err := testApp.CreateQuote(context.Background(), s.slug, in, phone)
	noErr(t, err)
	return q
}

func couponStatus(t *testing.T, id int64) (status string, orderID *int64) {
	t.Helper()
	noErr(t, testApp.DB.QueryRow(context.Background(), `SELECT status, order_id FROM customer_coupons WHERE id=$1`, id).
		Scan(&status, &orderID))
	return
}

func TestCouponsStackInOrder(t *testing.T) {
	s := newStore(t, true) // tax included; cart is 437.00 of food, delivery 69.00
	ctx := context.Background()
	a := testApp
	cust, phone := customer(t)
	threshold := s.give(t, cust, app.CouponStoreThreshold, 4000, 40000, 10)
	small := s.give(t, cust, app.CouponStoreFree, 1500, 0, 2)
	free := s.give(t, cust, app.CouponStoreFree, 2000, 0, 20)
	ship := s.give(t, cust, app.CouponStoreDelivery, 3000, 20000, 10)
	platform := s.give(t, cust, app.CouponPlatformFree, 2500, 0, 10)
	tooBig := s.give(t, cust, app.CouponStoreThreshold, 8000, 60000, 10)
	other := newStore(t, true)
	other.give(t, cust, app.CouponStoreFree, 5000, 0, 10) // another store's coupon is not offered here

	q := s.quoteAs(t, phone, "delivery", nil)
	// one of each kind, the bigger no-minimum coupon, in the fixed order
	want := []int64{threshold, free, ship, platform}
	if len(q.Coupons) != 4 {
		t.Fatalf("coupons used: %+v", q.Coupons)
	}
	for i, c := range q.Coupons {
		if c.ID != want[i] {
			t.Fatalf("coupon %d is %d, want %d", i, c.ID, want[i])
		}
	}
	tot := q.Totals
	// food 437 - 40 - 20 = 377; service charge 5% = 18.85; delivery 69 - 30
	if tot.ServiceCharge != 1885 || tot.MerchantDiscount != 9000 || tot.PlatformSubsidy != 2500 || tot.Discount != 11500 ||
		tot.Total != 43700+1000+6900+1885-9000-2500 {
		t.Fatalf("totals: %+v", tot)
	}
	// the platform coupon does not change the tax: 377 + 10 + 18.85 at 12% included
	if tot.TaxAmount != 4348 {
		t.Fatalf("tax %d", tot.TaxAmount)
	}
	if len(q.CouponChoices) != 6 {
		t.Fatalf("choices: %d", len(q.CouponChoices))
	}
	for _, ch := range q.CouponChoices {
		switch ch.ID {
		case tooBig:
			if ch.Usable || ch.Reason != "below_threshold" || ch.Short != 60000-43700 {
				t.Fatalf("too big: %+v", ch)
			}
		case small:
			if !ch.Usable || ch.Selected {
				t.Fatalf("small: %+v", ch)
			}
		}
	}

	// pickup: the delivery coupon cannot be used
	pq := s.quoteAs(t, phone, "pickup", nil)
	for _, ch := range pq.CouponChoices {
		if ch.ID == ship && (ch.Usable || ch.Reason != "delivery_only") {
			t.Fatalf("delivery coupon on pickup: %+v", ch)
		}
	}
	// the customer can choose none, or choose some
	none := s.quoteAs(t, phone, "delivery", &[]int64{})
	if len(none.Coupons) != 0 || none.Totals.Discount != 0 {
		t.Fatalf("no coupons: %+v", none.Totals)
	}
	some := s.quoteAs(t, phone, "delivery", &[]int64{small, platform})
	if len(some.Coupons) != 2 || some.Totals.MerchantDiscount != 1500 {
		t.Fatalf("chosen: %+v", some.Coupons)
	}
	_, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: s.cart(),
		CouponIDs: &[]int64{small, free}}, phone)
	wantCode(t, err, "VALIDATION")
	// someone else's phone sees none of them
	if q := s.quote(t, "delivery", s.cart()); len(q.CouponChoices) != 0 {
		t.Fatalf("another customer saw coupons: %+v", q.CouponChoices)
	}

	// the order uses them and records each discount
	p, err := a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main"})
	noErr(t, err)
	o, err := a.OrderByToken(ctx, p.AccessToken)
	noErr(t, err)
	if o.Totals != func() app.Totals { x := tot; x.PromotionDiscount = 0; return x }() {
		t.Fatalf("order totals %+v, quote %+v", o.Totals, tot)
	}
	if len(o.Discounts) != 4 || o.Discounts[3].FundedBy != "platform" || o.Discounts[3].Amount != 2500 {
		t.Fatalf("discounts: %+v", o.Discounts)
	}
	for _, id := range want {
		if st, oid := couponStatus(t, id); st != "used" || oid == nil || *oid != o.ID {
			t.Fatalf("coupon %d: %s", id, st)
		}
	}
	// the cash collected is what the customer pays
	if o.Totals.Total != 41985 {
		t.Fatalf("total %d", o.Totals.Total)
	}

	// the customer cancels: the coupons come back
	_, err = a.CustomerCancel(ctx, p.AccessToken)
	noErr(t, err)
	for _, id := range want {
		if st, oid := couponStatus(t, id); st != "unused" || oid != nil {
			t.Fatalf("coupon %d after cancel: %s", id, st)
		}
	}
	mine, err := a.MyCoupons(ctx, cust)
	noErr(t, err)
	if len(mine) != 7 || mine[0].ID != small || mine[0].Status != "available" {
		t.Fatalf("my coupons: %+v", mine)
	}
}

func TestCouponUsedTwiceAtOnce(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	cust, phone := customer(t)
	id := s.give(t, cust, app.CouponStoreFree, 1000, 0, 5)
	q1 := s.quoteAs(t, phone, "pickup", nil)
	q2 := s.quoteAs(t, phone, "pickup", nil)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, q := range []*app.Quote{q1, q2} {
		wg.Add(1)
		go func(i int, q *app.Quote) {
			defer wg.Done()
			_, errs[i] = a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
		}(i, q)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else {
			wantCode(t, err, "QUOTE_CHANGED")
		}
	}
	if ok != 1 {
		t.Fatalf("placed %d orders with one coupon", ok)
	}
	if st, _ := couponStatus(t, id); st != "used" {
		t.Fatalf("coupon %s", st)
	}
	// an expired coupon is not offered
	old := s.give(t, cust, app.CouponStoreFree, 1000, 0, 0)
	if q := s.quoteAs(t, phone, "pickup", nil); len(q.CouponChoices) != 0 {
		t.Fatalf("expired coupon offered: %+v (%d)", q.CouponChoices, old)
	}
}

func TestRepurchaseGift(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	cust, phone := customer(t)
	r, err := a.RepurchaseRule(ctx, s.branchID)
	noErr(t, err)
	if r.Enabled || r.Value != 2000 || r.ValidDays != 30 || r.Kind != app.CouponStoreFree {
		t.Fatalf("default rule: %+v", r)
	}
	wantCode(t, a.SaveRepurchaseRule(ctx, s.branchID, app.RepurchaseInput{Enabled: true, Kind: app.CouponStoreFree,
		Value: 2000, ValidDays: 120}, s.staff), "VALIDATION")
	wantCode(t, a.SaveRepurchaseRule(ctx, s.branchID, app.RepurchaseInput{Enabled: true, Kind: app.CouponStoreThreshold,
		Value: 2000, Threshold: 1000, ValidDays: 30}, s.staff), "VALIDATION")
	noErr(t, a.SaveRepurchaseRule(ctx, s.branchID, app.RepurchaseInput{Enabled: true, Kind: app.CouponStoreFree,
		Value: 2000, ValidDays: 30, MinOrder: 10000}, s.staff))

	finish := func(qty int) (*app.Order, string) {
		q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup",
			Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: qty}}}, phone)
		noErr(t, err)
		p, err := a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
		noErr(t, err)
		o, err := a.Accept(ctx, p.OrderID, 0, s.staff)
		noErr(t, err)
		o, err = a.SetItemStatus(ctx, o.ID, o.Items[0].ID, "ready", s.staff)
		noErr(t, err)
		o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
		noErr(t, err)
		_, err = a.StaffHandoff(ctx, o.ID, s.staff)
		noErr(t, err)
		o, err = a.OrderByToken(ctx, p.AccessToken)
		noErr(t, err)
		return o, p.AccessToken
	}
	// a cancelled order earns nothing
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup",
		Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 2}}}, phone)
	noErr(t, err)
	p, err := a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)
	_, err = a.CustomerCancel(ctx, p.AccessToken)
	noErr(t, err)

	// the first completed order is below the minimum (69.00 < 100.00): no gift,
	// and it was the first, so the next one does not count as a first order either
	o, _ := finish(1)
	if o.Gift != nil {
		t.Fatalf("gift below the minimum: %+v", o.Gift)
	}
	o, _ = finish(2)
	if o.Gift != nil {
		t.Fatal("second completed order got the first-order gift")
	}

	// a new customer whose first order qualifies gets one, valid for 30 days
	cust, phone = customer(t)
	o, _ = finish(2)
	g := o.Gift
	if g == nil || g.Kind != app.CouponStoreFree || g.Value != 2000 || g.Status != "available" || g.StoreSlug != s.slug {
		t.Fatalf("gift: %+v", g)
	}
	manila := time.FixedZone("PHT", 8*3600)
	now := time.Now().In(manila)
	if want := time.Date(now.Year(), now.Month(), now.Day()+30, 0, 0, 0, 0, manila); !g.ExpiresAt.Equal(want) {
		t.Fatalf("expires %v, want %v", g.ExpiresAt, want)
	}
	var msgs int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM customer_messages WHERE customer_id=$1 AND kind='wallet'`, cust).Scan(&msgs))
	if msgs != 1 {
		t.Fatalf("messages %d", msgs)
	}
	// it is used on the next order, which earns nothing more
	q2 := s.quoteAs(t, phone, "pickup", nil)
	if len(q2.Coupons) != 1 || q2.Coupons[0].ID != g.ID || q2.Coupons[0].Amount != 2000 {
		t.Fatalf("next order: %+v", q2.Coupons)
	}
	// switching off and on does not give it twice
	noErr(t, a.SaveRepurchaseRule(ctx, s.branchID, app.RepurchaseInput{Kind: app.CouponStoreFree, Value: 2000, ValidDays: 30}, s.staff))
	noErr(t, a.SaveRepurchaseRule(ctx, s.branchID, app.RepurchaseInput{Enabled: true, Kind: app.CouponStoreFree, Value: 3000, ValidDays: 30}, s.staff))
	o, _ = finish(2)
	if o.Gift != nil {
		t.Fatal("gift given twice")
	}
	r, err = a.RepurchaseRule(ctx, s.branchID)
	noErr(t, err)
	if !r.Enabled || r.Value != 3000 || r.Stats.Issued != 1 || r.Stats.Used != 1 || r.Stats.Cost != 2000 {
		t.Fatalf("rule and stats: %+v", r)
	}

	// reminders go out once, for coupons about to run out
	_, err = a.DB.Exec(ctx, `UPDATE customer_coupons SET issued_at = now() - interval '28 days',
		expires_at = now() + interval '2 days', status='unused', order_id=NULL WHERE id=$1`, g.ID)
	noErr(t, err)
	n, err := a.RemindExpiringCoupons(ctx)
	noErr(t, err)
	if n < 1 {
		t.Fatal("no reminder")
	}
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM customer_messages WHERE customer_id=$1 AND kind='wallet'`, cust).Scan(&msgs))
	if msgs != 2 {
		t.Fatalf("messages after reminder %d", msgs)
	}
}
