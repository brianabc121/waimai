package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Adding dishes to a delivery or pickup order before it leaves the store
// (docs/ADD_ON_ORDER_DESIGN.md). An add-on is one more batch of the same
// order, like a dine-in round: what was sent to the kitchen stays as it is.
// Staff add-ons take effect at once; the customer's own wait for the store.

// AddonCustomerLimit is how many add-ons a customer may make on one order.
const AddonCustomerLimit = 3

// OrderBatch is one batch of an order as the order pages show it: the
// original order is batch 1, each add-on (or dine-in round) one more.
type OrderBatch struct {
	Seq       int        `json:"seq"`
	Source    string     `json:"source"`
	Status    string     `json:"status"`
	At        time.Time  `json:"at"`
	By        string     `json:"by,omitempty"`
	RoundTags []string   `json:"round_tags"`
	Before    *int64     `json:"total_before"`
	After     *int64     `json:"total_after"`
	Reason    string     `json:"reject_reason,omitempty"`
	ExpiresAt *time.Time `json:"expires_at"`
	// Lines are the dishes of an add-on still waiting for the store (the
	// others are in the order's items).
	Lines []QuoteLine `json:"lines,omitempty"`
}

// AddonState says whether the order can still take dishes and why not.
type AddonState struct {
	Open bool `json:"open"`
	// Why is the reason it is closed: handed_off, closed, dine_in.
	Why string `json:"why,omitempty"`
	// Repack is true when the order is packed: adding means packing again.
	Repack bool `json:"repack"`
	// CustomerLeft is how many more add-ons the customer may make.
	CustomerLeft int `json:"customer_left"`
	// Pending is an add-on of the customer's waiting for the store.
	Pending bool `json:"pending"`
}

var errAddonClosed = Err(http.StatusConflict, "ADDON_CLOSED",
	"the order has left the store; dishes can no longer be added to it")

// addonOpen checks the cut-off: a delivery until the rider takes it, a pickup
// until the customer collects it, and never after the order was closed.
func addonOpen(o *lockedOrder) (bool, string) {
	switch {
	case o.ServiceType == "dine_in":
		return false, "dine_in"
	case o.Acceptance != "pending" && o.Acceptance != "accepted":
		return false, "closed"
	case o.ServiceType == "delivery" && o.Fulfillment != "none" && o.Fulfillment != "awaiting_handoff":
		return false, "handed_off"
	case o.ServiceType == "pickup" && o.Fulfillment != "none" && o.Fulfillment != "awaiting_pickup":
		return false, "handed_off"
	}
	return true, ""
}

func addonState(ctx context.Context, q Querier, o *lockedOrder) (*AddonState, error) {
	open, why := addonOpen(o)
	st := &AddonState{Open: open, Why: why, Repack: o.Packing == "packed"}
	var used int
	if err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status IN ('pending','accepted')),
		count(*) FILTER (WHERE status='pending') > 0 FROM order_batches WHERE order_id=$1 AND source='customer'`, o.ID).
		Scan(&used, &st.Pending); err != nil {
		return nil, err
	}
	st.CustomerLeft = max(0, AddonCustomerLimit-used)
	return st, nil
}

// ---- pricing ----

// orderMoney is what an order was priced with, read under the order lock.
type orderMoney struct {
	Subtotal, DeliveryFee, PackagingFee int64
	TaxRateBP                           int
	TaxInclusive                        bool
	PromotionID                         *int64
	Phone, ServiceType                  string
	Total                               int64
	// What the order's discounts took off, by who pays and on what.
	promoAmount                    int64
	couponGoods, couponShip, platf int64
}

func readOrderMoney(ctx context.Context, q Querier, orderID int64) (*orderMoney, error) {
	var m orderMoney
	if err := q.QueryRow(ctx, `SELECT subtotal, delivery_fee, packaging_fee, tax_rate_bp, tax_inclusive,
		promotion_id, customer_phone, service_type, total FROM orders WHERE id=$1`, orderID).Scan(&m.Subtotal,
		&m.DeliveryFee, &m.PackagingFee, &m.TaxRateBP, &m.TaxInclusive, &m.PromotionID, &m.Phone, &m.ServiceType,
		&m.Total); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT kind, amount FROM order_discounts WHERE order_id=$1`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var amount int64
		if err := rows.Scan(&kind, &amount); err != nil {
			return nil, err
		}
		switch kind {
		case "promotion":
			m.promoAmount += amount
		case CouponStoreThreshold, CouponStoreFree:
			m.couponGoods += amount
		case CouponStoreDelivery:
			m.couponShip += amount
		case CouponPlatformFree:
			m.platf += amount
		}
	}
	return &m, rows.Err()
}

// addonPrice is the order's amounts with an add-on of extra (dish money).
type addonPrice struct {
	Totals Totals
	// promo is the promotion now applied, nil when none; switched says it is
	// another one than the order had.
	promo    *Promotion
	promoD   Discount
	switched bool
}

// priceWithAddon recomputes an order's amounts with extra more dish money
// (docs/ADD_ON_ORDER_DESIGN.md §5): the delivery fee and packaging fee stay,
// the coupons used stay, and the promotion is chosen again on the new amount
// but never takes off less than it did.
func priceWithAddon(ctx context.Context, q Querier, b *Branch, m *orderMoney, extra int64, now time.Time) (*addonPrice, error) {
	sub := m.Subtotal + extra
	var keep *Promotion
	var keepD Discount
	if m.PromotionID != nil {
		ps, err := loadPromotions(ctx, q, b.ID, "p.id = "+fmt.Sprint(*m.PromotionID), "")
		if err != nil {
			return nil, err
		}
		if len(ps) == 1 {
			keep = &ps[0]
			keepD = keep.discount(sub, m.DeliveryFee)
			if keepD.total() < m.promoAmount { // never less than the order had
				if keep.Kind == "free_delivery" {
					keepD = Discount{Delivery: m.promoAmount}
				} else {
					keepD = Discount{Goods: m.promoAmount}
				}
			}
		}
	}
	res := &addonPrice{promo: keep, promoD: keepD}
	promos, err := loadPromotions(ctx, q, b.ID, "p.status='active'", m.Phone)
	if err != nil {
		return nil, err
	}
	if best, d := bestPromotion(promos, now, b.location(), m.ServiceType, sub, m.DeliveryFee); best != nil &&
		(keep == nil || best.ID != keep.ID) && d.total() > keepD.total() {
		res.promo, res.promoD, res.switched = best, d, true
	}
	cfg := b.taxConfig()
	cfg.PackagingFee, cfg.TaxRateBP, cfg.TaxInclusive = m.PackagingFee, m.TaxRateBP, m.TaxInclusive
	d := Discount{Goods: res.promoD.Goods + m.couponGoods, Delivery: res.promoD.Delivery + m.couponShip}
	t := ComputeTotals(sub, m.DeliveryFee, cfg, d)
	if res.promo != nil {
		t.PromotionID, t.PromotionName, t.PromotionNameEn = res.promo.ID, res.promo.Name, res.promo.NameEn
		t.PromotionDiscount = min(res.promoD.total(), t.MerchantDiscount)
	}
	t.applyPlatform(m.platf)
	res.Totals = t
	return res, nil
}

// AddonQuote is what an add-on would cost, before it is placed.
type AddonQuote struct {
	Currency string      `json:"currency"`
	Lines    []QuoteLine `json:"lines"`
	// Subtotal is the dishes of this add-on.
	Subtotal int64 `json:"subtotal"`
	// Before and After are the whole order's amounts.
	Before Totals `json:"before"`
	After  Totals `json:"after"`
	// Extra is what the customer pays more.
	Extra int64 `json:"extra"`
	// PromoUp is set when the promotion takes off more than before.
	PromoUp bool        `json:"promo_up"`
	State   *AddonState `json:"state"`
}

// AddonInput is the dishes to add. Staff lines may repeat an option.
type AddonInput struct {
	Items     []QuoteLineInput `json:"items"`
	RoundTags []string         `json:"round_tags"`
	// ExpectedTotal is the order total the client showed; a different one
	// means prices moved and the add-on is refused (QUOTE_CHANGED).
	ExpectedTotal *int64 `json:"expected_total"`
	Lang          string `json:"lang"`
}

// StaffAddonInput is AddonInput as the order desk sends it.
type StaffAddonInput struct {
	Items         []StaffLine `json:"items"`
	RoundTags     []string    `json:"round_tags"`
	ExpectedTotal *int64      `json:"expected_total"`
}

func (in StaffAddonInput) addon() AddonInput {
	out := AddonInput{RoundTags: in.RoundTags, ExpectedTotal: in.ExpectedTotal}
	for _, l := range in.Items {
		out.Items = append(out.Items, QuoteLineInput{ItemID: l.ItemID, Qty: l.Qty, OptionIDs: l.OptionIDs,
			Note: strings.TrimSpace(l.Note), NoteTagIDs: l.NoteTagIDs, repeat: true})
	}
	return out
}

func readBranch(ctx context.Context, q Querier, id int64) (*Branch, error) {
	return scanBranch(q.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, id))
}

// quoteAddon prices an add-on against the live menu, inside or outside a
// transaction. The order must still be open for dishes.
func quoteAddon(ctx context.Context, q Querier, b *Branch, o *lockedOrder, in AddonInput, customer bool) (*AddonQuote, error) {
	if open, why := addonOpen(o); !open {
		if why == "dine_in" {
			return nil, ErrInvalidState("add dishes to a table from the dine-in desk")
		}
		return nil, errAddonClosed
	}
	if len(in.Items) == 0 {
		return nil, ErrValidation("add at least one dish")
	}
	lines, sub, err := priceLines(ctx, q, b.ID, o.ServiceType, in.Items)
	if err != nil {
		return nil, err
	}
	for i := range lines {
		lines[i].Basket = 1
	}
	if customer {
		if err := localizeLines(ctx, q, b, in.Lang, lines); err != nil {
			return nil, err
		}
	}
	m, err := readOrderMoney(ctx, q, o.ID)
	if err != nil {
		return nil, err
	}
	before, err := priceWithAddon(ctx, q, b, m, 0, time.Now())
	if err != nil {
		return nil, err
	}
	// "Before" is what the order says now, not a fresh pricing of it.
	before.Totals.Total = m.Total
	after, err := priceWithAddon(ctx, q, b, m, sub, time.Now())
	if err != nil {
		return nil, err
	}
	st, err := addonState(ctx, q, o)
	if err != nil {
		return nil, err
	}
	return &AddonQuote{Currency: b.Currency, Lines: lines, Subtotal: sub, Before: before.Totals, After: after.Totals,
		Extra: after.Totals.Total - m.Total, PromoUp: after.promoD.total() > m.promoAmount, State: st}, nil
}

// ---- applying an add-on ----

// applyAddon writes the dishes into the order as batch seq (already inserted
// as batchID), reprices the order and tells the kitchen, the packer and the
// rider. The order is locked by the caller.
func (a *App) applyAddon(ctx context.Context, tx pgx.Tx, b *Branch, o *lockedOrder, batchID int64, lines []QuoteLine, actor Actor) (Totals, error) {
	if err := reserveStock(ctx, tx, lines); err != nil {
		return Totals{}, err
	}
	accepted := o.Acceptance == "accepted"
	var sub int64
	for _, l := range lines {
		sub += l.LineTotal
		opts, _ := json.Marshal(l.Options)
		status := "pending"
		if accepted && !l.NeedsPrep {
			status = "ready"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO order_items (order_id, branch_id, batch_id, item_id, name, name_en,
			unit_price, qty, line_total, options, note, needs_prep, prep_status, basket_no, name_customer, note_tags)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, o.ID, b.ID, batchID, l.ItemID, l.Name,
			l.NameEn, l.UnitPrice, l.Qty, l.LineTotal, opts, l.Note, l.NeedsPrep, status, max(1, l.Basket), l.NameTr,
			noteTagsJSON(l.NoteTags)); err != nil {
			return Totals{}, wrap("insert add-on item", err)
		}
	}
	if accepted {
		// An accepted order's kitchen work exists already: send these too.
		if _, err := tx.Exec(ctx, `UPDATE order_items oi SET station_id = st.id, station_name = st.name
			FROM menu_items m JOIN kitchen_stations st ON st.id = m.station_id AND st.active
			WHERE oi.batch_id=$1 AND oi.needs_prep AND m.id = oi.item_id`, batchID); err != nil {
			return Totals{}, err
		}
	}
	m, err := readOrderMoney(ctx, tx, o.ID)
	if err != nil {
		return Totals{}, err
	}
	p, err := priceWithAddon(ctx, tx, b, m, sub, time.Now())
	if err != nil {
		return Totals{}, err
	}
	if p.switched {
		// Another promotion now saves more: give the old use back and take one
		// of the new. If the new one ran out meanwhile, keep the old one.
		if err := switchPromotion(ctx, tx, b.ID, p.promo.ID, o.ID, m.Phone, p.Totals.PromotionDiscount); err != nil {
			if !isCode(err, "QUOTE_CHANGED") {
				return Totals{}, err
			}
			if p, err = priceWithAddonOnly(ctx, tx, b, m, sub); err != nil {
				return Totals{}, err
			}
		}
	}
	t := p.Totals
	var promoID *int64
	if p.promo != nil {
		promoID = &p.promo.ID
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET subtotal=$2, service_charge=$3, tax_amount=$4, discount=$5, total=$6,
		merchant_discount=$7, platform_subsidy=$8, promotion_id=$9, promotion_name=$10, promotion_name_en=$11,
		version=version+1, manifest_version=manifest_version+1, updated_at=now() WHERE id=$1`,
		o.ID, t.Subtotal, t.ServiceCharge, t.TaxAmount, t.Discount, t.Total, t.MerchantDiscount, t.PlatformSubsidy,
		promoID, t.PromotionName, t.PromotionNameEn); err != nil {
		return Totals{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM order_discounts WHERE order_id=$1 AND kind='promotion'`, o.ID); err != nil {
		return Totals{}, err
	}
	if promoID != nil && t.PromotionDiscount > 0 {
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
	if _, err := tx.Exec(ctx, `UPDATE order_batches SET total_before=$2, total_after=$3 WHERE id=$1`,
		batchID, m.Total, t.Total); err != nil {
		return Totals{}, err
	}
	if accepted {
		// A packed order goes back to waiting for its dishes and is packed again.
		if o.Packing == "packed" {
			if _, err := tx.Exec(ctx, `UPDATE orders SET packing_status='waiting_items', fulfillment_status='none'
				WHERE id=$1`, o.ID); err != nil {
				return Totals{}, err
			}
		}
		if err := recomputePacking(ctx, tx, o.ID); err != nil {
			return Totals{}, err
		}
	}
	riders := []int64{}
	var riderID int64
	err = tx.QueryRow(ctx, `SELECT rider_id FROM delivery_tasks WHERE order_id=$1 AND status='assigned'`, o.ID).Scan(&riderID)
	if err == nil {
		riders = append(riders, riderID)
		if err := a.pushRider(ctx, tx, riderID, o.ID, "appended"); err != nil {
			return Totals{}, err
		}
	} else if !isNoRows(err) {
		return Totals{}, err
	}
	if err := audit(ctx, tx, b.ID, o.ID, actor, "order.addon", map[string]any{"batch_id": batchID,
		"lines": len(lines), "batch_total": sub, "total_before": m.Total, "total": t.Total}); err != nil {
		return Totals{}, err
	}
	return t, notify(ctx, tx, Event{BranchID: b.ID, OrderID: o.ID, RiderIDs: riders})
}

// priceWithAddonOnly is priceWithAddon without looking for a better promotion.
func priceWithAddonOnly(ctx context.Context, q Querier, b *Branch, m *orderMoney, extra int64) (*addonPrice, error) {
	sub := m.Subtotal + extra
	res := &addonPrice{}
	if m.PromotionID != nil {
		ps, err := loadPromotions(ctx, q, b.ID, "p.id = "+fmt.Sprint(*m.PromotionID), "")
		if err != nil {
			return nil, err
		}
		if len(ps) == 1 {
			res.promo = &ps[0]
			res.promoD = res.promo.discount(sub, m.DeliveryFee)
			if res.promoD.total() < m.promoAmount {
				res.promoD = Discount{Goods: m.promoAmount}
				if res.promo.Kind == "free_delivery" {
					res.promoD = Discount{Delivery: m.promoAmount}
				}
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

// switchPromotion moves the order's promotion use to another promotion
// inside a savepoint, so that a promotion that ran out leaves the old use as
// it was and the transaction usable.
func switchPromotion(ctx context.Context, tx pgx.Tx, branchID, promoID, orderID int64, phone string, amount int64) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	err = releasePromotion(ctx, sp, orderID)
	if err == nil {
		_, err = sp.Exec(ctx, `DELETE FROM promotion_redemptions WHERE order_id=$1`, orderID)
	}
	if err == nil {
		err = claimPromotion(ctx, sp, branchID, promoID, orderID, phone, amount)
	}
	if err != nil {
		_ = sp.Rollback(ctx)
		return err
	}
	return sp.Commit(ctx)
}

func isCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// ---- the order desk ----

func (a *App) addonOrder(ctx context.Context, q Querier, orderID int64) (*Branch, *lockedOrder, error) {
	var o lockedOrder
	err := q.QueryRow(ctx, `SELECT id, branch_id, service_type, acceptance_status, packing_status,
		fulfillment_status, payment_status, version, manifest_version, total FROM orders WHERE id=$1`, orderID).
		Scan(&o.ID, &o.BranchID, &o.ServiceType, &o.Acceptance, &o.Packing, &o.Fulfillment, &o.Payment,
			&o.Version, &o.ManifestVersion, &o.Total)
	if isNoRows(err) {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	b, err := readBranch(ctx, q, o.BranchID)
	return b, &o, err
}

// StaffAddonQuote prices dishes the store would add to an order.
func (a *App) StaffAddonQuote(ctx context.Context, orderID int64, in StaffAddonInput) (*AddonQuote, error) {
	b, o, err := a.addonOrder(ctx, a.DB, orderID)
	if err != nil {
		return nil, err
	}
	return quoteAddon(ctx, a.DB, b, o, in.addon(), false)
}

// StaffAddon adds dishes to an order for the customer (the customer asked on
// the phone or in a chat). It takes effect at once.
func (a *App) StaffAddon(ctx context.Context, orderID int64, idemKey string, in StaffAddonInput, actor Actor) (*Order, error) {
	ai := in.addon()
	return a.placeAddon(ctx, orderID, "staff", idemKey, ai, actor)
}

func quoteChanged(total int64) error {
	return ErrD(http.StatusConflict, "QUOTE_CHANGED", "the amount changed, please check it again",
		map[string]any{"total": total})
}

// placeAddon adds one batch to the order. Staff batches and the customer's on
// an order the store has not accepted yet take effect at once; the customer's
// on an accepted order wait for the store as status pending.
func (a *App) placeAddon(ctx context.Context, orderID int64, source, idemKey string, in AddonInput, actor Actor) (*Order, error) {
	if idemKey == "" || len(idemKey) > 100 {
		return nil, ErrValidation("Idempotency-Key header is required")
	}
	if source != "staff" {
		in.RoundTags = nil
	}
	tags, err := cleanTags(in.RoundTags)
	if err != nil {
		return nil, err
	}
	var placedBy *int64
	if source == "staff" && actor.ID != 0 {
		placedBy = &actor.ID
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		o, err := lockOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		var rec struct {
			Seq int `json:"seq"`
		}
		replayed, err := idemBegin(ctx, tx, fmt.Sprintf("order-addon:%d", o.ID), idemKey, hashJSON(in), &rec)
		if err != nil || replayed {
			return err
		}
		b, err := readBranch(ctx, tx, o.BranchID)
		if err != nil {
			return err
		}
		if !b.takesOrders() {
			return errStoreClosed
		}
		customer := source == "customer"
		q, err := quoteAddon(ctx, tx, b, o, in, customer)
		if err != nil {
			return err
		}
		wait := false
		if customer {
			if q.State.Pending {
				return Err(http.StatusConflict, "ADDON_PENDING", "the store has not answered your last add-on yet")
			}
			if q.State.CustomerLeft <= 0 {
				return Err(http.StatusConflict, "ADDON_LIMIT", "this order cannot take more add-ons")
			}
			wait = o.Acceptance == "accepted"
		}
		if in.ExpectedTotal != nil && *in.ExpectedTotal != q.After.Total {
			return quoteChanged(q.After.Total)
		}
		status := "accepted"
		var pending []byte
		var expires *time.Time
		if wait {
			status = "pending"
			pending, _ = json.Marshal(q.Lines)
			t := time.Now().Add(time.Duration(max(1, b.AddonConfirmMinutes)) * time.Minute)
			expires = &t
		}
		var batchID int64
		if err := tx.QueryRow(ctx, `INSERT INTO order_batches (order_id, seq, source, round_tags, placed_by, status,
			pending_lines, total_before, total_after, expires_at)
			VALUES ($1, (SELECT COALESCE(max(seq), 0) + 1 FROM order_batches WHERE order_id=$1), $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id, seq`, o.ID, source, tags, placedBy, status, pending, o.Total, q.After.Total, expires).
			Scan(&batchID, &rec.Seq); err != nil {
			return err
		}
		if wait {
			if err := bump(ctx, tx, o.ID); err != nil {
				return err
			}
			if err := audit(ctx, tx, b.ID, o.ID, actor, "order.addon_requested", map[string]any{"batch_id": batchID,
				"lines": len(q.Lines), "batch_total": q.Subtotal, "total_after": q.After.Total}); err != nil {
				return err
			}
			if err := notify(ctx, tx, Event{BranchID: b.ID, OrderID: o.ID}); err != nil {
				return err
			}
		} else {
			if _, err := a.applyAddon(ctx, tx, b, o, batchID, q.Lines, actor); err != nil {
				return err
			}
			if !customer {
				if err := a.notifyCustomer(ctx, tx, o.ID, "appended"); err != nil {
					return err
				}
			}
		}
		return idemSave(ctx, tx, fmt.Sprintf("order-addon:%d", o.ID), idemKey, rec)
	})
	if err != nil {
		return nil, err
	}
	return loadOrder(ctx, a.DB, orderID)
}

// ---- the customer's own add-ons ----

func (a *App) orderIDByToken(ctx context.Context, q Querier, token string) (int64, error) {
	var id int64
	err := q.QueryRow(ctx, `SELECT id FROM orders WHERE access_token_hash=$1`, hashToken(token)).Scan(&id)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return id, err
}

// CustomerAddonQuote prices dishes the customer would add to their order.
func (a *App) CustomerAddonQuote(ctx context.Context, token string, in AddonInput) (*AddonQuote, error) {
	id, err := a.orderIDByToken(ctx, a.DB, token)
	if err != nil {
		return nil, err
	}
	b, o, err := a.addonOrder(ctx, a.DB, id)
	if err != nil {
		return nil, err
	}
	for i := range in.Items {
		in.Items[i].repeat = false
	}
	return quoteAddon(ctx, a.DB, a.forCustomer(b), o, in, true)
}

// CustomerAddon sends the customer's add-on. On an accepted order it waits
// for the store (AddonConfirmMinutes); before that it joins the order at once.
func (a *App) CustomerAddon(ctx context.Context, token, idemKey string, in AddonInput) (*Order, error) {
	id, err := a.orderIDByToken(ctx, a.DB, token)
	if err != nil {
		return nil, err
	}
	for i := range in.Items {
		in.Items[i].repeat = false
	}
	var customerID int64
	_ = a.DB.QueryRow(ctx, `SELECT COALESCE(customer_id, 0) FROM orders WHERE id=$1`, id).Scan(&customerID)
	return a.placeAddon(ctx, id, "customer", idemKey, in, Actor{Type: "customer", ID: customerID})
}

// CancelCustomerAddon withdraws the customer's add-on still waiting for the store.
func (a *App) CancelCustomerAddon(ctx context.Context, token string, seq int) (*Order, error) {
	id, err := a.orderIDByToken(ctx, a.DB, token)
	if err != nil {
		return nil, err
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		o, err := lockOrder(ctx, tx, id)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE order_batches SET status='cancelled', decided_at=now()
			WHERE order_id=$1 AND seq=$2 AND source='customer' AND status='pending'`, o.ID, seq)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrInvalidState("this add-on is no longer waiting")
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, Actor{Type: "customer"}, "order.addon_withdrawn",
			map[string]any{"seq": seq}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
	if err != nil {
		return nil, err
	}
	return loadOrder(ctx, a.DB, id)
}

// ---- the store answers a customer's add-on ----

type pendingBatch struct {
	ID    int64
	Lines []QuoteLine
}

func lockPendingBatch(ctx context.Context, tx pgx.Tx, orderID int64, seq int) (*pendingBatch, error) {
	var pb pendingBatch
	var raw []byte
	var status string
	err := tx.QueryRow(ctx, `SELECT id, status, pending_lines FROM order_batches WHERE order_id=$1 AND seq=$2 FOR UPDATE`,
		orderID, seq).Scan(&pb.ID, &status, &raw)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if status != "pending" {
		return nil, Err(http.StatusConflict, "ADDON_DECIDED", "this add-on was already answered or has expired")
	}
	if err := json.Unmarshal(raw, &pb.Lines); err != nil {
		return nil, err
	}
	return &pb, nil
}

// AcceptAddon puts the customer's waiting add-on into the order.
func (a *App) AcceptAddon(ctx context.Context, orderID int64, seq int, actor Actor) (*Order, error) {
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		pb, err := lockPendingBatch(ctx, tx, o.ID, seq)
		if err != nil {
			return err
		}
		if open, _ := addonOpen(o); !open {
			return errAddonClosed
		}
		b, err := readBranch(ctx, tx, o.BranchID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE order_batches SET status='accepted', pending_lines=NULL, decided_by=$2,
			decided_at=now(), created_at=now() WHERE id=$1`, pb.ID, nullID(actor.ID)); err != nil {
			return err
		}
		if _, err := a.applyAddon(ctx, tx, b, o, pb.ID, pb.Lines, actor); err != nil {
			return err
		}
		return a.notifyCustomer(ctx, tx, o.ID, "addon_accepted")
	})
}

// RejectAddon turns the customer's waiting add-on down; the order stays as it was.
func (a *App) RejectAddon(ctx context.Context, orderID int64, seq int, reason string, actor Actor) (*Order, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 200 {
		return nil, ErrValidation("the reason is up to 200 characters")
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		pb, err := lockPendingBatch(ctx, tx, o.ID, seq)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE order_batches SET status='rejected', reject_reason=$3, decided_by=$2,
			decided_at=now() WHERE id=$1`, pb.ID, nullID(actor.ID), reason); err != nil {
			return err
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.addon_rejected",
			map[string]any{"seq": seq, "reason": reason}); err != nil {
			return err
		}
		if err := a.notifyCustomer(ctx, tx, o.ID, "addon_rejected"); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
}

func nullID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// endPendingAddons closes the order's waiting add-ons when the order leaves
// the store (expired) or is closed (cancelled). The order is locked.
func (a *App) endPendingAddons(ctx context.Context, tx pgx.Tx, orderID int64, status string) error {
	tag, err := tx.Exec(ctx, `UPDATE order_batches SET status=$2, decided_at=now()
		WHERE order_id=$1 AND status='pending'`, orderID, status)
	if err != nil || tag.RowsAffected() == 0 || status != "expired" {
		return err
	}
	return a.notifyCustomer(ctx, tx, orderID, "addon_expired")
}

// ExpireAddons drops customer add-ons the store did not answer in time.
func (a *App) ExpireAddons(ctx context.Context) (int, error) {
	n := 0
	for {
		done := false
		err := a.inTx(ctx, func(tx pgx.Tx) error {
			var orderID int64
			err := tx.QueryRow(ctx, `SELECT order_id FROM order_batches WHERE status='pending' AND expires_at < now()
				ORDER BY expires_at LIMIT 1`).Scan(&orderID)
			if isNoRows(err) {
				done = true
				return nil
			} else if err != nil {
				return err
			}
			o, err := lockOrder(ctx, tx, orderID)
			if err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `UPDATE order_batches SET status='expired', decided_at=now()
				WHERE order_id=$1 AND status='pending' AND expires_at < now()`, o.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return nil
			}
			n += int(tag.RowsAffected())
			if err := bump(ctx, tx, o.ID); err != nil {
				return err
			}
			if err := audit(ctx, tx, o.BranchID, o.ID, Actor{Type: "system"}, "order.addon_expired", nil); err != nil {
				return err
			}
			if err := a.notifyCustomer(ctx, tx, o.ID, "addon_expired"); err != nil {
				return err
			}
			return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
		})
		if err != nil {
			return n, err
		}
		if done {
			return n, nil
		}
	}
}

// loadBatches fills the orders' batches and, for delivery and pickup, whether
// dishes can still be added.
func loadBatches(ctx context.Context, q Querier, byID map[int64]*Order, ids []int64) error {
	rows, err := q.Query(ctx, `SELECT b.order_id, b.seq, b.source, b.status, b.created_at,
		COALESCE((SELECT u.display_name FROM users u WHERE u.id = b.placed_by), ''), b.round_tags,
		b.total_before, b.total_after, b.reject_reason, b.expires_at, b.pending_lines
		FROM order_batches b WHERE b.order_id = ANY($1) ORDER BY b.order_id, b.seq`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ob OrderBatch
		var orderID int64
		var raw []byte
		if err := rows.Scan(&orderID, &ob.Seq, &ob.Source, &ob.Status, &ob.At, &ob.By, &ob.RoundTags, &ob.Before,
			&ob.After, &ob.Reason, &ob.ExpiresAt, &raw); err != nil {
			return err
		}
		if raw != nil {
			_ = json.Unmarshal(raw, &ob.Lines)
		}
		if ob.RoundTags == nil {
			ob.RoundTags = []string{}
		}
		o := byID[orderID]
		o.Batches = append(o.Batches, ob)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, o := range byID {
		if o.Batches == nil {
			o.Batches = []OrderBatch{}
		}
		if o.ServiceType == "dine_in" {
			continue
		}
		lo := &lockedOrder{ServiceType: o.ServiceType, Acceptance: o.AcceptanceStatus, Packing: o.PackingStatus,
			Fulfillment: o.FulfillmentStatus}
		open, why := addonOpen(lo)
		st := &AddonState{Open: open, Why: why, Repack: o.PackingStatus == "packed"}
		used := 0
		for _, b := range o.Batches {
			if b.Source == "customer" && (b.Status == "pending" || b.Status == "accepted") {
				used++
			}
			if b.Status == "pending" {
				st.Pending = true
			}
		}
		st.CustomerLeft = max(0, AddonCustomerLimit-used)
		o.Addon = st
	}
	return nil
}
