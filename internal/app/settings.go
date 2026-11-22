package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/sms"
)

// Keys of outside services, set by the platform administrator in the back
// office. Each one has the name of the environment variable it replaces; a
// value saved here wins, and the environment stays the fallback. The database
// address and APP_SECRET are not here: the server needs them before it can
// read anything.

type settingDef struct {
	Key    string
	Group  string // maps, web_push, app_push, sms
	Secret bool   // shown masked, never sent back to the browser
	field  func(*config.Config) *string
}

var settingDefs = []settingDef{
	{"GOOGLE_MAPS_API_KEY", "maps", true, func(c *config.Config) *string { return &c.GoogleMapsKey }},
	{"GOOGLE_ROUTES_API_KEY", "maps", true, func(c *config.Config) *string { return &c.GoogleRoutesKey }},
	{"VAPID_PUBLIC_KEY", "web_push", false, func(c *config.Config) *string { return &c.VAPIDPublicKey }},
	{"VAPID_PRIVATE_KEY", "web_push", true, func(c *config.Config) *string { return &c.VAPIDPrivateKey }},
	{"VAPID_SUBJECT", "web_push", false, func(c *config.Config) *string { return &c.VAPIDSubject }},
	{"FCM_SERVICE_ACCOUNT", "app_push", true, func(c *config.Config) *string { return &c.FCMServiceAccount }},
	{"SMS_PROVIDER", "sms", false, func(c *config.Config) *string { return &c.SMSProvider }},
	{"SMS_WEBHOOK_URL", "sms", false, func(c *config.Config) *string { return &c.SMSWebhookURL }},
	{"SMS_WEBHOOK_AUTH", "sms", true, func(c *config.Config) *string { return &c.SMSWebhookAuth }},
	{"TWILIO_ACCOUNT_SID", "sms", false, func(c *config.Config) *string { return &c.TwilioAccountSID }},
	{"TWILIO_AUTH_TOKEN", "sms", true, func(c *config.Config) *string { return &c.TwilioAuthToken }},
	{"TWILIO_FROM", "sms", false, func(c *config.Config) *string { return &c.TwilioFrom }},
	{"TWILIO_MESSAGING_SERVICE_SID", "sms", false, func(c *config.Config) *string { return &c.TwilioMessagingSID }},
}

func settingDefByKey(key string) *settingDef {
	for i := range settingDefs {
		if settingDefs[i].Key == key {
			return &settingDefs[i]
		}
	}
	return nil
}

const maxSettingLen = 16 << 10

// ---- encryption ----

func (a *App) settingsCipher() (cipher.AEAD, error) {
	m := hmac.New(sha256.New, a.Secret)
	m.Write([]byte("platform-settings-v1"))
	block, err := aes.NewCipher(m.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// sealSetting encrypts a value. The name is bound in, so a stored value
// cannot be moved under another name.
func (a *App) sealSetting(key, value string) ([]byte, error) {
	gcm, err := a.settingsCipher()
	if err != nil {
		return nil, err
	}
	nonce := randomBytes(gcm.NonceSize())
	return gcm.Seal(nonce, nonce, []byte(value), []byte(key)), nil
}

func (a *App) openSetting(key string, sealed []byte) (string, error) {
	gcm, err := a.settingsCipher()
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("stored setting is too short")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(key))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ---- what the keys turn on ----

// integrations is everything built from the keys.
type integrations struct {
	cfg     config.Config
	sms     SMSSender
	push    map[string]PushSender
	router  Router
	mapsOff bool
}

func buildIntegrations(cfg config.Config) (*integrations, error) {
	in := &integrations{cfg: cfg, sms: LogSMS{}, push: map[string]PushSender{}, mapsOff: cfg.GoogleMapsKey == ""}
	switch cfg.GoogleRoutesKey {
	case "":
	case "stub":
		in.router = StubRouter{}
	default:
		in.router = GoogleRoutes{Key: cfg.GoogleRoutesKey}
	}
	sender, err := sms.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	if sender != nil {
		in.sms = sender
	}
	if cfg.VAPIDPublicKey != "" && cfg.VAPIDPrivateKey != "" {
		subject := cfg.VAPIDSubject
		if subject == "" {
			subject = cfg.PublicBaseURL
		}
		in.push["web"] = WebPush{PublicKey: cfg.VAPIDPublicKey, PrivateKey: cfg.VAPIDPrivateKey, Subject: subject}
	}
	if cfg.FCMServiceAccount != "" {
		fcm, err := NewFCM(cfg.FCMServiceAccount)
		if err != nil {
			return nil, err
		}
		in.push["android"], in.push["ios"] = fcm, fcm
	}
	return in, nil
}

func (a *App) install(in *integrations, signature string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.SMS, a.Push, a.Router, a.MapsOff = in.sms, in.push, in.router, in.mapsOff
	a.effective, a.settingsSig = &in.cfg, signature
}

// The senders are swapped when an administrator saves a key, so they are read
// through these.

func (a *App) smsSender() SMSSender {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.SMS
}

func (a *App) pushSender(platform string) PushSender {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Push[platform]
}

func (a *App) router() Router {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Router
}

func (a *App) mapsOff() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.MapsOff
}

// PublicConfig is what every page may know before anyone signs in: the
// browser map key, the public half of the web push pair and whether the
// phone apps can be pushed to. fallback serves an App that was not built
// from a configuration.
func (a *App) PublicConfig(fallback config.Config) (mapsKey, pushKey string, appPush bool) {
	a.mu.RLock()
	cfg := a.effective
	a.mu.RUnlock()
	if cfg == nil {
		cfg = &fallback
	}
	return cfg.GoogleMapsKey, cfg.VAPIDPublicKey, cfg.FCMServiceAccount != ""
}

// ---- reading ----

type storedSetting struct {
	value      string
	unreadable bool // encrypted with another APP_SECRET
	updatedAt  time.Time
	updatedBy  string
}

// readSettings returns the saved values and a signature that changes
// whenever any of them does.
func (a *App) readSettings(ctx context.Context, q Querier) (map[string]storedSetting, string, error) {
	rows, err := q.Query(ctx, `SELECT s.key, s.value, s.updated_at, COALESCE(u.display_name, '')
		FROM platform_settings s LEFT JOIN users u ON u.id = s.updated_by ORDER BY s.key`)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := map[string]storedSetting{}
	sig := sha256.New()
	for rows.Next() {
		var key string
		var sealed []byte
		var st storedSetting
		if err := rows.Scan(&key, &sealed, &st.updatedAt, &st.updatedBy); err != nil {
			return nil, "", err
		}
		sig.Write([]byte(key))
		sig.Write(sealed)
		if settingDefByKey(key) == nil {
			continue
		}
		if st.value, err = a.openSetting(key, sealed); err != nil {
			st.unreadable = true
		}
		out[key] = st
	}
	return out, fmt.Sprintf("%x", sig.Sum(nil)), rows.Err()
}

// merged lays the saved values over the environment.
func (a *App) merged(saved map[string]storedSetting) config.Config {
	cfg := a.base
	for _, d := range settingDefs {
		if st, ok := saved[d.Key]; ok && !st.unreadable && st.value != "" {
			*d.field(&cfg) = st.value
		}
	}
	return cfg
}

// ReloadSettings rebuilds the senders from the environment and the saved
// keys. On an error the ones in use stay.
func (a *App) ReloadSettings(ctx context.Context) error {
	saved, sig, err := a.readSettings(ctx, a.DB)
	if err != nil {
		return err
	}
	in, err := buildIntegrations(a.merged(saved))
	if err != nil {
		return err
	}
	a.install(in, sig)
	return nil
}

// settingsPoll is how often a process looks for keys saved through another
// one (the worker, or a second API server).
const settingsPoll = 15 * time.Second

// WatchSettings picks up keys saved through another process, until ctx ends.
func (a *App) WatchSettings(ctx context.Context, logf func(string, ...any)) {
	t := time.NewTicker(settingsPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		changed, err := a.SettingsChanged(ctx)
		if err == nil && changed {
			if err = a.ReloadSettings(ctx); err == nil {
				logf("settings: keys changed in the back office, now in use")
			}
		}
		if err != nil && ctx.Err() == nil {
			logf("settings: %v", err)
		}
	}
}

// SettingsChanged reports whether keys were saved since this process last read them.
func (a *App) SettingsChanged(ctx context.Context) (bool, error) {
	_, sig, err := a.readSettings(ctx, a.DB)
	if err != nil {
		return false, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return sig != a.settingsSig, nil
}

// ---- the back office page ----

// Setting is one key as the back office shows it.
type Setting struct {
	Key    string `json:"key"`
	Group  string `json:"group"`
	Secret bool   `json:"secret"`
	// Source is where the value in use comes from: admin (saved here), env
	// (the server's environment) or none.
	Source string `json:"source"`
	// Value is the value in use; of a secret only a masked hint.
	Value string `json:"value"`
	// Unreadable: saved here but encrypted with another APP_SECRET.
	Unreadable bool       `json:"unreadable"`
	UpdatedAt  *time.Time `json:"updated_at"`
	UpdatedBy  string     `json:"updated_by"`
}

// SettingsView is the keys page.
type SettingsView struct {
	Items []Setting `json:"items"`
	// Active says what the keys in use turn on.
	Active struct {
		Maps    string `json:"maps"`   // on, stub, off
		Routes  string `json:"routes"` // on, stub, off
		WebPush bool   `json:"web_push"`
		AppPush bool   `json:"app_push"`
		SMS     string `json:"sms"` // log, webhook, twilio
	} `json:"active"`
	// WeakSecret: APP_SECRET is the development default, so the encryption
	// protects nothing.
	WeakSecret bool `json:"weak_secret"`
	// OTPSentToday counts the verification codes texted in the last 24 hours
	// (each costs a message). Only on the page load, not after a save.
	OTPSentToday *int `json:"otp_sent_today,omitempty"`
}

func maskSecret(key, v string) string {
	if key == "FCM_SERVICE_ACCOUNT" {
		var sa struct {
			ProjectID   string `json:"project_id"`
			ClientEmail string `json:"client_email"`
		}
		if json.Unmarshal([]byte(v), &sa) == nil && sa.ProjectID != "" {
			return sa.ProjectID + " · " + sa.ClientEmail
		}
		return "••••••••"
	}
	if len(v) < 12 {
		return "••••••••"
	}
	return "••••••••" + v[len(v)-4:]
}

func keyState(v string) string {
	switch v {
	case "":
		return "off"
	case "stub":
		return "stub"
	}
	return "on"
}

func (a *App) settingsView(saved map[string]storedSetting) *SettingsView {
	cfg := a.merged(saved)
	env := a.base
	out := &SettingsView{WeakSecret: string(a.Secret) == "dev-secret-change-me"}
	for _, d := range settingDefs {
		s := Setting{Key: d.Key, Group: d.Group, Secret: d.Secret, Source: "none"}
		st, isSaved := saved[d.Key]
		value := *d.field(&cfg)
		switch {
		case isSaved && !st.unreadable && st.value != "":
			s.Source = "admin"
		case *d.field(&env) != "" && !(d.Key == "SMS_PROVIDER" && value == "log"):
			s.Source = "env"
		}
		if isSaved {
			at := st.updatedAt
			s.UpdatedAt, s.UpdatedBy, s.Unreadable = &at, st.updatedBy, st.unreadable
		}
		if s.Source != "none" || d.Key == "SMS_PROVIDER" {
			s.Value = value
			if d.Secret {
				s.Value = maskSecret(d.Key, value)
			}
		}
		out.Items = append(out.Items, s)
	}
	out.Active.Maps, out.Active.Routes = keyState(cfg.GoogleMapsKey), keyState(cfg.GoogleRoutesKey)
	out.Active.WebPush = cfg.VAPIDPublicKey != "" && cfg.VAPIDPrivateKey != ""
	out.Active.AppPush = cfg.FCMServiceAccount != ""
	out.Active.SMS = cfg.SMSProvider
	if out.Active.SMS == "" {
		out.Active.SMS = "log"
	}
	return out
}

// AdminSettings lists the keys for the back office.
func (a *App) AdminSettings(ctx context.Context) (*SettingsView, error) {
	saved, _, err := a.readSettings(ctx, a.DB)
	if err != nil {
		return nil, wrap("settings", err)
	}
	v := a.settingsView(saved)
	if n, err := a.OTPSentToday(ctx); err == nil {
		v.OTPSentToday = &n
	}
	return v, nil
}

func checkSetting(key, v string) error {
	if len(v) > maxSettingLen {
		return ErrValidation(key + " is too long")
	}
	switch key {
	case "FCM_SERVICE_ACCOUNT":
		// the environment may name a file; the back office takes the JSON only
		if !strings.HasPrefix(v, "{") {
			return ErrValidation("FCM_SERVICE_ACCOUNT must be the JSON content of the service account file")
		}
	case "SMS_PROVIDER":
		if v != "log" && v != "webhook" && v != "twilio" {
			return ErrValidation("SMS_PROVIDER must be log, webhook or twilio")
		}
	case "SMS_WEBHOOK_URL":
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return ErrValidation("SMS_WEBHOOK_URL must be an http(s) address")
		}
	case "VAPID_SUBJECT":
		if !strings.HasPrefix(v, "mailto:") && !strings.HasPrefix(v, "https://") {
			return ErrValidation("VAPID_SUBJECT must start with mailto: or https://")
		}
	}
	if key != "FCM_SERVICE_ACCOUNT" && strings.ContainsAny(v, "\r\n") {
		return ErrValidation(key + " must be a single line")
	}
	return nil
}

// AdminSaveSettings saves keys from the back office and puts them to use at
// once. An empty value removes the saved key, which falls back to the
// environment. Names that are not in values stay as they are. Nothing is
// saved when the result would not work (a provider without its token, a
// service account that is not one).
func (a *App) AdminSaveSettings(ctx context.Context, actor AdminActor, values map[string]string) (*SettingsView, error) {
	var set, cleared []string
	for key := range values {
		if settingDefByKey(key) == nil {
			return nil, ErrValidation("unknown setting " + key)
		}
		values[key] = strings.TrimSpace(values[key])
		if values[key] == "" {
			cleared = append(cleared, key)
			continue
		}
		if err := checkSetting(key, values[key]); err != nil {
			return nil, err
		}
		set = append(set, key)
	}
	if len(values) == 0 {
		return a.AdminSettings(ctx)
	}
	sort.Strings(set)
	sort.Strings(cleared)

	var in *integrations
	var sig string
	var saved map[string]storedSetting
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		// one administrator at a time: the check below must see the other's keys
		if _, err := tx.Exec(ctx, `LOCK TABLE platform_settings IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return err
		}
		for _, key := range cleared {
			if _, err := tx.Exec(ctx, `DELETE FROM platform_settings WHERE key=$1`, key); err != nil {
				return err
			}
		}
		for _, key := range set {
			sealed, err := a.sealSetting(key, values[key])
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO platform_settings (key, value, updated_by) VALUES ($1,$2,$3)
				ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now(), updated_by=EXCLUDED.updated_by`,
				key, sealed, actor.ID); err != nil {
				return err
			}
		}
		var err error
		if saved, sig, err = a.readSettings(ctx, tx); err != nil {
			return err
		}
		if in, err = buildIntegrations(a.merged(saved)); err != nil {
			return Err(http.StatusUnprocessableEntity, "SETTINGS_INCOMPLETE", err.Error())
		}
		// the names only, never the values
		return adminAudit(ctx, tx, actor, "settings.update", "settings", 0, map[string]any{"set": set, "cleared": cleared})
	})
	if err != nil {
		return nil, wrap("save settings", err)
	}
	a.install(in, sig)
	return a.settingsView(saved), nil
}

// AdminNewVAPIDKeys makes a fresh web push key pair and saves it. Browsers
// that subscribed with the old pair have to turn reminders on again.
func (a *App) AdminNewVAPIDKeys(ctx context.Context, actor AdminActor) (*SettingsView, error) {
	private, public, err := NewVAPIDKeys()
	if err != nil {
		return nil, wrap("vapid keys", err)
	}
	return a.AdminSaveSettings(ctx, actor, map[string]string{"VAPID_PUBLIC_KEY": public, "VAPID_PRIVATE_KEY": private})
}
