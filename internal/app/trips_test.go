package app_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

type tripRow struct {
	id                        int64
	status, reason            string
	started                   time.Time
	left, lastDrop, returned  *time.Time
	ended, refine             *time.Time
	orders, delivered, failed int
	distance                  *int
	path, gaps                []byte
	points, gapS              int
	shift                     *int64
}

func tripsOf(t *testing.T, riderID int64) []tripRow {
	t.Helper()
	rows, err := testApp.DB.Query(context.Background(), `SELECT id, status, end_reason, started_at, left_store_at, last_drop_at,
		returned_at, ended_at, refine_at, order_count, delivered_count, failed_count, actual_distance_m, actual_path,
		track_gaps, track_points, track_gap_s, shift_id FROM rider_trips WHERE rider_id=$1 ORDER BY id`, riderID)
	noErr(t, err)
	defer rows.Close()
	var out []tripRow
	for rows.Next() {
		var r tripRow
		noErr(t, rows.Scan(&r.id, &r.status, &r.reason, &r.started, &r.left, &r.lastDrop, &r.returned, &r.ended, &r.refine,
			&r.orders, &r.delivered, &r.failed, &r.distance, &r.path, &r.gaps, &r.points, &r.gapS, &r.shift))
		out = append(out, r)
	}
	noErr(t, rows.Err())
	return out
}

// packed takes a delivery order to "waiting for the rider" with rider assigned.
func (s *store) packed(t *testing.T, rider int64) *app.Order {
	t.Helper()
	ctx := context.Background()
	a := testApp
	o := s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	o, err := a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
	noErr(t, err)
	o, err = a.AssignRider(ctx, o.ID, rider, s.staff)
	noErr(t, err)
	return o
}

func TestRiderTrips(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	const lat, lng = 14.55, 121.02
	_, err := a.DB.Exec(ctx, `UPDATE branches SET lat=$2, lng=$3 WHERE id=$1`, s.branchID, lat, lng)
	noErr(t, err)
	var t0 time.Time
	fix := func(rider int64, after time.Duration, dLat, dLng float64) {
		t.Helper()
		_, err := a.DB.Exec(ctx, `INSERT INTO rider_tracks (user_id, lat, lng, accuracy, at) VALUES ($1,$2,$3,10,$4)`,
			rider, lat+dLat, lng+dLng, t0.Add(after))
		noErr(t, err)
	}
	advance := func(after time.Duration) {
		t.Helper()
		_, err := a.AdvanceTripsAt(ctx, t0.Add(after))
		noErr(t, err)
	}
	// The test moves the clock by hand: say when the last order was dropped.
	droppedAt := func(tripID int64, after time.Duration) {
		t.Helper()
		_, err := a.DB.Exec(ctx, `UPDATE rider_trips SET last_drop_at=$2 WHERE id=$1`, tripID, t0.Add(after))
		noErr(t, err)
	}
	one := func(rider int64) tripRow {
		t.Helper()
		tr := tripsOf(t, rider)
		if len(tr) == 0 {
			t.Fatal("no trip")
		}
		return tr[len(tr)-1]
	}

	// ---- two orders collected at the store ride on one trip ----
	o1, o2 := s.packed(t, s.rider1), s.packed(t, s.rider1)
	_, err = a.RiderPickup(ctx, o1.Delivery.ID, s.rider1)
	noErr(t, err)
	_, err = a.StaffHandoff(ctx, o2.ID, s.staff) // the counter hands over the second one
	noErr(t, err)
	tr := one(s.rider1)
	t0 = tr.started
	if len(tripsOf(t, s.rider1)) != 1 || tr.status != "loading" || tr.orders != 2 {
		t.Fatalf("after collecting: %+v", tr)
	}

	// still in front of the store
	fix(s.rider1, 30*time.Second, 0.0001, 0)
	advance(time.Minute)
	if tr = one(s.rider1); tr.status != "loading" || tr.left != nil {
		t.Fatalf("at the store: %+v", tr)
	}
	// 220 m away: they left
	fix(s.rider1, 2*time.Minute, 0.002, 0)
	advance(3 * time.Minute)
	if tr = one(s.rider1); tr.status != "out" || tr.left == nil || !tr.left.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("after leaving: %+v", tr)
	}
	if tr.shift == nil {
		t.Fatal("the trip should belong to the shift that collecting the order started")
	}

	fix(s.rider1, 3*time.Minute, 0.005, 0)
	fix(s.rider1, 4*time.Minute, 0.009, 0)
	_, err = a.RiderDeliver(ctx, o1.Delivery.ID, s.rider1, o1.Totals.Total)
	noErr(t, err)
	if tr = one(s.rider1); tr.delivered != 1 || tr.lastDrop != nil {
		t.Fatalf("one dropped, one to go: %+v", tr)
	}
	fix(s.rider1, 6*time.Minute, 0.009, 0.005)
	_, err = a.RiderFail(ctx, o2.Delivery.ID, s.rider1, "nobody home")
	noErr(t, err)
	if tr = one(s.rider1); tr.delivered != 1 || tr.failed != 1 || tr.lastDrop == nil || tr.status != "out" {
		t.Fatalf("both dropped: %+v", tr)
	}
	var seq1, seq2 int
	noErr(t, a.DB.QueryRow(ctx, `SELECT drop_seq FROM rider_trip_orders WHERE order_id=$1`, o1.ID).Scan(&seq1))
	noErr(t, a.DB.QueryRow(ctx, `SELECT drop_seq FROM rider_trip_orders WHERE order_id=$1`, o2.ID).Scan(&seq2))
	if seq1 != 1 || seq2 != 2 {
		t.Fatalf("drop order: %d %d", seq1, seq2)
	}
	droppedAt(tr.id, 6*time.Minute+30*time.Second)

	// on the way back, not there yet
	advance(7 * time.Minute)
	if tr = one(s.rider1); tr.status != "out" || tr.distance == nil || *tr.distance < 1400 || *tr.distance > 1600 {
		t.Fatalf("on the way back: %+v", tr)
	}
	// no signal for six minutes, then back within 100 m of the store
	fix(s.rider1, 12*time.Minute, 0.003, 0.001)
	fix(s.rider1, 13*time.Minute, 0.0005, 0)
	fix(s.rider1, 14*time.Minute, 0.0001, 0) // already inside: not part of the trip
	advance(14*time.Minute + 30*time.Second)
	tr = one(s.rider1)
	if tr.status != "done" || tr.reason != "returned" || tr.returned == nil || !tr.returned.Equal(t0.Add(13*time.Minute)) ||
		!tr.ended.Equal(*tr.returned) {
		t.Fatalf("back at the store: %+v", tr)
	}
	if *tr.distance < 2500 || *tr.distance > 2750 || tr.gapS != 360 {
		t.Fatalf("distance %d, gap %d s", *tr.distance, tr.gapS)
	}
	var path [][3]float64
	var gaps []int
	noErr(t, json.Unmarshal(tr.path, &path))
	noErr(t, json.Unmarshal(tr.gaps, &gaps))
	if len(gaps) != 1 || len(path) < 4 || path[len(path)-1][2] != 780 {
		t.Fatalf("path %v gaps %v", path, gaps)
	}
	if tr.refine == nil || !tr.refine.Equal(t0.Add(28*time.Minute)) {
		t.Fatalf("refine at %v", tr.refine)
	}

	// The phone sends the fixes it could not send before: worked out once more.
	fix(s.rider1, 8*time.Minute, 0.007, 0.004)
	fix(s.rider1, 10*time.Minute, 0.005, 0.0025)
	advance(20 * time.Minute)
	if got := one(s.rider1); got.points != tr.points {
		t.Fatal("refined too early")
	}
	advance(29 * time.Minute)
	got := one(s.rider1)
	if got.refine != nil || got.points != tr.points+2 || got.gapS != 0 {
		t.Fatalf("refined: %+v", got)
	}
	advance(30 * time.Minute)

	// ---- the next order starts the next trip ----
	o3 := s.packed(t, s.rider1)
	_, err = a.RiderPickup(ctx, o3.Delivery.ID, s.rider1)
	noErr(t, err)
	all := tripsOf(t, s.rider1)
	if len(all) != 2 || all[1].status != "loading" || all[1].orders != 1 {
		t.Fatalf("second trip: %+v", all)
	}
	// delivered, and the rider clocks off without coming back
	_, err = a.RiderDeliver(ctx, o3.Delivery.ID, s.rider1, o3.Totals.Total)
	noErr(t, err)
	_, err = a.SetDuty(ctx, s.rider1, false, "")
	noErr(t, err)
	if tr = one(s.rider1); tr.status != "done" || tr.reason != "off_duty" || tr.returned != nil || tr.delivered != 1 {
		t.Fatalf("clocked off on the road: %+v", tr)
	}

	// ---- a trip that was not seen coming back is closed by the next pickup ----
	t0 = time.Now()
	o4 := s.packed(t, s.rider2)
	_, err = a.RiderPickup(ctx, o4.Delivery.ID, s.rider2)
	noErr(t, err)
	// no positions at all: ten minutes after collecting they are taken to have left
	advance(5 * time.Minute)
	if tr = one(s.rider2); tr.status != "loading" {
		t.Fatalf("no positions, 5 minutes: %+v", tr)
	}
	advance(11 * time.Minute)
	if tr = one(s.rider2); tr.status != "out" || tr.left != nil {
		t.Fatalf("no positions, 11 minutes: %+v", tr)
	}
	_, err = a.RiderDeliver(ctx, o4.Delivery.ID, s.rider2, o4.Totals.Total)
	noErr(t, err)
	o5 := s.packed(t, s.rider2)
	_, err = a.RiderPickup(ctx, o5.Delivery.ID, s.rider2)
	noErr(t, err)
	all = tripsOf(t, s.rider2)
	if len(all) != 2 || all[0].status != "done" || all[0].reason != "next_trip" || all[0].returned == nil || all[0].distance != nil {
		t.Fatalf("closed by the next pickup: %+v", all)
	}
	// delivered and never heard of again: given up after half an hour
	_, err = a.RiderDeliver(ctx, o5.Delivery.ID, s.rider2, o5.Totals.Total)
	noErr(t, err)
	tr = one(s.rider2)
	t0 = *tr.lastDrop
	advance(20 * time.Minute)
	if tr = one(s.rider2); tr.status == "done" {
		t.Fatalf("given up too early: %+v", tr)
	}
	advance(31 * time.Minute)
	if tr = one(s.rider2); tr.status != "done" || tr.reason != "timeout" || tr.returned != nil {
		t.Fatalf("never came back: %+v", tr)
	}

	// ---- a failed order that goes out again is on both trips ----
	_, err = a.Redeliver(ctx, o2.ID, s.rider2, s.staff)
	noErr(t, err)
	_, err = a.RiderPickup(ctx, o2.Delivery.ID, s.rider2)
	noErr(t, err)
	var n int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(DISTINCT trip_id) FROM rider_trip_orders WHERE order_id=$1`, o2.ID).Scan(&n))
	if n != 2 {
		t.Fatalf("the order is on %d trips", n)
	}

	// ---- a store without a position: trips and orders are counted, nothing else ----
	_, err = a.DB.Exec(ctx, `UPDATE branches SET lat=NULL, lng=NULL WHERE id=$1`, s.branchID)
	noErr(t, err)
	_, err = a.RiderDeliver(ctx, o2.Delivery.ID, s.rider2, o2.Totals.Total)
	noErr(t, err)
	tr = one(s.rider2)
	t0 = *tr.lastDrop
	advance(time.Minute)
	if tr = one(s.rider2); tr.status != "done" || tr.reason != "no_store" || tr.delivered != 1 {
		t.Fatalf("store without a position: %+v", tr)
	}
}

func TestRiderReport(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	const lat, lng = 14.55, 121.02
	_, err := a.DB.Exec(ctx, `UPDATE branches SET lat=$2, lng=$3 WHERE id=$1`, s.branchID, lat, lng)
	noErr(t, err)
	at := func(o *app.Order, dLat, dLng float64) {
		t.Helper()
		_, err := a.DB.Exec(ctx, `UPDATE orders SET dest_lat=$2, dest_lng=$3 WHERE id=$1`, o.ID, lat+dLat, lng+dLng)
		noErr(t, err)
	}
	// Two orders: one 1 km north, one 2 km north. The best round is 4 km
	// whichever comes first; the rider goes far first, with a detour, and rides 5.7 km.
	near, far := s.packed(t, s.rider1), s.packed(t, s.rider1)
	at(near, 0.009, 0)
	at(far, 0.018, 0)
	_, err = a.RiderPickup(ctx, near.Delivery.ID, s.rider1)
	noErr(t, err)
	_, err = a.RiderPickup(ctx, far.Delivery.ID, s.rider1)
	noErr(t, err)
	tr := tripsOf(t, s.rider1)[0]
	t0 := tr.started
	fix := func(after time.Duration, dLat, dLng float64) {
		t.Helper()
		_, err := a.DB.Exec(ctx, `INSERT INTO rider_tracks (user_id, lat, lng, accuracy, at) VALUES ($1,$2,$3,10,$4)`,
			s.rider1, lat+dLat, lng+dLng, t0.Add(after))
		noErr(t, err)
	}
	fix(1*time.Minute, 0.004, 0)
	_, err = a.AdvanceTripsAt(ctx, t0.Add(90*time.Second))
	noErr(t, err)

	// on the road: the planned route is there, nothing to compare yet
	today := time.Now().In(time.FixedZone("", 8*3600)).Format("2006-01-02")
	rep, err := a.RiderReport(ctx, s.branchID, today, today)
	noErr(t, err)
	if !rep.StoreSet || len(rep.Riders) != 2 || rep.Riders[0].ID != s.rider1 || !rep.Riders[0].OnTrip || rep.Riders[0].Trips != 1 ||
		rep.Riders[0].Orders != 2 || rep.Riders[0].CompareM != 0 || rep.Riders[1].Trips != 0 {
		t.Fatalf("on the road: %+v", rep)
	}
	d, err := a.TripDetail(ctx, s.branchID, tr.id)
	noErr(t, err)
	if d.PlannedSource != "straight" || d.PlannedM == nil || *d.PlannedM < 3950 || *d.PlannedM > 4050 || d.OverM != nil || d.Store == nil {
		t.Fatalf("planned: %+v", d.Trip)
	}

	fix(3*time.Minute, 0.018, 0)
	_, err = a.RiderDeliver(ctx, far.Delivery.ID, s.rider1, far.Totals.Total)
	noErr(t, err)
	fix(5*time.Minute, 0.018, 0.011) // a detour of 1.2 km to the east and back
	fix(7*time.Minute, 0.009, 0)
	_, err = a.RiderFail(ctx, near.Delivery.ID, s.rider1, "nobody home")
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE rider_trips SET last_drop_at=$2 WHERE id=$1`, tr.id, t0.Add(7*time.Minute))
	noErr(t, err)
	fix(9*time.Minute, 0.0003, 0)
	_, err = a.AdvanceTripsAt(ctx, t0.Add(10*time.Minute))
	noErr(t, err)

	rep, err = a.RiderReport(ctx, s.branchID, today, today)
	noErr(t, err)
	l := rep.Riders[0]
	if l.Trips != 1 || l.Delivered != 1 || l.Failed != 1 || l.OnTrip || l.Detours != 1 || l.NoTrack != 0 ||
		l.CompareM < 3950 || l.CompareM > 4050 || l.ActualM != l.ComparedActualM || l.ActualM < 5500 || l.ActualM > 5900 ||
		l.Cash != far.Totals.Total || l.ShiftMinutes < 0 {
		t.Fatalf("after the trip: %+v", l)
	}
	det, err := a.RiderDetail(ctx, s.branchID, s.rider1, today, today)
	noErr(t, err)
	if len(det.Days) != 1 || det.Days[0].Trips != 1 || det.Days[0].Delivered != 1 || len(det.Trips) != 1 {
		t.Fatalf("detail: %+v", det)
	}
	tp := det.Trips[0]
	if !tp.Detour || tp.OverM == nil || *tp.OverM < 1500 || len(tp.Items) != 2 || tp.Items[0].PublicNo != far.PublicNo ||
		*tp.Items[0].DropSeq != 1 || tp.Items[1].Outcome != "failed" || tp.Items[1].FailReason != "nobody home" || tp.Items[0].Lat != nil {
		t.Fatalf("trip in the list: %+v", tp)
	}
	d, err = a.TripDetail(ctx, s.branchID, tr.id)
	noErr(t, err)
	var path [][3]float64
	var planned [][2]float64
	var order []int64
	noErr(t, json.Unmarshal(d.ActualPath, &path))
	noErr(t, json.Unmarshal(d.PlannedPath, &planned))
	noErr(t, json.Unmarshal(d.PlannedOrder, &order))
	if len(path) < 5 || len(planned) != 4 || len(order) != 2 || d.RiderName != "R1" || d.Items[0].Lat == nil || d.Items[0].Address == "" || d.PathForgotten {
		t.Fatalf("trip detail: %+v path %v planned %v order %v", d.Trip, path, planned, order)
	}
	// another store cannot read it
	other := newStore(t, true)
	_, err = a.TripDetail(ctx, other.branchID, tr.id)
	wantCode(t, err, "NOT_FOUND")

	// a trip that ended on the road is compared without the way back
	solo := s.packed(t, s.rider2)
	at(solo, 0.009, 0)
	_, err = a.RiderPickup(ctx, solo.Delivery.ID, s.rider2)
	noErr(t, err)
	_, err = a.RiderDeliver(ctx, solo.Delivery.ID, s.rider2, 0)
	noErr(t, err)
	_, err = a.SetDuty(ctx, s.rider2, false, "")
	noErr(t, err)
	_, err = a.AdvanceTrips(ctx)
	noErr(t, err)
	det, err = a.RiderDetail(ctx, s.branchID, s.rider2, today, today)
	noErr(t, err)
	if tp := det.Trips[0]; tp.EndReason != "off_duty" || tp.PlannedM == nil || tp.CompareM == nil || *tp.CompareM*2 < *tp.PlannedM-5 ||
		*tp.CompareM*2 > *tp.PlannedM+5 || tp.ActualM != nil || tp.OverM != nil {
		t.Fatalf("ended on the road: %+v", tp)
	}

	// an order without a position on the map: no planned route
	blind := s.packed(t, s.rider2)
	_, err = a.RiderPickup(ctx, blind.Delivery.ID, s.rider2)
	noErr(t, err)
	_, err = a.RiderDeliver(ctx, blind.Delivery.ID, s.rider2, 0)
	noErr(t, err)
	_, err = a.AdvanceTrips(ctx)
	noErr(t, err)
	det, err = a.RiderDetail(ctx, s.branchID, s.rider2, today, today)
	noErr(t, err)
	if tp := det.Trips[1]; tp.PlannedM != nil || tp.PlannedSource != "" {
		t.Fatalf("no position: %+v", tp)
	}
}
