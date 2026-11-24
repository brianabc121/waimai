package httpapi_test

import (
	"fmt"
	"testing"
)

// Posts decide what a person can reach: a station or the packing counter
// opens the kitchen, the rider post allows deliveries, and neither opens the
// back office. Managers run the staff page; owners are out of their reach.
func TestStaffPostsOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := login(t, f.owner)
	staffURL := fmt.Sprintf("/merchant/branches/%d/staff", f.branchID)
	kitchenURL := fmt.Sprintf("/kitchen/branches/%d/board", f.branchID)

	st := owner.want(200, "", "POST", fmt.Sprintf("/merchant/branches/%d/stations", f.branchID), map[string]any{"name": "Fryer", "active": true})
	station := st["id"]

	// A person with a station post, one with no posts, and the legacy accounts.
	cookName, idleName := uniq("cook"), uniq("idle")
	cook := owner.want(200, "", "POST", staffURL, map[string]any{"username": cookName, "password": "password1", "display_name": "Cook",
		"role": "staff", "posts": []map[string]any{{"kind": "station", "station_id": station}}})["id"]
	owner.want(200, "", "POST", staffURL, map[string]any{"username": idleName, "password": "password1", "display_name": "Idle", "role": "staff"})

	c := login(t, cookName)
	me := c.want(200, "", "GET", "/auth/me", nil)
	role := me["roles"].([]any)[0].(map[string]any)
	if posts := role["posts"].([]any); role["role"] != "staff" || len(posts) != 1 || posts[0].(map[string]any)["station_name"] != "Fryer" {
		t.Fatalf("me: %v", role)
	}
	c.want(200, "", "GET", kitchenURL, nil)
	c.want(403, "FORBIDDEN", "GET", staffURL, nil)
	c.want(403, "FORBIDDEN", "GET", fmt.Sprintf("/merchant/branches/%d/orders", f.branchID), nil)
	login(t, idleName).want(403, "FORBIDDEN", "GET", kitchenURL, nil)
	login(t, f.rider).want(403, "FORBIDDEN", "GET", kitchenURL, nil)
	login(t, f.kitchen).want(200, "", "GET", kitchenURL, nil)

	// The rider list is whoever holds the post; giving the cook the post adds them at once.
	riders := func() int {
		return len(owner.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%d/riders", f.branchID), nil)["riders"].([]any))
	}
	if riders() != 1 {
		t.Fatalf("riders = %d", riders())
	}
	owner.want(200, "", "PATCH", fmt.Sprintf("%s/%v", staffURL, cook), map[string]any{
		"posts": []map[string]any{{"kind": "station", "station_id": station}, {"kind": "rider"}}})
	if riders() != 2 {
		t.Fatalf("riders after giving the post = %d", riders())
	}

	// Only a rider delivers: the cashier assigns the order, the post is taken
	// away from under a finished assignment, and the old holder is refused.
	cust := verifiedCustomer(t)
	id, _ := f.order(t, cust)
	desk := login(t, f.cashier)
	o := desk.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil)
	desk.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/accept", id), map[string]any{"expected_version": o["version"]})
	desk.want(422, "VALIDATION", "POST", fmt.Sprintf("/merchant/orders/%d/assign-rider", id), map[string]any{"rider_id": owner.want(200, "", "GET", "/auth/me", nil)["id"]})
	desk.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/assign-rider", id), map[string]any{"rider_id": cook})
	tasks := c.want(200, "", "GET", "/rider/tasks", nil)["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	owner.want(409, "STAFF_HAS_DELIVERIES", "PATCH", fmt.Sprintf("%s/%v", staffURL, cook), map[string]any{"posts": []map[string]any{}})
	owner.want(409, "STAFF_HAS_DELIVERIES", "DELETE", fmt.Sprintf("%s/%v", staffURL, cook), nil)

	// Staff management is for managers; a manager cannot reach the owner.
	desk.want(403, "FORBIDDEN", "POST", staffURL, map[string]any{"username": uniq("x"), "password": "password1", "display_name": "X", "role": "staff"})
	mgrName := uniq("mgr")
	owner.want(200, "", "POST", staffURL, map[string]any{"username": mgrName, "password": "password1", "display_name": "M", "role": "manager"})
	mgr := login(t, mgrName)
	ownerID := owner.want(200, "", "GET", "/auth/me", nil)["id"]
	mgr.want(403, "OWNER_ONLY", "PATCH", fmt.Sprintf("%s/%v", staffURL, ownerID), map[string]any{"display_name": "hacked"})
	mgr.want(403, "OWNER_ONLY", "POST", fmt.Sprintf("%s/%v/password", staffURL, ownerID), map[string]any{"password": "password2"})
	mgr.want(403, "OWNER_ONLY", "POST", staffURL, map[string]any{"username": uniq("o"), "password": "password1", "display_name": "O", "role": "owner"})
	// Only the owner appoints managers (docs/STAFF_ACCESS_DESIGN.md §6.3).
	mgr.want(403, "OWNER_ONLY", "POST", staffURL, map[string]any{"username": uniq("m"), "password": "password1", "display_name": "M2", "role": "manager"})
	mgr.want(403, "OWNER_ONLY", "PATCH", fmt.Sprintf("%s/%v", staffURL, cook), map[string]any{"role": "manager"})
	mgr.want(200, "", "POST", fmt.Sprintf("%s/%v/password", staffURL, cook), map[string]any{"password": "password2"})
	c.want(401, "UNAUTHENTICATED", "GET", "/auth/me", nil) // signed out by the reset

	// Another store's manager sees none of it.
	other := login(t, newFixture(t).owner)
	other.want(404, "NOT_FOUND", "GET", staffURL, nil)
	other.want(404, "NOT_FOUND", "PATCH", fmt.Sprintf("%s/%v", staffURL, cook), map[string]any{"display_name": "x"})
}
