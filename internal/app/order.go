package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type OrderItem struct {
	ID         int64        `json:"id"`
	ItemID     int64        `json:"item_id"`
	Name       string       `json:"name"`
	NameEn     string       `json:"name_en"`
	UnitPrice  int64        `json:"unit_price"`
	Qty        int          `json:"qty"`
	LineTotal  int64        `json:"line_total"`
	Options    []LineOption `json:"options"`
	Note       string       `json:"note"`
	NeedsPrep  bool         `json:"needs_prep"`
	PrepStatus string       `json:"prep_status"`
}

type DeliveryTask struct {
	ID                int64      `json:"id"`
	RiderID           int64      `json:"rider_id"`
	RiderName         string     `json:"rider_name"`
	RiderPhone        string     `json:"rider_phone"`
	AssignmentVersion int        `json:"assignment_version"`
	Status            string     `json:"status"`
	FailReason        string     `json:"fail_reason"`
	AssignedAt        time.Time  `json:"assigned_at"`
	PickedUpAt        *time.Time `json:"picked_up_at"`
	ArrivedAt         *time.Time `json:"arrived_at"`
	DeliveredAt       *time.Time `json:"delivered_at"`
}

type CashCollection struct {
	ID              int64      `json:"id"`
	OrderID         int64      `json:"order_id"`
	PublicNo        string     `json:"public_no,omitempty"`
	RiderID         int64      `json:"rider_id"`
	RiderName       string     `json:"rider_name"`
	AmountDue       int64      `json:"amount_due"`
	AmountCollected int64      `json:"amount_collected"`
	ReconcileStatus string     `json:"reconcile_status"`
	AmountReturned  *int64     `json:"amount_returned"`
	ReconciledAt    *time.Time `json:"reconciled_at"`
	Note            string     `json:"note"`
	CreatedAt       time.Time  `json:"created_at"`
}

// Order is the full staff-facing view. Customer and rider views are trimmed
// from it in views.go.
type Order struct {
	ID                int64           `json:"id"`
	MerchantID        int64           `json:"-"`
	BranchID          int64           `json:"branch_id"`
	PublicNo          string          `json:"public_no"`
	PickupCode        string          `json:"pickup_code"`
	Channel           string          `json:"channel"`
	ServiceType       string          `json:"service_type"`
	CustomerName      string          `json:"customer_name"`
	CustomerPhone     string          `json:"customer_phone"`
	ZoneName          string          `json:"zone_name"`
	AddressLine       string          `json:"address_line"`
	AddressUnit       string          `json:"address_unit"`
	AddressNote       string          `json:"address_note"`
	Note              string          `json:"note"`
	Currency          string          `json:"currency"`
	Totals            Totals          `json:"totals"`
	PaymentMethod     string          `json:"payment_method"`
	AcceptanceStatus  string          `json:"acceptance_status"`
	PackingStatus     string          `json:"packing_status"`
	FulfillmentStatus string          `json:"fulfillment_status"`
	PaymentStatus     string          `json:"payment_status"`
	DisplayStatus     string          `json:"display_status"`
	CloseReason       string          `json:"close_reason"`
	Bags              *int            `json:"bags"`
	Version           int             `json:"version"`
	ManifestVersion   int             `json:"manifest_version"`
	AcceptDeadline    time.Time       `json:"accept_deadline"`
	CreatedAt         time.Time       `json:"created_at"`
	AcceptedAt        *time.Time      `json:"accepted_at"`
	PackedAt          *time.Time      `json:"packed_at"`
	HandedOffAt       *time.Time      `json:"handed_off_at"`
	CompletedAt       *time.Time      `json:"completed_at"`
	ClosedAt          *time.Time      `json:"closed_at"`
	Items             []OrderItem     `json:"items"`
	Delivery          *DeliveryTask   `json:"delivery"`
	Cash              *CashCollection `json:"cash"`
	tokenNonce        []byte
}

// DisplayStatus derives the single customer-facing status from the four
// independent dimensions (docs/DESIGN.md §5.3). First match wins.
func DisplayStatus(acceptance, fulfillment string) string {
	switch acceptance {
	case "rejected":
		return "rejected"
	case "cancelled":
		return "cancelled"
	case "pending":
		return "pending_acceptance"
	}
	switch fulfillment {
	case "delivered":
		return "delivered"
	case "picked_up":
		return "picked_up"
	case "delivery_failed":
		return "delivery_failed"
	case "arrived":
		return "rider_arrived"
	case "out_for_delivery":
		return "out_for_delivery"
	case "awaiting_pickup":
		return "ready_for_pickup"
	case "awaiting_handoff":
		return "awaiting_rider"
	}
	return "preparing"
}

const orderCols = `o.id, o.merchant_id, o.branch_id, o.public_no, o.pickup_code, o.channel, o.service_type,
	o.customer_name, o.customer_phone, o.zone_name, o.address_line, o.address_unit, o.address_note, o.note,
	o.currency, o.subtotal, o.packaging_fee, o.delivery_fee, o.service_charge, o.tax_amount, o.tax_inclusive,
	o.tax_rate_bp, o.discount, o.total, o.payment_method, o.acceptance_status, o.packing_status,
	o.fulfillment_status, o.payment_status, o.close_reason, o.bags, o.version, o.manifest_version,
	o.accept_deadline, o.created_at, o.accepted_at, o.packed_at, o.handed_off_at, o.completed_at, o.closed_at,
	o.token_nonce`

func scanOrder(row interface{ Scan(...any) error }) (*Order, error) {
	o := &Order{Items: []OrderItem{}}
	t := &o.Totals
	err := row.Scan(&o.ID, &o.MerchantID, &o.BranchID, &o.PublicNo, &o.PickupCode, &o.Channel, &o.ServiceType,
		&o.CustomerName, &o.CustomerPhone, &o.ZoneName, &o.AddressLine, &o.AddressUnit, &o.AddressNote, &o.Note,
		&o.Currency, &t.Subtotal, &t.PackagingFee, &t.DeliveryFee, &t.ServiceCharge, &t.TaxAmount, &t.TaxInclusive,
		&t.TaxRateBP, &t.Discount, &t.Total, &o.PaymentMethod, &o.AcceptanceStatus, &o.PackingStatus,
		&o.FulfillmentStatus, &o.PaymentStatus, &o.CloseReason, &o.Bags, &o.Version, &o.ManifestVersion,
		&o.AcceptDeadline, &o.CreatedAt, &o.AcceptedAt, &o.PackedAt, &o.HandedOffAt, &o.CompletedAt, &o.ClosedAt,
		&o.tokenNonce)
	if err != nil {
		return nil, err
	}
	o.DisplayStatus = DisplayStatus(o.AcceptanceStatus, o.FulfillmentStatus)
	return o, nil
}

// queryOrders loads orders matching a WHERE clause (on alias o) together with
// their items, delivery task and cash collection in three batched queries.
func queryOrders(ctx context.Context, q Querier, where string, args ...any) ([]*Order, error) {
	rows, err := q.Query(ctx, `SELECT `+orderCols+` FROM orders o WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	orders := []*Order{}
	byID := map[int64]*Order{}
	ids := []int64{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		orders = append(orders, o)
		byID[o.ID] = o
		ids = append(ids, o.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return orders, nil
	}

	rows, err = q.Query(ctx, `SELECT id, order_id, item_id, name, name_en, unit_price, qty, line_total, options,
		note, needs_prep, prep_status FROM order_items WHERE order_id = ANY($1) ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var it OrderItem
		var orderID int64
		var opts []byte
		if err := rows.Scan(&it.ID, &orderID, &it.ItemID, &it.Name, &it.NameEn, &it.UnitPrice, &it.Qty,
			&it.LineTotal, &opts, &it.Note, &it.NeedsPrep, &it.PrepStatus); err != nil {
			rows.Close()
			return nil, err
		}
		it.Options = []LineOption{}
		_ = json.Unmarshal(opts, &it.Options)
		byID[orderID].Items = append(byID[orderID].Items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(ctx, `SELECT t.id, t.order_id, t.rider_id, u.display_name, u.phone, t.assignment_version,
		t.status, t.fail_reason, t.assigned_at, t.picked_up_at, t.arrived_at, t.delivered_at
		FROM delivery_tasks t JOIN users u ON u.id = t.rider_id WHERE t.order_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t DeliveryTask
		var orderID int64
		if err := rows.Scan(&t.ID, &orderID, &t.RiderID, &t.RiderName, &t.RiderPhone, &t.AssignmentVersion,
			&t.Status, &t.FailReason, &t.AssignedAt, &t.PickedUpAt, &t.ArrivedAt, &t.DeliveredAt); err != nil {
			rows.Close()
			return nil, err
		}
		byID[orderID].Delivery = &t
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	cash, err := queryCash(ctx, q, `c.order_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for _, c := range cash {
		byID[c.OrderID].Cash = c
	}
	return orders, nil
}

func loadOrder(ctx context.Context, q Querier, id int64) (*Order, error) {
	orders, err := queryOrders(ctx, q, `o.id=$1`, id)
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, ErrNotFound
	}
	return orders[0], nil
}

// lockedOrder is the minimal row state read under FOR UPDATE by commands.
type lockedOrder struct {
	ID, BranchID                              int64
	ServiceType                               string
	Acceptance, Packing, Fulfillment, Payment string
	Version, ManifestVersion                  int
	Total                                     int64
}

// lockOrder takes the order row lock. Every command that touches an order,
// its items or its delivery task takes this lock first (fixed lock order).
func lockOrder(ctx context.Context, tx pgx.Tx, id int64) (*lockedOrder, error) {
	var o lockedOrder
	err := tx.QueryRow(ctx, `SELECT id, branch_id, service_type, acceptance_status, packing_status,
		fulfillment_status, payment_status, version, manifest_version, total FROM orders WHERE id=$1 FOR UPDATE`, id).
		Scan(&o.ID, &o.BranchID, &o.ServiceType, &o.Acceptance, &o.Packing, &o.Fulfillment, &o.Payment,
			&o.Version, &o.ManifestVersion, &o.Total)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return &o, err
}

func bump(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `UPDATE orders SET version = version + 1, updated_at = now() WHERE id=$1`, id)
	return err
}

// ---- placing an order ----

type PlaceOrderInput struct {
	QuoteID      string `json:"quote_id"`
	CustomerName string `json:"customer_name"`
	AddressLine  string `json:"address_line"`
	AddressUnit  string `json:"address_unit"`
	AddressNote  string `json:"address_note"`
	Note         string `json:"note"`
}

type PlacedOrder struct {
	OrderID     int64  `json:"order_id"`
	PublicNo    string `json:"public_no"`
	AccessToken string `json:"access_token"`
}

type placedRecord struct {
	OrderID int64 `json:"order_id"`
}

// PlaceOrder turns a confirmed quote into an order. phone comes from the
// verified customer session. The idempotency key is mandatory: a retry with
// the same key returns the original order.
func (a *App) PlaceOrder(ctx context.Context, slug, phone, idemKey string, in PlaceOrderInput) (*PlacedOrder, error) {
	in.CustomerName = strings.TrimSpace(in.CustomerName)
	in.AddressLine = strings.TrimSpace(in.AddressLine)
	if idemKey == "" || len(idemKey) > 100 {
		return nil, ErrValidation("Idempotency-Key header is required")
	}
	if in.CustomerName == "" || len(in.CustomerName) > 80 {
		return nil, ErrValidation("customer_name is required")
	}
	if len(in.AddressLine) > 300 || len(in.AddressUnit) > 100 || len(in.AddressNote) > 300 || len(in.Note) > 300 {
		return nil, ErrValidation("text too long")
	}

	var out PlacedOrder
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		b, err := branchBySlug(ctx, tx, slug)
		if err != nil {
			return err
		}
		scope := fmt.Sprintf("place-order:%d:%s", b.ID, phone)
		var rec placedRecord
		replayed, err := idemBegin(ctx, tx, scope, idemKey, hashJSON(in), &rec)
		if err != nil {
			return err
		}
		if replayed {
			var nonce []byte
			if err := tx.QueryRow(ctx, `SELECT public_no, token_nonce FROM orders WHERE id=$1`, rec.OrderID).
				Scan(&out.PublicNo, &nonce); err != nil {
				return err
			}
			out.OrderID, out.AccessToken = rec.OrderID, a.orderToken(nonce)
			return nil
		}

		var payload []byte
		var expires time.Time
		var used *int64
		err = tx.QueryRow(ctx, `SELECT payload, expires_at, used_order_id FROM quotes
			WHERE id::text=$1 AND branch_id=$2 FOR UPDATE`, in.QuoteID, b.ID).Scan(&payload, &expires, &used)
		if isNoRows(err) {
			return Err(http.StatusNotFound, "QUOTE_NOT_FOUND", "quote not found")
		} else if err != nil {
			return err
		}
		if used != nil {
			return Err(http.StatusConflict, "QUOTE_USED", "this quote was already used for an order")
		}
		if time.Now().After(expires) {
			return Err(http.StatusConflict, "QUOTE_EXPIRED", "the quote expired, please review the amount again")
		}
		var quoted Quote
		if err := json.Unmarshal(payload, &quoted); err != nil {
			return err
		}
		if quoted.ServiceType == "delivery" && in.AddressLine == "" {
			return ErrValidation("address_line is required for delivery")
		}

		// Re-price against the live menu; anything that moved needs a new confirmation.
		cur, err := computeQuote(ctx, tx, b, quoted.Input)
		if err != nil {
			return err
		}
		if !sameQuote(&quoted, cur) {
			return ErrD(http.StatusConflict, "QUOTE_CHANGED", "prices or fees changed, please confirm the new amount",
				map[string]any{"totals": cur.Totals})
		}
		if err := reserveStock(ctx, tx, cur.Lines); err != nil {
			return err
		}

		// Per-branch daily sequence in branch local time.
		today := time.Now().In(b.location())
		var seq int
		err = tx.QueryRow(ctx, `UPDATE branches SET
			order_seq = CASE WHEN order_seq_date = $2::date THEN order_seq + 1 ELSE 1 END,
			order_seq_date = $2::date WHERE id=$1 RETURNING order_seq`, b.ID, today.Format("2006-01-02")).Scan(&seq)
		if err != nil {
			return err
		}
		pickupCode := fmt.Sprintf("%03d", seq)
		publicNo := fmt.Sprintf("W%s-%s", today.Format("0102"), pickupCode)
		nonce := randomBytes(16)
		token := a.orderToken(nonce)
		payMethod := "cod"
		if cur.ServiceType == "pickup" {
			payMethod = "pay_at_pickup"
		}
		t := cur.Totals
		var orderID int64
		err = tx.QueryRow(ctx, `INSERT INTO orders (merchant_id, branch_id, public_no, pickup_code,
			access_token_hash, token_nonce, service_type, customer_name, customer_phone, zone_id, zone_name,
			address_line, address_unit, address_note, note, currency, subtotal, packaging_fee, delivery_fee,
			service_charge, tax_amount, tax_inclusive, tax_rate_bp, discount, total, payment_method, accept_deadline)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,
				now() + make_interval(mins => $27))
			RETURNING id`,
			b.MerchantID, b.ID, publicNo, pickupCode, hashToken(token), nonce, cur.ServiceType, in.CustomerName,
			phone, cur.ZoneID, cur.ZoneName, in.AddressLine, in.AddressUnit, in.AddressNote, in.Note, b.Currency,
			t.Subtotal, t.PackagingFee, t.DeliveryFee, t.ServiceCharge, t.TaxAmount, t.TaxInclusive, t.TaxRateBP,
			t.Discount, t.Total, payMethod, b.AcceptTimeoutMinutes).Scan(&orderID)
		if err != nil {
			return wrap("insert order", err)
		}
		var batchID int64
		if err := tx.QueryRow(ctx, `INSERT INTO order_batches (order_id, seq) VALUES ($1, 1) RETURNING id`,
			orderID).Scan(&batchID); err != nil {
			return err
		}
		for _, l := range cur.Lines {
			opts, _ := json.Marshal(l.Options)
			if _, err := tx.Exec(ctx, `INSERT INTO order_items (order_id, branch_id, batch_id, item_id, name,
				name_en, unit_price, qty, line_total, options, note, needs_prep)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, orderID, b.ID, batchID, l.ItemID, l.Name,
				l.NameEn, l.UnitPrice, l.Qty, l.LineTotal, opts, l.Note, l.NeedsPrep); err != nil {
				return wrap("insert order item", err)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE quotes SET used_order_id=$2 WHERE id::text=$1`, in.QuoteID, orderID); err != nil {
			return err
		}
		if err := audit(ctx, tx, b.ID, orderID, Actor{Type: "customer"}, "order.placed",
			map[string]any{"total": t.Total, "service_type": cur.ServiceType}); err != nil {
			return err
		}
		if err := idemSave(ctx, tx, scope, idemKey, placedRecord{OrderID: orderID}); err != nil {
			return err
		}
		out = PlacedOrder{OrderID: orderID, PublicNo: publicNo, AccessToken: token}
		return notify(ctx, tx, Event{BranchID: b.ID, OrderID: orderID})
	})
	if err != nil {
		return nil, wrap("place order", err)
	}
	return &out, nil
}

// OrderByToken resolves a customer's access token to the order.
func (a *App) OrderByToken(ctx context.Context, token string) (*Order, error) {
	orders, err := queryOrders(ctx, a.DB, `o.access_token_hash=$1`, hashToken(token))
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, ErrNotFound
	}
	return orders[0], nil
}

// closeOrder is shared by reject, cancel and timeout: it releases stock, marks
// items and the delivery task cancelled and records the reason.
func closeOrder(ctx context.Context, tx pgx.Tx, o *lockedOrder, status, reason string, actor Actor) error {
	if err := releaseStock(ctx, tx, o.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE order_items SET prep_status='cancelled' WHERE order_id=$1`, o.ID); err != nil {
		return err
	}
	riders := []int64{}
	var riderID int64
	err := tx.QueryRow(ctx, `UPDATE delivery_tasks SET status='cancelled' WHERE order_id=$1
		AND status IN ('assigned','picked_up','arrived') RETURNING rider_id`, o.ID).Scan(&riderID)
	if err == nil {
		riders = append(riders, riderID)
	} else if !isNoRows(err) {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET acceptance_status=$2, close_reason=$3, closed_at=now(),
		version=version+1, manifest_version=manifest_version+1, updated_at=now() WHERE id=$1`,
		o.ID, status, reason); err != nil {
		return err
	}
	if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order."+status,
		map[string]any{"reason": reason, "from": o.Acceptance}); err != nil {
		return err
	}
	return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID, RiderIDs: riders})
}

// CustomerCancel lets the customer cancel while the order is still pending.
func (a *App) CustomerCancel(ctx context.Context, token string) (*Order, error) {
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id FROM orders WHERE access_token_hash=$1`, hashToken(token)).Scan(&id); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		o, err := lockOrder(ctx, tx, id)
		if err != nil {
			return err
		}
		if o.Acceptance == "cancelled" {
			return nil // already cancelled: idempotent
		}
		if o.Acceptance != "pending" {
			return ErrInvalidState("the store already accepted this order, please contact the store")
		}
		return closeOrder(ctx, tx, o, "cancelled", "customer_cancelled", Actor{Type: "customer"})
	})
	if err != nil {
		return nil, wrap("customer cancel", err)
	}
	return loadOrder(ctx, a.DB, id)
}
