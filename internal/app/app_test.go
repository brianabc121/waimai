package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/db"
)

// These tests need a real PostgreSQL: set TEST_DATABASE_URL to an empty
// database. They are skipped otherwise.

var (
	testApp *app.App
	seq     atomic.Int64
)

func TestMain(m *testing.M) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		fmt.Println("TEST_DATABASE_URL not set; skipping database tests")
		os.Exit(m.Run())
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, url); err != nil {
		fmt.Println("migrate:", err)
		os.Exit(1)
	}
	pool, err := db.Connect(ctx, url)
	if err != nil {
		fmt.Println("connect:", err)
		os.Exit(1)
	}
	testApp = app.New(pool, "test-secret")
	testApp.OTPDevEcho = true
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

type store struct {
	slug                         string
	branchID                     int64
	zoneID                       int64
	chicken, tea, water, limited int64
	spiceMild                    int64
	rider1, rider2               int64
	staff                        app.Actor
}

func uniq(prefix string) string {
	return fmt.Sprintf("%s%d-%d", prefix, time.Now().UnixNano()%1e9, seq.Add(1))
}

func need(t *testing.T) *app.App {
	t.Helper()
	if testApp == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return testApp
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var ae *app.Error
	if !errors.As(err, &ae) || ae.Code != code {
		t.Fatalf("want error %s, got %v", code, err)
	}
}

// newStore creates a merchant with a small menu: chicken (needs prep, one
// required option group), tea (needs prep), water (no prep), limited (stock 1).
func newStore(t *testing.T, taxInclusive bool) *store {
	t.Helper()
	a := need(t)
	ctx := context.Background()
	s := &store{slug: uniq("s")}
	m, err := a.CreateMerchant(ctx, app.NewMerchant{MerchantName: "T", BranchName: "T", Slug: s.slug,
		Owner: app.NewUser{Username: uniq("owner"), Password: "password1", DisplayName: "Owner"}})
	noErr(t, err)
	s.branchID = m.BranchID
	_, err = a.SaveSettings(ctx, s.branchID, app.SettingsInput{Name: "T", TaxRateBP: 1200, TaxInclusive: taxInclusive,
		ServiceChargeBP: 500, PackagingFee: 1000, AcceptTimeoutMinutes: 10, DeliveryEnabled: true, PickupEnabled: true,
		OpenTime: "00:00", CloseTime: "00:00"})
	noErr(t, err)
	s.zoneID, err = a.SaveZone(ctx, s.branchID, 0, app.ZoneInput{Name: "Z", Fee: 6900, Active: true})
	noErr(t, err)
	cat, err := a.SaveCategory(ctx, s.branchID, 0, app.CategoryInput{Name: "C"})
	noErr(t, err)
	mk := func(in app.ItemInput) int64 {
		in.CategoryID, in.Published, in.DeliveryAvailable, in.PickupAvailable = cat, true, true, true
		id, err := a.SaveItem(ctx, s.branchID, 0, in)
		noErr(t, err)
		return id
	}
	s.chicken = mk(app.ItemInput{Name: "chicken", Price: 16900, NeedsPrep: true, Groups: []app.OptionGroupInput{
		{Name: "spice", MinSelect: 1, MaxSelect: 1, Options: []app.OptionInput{{Name: "mild"}, {Name: "hot", PriceDelta: 1000}}}}})
	s.tea = mk(app.ItemInput{Name: "tea", Price: 6900, NeedsPrep: true})
	s.water = mk(app.ItemInput{Name: "water", Price: 3000, NeedsPrep: false})
	one := 1
	s.limited = mk(app.ItemInput{Name: "limited", Price: 10000, NeedsPrep: true, StockQty: &one})
	menu, err := a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	for _, it := range menu.Categories[0].Items {
		if it.ID == s.chicken {
			s.spiceMild = it.Groups[0].Options[0].ID
		}
	}
	s.rider1, err = a.CreateStaff(ctx, s.branchID, app.NewUser{Username: uniq("r"), Password: "password1", DisplayName: "R1", Role: "rider"})
	noErr(t, err)
	s.rider2, err = a.CreateStaff(ctx, s.branchID, app.NewUser{Username: uniq("r"), Password: "password1", DisplayName: "R2", Role: "rider"})
	noErr(t, err)
	s.staff = app.Actor{Type: "staff", ID: 1}
	return s
}

func (s *store) cart() []app.QuoteLineInput {
	return []app.QuoteLineInput{
		{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}},
		{ItemID: s.tea, Qty: 1},
		{ItemID: s.water, Qty: 1},
	}
}

func (s *store) quote(t *testing.T, service string, lines []app.QuoteLineInput) *app.Quote {
	t.Helper()
	in := app.QuoteInput{ServiceType: service, Items: lines}
	if service == "delivery" {
		in.ZoneID = &s.zoneID
	}
	q, err := testApp.CreateQuote(context.Background(), s.slug, in)
	noErr(t, err)
	return q
}

func (s *store) place(t *testing.T, service string, lines []app.QuoteLineInput) *app.Order {
	t.Helper()
	q := s.quote(t, service, lines)
	p, err := testApp.PlaceOrder(context.Background(), s.slug, "+639170000001", uniq("k"),
		app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main St"})
	noErr(t, err)
	o, err := testApp.OrderByToken(context.Background(), p.AccessToken)
	noErr(t, err)
	return o
}

func TestComputeTotals(t *testing.T) {
	// subtotal 520.00, packaging 10.00, delivery 69.00, service 5%, tax 12%
	incl := app.ComputeTotals(52000, 6900, app.TaxConfig{TaxRateBP: 1200, TaxInclusive: true, ServiceChargeBP: 500, PackagingFee: 1000})
	if incl.ServiceCharge != 2600 {
		t.Fatalf("service charge = %d", incl.ServiceCharge)
	}
	// base 55600; inclusive tax = 55600*12/112 = 5957.14 -> 5957; total excludes tax
	if incl.TaxAmount != 5957 || incl.Total != 52000+1000+6900+2600 {
		t.Fatalf("inclusive: %+v", incl)
	}
	excl := app.ComputeTotals(52000, 6900, app.TaxConfig{TaxRateBP: 1200, TaxInclusive: false, ServiceChargeBP: 500, PackagingFee: 1000})
	if excl.TaxAmount != 6672 || excl.Total != 52000+1000+6900+2600+6672 {
		t.Fatalf("exclusive: %+v", excl)
	}
	zero := app.ComputeTotals(46000, 6900, app.TaxConfig{})
	if zero.Total != 52900 || zero.TaxAmount != 0 {
		t.Fatalf("zero config: %+v", zero)
	}
}

func TestDisplayStatus(t *testing.T) {
	cases := [][3]string{
		{"pending", "none", "pending_acceptance"},
		{"accepted", "none", "preparing"},
		{"accepted", "awaiting_handoff", "awaiting_rider"}, // packed is not "out for delivery"
		{"accepted", "out_for_delivery", "out_for_delivery"},
		{"accepted", "delivered", "delivered"},
		{"cancelled", "awaiting_handoff", "cancelled"},
		{"rejected", "none", "rejected"},
		{"accepted", "awaiting_pickup", "ready_for_pickup"},
	}
	for _, c := range cases {
		if got := app.DisplayStatus(c[0], c[1]); got != c[2] {
			t.Errorf("DisplayStatus(%s,%s)=%s want %s", c[0], c[1], got, c[2])
		}
	}
}

func TestQuoteValidation(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	// required option missing
	_, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s.chicken, Qty: 1}}})
	wantCode(t, err, "OPTIONS_INVALID")
	// delivery without a zone
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "delivery", Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}})
	wantCode(t, err, "OUT_OF_ZONE")
	// more than the limited stock
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s.limited, Qty: 2}}})
	wantCode(t, err, "ITEM_UNAVAILABLE")
	// item from another store
	other := newStore(t, true)
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: other.tea, Qty: 1}}})
	wantCode(t, err, "ITEM_UNAVAILABLE")

	q := s.quote(t, "delivery", s.cart())
	// 2*16900 + 6900 + 3000 = 43700
	if q.Totals.Subtotal != 43700 || q.Totals.DeliveryFee != 6900 || q.Totals.PackagingFee != 1000 {
		t.Fatalf("totals: %+v", q.Totals)
	}
	if p := s.quote(t, "pickup", s.cart()); p.Totals.DeliveryFee != 0 {
		t.Fatalf("pickup must not charge delivery: %+v", p.Totals)
	}
}

func TestQuoteChangedAndReuse(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	q := s.quote(t, "delivery", s.cart())
	// the delivery fee changes after the quote was shown
	_, err := testApp.SaveZone(ctx, s.branchID, s.zoneID, app.ZoneInput{Name: "Z", Fee: 9900, Active: true})
	noErr(t, err)
	in := app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main St"}
	_, err = testApp.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), in)
	wantCode(t, err, "QUOTE_CHANGED")

	q2 := s.quote(t, "delivery", s.cart())
	in.QuoteID = q2.ID
	_, err = testApp.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), in)
	noErr(t, err)
	// one quote, one order
	_, err = testApp.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), in)
	wantCode(t, err, "QUOTE_USED")
}

func TestPlaceOrderIdempotentUnderConcurrency(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	q := s.quote(t, "delivery", s.cart())
	in := app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main St"}
	key := uniq("k")
	var wg sync.WaitGroup
	results := make([]*app.PlacedOrder, 12)
	errs := make([]error, 12)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = testApp.PlaceOrder(ctx, s.slug, "+639170000001", key, in)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		noErr(t, err)
		if results[i].OrderID != results[0].OrderID || results[i].AccessToken != results[0].AccessToken {
			t.Fatalf("retry %d returned a different order", i)
		}
	}
	orders, err := testApp.ListOrders(ctx, s.branchID, "active")
	noErr(t, err)
	if len(orders) != 1 {
		t.Fatalf("want exactly 1 order, got %d", len(orders))
	}
	// same key, different body
	in.CustomerName = "Bob"
	_, err = testApp.PlaceOrder(ctx, s.slug, "+639170000001", key, in)
	wantCode(t, err, "IDEMPOTENCY_CONFLICT")
}

func TestLimitedStockUnderConcurrency(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	line := []app.QuoteLineInput{{ItemID: s.limited, Qty: 1}}
	const n = 8
	quotes := make([]*app.Quote, n)
	for i := range quotes {
		quotes[i] = s.quote(t, "pickup", line)
	}
	var wg sync.WaitGroup
	var ok, soldOut atomic.Int32
	tokens := make([]string, n)
	for i := range quotes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := testApp.PlaceOrder(ctx, s.slug, fmt.Sprintf("+63917000%04d", i), uniq("k"),
				app.PlaceOrderInput{QuoteID: quotes[i].ID, CustomerName: "Ann"})
			var ae *app.Error
			switch {
			case err == nil:
				ok.Add(1)
				tokens[i] = p.AccessToken
			case errors.As(err, &ae) && ae.Code == "ITEM_UNAVAILABLE":
				soldOut.Add(1)
			default:
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || soldOut.Load() != n-1 {
		t.Fatalf("stock 1: %d succeeded, %d sold out", ok.Load(), soldOut.Load())
	}
	// cancelling returns the portion
	for _, tok := range tokens {
		if tok != "" {
			_, err := testApp.CustomerCancel(ctx, tok)
			noErr(t, err)
		}
	}
	s.place(t, "pickup", line)
}

func TestConcurrentAcceptOnlyOnce(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	o := s.place(t, "delivery", s.cart())
	var wg sync.WaitGroup
	var ok, conflict atomic.Int32
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := testApp.Accept(ctx, o.ID, o.Version, s.staff)
			var ae *app.Error
			switch {
			case err == nil:
				ok.Add(1)
			case errors.As(err, &ae) && (ae.Code == "INVALID_STATE" || ae.Code == "VERSION_CONFLICT"):
				conflict.Add(1)
			default:
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != 5 {
		t.Fatalf("accept: %d ok, %d conflicts", ok.Load(), conflict.Load())
	}
	// a customer can no longer cancel an accepted order
	_, err := testApp.CustomerCancel(ctx, "nope")
	wantCode(t, err, "NOT_FOUND")
}

func itemByName(o *app.Order, name string) app.OrderItem {
	for _, it := range o.Items {
		if it.Name == name {
			return it
		}
	}
	panic("no item " + name)
}

func TestDeliveryFlow(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	o := s.place(t, "delivery", s.cart())
	if o.DisplayStatus != "pending_acceptance" || o.PaymentMethod != "cod" {
		t.Fatalf("new order: %s %s", o.DisplayStatus, o.PaymentMethod)
	}
	// nothing can be packed before acceptance
	_, err := a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	wantCode(t, err, "INVALID_STATE")

	o, err = a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	if o.PackingStatus != "waiting_items" || itemByName(o, "water").PrepStatus != "ready" {
		t.Fatalf("after accept: packing=%s water=%s", o.PackingStatus, itemByName(o, "water").PrepStatus)
	}
	chicken, tea := itemByName(o, "chicken"), itemByName(o, "tea")

	// tea not ready yet: packing is blocked
	o, err = a.SetItemStatus(ctx, o.ID, chicken.ID, "ready", s.staff)
	noErr(t, err)
	_, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	wantCode(t, err, "ITEMS_NOT_READY")

	o, err = a.SetItemStatus(ctx, o.ID, tea.ID, "start", s.staff)
	noErr(t, err)
	o, err = a.SetItemStatus(ctx, o.ID, tea.ID, "ready", s.staff)
	noErr(t, err)
	if o.PackingStatus != "ready_to_pack" {
		t.Fatalf("packing=%s", o.PackingStatus)
	}
	staleManifest := o.ManifestVersion

	// undo invalidates the list the packer was looking at
	o, err = a.SetItemStatus(ctx, o.ID, tea.ID, "undo", s.staff)
	noErr(t, err)
	if o.PackingStatus != "waiting_items" {
		t.Fatalf("after undo packing=%s", o.PackingStatus)
	}
	o, err = a.SetItemStatus(ctx, o.ID, tea.ID, "ready", s.staff)
	noErr(t, err)
	_, err = a.Pack(ctx, o.ID, 1, staleManifest, s.staff)
	wantCode(t, err, "MANIFEST_CHANGED")

	// handoff needs packing first
	_, err = a.StaffHandoff(ctx, o.ID, s.staff)
	wantCode(t, err, "ITEMS_NOT_READY")

	o, err = a.Pack(ctx, o.ID, 2, o.ManifestVersion, s.staff)
	noErr(t, err)
	if o.DisplayStatus != "awaiting_rider" || *o.Bags != 2 {
		t.Fatalf("after pack: %s", o.DisplayStatus)
	}
	// packed items cannot be un-readied
	_, err = a.SetItemStatus(ctx, o.ID, tea.ID, "undo", s.staff)
	wantCode(t, err, "INVALID_STATE")
	// handoff needs a rider
	_, err = a.StaffHandoff(ctx, o.ID, s.staff)
	wantCode(t, err, "INVALID_STATE")

	// assign rider1, then reassign to rider2: rider1 is locked out
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	task := o.Delivery.ID
	o, err = a.AssignRider(ctx, o.ID, s.rider2, s.staff)
	noErr(t, err)
	if o.Delivery.AssignmentVersion != 2 {
		t.Fatalf("assignment version = %d", o.Delivery.AssignmentVersion)
	}
	_, err = a.RiderPickup(ctx, task, s.rider1)
	wantCode(t, err, "NOT_ASSIGNED")
	if tasks, _ := a.RiderTasks(ctx, s.rider1); len(tasks) != 0 {
		t.Fatalf("old rider still sees %d tasks", len(tasks))
	}

	// both sides confirm the handoff; it advances once
	o, err = a.RiderPickup(ctx, task, s.rider2)
	noErr(t, err)
	v := o.Version
	o, err = a.StaffHandoff(ctx, o.ID, s.staff)
	noErr(t, err)
	if o.DisplayStatus != "out_for_delivery" || o.Version != v {
		t.Fatalf("second handoff changed the order: %s v%d->%d", o.DisplayStatus, v, o.Version)
	}
	// the store cannot cancel once the order left
	_, err = a.StaffCancel(ctx, o.ID, 0, "late", s.staff)
	wantCode(t, err, "INVALID_STATE")

	o, err = a.RiderArrive(ctx, task, s.rider2)
	noErr(t, err)
	if o.DisplayStatus != "rider_arrived" || o.PaymentStatus != "unpaid" {
		t.Fatalf("arrived: %s %s", o.DisplayStatus, o.PaymentStatus)
	}

	// deliver twice concurrently-ish: one cash record
	o, err = a.RiderDeliver(ctx, task, s.rider2, o.Totals.Total)
	noErr(t, err)
	o, err = a.RiderDeliver(ctx, task, s.rider2, 1)
	noErr(t, err)
	if o.DisplayStatus != "delivered" || o.PaymentStatus != "paid" || o.Cash.AmountCollected != o.Totals.Total {
		t.Fatalf("delivered: %s %s %+v", o.DisplayStatus, o.PaymentStatus, o.Cash)
	}
	cash, err := a.ListCash(ctx, s.branchID)
	noErr(t, err)
	if len(cash) != 1 || cash[0].ReconcileStatus != "pending" {
		t.Fatalf("cash collections: %+v", cash)
	}
	// rider hands back less than collected
	c, err := a.Reconcile(ctx, cash[0].ID, cash[0].AmountCollected-500, "short", s.staff)
	noErr(t, err)
	if c.ReconcileStatus != "discrepancy" {
		t.Fatalf("reconcile status = %s", c.ReconcileStatus)
	}
	_, err = a.Reconcile(ctx, cash[0].ID, cash[0].AmountCollected, "", s.staff)
	wantCode(t, err, "INVALID_STATE")
}

func TestShortCollectionStaysUnpaid(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	o := s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	o, err := a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff) // water needs no prep
	noErr(t, err)
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	_, err = a.RiderDeliver(ctx, o.Delivery.ID, s.rider1, 0)
	wantCode(t, err, "INVALID_STATE") // not picked up yet
	_, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider1)
	noErr(t, err)
	o, err = a.RiderDeliver(ctx, o.Delivery.ID, s.rider1, o.Totals.Total-100)
	noErr(t, err)
	if o.PaymentStatus != "unpaid" || o.Cash.AmountDue-o.Cash.AmountCollected != 100 {
		t.Fatalf("short collection: %s %+v", o.PaymentStatus, o.Cash)
	}
}

func TestPickupFlowAndCancel(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	if o.PaymentMethod != "pay_at_pickup" || o.Totals.DeliveryFee != 0 || o.Totals.TaxInclusive {
		t.Fatalf("pickup order: %+v", o.Totals)
	}
	o, err := a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	_, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	wantCode(t, err, "INVALID_STATE")
	o, err = a.SetItemStatus(ctx, o.ID, o.Items[0].ID, "ready", s.staff)
	noErr(t, err)
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	if o.DisplayStatus != "ready_for_pickup" {
		t.Fatalf("status %s", o.DisplayStatus)
	}
	o, err = a.StaffHandoff(ctx, o.ID, s.staff)
	noErr(t, err)
	if o.DisplayStatus != "picked_up" || o.PaymentStatus != "paid" {
		t.Fatalf("picked up: %s %s", o.DisplayStatus, o.PaymentStatus)
	}

	// cancel after acceptance: kitchen keeps seeing it as cancelled
	o2 := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	_, err = a.Reject(ctx, o2.ID, 0, "", s.staff)
	wantCode(t, err, "VALIDATION")
	o2, err = a.Accept(ctx, o2.ID, 0, s.staff)
	noErr(t, err)
	o2, err = a.StaffCancel(ctx, o2.ID, o2.Version, "out of tea", s.staff)
	noErr(t, err)
	if o2.DisplayStatus != "cancelled" || o2.Items[0].PrepStatus != "cancelled" {
		t.Fatalf("cancelled: %s %s", o2.DisplayStatus, o2.Items[0].PrepStatus)
	}
	board, err := a.KitchenBoard(ctx, s.branchID)
	noErr(t, err)
	found := false
	for _, b := range board {
		if b.ID == o2.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("recently cancelled order should stay on the kitchen board as a notice")
	}
	_, err = a.Accept(ctx, o2.ID, o2.Version, s.staff)
	wantCode(t, err, "INVALID_STATE")
}

func TestAcceptTimeout(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.limited, Qty: 1}})
	fresh := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	_, err := a.DB.Exec(ctx, `UPDATE orders SET accept_deadline = now() - interval '1 minute' WHERE id=$1`, o.ID)
	noErr(t, err)
	_, err = a.ExpirePendingOrders(ctx)
	noErr(t, err)
	o, err = a.GetOrder(ctx, o.ID)
	noErr(t, err)
	if o.DisplayStatus != "cancelled" || o.CloseReason != "accept_timeout" {
		t.Fatalf("expired order: %s %s", o.DisplayStatus, o.CloseReason)
	}
	fresh, err = a.GetOrder(ctx, fresh.ID)
	noErr(t, err)
	if fresh.AcceptanceStatus != "pending" {
		t.Fatal("an order inside its deadline must stay pending")
	}
	// the limited portion came back
	s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.limited, Qty: 1}})
}

func TestOTP(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	phone := fmt.Sprintf("+6391%08d", time.Now().UnixNano()%1e8)
	code, err := a.RequestOTP(ctx, phone)
	noErr(t, err)
	_, err = a.RequestOTP(ctx, phone)
	wantCode(t, err, "RATE_LIMITED")
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}
	for i := 0; i < 5; i++ {
		_, _, err = a.VerifyOTP(ctx, phone, wrong)
		wantCode(t, err, "OTP_INVALID")
	}
	// five wrong attempts void the code, even the right one
	_, _, err = a.VerifyOTP(ctx, phone, code)
	wantCode(t, err, "OTP_INVALID")

	phone2 := phone + "1"
	code, err = a.RequestOTP(ctx, phone2)
	noErr(t, err)
	token, _, err := a.VerifyOTP(ctx, phone2, code)
	noErr(t, err)
	got, err := a.CustomerPhone(ctx, token)
	noErr(t, err)
	if got != phone2 {
		t.Fatalf("phone %s", got)
	}
	// a code works once
	_, _, err = a.VerifyOTP(ctx, phone2, code)
	wantCode(t, err, "OTP_INVALID")
	_, err = a.CustomerPhone(ctx, "bogus")
	wantCode(t, err, "PHONE_NOT_VERIFIED")
}
