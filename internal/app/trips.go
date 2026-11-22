package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// A trip is one outing of a rider: it starts when they collect an order at
// the store with no trip under way, takes on every order collected before
// they leave, and ends when they are back at the store. Collecting and
// dropping orders is recorded as it happens; leaving, coming back and the
// distance ridden are worked out by the worker from the phone's positions.

const (
	tripInterval = time.Minute
	// tripLeaveMeters from the store is "left"; tripReturnMeters is "back".
	tripLeaveMeters  = 150
	tripReturnMeters = 100
	// tripLoadingWait: without positions, a rider is taken to have left this
	// long after collecting the last order.
	tripLoadingWait = 10 * time.Minute
	// tripReturnWait: a rider not seen back this long after the last drop is
	// not coming back; tripSilentWait is the same for a phone that went quiet.
	tripReturnWait = 60 * time.Minute
	tripSilentWait = 30 * time.Minute
	tripMaxAge     = 24 * time.Hour
	// tripRefineAfter: the track is worked out once more this long after the
	// end, for positions a phone without signal sent late.
	tripRefineAfter = 15 * time.Minute
	tripPathDays    = 90

	trackMaxAccuracyM = 100
	trackMaxSpeed     = 120 / 3.6 // m/s
	// trackJitterM: moves shorter than this are a phone standing still.
	trackJitterM = 25
	// A leg with no fix for trackGapTime that is longer than trackGapM was not recorded.
	trackGapTime   = 2 * time.Minute
	trackGapM      = 300
	trackSimplifyM = 10
)

type trackPoint struct {
	Pos LatLng
	At  time.Time
}

// Track is what a rider rode, worked out from position fixes.
type Track struct {
	DistanceM int
	// Points is the path with the points that add no shape taken out.
	Points []trackPoint
	// Gaps are indexes into Points: the leg from that point to the next was
	// not recorded and counts as a straight line.
	Gaps       []int
	GapSeconds int
	// Fixes is how many usable fixes the track was made from.
	Fixes int
}

// cleanFixes drops fixes that cannot be trusted: a poor accuracy, or a jump
// no motorbike could make. fixes are in time order.
func cleanFixes(fixes []trackPoint, accuracy []*float32) []trackPoint {
	out := make([]trackPoint, 0, len(fixes))
	rejected := 0
	for i, f := range fixes {
		if accuracy[i] != nil && *accuracy[i] > trackMaxAccuracyM {
			continue
		}
		if n := len(out); n > 0 {
			prev := out[n-1]
			dt := f.At.Sub(prev.At).Seconds()
			if dt <= 0 {
				continue
			}
			if d := metresBetween(prev.Pos, f.Pos); d > trackJitterM && d/dt > trackMaxSpeed {
				// Three in a row disagree with the last one kept: that one was the stray.
				if rejected++; rejected < 3 {
					continue
				}
				out = out[:n-1]
			}
		}
		rejected = 0
		out = append(out, f)
	}
	return out
}

// buildTrack measures cleaned fixes and thins them out.
func buildTrack(clean []trackPoint) Track {
	t := Track{Fixes: len(clean), Points: []trackPoint{}, Gaps: []int{}}
	if len(clean) == 0 {
		return t
	}
	var runs [][]trackPoint
	cur := []trackPoint{clean[0]}
	anchor, prev := clean[0], clean[0]
	dist := 0.0
	for _, p := range clean[1:] {
		d := metresBetween(anchor.Pos, p.Pos)
		if d < trackJitterM {
			prev = p
			continue
		}
		dist += d
		if p.At.Sub(prev.At) > trackGapTime && metresBetween(prev.Pos, p.Pos) > trackGapM {
			t.GapSeconds += int(p.At.Sub(prev.At).Seconds())
			if prev.At.After(anchor.At) {
				cur = append(cur, trackPoint{anchor.Pos, prev.At})
			}
			runs = append(runs, cur)
			cur = []trackPoint{p}
		} else {
			// they stood here until prev.At
			if prev.At.After(anchor.At) {
				cur = append(cur, trackPoint{anchor.Pos, prev.At})
			}
			cur = append(cur, p)
		}
		anchor, prev = p, p
	}
	if prev.At.After(anchor.At) {
		cur = append(cur, trackPoint{anchor.Pos, prev.At})
	}
	runs = append(runs, cur)
	for i, run := range runs {
		t.Points = append(t.Points, simplifyTrack(run, trackSimplifyM)...)
		if i < len(runs)-1 {
			t.Gaps = append(t.Gaps, len(t.Points)-1)
		}
	}
	t.DistanceM = int(math.Round(dist))
	return t
}

// simplifyTrack takes out the points that lie within tolerance of the line
// through their neighbours (Douglas-Peucker).
func simplifyTrack(pts []trackPoint, toleranceM float64) []trackPoint {
	if len(pts) < 3 {
		return pts
	}
	pl := newPlanar(pts[0].Pos)
	xs := make([]xy, len(pts))
	for i, p := range pts {
		xs[i] = pl.pt(p.Pos)
	}
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	// Two points at one place say how long the rider stood there: both stay.
	for i := 1; i < len(pts); i++ {
		if pts[i].Pos == pts[i-1].Pos {
			keep[i-1], keep[i] = true, true
		}
	}
	var stack [][2]int
	for i, from := 1, 0; i < len(pts); i++ {
		if keep[i] {
			stack = append(stack, [2]int{from, i})
			from = i
		}
	}
	for len(stack) > 0 {
		seg := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		far, at := 0.0, -1
		for i := seg[0] + 1; i < seg[1]; i++ {
			if d := distSeg(xs[i], xs[seg[0]], xs[seg[1]]); d > far {
				far, at = d, i
			}
		}
		if at >= 0 && far > toleranceM {
			keep[at] = true
			stack = append(stack, [2]int{seg[0], at}, [2]int{at, seg[1]})
		}
	}
	out := make([]trackPoint, 0, len(pts))
	for i, p := range pts {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

// pathJSON is the stored form of a track: [[lat, lng, seconds since start], ...].
func pathJSON(pts []trackPoint, start time.Time) []byte {
	out := make([][3]float64, len(pts))
	for i, p := range pts {
		out[i] = [3]float64{math.Round(p.Pos[0]*1e6) / 1e6, math.Round(p.Pos[1]*1e6) / 1e6,
			math.Round(p.At.Sub(start).Seconds())}
	}
	b, _ := json.Marshal(out)
	return b
}

// ---- what happens as orders are collected and dropped ----

// tripPickup puts an order the rider just collected on their trip, starting
// a trip when none is under way.
func tripPickup(ctx context.Context, tx pgx.Tx, riderID, branchID, orderID int64) error {
	now := time.Now()
	var id, tripBranch int64
	var status string
	var pending int
	err := tx.QueryRow(ctx, `SELECT t.id, t.branch_id, t.status,
			(SELECT count(*) FROM rider_trip_orders o WHERE o.trip_id = t.id AND o.outcome = '')
		FROM rider_trips t WHERE t.rider_id=$1 AND t.status <> 'done' FOR UPDATE OF t`, riderID).
		Scan(&id, &tripBranch, &status, &pending)
	switch {
	case isNoRows(err):
		id = 0
	case err != nil:
		return err
	case tripBranch == branchID && (status == "loading" || pending > 0):
		// still at the store, or back for more with orders still on board
	default:
		// Orders are collected at the store, so collecting the next one says
		// the rider is back from the trip before.
		var returned *time.Time
		if tripBranch == branchID {
			returned = &now
		}
		if err := endTrip(ctx, tx, id, "next_trip", now, returned); err != nil {
			return err
		}
		id = 0
	}
	if id == 0 {
		if err := tx.QueryRow(ctx, `INSERT INTO rider_trips (branch_id, rider_id, started_at, shift_id)
			VALUES ($1,$2,$3,(SELECT id FROM staff_shifts WHERE user_id=$2 AND ended_at IS NULL AND NOT voided))
			RETURNING id`, branchID, riderID, now).Scan(&id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO rider_trip_orders (trip_id, order_id, picked_up_at) VALUES ($1,$2,$3)`,
		id, orderID, now); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE rider_trips SET order_count = order_count + 1, last_drop_at = NULL WHERE id=$1`, id)
	return err
}

// tripDrop records how an order left its trip: delivered, failed, or removed
// (cancelled on the way). An order that is on no trip is left alone.
func tripDrop(ctx context.Context, tx pgx.Tx, orderID int64, outcome string) error {
	var rowID, tripID int64
	err := tx.QueryRow(ctx, `SELECT id, trip_id FROM rider_trip_orders WHERE order_id=$1 AND outcome=''
		ORDER BY id DESC LIMIT 1`, orderID).Scan(&rowID, &tripID)
	if isNoRows(err) {
		return nil
	} else if err != nil {
		return err
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM rider_trips WHERE id=$1 FOR UPDATE`, tripID).Scan(&status); err != nil {
		return err
	}
	now := time.Now()
	if _, err := tx.Exec(ctx, `UPDATE rider_trip_orders SET outcome=$2, done_at=$3, drop_seq = CASE WHEN $2 = 'removed' THEN NULL
		ELSE (SELECT count(*) + 1 FROM rider_trip_orders x WHERE x.trip_id=$4 AND x.drop_seq IS NOT NULL) END
		WHERE id=$1`, rowID, outcome, now, tripID); err != nil {
		return err
	}
	var orders, pending int
	if err := tx.QueryRow(ctx, `UPDATE rider_trips t SET order_count = c.orders, delivered_count = c.delivered,
			failed_count = c.failed, last_drop_at = CASE WHEN c.pending = 0 THEN c.last_drop END
		FROM (SELECT count(*) FILTER (WHERE outcome <> 'removed') AS orders,
				count(*) FILTER (WHERE outcome = 'delivered') AS delivered,
				count(*) FILTER (WHERE outcome = 'failed') AS failed,
				count(*) FILTER (WHERE outcome = '') AS pending,
				max(done_at) FILTER (WHERE outcome IN ('delivered','failed')) AS last_drop
			FROM rider_trip_orders WHERE trip_id=$1) c
		WHERE t.id=$1 RETURNING c.orders, c.pending`, tripID).Scan(&orders, &pending); err != nil {
		return err
	}
	if orders == 0 && pending == 0 && status != "done" {
		return endTrip(ctx, tx, tripID, "empty", now, nil)
	}
	return nil
}

// tripOffDuty ends the trip of a rider who clocks off with nothing left to
// deliver before they are back at the store.
func tripOffDuty(ctx context.Context, tx pgx.Tx, riderID int64, at time.Time) error {
	var id int64
	var started time.Time
	err := tx.QueryRow(ctx, `SELECT t.id, t.started_at FROM rider_trips t WHERE t.rider_id=$1 AND t.status <> 'done'
		AND NOT EXISTS (SELECT 1 FROM rider_trip_orders o WHERE o.trip_id = t.id AND o.outcome = '')
		FOR UPDATE OF t`, riderID).Scan(&id, &started)
	if isNoRows(err) {
		return nil
	} else if err != nil {
		return err
	}
	if at.Before(started) {
		at = started
	}
	return endTrip(ctx, tx, id, "off_duty", at, nil)
}

// endTrip closes a trip. The track is left to the worker, which is told to
// work it out on its next turn.
func endTrip(ctx context.Context, tx pgx.Tx, id int64, reason string, at time.Time, returned *time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE rider_trips SET status='done', end_reason=$2, ended_at=$3, returned_at=$4,
		refine_at=now() WHERE id=$1`, id, reason, at, returned)
	return err
}

// ---- the worker ----

// AdvanceTrips looks at every trip under way: has the rider left the store,
// are they back, how far have they ridden. It also works out the track of
// trips that just ended. It returns how many trips it looked at.
func (a *App) AdvanceTrips(ctx context.Context) (int, error) {
	return a.AdvanceTripsAt(ctx, time.Now())
}

// AdvanceTripsAt is AdvanceTrips at a given time.
func (a *App) AdvanceTripsAt(ctx context.Context, now time.Time) (int, error) {
	rows, err := a.DB.Query(ctx, `SELECT id FROM rider_trips WHERE status <> 'done'
		OR (refine_at IS NOT NULL AND refine_at <= $1) ORDER BY id`, now)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := a.inTx(ctx, func(tx pgx.Tx) error { return advanceTrip(ctx, tx, id, now, a.Router == nil) }); err != nil {
			return 0, err
		}
	}
	if a.Router != nil {
		if err := a.planTrips(ctx, now); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// advanceTrip moves one trip on. straight: there is no route service, so a
// trip on the road gets the straight-line estimate right here.
func advanceTrip(ctx context.Context, tx pgx.Tx, id int64, now time.Time, straight bool) error {
	var (
		branchID, riderID               int64
		status, reason                  string
		started                         time.Time
		left, lastDrop, ended, returned *time.Time
		lastPickup                      *time.Time
		lat, lng                        *float64
		pending                         int
	)
	err := tx.QueryRow(ctx, `SELECT t.branch_id, t.rider_id, t.status, t.end_reason, t.started_at, t.left_store_at,
			t.last_drop_at, t.ended_at, t.returned_at, b.lat, b.lng,
			(SELECT count(*) FROM rider_trip_orders o WHERE o.trip_id = t.id AND o.outcome = ''),
			(SELECT max(picked_up_at) FROM rider_trip_orders o WHERE o.trip_id = t.id)
		FROM rider_trips t JOIN branches b ON b.id = t.branch_id WHERE t.id=$1 FOR UPDATE OF t`, id).
		Scan(&branchID, &riderID, &status, &reason, &started, &left, &lastDrop, &ended, &returned, &lat, &lng, &pending, &lastPickup)
	if isNoRows(err) {
		return nil
	} else if err != nil {
		return err
	}
	until := now
	if ended != nil {
		until = *ended
	}
	rows, err := tx.Query(ctx, `SELECT lat, lng, accuracy, at FROM rider_tracks WHERE user_id=$1 AND at >= $2 AND at <= $3
		ORDER BY at, id`, riderID, started, until)
	if err != nil {
		return err
	}
	var raw []trackPoint
	var acc []*float32
	for rows.Next() {
		var p trackPoint
		var a *float32
		if err := rows.Scan(&p.Pos[0], &p.Pos[1], &a, &p.At); err != nil {
			rows.Close()
			return err
		}
		raw, acc = append(raw, p), append(acc, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	fixes := cleanFixes(raw, acc)
	var store *LatLng
	if lat != nil && lng != nil {
		store = &LatLng{*lat, *lng}
	}
	lastSeen := started
	if n := len(fixes); n > 0 {
		lastSeen = fixes[n-1].At
	}

	if status == "loading" {
		if store != nil {
			for _, f := range fixes {
				if metresBetween(*store, f.Pos) > tripLeaveMeters {
					t := f.At
					left, status = &t, "out"
					break
				}
			}
		}
		if status == "loading" {
			since := started
			if lastPickup != nil {
				since = *lastPickup
			}
			switch {
			case pending == 0 && lastDrop != nil:
				status = "out" // dropped next door, without ever being seen away
			case (store == nil || len(fixes) == 0) && now.Sub(since) > tripLoadingWait:
				status = "out" // nothing to tell by: when they left is not known
			}
		}
	}
	if status == "out" {
		end := func(why string, at time.Time) {
			if at.Before(started) {
				at = started
			}
			status, reason, ended = "done", why, &at
		}
		switch {
		case pending == 0 && lastDrop != nil && store == nil:
			end("no_store", *lastDrop)
		case pending == 0 && lastDrop != nil:
			for _, f := range fixes {
				if !f.At.Before(*lastDrop) && metresBetween(*store, f.Pos) <= tripReturnMeters {
					t := f.At
					returned = &t
					end("returned", t)
					break
				}
			}
			if status == "out" {
				seen := lastSeen
				if seen.Before(*lastDrop) {
					seen = *lastDrop
				}
				if now.Sub(*lastDrop) > tripReturnWait || now.Sub(seen) > tripSilentWait {
					end("timeout", seen)
				}
			}
		case now.Sub(started) > tripMaxAge:
			end("timeout", lastSeen)
		}
	}

	if straight && status != "loading" && store != nil {
		if err := planStraight(ctx, tx, id, *store); err != nil {
			return err
		}
	}

	// The track runs to the end of the trip, not to the last fix received.
	if ended != nil {
		kept := fixes[:0:0]
		for _, f := range fixes {
			if !f.At.After(*ended) {
				kept = append(kept, f)
			}
		}
		fixes = kept
	}
	var refine *time.Time
	if status == "done" {
		// Once right away and once more after late fixes had time to arrive.
		if t := ended.Add(tripRefineAfter); t.After(now) {
			refine = &t
		}
	}
	var distance *int
	var path, gaps []byte
	// The trip starts at the store, wherever the first fix came from.
	if store != nil && len(fixes) > 0 && fixes[0].At.After(started) {
		fixes = append([]trackPoint{{*store, started}}, fixes...)
	}
	tr := buildTrack(fixes)
	if tr.Fixes > 0 {
		distance = &tr.DistanceM
		path = pathJSON(tr.Points, started)
		gaps, _ = json.Marshal(tr.Gaps)
	}
	_, err = tx.Exec(ctx, `UPDATE rider_trips t SET status=$2, end_reason=$3, left_store_at=$4, ended_at=$5, returned_at=$6,
			actual_distance_m=$7, actual_path=$8, track_gaps=$9, track_points=$10, track_gap_s=$11, refine_at=$12,
			shift_id = coalesce(t.shift_id, (SELECT s.id FROM staff_shifts s WHERE s.user_id = t.rider_id AND NOT s.voided
				AND s.started_at <= t.started_at + interval '5 minutes' AND (s.ended_at IS NULL OR s.ended_at >= t.started_at)
				ORDER BY s.started_at DESC LIMIT 1))
		WHERE t.id=$1`, id, status, reason, left, ended, returned, distance, path, gaps, tr.Fixes, tr.GapSeconds, refine)
	return err
}

// forgetOldTripPaths drops the route of trips that ended long ago. The
// figures (orders, distance) stay.
func (a *App) forgetOldTripPaths(ctx context.Context) error {
	_, err := a.DB.Exec(ctx, `UPDATE rider_trips SET actual_path=NULL, track_gaps=NULL, planned_path=NULL
		WHERE ended_at < now() - make_interval(days => $1) AND (actual_path IS NOT NULL OR planned_path IS NOT NULL)`, tripPathDays)
	return err
}

// ---- the route the system would take ----

// bestRound orders the stops of a round trip from the store and back so that
// the straight-line distance is shortest. Up to eight stops every order is
// tried; beyond that the nearest stop is taken next.
func bestRound(store LatLng, stops []LatLng) ([]int, float64) {
	n := len(stops)
	length := func(order []int) float64 {
		d, at := 0.0, store
		for _, i := range order {
			d += metresBetween(at, stops[i])
			at = stops[i]
		}
		return d + metresBetween(at, store)
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if n > 8 {
		used := make([]bool, n)
		at := store
		for k := 0; k < n; k++ {
			best := -1
			for i := range stops {
				if !used[i] && (best < 0 || metresBetween(at, stops[i]) < metresBetween(at, stops[best])) {
					best = i
				}
			}
			used[best], order[k], at = true, best, stops[best]
		}
		return order, length(order)
	}
	best, bestLen := append([]int(nil), order...), length(order)
	var permute func(k int)
	permute = func(k int) {
		if k == n {
			if l := length(order); l < bestLen-0.5 {
				bestLen = l
				copy(best, order)
			}
			return
		}
		for i := k; i < n; i++ {
			order[k], order[i] = order[i], order[k]
			permute(k + 1)
			order[k], order[i] = order[i], order[k]
		}
	}
	permute(0)
	return best, bestLen
}

// tripStops reads where a trip drops its orders. missing: an order has no
// position on the map, so no route can be planned.
func tripStops(ctx context.Context, q Querier, tripID int64) (ids []int64, stops []LatLng, missing bool, err error) {
	rows, err := q.Query(ctx, `SELECT o.id, o.dest_lat, o.dest_lng FROM rider_trip_orders x JOIN orders o ON o.id = x.order_id
		WHERE x.trip_id=$1 AND x.outcome <> 'removed' ORDER BY x.id`, tripID)
	if err != nil {
		return nil, nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var lat, lng *float64
		if err := rows.Scan(&id, &lat, &lng); err != nil {
			return nil, nil, false, err
		}
		if lat == nil || lng == nil {
			missing = true
			continue
		}
		ids, stops = append(ids, id), append(stops, LatLng{*lat, *lng})
	}
	return ids, stops, missing, rows.Err()
}

// savePlan stores the planned route of a trip that has none yet.
func savePlan(ctx context.Context, q Querier, tripID int64, ids []int64, r *PlannedRoute, source string) error {
	planned := make([]int64, len(r.Order))
	for k, i := range r.Order {
		planned[k] = ids[i]
	}
	pj, _ := json.Marshal(r.Path)
	oj, _ := json.Marshal(planned)
	var duration *int
	if r.DurationS > 0 {
		duration = &r.DurationS
	}
	_, err := q.Exec(ctx, `UPDATE rider_trips SET planned_distance_m=$2, planned_path=$3, planned_order=$4, planned_source=$5,
		planned_duration_s=$6, planned_return_m=$7, planned_mode=$8 WHERE id=$1 AND planned_source=''`,
		tripID, r.DistanceM, pj, oj, source, duration, r.ReturnM, r.Mode)
	return err
}

// planStraight fills in the planned route of a trip that has none yet, as
// straight lines from the store through the drops and back. It is worked out
// once, when the rider has left. A trip with an order that has no position on
// the map gets no planned route.
func planStraight(ctx context.Context, q Querier, tripID int64, store LatLng) error {
	var source string
	if err := q.QueryRow(ctx, `SELECT planned_source FROM rider_trips WHERE id=$1`, tripID).Scan(&source); err != nil || source != "" {
		return err
	}
	ids, stops, missing, err := tripStops(ctx, q, tripID)
	if err != nil || missing || len(stops) == 0 {
		return err
	}
	order, dist := bestRound(store, stops)
	r := &PlannedRoute{Order: order, DistanceM: int(math.Round(dist)), Path: [][2]float64{{store[0], store[1]}}}
	for _, i := range order {
		r.Path = append(r.Path, [2]float64{stops[i][0], stops[i][1]})
	}
	r.Path = append(r.Path, [2]float64{store[0], store[1]})
	r.ReturnM = int(math.Round(metresBetween(stops[order[len(order)-1]], store)))
	return savePlan(ctx, q, tripID, ids, r, "straight")
}

// planTries is how often the route service is asked for one trip before the
// straight-line estimate takes its place.
const planTries = 3

// planTrips asks the route service for the trips that are on the road and
// have no planned route yet: once per trip, at the time the rider left. The
// call is made outside any transaction. A trip the service fails on three
// times gets the straight-line estimate.
func (a *App) planTrips(ctx context.Context, now time.Time) error {
	rows, err := a.DB.Query(ctx, `SELECT t.id, b.lat, b.lng, t.planned_tries FROM rider_trips t JOIN branches b ON b.id = t.branch_id
		WHERE t.planned_source = '' AND t.status <> 'loading' AND t.planned_tries < $1 AND b.lat IS NOT NULL AND b.lng IS NOT NULL
			AND t.started_at > $2 ORDER BY t.id`, planTries, now.Add(-tripMaxAge))
	if err != nil {
		return err
	}
	type todo struct {
		id    int64
		store LatLng
		tries int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (todo, error) {
		var t todo
		return t, r.Scan(&t.id, &t.store[0], &t.store[1], &t.tries)
	})
	if err != nil {
		return err
	}
	for _, t := range list {
		ids, stops, missing, err := tripStops(ctx, a.DB, t.id)
		if err != nil {
			return err
		}
		if missing || len(stops) == 0 {
			// nothing to plan; do not look at it again
			if _, err := a.DB.Exec(ctx, `UPDATE rider_trips SET planned_tries=$2 WHERE id=$1`, t.id, planTries); err != nil {
				return err
			}
			continue
		}
		var route *PlannedRoute
		if len(stops) <= routeMaxStops {
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			route, err = a.Router.Route(callCtx, t.store, stops)
			cancel()
			if err == nil && (len(route.Order) != len(stops) || len(route.Path) < 2) {
				err = fmt.Errorf("routes: unusable answer")
			}
			if err != nil {
				log.Printf("trip %d: planned route: %v", t.id, err)
				route = nil
				if t.tries+1 < planTries {
					if _, err := a.DB.Exec(ctx, `UPDATE rider_trips SET planned_tries = planned_tries + 1 WHERE id=$1`, t.id); err != nil {
						return err
					}
					continue
				}
			}
		}
		if route != nil {
			err = savePlan(ctx, a.DB, t.id, ids, route, "google")
		} else {
			err = planStraight(ctx, a.DB, t.id, t.store)
		}
		if err != nil {
			return err
		}
		if _, err := a.DB.Exec(ctx, `UPDATE rider_trips SET planned_tries = planned_tries + 1 WHERE id=$1`, t.id); err != nil {
			return err
		}
	}
	return nil
}
