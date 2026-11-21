package app_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/brianabc121/waimai/internal/app"
)

func (s *store) settings(t *testing.T, mod func(*app.SettingsInput)) {
	t.Helper()
	in := app.SettingsInput{Name: "T", TaxRateBP: 1200, TaxInclusive: true, ServiceChargeBP: 500, PackagingFee: 1000,
		AcceptTimeoutMinutes: 10, DeliveryEnabled: true, PickupEnabled: true, OpenTime: "00:00", CloseTime: "00:00"}
	mod(&in)
	_, err := testApp.SaveSettings(context.Background(), s.branchID, in)
	noErr(t, err)
}

func TestScheduledOrders(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	tea := []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}
	quoteAt := func(at time.Time) (*app.Quote, error) {
		return a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: tea, ScheduledFor: &at}, "")
	}

	// off by default: no slots, and a scheduled quote is refused
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Slots) != 0 {
		t.Fatal("scheduling is off, yet slots are offered")
	}
	_, err = quoteAt(time.Now().Add(2 * time.Hour))
	wantCode(t, err, "SCHEDULE_INVALID")

	s.settings(t, func(in *app.SettingsInput) { in.SchedulingEnabled, in.PrepMinutes = true, 30 })
	sf, err = a.Storefront(ctx, s.slug)
	noErr(t, err)
	now := time.Now()
	if len(sf.Slots) < 96 {
		t.Fatalf("an all-day store should offer at least a day of slots, got %d", len(sf.Slots))
	}
	first, last := sf.Slots[0], sf.Slots[len(sf.Slots)-1]
	if first.Before(now.Add(30*time.Minute)) || first.After(now.Add(46*time.Minute)) || first.Minute()%15 != 0 {
		t.Fatalf("first slot %s is not the next quarter hour after the 30 minute lead time", first)
	}
	manila, _ := time.LoadLocation("Asia/Manila")
	l := now.In(manila)
	limit := time.Date(l.Year(), l.Month(), l.Day()+2, 0, 0, 0, 0, manila)
	if !last.Before(limit) || last.Before(limit.Add(-16*time.Minute)) {
		t.Fatalf("last slot %s should be the last quarter hour of tomorrow (limit %s)", last, limit)
	}

	// too soon and too far are refused
	_, err = quoteAt(now.Add(5 * time.Minute))
	wantCode(t, err, "SCHEDULE_INVALID")
	_, err = quoteAt(limit.Add(time.Hour))
	wantCode(t, err, "SCHEDULE_INVALID")

	// a scheduled order waits out of the kitchen's sight until its preparation time
	at := sf.Slots[8]
	q, err := quoteAt(at)
	noErr(t, err)
	p, err := a.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)
	o, err := a.GetOrder(ctx, p.OrderID)
	noErr(t, err)
	if o.ScheduledFor == nil || !o.ScheduledFor.Equal(at) || o.ReleaseAt == nil || !o.ReleaseAt.Equal(at.Add(-30*time.Minute)) {
		t.Fatalf("scheduled_for %v release_at %v, want %v", o.ScheduledFor, o.ReleaseAt, at)
	}
	asap := s.place(t, "pickup", tea)
	_, err = a.Accept(ctx, asap.ID, 0, s.staff)
	noErr(t, err)
	o, err = a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	if o.DisplayStatus != "scheduled" {
		t.Fatalf("accepted scheduled order should show as scheduled, got %s", o.DisplayStatus)
	}
	onBoard := func(id int64) bool {
		board, err := a.KitchenBoard(ctx, s.branchID)
		noErr(t, err)
		for _, x := range board {
			if x.ID == id {
				return true
			}
		}
		return false
	}
	if onBoard(o.ID) || !onBoard(asap.ID) {
		t.Fatal("the kitchen should see the ASAP order and not yet the scheduled one")
	}
	// the desk still lists it
	list, err := a.ListOrders(ctx, s.branchID, "active")
	noErr(t, err)
	if len(list) != 2 {
		t.Fatalf("desk should list both orders, got %d", len(list))
	}
	if n, err := a.ReleaseScheduled(ctx); err != nil || n != 0 {
		t.Fatalf("nothing is due yet: %d %v", n, err)
	}
	// preparation time arrives
	_, err = a.DB.Exec(ctx, `UPDATE orders SET release_at = now() - interval '1 second' WHERE id=$1`, o.ID)
	noErr(t, err)
	if !onBoard(o.ID) {
		t.Fatal("the kitchen should see the order once its preparation time has come")
	}
	o, err = a.GetOrder(ctx, o.ID)
	noErr(t, err)
	if o.DisplayStatus != "preparing" {
		t.Fatalf("released order should show as preparing, got %s", o.DisplayStatus)
	}
	n, err := a.ReleaseScheduled(ctx)
	noErr(t, err)
	n2, err := a.ReleaseScheduled(ctx)
	noErr(t, err)
	if n < 1 || n2 != 0 {
		t.Fatalf("the kitchen screens are told once: %d then %d", n, n2)
	}

	// opening hours: closed now, open in a window three hours from now
	open := now.In(manila).Add(3 * time.Hour)
	s.settings(t, func(in *app.SettingsInput) {
		in.SchedulingEnabled, in.PrepMinutes = true, 30
		in.OpenTime, in.CloseTime = open.Format("15:04"), open.Add(2*time.Hour).Format("15:04")
	})
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: tea}, "")
	wantCode(t, err, "STORE_CLOSED")
	_, err = quoteAt(now.Add(90 * time.Minute)) // before opening
	wantCode(t, err, "SCHEDULE_INVALID")
	sf, err = a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Slots) == 0 || sf.Slots[0].Before(open.Add(-time.Minute)) {
		t.Fatalf("slots must start at opening time: %v", sf.Slots)
	}
	// ordering ahead while the store is closed works
	q, err = quoteAt(sf.Slots[0])
	noErr(t, err)
	_, err = a.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
	noErr(t, err)

	// a paused store offers nothing
	s.settings(t, func(in *app.SettingsInput) { in.SchedulingEnabled, in.Paused = true, true })
	sf, err = a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Slots) != 0 {
		t.Fatal("a paused store must not offer slots")
	}
	_, err = quoteAt(now.Add(2 * time.Hour))
	wantCode(t, err, "STORE_CLOSED")
}

func sheet(t *testing.T, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		noErr(t, f.SetSheetRow("Sheet1", cell, &r))
	}
	var buf bytes.Buffer
	noErr(t, f.Write(&buf))
	return buf.Bytes()
}

func TestMenuImport(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	st, err := a.SaveStation(ctx, s.branchID, 0, app.StationInput{Name: "炸台", Active: true})
	noErr(t, err)

	// rejected before anything is stored
	_, err = a.CreateMenuImport(ctx, s.branchID, "menu.csv", []byte("a,b"), s.staff)
	wantCode(t, err, "VALIDATION")
	_, err = a.CreateMenuImport(ctx, s.branchID, "menu.xlsx", []byte("not a zip"), s.staff)
	wantCode(t, err, "VALIDATION")
	_, err = a.CreateMenuImport(ctx, s.branchID, "menu.xlsx", make([]byte, 3<<20), s.staff)
	wantCode(t, err, "VALIDATION")

	file := sheet(t, [][]any{
		{"分类", "菜名", "英文名", "价格", "描述", "工作台", "需要制作", "限量库存", "外卖", "自取"},
		{"炸物", "鸡排", "Chicken Cutlet", 189, "现炸", "炸台", "是", "", "是", "是"}, // 2 create, new category
		{"C", "tea", "", 79.9, "", "", "", "", "", ""},                     // 3 update price 69 -> 79.90
		{"炸物", "", "No name", 50, "", "", "", "", "", ""},                  // 4 name missing
		{"炸物", "薯条", "Fries", "abc", "", "", "", "", "", ""},               // 5 bad price
		{"炸物", "洋葱圈", "", 99, "", "烤台", "", "", "", ""},                    // 6 unknown station
		{"炸物", "鸡排", "", 199, "", "", "", "", "", ""},                      // 7 duplicate of line 2
		{}, // 8 blank line is skipped
		{"饮品", "苏打水", "Soda", 45, "", "", "否", 12, "是", "否"}, // 9 create, no prep, stock 12, no pickup
		{"饮品", "果汁", "", 60, "", "", "也许", "", "", ""},       // 10 bad yes/no
		{"饮品", "奶茶", "", 119.999, "", "", "", "", "", ""},    // 11 not a whole cent
		{"饮品", "冰沙", "", 88, "", "", "", -1, "", ""},         // 12 bad stock
	})
	id, err := a.CreateMenuImport(ctx, s.branchID, "菜单.xlsx", file, s.staff)
	noErr(t, err)
	m, err := a.GetMenuImport(ctx, s.branchID, id)
	noErr(t, err)
	if m.Status != "pending" {
		t.Fatalf("parsing is asynchronous, status is %s", m.Status)
	}
	// another store cannot see or apply it
	other := newStore(t, true)
	_, err = a.GetMenuImport(ctx, other.branchID, id)
	wantCode(t, err, "NOT_FOUND")
	_, err = a.ApplyMenuImport(ctx, s.branchID, id, s.staff)
	wantCode(t, err, "INVALID_STATE") // not parsed yet

	noErr(t, a.RunJobs(ctx))
	m, err = a.GetMenuImport(ctx, s.branchID, id)
	noErr(t, err)
	if m.Status != "parsed" || m.TotalRows != 10 || m.ValidRows != 3 || m.ErrorRows != 7 {
		t.Fatalf("parsed: %s total=%d valid=%d errors=%d (%s)", m.Status, m.TotalRows, m.ValidRows, m.ErrorRows, m.Error)
	}
	want := map[int]string{2: "", 3: "", 4: "name_required", 5: "price_invalid", 6: "station_unknown",
		7: "duplicate_of_line:2", 9: "", 10: "yes_no_invalid", 11: "price_invalid", 12: "stock_invalid"}
	for _, r := range m.Rows {
		got := ""
		if len(r.Errors) > 0 {
			got = r.Errors[0]
		}
		if w, ok := want[r.Line]; !ok || w != got {
			t.Fatalf("line %d: want error %q, got %v", r.Line, w, r.Errors)
		}
	}
	if m.Rows[0].Action != "create" || m.Rows[1].Action != "update" || m.Rows[1].OldPrice == nil ||
		*m.Rows[1].OldPrice != 6900 || m.Rows[1].Price != 7990 {
		t.Fatalf("preview actions: %+v %+v", m.Rows[0], m.Rows[1])
	}
	// nothing changed yet
	menu, err := a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	if len(menu.Categories) != 1 {
		t.Fatal("the preview must not touch the menu")
	}
	_, err = a.ApplyMenuImport(ctx, other.branchID, id, s.staff)
	wantCode(t, err, "NOT_FOUND")

	m, err = a.ApplyMenuImport(ctx, s.branchID, id, s.staff)
	noErr(t, err)
	if m.Status != "applied" || m.CreatedCount != 2 || m.UpdatedCount != 1 {
		t.Fatalf("applied: %s created=%d updated=%d", m.Status, m.CreatedCount, m.UpdatedCount)
	}
	_, err = a.ApplyMenuImport(ctx, s.branchID, id, s.staff)
	wantCode(t, err, "INVALID_STATE") // only once

	menu, err = a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	byName := map[string]app.Item{}
	for _, c := range menu.Categories {
		for _, it := range c.Items {
			byName[it.Name] = it
		}
	}
	if len(menu.Categories) != 3 {
		t.Fatalf("want the two new categories, got %d categories", len(menu.Categories))
	}
	cutlet, soda, tea := byName["鸡排"], byName["苏打水"], byName["tea"]
	if cutlet.Published || cutlet.Price != 18900 || cutlet.StationID == nil || *cutlet.StationID != st || cutlet.NameEn != "Chicken Cutlet" {
		t.Fatalf("new dish should be a draft at the fryer: %+v", cutlet)
	}
	if soda.Published || soda.NeedsPrep || soda.StockQty == nil || *soda.StockQty != 12 || soda.PickupAvailable || !soda.DeliveryAvailable {
		t.Fatalf("soda: %+v", soda)
	}
	if !tea.Published || tea.Price != 7990 || !tea.NeedsPrep {
		t.Fatalf("existing dish keeps its state and gets the new price: %+v", tea)
	}
	if _, ok := byName["洋葱圈"]; ok {
		t.Fatal("rows with errors must be skipped")
	}
	// customers see the new price, not the drafts
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Categories) != 1 {
		t.Fatalf("drafts leaked to the storefront: %d categories", len(sf.Categories))
	}

	// file-level failures are reported on the import, not retried forever
	for name, content := range map[string][]byte{
		"no header":  sheet(t, [][]any{{"foo", "bar"}, {"a", "b"}}),
		"empty":      sheet(t, [][]any{{"分类", "菜名", "价格"}}),
		"not a book": append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0}, 64)...),
	} {
		id, err := a.CreateMenuImport(ctx, s.branchID, "x.xlsx", content, s.staff)
		noErr(t, err)
		noErr(t, a.RunJobs(ctx))
		m, err := a.GetMenuImport(ctx, s.branchID, id)
		noErr(t, err)
		if m.Status != "failed" || m.Error == "" {
			t.Fatalf("%s: status %s error %q", name, m.Status, m.Error)
		}
	}

	// the template the app hands out parses cleanly
	tpl, err := app.MenuImportTemplate()
	noErr(t, err)
	id, err = a.CreateMenuImport(ctx, s.branchID, "menu-template.xlsx", tpl, s.staff)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	m, err = a.GetMenuImport(ctx, s.branchID, id)
	noErr(t, err)
	if m.Status != "parsed" || m.ErrorRows != 0 || m.ValidRows != 3 || m.Rows[2].Price != 21950 {
		t.Fatalf("template: %s errors=%d valid=%d", m.Status, m.ErrorRows, m.ValidRows)
	}
}
