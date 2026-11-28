package httpapi_test

import (
	"fmt"
	"testing"
	"time"
)

// Brands over HTTP (docs/MULTI_BRAND_DESIGN.md): only the owner manages
// them, and staff limited to some brands cannot work on the others.
func TestBrandsOverHTTP(t *testing.T) {
	f := newFixture(t)
	b := fmt.Sprint(f.branchID)
	owner, desk := login(t, f.owner), login(t, f.cashier)
	code := fmt.Sprintf("HT%05d", time.Now().UnixNano()/1000%100000)

	desk.want(403, "FORBIDDEN", "POST", "/merchant/branches/"+b+"/brands", map[string]any{"name": "X", "code": "XX", "delivery": true})
	br := owner.want(200, "", "POST", "/merchant/branches/"+b+"/brands",
		map[string]any{"name": "好吃党", "name_en": "Hao Chi Dang", "code": code, "delivery": true, "mall_visible": true})
	bid := fmt.Sprint(br["id"])
	owner.want(409, "BRAND_CODE_TAKEN", "POST", "/merchant/branches/"+b+"/brands", map[string]any{"name": "Y", "code": code, "delivery": true})
	if s := owner.want(200, "", "GET", "/merchant/branches/"+b+"/brand-code?name_en=Hao+Chi+Dang", nil); s["code"] == "" || s["code"] == code {
		t.Fatalf("suggested code: %v", s)
	}
	l := owner.want(200, "", "GET", "/merchant/branches/"+b+"/brands", nil)
	if len(l["brands"].([]any)) != 2 {
		t.Fatalf("brands: %v", l)
	}
	// Everyone sees the brands they work on with their role.
	me := desk.want(200, "", "GET", "/auth/me", nil)
	if n := len(me["roles"].([]any)[0].(map[string]any)["brands"].([]any)); n != 2 {
		t.Fatalf("cashier's brands: %d", n)
	}

	// The cashier is limited to the new brand: the main brand's members are closed to them.
	var deskID string
	for _, s := range owner.want(200, "", "GET", "/merchant/branches/"+b+"/staff", nil)["staff"].([]any) {
		if m := s.(map[string]any); m["username"] == f.cashier {
			deskID = fmt.Sprint(m["id"])
		}
	}
	owner.want(200, "", "PUT", "/merchant/branches/"+b+"/staff/"+deskID+"/brands", map[string]any{"brand_ids": []any{br["id"]}})
	desk.want(403, "BRAND_FORBIDDEN", "GET", "/merchant/branches/"+b+"/member-list", nil)
	desk.want(200, "", "GET", "/merchant/branches/"+b+"/member-list?brand="+bid, nil)
	desk.want(403, "BRAND_FORBIDDEN", "POST", "/merchant/branches/"+b+"/staff-quotes",
		map[string]any{"service_type": "pickup", "items": []any{map[string]any{"item_id": f.itemID, "qty": 1}}})
	m := desk.want(200, "", "PUT", "/merchant/branches/"+b+"/members/0?brand="+bid,
		map[string]any{"phone": "+639171239999", "source": "wechat"})
	if fmt.Sprint(m["brand_id"]) != bid {
		t.Fatalf("member brand: %v", m)
	}
	// docs/CODE_REVIEW_FIX_PLAN.md H03: only the owner sets who works on which brand; nobody lifts their own limit
	desk.want(403, "OWNER_ONLY", "PUT", "/merchant/branches/"+b+"/staff/"+deskID+"/brands", map[string]any{"brand_ids": []any{}})
	desk.want(403, "BRAND_FORBIDDEN", "GET", "/merchant/branches/"+b+"/member-list", nil)
	// Back to every brand.
	owner.want(200, "", "PUT", "/merchant/branches/"+b+"/staff/"+deskID+"/brands", map[string]any{"brand_ids": []any{}})
	desk.want(200, "", "GET", "/merchant/branches/"+b+"/member-list", nil)
}
