package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// docs/CODE_REVIEW_FIX_PLAN.md H05: which addresses count as a push service.
func TestPushEndpointOK(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc":                true,
		"https://updates.push.services.mozilla.com/wpush/v2/abc": true,
		"https://web.push.apple.com/abc":                         true,
		"https://db5p.notify.windows.com/w/?token=abc":           true,
		"https://FCM.googleapis.com:443/fcm/send/abc":            true,
		"http://fcm.googleapis.com/fcm/send/abc":                 false, // not https
		"https://fcm.googleapis.com:8443/x":                      false,
		"https://user:pw@fcm.googleapis.com/x":                   false,
		"https://fcm.googleapis.com.evil.example/x":              false,
		"https://evilfcm.googleapis.com.example/x":               false,
		"https://notify.windows.com.example/x":                   false,
		"https://169.254.169.254/latest/meta-data/":              false,
		"https://10.0.0.5/x":                                     false,
		"https://localhost/x":                                    false,
		"http://postgres:5432/":                                  false,
		"https://push.example/abc":                               false,
		"":                                                       false,
		"fcm.googleapis.com/x":                                   false,
		"https://[::1]/x":                                        false,
	} {
		if got := pushEndpointOK(raw); got != want {
			t.Errorf("pushEndpointOK(%q) = %v, want %v", raw, got, want)
		}
	}
	for ip, want := range map[string]bool{"8.8.8.8": true, "127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false,
		"172.16.0.1": false, "169.254.169.254": false, "::1": false, "fd00::1": false, "0.0.0.0": false, "2607:f8b0::1": true} {
		if got := publicIP(net.ParseIP(ip)); got != want {
			t.Errorf("publicIP(%s) = %v, want %v", ip, got, want)
		}
	}
}

// The guarded client connects to nothing inside the network and follows no redirect.
func TestPushClientGuards(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	if resp, err := pushClient.Get(srv.URL); err == nil {
		resp.Body.Close()
		t.Fatal("the guarded client reached a local address")
	}
	if hit {
		t.Fatal("the local server was reached")
	}
	if err := pushClient.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirects are followed: %v", err)
	}
}
