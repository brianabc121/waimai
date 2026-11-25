package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// The restaurant waiting list (docs/QUEUE_DISPLAY_DESIGN.md §6): groups by
// party size, numbers, suggestions for free tables, upgrading a smaller
// party, holding a table, missed numbers, moving up the line and seating.
func TestQueueRestaurant(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	table := func(name string, seats int) int64 {
		id, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: name, Active: true, Seats: seats})
		noErr(t, err)
		return id
	}
	t2 := table("Q2", 2)
	t4 := table("Q4", 4)
	t6 := table("Q6", 6)
	t10 := table("Q10", 10)

	// Without restaurant mode nobody can take a number.
	_, err := a.TakeTicket(ctx, s.branchID, app.TakeTicketInput{People: 2, Source: "qr"}, app.Actor{Type: "customer"})
	wantCode(t, err, "QUEUE_CLOSED")
	set, err := a.QueueSettings(ctx, s.branchID)
	noErr(t, err)
	if set.UpgradeMinutes != 15 || set.RequeueAfter != 3 || set.CallAlertMinutes != 5 || !set.Media {
		t.Fatalf("defaults: %+v", set)
	}
	set.Restaurant = true
	_, err = a.SaveQueueSettings(ctx, s.branchID, set, s.staff)
	noErr(t, err)

	// The default groups follow the tables: A 1–2, B 3–4, C 5–8, D 9+ (a 10-seat table).
	d, err := a.QueueDesk(ctx, s.branchID)
	noErr(t, err)
	if len(d.Groups) != 4 || d.Groups[0].Letter != "A" || d.Groups[3].Letter != "D" || d.Groups[3].MinPeople != 9 {
		t.Fatalf("groups: %+v", d.Groups)
	}
	if len(d.Groups[0].Tables) != 1 || d.Groups[0].Tables[0] != t2 || d.Groups[3].Tables[0] != t10 {
		t.Fatalf("group tables: %+v", d.Groups)
	}

	guest := func(people int, client string) *app.QueueTicket {
		t.Helper()
		tk, err := a.TakeTicket(ctx, s.branchID, app.TakeTicketInput{People: people, Source: "qr", Client: client}, app.Actor{Type: "customer"})
		noErr(t, err)
		return tk
	}
	a1 := guest(2, "c1")
	if a1.Number != "A001" || a1.Position != 1 || a1.Token == "" {
		t.Fatalf("first number: %+v", a1)
	}
	// One number at a time per browser.
	_, err = a.TakeTicket(ctx, s.branchID, app.TakeTicketInput{People: 2, Source: "qr", Client: "c1"}, app.Actor{Type: "customer"})
	wantCode(t, err, "QUEUE_HAS_TICKET")
	a2 := guest(1, "c2")
	b1 := guest(4, "c3")
	if a2.Number != "A002" || b1.Number != "B001" || a2.Position != 2 {
		t.Fatalf("numbers: %s %s pos %d", a2.Number, b1.Number, a2.Position)
	}
	// Too many for any group.
	_, err = a.TakeTicket(ctx, s.branchID, app.TakeTicketInput{People: 99, Source: "paper"}, s.staff)
	noErr(t, err) // D takes 9–99
	// The desk's paper number, put first with a reason.
	p, err := a.TakeTicket(ctx, s.branchID, app.TakeTicketInput{People: 2, Source: "paper", Priority: true, Reason: "预约到店"}, s.staff)
	noErr(t, err)
	if p.Number != "A003" || p.Position != 1 || p.JumpReason != "预约到店" {
		t.Fatalf("priority number: %+v", p)
	}

	// Free tables: Q2 goes to the head of A (the priority number), Q4 to B001.
	d, err = a.QueueDesk(ctx, s.branchID)
	noErr(t, err)
	sug := map[int64]app.QueueSuggestion{}
	for _, x := range d.Suggestions {
		sug[x.TableID] = x
	}
	if sug[t2].TicketID != p.ID || sug[t4].TicketID != b1.ID || sug[t4].Upgrade {
		t.Fatalf("suggestions: %+v", d.Suggestions)
	}
	// Q6 (group C, nobody waiting): A001 has not waited 15 minutes yet, so no upgrade.
	if _, ok := sug[t6]; ok {
		t.Fatalf("upgrade offered too early: %+v", sug[t6])
	}
	// After 20 minutes A001 is offered the 6-seat table.
	_, err = a.DB.Exec(ctx, `UPDATE queue_tickets SET created_at = now() - interval '20 minutes' WHERE id=$1`, a1.ID)
	noErr(t, err)
	d, err = a.QueueDesk(ctx, s.branchID)
	noErr(t, err)
	sug = map[int64]app.QueueSuggestion{}
	for _, x := range d.Suggestions {
		sug[x.TableID] = x
	}
	if sug[t6].TicketID != a1.ID || !sug[t6].Upgrade {
		t.Fatalf("upgrade: %+v", d.Suggestions)
	}

	// Calling to a table holds it; the screen and the guest see it.
	c, err := a.CallTicket(ctx, s.branchID, a1.ID, app.QueueAction{TableID: t6}, s.staff)
	noErr(t, err)
	if c.Status != "called" || c.Calls != 1 || c.CalledTable != "Q6" {
		t.Fatalf("called: %+v", c)
	}
	holds, err := a.TableHolds(ctx, s.branchID)
	noErr(t, err)
	if holds[t6] != "A001" {
		t.Fatalf("holds: %v", holds)
	}
	g, err := a.TicketByToken(ctx, a1.Token)
	noErr(t, err)
	if g.Status != "called" || g.Table != "Q6" || len(g.Queue.Calling) != 1 || g.Queue.Calling[0].Number != "A001" {
		t.Fatalf("guest page: %+v", g)
	}
	// Calling again keeps the table.
	c, err = a.CallTicket(ctx, s.branchID, a1.ID, app.QueueAction{}, s.staff)
	noErr(t, err)
	if c.Calls != 2 || c.CalledTable != "Q6" {
		t.Fatalf("recalled: %+v", c)
	}
	// Seating someone else at the held table needs force.
	_, _, err = a.SeatTicket(ctx, s.branchID, a2.ID, app.QueueAction{TableID: t6}, s.staff)
	wantCode(t, err, "TABLE_HELD")
	// Seating A001 opens the table with the party size.
	seated, bill, err := a.SeatTicket(ctx, s.branchID, a1.ID, app.QueueAction{TableID: t6}, s.staff)
	noErr(t, err)
	if seated.Status != "seated" || bill.Guests != 2 || bill.TableID != t6 || seated.SessionID == nil {
		t.Fatalf("seated: %+v bill %+v", seated, bill)
	}

	// Missed and back: behind the head by 3 places (here: last of a short line).
	_, err = a.CallTicket(ctx, s.branchID, a2.ID, app.QueueAction{}, s.staff)
	noErr(t, err)
	sk, err := a.SkipTicket(ctx, s.branchID, a2.ID, s.staff)
	noErr(t, err)
	if sk.Status != "skipped" {
		t.Fatalf("skipped: %+v", sk)
	}
	g, err = a.TicketByToken(ctx, a2.Token)
	noErr(t, err)
	if len(g.Queue.Skipped) != 1 || g.Queue.Skipped[0] != "A002" {
		t.Fatalf("skipped list: %+v", g.Queue.Skipped)
	}
	more := []*app.QueueTicket{guest(2, "c4"), guest(2, "c5"), guest(2, "c6"), guest(2, "c7")}
	back, err := a.RequeueTicket(ctx, s.branchID, a2.ID, s.staff)
	noErr(t, err)
	// Line now: A003 (priority), A004..A007; A002 goes 4th (after the head and 3 more).
	if back.Status != "waiting" || back.Position != 4 {
		t.Fatalf("requeued: %+v", back)
	}

	// Moving up the line, with a reason; the log records it.
	_, err = a.JumpTicket(ctx, s.branchID, more[3].ID, app.QueueAction{Position: 1}, s.staff)
	wantCode(t, err, "VALIDATION")
	j, err := a.JumpTicket(ctx, s.branchID, more[3].ID, app.QueueAction{Position: 2, Reason: "老人"}, s.staff)
	noErr(t, err)
	if j.Position != 2 || j.JumpReason != "老人" {
		t.Fatalf("jumped: %+v", j)
	}
	log, err := a.QueueLog(ctx, s.branchID, "")
	noErr(t, err)
	if log[0].Action != "jump" || log[0].Number != "A007" || log[0].Detail["to"].(float64) != 2 || log[0].Actor == "" {
		t.Fatalf("log: %+v", log[0])
	}

	// A bigger party moves to its group and keeps its number.
	pp, err := a.ChangePeople(ctx, s.branchID, more[0].ID, app.QueueAction{People: 4}, s.staff)
	noErr(t, err)
	if pp.Number != "A004" || pp.GroupID != b1.GroupID {
		t.Fatalf("people changed: %+v", pp)
	}

	// The guest cancels; a cancelled number cannot be called.
	noErr(t, a.CancelByGuest(ctx, more[1].Token))
	_, err = a.CallTicket(ctx, s.branchID, more[1].ID, app.QueueAction{}, s.staff)
	wantCode(t, err, "INVALID_STATE")

	// Paused: the guest page cannot take numbers, the desk still can.
	_, err = a.SetQueuePaused(ctx, s.branchID, true, s.staff)
	noErr(t, err)
	_, err = a.TakeTicket(ctx, s.branchID, app.TakeTicketInput{People: 2, Source: "qr", Client: "c9"}, app.Actor{Type: "customer"})
	wantCode(t, err, "QUEUE_CLOSED")
	_, err = a.TakeTicket(ctx, s.branchID, app.TakeTicketInput{People: 2, Source: "paper"}, s.staff)
	noErr(t, err)

	pub, err := a.PublicQueue(ctx, s.slug)
	noErr(t, err)
	if !pub.Paused || len(pub.Groups) != 4 || pub.Groups[0].Waiting == 0 || pub.Groups[0].Current != "A002" {
		t.Fatalf("public: %+v", pub)
	}

	// Yesterday's numbers are void the next day.
	_, err = a.DB.Exec(ctx, `UPDATE queue_tickets SET biz_date = biz_date - 1 WHERE branch_id=$1`, s.branchID)
	noErr(t, err)
	d, err = a.QueueDesk(ctx, s.branchID)
	noErr(t, err)
	if len(d.Tickets) != 0 {
		t.Fatalf("yesterday still in line: %d", len(d.Tickets))
	}
	var void int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM queue_tickets WHERE branch_id=$1 AND status='void'`, s.branchID).Scan(&void))
	if void == 0 {
		t.Fatal("no numbers voided")
	}
}

// Groups are the store's to change; two groups may not take the same size.
func TestQueueGroups(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	_, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "W1", Active: true, Seats: 4})
	noErr(t, err)
	d, err := a.QueueDesk(ctx, s.branchID)
	noErr(t, err)
	if len(d.Groups) != 3 {
		t.Fatalf("default groups without big tables: %d", len(d.Groups))
	}
	_, err = a.SaveQueueGroups(ctx, s.branchID, []app.QueueGroupInput{
		{Letter: "A", MinPeople: 1, MaxPeople: 4}, {Letter: "B", MinPeople: 4, MaxPeople: 8}}, s.staff)
	wantCode(t, err, "VALIDATION")
	g, err := a.SaveQueueGroups(ctx, s.branchID, []app.QueueGroupInput{
		{ID: d.Groups[0].ID, Letter: "B", Label: "两人", MinPeople: 1, MaxPeople: 3},
		{ID: d.Groups[1].ID, Letter: "A", MinPeople: 4, MaxPeople: 20}}, s.staff)
	noErr(t, err)
	if len(g) != 2 || g[0].Letter != "B" || g[0].Label != "两人" || len(g[1].Tables) != 1 {
		t.Fatalf("saved groups: %+v", g)
	}
}

// Tea mode (§5): pickup numbers by how far the kitchen got; ready numbers
// can be called again and handed over.
func TestQueueTeaBoard(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	accept := func(lines []app.QuoteLineInput) *app.Order {
		t.Helper()
		o := s.place(t, "pickup", lines)
		o, err := a.Accept(ctx, o.ID, o.Version, s.staff)
		noErr(t, err)
		return o
	}
	wait := accept(s.cart())
	cook := accept(s.cart())
	_, err := a.SetItemStatus(ctx, cook.ID, itemByName(cook, "chicken").ID, "start", s.staff)
	noErr(t, err)
	ready := accept([]app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	_, err = a.SetItemStatus(ctx, ready.ID, itemByName(ready, "tea").ID, "ready", s.staff)
	noErr(t, err)
	water := accept([]app.QuoteLineInput{{ItemID: s.water, Qty: 1}}) // nothing to cook: ready at once
	// A delivery order is left off unless the store shows them.
	dl := s.place(t, "delivery", s.cart())
	_, err = a.Accept(ctx, dl.ID, dl.Version, s.staff)
	noErr(t, err)

	b, err := a.TeaBoard(ctx, s.branchID, true)
	noErr(t, err)
	if len(b.Wait) != 1 || b.Wait[0].Code != wait.PickupCode || len(b.Cook) != 1 || b.Cook[0].OrderID != cook.ID {
		t.Fatalf("wait/cook: %+v / %+v", b.Wait, b.Cook)
	}
	if len(b.Ready) != 2 || b.Ready[0].Calls != 1 || !b.Ready[0].CanHandOver || b.Ready[0].Items == "" {
		t.Fatalf("ready: %+v", b.Ready)
	}
	// Called again: first in the list with a newer stamp.
	before := *b.Ready[1].Stamp
	other := b.Ready[1].OrderID
	time.Sleep(5 * time.Millisecond)
	noErr(t, a.RecallPickup(ctx, s.branchID, other, s.staff))
	b, err = a.TeaBoard(ctx, s.branchID, false)
	noErr(t, err)
	if b.Ready[0].OrderID != other || b.Ready[0].Calls != 2 || !b.Ready[0].Stamp.After(before) || b.Ready[0].Items != "" {
		t.Fatalf("recalled: %+v", b.Ready[0])
	}
	// Handed over: packed, picked up and off the board.
	noErr(t, a.HandOverPickup(ctx, s.branchID, ready.ID, s.staff))
	o, err := a.GetOrder(ctx, ready.ID)
	noErr(t, err)
	if o.FulfillmentStatus != "picked_up" {
		t.Fatalf("handed over: %s", o.FulfillmentStatus)
	}
	noErr(t, a.HandOverPickup(ctx, s.branchID, water.ID, s.staff))
	b, err = a.TeaBoard(ctx, s.branchID, false)
	noErr(t, err)
	if len(b.Ready) != 0 {
		t.Fatalf("still ready: %+v", b.Ready)
	}
	// A ready number leaves the screen after the stay time.
	_, err = a.SetItemStatus(ctx, cook.ID, itemByName(cook, "chicken").ID, "ready", s.staff)
	noErr(t, err)
	_, err = a.SetItemStatus(ctx, cook.ID, itemByName(cook, "tea").ID, "ready", s.staff)
	noErr(t, err)
	b, err = a.TeaBoard(ctx, s.branchID, false)
	noErr(t, err)
	if len(b.Ready) != 1 {
		t.Fatalf("cook order not ready: %+v", b)
	}
	_, err = a.DB.Exec(ctx, `UPDATE order_items SET ready_at = now() - interval '20 minutes' WHERE order_id=$1`, cook.ID)
	noErr(t, err)
	b, err = a.TeaBoard(ctx, s.branchID, false)
	noErr(t, err)
	if len(b.Ready) != 0 {
		t.Fatalf("stale number still shown: %+v", b.Ready)
	}
	// Showing delivery orders brings them in.
	set, err := a.QueueSettings(ctx, s.branchID)
	noErr(t, err)
	set.ShowDelivery = true
	_, err = a.SaveQueueSettings(ctx, s.branchID, set, s.staff)
	noErr(t, err)
	b, err = a.TeaBoard(ctx, s.branchID, false)
	noErr(t, err)
	if len(b.Wait) != 2 {
		t.Fatalf("delivery not shown: %+v", b.Wait)
	}
}

// Screens pair with a one-time code and read the board of their mode.
func TestQueueScreens(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	_, _, err := a.CreateQueueScreen(ctx, s.branchID, app.QueueScreenInput{Name: "TV", Mode: "bad"}, s.staff)
	wantCode(t, err, "VALIDATION")
	sc, code, err := a.CreateQueueScreen(ctx, s.branchID, app.QueueScreenInput{Name: "TV", Mode: "tea", Lang: "zh", Lang2: "en"}, s.staff)
	noErr(t, err)
	if len(code) != 6 || sc.Paired || sc.PairExpiresAt == nil {
		t.Fatalf("new screen: %+v %q", sc, code)
	}
	_, err = a.PairQueueScreen(ctx, "000000x")
	wantCode(t, err, "PAIR_CODE_INVALID")
	token, err := a.PairQueueScreen(ctx, code[:3]+" "+code[3:])
	noErr(t, err)
	_, err = a.PairQueueScreen(ctx, code)
	wantCode(t, err, "PAIR_CODE_INVALID") // used once
	acc, err := a.ScreenByToken(ctx, token)
	noErr(t, err)
	if acc.BranchID != s.branchID || acc.Screen.Mode != "tea" || acc.Screen.Lang2 != "en" {
		t.Fatalf("access: %+v", acc)
	}
	v, err := a.ScreenView(ctx, acc)
	noErr(t, err)
	if v.Tea == nil || v.Queue != nil {
		t.Fatalf("tea view: %+v", v)
	}
	_, err = a.UpdateQueueScreen(ctx, s.branchID, sc.ID, app.QueueScreenInput{Name: "TV", Mode: "restaurant", Orientation: "portrait"})
	noErr(t, err)
	acc, err = a.ScreenByToken(ctx, token)
	noErr(t, err)
	v, err = a.ScreenView(ctx, acc)
	noErr(t, err)
	if v.Tea != nil || v.Queue == nil || acc.Screen.Orientation != "portrait" {
		t.Fatalf("restaurant view: %+v", v)
	}
	list, err := a.QueueScreens(ctx, s.branchID)
	noErr(t, err)
	if len(list) != 1 || !list[0].Paired || list[0].LastSeenAt == nil {
		t.Fatalf("list: %+v", list)
	}
	noErr(t, a.RevokeQueueScreen(ctx, s.branchID, sc.ID))
	_, err = a.ScreenByToken(ctx, token)
	wantCode(t, err, "SCREEN_NOT_PAIRED")
}
