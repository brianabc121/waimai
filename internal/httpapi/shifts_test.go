package httpapi_test

import (
	"fmt"
	"testing"
	"time"
)

// Clocking on over HTTP, the manager's attendance page, and who may see it.
func TestAttendanceOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner, cashier, kitchen := login(t, f.owner), login(t, f.cashier), login(t, f.kitchen)
	today := time.Now().In(time.FixedZone("", 8*3600)).Format("2006-01-02")
	page := fmt.Sprintf("/merchant/branches/%d/attendance", f.branchID)
	period := page + "?from=" + today + "&to=" + today

	st := kitchen.want(200, "", "GET", "/work/shift", nil)
	if st["on_duty"] != false {
		t.Fatalf("not at work yet: %v", st)
	}
	kitchen.want(422, "VALIDATION", "POST", "/work/shift", map[string]any{"branch_id": f.branchID})
	kitchen.want(403, "FORBIDDEN", "POST", "/work/shift", map[string]any{"on_duty": true, "branch_id": 0})
	st = kitchen.want(200, "", "POST", "/work/shift", map[string]any{"on_duty": true, "branch_id": f.branchID, "lat": 14.55, "lng": 121.02})
	if st["on_duty"] != true || st["duty_since"] == nil {
		t.Fatalf("clocked on: %v", st)
	}

	// only owners and managers see attendance
	cashier.want(403, "FORBIDDEN", "GET", period, nil)
	kitchen.want(403, "FORBIDDEN", "GET", period, nil)
	owner.want(422, "VALIDATION", "GET", page, nil)
	out := owner.want(200, "", "GET", period, nil)
	shifts := out["shifts"].([]any)
	if len(shifts) != 1 || len(out["staff"].([]any)) < 4 {
		t.Fatalf("attendance: %v", out)
	}
	sh := shifts[0].(map[string]any)
	if sh["ended_at"] != nil || sh["start_source"] != "manual" || sh["day"] != today {
		t.Fatalf("shift: %v", sh)
	}
	id := int64(sh["id"].(float64))

	// the manager ends it, with a reason
	edit := fmt.Sprintf("%s/%d", page, id)
	kitchen.want(403, "FORBIDDEN", "PATCH", edit, map[string]any{"void": true, "reason": "x"})
	owner.want(422, "VALIDATION", "PATCH", edit, map[string]any{"void": true})
	done := owner.want(200, "", "PATCH", edit, map[string]any{"ended_at": time.Now().Format(time.RFC3339Nano), "reason": "left early"})
	if done["end_reason"] != "manager" || done["edit_reason"] != "left early" || done["edited"] != true {
		t.Fatalf("ended: %v", done)
	}
	if st := kitchen.want(200, "", "GET", "/work/shift", nil); st["on_duty"] != false {
		t.Fatalf("after the manager ended it: %v", st)
	}

	// adding one by hand
	start := time.Now().Add(-26 * time.Hour)
	added := owner.want(201, "", "POST", page, map[string]any{"user_id": f.riderID, "started_at": start.Format(time.RFC3339),
		"ended_at": start.Add(4 * time.Hour).Format(time.RFC3339), "reason": "no phone"})
	if added["minutes"] != float64(240) || added["start_source"] != "manager" {
		t.Fatalf("added: %v", added)
	}
	// another store's manager cannot touch it
	stranger := login(t, newFixture(t).owner)
	stranger.want(404, "NOT_FOUND", "GET", period, nil)
	stranger.want(404, "NOT_FOUND", "PATCH", edit, map[string]any{"void": true, "reason": "x"})
}

func TestKitchenReportOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner, cashier, kitchen := login(t, f.owner), login(t, f.cashier), login(t, f.kitchen)
	today := time.Now().In(time.FixedZone("", 8*3600)).Format("2006-01-02")
	page := fmt.Sprintf("/merchant/branches/%d/reports/kitchen", f.branchID)
	period := "?from=" + today + "&to=" + today

	cashier.want(403, "FORBIDDEN", "GET", page+period, nil)
	kitchen.want(403, "FORBIDDEN", "GET", page+"/0"+period, nil)
	owner.want(422, "VALIDATION", "GET", page, nil)
	out := owner.want(200, "", "GET", page+period, nil)
	if out["slow_minutes"] != float64(15) || out["total_qty"] != float64(0) || len(out["people"].([]any)) < 2 {
		t.Fatalf("report: %v", out)
	}
	d := owner.want(200, "", "GET", page+"/0"+period+"&slow=1&station=0&page=2", nil)
	if d["page"] != float64(2) || len(d["days"].([]any)) != 7 || len(d["items"].([]any)) != 0 {
		t.Fatalf("detail: %v", d)
	}
	owner.want(404, "NOT_FOUND", "GET", page+"/x"+period, nil)
	owner.want(422, "VALIDATION", "GET", page+"/0"+period+"&station=x", nil)
	login(t, newFixture(t).owner).want(404, "NOT_FOUND", "GET", page+period, nil)
}

func TestRiderReportOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner, cashier := login(t, f.owner), login(t, f.cashier)
	today := time.Now().In(time.FixedZone("", 8*3600)).Format("2006-01-02")
	page := fmt.Sprintf("/merchant/branches/%d/reports/riders", f.branchID)
	period := "?from=" + today + "&to=" + today

	cashier.want(403, "FORBIDDEN", "GET", page+period, nil)
	cashier.want(403, "FORBIDDEN", "GET", fmt.Sprintf("%s/%d%s", page, f.riderID, period), nil)
	cashier.want(403, "FORBIDDEN", "GET", fmt.Sprintf("/merchant/branches/%d/trips/1", f.branchID), nil)
	owner.want(422, "VALIDATION", "GET", page, nil)
	out := owner.want(200, "", "GET", page+period, nil)
	if out["detour_percent"] != float64(30) || out["store_set"] != false || len(out["riders"].([]any)) != 1 {
		t.Fatalf("report: %v", out)
	}
	d := owner.want(200, "", "GET", fmt.Sprintf("%s/%d%s", page, f.riderID, period), nil)
	if len(d["trips"].([]any)) != 0 || len(d["days"].([]any)) != 0 {
		t.Fatalf("detail: %v", d)
	}
	owner.want(404, "NOT_FOUND", "GET", fmt.Sprintf("/merchant/branches/%d/trips/987654321", f.branchID), nil)
	login(t, newFixture(t).owner).want(404, "NOT_FOUND", "GET", page+period, nil)
}
