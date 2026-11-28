package app

import (
	"container/heap"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Restaurant mode of the queue display (docs/QUEUE_DISPLAY_DESIGN.md §6–§8):
// guests take a number for a group of tables by party size, staff call it
// (to a table or the front desk), seat it, mark it missed, put it back, or
// move it up the line. Numbers are letter + the day's sequence per group
// (A012). The system suggests who should get a table that is free; staff
// decide.

// QueueGroup is a group of tables by party size.
type QueueGroup struct {
	ID        int64   `json:"id"`
	Letter    string  `json:"letter"`
	Preset    string  `json:"preset"`
	Label     string  `json:"label"`
	LabelEn   string  `json:"label_en"`
	MinPeople int     `json:"min_people"`
	MaxPeople int     `json:"max_people"`
	TableIDs  []int64 `json:"table_ids"`
	Sort      int     `json:"sort"`
	// Tables are the tables the group uses: TableIDs, or by seats when empty.
	Tables []int64 `json:"tables"`
	// DineMinutes is how long a sitting at these tables takes (median of the
	// last 14 days); FromHistory says there were enough sittings to tell.
	DineMinutes int  `json:"dine_minutes"`
	FromHistory bool `json:"from_history"`
}

func (g *QueueGroup) holds(seats int, id int64) bool {
	if len(g.TableIDs) > 0 {
		return slices.Contains(g.TableIDs, id)
	}
	return seats >= g.MinPeople && seats <= g.MaxPeople
}

// QueueTicket is a number in the line, as staff see it.
type QueueTicket struct {
	ID       int64      `json:"id"`
	Number   string     `json:"number"`
	GroupID  int64      `json:"group_id"`
	Letter   string     `json:"letter"`
	People   int        `json:"people"`
	Source   string     `json:"source"`
	Phone    string     `json:"phone"`
	Tags     []string   `json:"tags"`
	Status   string     `json:"status"`
	Calls    int        `json:"calls"`
	CalledAt *time.Time `json:"called_at"`
	// The table it was called to (empty: the front desk) and sat at.
	CalledTableID *int64     `json:"called_table_id"`
	CalledTable   string     `json:"called_table"`
	SeatedTableID *int64     `json:"seated_table_id"`
	SeatedTable   string     `json:"seated_table"`
	SeatedAt      *time.Time `json:"seated_at"`
	SessionID     *int64     `json:"session_id"`
	JumpReason    string     `json:"jump_reason"`
	CreatedAt     time.Time  `json:"created_at"`
	ClosedAt      *time.Time `json:"closed_at"`
	// Position is the place in the group's line (1 = next), 0 when not waiting.
	Position int `json:"position"`
	// EstMinutes is the expected wait from now (waiting numbers).
	EstMinutes int `json:"est_minutes"`
	// Token opens the guest's page; staff need it to print the paper slip.
	Token string `json:"token,omitempty"`
	nonce []byte
	rank  float64
}

func ticketNumber(letter string, seq int) string { return fmt.Sprintf("%s%03d", letter, seq) }

// QueueTable is a table as the queue page sees it.
type QueueTable struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Area  string `json:"area"`
	Seats int    `json:"seats"`
	// GroupID is the first group the table belongs to (0: none).
	GroupID int64 `json:"group_id"`
	// State is free, busy or held (a called number is on its way).
	State    string     `json:"state"`
	HeldBy   string     `json:"held_by,omitempty"`
	OpenedAt *time.Time `json:"opened_at,omitempty"`
	Guests   int        `json:"guests,omitempty"`
	// Soon: busy for longer than the group's usual sitting.
	Soon bool `json:"soon,omitempty"`
}

// QueueSuggestion is who should get a free table.
type QueueSuggestion struct {
	TableID  int64 `json:"table_id"`
	TicketID int64 `json:"ticket_id"`
	// Upgrade: a smaller party, since nobody waits for this size.
	Upgrade bool `json:"upgrade"`
}

// LineStats are the numbers at the top of the page.
type LineStats struct {
	Waiting   int            `json:"waiting"`
	Called    int            `json:"called"`
	Seated    int            `json:"seated"`
	Skipped   int            `json:"skipped"`
	Cancelled int            `json:"cancelled"`
	AvgWait   int            `json:"avg_wait"`
	BySource  map[string]int `json:"by_source"`
}

// QueueDesk is the staff page.
type QueueDesk struct {
	Settings    QueueSettings     `json:"settings"`
	Groups      []QueueGroup      `json:"groups"`
	Tickets     []QueueTicket     `json:"tickets"`
	Tables      []QueueTable      `json:"tables"`
	Suggestions []QueueSuggestion `json:"suggestions"`
	Stats       LineStats         `json:"stats"`
	Timezone    string            `json:"timezone"`
	Now         time.Time         `json:"now"`
}

var errQueueClosed = Err(http.StatusConflict, "QUEUE_CLOSED", "the store is not taking numbers now")

// ---- groups ----

// defaultGroups follows the store's tables: small (1–2), medium (3–4),
// large (5–8) and, when there are tables of 9 or more, a room group.
func defaultGroups(seats []int) []QueueGroup {
	out := []QueueGroup{
		{Letter: "A", Preset: "small", MinPeople: 1, MaxPeople: 2, Sort: 1},
		{Letter: "B", Preset: "medium", MinPeople: 3, MaxPeople: 4, Sort: 2},
		{Letter: "C", Preset: "large", MinPeople: 5, MaxPeople: 8, Sort: 3},
	}
	if slices.ContainsFunc(seats, func(s int) bool { return s >= 9 }) {
		out = append(out, QueueGroup{Letter: "D", Preset: "room", MinPeople: 9, MaxPeople: 99, Sort: 4})
	}
	return out
}

// ensureGroups creates the default groups the first time a store's queue
// is used.
func ensureGroups(ctx context.Context, tx pgx.Tx, branchID int64) error {
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM queue_groups WHERE branch_id=$1 AND active`, branchID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7070, $1::int)`, branchID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM queue_groups WHERE branch_id=$1 AND active`, branchID).Scan(&n); err != nil || n > 0 {
		return err
	}
	seats := []int{}
	rows, err := tx.Query(ctx, `SELECT seats FROM dining_tables WHERE branch_id=$1 AND active`, branchID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var s int
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return err
		}
		seats = append(seats, s)
	}
	rows.Close()
	for _, g := range defaultGroups(seats) {
		if _, err := tx.Exec(ctx, `INSERT INTO queue_groups (branch_id, letter, preset, min_people, max_people, sort)
			VALUES ($1,$2,$3,$4,$5,$6)`, branchID, g.Letter, g.Preset, g.MinPeople, g.MaxPeople, g.Sort); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) withGroups(ctx context.Context, branchID int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error { return ensureGroups(ctx, tx, branchID) })
}

type tableRow struct {
	id     int64
	name   string
	area   string
	seats  int
	open   *time.Time
	guests int
}

func loadTables(ctx context.Context, q Querier, branchID int64) ([]tableRow, error) {
	rows, err := q.Query(ctx, `SELECT t.id, t.name, t.area, t.seats, s.opened_at, COALESCE(s.guests, 0)
		FROM dining_tables t LEFT JOIN dining_sessions s ON s.table_id = t.id AND s.status = 'open'
		WHERE t.branch_id=$1 AND t.active ORDER BY t.sort, t.name, t.id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []tableRow{}
	for rows.Next() {
		var t tableRow
		if err := rows.Scan(&t.id, &t.name, &t.area, &t.seats, &t.open, &t.guests); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// loadGroups reads the active groups with their tables and usual sitting.
func loadGroups(ctx context.Context, q Querier, branchID int64, tables []tableRow, set QueueSettings) ([]QueueGroup, error) {
	rows, err := q.Query(ctx, `SELECT id, letter, preset, label, label_en, min_people, max_people, table_ids, sort
		FROM queue_groups WHERE branch_id=$1 AND active ORDER BY sort, letter`, branchID)
	if err != nil {
		return nil, err
	}
	out := []QueueGroup{}
	for rows.Next() {
		var g QueueGroup
		if err := rows.Scan(&g.ID, &g.Letter, &g.Preset, &g.Label, &g.LabelEn, &g.MinPeople, &g.MaxPeople, &g.TableIDs, &g.Sort); err != nil {
			rows.Close()
			return nil, err
		}
		g.Tables = []int64{}
		for _, t := range tables {
			if g.holds(t.seats, t.id) {
				g.Tables = append(g.Tables, t.id)
			}
		}
		g.DineMinutes = set.DineMinutes
		out = append(out, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The usual sitting: the median of the last 14 days at the group's
	// tables, once there are 20 sittings to go by.
	for i := range out {
		g := &out[i]
		if len(g.Tables) == 0 {
			continue
		}
		var n int
		var med *float64
		if err := q.QueryRow(ctx, `SELECT count(*), percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM closed_at - opened_at))
			FROM dining_sessions s WHERE branch_id=$1 AND table_id = ANY($2) AND status='closed'
				AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.id = s.order_id AND o.acceptance_status = 'cancelled')
				AND closed_at > now() - interval '14 days' AND closed_at - opened_at BETWEEN interval '5 minutes' AND interval '6 hours'`,
			branchID, g.Tables).Scan(&n, &med); err != nil {
			return nil, err
		}
		if n >= 20 && med != nil {
			g.DineMinutes = max(10, int(*med/60+0.5))
			g.FromHistory = true
		}
	}
	return out, nil
}

func groupFor(groups []QueueGroup, people int) *QueueGroup {
	for i := range groups {
		if people >= groups[i].MinPeople && people <= groups[i].MaxPeople {
			return &groups[i]
		}
	}
	return nil
}

func groupByID(groups []QueueGroup, id int64) *QueueGroup {
	for i := range groups {
		if groups[i].ID == id {
			return &groups[i]
		}
	}
	return nil
}

// QueueGroupInput is one group as the settings page saves it.
type QueueGroupInput struct {
	ID        int64   `json:"id"`
	Letter    string  `json:"letter"`
	Preset    string  `json:"preset"`
	Label     string  `json:"label"`
	LabelEn   string  `json:"label_en"`
	MinPeople int     `json:"min_people"`
	MaxPeople int     `json:"max_people"`
	TableIDs  []int64 `json:"table_ids"`
}

var letterRe = regexp.MustCompile(`^[A-Z]$`)

// SaveQueueGroups replaces the store's groups. A group left out is retired:
// numbers already taken for it keep it.
func (a *App) SaveQueueGroups(ctx context.Context, branchID int64, in []QueueGroupInput, actor Actor) ([]QueueGroup, error) {
	if len(in) == 0 || len(in) > 10 {
		return nil, ErrValidation("1 to 10 groups")
	}
	seen := map[string]bool{}
	for i := range in {
		g := &in[i]
		g.Letter = strings.ToUpper(strings.TrimSpace(g.Letter))
		g.Label, g.LabelEn = strings.TrimSpace(g.Label), strings.TrimSpace(g.LabelEn)
		if !letterRe.MatchString(g.Letter) || seen[g.Letter] {
			return nil, ErrValidation("each group needs its own letter A–Z")
		}
		seen[g.Letter] = true
		if g.MinPeople < 1 || g.MaxPeople > 99 || g.MinPeople > g.MaxPeople {
			return nil, ErrValidation("people must be 1 to 99, the least not above the most")
		}
		if len([]rune(g.Label)) > 20 || len([]rune(g.LabelEn)) > 30 {
			return nil, ErrValidation("group name too long")
		}
		switch g.Preset {
		case "", "small", "medium", "large", "room":
		default:
			g.Preset = ""
		}
		if g.TableIDs == nil {
			g.TableIDs = []int64{}
		}
	}
	// Two groups may not both take the same party size: the number would be ambiguous.
	for i := range in {
		for j := i + 1; j < len(in); j++ {
			if in[i].MinPeople <= in[j].MaxPeople && in[j].MinPeople <= in[i].MaxPeople {
				return nil, ErrValidation("two groups take the same party size")
			}
		}
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7070, $1::int)`, branchID); err != nil {
			return err
		}
		// The rows are replaced: retire every active group (the letters are
		// free again), add the new list, and move the numbers in line over.
		if _, err := tx.Exec(ctx, `UPDATE queue_groups SET active=false WHERE branch_id=$1 AND active`, branchID); err != nil {
			return err
		}
		for i, g := range in {
			var id int64
			if err := tx.QueryRow(ctx, `INSERT INTO queue_groups (branch_id, letter, preset, label, label_en, min_people, max_people, table_ids, sort)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, branchID, g.Letter, g.Preset, g.Label, g.LabelEn,
				g.MinPeople, g.MaxPeople, g.TableIDs, i+1).Scan(&id); err != nil {
				return err
			}
			if g.ID != 0 {
				// Numbers waiting for the old row follow the new one.
				if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET group_id=$1 WHERE group_id=$2 AND branch_id=$3
					AND status IN ('waiting','called','skipped')`, id, g.ID, branchID); err != nil {
					return err
				}
			}
		}
		if err := audit(ctx, tx, branchID, 0, actor, "queue.groups", in); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID})
	})
	if err != nil {
		return nil, err
	}
	set, err := queueSettings(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	tables, err := loadTables(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	return loadGroups(ctx, a.DB, branchID, tables, set)
}

// ---- the day ----

func (b *Branch) bizDate(t time.Time) string { return t.In(b.location()).Format("2006-01-02") }

// voidOldTickets ends yesterday's numbers that were never seated.
func voidOldTickets(ctx context.Context, q Querier, branchID int64, today string) error {
	_, err := q.Exec(ctx, `UPDATE queue_tickets SET status='void', closed_at=now()
		WHERE branch_id=$1 AND biz_date < $2::date AND status IN ('waiting','called','skipped')`, branchID, today)
	return err
}

const ticketCols = `t.id, t.letter, t.seq, t.group_id, t.people, t.source, t.phone, t.tags, t.status, t.calls, t.called_at,
	t.called_table_id, COALESCE(ct.name, ''), t.seated_table_id, COALESCE(st.name, ''), t.seated_at, t.session_id,
	t.jump_reason, t.created_at, t.closed_at, t.token_nonce, t.rank`

const ticketFrom = ` FROM queue_tickets t LEFT JOIN dining_tables ct ON ct.id = t.called_table_id
	LEFT JOIN dining_tables st ON st.id = t.seated_table_id `

func scanTicket(r pgx.Row) (QueueTicket, error) {
	var t QueueTicket
	var seq int
	err := r.Scan(&t.ID, &t.Letter, &seq, &t.GroupID, &t.People, &t.Source, &t.Phone, &t.Tags, &t.Status, &t.Calls, &t.CalledAt,
		&t.CalledTableID, &t.CalledTable, &t.SeatedTableID, &t.SeatedTable, &t.SeatedAt, &t.SessionID,
		&t.JumpReason, &t.CreatedAt, &t.ClosedAt, &t.nonce, &t.rank)
	t.Number = ticketNumber(t.Letter, seq)
	if t.Tags == nil {
		t.Tags = []string{}
	}
	return t, err
}

func todayTickets(ctx context.Context, q Querier, branchID int64, today string) ([]QueueTicket, error) {
	rows, err := q.Query(ctx, `SELECT `+ticketCols+ticketFrom+`WHERE t.branch_id=$1 AND t.biz_date=$2::date ORDER BY t.rank, t.id`,
		branchID, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueTicket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- the picture of the queue ----

// queueState is today's line worked out: positions, estimates, table states
// and suggestions. The staff page, the public pages and the screens all read it.
type queueState struct {
	branch   *Branch
	set      QueueSettings
	groups   []QueueGroup
	tables   []QueueTable
	tickets  []QueueTicket
	suggest  []QueueSuggestion
	now      time.Time
	estimate map[int64]int // group -> wait for a number taken now
}

func (a *App) queueState(ctx context.Context, branchID int64) (*queueState, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	set, err := queueSettings(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	if err := a.withGroups(ctx, branchID); err != nil {
		return nil, err
	}
	now := time.Now()
	today := b.bizDate(now)
	if err := voidOldTickets(ctx, a.DB, branchID, today); err != nil {
		return nil, err
	}
	rows, err := loadTables(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	groups, err := loadGroups(ctx, a.DB, branchID, rows, set)
	if err != nil {
		return nil, err
	}
	tickets, err := todayTickets(ctx, a.DB, branchID, today)
	if err != nil {
		return nil, err
	}
	st := &queueState{branch: b, set: set, groups: groups, tickets: tickets, now: now, estimate: map[int64]int{}}
	held := map[int64]string{}
	for _, t := range tickets {
		if t.Status == "called" && t.CalledTableID != nil {
			held[*t.CalledTableID] = t.Number
		}
	}
	for _, r := range rows {
		qt := QueueTable{ID: r.id, Name: r.name, Area: r.area, Seats: r.seats, State: "free"}
		for _, g := range groups {
			if slices.Contains(g.Tables, r.id) {
				qt.GroupID = g.ID
				break
			}
		}
		switch {
		case r.open != nil:
			qt.State, qt.OpenedAt, qt.Guests = "busy", r.open, r.guests
			if g := groupByID(groups, qt.GroupID); g != nil && now.Sub(*r.open) >= time.Duration(g.DineMinutes)*time.Minute {
				qt.Soon = true
			} else if g == nil && now.Sub(*r.open) >= time.Duration(set.DineMinutes)*time.Minute {
				qt.Soon = true
			}
		case held[r.id] != "":
			qt.State, qt.HeldBy = "held", held[r.id]
		}
		st.tables = append(st.tables, qt)
	}
	if st.tables == nil {
		st.tables = []QueueTable{}
	}
	st.positions()
	st.suggestions()
	return st, nil
}

// minHeap of minutes until a table is free.
type minHeap []float64

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(float64)) }
func (h *minHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// positions numbers each group's line and estimates the waits: every table
// of the group frees up after the usual sitting (a free table now, a busy
// one when its sitting has run its usual time, a held one after a whole
// sitting) and takes the next number in line.
func (st *queueState) positions() {
	for gi := range st.groups {
		g := &st.groups[gi]
		dine := float64(g.DineMinutes)
		h := &minHeap{}
		for _, t := range st.tables {
			if !slices.Contains(g.Tables, t.ID) {
				continue
			}
			switch t.State {
			case "free":
				*h = append(*h, 0)
			case "held":
				*h = append(*h, dine)
			default:
				*h = append(*h, max(0, dine-st.now.Sub(*t.OpenedAt).Minutes()))
			}
		}
		if h.Len() == 0 {
			*h = append(*h, dine) // no tables of this size: staff fit them somewhere
		}
		heap.Init(h)
		next := func() int {
			m := heap.Pop(h).(float64)
			heap.Push(h, m+dine)
			return int(m + 0.5)
		}
		pos := 0
		for i := range st.tickets {
			t := &st.tickets[i]
			if t.GroupID != g.ID || t.Status != "waiting" {
				continue
			}
			pos++
			t.Position = pos
			t.EstMinutes = next()
		}
		st.estimate[g.ID] = next()
	}
}

// suggestions finds, for each free table, the number that should get it
// (§6.3): the head of its group's line; when nobody waits for the group,
// the smaller party that has waited longest, if past the upgrade time.
func (st *queueState) suggestions() {
	st.suggest = []QueueSuggestion{}
	used := map[int64]bool{}
	upgradeAfter := time.Duration(st.set.UpgradeMinutes) * time.Minute
	for _, tb := range st.tables {
		if tb.State != "free" {
			continue
		}
		g := groupByID(st.groups, tb.GroupID)
		var pick *QueueTicket
		if g != nil {
			for i := range st.tickets {
				t := &st.tickets[i]
				if t.GroupID == g.ID && t.Status == "waiting" && !used[t.ID] {
					pick = t
					break
				}
			}
			if pick == nil && slices.ContainsFunc(st.tickets, func(t QueueTicket) bool {
				return t.GroupID == g.ID && t.Status == "waiting"
			}) {
				continue // the group's numbers already have tables suggested
			}
		}
		if pick != nil {
			used[pick.ID] = true
			st.suggest = append(st.suggest, QueueSuggestion{TableID: tb.ID, TicketID: pick.ID})
			continue
		}
		// Upgrade: a smaller group's longest wait that fits and waited long enough.
		var best *QueueTicket
		for i := range st.tickets {
			t := &st.tickets[i]
			if t.Status != "waiting" || used[t.ID] || t.People > tb.Seats {
				continue
			}
			tg := groupByID(st.groups, t.GroupID)
			if tg == nil || (g != nil && tg.MaxPeople >= g.MinPeople) || st.now.Sub(t.CreatedAt) < upgradeAfter {
				continue
			}
			if best == nil || t.CreatedAt.Before(best.CreatedAt) {
				best = t
			}
		}
		if best != nil {
			used[best.ID] = true
			st.suggest = append(st.suggest, QueueSuggestion{TableID: tb.ID, TicketID: best.ID, Upgrade: true})
		}
	}
}

// QueueDesk is the staff page.
func (a *App) QueueDesk(ctx context.Context, branchID int64) (*QueueDesk, error) {
	st, err := a.queueState(ctx, branchID)
	if err != nil {
		return nil, err
	}
	out := &QueueDesk{Settings: st.set, Groups: st.groups, Tables: st.tables, Suggestions: st.suggest,
		Timezone: st.branch.Timezone, Now: st.now, Tickets: []QueueTicket{},
		Stats: LineStats{BySource: map[string]int{"qr": 0, "paper": 0, "staff": 0}}}
	var waitSum, waitN int
	for _, t := range st.tickets {
		switch t.Status {
		case "waiting":
			out.Stats.Waiting++
		case "called":
			out.Stats.Called++
		case "seated":
			out.Stats.Seated++
			out.Stats.BySource[t.Source]++
			if t.SeatedAt != nil {
				waitSum += int(t.SeatedAt.Sub(t.CreatedAt).Minutes())
				waitN++
			}
		case "skipped":
			out.Stats.Skipped++
		case "cancelled", "void":
			out.Stats.Cancelled++
		}
		switch t.Status {
		case "waiting", "called", "skipped":
			t.Token = a.ticketToken(t.nonce)
			out.Tickets = append(out.Tickets, t)
		}
	}
	if waitN > 0 {
		out.Stats.AvgWait = (waitSum + waitN/2) / waitN
	}
	return out, nil
}

func (a *App) ticketToken(nonce []byte) string { return a.derive("queue-ticket:", nonce) }

// ---- taking a number ----

// TakeTicketInput is a new number: from the guest's phone (qr) or the desk
// (paper: printed slip, staff: entered for a guest).
type TakeTicketInput struct {
	People int      `json:"people"`
	Phone  string   `json:"phone"`
	Tags   []string `json:"tags"`
	Source string   `json:"source"`
	// Priority puts the number first in line (desk only, needs queue.jump).
	Priority bool   `json:"priority"`
	Reason   string `json:"reason"`
	Lang     string `json:"lang"`
	// Client is the guest's browser id (one number at a time per browser).
	Client string `json:"client"`
}

var queuePhoneRe = regexp.MustCompile(`^\+?[0-9]{6,16}$`)

// TakeTicket gives out a number.
func (a *App) TakeTicket(ctx context.Context, branchID int64, in TakeTicketInput, actor Actor) (*QueueTicket, error) {
	if in.People < 1 || in.People > 99 {
		return nil, ErrValidation("people must be 1 to 99")
	}
	in.Phone = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(in.Phone))
	if in.Phone != "" && !queuePhoneRe.MatchString(in.Phone) {
		return nil, ErrValidation("phone number looks wrong")
	}
	tags, err := cleanTags(in.Tags)
	if err != nil {
		return nil, err
	}
	switch in.Source {
	case "qr", "paper", "staff":
	default:
		return nil, ErrValidation("source must be qr, paper or staff")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Priority && in.Reason == "" {
		return nil, ErrValidation("a reason is needed to put a number first")
	}
	if !IsLang(in.Lang) {
		in.Lang = ""
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	set, err := queueSettings(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	if !set.Restaurant {
		return nil, errQueueClosed
	}
	if in.Source == "qr" && (set.Paused || b.Suspended) {
		return nil, errQueueClosed
	}
	if err := a.withGroups(ctx, branchID); err != nil {
		return nil, err
	}
	now := time.Now()
	today := b.bizDate(now)
	var clientHash []byte
	if in.Client != "" {
		clientHash = hashToken("queue-client:" + in.Client)
	}
	var id int64
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		// One line per store and day: take the store's lock.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7071, $1::int)`, branchID); err != nil {
			return err
		}
		tables, err := loadTables(ctx, tx, branchID)
		if err != nil {
			return err
		}
		groups, err := loadGroups(ctx, tx, branchID, tables, set)
		if err != nil {
			return err
		}
		g := groupFor(groups, in.People)
		if g == nil {
			return Err(http.StatusUnprocessableEntity, "QUEUE_NO_GROUP", "no tables for this party size; ask the staff")
		}
		if in.Source == "qr" {
			var busy bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM queue_tickets WHERE branch_id=$1 AND biz_date=$2::date
				AND status IN ('waiting','called') AND ((client_hash IS NOT NULL AND client_hash = $3) OR ($4 <> '' AND phone = $4)))`,
				branchID, today, clientHash, in.Phone).Scan(&busy); err != nil {
				return err
			}
			if busy {
				return Err(http.StatusConflict, "QUEUE_HAS_TICKET", "you already have a number in this line")
			}
		}
		var seq int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) + 1 FROM queue_tickets WHERE branch_id=$1 AND biz_date=$2::date AND letter=$3`,
			branchID, today, g.Letter).Scan(&seq); err != nil {
			return err
		}
		rank := float64(now.UnixMilli()) / 1000
		if in.Priority {
			var first *float64
			if err := tx.QueryRow(ctx, `SELECT min(rank) FROM queue_tickets WHERE group_id=$1 AND biz_date=$2::date AND status='waiting'`,
				g.ID, today).Scan(&first); err != nil {
				return err
			}
			if first != nil {
				rank = *first - 1
			}
		}
		nonce := randomBytes(16)
		var creator *int64
		if actor.Type == "staff" && actor.ID != 0 {
			creator = &actor.ID
		}
		jump := ""
		if in.Priority {
			jump = in.Reason
		}
		if err := tx.QueryRow(ctx, `INSERT INTO queue_tickets (branch_id, biz_date, group_id, letter, seq, people, source, phone, tags,
			rank, jump_reason, lang, token_hash, token_nonce, client_hash, created_by)
			VALUES ($1,$2::date,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`,
			branchID, today, g.ID, g.Letter, seq, in.People, in.Source, in.Phone, tags, rank, jump, in.Lang,
			hashToken(a.ticketToken(nonce)), nonce, clientHash, creator).Scan(&id); err != nil {
			return err
		}
		detail := map[string]any{"people": in.People, "source": in.Source}
		if in.Priority {
			detail["priority"], detail["reason"] = true, in.Reason
		}
		if err := queueEvent(ctx, tx, branchID, id, actor, "take", detail); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID})
	})
	if err != nil {
		return nil, err
	}
	return a.deskTicket(ctx, branchID, id)
}

func queueEvent(ctx context.Context, tx pgx.Tx, branchID, ticketID int64, actor Actor, action string, detail any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	var by *int64
	if actor.Type == "staff" && actor.ID != 0 {
		by = &actor.ID
	}
	_, err := tx.Exec(ctx, `INSERT INTO queue_events (branch_id, ticket_id, action, actor_id, detail) VALUES ($1,$2,$3,$4,$5)`,
		branchID, ticketID, action, by, detail)
	return err
}

// deskTicket is one number with its place and estimate, as staff see it.
func (a *App) deskTicket(ctx context.Context, branchID, id int64) (*QueueTicket, error) {
	st, err := a.queueState(ctx, branchID)
	if err != nil {
		return nil, err
	}
	for _, t := range st.tickets {
		if t.ID == id {
			t.Token = a.ticketToken(t.nonce)
			return &t, nil
		}
	}
	// Not today's (or gone): read it plainly.
	t, err := scanTicket(a.DB.QueryRow(ctx, `SELECT `+ticketCols+ticketFrom+`WHERE t.id=$1 AND t.branch_id=$2`, id, branchID))
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Token = a.ticketToken(t.nonce)
	return &t, nil
}

// ---- staff actions ----

// QueueAction is a step staff take on a number.
type QueueAction struct {
	// TableID: call to this table (0 = the front desk), or seat here.
	TableID int64 `json:"table_id"`
	// Force seats at a table held for another number.
	Force bool `json:"force"`
	// Position (1 = first) and Reason, for moving a number up the line.
	Position int    `json:"position"`
	Reason   string `json:"reason"`
	People   int    `json:"people"`
}

type lockedTicket struct {
	QueueTicket
	branchID int64
	bizDate  string
	lang     string
}

func lockTicket(ctx context.Context, tx pgx.Tx, branchID, id int64) (*lockedTicket, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7071, $1::int)`, branchID); err != nil {
		return nil, err
	}
	var lt lockedTicket
	var d time.Time
	row := tx.QueryRow(ctx, `SELECT `+ticketCols+`, t.branch_id, t.biz_date, t.lang`+ticketFrom+`WHERE t.id=$1 AND t.branch_id=$2 FOR UPDATE OF t`, id, branchID)
	var seq int
	err := row.Scan(&lt.ID, &lt.Letter, &seq, &lt.GroupID, &lt.People, &lt.Source, &lt.Phone, &lt.Tags, &lt.Status, &lt.Calls, &lt.CalledAt,
		&lt.CalledTableID, &lt.CalledTable, &lt.SeatedTableID, &lt.SeatedTable, &lt.SeatedAt, &lt.SessionID,
		&lt.JumpReason, &lt.CreatedAt, &lt.ClosedAt, &lt.nonce, &lt.rank, &lt.branchID, &d, &lt.lang)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	lt.Number = ticketNumber(lt.Letter, seq)
	lt.bizDate = d.Format("2006-01-02")
	return &lt, nil
}

func (a *App) ticketStep(ctx context.Context, branchID, id int64, fn func(tx pgx.Tx, t *lockedTicket) error) (*QueueTicket, error) {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		t, err := lockTicket(ctx, tx, branchID, id)
		if err != nil {
			return err
		}
		if err := fn(tx, t); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID})
	})
	if err != nil {
		return nil, err
	}
	return a.deskTicket(ctx, branchID, id)
}

func tableOf(ctx context.Context, tx pgx.Tx, branchID, tableID int64) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM dining_tables WHERE id=$1 AND branch_id=$2 AND active`, tableID, branchID).Scan(&name)
	if isNoRows(err) {
		return "", ErrValidation("table not found")
	}
	return name, err
}

// CallTicket calls a number, to a table (held for it) or to the front desk.
// Calling it again ("重叫") does the same once more.
func (a *App) CallTicket(ctx context.Context, branchID, id int64, in QueueAction, actor Actor) (*QueueTicket, error) {
	return a.ticketStep(ctx, branchID, id, func(tx pgx.Tx, t *lockedTicket) error {
		if t.Status != "waiting" && t.Status != "called" && t.Status != "skipped" {
			return ErrInvalidState("this number is no longer in line")
		}
		var table *int64
		place := ""
		if in.TableID != 0 {
			name, err := tableOf(ctx, tx, branchID, in.TableID)
			if err != nil {
				return err
			}
			table, place = &in.TableID, name
		} else if t.Status == "called" && t.CalledTableID != nil {
			table, place = t.CalledTableID, t.CalledTable // calling again keeps the table
		}
		if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET status='called', calls=calls+1, called_at=now(), called_table_id=$2
			WHERE id=$1`, t.ID, table); err != nil {
			return err
		}
		action := "call"
		if t.Status == "called" {
			action = "recall"
		}
		if err := queueEvent(ctx, tx, branchID, t.ID, actor, action, map[string]any{"table": place}); err != nil {
			return err
		}
		return a.textCalled(ctx, tx, branchID, &t.QueueTicket, t.lang)
	})
}

// textCalled sends the guest an SMS when the store chose to (§6.5).
func (a *App) textCalled(ctx context.Context, tx pgx.Tx, branchID int64, t *QueueTicket, lang string) error {
	if t.Phone == "" {
		return nil
	}
	set, err := queueSettings(ctx, tx, branchID)
	if err != nil || !set.SMS {
		return err
	}
	store := ""
	if err := tx.QueryRow(ctx, `SELECT name FROM branches WHERE id=$1`, branchID).Scan(&store); err != nil {
		return err
	}
	if lang == "" {
		lang = "en"
	}
	if names, err := storeNames(ctx, tx, lang, []int64{branchID}); err == nil && names[branchID] != "" {
		store = names[branchID]
	}
	msg := a.sysTextIn(ctx, tx, lang, "sys.sms.queue_called", "store", store, "no", t.Number)
	if a.PublicBaseURL != "" {
		msg += " " + strings.TrimRight(a.PublicBaseURL, "/") + "/q/t/" + a.ticketToken(t.nonce)
	}
	return enqueue(ctx, tx, "sms", smsJob{Phone: t.Phone, Message: msg})
}

// SeatTicket opens the table for the number: the sitting starts with the
// party's size and tags, like opening it on the dine-in desk.
func (a *App) SeatTicket(ctx context.Context, branchID, id int64, in QueueAction, actor Actor) (*QueueTicket, *Bill, error) {
	if in.TableID == 0 {
		return nil, nil, ErrValidation("choose a table")
	}
	var people int
	var tags []string
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		t, err := lockTicket(ctx, tx, branchID, id)
		if err != nil {
			return err
		}
		if t.Status != "waiting" && t.Status != "called" && t.Status != "skipped" {
			return ErrInvalidState("this number is no longer in line")
		}
		if _, err := tableOf(ctx, tx, branchID, in.TableID); err != nil {
			return err
		}
		var holder string
		if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT letter || lpad(seq::text, 3, '0') FROM queue_tickets
			WHERE called_table_id=$1 AND status='called' AND id <> $2 AND biz_date=$3::date LIMIT 1), '')`,
			in.TableID, t.ID, t.bizDate).Scan(&holder); err != nil {
			return err
		}
		if holder != "" && !in.Force {
			return ErrD(http.StatusConflict, "TABLE_HELD", "this table is held for another number", map[string]string{"number": holder})
		}
		people, tags = t.People, t.Tags
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	bill, err := a.OpenTable(ctx, in.TableID, people, tags, actor)
	if err != nil {
		return nil, nil, err
	}
	tk, err := a.ticketStep(ctx, branchID, id, func(tx pgx.Tx, t *lockedTicket) error {
		if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET status='seated', seated_at=now(), seated_table_id=$2, session_id=$3,
			closed_at=now() WHERE id=$1`, t.ID, in.TableID, bill.SessionID); err != nil {
			return err
		}
		// Another number held at this table (seated anyway) goes back to the desk.
		if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET called_table_id=NULL WHERE called_table_id=$1 AND status='called' AND id <> $2`,
			in.TableID, t.ID); err != nil {
			return err
		}
		return queueEvent(ctx, tx, branchID, t.ID, actor, "seat", map[string]any{"table": bill.TableName, "session_id": bill.SessionID})
	})
	return tk, bill, err
}

// SkipTicket marks a called number as missed (§6.5).
func (a *App) SkipTicket(ctx context.Context, branchID, id int64, actor Actor) (*QueueTicket, error) {
	return a.ticketStep(ctx, branchID, id, func(tx pgx.Tx, t *lockedTicket) error {
		if t.Status != "waiting" && t.Status != "called" {
			return ErrInvalidState("only a number in line can be missed")
		}
		if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET status='skipped', called_table_id=NULL, closed_at=now() WHERE id=$1`, t.ID); err != nil {
			return err
		}
		return queueEvent(ctx, tx, branchID, t.ID, actor, "skip", nil)
	})
}

// placeAt gives a number the rank that puts it at pos (1 = first) in its
// group's line, not counting itself.
func placeAt(ctx context.Context, tx pgx.Tx, t *lockedTicket, pos int) (int, error) {
	rows, err := tx.Query(ctx, `SELECT rank FROM queue_tickets WHERE group_id=$1 AND biz_date=$2::date AND status='waiting' AND id <> $3
		ORDER BY rank, id`, t.GroupID, t.bizDate, t.ID)
	if err != nil {
		return 0, err
	}
	ranks := []float64{}
	for rows.Next() {
		var r float64
		if err := rows.Scan(&r); err != nil {
			rows.Close()
			return 0, err
		}
		ranks = append(ranks, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	pos = min(max(pos, 1), len(ranks)+1)
	var rank float64
	switch {
	case len(ranks) == 0:
		rank = float64(time.Now().UnixMilli()) / 1000
	case pos == 1:
		rank = ranks[0] - 1
	case pos > len(ranks):
		rank = ranks[len(ranks)-1] + 1
	default:
		rank = (ranks[pos-2] + ranks[pos-1]) / 2
	}
	_, err = tx.Exec(ctx, `UPDATE queue_tickets SET rank=$2 WHERE id=$1`, t.ID, rank)
	return pos, err
}

// RequeueTicket puts a missed number back: behind the head of its group's
// line by the store's setting (§6.5).
func (a *App) RequeueTicket(ctx context.Context, branchID, id int64, actor Actor) (*QueueTicket, error) {
	set, err := queueSettings(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	return a.ticketStep(ctx, branchID, id, func(tx pgx.Tx, t *lockedTicket) error {
		if t.Status != "skipped" {
			return ErrInvalidState("only a missed number can be put back")
		}
		if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET status='waiting', called_table_id=NULL, closed_at=NULL WHERE id=$1`, t.ID); err != nil {
			return err
		}
		pos, err := placeAt(ctx, tx, t, set.RequeueAfter+1)
		if err != nil {
			return err
		}
		return queueEvent(ctx, tx, branchID, t.ID, actor, "requeue", map[string]any{"position": pos})
	})
}

// JumpTicket moves a number up (or down) its group's line, with a reason
// (§6.6). The number stays the same.
func (a *App) JumpTicket(ctx context.Context, branchID, id int64, in QueueAction, actor Actor) (*QueueTicket, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		return nil, ErrValidation("a reason is needed")
	}
	if len([]rune(in.Reason)) > 60 {
		return nil, ErrValidation("reason too long")
	}
	return a.ticketStep(ctx, branchID, id, func(tx pgx.Tx, t *lockedTicket) error {
		if t.Status != "waiting" {
			return ErrInvalidState("only a waiting number can be moved")
		}
		var from int
		if err := tx.QueryRow(ctx, `SELECT count(*) + 1 FROM queue_tickets WHERE group_id=$1 AND biz_date=$2::date AND status='waiting'
			AND (rank < $3 OR (rank = $3 AND id < $4))`, t.GroupID, t.bizDate, t.rank, t.ID).Scan(&from); err != nil {
			return err
		}
		pos, err := placeAt(ctx, tx, t, in.Position)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET jump_reason=$2 WHERE id=$1`, t.ID, in.Reason); err != nil {
			return err
		}
		return queueEvent(ctx, tx, branchID, t.ID, actor, "jump", map[string]any{"from": from, "to": pos, "reason": in.Reason})
	})
}

// ChangePeople corrects the party size; the number moves to the group of
// the new size and keeps its place in time.
func (a *App) ChangePeople(ctx context.Context, branchID, id int64, in QueueAction, actor Actor) (*QueueTicket, error) {
	if in.People < 1 || in.People > 99 {
		return nil, ErrValidation("people must be 1 to 99")
	}
	set, err := queueSettings(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	return a.ticketStep(ctx, branchID, id, func(tx pgx.Tx, t *lockedTicket) error {
		if t.Status != "waiting" && t.Status != "called" && t.Status != "skipped" {
			return ErrInvalidState("this number is no longer in line")
		}
		tables, err := loadTables(ctx, tx, branchID)
		if err != nil {
			return err
		}
		groups, err := loadGroups(ctx, tx, branchID, tables, set)
		if err != nil {
			return err
		}
		g := groupFor(groups, in.People)
		if g == nil {
			return Err(http.StatusUnprocessableEntity, "QUEUE_NO_GROUP", "no tables for this party size")
		}
		if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET people=$2, group_id=$3,
			rank = CASE WHEN group_id = $3 THEN rank ELSE extract(epoch FROM created_at) END WHERE id=$1`,
			t.ID, in.People, g.ID); err != nil {
			return err
		}
		return queueEvent(ctx, tx, branchID, t.ID, actor, "people", map[string]any{"from": t.People, "to": in.People})
	})
}

// CancelTicket ends a number (the guest is not coming).
func (a *App) CancelTicket(ctx context.Context, branchID, id int64, actor Actor) (*QueueTicket, error) {
	return a.ticketStep(ctx, branchID, id, func(tx pgx.Tx, t *lockedTicket) error {
		if t.Status != "waiting" && t.Status != "called" && t.Status != "skipped" {
			return ErrInvalidState("this number is no longer in line")
		}
		if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET status='cancelled', called_table_id=NULL, closed_at=now() WHERE id=$1`, t.ID); err != nil {
			return err
		}
		return queueEvent(ctx, tx, branchID, t.ID, actor, "cancel", nil)
	})
}

// ---- the log ----

// QueueLogEntry is one step of one number.
type QueueLogEntry struct {
	ID       int64          `json:"id"`
	TicketID int64          `json:"ticket_id"`
	Number   string         `json:"number"`
	People   int            `json:"people"`
	Action   string         `json:"action"`
	Actor    string         `json:"actor"`
	Detail   map[string]any `json:"detail"`
	At       time.Time      `json:"at"`
}

// QueueLog lists a day's steps, newest first (YYYY-MM-DD on the store's
// clock; empty for today).
func (a *App) QueueLog(ctx context.Context, branchID int64, date string) ([]QueueLogEntry, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	if date == "" {
		date = b.bizDate(time.Now())
	} else if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, ErrValidation("date must be YYYY-MM-DD")
	}
	rows, err := a.DB.Query(ctx, `SELECT e.id, t.id, t.letter, t.seq, t.people, e.action,
		COALESCE((SELECT u.display_name FROM users u WHERE u.id = e.actor_id), ''), e.detail, e.created_at
		FROM queue_events e JOIN queue_tickets t ON t.id = e.ticket_id
		WHERE e.branch_id=$1 AND t.biz_date=$2::date ORDER BY e.id DESC LIMIT 1000`, branchID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueLogEntry{}
	for rows.Next() {
		var e QueueLogEntry
		var letter string
		var seq int
		if err := rows.Scan(&e.ID, &e.TicketID, &letter, &seq, &e.People, &e.Action, &e.Actor, &e.Detail, &e.At); err != nil {
			return nil, err
		}
		e.Number = ticketNumber(letter, seq)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- what guests and screens see ----

// QueuePublicGroup is one group on the screen and the guest's page.
type QueuePublicGroup struct {
	ID        int64    `json:"id"`
	Letter    string   `json:"letter"`
	Preset    string   `json:"preset"`
	Label     string   `json:"label"`
	LabelEn   string   `json:"label_en"`
	MinPeople int      `json:"min_people"`
	MaxPeople int      `json:"max_people"`
	Waiting   int      `json:"waiting"`
	EstMin    int      `json:"est_minutes"`
	Current   string   `json:"current"`
	Next      []string `json:"next"`
}

// QueueCall is a number being called.
type QueueCall struct {
	Number string    `json:"number"`
	People int       `json:"people"`
	Table  string    `json:"table"`
	Calls  int       `json:"calls"`
	At     time.Time `json:"at"`
}

// QueuePublic is the line without anyone's details.
type QueuePublic struct {
	Slug     string             `json:"slug"`
	Store    string             `json:"store"`
	StoreEn  string             `json:"store_en"`
	Logo     string             `json:"logo"`
	Open     bool               `json:"open"`
	Paused   bool               `json:"paused"`
	Groups   []QueuePublicGroup `json:"groups"`
	Calling  []QueueCall        `json:"calling"`
	Skipped  []string           `json:"skipped"`
	Settings struct {
		CallAlertMinutes int `json:"call_alert_minutes"`
		RequeueAfter     int `json:"requeue_after"`
	} `json:"settings"`
	Now time.Time `json:"now"`
}

func (a *App) publicQueue(ctx context.Context, branchID int64, screen bool) (*QueuePublic, error) {
	st, err := a.queueState(ctx, branchID)
	if err != nil {
		return nil, err
	}
	return st.public(screen), nil
}

func (st *queueState) public(screen bool) *QueuePublic {
	out := &QueuePublic{Slug: st.branch.Slug, Store: st.branch.Name, StoreEn: st.branch.NameEn, Open: st.set.Restaurant,
		Paused: st.set.Paused || st.branch.Suspended, Groups: []QueuePublicGroup{}, Calling: []QueueCall{}, Skipped: []string{}, Now: st.now}
	out.Settings.CallAlertMinutes, out.Settings.RequeueAfter = st.set.CallAlertMinutes, st.set.RequeueAfter
	for _, g := range st.groups {
		pg := QueuePublicGroup{ID: g.ID, Letter: g.Letter, Preset: g.Preset, Label: g.Label, LabelEn: g.LabelEn,
			MinPeople: g.MinPeople, MaxPeople: g.MaxPeople, EstMin: st.estimate[g.ID], Next: []string{}}
		var last *time.Time
		for _, t := range st.tickets {
			if t.GroupID != g.ID {
				continue
			}
			if t.Status == "waiting" {
				pg.Waiting++
				if len(pg.Next) < 8 {
					pg.Next = append(pg.Next, t.Number)
				}
			}
			if t.CalledAt != nil && (last == nil || t.CalledAt.After(*last)) {
				last, pg.Current = t.CalledAt, t.Number
			}
		}
		if pg.Waiting == 0 {
			pg.EstMin = 0
		}
		out.Groups = append(out.Groups, pg)
	}
	calls := []QueueCall{}
	for _, t := range st.tickets {
		if t.Status == "called" && t.CalledAt != nil {
			calls = append(calls, QueueCall{Number: t.Number, People: t.People, Table: t.CalledTable, Calls: t.Calls, At: *t.CalledAt})
		}
		if t.Status == "skipped" && t.ClosedAt != nil && st.now.Sub(*t.ClosedAt) < 30*time.Minute {
			out.Skipped = append(out.Skipped, t.Number)
		}
	}
	// Recently seated numbers stay in "just called" for a while on the screen.
	for _, t := range st.tickets {
		if screen && t.Status == "seated" && t.CalledAt != nil && st.now.Sub(*t.CalledAt) < 10*time.Minute {
			calls = append(calls, QueueCall{Number: t.Number, People: t.People, Table: t.SeatedTable, Calls: t.Calls, At: *t.CalledAt})
		}
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].At.After(calls[j].At) })
	if len(calls) > 4 {
		calls = calls[:4]
	}
	out.Calling = calls
	return out
}

// PublicQueue is the guest's "take a number" page.
func (a *App) PublicQueue(ctx context.Context, slug string) (*QueuePublic, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	q, err := a.publicQueue(ctx, b.ID, false)
	if err != nil {
		return nil, err
	}
	var logo string
	_ = a.DB.QueryRow(ctx, `SELECT COALESCE((SELECT published->>'logo_url' FROM channel_templates WHERE branch_id=$1 AND channel='delivery'), '')`,
		b.ID).Scan(&logo)
	q.Logo = logo
	return q, nil
}

// BranchIDBySlug is for the guest's "take a number" request.
func (a *App) BranchIDBySlug(ctx context.Context, slug string) (int64, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return b.ID, nil
}

// GuestTicket is the guest's own number.
type GuestTicket struct {
	Number     string           `json:"number"`
	Letter     string           `json:"letter"`
	People     int              `json:"people"`
	Source     string           `json:"source"`
	Status     string           `json:"status"`
	Ahead      int              `json:"ahead"`
	EstMinutes int              `json:"est_minutes"`
	Calls      int              `json:"calls"`
	CalledAt   *time.Time       `json:"called_at"`
	Table      string           `json:"table"`
	CreatedAt  time.Time        `json:"created_at"`
	Group      QueuePublicGroup `json:"group"`
	Queue      *QueuePublic     `json:"queue"`
	// BranchID is for the event stream.
	BranchID int64 `json:"-"`
}

// TicketByToken is the guest's page.
func (a *App) TicketByToken(ctx context.Context, token string) (*GuestTicket, error) {
	var id, branchID int64
	err := a.DB.QueryRow(ctx, `SELECT id, branch_id FROM queue_tickets WHERE token_hash=$1`, hashToken(token)).Scan(&id, &branchID)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	st, err := a.queueState(ctx, branchID)
	if err != nil {
		return nil, err
	}
	var t *QueueTicket
	for i := range st.tickets {
		if st.tickets[i].ID == id {
			t = &st.tickets[i]
		}
	}
	if t == nil {
		tk, err := scanTicket(a.DB.QueryRow(ctx, `SELECT `+ticketCols+ticketFrom+`WHERE t.id=$1`, id))
		if err != nil {
			return nil, err
		}
		t = &tk
	}
	pub := st.public(false)
	var logo string
	_ = a.DB.QueryRow(ctx, `SELECT COALESCE((SELECT published->>'logo_url' FROM channel_templates WHERE branch_id=$1 AND channel='delivery'), '')`,
		branchID).Scan(&logo)
	pub.Logo = logo
	out := &GuestTicket{Number: t.Number, Letter: t.Letter, People: t.People, Source: t.Source, Status: t.Status,
		EstMinutes: t.EstMinutes, Calls: t.Calls, CalledAt: t.CalledAt, Table: t.CalledTable, CreatedAt: t.CreatedAt,
		Queue: pub, BranchID: branchID}
	if t.Position > 0 {
		out.Ahead = t.Position - 1
	}
	if t.Status == "seated" {
		out.Table = t.SeatedTable
	}
	for _, g := range pub.Groups {
		if g.ID == t.GroupID {
			out.Group = g
		}
	}
	return out, nil
}

// CancelByGuest is "cancel" on the guest's page.
func (a *App) CancelByGuest(ctx context.Context, token string) error {
	var id, branchID int64
	err := a.DB.QueryRow(ctx, `SELECT id, branch_id FROM queue_tickets WHERE token_hash=$1`, hashToken(token)).Scan(&id, &branchID)
	if isNoRows(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = a.ticketStep(ctx, branchID, id, func(tx pgx.Tx, t *lockedTicket) error {
		if t.Status != "waiting" && t.Status != "called" {
			return ErrInvalidState("this number is no longer in line")
		}
		if _, err := tx.Exec(ctx, `UPDATE queue_tickets SET status='cancelled', called_table_id=NULL, closed_at=now() WHERE id=$1`, t.ID); err != nil {
			return err
		}
		return queueEvent(ctx, tx, branchID, t.ID, Actor{Type: "customer"}, "cancel", map[string]any{"by": "guest"})
	})
	return err
}

// TableHolds lists the tables held for a called number, for the dine-in desk.
func (a *App) TableHolds(ctx context.Context, branchID int64) (map[int64]string, error) {
	rows, err := a.DB.Query(ctx, `SELECT called_table_id, letter || lpad(seq::text, 3, '0') FROM queue_tickets
		WHERE branch_id=$1 AND status='called' AND called_table_id IS NOT NULL AND biz_date >= current_date - 1`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var no string
		if err := rows.Scan(&id, &no); err != nil {
			return nil, err
		}
		out[id] = no
	}
	return out, rows.Err()
}
