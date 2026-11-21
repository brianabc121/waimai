package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	a      *app.App
	server *httptest.Server
	dbURL  string
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
	server = httptest.NewServer(httpapi.NewServer(a, config.Config{}, hub).Router())
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
