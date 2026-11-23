package app

import (
	"context"
	"encoding/json"
	"strings"
)

// The platform's own texts customers read: mall categories and banners
// (docs/I18N_DESIGN.md §4). They are written in Simplified Chinese with an
// optional English; the rest is translated by a "translate_platform" job
// and kept in content_translations with no store.

const platformLang = "zh"

func platformFields(ctx context.Context, q Querier) ([]transField, error) {
	var out []transField
	add := func(entity string, id int64, field, text, en string) {
		if text = strings.TrimSpace(text); text != "" {
			out = append(out, transField{Entity: entity, ID: id, Field: field, Text: text, En: strings.TrimSpace(en)})
		}
	}
	rows, err := q.Query(ctx, `SELECT id, name, name_en FROM store_categories ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name, en string
		if err := rows.Scan(&id, &name, &en); err != nil {
			rows.Close()
			return nil, err
		}
		add("mall_category", id, "name", name, en)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT id, title, title_en, subtitle, subtitle_en FROM mall_banners ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var title, titleEn, sub, subEn string
		if err := rows.Scan(&id, &title, &titleEn, &sub, &subEn); err != nil {
			return nil, err
		}
		add("mall_banner", id, "title", title, titleEn)
		add("mall_banner", id, "subtitle", sub, subEn)
	}
	return out, rows.Err()
}

func loadPlatformTranslations(ctx context.Context, q Querier, langs []string) (map[string]Translation, error) {
	rows, err := q.Query(ctx, `SELECT entity, entity_id, field, lang, text, status, provider, updated_at, source_hash
		FROM content_translations WHERE branch_id IS NULL AND (COALESCE(cardinality($1::text[]), 0) = 0 OR lang = ANY($1))`, langs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Translation{}
	for rows.Next() {
		var t Translation
		if err := rows.Scan(&t.Entity, &t.EntityID, &t.Field, &t.Lang, &t.Text, &t.Status, &t.Provider, &t.UpdatedAt, &t.hash); err != nil {
			return nil, err
		}
		out[transField{Entity: t.Entity, ID: t.EntityID, Field: t.Field}.key(t.Lang)] = t
	}
	return out, rows.Err()
}

// QueueTranslatePlatform asks for the platform's texts to be translated a few
// seconds from now.
func (a *App) QueueTranslatePlatform(ctx context.Context) error {
	p, _ := json.Marshal(map[string]any{})
	_, err := a.DB.Exec(ctx, `INSERT INTO jobs (kind, payload, run_at)
		SELECT 'translate_platform', $1::jsonb, now() + interval '3 seconds'
		WHERE NOT EXISTS (SELECT 1 FROM jobs WHERE kind='translate_platform' AND status='pending' AND attempts=0)`, p)
	return err
}

// TranslatePlatform brings the translations of the mall's categories and
// banners up to date. Texts that are gone lose their translations.
func (a *App) TranslatePlatform(ctx context.Context, force bool) error {
	fields, err := platformFields(ctx, a.DB)
	if err != nil {
		return err
	}
	have, err := loadPlatformTranslations(ctx, a.DB, nil)
	if err != nil {
		return err
	}
	if _, err := a.DB.Exec(ctx, `DELETE FROM content_translations t WHERE branch_id IS NULL AND NOT (
		(t.entity = 'mall_category' AND EXISTS (SELECT 1 FROM store_categories c WHERE c.id = t.entity_id)) OR
		(t.entity = 'mall_banner' AND EXISTS (SELECT 1 FROM mall_banners b WHERE b.id = t.entity_id)))`); err != nil {
		return err
	}
	skip := func(_ transField, l string) bool { return l == platformLang }
	return a.syncTranslations(ctx, nil, platformLang, fields, have, skip, force)
}

// platformLocale picks the platform's texts in one language.
type platformLocale struct {
	lang string
	tr   map[string]Translation
}

func loadPlatformLocale(ctx context.Context, q Querier, lang string) (*platformLocale, error) {
	if !IsLang(lang) || lang == platformLang {
		return nil, nil
	}
	tr, err := loadPlatformTranslations(ctx, q, []string{lang, "en"})
	if err != nil {
		return nil, err
	}
	return &platformLocale{lang: lang, tr: tr}, nil
}

// text is the field in the reader's language, then English; "" leaves the
// original.
func (p *platformLocale) text(entity string, id int64, field, orig, origEn string) string {
	if p == nil || strings.TrimSpace(orig) == "" {
		return ""
	}
	f := transField{Entity: entity, ID: id, Field: field}
	if t, ok := p.tr[f.key(p.lang)]; ok {
		return t.Text
	}
	if p.lang == "zh-Hant" {
		return convertChinese(orig, "zh-Hant")
	}
	if origEn != "" {
		return origEn
	}
	if t, ok := p.tr[f.key("en")]; ok {
		return t.Text
	}
	return ""
}

// LocalizeMallHome names the mall's categories and banners in the reader's language.
func (a *App) LocalizeMallHome(ctx context.Context, h *MallHome, lang string) error {
	p, err := loadPlatformLocale(ctx, a.DB, lang)
	if err != nil || p == nil {
		return err
	}
	for i := range h.Categories {
		c := &h.Categories[i]
		c.NameTr = p.text("mall_category", c.ID, "name", c.Name, c.NameEn)
	}
	for i := range h.Banners {
		b := &h.Banners[i]
		b.TitleTr = p.text("mall_banner", b.ID, "title", b.Title, b.TitleEn)
		b.SubtitleTr = p.text("mall_banner", b.ID, "subtitle", b.Subtitle, b.SubtitleEn)
	}
	return nil
}
