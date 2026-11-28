package httpapi_test

import (
	"fmt"
	"testing"
	"time"
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
