package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// markTranslator "translates" by marking the text with the language, and can
// be told to fail.
type markTranslator struct {
	calls int
	fail  bool
}

func (*markTranslator) Name() string { return "mark" }

func (m *markTranslator) Translate(_ context.Context, _, to string, texts []string) ([]string, error) {
	m.calls++
	if m.fail {
		return nil, errors.New("service down")
	}
	out := make([]string, len(texts))
	for i, s := range texts {
		out[i] = to + ":" + s
	}
	return out, nil
}

func findItem(sf *app.Storefront, id int64) *app.Item {
	for i := range sf.Categories {
		for j := range sf.Categories[i].Items {
			if sf.Categories[i].Items[j].ID == id {
				return &sf.Categories[i].Items[j]
			}
		}
	}
	return nil
}

func TestMenuTranslation(t *testing.T) {
	need(t)
	ctx := context.Background()
	s := newStore(t, false)
	a := app.New(testApp.DB, "test-secret")
	tr := &markTranslator{}
	a.Translator = tr

	noErr(t, a.TranslateMenu(ctx, s.branchID, false))
	view := func(lang string) *app.Storefront {
		sf, err := a.Storefront(ctx, s.slug)
		noErr(t, err)
		noErr(t, a.LocalizeStorefront(ctx, sf, lang))
		return sf
	}
	sf := view("th")
	it := findItem(sf, s.chicken)
	if it.NameTr != "th:chicken" || it.Groups[0].NameTr != "th:spice" || it.Groups[0].Options[1].NameTr != "th:hot" {
		t.Fatalf("thai menu: %q %q %q", it.NameTr, it.Groups[0].NameTr, it.Groups[0].Options[1].NameTr)
	}
	if sf.Categories[0].NameTr != "th:C" || !sf.MachineTranslated {
		t.Fatalf("category %q machine %v", sf.Categories[0].NameTr, sf.MachineTranslated)
	}
	// the menu's own language is left alone; Traditional Chinese is converted
	if it := findItem(view("zh"), s.chicken); it.NameTr != "" {
		t.Fatalf("zh got %q", it.NameTr)
	}
	ms, err := a.MenuTranslations(ctx, s.branchID)
	noErr(t, err)
	for _, x := range ms.Translations {
		if x.Lang == "zh-Hant" && x.Status != "converted" {
			t.Fatalf("zh-Hant status %s", x.Status)
		}
		if x.Lang == "zh" {
			t.Fatal("the menu's own language was translated")
		}
	}

	// nothing changed: nothing is sent again
	calls := tr.calls
	noErr(t, a.TranslateMenu(ctx, s.branchID, false))
	if tr.calls != calls {
		t.Fatalf("translated again without a change (%d calls)", tr.calls-calls)
	}

	// the merchant's own Thai name is kept when the original changes, and marked for checking
	noErr(t, a.SaveTranslation(ctx, s.branchID, app.TranslationEdit{Entity: "item", EntityID: s.tea, Field: "name",
		Lang: "th", Text: "ชา"}, s.staff))
	_, err = a.DB.Exec(ctx, `UPDATE menu_items SET name='green tea' WHERE id=$1`, s.tea)
	noErr(t, err)
	noErr(t, a.TranslateMenu(ctx, s.branchID, false))
	if it := findItem(view("th"), s.tea); it.NameTr != "ชา" {
		t.Fatalf("merchant's thai name: %q", it.NameTr)
	}
	if it := findItem(view("fr"), s.tea); it.NameTr != "fr:green tea" {
		t.Fatalf("french after the change: %q", it.NameTr)
	}
	ms, err = a.MenuTranslations(ctx, s.branchID)
	noErr(t, err)
	stale := false
	for _, x := range ms.Translations {
		stale = stale || (x.EntityID == s.tea && x.Lang == "th" && x.Status == "stale")
	}
	if !stale {
		t.Fatal("the merchant's translation of a changed name should wait for checking")
	}
	noErr(t, a.SaveTranslation(ctx, s.branchID, app.TranslationEdit{Entity: "item", EntityID: s.tea, Field: "name",
		Lang: "th", Keep: true}, s.staff))

	// English typed by the merchant is theirs, and the English column follows the card
	noErr(t, a.SaveTranslation(ctx, s.branchID, app.TranslationEdit{Entity: "item", EntityID: s.water, Field: "name",
		Lang: "en", Text: "Mineral water"}, s.staff))
	var en string
	noErr(t, a.DB.QueryRow(ctx, `SELECT name_en FROM menu_items WHERE id=$1`, s.water).Scan(&en))
	if en != "Mineral water" {
		t.Fatalf("name_en = %q", en)
	}

	// a language turned off reads English
	noErr(t, a.SaveMenuLangs(ctx, s.branchID, "zh", []string{"ar"}))
	if it := findItem(view("ar"), s.water); it.NameTr != "Mineral water" {
		t.Fatalf("arabic turned off: %q", it.NameTr)
	}

	// the order keeps the name in the customer's language
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Lang: "th",
		Items: []app.QuoteLineInput{{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}}}}, "")
	noErr(t, err)
	if q.Lines[0].NameTr != "th:chicken" || q.Lines[0].Options[0].NameTr != "th:mild" {
		t.Fatalf("quote line %+v", q.Lines[0])
	}
	p, err := a.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)
	o, err := a.GetOrder(ctx, p.OrderID)
	noErr(t, err)
	if o.Items[0].Name != "chicken" || o.Items[0].NameTr != "th:chicken" {
		t.Fatalf("order item %q / %q", o.Items[0].Name, o.Items[0].NameTr)
	}

	// a failing service is noted on the store, and cleared by the next run
	_, err = a.DB.Exec(ctx, `UPDATE menu_items SET description='new' WHERE id=$1`, s.water)
	noErr(t, err)
	tr.fail = true
	if err := a.TranslateMenu(ctx, s.branchID, false); err == nil {
		t.Fatal("a failing service should fail the run")
	}
	ms, err = a.MenuTranslations(ctx, s.branchID)
	noErr(t, err)
	if !strings.Contains(ms.Error, "service down") {
		t.Fatalf("error = %q", ms.Error)
	}
	tr.fail = false
	noErr(t, a.TranslateMenu(ctx, s.branchID, false))
	ms, err = a.MenuTranslations(ctx, s.branchID)
	noErr(t, err)
	if ms.Error != "" {
		t.Fatalf("error kept: %q", ms.Error)
	}
}

// The store's name and notice go into Simplified Chinese, English and
// Traditional Chinese only; the other languages read English.
func TestStoreTexts(t *testing.T) {
	need(t)
	ctx := context.Background()
	s := newStore(t, false)
	a := app.New(testApp.DB, "test-secret")
	a.Translator = &markTranslator{}
	_, err := a.DB.Exec(ctx, `UPDATE branches SET name='老王面馆', name_en='', notice='今天休息' WHERE id=$1`, s.branchID)
	noErr(t, err)
	noErr(t, a.TranslateMenu(ctx, s.branchID, false))

	v, err := a.StoreTexts(ctx, s.branchID)
	noErr(t, err)
	langs := map[string]bool{}
	for _, x := range v.Translations {
		langs[x.Lang] = true
	}
	if !langs["en"] || !langs["zh-Hant"] || len(langs) != 2 {
		t.Fatalf("store text languages %v", langs)
	}
	if v.Pending != 4 { // name and notice, in English and Traditional Chinese
		t.Fatalf("pending = %d", v.Pending)
	}
	ms, err := a.MenuTranslations(ctx, s.branchID)
	noErr(t, err)
	for _, x := range ms.Translations {
		if x.Entity == "branch" && x.Lang == "th" {
			t.Fatal("the store's name went into Thai")
		}
	}
	branch := func(lang string) *app.Branch {
		sf, err := a.Storefront(ctx, s.slug)
		noErr(t, err)
		noErr(t, a.LocalizeStorefront(ctx, sf, lang))
		return sf.Branch
	}
	if b := branch("th"); b.NameTr != "en:老王面馆" || b.NoticeTr != "en:今天休息" {
		t.Fatalf("thai reader got %q / %q", b.NameTr, b.NoticeTr)
	}
	if b := branch("zh-Hant"); b.NameTr != "老王麵館" {
		t.Fatalf("traditional name %q", b.NameTr)
	}

	// the merchant confirms the English name by saving it; it is also the store's name_en
	err = a.SaveTranslation(ctx, s.branchID, app.TranslationEdit{Entity: "branch", EntityID: s.branchID, Field: "name",
		Lang: "en", Text: "Wang's Noodles"}, s.staff)
	noErr(t, err)
	var en string
	noErr(t, a.DB.QueryRow(ctx, `SELECT name_en FROM branches WHERE id=$1`, s.branchID).Scan(&en))
	if en != "Wang's Noodles" {
		t.Fatalf("name_en = %q", en)
	}
	v, err = a.StoreTexts(ctx, s.branchID)
	noErr(t, err)
	if v.Pending != 3 {
		t.Fatalf("pending after confirming = %d", v.Pending)
	}
	if err := a.SaveTranslation(ctx, s.branchID, app.TranslationEdit{Entity: "branch", EntityID: s.branchID, Field: "name",
		Lang: "th", Text: "x"}, s.staff); err == nil {
		t.Fatal("a Thai store name should be refused")
	}
}

// The mall's categories and banners are translated into every language.
func TestPlatformTranslation(t *testing.T) {
	need(t)
	ctx := context.Background()
	a := app.New(testApp.DB, "test-secret")
	a.Translator = &markTranslator{}
	var bid int64
	noErr(t, a.DB.QueryRow(ctx, `INSERT INTO mall_banners (title, subtitle, sort, active) VALUES ('新店开业', '满减', 99, true)
		RETURNING id`).Scan(&bid))
	t.Cleanup(func() { _, _ = a.DB.Exec(ctx, `DELETE FROM mall_banners WHERE id=$1`, bid) })
	noErr(t, a.TranslatePlatform(ctx, false))

	home := func(lang string) *app.MallHome {
		h, err := a.MallHome(ctx)
		noErr(t, err)
		noErr(t, a.LocalizeMallHome(ctx, h, lang))
		return h
	}
	h := home("th")
	var ban *app.MallBanner
	for i := range h.Banners {
		if h.Banners[i].ID == bid {
			ban = &h.Banners[i]
		}
	}
	if ban == nil || ban.TitleTr != "th:新店开业" || ban.SubtitleTr != "th:满减" {
		t.Fatalf("thai banner %+v", ban)
	}
	if len(h.Categories) == 0 || h.Categories[0].NameTr != "th:"+h.Categories[0].Name {
		t.Fatalf("thai category %+v", h.Categories)
	}
	// English typed by the platform is used as it is
	for _, c := range home("en").Categories {
		if c.NameEn != "" && c.NameTr != c.NameEn {
			t.Fatalf("english category %q, typed %q", c.NameTr, c.NameEn)
		}
	}
	for _, b := range home("zh").Banners {
		if b.TitleTr != "" {
			t.Fatalf("the original language got %q", b.TitleTr)
		}
	}
	// a deleted banner loses its translations
	_, err := a.DB.Exec(ctx, `DELETE FROM mall_banners WHERE id=$1`, bid)
	noErr(t, err)
	noErr(t, a.TranslatePlatform(ctx, false))
	var n int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM content_translations WHERE entity='mall_banner' AND entity_id=$1`, bid).Scan(&n))
	if n != 0 {
		t.Fatalf("%d translations of a deleted banner", n)
	}
}
