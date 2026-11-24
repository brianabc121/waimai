package httpapi_test

import (
	"fmt"
	"slices"
	"testing"
)

func permsOf(t *testing.T, c *client) []string {
	t.Helper()
	me := c.want(200, "", "GET", "/auth/me", nil)
	out := []string{}
	for _, p := range me["roles"].([]any)[0].(map[string]any)["perms"].([]any) {
		out = append(out, p.(string))
	}
	return out
}

// The owner decides what order takers and managers may do
// (docs/STAFF_ACCESS_DESIGN.md §6): the defaults are what the roles could do
// before, a change applies on the next request, and only the owner sees or
// changes the table.
func TestRolePermissions(t *testing.T) {
	f := newFixture(t)
	owner := login(t, f.owner)
	desk := login(t, f.cashier)
	permsURL := fmt.Sprintf("/merchant/branches/%d/role-perms", f.branchID)
	staffURL := fmt.Sprintf("/merchant/branches/%d/staff", f.branchID)
	mgrName := uniq("mgr")
	owner.want(200, "", "POST", staffURL, map[string]any{"username": mgrName, "password": "password1", "display_name": "M", "role": "manager"})
	mgr := login(t, mgrName)

	// defaults
	if p := permsOf(t, desk); !slices.Contains(p, "order.cancel") || slices.Contains(p, "bills") {
		t.Fatalf("cashier defaults: %v", p)
	}
	if p := permsOf(t, mgr); !slices.Contains(p, "bills") || !slices.Contains(p, "staff.manage") {
		t.Fatalf("manager defaults: %v", p)
	}
	if p := permsOf(t, owner); len(p) != 28 {
		t.Fatalf("owner: %d perms", len(p))
	}
	mgr.want(403, "OWNER_ONLY", "GET", permsURL, nil)
	desk.want(403, "OWNER_ONLY", "GET", permsURL, nil)
	tbl := owner.want(200, "", "GET", permsURL, nil)
	if tbl["cashier_count"].(float64) != 1 || tbl["manager_count"].(float64) != 1 {
		t.Fatalf("counts: %v", tbl)
	}

	cust := verifiedCustomer(t)
	id, _ := f.order(t, cust)
	o := desk.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil)
	desk.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/accept", id), map[string]any{"expected_version": o["version"]})
	billsURL := fmt.Sprintf("/merchant/branches/%d/cash-collections", f.branchID)
	mgr.want(200, "", "GET", billsURL, nil)

	// The owner takes cancelling away from order takers and bills from managers.
	// Order takers keep "see orders" whatever is sent; "manage staff" brings "see staff".
	out := owner.want(200, "", "PUT", permsURL, map[string]any{
		"cashier": []string{"order.handle", "table.use", "member.view"},
		"manager": []string{"order.view", "order.handle", "order.cancel", "staff.manage"},
	})
	cashier := out["cashier"].([]any)
	if len(cashier) != 4 || cashier[0] != "order.view" {
		t.Fatalf("cashier saved: %v", cashier)
	}
	if m := out["manager"].([]any); !slices.Contains(m, any("staff.view")) || slices.Contains(m, any("bills")) {
		t.Fatalf("manager saved: %v", m)
	}
	o = desk.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil)
	desk.want(403, "FORBIDDEN", "POST", fmt.Sprintf("/merchant/orders/%d/cancel", id), map[string]any{"expected_version": o["version"], "reason": "x"})
	desk.want(403, "FORBIDDEN", "GET", fmt.Sprintf("/merchant/branches/%d/chats", f.branchID), nil)
	mgr.want(403, "FORBIDDEN", "GET", billsURL, nil)
	mgr.want(200, "", "GET", staffURL, nil)
	mgr.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/cancel", id), map[string]any{"expected_version": o["version"], "reason": "x"})

	// unknown permissions are refused; only the owner saves
	owner.want(422, "VALIDATION", "PUT", permsURL, map[string]any{"cashier": []string{"root"}, "manager": []string{}})
	mgr.want(403, "OWNER_ONLY", "PUT", permsURL, map[string]any{"cashier": []string{}, "manager": []string{"bills"}})

	// restoring the defaults gives everything back
	def := owner.want(200, "", "GET", permsURL, nil)["catalog"].([]any)
	var c, m []string
	for _, d := range def {
		d := d.(map[string]any)
		if d["cashier"] == true {
			c = append(c, d["code"].(string))
		}
		if d["manager"] == true {
			m = append(m, d["code"].(string))
		}
	}
	owner.want(200, "", "PUT", permsURL, map[string]any{"cashier": c, "manager": m})
	mgr.want(200, "", "GET", billsURL, nil)
	desk.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%d/chats", f.branchID), nil)
}
