package app_test

import (
	"context"
	"sync"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func (s *store) table(t *testing.T, name string) (id int64, code string) {
	t.Helper()
	ctx := context.Background()
	id, err := testApp.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: name, Active: true})
	noErr(t, err)
	tables, err := testApp.ListTables(ctx, s.branchID)
	noErr(t, err)
	for _, tb := range tables {
		if tb.ID == id {
			return id, tb.QRToken
		}
	}
	t.Fatal("table not listed")
	return 0, ""
}

func batch(lines ...app.QuoteLineInput) app.BatchInput { return app.BatchInput{Items: lines} }

func live(o *app.Order) []app.OrderItem {
	var out []app.OrderItem
	for _, it := range o.Items {
		if it.PrepStatus != "cancelled" {
			out = append(out, it)
		}
	}
	return out
}

// The demo from the task list: table 08, the phone orders fried food, the
// tablet adds a drink, each station marks its dish ready, the runner serves,
// the cashier closes; the next scan only reaches a new session.
func TestDineInFullLoop(t *testing.T) {
	s := newStore(t, true) // 12% tax included, 5% service charge, packaging 10.00
	ctx := context.Background()
	a := testApp
	fryer, err := a.SaveStation(ctx, s.branchID, 0, app.StationInput{Name: "炸台", Active: true})
	noErr(t, err)
	bar, err := a.SaveStation(ctx, s.branchID, 0, app.StationInput{Name: "饮品台", Active: true})
	noErr(t, err)
	_, err = a.AssignStation(ctx, s.branchID, []int64{s.chicken}, &fryer)
	noErr(t, err)
	_, err = a.AssignStation(ctx, s.branchID, []int64{s.tea}, &bar)
	noErr(t, err)
	tableID, code := s.table(t, "08")

	// scanning before the staff open the table joins nothing
	_, err = a.ExchangeTableCode(ctx, code)
	wantCode(t, err, "TABLE_NOT_OPEN")
	_, err = a.ExchangeTableCode(ctx, "no-such-code")
	wantCode(t, err, "NOT_FOUND")

	bill, err := a.OpenTable(ctx, tableID, 2, s.staff)
	noErr(t, err)
	if bill.Status != "open" || bill.Order.Totals.Total != 0 || bill.Order.ServiceType != "dine_in" || bill.Order.TableName != "08" {
		t.Fatalf("new bill: %+v", bill.Order)
	}
	_, err = a.OpenTable(ctx, tableID, 2, s.staff)
	wantCode(t, err, "TABLE_OCCUPIED")

	// phone: scan, then order fried chicken
	token, err := a.ExchangeTableCode(ctx, code)
	noErr(t, err)
	phone, err := a.AccessByToken(ctx, token)
	noErr(t, err)
	if phone.SessionID != bill.SessionID || phone.Mode != "qr" || phone.TableName != "08" {
		t.Fatalf("phone access: %+v", phone)
	}
	key1 := uniq("k")
	in1 := batch(app.QuoteLineInput{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}})
	bill, err = a.AddBatch(ctx, phone.SessionID, "qr", key1, in1, app.Actor{Type: "guest"})
	noErr(t, err)

	// tablet: paired by staff, adds a drink and a bottle of water
	pairCode, err := a.CreateDevicePairing(ctx, tableID, "iPad 08", s.staff)
	noErr(t, err)
	_, err = a.PairDevice(ctx, "WRONG123")
	wantCode(t, err, "PAIR_CODE_INVALID")
	devToken, err := a.PairDevice(ctx, pairCode)
	noErr(t, err)
	_, err = a.PairDevice(ctx, pairCode)
	wantCode(t, err, "PAIR_CODE_INVALID") // one use only
	tablet, err := a.AccessByDevice(ctx, devToken)
	noErr(t, err)
	if tablet.SessionID != bill.SessionID || tablet.Mode != "ipad" {
		t.Fatalf("tablet access: %+v", tablet)
	}
	bill, err = a.AddBatch(ctx, tablet.SessionID, "ipad", uniq("k"),
		batch(app.QuoteLineInput{ItemID: s.tea, Qty: 1}, app.QuoteLineInput{ItemID: s.water, Qty: 1}), app.Actor{Type: "guest"})
	noErr(t, err)

	// one bill, two separate batches, each remembering where it came from
	o := bill.Order
	if len(o.Items) != 3 || o.Items[0].BatchSeq != 1 || o.Items[0].BatchSource != "qr" ||
		o.Items[1].BatchSeq != 2 || o.Items[1].BatchSource != "ipad" || o.Items[2].BatchSeq != 2 {
		t.Fatalf("batches: %+v", o.Items)
	}
	// 2 x 169 + 69 + 30 = 437; service charge 5% = 21.85; no packaging fee when eating in
	if o.Totals.Subtotal != 43700 || o.Totals.PackagingFee != 0 || o.Totals.ServiceCharge != 2185 || o.Totals.Total != 45885 {
		t.Fatalf("bill totals: %+v", o.Totals)
	}
	// dispatched per station; water needs no kitchen work
	if o.Items[0].StationName != "炸台" || o.Items[1].StationName != "饮品台" || o.Items[2].PrepStatus != "ready" {
		t.Fatalf("dispatch: %+v", o.Items)
	}

	// the phone's request timed out and is sent again with the same key: still one batch
	bill, err = a.AddBatch(ctx, phone.SessionID, "qr", key1, in1, app.Actor{Type: "guest"})
	noErr(t, err)
	if len(bill.Order.Items) != 3 {
		t.Fatalf("retry added a batch: %d items", len(bill.Order.Items))
	}
	_, err = a.AddBatch(ctx, phone.SessionID, "qr", key1, batch(app.QuoteLineInput{ItemID: s.tea, Qty: 5}), app.Actor{Type: "guest"})
	wantCode(t, err, "IDEMPOTENCY_CONFLICT")

	// the kitchen board shows the bill; the delivery desk does not
	board, err := a.KitchenBoard(ctx, s.branchID)
	noErr(t, err)
	if len(board) != 1 || board[0].ID != o.ID {
		t.Fatalf("kitchen board: %d orders", len(board))
	}
	desk, err := a.ListOrders(ctx, s.branchID, "active")
	noErr(t, err)
	if len(desk) != 0 {
		t.Fatal("dine-in bills do not belong on the delivery desk")
	}
	// delivery commands are refused for a bill
	_, err = a.StaffCancel(ctx, o.ID, 0, "x", s.staff)
	wantCode(t, err, "INVALID_STATE")
	_, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	wantCode(t, err, "INVALID_STATE")

	// cannot serve what is not ready; cannot close while the kitchen is busy
	chicken, tea, water := o.Items[0].ID, o.Items[1].ID, o.Items[2].ID
	_, err = a.ServeItem(ctx, chicken, s.staff)
	wantCode(t, err, "INVALID_STATE")
	_, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: bill.Order.ManifestVersion, AmountReceived: 45885}, s.staff)
	wantCode(t, err, "ITEMS_IN_PROGRESS")

	// each station marks its own dish; the runner serves
	_, err = a.SetItemStatus(ctx, o.ID, chicken, "ready", s.staff)
	noErr(t, err)
	_, err = a.SetItemStatus(ctx, o.ID, tea, "ready", s.staff)
	noErr(t, err)
	bill, err = a.ServeItem(ctx, chicken, s.staff)
	noErr(t, err)
	bill, err = a.ServeItem(ctx, chicken, s.staff) // twice is harmless
	noErr(t, err)
	if bill.Order.Items[0].ServedAt == nil {
		t.Fatal("served_at not recorded")
	}
	_, err = a.SetItemStatus(ctx, o.ID, chicken, "undo", s.staff)
	wantCode(t, err, "INVALID_STATE") // already on the table
	_, _ = tea, water

	// the cashier must be looking at the latest bill
	stale := bill.Order.ManifestVersion
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: s.water, Qty: 1}), s.staff)
	noErr(t, err)
	_, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: stale, AmountReceived: 99999}, s.staff)
	wantCode(t, err, "VERSION_CONFLICT")
	_, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{AmountReceived: 99999}, s.staff)
	wantCode(t, err, "VERSION_CONFLICT") // the version is not optional
	total := bill.Order.Totals.Total     // 437 + 30 = 467, + 23.35 service charge
	if total != 46700+2335 {
		t.Fatalf("total after third batch: %d", total)
	}
	_, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: bill.Order.ManifestVersion, AmountReceived: total - 1}, s.staff)
	wantCode(t, err, "VALIDATION")
	closed, err := a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: bill.Order.ManifestVersion, AmountReceived: 50000}, s.staff)
	noErr(t, err)
	co := closed.Order
	if closed.Status != "closed" || co.PaymentStatus != "paid" || co.DisplayStatus != "completed" ||
		co.AmountReceived == nil || *co.AmountReceived != 50000 {
		t.Fatalf("closed bill: %s %s %s %v", closed.Status, co.PaymentStatus, co.DisplayStatus, co.AmountReceived)
	}
	for _, it := range co.Items {
		if it.ServedAt == nil {
			t.Fatalf("ready dishes are taken as served on closing: %+v", it)
		}
	}
	// closing again changes nothing
	again, err := a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: 1, AmountReceived: 1}, s.staff)
	noErr(t, err)
	if *again.Order.AmountReceived != 50000 {
		t.Fatal("second close overwrote the payment")
	}

	// the old phone is out; its late add-on is refused; the original key still answers
	_, err = a.AccessByToken(ctx, token)
	wantCode(t, err, "TABLE_SESSION_CLOSED")
	_, err = a.AddBatch(ctx, bill.SessionID, "qr", uniq("k"), batch(app.QuoteLineInput{ItemID: s.tea, Qty: 1}), app.Actor{Type: "guest"})
	wantCode(t, err, "TABLE_SESSION_CLOSED")
	replay, err := a.AddBatch(ctx, bill.SessionID, "qr", key1, in1, app.Actor{Type: "guest"})
	noErr(t, err)
	if len(live(replay.Order)) != 4 {
		t.Fatalf("replay after close should return the final bill: %d lines", len(live(replay.Order)))
	}
	_, err = a.ExchangeTableCode(ctx, code)
	wantCode(t, err, "TABLE_NOT_OPEN")
	// the tablet stays paired but has no session until the next one
	tablet, err = a.AccessByDevice(ctx, devToken)
	noErr(t, err)
	if tablet.SessionID != 0 {
		t.Fatal("tablet still sees the closed session")
	}
	// the kitchen board no longer shows the closed bill
	board, err = a.KitchenBoard(ctx, s.branchID)
	noErr(t, err)
	if len(board) != 0 {
		t.Fatal("closed bill still on the kitchen board")
	}

	// next guests: a new session, a new bill, a new token
	next, err := a.OpenTable(ctx, tableID, 4, s.staff)
	noErr(t, err)
	token2, err := a.ExchangeTableCode(ctx, code)
	noErr(t, err)
	acc2, err := a.AccessByToken(ctx, token2)
	noErr(t, err)
	if next.SessionID == bill.SessionID || acc2.SessionID != next.SessionID || token2 == token || len(next.Order.Items) != 0 {
		t.Fatal("the next sitting must be a fresh session")
	}
	_, err = a.AccessByToken(ctx, token)
	wantCode(t, err, "TABLE_SESSION_CLOSED") // the old token never comes back
	tablet, err = a.AccessByDevice(ctx, devToken)
	noErr(t, err)
	if tablet.SessionID != next.SessionID {
		t.Fatal("tablet should follow its table to the new session")
	}
	recent, err := a.RecentBills(ctx, s.branchID)
	noErr(t, err)
	if len(recent) != 1 || recent[0].SessionID != bill.SessionID {
		t.Fatalf("recent bills: %d", len(recent))
	}

	// revoking the tablet ends its access; another store cannot revoke it
	tables, err := a.ListTables(ctx, s.branchID)
	noErr(t, err)
	dev := tables[0].Devices[0].ID
	other := newStore(t, true)
	wantCode(t, a.RevokeDevice(ctx, other.branchID, dev), "NOT_FOUND")
	noErr(t, a.RevokeDevice(ctx, s.branchID, dev))
	_, err = a.AccessByDevice(ctx, devToken)
	wantCode(t, err, "DEVICE_NOT_PAIRED")
}

func TestDineInPricesStockAndVoid(t *testing.T) {
	s := newStore(t, false) // tax added on top
	ctx := context.Background()
	a := testApp
	tableID, _ := s.table(t, "A1")
	bill, err := a.OpenTable(ctx, tableID, 1, s.staff)
	noErr(t, err)
	sid := bill.SessionID
	guest := app.Actor{Type: "guest"}

	// the amount the guest saw must still be right
	wrong := int64(1)
	_, err = a.AddBatch(ctx, sid, "qr", uniq("k"), app.BatchInput{Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}, ExpectedTotal: &wrong}, guest)
	wantCode(t, err, "QUOTE_CHANGED")
	right := int64(6900)
	bill, err = a.AddBatch(ctx, sid, "qr", uniq("k"), app.BatchInput{Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}, ExpectedTotal: &right}, guest)
	noErr(t, err)
	// 69 + 5% service 3.45 = 72.45, tax 12% = 8.69
	if tt := bill.Order.Totals; tt.TaxAmount != 869 || tt.Total != 6900+345+869 {
		t.Fatalf("tax-exclusive bill: %+v", tt)
	}

	// invalid carts
	_, err = a.AddBatch(ctx, sid, "qr", uniq("k"), batch(), guest)
	wantCode(t, err, "VALIDATION")
	_, err = a.AddBatch(ctx, sid, "qr", "", batch(app.QuoteLineInput{ItemID: s.tea, Qty: 1}), guest)
	wantCode(t, err, "VALIDATION")
	_, err = a.AddBatch(ctx, sid, "qr", uniq("k"), batch(app.QuoteLineInput{ItemID: s.chicken, Qty: 1}), guest)
	wantCode(t, err, "OPTIONS_INVALID")
	other := newStore(t, true)
	_, err = a.AddBatch(ctx, sid, "qr", uniq("k"), batch(app.QuoteLineInput{ItemID: other.tea, Qty: 1}), guest)
	wantCode(t, err, "ITEM_UNAVAILABLE")

	// limited stock is shared with every other channel
	bill, err = a.AddBatch(ctx, sid, "qr", uniq("k"), batch(app.QuoteLineInput{ItemID: s.limited, Qty: 1}), guest)
	noErr(t, err)
	_, err = a.AddBatch(ctx, sid, "qr", uniq("k"), batch(app.QuoteLineInput{ItemID: s.limited, Qty: 1}), guest)
	wantCode(t, err, "ITEM_UNAVAILABLE")
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s.limited, Qty: 1}}}, "")
	wantCode(t, err, "ITEM_UNAVAILABLE")

	// voiding takes the line off the bill, returns the stock and needs a reason
	var limitedLine int64
	for _, it := range bill.Order.Items {
		if it.ItemID == s.limited {
			limitedLine = it.ID
		}
	}
	_, err = a.VoidItem(ctx, limitedLine, " ", s.staff)
	wantCode(t, err, "VALIDATION")
	before := bill.Order.ManifestVersion
	bill, err = a.VoidItem(ctx, limitedLine, "guest changed their mind", s.staff)
	noErr(t, err)
	if bill.Order.Totals.Subtotal != 6900 || bill.Order.ManifestVersion == before || len(live(bill.Order)) != 1 {
		t.Fatalf("after void: %+v", bill.Order.Totals)
	}
	bill, err = a.VoidItem(ctx, limitedLine, "again", s.staff) // twice is harmless
	noErr(t, err)
	s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.limited, Qty: 1}}) // the portion is back

	// a table with unfinished dishes can be closed once they are voided; an empty bill closes at zero
	var teaLine int64
	for _, it := range live(bill.Order) {
		teaLine = it.ID
	}
	bill, err = a.VoidItem(ctx, teaLine, "kitchen ran out", s.staff)
	noErr(t, err)
	closed, err := a.CloseTable(ctx, sid, app.CloseInput{ExpectedManifest: bill.Order.ManifestVersion}, s.staff)
	noErr(t, err)
	if closed.Status != "closed" || closed.Order.Totals.Total != 0 {
		t.Fatalf("empty bill: %+v", closed.Order.Totals)
	}
	_, err = a.VoidItem(ctx, teaLine, "late", s.staff)
	wantCode(t, err, "TABLE_SESSION_CLOSED") // a closed bill is final
	// a disabled table cannot be opened; a busy one cannot be disabled
	_, err = a.SaveTable(ctx, s.branchID, tableID, app.TableInput{Name: "A1", Active: false})
	noErr(t, err)
	_, err = a.OpenTable(ctx, tableID, 1, s.staff)
	wantCode(t, err, "INVALID_STATE")
	t2, _ := s.table(t, "A2")
	_, err = a.OpenTable(ctx, t2, 1, s.staff)
	noErr(t, err)
	_, err = a.SaveTable(ctx, s.branchID, t2, app.TableInput{Name: "A2", Active: false})
	wantCode(t, err, "INVALID_STATE")
	_, err = a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "A2", Active: true})
	wantCode(t, err, "VALIDATION") // duplicate name
}

// Closing and adding at the same moment: either the batch lands first and the
// cashier is told the bill changed, or the close lands first and the batch is
// told the table has closed. Never both, never a paid bill with unpaid dishes.
func TestDineInCloseVersusAddOn(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	for round := 0; round < 12; round++ {
		tableID, _ := s.table(t, uniq("T"))
		bill, err := a.OpenTable(ctx, tableID, 1, s.staff)
		noErr(t, err)
		bill, err = a.AddBatch(ctx, bill.SessionID, "qr", uniq("k"), batch(app.QuoteLineInput{ItemID: s.water, Qty: 1}), app.Actor{Type: "guest"})
		noErr(t, err)
		seen := bill.Order
		var wg sync.WaitGroup
		var closeErr, addErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, closeErr = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: seen.ManifestVersion, AmountReceived: seen.Totals.Total}, s.staff)
		}()
		go func() {
			defer wg.Done()
			_, addErr = a.AddBatch(ctx, bill.SessionID, "ipad", uniq("k"), batch(app.QuoteLineInput{ItemID: s.water, Qty: 1}), app.Actor{Type: "guest"})
		}()
		wg.Wait()
		final, err := a.GetBill(ctx, bill.SessionID)
		noErr(t, err)
		switch {
		case closeErr == nil && addErr != nil:
			wantCode(t, addErr, "TABLE_SESSION_CLOSED")
			if final.Status != "closed" || len(final.Order.Items) != 1 || *final.Order.AmountReceived != final.Order.Totals.Total {
				t.Fatalf("round %d: close won but the bill is wrong: %+v", round, final.Order.Totals)
			}
		case closeErr != nil && addErr == nil:
			wantCode(t, closeErr, "VERSION_CONFLICT")
			if final.Status != "open" || len(final.Order.Items) != 2 || final.Order.PaymentStatus != "unpaid" {
				t.Fatalf("round %d: add-on won but the bill is wrong", round)
			}
		default:
			t.Fatalf("round %d: exactly one must win: close=%v add=%v", round, closeErr, addErr)
		}
	}
}

func TestDineInSameBatchSubmittedTwiceAtOnce(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	tableID, _ := s.table(t, "B1")
	bill, err := a.OpenTable(ctx, tableID, 1, s.staff)
	noErr(t, err)
	key := uniq("k")
	in := batch(app.QuoteLineInput{ItemID: s.tea, Qty: 2})
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = a.AddBatch(ctx, bill.SessionID, "qr", key, in, app.Actor{Type: "guest"})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		noErr(t, err)
	}
	final, err := a.GetBill(ctx, bill.SessionID)
	noErr(t, err)
	if len(final.Order.Items) != 1 || final.Order.Totals.Subtotal != 13800 {
		t.Fatalf("double tap produced %d lines, subtotal %d", len(final.Order.Items), final.Order.Totals.Subtotal)
	}
	// two tables opened at once on the same table: one session
	t2, _ := s.table(t, "B2")
	opened := 0
	var mu sync.Mutex
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.OpenTable(ctx, t2, 1, s.staff); err == nil {
				mu.Lock()
				opened++
				mu.Unlock()
			} else {
				wantCode(t, err, "TABLE_OCCUPIED")
			}
		}()
	}
	wg.Wait()
	if opened != 1 {
		t.Fatalf("table opened %d times", opened)
	}
}
