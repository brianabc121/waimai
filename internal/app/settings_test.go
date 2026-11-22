package app_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/sms"
)

func settingOf(t *testing.T, v *app.SettingsView, key string) app.Setting {
	t.Helper()
	for _, s := range v.Items {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("no setting %s", key)
	return app.Setting{}
}

// a second server process: its own App on the same database
func settingsApp(t *testing.T, cfg config.Config) *app.App {
	t.Helper()
	if cfg.AppSecret == "" {
		cfg.AppSecret = "test-secret"
	}
	a, err := app.NewFromConfig(testApp.DB, cfg)
	noErr(t, err)
	return a
}

func TestAdminSettings(t *testing.T) {
	need(t)
	ctx := context.Background()
	actor := adminActor(t)
	clear := func() { _, _ = testApp.DB.Exec(ctx, `DELETE FROM platform_settings`) }
	clear()
	t.Cleanup(clear)

	a := settingsApp(t, config.Config{GoogleRoutesKey: "env-routes-key-0001"})
	v, err := a.AdminSettings(ctx)
	noErr(t, err)
	if !a.MapsOff || v.Active.Maps != "off" || v.Active.Routes != "on" || v.Active.SMS != "log" {
		t.Fatalf("before anything is saved: %+v mapsOff=%v", v.Active, a.MapsOff)
	}
	if s := settingOf(t, v, "GOOGLE_ROUTES_API_KEY"); s.Source != "env" || s.Value != "••••••••0001" {
		t.Fatalf("a key from the environment is shown masked: %+v", s)
	}
	if s := settingOf(t, v, "GOOGLE_MAPS_API_KEY"); s.Source != "none" || s.Value != "" {
		t.Fatalf("unset key: %+v", s)
	}

	// saving puts the key to use at once, in this process
	before := auditCount(t, "settings.")
	const mapsKey = "AIzaSy-browser-key-abcd"
	v, err = a.AdminSaveSettings(ctx, actor, map[string]string{"GOOGLE_MAPS_API_KEY": " " + mapsKey + " ", "GOOGLE_ROUTES_API_KEY": "stub"})
	noErr(t, err)
	if a.MapsOff || v.Active.Maps != "on" || v.Active.Routes != "stub" {
		t.Fatalf("after saving: %+v mapsOff=%v", v.Active, a.MapsOff)
	}
	if _, ok := a.Router.(app.StubRouter); !ok {
		t.Fatalf("the saved key wins over the environment: router is %T", a.Router)
	}
	if s := settingOf(t, v, "GOOGLE_MAPS_API_KEY"); s.Source != "admin" || s.Value != "••••••••abcd" || s.UpdatedBy == "" || s.UpdatedAt == nil {
		t.Fatalf("saved key: %+v", s)
	}
	if got, _, _ := a.PublicConfig(config.Config{}); got != mapsKey {
		t.Fatalf("pages get the map key %q", got)
	}

	// the database holds it encrypted, and the audit trail has the names only
	var sealed []byte
	noErr(t, testApp.DB.QueryRow(ctx, `SELECT value FROM platform_settings WHERE key='GOOGLE_MAPS_API_KEY'`).Scan(&sealed))
	if bytes.Contains(sealed, []byte(mapsKey)) {
		t.Fatal("the key is stored in the clear")
	}
	if auditCount(t, "settings.") != before+1 {
		t.Fatal("saving keys is not in the audit trail")
	}
	var detail string
	noErr(t, testApp.DB.QueryRow(ctx, `SELECT detail::text FROM admin_audit_logs WHERE action='settings.update' ORDER BY id DESC LIMIT 1`).Scan(&detail))
	if strings.Contains(detail, mapsKey) || !strings.Contains(detail, "GOOGLE_MAPS_API_KEY") {
		t.Fatalf("audit detail: %s", detail)
	}

	// keys that do not work together are refused, and nothing is saved
	_, err = a.AdminSaveSettings(ctx, actor, map[string]string{"SMS_PROVIDER": "twilio", "TWILIO_ACCOUNT_SID": "AC1"})
	wantCode(t, err, "SETTINGS_INCOMPLETE")
	var n int
	noErr(t, testApp.DB.QueryRow(ctx, `SELECT count(*) FROM platform_settings`).Scan(&n))
	if n != 2 {
		t.Fatalf("%d saved settings after a refused save, want 2", n)
	}
	for _, bad := range []map[string]string{
		{"DATABASE_URL": "postgres://x"}, {"SMS_PROVIDER": "pigeon"}, {"FCM_SERVICE_ACCOUNT": "/etc/passwd"},
		{"SMS_WEBHOOK_URL": "ftp://x"}, {"TWILIO_AUTH_TOKEN": "a\nb"}, {"FCM_SERVICE_ACCOUNT": `{"project_id":"p"}`},
	} {
		if _, err := a.AdminSaveSettings(ctx, actor, bad); err == nil {
			t.Fatalf("%v was accepted", bad)
		}
	}

	// another process (the worker) picks the keys up without a restart
	other := settingsApp(t, config.Config{})
	if other.MapsOff {
		t.Fatal("a process that starts later reads the saved keys")
	}
	_, err = a.AdminSaveSettings(ctx, actor, map[string]string{"SMS_PROVIDER": "twilio", "TWILIO_ACCOUNT_SID": "AC1",
		"TWILIO_AUTH_TOKEN": "token-secret-9999", "TWILIO_FROM": "+15550001111"})
	noErr(t, err)
	if _, ok := other.SMS.(app.LogSMS); !ok {
		t.Fatalf("other process before its next look: %T", other.SMS)
	}
	changed, err := other.SettingsChanged(ctx)
	noErr(t, err)
	if !changed {
		t.Fatal("the other process does not see the change")
	}
	noErr(t, other.ReloadSettings(ctx))
	if tw, ok := other.SMS.(sms.Twilio); !ok || tw.AuthToken != "token-secret-9999" || tw.From != "+15550001111" {
		t.Fatalf("other process after reloading: %#v", other.SMS)
	}
	if changed, _ := other.SettingsChanged(ctx); changed {
		t.Fatal("nothing changed since the reload")
	}

	// an emptied key falls back to the environment
	v, err = a.AdminSaveSettings(ctx, actor, map[string]string{"GOOGLE_ROUTES_API_KEY": ""})
	noErr(t, err)
	if s := settingOf(t, v, "GOOGLE_ROUTES_API_KEY"); s.Source != "env" || v.Active.Routes != "on" {
		t.Fatalf("cleared key: %+v %+v", s, v.Active)
	}
	if _, ok := a.Router.(app.GoogleRoutes); !ok {
		t.Fatalf("router after clearing: %T", a.Router)
	}

	// with another APP_SECRET the saved keys cannot be read: the server
	// still starts, on the environment alone, and the page says so
	moved := settingsApp(t, config.Config{AppSecret: "another-secret"})
	if !moved.MapsOff {
		t.Fatal("an unreadable key must not be used")
	}
	v, err = moved.AdminSettings(ctx)
	noErr(t, err)
	if s := settingOf(t, v, "GOOGLE_MAPS_API_KEY"); !s.Unreadable || s.Source != "none" || s.Value != "" {
		t.Fatalf("unreadable key: %+v", s)
	}

	// a fresh web push pair
	v, err = a.AdminNewVAPIDKeys(ctx, actor)
	noErr(t, err)
	pub := settingOf(t, v, "VAPID_PUBLIC_KEY")
	if !v.Active.WebPush || pub.Value == "" || a.Push["web"] == nil || strings.Contains(settingOf(t, v, "VAPID_PRIVATE_KEY").Value, pub.Value) {
		t.Fatalf("web push pair: %+v", v.Active)
	}
	if _, key, _ := a.PublicConfig(config.Config{}); key != pub.Value {
		t.Fatal("pages do not get the new public key")
	}
}
