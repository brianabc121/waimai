package sms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhook(t *testing.T) {
	var got map[string]string
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	if err := (Webhook{URL: srv.URL, AuthHeader: "Bearer k"}).Send(context.Background(), "+639170000000", "hello"); err != nil {
		t.Fatal(err)
	}
	if got["to"] != "+639170000000" || got["message"] != "hello" || auth != "Bearer k" {
		t.Fatalf("got %v auth %q", got, auth)
	}
}

func TestTwilio(t *testing.T) {
	var path, user, pass string
	var form map[string][]string
	status := 201
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		user, pass, _ = r.BasicAuth()
		_ = r.ParseForm()
		form = r.PostForm
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"message":"bad number"}`))
	}))
	defer srv.Close()
	tw := Twilio{AccountSID: "AC1", AuthToken: "tok", From: "+15550001111", BaseURL: srv.URL}
	if err := tw.Send(context.Background(), "+639170000000", "你好"); err != nil {
		t.Fatal(err)
	}
	if path != "/2010-04-01/Accounts/AC1/Messages.json" || user != "AC1" || pass != "tok" ||
		form["To"][0] != "+639170000000" || form["From"][0] != "+15550001111" || form["Body"][0] != "你好" {
		t.Fatalf("request: %s %s %v", path, user, form)
	}
	status = 400
	err := tw.Send(context.Background(), "+1", "x")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("want a 400 error, got %v", err)
	}
}
