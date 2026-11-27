package app

import "testing"

// Hand-worked samples W-S1 and W-S2 (docs/PRICING_CHECK.md §4,
// docs/REFUND_BALANCE_DESIGN.md §11): the numbers below were worked out by
// hand, not copied from the program.
func TestRefundSplitSamples(t *testing.T) {
	cases := []struct {
		name                                         string
		total, credit, refundedBefore, cashBefore, r int64
		cash                                         int64
	}{
		// W-S1: 150.00 = balance 50.00 + cash 100.00; refund 60.00:
		// cash floor(6000 × 10000 / 15000) = 4000, balance 2000
		{"W-S1", 15000, 5000, 0, 0, 6000, 4000},
		// W-S1 then the rest 90.00: cash floor(15000 × 10000 / 15000) − 4000 = 6000
		{"W-S1 rest", 15000, 5000, 6000, 4000, 9000, 6000},
		// W-S2: 299.00 = 50.00 + 249.00; 99.99: floor(9999 × 24900 / 29900) = floor(8326.93) = 8326
		{"W-S2 first", 29900, 5000, 0, 0, 9999, 8326},
		// W-S2 second, 199.01 (all of it): 24900 − 8326 = 16574
		{"W-S2 second", 29900, 5000, 9999, 8326, 19901, 16574},
		// no balance used: all of it is cash
		{"cash only", 12345, 0, 0, 0, 345, 345},
		// paid all from the balance: no cash at all
		{"balance only", 8000, 8000, 0, 0, 8000, 0},
		// 1 cent of 3 (1 balance + 2 cash): floor(1 × 2 / 3) = 0, then
		// floor(2 × 2 / 3) − 0 = 1, then floor(3 × 2 / 3) − 1 = 1
		{"cents 1", 3, 1, 0, 0, 1, 0},
		{"cents 2", 3, 1, 1, 0, 1, 1},
		{"cents 3", 3, 1, 2, 1, 1, 1},
		// a refund of a free order
		{"free", 0, 0, 0, 0, 0, 0},
	}
	for _, c := range cases {
		if got := refundSplit(c.total, c.credit, c.refundedBefore, c.cashBefore, c.r); got != c.cash {
			t.Errorf("%s: cash part %d, want %d", c.name, got, c.cash)
		}
	}
}

// Hand-worked samples SV-S1 to SV-S4 and SV-S7 (docs/STORED_VALUE_DESIGN.md
// §11, docs/PRICING_CHECK.md §4): the numbers were worked out by hand.
func TestStoredTakeSamples(t *testing.T) {
	cases := []struct {
		name                string
		x, principal, bonus int64
		p, b                int64
	}{
		// SV-S1: 1000.00 + 200.00, spend 300.00: bonus floor(30000 × 20000 / 120000) = 5000
		{"SV-S1", 30000, 100000, 20000, 25000, 5000},
		// SV-S2: 1000.00 + 150.00, spend 99.99: floor(149985000 / 115000) = floor(1304.217) = 1304
		{"SV-S2", 9999, 100000, 15000, 8695, 1304},
		// more than it holds: all of it
		{"all", 50000, 30000, 6000, 30000, 6000},
		// principal only, bonus only
		{"principal only", 700, 1000, 0, 700, 0},
		{"bonus only", 700, 0, 1000, 0, 700},
		// 1 cent of 1 principal + 1 bonus: bonus floor(1 × 1 / 2) = 0, the cent is principal
		{"one cent", 1, 1, 1, 1, 0},
		{"nothing", 0, 100, 100, 0, 0},
	}
	for _, c := range cases {
		if p, b := storedTake(c.x, c.principal, c.bonus); p != c.p || b != c.b {
			t.Errorf("%s: %d + %d, want %d + %d", c.name, p, b, c.p, c.b)
		}
	}
	// SV-S7: the order paid 100.00 + 20.00 from stored value and 60.00 goes
	// back: principal floor(6000 × 10000 / 12000) = 5000, bonus 1000
	if p, b := storedGiveBack(6000, 10000, 2000); p != 5000 || b != 1000 {
		t.Errorf("SV-S7: %d + %d, want 5000 + 1000", p, b)
	}
	// 1 cent back of 1 + 1: principal floor(1 × 1 / 2) = 0, the cent is bonus
	if p, b := storedGiveBack(1, 1, 1); p != 0 || b != 1 {
		t.Errorf("one cent back: %d + %d, want 0 + 1", p, b)
	}
}

func TestStoredRefundSplitSamples(t *testing.T) {
	cases := []struct {
		name                                  string
		total                                 int64
		oc                                    orderCredit
		paidBack, cashBack, prinBack, bonBack int64
		r                                     int64
		cash, p, b                            int64
	}{
		// SV-S3: 300.00 all stored value (250.00 + 50.00); 100.00: principal floor(10000 × 25000 / 30000) = 8333
		{"SV-S3 first", 30000, orderCredit{CreditPaid: 30000, StoredPrincipal: 25000, StoredBonus: 5000}, 0, 0, 0, 0, 10000, 0, 8333, 1667},
		// then 200.00: 25000 − 8333 = 16667, bonus 3333
		{"SV-S3 rest", 30000, orderCredit{CreditPaid: 30000, StoredPrincipal: 25000, StoredBonus: 5000}, 10000, 0, 8333, 1667, 20000, 0, 16667, 3333},
		// SV-S4: 500.00 = cash 100.00 + refund balance 100.00 + 250.00 + 50.00; 99.99:
		// cash floor(1999.8) = 1999, principal floor(4999.5) = 4999, refund balance 1999, bonus 1002
		{"SV-S4 first", 50000, orderCredit{CreditPaid: 40000, StoredPrincipal: 25000, StoredBonus: 5000}, 0, 0, 0, 0, 9999, 1999, 4999, 1002},
		// then 400.01: cash 8001, principal 20001, refund balance 8001, bonus 3998
		{"SV-S4 rest", 50000, orderCredit{CreditPaid: 40000, StoredPrincipal: 25000, StoredBonus: 5000}, 9999, 1999, 4999, 1002, 40001, 8001, 20001, 3998},
		// no stored value: nothing to split
		{"no stored", 10000, orderCredit{CreditPaid: 4000}, 0, 0, 0, 0, 5000, 3000, 0, 0},
		// the bonus would take more than it paid: 100.00 = 33.33 cash + 33.33 refund balance + 33.33 principal + 0.01 bonus;
		// 99.99: cash floor(3332.67) = 3332, principal 3332, refund balance 3332, bonus 3 > 1: of the 2 over,
		// 1 fills the refund balance (3333) and 1 the principal (3333); 3332 + 3333 + 3333 + 1 = 9999
		{"bonus capped", 10000, orderCredit{CreditPaid: 6667, StoredPrincipal: 3333, StoredBonus: 1}, 0, 0, 0, 0, 9999, 3332, 3333, 1},
	}
	for _, c := range cases {
		cash := refundSplit(c.total, c.oc.CreditPaid, c.paidBack, c.cashBack, c.r)
		got := storedRefundSplit(c.total, &c.oc, c.paidBack, c.cashBack, c.prinBack, c.bonBack, c.r, c.r-cash)
		if cash != c.cash || got.Principal != c.p || got.Bonus != c.b {
			t.Errorf("%s: cash %d, stored %d + %d; want %d, %d + %d", c.name, cash, got.Principal, got.Bonus, c.cash, c.p, c.b)
		}
	}
}

// SV-S5, SV-S6 (rule W14), worked out by hand.
func TestCashoutPaySamples(t *testing.T) {
	cases := []struct {
		name                    string
		principal, bonus, given int64
		reason                  string
		used, paid              int64
	}{
		// SV-S5: 1,000.00 + 200.00, spent 300.00 (250.00 + 50.00): 750.00 and 150.00 left
		{"SV-S5 customer", 75000, 15000, 20000, "customer", 5000, 70000},
		{"SV-S5 store", 75000, 15000, 20000, "store", 5000, 75000},
		// SV-S6: 100.00 + 100.00, spent 180.00 (90.00 + 90.00): 10.00 and 10.00 left
		{"SV-S6 customer", 1000, 1000, 10000, "customer", 9000, 0},
		{"SV-S6 store", 1000, 1000, 10000, "store", 9000, 1000},
		// nothing spent: all the principal either way
		{"unspent", 50000, 10000, 10000, "customer", 0, 50000},
	}
	for _, c := range cases {
		if used, paid := cashoutPay(c.principal, c.bonus, c.given, c.reason); used != c.used || paid != c.paid {
			t.Errorf("%s: used %d, paid %d; want %d, %d", c.name, used, paid, c.used, c.paid)
		}
	}
}
