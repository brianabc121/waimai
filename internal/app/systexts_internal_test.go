package app

import "context"

// SysTextForTest lets the outside tests read system texts.
func SysTextForTest(a *App, ctx context.Context, lang, key string, args ...string) string {
	return a.sysText(ctx, lang, key, args...)
}
