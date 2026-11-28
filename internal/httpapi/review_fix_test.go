package httpapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// docs/CODE_REVIEW_FIX_PLAN.md M09: someone who works on some brands sees and
// changes only those brands' promotions and claimable coupons.
func TestPromoBrandScopeOverHTTP(t *testing.T) {
	f := newFixture(t)
	b := fmt.Sprint(f.branchID)
	owner := login(t, f.owner)
	code := fmt.Sprintf("PB%05d", time.Now().UnixNano()/1000%100000)
	br := owner.want(200, "", "POST", "/merchant/branches/"+b+"/brands",
		map[string]any{"name": "好吃党", "name_en": "Hao Chi Dang", "code": code, "delivery": true, "mall_visible": true})
	other := br["id"]
	var main any
	for _, x := range owner.want(200, "", "GET", "/merchant/branches/"+b+"/brands", nil)["brands"].([]any) {
		if m := x.(map[string]any); m["is_main"] == true {
			main = m["id"]
		}
	}
	// a manager who works on the new brand only
	name := uniq("m")
	st := owner.want(200, "", "POST", "/merchant/branches/"+b+"/staff",
		map[string]any{"username": name, "display_name": "M", "role": "manager", "password": "password1", "posts": []any{}})
	mid := fmt.Sprint(st["id"])
	owner.want(200, "", "PUT", "/merchant/branches/"+b+"/staff/"+mid+"/brands", map[string]any{"brand_ids": []any{other}})
	mgr := login(t, name)

	promo := func(brands ...any) map[string]any {
		return map[string]any{"name": uniq("p"), "kind": "amount_off", "threshold": 10000, "value": 1000,
			"applies_delivery": true, "applies_pickup": true, "brand_ids": brands}
	}
	mainPromo := fmt.Sprint(owner.want(200, "", "POST", "/merchant/branches/"+b+"/promotions", promo(main))["id"])
	bothPromo := fmt.Sprint(owner.want(200, "", "POST", "/merchant/branches/"+b+"/promotions", promo(main, other))["id"])

	// the manager's own brand: fine
	mine := fmt.Sprint(mgr.want(200, "", "POST", "/merchant/branches/"+b+"/promotions", promo(other))["id"])
	mgr.want(200, "", "POST", "/merchant/branches/"+b+"/promotions/"+mine+"/status", map[string]any{"status": "active"})
	// the list holds what touches their brand, not the main brand's own
	seen := map[string]bool{}
	for _, x := range mgr.want(200, "", "GET", "/merchant/branches/"+b+"/promotions", nil)["promotions"].([]any) {
		seen[fmt.Sprint(x.(map[string]any)["id"])] = true
	}
	if !seen[mine] || !seen[bothPromo] || seen[mainPromo] {
		t.Fatalf("the manager's list: %v", seen)
	}
	if n := len(owner.want(200, "", "GET", "/merchant/branches/"+b+"/promotions", nil)["promotions"].([]any)); n != 3 {
		t.Fatalf("the owner's list: %d", n)
	}
	// another brand's, or one shared with another brand: closed, whichever way
	mgr.want(403, "BRAND_FORBIDDEN", "POST", "/merchant/branches/"+b+"/promotions", promo(main))
	mgr.want(403, "BRAND_FORBIDDEN", "POST", "/merchant/branches/"+b+"/promotions", promo(main, other))
	mgr.want(403, "BRAND_FORBIDDEN", "POST", "/merchant/branches/"+b+"/promotions", promo()) // none = the main brand
	mgr.want(403, "BRAND_FORBIDDEN", "PUT", "/merchant/branches/"+b+"/promotions/"+mainPromo, promo(other))
	mgr.want(403, "BRAND_FORBIDDEN", "PUT", "/merchant/branches/"+b+"/promotions/"+bothPromo, promo(other))
	mgr.want(403, "BRAND_FORBIDDEN", "PUT", "/merchant/branches/"+b+"/promotions/"+mine, promo(main))
	mgr.want(403, "BRAND_FORBIDDEN", "POST", "/merchant/branches/"+b+"/promotions/"+mainPromo+"/status", map[string]any{"status": "active"})
	mgr.want(403, "BRAND_FORBIDDEN", "POST", "/merchant/branches/"+b+"/promotions/"+bothPromo+"/status", map[string]any{"status": "active"})

	// coupons customers claim
	batch := func(brand any) map[string]any {
		return map[string]any{"name": uniq("c"), "kind": "store_free", "value": 500, "valid_days": 7, "per_customer": 1, "brand_id": brand}
	}
	mainBatch := fmt.Sprint(owner.want(200, "", "POST", "/merchant/branches/"+b+"/coupons/batches", batch(main))["id"])
	myBatch := fmt.Sprint(mgr.want(200, "", "POST", "/merchant/branches/"+b+"/coupons/batches", batch(other))["id"])
	mgr.want(403, "BRAND_FORBIDDEN", "POST", "/merchant/branches/"+b+"/coupons/batches", batch(main))
	mgr.want(403, "BRAND_FORBIDDEN", "PUT", "/merchant/branches/"+b+"/coupons/batches/"+mainBatch, batch(other))
	mgr.want(403, "BRAND_FORBIDDEN", "PUT", "/merchant/branches/"+b+"/coupons/batches/"+myBatch, batch(main))
	mgr.want(403, "BRAND_FORBIDDEN", "POST", "/merchant/branches/"+b+"/coupons/batches/"+mainBatch+"/status", map[string]any{"status": "paused"})
	seen = map[string]bool{}
	for _, x := range mgr.want(200, "", "GET", "/merchant/branches/"+b+"/coupons/batches", nil)["batches"].([]any) {
		seen[fmt.Sprint(x.(map[string]any)["id"])] = true
	}
	if !seen[myBatch] || seen[mainBatch] {
		t.Fatalf("the manager's batches: %v", seen)
	}

	// H03: a manager, who may manage staff, still cannot change who works on which brand
	mgr.want(403, "OWNER_ONLY", "PUT", "/merchant/branches/"+b+"/staff/"+mid+"/brands", map[string]any{"brand_ids": []any{}})
	mgr.want(200, "", "PATCH", "/merchant/branches/"+b+"/staff/"+mid, map[string]any{"display_name": "M2"})
	mgr.want(403, "BRAND_FORBIDDEN", "POST", "/merchant/branches/"+b+"/promotions", promo(main))
}

// M08: a manager's approval is not lost on a request that is refused, and
// the same request sent again (its answer was lost) finds what it did
// instead of "the approval was used".
func TestApprovalSurvivesRefusalAndReplay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner, desk := login(t, f.owner), login(t, f.cashier)
	owner.want(200, "", "PUT", "/auth/approve-pin", map[string]string{"password": "password1", "pin": "246810"})
	ownerID := owner.want(200, "", "GET", "/auth/me", nil)["id"]
	cust := verifiedCustomer(t)
	id, _ := f.order(t, cust)
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

	ask := func() string {
		t.Helper()
		return desk.want(200, "", "POST", fmt.Sprintf("/merchant/branches/%d/approvals", f.branchID),
			map[string]any{"approver_id": ownerID, "perm": "wallet.refund", "pin": "246810"})["token"].(string)
	}
	url := fmt.Sprintf("/merchant/orders/%d/refunds", id)
	body := func(amount int) map[string]any {
		return map[string]any{"lines": []map[string]any{{"item_id": o.Items[0].ID, "qty": 1}}, "reason": "凉了",
			"to_credit": true, "consent": "in_person", "amount": amount}
	}
	desk.want(403, "FORBIDDEN", "POST", url, body(1000))
	refunds := func() (n int) {
		t.Helper()
		noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM order_refunds WHERE order_id=$1`, id).Scan(&n))
		return n
	}

	// refused by the action itself (more than the order): the approval is still good
	token := ask()
	desk.want(422, "VALIDATION", "POST", url, body(99999999), "Idempotency-Key", uniq("idem"), "X-Approval", token)
	key := uniq("idem")
	first := desk.want(200, "", "POST", url, body(1000), "Idempotency-Key", key, "X-Approval", token)
	// the answer was lost: the same request again is the same refund
	again := desk.want(200, "", "POST", url, body(1000), "Idempotency-Key", key, "X-Approval", token)
	if first["quote"].(map[string]any)["refund_id"] != again["quote"].(map[string]any)["refund_id"] || refunds() != 1 {
		t.Fatalf("replay: %v / %v, %d refunds", first["quote"], again["quote"], refunds())
	}
	// the same key with something else is not a replay
	desk.want(409, "IDEMPOTENCY_CONFLICT", "POST", url, body(500), "Idempotency-Key", key, "X-Approval", token)
	// and a spent approval pays for nothing new: another key, or no key
	desk.want(403, "APPROVAL_INVALID", "POST", url, body(500), "Idempotency-Key", uniq("idem"), "X-Approval", token)
	desk.want(403, "APPROVAL_INVALID", "POST", url, body(500), "X-Approval", token)
	if refunds() != 1 {
		t.Fatalf("%d refunds", refunds())
	}
}

// H09: the answer to "place order" was lost. Asking the price again and
// ordering again with the same key (the basket did not change) finds the
// first order; the page can also ask whether the order went through.
func TestOrderRetryAfterLostAnswer(t *testing.T) {
	f := newFixture(t)
	cust := verifiedCustomer(t)
	quote := func() any {
		return cust.want(200, "", "POST", "/stores/"+f.slug+"/quotes", map[string]any{"service_type": "delivery",
			"zone_id": f.zoneID, "items": []map[string]any{{"item_id": f.itemID, "qty": 1}}})["id"]
	}
	key := uniq("key")
	placed := "/stores/" + f.slug + "/orders/placed"
	cust.want(404, "NOT_FOUND", "GET", placed, nil, "Idempotency-Key", key)
	first := cust.want(200, "", "POST", "/stores/"+f.slug+"/orders",
		map[string]any{"quote_id": quote(), "customer_name": "Ann", "address_line": "1 Main"}, "Idempotency-Key", key)
	// a new quote for the same basket, the same key: the same order
	again := cust.want(200, "", "POST", "/stores/"+f.slug+"/orders",
		map[string]any{"quote_id": quote(), "customer_name": "Ann", "address_line": "1 Main"}, "Idempotency-Key", key)
	found := cust.want(200, "", "GET", placed, nil, "Idempotency-Key", key)
	if first["order_id"] != again["order_id"] || found["order_id"] != first["order_id"] || found["access_token"] != first["access_token"] {
		t.Fatalf("retry: %v / %v / %v", first, again, found)
	}
	var n int
	noErr(t, a.DB.QueryRow(context.Background(), `SELECT count(*) FROM orders WHERE branch_id=$1`, f.branchID).Scan(&n))
	if n != 1 {
		t.Fatalf("%d orders", n)
	}
	// what the customer typed is part of the request
	cust.want(409, "IDEMPOTENCY_CONFLICT", "POST", "/stores/"+f.slug+"/orders",
		map[string]any{"quote_id": quote(), "customer_name": "Bob", "address_line": "1 Main"}, "Idempotency-Key", key)
	// someone else's key says nothing
	verifiedCustomer(t).want(404, "NOT_FOUND", "GET", placed, nil, "Idempotency-Key", key)
}

// Q1 (docs/PRICING_CHECK.md P2): an order the rider collected too little for
// is settled on the rider cash page, by those who may check rider cash.
func TestSettleShortOverHTTP(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	desk, kitchen := login(t, f.owner), login(t, f.kitchen)
	id, _ := f.order(t, verifiedCustomer(t))
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
	o, err = a.RiderDeliver(ctx, o.Delivery.ID, f.riderID, o.Totals.Total-500)
	noErr(t, err)
	list := desk.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%d/cash-collections", f.branchID), nil)["collections"].([]any)
	row := list[0].(map[string]any)
	if row["short"].(float64) != 500 {
		t.Fatalf("short: %v", row)
	}
	url := fmt.Sprintf("/merchant/cash-collections/%.0f/settle-short", row["id"].(float64))
	kitchen.want(403, "FORBIDDEN", "POST", url, map[string]any{"action": "collected"})
	desk.want(422, "VALIDATION", "POST", url, map[string]any{"action": "waived"})
	desk.want(422, "VALIDATION", "POST", url, map[string]any{"action": "forget"})
	out := desk.want(200, "", "POST", url, map[string]any{"action": "waived", "note": "熟客"})
	if out["short"].(float64) != 0 || out["waived"].(float64) != 500 {
		t.Fatalf("settled: %v", out)
	}
	desk.want(409, "INVALID_STATE", "POST", url, map[string]any{"action": "collected"})
	if got := desk.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil); got["payment_status"] != "paid" {
		t.Fatalf("order: %v", got["payment_status"])
	}
}
