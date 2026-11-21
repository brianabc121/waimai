package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// The photo at the door: only a JPEG, only from the rider carrying the order,
// seen by the order desk and by nobody else.
func TestDeliveryProofOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner, rider, kitchen := login(t, f.owner), login(t, f.rider), login(t, f.kitchen)
	id, token := f.order(t, verifiedCustomer(t))
	o := owner.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/accept", id), map[string]any{"expected_version": 1})
	o = owner.want(200, "", "POST", fmt.Sprintf("/merchant/orders/%d/assign-rider", id), map[string]any{"rider_id": f.riderID, "expected_version": o["version"]})
	task := int64(o["delivery"].(map[string]any)["id"].(float64))
	proofURL := fmt.Sprintf("/rider/tasks/%d/proof", task)

	send := func(c *client, body []byte) (int, string) {
		req, _ := http.NewRequest("POST", server.URL+"/api/v1"+proofURL, bytes.NewReader(body))
		req.Header.Set("X-Requested-With", "test")
		req.Header.Set("X-Forwarded-For", c.ip)
		req.Header.Set("Content-Type", "application/octet-stream")
		for k, v := range c.cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, errCode(out)
	}
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{7}, 2000)...)

	if st, code := send(rider, jpeg); st != 409 || code != "INVALID_STATE" {
		t.Fatalf("a photo before pickup: %d %s", st, code)
	}
	for _, it := range o["items"].([]any) {
		owner.want(200, "", "POST", fmt.Sprintf("/kitchen/items/%v/ready", it.(map[string]any)["id"]), nil)
	}
	o = owner.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil)
	owner.want(200, "", "POST", fmt.Sprintf("/kitchen/orders/%d/pack", id), map[string]any{"bags": 1, "manifest_version": o["manifest_version"]})
	rider.want(200, "", "POST", fmt.Sprintf("/rider/tasks/%d/pickup", task), nil)

	if st, code := send(rider, []byte("GIF89a not a jpeg")); st != 422 || code != "VALIDATION" {
		t.Fatalf("not a JPEG: %d %s", st, code)
	}
	if st, code := send(rider, append(jpeg, make([]byte, 700<<10)...)); st != 413 || code != "FILE_TOO_LARGE" {
		t.Fatalf("too large: %d %s", st, code)
	}
	if st, code := send(owner, jpeg); st != 404 || code != "NOT_ASSIGNED" {
		t.Fatalf("someone else's delivery: %d %s", st, code)
	}
	if st, code := send(rider, jpeg); st != 200 {
		t.Fatalf("the photo: %d %s", st, code)
	}
	o = owner.want(200, "", "GET", fmt.Sprintf("/merchant/orders/%d", id), nil)
	if o["delivery"].(map[string]any)["proof_at"] == nil {
		t.Fatalf("the desk should know there is a photo: %v", o["delivery"])
	}

	fetch := func(c *client, path string) (int, string, int) {
		req, _ := http.NewRequest("GET", server.URL+"/api/v1"+path, nil)
		req.Header.Set("X-Forwarded-For", c.ip)
		for k, v := range c.cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), len(b)
	}
	deskURL := fmt.Sprintf("/merchant/orders/%d/proof", id)
	if st, ct, n := fetch(owner, deskURL); st != 200 || ct != "image/jpeg" || n != len(jpeg) {
		t.Fatalf("the desk's copy: %d %s %d bytes", st, ct, n)
	}
	if st, _, n := fetch(rider, proofURL); st != 200 || n != len(jpeg) {
		t.Fatalf("the rider's copy: %d, %d bytes", st, n)
	}
	if st, _, _ := fetch(kitchen, deskURL); st != 403 {
		t.Fatalf("the kitchen must not see the photo: %d", st)
	}
	if st, _, _ := fetch(newClient(t), deskURL); st != 401 {
		t.Fatalf("a stranger must not see the photo: %d", st)
	}
	if st, _, _ := fetch(login(t, newFixture(t).owner), deskURL); st != 403 && st != 404 {
		t.Fatalf("another store must not see the photo: %d", st)
	}
	// The customer's order shows that a photo exists, never the photo itself.
	if c := newClient(t).want(200, "", "GET", "/customer/orders/"+token, nil); c["delivery"].(map[string]any)["proof_at"] == nil {
		t.Fatalf("customer order: %v", c["delivery"])
	}
}
