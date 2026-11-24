package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// The reconciliation page (docs/CHECKOUT_DISCOUNT_DESIGN.md §8): takings by
// method, checking online payments, the cash count and the discount log.
func TestReconciliation(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	gcash := payPH(t, s)
	b, err := a.BranchByID(ctx, s.branchID)
	noErr(t, err)
	loc, _ := time.LoadLocation(b.Timezone)
	today := time.Now().In(loc).Format("2006-01-02")

	pay := func(name string, in app.CloseInput, adj *app.Adjust) *app.Order {
		t.Helper()
		tbl, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: name, Active: true})
		noErr(t, err)
		bill, err := a.OpenTable(ctx, tbl, 1, nil, s.staff)
		noErr(t, err)
		bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: s.water, Qty: 2}), s.staff)
		noErr(t, err)
		bill = finishKitchen(t, bill)
		in.ExpectedManifest, in.Adjust = bill.Order.ManifestVersion, adj
		if in.PayType == "cash" {
			in.AmountReceived = 1000000
		}
		bill, err = a.CloseTable(ctx, bill.SessionID, in, s.staff)
		noErr(t, err)
		return bill.Order
	}
	cash := pay("R1", app.CloseInput{PayType: "cash"}, &app.Adjust{PercentBP: 9000, Reason: "熟客"})
	online := pay("R2", app.CloseInput{PayType: "online", PayChannelID: gcash}, nil)
	card := pay("R3", app.CloseInput{PayType: "card"}, &app.Adjust{Mode: "set_total", Amount: 100, Reason: "员工餐"})

	r, err := a.Reconciliation(ctx, s.branchID, today, today)
	noErr(t, err)
	if r.Count != 3 || r.Total != cash.Totals.Total+online.Totals.Total+card.Totals.Total || len(r.Rows) != 3 {
		t.Fatalf("summary: %+v", r)
	}
	if r.Rows[0].PayType != "cash" || r.Rows[1].PayType != "card" || r.Rows[2].PayType != "online" || r.Rows[2].Name != "GCash" ||
		r.Rows[2].Unchecked != 1 || r.Rows[0].Dine != cash.Totals.Total {
		t.Fatalf("rows: %+v", r.Rows)
	}
	if r.Cash.Counter != cash.Totals.Total || r.Cash.Expected != cash.Totals.Total ||
		r.Discounts.Staff != cash.Totals.StaffDiscount || r.Discounts.RoundOff != card.Totals.RoundOff || r.CashCount != nil {
		t.Fatalf("cash and discounts: %+v %+v", r.Cash, r.Discounts)
	}

	// online payments: checked one by one
	l, err := a.OnlinePayments(ctx, s.branchID, today, today, 0, "unchecked")
	noErr(t, err)
	if len(l) != 1 || l[0].OrderID != online.ID || l[0].Method != "GCash" || l[0].Amount != online.Totals.Total {
		t.Fatalf("online: %+v", l)
	}
	wantCode(t, a.SetPaymentChecks(ctx, s.branchID, []int64{online.ID}, "problem", "", s.staff), "VALIDATION")
	wantCode(t, a.SetPaymentChecks(ctx, s.branchID, []int64{cash.ID}, "ok", "", s.staff), "VALIDATION")
	noErr(t, a.SetPaymentChecks(ctx, s.branchID, []int64{online.ID}, "problem", "没收到", s.staff))
	o, err := a.GetOrder(ctx, online.ID)
	noErr(t, err)
	if o.PayCheck != "problem" || o.PayCheckNote != "没收到" {
		t.Fatalf("order check: %q %q", o.PayCheck, o.PayCheckNote)
	}
	r, err = a.Reconciliation(ctx, s.branchID, today, today)
	noErr(t, err)
	if r.Problems != 1 || r.Rows[2].Problem != 1 {
		t.Fatalf("problem: %+v", r)
	}
	noErr(t, a.SetPaymentChecks(ctx, s.branchID, []int64{online.ID}, "ok", "ignored", s.staff))
	l, err = a.OnlinePayments(ctx, s.branchID, today, today, gcash, "ok")
	noErr(t, err)
	if len(l) != 1 || l[0].Note != "" || l[0].CheckedBy == "" {
		t.Fatalf("ok: %+v", l)
	}

	// the cash count: what the drawer should hold is worked out by the server
	k, err := a.SaveCashCount(ctx, s.branchID, today, cash.Totals.Total-5000, "少 50", s.staff)
	noErr(t, err)
	if k.Expected != cash.Totals.Total || k.Counted != cash.Totals.Total-5000 || k.CountedBy == "" {
		t.Fatalf("count: %+v", k)
	}
	r, err = a.Reconciliation(ctx, s.branchID, today, today)
	noErr(t, err)
	if r.CashCount == nil || r.CashCount.Note != "少 50" {
		t.Fatalf("count on the page: %+v", r.CashCount)
	}

	// the discount log, by who gave them
	d, err := a.Discounts(ctx, s.branchID, today, today)
	noErr(t, err)
	if len(d.Entries) != 2 || len(d.ByStaff) != 1 || d.ByStaff[0].Count != 2 ||
		d.ByStaff[0].Amount != cash.Totals.StaffDiscount+card.Totals.RoundOff {
		t.Fatalf("discounts: %+v", d)
	}
}
