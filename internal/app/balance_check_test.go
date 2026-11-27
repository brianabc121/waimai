package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// The hand-worked samples of docs/BALANCE_CHECK.md §4 (B-S1 to B-S4), run
// on the real database. Every number here was worked out by hand in that
// section; never copy a number from the program's output into this file.

// plainStore is a store with no tax, no service charge and no packaging
// fee, so an order's total is just its dishes: 2 chicken = 338.00,
// tea 69.00, water 30.00.
func plainStore(t *testing.T, s *store) {
	t.Helper()
	_, err := testApp.DB.Exec(context.Background(), `UPDATE branches SET tax_rate_bp=0, service_charge_bp=0, packaging_fee=0 WHERE id=$1`, s.branchID)
	noErr(t, err)
}

// storeBooks is every one of a store's own accounts with its balance.
func storeBooks(t *testing.T, branchID int64) map[string]int64 {
	t.Helper()
	rows, err := testApp.DB.Query(context.Background(), `SELECT kind, balance FROM wallet_accounts
		WHERE branch_id=$1 AND customer_id IS NULL`, branchID)
	noErr(t, err)
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var b int64
		noErr(t, rows.Scan(&k, &b))
		out[k] = b
	}
	noErr(t, rows.Err())
	return out
}

func wantBooks(t *testing.T, step string, got, want map[string]int64) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: %s = %d, hand-worked %d (all: %v)", step, k, got[k], v, got)
		}
	}
	for k, v := range got {
		if _, ok := want[k]; !ok && v != 0 {
			t.Fatalf("%s: %s = %d, hand-worked 0", step, k, v)
		}
	}
}

// B-S1, B-S2: paying stored value back books where the money went (H1).
// Top-up 1,000.00 + 200.00 (which gives two 50.00 coupons), 2 chicken
// 338.00 − coupon 50.00 = 288.00 from stored value:
//
//	bonus = ⌊28800 × 20000 ÷ 120000⌋ = 48.00, principal 240.00
//	left: principal 760.00, bonus 152.00
//
// B-S1 customer's reason: paid out 760.00 − 48.00 = 712.00, kept back 48.00,
// void 152.00. B-S2 store's reason: paid out 760.00, kept back 0, void 152.00.
func TestBalanceSampleCashout(t *testing.T) {
	for _, c := range []struct {
		reason                 string
		paid, clawback, voided int64
	}{{"customer", 71200, 4800, 15200}, {"store", 76000, 0, 15200}} {
		t.Run(c.reason, func(t *testing.T) {
			s := newStoredStore(t)
			plainStore(t, s.store)
			ctx, a := context.Background(), testApp
			cust, phone := customer(t)
			s.topupByCode(t, cust, s.big)
			wantBooks(t, "top-up", storeBooks(t, s.branchID), map[string]int64{"store_topup": -100000, "store_bonus": -20000})
			o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}, true, nil)
			noErr(t, err)
			if o.Totals.Total != 28800 || o.StoredBonusPaid != 4800 || o.StoredPrincipalPaid != 24000 {
				t.Fatalf("spend: total %d, bonus %d, principal %d; hand-worked 28800, 4800, 24000",
					o.Totals.Total, o.StoredBonusPaid, o.StoredPrincipalPaid)
			}
			checked(t, "B-S1 spend", o.ID)
			wantBooks(t, "spend", storeBooks(t, s.branchID), map[string]int64{"store_topup": -100000, "store_bonus": -20000,
				"store_stored_sales": 28800})
			if bal := storedAt(t, cust, s.branchID); bal.Principal != 76000 || bal.Bonus != 15200 {
				t.Fatalf("left %+v, hand-worked 76000 + 15200", bal)
			}
			q, err := a.StoredCashout(ctx, s.branchID, cust, app.CashoutInput{Reason: c.reason, PayType: "cash", Consent: "in_person",
				IdemKey: uniq("bs1")}, s.owner)
			noErr(t, err)
			if q.PaidOut != c.paid || q.Clawback != c.clawback || q.Bonus != c.voided || q.BonusUsed != 4800 {
				t.Fatalf("payback %+v, hand-worked paid %d kept %d void %d", q, c.paid, c.clawback, c.voided)
			}
			want := map[string]int64{"store_topup": -100000, "store_bonus": -20000, "store_stored_sales": 28800,
				"store_cashout": c.paid, "store_bonus_void": c.voided}
			if c.clawback != 0 {
				want["store_clawback"] = c.clawback
			}
			wantBooks(t, "payback", storeBooks(t, s.branchID), want)
			if bal := storedAt(t, cust, s.branchID); bal.Principal != 0 || bal.Bonus != 0 {
				t.Fatalf("left after payback %+v", bal)
			}
			walletClean(t)
		})
	}
}

// B-S3: a refund's balance share reverses balance sales (M2, W16).
//
//	A 338.00 cash, refund one chicken 169.00 into the balance: refund −169.00
//	B 99.00 (tea + water) all from the balance: sales +99.00, balance 70.00
//	refund B's tea 69.00 in money: all of it was balance, so it goes back:
//	  sales −69.00, refund unchanged; balance 139.00
//	C 338.00 = balance 139.00 + cash 199.00; refund one chicken 169.00, all
//	  kept as balance: cash share ⌊16900 × 19900 ÷ 33800⌋ = 99.50, balance
//	  share 69.50: sales −69.50, refund −99.50; balance 169.00
//
// After: refund −268.50, sales 99.00 − 69.00 + 139.00 − 69.50 = 99.50;
// sum −169.00 = minus the customer's 169.00.
func TestBalanceSampleRefundShare(t *testing.T) {
	s := newStore(t, false)
	plainStore(t, s)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)
	owner := ownerOf(t, s.branchID)
	two := []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}

	o, err := s.placeAs(t, phone, "pickup", two, false, nil)
	noErr(t, err)
	if o.Totals.Total != 33800 {
		t.Fatalf("A total %d, hand-worked 33800", o.Totals.Total)
	}
	o = finishPickup(t, o, s.staff)
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "chicken").ID, 1), Reason: "x", Method: "现金",
		ToCredit: true, Consent: "in_person"}, owner)
	noErr(t, err)
	checked(t, "B-S3 A refunded into the balance", o.ID)
	wantBooks(t, "A", storeBooks(t, s.branchID), map[string]int64{"store_refund": -16900})

	b, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}, {ItemID: s.water, Qty: 1}}, true, nil)
	noErr(t, err)
	if b.Totals.Total != 9900 || b.CreditPaid != 9900 {
		t.Fatalf("B total %d paid from balance %d, hand-worked 9900", b.Totals.Total, b.CreditPaid)
	}
	b = finishPickup(t, b, s.staff)
	wantBooks(t, "B", storeBooks(t, s.branchID), map[string]int64{"store_refund": -16900, "store_sales": 9900})
	rq, _, err := a.Refund(ctx, b.ID, app.RefundInput{Lines: refundLines(itemByName(b, "tea").ID, 1), Reason: "x", Method: "现金"}, owner)
	noErr(t, err)
	if rq.CashPart != 0 || rq.CreditPart != 6900 {
		t.Fatalf("B refund %+v, hand-worked cash 0, balance 6900", rq)
	}
	checked(t, "B-S3 B refunded", b.ID)
	wantBooks(t, "B refund", storeBooks(t, s.branchID), map[string]int64{"store_refund": -16900, "store_sales": 3000})
	if c := creditAt(t, cust, s.branchID); c != 13900 {
		t.Fatalf("balance %d, hand-worked 13900", c)
	}

	c, err := s.placeAs(t, phone, "pickup", two, true, nil)
	noErr(t, err)
	if c.CreditPaid != 13900 {
		t.Fatalf("C paid from balance %d, hand-worked 13900", c.CreditPaid)
	}
	c = finishPickup(t, c, s.staff)
	wantBooks(t, "C", storeBooks(t, s.branchID), map[string]int64{"store_refund": -16900, "store_sales": 16900})
	rq, _, err = a.Refund(ctx, c.ID, app.RefundInput{Lines: refundLines(itemByName(c, "chicken").ID, 1), Reason: "x", Method: "现金",
		ToCredit: true, Consent: "in_person"}, owner)
	noErr(t, err)
	if rq.CashPart != 9950 || rq.CreditPart != 6950 {
		t.Fatalf("C refund %+v, hand-worked cash 9950, balance 6950", rq)
	}
	checked(t, "B-S3 C refunded into the balance", c.ID)
	wantBooks(t, "C refund", storeBooks(t, s.branchID), map[string]int64{"store_refund": -26850, "store_sales": 9950})
	if cr := creditAt(t, cust, s.branchID); cr != 16900 {
		t.Fatalf("balance %d, hand-worked 16900", cr)
	}
	walletClean(t)
}

// B-S4: the day's trial balance. B-S1's top-up and 288.00 spend on one day:
//
//	principal    0 + 1,000.00 − 240.00 =    760.00
//	bonus        0 +   200.00 −  48.00 =    152.00
//	topup        0 −             1,000.00 = −1,000.00
//	bonus fund   0 −               200.00 =   −200.00
//	stored sales 0 +   288.00             =    288.00
//	closings: 760.00 + 152.00 − 1,000.00 − 200.00 + 288.00 = 0
func TestBalanceSampleTrialBalance(t *testing.T) {
	s := newStoredStore(t)
	plainStore(t, s.store)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)
	s.topupByCode(t, cust, s.big)
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}, true, nil)
	noErr(t, err)
	checked(t, "B-S4 spend", o.ID)
	var tz string
	noErr(t, a.DB.QueryRow(ctx, `SELECT timezone FROM branches WHERE id=$1`, s.branchID).Scan(&tz))
	loc, err := time.LoadLocation(tz)
	noErr(t, err)
	day := time.Now().In(loc).Format("2006-01-02")
	_, err = a.DB.Exec(ctx, `SELECT wallet_close_day($1, $2::date)`, s.branchID, day)
	noErr(t, err)
	tb, err := a.WalletTrialBalance(ctx, s.branchID, false, day)
	noErr(t, err)
	want := map[string][4]int64{ // opening, credits, debits, closing
		"stored_principal":   {0, 100000, 24000, 76000},
		"stored_bonus":       {0, 20000, 4800, 15200},
		"store_topup":        {0, 0, 100000, -100000},
		"store_bonus":        {0, 0, 20000, -20000},
		"store_stored_sales": {0, 28800, 0, 28800},
	}
	if !tb.Balanced || tb.Unclosed || len(tb.Rows) != len(want) {
		t.Fatalf("trial balance %+v", tb)
	}
	for _, r := range tb.Rows {
		w, ok := want[r.AccountKind]
		if !ok || [4]int64{r.Opening, r.Credits, r.Debits, r.Closing} != w {
			t.Fatalf("%s: %d + %d − %d = %d, hand-worked %v", r.AccountKind, r.Opening, r.Credits, r.Debits, r.Closing, w)
		}
	}
	walletClean(t)
}
