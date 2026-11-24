package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

func TestBills(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	admin := adminActor(t)
	cust, phone := customer(t)
	sf := s.give(t, cust, app.CouponStoreFree, 2000, 0, 30)
	pf := s.give(t, cust, app.CouponPlatformFree, 1000, 0, 30)

	order := func(coupons []int64) (int64, *app.Quote) {
		q := s.quoteAs(t, phone, "pickup", &coupons)
		p, err := a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
		noErr(t, err)
		return p.OrderID, q
	}
	done := func(id int64, at string) {
		_, err := a.DB.Exec(ctx, `UPDATE orders SET acceptance_status='accepted', fulfillment_status='picked_up',
			completed_at=`+at+`, created_at=LEAST(created_at, `+at+`) WHERE id=$1`, id)
		noErr(t, err)
	}
	// one order with both coupons, finished; one plain, cancelled; one plain, finished
	o1, q1 := order([]int64{sf, pf})
	done(o1, "now()")
	o2, _ := order(nil)
	_, err := a.DB.Exec(ctx, `UPDATE orders SET acceptance_status='cancelled' WHERE id=$1`, o2)
	noErr(t, err)
	o3, q3 := order([]int64{})
	done(o3, "now()")

	today := time.Now().In(time.FixedZone("PHT", 8*3600)).Format("2006-01-02")
	l, err := a.Bills(ctx, s.branchID, today, today, "")
	noErr(t, err)
	if len(l.Orders) != 3 || l.Count != 3 {
		t.Fatalf("rows: %d", len(l.Orders))
	}
	var r1, r2 app.BillOrder
	for _, o := range l.Orders {
		switch o.ID {
		case o1:
			r1 = o
		case o2:
			r2 = o
		}
	}
	if r1.StoreFree != 2000 || r1.PlatformFree != 1000 || r1.Paid != q1.Totals.Total || r1.Takings != q1.Totals.Total+1000 ||
		len(r1.Discounts) < 2 || r1.State != "completed" {
		t.Fatalf("order with coupons: %+v", r1)
	}
	if r2.State != "cancelled" || r2.Paid != 0 {
		t.Fatalf("cancelled: %+v", r2)
	}
	if l.Sum.Paid != q1.Totals.Total+q3.Totals.Total || l.Sum.PlatformFree != 1000 || l.Sum.StoreFree != 2000 {
		t.Fatalf("sum: %+v", l.Sum)
	}
	for f, want := range map[string]int{"coupon": 1, "none": 2, app.CouponPlatformFree: 1, app.CouponStoreThreshold: 0} {
		l, err := a.Bills(ctx, s.branchID, today, today, f)
		noErr(t, err)
		if len(l.Orders) != want {
			t.Fatalf("filter %s: %d rows, want %d", f, len(l.Orders), want)
		}
	}
	_, err = a.Bills(ctx, s.branchID, today, today, "x")
	wantCode(t, err, "VALIDATION")

	sum, err := a.BillSummary(ctx, s.branchID, today, today)
	noErr(t, err)
	if sum.Orders != 2 || sum.Cancelled != 1 || sum.PlatformDiscount != 1000 || sum.Takings != sum.Paid+1000 || len(sum.Kinds) != 6 {
		t.Fatalf("summary: %+v", sum)
	}
	for _, k := range sum.Kinds {
		if k.Kind == app.CouponStoreFree && (k.Orders != 1 || k.Amount != 2000 || k.FundedBy != "store") ||
			k.Kind == app.CouponPlatformFree && (k.Orders != 1 || k.Amount != 1000 || k.FundedBy != "platform") {
			t.Fatalf("kind: %+v", k)
		}
	}
	if len(sum.Days) != 1 || sum.Days[0].Orders != 2 {
		t.Fatalf("days: %+v", sum.Days)
	}

	// last month's platform coupons become a statement, once
	pf2 := s.give(t, cust, app.CouponPlatformFree, 500, 0, 30)
	o5, _ := order([]int64{pf2})
	done(o5, "date_trunc('month', now() AT TIME ZONE 'Asia/Manila') AT TIME ZONE 'Asia/Manila' - interval '5 days'")
	_, err = a.MakeSettlements(ctx)
	noErr(t, err)
	n, err := a.MakeSettlements(ctx)
	noErr(t, err)
	if n != 0 {
		t.Fatalf("made %d statements twice", n)
	}
	all, err := a.ListSettlements(ctx, "pending")
	noErr(t, err)
	var st *app.Settlement
	for i := range all {
		if all[i].BranchID == s.branchID {
			st = &all[i]
		}
	}
	if st == nil || st.Orders != 1 || st.Amount != 500 || st.Currency != "PHP" {
		t.Fatalf("statement: %+v", st)
	}
	os, err := a.SettlementOrders(ctx, s.branchID, st.ID)
	noErr(t, err)
	if len(os) != 1 || os[0].ID != o5 || os[0].PlatformFree != 500 {
		t.Fatalf("statement orders: %+v", os)
	}
	other := newStore(t, false)
	_, err = a.SettlementOrders(ctx, other.branchID, st.ID)
	wantCode(t, err, "NOT_FOUND")
	wantCode(t, a.MarkSettlementPaid(ctx, admin, st.ID, "2026-12-05", ""), "VALIDATION")
	noErr(t, a.MarkSettlementPaid(ctx, admin, st.ID, "2026-12-05", "BDO-123"))
	wantCode(t, a.MarkSettlementPaid(ctx, admin, st.ID, "2026-12-05", "BDO-123"), "INVALID_STATE")
	sum, err = a.BillSummary(ctx, s.branchID, today, today)
	noErr(t, err)
	if len(sum.Settlements) != 1 || sum.Settlements[0].Status != "paid" || *sum.Settlements[0].PaidOn != "2026-12-05" {
		t.Fatalf("store sees: %+v", sum.Settlements)
	}

	sb, err := a.StoreBills(ctx, today, today)
	noErr(t, err)
	var found bool
	for _, x := range sb {
		if x.BranchID == s.branchID {
			found = true
			if x.Orders != 2 || x.PlatformDiscount != 1000 {
				t.Fatalf("store bill: %+v", x)
			}
		}
	}
	if !found {
		t.Fatal("store missing from the platform's bill")
	}
}
