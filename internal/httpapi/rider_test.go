package httpapi_test

import (
	"fmt"
	"testing"
	"time"
)

// The app signs in with a token instead of a cookie, clocks on, reports its
// position and registers for push; signing out ends all of it.
func TestRiderAppOverHTTP(t *testing.T) {
	f := newFixture(t)
	if cfg := newClient(t).want(200, "", "GET", "/config", nil); cfg["push_key"] != "" {
		t.Fatalf("web push is not set up in tests: %v", cfg)
	}

	app := newClient(t)
	app.want(401, "BAD_CREDENTIALS", "POST", "/auth/token", map[string]string{"username": f.rider, "password": "nope"})
	out := app.want(200, "", "POST", "/auth/token", map[string]string{"username": f.rider, "password": "password1"})
	token, _ := out["token"].(string)
	if len(token) < 30 || out["user"].(map[string]any)["username"] != f.rider || len(app.cookies) != 0 {
		t.Fatalf("token login: %v, cookies %v", out, app.cookies)
	}
	app.want(401, "UNAUTHENTICATED", "GET", "/rider/duty", nil)
	app.want(401, "UNAUTHENTICATED", "GET", "/rider/duty", nil, "Authorization", "Bearer not-a-token")
	auth := []string{"Authorization", "Bearer " + token}
	if st := app.want(200, "", "GET", "/rider/duty", nil, auth...); st["on_duty"] != false {
		t.Fatalf("duty before clocking on: %v", st)
	}
	// Writes still need the header a cross-site form cannot set.
	app.want(403, "CSRF", "POST", "/rider/duty", map[string]any{"on_duty": true}, append(auth, "X-Requested-With", "")...)
	app.want(422, "VALIDATION", "POST", "/rider/duty", map[string]any{}, auth...)

	now := time.Now().UTC().Format(time.RFC3339)
	points := map[string]any{"points": []map[string]any{{"lat": 3.139, "lng": 101.6869, "accuracy": 9.5, "battery": 77, "at": now}}}
	app.want(409, "OFF_DUTY", "POST", "/rider/locations", points, auth...)
	if st := app.want(200, "", "POST", "/rider/duty", map[string]any{"on_duty": true}, auth...); st["on_duty"] != true {
		t.Fatalf("clocking on: %v", st)
	}
	if got := app.want(200, "", "POST", "/rider/locations", points, auth...); got["accepted"].(float64) != 1 {
		t.Fatalf("locations: %v", got)
	}
	if st := app.want(200, "", "GET", "/rider/duty", nil, auth...); st["lat"].(float64) != 3.139 || st["battery"].(float64) != 77 {
		t.Fatalf("position: %v", st)
	}

	app.want(422, "VALIDATION", "POST", "/rider/devices", map[string]any{"platform": "web", "token": "x"}, auth...)
	dev := app.want(200, "", "POST", "/rider/devices", map[string]any{"platform": "android", "token": uniq("fcm"), "app_version": "1.0"}, auth...)
	if dev["id"].(float64) < 1 || dev["token"] != nil {
		t.Fatalf("device: %v", dev)
	}
	extra := app.want(200, "", "POST", "/rider/devices", map[string]any{"platform": "ios", "token": uniq("apn")}, auth...)
	app.want(200, "", "DELETE", fmt.Sprintf("/rider/devices/%v", extra["id"]), nil, auth...)

	// The store sees who is on duty; someone without the rider post cannot clock on.
	owner := login(t, f.owner)
	riders := owner.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%d/riders", f.branchID), nil)["riders"].([]any)
	if len(riders) != 1 || riders[0].(map[string]any)["on_duty"] != true {
		t.Fatalf("riders: %v", riders)
	}
	owner.want(403, "NOT_RIDER", "POST", "/rider/duty", map[string]any{"on_duty": true})

	app.want(200, "", "POST", "/auth/logout", nil, auth...)
	app.want(401, "UNAUTHENTICATED", "GET", "/rider/duty", nil, auth...)
	riders = owner.want(200, "", "GET", fmt.Sprintf("/merchant/branches/%d/riders", f.branchID), nil)["riders"].([]any)
	if riders[0].(map[string]any)["on_duty"] != false {
		t.Fatalf("signing out should end the shift: %v", riders)
	}
}

// The rider list with positions is for the order desk only; the customer's
// tracking link answers by order token and gives nothing away before pickup.
func TestRiderBoardAndTrackingOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner, rider := login(t, f.owner), login(t, f.rider)
	ridersURL := fmt.Sprintf("/merchant/branches/%d/riders", f.branchID)
	rider.want(403, "FORBIDDEN", "GET", ridersURL, nil)

	rider.want(200, "", "POST", "/rider/duty", map[string]any{"on_duty": true})
	rider.want(200, "", "POST", "/rider/locations", map[string]any{"points": []map[string]any{
		{"lat": 14.55, "lng": 121.02, "at": time.Now().UTC().Format(time.RFC3339)}}})
	out := owner.want(200, "", "GET", ridersURL, nil)
	r := out["riders"].([]any)[0].(map[string]any)
	if r["contact"] != "ok" || r["lat"].(float64) != 14.55 || out["store"] != nil {
		t.Fatalf("rider board: %v", out)
	}

	id, token := f.order(t, verifiedCustomer(t))
	anon := newClient(t)
	anon.want(404, "NOT_FOUND", "GET", "/customer/orders/nope/tracking", nil)
	if tr := anon.want(200, "", "GET", "/customer/orders/"+token+"/tracking", nil); tr["active"] != false || tr["lat"] != nil {
		t.Fatalf("tracking before pickup: %v", tr)
	}
	o := owner.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/accept", id), map[string]any{"expected_version": 1})
	o = owner.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/assign-rider", id), map[string]any{"rider_id": f.riderID, "expected_version": o["version"]})
	task := o["delivery"].(map[string]any)
	if task["seen_at"] != nil {
		t.Fatalf("a new assignment is unseen: %v", task)
	}
	rider.want(200, "", "POST", "/rider/seen", map[string]any{"ids": []any{task["id"]}})
	o = owner.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil)
	if o["delivery"].(map[string]any)["seen_at"] == nil {
		t.Fatalf("the desk should see that the rider looked: %v", o["delivery"])
	}
}
