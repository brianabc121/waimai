package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// The store's own page: the host says the store, what is ordered there is an
// Online- order, and the page lists that store's orders only
// (docs/ORDER_TYPES_DESIGN.md §5).
func TestStoreSiteOverHTTP(t *testing.T) {
	f := newFixture(t)
	g := newFixture(t)
	var no int
	if err := a.DB.QueryRow(context.Background(), `SELECT store_no FROM branches WHERE id=$1`, f.branchID).Scan(&no); err != nil {
		t.Fatal(err)
	}
	own := fmt.Sprintf("%d.localhost:5173", no)
	cust := verifiedCustomer(t)

	site := cust.want(200, "", "GET", "/site", nil, "Host", own)
	if site["mode"] != "web" || site["store"].(map[string]any)["slug"] != f.slug {
		t.Fatalf("own page site: %v", site)
	}
	if mall := cust.want(200, "", "GET", "/site", nil); mall["mode"] != "mall" || mall["store"] != nil {
		t.Fatalf("mall site: %v", mall)
	}

	cart := map[string]any{"service_type": "delivery", "zone_id": f.zoneID, "items": []map[string]any{{"item_id": f.itemID, "qty": 1}}}
	// another store cannot be ordered from on this page
	cust.want(404, "NOT_FOUND", "POST", "/stores/"+g.slug+"/quotes", cart, "Host", own)
	// the browser cannot make a mall order look like an own-page one
	cart["own_site"] = true
	q := cust.want(200, "", "POST", "/stores/"+f.slug+"/quotes", cart)
	cust.want(409, "QUOTE_EXPIRED", "POST", "/stores/"+f.slug+"/orders", map[string]any{"quote_id": q["id"],
		"customer_name": "Ann", "address_line": "1 Main"}, "Idempotency-Key", uniq("key"), "Host", own)
	delete(cart, "own_site")
	q = cust.want(200, "", "POST", "/stores/"+f.slug+"/quotes", cart, "Host", own)
	out := cust.want(200, "", "POST", "/stores/"+f.slug+"/orders", map[string]any{"quote_id": q["id"],
		"customer_name": "Ann", "address_line": "1 Main"}, "Idempotency-Key", uniq("key"), "Host", own)
	if out["public_no"] != "Online-1" {
		t.Fatalf("own page order number %v", out["public_no"])
	}
	f.order(t, cust) // a mall order of the same store
	g.order(t, cust) // and one of another store

	if n := len(cust.want(200, "", "GET", "/customer/orders", nil)["orders"].([]any)); n != 3 {
		t.Fatalf("mall lists %d orders", n)
	}
	if n := len(cust.want(200, "", "GET", "/customer/orders", nil, "Host", own)["orders"].([]any)); n != 2 {
		t.Fatalf("own page lists %d orders", n)
	}

	// Caddy may get a certificate for store pages only.
	for host, want := range map[string]int{fmt.Sprintf("%d.localhost", no): 200, "evil.example": 404} {
		resp, err := http.Get(server.URL + "/internal/tls-ask?domain=" + host)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("tls-ask %s: %d", host, resp.StatusCode)
		}
	}

	// The merchant's store link page and a domain request; the platform sees it.
	owner := login(t, f.owner)
	path := fmt.Sprintf("/merchant/branches/%d", f.branchID)
	links := owner.want(200, "", "GET", path+"/site", nil)
	if int(links["store_no"].(float64)) != no {
		t.Fatalf("links %v", links)
	}
	d := owner.want(200, "", "POST", path+"/domains", map[string]any{"kind": "buy", "candidates": "my-shop.test"})
	login(t, g.owner).want(404, "NOT_FOUND", "GET", path+"/site", nil) // not their store
	owner.want(200, "", "DELETE", fmt.Sprintf("%s/domains/%v", path, d["id"]), nil)
}
