package httpapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
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

	// the reconciliation page: 141.80 owed, 30.00 commission, 5.00 taken back → 106.80 expected
	day := func(d int) string { return time.Now().UTC().AddDate(0, 0, d).Format("2006-01-02") }
	rq := "?from=" + day(-1) + "&to=" + day(1)
	rec := owner.want(200, "", "GET", base+"/reconcile"+rq, nil)
	rp := rec["platforms"].([]any)[0].(map[string]any)
	if rp["receivable"].(float64) != 14180 || rp["commission"].(float64) != 3000 || rp["deducted"].(float64) != 500 ||
		rp["payout"].(float64) != 10680 || rp["actual"] != nil {
		t.Fatalf("reconcile platform block: %v", rp)
	}
	if l := owner.want(200, "", "GET", base+"/reconcile/platform-orders"+rq, nil)["orders"].([]any); len(l) != 1 {
		t.Fatalf("platform orders: %v", l)
	}
	owner.want(422, "VALIDATION", "PUT", base+"/reconcile/platform-payouts", map[string]any{"platform_id": grab, "from": day(-1), "to": day(1)})
	owner.want(200, "", "PUT", base+"/reconcile/platform-payouts", map[string]any{"platform_id": grab, "from": day(-1), "to": day(1), "amount": 10000})
	rp = owner.want(200, "", "GET", base+"/reconcile"+rq, nil)["platforms"].([]any)[0].(map[string]any)
	if rp["actual"].(float64) != 10000 || rp["diff"].(float64) != -680 {
		t.Fatalf("actual payout: %v", rp)
	}
	// the Excel file has the platform sheet with the order
	st, _, body := owner.raw(base + "/reconcile" + rq + "&format=xlsx")
	if _, sh := sheets(t, body); st != 200 || len(sh) != 4 || !strings.Contains(sh[3], cart["platform_no"].(string)) ||
		!strings.Contains(sh[0], "营业额按类型") {
		t.Fatalf("reconcile Excel: %d, %d sheets", st, len(sh))
	}
	pid := int64(rp["payouts"].([]any)[0].(map[string]any)["id"].(float64))
	other.want(404, "NOT_FOUND", "DELETE", fmt.Sprintf("/merchant/branches/%d/reconcile/platform-payouts/%d", g.branchID, pid), nil)
	owner.want(200, "", "DELETE", fmt.Sprintf("%s/reconcile/platform-payouts/%d", base, pid), nil)
	owner.want(404, "NOT_FOUND", "DELETE", fmt.Sprintf("%s/reconcile/platform-payouts/%d", base, pid), nil)

	// the owner takes platform orders away from order takers
	owner.want(200, "", "PUT", base+"/role-perms", map[string]any{"cashier": []string{"order.handle"}, "manager": []string{"order.platform"}})
	desk.want(403, "FORBIDDEN", "POST", base+"/platform-orders/quote", cart)
}
