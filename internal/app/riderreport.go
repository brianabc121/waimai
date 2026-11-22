package app

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// The rider work summary: how many orders each rider delivered, in how many
// trips, and how far they rode compared with the route the system would take.
// The figures are for looking at; nothing is paid or deducted by them.

const (
	riderReportDays = 92
)

// TripOrder is one order a trip carried.
type TripOrder struct {
	OrderID    int64      `json:"order_id"`
	PublicNo   string     `json:"public_no"`
	PickedUpAt time.Time  `json:"picked_up_at"`
	Outcome    string     `json:"outcome"` // "", delivered, failed, removed
	DoneAt     *time.Time `json:"done_at"`
	DropSeq    *int       `json:"drop_seq"`
	FailReason string     `json:"fail_reason,omitempty"`
	// only in the detail of a trip
	CustomerName string   `json:"customer_name,omitempty"`
	Address      string   `json:"address,omitempty"`
	Lat          *float64 `json:"lat,omitempty"`
	Lng          *float64 `json:"lng,omitempty"`
}

// Trip is one outing of a rider.
type Trip struct {
	ID          int64      `json:"id"`
	RiderID     int64      `json:"rider_id"`
	Day         string     `json:"day"`
	Status      string     `json:"status"`
	EndReason   string     `json:"end_reason"`
	StartedAt   time.Time  `json:"started_at"`
	LeftStoreAt *time.Time `json:"left_store_at"`
	LastDropAt  *time.Time `json:"last_drop_at"`
	ReturnedAt  *time.Time `json:"returned_at"`
	EndedAt     *time.Time `json:"ended_at"`
	Orders      int        `json:"order_count"`
	Delivered   int        `json:"delivered_count"`
	Failed      int        `json:"failed_count"`
	// ActualM is nil when the phone sent no position.
	ActualM     *int `json:"actual_m"`
	TrackPoints int  `json:"track_points"`
	GapSeconds  int  `json:"gap_seconds"`
	// PlannedM is the planned round trip; nil when there is no planned route.
	PlannedM      *int   `json:"planned_m"`
	PlannedSource string `json:"planned_source"`
	// CompareM is what ActualM is compared with: the planned route, without
	// the way back when the rider was not seen coming back.
	CompareM *int `json:"compare_m"`
	// OverM = ActualM - CompareM; Detour says that it is beyond the limits.
	OverM  *int        `json:"over_m"`
	Detour bool        `json:"detour"`
	Items  []TripOrder `json:"orders"`

	plannedPath []byte
}

const tripCols = `t.id, t.rider_id, t.status, t.end_reason, t.started_at, t.left_store_at, t.last_drop_at, t.returned_at, t.ended_at,
	t.order_count, t.delivered_count, t.failed_count, t.actual_distance_m, t.track_points, t.track_gap_s,
	t.planned_distance_m, t.planned_source, t.planned_path`

func scanTrip(r pgx.CollectableRow, b *Branch) (Trip, error) {
	loc := b.location()
	t := Trip{Items: []TripOrder{}}
	err := r.Scan(&t.ID, &t.RiderID, &t.Status, &t.EndReason, &t.StartedAt, &t.LeftStoreAt, &t.LastDropAt, &t.ReturnedAt, &t.EndedAt,
		&t.Orders, &t.Delivered, &t.Failed, &t.ActualM, &t.TrackPoints, &t.GapSeconds, &t.PlannedM, &t.PlannedSource, &t.plannedPath)
	if err != nil {
		return t, err
	}
	t.Day = t.StartedAt.In(loc).Format("2006-01-02")
	t.compare(b)
	return t, nil
}

// compare works out what the distance ridden is held against, and whether
// the store calls the difference a detour.
func (t *Trip) compare(b *Branch) {
	if t.PlannedM == nil {
		return
	}
	c := *t.PlannedM
	if t.Status == "done" && t.ReturnedAt == nil {
		// not seen coming back: the way back is left out on both sides
		var path [][2]float64
		if json.Unmarshal(t.plannedPath, &path) == nil && len(path) >= 2 {
			n := len(path)
			c -= int(math.Round(metresBetween(LatLng(path[n-2]), LatLng(path[n-1]))))
		}
	}
	t.CompareM = &c
	if t.ActualM == nil || t.Status != "done" {
		return
	}
	over := *t.ActualM - c
	t.OverM = &over
	t.Detour = over > b.DetourMeters && over*100 > c*b.DetourPercent
}

// RiderLine is one rider in the summary.
type RiderLine struct {
	ID           int64  `json:"id"`
	DisplayName  string `json:"display_name"`
	Username     string `json:"username"`
	Gone         bool   `json:"gone"`
	ShiftMinutes int    `json:"shift_minutes"`
	Trips        int    `json:"trips"`
	Orders       int    `json:"orders"`
	Delivered    int    `json:"delivered"`
	Failed       int    `json:"failed"`
	// ActualM sums the trips that have a distance; NoTrack counts those that have none.
	ActualM int `json:"actual_m"`
	NoTrack int `json:"no_track"`
	// CompareM and ComparedActualM sum the finished trips that have both figures.
	CompareM        int   `json:"compare_m"`
	ComparedActualM int   `json:"compared_actual_m"`
	Detours         int   `json:"detours"`
	Gaps            int   `json:"gaps"`
	Cash            int64 `json:"cash"`
	OnTrip          bool  `json:"on_trip"`
}

type RiderReport struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Timezone      string `json:"timezone"`
	Currency      string `json:"currency"`
	DetourPercent int    `json:"detour_percent"`
	DetourMeters  int    `json:"detour_meters"`
	// StoreSet is false when the store has no position: trips are counted but
	// coming back, distances and routes are not known.
	StoreSet bool        `json:"store_set"`
	Riders   []RiderLine `json:"riders"`
}

func (a *App) tripsIn(ctx context.Context, b *Branch, riderID *int64, start, end time.Time) ([]Trip, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+tripCols+` FROM rider_trips t WHERE t.branch_id=$1 AND t.started_at >= $2 AND t.started_at < $3
		AND ($4::bigint IS NULL OR t.rider_id = $4) AND t.end_reason <> 'empty' ORDER BY t.started_at, t.id`, b.ID, start, end, riderID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Trip, error) { return scanTrip(r, b) })
}

// RiderReport sums up the trips of every rider in the period.
func (a *App) RiderReport(ctx context.Context, branchID int64, from, to string) (*RiderReport, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	loc := b.location()
	start, end, err := dayRange(loc, from, to, riderReportDays)
	if err != nil {
		return nil, err
	}
	out := &RiderReport{From: from, To: to, Timezone: b.Timezone, Currency: b.Currency, DetourPercent: b.DetourPercent,
		DetourMeters: b.DetourMeters, StoreSet: b.Lat != nil && b.Lng != nil}
	trips, err := a.tripsIn(ctx, b, nil, start, end)
	if err != nil {
		return nil, err
	}
	by := map[int64]*RiderLine{}
	line := func(id int64) *RiderLine {
		if by[id] == nil {
			by[id] = &RiderLine{ID: id, Gone: true}
		}
		return by[id]
	}
	for _, t := range trips {
		l := line(t.RiderID)
		l.Trips++
		l.Orders += t.Orders
		l.Delivered += t.Delivered
		l.Failed += t.Failed
		if t.ActualM != nil {
			l.ActualM += *t.ActualM
		} else {
			l.NoTrack++
		}
		if t.OverM != nil {
			l.CompareM += *t.CompareM
			l.ComparedActualM += *t.ActualM
		}
		if t.Detour {
			l.Detours++
		}
		if t.GapSeconds > 0 {
			l.Gaps++
		}
		if t.Status != "done" {
			l.OnTrip = true
		}
	}
	rows, err := a.DB.Query(ctx, `SELECT rider_id, sum(amount_collected) FROM cash_collections
		WHERE branch_id=$1 AND created_at >= $2 AND created_at < $3 GROUP BY 1`, branchID, start, end)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, cash int64
		if err := rows.Scan(&id, &cash); err != nil {
			rows.Close()
			return nil, err
		}
		line(id).Cash = cash
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	minutes, err := shiftMinutes(ctx, a.DB, branchID, start, end)
	if err != nil {
		return nil, err
	}
	staff, err := a.ListStaff(ctx, branchID)
	if err != nil {
		return nil, err
	}
	for _, m := range staff {
		rider := false
		for _, p := range m.Posts {
			rider = rider || p.Kind == PostRider
		}
		if by[m.ID] == nil && (!rider || !m.Active) {
			continue
		}
		l := line(m.ID)
		l.DisplayName, l.Username, l.Gone = m.DisplayName, m.Username, false
	}
	for id, l := range by {
		if l.Gone {
			if err := a.DB.QueryRow(ctx, `SELECT display_name, username FROM users WHERE id=$1`, id).Scan(&l.DisplayName, &l.Username); err != nil {
				return nil, err
			}
		}
		l.ShiftMinutes = minutes[id]
		out.Riders = append(out.Riders, *l)
	}
	if out.Riders == nil {
		out.Riders = []RiderLine{}
	}
	sort.Slice(out.Riders, func(i, j int) bool {
		x, y := out.Riders[i], out.Riders[j]
		if x.Delivered != y.Delivered {
			return x.Delivered > y.Delivered
		}
		if x.Trips != y.Trips {
			return x.Trips > y.Trips
		}
		return x.ID < y.ID
	})
	return out, nil
}

// tripOrders fills in the orders of the trips.
func (a *App) tripOrders(ctx context.Context, trips []Trip, full bool) error {
	if len(trips) == 0 {
		return nil
	}
	at := map[int64]int{}
	ids := make([]int64, len(trips))
	for i, t := range trips {
		at[t.ID], ids[i] = i, t.ID
	}
	rows, err := a.DB.Query(ctx, `SELECT x.trip_id, o.id, o.public_no, x.picked_up_at, x.outcome, x.done_at, x.drop_seq,
			CASE WHEN x.outcome = 'failed' THEN coalesce(d.fail_reason, '') ELSE '' END, o.customer_name, o.address_line, o.dest_lat, o.dest_lng
		FROM rider_trip_orders x JOIN orders o ON o.id = x.order_id LEFT JOIN delivery_tasks d ON d.order_id = o.id
		WHERE x.trip_id = ANY($1) ORDER BY x.drop_seq NULLS LAST, x.id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tripID int64
		var o TripOrder
		if err := rows.Scan(&tripID, &o.OrderID, &o.PublicNo, &o.PickedUpAt, &o.Outcome, &o.DoneAt, &o.DropSeq, &o.FailReason,
			&o.CustomerName, &o.Address, &o.Lat, &o.Lng); err != nil {
			return err
		}
		if !full {
			o.CustomerName, o.Address, o.Lat, o.Lng = "", "", nil, nil
		}
		t := &trips[at[tripID]]
		t.Items = append(t.Items, o)
	}
	return rows.Err()
}

// RiderDay is one day of a rider.
type RiderDay struct {
	Day       string `json:"day"`
	Trips     int    `json:"trips"`
	Delivered int    `json:"delivered"`
	Failed    int    `json:"failed"`
	ActualM   int    `json:"actual_m"`
}

type RiderDetail struct {
	From     string     `json:"from"`
	To       string     `json:"to"`
	Timezone string     `json:"timezone"`
	Days     []RiderDay `json:"days"`
	// Trips are in the order they started.
	Trips []Trip `json:"trips"`
}

// RiderDetail lists the trips of one rider in the period.
func (a *App) RiderDetail(ctx context.Context, branchID, riderID int64, from, to string) (*RiderDetail, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	loc := b.location()
	start, end, err := dayRange(loc, from, to, riderReportDays)
	if err != nil {
		return nil, err
	}
	out := &RiderDetail{From: from, To: to, Timezone: b.Timezone, Days: []RiderDay{}}
	if out.Trips, err = a.tripsIn(ctx, b, &riderID, start, end); err != nil {
		return nil, err
	}
	if out.Trips == nil {
		out.Trips = []Trip{}
	}
	if err := a.tripOrders(ctx, out.Trips, false); err != nil {
		return nil, err
	}
	for _, t := range out.Trips {
		if n := len(out.Days); n == 0 || out.Days[n-1].Day != t.Day {
			out.Days = append(out.Days, RiderDay{Day: t.Day})
		}
		d := &out.Days[len(out.Days)-1]
		d.Trips++
		d.Delivered += t.Delivered
		d.Failed += t.Failed
		if t.ActualM != nil {
			d.ActualM += *t.ActualM
		}
	}
	return out, nil
}

// TripDetail is one trip with its routes.
type TripDetail struct {
	Trip
	Timezone  string  `json:"timezone"`
	RiderName string  `json:"rider_name"`
	Store     *LatLng `json:"store"`
	// ActualPath is [[lat, lng, seconds since started_at], ...]; TrackGaps are
	// indexes into it: the leg from that point to the next was not recorded.
	ActualPath json.RawMessage `json:"actual_path"`
	TrackGaps  json.RawMessage `json:"track_gaps"`
	// PlannedPath is [[lat, lng], ...]; PlannedOrder the order ids in planned order.
	PlannedPath  json.RawMessage `json:"planned_path"`
	PlannedOrder json.RawMessage `json:"planned_order"`
	// PathForgotten: the trip is older than the routes are kept.
	PathForgotten bool `json:"path_forgotten"`
	PathDays      int  `json:"path_days"`
	DetourPercent int  `json:"detour_percent"`
	DetourMeters  int  `json:"detour_meters"`
}

// TripDetail returns one trip of the branch.
func (a *App) TripDetail(ctx context.Context, branchID, tripID int64) (*TripDetail, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT `+tripCols+` FROM rider_trips t WHERE t.id=$1 AND t.branch_id=$2`, tripID, branchID)
	if err != nil {
		return nil, err
	}
	trips, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Trip, error) { return scanTrip(r, b) })
	if err != nil {
		return nil, err
	}
	if len(trips) == 0 {
		return nil, ErrNotFound
	}
	if err := a.tripOrders(ctx, trips, true); err != nil {
		return nil, err
	}
	out := &TripDetail{Trip: trips[0], Timezone: b.Timezone, PathDays: tripPathDays, DetourPercent: b.DetourPercent, DetourMeters: b.DetourMeters}
	if b.Lat != nil && b.Lng != nil {
		out.Store = &LatLng{*b.Lat, *b.Lng}
	}
	var actual, gaps, planned, order []byte
	if err := a.DB.QueryRow(ctx, `SELECT u.display_name, t.actual_path, t.track_gaps, t.planned_path, t.planned_order
		FROM rider_trips t JOIN users u ON u.id = t.rider_id WHERE t.id=$1`, tripID).
		Scan(&out.RiderName, &actual, &gaps, &planned, &order); err != nil {
		return nil, err
	}
	raw := func(b []byte) json.RawMessage {
		if b == nil {
			return json.RawMessage("[]")
		}
		return b
	}
	out.ActualPath, out.TrackGaps, out.PlannedPath, out.PlannedOrder = raw(actual), raw(gaps), raw(planned), raw(order)
	out.PathForgotten = actual == nil && out.TrackPoints > 0
	return out, nil
}

// TripsInPeriod lists the trips of every rider in the period with their
// orders, for the export.
func (a *App) TripsInPeriod(ctx context.Context, branchID int64, from, to string) ([]Trip, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	start, end, err := dayRange(b.location(), from, to, riderReportDays)
	if err != nil {
		return nil, err
	}
	trips, err := a.tripsIn(ctx, b, nil, start, end)
	if err != nil {
		return nil, err
	}
	return trips, a.tripOrders(ctx, trips, false)
}
