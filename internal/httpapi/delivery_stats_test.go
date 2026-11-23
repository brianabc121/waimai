package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

func TestDeliveryStatsOverHTTP(t *testing.T) {
	f1, f2 := newFixture(t), newFixture(t)
	b1 := fmt.Sprint(f1.branchID)
	owner1, owner2, cashier, kitchen := login(t, f1.owner), login(t, f2.owner), login(t, f1.cashier), login(t, f1.kitchen)
	const nov = "from=2026-11-01&to=2026-11-30"

	l := cashier.want(200, "", "GET", "/merchant/branches/"+b1+"/customer-addresses", nil)
	if l["items"] == nil {
		t.Fatalf("list: %v", l)
	}
	owner1.want(200, "", "GET", "/merchant/branches/"+b1+"/delivery-stats/top?"+nov, nil)
	owner1.want(200, "", "GET", "/merchant/branches/"+b1+"/delivery-stats/top-detail?key=x&"+nov, nil)
	p := owner1.want(200, "", "GET", "/merchant/branches/"+b1+"/delivery-stats/points?"+nov, nil)
	if p["points"] == nil || p["zones"] == nil {
		t.Fatalf("points: %v", p)
	}
	owner1.want(400, "RANGE_TOO_LONG", "GET", "/merchant/branches/"+b1+"/delivery-stats/top?from=2026-10-01&to=2026-11-30", nil)
	owner1.want(400, "BAD_RANGE", "GET", "/merchant/branches/"+b1+"/delivery-stats/points", nil)
	owner1.want(200, "", "GET", "/merchant/branches/"+b1+"/customer-addresses/orders?cust=x&key=y&unit=", nil)

	// another store's staff, and the kitchen, cannot look
	owner2.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/customer-addresses", nil)
	owner2.want(404, "NOT_FOUND", "GET", "/merchant/branches/"+b1+"/delivery-stats/points?"+nov, nil)
	kitchen.want(403, "FORBIDDEN", "GET", "/merchant/branches/"+b1+"/delivery-stats/top?"+nov, nil)

	if err := a.EnsurePlatformAdmin(context.Background(), "admin-test", "password1"); err != nil {
		t.Fatal(err)
	}
	admin := login(t, "admin-test")
	owner1.want(403, "FORBIDDEN", "GET", "/admin/delivery-addresses", nil)
	admin.want(200, "", "GET", "/admin/delivery-addresses?page_size=5", nil)
	admin.want(200, "", "GET", "/admin/delivery-addresses?branch_id="+b1, nil)
	admin.want(200, "", "GET", "/admin/delivery-stats/top?country=PH&"+nov, nil)
	admin.want(200, "", "GET", "/admin/delivery-stats/points?merchant_id=1&"+nov, nil)
	admin.want(200, "", "GET", "/admin/delivery-addresses/regions", nil)
	admin.want(404, "NOT_FOUND", "GET", "/admin/delivery-addresses/orders?cust=x&key=y", nil)
	admin.want(200, "", "GET", "/admin/delivery-addresses/orders?branch_id="+b1+"&cust=x&key=y", nil)
}
