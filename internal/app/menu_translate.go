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
	// En is the English name the merchant typed on the form, if any.
	En string `json:"-"`
}

func (f transField) key(lang string) string {
	return f.Entity + "/" + strconv.FormatInt(f.ID, 10) + "/" + f.Field + "/" + lang
}

func sourceHash(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// storeInfoLang: the store's name, notice and introduction are kept in
// Simplified Chinese, English and Traditional Chinese only; the merchant
// confirms them, since a wrong store name does harm.
func storeInfoLang(l string) bool { return l == "zh" || l == "en" || l == "zh-Hant" }

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
			var en string
			texts := make([]string, len(fields))
			dst := []any{&id, &en}
			for i := range texts {
				dst = append(dst, &texts[i])
			}
			if err := rows.Scan(dst...); err != nil {
				return err
			}
			for i, f := range fields {
				ent := strings.SplitN(f, ".", 2)
				tf := transField{Entity: ent[0], ID: id, Field: ent[1], Text: strings.TrimSpace(texts[i])}
				if tf.Field == "name" {
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
		{`SELECT id, name_en, name FROM menu_categories WHERE branch_id=$1 AND NOT archived ORDER BY sort, id`, []string{"category.name"}},
		{`SELECT id, name_en, name, description, details FROM menu_items WHERE branch_id=$1 AND NOT archived ORDER BY sort, id`,
			[]string{"item.name", "item.description", "item.details"}},
		{`SELECT g.id, g.name_en, g.name FROM option_groups g JOIN menu_items i ON i.id = g.item_id
			WHERE i.branch_id=$1 AND NOT i.archived ORDER BY g.sort, g.id`, []string{"group.name"}},
		{`SELECT o.id, o.name_en, o.name FROM options o JOIN option_groups g ON g.id = o.group_id JOIN menu_items i ON i.id = g.item_id
			WHERE i.branch_id=$1 AND NOT i.archived ORDER BY o.sort, o.id`, []string{"option.name"}},
		{`SELECT id, name_en, name FROM promotions WHERE branch_id=$1 AND status <> 'ended' ORDER BY id`, []string{"promotion.name"}},
		// the store's own name and texts, in three languages only (docs/I18N_DESIGN.md §7)
		{`SELECT id, name_en, name, notice, intro FROM branches WHERE id=$1`, []string{"branch.name", "branch.notice", "branch.intro"}},
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
	hash      string
}

func loadTranslations(ctx context.Context, q Querier, branchID int64, langs []string) (map[string]Translation, error) {
	rows, err := q.Query(ctx, `SELECT entity, entity_id, field, lang, text, status, provider, updated_at, source_hash
		FROM content_translations WHERE branch_id=$1 AND (COALESCE(cardinality($2::text[]), 0) = 0 OR lang = ANY($2))`, branchID, langs)
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

func upsertTranslation(ctx context.Context, q Querier, branchID any, f transField, lang, text, status, provider string,
	by *int64) error {
	_, err := q.Exec(ctx, `INSERT INTO content_translations (entity, entity_id, field, lang, branch_id, text, status,
			source_hash, provider, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now())
		ON CONFLICT (entity, entity_id, field, lang) DO UPDATE SET text=$6, status=$7, source_hash=$8, provider=$9,
			updated_by=$10, updated_at=now()`,
		f.Entity, f.ID, f.Field, lang, branchID, text, status, sourceHash(f.Text), provider, by)
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
		return l == menuLang || (l != "en" && slices.Contains(off, l) && f.Entity != "branch") ||
			(f.Entity == "branch" && !storeInfoLang(l))
	}
	return a.translateDone(ctx, branchID, a.syncTranslations(ctx, branchID, menuLang, fields, have, skip, force))
}

// syncTranslations brings the translations of fields up to date. owner is the
// store's id, or nil for the platform's own texts.
func (a *App) syncTranslations(ctx context.Context, owner any, from string, fields []transField,
	have map[string]Translation, skip func(transField, string) bool, force bool) error {
	todo := map[string][]transField{}
	for _, f := range fields {
		h := sourceHash(f.Text)
		for _, l := range Langs {
			if skip(f, l) {
				continue
			}
			t, ok := have[f.key(l)]
			// The English typed on the form is the writer's own.
			if l == "en" && f.En != "" {
				if !ok || t.Text != f.En || t.hash != h || t.Status != "human" {
					if err := upsertTranslation(ctx, a.DB, owner, f, "en", f.En, "human", "", nil); err != nil {
						return err
					}
				}
				continue
			}
			if ok {
				switch {
				case t.Status == "stale":
					continue
				case t.Status == "human":
					if t.hash != h {
						if _, err := a.DB.Exec(ctx, `UPDATE content_translations SET status='stale'
							WHERE entity=$1 AND entity_id=$2 AND field=$3 AND lang=$4`, f.Entity, f.ID, f.Field, l); err != nil {
							return err
						}
					}
					continue
				case t.hash == h && !(force && t.Status == "machine"):
					continue
				}
			}
			if (l == "zh-Hant" && from == "zh") || (l == "zh" && from == "zh-Hant") {
				if s := convertChinese(f.Text, l); s != "" {
					if err := upsertTranslation(ctx, a.DB, owner, f, l, s, "converted", "opencc", nil); err != nil {
						return err
					}
				}
				continue
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
			for i, f := range part {
				texts[i] = f.Text
			}
			out, err := tr.Translate(ctx, from, l, texts)
			if err != nil {
				return err
			}
			for i, f := range part {
				if err := upsertTranslation(ctx, a.DB, owner, f, l, out[i], "machine", tr.Name(), nil); err != nil {
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
	it.DescriptionTr = m.text("item", it.ID, "description", it.Description, "")
	it.DetailsTr = m.text("item", it.ID, "details", it.Details, "")
	for i := range it.Groups {
		g := &it.Groups[i]
		g.NameTr = m.text("group", g.ID, "name", g.Name, g.NameEn)
		for j := range g.Options {
			o := &g.Options[j]
			o.NameTr = m.text("option", o.ID, "name", o.Name, o.NameEn)
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
	for i := range sf.Categories {
		c := &sf.Categories[i]
		c.NameTr = m.text("category", c.ID, "name", c.Name, c.NameEn)
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
	}
	return nil
}

// ---- the merchant's view ----

// MenuTranslations is the menu page's language card and checking grid.
type MenuTranslations struct {
	MenuLang     string        `json:"menu_lang"`
	MenuLangsOff []string      `json:"menu_langs_off"`
	Provider     string        `json:"provider"` // "" when no service is set up
	Pending      bool          `json:"pending"`  // a translation run is waiting or running
	Error        string        `json:"error"`
	ErrorAt      *time.Time    `json:"error_at"`
	Fields       []transField  `json:"fields"`
	Translations []Translation `json:"translations"`
}

func (a *App) MenuTranslations(ctx context.Context, branchID int64) (*MenuTranslations, error) {
	out := &MenuTranslations{Translations: []Translation{}}
	if err := a.DB.QueryRow(ctx, `SELECT menu_lang, menu_langs_off, translate_error, translate_error_at,
			EXISTS (SELECT 1 FROM jobs WHERE kind='translate_menu' AND status='pending' AND (payload->>'branch_id')::bigint = branches.id)
		FROM branches WHERE id=$1 AND deleted_at IS NULL`, branchID).
		Scan(&out.MenuLang, &out.MenuLangsOff, &out.Error, &out.ErrorAt, &out.Pending); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if tr := a.translator(); tr != nil {
		out.Provider = tr.Name()
	}
	var err error
	if out.Fields, err = menuFields(ctx, a.DB, branchID); err != nil {
		return nil, err
	}
	if out.Fields == nil {
		out.Fields = []transField{}
	}
	have, err := loadTranslations(ctx, a.DB, branchID, nil)
	if err != nil {
		return nil, err
	}
	for _, t := range have {
		out.Translations = append(out.Translations, t)
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
	// Keep confirms a translation whose original changed, as it is.
	Keep bool `json:"keep"`
}

var nameEnTables = map[string]string{"category": "menu_categories", "item": "menu_items", "group": "option_groups",
	"option": "options", "promotion": "promotions", "branch": "branches"}

// SaveTranslation stores the merchant's own translation of one field.
func (a *App) SaveTranslation(ctx context.Context, branchID int64, in TranslationEdit, actor Actor) error {
	in.Text = strings.TrimSpace(in.Text)
	if !IsLang(in.Lang) || (in.Entity == "branch" && !storeInfoLang(in.Lang)) {
		return ErrValidation("unknown language")
	}
	if len(in.Text) > 4000 {
		return ErrValidation("translation is too long")
	}
	fields, err := menuFields(ctx, a.DB, branchID)
	if err != nil {
		return err
	}
	var f *transField
	for i := range fields {
		if fields[i].Entity == in.Entity && fields[i].ID == in.EntityID && fields[i].Field == in.Field {
			f = &fields[i]
		}
	}
	if f == nil {
		return ErrNotFound
	}
	var by *int64
	if actor.ID != 0 {
		by = &actor.ID
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		switch {
		case in.Keep:
			if _, err := tx.Exec(ctx, `UPDATE content_translations SET status='human', source_hash=$5, updated_by=$6, updated_at=now()
				WHERE entity=$1 AND entity_id=$2 AND field=$3 AND lang=$4`, f.Entity, f.ID, f.Field, in.Lang,
				sourceHash(f.Text), by); err != nil {
				return err
			}
		case in.Text == "":
			if _, err := tx.Exec(ctx, `DELETE FROM content_translations WHERE entity=$1 AND entity_id=$2 AND field=$3 AND lang=$4`,
				f.Entity, f.ID, f.Field, in.Lang); err != nil {
				return err
			}
		default:
			if err := upsertTranslation(ctx, tx, branchID, *f, in.Lang, in.Text, "human", "", by); err != nil {
				return err
			}
		}
		// The English name stays on the menu tables, where staff screens read it.
		if in.Lang == "en" && f.Field == "name" && !in.Keep {
			if _, err := tx.Exec(ctx, `UPDATE `+nameEnTables[f.Entity]+` SET name_en=$2 WHERE id=$1`, f.ID, in.Text); err != nil {
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
type StoreTexts struct{ Name, Notice, Intro string }

// storeTexts picks the stores' texts for a customer reading lang (docs/I18N_DESIGN.md
// §7): Simplified Chinese, English and Traditional Chinese readers get their own
// version; readers of the language the store writes in get the original; everyone
// else English, then the original. "" leaves a field as it is.
func storeTexts(ctx context.Context, q Querier, lang string, stores []*Branch) (map[int64]StoreTexts, error) {
	out := map[int64]StoreTexts{}
	if len(stores) == 0 || !IsLang(lang) {
		return out, nil
	}
	ids := make([]int64, len(stores))
	for i, b := range stores {
		ids[i] = b.ID
	}
	want := lang
	if !storeInfoLang(lang) {
		want = "en"
	}
	rows, err := q.Query(ctx, `SELECT entity_id, field, lang, text FROM content_translations
		WHERE entity='branch' AND entity_id = ANY($1) AND lang IN ($2, 'en')`, ids, want)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tr := map[string]string{}
	for rows.Next() {
		var id int64
		var field, l, text string
		if err := rows.Scan(&id, &field, &l, &text); err != nil {
			return nil, err
		}
		tr[strconv.FormatInt(id, 10)+"/"+field+"/"+l] = text
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, b := range stores {
		if lang == b.MenuLang {
			continue
		}
		pick := func(field, orig, origEn string) string {
			if strings.TrimSpace(orig) == "" {
				return ""
			}
			key := strconv.FormatInt(b.ID, 10) + "/" + field + "/"
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
		out[b.ID] = StoreTexts{Name: pick("name", b.Name, b.NameEn), Notice: pick("notice", b.Notice, ""), Intro: pick("intro", b.Intro, "")}
	}
	return out, nil
}

// localizeBranch fills in one store's texts in the customer's language.
func localizeBranch(ctx context.Context, q Querier, b *Branch, lang string) error {
	t, err := storeTexts(ctx, q, lang, []*Branch{b})
	if err != nil {
		return err
	}
	x := t[b.ID]
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

// StoreTextsView is the store's name, notice and introduction in the three
// languages, for the settings page.
type StoreTextsView struct {
	MenuLang     string        `json:"menu_lang"`
	Fields       []transField  `json:"fields"`
	Translations []Translation `json:"translations"`
	// Pending counts the texts the system filled in that the merchant has not confirmed.
	Pending int `json:"pending"`
}

func (a *App) StoreTexts(ctx context.Context, branchID int64) (*StoreTextsView, error) {
	all, err := a.MenuTranslations(ctx, branchID)
	if err != nil {
		return nil, err
	}
	out := &StoreTextsView{MenuLang: all.MenuLang, Fields: []transField{}, Translations: []Translation{}}
	has := map[string]bool{}
	for _, f := range all.Fields {
		if f.Entity == "branch" {
			out.Fields = append(out.Fields, f)
			has[f.Field] = true
		}
	}
	for _, t := range all.Translations {
		if t.Entity != "branch" || !storeInfoLang(t.Lang) || !has[t.Field] || t.Lang == all.MenuLang {
			continue
		}
		out.Translations = append(out.Translations, t)
		if t.Status != "human" {
			out.Pending++
		}
	}
	return out, nil
}
