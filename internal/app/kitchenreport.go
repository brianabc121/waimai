package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// The in-store work summary: how many dishes each person made, day by day,
// with every dish listed. A dish counts for whoever marked it ready. What was
// marked on the shared kitchen screen counts for the screen, not for the
// account that happens to be signed in there.

const (
	// kitchenSlowMinutes: a dish that took longer from "start" to "ready" is slow.
	kitchenSlowMinutes = 15
	kitchenReportDays  = 92
	kitchenPageSize    = 50
	// kitchenChartDays is the least number of days the per-day chart shows.
	kitchenChartDays = 7
)

// KitchenPerson is one line of the summary. ID 0 is the kitchen screen.
type KitchenPerson struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	Posts       []Post `json:"posts"`
	// Gone: no longer works here, but did in the period.
	Gone bool `json:"gone"`
	// Qty counts portions ("2 x wings" is 2), Dishes counts lines.
	Qty    int `json:"qty"`
	Dishes int `json:"dishes"`
	Orders int `json:"orders"`
	// Packed is how many orders they packed.
	Packed       int `json:"packed"`
	ShiftMinutes int `json:"shift_minutes"`
}

type KitchenReport struct {
	From        string          `json:"from"`
	To          string          `json:"to"`
	Timezone    string          `json:"timezone"`
	SlowMinutes int             `json:"slow_minutes"`
	TotalQty    int             `json:"total_qty"`
	BoardQty    int             `json:"board_qty"`
	People      []KitchenPerson `json:"people"`
}

type kitchenPeriod struct {
	branch     *Branch
	loc        *time.Location
	start, end time.Time
}

func (a *App) kitchenPeriod(ctx context.Context, branchID int64, from, to string) (*kitchenPeriod, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	p := &kitchenPeriod{branch: b, loc: b.location()}
	p.start, p.end, err = dayRange(p.loc, from, to, kitchenReportDays)
	return p, err
}

// shiftMinutes is how long each person was at work in shifts that started in the period.
func shiftMinutes(ctx context.Context, q Querier, branchID int64, start, end time.Time) (map[int64]int, error) {
	rows, err := q.Query(ctx, `SELECT user_id, sum(extract(epoch FROM coalesce(ended_at, now()) - started_at))::bigint / 60
		FROM staff_shifts WHERE branch_id=$1 AND NOT voided AND started_at >= $2 AND started_at < $3 GROUP BY 1`, branchID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var m int
		if err := rows.Scan(&id, &m); err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, rows.Err()
}

// KitchenReport sums up the dishes made and orders packed per person.
func (a *App) KitchenReport(ctx context.Context, branchID int64, from, to string) (*KitchenReport, error) {
	p, err := a.kitchenPeriod(ctx, branchID, from, to)
	if err != nil {
		return nil, err
	}
	out := &KitchenReport{From: from, To: to, Timezone: p.branch.Timezone, SlowMinutes: kitchenSlowMinutes}
	by := map[int64]*KitchenPerson{}
	person := func(id int64) *KitchenPerson {
		if by[id] == nil {
			by[id] = &KitchenPerson{ID: id, Posts: []Post{}, Gone: id != 0}
		}
		return by[id]
	}
	rows, err := a.DB.Query(ctx, `SELECT CASE WHEN ready_via = 'board' THEN 0 ELSE ready_by END, sum(qty), count(*), count(DISTINCT order_id)
		FROM order_items WHERE branch_id=$1 AND ready_by IS NOT NULL AND ready_at >= $2 AND ready_at < $3 GROUP BY 1`,
		branchID, p.start, p.end)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var qty, dishes, orders int
		if err := rows.Scan(&id, &qty, &dishes, &orders); err != nil {
			rows.Close()
			return nil, err
		}
		k := person(id)
		k.Qty, k.Dishes, k.Orders = qty, dishes, orders
		out.TotalQty += qty
		if id == 0 {
			out.BoardQty = qty
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = a.DB.Query(ctx, `SELECT CASE WHEN packed_via = 'board' THEN 0 ELSE packed_by END, count(*)
		FROM orders WHERE branch_id=$1 AND packed_by IS NOT NULL AND packed_at >= $2 AND packed_at < $3 GROUP BY 1`,
		branchID, p.start, p.end)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		person(id).Packed = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	minutes, err := shiftMinutes(ctx, a.DB, branchID, p.start, p.end)
	if err != nil {
		return nil, err
	}
	staff, err := a.ListStaff(ctx, branchID)
	if err != nil {
		return nil, err
	}
	for _, m := range staff {
		// People who only ride show when they made something, not otherwise.
		inStore := len(m.Posts) == 0 && m.Role != "rider"
		for _, post := range m.Posts {
			if post.Kind != PostRider {
				inStore = true
			}
		}
		if by[m.ID] == nil && (!inStore || !m.Active) {
			continue
		}
		k := person(m.ID)
		k.DisplayName, k.Username, k.Role, k.Posts, k.Gone = m.DisplayName, m.Username, m.Role, m.Posts, false
	}
	var gone []int64
	for id, k := range by {
		if id != 0 && k.Gone {
			gone = append(gone, id)
		}
	}
	if len(gone) > 0 {
		rows, err := a.DB.Query(ctx, `SELECT id, display_name, username FROM users WHERE id = ANY($1)`, gone)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var name, username string
			if err := rows.Scan(&id, &name, &username); err != nil {
				rows.Close()
				return nil, err
			}
			by[id].DisplayName, by[id].Username, by[id].Role = name, username, "staff"
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out.People = make([]KitchenPerson, 0, len(by))
	for id, k := range by {
		k.ShiftMinutes = minutes[id]
		out.People = append(out.People, *k)
	}
	// Most dishes first; the kitchen screen always last.
	sort.Slice(out.People, func(i, j int) bool {
		x, y := out.People[i], out.People[j]
		switch {
		case (x.ID == 0) != (y.ID == 0):
			return y.ID == 0
		case x.Qty != y.Qty:
			return x.Qty > y.Qty
		case x.Packed != y.Packed:
			return x.Packed > y.Packed
		}
		return x.ID < y.ID
	})
	return out, nil
}

// KitchenDay is one bar of the per-day chart.
type KitchenDay struct {
	Day    string `json:"day"`
	Qty    int    `json:"qty"`
	Dishes int    `json:"dishes"`
	Packed int    `json:"packed"`
	// Worked says whether the person has a shift that day.
	Worked bool `json:"worked"`
}

type KitchenCount struct {
	ID     int64  `json:"id,omitempty"`
	Name   string `json:"name"`
	NameEn string `json:"name_en,omitempty"`
	Qty    int    `json:"qty"`
}

// KitchenDish is one dish somebody made.
type KitchenDish struct {
	ID          int64        `json:"id"`
	ReadyAt     time.Time    `json:"ready_at"`
	Name        string       `json:"name"`
	NameEn      string       `json:"name_en"`
	Options     []LineOption `json:"options"`
	Note        string       `json:"note"`
	Qty         int          `json:"qty"`
	StationID   *int64       `json:"station_id"`
	StationName string       `json:"station_name"`
	OrderID     int64        `json:"order_id"`
	PublicNo    string       `json:"public_no"`
	ServiceType string       `json:"service_type"`
	TableName   string       `json:"table_name"`
	Cancelled   bool         `json:"cancelled"`
	StartedAt   *time.Time   `json:"started_at"`
	StartedBy   string       `json:"started_by"`
	// Seconds from "start" to "ready"; nil when nobody tapped "start".
	Seconds *int `json:"seconds"`
	Slow    bool `json:"slow"`
	// Via is where "ready" was tapped: work, board or "" (not recorded).
	Via string `json:"via"`
	// ReadyBy is the account that tapped it, of interest for the kitchen screen.
	ReadyBy   string `json:"ready_by"`
	UndoCount int    `json:"undo_count"`
}

type KitchenDetail struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Timezone    string `json:"timezone"`
	SlowMinutes int    `json:"slow_minutes"`
	Qty         int    `json:"qty"`
	Dishes      int    `json:"dishes"`
	Orders      int    `json:"orders"`
	Packed      int    `json:"packed"`
	// AvgSeconds is over the dishes that have a start; nil when none has.
	AvgSeconds   *int `json:"avg_seconds"`
	Timed        int  `json:"timed"`
	SlowQty      int  `json:"slow_qty"`
	SlowDishes   int  `json:"slow_dishes"`
	UndoCount    int  `json:"undo_count"`
	ShiftMinutes int  `json:"shift_minutes"`
	// Days covers the period, and at least the week that ends with it.
	Days     []KitchenDay   `json:"days"`
	Top      []KitchenCount `json:"top"`
	Stations []KitchenCount `json:"stations"`
	// Items is one page of the dishes, newest first, after the filter.
	Items     []KitchenDish `json:"items"`
	ItemTotal int           `json:"item_total"`
	Page      int           `json:"page"`
	PageSize  int           `json:"page_size"`
}

// KitchenFilter narrows the list of dishes (not the figures).
type KitchenFilter struct {
	StationID *int64
	SlowOnly  bool
	Page      int
}

// KitchenDetail is the work of one person in the period; userID 0 is what
// was marked on the kitchen screen.
func (a *App) KitchenDetail(ctx context.Context, branchID, userID int64, from, to string, f KitchenFilter) (*KitchenDetail, error) {
	p, err := a.kitchenPeriod(ctx, branchID, from, to)
	if err != nil {
		return nil, err
	}
	if f.Page < 1 {
		f.Page = 1
	}
	out := &KitchenDetail{From: from, To: to, Timezone: p.branch.Timezone, SlowMinutes: kitchenSlowMinutes,
		Page: f.Page, PageSize: kitchenPageSize, Days: []KitchenDay{}, Top: []KitchenCount{}, Stations: []KitchenCount{}, Items: []KitchenDish{}}
	// $1 branch, $2 and $3 the period, $4 the person
	mine, packedMine := `oi.ready_by = $4 AND oi.ready_via <> 'board'`, `o.packed_by = $4 AND o.packed_via <> 'board'`
	if userID == 0 {
		mine, packedMine = `oi.ready_by IS NOT NULL AND oi.ready_via = 'board' AND $4 = 0`, `o.packed_by IS NOT NULL AND o.packed_via = 'board' AND $4 = 0`
	}
	where := `oi.branch_id=$1 AND oi.ready_at >= $2 AND oi.ready_at < $3 AND ` + mine
	secs := `extract(epoch FROM oi.ready_at - oi.prep_started_at)`
	slow := fmt.Sprintf(`(oi.prep_started_at IS NOT NULL AND %s > %d)`, secs, kitchenSlowMinutes*60)
	args := []any{branchID, p.start, p.end, userID}

	var avg *float64
	if err := a.DB.QueryRow(ctx, `SELECT coalesce(sum(qty),0), count(*), count(DISTINCT order_id),
			avg(`+secs+`) FILTER (WHERE oi.prep_started_at IS NOT NULL), count(*) FILTER (WHERE oi.prep_started_at IS NOT NULL),
			coalesce(sum(qty) FILTER (WHERE `+slow+`),0), count(*) FILTER (WHERE `+slow+`), coalesce(sum(undo_count),0)
		FROM order_items oi WHERE `+where, args...).
		Scan(&out.Qty, &out.Dishes, &out.Orders, &avg, &out.Timed, &out.SlowQty, &out.SlowDishes, &out.UndoCount); err != nil {
		return nil, err
	}
	if avg != nil {
		s := int(*avg + 0.5)
		out.AvgSeconds = &s
	}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM orders o WHERE o.branch_id=$1 AND o.packed_at >= $2 AND o.packed_at < $3 AND `+packedMine,
		args...).Scan(&out.Packed); err != nil {
		return nil, err
	}
	if userID != 0 {
		m, err := shiftMinutes(ctx, a.DB, branchID, p.start, p.end)
		if err != nil {
			return nil, err
		}
		out.ShiftMinutes = m[userID]
	}

	// ---- per day: the period, or the week that ends with it when it is shorter ----
	chartStart := p.start
	if w := p.end.AddDate(0, 0, -kitchenChartDays); w.Before(chartStart) {
		chartStart = w
	}
	days := map[string]*KitchenDay{}
	for d := chartStart; d.Before(p.end); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		out.Days = append(out.Days, KitchenDay{Day: k})
	}
	for i := range out.Days {
		days[out.Days[i].Day] = &out.Days[i]
	}
	tz := p.loc.String()
	chart := []any{branchID, chartStart, p.end, userID, tz}
	rows, err := a.DB.Query(ctx, `SELECT to_char(oi.ready_at AT TIME ZONE $5, 'YYYY-MM-DD'), sum(qty), count(*)
		FROM order_items oi WHERE `+where+` GROUP BY 1`, chart...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		var qty, n int
		if err := rows.Scan(&d, &qty, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if k := days[d]; k != nil {
			k.Qty, k.Dishes = qty, n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = a.DB.Query(ctx, `SELECT to_char(o.packed_at AT TIME ZONE $5, 'YYYY-MM-DD'), count(*)
		FROM orders o WHERE o.branch_id=$1 AND o.packed_at >= $2 AND o.packed_at < $3 AND `+packedMine+` GROUP BY 1`, chart...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if k := days[d]; k != nil {
			k.Packed = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if userID != 0 {
		rows, err = a.DB.Query(ctx, `SELECT DISTINCT to_char(started_at AT TIME ZONE $5, 'YYYY-MM-DD') FROM staff_shifts
			WHERE branch_id=$1 AND started_at >= $2 AND started_at < $3 AND user_id=$4 AND NOT voided`, chart...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				rows.Close()
				return nil, err
			}
			if k := days[d]; k != nil {
				k.Worked = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// ---- by dish and by station ----
	counts := func(sql string) ([]KitchenCount, error) {
		rows, err := a.DB.Query(ctx, sql, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (KitchenCount, error) {
			var c KitchenCount
			return c, r.Scan(&c.ID, &c.Name, &c.NameEn, &c.Qty)
		})
	}
	if out.Top, err = counts(`SELECT oi.item_id, min(oi.name), min(oi.name_en), sum(oi.qty) FROM order_items oi WHERE ` + where + `
		GROUP BY oi.item_id ORDER BY 4 DESC, 1`); err != nil {
		return nil, err
	}
	if out.Stations, err = counts(`SELECT coalesce(oi.station_id, 0), min(oi.station_name), coalesce(min(s.name_en), ''), sum(oi.qty)
		FROM order_items oi LEFT JOIN kitchen_stations s ON s.id = oi.station_id WHERE ` + where + `
		GROUP BY coalesce(oi.station_id, 0) ORDER BY 4 DESC, 1`); err != nil {
		return nil, err
	}

	// ---- the dishes ----
	filter := where
	if f.StationID != nil {
		args = append(args, *f.StationID)
		filter += fmt.Sprintf(` AND coalesce(oi.station_id, 0) = $%d`, len(args))
	}
	if f.SlowOnly {
		filter += ` AND ` + slow
	}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM order_items oi WHERE `+filter, args...).Scan(&out.ItemTotal); err != nil {
		return nil, err
	}
	rows, err = a.DB.Query(ctx, `SELECT oi.id, oi.ready_at, oi.name, oi.name_en, oi.options, oi.note, oi.qty, oi.station_id, oi.station_name,
			o.id, o.public_no, o.service_type, o.table_name, o.acceptance_status = 'cancelled', oi.prep_started_at,
			coalesce(sb.display_name, ''), CASE WHEN oi.prep_started_at IS NOT NULL THEN `+secs+`::int END, `+slow+`,
			oi.ready_via, coalesce(rb.display_name, ''), oi.undo_count
		FROM order_items oi JOIN orders o ON o.id = oi.order_id
			LEFT JOIN users sb ON sb.id = oi.prep_started_by LEFT JOIN users rb ON rb.id = oi.ready_by
		WHERE `+filter+fmt.Sprintf(` ORDER BY oi.ready_at DESC, oi.id DESC LIMIT %d OFFSET %d`, kitchenPageSize, (f.Page-1)*kitchenPageSize), args...)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (KitchenDish, error) {
		var d KitchenDish
		var opts []byte
		err := r.Scan(&d.ID, &d.ReadyAt, &d.Name, &d.NameEn, &opts, &d.Note, &d.Qty, &d.StationID, &d.StationName,
			&d.OrderID, &d.PublicNo, &d.ServiceType, &d.TableName, &d.Cancelled, &d.StartedAt, &d.StartedBy, &d.Seconds, &d.Slow,
			&d.Via, &d.ReadyBy, &d.UndoCount)
		if err != nil {
			return d, err
		}
		d.Options = []LineOption{}
		return d, json.Unmarshal(opts, &d.Options)
	})
	if err != nil {
		return nil, err
	}
	if items != nil {
		out.Items = items
	}
	return out, nil
}
