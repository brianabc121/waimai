package app_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// fakeSMS records messages and can be told to fail.
type fakeSMS struct {
	mu   sync.Mutex
	sent []string
	fail int // number of sends that should still fail
}

func (f *fakeSMS) Send(_ context.Context, phone, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail > 0 {
		f.fail--
		return errors.New("provider down")
	}
	f.sent = append(f.sent, phone+"|"+msg)
	return nil
}

func (f *fakeSMS) forPhone(phone string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, s := range f.sent {
		if strings.HasPrefix(s, phone+"|") {
			out = append(out, s)
		}
	}
	return out
}

func TestStationsDispatchAndMove(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	fryer, err := a.SaveStation(ctx, s.branchID, 0, app.StationInput{Name: "炸台", NameEn: "Fryer", Active: true})
	noErr(t, err)
	drinks, err := a.SaveStation(ctx, s.branchID, 0, app.StationInput{Name: "饮品台", Active: true})
	noErr(t, err)
	_, err = a.SaveStation(ctx, s.branchID, 0, app.StationInput{Name: "炸台", Active: true})
	wantCode(t, err, "STATION_NAME_TAKEN")

	n, err := a.AssignStation(ctx, s.branchID, []int64{s.chicken}, &fryer)
	noErr(t, err)
	if n != 1 {
		t.Fatalf("assigned %d", n)
	}
	// a station of another store cannot be used
	other := newStore(t, true)
	_, err = a.AssignStation(ctx, other.branchID, []int64{other.tea}, &fryer)
	wantCode(t, err, "VALIDATION")
	// an item id of another store is silently not touched
	n, err = a.AssignStation(ctx, s.branchID, []int64{other.tea}, &fryer)
	noErr(t, err)
	if n != 0 {
		t.Fatal("must not assign another store's item")
	}

	o := s.place(t, "pickup", s.cart()) // chicken -> fryer, tea unassigned, water no prep
	o, err = a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	chicken, tea, water := itemByName(o, "chicken"), itemByName(o, "tea"), itemByName(o, "water")
	if chicken.StationID == nil || *chicken.StationID != fryer || chicken.StationName != "炸台" {
		t.Fatalf("chicken station: %+v", chicken)
	}
	if tea.StationID != nil || water.StationID != nil {
		t.Fatal("tea is unassigned and water needs no prep: neither has a station")
	}
	// the unassigned item still blocks packing until someone handles it
	o, err = a.SetItemStatus(ctx, o.ID, chicken.ID, "ready", s.staff)
	noErr(t, err)
	if o.PackingStatus != "waiting_items" {
		t.Fatalf("unassigned item must block packing, got %s", o.PackingStatus)
	}

	// routing changes only affect new orders
	_, err = a.AssignStation(ctx, s.branchID, []int64{s.tea}, &drinks)
	noErr(t, err)
	o, err = a.GetOrder(ctx, o.ID)
	noErr(t, err)
	if itemByName(o, "tea").StationID != nil {
		t.Fatal("an order already in the kitchen must keep its dispatch")
	}
	// explicit, audited move of an in-progress item
	o, err = a.MoveItemStation(ctx, o.ID, tea.ID, &drinks, "补分", s.staff)
	noErr(t, err)
	if got := itemByName(o, "tea"); got.StationID == nil || *got.StationID != drinks {
		t.Fatalf("tea after move: %+v", got)
	}
	_, err = a.MoveItemStation(ctx, o.ID, chicken.ID, &drinks, "", s.staff) // already ready
	wantCode(t, err, "INVALID_STATE")

	// deactivating: no new dispatch, existing work stays and can be finished
	_, err = a.SaveStation(ctx, s.branchID, drinks, app.StationInput{Name: "饮品台", Active: false})
	noErr(t, err)
	stations, err := a.ListStations(ctx, s.branchID)
	noErr(t, err)
	for _, st := range stations {
		if st.ID == drinks && (st.Active || st.OpenTasks != 1) {
			t.Fatalf("inactive station should still show its open task: %+v", st)
		}
	}
	_, err = a.MoveItemStation(ctx, o.ID, tea.ID, &drinks, "", s.staff)
	wantCode(t, err, "VALIDATION")
	o, err = a.SetItemStatus(ctx, o.ID, tea.ID, "ready", s.staff)
	noErr(t, err)
	if o.PackingStatus != "ready_to_pack" {
		t.Fatalf("packing=%s", o.PackingStatus)
	}
	o2 := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	o2, err = a.Accept(ctx, o2.ID, 0, s.staff)
	noErr(t, err)
	if o2.Items[0].StationID != nil {
		t.Fatal("an inactive station must not receive new work")
	}
}

func TestFailedDelivery(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	carry := func() *app.Order {
		o := s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
		o, err := a.Accept(ctx, o.ID, 0, s.staff)
		noErr(t, err)
		o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
		noErr(t, err)
		o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
		noErr(t, err)
		_, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider1)
		noErr(t, err)
		o, err = a.RiderFail(ctx, o.Delivery.ID, s.rider1, "no answer")
		noErr(t, err)
		if o.DisplayStatus != "delivery_failed" {
			t.Fatalf("status %s", o.DisplayStatus)
		}
		return o
	}

	o := carry()
	_, err := a.Redeliver(ctx, o.ID, 999999, s.staff)
	wantCode(t, err, "VALIDATION")
	o, err = a.Redeliver(ctx, o.ID, s.rider2, s.staff)
	noErr(t, err)
	if o.DisplayStatus != "awaiting_rider" || o.Delivery.RiderID != s.rider2 || o.Delivery.Status != "assigned" {
		t.Fatalf("after redeliver: %s %+v", o.DisplayStatus, o.Delivery)
	}
	_, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider1)
	wantCode(t, err, "NOT_ASSIGNED")
	_, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider2)
	noErr(t, err)
	o, err = a.RiderDeliver(ctx, o.Delivery.ID, s.rider2, o.Totals.Total)
	noErr(t, err)
	if o.DisplayStatus != "delivered" || o.PaymentStatus != "paid" {
		t.Fatalf("second attempt: %s %s", o.DisplayStatus, o.PaymentStatus)
	}
	_, err = a.Redeliver(ctx, o.ID, s.rider1, s.staff)
	wantCode(t, err, "INVALID_STATE")

	o = carry()
	_, err = a.CloseFailed(ctx, o.ID, " ", s.staff)
	wantCode(t, err, "VALIDATION")
	o, err = a.CloseFailed(ctx, o.ID, "customer unreachable", s.staff)
	noErr(t, err)
	if o.DisplayStatus != "cancelled" || o.PaymentStatus != "unpaid" || o.Cash != nil {
		t.Fatalf("closed: %s %s %+v", o.DisplayStatus, o.PaymentStatus, o.Cash)
	}
	active, err := a.ListOrders(ctx, s.branchID, "active")
	noErr(t, err)
	if len(active) != 0 {
		t.Fatalf("%d orders still active", len(active))
	}
}

func TestCustomerNotificationsAndRetry(t *testing.T) {
	s := newStore(t, true)
	ctx := context.Background()
	a := testApp
	fake := &fakeSMS{}
	old, oldBase := a.SMS, a.PublicBaseURL
	a.SMS, a.PublicBaseURL = fake, "https://example.test"
	t.Cleanup(func() { a.SMS, a.PublicBaseURL = old, oldBase })
	phone := "+639170000001"
	// jobs left behind by an earlier, aborted run would be sent to the fake
	_, err0 := a.DB.Exec(ctx, `DELETE FROM jobs WHERE status='pending'`)
	noErr(t, err0)

	// off by default: nothing is queued
	o := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	_, err := a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	if n := len(fake.forPhone(phone)); n != 0 {
		t.Fatalf("notifications are off, but %d were sent", n)
	}

	_, err = a.SaveSettings(ctx, s.branchID, app.SettingsInput{Name: "T", TaxRateBP: 1200, TaxInclusive: true,
		AcceptTimeoutMinutes: 10, DeliveryEnabled: true, PickupEnabled: true, OpenTime: "00:00", CloseTime: "00:00", NotifySMS: true})
	noErr(t, err)
	o = s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	fake.mu.Lock()
	fake.fail = 1 // the provider is down for the first attempt
	fake.mu.Unlock()
	o, err = a.Accept(ctx, o.ID, 0, s.staff)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	if n := len(fake.forPhone(phone)); n != 0 {
		t.Fatalf("first attempt failed, yet %d sent", n)
	}
	var status string
	var attempts int
	noErr(t, a.DB.QueryRow(ctx, `SELECT status, attempts FROM jobs WHERE payload->>'message' LIKE '%' || $1 || '%'
		ORDER BY id DESC LIMIT 1`, // order numbers repeat across stores and test runs
		o.PublicNo+" 商家已接单").Scan(&status, &attempts))
	if status != "pending" || attempts != 1 {
		t.Fatalf("failed job should be pending for retry: %s attempts=%d", status, attempts)
	}
	// make the retry due now
	_, err = a.DB.Exec(ctx, `UPDATE jobs SET run_at = now() WHERE status='pending'`)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	sent := fake.forPhone(phone)
	if len(sent) != 1 || !strings.Contains(sent[0], "商家已接单") || !strings.Contains(sent[0], "https://example.test/o/") {
		t.Fatalf("after retry: %v", sent)
	}
	// running again sends nothing twice
	noErr(t, a.RunJobs(ctx))
	if n := len(fake.forPhone(phone)); n != 1 {
		t.Fatalf("job ran twice: %d", n)
	}

	// a rolled-back change sends nothing: reject without a reason fails validation
	o2 := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	_, err = a.Reject(ctx, o2.ID, 0, "", s.staff)
	wantCode(t, err, "VALIDATION")
	_, err = a.Reject(ctx, o2.ID, 0, "closed early", s.staff)
	noErr(t, err)
	// the customer's own cancel does not text them
	o3 := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	_ = o3
	noErr(t, a.RunJobs(ctx))
	sent = fake.forPhone(phone)
	if len(sent) != 2 || !strings.Contains(sent[1], "商家未能接单") {
		t.Fatalf("after reject: %v", sent)
	}
}
