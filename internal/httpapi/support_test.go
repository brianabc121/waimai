package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

// Platform support: customers and stores write to the platform's shared
// inbox, and administrators answer from it.
func TestPlatformSupport(t *testing.T) {
	f := newFixture(t)
	im := &fakeIM{users: map[string]string{}}
	a.IM = im
	t.Cleanup(func() { a.IM = nil })
	cust, other, admin := verifiedCustomer(t), verifiedCustomer(t), adminClient(t)
	owner, cashier, kitchen := login(t, f.owner), login(t, f.cashier), login(t, f.kitchen)
	bid := fmt.Sprint(f.branchID)
	id := func(c *client) int64 { return int64(c.want(200, "", "GET", "/customer/me", nil)["id"].(float64)) }
	custID, otherID := id(cust), id(other)
	cIM, oIM, bIM, pIM := fmt.Sprintf("c_%d", custID), fmt.Sprintf("c_%d", otherID), "b_"+bid, "p_1"
	ownerID := int64(owner.want(200, "", "GET", "/auth/me", nil)["id"].(float64))
	adminID := int64(admin.want(200, "", "GET", "/auth/me", nil)["id"].(float64))
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 400)...)
	orderID, _ := f.order(t, cust)
	refund, menu := uniq("refund"), uniq("menu") // words only this run's messages have

	// the administrators sign in to one shared account
	owner.want(403, "FORBIDDEN", "POST", "/admin/support/login", nil)
	if l := admin.want(200, "", "POST", "/admin/support/login", nil); l["user_id"] != pIM {
		t.Fatalf("platform login: %v", l)
	}

	// the customer's page knows the platform and the customer's orders
	s := cust.want(200, "", "GET", "/customer/support", nil)
	if s["peer"] != pIM || s["enabled"] != true || s["thread_id"].(float64) != 0 || len(list(s, "orders")) != 1 {
		t.Fatalf("customer support: %v", s)
	}
	if o := list(s, "orders")[0].(map[string]any); o["store_name"] != "H" || o["token"] == "" {
		t.Fatalf("customer's order: %v", o)
	}

	// who may write
	if why := say(t, cIM, pIM, "where is my "+refund, 0); why != "" {
		t.Fatalf("customer writes to support: %q", why)
	}
	if why := say(t, bIM, pIM, "please check "+menu, 0); why != "FORBIDDEN" {
		t.Fatalf("a store's message needs a member of the desk: %q", why)
	}
	if why := say(t, bIM, pIM, "please check "+menu, ownerID); why != "" {
		t.Fatalf("store writes to support: %q", why)
	}
	if why := say(t, pIM, cIM, "looking into it", ownerID); why != "FORBIDDEN" {
		t.Fatalf("only an administrator answers for the platform: %q", why)
	}
	if why := say(t, pIM, "c_999999999", "hi", adminID); why != "NOT_FOUND" {
		t.Fatalf("nobody: %q", why)
	}

	// the platform's inbox
	owner.want(403, "FORBIDDEN", "GET", "/admin/support", nil)
	l := admin.want(200, "", "GET", "/admin/support?q="+refund, nil)["list"].(map[string]any)
	rows := list(l, "chats")
	if len(rows) != 1 {
		t.Fatalf("admin support search: %v", l)
	}
	row := rows[0].(map[string]any)
	if row["party"] != cIM || row["kind"] != "customer" || row["unread"].(float64) != 1 || row["name"] != "Ann" || row["resolved_at"] != nil {
		t.Fatalf("admin support row: %v", row)
	}
	if got := list(admin.want(200, "", "GET", "/admin/support?who=store&q="+menu, nil)["list"].(map[string]any), "chats"); len(got) != 1 ||
		got[0].(map[string]any)["name"] != "H" {
		t.Fatalf("stores only: %v", got)
	}
	if n := admin.want(200, "", "GET", "/admin/support/unread", nil)["unread"].(float64); n < 2 {
		t.Fatalf("admin unread: %v", n)
	}
	d := admin.want(200, "", "GET", "/admin/support/"+cIM, nil)
	if d["phone"] == "" || len(list(d, "orders")) != 1 || d["started_at"] == nil {
		t.Fatalf("admin support detail: %v", d)
	}
	admin.want(404, "NOT_FOUND", "GET", "/admin/support/p_1", nil)
	// the platform can write first, to a customer who never wrote
	if d := admin.want(200, "", "GET", "/admin/support/"+oIM, nil); d["id"].(float64) != 0 || d["started_at"] != nil {
		t.Fatalf("no conversation yet: %v", d)
	}
	if why := say(t, pIM, oIM, "about your account", adminID); why != "" {
		t.Fatalf("platform writes first: %q", why)
	}

	// answering and reading
	admin.want(200, "", "POST", "/admin/support/"+cIM+"/read", nil)
	if im.read[len(im.read)-1] != pIM+">"+cIM {
		t.Fatalf("mark read: %v", im.read)
	}
	if why := say(t, pIM, cIM, "refund sent", adminID); why != "" {
		t.Fatalf("platform answers: %q", why)
	}
	if u := cust.want(200, "", "GET", "/customer/unread", nil); u["support"].(float64) != 1 || u["chats"].(float64) != 1 {
		t.Fatalf("customer unread: %v", u)
	}
	if p := cust.want(200, "", "GET", "/customer/me", nil); p["chat_unread"].(float64) != 1 {
		t.Fatalf("profile badge: %v", p)
	}
	cl := cust.want(200, "", "GET", "/customer/chats", nil)
	if sup, ok := cl["support"].(map[string]any); !ok || sup["unread"].(float64) != 1 || sup["last_text"] != "refund sent" || sup["last_sender"] != "store" {
		t.Fatalf("support row in the customer's list: %v", cl)
	}
	if s := cust.want(200, "", "GET", "/customer/support", nil); s["peer_read_at"] == nil || s["unread"].(float64) != 1 {
		t.Fatalf("read by the platform: %v", s)
	}
	cust.want(200, "", "POST", "/customer/support/read", nil)
	if u := cust.want(200, "", "GET", "/customer/unread", nil); u["support"].(float64) != 0 {
		t.Fatalf("after reading: %v", u)
	}
	var jobs int
	if err := a.DB.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE kind='support_alert'`).Scan(&jobs); err != nil || jobs < 1 {
		t.Fatalf("reminder queued: %d %v", jobs, err)
	}

	// the store's side
	kitchen.want(403, "FORBIDDEN", "GET", "/merchant/branches/"+bid+"/support", nil)
	if why := say(t, pIM, bIM, "menu fixed", adminID); why != "" {
		t.Fatalf("platform answers the store: %q", why)
	}
	if sl := cashier.want(200, "", "GET", "/merchant/branches/"+bid+"/chats", nil); sl["support_unread"].(float64) != 1 {
		t.Fatalf("store badge: %v", sl)
	}
	ss := cashier.want(200, "", "GET", "/merchant/branches/"+bid+"/support", nil)
	if ss["peer"] != pIM || ss["unread"].(float64) != 1 || len(list(ss, "orders")) != 1 || list(ss, "orders")[0].(map[string]any)["token"] != nil {
		t.Fatalf("store support: %v", ss)
	}
	cashier.want(200, "", "POST", "/merchant/branches/"+bid+"/support/read", nil)

	// pictures and order cards belong to their conversation
	st, up := raw(t, cust, "/customer/support/images", jpeg)
	if st != 200 {
		t.Fatalf("upload: %d %v", st, up)
	}
	token := up["token"].(string)
	if why := card(t, oIM, pIM, map[string]any{"t": "image", "token": token}, 0); why != "VALIDATION" {
		t.Fatalf("somebody else's picture: %q", why)
	}
	if why := card(t, cIM, pIM, map[string]any{"t": "image", "token": token}, 0); why != "" {
		t.Fatalf("own picture: %q", why)
	}
	if st, _, _ := cust.raw("/chat-images/" + token); st != 200 {
		t.Fatal("the picture is served")
	}
	if why := card(t, oIM, pIM, map[string]any{"t": "order", "order": orderID}, 0); why != "VALIDATION" {
		t.Fatalf("somebody else's order: %q", why)
	}
	if why := card(t, cIM, pIM, map[string]any{"t": "order", "order": orderID}, 0); why != "" {
		t.Fatalf("own order: %q", why)
	}
	if why := card(t, bIM, pIM, map[string]any{"t": "order", "order": orderID}, ownerID); why != "" {
		t.Fatalf("the store's order: %q", why)
	}
	st, up = raw(t, admin, "/admin/support/"+cIM+"/images", jpeg)
	if st != 200 {
		t.Fatalf("platform upload: %d %v", st, up)
	}
	if why := card(t, pIM, cIM, map[string]any{"t": "image", "token": up["token"]}, adminID); why != "" {
		t.Fatalf("platform picture: %q", why)
	}

	// dealt with, and open again with the next question
	admin.want(200, "", "POST", "/admin/support/"+cIM+"/resolve", map[string]any{"resolved": true})
	if d := admin.want(200, "", "GET", "/admin/support/"+cIM, nil); d["resolved_at"] == nil || d["resolved_by"] == "" {
		t.Fatalf("resolved: %v", d)
	}
	if got := list(admin.want(200, "", "GET", "/admin/support?open=1&q="+refund, nil)["list"].(map[string]any), "chats"); len(got) != 0 {
		t.Fatalf("not open any more: %v", got)
	}
	// a muted customer can still reach the platform
	admin.want(200, "", "POST", fmt.Sprintf("/admin/chat-customers/%d/mute", custID), map[string]any{"muted": true})
	if why := say(t, cIM, pIM, "why can't I write to stores?", 0); why != "" {
		t.Fatalf("muted customer to support: %q", why)
	}
	if d := admin.want(200, "", "GET", "/admin/support/"+cIM, nil); d["resolved_at"] != nil || d["muted"] != true {
		t.Fatalf("reopened: %v", d)
	}
	admin.want(404, "NOT_FOUND", "POST", "/admin/support/c_999999999/resolve", map[string]any{"resolved": true})
}
