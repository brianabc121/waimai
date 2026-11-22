package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

func TestKitchenReport(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	mk := func(name string) int64 {
		id, err := a.CreateStaff(ctx, s.branchID, app.NewUser{Username: uniq("cook"), Password: "password1", DisplayName: name, Role: "staff",
			Posts: []app.PostInput{{Kind: app.PostPacking}}})
		noErr(t, err)
		return id
	}
	ann, ben := mk("Ann"), mk("Ben")
	work := func(id int64) app.Actor { return app.Actor{Type: "staff", ID: id, Via: "work"} }
	board := app.Actor{Type: "staff", ID: ann, Via: "board"}
	do := func(o *app.Order, name, action string, by app.Actor) *app.Order {
		t.Helper()
		o, err := a.SetItemStatus(ctx, o.ID, itemByName(o, name).ID, action, by)
		noErr(t, err)
		return o
	}
	accepted := func() *app.Order {
		t.Helper()
		o := s.place(t, "delivery", s.cart()) // 2 x chicken, tea, water (no cooking)
		o, err := a.Accept(ctx, o.ID, o.Version, s.staff)
		noErr(t, err)
		return o
	}

	// order 1: Ann cooks the chicken in 20 minutes on her phone, the tea is
	// marked on the kitchen screen (signed in as Ann), Ann packs.
	o1 := accepted()
	o1 = do(o1, "chicken", "start", work(ann))
	_, err := a.DB.Exec(ctx, `UPDATE order_items SET prep_started_at = now() - interval '20 minutes' WHERE id=$1`, itemByName(o1, "chicken").ID)
	noErr(t, err)
	o1 = do(o1, "chicken", "ready", work(ann))
	o1 = do(o1, "tea", "ready", board)
	_, err = a.Pack(ctx, o1.ID, 1, o1.ManifestVersion, work(ann))
	noErr(t, err)

	// order 2: Ben starts the chicken, Ann finishes it; Ben makes the tea,
	// takes it back and makes it again. Then the order is cancelled.
	o2 := accepted()
	o2 = do(o2, "chicken", "start", work(ben))
	o2 = do(o2, "chicken", "ready", work(ann))
	o2 = do(o2, "tea", "ready", work(ben))
	o2 = do(o2, "tea", "undo", work(ben))
	o2 = do(o2, "tea", "ready", work(ben))
	_, err = a.StaffCancel(ctx, o2.ID, o2.Version, "customer left", s.staff)
	noErr(t, err)

	// yesterday Ben made three teas
	o3 := accepted()
	o3 = do(o3, "tea", "ready", work(ben))
	_, err = a.DB.Exec(ctx, `UPDATE order_items SET ready_at = ready_at - interval '1 day', qty = 3 WHERE id=$1`, itemByName(o3, "tea").ID)
	noErr(t, err)

	rep, err := a.KitchenReport(ctx, s.branchID, "2000-01-01", "2000-01-01")
	noErr(t, err)
	loc, err := time.LoadLocation(rep.Timezone)
	noErr(t, err)
	now := time.Now().In(loc)
	today, yesterday := now.Format("2006-01-02"), now.AddDate(0, 0, -1).Format("2006-01-02")
	if now.Hour() == 0 && now.Minute() < 25 {
		t.Skip("too close to midnight in the store's time zone for the back-dated dish")
	}

	rep, err = a.KitchenReport(ctx, s.branchID, today, today)
	noErr(t, err)
	who := func(id int64) app.KitchenPerson {
		t.Helper()
		for _, p := range rep.People {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("person %d is not in the report: %+v", id, rep.People)
		return app.KitchenPerson{}
	}
	// Ann: 2 + 2 chicken. Ben: 1 tea. The screen: 1 tea.
	if p := who(ann); p.Qty != 4 || p.Dishes != 2 || p.Orders != 2 || p.Packed != 1 || p.ShiftMinutes < 0 || p.DisplayName != "Ann" {
		t.Fatalf("Ann: %+v", p)
	}
	if p := who(ben); p.Qty != 1 || p.Dishes != 1 || p.Packed != 0 {
		t.Fatalf("Ben: %+v", p)
	}
	if p := who(0); p.Qty != 1 {
		t.Fatalf("kitchen screen: %+v", p)
	}
	if rep.TotalQty != 6 || rep.BoardQty != 1 || rep.People[0].ID != ann || rep.People[len(rep.People)-1].ID != 0 {
		t.Fatalf("report: %+v", rep)
	}
	// riders make nothing and are not listed
	for _, p := range rep.People {
		if p.ID == s.rider1 {
			t.Fatal("a rider without an in-store post is listed")
		}
	}

	d, err := a.KitchenDetail(ctx, s.branchID, ann, today, today, app.KitchenFilter{})
	noErr(t, err)
	if d.Qty != 4 || d.Dishes != 2 || d.Orders != 2 || d.Packed != 1 || d.Timed != 2 || d.SlowQty != 2 || d.SlowDishes != 1 || d.UndoCount != 0 {
		t.Fatalf("Ann's figures: %+v", d)
	}
	if d.AvgSeconds == nil || *d.AvgSeconds < 590 || *d.AvgSeconds > 620 {
		t.Fatalf("average of 20 minutes and a moment: %v", d.AvgSeconds)
	}
	if len(d.Days) != 7 || d.Days[6].Day != today || d.Days[6].Qty != 4 || d.Days[6].Packed != 1 || !d.Days[6].Worked || d.Days[5].Qty != 0 {
		t.Fatalf("Ann's days: %+v", d.Days)
	}
	if len(d.Top) != 1 || d.Top[0].Name != "chicken" || d.Top[0].Qty != 4 || len(d.Stations) != 1 {
		t.Fatalf("top %+v stations %+v", d.Top, d.Stations)
	}
	if d.ItemTotal != 2 || len(d.Items) != 2 {
		t.Fatalf("items: %+v", d.Items)
	}
	// newest first: the chicken of the cancelled order, started by Ben
	if it := d.Items[0]; !it.Cancelled || it.StartedBy != "Ben" || it.Slow || it.Via != "work" || it.Qty != 2 || len(it.Options) != 1 || it.PublicNo != o2.PublicNo {
		t.Fatalf("first item: %+v", it)
	}
	if it := d.Items[1]; it.Cancelled || !it.Slow || it.Seconds == nil || *it.Seconds < 1195 {
		t.Fatalf("second item: %+v", it)
	}
	slow, err := a.KitchenDetail(ctx, s.branchID, ann, today, today, app.KitchenFilter{SlowOnly: true})
	noErr(t, err)
	if slow.ItemTotal != 1 || slow.Qty != 4 {
		t.Fatalf("only the slow ones: %+v", slow)
	}
	none := int64(987654321)
	other, err := a.KitchenDetail(ctx, s.branchID, ann, today, today, app.KitchenFilter{StationID: &none, Page: 3})
	noErr(t, err)
	if other.ItemTotal != 0 || len(other.Items) != 0 || other.Page != 3 {
		t.Fatalf("another station: %+v", other)
	}

	// Ben: the tea he took back counts once; no start, so no time
	d, err = a.KitchenDetail(ctx, s.branchID, ben, today, today, app.KitchenFilter{})
	noErr(t, err)
	if d.Qty != 1 || d.UndoCount != 1 || d.AvgSeconds != nil || d.Timed != 0 || d.Items[0].Seconds != nil {
		t.Fatalf("Ben today: %+v", d)
	}
	if d.Days[5].Day != yesterday || d.Days[5].Qty != 3 {
		t.Fatalf("Ben's chart shows yesterday too: %+v", d.Days)
	}
	d, err = a.KitchenDetail(ctx, s.branchID, ben, yesterday, today, app.KitchenFilter{})
	noErr(t, err)
	if d.Qty != 4 || len(d.Days) != 7 {
		t.Fatalf("Ben, two days: %+v", d)
	}

	// the kitchen screen: the tea, with the account that was signed in
	d, err = a.KitchenDetail(ctx, s.branchID, 0, today, today, app.KitchenFilter{})
	noErr(t, err)
	if d.Qty != 1 || len(d.Items) != 1 || d.Items[0].Via != "board" || d.Items[0].ReadyBy != "Ann" || d.ShiftMinutes != 0 {
		t.Fatalf("kitchen screen: %+v", d)
	}

	_, err = a.KitchenReport(ctx, s.branchID, today, "nonsense")
	wantCode(t, err, "VALIDATION")
	_, err = a.KitchenReport(ctx, s.branchID, "2020-01-01", today)
	wantCode(t, err, "VALIDATION")
}
