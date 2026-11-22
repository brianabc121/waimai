package httpapi_test

import (
	"fmt"
	"strings"
	"testing"
)

// The dish page: more pictures and a longer text, set by the merchant and
// shown on the storefront.
func TestItemPhotosAndDetails(t *testing.T) {
	f := newFixture(t)
	owner := login(t, f.owner)
	base := fmt.Sprintf("/merchant/branches/%d", f.branchID)
	menu := owner.want(200, "", "GET", base+"/menu", nil)
	cat := menu["categories"].([]any)[0].(map[string]any)["id"]
	item := func(photos []string, details string) map[string]any {
		return map[string]any{"category_id": cat, "name": "tea", "price": 6900, "published": true, "needs_prep": true,
			"delivery_available": true, "pickup_available": true, "photos": photos, "details": details}
	}
	path := fmt.Sprintf("%s/items/%d", base, f.itemID)
	owner.want(200, "", "PUT", path, item([]string{"/demo/a.jpg", " ", "/demo/b.jpg"}, "Leaves: Assam\nCup: 500 ml"))

	sf := newClient(t).want(200, "", "GET", "/stores/"+f.slug, nil)
	got := sf["categories"].([]any)[0].(map[string]any)["items"].([]any)[0].(map[string]any)
	if p := got["photos"].([]any); len(p) != 2 || p[0] != "/demo/a.jpg" || p[1] != "/demo/b.jpg" {
		t.Fatalf("photos on the storefront: %v", got["photos"])
	}
	if got["details"] != "Leaves: Assam\nCup: 500 ml" {
		t.Fatalf("details on the storefront: %v", got["details"])
	}

	many := make([]string, 9)
	for i := range many {
		many[i] = fmt.Sprintf("/demo/%d.jpg", i)
	}
	owner.want(422, "VALIDATION", "PUT", path, item(many, ""))
	owner.want(422, "VALIDATION", "PUT", path, item([]string{"javascript:alert(1)"}, ""))
	owner.want(422, "VALIDATION", "PUT", path, item(nil, strings.Repeat("长", 2001)))

	// Saving without the new fields (an older client) clears them, like every other field.
	owner.want(200, "", "PUT", path, item(nil, ""))
	sf = newClient(t).want(200, "", "GET", "/stores/"+f.slug, nil)
	got = sf["categories"].([]any)[0].(map[string]any)["items"].([]any)[0].(map[string]any)
	if len(got["photos"].([]any)) != 0 || got["details"] != "" {
		t.Fatalf("after clearing: %v", got)
	}
}
