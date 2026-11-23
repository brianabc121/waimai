package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

func TestAddressBookOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := login(t, f.owner)
	if err := a.EnsurePlatformAdmin(context.Background(), "admin-test", "password1"); err != nil {
		t.Fatal(err)
	}
	admin := login(t, "admin-test")
	city := fmt.Sprintf("Town-%d", f.branchID)
	owner.want(403, "FORBIDDEN", "GET", "/admin/addresses", nil)
	owner.want(403, "FORBIDDEN", "POST", "/admin/addresses", map[string]any{"line": "x"})

	in := map[string]any{"country_code": "PH", "country": "Philippines", "province": "Cebu", "city": city,
		"line": "IT Park Tower 2", "lat": 10.33, "lng": 123.90}
	x := admin.want(200, "", "POST", "/admin/addresses", in)
	id := fmt.Sprint(x["id"])
	admin.want(409, "DUPLICATE_ADDRESS", "POST", "/admin/addresses", in)
	in["line"] = "IT Park Tower Two"
	admin.want(200, "", "PUT", "/admin/addresses/"+id, in)
	l := admin.want(200, "", "GET", "/admin/addresses?country_code=PH&province=Cebu&city="+city+"&q=tower+two", nil)
	if l["total"].(float64) != 1 {
		t.Fatalf("list: %v", l)
	}
	if l := admin.want(200, "", "GET", "/admin/addresses?city=", nil); l["total"].(float64) < 0 {
		t.Fatalf("unknown region: %v", l)
	}
	r := admin.want(200, "", "GET", "/admin/address-regions", nil)
	if len(r["regions"].([]any)) == 0 {
		t.Fatalf("regions: %v", r)
	}
	admin.want(200, "", "DELETE", "/admin/addresses/"+id, nil)
	admin.want(404, "NOT_FOUND", "DELETE", "/admin/addresses/"+id, nil)
}
