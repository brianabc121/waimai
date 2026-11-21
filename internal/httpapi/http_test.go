package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/db"
	"github.com/brianabc121/waimai/internal/httpapi"
)

var (
	a       *app.App
	server  *httptest.Server
	metrics *app.Metrics
	dbURL   string
)

func TestMain(m *testing.M) {
	dbURL = os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		fmt.Println("TEST_DATABASE_URL not set; skipping HTTP tests")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := db.Migrate(ctx, dbURL); err != nil {
		panic(err)
	}
	pool, err := db.Connect(ctx, dbURL)
	if err != nil {
		panic(err)
	}
	a = app.New(pool, "test-secret")
	a.OTPDevEcho = true
	hub := httpapi.NewHub()
	go hub.Listen(ctx, dbURL)
	srv := httpapi.NewServer(a, config.Config{}, hub)
	metrics = a.NewMetrics(log.Printf)
	srv.Metrics = metrics
	server = httptest.NewServer(srv.Router())
	code := m.Run()
	server.CloseClientConnections()
	server.Close()
	cancel()
	pool.Close()
	os.Exit(code)
}

// client is a tiny cookie-keeping API client.
// Each client pretends to come from its own address (the test server trusts
// loopback as a proxy), so the per-IP rate limits do not trip across tests.
type client struct {
	t       *testing.T
	cookies map[string]string
	ip      string
}

func newClient(t *testing.T) *client {
	n++
	return &client{t: t, cookies: map[string]string{}, ip: fmt.Sprintf("203.0.%d.%d", n/250, n%250+1)}
}

func (c *client) do(method, path string, body any, headers ...string) (int, map[string]any) {
	c.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, server.URL+"/api/v1"+path, rd)
	req.Header.Set("X-Requested-With", "test")
	req.Header.Set("X-Forwarded-For", c.ip)
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			req.Header.Del(headers[i])
		} else {
			req.Header.Set(headers[i], headers[i+1])
		}
	}
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, ck := range resp.Cookies() {
		c.cookies[ck.Name] = ck.Value
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func errCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (c *client) want(status int, code string, method, path string, body any, headers ...string) map[string]any {
	c.t.Helper()
	st, out := c.do(method, path, body, headers...)
	if st != status || errCode(out) != code {
		c.t.Fatalf("%s %s: want %d %q, got %d %q (%v)", method, path, status, code, st, errCode(out), out)
	}
	return out
}

type fixture struct {
	slug                           string
	branchID, zoneID               int64
	itemID                         int64
	owner, cashier, kitchen, rider string
	riderID                        int64
}

var n int

func uniq(p string) string {
	n++
	return fmt.Sprintf("%s%d-%d", p, time.Now().UnixNano()%1e9, n)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	f := &fixture{slug: uniq("h"), owner: uniq("o"), cashier: uniq("c"), kitchen: uniq("k"), rider: uniq("r")}
	m, err := a.CreateMerchant(ctx, app.NewMerchant{MerchantName: "H", BranchName: "H", Slug: f.slug,
		Owner: app.NewUser{Username: f.owner, Password: "password1", DisplayName: "O"}})
	if err != nil {
		t.Fatal(err)
	}
	f.branchID = m.BranchID
	f.zoneID, _ = a.SaveZone(ctx, f.branchID, 0, app.ZoneInput{Name: "Z", Fee: 5000, Active: true})
	cat, _ := a.SaveCategory(ctx, f.branchID, 0, app.CategoryInput{Name: "C"})
	f.itemID, err = a.SaveItem(ctx, f.branchID, 0, app.ItemInput{CategoryID: cat, Name: "tea", Price: 6900,
		NeedsPrep: true, Published: true, DeliveryAvailable: true, PickupAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	for user, role := range map[string]string{f.cashier: "cashier", f.kitchen: "kitchen", f.rider: "rider"} {
		id, err := a.CreateStaff(ctx, f.branchID, app.NewUser{Username: user, Password: "password1", DisplayName: role, Role: role})
		if err != nil {
			t.Fatal(err)
		}
		if role == "rider" {
			f.riderID = id
		}
	}
	return f
}

func login(t *testing.T, user string) *client {
	t.Helper()
	c := newClient(t)
	c.want(200, "", "POST", "/auth/login", map[string]string{"username": user, "password": "password1"})
	return c
}

// verifiedCustomer goes through the OTP flow over HTTP.
func verifiedCustomer(t *testing.T) *client {
	t.Helper()
	c := newClient(t)
	phone := fmt.Sprintf("+6392%08d", time.Now().UnixNano()%1e8)
	out := c.want(200, "", "POST", "/otp/request", map[string]string{"phone": phone})
	c.want(200, "", "POST", "/otp/verify", map[string]string{"phone": phone, "code": out["dev_code"].(string)})
	return c
}

func (f *fixture) order(t *testing.T, c *client) (id int64, token string) {
	t.Helper()
	q := c.want(200, "", "POST", "/stores/"+f.slug+"/quotes", map[string]any{"service_type": "delivery",
		"zone_id": f.zoneID, "items": []map[string]any{{"item_id": f.itemID, "qty": 1}}})
	out := c.want(200, "", "POST", "/stores/"+f.slug+"/orders", map[string]any{"quote_id": q["id"],
		"customer_name": "Ann", "address_line": "1 Main"}, "Idempotency-Key", uniq("key"))
	return int64(out["order_id"].(float64)), out["access_token"].(string)
}

func TestCustomerMustVerifyPhone(t *testing.T) {
	f := newFixture(t)
	anon := newClient(t)
	sf := anon.want(200, "", "GET", "/stores/"+f.slug, nil)
	if sf["branch"] == nil || len(sf["categories"].([]any)) != 1 {
		t.Fatalf("storefront: %v", sf)
	}
	q := anon.want(200, "", "POST", "/stores/"+f.slug+"/quotes", map[string]any{"service_type": "delivery",
		"zone_id": f.zoneID, "items": []map[string]any{{"item_id": f.itemID, "qty": 1}}})
	body := map[string]any{"quote_id": q["id"], "customer_name": "Ann", "address_line": "1 Main"}
	anon.want(403, "PHONE_NOT_VERIFIED", "POST", "/stores/"+f.slug+"/orders", body, "Idempotency-Key", "k1")

	cust := verifiedCustomer(t)
	cust.want(422, "VALIDATION", "POST", "/stores/"+f.slug+"/orders", body) // no idempotency key
	cust.want(403, "CSRF", "POST", "/stores/"+f.slug+"/orders", body, "X-Requested-With", "")
	key := uniq("key")
	first := cust.want(200, "", "POST", "/stores/"+f.slug+"/orders", body, "Idempotency-Key", key)
	again := cust.want(200, "", "POST", "/stores/"+f.slug+"/orders", body, "Idempotency-Key", key)
	if first["order_id"] != again["order_id"] || first["access_token"] != again["access_token"] {
		t.Fatal("retry with the same key must return the same order")
	}

	// the order number alone does not open the order; the token does
	anon.want(404, "NOT_FOUND", "GET", "/customer/orders/"+first["public_no"].(string), nil)
	o := anon.want(200, "", "GET", "/customer/orders/"+first["access_token"].(string), nil)
	if o["display_status"] != "pending_acceptance" || o["cash"] != nil {
		t.Fatalf("customer view: %v", o)
	}
	o = anon.want(200, "", "POST", "/customer/orders/"+first["access_token"].(string)+"/cancel", nil)
	if o["display_status"] != "cancelled" {
		t.Fatalf("cancel: %v", o["display_status"])
	}
}

func TestTenantIsolationAndRoles(t *testing.T) {
	f1, f2 := newFixture(t), newFixture(t)
	cust := verifiedCustomer(t)
	orderID, _ := f1.order(t, cust)
	oid := fmt.Sprint(orderID)
	b1, b2 := fmt.Sprint(f1.branchID), fmt.Sprint(f2.branchID)

	newClient(t).want(401, "UNAUTHENTICATED", "GET", "/merchant/branches/"+b1+"/orders", nil)

	owner1, owner2 := login(t, f1.owner), login(t, f2.owner)
	list := owner1.want(200, "", "GET", "/merchant/branches/"+b1+"/orders", nil)
	if len(list["orders"].([]any)) != 1 {
		t.Fatalf("owner1 orders: %v", list)
	}
	// another merchant: every door looks like "not found"
	owner2.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/orders", nil)
	owner2.want(404, "NOT_FOUND", "GET", "/merchant/orders/"+oid, nil)
	owner2.want(404, "NOT_FOUND", "POST", "/merchant/orders/"+oid+"/accept", map[string]any{})
	owner2.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/menu", nil)
	owner2.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/events", nil)
	owner2.want(404, "NOT_FOUND", "GET", "/kitchen/branches/"+b1+"/board", nil)
	owner2.want(404, "NOT_FOUND", "PUT", "/merchant/branches/"+b1+"/items/"+fmt.Sprint(f1.itemID), map[string]any{"name": "x"})
	// an item id from store 1 cannot be edited through store 2's branch
	owner2.want(404, "NOT_FOUND", "PATCH", "/merchant/branches/"+b2+"/items/"+fmt.Sprint(f1.itemID), map[string]any{"sold_out": true})
	owner2.want(403, "FORBIDDEN", "GET", "/admin/merchants", nil)

	// roles inside one store
	cashier, kitchen, rider := login(t, f1.cashier), login(t, f1.kitchen), login(t, f1.rider)
	kitchen.want(403, "FORBIDDEN", "POST", "/merchant/orders/"+oid+"/accept", map[string]any{})
	cashier.want(403, "FORBIDDEN", "PUT", "/merchant/branches/"+b1+"/settings", map[string]any{})
	cashier.want(403, "FORBIDDEN", "GET", "/merchant/branches/"+b1+"/cash-collections", nil)
	rider.want(403, "FORBIDDEN", "GET", "/merchant/branches/"+b1+"/orders", nil)
	rider.want(403, "FORBIDDEN", "GET", "/kitchen/branches/"+b1+"/board", nil)

	o := cashier.want(200, "", "POST", "/merchant/orders/"+oid+"/accept", map[string]any{"expected_version": 1})
	cashier.want(409, "INVALID_STATE", "POST", "/merchant/orders/"+oid+"/accept", map[string]any{"expected_version": 1})
	itemID := fmt.Sprint(int64(o["items"].([]any)[0].(map[string]any)["id"].(float64)))

	// kitchen sees dishes but not the customer's phone or address
	board := kitchen.want(200, "", "GET", "/kitchen/branches/"+b1+"/board", nil)
	ko := board["orders"].([]any)[0].(map[string]any)
	if ko["customer_phone"] != "" || ko["address_line"] != "" {
		t.Fatalf("kitchen board leaks contact details: %v", ko)
	}
	owner2.want(404, "NOT_FOUND", "POST", "/kitchen/items/"+itemID+"/ready", nil)
	o = kitchen.want(200, "", "POST", "/kitchen/items/"+itemID+"/ready", nil)
	o = kitchen.want(200, "", "POST", "/kitchen/orders/"+oid+"/pack",
		map[string]any{"bags": 1, "manifest_version": o["manifest_version"]})

	// rider sees nothing until assigned
	tasks := rider.want(200, "", "GET", "/rider/tasks", nil)
	if len(tasks["tasks"].([]any)) != 0 {
		t.Fatal("unassigned rider must have no tasks")
	}
	o = cashier.want(200, "", "POST", "/merchant/orders/"+oid+"/assign-rider", map[string]any{"rider_id": f1.riderID})
	taskID := fmt.Sprint(int64(o["delivery"].(map[string]any)["id"].(float64)))
	// a rider from another store cannot touch the task
	rider2 := login(t, f2.rider)
	rider2.want(404, "NOT_ASSIGNED", "POST", "/rider/tasks/"+taskID+"/pickup", nil)
	// assigning a rider from another store is refused
	cashier.want(422, "VALIDATION", "POST", "/merchant/orders/"+oid+"/assign-rider", map[string]any{"rider_id": f2.riderID})

	rider.want(200, "", "POST", "/rider/tasks/"+taskID+"/pickup", nil)
	total := o["totals"].(map[string]any)["total"]
	done := rider.want(200, "", "POST", "/rider/tasks/"+taskID+"/deliver", map[string]any{"collected_amount": total})
	if done["display_status"] != "delivered" || done["customer_phone"] != "" {
		t.Fatalf("rider view after delivery: %v %v", done["display_status"], done["customer_phone"])
	}
	cash := owner1.want(200, "", "GET", "/merchant/branches/"+b1+"/cash-collections", nil)
	cid := fmt.Sprint(int64(cash["collections"].([]any)[0].(map[string]any)["id"].(float64)))
	owner2.want(404, "NOT_FOUND", "POST", "/merchant/cash-collections/"+cid+"/reconcile", map[string]any{"amount_returned": total})
	rec := owner1.want(200, "", "POST", "/merchant/cash-collections/"+cid+"/reconcile", map[string]any{"amount_returned": total})
	if rec["reconcile_status"] != "reconciled" {
		t.Fatalf("reconcile: %v", rec)
	}

	// a disabled account loses access at once
	owner1.want(200, "", "PATCH", "/merchant/branches/"+b1+"/staff/"+fmt.Sprint(f1.riderID), map[string]any{"active": false})
	rider.want(401, "UNAUTHENTICATED", "GET", "/rider/tasks", nil)
}

func TestLoginRateLimit(t *testing.T) {
	c := newClient(t)
	limited := false
	for i := 0; i < 15; i++ {
		st, out := c.do("POST", "/auth/login", map[string]string{"username": "nobody-here", "password": "x"})
		if st == 429 && errCode(out) == "RATE_LIMITED" {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("repeated logins from one address should be rate limited")
	}
}

func TestLoginAndAdmin(t *testing.T) {
	f := newFixture(t)
	c := newClient(t)
	c.want(401, "BAD_CREDENTIALS", "POST", "/auth/login", map[string]string{"username": f.owner, "password": "wrong-password"})
	c.want(401, "BAD_CREDENTIALS", "POST", "/auth/login", map[string]string{"username": "nobody-here", "password": "password1"})

	if err := a.EnsurePlatformAdmin(context.Background(), "admin-test", "password1"); err != nil {
		t.Fatal(err)
	}
	admin := login(t, "admin-test")
	slug := uniq("new")
	body := map[string]any{"merchant_name": "N", "branch_name": "N", "slug": slug,
		"owner": map[string]any{"username": uniq("no"), "password": "password1", "display_name": "N"}}
	admin.want(200, "", "POST", "/admin/merchants", body)
	admin.want(409, "SLUG_TAKEN", "POST", "/admin/merchants", body)
	admin.want(200, "", "POST", "/auth/logout", nil)
	admin.want(401, "UNAUTHENTICATED", "GET", "/admin/merchants", nil)
}

// TestSSE checks that an order change reaches the branch stream and that
// another merchant's stream stays quiet.
func TestSSE(t *testing.T) {
	f1, f2 := newFixture(t), newFixture(t)
	owner1, owner2 := login(t, f1.owner), login(t, f2.owner)
	open := func(c *client, bid int64) chan string {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/merchant/branches/%d/events", server.URL, bid), nil)
		for k, v := range c.cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("open sse: %v %v", err, resp)
		}
		t.Cleanup(func() { resp.Body.Close() })
		ch := make(chan string, 16)
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := resp.Body.Read(buf)
				if n > 0 {
					for _, line := range strings.Split(string(buf[:n]), "\n") {
						if strings.HasPrefix(line, "event: ") {
							ch <- strings.TrimPrefix(line, "event: ")
						}
					}
				}
				if err != nil {
					return
				}
			}
		}()
		return ch
	}
	s1, s2 := open(owner1, f1.branchID), open(owner2, f2.branchID)
	wait := func(ch chan string, want string, d time.Duration) bool {
		deadline := time.After(d)
		for {
			select {
			case ev := <-ch:
				if ev == want {
					return true
				}
			case <-deadline:
				return false
			}
		}
	}
	if !wait(s1, "ready", 3*time.Second) || !wait(s2, "ready", 3*time.Second) {
		t.Fatal("stream did not open")
	}
	// open streams are counted as connections, not as requests
	if err := metrics.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats, err := a.APIMetrics(context.Background(), "1h")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Streams.Store < 2 {
		t.Fatalf("two store streams are open, counted %d", stats.Streams.Store)
	}
	for _, r := range stats.Routes {
		if strings.HasSuffix(r.Route, "/events") {
			t.Fatalf("an event stream was counted as a request: %+v", r)
		}
	}
	time.Sleep(300 * time.Millisecond) // let the listener settle; drain stray refreshes
	for len(s1) > 0 {
		<-s1
	}
	for len(s2) > 0 {
		<-s2
	}
	f1.order(t, verifiedCustomer(t))
	if !wait(s1, "refresh", 3*time.Second) {
		t.Fatal("branch 1 did not get a refresh hint for its new order")
	}
	if wait(s2, "refresh", 700*time.Millisecond) {
		t.Fatal("branch 2 received a hint about branch 1's order")
	}
}

func TestStationsAndFailedDeliveryOverHTTP(t *testing.T) {
	f1, f2 := newFixture(t), newFixture(t)
	b1 := fmt.Sprint(f1.branchID)
	owner, owner2 := login(t, f1.owner), login(t, f2.owner)
	cashier, kitchen, rider := login(t, f1.cashier), login(t, f1.kitchen), login(t, f1.rider)

	// only managers define stations; other merchants see nothing
	cashier.want(403, "FORBIDDEN", "POST", "/merchant/branches/"+b1+"/stations", map[string]any{"name": "Fryer", "active": true})
	owner2.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/stations", nil)
	st := owner.want(200, "", "POST", "/merchant/branches/"+b1+"/stations", map[string]any{"name": "Fryer", "active": true})
	st2 := owner.want(200, "", "POST", "/merchant/branches/"+b1+"/stations", map[string]any{"name": "Bar", "active": true})
	foreign := owner2.want(200, "", "POST", "/merchant/branches/"+fmt.Sprint(f2.branchID)+"/stations", map[string]any{"name": "X", "active": true})
	// a station of another store cannot be used here
	owner.want(422, "VALIDATION", "POST", "/merchant/branches/"+b1+"/station-assignments",
		map[string]any{"item_ids": []int64{f1.itemID}, "station_id": foreign["id"]})
	up := owner.want(200, "", "POST", "/merchant/branches/"+b1+"/station-assignments",
		map[string]any{"item_ids": []int64{f1.itemID}, "station_id": st["id"]})
	if up["updated"].(float64) != 1 {
		t.Fatalf("assign: %v", up)
	}

	orderID, _ := f1.order(t, verifiedCustomer(t))
	oid := fmt.Sprint(orderID)
	o := cashier.want(200, "", "POST", "/merchant/orders/"+oid+"/accept", map[string]any{})
	it := o["items"].([]any)[0].(map[string]any)
	if it["station_name"] != "Fryer" {
		t.Fatalf("item should be routed to the fryer: %v", it)
	}
	itemID := fmt.Sprint(int64(it["id"].(float64)))
	owner2.want(404, "NOT_FOUND", "POST", "/kitchen/items/"+itemID+"/station", map[string]any{"station_id": nil})
	kitchen.want(422, "VALIDATION", "POST", "/kitchen/items/"+itemID+"/station", map[string]any{"station_id": foreign["id"]})
	o = kitchen.want(200, "", "POST", "/kitchen/items/"+itemID+"/station", map[string]any{"station_id": st2["id"]})
	if o["items"].([]any)[0].(map[string]any)["station_name"] != "Bar" {
		t.Fatalf("move: %v", o["items"])
	}
	list := kitchen.want(200, "", "GET", "/merchant/branches/"+b1+"/stations", nil)
	if n := list["stations"].([]any)[1].(map[string]any)["open_tasks"].(float64); n != 1 {
		t.Fatalf("bar should hold one open task, got %v", n)
	}

	// failed delivery: redeliver, fail again, close
	o = kitchen.want(200, "", "POST", "/kitchen/items/"+itemID+"/ready", nil)
	kitchen.want(200, "", "POST", "/kitchen/orders/"+oid+"/pack", map[string]any{"bags": 1, "manifest_version": o["manifest_version"]})
	o = cashier.want(200, "", "POST", "/merchant/orders/"+oid+"/assign-rider", map[string]any{"rider_id": f1.riderID})
	taskID := fmt.Sprint(int64(o["delivery"].(map[string]any)["id"].(float64)))
	cashier.want(409, "INVALID_STATE", "POST", "/merchant/orders/"+oid+"/redeliver", map[string]any{"rider_id": f1.riderID})
	rider.want(200, "", "POST", "/rider/tasks/"+taskID+"/pickup", nil)
	rider.want(200, "", "POST", "/rider/tasks/"+taskID+"/fail", map[string]any{"reason": "nobody home"})
	kitchen.want(403, "FORBIDDEN", "POST", "/merchant/orders/"+oid+"/redeliver", map[string]any{"rider_id": f1.riderID})
	owner2.want(404, "NOT_FOUND", "POST", "/merchant/orders/"+oid+"/close-failed", map[string]any{"reason": "x"})
	cashier.want(422, "VALIDATION", "POST", "/merchant/orders/"+oid+"/redeliver", map[string]any{"rider_id": f2.riderID})
	o = cashier.want(200, "", "POST", "/merchant/orders/"+oid+"/redeliver", map[string]any{"rider_id": f1.riderID})
	if o["fulfillment_status"] != "awaiting_handoff" {
		t.Fatalf("redeliver: %v", o["fulfillment_status"])
	}
	rider.want(200, "", "POST", "/rider/tasks/"+taskID+"/pickup", nil)
	rider.want(200, "", "POST", "/rider/tasks/"+taskID+"/fail", map[string]any{"reason": "wrong address"})
	cashier.want(422, "VALIDATION", "POST", "/merchant/orders/"+oid+"/close-failed", map[string]any{"reason": " "})
	o = cashier.want(200, "", "POST", "/merchant/orders/"+oid+"/close-failed", map[string]any{"reason": "customer unreachable"})
	if o["acceptance_status"] != "cancelled" || o["payment_status"] != "unpaid" {
		t.Fatalf("close: %v %v", o["acceptance_status"], o["payment_status"])
	}
}

func TestPromotionsOverHTTP(t *testing.T) {
	f1, f2 := newFixture(t), newFixture(t)
	b1 := fmt.Sprint(f1.branchID)
	owner, owner2, cashier := login(t, f1.owner), login(t, f2.owner), login(t, f1.cashier)
	body := map[string]any{"name": "满50减10", "kind": "amount_off", "threshold": 5000, "value": 1000,
		"applies_delivery": true, "applies_pickup": true, "per_phone_limit": 1}

	cashier.want(403, "FORBIDDEN", "POST", "/merchant/branches/"+b1+"/promotions", body)
	cashier.want(403, "FORBIDDEN", "GET", "/merchant/branches/"+b1+"/promotions", nil)
	owner2.want(404, "NOT_FOUND", "POST", "/merchant/branches/"+b1+"/promotions", body)
	owner.want(422, "VALIDATION", "POST", "/merchant/branches/"+b1+"/promotions", map[string]any{"name": "x", "kind": "amount_off"})
	p := owner.want(200, "", "POST", "/merchant/branches/"+b1+"/promotions", body)
	pid := fmt.Sprint(int64(p["id"].(float64)))
	// another merchant cannot publish or edit it, even through their own branch
	owner2.want(404, "NOT_FOUND", "POST", "/merchant/branches/"+b1+"/promotions/"+pid+"/status", map[string]any{"status": "active"})
	owner2.want(404, "NOT_FOUND", "POST", "/merchant/branches/"+fmt.Sprint(f2.branchID)+"/promotions/"+pid+"/status", map[string]any{"status": "active"})
	owner2.want(404, "NOT_FOUND", "PUT", "/merchant/branches/"+fmt.Sprint(f2.branchID)+"/promotions/"+pid, body)

	anon := newClient(t)
	if sf := anon.want(200, "", "GET", "/stores/"+f1.slug, nil); len(sf["promotions"].([]any)) != 0 {
		t.Fatal("a draft must not be advertised")
	}
	owner.want(200, "", "POST", "/merchant/branches/"+b1+"/promotions/"+pid+"/status", map[string]any{"status": "active"})
	sf := anon.want(200, "", "GET", "/stores/"+f1.slug, nil)
	adv := sf["promotions"].([]any)[0].(map[string]any)
	if adv["used_count"].(float64) != 0 || adv["per_phone_limit"] != nil {
		t.Fatalf("storefront leaks usage numbers: %v", adv)
	}
	trial := owner.want(200, "", "POST", "/merchant/branches/"+b1+"/promotion-trial",
		map[string]any{"service_type": "delivery", "subtotal": 6900, "delivery_fee": 5000})
	if trial["totals"].(map[string]any)["discount"].(float64) != 1000 {
		t.Fatalf("trial: %v", trial)
	}

	// a verified customer uses it once; their next quote is at full price
	cust := verifiedCustomer(t)
	orderID, token := f1.order(t, cust)
	o := anon.want(200, "", "GET", "/customer/orders/"+token, nil)
	tot := o["totals"].(map[string]any)
	if tot["discount"].(float64) != 1000 || tot["promotion_name"] != "满50减10" {
		t.Fatalf("order totals: %v", tot)
	}
	quote := map[string]any{"service_type": "delivery", "zone_id": f1.zoneID, "items": []map[string]any{{"item_id": f1.itemID, "qty": 1}}}
	if q := cust.want(200, "", "POST", "/stores/"+f1.slug+"/quotes", quote); q["totals"].(map[string]any)["discount"].(float64) != 0 {
		t.Fatal("the once-per-customer offer was quoted twice")
	}
	if q := anon.want(200, "", "POST", "/stores/"+f1.slug+"/quotes", quote); q["totals"].(map[string]any)["discount"].(float64) != 1000 {
		t.Fatal("an anonymous visitor should still see the offer")
	}
	list := owner.want(200, "", "GET", "/merchant/branches/"+b1+"/promotions", nil)
	if list["promotions"].([]any)[0].(map[string]any)["used_count"].(float64) != 1 {
		t.Fatalf("used_count: %v", list)
	}
	// rejecting gives the use back
	cashier.want(200, "", "POST", "/merchant/orders/"+fmt.Sprint(orderID)+"/reject", map[string]any{"reason": "busy"})
	list = owner.want(200, "", "GET", "/merchant/branches/"+b1+"/promotions", nil)
	if list["promotions"].([]any)[0].(map[string]any)["used_count"].(float64) != 0 {
		t.Fatalf("used_count after reject: %v", list)
	}
}

func TestImportAndScheduleOverHTTP(t *testing.T) {
	f1, f2 := newFixture(t), newFixture(t)
	b1 := fmt.Sprint(f1.branchID)
	owner, owner2, cashier := login(t, f1.owner), login(t, f2.owner), login(t, f1.cashier)

	raw := func(c *client, path string, body []byte) (int, map[string]any) {
		req, _ := http.NewRequest("POST", server.URL+"/api/v1"+path, bytes.NewReader(body))
		req.Header.Set("X-Requested-With", "test")
		req.Header.Set("X-Forwarded-For", c.ip)
		for k, v := range c.cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	tpl, err := app.MenuImportTemplate()
	if err != nil {
		t.Fatal(err)
	}
	path := "/merchant/branches/" + b1 + "/menu-imports"
	if code, _ := raw(cashier, path+"?filename=m.xlsx", tpl); code != 403 {
		t.Fatalf("cashier upload: %d", code)
	}
	if code, _ := raw(owner2, path+"?filename=m.xlsx", tpl); code != 404 {
		t.Fatalf("other merchant upload: %d", code)
	}
	if code, _ := raw(owner, path+"?filename=m.xlsx", make([]byte, 2<<20+10)); code != 413 {
		t.Fatalf("oversize upload: %d", code)
	}
	if code, out := raw(owner, path+"?filename=m.txt", tpl); code != 422 {
		t.Fatalf("wrong extension: %d %v", code, out)
	}
	code, out := raw(owner, path+"?filename=m.xlsx", tpl)
	if code != 200 {
		t.Fatalf("upload: %d %v", code, out)
	}
	id := fmt.Sprint(int64(out["id"].(float64)))
	owner2.want(404, "NOT_FOUND", "GET", path+"/"+id, nil)
	owner2.want(404, "NOT_FOUND", "POST", "/merchant/branches/"+fmt.Sprint(f2.branchID)+"/menu-imports/"+id+"/apply", nil)
	cashier.want(403, "FORBIDDEN", "POST", path+"/"+id+"/apply", nil)
	if err := a.RunJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := owner.want(200, "", "GET", path+"/"+id, nil)
	if m["status"] != "parsed" || m["valid_rows"].(float64) != 3 {
		t.Fatalf("import: %v", m)
	}
	m = owner.want(200, "", "POST", path+"/"+id+"/apply", nil)
	if m["status"] != "applied" || m["created_count"].(float64) != 3 {
		t.Fatalf("apply: %v", m)
	}
	owner.want(409, "INVALID_STATE", "POST", path+"/"+id+"/apply", nil)
	// drafts are not on the storefront
	anon := newClient(t)
	if sf := anon.want(200, "", "GET", "/stores/"+f1.slug, nil); len(sf["categories"].([]any)) != 1 {
		t.Fatalf("imported drafts leaked: %v", sf["categories"])
	}

	// scheduling over HTTP: off -> refused; on -> slots offered and an order carries the time
	later := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	quote := map[string]any{"service_type": "delivery", "zone_id": f1.zoneID, "scheduled_for": later,
		"items": []map[string]any{{"item_id": f1.itemID, "qty": 1}}}
	anon.want(422, "SCHEDULE_INVALID", "POST", "/stores/"+f1.slug+"/quotes", quote)
	owner.want(200, "", "PUT", "/merchant/branches/"+b1+"/settings", map[string]any{"name": "H", "accept_timeout_minutes": 10,
		"delivery_enabled": true, "pickup_enabled": true, "open_time": "00:00", "close_time": "00:00",
		"scheduling_enabled": true, "prep_minutes": 45})
	sf := anon.want(200, "", "GET", "/stores/"+f1.slug, nil)
	slots := sf["slots"].([]any)
	if len(slots) == 0 {
		t.Fatal("no slots offered")
	}
	quote["scheduled_for"] = slots[3]
	cust := verifiedCustomer(t)
	q := cust.want(200, "", "POST", "/stores/"+f1.slug+"/quotes", quote)
	placed := cust.want(200, "", "POST", "/stores/"+f1.slug+"/orders", map[string]any{"quote_id": q["id"],
		"customer_name": "Ann", "address_line": "1 Main"}, "Idempotency-Key", uniq("key"))
	o := anon.want(200, "", "GET", "/customer/orders/"+placed["access_token"].(string), nil)
	if o["scheduled_for"] == nil {
		t.Fatalf("order lost its time: %v", o)
	}
	oid := fmt.Sprint(int64(placed["order_id"].(float64)))
	o = cashier.want(200, "", "POST", "/merchant/orders/"+oid+"/accept", map[string]any{})
	if o["display_status"] != "scheduled" {
		t.Fatalf("display status: %v", o["display_status"])
	}
	board := login(t, f1.kitchen).want(200, "", "GET", "/kitchen/branches/"+b1+"/board", nil)
	if len(board["orders"].([]any)) != 0 {
		t.Fatal("scheduled order reached the kitchen early")
	}
}

func TestDineInOverHTTP(t *testing.T) {
	f1, f2 := newFixture(t), newFixture(t)
	b1 := fmt.Sprint(f1.branchID)
	owner, cashier, kitchen, rider, other := login(t, f1.owner), login(t, f1.cashier), login(t, f1.kitchen), login(t, f1.rider), login(t, f2.owner)

	// tables are a manager's job; another merchant sees nothing
	cashier.want(403, "FORBIDDEN", "POST", "/merchant/branches/"+b1+"/tables", map[string]any{"name": "08", "active": true})
	tid := fmt.Sprint(int64(owner.want(200, "", "POST", "/merchant/branches/"+b1+"/tables", map[string]any{"name": "08", "active": true})["id"].(float64)))
	owner.want(422, "VALIDATION", "POST", "/merchant/branches/"+b1+"/tables", map[string]any{"name": "08", "active": true})
	other.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/tables", nil)
	other.want(404, "NOT_FOUND", "POST", "/merchant/tables/"+tid+"/open", map[string]any{"guests": 2})
	kitchen.want(403, "FORBIDDEN", "POST", "/merchant/tables/"+tid+"/open", map[string]any{"guests": 2})
	rider.want(403, "FORBIDDEN", "GET", "/merchant/branches/"+b1+"/tables", nil)

	list := cashier.want(200, "", "GET", "/merchant/branches/"+b1+"/tables", nil)
	qr := list["tables"].([]any)[0].(map[string]any)["qr_token"].(string)

	// a guest cannot get in before the table is open, or with a made-up code
	phone := newClient(t)
	phone.want(409, "TABLE_NOT_OPEN", "POST", "/table-sessions/exchange", map[string]any{"code": qr})
	phone.want(404, "NOT_FOUND", "POST", "/table-sessions/exchange", map[string]any{"code": "nope"})
	phone.want(409, "TABLE_SESSION_CLOSED", "GET", "/table-sessions/current", nil)

	bill := cashier.want(200, "", "POST", "/merchant/tables/"+tid+"/open", map[string]any{"guests": 2})
	sid := fmt.Sprint(int64(bill["session_id"].(float64)))
	cashier.want(409, "TABLE_OCCUPIED", "POST", "/merchant/tables/"+tid+"/open", map[string]any{"guests": 2})
	other.want(404, "NOT_FOUND", "GET", "/merchant/table-sessions/"+sid, nil)
	other.want(404, "NOT_FOUND", "POST", "/merchant/table-sessions/"+sid+"/close", map[string]any{})

	// phone: scan, then one batch; the same key again adds nothing
	phone.want(200, "", "POST", "/table-sessions/exchange", map[string]any{"code": qr})
	cur := phone.want(200, "", "GET", "/table-sessions/current", nil)
	if acc := cur["access"].(map[string]any); acc["table_name"] != "08" || acc["mode"] != "qr" || acc["slug"] != f1.slug {
		t.Fatalf("access: %v", acc)
	}
	batch := map[string]any{"items": []map[string]any{{"item_id": f1.itemID, "qty": 2}}, "expected_total": 13800}
	phone.want(422, "VALIDATION", "POST", "/table-sessions/current/batches", batch)
	phone.want(409, "QUOTE_CHANGED", "POST", "/table-sessions/current/batches",
		map[string]any{"items": batch["items"], "expected_total": 1}, "Idempotency-Key", uniq("k"))
	key := uniq("k")
	phone.want(200, "", "POST", "/table-sessions/current/batches", batch, "Idempotency-Key", key)
	got := phone.want(200, "", "POST", "/table-sessions/current/batches", batch, "Idempotency-Key", key)
	order := got["order"].(map[string]any)
	if items := order["items"].([]any); len(items) != 1 || order["totals"].(map[string]any)["subtotal"].(float64) != 13800 {
		t.Fatalf("bill after replay: %v", order)
	}
	if order["customer_phone"] != "" || order["cash"] != nil {
		t.Fatalf("guest bill leaks: %v", order)
	}

	// tablet: pair with a single-use code, then add a second batch to the same bill
	kitchen.want(403, "FORBIDDEN", "POST", "/merchant/tables/"+tid+"/devices", map[string]any{})
	code := cashier.want(200, "", "POST", "/merchant/tables/"+tid+"/devices", map[string]any{"name": "pad"})["code"].(string)
	pad := newClient(t)
	pad.want(401, "DEVICE_NOT_PAIRED", "GET", "/table-sessions/current?via=device", nil)
	pad.want(422, "PAIR_CODE_INVALID", "POST", "/table-devices/pair", map[string]any{"code": "WRONG123"})
	pad.want(200, "", "POST", "/table-devices/pair", map[string]any{"code": code})
	newClient(t).want(422, "PAIR_CODE_INVALID", "POST", "/table-devices/pair", map[string]any{"code": code})
	got = pad.want(200, "", "POST", "/table-sessions/current/batches?via=device",
		map[string]any{"items": []map[string]any{{"item_id": f1.itemID, "qty": 1}}}, "Idempotency-Key", uniq("k"))
	items := got["order"].(map[string]any)["items"].([]any)
	if len(items) != 2 || items[1].(map[string]any)["batch_seq"].(float64) != 2 || items[1].(map[string]any)["batch_source"] != "ipad" {
		t.Fatalf("second batch: %v", items)
	}
	i1 := fmt.Sprint(int64(items[0].(map[string]any)["id"].(float64)))
	i2 := fmt.Sprint(int64(items[1].(map[string]any)["id"].(float64)))

	// kitchen sees the table on its board; serve needs a ready dish; void is the desk's
	board := kitchen.want(200, "", "GET", "/kitchen/branches/"+b1+"/board", nil)["orders"].([]any)
	if len(board) != 1 || board[0].(map[string]any)["table_name"] != "08" {
		t.Fatalf("board: %v", board)
	}
	kitchen.want(409, "INVALID_STATE", "POST", "/merchant/bill-items/"+i1+"/serve", map[string]any{})
	kitchen.want(200, "", "POST", "/kitchen/items/"+i1+"/ready", map[string]any{})
	other.want(404, "NOT_FOUND", "POST", "/merchant/bill-items/"+i1+"/serve", map[string]any{})
	rider.want(403, "FORBIDDEN", "POST", "/merchant/bill-items/"+i1+"/serve", map[string]any{})
	kitchen.want(200, "", "POST", "/merchant/bill-items/"+i1+"/serve", map[string]any{})
	kitchen.want(403, "FORBIDDEN", "POST", "/merchant/bill-items/"+i2+"/void", map[string]any{"reason": "x"})

	// closing: stale list is refused, so is a dish still cooking; then void and pay
	b := cashier.want(200, "", "GET", "/merchant/table-sessions/"+sid, nil)
	mv := b["order"].(map[string]any)["manifest_version"].(float64)
	cashier.want(409, "VERSION_CONFLICT", "POST", "/merchant/table-sessions/"+sid+"/close",
		map[string]any{"expected_manifest_version": mv - 1, "amount_received": 99999})
	cashier.want(409, "ITEMS_IN_PROGRESS", "POST", "/merchant/table-sessions/"+sid+"/close",
		map[string]any{"expected_manifest_version": mv, "amount_received": 99999})
	b = cashier.want(200, "", "POST", "/merchant/bill-items/"+i2+"/void", map[string]any{"reason": "guest changed mind"})
	mv = b["order"].(map[string]any)["manifest_version"].(float64)
	total := b["order"].(map[string]any)["totals"].(map[string]any)["total"].(float64)
	cashier.want(422, "VALIDATION", "POST", "/merchant/table-sessions/"+sid+"/close",
		map[string]any{"expected_manifest_version": mv, "amount_received": total - 1})
	kitchen.want(403, "FORBIDDEN", "POST", "/merchant/table-sessions/"+sid+"/close",
		map[string]any{"expected_manifest_version": mv, "amount_received": total})
	b = cashier.want(200, "", "POST", "/merchant/table-sessions/"+sid+"/close",
		map[string]any{"expected_manifest_version": mv, "amount_received": total + 200})
	if b["status"] != "closed" || b["order"].(map[string]any)["payment_status"] != "paid" {
		t.Fatalf("closed bill: %v", b)
	}

	// after closing: the phone's cookie is dead, the tablet waits for the next sitting,
	// and a lost answer can still be recovered with the original key
	phone.want(409, "TABLE_SESSION_CLOSED", "GET", "/table-sessions/current", nil)
	phone.want(409, "TABLE_SESSION_CLOSED", "POST", "/table-sessions/current/batches", batch, "Idempotency-Key", uniq("k"))
	phone.want(409, "TABLE_NOT_OPEN", "POST", "/table-sessions/exchange", map[string]any{"code": qr})
	idle := pad.want(200, "", "GET", "/table-sessions/current?via=device", nil)
	if idle["bill"] != nil {
		t.Fatalf("tablet should be idle: %v", idle)
	}
	pad.want(409, "TABLE_SESSION_CLOSED", "POST", "/table-sessions/current/batches?via=device", batch, "Idempotency-Key", uniq("k"))

	// next guests: a new session, an empty bill; the old phone has to scan again
	b = cashier.want(200, "", "POST", "/merchant/tables/"+tid+"/open", map[string]any{"guests": 4})
	if fmt.Sprint(int64(b["session_id"].(float64))) == sid || len(b["order"].(map[string]any)["items"].([]any)) != 0 {
		t.Fatalf("new sitting: %v", b)
	}
	next := pad.want(200, "", "GET", "/table-sessions/current?via=device", nil)
	if next["bill"].(map[string]any)["session_id"] != b["session_id"] {
		t.Fatalf("tablet did not follow the new sitting: %v", next)
	}

	// unpairing cuts the tablet off at once
	dev := cashier.want(200, "", "GET", "/merchant/branches/"+b1+"/tables", nil)["tables"].([]any)[0].(map[string]any)["devices"].([]any)[0].(map[string]any)
	other.want(404, "NOT_FOUND", "POST", "/merchant/branches/"+fmt.Sprint(f2.branchID)+"/table-devices/"+fmt.Sprint(int64(dev["id"].(float64)))+"/revoke", map[string]any{})
	cashier.want(200, "", "POST", "/merchant/branches/"+b1+"/table-devices/"+fmt.Sprint(int64(dev["id"].(float64)))+"/revoke", map[string]any{})
	pad.want(401, "DEVICE_NOT_PAIRED", "GET", "/table-sessions/current?via=device", nil)

	// dine-in bills never show up in the delivery order list or its commands
	if l := cashier.want(200, "", "GET", "/merchant/branches/"+b1+"/orders", nil)["orders"].([]any); len(l) != 0 {
		t.Fatalf("dine-in order in delivery list: %v", l)
	}
}

func TestTemplatesOverHTTP(t *testing.T) {
	f1, f2 := newFixture(t), newFixture(t)
	b1 := "/merchant/branches/" + fmt.Sprint(f1.branchID) + "/templates"
	owner, cashier, other := login(t, f1.owner), login(t, f1.cashier), login(t, f2.owner)
	guest := newClient(t)
	look := func(channel string) map[string]any {
		t.Helper()
		return guest.want(200, "", "GET", "/stores/"+f1.slug, nil)["templates"].(map[string]any)[channel].(map[string]any)
	}

	// defaults before anything is saved
	if look("delivery")["layout"] != "list" || look("ipad")["layout"] != "grid" {
		t.Fatalf("defaults: %v", guest.want(200, "", "GET", "/stores/"+f1.slug, nil)["templates"])
	}
	cashier.want(403, "FORBIDDEN", "GET", b1, nil)
	other.want(404, "NOT_FOUND", "GET", b1, nil)
	other.want(404, "NOT_FOUND", "PUT", b1+"/delivery", map[string]any{"layout": "grid", "theme_color": "#112233"})

	// validation
	owner.want(422, "VALIDATION", "PUT", b1+"/delivery", map[string]any{"layout": "fancy", "theme_color": "#112233"})
	owner.want(422, "VALIDATION", "PUT", b1+"/delivery", map[string]any{"layout": "grid", "theme_color": "red"})
	owner.want(422, "VALIDATION", "PUT", b1+"/delivery", map[string]any{"layout": "grid", "theme_color": "#112233", "logo_url": "javascript:alert(1)"})
	owner.want(404, "NOT_FOUND", "PUT", b1+"/kiosk", map[string]any{"layout": "grid", "theme_color": "#112233"})
	owner.want(409, "NO_DRAFT", "POST", b1+"/delivery/publish", map[string]any{})

	// a saved draft is not visible to guests
	owner.want(200, "", "PUT", b1+"/delivery", map[string]any{"layout": "featured", "theme_color": "#0B7285", "cover_url": "https://example.com/c.jpg"})
	if look("delivery")["layout"] != "list" {
		t.Fatal("draft leaked to the storefront")
	}
	list := owner.want(200, "", "GET", b1, nil)["channels"].([]any)
	if d := list[0].(map[string]any); d["channel"] != "delivery" || d["draft"].(map[string]any)["layout"] != "featured" || d["published"].(map[string]any)["layout"] != "list" {
		t.Fatalf("draft state: %v", d)
	}

	// publishing delivery changes delivery only
	cashier.want(403, "FORBIDDEN", "POST", b1+"/delivery/publish", map[string]any{})
	owner.want(200, "", "POST", b1+"/delivery/publish", map[string]any{})
	if d := look("delivery"); d["layout"] != "featured" || d["theme_color"] != "#0b7285" || d["cover_url"] != "https://example.com/c.jpg" {
		t.Fatalf("published: %v", d)
	}
	if look("qr")["layout"] != "list" || look("ipad")["layout"] != "grid" {
		t.Fatal("publishing delivery changed another channel")
	}
	owner.want(409, "NO_DRAFT", "POST", b1+"/delivery/publish", map[string]any{})

	// discard keeps what is live
	owner.want(200, "", "PUT", b1+"/qr", map[string]any{"layout": "grid", "theme_color": "#112233"})
	owner.want(200, "", "POST", b1+"/qr/discard", map[string]any{})
	owner.want(409, "NO_DRAFT", "POST", b1+"/qr/publish", map[string]any{})
	if look("qr")["layout"] != "list" {
		t.Fatal("discard changed the live template")
	}

	// the menu and ordering are the same under any template
	cust := verifiedCustomer(t)
	if id, _ := f1.order(t, cust); id == 0 {
		t.Fatal("ordering broke after a template change")
	}
	// the other store is untouched
	if l := guest.want(200, "", "GET", "/stores/"+f2.slug, nil)["templates"].(map[string]any)["delivery"].(map[string]any)["layout"]; l != "list" {
		t.Fatalf("template crossed merchants: %v", l)
	}
}

// TestMapZonesOverHTTP covers who may change the delivery map and what the
// customer side gets to see.
func TestMapZonesOverHTTP(t *testing.T) {
	f, other := newFixture(t), newFixture(t)
	b := "/merchant/branches/" + fmt.Sprint(f.branchID)
	owner, cashier, stranger := login(t, f.owner), login(t, f.cashier), login(t, other.owner)
	anon := newClient(t)

	if cfg := anon.want(200, "", "GET", "/config", nil); cfg["maps_key"] != "" {
		t.Fatalf("config without a key: %v", cfg)
	}

	loc := map[string]any{"lat": 14.55, "lng": 121.02, "address": "1 Map St"}
	cashier.want(403, "FORBIDDEN", "PUT", b+"/location", loc)
	stranger.want(404, "NOT_FOUND", "PUT", b+"/location", loc)
	anon.want(401, "UNAUTHENTICATED", "PUT", b+"/location", loc)
	if br := owner.want(200, "", "PUT", b+"/location", loc); br["lat"] != 14.55 || br["address"] != "1 Map St" {
		t.Fatalf("location: %v", br)
	}

	// about 1.1 km square with the store in a corner
	square := [][]float64{{14.55, 121.02}, {14.55, 121.03}, {14.56, 121.03}, {14.56, 121.02}}
	zone := map[string]any{"name": "Near", "fee": 4900, "min_order": 0, "active": true, "polygon": square,
		"color": "#2a78d6", "eta_minutes": 25}
	cashier.want(403, "FORBIDDEN", "POST", b+"/zones", zone)
	stranger.want(404, "NOT_FOUND", "POST", b+"/zones", zone)
	bow := map[string]any{"name": "Bow", "fee": 1, "active": true,
		"polygon": [][]float64{{14.55, 121.02}, {14.56, 121.03}, {14.55, 121.03}, {14.56, 121.02}}}
	owner.want(422, "ZONE_SELF_INTERSECTS", "POST", b+"/zones", bow)
	zid := fmt.Sprint(int64(owner.want(200, "", "POST", b+"/zones", zone)["id"].(float64)))

	order := map[string]any{"ids": []any{f.zoneID}}
	owner.want(422, "VALIDATION", "PUT", b+"/zone-order", order)
	order["ids"] = []any{json.Number(zid), f.zoneID}
	cashier.want(403, "FORBIDDEN", "PUT", b+"/zone-order", order)
	owner.want(200, "", "PUT", b+"/zone-order", order)

	mode := map[string]any{"zone_mode": "map", "zone_tolerance_m": 200}
	cashier.want(403, "FORBIDDEN", "PUT", b+"/zone-mode", mode)
	if br := owner.want(200, "", "PUT", b+"/zone-mode", mode); br["zone_mode"] != "map" {
		t.Fatalf("zone mode: %v", br)
	}

	// The platform admin may look but not touch.
	if err := a.EnsurePlatformAdmin(context.Background(), "admin-map", "password1"); err != nil {
		t.Fatal(err)
	}
	admin := login(t, "admin-map")
	if zs := admin.want(200, "", "GET", b+"/menu", nil)["zones"].([]any); len(zs) != 2 ||
		len(zs[0].(map[string]any)["polygon"].([]any)) != 4 {
		t.Fatalf("admin view of zones: %v", zs)
	}
	admin.want(403, "READ_ONLY", "PUT", b+"/location", loc)
	admin.want(403, "READ_ONLY", "DELETE", b+"/zones/"+zid, nil)

	// The storefront carries what the customer's map needs.
	sf := anon.want(200, "", "GET", "/stores/"+f.slug, nil)
	br := sf["branch"].(map[string]any)
	if br["zone_mode"] != "map" || br["lat"] != 14.55 || len(sf["zones"].([]any)[0].(map[string]any)["polygon"].([]any)) != 4 {
		t.Fatalf("storefront: %v %v", br, sf["zones"])
	}

	at := func(lat, lng float64) map[string]any { return map[string]any{"lat": lat, "lng": lng} }
	in := anon.want(200, "", "POST", "/stores/"+f.slug+"/locate", at(14.555, 121.025))
	if z, _ := in["zone"].(map[string]any); z == nil || z["name"] != "Near" || z["eta_minutes"] != float64(25) || in["distance_m"] != float64(0) {
		t.Fatalf("locate inside: %v", in)
	}
	out := anon.want(200, "", "POST", "/stores/"+f.slug+"/locate", at(14.60, 121.025))
	if out["zone"] != nil || out["nearest"].(map[string]any)["name"] != "Near" {
		t.Fatalf("locate outside: %v", out)
	}
	anon.want(422, "VALIDATION", "POST", "/stores/"+f.slug+"/locate", map[string]any{"lat": 14.5})
	anon.want(422, "VALIDATION", "POST", "/stores/"+f.slug+"/locate", at(100, 0))
	anon.want(404, "NOT_FOUND", "POST", "/stores/no-such-store/locate", at(14.5, 121))

	// Quotes follow the pin, not the zone id the browser sends.
	items := []map[string]any{{"item_id": f.itemID, "qty": 1}}
	anon.want(422, "OUT_OF_ZONE", "POST", "/stores/"+f.slug+"/quotes", map[string]any{"service_type": "delivery",
		"zone_id": f.zoneID, "items": items})
	anon.want(422, "OUT_OF_ZONE", "POST", "/stores/"+f.slug+"/quotes", map[string]any{"service_type": "delivery",
		"zone_id": f.zoneID, "lat": 14.60, "lng": 121.025, "items": items})
	q := anon.want(200, "", "POST", "/stores/"+f.slug+"/quotes", map[string]any{"service_type": "delivery",
		"zone_id": f.zoneID, "lat": 14.555, "lng": 121.025, "items": items})
	if q["zone_name"] != "Near" || q["totals"].(map[string]any)["delivery_fee"] != float64(4900) {
		t.Fatalf("quote: %v", q)
	}
	cust := verifiedCustomer(t)
	placed := cust.want(200, "", "POST", "/stores/"+f.slug+"/orders", map[string]any{"quote_id": q["id"],
		"customer_name": "Ann", "address_line": "1 Main"}, "Idempotency-Key", uniq("key"))
	o := owner.want(200, "", "GET", "/merchant/orders/"+fmt.Sprint(int64(placed["order_id"].(float64))), nil)
	if o["dest_lat"] != 14.555 || o["dest_lng"] != 121.025 || o["zone_distance_m"] != nil {
		t.Fatalf("order location: %v", o)
	}

	// Removing the only drawn zone is allowed; customers are then out of range everywhere.
	stranger.want(404, "NOT_FOUND", "DELETE", b+"/zones/"+zid, nil)
	cashier.want(403, "FORBIDDEN", "DELETE", b+"/zones/"+zid, nil)
	owner.want(200, "", "DELETE", b+"/zones/"+zid, nil)
	owner.want(404, "NOT_FOUND", "DELETE", b+"/zones/"+zid, nil)
	anon.want(422, "OUT_OF_ZONE", "POST", "/stores/"+f.slug+"/quotes", map[string]any{"service_type": "delivery",
		"lat": 14.555, "lng": 121.025, "items": items})
}
