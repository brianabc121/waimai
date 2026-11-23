package app

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// Texts the server sends itself, in every language (docs/I18N_DESIGN.md §10):
// SMS, push reminders. They ship in systexts/texts.json; the platform's
// language page lists them (keys "sys.…") and its corrections win.

//go:embed systexts/texts.json
var sysTextsJSON []byte

var sysTexts = func() map[string]map[string]string {
	m := map[string]map[string]string{}
	if err := json.Unmarshal(sysTextsJSON, &m); err != nil {
		panic("systexts: " + err.Error())
	}
	return m
}()

// sysFixCache keeps each language's corrections for a minute.
type sysFixCache struct {
	mu sync.Mutex
	m  map[string]sysFixes
}

type sysFixes struct {
	at    time.Time
	texts map[string]string
}

func (a *App) sysFix(ctx context.Context, lang string) map[string]string {
	c := a.fixed
	if c == nil {
		return nil
	}
	c.mu.Lock()
	f, ok := c.m[lang]
	c.mu.Unlock()
	if ok && time.Since(f.at) < time.Minute {
		return f.texts
	}
	texts := map[string]string{}
	rows, err := a.DB.Query(ctx, `SELECT key, text FROM ui_translations WHERE lang=$1 AND key LIKE 'sys.%'`, lang)
	if err == nil {
		for rows.Next() {
			var k, t string
			if rows.Scan(&k, &t) == nil {
				texts[k] = t
			}
		}
		rows.Close()
	}
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]sysFixes{}
	}
	c.m[lang] = sysFixes{at: time.Now(), texts: texts}
	c.mu.Unlock()
	return texts
}

// sysText is a system text in lang (English when the language is unknown),
// with {name} filled in from args.
func (a *App) sysText(ctx context.Context, lang, key string, args ...string) string {
	if !IsLang(lang) {
		lang = "en"
	}
	s, ok := a.sysFix(ctx, lang)[key]
	if !ok {
		s = sysTexts[key][lang]
	}
	if s == "" {
		s = sysTexts[key]["en"]
	}
	for i := 0; i+1 < len(args); i += 2 {
		s = strings.ReplaceAll(s, "{"+args[i]+"}", args[i+1])
	}
	return s
}
