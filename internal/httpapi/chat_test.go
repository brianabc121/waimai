package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// fakeIM stands in for the OpenIM server: it remembers the accounts made.
type fakeIM struct {
	users map[string]string
	read  []string
	sent  []string
}

func (f *fakeIM) EnsureUser(_ context.Context, id, nick string) error { f.users[id] = nick; return nil }
func (f *fakeIM) UserToken(_ context.Context, id string) (string, error) {
	return "tok-" + id, nil
}
func (f *fakeIM) MarkRead(_ context.Context, id, peer string) error {
	f.read = append(f.read, id+">"+peer)
	return nil
}
func (f *fakeIM) SendText(_ context.Context, from, to, text, ex string) error {
	f.sent = append(f.sent, from+">"+to+":"+ex)
	return nil
}
func (f *fakeIM) Check(context.Context) error { return nil }

// hook posts what OpenIM would call back with and returns its answer.
func hook(t *testing.T, key, command string, body map[string]any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(server.URL+"/im-hooks/"+key+"/"+command, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

var msgSeq int

// say plays one message through both callbacks, as OpenIM does, and returns
// the reason it was refused ("" when it went out).
func say(t *testing.T, from, to, text string, uid int64) string {
	t.Helper()
	msgSeq++
	content, _ := json.Marshal(map[string]string{"content": text})
	body := map[string]any{"sendID": from, "recvID": to, "contentType": 101, "sessionType": 1, "content": string(content),
		"serverMsgID": fmt.Sprintf("m-%d-%d", time.Now().UnixNano(), msgSeq), "sendTime": time.Now().UnixMilli()}
	if uid != 0 {
		body["ex"] = fmt.Sprintf(`{"uid":%d}`, uid)
	}
	st, out := hook(t, "hook-key", "callbackBeforeSendSingleMsgCommand", body)
	if st != 200 {
		t.Fatalf("before hook: %d %v", st, out)
	}
	if out["nextCode"].(float64) != 0 {
		return out["errMsg"].(string)
	}
	if st, out := hook(t, "hook-key", "callbackAfterSendSingleMsgCommand", body); st != 200 {
		t.Fatalf("after hook: %d %v", st, out)
	}
	// a repeated callback is stored once
	hook(t, "hook-key", "callbackAfterSendSingleMsgCommand", body)
	return ""
}

func TestChatOverHTTP(t *testing.T) {
	f := newFixture(t)
	im := &fakeIM{users: map[string]string{}}
	a.IM = im
	t.Cleanup(func() { a.IM = nil })
	anon, cust, other, admin := newClient(t), verifiedCustomer(t), verifiedCustomer(t), adminClient(t)
	owner, cashier, kitchen := login(t, f.owner), login(t, f.cashier), login(t, f.kitchen)
	bid := fmt.Sprint(f.branchID)
	id := func(c *client) int64 { return int64(c.want(200, "", "GET", "/customer/me", nil)["id"].(float64)) }
	custID, otherID := id(cust), id(other)
	cIM, oIM, bIM := fmt.Sprintf("c_%d", custID), fmt.Sprintf("c_%d", otherID), "b_"+bid
	var ownerID int64
	if err := a.DB.QueryRow(context.Background(), `SELECT id FROM users WHERE username=$1`, f.owner).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}

	// signing in to the chat server
	if cfg := anon.want(200, "", "GET", "/config", nil); cfg["chat"] != true {
		t.Fatalf("config: %v", cfg)
	}
	anon.want(403, "PHONE_NOT_VERIFIED", "POST", "/customer/chats/login", nil)
	if l := cust.want(200, "", "POST", "/customer/chats/login", nil); l["user_id"] != cIM || l["token"] != "tok-"+cIM {
		t.Fatalf("customer login: %v", l)
	}
	if l := cashier.want(200, "", "POST", "/merchant/branches/"+bid+"/chats/login", nil); l["user_id"] != bIM || im.users[bIM] != "H" {
		t.Fatalf("store login: %v %v", l, im.users)
	}
	kitchen.want(403, "FORBIDDEN", "POST", "/merchant/branches/"+bid+"/chats/login", nil)
	if im.users[cIM] != "customer" {
		t.Fatalf("a customer's account carries no name of the customer's: %q", im.users[cIM])
	}

	// the callbacks only answer to the key
	if st, _ := hook(t, "wrong", "callbackBeforeSendSingleMsgCommand", map[string]any{}); st != 404 {
		t.Fatalf("wrong key: %d", st)
	}
	if _, out := hook(t, "hook-key", "callbackBeforeCreateGroupCommand", map[string]any{}); out["nextCode"].(float64) != 1 {
		t.Fatalf("groups are refused: %v", out)
	}

	// who may write to whom
	if why := say(t, cIM, oIM, "hi", 0); why != "VALIDATION" {
		t.Fatalf("customer to customer: %q", why)
	}
	if why := say(t, bIM, cIM, "hello", ownerID); why != "CHAT_NO_ORDER" {
		t.Fatalf("a store writes first only after an order: %q", why)
	}
	if why := say(t, cIM, bIM, "  ", 0); why != "VALIDATION" {
		t.Fatalf("empty message: %q", why)
	}
	if why := say(t, cIM, bIM, "can I change my order?", 0); why != "" {
		t.Fatalf("customer writes: %q", why)
	}
	if why := say(t, bIM, cIM, "sure", 0); why != "FORBIDDEN" {
		t.Fatalf("the store's answer needs a member of the desk: %q", why)
	}
	if why := say(t, bIM, cIM, "sure", f.riderID); why != "FORBIDDEN" {
		t.Fatalf("a rider cannot answer: %q", why)
	}

	// the customer's list and badge
	chat := cust.want(200, "", "GET", "/customer/chats/"+f.slug, nil)
	if chat["peer"] != bIM || chat["enabled"] != true || chat["muted"] != false || chat["thread_id"].(float64) == 0 {
		t.Fatalf("customer chat: %v", chat)
	}
	if u := cust.want(200, "", "GET", "/customer/unread", nil); u["chats"].(float64) != 0 {
		t.Fatalf("own message is not unread: %v", u)
	}
	if u := anon.want(200, "", "GET", "/customer/unread", nil); u["total"].(float64) != 0 {
		t.Fatalf("anonymous unread: %v", u)
	}

	// the store's list
	kitchen.want(403, "FORBIDDEN", "GET", "/merchant/branches/"+bid+"/chats", nil)
	sl := owner.want(200, "", "GET", "/merchant/branches/"+bid+"/chats", nil)
	rows := list(sl, "chats")
	if len(rows) != 1 || sl["unread"].(float64) != 1 {
		t.Fatalf("store chats: %v", sl)
	}
	row := rows[0].(map[string]any)
	if row["peer"] != cIM || row["last_text"] != "can I change my order?" || row["unread"].(float64) != 1 || row["order"] != nil {
		t.Fatalf("store chat row: %v", row)
	}
	if len(list(owner.want(200, "", "GET", "/merchant/branches/"+bid+"/chats?active=1", nil), "chats")) != 0 {
		t.Fatal("no open order yet")
	}

	// an order shows beside the conversation, and lets the store reach the other customer
	f.order(t, cust)
	f.order(t, other)
	sc := owner.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%s/chats/%d", bid, custID), nil)
	if sc["customer"] != "Ann" || sc["order"] == nil || len(list(sc, "orders")) != 1 || sc["order_count"].(float64) != 1 {
		t.Fatalf("store chat: %v", sc)
	}
	if len(list(owner.want(200, "", "GET", "/merchant/branches/"+bid+"/chats?active=1&q=ann", nil), "chats")) != 1 {
		t.Fatal("search by name with an open order")
	}
	owner.want(200, "", "POST", fmt.Sprintf("/merchant/branches/%s/chats/%d/read", bid, custID), nil)
	if len(im.read) != 1 || im.read[0] != bIM+">"+cIM {
		t.Fatalf("the chat server was not told the store read the conversation: %v", im.read)
	}
	if sl := owner.want(200, "", "GET", "/merchant/branches/"+bid+"/chats?unread=1", nil); len(list(sl, "chats")) != 0 || sl["unread"].(float64) != 0 {
		t.Fatalf("read: %v", sl)
	}
	if chat := cust.want(200, "", "GET", "/customer/chats/"+f.slug, nil); chat["peer_read_at"] == nil {
		t.Fatalf("the customer sees it was read: %v", chat)
	}
	if why := say(t, bIM, cIM, "sure, done", ownerID); why != "" {
		t.Fatalf("store answers: %q", why)
	}
	if why := say(t, bIM, oIM, "your rider is late", ownerID); why != "" {
		t.Fatalf("store writes first after an order: %q", why)
	}
	if u := cust.want(200, "", "GET", "/customer/unread", nil); u["chats"].(float64) != 1 || u["total"].(float64) < 1 {
		t.Fatalf("customer unread: %v", u)
	}
	cl := list(cust.want(200, "", "GET", "/customer/chats", nil), "chats")
	if len(cl) != 1 || cl[0].(map[string]any)["slug"] != f.slug || cl[0].(map[string]any)["unread"].(float64) != 1 || cl[0].(map[string]any)["count"].(float64) != 2 {
		t.Fatalf("customer chats: %v", cl)
	}
	cust.want(200, "", "POST", "/customer/chats/"+f.slug+"/read", nil)
	if u := cust.want(200, "", "GET", "/customer/unread", nil); u["chats"].(float64) != 0 {
		t.Fatalf("after reading: %v", u)
	}
	// another store's staff cannot see it
	g := newFixture(t)
	login(t, g.owner).want(404, "NOT_FOUND", "GET", fmt.Sprintf("/merchant/branches/%s/chats/%d", bid, custID), nil)
	login(t, g.owner).want(404, "NOT_FOUND", "GET", fmt.Sprintf("/merchant/branches/%d/chats/%d", g.branchID, custID), nil)

	// the platform sees every conversation, and looking is written down
	owner.want(403, "FORBIDDEN", "GET", "/admin/chats", nil)
	al := admin.want(200, "", "GET", "/admin/chats?branch_id="+bid, nil)
	if al["total"].(float64) != 2 || len(list(al, "chats")) != 2 {
		t.Fatalf("admin chats: %v", al)
	}
	if got := list(admin.want(200, "", "GET", "/admin/chats?q=rider+is+late&branch_id="+bid, nil), "chats"); len(got) != 1 {
		t.Fatalf("search in messages: %v", got)
	}
	threadID := int64(chat["thread_id"].(float64))
	ac := admin.want(200, "", "GET", fmt.Sprintf("/admin/chats/%d", threadID), nil)
	msgs := list(ac, "messages")
	if len(msgs) != 2 || msgs[0].(map[string]any)["sender"] != "customer" || msgs[1].(map[string]any)["staff"] != "O" || ac["store_name"] != "H" {
		t.Fatalf("admin chat: %v", ac)
	}
	var views int
	if err := a.DB.QueryRow(context.Background(), `SELECT count(*) FROM admin_audit_logs WHERE action='chat.view' AND target_id=$1`, threadID).Scan(&views); err != nil || views != 1 {
		t.Fatalf("audit of viewing: %d %v", views, err)
	}

	// the platform can stop a customer and close a store's chat
	admin.want(200, "", "POST", fmt.Sprintf("/admin/chat-customers/%d/mute", custID), map[string]any{"muted": true})
	if why := say(t, cIM, bIM, "hello?", 0); why != "CHAT_MUTED" {
		t.Fatalf("muted: %q", why)
	}
	if chat := cust.want(200, "", "GET", "/customer/chats/"+f.slug, nil); chat["muted"] != true {
		t.Fatalf("muted flag: %v", chat)
	}
	admin.want(200, "", "POST", fmt.Sprintf("/admin/chat-customers/%d/mute", custID), map[string]any{"muted": false})
	admin.want(200, "", "POST", "/admin/chat-stores/"+bid, map[string]any{"enabled": false})
	if why := say(t, cIM, bIM, "hello?", 0); why != "CHAT_CLOSED" {
		t.Fatalf("closed: %q", why)
	}
	admin.want(200, "", "POST", "/admin/chat-stores/"+bid, map[string]any{"enabled": true})

	// too many messages in a minute
	for i := 0; i < 20; i++ {
		say(t, oIM, bIM, fmt.Sprintf("spam %d", i), 0)
	}
	if why := say(t, oIM, bIM, "one more", 0); why != "CHAT_TOO_FAST" {
		t.Fatalf("rate: %q", why)
	}

	// without a chat server nothing can sign in
	a.IM = nil
	cust.want(503, "CHAT_OFF", "POST", "/customer/chats/login", nil)
}

// card plays a picture or an order card through both callbacks, like say.
func card(t *testing.T, from, to string, c map[string]any, uid int64) string {
	t.Helper()
	msgSeq++
	data, _ := json.Marshal(c)
	content, _ := json.Marshal(map[string]string{"data": string(data)})
	body := map[string]any{"sendID": from, "recvID": to, "contentType": 110, "sessionType": 1, "content": string(content),
		"serverMsgID": fmt.Sprintf("c-%d-%d", time.Now().UnixNano(), msgSeq), "sendTime": time.Now().UnixMilli()}
	if uid != 0 {
		body["ex"] = fmt.Sprintf(`{"uid":%d}`, uid)
	}
	st, out := hook(t, "hook-key", "callbackBeforeSendSingleMsgCommand", body)
	if st != 200 {
		t.Fatalf("before hook: %d %v", st, out)
	}
	if out["nextCode"].(float64) != 0 {
		return out["errMsg"].(string)
	}
	if st, out := hook(t, "hook-key", "callbackAfterSendSingleMsgCommand", body); st != 200 {
		t.Fatalf("after hook: %d %v", st, out)
	}
	return ""
}

// raw posts a body as it is (an uploaded picture) and returns the status and the answer.
func raw(t *testing.T, c *client, path string, body []byte) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", server.URL+"/api/v1"+path, bytes.NewReader(body))
	req.Header.Set("X-Requested-With", "test")
	req.Header.Set("X-Forwarded-For", c.ip)
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// Pictures, order cards, quick replies and the closed store's own answer.
func TestChatCards(t *testing.T) {
	f := newFixture(t)
	im := &fakeIM{users: map[string]string{}}
	a.IM = im
	t.Cleanup(func() { a.IM = nil })
	cust, other, admin := verifiedCustomer(t), verifiedCustomer(t), adminClient(t)
	owner, cashier, kitchen := login(t, f.owner), login(t, f.cashier), login(t, f.kitchen)
	bid := fmt.Sprint(f.branchID)
	id := func(c *client) int64 { return int64(c.want(200, "", "GET", "/customer/me", nil)["id"].(float64)) }
	custID, otherID := id(cust), id(other)
	cIM, oIM, bIM := fmt.Sprintf("c_%d", custID), fmt.Sprintf("c_%d", otherID), "b_"+bid
	ownerID := int64(owner.want(200, "", "GET", "/auth/me", nil)["id"].(float64))
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 400)...)

	// a picture is uploaded first, then sent by its token
	if st, _ := raw(t, cust, "/customer/chats/"+f.slug+"/images", []byte("GIF89a")); st != 422 {
		t.Fatalf("not a JPEG: %d", st)
	}
	st, up := raw(t, cust, "/customer/chats/"+f.slug+"/images", jpeg)
	if st != 200 || up["token"] == "" {
		t.Fatalf("upload: %d %v", st, up)
	}
	token := up["token"].(string)
	if why := card(t, oIM, bIM, map[string]any{"t": "image", "token": token}, 0); why != "VALIDATION" {
		t.Fatalf("somebody else's picture: %q", why)
	}
	if why := card(t, cIM, bIM, map[string]any{"t": "image", "token": "nope"}, 0); why != "VALIDATION" {
		t.Fatalf("a picture that was never uploaded: %q", why)
	}
	if why := card(t, cIM, bIM, map[string]any{"t": "poll"}, 0); why != "VALIDATION" {
		t.Fatalf("an unknown card: %q", why)
	}
	if why := card(t, cIM, bIM, map[string]any{"t": "image", "token": token}, 0); why != "" {
		t.Fatalf("the picture: %q", why)
	}
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/chat-images/"+token, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("the picture is served: %v %v", resp, err)
	}
	resp.Body.Close()
	row := list(owner.want(200, "", "GET", "/merchant/branches/"+bid+"/chats", nil), "chats")[0].(map[string]any)
	if row["last_kind"] != "image" || row["last_text"] != "" {
		t.Fatalf("the list says a picture came: %v", row)
	}

	// the store's side uploads to the conversation with that customer
	kitchen.want(403, "FORBIDDEN", "GET", "/merchant/branches/"+bid+"/chat-setup", nil)
	if st, _ := raw(t, kitchen, fmt.Sprintf("/merchant/branches/%s/chats/%d/images", bid, custID), jpeg); st != 403 {
		t.Fatalf("the kitchen cannot upload: %d", st)
	}
	st, up = raw(t, cashier, fmt.Sprintf("/merchant/branches/%s/chats/%d/images", bid, custID), jpeg)
	if st != 200 {
		t.Fatalf("store upload: %d %v", st, up)
	}
	if why := card(t, bIM, cIM, map[string]any{"t": "image", "token": up["token"]}, ownerID); why != "" {
		t.Fatalf("the store's picture: %q", why)
	}

	// an order card: only an order this customer placed with this store
	orderID, orderToken := f.order(t, cust)
	otherOrder, _ := f.order(t, other)
	if why := card(t, cIM, bIM, map[string]any{"t": "order", "order": otherOrder}, 0); why != "VALIDATION" {
		t.Fatalf("somebody else's order: %q", why)
	}
	if why := card(t, cIM, bIM, map[string]any{"t": "order", "order": orderID}, 0); why != "" {
		t.Fatalf("the order card: %q", why)
	}
	chat := cust.want(200, "", "GET", "/customer/chats/"+f.slug, nil)
	orders := list(chat, "orders")
	if len(orders) != 1 || orders[0].(map[string]any)["token"] != orderToken {
		t.Fatalf("the customer's orders with the store, with their links: %v", orders)
	}
	sc := owner.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%s/chats/%d", bid, custID), nil)
	if o := list(sc, "orders"); len(o) != 1 || o[0].(map[string]any)["token"] != nil {
		t.Fatalf("the store sees the order without the customer's link: %v", o)
	}
	ac := admin.want(200, "", "GET", fmt.Sprintf("/admin/chats/%d", int64(chat["thread_id"].(float64))), nil)
	msgs := list(ac, "messages")
	if len(msgs) != 3 || msgs[0].(map[string]any)["body"] != "/api/v1/chat-images/"+token || msgs[2].(map[string]any)["order_no"] == "" {
		t.Fatalf("the platform sees the picture and the order: %v", msgs)
	}

	// "message the customer" on the order desk
	if got := owner.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%s/chat-order/%d", bid, orderID), nil); int64(got["customer_id"].(float64)) != custID {
		t.Fatalf("the order's customer: %v", got)
	}
	owner.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+bid+"/chat-order/999999", nil)

	// quick replies: the desk keeps them
	q := cashier.want(200, "", "POST", "/merchant/branches/"+bid+"/chat-quick-replies", map[string]any{"body": "  on its way  "})
	cashier.want(422, "VALIDATION", "POST", "/merchant/branches/"+bid+"/chat-quick-replies", map[string]any{"body": " "})
	su := owner.want(200, "", "GET", "/merchant/branches/"+bid+"/chat-setup", nil)
	if qs := list(su, "quick_replies"); len(qs) != 1 || qs[0].(map[string]any)["body"] != "on its way" || su["auto_reply_on"] != true {
		t.Fatalf("setup: %v", su)
	}
	cashier.want(200, "", "DELETE", fmt.Sprintf("/merchant/branches/%s/chat-quick-replies/%d", bid, int64(q["id"].(float64))), nil)

	// a closed store answers by itself, once in a while; an open one does not
	if len(im.sent) != 0 {
		t.Fatalf("an open store does not answer by itself: %v", im.sent)
	}
	cashier.want(403, "FORBIDDEN", "PUT", "/merchant/branches/"+bid+"/chat-auto-reply", map[string]any{"on": true, "text": "x"})
	owner.want(200, "", "PUT", "/merchant/branches/"+bid+"/chat-auto-reply", map[string]any{"on": true, "text": "back at nine"})
	if _, err := a.DB.Exec(context.Background(), `UPDATE branches SET paused=true WHERE id=$1`, f.branchID); err != nil {
		t.Fatal(err)
	}
	say(t, cIM, bIM, "anyone there?", 0)
	say(t, cIM, bIM, "hello?", 0)
	if len(im.sent) != 1 || im.sent[0] != bIM+">"+cIM+`:{"auto":true}` {
		t.Fatalf("one automatic answer: %v", im.sent)
	}
	// that answer comes back through the callbacks without a member of staff
	msgSeq++
	content, _ := json.Marshal(map[string]string{"content": "back at nine"})
	body := map[string]any{"sendID": bIM, "recvID": cIM, "contentType": 101, "sessionType": 1, "content": string(content),
		"serverMsgID": fmt.Sprintf("auto-%d", msgSeq), "sendTime": time.Now().UnixMilli(), "ex": `{"auto":true}`}
	if _, out := hook(t, "hook-key", "callbackBeforeSendSingleMsgCommand", body); out["nextCode"].(float64) != 0 {
		t.Fatalf("the automatic answer is let through: %v", out)
	}
	owner.want(200, "", "PUT", "/merchant/branches/"+bid+"/chat-auto-reply", map[string]any{"on": false, "text": ""})
	say(t, oIM, bIM, "hi", 0)
	if len(im.sent) != 1 {
		t.Fatalf("switched off: %v", im.sent)
	}
}
