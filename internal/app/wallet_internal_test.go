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
