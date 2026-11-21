package app

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Templates (design §16). A template is presentation only: which of the three
// menu layouts a channel uses, plus logo, cover and theme colour. Every layout
// runs the same menu, cart, pricing and ordering code.

// TemplateChannels are the ordering entrances that are themed separately.
var TemplateChannels = []string{"delivery", "qr", "ipad"}

var templateLayouts = map[string]bool{"list": true, "featured": true, "grid": true}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Template is one channel's look.
type Template struct {
	Layout     string `json:"layout"`      // list, featured or grid
	ThemeColor string `json:"theme_color"` // #rrggbb
	LogoURL    string `json:"logo_url"`
	CoverURL   string `json:"cover_url"`
}

// DefaultTemplate is what a channel uses until the merchant publishes one.
func DefaultTemplate(channel string) Template {
	layout := "list"
	if channel == "ipad" {
		layout = "grid"
	}
	return Template{Layout: layout, ThemeColor: "#d9480f"}
}

func (t *Template) normalize() error {
	t.Layout = strings.TrimSpace(t.Layout)
	t.ThemeColor = strings.ToLower(strings.TrimSpace(t.ThemeColor))
	t.LogoURL, t.CoverURL = strings.TrimSpace(t.LogoURL), strings.TrimSpace(t.CoverURL)
	if !templateLayouts[t.Layout] {
		return ErrValidation("layout must be list, featured or grid")
	}
	if !hexColor.MatchString(t.ThemeColor) {
		return ErrValidation("theme colour must look like #d9480f")
	}
	for _, u := range []string{t.LogoURL, t.CoverURL} {
		if u == "" {
			continue
		}
		p, err := url.Parse(u)
		if err != nil || p.Scheme != "https" || p.Host == "" || len(u) > 500 {
			return ErrValidation("image links must be https addresses of at most 500 characters")
		}
	}
	return nil
}

// ChannelTemplate is the merchant's view of one channel.
type ChannelTemplate struct {
	Channel     string     `json:"channel"`
	Published   Template   `json:"published"`
	Draft       *Template  `json:"draft"`
	PublishedAt *time.Time `json:"published_at"`
}

func validChannel(channel string) error {
	for _, c := range TemplateChannels {
		if c == channel {
			return nil
		}
	}
	return ErrNotFound
}

// ListTemplates returns all channels, filling in defaults for those never saved.
func (a *App) ListTemplates(ctx context.Context, branchID int64) ([]ChannelTemplate, error) {
	rows, err := a.DB.Query(ctx, `SELECT channel, published, draft, published_at FROM channel_templates WHERE branch_id=$1`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	saved := map[string]ChannelTemplate{}
	for rows.Next() {
		var ct ChannelTemplate
		var pub, draft []byte
		if err := rows.Scan(&ct.Channel, &pub, &draft, &ct.PublishedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(pub, &ct.Published); err != nil {
			return nil, err
		}
		if draft != nil {
			ct.Draft = &Template{}
			if err := json.Unmarshal(draft, ct.Draft); err != nil {
				return nil, err
			}
		}
		saved[ct.Channel] = ct
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ChannelTemplate, 0, len(TemplateChannels))
	for _, c := range TemplateChannels {
		ct, ok := saved[c]
		if !ok {
			ct = ChannelTemplate{Channel: c, Published: DefaultTemplate(c)}
		}
		out = append(out, ct)
	}
	return out, nil
}

// PublishedTemplates is what guests get: channel -> published template.
func (a *App) PublishedTemplates(ctx context.Context, branchID int64) (map[string]Template, error) {
	list, err := a.ListTemplates(ctx, branchID)
	if err != nil {
		return nil, err
	}
	out := map[string]Template{}
	for _, ct := range list {
		out[ct.Channel] = ct.Published
	}
	return out, nil
}

// SaveTemplateDraft stores a draft for one channel. Guests keep seeing the
// published template until PublishTemplate.
func (a *App) SaveTemplateDraft(ctx context.Context, branchID int64, channel string, t Template) error {
	if err := validChannel(channel); err != nil {
		return err
	}
	if err := t.normalize(); err != nil {
		return err
	}
	draft, _ := json.Marshal(t)
	def, _ := json.Marshal(DefaultTemplate(channel))
	_, err := a.DB.Exec(ctx, `INSERT INTO channel_templates (branch_id, channel, published, draft) VALUES ($1,$2,$3,$4)
		ON CONFLICT (branch_id, channel) DO UPDATE SET draft = EXCLUDED.draft, updated_at = now()`,
		branchID, channel, def, draft)
	return err
}

// DiscardTemplateDraft drops the draft and leaves the published template alone.
func (a *App) DiscardTemplateDraft(ctx context.Context, branchID int64, channel string) error {
	if err := validChannel(channel); err != nil {
		return err
	}
	_, err := a.DB.Exec(ctx, `UPDATE channel_templates SET draft = NULL, updated_at = now() WHERE branch_id=$1 AND channel=$2`,
		branchID, channel)
	return err
}

// PublishTemplate makes one channel's draft live. Other channels are not
// touched. Open pages get a refresh hint and re-read the storefront.
func (a *App) PublishTemplate(ctx context.Context, branchID int64, channel string, actor Actor) error {
	if err := validChannel(channel); err != nil {
		return err
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var draft []byte
		err := tx.QueryRow(ctx, `SELECT draft FROM channel_templates WHERE branch_id=$1 AND channel=$2 FOR UPDATE`,
			branchID, channel).Scan(&draft)
		if isNoRows(err) || (err == nil && draft == nil) {
			return Err(409, "NO_DRAFT", "there is no draft to publish")
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE channel_templates SET published = draft, draft = NULL, published_at = now(),
			updated_at = now() WHERE branch_id=$1 AND channel=$2`, branchID, channel); err != nil {
			return err
		}
		var t Template
		_ = json.Unmarshal(draft, &t)
		if err := audit(ctx, tx, branchID, 0, actor, "template.published",
			map[string]any{"channel": channel, "layout": t.Layout, "theme_color": t.ThemeColor}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID})
	})
}
