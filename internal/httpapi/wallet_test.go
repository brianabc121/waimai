package httpapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// Store credit over HTTP (docs/REFUND_BALANCE_DESIGN.md §6.4, §8): who may
// refund into a balance, what the customer and the store see, the platform's freeze.
func TestStoreCreditRoutes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner, desk := login(t, f.owner), login(t, f.cashier)
	cust := verifiedCustomer(t)
	id, _ := f.order(t, cust)

	// delivered and paid in cash
	staff := app.Actor{Type: "staff", ID: 1}
	o, err := a.Accept(ctx, id, 0, staff)
	noErr(t, err)
	for _, it := range o.Items {
		o, err = a.SetItemStatus(ctx, id, it.ID, "ready", app.Actor{Type: "system"})
		noErr(t, err)
	}
	o, err = a.Pack(ctx, id, 1, o.ManifestVersion, staff)
	noErr(t, err)
	o, err = a.AssignRider(ctx, id, f.riderID, staff)
	noErr(t, err)
	_, err = a.RiderPickup(ctx, o.Delivery.ID, f.riderID)
	noErr(t, err)
	_, err = a.StaffHandoff(ctx, id, staff)
	noErr(t, err)
	o, err = a.RiderDeliver(ctx, o.Delivery.ID, f.riderID, o.Totals.Total)
	noErr(t, err)

	url := fmt.Sprintf("/merchant/orders/%d/refunds", id)
	body := map[string]any{"lines": []map[string]any{{"item_id": o.Items[0].ID, "qty": 1}}, "reason": "凉了",
		"to_credit": true, "consent": "in_person", "amount": 1000}
	desk.want(403, "FORBIDDEN", "POST", url, body)
	key := uniq("idem")
	r := owner.want(200, "", "POST", url, body, "Idempotency-Key", key)
	again := owner.want(200, "", "POST", url, body, "Idempotency-Key", key)
	if r["quote"].(map[string]any)["refund_id"] != again["quote"].(map[string]any)["refund_id"] {
		t.Fatalf("a retry refunded again: %v / %v", r, again)
	}

	// the customer sees one balance at the store, with its history
	mine := cust.want(200, "", "GET", "/customer/store-credits", nil)["items"].([]any)
	if len(mine) != 1 || mine[0].(map[string]any)["balance"].(float64) != 1000 {
		t.Fatalf("customer balances: %v", mine)
	}
	sc := mine[0].(map[string]any)
	es := cust.want(200, "", "GET", fmt.Sprintf("/customer/store-credits/%d", f.branchID), nil)["items"].([]any)
	if len(es) != 1 || es[0].(map[string]any)["kind"] != "refund" {
		t.Fatalf("customer history: %v", es)
	}

	// the store's page: owners and managers, not order takers
	page := fmt.Sprintf("/merchant/branches/%d/credit", f.branchID)
	desk.want(403, "FORBIDDEN", "GET", page, nil)
	bc := owner.want(200, "", "GET", page, nil)
	if bc["owed"].(float64) != 1000 || len(bc["entries"].([]any)) != 1 {
		t.Fatalf("store page: %v", bc)
	}
	limits := fmt.Sprintf("/merchant/branches/%d/credit-limits", f.branchID)
	desk.want(403, "OWNER_ONLY", "PUT", limits, map[string]any{"approve_over": 1, "daily_limit": 1})
	owner.want(200, "", "PUT", limits, map[string]any{"approve_over": 50000, "daily_limit": 100000})

	// the platform freezes it; the store cannot be deleted while it owes it
	admin := adminClient(t)
	admin.want(200, "", "POST", fmt.Sprintf("/admin/store-credits/%d/freeze", int64(sc["account_id"].(float64))),
		map[string]any{"frozen": true, "note": "争议"})
	l := admin.want(200, "", "GET", fmt.Sprintf("/admin/customers/%d/store-credits", customerID(t, cust)), nil)["items"].([]any)
	if len(l) != 1 || l[0].(map[string]any)["frozen"] != true {
		t.Fatalf("admin balances: %v", l)
	}
	admin.want(200, "", "GET", "/admin/wallet-check", nil)
	owner.want(200, "", "POST", fmt.Sprintf("/merchant/refunds/%v/revoke-credit", r["quote"].(map[string]any)["refund_id"]), map[string]any{})

	// the ledger: the store sees its own movements with phones masked, the
	// platform everything (docs/BALANCE_CHECK.md §5)
	ledger := fmt.Sprintf("/merchant/branches/%d/wallet-ledger", f.branchID)
	desk.want(403, "FORBIDDEN", "GET", ledger, nil)
	lg := owner.want(200, "", "GET", ledger, nil)
	rows := lg["rows"].([]any)
	if len(rows) != 4 { // refund into the balance (2 postings) and its revoke (2)
		t.Fatalf("store ledger: %v", rows)
	}
	for _, x := range rows {
		row := x.(map[string]any)
		if p := row["phone"].(string); p != "" && !strings.Contains(p, "•") {
			t.Fatalf("store ledger shows a phone: %v", row)
		}
		if len(row["counter"].([]any)) == 0 {
			t.Fatalf("no other side: %v", row)
		}
	}
	al := admin.want(200, "", "GET", fmt.Sprintf("/admin/wallet-ledger?branch=%d&kind=revoke", f.branchID), nil)
	if len(al["rows"].([]any)) != 2 || al["check"] == nil {
		t.Fatalf("platform ledger: %v", al)
	}
	owner.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%d/wallet-days", f.branchID), nil)
	admin.want(200, "", "GET", "/admin/wallet-days?all=1", nil)
	admin.want(422, "VALIDATION", "GET", "/admin/wallet-ledger?from=2026-13-01&to=2026-01-01", nil)
}

func customerID(t *testing.T, c *client) int64 {
	t.Helper()
	return int64(c.want(200, "", "GET", "/customer/me", nil)["id"].(float64))
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
