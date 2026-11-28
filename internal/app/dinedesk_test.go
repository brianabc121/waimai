package app_test

import (
	"context"
	"sync"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// The staff desk (docs/DINE_IN_ORDER_DESIGN.md): areas and seats, tags on a
// sitting and on a round, changing guests, voiding part of a line, rushing,
// moving to a free table and recording how the bill was paid.
func TestDineDesk(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp

	a1, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "A1", Active: true, Area: " 大厅 ", Seats: 6})
	noErr(t, err)
	a2, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "A2", Active: true})
	noErr(t, err)
	_, err = a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "A3", Active: true, Seats: 100})
	wantCode(t, err, "VALIDATION")
	tables, err := a.ListTables(ctx, s.branchID)
	noErr(t, err)
	for _, tb := range tables {
		if tb.ID == a1 && (tb.Area != "大厅" || tb.Seats != 6) || tb.ID == a2 && tb.Seats != 4 {
			t.Fatalf("area and seats: %+v", tb)
		}
	}
	// editing without seats keeps them
	_, err = a.SaveTable(ctx, s.branchID, a1, app.TableInput{Name: "A1", Active: true, Area: "包间"})
	noErr(t, err)

	_, err = a.OpenTable(ctx, a1, 3, []string{"有小孩", "有小孩", " ", "123456789012345678901"}, s.staff)
	wantCode(t, err, "VALIDATION") // a tag over 20 characters
	bill, err := a.OpenTable(ctx, a1, 3, []string{"有小孩", "有小孩", " ", "生日"}, s.staff)
	noErr(t, err)
	if len(bill.Tags) != 2 || bill.Tags[1] != "生日" || bill.Area != "包间" || bill.Seats != 6 {
		t.Fatalf("sitting tags: %+v", bill)
	}
	sid := bill.SessionID

	// a round from the desk, with round notes; a guest's round drops them
	in := batch(app.QuoteLineInput{ItemID: s.chicken, Qty: 3, OptionIDs: []int64{s.spiceMild}}, app.QuoteLineInput{ItemID: s.tea, Qty: 1})
	in.RoundTags = []string{"先上饮料"}
	bill, err = a.AddBatch(ctx, sid, "staff", uniq("k"), in, s.staff)
	noErr(t, err)
	guestIn := batch(app.QuoteLineInput{ItemID: s.water, Qty: 1})
	guestIn.RoundTags = []string{"加急"}
	bill, err = a.AddBatch(ctx, sid, "qr", uniq("k"), guestIn, app.Actor{Type: "guest"})
	noErr(t, err)
	o := bill.Order
	if len(o.Items) != 3 || len(o.Items[0].RoundTags) != 1 || o.Items[0].RoundTags[0] != "先上饮料" ||
		o.Items[0].BatchBy == "" || len(o.Items[2].RoundTags) != 0 || o.Items[2].BatchBy != "" {
		t.Fatalf("round tags: %+v", o.Items)
	}
	if len(o.TableTags) != 2 {
		t.Fatalf("the kitchen sees the sitting's tags: %+v", o.TableTags)
	}

	// guests and tags change
	g := 4
	tags := []string{"VIP"}
	bill, err = a.UpdateSession(ctx, sid, app.SessionPatch{Guests: &g, Tags: &tags}, s.staff)
	noErr(t, err)
	if bill.Guests != 4 || len(bill.Tags) != 1 {
		t.Fatalf("patch: %+v", bill)
	}

	// rush the chicken; water needs no cooking and cannot be rushed
	chicken, water := o.Items[0].ID, o.Items[2].ID
	bill, err = a.RushItem(ctx, chicken, s.staff)
	noErr(t, err)
	if bill.Order.Items[0].RushAt == nil {
		t.Fatal("rush not recorded")
	}
	_, err = a.RushItem(ctx, water, s.staff)
	wantCode(t, err, "INVALID_STATE")

	// void one of three chickens: the line splits and the bill drops by one
	before := bill.Order.Totals.Subtotal
	unit := bill.Order.Items[0].UnitPrice
	bill, err = a.VoidItem(ctx, chicken, "点错了", 1, s.staff)
	noErr(t, err)
	var liveQty, voidQty int
	for _, it := range bill.Order.Items {
		if it.ItemID != s.chicken {
			continue
		}
		if it.PrepStatus == "cancelled" {
			voidQty += it.Qty
			if it.VoidReason != "点错了" || it.LineTotal != unit {
				t.Fatalf("voided part: %+v", it)
			}
		} else {
			liveQty += it.Qty
			if it.LineTotal != unit*2 || it.RushAt == nil {
				t.Fatalf("remaining part: %+v", it)
			}
		}
	}
	if liveQty != 2 || voidQty != 1 || bill.Order.Totals.Subtotal != before-unit {
		t.Fatalf("partial void: live %d void %d subtotal %d -> %d", liveQty, voidQty, before, bill.Order.Totals.Subtotal)
	}

	// move to A2: A1 is free again, A2 carries the bill
	b2, err := a.OpenTable(ctx, a2, 1, nil, s.staff)
	noErr(t, err)
	_, err = a.MoveTable(ctx, sid, a2, s.staff)
	wantCode(t, err, "TABLE_OCCUPIED")
	// close the empty A2 sitting so it is free
	_, err = a.CloseTable(ctx, b2.SessionID, app.CloseInput{ExpectedManifest: b2.Order.ManifestVersion}, s.staff)
	noErr(t, err)
	bill, err = a.MoveTable(ctx, sid, a2, s.staff)
	noErr(t, err)
	if bill.TableID != a2 || bill.TableName != "A2" || bill.MovedFrom != "A1" || bill.Order.TableName != "A2" {
		t.Fatalf("moved: %+v", bill)
	}
	tables, err = a.ListTables(ctx, s.branchID)
	noErr(t, err)
	for _, tb := range tables {
		if tb.ID == a1 && tb.Session != nil || tb.ID == a2 && (tb.Session == nil || tb.Session.ID != sid || tb.Session.Rounds != 2) {
			t.Fatalf("after move: %+v %+v", tb, tb.Session)
		}
	}
	_, err = a.MoveTable(ctx, sid, 999999, s.staff)
	wantCode(t, err, "NOT_FOUND")

	// finish the kitchen and close with a payment method
	for _, it := range bill.Order.Items {
		if it.PrepStatus == "pending" || it.PrepStatus == "preparing" {
			_, err = a.SetItemStatus(ctx, bill.Order.ID, it.ID, "ready", s.staff)
			noErr(t, err)
		}
	}
	bill, err = a.GetBill(ctx, sid)
	noErr(t, err)
	bill, err = a.CloseTable(ctx, sid, app.CloseInput{ExpectedManifest: bill.Order.ManifestVersion,
		AmountReceived: bill.Order.Totals.Total, PayMethod: "GCash"}, s.staff)
	noErr(t, err)
	if bill.Order.PayMethod != "GCash" || bill.Status != "closed" {
		t.Fatalf("closed: %+v", bill.Order)
	}

	// the most eaten-in dishes
	top, err := a.DineTop(ctx, s.branchID)
	noErr(t, err)
	if len(top) == 0 || top[0] != s.chicken {
		t.Fatalf("top: %v", top)
	}

	// presets
	p, err := a.GetDinePresets(ctx, s.branchID)
	noErr(t, err)
	if p.TableTags != nil || p.LongMinutes != 0 {
		t.Fatalf("default presets: %+v", p)
	}
	noErr(t, a.SaveDinePresets(ctx, s.branchID, app.DinePresets{RoundTags: []string{"先上饮料", " ", "先上饮料"}, LongMinutes: 120}))
	p, err = a.GetDinePresets(ctx, s.branchID)
	noErr(t, err)
	if len(p.RoundTags) != 1 || p.LongMinutes != 120 || p.VoidReasons != nil {
		t.Fatalf("saved presets: %+v", p)
	}
}

// Two desks moving two sittings to the same free table: only one wins.
func TestDineMoveRace(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	t1, _ := s.table(t, "R1")
	t2, _ := s.table(t, "R2")
	free, _ := s.table(t, "R3")
	b1, err := a.OpenTable(ctx, t1, 1, nil, s.staff)
	noErr(t, err)
	b2, err := a.OpenTable(ctx, t2, 1, nil, s.staff)
	noErr(t, err)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, sid := range []int64{b1.SessionID, b2.SessionID} {
		wg.Add(1)
		go func(i int, sid int64) {
			defer wg.Done()
			_, errs[i] = a.MoveTable(ctx, sid, free, s.staff)
		}(i, sid)
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("exactly one move should win: %v / %v", errs[0], errs[1])
	}
}

// 退桌: a table opened by mistake, or whose guests left before ordering, is
// freed without a sale; a table that has ordered is refused.
func TestDineRelease(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	t1, _ := s.table(t, "Q1")
	t2, _ := s.table(t, "Q2")

	b1, err := a.OpenTable(ctx, t1, 2, nil, s.staff)
	noErr(t, err)
	checked(t, "开台", b1.Order.ID)
	out, err := a.ReleaseTable(ctx, b1.SessionID, s.staff)
	noErr(t, err)
	if out.Status != "closed" || out.Order.AcceptanceStatus != "cancelled" || out.Order.Totals.Total != 0 || out.Order.PaymentStatus == "paid" {
		t.Fatalf("released bill: %+v", out)
	}
	checked(t, "退桌", b1.Order.ID)
	// a second tap changes nothing, and the table opens again for the next guests
	_, err = a.ReleaseTable(ctx, b1.SessionID, s.staff)
	noErr(t, err)
	_, err = a.AddBatch(ctx, b1.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: s.tea, Qty: 1}), s.staff)
	wantCode(t, err, "TABLE_SESSION_CLOSED")
	again, err := a.OpenTable(ctx, t1, 2, nil, s.staff)
	noErr(t, err)
	if again.SessionID == b1.SessionID {
		t.Fatal("a new sitting was expected")
	}

	// a table that has ordered cannot be released, not even after voiding everything
	b2, err := a.OpenTable(ctx, t2, 2, nil, s.staff)
	noErr(t, err)
	b2, err = a.AddBatch(ctx, b2.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: s.tea, Qty: 1}), s.staff)
	noErr(t, err)
	checked(t, "点单", b2.Order.ID)
	_, err = a.ReleaseTable(ctx, b2.SessionID, s.staff)
	wantCode(t, err, "TABLE_HAS_ORDER")
	checked(t, "退桌被拒", b2.Order.ID)
	// a paid table is not "released" either
	t3, _ := s.table(t, "Q3")
	paid, err := a.OpenTable(ctx, t3, 1, nil, s.staff)
	noErr(t, err)
	_, err = a.CloseTable(ctx, paid.SessionID, app.CloseInput{ExpectedManifest: paid.Order.ManifestVersion}, s.staff)
	noErr(t, err)
	_, err = a.ReleaseTable(ctx, paid.SessionID, s.staff)
	wantCode(t, err, "TABLE_SESSION_CLOSED")
}
