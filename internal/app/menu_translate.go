package app

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Menu translation (docs/I18N_DESIGN.md §6). The menu tables keep what the
// merchant wrote; content_translations has it in the other languages. A
// "translate_menu" job brings a store's translations up to date after any
// change to its menu.

// transField is one piece of a store's menu that gets translated.
type transField struct {
	Entity string `json:"entity"` // category, item, group, option, promotion
	ID     int64  `json:"entity_id"`
	Field  string `json:"field"` // name, description, details
	Text   string `json:"text"`
	// En is the English name kept on the record itself (name_en), if any.
	En string `json:"-"`
	// Owner is the record whose language window shows this text: an option
	// belongs to its item, a flavour tag to its template.
	Owner string `json:"owner"`
}

func (f transField) key(lang string) string {
	return f.Entity + "/" + strconv.FormatInt(f.ID, 10) + "/" + f.Field + "/" + lang
}

func sourceHash(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// storeInfoLang: a store's or brand's name goes into Simplified Chinese,
// English and Traditional Chinese on its own; a name is a sign, and a machine
// making up eleven of them does harm. The merchant may write any other
// language by hand (docs/I18N_SIMPLE_DESIGN.md §4.5).
func storeInfoLang(l string) bool { return l == "zh" || l == "en" || l == "zh-Hant" }

// nameOnly reports the texts storeInfoLang holds back.
func nameOnly(f transField) bool {
	return (f.Entity == "branch" || f.Entity == "brand") && f.Field == "name"
}

// brandItemKey names a brand's own name of an item in the translations: the
// row has no id of its own, and is written again on every save of the brand's
// catalog.
func brandItemKey(brandID, itemID int64) int64 { return brandID<<32 + itemID }

// menuFields lists everything of a store that gets translated.
func menuFields(ctx context.Context, q Querier, branchID int64) ([]transField, error) {
	var out []transField
	add := func(sql string, fields ...string) error {
		rows, err := q.Query(ctx, sql, branchID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var en, owner string
			texts := make([]string, len(fields))
			dst := []any{&id, &en, &owner}
			for i := range texts {
				dst = append(dst, &texts[i])
			}
			if err := rows.Scan(dst...); err != nil {
				return err
			}
			for i, f := range fields {
				ent := strings.SplitN(f, ".", 2)
				tf := transField{Entity: ent[0], ID: id, Field: ent[1], Text: strings.TrimSpace(texts[i]), Owner: owner}
				if i == 0 { // the English kept on the record is of its name
					tf.En = strings.TrimSpace(en)
				}
				if tf.Text != "" {
					out = append(out, tf)
				}
			}
		}
		return rows.Err()
	}
	steps := []struct {
		sql    string
		fields []string
	}{
		{`SELECT id, name_en, 'category/' || id, name FROM menu_categories WHERE branch_id=$1 AND NOT archived ORDER BY sort, id`, []string{"category.name"}},
		{`SELECT id, name_en, 'item/' || id, name, description, details FROM menu_items WHERE branch_id=$1 AND NOT archived ORDER BY sort, id`,
			[]string{"item.name", "item.description", "item.details"}},
		{`SELECT g.id, g.name_en, 'item/' || i.id, g.name FROM option_groups g JOIN menu_items i ON i.id = g.item_id
			WHERE i.branch_id=$1 AND NOT i.archived AND g.topping_template_id IS NULL ORDER BY g.sort, g.id`, []string{"group.name"}},
		{`SELECT o.id, o.name_en, 'item/' || i.id, o.name FROM options o JOIN option_groups g ON g.id = o.group_id JOIN menu_items i ON i.id = g.item_id
			WHERE i.branch_id=$1 AND NOT i.archived AND o.topping_id IS NULL ORDER BY o.sort, o.id`, []string{"option.name"}},
		// toppings are translated once, on their template, for every item that uses it
		{`SELECT id, '', 'topping_template/' || id, title FROM topping_templates WHERE branch_id=$1 ORDER BY sort, id`, []string{"topping_template.title"}},
		{`SELECT o.id, o.name_en, 'topping_template/' || t.id, o.name FROM topping_options o JOIN topping_templates t ON t.id = o.template_id
			WHERE t.branch_id=$1 ORDER BY t.sort, t.id, o.sort, o.id`, []string{"topping_option.name"}},
		{`SELECT g.id, g.name_en, 'note_template/' || t.id, g.name FROM note_groups g JOIN note_templates t ON t.id = g.template_id
			WHERE t.branch_id=$1 ORDER BY t.sort, t.id, g.sort, g.id`, []string{"note_group.name"}},
		{`SELECT x.id, x.name_en, 'note_template/' || t.id, x.name FROM note_tags x JOIN note_groups g ON g.id = x.group_id JOIN note_templates t ON t.id = g.template_id
			WHERE t.branch_id=$1 ORDER BY t.sort, t.id, g.sort, g.id, x.sort, x.id`, []string{"note_tag.name"}},
		{`SELECT id, name_en, 'promotion/' || id, name FROM promotions WHERE branch_id=$1 AND status <> 'ended' ORDER BY id`, []string{"promotion.name"}},
		// the store's own name and texts (docs/I18N_SIMPLE_DESIGN.md §4.5)
		{`SELECT id, name_en, 'branch/' || id, name, notice, intro FROM branches WHERE id=$1`, []string{"branch.name", "branch.notice", "branch.intro"}},
		// the rest of what customers read (§4.8)
		{`SELECT id, name_en, 'brand/' || id, name, intro FROM brands WHERE branch_id=$1 AND NOT is_main ORDER BY id`, []string{"brand.name", "brand.intro"}},
		{`SELECT c.id, c.name_en, 'brand_category/' || c.id, c.name FROM brand_categories c JOIN brands b ON b.id = c.brand_id
			WHERE b.branch_id=$1 ORDER BY b.id, c.sort, c.id`, []string{"brand_category.name"}},
		{`SELECT (bi.brand_id << 32) + bi.item_id, bi.name_en, 'brand_item/' || ((bi.brand_id << 32) + bi.item_id), bi.name
			FROM brand_items bi JOIN brands b ON b.id = bi.brand_id WHERE b.branch_id=$1 AND bi.name <> '' ORDER BY b.id, bi.sort`, []string{"brand_item.name"}},
		{`SELECT id, COALESCE(names->>'en', ''), 'stored_campaign/' || id, name, note FROM stored_campaigns
			WHERE branch_id=$1 AND (ends_at IS NULL OR ends_at > now()) ORDER BY id`, []string{"stored_campaign.name", "stored_campaign.note"}},
		{`SELECT t.id, '', 'stored_campaign/' || c.id, t.label FROM stored_tiers t JOIN stored_campaigns c ON c.id = t.campaign_id
			WHERE c.branch_id=$1 AND NOT t.removed AND (c.ends_at IS NULL OR c.ends_at > now()) ORDER BY c.id, t.sort, t.id`, []string{"stored_tier.label"}},
		{`SELECT id, name_en, 'coupon_batch/' || id, name FROM coupon_batches WHERE branch_id=$1 AND status <> 'ended' ORDER BY id`, []string{"coupon_batch.name"}},
		{`SELECT id, '', 'media/' || id, title FROM store_media WHERE branch_id=$1 ORDER BY id`, []string{"media.title"}},
		{`SELECT id, label_en, 'queue/' || branch_id, label FROM queue_groups WHERE branch_id=$1 AND active ORDER BY sort, id`, []string{"queue_group.label"}},
	}
	for _, s := range steps {
		if err := add(s.sql, s.fields...); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Translation is one stored translation.
type Translation struct {
	Entity    string    `json:"entity"`
	EntityID  int64     `json:"entity_id"`
	Field     string    `json:"field"`
	Lang      string    `json:"lang"`
	Text      string    `json:"text"`
	Status    string    `json:"status"` // machine, converted, human, stale
	Provider  string    `json:"provider"`
	UpdatedAt time.Time `json:"updated_at"`
	// Prev is the merchant's own translation of an earlier wording, replaced
	// when the original changed; the language window offers it back.
	Prev string `json:"prev,omitempty"`
	hash string
}

func loadTranslations(ctx context.Context, q Querier, branchID int64, langs []string) (map[string]Translation, error) {
	rows, err := q.Query(ctx, `SELECT entity, entity_id, field, lang, text, status, provider, updated_at, source_hash, prev_text
		FROM content_translations WHERE branch_id=$1 AND (COALESCE(cardinality($2::text[]), 0) = 0 OR lang = ANY($2))`, branchID, langs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Translation{}
	for rows.Next() {
		var t Translation
		if err := rows.Scan(&t.Entity, &t.EntityID, &t.Field, &t.Lang, &t.Text, &t.Status, &t.Provider, &t.UpdatedAt, &t.hash, &t.Prev); err != nil {
			return nil, err
		}
		out[transField{Entity: t.Entity, ID: t.EntityID, Field: t.Field}.key(t.Lang)] = t
	}
	return out, rows.Err()
}

// upsertTranslation stores one translation. prev is the merchant's own earlier
// translation it replaces, "" for none.
func upsertTranslation(ctx context.Context, q Querier, branchID any, f transField, lang, text, status, provider, prev string,
	by *int64) error {
	if _, err := q.Exec(ctx, `INSERT INTO content_translations (entity, entity_id, field, lang, branch_id, text, status,
			source_hash, provider, updated_by, updated_at, prev_text)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),$11)
		ON CONFLICT (entity, entity_id, field, lang) DO UPDATE SET text=$6, status=$7, source_hash=$8, provider=$9,
			updated_by=$10, updated_at=now(), prev_text=$11`,
		f.Entity, f.ID, f.Field, lang, branchID, text, status, sourceHash(f.Text), provider, by, prev); err != nil {
		return err
	}
	return syncNameEn(ctx, q, f, lang, text, status)
}

// syncNameEn keeps a record's name_en equal to the merchant's own English
// name, and empty when the English is the translation service's: kitchen
// screens and tickets print name_en next to the original, and only what the
// merchant wrote belongs there.
func syncNameEn(ctx context.Context, q Querier, f transField, lang, text, status string) error {
	sql := nameEnSQL[f.Entity+"."+f.Field]
	if lang != "en" || sql == "" {
		return nil
	}
	if status != "human" {
		text = ""
	}
	if text == f.En {
		return nil
	}
	_, err := q.Exec(ctx, sql, f.ID, text)
	return err
}

// ---- the job ----

type translateJob struct {
	BranchID int64 `json:"branch_id"`
	// Force translates again what the service translated before (the
	// merchant's own translations are kept).
	Force bool `json:"force,omitempty"`
}

// QueueTranslate asks for the store's menu to be translated a few seconds
// from now. Saves in quick succession share one job.
func (a *App) QueueTranslate(ctx context.Context, branchID int64, force bool) error {
	p, _ := json.Marshal(translateJob{BranchID: branchID, Force: force})
	_, err := a.DB.Exec(ctx, `INSERT INTO jobs (kind, payload, run_at)
		SELECT 'translate_menu', $1::jsonb, now() + interval '3 seconds'
		WHERE NOT EXISTS (SELECT 1 FROM jobs WHERE kind='translate_menu' AND status='pending' AND attempts=0
			AND payload = $1::jsonb)`, p)
	return err
}

// TranslateMenu brings a store's translations up to date: new and changed
// text is translated, the merchant's own translations of changed text are
// marked for checking, and Traditional Chinese is converted from Simplified.
func (a *App) TranslateMenu(ctx context.Context, branchID int64, force bool) error {
	var menuLang string
	var off []string
	if err := a.DB.QueryRow(ctx, `SELECT menu_lang, menu_langs_off FROM branches WHERE id=$1`, branchID).
		Scan(&menuLang, &off); err != nil {
		if isNoRows(err) {
			return nil
		}
		return err
	}
	fields, err := menuFields(ctx, a.DB, branchID)
	if err != nil {
		return err
	}
	have, err := loadTranslations(ctx, a.DB, branchID, nil)
	if err != nil {
		return err
	}
	skip := func(f transField, l string) bool {
		return l == menuLang || (l != "en" && slices.Contains(off, l) && !nameOnly(f)) ||
			(nameOnly(f) && !storeInfoLang(l))
	}
	return a.translateDone(ctx, branchID, a.syncTranslations(ctx, branchID, menuLang, fields, have, skip, force))
}

// syncTranslations brings the translations of fields up to date. owner is the
// store's id, or nil for the platform's own texts.
func (a *App) syncTranslations(ctx context.Context, owner any, from string, fields []transField,
	have map[string]Translation, skip func(transField, string) bool, force bool) error {
	todo := map[string][]transField{}
	hints := map[string]string{} // key -> the merchant's own translation of the earlier wording
	for _, f := range fields {
		h := sourceHash(f.Text)
		for _, l := range Langs {
			if skip(f, l) {
				continue
			}
			t, ok := have[f.key(l)]
			// An English name put on the record outside the language window (an
			// import, the platform's forms) is the writer's own.
			// The platform's own texts always take the English typed on its forms.
			if l == "en" && f.En != "" && (owner == nil || !ok || t.Text != f.En) {
				if !ok || t.Text != f.En || t.hash != h || t.Status != "human" {
					if err := upsertTranslation(ctx, a.DB, owner, f, "en", f.En, "human", "", "", nil); err != nil {
						return err
					}
				}
				continue
			}
			hint := ""
			if ok {
				if t.hash == h && !(force && t.Status == "machine") {
					if err := syncNameEn(ctx, a.DB, f, l, t.Text, t.Status); err != nil {
						return err
					}
					continue
				}
				// The original changed: translate again, the merchant's own
				// translation too, and keep theirs to offer back (§4.4).
				hint = t.Prev
				if t.Status == "human" || t.Status == "stale" {
					hint = t.Text
				}
			}
			if (l == "zh-Hant" && from == "zh") || (l == "zh" && from == "zh-Hant") {
				if s := convertChinese(f.Text, l); s != "" {
					if err := upsertTranslation(ctx, a.DB, owner, f, l, s, "converted", "opencc", hint, nil); err != nil {
						return err
					}
				}
				continue
			}
			if hint != "" {
				hints[f.key(l)] = hint
			}
			todo[l] = append(todo[l], f)
		}
	}
	tr := a.translator()
	if tr == nil || len(todo) == 0 {
		return nil
	}
	for _, l := range Langs {
		list := todo[l]
		for start := 0; start < len(list); start += translateBatch {
			part := list[start:min(start+translateBatch, len(list))]
			texts := make([]string, len(part))
			earlier := make([]string, len(part))
			hinted := false
			for i, f := range part {
				texts[i] = f.Text
				earlier[i] = hints[f.key(l)]
				hinted = hinted || earlier[i] != ""
			}
			var out []string
			var err error
			if ht, ok := tr.(HintTranslator); ok && hinted {
				out, err = ht.TranslateWithHints(ctx, from, l, texts, earlier)
			} else {
				out, err = tr.Translate(ctx, from, l, texts)
			}
			if err != nil {
				return err
			}
			for i, f := range part {
				if err := upsertTranslation(ctx, a.DB, owner, f, l, out[i], "machine", tr.Name(), earlier[i], nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// translateDone notes on the store whether the last run failed, so the
// merchant sees it on the menu page.
func (a *App) translateDone(ctx context.Context, branchID int64, runErr error) error {
	msg := ""
	if runErr != nil {
		msg = truncate(runErr.Error(), 300)
	}
	if _, err := a.DB.Exec(ctx, `UPDATE branches SET translate_error=$2,
		translate_error_at = CASE WHEN $2 = '' THEN NULL ELSE now() END WHERE id=$1`, branchID, msg); err != nil {
		return err
	}
	return runErr
}

// ---- what customers see ----

// menuLocale picks the text of the store's menu in one language.
type menuLocale struct {
	lang, menuLang string
	tr             map[string]Translation
	// Machine is set once a machine translation was shown.
	Machine bool
}

// localeFor loads a store's translations for a customer reading lang. It
// returns nil when the customer reads the menu's own language.
func localeFor(ctx context.Context, q Querier, b *Branch, lang string) (*menuLocale, error) {
	if !IsLang(lang) || lang == b.MenuLang {
		return nil, nil
	}
	if slices.Contains(b.MenuLangsOff, lang) {
		lang = "en"
		if lang == b.MenuLang {
			return nil, nil
		}
	}
	tr, err := loadTranslations(ctx, q, b.ID, []string{lang, "en"})
	if err != nil {
		return nil, err
	}
	return &menuLocale{lang: lang, menuLang: b.MenuLang, tr: tr}, nil
}

// text is the field in the customer's language, falling back to English;
// "" leaves the original.
func (m *menuLocale) text(entity string, id int64, field, orig, origEn string) string {
	if m == nil || strings.TrimSpace(orig) == "" {
		return ""
	}
	f := transField{Entity: entity, ID: id, Field: field}
	if t, ok := m.tr[f.key(m.lang)]; ok {
		m.Machine = m.Machine || t.Status == "machine"
		return t.Text
	}
	if m.lang == "zh-Hant" && m.menuLang == "zh" {
		return convertChinese(orig, "zh-Hant")
	}
	if m.lang == "en" || m.menuLang == "en" {
		return origEn
	}
	if t, ok := m.tr[f.key("en")]; ok {
		m.Machine = m.Machine || t.Status == "machine"
		return t.Text
	}
	return origEn
}

func (m *menuLocale) item(it *Item) {
	it.NameTr = m.text("item", it.ID, "name", it.Name, it.NameEn)
	if it.BaseName != "" {
		// a brand's own name for the dish: the store's translations are of its name
		it.NameTr = m.text("brand_item", it.brandKey, "name", it.Name, it.NameEn)
	}
	it.DescriptionTr = m.text("item", it.ID, "description", it.Description, "")
	it.DetailsTr = m.text("item", it.ID, "details", it.Details, "")
	for i := range it.Groups {
		g := &it.Groups[i]
		g.NameTr = m.text("group", g.ID, "name", g.Name, g.NameEn)
		if g.ToppingTemplateID != nil { // translated once, on the template
			g.NameTr = m.text("topping_template", *g.ToppingTemplateID, "title", g.Name, g.NameEn)
		}
		for j := range g.Options {
			o := &g.Options[j]
			o.NameTr = m.text("option", o.ID, "name", o.Name, o.NameEn)
			if o.toppingID != nil {
				o.NameTr = m.text("topping_option", *o.toppingID, "name", o.Name, o.NameEn)
			}
		}
	}
	for i := range it.Notes {
		g := &it.Notes[i]
		g.NameTr = m.text("note_group", g.ID, "name", g.Name, g.NameEn)
		for j := range g.Tags {
			x := &g.Tags[j]
			x.NameTr = m.text("note_tag", x.ID, "name", x.Name, x.NameEn)
		}
	}
}

// LocalizeStorefront fills in the menu in the customer's language.
func (a *App) LocalizeStorefront(ctx context.Context, sf *Storefront, lang string) error {
	if err := localizeBranch(ctx, a.DB, sf.Branch, lang); err != nil {
		return err
	}
	m, err := localeFor(ctx, a.DB, sf.Branch, lang)
	if err != nil || m == nil {
		return err
	}
	for i := range sf.Media {
		x := &sf.Media[i]
		x.TitleTr = m.text("media", x.ID, "title", x.Title, "")
	}
	for i := range sf.Categories {
		c := &sf.Categories[i]
		entity := "category"
		if c.brand {
			entity = "brand_category" // a brand's own: the store's translations are not of it
		}
		c.NameTr = m.text(entity, c.ID, "name", c.Name, c.NameEn)
		for j := range c.Items {
			m.item(&c.Items[j])
		}
	}
	for i := range sf.Promotions {
		p := &sf.Promotions[i]
		p.NameTr = m.text("promotion", p.ID, "name", p.Name, p.NameEn)
	}
	sf.MachineTranslated = m.Machine
	return nil
}

// localizeLines names the priced lines in the customer's language; an order
// keeps that name next to the original.
func localizeLines(ctx context.Context, q Querier, b *Branch, lang string, lines []QuoteLine) error {
	m, err := localeFor(ctx, q, b, lang)
	if err != nil || m == nil {
		return err
	}
	items, err := loadItems(ctx, q, b.ID, true)
	if err != nil {
		return err
	}
	byID := map[int64]*Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for i := range lines {
		l := &lines[i]
		it := byID[l.ItemID]
		if it == nil {
			continue
		}
		m.item(it)
		l.NameTr = it.NameTr
		if l.BaseName != "" {
			l.NameTr = ""
			if b.brandID != 0 {
				l.NameTr = m.text("brand_item", brandItemKey(b.brandID, it.ID), "name", l.Name, l.NameEn)
			}
		}
		opts := map[string]string{}
		groups := map[string]string{}
		for _, g := range it.Groups {
			groups[g.Name] = g.NameTr
			for _, o := range g.Options {
				opts[g.Name+"\x00"+o.Name] = o.NameTr
			}
		}
		for j := range l.Options {
			o := &l.Options[j]
			o.GroupTr, o.NameTr = groups[o.Group], opts[o.Group+"\x00"+o.Name]
		}
		tags := map[int64][2]string{}
		for _, g := range it.Notes {
			for _, x := range g.Tags {
				tags[x.ID] = [2]string{g.NameTr, x.NameTr}
			}
		}
		for j := range l.NoteTags {
			x := &l.NoteTags[j]
			x.GroupTr, x.NameTr = tags[x.ID][0], tags[x.ID][1]
		}
	}
	return nil
}

// ---- the merchant's view ----

// MenuTranslations is what the language window, the settings card and the
// page of all translations show.
type MenuTranslations struct {
	MenuLang     string     `json:"menu_lang"`
	MenuLangsOff []string   `json:"menu_langs_off"`
	Provider     string     `json:"provider"` // "" when no service is set up
	Pending      bool       `json:"pending"`  // a translation run is waiting or running
	Error        string     `json:"error"`
	ErrorAt      *time.Time `json:"error_at"`
	// DefaultLang is the language the window opens on: the one this store's
	// customers ordered in most over the last 30 days, English without orders.
	DefaultLang  string        `json:"default_lang"`
	UpdatedAt    *time.Time    `json:"updated_at"` // the newest translation
	Fields       []transField  `json:"fields"`
	Translations []Translation `json:"translations"`
}

// MenuTranslations lists a store's texts and their translations. scope narrows
// it to one record's language window ("item/12", "branch/3"); "" is everything
// and "none" only the settings.
func (a *App) MenuTranslations(ctx context.Context, branchID int64, scope string) (*MenuTranslations, error) {
	out := &MenuTranslations{Fields: []transField{}, Translations: []Translation{}}
	if err := a.DB.QueryRow(ctx, `SELECT menu_lang, menu_langs_off, translate_error, translate_error_at,
			EXISTS (SELECT 1 FROM jobs WHERE kind='translate_menu' AND status='pending' AND (payload->>'branch_id')::bigint = branches.id),
			(SELECT max(updated_at) FROM content_translations WHERE branch_id = branches.id)
		FROM branches WHERE id=$1 AND deleted_at IS NULL`, branchID).
		Scan(&out.MenuLang, &out.MenuLangsOff, &out.Error, &out.ErrorAt, &out.Pending, &out.UpdatedAt); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if tr := a.translator(); tr != nil {
		out.Provider = tr.Name()
	}
	out.DefaultLang = "en"
	var top string
	err := a.DB.QueryRow(ctx, `SELECT customer_lang FROM orders WHERE branch_id=$1 AND created_at > now() - interval '30 days'
		AND customer_lang <> '' AND customer_lang <> $2 AND customer_lang <> ALL($3)
		GROUP BY customer_lang ORDER BY count(*) DESC, customer_lang LIMIT 1`, branchID, out.MenuLang, out.MenuLangsOff).Scan(&top)
	if err != nil && !isNoRows(err) {
		return nil, err
	}
	if IsLang(top) {
		out.DefaultLang = top
	} else if out.MenuLang == "en" {
		out.DefaultLang = "zh"
	}
	if scope == "none" {
		return out, nil
	}
	fields, err := menuFields(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	shown := map[string]bool{}
	for _, f := range fields {
		if scope == "" || f.Owner == scope {
			out.Fields = append(out.Fields, f)
			shown[f.key("")] = true
		}
	}
	have, err := loadTranslations(ctx, a.DB, branchID, nil)
	if err != nil {
		return nil, err
	}
	for _, t := range have {
		if shown[transField{Entity: t.Entity, ID: t.EntityID, Field: t.Field}.key("")] {
			out.Translations = append(out.Translations, t)
		}
	}
	slices.SortFunc(out.Translations, func(x, y Translation) int {
		return strings.Compare(transField{Entity: x.Entity, ID: x.EntityID, Field: x.Field}.key(x.Lang),
			transField{Entity: y.Entity, ID: y.EntityID, Field: y.Field}.key(y.Lang))
	})
	return out, nil
}

// TranslationEdit is the merchant changing one translation.
type TranslationEdit struct {
	Entity   string `json:"entity"`
	EntityID int64  `json:"entity_id"`
	Field    string `json:"field"`
	Lang     string `json:"lang"`
	// Text is the merchant's translation; "" hands it back to the service.
	Text string `json:"text"`
}

// nameEnSQL sets the English kept on a record itself ($1 the id, $2 the text).
var nameEnSQL = map[string]string{
	"category.name": `UPDATE menu_categories SET name_en=$2 WHERE id=$1`, "item.name": `UPDATE menu_items SET name_en=$2 WHERE id=$1`,
	"group.name": `UPDATE option_groups SET name_en=$2 WHERE id=$1`, "option.name": `UPDATE options SET name_en=$2 WHERE id=$1`,
	"promotion.name": `UPDATE promotions SET name_en=$2 WHERE id=$1`, "branch.name": `UPDATE branches SET name_en=$2 WHERE id=$1`,
	"note_group.name": `UPDATE note_groups SET name_en=$2 WHERE id=$1`, "note_tag.name": `UPDATE note_tags SET name_en=$2 WHERE id=$1`,
	"brand.name": `UPDATE brands SET name_en=$2 WHERE id=$1`, "brand_category.name": `UPDATE brand_categories SET name_en=$2 WHERE id=$1`,
	"brand_item.name":   `UPDATE brand_items SET name_en=$2 WHERE brand_id = $1::bigint >> 32 AND item_id = $1::bigint & 4294967295`,
	"coupon_batch.name": `UPDATE coupon_batches SET name_en=$2 WHERE id=$1`,
	"queue_group.label": `UPDATE queue_groups SET label_en=$2 WHERE id=$1`,
	// the options copied onto the items follow
	"topping_option.name": `WITH a AS (UPDATE topping_options SET name_en=$2 WHERE id=$1) UPDATE options SET name_en=$2 WHERE topping_id=$1`,
	"stored_campaign.name": `UPDATE stored_campaigns SET names = CASE WHEN $2::text = '' THEN names - 'en'
		ELSE jsonb_set(names, '{en}', to_jsonb($2::text)) END WHERE id=$1`,
}

// SaveTranslation stores the merchant's own translation of one field.
func (a *App) SaveTranslation(ctx context.Context, branchID int64, in TranslationEdit, actor Actor) error {
	return a.SaveTranslations(ctx, branchID, []TranslationEdit{in}, actor)
}

// SaveTranslations stores what the merchant changed in a language window: all
// of it or none.
func (a *App) SaveTranslations(ctx context.Context, branchID int64, edits []TranslationEdit, actor Actor) error {
	if len(edits) == 0 {
		return nil
	}
	if len(edits) > 500 {
		return ErrValidation("too many translations at once")
	}
	fields, err := menuFields(ctx, a.DB, branchID)
	if err != nil {
		return err
	}
	byKey := map[string]*transField{}
	for i := range fields {
		byKey[fields[i].key("")] = &fields[i]
	}
	var by *int64
	if actor.ID != 0 {
		by = &actor.ID
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		for _, in := range edits {
			in.Text = strings.TrimSpace(in.Text)
			if !IsLang(in.Lang) {
				return ErrValidation("unknown language")
			}
			if len(in.Text) > 4000 {
				return ErrValidation("translation is too long")
			}
			f := byKey[transField{Entity: in.Entity, ID: in.EntityID, Field: in.Field}.key("")]
			if f == nil {
				return ErrNotFound
			}
			if in.Text == "" {
				if _, err := tx.Exec(ctx, `DELETE FROM content_translations WHERE entity=$1 AND entity_id=$2 AND field=$3 AND lang=$4`,
					f.Entity, f.ID, f.Field, in.Lang); err != nil {
					return err
				}
				// the next run fills the English name in again
				if sql := nameEnSQL[f.Entity+"."+f.Field]; in.Lang == "en" && sql != "" {
					if _, err := tx.Exec(ctx, sql, f.ID, ""); err != nil {
						return err
					}
				}
				continue
			}
			if err := upsertTranslation(ctx, tx, branchID, *f, in.Lang, in.Text, "human", "", "", by); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return a.QueueTranslate(ctx, branchID, false)
}

// SaveMenuLangs sets the language the menu is written in and the languages
// customers get in English instead.
func (a *App) SaveMenuLangs(ctx context.Context, branchID int64, menuLang string, off []string) error {
	if !IsLang(menuLang) {
		return ErrValidation("unknown menu language")
	}
	clean := []string{}
	for _, l := range off {
		if !IsLang(l) {
			return ErrValidation("unknown language " + l)
		}
		if l != "en" && l != menuLang && !slices.Contains(clean, l) {
			clean = append(clean, l)
		}
	}
	tag, err := a.DB.Exec(ctx, `UPDATE branches SET menu_lang=$2, menu_langs_off=$3 WHERE id=$1 AND deleted_at IS NULL`,
		branchID, menuLang, clean)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return a.QueueTranslate(ctx, branchID, false)
}

var errNoTranslator = Err(http.StatusConflict, "NO_TRANSLATOR", "no translation service is set up")

// Retranslate translates the whole menu again; the merchant's own
// translations stay.
func (a *App) Retranslate(ctx context.Context, branchID int64) error {
	if a.translator() == nil {
		return errNoTranslator
	}
	return a.QueueTranslate(ctx, branchID, true)
}

// ---- the store's name, notice and introduction ----

// StoreTexts are a store's name, notice and introduction as one customer reads them.
// Tagline is a brand's own line on its mall card.
type StoreTexts struct{ Name, Notice, Intro, Tagline string }

// textsKey is where storeTexts puts a store's texts: under the store's id, or
// under minus the brand's for a brand that goes by its own name (several
// brands of one store are listed side by side in the mall).
func (b *Branch) textsKey() int64 {
	if b.brandNamed {
		return -b.brandID
	}
	return b.ID
}

// storeTexts picks the stores' texts for a customer reading lang
// (docs/I18N_SIMPLE_DESIGN.md §4.5): the customer's language when there is a
// translation (the notice and introduction go into every language, the name
// into three unless the merchant wrote more), then English, then the original.
// Readers of the language the store writes in get the original, and a language
// the store turned off reads English. "" leaves a field as it is.
func storeTexts(ctx context.Context, q Querier, lang string, stores []*Branch) (map[int64]StoreTexts, error) {
	out := map[int64]StoreTexts{}
	if len(stores) == 0 || !IsLang(lang) {
		return out, nil
	}
	ids := make([]int64, len(stores))
	for i, b := range stores {
		ids[i] = b.ID
	}
	var brands []int64
	for _, b := range stores {
		if b.brandNamed {
			brands = append(brands, b.brandID)
		}
	}
	rows, err := q.Query(ctx, `SELECT entity, entity_id, field, lang, text FROM content_translations
		WHERE ((entity='branch' AND entity_id = ANY($1)) OR (entity='brand' AND entity_id = ANY($3))) AND lang IN ($2, 'en')`,
		ids, lang, brands)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tr := map[string]string{}
	for rows.Next() {
		var id int64
		var entity, field, l, text string
		if err := rows.Scan(&entity, &id, &field, &l, &text); err != nil {
			return nil, err
		}
		tr[entity+"/"+strconv.FormatInt(id, 10)+"/"+field+"/"+l] = text
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, b := range stores {
		if lang == b.MenuLang {
			continue
		}
		want := lang
		if slices.Contains(b.MenuLangsOff, lang) {
			want = "en"
			if b.MenuLang == "en" {
				continue
			}
		}
		pickOf := func(entity string, id int64, field, orig, origEn string) string {
			if strings.TrimSpace(orig) == "" {
				return ""
			}
			key := entity + "/" + strconv.FormatInt(id, 10) + "/" + field + "/"
			if s, ok := tr[key+want]; ok {
				return s
			}
			if want == "zh-Hant" && b.MenuLang == "zh" {
				return convertChinese(orig, "zh-Hant")
			}
			if want == "en" || b.MenuLang == "en" {
				return origEn
			}
			if s, ok := tr[key+"en"]; ok {
				return s
			}
			return origEn
		}
		pick := func(field, orig, origEn string) string { return pickOf("branch", b.ID, field, orig, origEn) }
		x := StoreTexts{Name: pick("name", b.Name, b.NameEn), Notice: pick("notice", b.Notice, ""), Intro: pick("intro", b.Intro, "")}
		if b.brandNamed {
			x.Name = pickOf("brand", b.brandID, "name", b.Name, b.NameEn)
			x.Tagline = pickOf("brand", b.brandID, "intro", b.brandIntro, "")
		}
		out[b.textsKey()] = x
	}
	return out, nil
}

// localizeBranch fills in one store's texts in the customer's language.
func localizeBranch(ctx context.Context, q Querier, b *Branch, lang string) error {
	t, err := storeTexts(ctx, q, lang, []*Branch{b})
	if err != nil {
		return err
	}
	x := t[b.textsKey()]
	b.NameTr, b.NoticeTr, b.IntroTr = x.Name, x.Notice, x.Intro
	return nil
}

// storeNames are the stores' names in the customer's language, where they differ.
func storeNames(ctx context.Context, q Querier, lang string, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 || !IsLang(lang) {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT `+branchCols+` FROM branches WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	var list []*Branch
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	t, err := storeTexts(ctx, q, lang, list)
	if err != nil {
		return nil, err
	}
	for id, x := range t {
		if x.Name != "" {
			out[id] = x.Name
		}
	}
	return out, nil
}
