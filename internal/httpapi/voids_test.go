package httpapi_test

import (
	"fmt"
	"testing"
)

// Voids over HTTP (docs/VOID_ITEM_DESIGN.md §9): an order taker voids a dish
// not started; one the kitchen started needs a manager's code; the kitchen
// acknowledges; the void log is the manager's.
func TestVoidOverHTTP(t *testing.T) {
	f := newFixture(t)
	bid := f.branchID
	owner := login(t, f.owner)
	owner.want(200, "", "PUT", "/auth/approve-pin", map[string]string{"password": "password1", "pin": "246810"})
	ownerID := owner.want(200, "", "GET", "/auth/me", nil)["id"]
	desk := login(t, f.cashier)

	// three of the same dish on one line
	cust := verifiedCustomer(t)
	qt := cust.want(200, "", "POST", "/stores/"+f.slug+"/quotes", map[string]any{"service_type": "delivery",
		"zone_id": f.zoneID, "items": []map[string]any{{"item_id": f.itemID, "qty": 3}}})
	out := cust.want(200, "", "POST", "/stores/"+f.slug+"/orders", map[string]any{"quote_id": qt["id"],
		"customer_name": "Ann", "address_line": "1 Main"}, "Idempotency-Key", uniq("key"))
	id := int64(out["order_id"].(float64))
	o := desk.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil)
	o = desk.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/accept", id), map[string]any{"expected_version": o["version"]})
	if o["void_open"] != true {
		t.Fatalf("void_open: %v", o["void_open"])
	}
	line := fmt.Sprint(int64(o["items"].([]any)[0].(map[string]any)["id"].(float64)))

	// one not started: the desk alone
	q := desk.want(200, "", "POST", "/merchant/order-items/"+line+"/void-quote", map[string]any{"qty": 1})
	if q["needs_approval"] != false || q["wasted"] != false {
		t.Fatalf("quote: %v", q)
	}
	desk.want(422, "VALIDATION", "POST", "/merchant/order-items/"+line+"/void", map[string]any{"qty": 1})
	desk.want(200, "", "POST", "/merchant/order-items/"+line+"/void", map[string]any{"qty": 1, "reason": "点错了", "reason_kind": "customer"})

	// the kitchen starts the rest: the desk needs a manager for it
	owner.want(200, "", "POST", "/kitchen/items/"+line+"/start", map[string]any{})
	q = desk.want(200, "", "POST", "/merchant/order-items/"+line+"/void-quote", map[string]any{"qty": 1})
	if q["needs_approval"] != true || q["wasted"] != true {
		t.Fatalf("quote: %v", q)
	}
	desk.want(409, "LAST_DISH", "POST", "/merchant/order-items/"+line+"/void-quote", map[string]any{"qty": 2})
	desk.want(403, "VOID_NEEDS_APPROVAL", "POST", "/merchant/order-items/"+line+"/void", map[string]any{"qty": 1, "reason": "上太慢"})
	ok := desk.want(200, "", "POST", fmt.Sprintf("/merchant/branches/%d/approvals", bid),
		map[string]any{"approver_id": ownerID, "perm": "order.void", "pin": "246810"})
	r := desk.want(200, "", "POST", "/merchant/order-items/"+line+"/void", map[string]any{"qty": 1, "reason": "上太慢"},
		"X-Approval", ok["token"].(string))
	voids := r["order"].(map[string]any)["voids"].([]any)
	if len(voids) != 2 {
		t.Fatalf("voids: %v", voids)
	}
	v := voids[1].(map[string]any)
	if v["approved_by"] == nil || v["needs_ack"] != true {
		t.Fatalf("void: %v", v)
	}
	// the kitchen sees it and says so; the customer's page hides who did it
	owner.want(200, "", "POST", fmt.Sprintf("/kitchen/voids/%d/ack", int64(v["id"].(float64))), map[string]any{})
	co := cust.want(200, "", "GET", "/customer/orders/"+out["access_token"].(string), nil)
	cv := co["voids"].([]any)[1].(map[string]any)
	if cv["approved_by"] != nil || cv["wasted"] == true || co["void_open"] == true {
		t.Fatalf("customer view: %v", cv)
	}

	// the log is the managers' and the owner's
	desk.want(403, "FORBIDDEN", "GET", fmt.Sprintf("/merchant/branches/%d/voids", bid), nil)
	log := owner.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%d/voids", bid), nil)
	if log["voids"].(float64) != 2 || log["wasted_amount"].(float64) <= 0 {
		t.Fatalf("log: %v", log)
	}
}
