package app_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/xuri/excelize/v2"
)

// Delivery platforms, the store's terms on them and dish platform prices
// (docs/ORDER_TYPES_DESIGN.md §6).
func TestDeliveryPlatforms(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	admin := adminActor(t)
	_, err := a.DB.Exec(ctx, `UPDATE branches SET country_code='PH' WHERE id=$1`, s.branchID)
	noErr(t, err)

	prefix := fmt.Sprintf("T%d", time.Now().UnixNano()%1e8)
	in := app.PlatformInput{Name: "TestEats", Prefix: prefix, Countries: []string{"ph", "TH"}, Color: "#123456",
		CommissionBP: 3000, MarkupBP: 2000, Active: true, NoPattern: `^TE-\d+$`}
	for _, bad := range []func(app.PlatformInput) app.PlatformInput{
		func(p app.PlatformInput) app.PlatformInput { p.Name = ""; return p },
		func(p app.PlatformInput) app.PlatformInput { p.Prefix = "T-E"; return p },
		func(p app.PlatformInput) app.PlatformInput { p.Countries = []string{"PHL"}; return p },
		func(p app.PlatformInput) app.PlatformInput { p.Color = "green"; return p },
		func(p app.PlatformInput) app.PlatformInput { p.NoPattern = "("; return p },
		func(p app.PlatformInput) app.PlatformInput { p.CommissionBP = 10001; return p },
		func(p app.PlatformInput) app.PlatformInput { p.CommissionBase = "net"; return p },
	} {
		_, err := a.SavePlatform(ctx, admin, 0, bad(in))
		wantCode(t, err, "VALIDATION")
	}
	id, err := a.SavePlatform(ctx, admin, 0, in)
	noErr(t, err)
	dup := in
	dup.Prefix = "t" + prefix[1:] // the prefix is unique whatever the case
	_, err = a.SavePlatform(ctx, admin, 0, dup)
	wantCode(t, err, "DUPLICATE")
	far := in
	far.Name, far.Prefix, far.Countries = "FarEats", prefix+"F", []string{"JP"}
	farID, err := a.SavePlatform(ctx, admin, 0, far)
	noErr(t, err)
	all, err := a.AdminPlatforms(ctx)
	noErr(t, err)
	var got *app.DeliveryPlatform
	for i := range all {
		if all[i].ID == id {
			got = &all[i]
		}
	}
	if got == nil || got.Countries[0] != "PH" || got.CommissionBase != "after_merchant" {
		t.Fatalf("saved platform %+v", got)
	}

	// The store sees the platforms of its country only, Grab and foodpanda among them.
	offered, err := a.BranchPlatforms(ctx, a.DB, s.branchID, 0, false)
	noErr(t, err)
	names := map[int64]bool{}
	for _, p := range offered {
		names[p.ID] = true
		if p.Enabled {
			t.Fatalf("%s on before the store turned it on", p.Name)
		}
	}
	if !names[id] || names[farID] {
		t.Fatalf("offered %+v", offered)
	}
	_, err = a.SaveBranchPlatforms(ctx, s.branchID, 0, []app.BranchPlatformInput{{PlatformID: farID, Enabled: true, RoundStep: 100}})
	wantCode(t, err, "VALIDATION")
	_, err = a.SaveBranchPlatforms(ctx, s.branchID, 0, []app.BranchPlatformInput{{PlatformID: id, Enabled: true, RoundStep: 300}})
	wantCode(t, err, "VALIDATION")
	own := 2500
	list, err := a.SaveBranchPlatforms(ctx, s.branchID, 0, []app.BranchPlatformInput{{PlatformID: id, Enabled: true, ShopRef: "PH-123",
		OwnCommission: &own, RoundStep: 100}})
	noErr(t, err)
	if list[0].ID != id || !list[0].Enabled || list[0].Commission != 2500 || list[0].Markup != 2000 || list[0].ShopRef != "PH-123" {
		t.Fatalf("store terms %+v", list[0])
	}

	menu, err := a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	cat := menu.Categories[0].ID
	// Dish prices: 169.00 × 1.2 = 202.80 → 203.00; water set by hand to 35.00.
	_, err = a.SaveItem(ctx, s.branchID, s.water, app.ItemInput{CategoryID: cat, Name: "water", Price: 3000, Published: true,
		DeliveryAvailable: true, PickupAvailable: true, PlatformPrices: &map[int64]int64{id: 3500}})
	noErr(t, err)
	menu, err = a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	if len(menu.Platforms) != 1 || menu.Platforms[0].ID != id {
		t.Fatalf("menu platforms %+v", menu.Platforms)
	}
	bp := menu.Platforms[0]
	for _, it := range menu.Categories[0].Items {
		var hand *int64
		if p, ok := it.PlatformPrices[id]; ok {
			hand = &p
		}
		price := bp.PlatformPrice(it.Price, hand)
		switch it.ID {
		case s.chicken:
			if price != 20300 {
				t.Fatalf("chicken on the platform %d", price)
			}
		case s.water:
			if hand == nil || price != 3500 {
				t.Fatalf("water on the platform %d", price)
			}
		}
	}
	// nil keeps the hand-set prices; an empty map clears them
	_, err = a.SaveItem(ctx, s.branchID, s.water, app.ItemInput{CategoryID: cat, Name: "water", Price: 3000, Published: true})
	noErr(t, err)
	menu, _ = a.AdminMenu(ctx, s.branchID)
	if p := menuItem(menu, s.water).PlatformPrices[id]; p != 3500 {
		t.Fatalf("kept price %d", p)
	}
	_, err = a.SaveItem(ctx, s.branchID, s.water, app.ItemInput{CategoryID: cat, Name: "water", Price: 3000, Published: true, PlatformPrices: &map[int64]int64{}})
	noErr(t, err)
	menu, _ = a.AdminMenu(ctx, s.branchID)
	if len(menuItem(menu, s.water).PlatformPrices) != 0 {
		t.Fatal("prices not cleared")
	}
	_, err = a.SaveItem(ctx, s.branchID, s.water, app.ItemInput{CategoryID: cat, Name: "water", Price: 3000, PlatformPrices: &map[int64]int64{id: -1}})
	wantCode(t, err, "VALIDATION")
}

func menuItem(m *app.AdminMenu, id int64) *app.Item {
	for ci := range m.Categories {
		for ii := range m.Categories[ci].Items {
			if m.Categories[ci].Items[ii].ID == id {
				return &m.Categories[ci].Items[ii]
			}
		}
	}
	return nil
}

// 平台价:Grab columns in the menu spreadsheet: a price sets it, "无" goes back
// to the price with the markup, a blank keeps it; the export writes them.
func TestMenuImportPlatformPrices(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	var grab, fp int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM delivery_platforms WHERE prefix='Grab'`).Scan(&grab))
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM delivery_platforms WHERE prefix='FP'`).Scan(&fp))
	_, err := a.DB.Exec(ctx, `UPDATE branches SET country_code='PH' WHERE id=$1`, s.branchID)
	noErr(t, err)
	_, err = a.SaveBranchPlatforms(ctx, s.branchID, 0, []app.BranchPlatformInput{{PlatformID: grab, Enabled: true, RoundStep: 100}})
	noErr(t, err)

	head := []any{"分类", "菜名", "价格", "平台价：Grab", "平台价:FP", "平台价:Nope"}
	m := runImport(t, s.branchID, sheet(t, [][]any{head,
		{"C", "chicken", 169, 199, "", ""},
		{"C", "water", 30, "abc", "", ""},
		{"C", "tea", 69, "", 80, "1"},
	}))
	codes := map[string][]string{}
	for _, r := range m.Rows {
		codes[r.Name] = r.Errors
	}
	if len(codes["chicken"]) != 0 || codes["water"][0] != "platform_price_invalid" || codes["tea"][0] != "platform_unknown:nope" {
		t.Fatalf("errors %v", codes)
	}
	_, err = a.ApplyMenuImport(ctx, s.branchID, m.ID, app.Actor{Type: "staff"})
	noErr(t, err)
	items := itemsByName(t, s.branchID)
	if items["chicken"].PlatformPrices[grab] != 19900 {
		t.Fatalf("chicken %v", items["chicken"].PlatformPrices)
	}

	// The export carries the store's platforms; "无" clears, a blank keeps.
	file, err := a.MenuTemplate(ctx, s.branchID, true)
	noErr(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(file))
	noErr(t, err)
	rows, err := f.GetRows("菜品")
	noErr(t, err)
	last := rows[0][len(rows[0])-1]
	if last != "平台价:Grab" {
		t.Fatalf("export header ends with %q", last)
	}
	found := false
	for _, r := range rows[1:] {
		if r[1] == "chicken" && len(r) == len(rows[0]) && r[len(r)-1] == "199" {
			found = true
		}
	}
	if !found {
		t.Fatalf("export rows %v", rows)
	}
	m = runImport(t, s.branchID, sheet(t, [][]any{{"分类", "菜名", "价格", "平台价:Grab"}, {"C", "chicken", 169, "无"}, {"C", "water", 30, ""}}))
	_, err = a.ApplyMenuImport(ctx, s.branchID, m.ID, app.Actor{Type: "staff"})
	noErr(t, err)
	if p, ok := itemsByName(t, s.branchID)["chicken"].PlatformPrices[grab]; ok {
		t.Fatalf("chicken price kept %d", p)
	}
	_ = fp
}
