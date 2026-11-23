package app

import (
	"context"
	"strings"
)

// Langs are the languages the site speaks (docs/I18N_DESIGN.md §3), in the
// order the language picker lists them. "zh" is Simplified Chinese. Keep in
// step with web/src/langs.ts.
var Langs = []string{"zh", "en", "zh-Hant", "th", "vi", "fr", "ar", "es", "ru", "ja", "my", "km"}

// IsLang reports whether s is one of Langs.
func IsLang(s string) bool {
	for _, l := range Langs {
		if l == s {
			return true
		}
	}
	return false
}

// LangFromHeader picks the first supported language of an Accept-Language
// header ("zh-TW,zh;q=0.9,en;q=0.8"), or "" when none is.
func LangFromHeader(h string) string {
	for _, part := range strings.Split(h, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if strings.HasPrefix(tag, "zh") {
			if strings.Contains(tag, "hant") || strings.Contains(tag, "-tw") || strings.Contains(tag, "-hk") || strings.Contains(tag, "-mo") {
				return "zh-Hant"
			}
			return "zh"
		}
		if p := strings.SplitN(tag, "-", 2)[0]; IsLang(p) {
			return p
		}
	}
	return ""
}

// CustomerLang is the language the customer chose, "" when they never did.
func (a *App) CustomerLang(ctx context.Context, customerID int64) (string, error) {
	var l string
	err := a.DB.QueryRow(ctx, `SELECT lang FROM customers WHERE id=$1`, customerID).Scan(&l)
	return l, err
}

// SaveCustomerLang remembers the customer's language; "" forgets it (the
// site follows the device again).
func (a *App) SaveCustomerLang(ctx context.Context, customerID int64, lang string) error {
	if lang != "" && !IsLang(lang) {
		return ErrValidation("unknown language")
	}
	_, err := a.DB.Exec(ctx, `UPDATE customers SET lang=$2 WHERE id=$1`, customerID, lang)
	return err
}
