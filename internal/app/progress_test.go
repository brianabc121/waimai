package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func TestCookingProgress(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	accepted := func() *app.Order {
		t.Helper()
		o := s.place(t, "delivery", s.cart()) // 2 x chicken, tea, water (no cooking)
		o, err := a.Accept(ctx, o.ID, o.Version, s.staff)
		noErr(t, err)
		return o
	}
	do := func(o *app.Order, name, action string) *app.Order {
		t.Helper()
		o, err := a.SetItemStatus(ctx, o.ID, itemByName(o, name).ID, action, s.staff)
		noErr(t, err)
		return o
	}

	o1 := accepted()
	o2 := accepted()
	o2 = do(o2, "chicken", "start")
	if it := itemByName(o2, "chicken"); it.PrepStartedAt == nil || it.KitchenAt == nil || it.ReadyAt != nil {
		t.Fatalf("started dish: %+v", it)
	}

	p, err := a.Progress(ctx, s.branchID)
	noErr(t, err)
	if p.TargetMinutes != 15 || len(p.Orders) != 2 || p.Today.Count != 0 {
		t.Fatalf("progress: target %d, %d orders, today %+v", p.TargetMinutes, len(p.Orders), p.Today)
	}

	// The cooking target is a setting.
	_, err = a.DB.Exec(ctx, `UPDATE branches SET cook_target_minutes = 10 WHERE id=$1`, s.branchID)
	noErr(t, err)

	// o1: everything ready 12 minutes after it reached the kitchen: late.
	o1 = do(o1, "chicken", "ready")
	o1 = do(o1, "tea", "ready")
	_, err = a.DB.Exec(ctx, `UPDATE orders SET accepted_at = now() - interval '12 minutes' WHERE id=$1`, o1.ID)
	noErr(t, err)

	// Rushing: a dish being made can be rushed, a ready one cannot.
	o2, err = a.RushDish(ctx, itemByName(o2, "chicken").ID, s.staff)
	noErr(t, err)
	if itemByName(o2, "chicken").RushAt == nil {
		t.Fatal("chicken was not rushed")
	}
	if _, err := a.RushDish(ctx, itemByName(o1, "tea").ID, s.staff); err == nil {
		t.Fatal("a ready dish was rushed")
	}

	d, err := a.ProgressDone(ctx, s.branchID, "")
	noErr(t, err)
	if len(d.Batches) != 1 || d.TargetMinutes != 10 {
		t.Fatalf("done: %+v", d)
	}
	b := d.Batches[0]
	if b.OrderID != o1.ID || b.Qty != 3 || b.Minutes != 12 || !b.Late || b.BatchSeq != 1 || len(b.Cooks) != 1 {
		t.Fatalf("done batch: %+v", b)
	}
	if d.Summary.Count != 1 || d.Summary.Late != 1 || d.Summary.AvgMinute != 12 || d.Summary.MedianMinutes != 12 {
		t.Fatalf("summary: %+v", d.Summary)
	}

	// A dish taken back makes the batch unfinished again.
	o1 = do(o1, "tea", "undo")
	d, err = a.ProgressDone(ctx, s.branchID, "")
	noErr(t, err)
	if len(d.Batches) != 0 {
		t.Fatalf("an unfinished order counted as done: %+v", d.Batches)
	}

	// Another day has nothing.
	d, err = a.ProgressDone(ctx, s.branchID, "2001-02-03")
	noErr(t, err)
	if len(d.Batches) != 0 || d.Date != "2001-02-03" {
		t.Fatalf("old day: %+v", d)
	}
	if _, err := a.ProgressDone(ctx, s.branchID, "yesterday"); err == nil {
		t.Fatal("a bad date was accepted")
	}

	// A cancelled order cannot be rushed.
	_, err = a.StaffCancel(ctx, o2.ID, o2.Version, "customer left", s.staff)
	noErr(t, err)
	if _, err := a.RushDish(ctx, itemByName(o2, "chicken").ID, s.staff); err == nil {
		t.Fatal("a cancelled order was rushed")
	}
	p, err = a.Progress(ctx, s.branchID)
	noErr(t, err)
	cancelled := 0
	for _, o := range p.Orders {
		if o.AcceptanceStatus == "cancelled" {
			cancelled++
		}
	}
	if cancelled != 1 {
		t.Fatalf("the just cancelled order should stay on the page for the kitchen: %d", cancelled)
	}
}

// The new permission is on by default and needs "see orders".
func TestProgressPermission(t *testing.T) {
	for _, role := range []string{"cashier", "manager"} {
		found := false
		for _, p := range app.DefaultPerms(role) {
			found = found || p == app.PermOrderProgress
		}
		if !found {
			t.Fatalf("%s cannot see cooking progress by default", role)
		}
	}
}
