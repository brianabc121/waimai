package app

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
)

// The platform's language page (docs/I18N_DESIGN.md §8): admins correct the
// app's own texts, in any language, without a new release. The texts that
// ship live in the web app; ui_translations holds the corrections.

// UIFix is one correction.
type UIFix struct {
	Key       string    `json:"key"`
	Text      string    `json:"text"`
	Status    string    `json:"status"` // human, machine
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}

// UIFixes is what browsers load on top of a language's texts.
type UIFixes struct {
	Version int64             `json:"version"`
	Texts   map[string]string `json:"texts"`
}

const maxUIText = 2000

func (a *App) UIFixes(ctx context.Context, lang string) (*UIFixes, error) {
	out := &UIFixes{Texts: map[string]string{}}
	if !IsLang(lang) {
		return nil, ErrValidation("unknown language")
	}
	rows, err := a.DB.Query(ctx, `SELECT key, text, updated_at FROM ui_translations WHERE lang=$1`, lang)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, t string
		var at time.Time
		if err := rows.Scan(&k, &t, &at); err != nil {
			return nil, err
		}
		out.Texts[k] = t
		out.Version = max(out.Version, at.Unix())
	}
	return out, rows.Err()
}

// AdminUIFixes lists a language's corrections with who made them.
func (a *App) AdminUIFixes(ctx context.Context, lang string) ([]UIFix, error) {
	if !IsLang(lang) {
		return nil, ErrValidation("unknown language")
	}
	rows, err := a.DB.Query(ctx, `SELECT t.key, t.text, t.status, t.updated_at, COALESCE(u.username, '')
		FROM ui_translations t LEFT JOIN users u ON u.id = t.updated_by WHERE t.lang=$1 ORDER BY t.key`, lang)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UIFix{}
	for rows.Next() {
		var f UIFix
		if err := rows.Scan(&f.Key, &f.Text, &f.Status, &f.UpdatedAt, &f.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UIEdit is one text set on the language page; "" goes back to the text
// that ships with the app.
type UIEdit struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

func validUIKey(k string) bool {
	if k == "" || len(k) > 120 {
		return false
	}
	for _, r := range k {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// SaveUITexts stores admins' corrections and logs each one.
func (a *App) SaveUITexts(ctx context.Context, actor AdminActor, lang string, edits []UIEdit) error {
	if !IsLang(lang) {
		return ErrValidation("unknown language")
	}
	if len(edits) == 0 || len(edits) > 5000 {
		return ErrValidation("nothing to save")
	}
	for _, e := range edits {
		if !validUIKey(e.Key) || len(e.Text) > maxUIText {
			return ErrValidation("invalid text " + truncate(e.Key, 40))
		}
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		for _, e := range edits {
			e.Text = strings.TrimSpace(e.Text)
			var before string
			_ = tx.QueryRow(ctx, `SELECT text FROM ui_translations WHERE lang=$1 AND key=$2`, lang, e.Key).Scan(&before)
			if e.Text == "" {
				if _, err := tx.Exec(ctx, `DELETE FROM ui_translations WHERE lang=$1 AND key=$2`, lang, e.Key); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `INSERT INTO ui_translations (lang, key, text, status, updated_by)
				VALUES ($1,$2,$3,'human',$4) ON CONFLICT (lang, key) DO UPDATE SET text=$3, status='human', updated_by=$4, updated_at=now()`,
				lang, e.Key, e.Text, actor.ID); err != nil {
				return err
			}
			if before == e.Text {
				continue
			}
			if err := adminAudit(ctx, tx, actor, "ui_text.save", "ui_text", 0,
				map[string]any{"lang": lang, "key": e.Key, "before": before, "after": e.Text}); err != nil {
				return err
			}
		}
		return nil
	})
}

// UISource is a text to fill in: its key and its English and Chinese.
type UISource struct {
	Key string `json:"key"`
	En  string `json:"en"`
	Zh  string `json:"zh"`
}

const uiPrompt = `You translate the interface of a food ordering app (customer pages, merchant back office, staff screens). You get a JSON list of interface texts in English with their Chinese, and translate each English text into the target language.

- Keep {placeholders} exactly as they are, and keep ICU plural forms like "{n, plural, one {# item} other {# items}}" valid, with the plural categories the target language uses.
- Use the words food delivery apps in that language already use. Keep it as short as the English: many texts are buttons and labels on phones.
- Return one translation for every id.`

// FillUITexts machine-translates texts a language is missing and stores them
// as "machine" corrections, for an admin to check.
func (a *App) FillUITexts(ctx context.Context, actor AdminActor, lang string, src []UISource) (map[string]string, error) {
	if !IsLang(lang) || lang == "zh" || lang == "en" {
		return nil, ErrValidation("pick a language other than Chinese and English")
	}
	if len(src) == 0 || len(src) > 1000 {
		return nil, ErrValidation("nothing to fill")
	}
	out := map[string]string{}
	var todo []UISource
	for _, s := range src {
		if !validUIKey(s.Key) || len(s.En) > maxUIText || len(s.Zh) > maxUIText {
			return nil, ErrValidation("invalid text " + truncate(s.Key, 40))
		}
		if lang == "zh-Hant" {
			if t := convertChinese(s.Zh, "zh-Hant"); t != "" {
				out[s.Key] = t
			}
			continue
		}
		todo = append(todo, s)
	}
	if len(todo) > 0 {
		tr := a.translator()
		if tr == nil {
			return nil, ErrValidation("no translation service is set up (keys page)")
		}
		for start := 0; start < len(todo); start += translateBatch {
			part := todo[start:min(start+translateBatch, len(todo))]
			texts := make([]string, len(part))
			for i, s := range part {
				texts[i] = s.En
			}
			res, err := translateUI(ctx, tr, lang, texts)
			if err != nil {
				return nil, err
			}
			for i, s := range part {
				out[s.Key] = res[i]
			}
		}
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		for k, t := range out {
			if _, err := tx.Exec(ctx, `INSERT INTO ui_translations (lang, key, text, status, updated_by)
				VALUES ($1,$2,$3,'machine',$4) ON CONFLICT (lang, key) DO NOTHING`, lang, k, t, actor.ID); err != nil {
				return err
			}
		}
		return adminAudit(ctx, tx, actor, "ui_text.fill", "ui_text", 0, map[string]any{"lang": lang, "count": len(out)})
	})
	return out, err
}

// translateUI uses the interface prompt where the service takes one.
func translateUI(ctx context.Context, tr Translator, lang string, texts []string) ([]string, error) {
	if c, ok := tr.(*claudeTranslator); ok {
		return c.translate(ctx, uiPrompt, "en", lang, texts)
	}
	return tr.Translate(ctx, "en", lang, texts)
}

// ---- Excel ----

// UIRow is one line of the language page's spreadsheet.
type UIRow struct {
	Key  string `json:"key"`
	Zh   string `json:"zh"`
	En   string `json:"en"`
	Text string `json:"text"`
}

// UITextsSheet writes a language's texts for checking offline: one row per
// text, key, Chinese, English and the language.
func UITextsSheet(lang string, rows []UIRow) ([]byte, error) {
	if !IsLang(lang) {
		return nil, ErrValidation("unknown language")
	}
	f := excelize.NewFile()
	defer f.Close()
	sh := "texts"
	f.SetSheetName("Sheet1", sh)
	sw, err := f.NewStreamWriter(sh)
	if err != nil {
		return nil, err
	}
	_ = sw.SetColWidth(1, 1, 28)
	_ = sw.SetColWidth(2, 4, 48)
	if err := sw.SetRow("A1", []any{"key", "zh", "en", lang}); err != nil {
		return nil, err
	}
	for i, r := range rows {
		if err := sw.SetRow(fmt.Sprintf("A%d", i+2), []any{r.Key, r.Zh, r.En, r.Text}); err != nil {
			return nil, err
		}
	}
	if err := sw.Flush(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ReadUITextsSheet reads a spreadsheet written by UITextsSheet back: the key
// column and the column headed with the language.
func ReadUITextsSheet(lang string, content []byte) ([]UIEdit, error) {
	f, err := excelize.OpenReader(bytes.NewReader(content), excelize.Options{UnzipSizeLimit: 64 << 20, UnzipXMLSizeLimit: 32 << 20})
	if err != nil {
		return nil, ErrValidation("this is not an .xlsx file")
	}
	defer f.Close()
	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil || len(rows) == 0 {
		return nil, ErrValidation("the sheet is empty")
	}
	kc, lc := -1, -1
	for i, h := range rows[0] {
		switch strings.TrimSpace(h) {
		case "key":
			kc = i
		case lang:
			lc = i
		}
	}
	if kc < 0 || lc < 0 {
		return nil, ErrValidation("the first row needs a \"key\" column and a \"" + lang + "\" column")
	}
	out := []UIEdit{}
	for _, r := range rows[1:] {
		if kc >= len(r) || lc >= len(r) || !validUIKey(strings.TrimSpace(r[kc])) || strings.TrimSpace(r[lc]) == "" {
			continue
		}
		out = append(out, UIEdit{Key: strings.TrimSpace(r[kc]), Text: strings.TrimSpace(r[lc])})
	}
	return out, nil
}
