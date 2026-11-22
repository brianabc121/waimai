package httpapi_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestMallOverHTTP covers the marketplace: which stores a spot gets, what a
// card says, that nothing about delivery areas leaves the server, and how the
// merchant and the platform take a store off.
func TestMallOverHTTP(t *testing.T) {
	// A spot of the map no other test and no earlier run uses, so only these stores are near.
	lat0, lng0 := -20-float64(time.Now().UnixNano()%20000)/1000, 60+float64(time.Now().UnixNano()/20000%20000)/1000
	near, far := newFixture(t), newFixture(t)
	owner, cashier, farOwner := login(t, near.owner), login(t, near.cashier), login(t, far.owner)
	anon, admin := newClient(t), adminClient(t)
	city := uniq("City")
	region := map[string]any{"country_code": "PH", "country": "Philippines", "province": "Cebu", "city": city, "district": "Lahug"}
	set := func(c *client, bid int64, lat, lng float64) {
		b := "/merchant/branches/" + fmt.Sprint(bid)
		c.want(200, "", "PUT", b+"/location", map[string]any{"lat": lat, "lng": lng, "address": "1 Mall St", "region": region})
		c.want(200, "", "PUT", b+"/zone-mode", map[string]any{"zone_mode": "map", "zone_tolerance_m": 200})
	}
	set(owner, near.branchID, lat0, lng0)
	// the far store is 3 km north and delivers only around itself
	set(farOwner, far.branchID, lat0+0.027, lng0)
	zone := func(lat, lng float64, fee int) map[string]any {
		return map[string]any{"name": "secret area", "fee": fee, "min_order": 20000, "active": true, "eta_minutes": 25, "color": "#2a78d6",
			"polygon": [][]float64{{lat - 0.005, lng - 0.005}, {lat - 0.005, lng + 0.005}, {lat + 0.005, lng + 0.005}, {lat + 0.005, lng - 0.005}}}
	}
	owner.want(200, "", "POST", fmt.Sprintf("/merchant/branches/%d/zones", near.branchID), zone(lat0, lng0, 4900))
	farOwner.want(200, "", "POST", fmt.Sprintf("/merchant/branches/%d/zones", far.branchID), zone(lat0+0.027, lng0, 5900))

	list := func(c *client, query string) (stores, pickup map[string]map[string]any, out map[string]any) {
		t.Helper()
		out = c.want(200, "", "GET", "/mall/stores?"+query, nil)
		index := func(key string) map[string]map[string]any {
			m := map[string]map[string]any{}
			for _, x := range out[key].([]any) {
				s := x.(map[string]any)
				m[s["slug"].(string)] = s
			}
			return m
		}
		raw, _ := json.Marshal(out)
		for _, word := range []string{"secret area", "polygon", "zone"} {
			if strings.Contains(string(raw), word) {
				t.Fatalf("the marketplace tells customers about delivery areas (%q): %s", word, raw)
			}
		}
		return index("stores"), index("pickup"), out
	}
	here := fmt.Sprintf("lat=%f&lng=%f", lat0+0.002, lng0+0.002)

	// A spot inside the near store's area: it delivers, the far one is close enough to collect from.
	stores, pickup, out := list(anon, here)
	s := stores[near.slug]
	if s == nil || len(stores) != 1 || pickup[far.slug] == nil {
		t.Fatalf("stores at the spot: %v", out)
	}
	d := s["delivery"].(map[string]any)
	if d["fee"] != float64(4900) || d["min_order"] != float64(20000) || d["eta_minutes"] != float64(25) || len(d) != 3 {
		t.Fatalf("delivery terms: %v", d)
	}
	if s["status"] != "open" || s["rating"] != nil || s["distance_m"].(float64) < 200 || s["distance_m"].(float64) > 400 || s["currency"] != "PHP" {
		t.Fatalf("card: %v", s)
	}
	if pickup[far.slug]["delivery"] != nil {
		t.Fatalf("a store that does not deliver here shows delivery terms: %v", pickup[far.slug])
	}
	if r := out["region"].(map[string]any); r["city"] != city || r["country_code"] != "PH" {
		t.Fatalf("region of the spot: %v", r)
	}
	if len(out["frequent"].([]any)) != 0 {
		t.Fatalf("frequent stores for nobody: %v", out["frequent"])
	}

	// Too far for either: nothing, though the region is still known.
	stores, pickup, out = list(anon, fmt.Sprintf("lat=%f&lng=%f", lat0+0.2, lng0))
	if len(stores) != 0 || len(pickup) != 0 || out["region"] == nil {
		t.Fatalf("far from every store: %v", out)
	}
	if _, _, out = list(anon, "lat=60&lng=10"); out["region"] != nil || len(out["stores"].([]any)) != 0 {
		t.Fatalf("another continent: %v", out)
	}
	anon.want(422, "VALIDATION", "GET", "/mall/stores?lat=95&lng=0", nil)
	anon.want(422, "VALIDATION", "GET", "/mall/stores?lat=abc&lng=0", nil)
	anon.want(422, "VALIDATION", "GET", "/mall/stores", nil)

	// By city: both stores, without delivery terms.
	stores, _, _ = list(anon, "country=PH&city="+city)
	if len(stores) != 2 || stores[near.slug]["delivery"] != nil || stores[near.slug]["min_order_from"] != float64(20000) {
		t.Fatalf("stores of the city: %v", stores)
	}
	if stores, _, _ = list(anon, "country=PH&city="+city+"&district=Nowhere"); len(stores) != 0 {
		t.Fatalf("stores of an empty district: %v", stores)
	}
	found := false
	for _, c := range anon.want(200, "", "GET", "/mall/regions", nil)["countries"].([]any) {
		for _, ci := range c.(map[string]any)["cities"].([]any) {
			if x := ci.(map[string]any); x["city"] == city {
				found = x["stores"] == float64(2) && len(x["districts"].([]any)) == 1
			}
		}
	}
	if !found {
		t.Fatal("the city is missing from the regions")
	}

	// Search: by a dish, and with nothing to find.
	if stores, _, _ = list(anon, here+"&q=tea"); len(stores) != 1 || len(stores[near.slug]["dishes"].([]any)) != 1 {
		t.Fatalf("search by dish: %v", stores)
	}
	if stores, pickup, _ = list(anon, here+"&q=no-such-thing"); len(stores)+len(pickup) != 0 {
		t.Fatalf("search with no match: %v %v", stores, pickup)
	}

	// Categories: the merchant picks up to two, customers filter by them.
	b := fmt.Sprintf("/merchant/branches/%d/mall", near.branchID)
	cashier.want(403, "FORBIDDEN", "PUT", b, map[string]any{"listed": true})
	farOwner.want(404, "NOT_FOUND", "GET", b, nil)
	l := owner.want(200, "", "GET", b, nil)
	if l["listed"] != true || l["has_location"] != true || l["has_zones"] != true || l["has_logo"] != false || len(l["categories"].([]any)) != 7 {
		t.Fatalf("listing: %v", l)
	}
	cats := l["categories"].([]any)
	id := func(i int) any { return cats[i].(map[string]any)["id"] }
	code := cats[0].(map[string]any)["code"].(string)
	owner.want(422, "VALIDATION", "PUT", b, map[string]any{"listed": true, "category_ids": []any{id(0), id(1), id(2)}})
	owner.want(422, "VALIDATION", "PUT", b, map[string]any{"listed": true, "category_ids": []any{999999}})
	l = owner.want(200, "", "PUT", b, map[string]any{"listed": true, "tagline": " Fresh noodles ", "category_ids": []any{id(0), id(1)}})
	if l["tagline"] != "Fresh noodles" || len(l["category_ids"].([]any)) != 2 {
		t.Fatalf("saved listing: %v", l)
	}
	if stores, _, _ = list(anon, here+"&category="+code); len(stores) != 1 || stores[near.slug]["tagline"] != "Fresh noodles" {
		t.Fatalf("stores of the category: %v", stores)
	}
	if stores, _, _ = list(anon, here+"&category=dessert"); len(stores) != 0 {
		t.Fatalf("stores of another category: %v", stores)
	}

	// Someone who keeps a store finds it among "frequent".
	cust := verifiedCustomer(t)
	cust.want(200, "", "PUT", "/customer/favorites/"+near.slug, map[string]any{})
	if _, _, out = list(cust, here); len(out["frequent"].([]any)) != 1 || out["frequent"].([]any)[0] != near.slug {
		t.Fatalf("frequent stores: %v", out["frequent"])
	}

	// The merchant opts out; the store's own page keeps working.
	owner.want(200, "", "PUT", b, map[string]any{"listed": false, "category_ids": []any{id(0)}})
	if stores, _, _ = list(anon, here); len(stores) != 0 {
		t.Fatalf("a store that opted out is listed: %v", stores)
	}
	anon.want(200, "", "GET", "/stores/"+near.slug, nil)
	owner.want(200, "", "PUT", b, map[string]any{"listed": true, "category_ids": []any{id(0)}})

	// The platform takes it off, and sees why a store does not show.
	anon.want(401, "UNAUTHENTICATED", "GET", "/admin/mall", nil)
	owner.want(403, "FORBIDDEN", "GET", "/admin/mall", nil)
	hide := fmt.Sprintf("/admin/mall/stores/%d", near.branchID)
	owner.want(403, "FORBIDDEN", "PUT", hide, map[string]any{"hidden": true})
	admin.want(200, "", "PUT", hide, map[string]any{"hidden": true})
	if stores, _, _ = list(anon, here); len(stores) != 0 {
		t.Fatalf("a store the platform took off is listed: %v", stores)
	}
	state := ""
	for _, x := range admin.want(200, "", "GET", "/admin/mall", nil)["stores"].([]any) {
		if st := x.(map[string]any); st["slug"] == near.slug {
			state = st["state"].(string)
		}
	}
	if state != "hidden" {
		t.Fatalf("state of a hidden store: %q", state)
	}
	if l = owner.want(200, "", "GET", b, nil); l["hidden"] != true {
		t.Fatalf("the merchant is not told the platform took the store off: %v", l)
	}
	admin.want(200, "", "PUT", hide, map[string]any{"hidden": false})
	if stores, _, _ = list(anon, here); len(stores) != 1 {
		t.Fatalf("a store put back is missing: %v", stores)
	}

	// Banners.
	admin.want(422, "VALIDATION", "POST", "/admin/mall/banners", map[string]any{"title": ""})
	admin.want(422, "VALIDATION", "POST", "/admin/mall/banners", map[string]any{"title": "x", "link_slug": "no-such-store"})
	admin.want(422, "VALIDATION", "POST", "/admin/mall/banners", map[string]any{"title": "x", "link_url": "javascript:alert(1)"})
	bid := admin.want(200, "", "POST", "/admin/mall/banners", map[string]any{"title": "Grand opening", "link_slug": near.slug, "active": true})["id"]
	home := anon.want(200, "", "GET", "/mall/home", nil)
	seen := false
	for _, x := range home["banners"].([]any) {
		if bn := x.(map[string]any); bn["id"] == bid {
			seen = bn["link_slug"] == near.slug && bn["image_url"] == ""
		}
	}
	if !seen || len(home["categories"].([]any)) != 7 {
		t.Fatalf("home: %v", home)
	}
	admin.want(200, "", "DELETE", fmt.Sprintf("/admin/mall/banners/%v", bid), nil)
	admin.want(404, "NOT_FOUND", "DELETE", fmt.Sprintf("/admin/mall/banners/%v", bid), nil)
}
