package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

func TestMemberPagesOverHTTP(t *testing.T) {
	f1, f2 := newFixture(t), newFixture(t)
	b1, b2 := fmt.Sprint(f1.branchID), fmt.Sprint(f2.branchID)
	owner1, owner2, cashier, kitchen := login(t, f1.owner), login(t, f2.owner), login(t, f1.cashier), login(t, f1.kitchen)

	m := cashier.want(200, "", "PUT", "/merchant/branches/"+b1+"/members/0",
		map[string]any{"phone": "+639171230001", "source": "wechat", "name": "Ann"})
	no := fmt.Sprint(m["no"])
	owner2.want(200, "", "PUT", "/merchant/branches/"+b2+"/members/0",
		map[string]any{"phone": "+639171230002", "source": "line", "name": "Ben"})

	// a store lists its own members only
	l := owner1.want(200, "", "GET", "/merchant/branches/"+b1+"/member-list?q=ann", nil)
	if l["total"].(float64) != 1 || l["items"].([]any)[0].(map[string]any)["name"] != "Ann" {
		t.Fatalf("store list: %v", l)
	}
	if l := cashier.want(200, "", "GET", "/merchant/branches/"+b1+"/member-list?q=ben", nil); l["total"].(float64) != 0 {
		t.Fatalf("another store's member: %v", l)
	}
	owner2.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/member-list", nil)
	owner2.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/members/"+no+"/orders", nil)
	kitchen.want(403, "FORBIDDEN", "GET", "/merchant/branches/"+b1+"/member-list", nil)
	d := owner1.want(200, "", "GET", "/merchant/branches/"+b1+"/members/"+no, nil)
	if d["stats"] == nil || d["branch_id"].(float64) != float64(f1.branchID) {
		t.Fatalf("detail: %v", d)
	}
	if h := owner1.want(200, "", "GET", "/merchant/branches/"+b1+"/members/"+no+"/orders", nil); h["total"].(float64) != 0 {
		t.Fatalf("history: %v", h)
	}
	owner1.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/members/99999/orders", nil)

	// the platform lists every store's members and narrows to one
	if err := a.EnsurePlatformAdmin(context.Background(), "admin-test", "password1"); err != nil {
		t.Fatal(err)
	}
	admin := adminLogin(t, "admin-test")
	owner1.want(401, "UNAUTHENTICATED", "GET", "/admin/members", nil)
	all := admin.want(200, "", "GET", "/admin/members?q=+6391712300&page_size=100", nil)
	if all["total"].(float64) < 2 {
		t.Fatalf("all members: %v", all)
	}
	one := admin.want(200, "", "GET", "/admin/members?branch_id="+b2, nil)
	if one["total"].(float64) != 1 || one["items"].([]any)[0].(map[string]any)["name"] != "Ben" {
		t.Fatalf("one store: %v", one)
	}
	id := fmt.Sprint(m["id"])
	admin.want(200, "", "GET", "/admin/members/"+id, nil)
	admin.want(200, "", "GET", "/admin/members/"+id+"/orders", nil)
	admin.want(422, "SOURCE_REQUIRED", "PUT", "/admin/members/"+id, map[string]any{"phone": "+639171230001"})
	e := admin.want(200, "", "PUT", "/admin/members/"+id, map[string]any{"phone": "+639171230001", "source": "phone", "name": "Ann L."})
	if e["name"] != "Ann L." || e["source"] != "phone" || fmt.Sprint(e["no"]) != no {
		t.Fatalf("admin edit: %v", e)
	}
	admin.want(404, "NOT_FOUND", "GET", "/admin/members/999999999", nil)
}
