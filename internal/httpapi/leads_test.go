package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

// The company website's form (docs/WEBSITE_DESIGN.md §8): no sign-in, robots
// told all went well, a few sends per address, and the platform sees and
// follows up on it under "Applications".
func TestWebsiteLeadOverHTTP(t *testing.T) {
	if _, err := a.DB.Exec(context.Background(), `DELETE FROM merchant_applications WHERE source='website'`); err != nil {
		t.Fatal(err)
	}
	visitor, admin, owner := newClient(t), adminClient(t), login(t, newFixture(t).owner)
	form := map[string]any{"store_name": "河马茶餐厅", "contact_name": "Lim", "country_code": "SG", "country": "Singapore",
		"phone": "+65 8123 4567", "store_type": "tea", "wants": []string{"pickup"}, "im_kind": "whatsapp",
		"lang": "en", "page": "/en/demo/", "agree": true, "elapsed_ms": 12000}

	visitor.want(422, "VALIDATION", "POST", "/public/leads", map[string]any{"store_name": "x", "elapsed_ms": 12000})
	// a robot: the hidden field filled in, or sent at once; nothing saved
	robot := map[string]any{}
	for k, v := range form {
		robot[k] = v
	}
	robot["website"] = "http://spam.example"
	if out := visitor.want(200, "", "POST", "/public/leads", robot); out["id"] != nil {
		t.Fatalf("robot saved: %v", out)
	}
	robot["website"], robot["elapsed_ms"] = "", 800
	visitor.want(200, "", "POST", "/public/leads", robot)
	out := visitor.want(200, "", "POST", "/public/leads", form)
	id := out["id"]
	if id == nil {
		t.Fatalf("no id: %v", out)
	}
	// sent twice: still one
	if again := visitor.want(200, "", "POST", "/public/leads", form); again["id"] != id {
		t.Fatalf("second send: %v", again)
	}
	// five sends from one address, then one a minute
	visitor.want(429, "RATE_LIMITED", "POST", "/public/leads", form)

	l := admin.want(200, "", "GET", "/admin/applications?source=website", nil)
	items := list(l, "items")
	if len(items) != 1 || l["new"].(float64) != 1 {
		t.Fatalf("admin list: %v", l)
	}
	it := items[0].(map[string]any)
	lead := it["lead"].(map[string]any)
	if it["store_name"] != "河马茶餐厅" || it["customer_id"] != nil || lead["source"] != "website" || lead["im_kind"] != "whatsapp" {
		t.Fatalf("application: %v", it)
	}
	path := fmt.Sprintf("/admin/applications/%v/follow-up", id)
	admin.want(422, "VALIDATION", "POST", path, map[string]string{"follow_up": "x"})
	owner.want(401, "UNAUTHENTICATED", "POST", path, map[string]string{"follow_up": "contacted"})
	admin.want(200, "", "POST", path, map[string]string{"follow_up": "demoed", "note": "已演示"})
	if l = admin.want(200, "", "GET", "/admin/applications?source=website&follow_up=demoed", nil); len(list(l, "items")) != 1 {
		t.Fatalf("follow-up filter: %v", l)
	}
	admin.want(200, "", "POST", fmt.Sprintf("/admin/applications/%v/decide", id), map[string]string{"status": "rejected", "reply": "不在服务范围"})
	if _, err := a.DB.Exec(context.Background(), `DELETE FROM merchant_applications WHERE source='website'`); err != nil {
		t.Fatal(err)
	}
}
