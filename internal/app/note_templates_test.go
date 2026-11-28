package app_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/xuri/excelize/v2"
)

// milkTea adds the sweetness / temperature / extras template and binds it to tea.
func (s *store) milkTea(t *testing.T) (int64, map[string]int64) {
	t.Helper()
	ctx := context.Background()
	a := testApp
	id, err := a.SaveNoteTemplate(ctx, s.branchID, 0, app.NoteTemplateInput{Name: "奶茶类", Icon: "🧋", Groups: []app.NoteGroupInput{
		{Name: "甜度", Tags: []app.NoteTagInput{{Name: "少糖"}, {Name: "无糖"}}},
		{Name: "温度", Tags: []app.NoteTagInput{{Name: "去冰"}, {Name: "热"}}},
		{Name: "", Multi: true, Tags: []app.NoteTagInput{{Name: "打包分开装"}, {Name: "不要吸管"}}},
	}})
	noErr(t, err)
	noErr(t, a.SetNoteTemplateItems(ctx, s.branchID, id, []int64{s.tea}))
	tags := map[string]int64{}
	l, err := a.NoteTemplates(ctx, s.branchID)
	noErr(t, err)
	for _, g := range l[0].Groups {
		for _, x := range g.Tags {
			tags[x.Name] = x.ID
		}
	}
	return id, tags
}

func TestNoteTemplatesOnOrders(t *testing.T) {
	need(t)
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	tpl, tags := s.milkTea(t)

	// The storefront shows the dish's groups and live tags.
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	tea := findItem(sf, s.tea)
	if len(tea.Notes) != 3 || tea.Notes[0].Name != "甜度" || len(tea.Notes[0].Tags) != 2 || !tea.Notes[2].Multi ||
		len(tea.NoteTemplateIDs) != 1 || tea.NoteTemplateIDs[0] != tpl {
		t.Fatalf("notes on the menu: %+v", tea.Notes)
	}
	if w := findItem(sf, s.water); len(w.Notes) != 0 {
		t.Fatalf("water has notes: %+v", w.Notes)
	}

	// Two from a pick-one group, a tag of another dish, a tag twice: refused.
	bad := [][]int64{{tags["少糖"], tags["无糖"]}, {tags["少糖"], 99999999}, {tags["热"], tags["热"]}}
	for _, ids := range bad {
		_, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup",
			Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 1, NoteTagIDs: ids}}}, "")
		if err == nil {
			t.Fatalf("tags %v were taken", ids)
		}
	}
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup",
		Items: []app.QuoteLineInput{{ItemID: s.water, Qty: 1, NoteTagIDs: []int64{tags["热"]}}}}, "")
	wantCode(t, err, "NOTE_TAGS_INVALID")

	// The order keeps the tags in the dish's order, the free text beside them;
	// the price is untouched.
	q := s.quote(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1, Note: "珍珠多一点",
		NoteTagIDs: []int64{tags["打包分开装"], tags["热"], tags["少糖"], tags["不要吸管"]}}})
	p, err := a.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)
	o, err := a.OrderByToken(ctx, p.AccessToken)
	noErr(t, err)
	it := o.Items[0]
	if it.UnitPrice != 6900 || it.Note != "珍珠多一点" || len(it.NoteTags) != 4 || it.NoteTags[0].Name != "少糖" ||
		it.NoteTags[0].Group != "甜度" || it.NoteTags[1].Name != "热" || it.NoteTags[2].Name != "打包分开装" {
		t.Fatalf("order line: %+v", it)
	}

	// Renaming a tag and dropping another keeps ids of the rest; the order stays.
	l, err := a.NoteTemplates(ctx, s.branchID)
	noErr(t, err)
	in := app.NoteTemplateInput{Name: "奶茶", Icon: "🧋"}
	for _, g := range l[0].Groups {
		gi := app.NoteGroupInput{ID: g.ID, Name: g.Name, Multi: g.Multi}
		for _, x := range g.Tags {
			name := x.Name
			if name == "少糖" {
				name = "少少糖"
			}
			if name == "热" {
				continue
			}
			gi.Tags = append(gi.Tags, app.NoteTagInput{ID: x.ID, Name: name})
		}
		in.Groups = append(in.Groups, gi)
	}
	_, err = a.SaveNoteTemplate(ctx, s.branchID, tpl, in)
	noErr(t, err)
	l, err = a.NoteTemplates(ctx, s.branchID)
	noErr(t, err)
	if l[0].Name != "奶茶" || l[0].Groups[0].Tags[0].ID != tags["少糖"] || l[0].Groups[0].Tags[0].Name != "少少糖" ||
		len(l[0].Groups[1].Tags) != 1 || len(l[0].ItemIDs) != 1 {
		t.Fatalf("after edit: %+v", l[0])
	}
	got, err := a.OrderByToken(ctx, p.AccessToken)
	noErr(t, err)
	if got.Items[0].NoteTags[0].Name != "少糖" {
		t.Fatal("the order changed with the template")
	}

	// Again: the tags still offered by id, the one dropped as words.
	r, err := a.Reorder(ctx, p.AccessToken)
	noErr(t, err)
	if len(r.Lines) != 1 || len(r.Lines[0].NoteTagIDs) != 3 || r.Lines[0].Note != "热 珍珠多一点" {
		t.Fatalf("reorder: %+v", r.Lines)
	}
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup",
		Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 1, NoteTagIDs: []int64{tags["热"]}}}}, "")
	wantCode(t, err, "NOTE_TAGS_INVALID")

	// A dish shows at most three templates; deleting one unbinds it.
	for _, n := range []string{"A", "B", "C"} {
		id, err := a.SaveNoteTemplate(ctx, s.branchID, 0, app.NoteTemplateInput{Name: n,
			Groups: []app.NoteGroupInput{{Tags: []app.NoteTagInput{{Name: "x"}}}}})
		noErr(t, err)
		err = a.SetNoteTemplateItems(ctx, s.branchID, id, []int64{s.tea})
		if n == "C" {
			wantCode(t, err, "NOTE_TEMPLATES_FULL")
		} else {
			noErr(t, err)
		}
	}
	noErr(t, a.DeleteNoteTemplate(ctx, s.branchID, tpl))
	sf, err = a.Storefront(ctx, s.slug)
	noErr(t, err)
	tea = findItem(sf, s.tea)
	if len(tea.NoteTemplateIDs) != 2 {
		t.Fatalf("after delete: %v", tea.NoteTemplateIDs)
	}
	// The dish form replaces the dish's templates; left out it keeps them.
	_, err = a.SaveItem(ctx, s.branchID, s.water, app.ItemInput{CategoryID: tea.CategoryID, Name: "water", Price: 3000,
		Published: true, PickupAvailable: true, NoteTemplateIDs: &tea.NoteTemplateIDs})
	noErr(t, err)
	_, err = a.SaveItem(ctx, s.branchID, s.water, app.ItemInput{CategoryID: tea.CategoryID, Name: "water", Price: 3000,
		Published: true, PickupAvailable: true})
	noErr(t, err)
	sf, err = a.Storefront(ctx, s.slug)
	noErr(t, err)
	if w := findItem(sf, s.water); len(w.NoteTemplateIDs) != 2 {
		t.Fatalf("water templates: %v", w.NoteTemplateIDs)
	}
}

func TestStarterNoteTemplatesAndTranslation(t *testing.T) {
	need(t)
	s := newStore(t, false)
	ctx := context.Background()
	a := app.New(testApp.DB, "test-secret")
	a.Translator = &markTranslator{}
	st, err := a.StarterNoteTemplates(ctx, s.branchID)
	noErr(t, err)
	if len(st) != 5 || st[0].Name != "奶茶类" || st[0].Added {
		t.Fatalf("starters: %+v", st[0])
	}
	ids, err := a.AddStarterNoteTemplates(ctx, s.branchID, []string{"milk_tea", "general"})
	noErr(t, err)
	again, err := a.AddStarterNoteTemplates(ctx, s.branchID, []string{"milk_tea"})
	noErr(t, err)
	if len(ids) != 2 || len(again) != 0 {
		t.Fatalf("added %v, again %v", ids, again)
	}
	noErr(t, a.SetNoteTemplateItems(ctx, s.branchID, ids[0], []int64{s.tea}))
	noErr(t, a.TranslateMenu(ctx, s.branchID, false))
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	noErr(t, a.LocalizeStorefront(ctx, sf, "th"))
	tea := findItem(sf, s.tea)
	if tea.Notes[0].NameTr != "th:甜度" || tea.Notes[0].Tags[1].NameTr != "th:少糖" {
		t.Fatalf("thai: %q %q", tea.Notes[0].NameTr, tea.Notes[0].Tags[1].NameTr)
	}
	sf, err = a.Storefront(ctx, s.slug)
	noErr(t, err)
	noErr(t, a.LocalizeStorefront(ctx, sf, "en"))
	if tea := findItem(sf, s.tea); tea.Notes[1].Tags[2].NameTr != "No ice" {
		t.Fatalf("english: %+v", tea.Notes[1].Tags)
	}
	// An order in English keeps the tag's English beside the Chinese.
	tag := tea.Notes[0].Tags[1].ID
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Lang: "en",
		Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 1, NoteTagIDs: []int64{tag}}}}, "")
	noErr(t, err)
	if x := q.Lines[0].NoteTags[0]; x.Name != "少糖" || x.NameTr != "Less sugar" || x.GroupTr != "Sweetness" {
		t.Fatalf("line tag: %+v", x)
	}
}

// The 口味模板 column of the menu sheet: names of the store's templates; an
// unknown name is a row error, "无" takes them off, a blank cell keeps them;
// the exported sheet lists them.
func TestNoteTemplatesInMenuImport(t *testing.T) {
	need(t)
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	tpl, _ := s.milkTea(t)
	other, err := a.SaveNoteTemplate(ctx, s.branchID, 0, app.NoteTemplateInput{Name: "通用",
		Groups: []app.NoteGroupInput{{Multi: true, Tags: []app.NoteTagInput{{Name: "不要餐具"}}}}})
	noErr(t, err)
	head := []any{"分类 *", "菜名 *", "价格 *", "口味模板"}
	m := runImport(t, s.branchID, sheet(t, [][]any{head,
		{"C", "奶盖", 90, "奶茶类，通用"},
		{"C", "water", 30, "奶茶类, 没有这个"},
		{"C", "tea", 69, "无"},
		{"C", "chicken", 169, ""},
	}))
	if m.Status != "parsed" || m.ValidRows != 3 || m.Rows[1].Errors[0] != "note_template_unknown:没有这个" {
		t.Fatalf("parsed: %+v", m.Rows)
	}
	_, err = a.ApplyMenuImport(ctx, s.branchID, m.ID, app.Actor{Type: "staff"})
	noErr(t, err)
	items := itemsByName(t, s.branchID)
	if ids := items["奶盖"].NoteTemplateIDs; len(ids) != 2 || ids[0] != tpl || ids[1] != other {
		t.Fatalf("new dish: %v", ids)
	}
	if len(items["tea"].NoteTemplateIDs) != 0 || len(items["water"].NoteTemplateIDs) != 0 {
		t.Fatalf("tea %v water %v", items["tea"].NoteTemplateIDs, items["water"].NoteTemplateIDs)
	}
	b, err := a.MenuTemplate(ctx, s.branchID, true)
	noErr(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(b))
	noErr(t, err)
	rows, err := f.GetRows("商品")
	noErr(t, err)
	found := false
	for _, r := range rows {
		if len(r) > 1 && r[1] == "奶盖" && r[len(r)-1] == "奶茶类，通用" {
			found = true
		}
	}
	if !found || rows[0][len(rows[0])-1] != "口味模板" {
		t.Fatalf("export: %v", rows)
	}
}
