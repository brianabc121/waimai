package app_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/xuri/excelize/v2"
)

func itemsByName(t *testing.T, branchID int64) map[string]app.Item {
	t.Helper()
	menu, err := testApp.AdminMenu(context.Background(), branchID)
	noErr(t, err)
	out := map[string]app.Item{}
	for _, c := range menu.Categories {
		for _, it := range c.Items {
			out[it.Name] = it
		}
	}
	return out
}

func runImport(t *testing.T, branchID int64, file []byte) *app.MenuImport {
	t.Helper()
	ctx := context.Background()
	id, err := testApp.CreateMenuImport(ctx, branchID, "m.xlsx", file, app.Actor{Type: "staff"})
	noErr(t, err)
	noErr(t, testApp.RunJobs(ctx))
	m, err := testApp.GetMenuImport(ctx, branchID, id)
	noErr(t, err)
	return m
}

// Pictures, details, options, dine-in and 上架 come in from the sheet; on an
// existing dish a blank cell keeps what it has and "无" clears it.
func TestMenuImportFullDish(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	head := []any{"分类 *", "菜名 *", "价格（PHP） *", "规格", "主图链接", "更多图片链接", "商品详情", "上架", "堂食", "外卖", "自取"}
	m := runImport(t, s.branchID, sheet(t, [][]any{head,
		{"面", "牛肉面", 180, "辣度：不辣，大辣+5\n加料[多选]：蛋+15，面加量+20", "https://img.example/beef.jpg",
			"https://img.example/1.jpg\nhttps://img.example/2.jpg", "慢炖六小时", "是", "否", "是", "是"},
		{"面", "阳春面", 90, "", "javascript:alert(1)", "", "", "", "", "", ""},
		{"面", "炸酱面", 120, "辣度", "", "", "", "", "", "", ""},
		{"面", "凉面", 100, "", "", "https://a.example/1 https://a.example/2 https://a.example/3 https://a.example/4 https://a.example/5 https://a.example/6 https://a.example/7 https://a.example/8 https://a.example/9", "", "", "", "", ""},
		{"C", "chicken", 175, "", "", "", "", "", "", "", ""},
	}))
	if m.Status != "parsed" || m.ValidRows != 2 {
		t.Fatalf("parsed: %s valid=%d %+v", m.Status, m.ValidRows, m.Rows)
	}
	codes := map[int]string{}
	for _, r := range m.Rows {
		if len(r.Errors) > 0 {
			codes[r.Line] = r.Errors[0]
		}
	}
	if codes[3] != "image_invalid" || codes[4] != "spec_invalid:辣度" || codes[5] != "photos_too_many:8" {
		t.Fatalf("row errors: %v", codes)
	}
	if !m.Rows[0].NewCategory || m.Rows[0].Action != "create" || len(m.Rows[0].Specs) != 2 || m.Rows[4].Action != "update" {
		t.Fatalf("preview: %+v / %+v", m.Rows[0], m.Rows[4])
	}
	m, err := a.ApplyMenuImport(ctx, s.branchID, m.ID, app.Actor{Type: "staff"})
	noErr(t, err)
	if m.CreatedCount != 1 || m.UpdatedCount != 1 {
		t.Fatalf("applied: %+v", m)
	}
	items := itemsByName(t, s.branchID)
	beef := items["牛肉面"]
	if !beef.Published || beef.DineInAvailable || !beef.DeliveryAvailable || beef.ImageURL != "https://img.example/beef.jpg" ||
		len(beef.Photos) != 2 || beef.Details != "慢炖六小时" || len(beef.Groups) != 2 ||
		beef.Groups[0].Options[1].PriceDelta != 500 || beef.Groups[1].MinSelect != 0 || beef.Groups[1].MaxSelect != 2 {
		t.Fatalf("beef noodles: %+v", beef)
	}
	// the existing chicken keeps its options and state: only the price changed
	chicken := items["chicken"]
	if chicken.Price != 17500 || len(chicken.Groups) != 1 || !chicken.Published {
		t.Fatalf("chicken: %+v", chicken)
	}

	// second round: blank keeps, "无" clears
	m = runImport(t, s.branchID, sheet(t, [][]any{head,
		{"面", "牛肉面", 190, "无", "", "无", "", "否", "", "", ""},
		{"C", "chicken", 175, "辣度[可选]：小辣，中辣+3", "/api/v1/images/abc", "", "无", "", "", "", ""},
	}))
	_, err = a.ApplyMenuImport(ctx, s.branchID, m.ID, app.Actor{Type: "staff"})
	noErr(t, err)
	items = itemsByName(t, s.branchID)
	beef, chicken = items["牛肉面"], items["chicken"]
	if beef.Price != 19000 || beef.Published || len(beef.Groups) != 0 || len(beef.Photos) != 0 ||
		beef.ImageURL != "https://img.example/beef.jpg" || beef.Details != "慢炖六小时" || beef.DineInAvailable {
		t.Fatalf("beef after update: %+v", beef)
	}
	if chicken.ImageURL != "/api/v1/images/abc" || len(chicken.Groups) != 1 || chicken.Groups[0].MinSelect != 0 ||
		chicken.Groups[0].Options[1].PriceDelta != 300 {
		t.Fatalf("chicken after update: %+v", chicken)
	}

	// the export of the current menu imports back without changing anything
	file, err := a.MenuTemplate(ctx, s.branchID, true)
	noErr(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(file))
	noErr(t, err)
	if sh := f.GetSheetList(); len(sh) != 2 || sh[0] != "菜品" {
		t.Fatalf("sheets: %v", sh)
	}
	h, _ := f.GetCellValue("菜品", "C1")
	f.Close()
	if h != "价格（PHP） *" {
		t.Fatalf("price header: %q", h)
	}
	m = runImport(t, s.branchID, file)
	if m.Status != "parsed" || m.ErrorRows != 0 || m.ValidRows != len(items) {
		t.Fatalf("export round trip: %s valid=%d errors=%d %+v", m.Status, m.ValidRows, m.ErrorRows, m.Rows)
	}
	for _, r := range m.Rows {
		if r.Action != "update" || *r.OldPrice != r.Price {
			t.Fatalf("round trip row: %+v", r)
		}
	}
	_, err = a.ApplyMenuImport(ctx, s.branchID, m.ID, app.Actor{Type: "staff"})
	noErr(t, err)
	after := itemsByName(t, s.branchID)
	for name, it := range items {
		b := after[name]
		if b.Price != it.Price || b.Published != it.Published || b.ImageURL != it.ImageURL || len(b.Photos) != len(it.Photos) ||
			b.Details != it.Details || len(b.Groups) != len(it.Groups) || b.NeedsPrep != it.NeedsPrep ||
			b.DineInAvailable != it.DineInAvailable || b.PickupAvailable != it.PickupAvailable ||
			(b.StockQty == nil) != (it.StockQty == nil) {
			t.Fatalf("%s changed by its own export:\n%+v\n%+v", name, it, b)
		}
		for i, g := range it.Groups {
			if b.Groups[i].Name != g.Name || b.Groups[i].MinSelect != g.MinSelect || b.Groups[i].MaxSelect != g.MaxSelect ||
				len(b.Groups[i].Options) != len(g.Options) {
				t.Fatalf("%s group %d changed: %+v", name, i, b.Groups[i])
			}
		}
	}
}

// A platform admin imports for a store; it is logged on both sides.
func TestAdminMenuImport(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	actor := adminActor(t)
	before := auditCount(t, "menu.import")
	tpl, err := a.MenuTemplate(ctx, s.branchID, false)
	noErr(t, err)
	id, err := a.AdminCreateMenuImport(ctx, actor, s.branchID, "t.xlsx", tpl)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	m, err := a.AdminApplyMenuImport(ctx, actor, s.branchID, id)
	noErr(t, err)
	if m.CreatedCount != 3 {
		t.Fatalf("admin import: %+v", m)
	}
	if auditCount(t, "menu.import") != before+1 {
		t.Fatal("the admin import is not in the admin log")
	}
	if _, err := a.AdminCreateMenuImport(ctx, actor, 999999999, "t.xlsx", tpl); err == nil {
		t.Fatal("unknown store")
	}
	it := itemsByName(t, s.branchID)["椒盐杏鲍菇"]
	if it.Published || len(it.Groups) != 2 || it.Details == "" {
		t.Fatalf("template example: %+v", it)
	}
}
