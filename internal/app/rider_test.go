package app_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// fakePush records what would have been pushed.
type fakePush struct {
	mu   sync.Mutex
	sent []app.PushMessage
	err  error
}

func (f *fakePush) Send(_ context.Context, _ app.Device, m app.PushMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakePush) take() []app.PushMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.sent
	f.sent = nil
	return out
}

// A rider clocks on, reports positions only while on duty, and is clocked off
// by signing out or by a shift nobody ended.
func TestRiderDutyAndLocations(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	packer, err := a.CreateStaff(ctx, s.branchID, app.NewUser{Username: uniq("p"), Password: "password1", DisplayName: "P", Role: "staff",
		Posts: []app.PostInput{{Kind: app.PostPacking}}})
	noErr(t, err)
	_, err = a.SetDuty(ctx, packer, true, "")
	wantCode(t, err, "NOT_RIDER")

	now := time.Now()
	point := app.LocationPoint{Lat: 3.139, Lng: 101.6869, At: now}
	_, err = a.ReportLocations(ctx, s.rider1, []app.LocationPoint{point})
	wantCode(t, err, "OFF_DUTY")

	st, err := a.SetDuty(ctx, s.rider1, true, "")
	noErr(t, err)
	if !st.OnDuty || st.DutySince == nil || st.Lat != nil {
		t.Fatalf("on duty: %+v", st)
	}
	since := *st.DutySince
	st, err = a.SetDuty(ctx, s.rider1, true, "") // a repeat keeps the start of the shift
	noErr(t, err)
	if !st.DutySince.Equal(since) {
		t.Fatalf("the shift start moved: %v then %v", since, st.DutySince)
	}
	if m := staffOf(t, s.branchID, s.rider1); !m.OnDuty {
		t.Fatal("the staff list should show the rider on duty")
	}
	if m := staffOf(t, s.branchID, s.rider2); m.OnDuty {
		t.Fatal("the other rider is not on duty")
	}

	// A batch saved while offline: the stale point is dropped, the newest fix
	// wins whatever the order, and a clock that runs ahead is pulled back.
	acc := float32(12)
	bat := int16(80)
	n, err := a.ReportLocations(ctx, s.rider1, []app.LocationPoint{
		{Lat: 3.2, Lng: 101.7, At: now.Add(-time.Minute), Accuracy: &acc, Battery: &bat},
		{Lat: 3.1, Lng: 101.6, At: now.Add(-3 * time.Minute)},
		{Lat: 3.0, Lng: 101.5, At: now.Add(-30 * time.Minute)},
	})
	noErr(t, err)
	if n != 2 {
		t.Fatalf("kept %d points, want 2", n)
	}
	st, err = a.RiderStatus(ctx, s.rider1)
	noErr(t, err)
	if st.Lat == nil || *st.Lat != 3.2 || st.Battery == nil || *st.Battery != 80 || st.Accuracy == nil {
		t.Fatalf("latest position: %+v", st)
	}
	_, err = a.ReportLocations(ctx, s.rider1, []app.LocationPoint{{Lat: 3.3, Lng: 101.8, At: now.Add(time.Hour)}})
	noErr(t, err)
	st, _ = a.RiderStatus(ctx, s.rider1)
	if *st.Lat != 3.3 || st.LocatedAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("a fix from the future: %+v", st)
	}
	_, err = a.ReportLocations(ctx, s.rider1, []app.LocationPoint{{Lat: 91, Lng: 0, At: now}})
	wantCode(t, err, "VALIDATION")
	_, err = a.ReportLocations(ctx, s.rider1, nil)
	wantCode(t, err, "VALIDATION")
	var tracks int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM rider_tracks WHERE user_id=$1`, s.rider1).Scan(&tracks))
	if tracks != 3 {
		t.Fatalf("%d track points, want 3", tracks)
	}

	// Clocking off forgets where the rider is.
	st, err = a.SetDuty(ctx, s.rider1, false, "")
	noErr(t, err)
	if st.OnDuty || st.Lat != nil || st.LocatedAt != nil {
		t.Fatalf("off duty: %+v", st)
	}

	// A shift nobody ended is closed by the cleanup, and week-old tracks go.
	_, err = a.SetDuty(ctx, s.rider1, true, "")
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE rider_status SET duty_since = now() - interval '13 hours' WHERE user_id=$1`, s.rider1)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE rider_tracks SET at = at - interval '8 days' WHERE user_id=$1`, s.rider1)
	noErr(t, err)
	noErr(t, a.Cleanup(ctx))
	if st, _ = a.RiderStatus(ctx, s.rider1); st.OnDuty {
		t.Fatal("a 13 hour shift should have been ended")
	}
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM rider_tracks WHERE user_id=$1`, s.rider1).Scan(&tracks))
	if tracks != 0 {
		t.Fatalf("%d old track points left", tracks)
	}
}

// Signing out where the shift was started ends it, and forgets the device
// that was registered in that sign-in. Other sign-ins do not matter.
func TestLogoutEndsShift(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	name := uniq("r")
	id, err := a.CreateStaff(ctx, s.branchID, app.NewUser{Username: name, Password: "password1", DisplayName: "R", Role: "staff",
		Posts: []app.PostInput{{Kind: app.PostRider}}})
	noErr(t, err)
	browser, _, err := a.Login(ctx, name, "password1")
	noErr(t, err)
	phone, _, expires, err := a.TokenLogin(ctx, name, "password1")
	noErr(t, err)
	if d := time.Until(expires); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Fatalf("the app session lasts %v", d)
	}
	_, err = a.RegisterDevice(ctx, id, "android", "fcm-"+name, "1.0", phone)
	noErr(t, err)
	_, err = a.SetDuty(ctx, id, true, phone)
	noErr(t, err)

	noErr(t, a.Logout(ctx, browser))
	if st, _ := a.RiderStatus(ctx, id); !st.OnDuty {
		t.Fatal("the shift was started on the phone; leaving the browser does not end it")
	}
	noErr(t, a.Logout(ctx, phone))
	if st, _ := a.RiderStatus(ctx, id); st.OnDuty {
		t.Fatal("signing out on the phone should end the shift")
	}
	var devices int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM rider_devices WHERE user_id=$1`, id).Scan(&devices))
	if devices != 0 {
		t.Fatal("the phone should have been forgotten")
	}
	if _, err := a.UserBySession(ctx, phone); err == nil {
		t.Fatal("the token should no longer work")
	}
}

// Riders are told about a new delivery, a packed order, a reassignment and a
// cancellation, on every device, and only when the change was committed.
func TestRiderPush(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	noErr(t, a.RunJobs(ctx)) // what earlier tests queued
	push := &fakePush{}
	a.Push = map[string]app.PushSender{"android": push}
	defer func() { a.Push = nil }()

	_, err := a.RegisterDevice(ctx, s.rider1, "fax", "x", "", "")
	wantCode(t, err, "VALIDATION")
	_, err = a.RegisterDevice(ctx, s.rider1, "web", "not a subscription", "", "")
	wantCode(t, err, "VALIDATION")
	d1, err := a.RegisterDevice(ctx, s.rider1, "android", uniq("tok"), "1.0", "")
	noErr(t, err)
	shared := uniq("tok")
	_, err = a.RegisterDevice(ctx, s.rider1, "android", shared, "1.0", "")
	noErr(t, err)
	// An iPhone is registered too, but this server has no sender for it.
	_, err = a.RegisterDevice(ctx, s.rider1, "ios", uniq("tok"), "1.0", "")
	noErr(t, err)

	o := s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	o, err = a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	got := push.take()
	if len(got) != 2 || got[0].Title != "新配送单 New delivery" || got[0].URL != "/work" || got[0].Body == "" {
		t.Fatalf("assigned: %+v", got)
	}
	// Assigning the same rider again changes nothing and tells nobody.
	_, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	if got = push.take(); len(got) != 0 {
		t.Fatalf("a repeat assignment pushed %+v", got)
	}

	_, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	if got = push.take(); len(got) != 2 || got[0].Title != "可以取餐了 Ready to collect" {
		t.Fatalf("packed: %+v", got)
	}

	// The phone changes hands: its token now belongs to the second rider.
	_, err = a.RegisterDevice(ctx, s.rider2, "android", shared, "1.1", "")
	noErr(t, err)
	_, err = a.AssignRider(ctx, o.ID, s.rider2, s.staff)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	got = push.take()
	titles := map[string]int{}
	for _, m := range got {
		titles[m.Title]++
	}
	if len(got) != 2 || titles["新配送单 New delivery"] != 1 || titles["配送单已改派给别人 Delivery reassigned to someone else"] != 1 {
		t.Fatalf("reassigned: %+v", got)
	}

	// A device the push service no longer knows is forgotten.
	noErr(t, a.RemoveDevice(ctx, s.rider2, d1.ID)) // not theirs: nothing happens
	push.err = app.ErrPushGone
	cur, err := a.GetOrder(ctx, o.ID)
	noErr(t, err)
	_, err = a.StaffCancel(ctx, o.ID, cur.Version, "customer asked", s.staff)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	var left int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM rider_devices WHERE user_id=$1`, s.rider2).Scan(&left))
	if left != 0 {
		t.Fatalf("%d dead devices kept", left)
	}
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM rider_devices WHERE id=$1`, d1.ID).Scan(&left))
	if left != 1 {
		t.Fatal("another rider must not remove this device")
	}

	// When the push service is down the reminder is kept for a retry.
	push.err = errors.New("push service is down")
	o2 := s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	o2, err = a.Accept(ctx, o2.ID, o2.Version, s.staff)
	noErr(t, err)
	_, err = a.AssignRider(ctx, o2.ID, s.rider1, s.staff)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	var status string
	var attempts int
	noErr(t, a.DB.QueryRow(ctx, `SELECT status, attempts FROM jobs WHERE kind='push' ORDER BY id DESC LIMIT 1`).Scan(&status, &attempts))
	if status != "pending" || attempts != 1 {
		t.Fatalf("failed push job: %s after %d attempts", status, attempts)
	}
	_, err = a.DB.Exec(ctx, `DELETE FROM jobs WHERE kind='push' AND status='pending'`)
	noErr(t, err)
}

// The browser sender speaks real Web Push: an encrypted body, signed with the
// server's key, and a subscription the push service dropped reads as gone.
func TestWebPushSender(t *testing.T) {
	private, public, err := app.NewVAPIDKeys()
	noErr(t, err)
	var gotAuth, gotEncoding string
	var gotBody int64
	status := http.StatusCreated
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotEncoding, gotBody = r.Header.Get("Authorization"), r.Header.Get("Content-Encoding"), r.ContentLength
		w.WriteHeader(status)
	}))
	defer srv.Close()

	key, err := ecdh.P256().GenerateKey(rand.Reader)
	noErr(t, err)
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	sub, _ := json.Marshal(map[string]any{"endpoint": srv.URL + "/push/abc", "keys": map[string]string{
		"p256dh": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		"auth":   base64.RawURLEncoding.EncodeToString(secret)}})
	w := app.WebPush{PublicKey: public, PrivateKey: private, Subject: "mailto:ops@example.com"}
	d := app.Device{Platform: "web", Token: string(sub)}
	noErr(t, w.Send(context.Background(), d, app.PushMessage{Title: "新配送单", Body: "A012"}))
	if len(gotAuth) < 20 || gotEncoding != "aes128gcm" || gotBody == 0 {
		t.Fatalf("push request: auth %q, encoding %q, %d bytes", gotAuth, gotEncoding, gotBody)
	}
	status = http.StatusGone
	if err := w.Send(context.Background(), d, app.PushMessage{Title: "x"}); !errors.Is(err, app.ErrPushGone) {
		t.Fatalf("a dropped subscription: %v", err)
	}
	status = http.StatusBadGateway
	if err := w.Send(context.Background(), d, app.PushMessage{Title: "x"}); err == nil || errors.Is(err, app.ErrPushGone) {
		t.Fatalf("a failing push service: %v", err)
	}
	if err := w.Send(context.Background(), app.Device{Token: "{}"}, app.PushMessage{}); !errors.Is(err, app.ErrPushGone) {
		t.Fatalf("an unusable subscription: %v", err)
	}
}

// The app sender signs in to Google with the service account, reuses the
// access token, and reads a removed app as gone.
func TestFCMSender(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	noErr(t, err)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	var signIns, sends int
	var lastAuth string
	var lastMsg map[string]any
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			signIns++
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || len(r.Form.Get("assertion")) < 100 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"at-1","expires_in":3600}`))
			return
		}
		sends++
		lastAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&lastMsg)
		w.WriteHeader(status)
		if status == http.StatusNotFound {
			_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
		}
	}))
	defer srv.Close()
	account, _ := json.Marshal(map[string]string{"project_id": "demo", "client_email": "push@demo.iam.gserviceaccount.com",
		"private_key": string(pemKey), "token_uri": srv.URL + "/token"})
	f, err := app.NewFCM(string(account))
	noErr(t, err)
	f.SetSendURL(srv.URL + "/send")

	ctx := context.Background()
	d := app.Device{Platform: "android", Token: "device-token"}
	noErr(t, f.Send(ctx, d, app.PushMessage{Title: "新配送单", Body: "A012", Tag: "order-1", URL: "/work"}))
	noErr(t, f.Send(ctx, d, app.PushMessage{Title: "x"}))
	if signIns != 1 || sends != 2 || lastAuth != "Bearer at-1" {
		t.Fatalf("%d sign-ins, %d sends, auth %q", signIns, sends, lastAuth)
	}
	msg := lastMsg["message"].(map[string]any)
	if msg["token"] != "device-token" || msg["android"].(map[string]any)["priority"] != "HIGH" {
		t.Fatalf("message: %v", msg)
	}
	status = http.StatusNotFound
	if err := f.Send(ctx, d, app.PushMessage{Title: "x"}); !errors.Is(err, app.ErrPushGone) {
		t.Fatalf("a removed app: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := f.Send(ctx, d, app.PushMessage{Title: "x"}); err == nil || errors.Is(err, app.ErrPushGone) {
		t.Fatalf("FCM down: %v", err)
	}
	// A rejected access token is dropped so the next send signs in again.
	status = http.StatusUnauthorized
	_ = f.Send(ctx, d, app.PushMessage{Title: "x"})
	status = http.StatusOK
	noErr(t, f.Send(ctx, d, app.PushMessage{Title: "x"}))
	if signIns != 2 {
		t.Fatalf("%d sign-ins after a rejected token", signIns)
	}
	if _, err := app.NewFCM(`{"project_id":"demo"}`); err == nil {
		t.Fatal("an incomplete service account must be refused")
	}
	if _, err := app.NewFCM("/no/such/file.json"); err == nil {
		t.Fatal("a missing file must be refused")
	}
}
