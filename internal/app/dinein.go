package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Dine-in (docs/DESIGN.md §15). Lock order: table session, then order, then
// items, then menu stock rows. Adding a batch and closing the table both take
// the session lock first, so exactly one of two racing requests wins and the
// other sees the new state.

var (
	ErrSessionClosed = Err(http.StatusConflict, "TABLE_SESSION_CLOSED", "this table has been closed, please scan the code again")
	errTableNotOpen  = Err(http.StatusConflict, "TABLE_NOT_OPEN", "this table is not open yet, please ask the staff")
)

const (
	devicePairTTL = 10 * time.Minute
	// DeviceTTL is how long a paired tablet stays signed in.
	DeviceTTL = 365 * 24 * time.Hour
	// TableSessionTTL bounds a scanned phone's cookie; closing the table ends it sooner.
	TableSessionTTL = 12 * time.Hour
)

// ---- tables ----

type TableDevice struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Paired   bool       `json:"paired"`
	PairedAt *time.Time `json:"paired_at"`
}

type SessionSummary struct {
	ID        int64     `json:"id"`
	OrderID   int64     `json:"order_id"`
	PublicNo  string    `json:"public_no"`
	Guests    int       `json:"guests"`
	OpenedAt  time.Time `json:"opened_at"`
	Total     int64     `json:"total"`
	ItemCount int       `json:"item_count"` // live lines
	Unserved  int       `json:"unserved"`   // live lines not yet at the table
	// UnservedQty counts portions (not lines) still to reach the table.
	UnservedQty int      `json:"unserved_qty"`
	Rounds      int      `json:"rounds"`
	Tags        []string `json:"tags"`
}

type Table struct {
	ID      int64           `json:"id"`
	Name    string          `json:"name"`
	QRToken string          `json:"qr_token"`
	Active  bool            `json:"active"`
	Sort    int             `json:"sort"`
	Area    string          `json:"area"`
	Seats   int             `json:"seats"`
	Session *SessionSummary `json:"session"`
	Devices []TableDevice   `json:"devices"`
}

type TableInput struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Sort   int    `json:"sort"`
	Area   string `json:"area"`
	// Seats left out (0) keeps a table's seats; a new table gets 4.
	Seats int `json:"seats"`
}

func isUnique(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && (constraint == "" || pg.ConstraintName == constraint)
}

// SaveTable creates (id 0) or edits a table. A new table gets a random code
// for its printed QR; the code only locates the table and grants nothing.
func (a *App) SaveTable(ctx context.Context, branchID, id int64, in TableInput) (int64, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 20 {
		return 0, ErrValidation("table name is required (up to 20 characters)")
	}
	in.Area = strings.TrimSpace(in.Area)
	if len([]rune(in.Area)) > 20 {
		return 0, ErrValidation("area name is up to 20 characters")
	}
	if in.Seats < 0 || in.Seats > 99 {
		return 0, ErrValidation("seats must be between 1 and 99")
	}
	var seats *int
	if in.Seats > 0 {
		seats = &in.Seats
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if id == 0 {
			code := base64.RawURLEncoding.EncodeToString(randomBytes(12))
			if err := tx.QueryRow(ctx, `INSERT INTO dining_tables (branch_id, name, qr_token, active, sort, area, seats)
				VALUES ($1,$2,$3,$4,$5,$6,COALESCE($7,4)) RETURNING id`, branchID, in.Name, code, in.Active, in.Sort,
				in.Area, seats).Scan(&id); err != nil {
				return err
			}
			return branchChanged(ctx, tx, branchID)
		}
		if !in.Active {
			var open bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dining_sessions WHERE table_id=$1 AND status='open')`,
				id).Scan(&open); err != nil {
				return err
			}
			if open {
				return ErrInvalidState("close the table before disabling it")
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE dining_tables SET name=$3, active=$4, sort=$5, area=$6, seats=COALESCE($7, seats)
			WHERE id=$1 AND branch_id=$2`, id, branchID, in.Name, in.Active, in.Sort, in.Area, seats)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return branchChanged(ctx, tx, branchID)
	})
	if isUnique(err, "dining_tables_active_name") {
		return 0, ErrValidation("another active table already has this name")
	}
	return id, err
}

// ListTables returns the branch's tables with their open session, if any.
func (a *App) ListTables(ctx context.Context, branchID int64) ([]Table, error) {
	rows, err := a.DB.Query(ctx, `SELECT t.id, t.name, t.qr_token, t.active, t.sort, t.area, t.seats,
		s.id, s.order_id, o.public_no, s.guests, s.opened_at, o.total,
		(SELECT count(*) FROM order_items i WHERE i.order_id = o.id AND i.prep_status <> 'cancelled'),
		(SELECT count(*) FROM order_items i WHERE i.order_id = o.id AND i.prep_status <> 'cancelled' AND i.served_at IS NULL),
		(SELECT COALESCE(sum(i.qty), 0) FROM order_items i WHERE i.order_id = o.id AND i.prep_status <> 'cancelled' AND i.served_at IS NULL),
		(SELECT count(*) FROM order_batches b WHERE b.order_id = o.id), s.tags
		FROM dining_tables t
		LEFT JOIN dining_sessions s ON s.table_id = t.id AND s.status = 'open'
		LEFT JOIN orders o ON o.id = s.order_id
		WHERE t.branch_id=$1 ORDER BY t.active DESC, t.sort, t.name`, branchID)
	if err != nil {
		return nil, err
	}
	out := []Table{}
	idx := map[int64]int{}
	for rows.Next() {
		t := Table{Devices: []TableDevice{}}
		var sid, oid *int64
		var no *string
		var guests, items, unserved, unservedQty, rounds *int
		var opened *time.Time
		var total *int64
		var tags []string
		if err := rows.Scan(&t.ID, &t.Name, &t.QRToken, &t.Active, &t.Sort, &t.Area, &t.Seats, &sid, &oid, &no, &guests, &opened,
			&total, &items, &unserved, &unservedQty, &rounds, &tags); err != nil {
			rows.Close()
			return nil, err
		}
		if sid != nil {
			t.Session = &SessionSummary{ID: *sid, OrderID: *oid, PublicNo: *no, Guests: *guests, OpenedAt: *opened,
				Total: *total, ItemCount: *items, Unserved: *unserved, UnservedQty: *unservedQty, Rounds: *rounds, Tags: tags}
			if t.Session.Tags == nil {
				t.Session.Tags = []string{}
			}
		}
		idx[t.ID] = len(out)
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = a.DB.Query(ctx, `SELECT id, table_id, name, token_hash IS NOT NULL, paired_at FROM table_devices
		WHERE branch_id=$1 AND revoked_at IS NULL AND (token_hash IS NOT NULL OR pair_expires_at > now()) ORDER BY id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d TableDevice
		var tableID int64
		if err := rows.Scan(&d.ID, &tableID, &d.Name, &d.Paired, &d.PairedAt); err != nil {
			return nil, err
		}
		if i, ok := idx[tableID]; ok {
			out[i].Devices = append(out[i].Devices, d)
		}
	}
	return out, rows.Err()
}

// ---- sessions ----

// Bill is a table session with its order. Batches are read off the order's
// items (batch_seq, batch_source).
type Bill struct {
	SessionID int64      `json:"session_id"`
	TableID   int64      `json:"table_id"`
	TableName string     `json:"table_name"`
	Status    string     `json:"status"` // open or closed
	Guests    int        `json:"guests"`
	OpenedAt  time.Time  `json:"opened_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	Order     *Order     `json:"order"`
	// Tags are the sitting's notes (children, birthday); MovedFrom is the
	// table it was moved from, if any; Area and Seats are the table's.
	Tags      []string `json:"tags"`
	MovedFrom string   `json:"moved_from"`
	Area      string   `json:"area"`
	Seats     int      `json:"seats"`
}

func loadBill(ctx context.Context, q Querier, sessionID int64) (*Bill, error) {
	var b Bill
	var orderID int64
	err := q.QueryRow(ctx, `SELECT s.id, s.table_id, t.name, s.status, s.guests, s.opened_at, s.closed_at, s.order_id,
		s.tags, s.moved_from, t.area, t.seats
		FROM dining_sessions s JOIN dining_tables t ON t.id = s.table_id WHERE s.id=$1`, sessionID).
		Scan(&b.SessionID, &b.TableID, &b.TableName, &b.Status, &b.Guests, &b.OpenedAt, &b.ClosedAt, &orderID,
			&b.Tags, &b.MovedFrom, &b.Area, &b.Seats)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if b.Tags == nil {
		b.Tags = []string{}
	}
	if b.Order, err = loadOrder(ctx, q, orderID); err != nil {
		return nil, err
	}
	return &b, nil
}

// GetBill returns a session's bill for staff.
func (a *App) GetBill(ctx context.Context, sessionID int64) (*Bill, error) {
	return loadBill(ctx, a.DB, sessionID)
}

// SessionBranch resolves a session to its branch for permission checks.
func (a *App) SessionBranch(ctx context.Context, sessionID int64) (int64, error) {
	var b int64
	err := a.DB.QueryRow(ctx, `SELECT branch_id FROM dining_sessions WHERE id=$1`, sessionID).Scan(&b)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return b, err
}

// TableBranch resolves a table to its branch for permission checks.
func (a *App) TableBranch(ctx context.Context, tableID int64) (int64, error) {
	var b int64
	err := a.DB.QueryRow(ctx, `SELECT branch_id FROM dining_tables WHERE id=$1`, tableID).Scan(&b)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return b, err
}

// RecentBills lists the branch's most recently closed sessions.
func (a *App) RecentBills(ctx context.Context, branchID int64) ([]*Bill, error) {
	rows, err := a.DB.Query(ctx, `SELECT id FROM dining_sessions WHERE branch_id=$1 AND status='closed'
		ORDER BY closed_at DESC LIMIT 30`, branchID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []*Bill{}
	for _, id := range ids {
		b, err := loadBill(ctx, a.DB, id)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// OpenTable starts a session on a free table and creates its (empty) bill.
// The table row lock and the one-open-session index make a double open
// impossible.
func (a *App) OpenTable(ctx context.Context, tableID int64, guests int, tags []string, actor Actor) (*Bill, error) {
	if guests < 0 || guests > 99 {
		return nil, ErrValidation("guests must be between 0 and 99")
	}
	tags, err := cleanTags(tags)
	if err != nil {
		return nil, err
	}
	var sessionID int64
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		var branchID int64
		var name string
		var active bool
		err := tx.QueryRow(ctx, `SELECT branch_id, name, active FROM dining_tables WHERE id=$1 FOR UPDATE`, tableID).
			Scan(&branchID, &name, &active)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if !active {
			return ErrInvalidState("this table is disabled")
		}
		var open bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dining_sessions WHERE table_id=$1 AND status='open')`,
			tableID).Scan(&open); err != nil {
			return err
		}
		if open {
			return Err(http.StatusConflict, "TABLE_OCCUPIED", "this table is already open")
		}
		b, err := scanBranch(tx.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, branchID))
		if err != nil {
			return err
		}
		if b.Suspended {
			return errStoreClosed
		}
		no, err := nextOrderNo(ctx, tx, b, nil, TypeDineIn)
		if err != nil {
			return err
		}
		publicNo, code := no.Public, no.Code
		// The bill starts empty; fees and tax are recomputed with every batch.
		t := ComputeTotals(0, 0, b.dineInTax(), Discount{})
		orderNonce := randomBytes(16)
		var orderID int64
		if err := tx.QueryRow(ctx, `INSERT INTO orders (merchant_id, branch_id, public_no, pickup_code,
			access_token_hash, token_nonce, channel, service_type, customer_name, customer_phone, currency, subtotal,
			packaging_fee, delivery_fee, service_charge, tax_amount, tax_inclusive, tax_rate_bp, total, payment_method,
			acceptance_status, accepted_at, accept_deadline, table_name)
			VALUES ($1,$2,$3,$4,$5,$6,'dine_in','dine_in','','',$7,0,0,0,0,0,$8,$9,0,'pay_at_counter',
				'accepted', now(), now(), $10) RETURNING id`,
			b.MerchantID, b.ID, publicNo, code, hashToken(a.orderToken(orderNonce)), orderNonce, b.Currency,
			t.TaxInclusive, t.TaxRateBP, name).Scan(&orderID); err != nil {
			return wrap("insert dine-in order", err)
		}
		// staff until the first round says otherwise (see AddRound)
		if err := stampOrder(ctx, tx, orderID, no, "staff"); err != nil {
			return err
		}
		nonce := randomBytes(16)
		var opener *int64
		if actor.ID != 0 {
			opener = &actor.ID
		}
		if err := tx.QueryRow(ctx, `INSERT INTO dining_sessions (branch_id, table_id, order_id, guests,
			access_token_hash, token_nonce, opened_by, tags) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			branchID, tableID, orderID, guests, hashToken(a.sessionToken(nonce)), nonce, opener, tags).Scan(&sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET session_id=$2 WHERE id=$1`, orderID, sessionID); err != nil {
			return err
		}
		if err := audit(ctx, tx, branchID, orderID, actor, "table.opened",
			map[string]any{"table": name, "session_id": sessionID, "guests": guests, "tags": tags}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID, OrderID: orderID})
	})
	if isUnique(err, "dining_sessions_one_open") {
		return nil, Err(http.StatusConflict, "TABLE_OCCUPIED", "this table is already open")
	}
	if err != nil {
		return nil, err
	}
	return loadBill(ctx, a.DB, sessionID)
}

// dineInTax is the branch's fee configuration for eating in: no packaging fee.
func (b *Branch) dineInTax() TaxConfig {
	c := b.taxConfig()
	c.PackagingFee = 0
	return c
}

func (a *App) sessionToken(nonce []byte) string {
	return a.derive("table-session:", nonce)
}

// lockedSession is the session row read under FOR UPDATE.
type lockedSession struct {
	ID, BranchID, TableID, OrderID int64
	Status, TableName              string
}

func lockSession(ctx context.Context, tx pgx.Tx, id int64) (*lockedSession, error) {
	var s lockedSession
	err := tx.QueryRow(ctx, `SELECT s.id, s.branch_id, s.table_id, s.order_id, s.status, t.name
		FROM dining_sessions s JOIN dining_tables t ON t.id = s.table_id WHERE s.id=$1 FOR UPDATE OF s`, id).
		Scan(&s.ID, &s.BranchID, &s.TableID, &s.OrderID, &s.Status, &s.TableName)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return &s, err
}

// recomputeBill sets a table bill's amounts from its live lines.
func recomputeBill(ctx context.Context, tx pgx.Tx, b *Branch, orderID int64) (Totals, error) {
	return repriceTable(ctx, tx, b, orderID, false)
}

// dinePolicy is how a table bill decides its promotion: the best dine-in
// promotion for the dishes (docs/CHECKOUT_DISCOUNT_DESIGN.md §4.2), its dates
// and daily hours checked at the time the table was opened, so a lunch offer
// still applies when the lunch guests pay at 14:20; its status and quota are
// checked now. A dish voided through the store's fault keeps the guests'
// promotion even below its threshold, scaled to the dishes still bought
// (docs/VOID_ITEM_DESIGN.md §6).
func dinePolicy(m *orderMoney, storeFault bool) promoPolicy {
	return promoPolicy{at: m.Opened, pick: true, current: storeFault, floor: storeFault, prorate: storeFault}
}

// repriceTable prices a table bill again and saves it.
func repriceTable(ctx context.Context, tx pgx.Tx, b *Branch, orderID int64, storeFault bool) (Totals, error) {
	m, err := loadMoney(ctx, tx, orderID)
	if err != nil {
		return Totals{}, err
	}
	p, _, err := m.reprice(ctx, tx, b, nil, dinePolicy(m, storeFault))
	if err != nil {
		return Totals{}, err
	}
	return p.Totals, saveMoney(ctx, tx, b, orderID, p, true)
}

type BatchInput struct {
	Items []QuoteLineInput `json:"items"`
	// ExpectedTotal is the sum of line totals the client showed. When prices
	// moved since, the batch is refused so the guest can look again.
	ExpectedTotal *int64 `json:"expected_total"`
	// Lang is the guest's language; the bill keeps the dishes' names in it.
	Lang string `json:"lang"`
	// RoundTags are notes for the whole round (drinks first); staff only.
	RoundTags []string `json:"round_tags"`
}

// AddBatch sends one round of dishes to the kitchen and appends it to the
// table's bill. source is qr, ipad or staff. A retry with the same
// idempotency key returns the current bill without adding anything.
func (a *App) AddBatch(ctx context.Context, sessionID int64, source, idemKey string, in BatchInput, actor Actor) (*Bill, error) {
	if idemKey == "" || len(idemKey) > 100 {
		return nil, ErrValidation("Idempotency-Key header is required")
	}
	if source != "staff" {
		in.RoundTags = nil
	}
	roundTags, err := cleanTags(in.RoundTags)
	if err != nil {
		return nil, err
	}
	var placedBy *int64
	if source == "staff" && actor.ID != 0 {
		placedBy = &actor.ID
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		s, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		scope := fmt.Sprintf("table-batch:%d", s.ID)
		var rec struct {
			BatchSeq int `json:"batch_seq"`
		}
		replayed, err := idemBegin(ctx, tx, scope, idemKey, hashJSON(in), &rec)
		if err != nil {
			return err
		}
		if replayed {
			return nil // the batch is already on the bill, even if the table has closed since
		}
		if s.Status != "open" {
			return ErrSessionClosed
		}
		if _, err := lockOrder(ctx, tx, s.OrderID); err != nil {
			return err
		}
		b, err := scanBranch(tx.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, s.BranchID))
		if err != nil {
			return err
		}
		if !b.takesOrders() {
			return errStoreClosed
		}
		lines, subtotal, err := priceLines(ctx, tx, b.ID, "dine_in", in.Items)
		if err != nil {
			return err
		}
		if source != "staff" {
			if err := localizeLines(ctx, tx, b, in.Lang, lines); err != nil {
				return err
			}
		}
		if in.ExpectedTotal != nil && *in.ExpectedTotal != subtotal {
			return ErrD(http.StatusConflict, "QUOTE_CHANGED", "prices changed, please check the amount again",
				map[string]any{"total": subtotal})
		}
		if err := reserveStock(ctx, tx, lines); err != nil {
			return err
		}
		var batchID int64
		var seq int
		if err := tx.QueryRow(ctx, `INSERT INTO order_batches (order_id, seq, source, round_tags, placed_by)
			VALUES ($1, (SELECT COALESCE(max(seq), 0) + 1 FROM order_batches WHERE order_id=$1), $2, $3, $4)
			RETURNING id, seq`, s.OrderID, source, roundTags, placedBy).Scan(&batchID, &seq); err != nil {
			return err
		}
		// A table counts as self-ordered when its first round came from the
		// customer's phone or the table tablet (docs/ORDER_TYPES_DESIGN.md §3.1).
		if seq == 1 && (source == "qr" || source == "ipad") {
			if _, err := tx.Exec(ctx, `UPDATE orders SET dine_source='self' WHERE id=$1`, s.OrderID); err != nil {
				return err
			}
		}
		for _, l := range lines {
			opts, _ := json.Marshal(l.Options)
			status := "pending"
			if !l.NeedsPrep {
				status = "ready"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO order_items (order_id, branch_id, batch_id, item_id, name, name_en,
				unit_price, qty, line_total, options, note, needs_prep, prep_status, name_customer, note_tags)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, s.OrderID, b.ID, batchID, l.ItemID, l.Name,
				l.NameEn, l.UnitPrice, l.Qty, l.LineTotal, opts, l.Note, l.NeedsPrep, status, l.NameTr,
				noteTagsJSON(l.NoteTags)); err != nil {
				return wrap("insert batch item", err)
			}
		}
		// Dispatch this batch to the stations as configured right now.
		if _, err := tx.Exec(ctx, `UPDATE order_items oi SET station_id = st.id, station_name = st.name
			FROM menu_items m JOIN kitchen_stations st ON st.id = m.station_id AND st.active
			WHERE oi.batch_id=$1 AND oi.needs_prep AND m.id = oi.item_id`, batchID); err != nil {
			return err
		}
		if err := moneyEvent(ctx, tx, "table", batchID, actor); err != nil {
			return err
		}
		t, err := recomputeBill(ctx, tx, b, s.OrderID)
		if err != nil {
			return err
		}
		if err := audit(ctx, tx, b.ID, s.OrderID, actor, "table.batch_added",
			map[string]any{"batch": seq, "source": source, "lines": len(lines), "batch_total": subtotal, "total": t.Total}); err != nil {
			return err
		}
		rec.BatchSeq = seq
		if err := idemSave(ctx, tx, scope, idemKey, rec); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: b.ID, OrderID: s.OrderID})
	})
	if err != nil {
		return nil, wrap("add batch", err)
	}
	return loadBill(ctx, a.DB, sessionID)
}

// dineItem locks the session and order of a dine-in order item and returns them.
func (a *App) dineItem(ctx context.Context, tx pgx.Tx, itemID int64) (*lockedSession, *lockedOrder, error) {
	var sessionID *int64
	err := tx.QueryRow(ctx, `SELECT o.session_id FROM order_items i JOIN orders o ON o.id = i.order_id WHERE i.id=$1`,
		itemID).Scan(&sessionID)
	if isNoRows(err) {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	if sessionID == nil {
		return nil, nil, ErrInvalidState("this is not a dine-in item")
	}
	s, err := lockSession(ctx, tx, *sessionID)
	if err != nil {
		return nil, nil, err
	}
	o, err := lockOrder(ctx, tx, s.OrderID)
	return s, o, err
}

// ServeItem records that a ready dish reached the table. Repeating it is harmless.
func (a *App) ServeItem(ctx context.Context, itemID int64, actor Actor) (*Bill, error) {
	var sessionID int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		s, o, err := a.dineItem(ctx, tx, itemID)
		if err != nil {
			return err
		}
		sessionID = s.ID
		var status string
		var served *time.Time
		if err := tx.QueryRow(ctx, `SELECT prep_status, served_at FROM order_items WHERE id=$1 FOR UPDATE`, itemID).
			Scan(&status, &served); err != nil {
			return err
		}
		if served != nil {
			return nil
		}
		if s.Status != "open" {
			return ErrSessionClosed
		}
		if status != "ready" {
			return ErrInvalidState("only a dish that is ready can be served")
		}
		if _, err := tx.Exec(ctx, `UPDATE order_items SET served_at = now() WHERE id=$1`, itemID); err != nil {
			return err
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
	if err != nil {
		return nil, err
	}
	return loadBill(ctx, a.DB, sessionID)
}

type CloseInput struct {
	// ExpectedManifest is the bill's manifest_version the cashier is looking
	// at. It is required: a batch or a void that happened meanwhile must be
	// seen before payment. (Kitchen progress does not change it.)
	ExpectedManifest int    `json:"expected_manifest_version"`
	AmountReceived   int64  `json:"amount_received"`
	Note             string `json:"note"`
	// PayMethod is how the guests paid (cash, card, a wallet), as the desk
	// shows it. Replaced by PayType and PayChannelID; kept for older desks.
	PayMethod string `json:"pay_method"`
	// PayType is cash, card or online; PayChannelID the online method.
	PayType      string `json:"pay_type"`
	PayChannelID int64  `json:"pay_channel_id"`
	// Adjust is the discount at checkout, nil for none
	// (docs/CHECKOUT_DISCOUNT_DESIGN.md §5); the caller checked who may give it.
	Adjust *Adjust `json:"adjust"`
	// ExpectedTotal is the amount to pay the cashier saw; another amount
	// means the bill changed meanwhile.
	ExpectedTotal *int64 `json:"expected_total"`
}

// CloseTable records the payment and ends the session. Afterwards the QR
// session token and any open carts for this sitting stop working; the table
// can be opened again for the next guests.
func (a *App) CloseTable(ctx context.Context, sessionID int64, in CloseInput, actor Actor) (*Bill, error) {
	in.PayMethod = strings.TrimSpace(in.PayMethod)
	if len(in.Note) > 200 || len([]rune(in.PayMethod)) > 20 {
		return nil, ErrValidation("note too long")
	}
	promoGone := false
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		s, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		if s.Status == "closed" {
			return nil // already closed: return the final bill, charge nothing twice
		}
		o, err := lockOrder(ctx, tx, s.OrderID)
		if err != nil {
			return err
		}
		if in.ExpectedManifest == 0 || in.ExpectedManifest != o.ManifestVersion {
			return ErrVersion
		}
		if err := moneyEvent(ctx, tx, "close", 0, actor); err != nil {
			return err
		}
		var cooking int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM order_items WHERE order_id=$1
			AND prep_status IN ('pending','preparing')`, o.ID).Scan(&cooking); err != nil {
			return err
		}
		if cooking > 0 {
			return ErrD(http.StatusConflict, "ITEMS_IN_PROGRESS",
				"some dishes are still being prepared; finish or void them before closing", map[string]any{"count": cooking})
		}
		total := o.Total
		var adjusted *priced
		var b *Branch
		if !in.Adjust.empty() {
			if b, err = readBranch(ctx, tx, s.BranchID); err != nil {
				return err
			}
			m, err := loadMoney(ctx, tx, o.ID)
			if err != nil {
				return err
			}
			if adjusted, err = m.withAdjust(*in.Adjust); err != nil {
				return err
			}
			total = adjusted.Totals.Total
		}
		if in.ExpectedTotal != nil && *in.ExpectedTotal != total {
			return ErrVersion
		}
		// what a store balance paid is not collected (docs/REFUND_BALANCE_DESIGN.md §5.1)
		var fromCredit int64
		if err := tx.QueryRow(ctx, `SELECT credit_paid FROM orders WHERE id=$1`, o.ID).Scan(&fromCredit); err != nil {
			return err
		}
		fromCredit = min(fromCredit, total)
		due := total - fromCredit
		var channel *int64
		if due == 0 && fromCredit > 0 {
			in.PayType, in.PayMethod, in.AmountReceived = "credit", "", 0
		} else if in.PayType != "" {
			ch, name, err := resolvePay(ctx, tx, s.BranchID, PayInput{Type: in.PayType, ChannelID: in.PayChannelID})
			if err != nil {
				return err
			}
			channel, in.PayMethod = ch, name
			if in.PayType != "cash" {
				in.AmountReceived = due // a card or an online payment is for the exact amount
			}
		}
		if in.AmountReceived < due {
			return ErrValidation("the amount received is less than the bill")
		}
		// The dine-in promotion takes its quota only now: an open table holds none.
		var promoID, discount int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(promotion_id, 0),
			COALESCE((SELECT sum(d.amount) FROM order_discounts d WHERE d.order_id = o.id AND d.kind = 'promotion'), o.discount)
			FROM orders o WHERE o.id=$1`, o.ID).
			Scan(&promoID, &discount); err != nil {
			return err
		}
		if promoID != 0 {
			if err := claimPromotion(ctx, tx, s.BranchID, promoID, o.ID, "", discount); err != nil {
				if isCode(err, "QUOTE_CHANGED") {
					promoGone = true
				}
				return err
			}
		}
		// Dishes that were ready but not ticked off are taken as served.
		if _, err := tx.Exec(ctx, `UPDATE order_items SET served_at = now()
			WHERE order_id=$1 AND prep_status='ready' AND served_at IS NULL`, o.ID); err != nil {
			return err
		}
		if adjusted != nil {
			if err := saveMoney(ctx, tx, b, o.ID, adjusted, false); err != nil {
				return err
			}
			if err := writeAdjusted(ctx, tx, o.ID, adjusted.Totals, in.Adjust.Reason, actor); err != nil {
				return err
			}
			if err := audit(ctx, tx, s.BranchID, o.ID, actor, "order.adjusted", map[string]any{"adjust": in.Adjust,
				"total_before": o.Total, "total_after": total, "staff_discount": adjusted.Totals.StaffDiscount,
				"round_off": adjusted.Totals.RoundOff}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET fulfillment_status='dined', payment_status='paid',
			amount_received=$2, payment_note=$3, pay_method=$4, pay_type=$5, pay_channel_id=$6, completed_at=now(),
			version=version+1, updated_at=now() WHERE id=$1`,
			o.ID, in.AmountReceived, in.Note, in.PayMethod, in.PayType, channel); err != nil {
			return err
		}
		var closer *int64
		if actor.ID != 0 {
			closer = &actor.ID
		}
		if _, err := tx.Exec(ctx, `UPDATE dining_sessions SET status='closed', closed_at=now(), closed_by=$2 WHERE id=$1`,
			s.ID, closer); err != nil {
			return err
		}
		if err := audit(ctx, tx, s.BranchID, o.ID, actor, "table.closed",
			map[string]any{"table": s.TableName, "total": total, "received": in.AmountReceived, "pay_type": in.PayType,
				"pay_method": in.PayMethod}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: s.BranchID, OrderID: o.ID})
	})
	if promoGone {
		// Another table took the promotion's last use: price the bill again
		// without it and have the cashier confirm the new amount.
		if err := a.inTx(ctx, func(tx pgx.Tx) error {
			var branchID, orderID int64
			if err := tx.QueryRow(ctx, `SELECT branch_id, order_id FROM dining_sessions WHERE id=$1`, sessionID).
				Scan(&branchID, &orderID); err != nil {
				return err
			}
			if _, err := lockOrder(ctx, tx, orderID); err != nil {
				return err
			}
			b, err := scanBranch(tx.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, branchID))
			if err != nil {
				return err
			}
			if _, err := recomputeBill(ctx, tx, b, orderID); err != nil {
				return err
			}
			return notify(ctx, tx, Event{BranchID: branchID, OrderID: orderID})
		}); err != nil {
			return nil, err
		}
		return nil, ErrD(http.StatusConflict, "VERSION_CONFLICT", "the promotion's quota ran out; the bill was priced again",
			map[string]any{"reason": "promotion_used_up"})
	}
	if err != nil {
		return nil, err
	}
	return loadBill(ctx, a.DB, sessionID)
}

// ---- guest access: QR code and paired tablets ----

// TableAccess is what a guest credential currently points at.
type TableAccess struct {
	BranchID  int64  `json:"-"`
	Slug      string `json:"slug"`
	StoreName string `json:"store_name"`
	TableName string `json:"table_name"`
	Mode      string `json:"mode"` // qr or ipad
	// SessionID is 0 when a tablet's table has no open session.
	SessionID int64 `json:"-"`
	OrderID   int64 `json:"-"`
}

// ExchangeTableCode turns the code printed on a table into a token for the
// session that is open there right now. Without an open session there is
// nothing to join: staff open the table first.
func (a *App) ExchangeTableCode(ctx context.Context, code string) (string, error) {
	var nonce []byte
	err := a.DB.QueryRow(ctx, `SELECT s.token_nonce FROM dining_tables t
		JOIN dining_sessions s ON s.table_id = t.id AND s.status='open'
		WHERE t.qr_token=$1 AND t.active`, code).Scan(&nonce)
	if isNoRows(err) {
		var exists bool
		if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dining_tables WHERE qr_token=$1 AND active)`, code).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return "", ErrNotFound
		}
		return "", errTableNotOpen
	} else if err != nil {
		return "", err
	}
	return a.sessionToken(nonce), nil
}

// AccessByToken resolves a scanned phone's token. A closed session is an
// explicit error so the page can tell the guest to scan again.
func (a *App) AccessByToken(ctx context.Context, token string) (*TableAccess, error) {
	acc := TableAccess{Mode: "qr"}
	var status string
	err := a.DB.QueryRow(ctx, `SELECT s.id, s.order_id, s.status, s.branch_id, b.slug, b.name, t.name
		FROM dining_sessions s JOIN dining_tables t ON t.id = s.table_id JOIN branches b ON b.id = s.branch_id
		WHERE s.access_token_hash=$1`, hashToken(token)).
		Scan(&acc.SessionID, &acc.OrderID, &status, &acc.BranchID, &acc.Slug, &acc.StoreName, &acc.TableName)
	if isNoRows(err) {
		return nil, ErrSessionClosed
	} else if err != nil {
		return nil, err
	}
	if status != "open" {
		return nil, ErrSessionClosed
	}
	return &acc, nil
}

// CreateDevicePairing issues a one-time code that binds a tablet to a table.
func (a *App) CreateDevicePairing(ctx context.Context, tableID int64, name string, actor Actor) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) > 40 {
		return "", ErrValidation("name too long")
	}
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no look-alikes
	raw := randomBytes(8)
	code := make([]byte, 8)
	for i, b := range raw {
		code[i] = alphabet[int(b)%len(alphabet)]
	}
	var creator *int64
	if actor.ID != 0 {
		creator = &actor.ID
	}
	tag, err := a.DB.Exec(ctx, `INSERT INTO table_devices (branch_id, table_id, name, pair_code_hash, pair_expires_at, created_by)
		SELECT branch_id, id, $2, $3, now() + make_interval(secs => $4), $5 FROM dining_tables WHERE id=$1 AND active`,
		tableID, name, hashToken(string(code)), devicePairTTL.Seconds(), creator)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return string(code), nil
}

// PairDevice consumes a pairing code and returns the tablet's long-lived token.
func (a *App) PairDevice(ctx context.Context, code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	token, hash := newToken()
	tag, err := a.DB.Exec(ctx, `UPDATE table_devices SET token_hash=$2, paired_at=now(), pair_code_hash=NULL
		WHERE pair_code_hash=$1 AND pair_expires_at > now() AND revoked_at IS NULL AND token_hash IS NULL`,
		hashToken(code), hash)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", Err(http.StatusUnprocessableEntity, "PAIR_CODE_INVALID", "the pairing code is wrong or has expired")
	}
	return token, nil
}

// RevokeDevice ends a tablet's access.
func (a *App) RevokeDevice(ctx context.Context, branchID, deviceID int64) error {
	tag, err := a.DB.Exec(ctx, `UPDATE table_devices SET revoked_at=now() WHERE id=$1 AND branch_id=$2 AND revoked_at IS NULL`,
		deviceID, branchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return notify(ctx, a.DB, Event{BranchID: branchID})
}

// AccessByDevice resolves a paired tablet to its table and whatever session
// is open there now. The tablet never keeps a session of its own, so after a
// table is closed it simply sees "no session" until the next one opens.
func (a *App) AccessByDevice(ctx context.Context, token string) (*TableAccess, error) {
	acc := TableAccess{Mode: "ipad"}
	var sid, oid *int64
	err := a.DB.QueryRow(ctx, `SELECT d.branch_id, b.slug, b.name, t.name, s.id, s.order_id
		FROM table_devices d JOIN dining_tables t ON t.id = d.table_id JOIN branches b ON b.id = d.branch_id
		LEFT JOIN dining_sessions s ON s.table_id = t.id AND s.status='open'
		WHERE d.token_hash=$1 AND d.revoked_at IS NULL AND t.active`, hashToken(token)).
		Scan(&acc.BranchID, &acc.Slug, &acc.StoreName, &acc.TableName, &sid, &oid)
	if isNoRows(err) {
		return nil, Err(http.StatusUnauthorized, "DEVICE_NOT_PAIRED", "this tablet is not paired with a table")
	} else if err != nil {
		return nil, err
	}
	if sid != nil {
		acc.SessionID, acc.OrderID = *sid, *oid
	}
	return &acc, nil
}

// GuestBill is the bill as guests at the table see it.
func (a *App) GuestBill(ctx context.Context, sessionID int64) (*Bill, error) {
	b, err := loadBill(ctx, a.DB, sessionID)
	if err != nil {
		return nil, err
	}
	b.Order.Cash, b.Order.Delivery = nil, nil
	return b, nil
}
