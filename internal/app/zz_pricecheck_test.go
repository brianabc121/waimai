package app_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Runs after the other tests of the package (files go in name order) over
// every order they made — placed, by staff, at tables, with add-ons, voids,
// refunds, discounts at checkout, coupons and dish promotions — and checks
// each against docs/PRICING_CHECK.md §2, replay included. A test that adds a
// new way to change an order's money is checked here without more code.
func TestPricingCheckOverAllOrders(t *testing.T) {
	need(t)
	n, broken, err := testApp.CheckAllOrderMoney(context.Background(), time.Now().Add(-30*time.Minute))
	noErr(t, err)
	for id, bad := range broken {
		t.Errorf("order %d:\n  %s", id, strings.Join(bad, "\n  "))
	}
	if n < 50 {
		t.Errorf("only %d orders to check: run the whole package", n)
	}
	t.Logf("%d orders checked, %d broken", n, len(broken))
}

// The store-credit books as a whole (docs/PRICING_CHECK.md §2, W1 and W2).
func TestWalletCheckOverAll(t *testing.T) {
	need(t)
	bad, err := testApp.CheckWallet(context.Background())
	noErr(t, err)
	if len(bad) > 0 {
		t.Errorf("wallet:\n  %s", strings.Join(bad, "\n  "))
	}
}
