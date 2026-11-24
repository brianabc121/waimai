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
	return &VoidResult{Quote: q, Order: o, SessionID: o.SessionID}, nil
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
	if o.ServiceType == "dine_in" && before.Totals.PromotionID != 0 {
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
			station_id, station_name, served_at, void_reason, basket_no, prep_started_at, prep_started_by, ready_at, ready_by)
			SELECT order_id, branch_id, batch_id, item_id, name, name_en, unit_price, $2::int, unit_price * $2::int, options, note,
			needs_prep, 'cancelled', name_customer, note_tags, station_id, station_name, served_at, $3, basket_no,
			prep_started_at, prep_started_by, ready_at, ready_by
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

	// The money.
	var after Totals
	if o.ServiceType == "dine_in" {
		var keepID, keepAmount int64
		if in.ReasonKind == "store" {
			keepID, keepAmount = before.Totals.PromotionID, before.Totals.MerchantDiscount
		}
		if after, err = recomputeBillKeep(ctx, tx, b, o.ID, keepID, keepAmount); err != nil {
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

// voidPrice is a delivery or pickup order's amounts with removed less dish
// money (docs/VOID_ITEM_DESIGN.md §6): the delivery and packaging fees and
// the coupons stay; the promotion keeps its amount when the store is at
// fault, and is worked out again on what is left when the customer is.
func voidPrice(ctx context.Context, q Querier, b *Branch, m *orderMoney, ordered time.Time, removed int64, kind string, mayPick bool) (*addonPrice, error) {
	sub := max(m.Subtotal-removed, 0)
	res := &addonPrice{}
	var cur *Promotion
	if m.PromotionID != nil {
		ps, err := loadPromotions(ctx, q, b.ID, "p.id = "+fmt.Sprint(*m.PromotionID), "")
		if err != nil {
			return nil, err
		}
		if len(ps) == 1 {
			cur = &ps[0]
		}
	}
	if kind == "store" {
		if cur != nil && m.promoAmount > 0 {
			res.promo = cur
			if cur.Kind == "free_delivery" {
				res.promoD = Discount{Delivery: min(m.promoAmount, m.DeliveryFee)}
			} else {
				res.promoD = Discount{Goods: min(m.promoAmount, sub)}
			}
		}
	} else {
		if cur != nil && sub >= cur.Threshold {
			res.promo, res.promoD = cur, cur.discount(sub, m.DeliveryFee)
		}
		if mayPick {
			promos, err := loadPromotions(ctx, q, b.ID, "p.status='active'", m.Phone)
			if err != nil {
				return nil, err
			}
			// The promotion's hours are those of when the customer ordered.
			if best, d := bestPromotion(promos, ordered, b.location(), m.ServiceType, sub, m.DeliveryFee); best != nil &&
				(cur == nil || best.ID != cur.ID) && d.total() > res.promoD.total() {
				res.promo, res.promoD, res.switched = best, d, true
			}
		}
	}
	cfg := b.taxConfig()
	cfg.PackagingFee, cfg.TaxRateBP, cfg.TaxInclusive = m.PackagingFee, m.TaxRateBP, m.TaxInclusive
	t := ComputeTotals(sub, m.DeliveryFee, cfg, Discount{Goods: res.promoD.Goods + m.couponGoods,
		Delivery: res.promoD.Delivery + m.couponShip})
	if res.promo != nil {
		t.PromotionID, t.PromotionName, t.PromotionNameEn = res.promo.ID, res.promo.Name, res.promo.NameEn
		t.PromotionDiscount = min(res.promoD.total(), t.MerchantDiscount)
	}
	t.applyPlatform(m.platf)
	res.Totals = t
	return res, nil
}

// repriceAfterVoid writes a delivery or pickup order's new amounts after its
// dishes changed (the voided line is already cancelled).
func (a *App) repriceAfterVoid(ctx context.Context, tx pgx.Tx, b *Branch, o *lockedOrder, kind string) (Totals, error) {
	m, err := readOrderMoney(ctx, tx, o.ID)
	if err != nil {
		return Totals{}, err
	}
	var live int64
	var ordered time.Time
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT sum(line_total) FROM order_items WHERE order_id=$1
		AND prep_status <> 'cancelled'), 0), (SELECT created_at FROM orders WHERE id=$1)`, o.ID).Scan(&live, &ordered); err != nil {
		return Totals{}, err
	}
	p, err := voidPrice(ctx, tx, b, m, ordered, m.Subtotal-live, kind, true)
	if err != nil {
		return Totals{}, err
	}
	if p.switched {
		if err := switchPromotion(ctx, tx, b.ID, p.promo.ID, o.ID, m.Phone, p.Totals.PromotionDiscount); err != nil {
			if !isCode(err, "QUOTE_CHANGED") {
				return Totals{}, err
			}
			// The other promotion ran out meanwhile: the order's own, if it still applies.
			if p, err = voidPrice(ctx, tx, b, m, ordered, m.Subtotal-live, kind, false); err != nil {
				return Totals{}, err
			}
		}
	}
	t := p.Totals
	var promoID *int64
	if p.promo != nil && t.PromotionDiscount > 0 {
		promoID = &p.promo.ID
	} else {
		t.PromotionID, t.PromotionName, t.PromotionNameEn, t.PromotionDiscount = 0, "", "", 0
		if m.PromotionID != nil {
			// Nothing applies any more: the promotion's use is given back.
			if err := releasePromotion(ctx, tx, o.ID); err != nil {
				return Totals{}, err
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET subtotal=$2, service_charge=$3, tax_amount=$4, discount=$5, total=$6,
		merchant_discount=$7, platform_subsidy=$8, promotion_id=$9, promotion_name=$10, promotion_name_en=$11,
		version=version+1, updated_at=now() WHERE id=$1`,
		o.ID, t.Subtotal, t.ServiceCharge, t.TaxAmount, t.Discount, t.Total, t.MerchantDiscount, t.PlatformSubsidy,
		promoID, t.PromotionName, t.PromotionNameEn); err != nil {
		return Totals{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM order_discounts WHERE order_id=$1 AND kind='promotion'`, o.ID); err != nil {
		return Totals{}, err
	}
	if promoID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO order_discounts (order_id, branch_id, kind, funded_by, name, name_en,
			amount, promotion_id) VALUES ($1,$2,'promotion','store',$3,$4,$5,$6)`, o.ID, b.ID, t.PromotionName,
			t.PromotionNameEn, t.PromotionDiscount, *promoID); err != nil {
			return Totals{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE promotion_redemptions SET amount=$2 WHERE order_id=$1 AND status='applied'`,
			o.ID, t.PromotionDiscount); err != nil {
			return Totals{}, err
		}
	}
	// Coupons stay, but never take off more than there is left.
	if t.Subtotal < m.couponGoods {
		if _, err := tx.Exec(ctx, `WITH c AS (SELECT id, amount, sum(amount) OVER (ORDER BY id) AS run FROM order_discounts
			WHERE order_id=$1 AND kind IN ('store_threshold','store_free'))
			UPDATE order_discounts d SET amount = GREATEST(0, LEAST(c.amount, $2 - (c.run - c.amount)))
			FROM c WHERE d.id = c.id`, o.ID, t.Subtotal); err != nil {
			return Totals{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE order_discounts SET amount=$2 WHERE order_id=$1 AND kind='platform_free'`,
		o.ID, t.PlatformSubsidy); err != nil {
		return Totals{}, err
	}
	return t, nil
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
		COALESCE((SELECT u.display_name FROM users u WHERE u.id = r.approved_by), ''), r.created_at
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
			&r.ApprovedBy, &r.At); err != nil {
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
	Amount     *int64 `json:"amount"`
	Method     string `json:"method"`
	Reason     string `json:"reason"`
	ReasonKind string `json:"reason_kind"`
	// Dry only works the amount out.
	Dry bool `json:"dry"`
}

// RefundQuote is the suggested amount of a refund.
type RefundQuote struct {
	Lines     []RefundLine `json:"lines"`
	Dishes    int64        `json:"dishes"`
	Suggested int64        `json:"suggested"`
	Left      int64        `json:"left"`
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
	if !in.Dry && (in.Reason == "" || len(in.Reason) > 200 || in.Method == "" || len([]rune(in.Method)) > 20) {
		return nil, nil, ErrValidation("reason and method are required")
	}
	if len(in.Lines) == 0 {
		return nil, nil, ErrValidation("pick at least one dish")
	}
	var out *RefundQuote
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		o, err := lockOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if o.Acceptance != "accepted" ||
			(o.Fulfillment != "delivered" && o.Fulfillment != "picked_up" && o.Fulfillment != "dined") {
			return Err(http.StatusConflict, "REFUND_NOT_YET", "refunds by dish are for orders the customer has received; void dishes before that")
		}
		b, err := readBranch(ctx, tx, o.BranchID)
		if err != nil {
			return err
		}
		// What each line still has that was not refunded.
		refunded := map[int64]int{}
		var paidBack int64
		rows, err := tx.Query(ctx, `SELECT lines, amount FROM order_refunds WHERE order_id=$1`, o.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw []byte
			var amt int64
			if err := rows.Scan(&raw, &amt); err != nil {
				rows.Close()
				return err
			}
			var ls []RefundLine
			_ = json.Unmarshal(raw, &ls)
			for _, l := range ls {
				refunded[l.ItemID] += l.Qty
			}
			paidBack += amt
		}
		rows.Close()
		q := &RefundQuote{Lines: []RefundLine{}}
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
		}
		// Price the order as if these dishes had been voided.
		var after Totals
		if o.ServiceType == "dine_in" {
			var sub int64
			if err := tx.QueryRow(ctx, `SELECT subtotal FROM orders WHERE id=$1`, o.ID).Scan(&sub); err != nil {
				return err
			}
			after = ComputeTotals(max(sub-q.Dishes, 0), 0, b.dineInTax(), Discount{})
		} else {
			m, err := readOrderMoney(ctx, tx, o.ID)
			if err != nil {
				return err
			}
			var ordered time.Time
			if err := tx.QueryRow(ctx, `SELECT created_at FROM orders WHERE id=$1`, o.ID).Scan(&ordered); err != nil {
				return err
			}
			// No other promotion is taken for a refund: the order keeps its own or none.
			p, err := voidPrice(ctx, tx, b, m, ordered, q.Dishes, in.ReasonKind, false)
			if err != nil {
				return err
			}
			after = p.Totals
		}
		q.Left = o.Total - paidBack
		q.Suggested = max(0, min(o.Total-after.Total, q.Left))
		out = q
		if in.Dry {
			return nil
		}
		amount := q.Suggested
		if in.Amount != nil {
			amount = *in.Amount
		}
		if amount <= 0 || amount > q.Suggested {
			return ErrValidation("the amount must be more than 0 and at most the suggested amount")
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
		if _, err := tx.Exec(ctx, `INSERT INTO order_refunds (order_id, branch_id, lines, amount, method, reason, reason_kind,
			refunded_by, approved_by, aftersale_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, o.ID, o.BranchID, lines, amount,
			in.Method, in.Reason, in.ReasonKind, by, approvedBy, asID); err != nil {
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
