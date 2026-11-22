package httpapi_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// raw fetches a path as the signed-in client and returns the body as it is.
func (c *client) raw(path string) (int, http.Header, []byte) {
	c.t.Helper()
	req, _ := http.NewRequest("GET", server.URL+"/api/v1"+path, nil)
	req.Header.Set("X-Forwarded-For", c.ip)
	for k, v := range c.cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

// sheets opens an Excel file and returns the text of each sheet by number.
func sheets(t *testing.T, b []byte) (workbook string, sheet []string) {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not an Excel file: %v", err)
	}
	part := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		s, _ := io.ReadAll(r)
		r.Close()
		part[f.Name] = string(s)
	}
	for i := 1; part[fmt.Sprintf("xl/worksheets/sheet%d.xml", i)] != ""; i++ {
		sheet = append(sheet, part[fmt.Sprintf("xl/worksheets/sheet%d.xml", i)])
	}
	return part["xl/workbook.xml"], sheet
}

// A person sees their own shifts, and only theirs.
func TestMyAttendanceOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner, kitchen, rider := login(t, f.owner), login(t, f.kitchen), login(t, f.rider)
	mine := fmt.Sprintf("/work/attendance?branch_id=%d", f.branchID)
	month := time.Now().In(time.FixedZone("", 8*3600)).Format("2006-01")

	newClient(t).want(401, "UNAUTHENTICATED", "GET", mine, nil)
	kitchen.want(403, "FORBIDDEN", "GET", "/work/attendance", nil)
	kitchen.want(422, "VALIDATION", "GET", mine+"&month=2026", nil)
	out := kitchen.want(200, "", "GET", mine, nil)
	if out["month"] != month || out["days"] != float64(0) || len(out["shifts"].([]any)) != 0 {
		t.Fatalf("nothing yet: %v", out)
	}

	kitchen.want(200, "", "POST", "/work/shift", map[string]any{"on_duty": true, "branch_id": f.branchID})
	rider.want(200, "", "POST", "/work/shift", map[string]any{"on_duty": true, "branch_id": f.branchID})
	out = kitchen.want(200, "", "GET", mine+"&month="+month, nil)
	shifts := out["shifts"].([]any)
	if out["days"] != float64(1) || len(shifts) != 1 {
		t.Fatalf("one shift of my own: %v", out)
	}
	sh := shifts[0].(map[string]any)
	if sh["ended_at"] != nil || sh["start_source"] != "manual" {
		t.Fatalf("shift: %v", sh)
	}

	// what the manager changed shows, with the reason; a struck shift does not count
	edit := fmt.Sprintf("/merchant/branches/%d/attendance/%d", f.branchID, int64(sh["id"].(float64)))
	owner.want(200, "", "PATCH", edit, map[string]any{"void": true, "reason": "clocked on by mistake"})
	out = kitchen.want(200, "", "GET", mine, nil)
	sh = out["shifts"].([]any)[0].(map[string]any)
	if out["days"] != float64(0) || out["minutes"] != float64(0) || sh["voided"] != true || sh["edit_reason"] != "clocked on by mistake" || sh["edited_by"] != "O" {
		t.Fatalf("after the manager struck it: %v", out)
	}
	// another store's people cannot ask for this store
	login(t, newFixture(t).kitchen).want(403, "FORBIDDEN", "GET", mine, nil)
}

// The store sets what counts as a slow dish and as a detour.
func TestReportSettingsOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := login(t, f.owner)
	today := time.Now().In(time.FixedZone("", 8*3600)).Format("2006-01-02")
	period := "?from=" + today + "&to=" + today
	base := fmt.Sprintf("/merchant/branches/%d", f.branchID)
	settings := map[string]any{"name": "H", "accept_timeout_minutes": 10, "delivery_enabled": true, "pickup_enabled": true,
		"open_time": "00:00", "close_time": "00:00", "prep_minutes": 30}
	with := func(k string, v any) map[string]any {
		m := map[string]any{k: v}
		for key, val := range settings {
			m[key] = val
		}
		return m
	}
	owner.want(422, "VALIDATION", "PUT", base+"/settings", with("kitchen_slow_minutes", 121))
	owner.want(422, "VALIDATION", "PUT", base+"/settings", with("detour_percent", 4))
	owner.want(422, "VALIDATION", "PUT", base+"/settings", with("detour_meters", 50))

	settings["kitchen_slow_minutes"], settings["detour_percent"], settings["detour_meters"] = 8, 50, 2500
	b := owner.want(200, "", "PUT", base+"/settings", settings)
	if b["kitchen_slow_minutes"] != float64(8) || b["detour_percent"] != float64(50) || b["detour_meters"] != float64(2500) {
		t.Fatalf("saved: %v", b)
	}
	if k := owner.want(200, "", "GET", base+"/reports/kitchen"+period, nil); k["slow_minutes"] != float64(8) {
		t.Fatalf("kitchen report: %v", k)
	}
	if r := owner.want(200, "", "GET", base+"/reports/riders"+period, nil); r["detour_percent"] != float64(50) || r["detour_meters"] != float64(2500) {
		t.Fatalf("rider report: %v", r)
	}
	// an older page that does not send them leaves them alone
	delete(settings, "kitchen_slow_minutes")
	delete(settings, "detour_percent")
	delete(settings, "detour_meters")
	if b := owner.want(200, "", "PUT", base+"/settings", settings); b["kitchen_slow_minutes"] != float64(8) || b["detour_meters"] != float64(2500) {
		t.Fatalf("left alone: %v", b)
	}
}

// The three lists download as Excel, for managers only.
func TestExcelOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner, cashier, kitchen := login(t, f.owner), login(t, f.cashier), login(t, f.kitchen)
	today := time.Now().In(time.FixedZone("", 8*3600)).Format("2006-01-02")
	period := "?from=" + today + "&to=" + today + "&format=xlsx"
	base := fmt.Sprintf("/merchant/branches/%d", f.branchID)
	kitchen.want(200, "", "POST", "/work/shift", map[string]any{"on_duty": true, "branch_id": f.branchID})

	for _, c := range []struct {
		path, file string
		zh, en     []string
	}{
		{"/attendance", "attendance", []string{"合计", "每一班", "出勤天数", "自己打卡"}, []string{"Totals", "Shifts", "Days worked", "clocked on"}},
		{"/reports/kitchen", "kitchen", []string{"汇总", "明细", "打包单数", "做好时间"}, []string{"Summary", "Dishes", "Orders packed", "Ready at"}},
		{"/reports/riders", "riders", []string{"汇总", "每一趟", "出车趟数", "实际里程（公里）"}, []string{"Summary", "Trips", "Ridden (km)"}},
	} {
		if st, _, _ := cashier.raw(base + c.path + period); st != 403 {
			t.Fatalf("%s: a cashier got %d", c.path, st)
		}
		for lang, words := range map[string][]string{"": c.zh, "&lang=en": c.en} {
			st, h, body := owner.raw(base + c.path + period + lang)
			if st != 200 || !strings.Contains(h.Get("Content-Type"), "spreadsheetml") ||
				h.Get("Content-Disposition") != fmt.Sprintf(`attachment; filename="%s_%s_%s.xlsx"`, c.file, today, today) {
				t.Fatalf("%s: %d %v", c.path, st, h)
			}
			wb, sh := sheets(t, body)
			if len(sh) != 2 {
				t.Fatalf("%s: %d sheets", c.path, len(sh))
			}
			all := wb + strings.Join(sh, "")
			for _, w := range words {
				if !strings.Contains(all, w) {
					t.Fatalf("%s%s: %q is missing", c.path, lang, w)
				}
			}
		}
	}
	// the person at work is in the attendance file
	_, _, body := owner.raw(base + "/attendance" + period)
	if _, sh := sheets(t, body); !strings.Contains(sh[1], f.kitchen) || !strings.Contains(sh[1], today) {
		t.Fatalf("the shift is not in the file")
	}
	if st, _, _ := owner.raw(base + "/attendance?format=xlsx"); st != 422 {
		t.Fatalf("no period: %d", st)
	}
}
