package httpapi_test

import (
	"fmt"
	"testing"
)

func list(out map[string]any, key string) []any {
	l, _ := out[key].([]any)
	return l
}

// The customer's own pages: profile, orders, favourites, messages, feedback,
// applications and the wallet, with the platform answering.
func TestCustomerCenterOverHTTP(t *testing.T) {
	f := newFixture(t)
	anon, cust, other, admin := newClient(t), verifiedCustomer(t), verifiedCustomer(t), adminClient(t)
	owner := login(t, f.owner)

	anon.want(403, "PHONE_NOT_VERIFIED", "GET", "/customer/me", nil)
	if fav := anon.want(200, "", "GET", "/customer/favorites/"+f.slug, nil); fav["favorite"] != false {
		t.Fatalf("anonymous favourite: %v", fav)
	}

	// profile
	me := cust.want(200, "", "GET", "/customer/me", nil)
	id := int64(me["id"].(float64))
	if me["member_no"] != fmt.Sprintf("M-%08d", id) || me["nickname"] != "" || me["unread"].(float64) != 0 {
		t.Fatalf("profile: %v", me)
	}
	cust.want(422, "VALIDATION", "PUT", "/customer/me", map[string]string{"nickname": "一二三四五六七八九十一二三四五六七八九十一"})
	if me = cust.want(200, "", "PUT", "/customer/me", map[string]string{"nickname": "  小  红 "}); me["nickname"] != "小 红" {
		t.Fatalf("nickname: %v", me["nickname"])
	}
	cust.want(404, "NOT_FOUND", "GET", "/customer/avatar", nil)

	// an order's updates land in the message box
	orderID, token := f.order(t, cust)
	owner.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/accept", orderID), map[string]any{})
	msgs := list(cust.want(200, "", "GET", "/customer/messages", nil), "messages")
	if len(msgs) != 1 || msgs[0].(map[string]any)["kind"] != "order" || msgs[0].(map[string]any)["link"] != "/o/"+token ||
		msgs[0].(map[string]any)["read"] != false {
		t.Fatalf("messages: %v", msgs)
	}
	if me = cust.want(200, "", "GET", "/customer/me", nil); me["unread"].(float64) != 1 {
		t.Fatalf("unread: %v", me["unread"])
	}
	cust.want(200, "", "POST", "/customer/messages/read", map[string]any{})
	if me = cust.want(200, "", "GET", "/customer/me", nil); me["unread"].(float64) != 0 {
		t.Fatalf("unread after reading: %v", me["unread"])
	}

	// stores: the one ordered from is known, kept ones are favourites
	stores := list(cust.want(200, "", "GET", "/customer/stores", nil), "stores")
	if len(stores) != 1 || stores[0].(map[string]any)["favorite"] != false {
		t.Fatalf("stores: %v", stores)
	}
	if l := list(cust.want(200, "", "GET", "/customer/stores?favorites=1", nil), "stores"); len(l) != 0 {
		t.Fatalf("favourites: %v", l)
	}
	cust.want(404, "NOT_FOUND", "PUT", "/customer/favorites/no-such-store", map[string]any{})
	cust.want(200, "", "PUT", "/customer/favorites/"+f.slug, map[string]any{})
	cust.want(200, "", "PUT", "/customer/favorites/"+f.slug, map[string]any{})
	if fav := cust.want(200, "", "GET", "/customer/favorites/"+f.slug, nil); fav["favorite"] != true {
		t.Fatalf("favourite: %v", fav)
	}
	if l := list(cust.want(200, "", "GET", "/customer/stores?favorites=1", nil), "stores"); len(l) != 1 {
		t.Fatalf("favourites: %v", l)
	}
	if me = cust.want(200, "", "GET", "/customer/me", nil); me["favorites"].(float64) != 1 {
		t.Fatalf("favourite count: %v", me["favorites"])
	}
	cust.want(200, "", "DELETE", "/customer/favorites/"+f.slug, nil)
	if l := list(cust.want(200, "", "GET", "/customer/stores?favorites=1", nil), "stores"); len(l) != 0 {
		t.Fatalf("favourites after removing: %v", l)
	}

	// offers: the store's running promotion is listed
	if l := list(cust.want(200, "", "GET", "/customer/offers", nil), "offers"); len(l) != 0 {
		t.Fatalf("offers: %v", l)
	}
	p := owner.want(200, "", "POST", fmt.Sprintf("/merchant/branches/%d/promotions", f.branchID), map[string]any{
		"name": "满100减10", "kind": "amount_off", "threshold": 10000, "value": 1000, "applies_delivery": true, "applies_pickup": true})
	owner.want(200, "", "POST", fmt.Sprintf("/merchant/branches/%d/promotions/%v/status", f.branchID, p["id"]), map[string]any{"status": "active"})
	offers := list(cust.want(200, "", "GET", "/customer/offers", nil), "offers")
	if len(offers) != 1 || len(list(offers[0].(map[string]any), "promotions")) != 1 {
		t.Fatalf("offers: %v", offers)
	}

	// feedback and the platform's reply
	cust.want(422, "VALIDATION", "POST", "/customer/feedback", map[string]string{"kind": "x", "content": "hi"})
	cust.want(422, "VALIDATION", "POST", "/customer/feedback", map[string]string{"kind": "problem", "content": " "})
	fb := cust.want(200, "", "POST", "/customer/feedback", map[string]string{"kind": "problem", "content": "地图打不开", "contact": "wx-1"})
	owner.want(401, "UNAUTHENTICATED", "GET", "/admin/feedback", nil)
	all := admin.want(200, "", "GET", "/admin/feedback?status=open&page_size=100", nil)
	found := false
	for _, x := range list(all, "items") {
		if x.(map[string]any)["id"] == fb["id"] {
			found = x.(map[string]any)["phone"] == me["phone"]
		}
	}
	if !found || all["open"].(float64) < 1 {
		t.Fatalf("admin feedback list: %v", all)
	}
	admin.want(422, "VALIDATION", "POST", fmt.Sprintf("/admin/feedback/%v/reply", fb["id"]), map[string]string{"reply": ""})
	admin.want(200, "", "POST", fmt.Sprintf("/admin/feedback/%v/reply", fb["id"]), map[string]string{"reply": "已修好"})
	mine := list(cust.want(200, "", "GET", "/customer/feedback", nil), "feedback")
	if len(mine) != 1 || mine[0].(map[string]any)["reply"] != "已修好" || mine[0].(map[string]any)["phone"] != nil {
		t.Fatalf("my feedback: %v", mine)
	}
	if l := list(other.want(200, "", "GET", "/customer/feedback", nil), "feedback"); len(l) != 0 {
		t.Fatalf("someone else's feedback: %v", l)
	}

	// an application to open a store
	app1 := map[string]any{"store_name": "阿红小馆", "contact_name": "阿红", "phone": "0917 555 0000", "city": "Makati",
		"country_code": "ph", "country": "Philippines", "province": "Metro Manila", "district": "Poblacion",
		"place_name": "Greenbelt 3", "place_address": "Ayala Center, Makati", "unit": "2F 铺位 12", "lat": 14.5528, "lng": 121.0213}
	cust.want(422, "VALIDATION", "POST", "/customer/applications", map[string]string{"store_name": "x"})
	// where the store is: a point on the map, or at least a city typed by hand
	cust.want(422, "VALIDATION", "POST", "/customer/applications", map[string]any{"store_name": "x", "contact_name": "y", "phone": "1"})
	cust.want(422, "VALIDATION", "POST", "/customer/applications", map[string]any{"store_name": "x", "contact_name": "y", "phone": "1", "lat": 14.5})
	cust.want(422, "VALIDATION", "POST", "/customer/applications", map[string]any{"store_name": "x", "contact_name": "y", "phone": "1", "lat": 95, "lng": 121})
	ap := cust.want(200, "", "POST", "/customer/applications", app1)
	cust.want(409, "APPLICATION_PENDING", "POST", "/customer/applications", app1)
	admin.want(422, "VALIDATION", "POST", fmt.Sprintf("/admin/applications/%v/decide", ap["id"]), map[string]string{"status": "rejected"})
	admin.want(200, "", "POST", fmt.Sprintf("/admin/applications/%v/decide", ap["id"]), map[string]string{"status": "approved", "reply": "我们会联系你"})
	admin.want(409, "ALREADY_DECIDED", "POST", fmt.Sprintf("/admin/applications/%v/decide", ap["id"]), map[string]string{"status": "approved"})
	aps := list(cust.want(200, "", "GET", "/customer/applications", nil), "applications")
	if len(aps) != 1 || aps[0].(map[string]any)["status"] != "approved" {
		t.Fatalf("my applications: %v", aps)
	}
	if a := aps[0].(map[string]any); a["country_code"] != "PH" || a["province"] != "Metro Manila" || a["unit"] != "2F 铺位 12" || a["lat"] != 14.5528 || a["lng"] != 121.0213 {
		t.Fatalf("where the store is: %v", a)
	}

	// the wallet is a ledger only the platform writes
	wallet := fmt.Sprintf("/admin/customers/%d/wallet", id)
	admin.want(422, "VALIDATION", "POST", wallet, map[string]any{"account": "balance", "currency": "PHP", "amount": 5000})
	admin.want(200, "", "POST", wallet, map[string]any{"account": "balance", "currency": "php", "amount": 9300, "note": "退款"})
	admin.want(200, "", "POST", wallet, map[string]any{"account": "bonus", "currency": "PHP", "amount": 500, "note": "活动奖励"})
	admin.want(409, "WALLET_SHORT", "POST", wallet, map[string]any{"account": "balance", "currency": "PHP", "amount": -9301, "note": "x"})
	admin.want(200, "", "POST", wallet, map[string]any{"account": "balance", "currency": "PHP", "amount": -300, "note": "更正"})
	owner.want(401, "UNAUTHENTICATED", "POST", wallet, map[string]any{"account": "balance", "currency": "PHP", "amount": 1, "note": "x"})
	me = cust.want(200, "", "GET", "/customer/me", nil)
	w := list(me, "wallets")
	if len(w) != 1 || w[0].(map[string]any)["balance"].(float64) != 9000 || w[0].(map[string]any)["bonus"].(float64) != 500 {
		t.Fatalf("wallets: %v", w)
	}
	if l := list(cust.want(200, "", "GET", "/customer/wallet/balance", nil), "entries"); len(l) != 2 {
		t.Fatalf("balance entries: %v", l)
	}
	cust.want(422, "VALIDATION", "GET", "/customer/wallet/x", nil)
	// feedback reply + application + three wallet changes
	if me["unread"].(float64) != 5 {
		t.Fatalf("unread: %v", me["unread"])
	}
	cs := admin.want(200, "", "GET", "/admin/customers?q="+fmt.Sprintf("M-%08d", id), nil)
	if len(list(cs, "items")) != 1 || list(cs, "items")[0].(map[string]any)["orders"].(float64) != 1 {
		t.Fatalf("admin customers: %v", cs)
	}

	// the texts about the platform
	admin.want(422, "VALIDATION", "PUT", "/admin/platform-info", map[string]string{"nope": "x"})
	admin.want(200, "", "PUT", "/admin/platform-info", map[string]string{"support_phone": "+63 2 8555 0100", "about": "关于"})
	if info := anon.want(200, "", "GET", "/platform/info", nil); info["support_phone"] != "+63 2 8555 0100" || info["about"] != "关于" {
		t.Fatalf("platform info: %v", info)
	}
	admin.want(200, "", "PUT", "/admin/platform-info", map[string]string{"support_phone": "", "about": ""})
}
