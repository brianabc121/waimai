package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

// Platform orders over HTTP (docs/ORDER_TYPES_DESIGN.md §7): who may enter
// them, the quote, one order per platform number a day, and what staff record
// afterwards.
func TestPlatformOrderOverHTTP(t *testing.T) {
	f := newFixture(t)
	g := newFixture(t)
	var grab int64
	if err := a.DB.QueryRow(context.Background(), `SELECT id FROM delivery_platforms WHERE prefix='Grab'`).Scan(&grab); err != nil {
		t.Fatal(err)
	}
	owner, desk, other := login(t, f.owner), login(t, f.cashier), login(t, g.owner)
	base := fmt.Sprintf("/merchant/branches/%d", f.branchID)
	cart := map[string]any{"platform_id": grab, "platform_no": uniq("GF-"), "items": []map[string]any{{"item_id": f.itemID, "qty": 2}},
		"merchant_promo": 1000}
	// the store is not on Grab yet
	desk.want(422, "PLATFORM_OFF", "POST", base+"/platform-orders/quote", cart)
	if r := desk.want(200, "", "GET", "/auth/me", nil)["roles"].([]any)[0].(map[string]any); r["platforms"] != false {
		t.Fatalf("role before: %v", r)
	}
	owner.want(200, "", "PUT", base+"/platforms", map[string]any{"platforms": []map[string]any{{"platform_id": grab,
		"enabled": true, "own_markup_bp": 1000, "own_commission_bp": 2500, "round_step": 0}}})
	// the 平台下单 menu shows only once the store is on a platform
	if r := desk.want(200, "", "GET", "/auth/me", nil)["roles"].([]any)[0].(map[string]any); r["platforms"] != true {
		t.Fatalf("role: %v", r)
	}
	// the order taker sees the platforms the store is on, not the ones it is not
	pl := desk.want(200, "", "GET", base+"/platform-orders/platforms", nil)["platforms"].([]any)
	if len(pl) != 1 || pl[0].(map[string]any)["prefix"] != "Grab" {
		t.Fatalf("platforms for the order page: %v", pl)
	}
	// tea 69.00 + 10% = 75.90, two of them 151.80, less 10.00: 141.80; 25% of that 35.45
	q := desk.want(200, "", "POST", base+"/platform-orders/quote", cart)
	tt := q["totals"].(map[string]any)
	if tt["subtotal"].(float64) != 15180 || tt["total"].(float64) != 14180 || q["commission"].(float64) != 3545 ||
		q["payout"].(float64) != 10635 {
		t.Fatalf("quote: %v commission %v payout %v", tt, q["commission"], q["payout"])
	}
	other.want(404, "NOT_FOUND", "POST", base+"/platform-orders/quote", cart)
	desk.want(422, "VALIDATION", "POST", base+"/platform-orders", map[string]any{"platform_id": grab,
		"items": cart["items"]}, "Idempotency-Key", uniq("k"))
	key := uniq("k")
	out := desk.want(200, "", "POST", base+"/platform-orders", cart, "Idempotency-Key", key)
	o := out["order"].(map[string]any)
	again := desk.want(200, "", "POST", base+"/platform-orders", cart, "Idempotency-Key", key)
	if again["order"].(map[string]any)["id"] != o["id"] {
		t.Fatal("a double press placed two orders")
	}
	if o["order_type"] != "platform" || o["public_no"] != "Grab "+cart["platform_no"].(string) || o["acceptance_status"] != "accepted" {
		t.Fatalf("placed: %v %v %v", o["order_type"], o["public_no"], o["acceptance_status"])
	}
	desk.want(409, "DUPLICATE", "POST", base+"/platform-orders", cart, "Idempotency-Key", uniq("k"))
	id := int64(o["id"].(float64))
	ou := fmt.Sprintf("/merchant/orders/%d", id)
	got := desk.want(200, "", "GET", ou, nil)
	if p := got["platform"].(map[string]any); p["commission"].(float64) != 3545 || p["prefix"] != "Grab" {
		t.Fatalf("platform block: %v", p)
	}
	desk.want(422, "VALIDATION", "PUT", ou+"/platform-commission", map[string]any{"amount": 3000})
	got = desk.want(200, "", "PUT", ou+"/platform-commission", map[string]any{"amount": 3000, "reason": "合同"})
	if got["platform"].(map[string]any)["commission"].(float64) != 3000 {
		t.Fatalf("typed commission: %v", got["platform"])
	}
	got = desk.want(200, "", "POST", ou+"/platform-deductions", map[string]any{"amount": 500, "reason": "漏菜"})
	if got["platform"].(map[string]any)["deducted"].(float64) != 500 || got["refunded"].(float64) != 500 {
		t.Fatalf("deduction: %v", got["platform"])
	}
	other.want(404, "NOT_FOUND", "POST", ou+"/platform-deductions", map[string]any{"amount": 1, "reason": "x"})

	// the owner takes platform orders away from order takers
	owner.want(200, "", "PUT", base+"/role-perms", map[string]any{"cashier": []string{"order.handle"}, "manager": []string{"order.platform"}})
	desk.want(403, "FORBIDDEN", "POST", base+"/platform-orders/quote", cart)
}
