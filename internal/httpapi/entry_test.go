package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

// Signing in says where to (docs/STAFF_ACCESS_DESIGN.md §5): a workbench
// sign-in cannot reach the back office until the password is entered again,
// people without a back-office role get the workbench, and the platform signs
// in on its own page with its own cookie.
func TestSignInEntries(t *testing.T) {
	f := newFixture(t)
	orders := fmt.Sprintf("/merchant/branches/%d/orders", f.branchID)
	stations := fmt.Sprintf("/merchant/branches/%d/stations", f.branchID)

	// The owner on the workbench: the workbench's own calls work, the back office does not.
	c := newClient(t)
	me := c.want(200, "", "POST", "/auth/login", map[string]string{"username": f.owner, "password": "password1", "entry": "work"})
	if me["entry"] != "work" {
		t.Fatalf("entry = %v", me["entry"])
	}
	c.want(200, "", "GET", stations, nil)
	c.want(403, "WORK_SESSION", "GET", orders, nil)
	c.want(401, "BAD_PASSWORD", "POST", "/auth/entry", map[string]string{"entry": "office", "password": "nope"})
	c.want(403, "WORK_SESSION", "GET", orders, nil)
	if me = c.want(200, "", "POST", "/auth/entry", map[string]string{"entry": "office", "password": "password1"}); me["entry"] != "office" {
		t.Fatalf("entry after switching = %v", me["entry"])
	}
	c.want(200, "", "GET", orders, nil)
	// Going to the workbench needs no password, and closes the back office again.
	c.want(200, "", "POST", "/auth/entry", map[string]string{"entry": "work"})
	c.want(403, "WORK_SESSION", "GET", orders, nil)
	c.want(422, "VALIDATION", "POST", "/auth/entry", map[string]string{"entry": "admin"})

	// Someone with only posts asks for the back office and gets the workbench.
	k := newClient(t)
	if me = k.want(200, "", "POST", "/auth/login", map[string]string{"username": f.kitchen, "password": "password1"}); me["entry"] != "work" {
		t.Fatalf("kitchen entry = %v", me["entry"])
	}
	k.want(403, "NO_OFFICE_ROLE", "POST", "/auth/entry", map[string]string{"entry": "office", "password": "password1"})
	k.want(200, "", "GET", fmt.Sprintf("/kitchen/branches/%d/board", f.branchID), nil)
	k.want(403, "FORBIDDEN", "GET", orders, nil)

	// The platform and the stores do not share a sign-in page or a cookie.
	if err := a.EnsurePlatformAdmin(context.Background(), "admin-test", "password1"); err != nil {
		t.Fatal(err)
	}
	newClient(t).want(401, "BAD_CREDENTIALS", "POST", "/auth/login", map[string]string{"username": "admin-test", "password": "password1"})
	newClient(t).want(401, "BAD_CREDENTIALS", "POST", "/admin/auth/login", map[string]string{"username": f.owner, "password": "password1"})
	owner := login(t, f.owner)
	owner.want(401, "UNAUTHENTICATED", "GET", "/admin/merchants", nil)
	admin := adminLogin(t, "admin-test")
	admin.want(200, "", "GET", "/admin/merchants", nil)
	admin.want(200, "", "GET", orders, nil) // the read-only view of a store
	admin.want(403, "READ_ONLY", "POST", stations, map[string]any{"name": "X", "active": true})
	admin.want(422, "VALIDATION", "POST", "/auth/entry", map[string]string{"entry": "office", "password": "password1"})
	admin.want(200, "", "POST", "/auth/logout", nil) // the store's sign-out leaves the platform's alone
	admin.want(200, "", "GET", "/admin/merchants", nil)
	admin.want(200, "", "POST", "/admin/auth/logout", nil)
	admin.want(401, "UNAUTHENTICATED", "GET", "/admin/merchants", nil)

	// What a person does is logged with the role they had.
	cust := verifiedCustomer(t)
	id, _ := f.order(t, cust)
	desk := login(t, f.cashier)
	o := desk.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil)
	desk.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/accept", id), map[string]any{"expected_version": o["version"]})
	var role string
	if err := a.DB.QueryRow(context.Background(), `SELECT actor_role FROM audit_logs WHERE order_id=$1 AND action LIKE '%accept%' ORDER BY id DESC LIMIT 1`, id).Scan(&role); err != nil || role != "cashier" {
		t.Fatalf("actor_role = %q, %v", role, err)
	}
}
