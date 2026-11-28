package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// The texts outside the store's own catalog (docs/I18N_SIMPLE_DESIGN.md §4.8):
// brands, a brand's own categories and item names, toppings on their template,
// stored value campaigns, coupons, queue groups.
func TestMoreTranslations(t *testing.T) {
	need(t)
	ctx := context.Background()
	b := newBrandStore(t)
	s := b.store
	a := app.New(testApp.DB, "test-secret")
	tr := &markTranslator{}
	a.Translator = tr

	// a topping template on two items
	tpl, err := a.SaveToppingTemplate(ctx, s.branchID, 0, app.ToppingTemplateInput{Name: "奶茶加料", Title: "加料", Options: []app.ToppingOptionInput{
		{Code: "T1", Name: "珍珠", Price: 500}, {Code: "T2", Name: "布丁", Price: 800}}})
	noErr(t, err)
	noErr(t, a.SetToppingTemplateItems(ctx, s.branchID, tpl, []int64{b.pork, b.rice}))
	// a queue group, a stored value campaign with a tier, a coupon campaign: only their texts matter here
	var group, campaign, tier, batch int64
	noErr(t, a.DB.QueryRow(ctx, `INSERT INTO queue_groups (branch_id, letter, label, min_people, max_people) VALUES ($1,'A','靠窗小桌',1,2) RETURNING id`,
		s.branchID).Scan(&group))
	noErr(t, a.DB.QueryRow(ctx, `INSERT INTO stored_campaigns (branch_id, name, names, note) VALUES ($1,'开业充值送','{"en":"Opening top-up"}','每人限一次') RETURNING id`,
		s.branchID).Scan(&campaign))
	noErr(t, a.DB.QueryRow(ctx, `INSERT INTO stored_tiers (campaign_id, pay, bonus, label) VALUES ($1,10000,2000,'最划算') RETURNING id`, campaign).Scan(&tier))
	noErr(t, a.DB.QueryRow(ctx, `INSERT INTO coupon_batches (issuer, branch_id, brand_id, kind, name, value, source, currency, valid_days)
		VALUES ('store',$1,$2,'store_free','新客券',1000,'claim','PHP',30) RETURNING id`, s.branchID, b.main).Scan(&batch))
	_, err = a.DB.Exec(ctx, `UPDATE brands SET intro='湘菜小炒' WHERE id=$1`, b.hcd)
	noErr(t, err)
	noErr(t, a.TranslateMenu(ctx, s.branchID, false))

	all, err := a.MenuTranslations(ctx, s.branchID, "")
	noErr(t, err)
	count := map[string]int{}
	for _, f := range all.Fields {
		count[f.Entity+"."+f.Field]++
	}
	// a topping is one text however many items use it, and the items' copies are not translated
	var copies int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM content_translations t WHERE t.branch_id=$1 AND ((t.entity='option' AND EXISTS
		(SELECT 1 FROM options o WHERE o.id = t.entity_id AND o.topping_id IS NOT NULL)) OR (t.entity='group' AND EXISTS
		(SELECT 1 FROM option_groups g WHERE g.id = t.entity_id AND g.topping_template_id IS NOT NULL)))`, s.branchID).Scan(&copies))
	if count["topping_option.name"] != 2 || count["topping_template.title"] != 1 || copies != 0 {
		t.Fatalf("topping texts: %v, %d translations of the items' copies", count, copies)
	}
	for _, k := range []string{"brand.name", "brand.intro", "brand_category.name", "brand_item.name", "queue_group.label",
		"stored_campaign.name", "stored_campaign.note", "stored_tier.label", "coupon_batch.name"} {
		if count[k] != 1 {
			t.Fatalf("%s: %d texts (%v)", k, count[k], count)
		}
	}
	// the window of a topping template holds its title and its toppings
	v, err := a.MenuTranslations(ctx, s.branchID, "topping_template/"+itoa(tpl))
	noErr(t, err)
	if len(v.Fields) != 3 {
		t.Fatalf("topping window: %+v", v.Fields)
	}

	// the brand's page for a Thai reader
	var slug string
	noErr(t, a.DB.QueryRow(ctx, `SELECT slug FROM brands WHERE id=$1`, b.hcd).Scan(&slug))
	page := func(lang string) *app.Storefront {
		sf, err := a.Storefront(ctx, slug)
		noErr(t, err)
		noErr(t, a.LocalizeStorefront(ctx, sf, lang))
		return sf
	}
	sf := page("th")
	// a brand's name is a sign: English, not a made-up Thai one
	if sf.Branch.NameTr != "en:好吃党" {
		t.Fatalf("brand name for a Thai reader: %q", sf.Branch.NameTr)
	}
	var cat *app.Category
	var pork *app.Item
	for i := range sf.Categories {
		c := &sf.Categories[i]
		if c.Name == "招牌" {
			cat = c
		}
		for j := range c.Items {
			if c.Items[j].ID == b.pork {
				pork = &c.Items[j]
			}
		}
	}
	if cat == nil || cat.NameTr != "th:招牌" || pork == nil || pork.NameTr != "th:辣椒炒肉" {
		t.Fatalf("brand catalog in Thai: %+v / %+v", cat, pork)
	}
	var topping string
	for _, g := range pork.Groups {
		if g.ToppingTemplateID != nil {
			if g.NameTr != "th:加料" {
				t.Fatalf("topping group %q", g.NameTr)
			}
			topping = g.Options[0].NameTr
		}
	}
	if topping != "th:珍珠" {
		t.Fatalf("topping in Thai: %q", topping)
	}

	// the merchant corrects the category and one topping; saving the brand's catalog again keeps both
	catID := cat.ID
	noErr(t, a.SaveTranslations(ctx, s.branchID, []app.TranslationEdit{
		{Entity: "brand_category", EntityID: catID, Field: "name", Lang: "th", Text: "เมนูเด็ด"},
		{Entity: "brand_item", EntityID: b.hcd<<32 + b.pork, Field: "name", Lang: "en", Text: "Chili Pork"},
	}, s.staff))
	var topID int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM topping_options WHERE template_id=$1 AND name='珍珠'`, tpl).Scan(&topID))
	noErr(t, a.SaveTranslation(ctx, s.branchID, app.TranslationEdit{Entity: "topping_option", EntityID: topID, Field: "name", Lang: "en", Text: "Pearls"}, s.staff))
	var in app.BrandMenuInput
	noErr(t, jsonInto(`{"own_menu": true, "categories": [{"key": "a", "name": "招牌"}, {"key": "b", "name": "主食"}],
		"items": [{"item_id": `+itoa(b.pork)+`, "category_key": "a", "name": "辣椒炒肉", "name_en": "Chili Pork", "price": 5200, "published": true},
			{"item_id": `+itoa(b.rice)+`, "category_key": "b", "published": true}]}`, &in))
	_, err = a.SaveBrandMenu(ctx, s.branchID, b.hcd, s.staff, in)
	noErr(t, err)
	before := tr.calls
	noErr(t, a.TranslateMenu(ctx, s.branchID, false))
	// only the new category goes to the service: 10 languages (Traditional Chinese is converted)
	if tr.calls-before != 10 {
		t.Fatalf("translation calls after saving the catalog again: %d", tr.calls-before)
	}
	sf = page("th")
	for i := range sf.Categories {
		if c := sf.Categories[i]; c.Name == "招牌" && c.NameTr != "เมนูเด็ด" {
			t.Fatalf("the corrected category after a save: %q", c.NameTr)
		}
	}
	// the merchant's own English is kept on the records, and on the items' copies of a topping
	var en1, en2 string
	noErr(t, a.DB.QueryRow(ctx, `SELECT name_en FROM brand_items WHERE brand_id=$1 AND item_id=$2`, b.hcd, b.pork).Scan(&en1))
	noErr(t, a.DB.QueryRow(ctx, `SELECT name_en FROM topping_options WHERE id=$1`, topID).Scan(&en2))
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM options WHERE topping_id=$1 AND name_en='Pearls'`, topID).Scan(&copies))
	if en1 != "Chili Pork" || en2 != "Pearls" || copies != 2 {
		t.Fatalf("english kept on the records: %q %q %d", en1, en2, copies)
	}

	// texts read by record
	one := func(lang, entity, field string, id int64) string {
		m, err := a.Texts(ctx, lang, entity, field, []int64{id})
		noErr(t, err)
		return m[id]
	}
	for _, c := range []struct {
		lang, entity, field string
		id                  int64
		want                string
	}{
		{"th", "queue_group", "label", group, "th:靠窗小桌"},
		{"th", "stored_campaign", "name", campaign, "th:开业充值送"},
		{"en", "stored_campaign", "name", campaign, "Opening top-up"}, // what the merchant typed before
		{"th", "stored_campaign", "note", campaign, "th:每人限一次"},
		{"ja", "stored_tier", "label", tier, "ja:最划算"},
		{"fr", "coupon_batch", "name", batch, "fr:新客券"},
		{"zh", "coupon_batch", "name", batch, ""}, // the store's own language: as written
	} {
		if got := one(c.lang, c.entity, c.field, c.id); got != c.want {
			t.Fatalf("%s %s.%s = %q, want %q", c.lang, c.entity, c.field, got, c.want)
		}
	}
	// the merchant's English of a campaign goes back onto it
	noErr(t, a.SaveTranslation(ctx, s.branchID, app.TranslationEdit{Entity: "stored_campaign", EntityID: campaign, Field: "name", Lang: "en", Text: "Grand opening"}, s.staff))
	noErr(t, a.SaveTranslation(ctx, s.branchID, app.TranslationEdit{Entity: "queue_group", EntityID: group, Field: "label", Lang: "en", Text: "Window table"}, s.staff))
	noErr(t, a.TranslateMenu(ctx, s.branchID, false))
	noErr(t, a.DB.QueryRow(ctx, `SELECT names->>'en' FROM stored_campaigns WHERE id=$1`, campaign).Scan(&en1))
	noErr(t, a.DB.QueryRow(ctx, `SELECT label_en FROM queue_groups WHERE id=$1`, group).Scan(&en2))
	if en1 != "Grand opening" || en2 != "Window table" || one("en", "stored_campaign", "name", campaign) != "Grand opening" {
		t.Fatalf("english of the campaign and the queue group: %q %q", en1, en2)
	}
	// a language the store turned off reads English
	noErr(t, a.SaveMenuLangs(ctx, s.branchID, "zh", []string{"th"}))
	if got := one("th", "queue_group", "label", group); got != "Window table" {
		t.Fatalf("thai turned off: %q", got)
	}
}
