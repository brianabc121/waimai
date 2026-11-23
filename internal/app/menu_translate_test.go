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
