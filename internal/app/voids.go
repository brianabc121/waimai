package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Voiding dishes on any order, and refunds by dish once paid
// (docs/VOID_ITEM_DESIGN.md). A void takes portions of one line off an order
// that is still in the store: a delivery until the rider takes it, a pickup
// until it is collected, a table until it pays. What was sent to the kitchen
// is never deleted: the portions go to a cancelled line with the reason.
// A dish the kitchen had already started is lost: its stock is not returned,
// the kitchen has to acknowledge the void, and voiding it needs the
// "void dishes already made" permission or a manager's approval.

// OrderVoid is one void as the order pages show it.
type OrderVoid struct {
	ID         int64      `json:"id"`
	ItemID     int64      `json:"item_id"`
	BatchSeq   int        `json:"batch_seq"`
	Name       string     `json:"name"`
	Qty        int        `json:"qty"`
	Amount     int64      `json:"amount"`
	Reason     string     `json:"reason"`
	ReasonKind string     `json:"reason_kind"`
	WasStatus  string     `json:"was_status"`
	Wasted     bool       `json:"wasted"`
	StationID  *int64     `json:"station_id"`
	Before     int64      `json:"total_before"`
	After      int64      `json:"total_after"`
	By         string     `json:"by,omitempty"`
	ApprovedBy string     `json:"approved_by,omitempty"`
	NeedsAck   bool       `json:"needs_ack"`
	AckAt      *time.Time `json:"ack_at"`
	AckBy      string     `json:"ack_by,omitempty"`
	At         time.Time  `json:"at"`
}

// RefundLine is one dish of a refund.
type RefundLine struct {
	ItemID int64  `json:"item_id"`
	Name   string `json:"name"`
	Qty    int    `json:"qty"`
	Amount int64  `json:"amount"`
}

// OrderRefund is money given back by dish after the customer paid.
type OrderRefund struct {
	ID         int64        `json:"id"`
	Lines      []RefundLine `json:"lines"`
	Amount     int64        `json:"amount"`
	Method     string       `json:"method"`
	Reason     string       `json:"reason"`
	ReasonKind string       `json:"reason_kind"`
	By         string       `json:"by,omitempty"`
	ApprovedBy string       `json:"approved_by,omitempty"`
	At         time.Time    `json:"at"`
	// ToCredit is how much went into the customer's store balance; the rest
	// was given back as Method says (docs/REFUND_BALANCE_DESIGN.md §6).
	ToCredit int64  `json:"to_credit"`
	PayType  string `json:"pay_type"`
}

// VoidInput is what the void sheet sends.
type VoidInput struct {
	// Qty is how many portions; 0 means the whole line.
	Qty    int    `json:"qty"`
	Reason string `json:"reason"`
	// ReasonKind is store (the store's fault: the promotion stays) or
	// customer (the promotion is worked out again on what is left).
	ReasonKind string `json:"reason_kind"`
	// ExpectedVersion is the order version the sheet was priced on; 0 skips the check.
	ExpectedVersion int `json:"expected_version"`
}

// VoidQuote is what a void would do, before it is done.
type VoidQuote struct {
	Name   string `json:"name"`
	Qty    int    `json:"qty"`
	MaxQty int    `json:"max_qty"`
	Amount int64  `json:"amount"`
	// WasStatus is how far the dish has got: pending, preparing, ready, served, packed.
	WasStatus string     `json:"was_status"`
	StartedAt *time.Time `json:"started_at"`
	Wasted    bool       `json:"wasted"`
	// NeedsApproval is set when this person may not void a dish already made.
	NeedsApproval bool   `json:"needs_approval"`
	Before        Totals `json:"before"`
	After         Totals `json:"after"`
	// Less is how much less the customer pays.
	Less int64 `json:"less"`
	// The promotion before and after, when it changes.
	PromoBefore string `json:"promo_before"`
	PromoAfter  string `json:"promo_after"`
	PromoDown   bool   `json:"promo_down"`
	Rider       string `json:"rider"`
	Packed      bool   `json:"packed"`
	Version     int    `json:"version"`
}

// VoidResult is the order (or table bill) after a void.
type VoidResult struct {
	Quote     *VoidQuote `json:"quote"`
	Order     *Order     `json:"order"`
	SessionID *int64     `json:"session_id"`
	// AccessToken opens the customer's order page, for the message the desk sends.
	AccessToken string `json:"access_token,omitempty"`
}

var (
	errVoidClosed = Err(http.StatusConflict, "VOID_CLOSED",
		"the order has left the store; dishes can no longer be voided (refund them once delivered)")
	errLastDish = Err(http.StatusConflict, "LAST_DISH",
		"this is the last dish of the order; cancel the order instead")
	errVoidApproval = ErrD(http.StatusForbidden, "VOID_NEEDS_APPROVAL",
		"the kitchen already started this dish; voiding it needs a manager", map[string]any{"perm": PermOrderVoid})
	errDryRun = errors.New("dry run")
)

// voidOpen is the cut-off for voiding: like adding dishes, but a table too.
func voidOpen(o *lockedOrder) bool {
	switch {
	case o.Acceptance != "pending" && o.Acceptance != "accepted":
		return false
	case o.ServiceType == "dine_in":
		return o.Fulfillment != "dined"
	case o.ServiceType == "delivery":
		return o.Fulfillment == "none" || o.Fulfillment == "awaiting_handoff"
	case o.ServiceType == "pickup":
		return o.Fulfillment == "none" || o.Fulfillment == "awaiting_pickup"
	}
	return false
}

// QuoteVoid works out a void without doing it: the void runs in a
// transaction that is rolled back.
func (a *App) QuoteVoid(ctx context.Context, itemID int64, in VoidInput, canMade bool) (*VoidQuote, error) {
	var q *VoidQuote
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		q, err = a.voidLine(ctx, tx, itemID, in, Actor{Type: "staff"}, canMade, true)
		if err != nil {
			return err
		}
		return errDryRun
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return nil, err
	}
	return q, nil
}

// VoidLine takes portions of a line off its order. canMade says the person
// may void a dish the kitchen already started (permission or approval).
func (a *App) VoidLine(ctx context.Context, itemID int64, in VoidInput, actor Actor, canMade bool) (*VoidResult, error) {
	var q *VoidQuote
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		q, err = a.voidLine(ctx, tx, itemID, in, actor, canMade, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	var orderID int64
	if err := a.DB.QueryRow(ctx, `SELECT order_id FROM order_items WHERE id=$1`, itemID).Scan(&orderID); err != nil {
		return nil, err
	}
	o, err := loadOrder(ctx, a.DB, orderID)
	if err != nil {
		return nil, err
	}
	r := &VoidResult{Quote: q, Order: o, SessionID: o.SessionID}
	if o.SessionID == nil {
		r.AccessToken = a.orderToken(o.tokenNonce)
	}
	return r, nil
}

// VoidItem is the table bill's void (PR #105): a reason the desk tapped,
// counted as the store's, by someone who may void anything on the table.
func (a *App) VoidItem(ctx context.Context, itemID int64, reason string, qty int, actor Actor) (*Bill, error) {
	r, err := a.VoidLine(ctx, itemID, VoidInput{Qty: qty, Reason: reason, ReasonKind: "store"}, actor, true)
	if err != nil {
		return nil, err
	}
	if r.SessionID == nil {
		return nil, ErrInvalidState("this is not a dine-in item")
	}
	return loadBill(ctx, a.DB, *r.SessionID)
}

func (a *App) voidLine(ctx context.Context, tx pgx.Tx, itemID int64, in VoidInput, actor Actor, canMade, dry bool) (*VoidQuote, error) {
	if in.Qty < 0 {
		return nil, ErrValidation("qty must be positive")
	}
	reason := strings.TrimSpace(in.Reason)
	if !dry && (reason == "" || len(reason) > 200) {
		return nil, ErrValidation("reason is required")
	}
	if reason == "" {
		reason = "-"
	}
	if in.ReasonKind == "" {
		in.ReasonKind = "store"
	}
	if in.ReasonKind != "store" && in.ReasonKind != "customer" {
		return nil, ErrValidation("reason_kind is store or customer")
	}
	// Lock order first (and the table, for a dine-in line), then the line.
	var orderID int64
	var sessionID *int64
	if err := tx.QueryRow(ctx, `SELECT i.order_id, o.session_id FROM order_items i JOIN orders o ON o.id = i.order_id
		WHERE i.id=$1`, itemID).Scan(&orderID, &sessionID); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if sessionID != nil {
		s, err := lockSession(ctx, tx, *sessionID)
		if err != nil {
			return nil, err
		}
		if s.Status != "open" {
			return nil, ErrSessionClosed
		}
	}
	o, err := lockOrder(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if !voidOpen(o) {
		return nil, errVoidClosed
	}
	if err := checkVersion(o, in.ExpectedVersion); err != nil {
		return nil, err
	}
	var (
		status, name            string
		menuItem, unit, batchID int64
		lineQty                 int
		needsPrep               bool
		served, started         *time.Time
		stationID               *int64
	)
	if err := tx.QueryRow(ctx, `SELECT prep_status, name, item_id, qty, unit_price, batch_id, needs_prep, served_at,
		prep_started_at, station_id FROM order_items WHERE id=$1 FOR UPDATE`, itemID).
		Scan(&status, &name, &menuItem, &lineQty, &unit, &batchID, &needsPrep, &served, &started, &stationID); err != nil {
		return nil, err
	}
	b, err := readBranch(ctx, tx, o.BranchID)
	if err != nil {
		return nil, err
	}
	if status == "cancelled" {
		if dry {
			return nil, ErrInvalidState("this dish was already voided")
		}
		return &VoidQuote{Name: name}, nil // already voided: harmless
	}
	qty := in.Qty
	if qty == 0 || qty > lineQty {
		qty = lineQty
	}
	if o.ServiceType != "dine_in" {
		var left int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(qty), 0) FROM order_items WHERE order_id=$1 AND prep_status <> 'cancelled'`,
			o.ID).Scan(&left); err != nil {
			return nil, err
		}
		if left-qty <= 0 {
			return nil, errLastDish
		}
	}
	was := status
	switch {
	case served != nil:
		was = "served"
	case o.ServiceType != "dine_in" && o.Packing == "packed":
		was = "packed"
	}
	// A dish that needs no cooking (a bottle from the fridge) is never lost
	// unless it already went out with the order.
	wasted := was != "pending" && (needsPrep || was == "served" || was == "packed")
	q := &VoidQuote{Name: name, Qty: qty, MaxQty: lineQty, Amount: unit * int64(qty), WasStatus: was,
		StartedAt: started, Wasted: wasted, NeedsApproval: wasted && !canMade, Packed: was == "packed",
		Version: o.Version}
	if !dry && q.NeedsApproval {
		return nil, errVoidApproval
	}
	before, err := loadOrder(ctx, tx, o.ID)
	if err != nil {
		return nil, err
	}
	q.Before = before.Totals
	if before.Delivery != nil && (before.Delivery.Status == "assigned") {
		q.Rider = before.Delivery.RiderName
	}
	q.PromoBefore = before.Totals.PromotionName
	var promoBefore int64
	for _, d := range before.Discounts {
		if d.Kind == "promotion" {
			promoBefore += d.Amount
		}
	}
	if o.ServiceType == "dine_in" && before.Totals.PromotionID != 0 && promoBefore == 0 {
		promoBefore = before.Totals.MerchantDiscount
	}

	// The line: all of it, or split with the voided portions on a new line.
	voidedLine := itemID
	if qty >= lineQty {
		if _, err := tx.Exec(ctx, `UPDATE order_items SET prep_status='cancelled', void_reason=$2 WHERE id=$1`, itemID, reason); err != nil {
			return nil, err
		}
	} else {
		if err := tx.QueryRow(ctx, `INSERT INTO order_items (order_id, branch_id, batch_id, item_id, name, name_en,
			unit_price, qty, line_total, options, note, needs_prep, prep_status, name_customer, note_tags,
			station_id, station_name, served_at, void_reason, basket_no, prep_started_at, prep_started_by, ready_at, ready_by, base_name)
			SELECT order_id, branch_id, batch_id, item_id, name, name_en, unit_price, $2::int, unit_price * $2::int, options, note,
			needs_prep, 'cancelled', name_customer, note_tags, station_id, station_name, served_at, $3, basket_no,
			prep_started_at, prep_started_by, ready_at, ready_by, base_name
			FROM order_items WHERE id=$1 RETURNING id`, itemID, qty, reason).Scan(&voidedLine); err != nil {
			return nil, wrap("split voided line", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE order_items SET qty = qty - $2::int, line_total = unit_price * (qty - $2::int) WHERE id=$1`,
			itemID, qty); err != nil {
			return nil, err
		}
	}
	if !wasted {
		if _, err := tx.Exec(ctx, `UPDATE menu_items SET stock_qty = stock_qty + $2 WHERE id=$1 AND stock_qty IS NOT NULL`,
			menuItem, qty); err != nil {
			return nil, err
		}
	}

	// The money: everything is priced again on what is left
	// (docs/VOID_ITEM_DESIGN.md §6); a percentage off at checkout carries
	// over, a round-off or a typed amount to pay is dropped.
	if err := moneyEvent(ctx, tx, "void", itemID, actor); err != nil {
		return nil, err
	}
	var after Totals
	if o.ServiceType == "dine_in" {
		if after, err = repriceTable(ctx, tx, b, o.ID, in.ReasonKind == "store"); err != nil {
			return nil, err
		}
	} else {
		if after, err = a.repriceAfterVoid(ctx, tx, b, o, in.ReasonKind); err != nil {
			return nil, err
		}
		if o.Acceptance == "accepted" {
			// A packed order is checked again without the dish.
			if o.Packing == "packed" {
				if _, err := tx.Exec(ctx, `UPDATE orders SET packing_status='waiting_items', fulfillment_status='none'
					WHERE id=$1`, o.ID); err != nil {
					return nil, err
				}
			}
			if err := recomputePacking(ctx, tx, o.ID); err != nil {
				return nil, err
			}
		}
	}
	q.After = after
	q.Less = q.Before.Total - after.Total
	q.PromoAfter = after.PromotionName
	q.PromoDown = after.PromotionDiscount < promoBefore
	if dry {
		return q, nil
	}

	var by, approvedBy *int64
	if actor.ID != 0 {
		by = &actor.ID
	}
	if actor.ApprovedBy != 0 {
		approvedBy = &actor.ApprovedBy
	}
	needsAck := wasted && needsPrep && (was == "preparing" || was == "ready")
	var voidID int64
	if err := tx.QueryRow(ctx, `INSERT INTO order_voids (order_id, branch_id, batch_id, item_id, name, qty, amount, reason,
		reason_kind, was_status, wasted, station_id, total_before, total_after, voided_by, approved_by, needs_ack)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
		o.ID, o.BranchID, batchID, voidedLine, name, qty, q.Amount, reason, in.ReasonKind, was, wasted, stationID,
		q.Before.Total, after.Total, by, approvedBy, needsAck).Scan(&voidID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE order_items SET void_id=$2 WHERE id=$1`, voidedLine, voidID); err != nil {
		return nil, err
	}
	if o.ServiceType != "dine_in" {
		if _, err := tx.Exec(ctx, `UPDATE orders SET version=version+1, manifest_version=manifest_version+1,
			updated_at=now() WHERE id=$1`, o.ID); err != nil {
			return nil, err
		}
	}
	action := "order.item_voided"
	if o.ServiceType == "dine_in" {
		action = "table.item_voided" // as PR #105 logged it
	}
	if err := audit(ctx, tx, o.BranchID, o.ID, actor, action, map[string]any{"item_id": itemID, "void_id": voidID,
		"name": name, "qty": qty, "reason": reason, "kind": in.ReasonKind, "was": was, "wasted": wasted,
		"total_before": q.Before.Total, "total": after.Total}); err != nil {
		return nil, err
	}
	riders := []int64{}
	if o.ServiceType != "dine_in" {
		var riderID int64
		err := tx.QueryRow(ctx, `SELECT rider_id FROM delivery_tasks WHERE order_id=$1 AND status='assigned'`, o.ID).Scan(&riderID)
		if err == nil {
			riders = append(riders, riderID)
			if err := a.pushRider(ctx, tx, riderID, o.ID, "voided"); err != nil {
				return nil, err
			}
		} else if !isNoRows(err) {
			return nil, err
		}
		if err := a.notifyCustomer(ctx, tx, o.ID, "item_voided"); err != nil {
			return nil, err
		}
	}
	return q, notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID, RiderIDs: riders})
}

// voidPolicy is how a void or a refund of a delivery or pickup order
// decides the promotion (docs/VOID_ITEM_DESIGN.md §6): the store's fault
// keeps it, scaled to the share of the dishes still bought (brian,
// 2026-11-24: a big void must not leave the rest almost free); the
// customer's works it out again on what is left,
// with the promotion hours of when they ordered, and may move to another
// promotion that now saves more (pick).
func voidPolicy(m *orderMoney, kind string, pick bool) promoPolicy {
	if kind == "store" {
		return promoPolicy{keep: true, prorate: true}
	}
	return promoPolicy{at: m.Created, current: true, pick: pick}
}

// repriceAfterVoid writes a delivery or pickup order's new amounts after its
// dishes changed (the voided line is already cancelled). Coupons stay but
// never take off more than the promotion left of the dishes.
func (a *App) repriceAfterVoid(ctx context.Context, tx pgx.Tx, b *Branch, o *lockedOrder, kind string) (Totals, error) {
	m, err := loadMoney(ctx, tx, o.ID)
	if err != nil {
		return Totals{}, err
	}
	p, switched, err := m.reprice(ctx, tx, b, nil, voidPolicy(m, kind, true))
	if err != nil {
		return Totals{}, err
	}
	if switched {
		if err := switchPromotion(ctx, tx, b.ID, p.Promo.ID, o.ID, m.Phone, p.Totals.PromotionDiscount); err != nil {
			if !isCode(err, "QUOTE_CHANGED") {
				return Totals{}, err
			}
			// The other promotion ran out meanwhile: the order's own, if it still applies.
			if p, _, err = m.reprice(ctx, tx, b, nil, voidPolicy(m, kind, false)); err != nil {
				return Totals{}, err
			}
		}
	}
	if p.Promo == nil && m.Promo != nil {
		// Nothing applies any more: the promotion's use is given back.
		if err := releasePromotion(ctx, tx, o.ID); err != nil {
			return Totals{}, err
		}
	}
	return p.Totals, saveMoney(ctx, tx, b, o.ID, p, false)
}

// ---- the kitchen ----

// VoidBranch resolves a void to its branch for permission checks.
func (a *App) VoidBranch(ctx context.Context, voidID int64) (int64, error) {
	var b int64
	err := a.DB.QueryRow(ctx, `SELECT branch_id FROM order_voids WHERE id=$1`, voidID).Scan(&b)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return b, err
}

// AckVoid records that the kitchen saw a void of a dish it had started.
func (a *App) AckVoid(ctx context.Context, voidID int64, actor Actor) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var orderID, branchID int64
		var by *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		err := tx.QueryRow(ctx, `UPDATE order_voids SET kitchen_ack_at = COALESCE(kitchen_ack_at, now()),
			kitchen_ack_by = COALESCE(kitchen_ack_by, $2) WHERE id=$1 RETURNING order_id, branch_id`, voidID, by).
			Scan(&orderID, &branchID)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID, OrderID: orderID})
	})
}

// ---- loading ----

const voidCols = `v.id, v.order_id, v.item_id, COALESCE(b.seq, 1), v.name, v.qty, v.amount, v.reason, v.reason_kind,
	v.was_status, v.wasted, v.station_id, v.total_before, v.total_after,
	COALESCE((SELECT u.display_name FROM users u WHERE u.id = v.voided_by), ''),
	COALESCE((SELECT u.display_name FROM users u WHERE u.id = v.approved_by), ''),
	v.needs_ack, v.kitchen_ack_at, COALESCE((SELECT u.display_name FROM users u WHERE u.id = v.kitchen_ack_by), ''),
	v.created_at`

func scanVoid(row interface{ Scan(...any) error }, orderID *int64) (OrderVoid, error) {
	var v OrderVoid
	err := row.Scan(&v.ID, orderID, &v.ItemID, &v.BatchSeq, &v.Name, &v.Qty, &v.Amount, &v.Reason, &v.ReasonKind,
		&v.WasStatus, &v.Wasted, &v.StationID, &v.Before, &v.After, &v.By, &v.ApprovedBy, &v.NeedsAck, &v.AckAt,
		&v.AckBy, &v.At)
	return v, err
}

// loadVoids fills in each order's voids and refunds.
func loadVoids(ctx context.Context, q Querier, byID map[int64]*Order, ids []int64) error {
	rows, err := q.Query(ctx, `SELECT `+voidCols+` FROM order_voids v LEFT JOIN order_batches b ON b.id = v.batch_id
		WHERE v.order_id = ANY($1) ORDER BY v.id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var orderID int64
		v, err := scanVoid(rows, &orderID)
		if err != nil {
			rows.Close()
			return err
		}
		byID[orderID].Voids = append(byID[orderID].Voids, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = q.Query(ctx, `SELECT r.id, r.order_id, r.lines, r.amount, r.method, r.reason, r.reason_kind,
		COALESCE((SELECT u.display_name FROM users u WHERE u.id = r.refunded_by), ''),
		COALESCE((SELECT u.display_name FROM users u WHERE u.id = r.approved_by), ''), r.created_at, r.to_credit, r.pay_type
		FROM order_refunds r WHERE r.order_id = ANY($1) ORDER BY r.id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r OrderRefund
		var orderID int64
		var lines []byte
		if err := rows.Scan(&r.ID, &orderID, &lines, &r.Amount, &r.Method, &r.Reason, &r.ReasonKind, &r.By,
			&r.ApprovedBy, &r.At, &r.ToCredit, &r.PayType); err != nil {
			return err
		}
		r.Lines = []RefundLine{}
		_ = json.Unmarshal(lines, &r.Lines)
		byID[orderID].Refunds = append(byID[orderID].Refunds, r)
	}
	return rows.Err()
}

// orderVoidState fills in whether dishes can still be voided, or refunded.
func (o *Order) voidState() {
	lo := &lockedOrder{ServiceType: o.ServiceType, Acceptance: o.AcceptanceStatus, Fulfillment: o.FulfillmentStatus}
	o.VoidOpen = voidOpen(lo)
	var refunded int64
	for _, r := range o.Refunds {
		refunded += r.Amount
	}
	o.Refunded = refunded
	o.RefundOpen = o.AcceptanceStatus == "accepted" && refunded < o.Totals.Total &&
		(o.FulfillmentStatus == "delivered" || o.FulfillmentStatus == "picked_up" || o.FulfillmentStatus == "dined")
}

// ---- refunds by dish ----

// RefundInput is what the refund sheet sends.
type RefundInput struct {
	Lines []struct {
		ItemID int64 `json:"item_id"`
		Qty    int   `json:"qty"`
	} `json:"lines"`
	// Amount is what was given back; nil takes the suggested amount. It may
	// be less than suggested, never more.
	Amount *int64 `json:"amount"`
	Method string `json:"method"`
	// PayType is how the money went back: cash (out of the drawer),
	// transfer, online or other. Empty works it out from Method.
	PayType    string `json:"pay_type"`
	Reason     string `json:"reason"`
	ReasonKind string `json:"reason_kind"`
	// Dry only works the amount out.
	Dry bool `json:"dry"`
	// ToCredit puts the part the customer paid in cash into their store
	// balance instead (docs/REFUND_BALANCE_DESIGN.md §6.3); Consent is how
	// they agreed: in_person, phone or chat. What was paid from the balance
	// always goes back to it.
	ToCredit bool   `json:"to_credit"`
	Consent  string `json:"consent"`
	// Verify is a code the customer was texted (§5.3), for an order the
	// staff entered: it names whose balance takes the refund.
	Verify *CreditVerify `json:"verify"`
	// IdemKey (the Idempotency-Key header) makes a retried refund record nothing twice.
	IdemKey string `json:"-"`
}

// RefundQuote is the suggested amount of a refund.
type RefundQuote struct {
	Lines     []RefundLine `json:"lines"`
	Dishes    int64        `json:"dishes"`
	Suggested int64        `json:"suggested"`
	Left      int64        `json:"left"`
	// The order's split (§6.1): what it paid from the store balance, and
	// whether the cash part may go into a balance without a texted code.
	CreditPaid int64 `json:"credit_paid"`
	CanCredit  bool  `json:"can_credit"`
	// CashPart and CreditPart split the suggested (or given) amount: the
	// credit part goes back to the balance it came from.
	CashPart   int64 `json:"cash_part"`
	CreditPart int64 `json:"credit_part"`
	// Refund is the refund recorded (not on a dry run).
	RefundID int64 `json:"refund_id,omitempty"`
}

type refundRecord struct {
	RefundID int64 `json:"refund_id"`
}

// Refund records money given back by dish once the customer paid. The
// amount follows the void rules (§6): the dishes' share of what was paid,
// less any promotion the rest no longer earns when the customer is at fault.
func (a *App) Refund(ctx context.Context, orderID int64, in RefundInput, actor Actor) (*RefundQuote, *Order, error) {
	in.Method = strings.TrimSpace(in.Method)
	in.Reason = strings.TrimSpace(in.Reason)
	if in.ReasonKind == "" {
		in.ReasonKind = "store"
	}
	if in.ReasonKind != "store" && in.ReasonKind != "customer" {
		return nil, nil, ErrValidation("reason_kind is store or customer")
	}
	if in.ToCredit {
		if in.Method == "" {
			in.Method = "店铺余额"
		}
		in.PayType = "store_credit"
		if in.Verify != nil {
			in.Consent = "sms"
		}
		if !in.Dry && in.Consent != "in_person" && in.Consent != "phone" && in.Consent != "chat" && in.Consent != "sms" {
			return nil, nil, Err(http.StatusUnprocessableEntity, "CREDIT_CONSENT", "say how the customer agreed to a store balance")
		}
	} else {
		in.Consent = ""
	}
	if !in.Dry && (in.Reason == "" || len(in.Reason) > 200 || in.Method == "" || len([]rune(in.Method)) > 20) {
		return nil, nil, ErrValidation("reason and method are required")
	}
	if len(in.Lines) == 0 {
		return nil, nil, ErrValidation("pick at least one dish")
	}
	if in.PayType == "" {
		in.PayType = refundPayType(in.Method)
	}
	if in.PayType != "cash" && in.PayType != "transfer" && in.PayType != "online" && in.PayType != "other" &&
		in.PayType != "store_credit" {
		return nil, nil, ErrValidation("pay_type is cash, transfer, online or other")
	}
	var out *RefundQuote
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		o, err := lockOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		scope := fmt.Sprintf("refund:%d", orderID)
		if !in.Dry {
			var rec refundRecord
			replayed, err := idemBegin(ctx, tx, scope, in.IdemKey, hashJSON(in), &rec)
			if err != nil {
				return err
			}
			if replayed {
				out = &RefundQuote{Lines: []RefundLine{}, RefundID: rec.RefundID}
				return nil
			}
		}
		if o.Acceptance != "accepted" ||
			(o.Fulfillment != "delivered" && o.Fulfillment != "picked_up" && o.Fulfillment != "dined") {
			return Err(http.StatusConflict, "REFUND_NOT_YET", "refunds by dish are for orders the customer has received; void dishes before that")
		}
		var isPlatform bool
		if err := tx.QueryRow(ctx, `SELECT order_type = 'platform' FROM orders WHERE id=$1`, o.ID).Scan(&isPlatform); err != nil {
			return err
		}
		if isPlatform {
			return ErrInvalidState("the platform refunds its customers; record what it took back instead")
		}
		b, err := readBranch(ctx, tx, o.BranchID)
		if err != nil {
			return err
		}
		// What each line still has that was not refunded.
		refunded := map[int64]int{}
		var paidBack, cashBack int64
		rows, err := tx.Query(ctx, `SELECT lines, amount, cash_part FROM order_refunds WHERE order_id=$1`, o.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw []byte
			var amt, cashPart int64
			if err := rows.Scan(&raw, &amt, &cashPart); err != nil {
				rows.Close()
				return err
			}
			cashBack += cashPart
			var ls []RefundLine
			_ = json.Unmarshal(raw, &ls)
			for _, l := range ls {
				refunded[l.ItemID] += l.Qty
			}
			paidBack += amt
		}
		rows.Close()
		q := &RefundQuote{Lines: []RefundLine{}}
		less := map[int64]int64{}
		seen := map[int64]bool{}
		for _, l := range in.Lines {
			if l.Qty <= 0 || seen[l.ItemID] {
				return ErrValidation("each dish once, with a quantity")
			}
			seen[l.ItemID] = true
			var name string
			var qty int
			var unit int64
			err := tx.QueryRow(ctx, `SELECT name, qty, unit_price FROM order_items WHERE id=$1 AND order_id=$2
				AND prep_status <> 'cancelled'`, l.ItemID, o.ID).Scan(&name, &qty, &unit)
			if isNoRows(err) {
				return ErrValidation("a dish is not on this order")
			} else if err != nil {
				return err
			}
			if l.Qty > qty-refunded[l.ItemID] {
				return ErrValidation("more portions than are left to refund")
			}
			q.Lines = append(q.Lines, RefundLine{ItemID: l.ItemID, Name: name, Qty: l.Qty, Amount: unit * int64(l.Qty)})
			q.Dishes += unit * int64(l.Qty)
			less[l.ItemID] += unit * int64(l.Qty)
		}
		// Price the order as if these dishes had been voided, with everything
		// the order had: its promotion (kept when the store is at fault), its
		// coupons, the percentage off and the round-off given at checkout
		// (docs/PRICING_REVIEW.md, problems 1 and 2). No other promotion is
		// taken for a refund.
		m, err := loadMoney(ctx, tx, o.ID)
		if err != nil {
			return err
		}
		pol := voidPolicy(m, in.ReasonKind, false)
		if o.ServiceType == "dine_in" {
			pol = dinePolicy(m, in.ReasonKind == "store")
		}
		p, _, err := m.reprice(ctx, tx, b, m.without(less), pol)
		if err != nil {
			return err
		}
		after := p.Totals
		after.Total = max(0, after.Total-m.RoundOff)
		q.Left = o.Total - paidBack
		q.Suggested = max(0, min(o.Total-after.Total, q.Left))
		out = q
		oc, err := readOrderCredit(ctx, tx, o.ID)
		if err != nil {
			return err
		}
		q.CreditPaid = oc.CreditPaid
		// Only an order the customer placed themselves names whose balance
		// it is; one the staff entered needs the customer's texted code.
		q.CanCredit = oc.CreditCustomer != 0 || ((oc.Type == TypeMall || oc.Type == TypeWeb) && oc.CustomerID != 0)
		amount := q.Suggested
		if in.Amount != nil && *in.Amount > 0 && *in.Amount <= q.Suggested {
			amount = *in.Amount
		}
		q.CashPart = refundSplit(o.Total, oc.CreditPaid, paidBack, cashBack, amount)
		q.CreditPart = amount - q.CashPart
		if in.Dry {
			return nil
		}
		if in.Amount != nil {
			amount = *in.Amount
		}
		if amount <= 0 || amount > q.Suggested {
			return ErrValidation("the amount must be more than 0 and at most the suggested amount")
		}
		cashPart := refundSplit(o.Total, oc.CreditPaid, paidBack, cashBack, amount)
		creditPart := amount - cashPart
		toCredit := creditPart
		if in.ToCredit {
			toCredit = amount
		}
		// whose balance: the one the order was paid from, or the customer who
		// placed it, or the one whose texted code the staff entered
		creditCustomer := oc.CreditCustomer
		if toCredit > 0 && creditCustomer == 0 {
			switch {
			case in.Verify != nil:
				if creditCustomer, err = a.useCreditVerify(ctx, tx, o.ID, "refund", *in.Verify); err != nil {
					return err
				}
			case q.CanCredit:
				creditCustomer = oc.CustomerID
			default:
				return Err(http.StatusConflict, "CREDIT_NO_CUSTOMER",
					"this order has no customer account: text the customer a code first")
			}
		}
		if in.ToCredit && cashPart > 0 {
			if err := creditRefundLimits(ctx, tx, o.BranchID, cashPart, actor); err != nil {
				return err
			}
		}
		var by, approvedBy *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if actor.ApprovedBy != 0 {
			approvedBy = &actor.ApprovedBy
		}
		var asID *int64
		if err := tx.QueryRow(ctx, `SELECT id FROM order_aftersales WHERE order_id=$1`, o.ID).Scan(&asID); err != nil && !isNoRows(err) {
			return err
		}
		lines, _ := json.Marshal(q.Lines)
		var refundID int64
		if err := tx.QueryRow(ctx, `INSERT INTO order_refunds (order_id, branch_id, lines, amount, method, reason, reason_kind,
			refunded_by, approved_by, aftersale_id, pay_type, cash_part, to_credit, consent)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, o.ID, o.BranchID, lines,
			amount, in.Method, in.Reason, in.ReasonKind, by, approvedBy, asID, in.PayType, cashPart, toCredit, in.Consent).
			Scan(&refundID); err != nil {
			return err
		}
		q.RefundID, q.CashPart, q.CreditPart = refundID, cashPart, creditPart
		if toCredit > 0 {
			if err := creditRefund(ctx, tx, o.ID, refundID, creditCustomer, toCredit, actor); err != nil {
				return err
			}
		}
		if err := idemSave(ctx, tx, scope, in.IdemKey, refundRecord{RefundID: refundID}); err != nil {
			return err
		}
		if asID != nil {
			// The customer's after-sales request is answered by the refund.
			var names []string
			for _, l := range q.Lines {
				names = append(names, fmt.Sprintf("%s ×%d", l.Name, l.Qty))
			}
			reply := fmt.Sprintf("%s %s (%s)", strings.Join(names, ", "), b.Currency+" "+fmt.Sprint(amount), in.Method)
			if _, err := tx.Exec(ctx, `UPDATE order_aftersales SET status='resolved', handled_by=$2, handled_at=now(),
				reply = CASE WHEN reply = '' THEN left($3, 500) ELSE reply END WHERE id=$1 AND status='open'`,
				*asID, by, reply); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET version=version+1, updated_at=now() WHERE id=$1`, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.refunded", map[string]any{"amount": amount,
			"method": in.Method, "reason": in.Reason, "lines": q.Lines}); err != nil {
			return err
		}
		if o.ServiceType != "dine_in" {
			if err := a.notifyCustomer(ctx, tx, o.ID, "refunded"); err != nil {
				return err
			}
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
	if err != nil {
		return nil, nil, err
	}
	if in.Dry {
		return out, nil, nil
	}
	o, err := loadOrder(ctx, a.DB, orderID)
	return out, o, err
}

// ---- the void log ----

// VoidLogRow is one void or refund on the void log page.
type VoidLogRow struct {
	Kind       string     `json:"kind"` // void or refund
	ID         int64      `json:"id"`
	OrderID    int64      `json:"order_id"`
	PublicNo   string     `json:"public_no"`
	Service    string     `json:"service_type"`
	TableName  string     `json:"table_name"`
	Name       string     `json:"name"`
	Qty        int        `json:"qty"`
	Amount     int64      `json:"amount"`
	Reason     string     `json:"reason"`
	ReasonKind string     `json:"reason_kind"`
	WasStatus  string     `json:"was_status"`
	Wasted     bool       `json:"wasted"`
	Method     string     `json:"method,omitempty"`
	By         string     `json:"by"`
	ApprovedBy string     `json:"approved_by"`
	AckAt      *time.Time `json:"ack_at"`
	AckBy      string     `json:"ack_by"`
	At         time.Time  `json:"at"`
}

// VoidSum is one line of a breakdown.
type VoidSum struct {
	Key    string `json:"key"`
	Count  int    `json:"count"`
	Qty    int    `json:"qty"`
	Amount int64  `json:"amount"`
	// Approved counts the voids a manager approved (by staff only).
	Approved int `json:"approved"`
}

// VoidLog is a day of voids and refunds in one store.
type VoidLog struct {
	Date         string       `json:"date"`
	Currency     string       `json:"currency"`
	Timezone     string       `json:"timezone"`
	Voids        int          `json:"voids"`
	VoidQty      int          `json:"void_qty"`
	VoidAmount   int64        `json:"void_amount"`
	WastedAmount int64        `json:"wasted_amount"`
	RefundAmount int64        `json:"refund_amount"`
	RefundOrders int          `json:"refund_orders"`
	ByReason     []VoidSum    `json:"by_reason"`
	ByStaff      []VoidSum    `json:"by_staff"`
	ByDish       []VoidSum    `json:"by_dish"`
	Rows         []VoidLogRow `json:"rows"`
}

// VoidLog lists a day's voids and refunds (the store's own day), newest first.
func (a *App) VoidLog(ctx context.Context, branchID int64, date string) (*VoidLog, error) {
	b, err := readBranch(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	loc := b.location()
	day := time.Now().In(loc)
	if date != "" {
		d, err := time.ParseInLocation("2006-01-02", date, loc)
		if err != nil {
			return nil, ErrValidation("date is YYYY-MM-DD")
		}
		day = d
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	out := &VoidLog{Date: start.Format("2006-01-02"), Currency: b.Currency, Timezone: loc.String(),
		ByReason: []VoidSum{}, ByStaff: []VoidSum{}, ByDish: []VoidSum{}, Rows: []VoidLogRow{}}
	rows, err := a.DB.Query(ctx, `SELECT `+voidCols+`, o.public_no, o.service_type, o.table_name
		FROM order_voids v LEFT JOIN order_batches b ON b.id = v.batch_id JOIN orders o ON o.id = v.order_id
		WHERE v.branch_id=$1 AND v.created_at >= $2 AND v.created_at < $3 ORDER BY v.created_at DESC`, branchID, start, end)
	if err != nil {
		return nil, err
	}
	sums := map[string]map[string]*VoidSum{"reason": {}, "staff": {}, "dish": {}}
	add := func(group, key string, qty int, amount int64, approved bool) {
		s := sums[group][key]
		if s == nil {
			s = &VoidSum{Key: key}
			sums[group][key] = s
		}
		s.Count++
		s.Qty += qty
		s.Amount += amount
		if approved {
			s.Approved++
		}
	}
	for rows.Next() {
		var orderID int64
		var r VoidLogRow
		v, err := scanVoidWith(rows, &orderID, &r)
		if err != nil {
			rows.Close()
			return nil, err
		}
		r.Kind, r.ID, r.OrderID, r.Name, r.Qty, r.Amount = "void", v.ID, orderID, v.Name, v.Qty, v.Amount
		r.Reason, r.ReasonKind, r.WasStatus, r.Wasted = v.Reason, v.ReasonKind, v.WasStatus, v.Wasted
		r.By, r.ApprovedBy, r.AckAt, r.AckBy, r.At = v.By, v.ApprovedBy, v.AckAt, v.AckBy, v.At
		out.Rows = append(out.Rows, r)
		out.Voids++
		out.VoidQty += v.Qty
		out.VoidAmount += v.Amount
		if v.Wasted {
			out.WastedAmount += v.Amount
		}
		add("reason", v.Reason, v.Qty, v.Amount, false)
		add("staff", v.By, v.Qty, v.Amount, v.ApprovedBy != "")
		add("dish", v.Name, v.Qty, v.Amount, false)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rr, err := a.DB.Query(ctx, `SELECT r.id, r.order_id, o.public_no, o.service_type, o.table_name, r.lines, r.amount,
		r.method, r.reason, r.reason_kind, COALESCE((SELECT u.display_name FROM users u WHERE u.id = r.refunded_by), ''),
		COALESCE((SELECT u.display_name FROM users u WHERE u.id = r.approved_by), ''), r.created_at
		FROM order_refunds r JOIN orders o ON o.id = r.order_id
		WHERE r.branch_id=$1 AND r.created_at >= $2 AND r.created_at < $3 ORDER BY r.created_at DESC`, branchID, start, end)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	orders := map[int64]bool{}
	for rr.Next() {
		var r VoidLogRow
		var raw []byte
		if err := rr.Scan(&r.ID, &r.OrderID, &r.PublicNo, &r.Service, &r.TableName, &raw, &r.Amount, &r.Method, &r.Reason,
			&r.ReasonKind, &r.By, &r.ApprovedBy, &r.At); err != nil {
			return nil, err
		}
		var ls []RefundLine
		_ = json.Unmarshal(raw, &ls)
		var names []string
		for _, l := range ls {
			names = append(names, fmt.Sprintf("%s ×%d", l.Name, l.Qty))
			r.Qty += l.Qty
		}
		r.Kind, r.Name = "refund", strings.Join(names, "、")
		out.Rows = append(out.Rows, r)
		out.RefundAmount += r.Amount
		orders[r.OrderID] = true
	}
	if err := rr.Err(); err != nil {
		return nil, err
	}
	out.RefundOrders = len(orders)
	for _, g := range []struct {
		key string
		to  *[]VoidSum
	}{{"reason", &out.ByReason}, {"staff", &out.ByStaff}, {"dish", &out.ByDish}} {
		for _, s := range sums[g.key] {
			*g.to = append(*g.to, *s)
		}
		sortSums(*g.to)
	}
	sortRows(out.Rows)
	return out, nil
}

func scanVoidWith(row interface{ Scan(...any) error }, orderID *int64, r *VoidLogRow) (OrderVoid, error) {
	var v OrderVoid
	err := row.Scan(&v.ID, orderID, &v.ItemID, &v.BatchSeq, &v.Name, &v.Qty, &v.Amount, &v.Reason, &v.ReasonKind,
		&v.WasStatus, &v.Wasted, &v.StationID, &v.Before, &v.After, &v.By, &v.ApprovedBy, &v.NeedsAck, &v.AckAt,
		&v.AckBy, &v.At, &r.PublicNo, &r.Service, &r.TableName)
	return v, err
}

func sortSums(s []VoidSum) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Amount != s[j].Amount {
			return s[i].Amount > s[j].Amount
		}
		return s[i].Key < s[j].Key
	})
}

func sortRows(r []VoidLogRow) {
	sort.SliceStable(r, func(i, j int) bool { return r[i].At.After(r[j].At) })
}

// ItemDineIn says whether a line is on a table bill.
func (a *App) ItemDineIn(ctx context.Context, itemID int64) (bool, error) {
	var dine bool
	err := a.DB.QueryRow(ctx, `SELECT o.service_type = 'dine_in' FROM order_items i JOIN orders o ON o.id = i.order_id
		WHERE i.id=$1`, itemID).Scan(&dine)
	if isNoRows(err) {
		return false, ErrNotFound
	}
	return dine, err
}

// cashWords are "cash" as the refund sheet offers it in each language.
var cashWords = map[string]bool{"现金": true, "現金": true, "cash": true, "efectivo": true, "espèces": true,
	"наличные": true, "tiền mặt": true, "เงินสด": true, "نقدًا": true, "ငွေသား": true, "សាច់ប្រាក់": true}

// refundPayType works out how a refund went back from its method's name.
func refundPayType(method string) string {
	if cashWords[strings.ToLower(strings.TrimSpace(method))] {
		return "cash"
	}
	return "other"
}
