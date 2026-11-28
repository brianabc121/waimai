package app_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// docs/PRICING_CHECK.md rule D7a and samples S12, S13 (docs/CODE_REVIEW_FIX_PLAN.md
// H07): refunding dishes one at a time gives back what refunding them
// together does. Every number is worked out by hand in the document.
//
// The store: pickup, tax 12% on top, service 5%, packaging 10.00; three
// waters at 30.00. With 15.00 off (second at half price, or 90 − 15) the
// order is 99.40; two waters at half price 64.12, two without an offer
// 81.76, one water 46.48.
func TestRefundSplitSamples_S12_S13(t *testing.T) {
	ctx, a := context.Background(), testApp
	for _, c := range []struct {
		name  string
		promo app.PromotionInput
		kind  string
		want  [3]int64
	}{
		// S12: 99.40 − 64.12, 64.12 − 46.48, and what is left
		{"S12 第二份半价 顾客原因", app.PromotionInput{Name: "half", Kind: "dish_second", Value: 5000}, "customer", [3]int64{3528, 1764, 4648}},
		{"S12 第二份半价 店铺原因", app.PromotionInput{Name: "half", Kind: "dish_second", Value: 5000}, "store", [3]int64{3528, 1764, 4648}},
		// S13: 99.40 − 81.76, 81.76 − 46.48, and what is left
		{"S13 满90减15 顾客原因", app.PromotionInput{Name: "m15", Kind: "amount_off", Threshold: 9000, Value: 1500}, "customer", [3]int64{1764, 3528, 4648}},
		// the store's fault keeps the offer's share of what is left: 15 × 60/90 = 10.00
		// off two waters: 50.00 + 2.50 + 10.00 + tax 7.50 = 70.00; 15 × 30/90 = 5.00 off
		// one: 25.00 + 1.25 + 10.00 + tax 4.35 = 40.60
		{"S13 满90减15 店铺原因", app.PromotionInput{Name: "m15", Kind: "amount_off", Threshold: 9000, Value: 1500}, "store", [3]int64{2940, 2940, 4060}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t, false)
			if c.promo.Kind == "dish_second" {
				c.promo.ItemIDs = []int64{s.water}
			}
			s.promo(t, c.promo)
			o := finishPickup(t, s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 3}}), s.staff)
			checked(t, "picked up", o.ID)
			if o.Totals.Total != 9940 || o.Totals.Discount != 1500 {
				t.Fatalf("order %+v", o.Totals)
			}
			it := o.Items[0].ID
			// together: two waters at once is the first two refunds
			two, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(it, 2), ReasonKind: c.kind, Dry: true}, s.staff)
			noErr(t, err)
			if two.Suggested != c.want[0]+c.want[1] {
				t.Fatalf("two at once: %d, want %d", two.Suggested, c.want[0]+c.want[1])
			}
			var sum int64
			for i, want := range c.want {
				q, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(it, 1), ReasonKind: c.kind, Dry: true}, s.staff)
				noErr(t, err)
				if q.Suggested != want {
					t.Fatalf("refund %d: suggested %d, want %d", i+1, q.Suggested, want)
				}
				_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(it, 1), ReasonKind: c.kind, Method: "现金", Reason: "x"}, s.staff)
				noErr(t, err)
				checked(t, "refund", o.ID)
				sum += want
			}
			if sum != o.Totals.Total {
				t.Fatalf("refunded %d of %d", sum, o.Totals.Total)
			}
		})
	}
}

// D7a on orders with more on them: whatever the order of the refunds, the
// running total equals one refund of the same dishes on a twin order.
func TestRefundSplitEqualsOnce(t *testing.T) {
	ctx, a := context.Background(), testApp
	type step struct {
		name string
		qty  int
	}
	lines := func(o *app.Order, steps []step) []struct {
		ItemID int64 `json:"item_id"`
		Qty    int   `json:"qty"`
	} {
		n := map[string]int{}
		for _, st := range steps {
			n[st.name] += st.qty
		}
		var out []struct {
			ItemID int64 `json:"item_id"`
			Qty    int   `json:"qty"`
		}
		for name, q := range n {
			out = append(out, refundLines(itemByName(o, name).ID, q)...)
		}
		return out
	}
	for _, c := range []struct {
		name    string
		setup   func(s *store)
		percent int // a percentage off at the counter
		kind    string
	}{
		{"满减 顾客原因", func(s *store) {
			s.promo(t, app.PromotionInput{Name: "m", Kind: "amount_off", Threshold: 20000, Value: 3000})
		}, 0, "customer"},
		{"满减 店铺原因", func(s *store) {
			s.promo(t, app.PromotionInput{Name: "m", Kind: "amount_off", Threshold: 20000, Value: 3000})
		}, 0, "store"},
		{"第二份半价 + 满减 顾客原因", func(s *store) {
			s.promo(t, app.PromotionInput{Name: "half", Kind: "dish_second", Value: 5000, ItemIDs: []int64{s.tea}})
			s.promo(t, app.PromotionInput{Name: "m", Kind: "amount_off", Threshold: 20000, Value: 3000})
		}, 0, "customer"},
		{"打折活动 店铺原因", func(s *store) {
			s.promo(t, app.PromotionInput{Name: "p", Kind: "percent_off", Threshold: 10000, Value: 2000})
		}, 0, "store"},
		{"没有活动", func(s *store) {}, 0, "customer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t, false)
			c.setup(s)
			cart := []app.QuoteLineInput{{ItemID: s.tea, Qty: 3}, {ItemID: s.water, Qty: 2}}
			steps := []step{{"tea", 1}, {"water", 1}, {"tea", 1}, {"water", 1}}
			o := finishPickup(t, s.place(t, "pickup", cart), s.staff)
			twin := finishPickup(t, s.place(t, "pickup", cart), s.staff)
			if o.Totals.Total != twin.Totals.Total {
				t.Fatalf("twins differ: %d %d", o.Totals.Total, twin.Totals.Total)
			}
			var sum int64
			for i, st := range steps {
				q, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, st.name).ID, st.qty),
					ReasonKind: c.kind, Method: "现金", Reason: "x"}, s.staff)
				noErr(t, err)
				checked(t, "refund", o.ID)
				var amt int64
				noErr(t, a.DB.QueryRow(ctx, `SELECT amount FROM order_refunds WHERE id=$1`, q.RefundID).Scan(&amt))
				sum += amt
				once, _, err := a.Refund(ctx, twin.ID, app.RefundInput{Lines: lines(twin, steps[:i+1]), ReasonKind: c.kind, Dry: true}, s.staff)
				noErr(t, err)
				if sum != once.Suggested {
					t.Fatalf("after %d refunds %d, at once %d", i+1, sum, once.Suggested)
				}
			}
		})
	}
}

// Sample S14 (docs/CODE_REVIEW_FIX_PLAN.md H01; brian 2026-11-28): only what
// was received can be refunded. A delivery of one tea is 161.34.
func TestRefundOnlyWhatWasReceived(t *testing.T) {
	ctx, a := context.Background(), testApp
	deliver := func(s *store, o *app.Order, collected int64) *app.Order { return deliverCollect(t, s, o, collected) }
	tea := func(s *store) []app.QuoteLineInput { return []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}} }
	refund := func(s *store, o *app.Order, in app.RefundInput) (*app.RefundQuote, error) {
		in.Lines, in.ReasonKind = refundLines(o.Items[0].ID, 1), "store"
		q, _, err := a.Refund(ctx, o.ID, in, s.staff)
		return q, err
	}
	s := newStore(t, false)

	// nothing collected: nothing to give back
	o := deliver(s, s.place(t, "delivery", tea(s)), 0)
	if o.Totals.Total != 16134 || o.PaymentStatus != "unpaid" {
		t.Fatalf("A %d %s", o.Totals.Total, o.PaymentStatus)
	}
	q, err := refund(s, o, app.RefundInput{Dry: true})
	noErr(t, err)
	if q.Received != 0 || q.Left != 0 || q.Suggested != 0 {
		t.Fatalf("A quote %+v", q)
	}
	_, err = refund(s, o, app.RefundInput{Method: "现金", Reason: "x"})
	wantCode(t, err, "REFUND_NOT_PAID")

	// 100.00 of 161.34 collected: at most 100.00, once
	o = deliver(s, s.place(t, "delivery", tea(s)), 10000)
	q, err = refund(s, o, app.RefundInput{Dry: true})
	noErr(t, err)
	if q.Received != 10000 || q.Suggested != 10000 || q.CashPart != 10000 {
		t.Fatalf("B quote %+v", q)
	}
	_, err = refund(s, o, app.RefundInput{Method: "现金", Reason: "x", Amount: i64(10001)})
	if err == nil {
		t.Fatal("B: more than was collected was refunded")
	}
	_, err = refund(s, o, app.RefundInput{Method: "现金", Reason: "x"})
	noErr(t, err)
	checked(t, "B refunded", o.ID)

	// all of it collected: all of it, as before
	o = deliver(s, s.place(t, "delivery", tea(s)), 16134)
	if o.PaymentStatus != "paid" {
		t.Fatalf("C %s", o.PaymentStatus)
	}
	q, err = refund(s, o, app.RefundInput{Dry: true})
	noErr(t, err)
	if q.Received != 16134 || q.Suggested != 16134 {
		t.Fatalf("C quote %+v", q)
	}

	// more than was due is paid too (N1)
	o = deliver(s, s.place(t, "delivery", tea(s)), 17000)
	if o.PaymentStatus != "paid" {
		t.Fatalf("D %s", o.PaymentStatus)
	}

	// 50.00 from the store balance and 100.00 of the 111.34 due collected:
	// 150.00 was received; all of it back is 100.00 in cash and 50.00 to
	// the balance
	cust, phone := customer(t)
	first, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}}}, false, nil)
	noErr(t, err)
	first = finishPickup(t, first, s.staff)
	_, _, err = a.Refund(ctx, first.ID, app.RefundInput{Lines: refundLines(first.Items[0].ID, 1), Reason: "x", ToCredit: true,
		Amount: i64(5000), Consent: "in_person"}, s.staff)
	noErr(t, err)
	o, err = s.placeAs(t, phone, "delivery", tea(s), true, i64(5000))
	noErr(t, err)
	o = deliver(s, o, 10000)
	if o.PaymentStatus != "unpaid" || creditAt(t, cust, s.branchID) != 0 {
		t.Fatalf("E %s balance %d", o.PaymentStatus, creditAt(t, cust, s.branchID))
	}
	q, err = refund(s, o, app.RefundInput{Dry: true})
	noErr(t, err)
	if q.Received != 15000 || q.Suggested != 15000 || q.CashPart != 10000 || q.CreditPart != 5000 {
		t.Fatalf("E quote %+v", q)
	}
	_, err = refund(s, o, app.RefundInput{Method: "现金", Reason: "x"})
	noErr(t, err)
	checked(t, "E refunded", o.ID)
	if creditAt(t, cust, s.branchID) != 5000 {
		t.Fatalf("E balance %d", creditAt(t, cust, s.branchID))
	}
	walletClean(t)
}

// D11 after a void (docs/CODE_REVIEW_FIX_PLAN.md M03): a platform order's
// charges stay within what it brings in.
func TestPlatformVoidKeepsPayout(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	grab := grabFor(t, s)
	// chicken 203.00 × 2 + water 36.00 = 442.00, all of it the store's
	o, err := a.PlacePlatformOrder(ctx, s.branchID, s.staff, uniq("k"), app.PlatformOrderInput{PlatformID: grab,
		PlatformNo: uniq("GF-"), CustomerName: "Ann",
		Items: []app.StaffLine{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.water, Qty: 1}}})
	noErr(t, err)
	if o.Totals.Total != 44200 {
		t.Fatalf("total %d", o.Totals.Total)
	}
	// Grab's commission typed at 60.00 and an advertising charge of 100.00: 282.00 comes in
	amt := int64(6000)
	_, err = a.SetPlatformCommission(ctx, o.ID, &amt, "合同价", s.staff)
	noErr(t, err)
	o, err = a.AddPlatformFee(ctx, o.ID, 10000, "广告费", s.staff)
	noErr(t, err)
	if _, p := platformFees(t, o); p != 28200 {
		t.Fatalf("payout %d", p)
	}
	checked(t, "charges", o.ID)
	// without the chicken the order is 36.00, less than the 160.00 of charges: refused
	chicken := itemByName(o, "chicken").ID
	_, err = a.VoidLine(ctx, chicken, app.VoidInput{Reason: "x", ReasonKind: "customer"}, s.staff, true)
	if err == nil {
		t.Fatal("a void left the payout below zero")
	}
	after, err := a.GetOrder(ctx, o.ID)
	noErr(t, err)
	if after.Totals.Total != 44200 {
		t.Fatalf("the refused void changed the order: %d", after.Totals.Total)
	}
	checked(t, "refused void", o.ID)
	// the water alone leaves 406.00 − 160.00
	r, err := a.VoidLine(ctx, itemByName(o, "water").ID, app.VoidInput{Reason: "x", ReasonKind: "customer"}, s.staff, true)
	noErr(t, err)
	if _, p := platformFees(t, r.Order); p != 40600-16000 {
		t.Fatalf("payout after the water %d", p)
	}
	checked(t, "void", o.ID)
}

// The audit finds refunds made the old way (docs/CODE_REVIEW_FIX_PLAN.md,
// batch 1): two refunds of 35.28 where 52.92 was right, and a refund of an
// order that took nothing in.
func TestRefundAudit(t *testing.T) {
	ctx, a := context.Background(), testApp
	s := newStore(t, false)
	s.promo(t, app.PromotionInput{Name: "half", Kind: "dish_second", Value: 5000, ItemIDs: []int64{s.water}})
	since := time.Now().Add(-time.Minute)
	old := func(o *app.Order, amount int64) {
		t.Helper()
		lines, _ := json.Marshal([]app.RefundLine{{ItemID: o.Items[0].ID, Name: "water", Qty: 1, Amount: 3000}})
		_, err := a.DB.Exec(ctx, `INSERT INTO order_refunds (order_id, branch_id, lines, amount, method, reason, reason_kind,
			pay_type, cash_part) VALUES ($1,$2,$3,$4,'现金','x','customer','cash',$4)`, o.ID, s.branchID, lines, amount)
		noErr(t, err)
	}
	three := []app.QuoteLineInput{{ItemID: s.water, Qty: 3}}
	split := finishPickup(t, s.place(t, "pickup", three), s.staff)
	old(split, 3528)
	old(split, 3528)
	fine := finishPickup(t, s.place(t, "pickup", three), s.staff)
	for i := 0; i < 2; i++ {
		_, _, err := a.Refund(ctx, fine.ID, app.RefundInput{Lines: refundLines(fine.Items[0].ID, 1), ReasonKind: "customer",
			Method: "现金", Reason: "x"}, s.staff)
		noErr(t, err)
	}
	unpaid := s.place(t, "delivery", three)
	old(unpaid, 3000)
	rows, err := a.RefundAudit(ctx, since)
	noErr(t, err)
	got := map[int64]app.RefundAuditRow{}
	for _, r := range rows {
		got[r.OrderID] = r
	}
	if r := got[split.ID]; r.Why != "split" || r.Refunded != 7056 || r.Should != 5292 {
		t.Fatalf("split: %+v", r)
	}
	if r := got[unpaid.ID]; r.Why != "unpaid" || r.Refunded != 3000 || r.Should != 0 {
		t.Fatalf("unpaid: %+v", r)
	}
	if r, ok := got[fine.ID]; ok {
		t.Fatalf("an order refunded the new way is listed: %+v", r)
	}
	// leave nothing the replay of every order at the end would trip over
	tamper(t, `DELETE FROM order_refunds WHERE order_id = ANY($1)`, []int64{split.ID, unpaid.ID})
}

// deliverCollect takes a one-dish delivery to the door, where the rider
// collects so much.
func deliverCollect(t *testing.T, s *store, o *app.Order, collected int64) *app.Order {
	t.Helper()
	ctx, a := context.Background(), testApp
	o, err := a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	o, err = a.SetItemStatus(ctx, o.ID, o.Items[0].ID, "ready", app.Actor{Type: "system"})
	noErr(t, err)
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	_, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider1)
	noErr(t, err)
	_, err = a.RiderArrive(ctx, o.Delivery.ID, s.rider1)
	noErr(t, err)
	o, err = a.RiderDeliver(ctx, o.Delivery.ID, s.rider1, collected)
	noErr(t, err)
	checked(t, "delivered", o.ID)
	return o
}
