package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// WebPush sends reminders to browsers with the standard Web Push protocol.
// The key pair identifies this server to the browsers' push services.
type WebPush struct {
	PublicKey  string
	PrivateKey string
	Subject    string // a mailto: or https: contact the push service can reach
	// Client replaces the guarded client, and AnyHost lifts the check of
	// the address; both are for tests against a local stand-in only.
	Client  *http.Client
	AnyHost bool
}

// pushHosts are the browsers' push services (Chrome and the Chromium
// browsers, Firefox, Safari, Edge): a subscription's address is on one of
// them or it is not a subscription. The address comes from the browser, so
// from whoever is signed in; without this check the worker would post to
// any address they name, inside the network too
// (docs/CODE_REVIEW_FIX_PLAN.md H05).
var pushHosts = []string{
	"fcm.googleapis.com",
	"android.googleapis.com",
	"updates.push.services.mozilla.com",
	".push.services.mozilla.com",
	".push.apple.com",
	".notify.windows.com",
}

// pushEndpointOK says whether raw is an address of a push service: https,
// the usual port, no user or password, and a known host.
func pushEndpointOK(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return false
	}
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range pushHosts {
		if host == h || (strings.HasPrefix(h, ".") && strings.HasSuffix(host, h)) {
			return true
		}
	}
	return false
}

// pushClient posts to push services only: it follows no redirect and
// connects to no address inside the network, whatever a name resolves to.
var pushClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || !publicIP(ip) {
				return fmt.Errorf("web push: %s is not a public address", host)
			}
			return nil
		}}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       time.Minute,
	},
}

// publicIP says whether ip is an address on the internet rather than this
// machine, this network or a reserved range.
func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast())
}

// NewVAPIDKeys makes a key pair for WebPush, as (private, public).
func NewVAPIDKeys() (string, string, error) { return webpush.GenerateVAPIDKeys() }

func (w WebPush) Send(ctx context.Context, d Device, m PushMessage) error {
	var sub webpush.Subscription
	if err := json.Unmarshal([]byte(d.Token), &sub); err != nil || sub.Endpoint == "" {
		return ErrPushGone
	}
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	// an address that is not a push service's was never a subscription
	if !w.AnyHost && !pushEndpointOK(sub.Endpoint) {
		return ErrPushGone
	}
	client := w.Client
	if client == nil {
		client = pushClient
	}
	resp, err := webpush.SendNotificationWithContext(ctx, body, &sub, &webpush.Options{
		HTTPClient: client, Subscriber: w.Subject, VAPIDPublicKey: w.PublicKey, VAPIDPrivateKey: w.PrivateKey,
		TTL: 600, Urgency: webpush.UrgencyHigh,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrPushGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("web push: the push service answered %d", resp.StatusCode)
	}
	return nil
}
