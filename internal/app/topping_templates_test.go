package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func TestToppingTemplates(t *testing.T) {
	need(t)
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp

	// bad templates are refused
	for _, in := range []app.ToppingTemplateInput{
		{Name: "", Options: []app.ToppingOptionInput{{Name: "上海青", Price: 2000}}},
		{Name: "麻辣烫", Options: nil},
		{Name: "麻辣烫", Options: []app.ToppingOptionInput{{Code: "AC02", Name: "上海青"}, {Code: "ac02", Name: "油麦菜"}}},
		{Name: "麻辣烫", Options: []app.ToppingOptionInput{{Name: "上海青", Price: -1}}},
	} {
		if _, err := a.SaveToppingTemplate(ctx, s.branchID, 0, in); err == nil {
			t.Fatalf("template %+v was taken", in)
		}
	}

	in := app.ToppingTemplateInput{Name: "麻辣烫", Options: []app.ToppingOptionInput{
		{Code: "ac02", Name: "上海青", Price: 2000}, {Code: "AC03", Name: "油麦菜", Price: 3000}}}
	tpl, err := a.SaveToppingTemplate(ctx, s.branchID, 0, in)
	noErr(t, err)
	noErr(t, a.SetToppingTemplateItems(ctx, s.branchID, tpl, []int64{s.tea}))

	group := func() *app.OptionGroup {
		sf, err := a.Storefront(ctx, s.slug)
		noErr(t, err)
		for i, g := range findItem(sf, s.tea).Groups {
			if g.ToppingTemplateID != nil && *g.ToppingTemplateID == tpl {
				return &findItem(sf, s.tea).Groups[i]
			}
		}
		return nil
	}
	g := group()
	if g == nil || g.Name != "加料" || g.MinSelect != 0 || g.MaxSelect != 2 || len(g.Options) != 2 ||
		g.Options[0].Code != "AC02" || g.Options[0].PriceDelta != 2000 {
		t.Fatalf("topping group on the dish: %+v", g)
	}
	keep := g.Options[0].ID

	// ordering with both toppings adds their prices
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup",
		Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 2, OptionIDs: []int64{g.Options[0].ID, g.Options[1].ID}}}}, "")
	noErr(t, err)
	sf, _ := a.Storefront(ctx, s.slug)
	if want := 2 * (findItem(sf, s.tea).Price + 5000); q.Totals.Subtotal != want {
		t.Fatalf("subtotal %d, want %d", q.Totals.Subtotal, want)
	}
	// the line marks them as toppings, so the customer's pages list them apart
	for _, o := range q.Lines[0].Options {
		if !o.Topping {
			t.Fatalf("option %s is not marked as a topping", o.Name)
		}
	}

	// editing the template follows on the dish; kept toppings keep their ids
	l, err := a.ToppingTemplates(ctx, s.branchID)
	noErr(t, err)
	if len(l) != 1 || len(l[0].ItemIDs) != 1 {
		t.Fatalf("templates: %+v", l)
	}
	o := l[0].Options
	_, err = a.SaveToppingTemplate(ctx, s.branchID, tpl, app.ToppingTemplateInput{Name: "麻辣烫", Title: "加菜", MaxSelect: 1, Options: []app.ToppingOptionInput{
		{ID: o[0].ID, Code: "AC02", Name: "上海青", Price: 2500, SoldOut: true}, {Code: "AC04", Name: "金针菇", Price: 1500}}})
	noErr(t, err)
	g = group()
	if g.Name != "加菜" || g.MaxSelect != 1 || len(g.Options) != 2 || g.Options[0].ID != keep || g.Options[0].PriceDelta != 2500 ||
		!g.Options[0].SoldOut || g.Options[1].Name != "金针菇" {
		t.Fatalf("after editing: %+v", g)
	}

	// saving the dish form keeps template groups out of its own groups
	tea := findItem(sf, s.tea)
	_, err = a.SaveItem(ctx, s.branchID, s.tea, app.ItemInput{CategoryID: tea.CategoryID, Name: tea.Name, Price: tea.Price,
		Published: true, PickupAvailable: true, Groups: []app.OptionGroupInput{{Name: "杯型", MinSelect: 1, MaxSelect: 1,
			Options: []app.OptionInput{{Name: "中杯"}, {Name: "大杯", PriceDelta: 500}}}}})
	noErr(t, err)
	sf, _ = a.Storefront(ctx, s.slug)
	tea = findItem(sf, s.tea)
	if len(tea.Groups) != 2 || tea.Groups[0].Name != "杯型" || group().Options[0].ID != keep || len(tea.ToppingTemplateIDs) != 1 {
		t.Fatalf("after the dish form: %+v", tea.Groups)
	}
	// the dish form can take the template off
	_, err = a.SaveItem(ctx, s.branchID, s.tea, app.ItemInput{CategoryID: tea.CategoryID, Name: tea.Name, Price: tea.Price,
		Published: true, PickupAvailable: true, ToppingTemplateIDs: &[]int64{}})
	noErr(t, err)
	if group() != nil {
		t.Fatal("the template group stayed after taking the template off")
	}

	// deleting the template removes its group from every dish
	noErr(t, a.SetToppingTemplateItems(ctx, s.branchID, tpl, []int64{s.tea, s.water}))
	noErr(t, a.DeleteToppingTemplate(ctx, s.branchID, tpl))
	sf, _ = a.Storefront(ctx, s.slug)
	for _, id := range []int64{s.tea, s.water} {
		for _, g := range findItem(sf, id).Groups {
			if g.ToppingTemplateID != nil {
				t.Fatalf("group left after delete: %+v", g)
			}
		}
	}
}
