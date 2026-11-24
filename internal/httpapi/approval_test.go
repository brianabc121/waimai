package httpapi_test

import (
	"context"
	"fmt"
	"testing"
)

// Manager approval (docs/STAFF_ACCESS_DESIGN.md §7): an order taker whose role
// may not cancel an accepted order asks a manager, who enters their own code;
// the approval is good for that one action, and wrong codes lock it.
func TestManagerApproval(t *testing.T) {
	f := newFixture(t)
	bid := f.branchID
	owner := login(t, f.owner)

	// The owner takes cancelling away from order takers.
	tb := owner.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%d/role-perms", bid), nil)
	cashier := []string{}
	for _, p := range tb["cashier"].([]any) {
		if p != "order.cancel" {
			cashier = append(cashier, p.(string))
		}
	}
	owner.want(200, "", "PUT", fmt.Sprintf("/merchant/branches/%d/role-perms", bid), map[string]any{"cashier": cashier, "manager": tb["manager"]})

	// Codes are set with the password, and are 6 digits.
	owner.want(401, "BAD_PASSWORD", "PUT", "/auth/approve-pin", map[string]string{"password": "nope", "pin": "246810"})
	owner.want(422, "VALIDATION", "PUT", "/auth/approve-pin", map[string]string{"password": "password1", "pin": "12ab"})
	owner.want(200, "", "PUT", "/auth/approve-pin", map[string]string{"password": "password1", "pin": "246810"})
	if me := owner.want(200, "", "GET", "/auth/me", nil); me["has_approve_pin"] != true {
		t.Fatalf("has_approve_pin: %v", me["has_approve_pin"])
	}
	mgrName := uniq("mgr")
	mgrID := owner.want(200, "", "POST", fmt.Sprintf("/merchant/branches/%d/staff", bid), map[string]any{"username": mgrName, "password": "password1", "display_name": "Chen", "role": "manager"})["id"]
	login(t, mgrName).want(200, "", "PUT", "/auth/approve-pin", map[string]string{"password": "password1", "pin": "135790"})

	desk := login(t, f.cashier)
	list := desk.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%d/approvers?perm=order.cancel", bid), nil)["approvers"].([]any)
	if len(list) != 2 {
		t.Fatalf("approvers: %v", list)
	}
	desk.want(422, "VALIDATION", "GET", fmt.Sprintf("/merchant/branches/%d/approvers?perm=menu.edit", bid), nil)

	accepted := func() (int64, any) {
		id, _ := f.order(t, verifiedCustomer(t))
		o := desk.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil)
		o = desk.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/accept", id), map[string]any{"expected_version": o["version"]})
		return id, o["version"]
	}
	id, ver := accepted()
	cancel := func(id int64, ver any, status int, code, token string) {
		t.Helper()
		desk.want(status, code, "POST", fmt.Sprintf("/merchant/orders/%d/cancel", id), map[string]any{"expected_version": ver, "reason": "customer called"}, "X-Approval", token)
	}
	desk.want(403, "FORBIDDEN", "POST", fmt.Sprintf("/merchant/orders/%d/cancel", id), map[string]any{"expected_version": ver, "reason": "x"})

	approvals := fmt.Sprintf("/merchant/branches/%d/approvals", bid)
	ask := func(pin string) map[string]any {
		return map[string]any{"approver_id": mgrID, "perm": "order.cancel", "pin": pin}
	}
	desk.want(422, "VALIDATION", "POST", approvals, map[string]any{"approver_id": mgrID, "perm": "menu.edit", "pin": "135790"})
	desk.want(422, "VALIDATION", "POST", approvals, map[string]any{"approver_id": desk.want(200, "", "GET", "/auth/me", nil)["id"], "perm": "order.cancel", "pin": "135790"})
	if out := desk.want(401, "BAD_PIN", "POST", approvals, ask("000000")); out["error"].(map[string]any)["details"].(map[string]any)["left"].(float64) != 4 {
		t.Fatalf("tries left: %v", out)
	}
	ok := desk.want(200, "", "POST", approvals, ask("135790"))
	if ok["approver_name"] != "Chen" {
		t.Fatalf("approval: %v", ok)
	}
	token := ok["token"].(string)
	cancel(id, ver, 200, "", token)

	var role string
	var by int64
	if err := a.DB.QueryRow(context.Background(), `SELECT actor_role, approved_by FROM audit_logs WHERE order_id=$1 AND approved_by IS NOT NULL`, id).Scan(&role, &by); err != nil ||
		role != "cashier" || fmt.Sprint(by) != fmt.Sprint(mgrID) {
		t.Fatalf("audit: %q %d %v", role, by, err)
	}

	// The approval was for one action only.
	id2, ver2 := accepted()
	cancel(id2, ver2, 403, "APPROVAL_INVALID", token)

	// Five wrong codes lock the manager's code for a while, even the right one.
	for i := 0; i < 4; i++ {
		desk.want(401, "BAD_PIN", "POST", approvals, ask("111111"))
	}
	desk.want(429, "APPROVAL_LOCKED", "POST", approvals, ask("111111"))
	desk.want(429, "APPROVAL_LOCKED", "POST", approvals, ask("135790"))
	// The owner is still there to approve.
	ok = desk.want(200, "", "POST", approvals, map[string]any{"approver_id": owner.want(200, "", "GET", "/auth/me", nil)["id"], "perm": "order.cancel", "pin": "246810"})
	cancel(id2, ver2, 200, "", ok["token"].(string))
}
