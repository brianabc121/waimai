package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func TestUITexts(t *testing.T) {
	need(t)
	ctx := context.Background()
	a := app.New(testApp.DB, "test-secret")
	a.Translator = &markTranslator{}
	admin := adminActor(t)
	key := uniq("t.k")
	noErr(t, a.SaveUITexts(ctx, admin, "th", []app.UIEdit{{Key: key, Text: " ปุ่ม "}}))
	f, err := a.UIFixes(ctx, "th")
	noErr(t, err)
	if f.Texts[key] != "ปุ่ม" || f.Version == 0 {
		t.Fatalf("fixes %v", f)
	}
	// machine fill leaves corrections alone and marks its own
	key2 := uniq("t.k")
	m, err := a.FillUITexts(ctx, admin, "th", []app.UISource{{Key: key, En: "Button"}, {Key: key2, En: "Pay {amount}"}})
	noErr(t, err)
	if m[key2] != "th:Pay {amount}" {
		t.Fatalf("filled %v", m)
	}
	l, err := a.AdminUIFixes(ctx, "th")
	noErr(t, err)
	st := map[string]string{}
	for _, x := range l {
		st[x.Key] = x.Status + ":" + x.Text
	}
	if st[key] != "human:ปุ่ม" || st[key2] != "machine:th:Pay {amount}" {
		t.Fatalf("statuses %v", st)
	}
	// Traditional Chinese is converted, not translated
	m, err = a.FillUITexts(ctx, admin, "zh-Hant", []app.UISource{{Key: key, En: "Noodles", Zh: "面条"}})
	noErr(t, err)
	if m[key] != "麵條" {
		t.Fatalf("zh-Hant %v", m)
	}
	// "" goes back to the built-in text
	noErr(t, a.SaveUITexts(ctx, admin, "th", []app.UIEdit{{Key: key, Text: ""}}))
	f, err = a.UIFixes(ctx, "th")
	noErr(t, err)
	if _, ok := f.Texts[key]; ok {
		t.Fatal("the correction was not removed")
	}
	if err := a.SaveUITexts(ctx, admin, "th", []app.UIEdit{{Key: "bad key!", Text: "x"}}); err == nil {
		t.Fatal("a bad key should be refused")
	}

	// the spreadsheet goes out and comes back
	b, err := app.UITextsSheet("th", []app.UIRow{{Key: key, Zh: "按钮", En: "Button", Text: "ปุ่มใหม่"}, {Key: key2, En: "x"}})
	noErr(t, err)
	e, err := app.ReadUITextsSheet("th", b)
	noErr(t, err)
	if len(e) != 1 || e[0].Key != key || e[0].Text != "ปุ่มใหม่" {
		t.Fatalf("read back %v", e)
	}
	if _, err := app.ReadUITextsSheet("vi", b); err == nil {
		t.Fatal("a sheet without the language's column should be refused")
	}
}

// Corrections of the server's own texts on the language page reach SMS.
func TestSysTextFix(t *testing.T) {
	need(t)
	ctx := context.Background()
	a := app.New(testApp.DB, "test-secret")
	if s := app.SysTextForTest(a, ctx, "th", "sys.sms.pickup", "code", "A12"); s != "รหัสรับอาหาร: A12" {
		t.Fatalf("thai %q", s)
	}
	if s := app.SysTextForTest(a, ctx, "xx", "sys.order.cancelled"); s != "Your order was cancelled" {
		t.Fatalf("unknown language %q", s)
	}
	noErr(t, a.SaveUITexts(ctx, adminActor(t), "ja", []app.UIEdit{{Key: "sys.order.cancelled", Text: "キャンセル済み"}}))
	t.Cleanup(func() {
		_ = a.SaveUITexts(ctx, adminActor(t), "ja", []app.UIEdit{{Key: "sys.order.cancelled", Text: ""}})
	})
	if s := app.SysTextForTest(a, ctx, "ja", "sys.order.cancelled"); s != "キャンセル済み" {
		t.Fatalf("corrected %q", s)
	}
}
