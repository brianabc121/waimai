package httpapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func adminClient(t *testing.T) *client {
	t.Helper()
	if err := a.EnsurePlatformAdmin(context.Background(), "admin-test", "password1"); err != nil {
		t.Fatal(err)
	}
	return adminLogin(t, "admin-test")
}

// adminLogin signs in on the platform's own sign-in page.
func adminLogin(t *testing.T, user string) *client {
	t.Helper()
	c := newClient(t)
	c.want(200, "", "POST", "/admin/auth/login", map[string]string{"username": user, "password": "password1"})
	return c
}

func TestAdminBackOffice(t *testing.T) {
	f := newFixture(t)
	admin, owner := adminClient(t), login(t, f.owner)
	bid := fmt.Sprint(f.branchID)

	// the back office is closed to merchants and to visitors
	for _, r := range [][2]string{{"GET", "/admin/merchants"}, {"GET", "/admin/items"}, {"GET", "/admin/audit"},
		{"GET", "/admin/branches/" + bid + "/menu"}, {"POST", "/admin/branches/" + bid + "/items/bulk"},
		{"DELETE", "/admin/branches/" + bid}, {"POST", "/admin/users/1/reset-password"}} {
		owner.want(401, "UNAUTHENTICATED", r[0], r[1], map[string]any{})
		newClient(t).want(401, "UNAUTHENTICATED", r[0], r[1], map[string]any{})
	}

	// find the merchant by its link
	out := admin.want(200, "", "GET", "/admin/merchants?q="+f.slug, nil)
	items := out["items"].([]any)
	if len(items) != 1 || out["total"].(float64) != 1 {
		t.Fatalf("merchant search: %v", out)
	}
	m := items[0].(map[string]any)
	mid := fmt.Sprint(int64(m["id"].(float64)))
	if m["status"] != "active" || m["branch_count"].(float64) != 1 {
		t.Fatalf("merchant row: %v", m)
	}
	admin.want(422, "VALIDATION", "GET", "/admin/merchants?status=nope", nil)

	// the platform can look at a store's back office but not act in it
	admin.want(200, "", "GET", "/merchant/branches/"+bid+"/menu", nil)
	admin.want(200, "", "GET", "/merchant/branches/"+bid+"/orders", nil)
	admin.want(403, "READ_ONLY", "PATCH", "/merchant/branches/"+bid+"/items/"+fmt.Sprint(f.itemID), map[string]any{"sold_out": true})
	admin.want(403, "READ_ONLY", "PUT", "/merchant/branches/"+bid+"/settings", map[string]any{"name": "x"})

	// menu edits go through the admin routes
	admin.want(200, "", "PATCH", "/admin/branches/"+bid+"/items/"+fmt.Sprint(f.itemID), map[string]any{"sold_out": true})
	out = admin.want(200, "", "GET", "/admin/items?branch_id="+bid+"&status=sold_out", nil)
	if out["total"].(float64) != 1 {
		t.Fatalf("sold out filter: %v", out)
	}
	out = admin.want(200, "", "POST", "/admin/branches/"+bid+"/items/bulk", map[string]any{"ids": []int64{f.itemID}, "action": "in_stock"})
	if out["changed"].(float64) != 1 {
		t.Fatalf("bulk: %v", out)
	}
	admin.want(404, "NOT_FOUND", "GET", "/admin/branches/999999999/menu", nil)

	// edit, suspend, delete with confirmation, restore
	admin.want(200, "", "PUT", "/admin/merchants/"+mid, map[string]any{"name": "H", "contact_name": "Ann", "contact_phone": "+639171112222"})
	admin.want(200, "", "GET", "/admin/merchants?q=%2B639171112222", nil)
	admin.want(409, "INVALID_STATE", "DELETE", "/admin/merchants/"+mid, map[string]any{"confirm_name": "H"})
	admin.want(200, "", "POST", "/admin/merchants/"+mid+"/suspend", map[string]any{"reason": "test"})
	out = newClient(t).want(200, "", "GET", "/stores/"+f.slug, nil)
	if out["branch"].(map[string]any)["paused"] != true {
		t.Fatal("a suspended store should show as not taking orders")
	}
	owner.want(200, "", "GET", "/merchant/branches/"+bid+"/orders", nil)
	admin.want(422, "VALIDATION", "DELETE", "/admin/merchants/"+mid, map[string]any{"confirm_name": "nope"})
	admin.want(200, "", "DELETE", "/admin/merchants/"+mid, map[string]any{"confirm_name": "H"})
	newClient(t).want(404, "NOT_FOUND", "GET", "/stores/"+f.slug, nil)
	owner.want(401, "UNAUTHENTICATED", "GET", "/merchant/branches/"+bid+"/orders", nil)
	admin.want(404, "NOT_FOUND", "POST", "/admin/merchants/"+mid+"/frobnicate", nil)
	admin.want(200, "", "POST", "/admin/merchants/"+mid+"/restore", nil)
	admin.want(200, "", "POST", "/admin/merchants/"+mid+"/resume", nil)
	newClient(t).want(200, "", "GET", "/stores/"+f.slug, nil)

	// reset a password: shown once, the old one stops working
	out = admin.want(200, "", "GET", "/admin/merchants/"+mid, nil)
	var uid string
	for _, u := range out["users"].([]any) {
		if u.(map[string]any)["username"] == f.owner {
			uid = fmt.Sprint(int64(u.(map[string]any)["id"].(float64)))
		}
	}
	out = admin.want(200, "", "POST", "/admin/users/"+uid+"/reset-password", nil)
	pw := out["password"].(string)
	c := newClient(t)
	c.want(401, "BAD_CREDENTIALS", "POST", "/auth/login", map[string]string{"username": f.owner, "password": "password1"})
	c.want(200, "", "POST", "/auth/login", map[string]string{"username": f.owner, "password": pw})
	admin.want(200, "", "PATCH", "/admin/users/"+uid, map[string]any{"active": false})
	c.want(401, "UNAUTHENTICATED", "GET", "/auth/me", nil)

	// every write above left a trace, with the address it came from
	out = admin.want(200, "", "GET", "/admin/audit?action=merchant.&page_size=100", nil)
	seen := map[string]bool{}
	for _, e := range out["items"].([]any) {
		e := e.(map[string]any)
		if fmt.Sprint(e["target_id"]) == mid {
			seen[e["action"].(string)] = true
			if e["ip"] != admin.ip {
				t.Fatalf("audit entry has address %v, want %s", e["ip"], admin.ip)
			}
		}
	}
	for _, want := range []string{"merchant.update", "merchant.suspend", "merchant.delete", "merchant.restore", "merchant.resume"} {
		if !seen[want] {
			t.Fatalf("no audit entry for %s (saw %v)", want, seen)
		}
	}
	// the new password never reaches the audit trail
	out = admin.want(200, "", "GET", "/admin/audit?action=user.reset_password&page_size=5", nil)
	if s := fmt.Sprint(out); strings.Contains(s, pw) {
		t.Fatal("the generated password leaked into the audit trail")
	}
}

func TestAdminWorkerAndJobs(t *testing.T) {
	f := newFixture(t)
	admin, owner := adminClient(t), login(t, f.owner)
	ctx := context.Background()

	var id int64
	if err := a.DB.QueryRow(ctx, `INSERT INTO jobs (kind, payload, status, attempts, last_error, finished_at)
		VALUES ('sms', '{"phone":"+639171234589","message":"hi https://x.test/o/secretToken"}', 'failed', 5, 'provider down', now())
		RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	job := "/admin/jobs/" + fmt.Sprint(id)

	for _, r := range [][2]string{{"GET", "/admin/worker"}, {"GET", "/admin/jobs"}, {"GET", job},
		{"POST", job + "/retry"}, {"POST", job + "/discard"}, {"POST", "/admin/jobs/retry-failed"}} {
		owner.want(401, "UNAUTHENTICATED", r[0], r[1], map[string]any{})
		newClient(t).want(401, "UNAUTHENTICATED", r[0], r[1], map[string]any{})
	}

	out := admin.want(200, "", "GET", "/admin/worker", nil)
	if len(out["tasks"].([]any)) != 5 || out["queue"].(map[string]any)["failed"].(float64) < 1 {
		t.Fatalf("worker status: %v", out)
	}
	out = admin.want(200, "", "GET", "/admin/jobs?status=failed&kind=sms", nil)
	row := out["items"].([]any)[0].(map[string]any)
	if row["subject"] != "+63917•••••89" || row["message"] != nil {
		t.Fatalf("job row leaks personal data: %v", row)
	}
	admin.want(422, "VALIDATION", "GET", "/admin/jobs?status=nope", nil)
	out = admin.want(200, "", "GET", job, nil)
	if msg := out["message"].(string); strings.Contains(msg, "secretToken") {
		t.Fatalf("order link not cut: %q", msg)
	}
	admin.want(404, "NOT_FOUND", "GET", "/admin/jobs/999999999", nil)
	admin.want(404, "NOT_FOUND", "POST", job+"/explode", map[string]any{})

	admin.want(200, "", "POST", job+"/discard", map[string]any{})
	admin.want(200, "", "POST", job+"/retry", map[string]any{})
	admin.want(409, "INVALID_STATE", "POST", job+"/retry", map[string]any{})
	admin.want(200, "", "POST", job+"/discard", map[string]any{})
	out = admin.want(200, "", "GET", job, nil)
	if out["status"] != "discarded" {
		t.Fatalf("job: %v", out)
	}
	out = admin.want(200, "", "GET", "/admin/audit?action=job.discard", nil)
	if out["total"].(float64) < 2 {
		t.Fatalf("job actions are not in the audit log: %v", out)
	}
}

func TestAdminSettingsOverHTTP(t *testing.T) {
	f := newFixture(t)
	admin, owner := adminClient(t), login(t, f.owner)
	ctx := context.Background()
	reset := func() {
		_, _ = a.DB.Exec(ctx, `DELETE FROM platform_settings`)
		_ = a.ReloadSettings(ctx)
		// the shared test App was not built from a configuration: maps stay on
		a.MapsOff, a.Router, a.Push = false, nil, nil
	}
	reset()
	t.Cleanup(reset)

	for _, r := range [][2]string{{"GET", "/admin/settings"}, {"PUT", "/admin/settings"}, {"POST", "/admin/settings/vapid"}} {
		owner.want(401, "UNAUTHENTICATED", r[0], r[1], map[string]any{})
		newClient(t).want(401, "UNAUTHENTICATED", r[0], r[1], map[string]any{})
	}

	out := admin.want(200, "", "GET", "/admin/settings", nil)
	if len(out["items"].([]any)) < 10 || out["active"].(map[string]any)["maps"] != "off" {
		t.Fatalf("settings: %v", out)
	}
	if cfg := newClient(t).want(200, "", "GET", "/config", nil); cfg["maps_key"] != "" {
		t.Fatalf("config before: %v", cfg)
	}

	const key = "AIzaSy-http-test-key-wxyz"
	out = admin.want(200, "", "PUT", "/admin/settings", map[string]any{"values": map[string]string{"GOOGLE_MAPS_API_KEY": key}})
	raw := fmt.Sprint(out)
	if strings.Contains(raw, key) || !strings.Contains(raw, "••••••••wxyz") {
		t.Fatalf("the answer must carry the masked key only: %v", out)
	}
	if strings.Contains(fmt.Sprint(admin.want(200, "", "GET", "/admin/settings", nil)), key) {
		t.Fatal("the settings page got the key in the clear")
	}
	// the customer pages get the browser key without a restart
	if cfg := newClient(t).want(200, "", "GET", "/config", nil); cfg["maps_key"] != key {
		t.Fatalf("config after: %v", cfg)
	}

	admin.want(422, "SETTINGS_INCOMPLETE", "PUT", "/admin/settings", map[string]any{"values": map[string]string{"SMS_PROVIDER": "webhook"}})
	admin.want(422, "VALIDATION", "PUT", "/admin/settings", map[string]any{"values": map[string]string{"APP_SECRET": "x"}})

	out = admin.want(200, "", "POST", "/admin/settings/vapid", map[string]any{})
	if out["active"].(map[string]any)["web_push"] != true {
		t.Fatalf("after making a pair: %v", out["active"])
	}
	if cfg := newClient(t).want(200, "", "GET", "/config", nil); len(fmt.Sprint(cfg["push_key"])) < 40 {
		t.Fatalf("push key: %v", cfg)
	}
	out = admin.want(200, "", "GET", "/admin/audit?action=settings.", nil)
	if out["total"].(float64) < 2 || strings.Contains(fmt.Sprint(out), key) {
		t.Fatalf("audit: %v", out)
	}
}
