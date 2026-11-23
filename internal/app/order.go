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
	ID     int64  `json:"id"`
	ItemID int64  `json:"item_id"`
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	// NameTr is the name in the language the customer ordered in, when it differs.
	NameTr      string       `json:"name_tr,omitempty"`
	UnitPrice   int64        `json:"unit_price"`
	Qty         int          `json:"qty"`
	LineTotal   int64        `json:"line_total"`
	Options     []LineOption `json:"options"`
	Note        string       `json:"note"`
	NeedsPrep   bool         `json:"needs_prep"`
	PrepStatus  string       `json:"prep_status"`
	StationID   *int64       `json:"station_id"`
	StationName string       `json:"station_name"`
	// Dine-in: which batch the line came in, who sent it, and when it reached the table.
	BatchSeq    int        `json:"batch_seq"`
	BatchSource string     `json:"batch_source"`
	ServedAt    *time.Time `json:"served_at"`
	VoidReason  string     `json:"void_reason,omitempty"`
	// Basket is the customer's basket this line was put in (1 to 4).
	Basket int `json:"basket"`
	// ImageURL is the dish's current picture; only the customer's order page gets it.
	ImageURL string `json:"image_url,omitempty"`
}

type DeliveryTask struct {
	ID                int64     `json:"id"`
	RiderID           int64     `json:"rider_id"`
	RiderName         string    `json:"rider_name"`
	RiderPhone        string    `json:"rider_phone"`
	AssignmentVersion int       `json:"assignment_version"`
	Status            string    `json:"status"`
	FailReason        string    `json:"fail_reason"`
	AssignedAt        time.Time `json:"assigned_at"`
	// SeenAt is when the rider first had this assignment on screen.
	SeenAt *time.Time `json:"seen_at"`
	// ProofAt is when the rider took a photo at the door, nil when there is none.
	ProofAt     *time.Time `json:"proof_at"`
	PickedUpAt  *time.Time `json:"picked_up_at"`
	ArrivedAt   *time.Time `json:"arrived_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
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
	ID            int64  `json:"id"`
	MerchantID    int64  `json:"-"`
	BranchID      int64  `json:"branch_id"`
	PublicNo      string `json:"public_no"`
	PickupCode    string `json:"pickup_code"`
	Channel       string `json:"channel"`
	ServiceType   string `json:"service_type"`
	CustomerName  string `json:"customer_name"`
	CustomerPhone string `json:"customer_phone"`
	ZoneName      string `json:"zone_name"`
	// DestLat/DestLng is the customer's pin (map-mode stores only);
	// ZoneDistanceM is how far outside the zone it was, nil when inside.
	DestLat       *float64 `json:"dest_lat"`
	DestLng       *float64 `json:"dest_lng"`
	ZoneDistanceM *int     `json:"zone_distance_m"`
	// Who the rider calls and how else to reach them; from the saved address.
	// ContactPhone may differ from CustomerPhone, the account that ordered.
	ContactPhone    string `json:"contact_phone"`
	ContactTitle    string `json:"contact_title"`
	ContactWechat   string `json:"contact_wechat"`
	ContactTelegram string `json:"contact_telegram"`
	ContactLine     string `json:"contact_line"`
	ContactWhatsapp string `json:"contact_whatsapp"`
	// AddressPhoto says the customer added a photo of the door.
	AddressPhoto      bool      `json:"address_photo"`
	AddressLine       string    `json:"address_line"`
	AddressUnit       string    `json:"address_unit"`
	AddressNote       string    `json:"address_note"`
	Note              string    `json:"note"`
	Currency          string    `json:"currency"`
	Totals            Totals    `json:"totals"`
	PaymentMethod     string    `json:"payment_method"`
	AcceptanceStatus  string    `json:"acceptance_status"`
	PackingStatus     string    `json:"packing_status"`
	FulfillmentStatus string    `json:"fulfillment_status"`
	PaymentStatus     string    `json:"payment_status"`
	DisplayStatus     string    `json:"display_status"`
	CloseReason       string    `json:"close_reason"`
	Bags              *int      `json:"bags"`
	Version           int       `json:"version"`
	ManifestVersion   int       `json:"manifest_version"`
	AcceptDeadline    time.Time `json:"accept_deadline"`
	// ScheduledFor is the time the customer asked for (nil = as soon as
	// possible); ReleaseAt is when the kitchen should start on it.
	ScheduledFor *time.Time `json:"scheduled_for"`
	ReleaseAt    *time.Time `json:"release_at"`
	// Timezone is the store's, so every screen shows times on the store's clock.
	Timezone string `json:"timezone"`
	// Dine-in only.
	SessionID      *int64          `json:"session_id"`
	TableName      string          `json:"table_name"`
	AmountReceived *int64          `json:"amount_received"`
	PaymentNote    string          `json:"payment_note"`
	CreatedAt      time.Time       `json:"created_at"`
	AcceptedAt     *time.Time      `json:"accepted_at"`
	PackedAt       *time.Time      `json:"packed_at"`
	HandedOffAt    *time.Time      `json:"handed_off_at"`
	CompletedAt    *time.Time      `json:"completed_at"`
	ClosedAt       *time.Time      `json:"closed_at"`
	Items          []OrderItem     `json:"items"`
	Delivery       *DeliveryTask   `json:"delivery"`
	Cash           *CashCollection `json:"cash"`
	// Aftersale is the customer's after-sales request, nil when there is none.
	Aftersale *Aftersale `json:"aftersale"`
	// Discounts lists the promotion and each coupon taken off.
	Discounts []OrderDiscount `json:"discounts"`
	// Gift is the coupon this order earned; only the customer's order page gets it.
	Gift *CustomerCoupon `json:"gift,omitempty"`
	// Store is who sold it; only the customer's order page gets it.
	Store *OrderStore `json:"store,omitempty"`
	// An order the store placed for a customer (channel "staff"): the
	// store's member number, where the customer asked (wechat, line,
	// whatsapp, phone, other), who placed it and the note only staff see.
	MemberNo    *int   `json:"member_no"`
	OrderSource string `json:"order_source"`
	PlacedBy    string `json:"placed_by"`
	StaffNote   string `json:"staff_note"`
	tokenNonce  []byte
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
	case "dined":
		return "completed"
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
	o.token_nonce, COALESCE(o.promotion_id, 0), o.promotion_name, o.promotion_name_en, o.scheduled_for, o.release_at,
	(SELECT b.timezone FROM branches b WHERE b.id = o.branch_id), o.session_id, o.table_name, o.amount_received,
	o.payment_note, o.dest_lat, o.dest_lng, o.zone_distance_m, o.contact_phone, o.contact_title, o.contact_wechat,
	o.contact_telegram, o.contact_line, o.contact_whatsapp, EXISTS (SELECT 1 FROM customer_address_photos p WHERE p.address_id = o.address_id),
	o.merchant_discount, o.platform_subsidy, (SELECT m.member_no FROM store_members m WHERE m.id = o.member_id),
	o.order_source, COALESCE((SELECT u.display_name FROM users u WHERE u.id = o.placed_by), ''), o.staff_note`

func scanOrder(row interface{ Scan(...any) error }) (*Order, error) {
	o := &Order{Items: []OrderItem{}, Discounts: []OrderDiscount{}}
	t := &o.Totals
	err := row.Scan(&o.ID, &o.MerchantID, &o.BranchID, &o.PublicNo, &o.PickupCode, &o.Channel, &o.ServiceType,
		&o.CustomerName, &o.CustomerPhone, &o.ZoneName, &o.AddressLine, &o.AddressUnit, &o.AddressNote, &o.Note,
		&o.Currency, &t.Subtotal, &t.PackagingFee, &t.DeliveryFee, &t.ServiceCharge, &t.TaxAmount, &t.TaxInclusive,
		&t.TaxRateBP, &t.Discount, &t.Total, &o.PaymentMethod, &o.AcceptanceStatus, &o.PackingStatus,
		&o.FulfillmentStatus, &o.PaymentStatus, &o.CloseReason, &o.Bags, &o.Version, &o.ManifestVersion,
		&o.AcceptDeadline, &o.CreatedAt, &o.AcceptedAt, &o.PackedAt, &o.HandedOffAt, &o.CompletedAt, &o.ClosedAt,
		&o.tokenNonce, &t.PromotionID, &t.PromotionName, &t.PromotionNameEn, &o.ScheduledFor, &o.ReleaseAt,
		&o.Timezone, &o.SessionID, &o.TableName, &o.AmountReceived, &o.PaymentNote, &o.DestLat, &o.DestLng,
		&o.ZoneDistanceM, &o.ContactPhone, &o.ContactTitle, &o.ContactWechat, &o.ContactTelegram, &o.ContactLine,
		&o.ContactWhatsapp, &o.AddressPhoto, &t.MerchantDiscount, &t.PlatformSubsidy, &o.MemberNo, &o.OrderSource,
		&o.PlacedBy, &o.StaffNote)
	if err != nil {
		return nil, err
	}
	o.DisplayStatus = DisplayStatus(o.AcceptanceStatus, o.FulfillmentStatus)
	// An accepted scheduled order that the kitchen has not started on yet.
	if o.DisplayStatus == "preparing" && o.ReleaseAt != nil && o.ReleaseAt.After(time.Now()) {
		o.DisplayStatus = "scheduled"
	}
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

	rows, err = q.Query(ctx, `SELECT i.id, i.order_id, i.item_id, i.name, i.name_en, i.unit_price, i.qty, i.line_total,
		i.options, i.note, i.needs_prep, i.prep_status, i.station_id, i.station_name, b.seq, b.source, i.served_at,
		i.void_reason, i.basket_no, i.name_customer FROM order_items i JOIN order_batches b ON b.id = i.batch_id
		WHERE i.order_id = ANY($1) ORDER BY i.id`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var it OrderItem
		var orderID int64
		var opts []byte
		if err := rows.Scan(&it.ID, &orderID, &it.ItemID, &it.Name, &it.NameEn, &it.UnitPrice, &it.Qty,
			&it.LineTotal, &opts, &it.Note, &it.NeedsPrep, &it.PrepStatus, &it.StationID, &it.StationName,
			&it.BatchSeq, &it.BatchSource, &it.ServedAt, &it.VoidReason, &it.Basket, &it.NameTr); err != nil {
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
		t.status, t.fail_reason, t.assigned_at, t.picked_up_at, t.arrived_at, t.delivered_at, t.seen_at,
		(SELECT p.created_at FROM delivery_proofs p WHERE p.task_id = t.id)
		FROM delivery_tasks t JOIN users u ON u.id = t.rider_id WHERE t.order_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t DeliveryTask
		var orderID int64
		if err := rows.Scan(&t.ID, &orderID, &t.RiderID, &t.RiderName, &t.RiderPhone, &t.AssignmentVersion,
			&t.Status, &t.FailReason, &t.AssignedAt, &t.PickedUpAt, &t.ArrivedAt, &t.DeliveredAt, &t.SeenAt, &t.ProofAt); err != nil {
			rows.Close()
			return nil, err
		}
		byID[orderID].Delivery = &t
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := loadDiscounts(ctx, q, byID, ids); err != nil {
		return nil, err
	}
	cash, err := queryCash(ctx, q, `c.order_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for _, c := range cash {
		byID[c.OrderID].Cash = c
	}
	rows, err = q.Query(ctx, `SELECT order_id, `+aftersaleCols+` FROM order_aftersales WHERE order_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var as Aftersale
		var orderID int64
		if err := rows.Scan(&orderID, &as.ID, &as.Reason, &as.Detail, &as.Status, &as.Reply, &as.CreatedAt, &as.HandledAt); err != nil {
			return nil, err
		}
		byID[orderID].Aftersale = &as
	}
	return orders, rows.Err()
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

// nextOrderNo takes the next number of the branch's daily sequence (in branch
// local time) and returns the public order number and its short code.
func nextOrderNo(ctx context.Context, tx pgx.Tx, b *Branch, prefix string) (publicNo, code string, err error) {
	today := time.Now().In(b.location())
	var seq int
	err = tx.QueryRow(ctx, `UPDATE branches SET
		order_seq = CASE WHEN order_seq_date = $2::date THEN order_seq + 1 ELSE 1 END,
		order_seq_date = $2::date WHERE id=$1 RETURNING order_seq`, b.ID, today.Format("2006-01-02")).Scan(&seq)
	if err != nil {
		return "", "", err
	}
	code = fmt.Sprintf("%03d", seq)
	return fmt.Sprintf("%s%s-%s", prefix, today.Format("0102"), code), code, nil
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
	if len(in.CustomerName) > 80 {
		return nil, ErrValidation("customer_name is too long")
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
		// A saved address brings the name and the address with it.
		if quoted.Input.AddressID == nil || quoted.ServiceType != "delivery" {
			if in.CustomerName == "" {
				return ErrValidation("customer_name is required")
			}
			if quoted.ServiceType == "delivery" && in.AddressLine == "" {
				return ErrValidation("address_line is required for delivery")
			}
		}

		// Re-price against the live menu; anything that moved needs a new confirmation.
		a.forCustomer(b)
		cur, err := computeQuote(ctx, tx, b, quoted.Input, phone)
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

		publicNo, pickupCode, err := nextOrderNo(ctx, tx, b, "W")
		if err != nil {
			return err
		}
		nonce := randomBytes(16)
		token := a.orderToken(nonce)
		payMethod := "cod"
		if cur.ServiceType == "pickup" {
			payMethod = "pay_at_pickup"
		}
		t := cur.Totals
		// The pin is only meaningful where the server matched the zone from it.
		var destLat, destLng *float64
		if cur.ServiceType == "delivery" && b.ZoneMode == "map" {
			destLat, destLng = cur.Input.Lat, cur.Input.Lng
		}
		// A saved address fills in who and where; the order keeps its own copy.
		var addressID *int64
		var contact Address
		if ad := cur.address; ad != nil {
			addressID, contact = &ad.ID, *ad
			contact.onlyAsked(b)
			in.CustomerName, in.AddressLine, in.AddressUnit, in.AddressNote = ad.ContactName, ad.line(), ad.Unit, ad.Note
			destLat, destLng = ad.Lat, ad.Lng
		}
		var orderID int64
		err = tx.QueryRow(ctx, `INSERT INTO orders (merchant_id, branch_id, public_no, pickup_code,
			access_token_hash, token_nonce, service_type, customer_name, customer_phone, zone_id, zone_name,
			address_line, address_unit, address_note, note, currency, subtotal, packaging_fee, delivery_fee,
			service_charge, tax_amount, tax_inclusive, tax_rate_bp, discount, total, payment_method, accept_deadline,
			promotion_id, promotion_name, promotion_name_en, scheduled_for, release_at, dest_lat, dest_lng,
			zone_distance_m, address_id, contact_phone, contact_title, contact_wechat, contact_telegram,
			contact_line, contact_whatsapp, customer_lang)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,
				now() + make_interval(mins => $27), NULLIF($28, 0), $29, $30,
				$31::timestamptz, $31::timestamptz - make_interval(mins => $32), $33, $34, $35, $36, $37, $38, $39, $40,
				$41, $42, $43)
			RETURNING id`,
			b.MerchantID, b.ID, publicNo, pickupCode, hashToken(token), nonce, cur.ServiceType, in.CustomerName,
			phone, cur.ZoneID, cur.ZoneName, in.AddressLine, in.AddressUnit, in.AddressNote, in.Note, b.Currency,
			t.Subtotal, t.PackagingFee, t.DeliveryFee, t.ServiceCharge, t.TaxAmount, t.TaxInclusive, t.TaxRateBP,
			t.Discount, t.Total, payMethod, b.AcceptTimeoutMinutes, t.PromotionID, t.PromotionName,
			t.PromotionNameEn, cur.ScheduledFor, b.PrepMinutes, destLat, destLng, cur.ZoneDistanceM,
			addressID, contact.Phone, contact.Title, contact.Wechat, contact.Telegram,
			contact.Line, contact.Whatsapp, langOrEmpty(cur.Input.Lang)).Scan(&orderID)
		if err != nil {
			return wrap("insert order", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE orders o SET customer_id = c.id FROM customers c WHERE o.id=$1 AND c.phone=$2`,
			orderID, phone); err != nil {
			return wrap("link customer", err)
		}
		if err := touchCustomerSessions(ctx, tx, phone); err != nil {
			return wrap("extend session", err)
		}
		if addressID != nil {
			if _, err := tx.Exec(ctx, `UPDATE customer_addresses SET last_used_at=now() WHERE id=$1`, *addressID); err != nil {
				return wrap("address used", err)
			}
		}
		if t.PromotionID != 0 {
			if err := claimPromotion(ctx, tx, b.ID, t.PromotionID, orderID, phone, t.PromotionDiscount); err != nil {
				return err
			}
		}
		if err := claimCoupons(ctx, tx, b, orderID, phone, cur); err != nil {
			return err
		}
		var batchID int64
		if err := tx.QueryRow(ctx, `INSERT INTO order_batches (order_id, seq) VALUES ($1, 1) RETURNING id`,
			orderID).Scan(&batchID); err != nil {
			return err
		}
		for _, l := range cur.Lines {
			opts, _ := json.Marshal(l.Options)
			if _, err := tx.Exec(ctx, `INSERT INTO order_items (order_id, branch_id, batch_id, item_id, name,
				name_en, unit_price, qty, line_total, options, note, needs_prep, basket_no, name_customer)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, orderID, b.ID, batchID, l.ItemID, l.Name,
				l.NameEn, l.UnitPrice, l.Qty, l.LineTotal, opts, l.Note, l.NeedsPrep, max(1, l.Basket), l.NameTr); err != nil {
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
	o := orders[0]
	if o.Gift, err = orderGift(ctx, a.DB, o.ID); err != nil {
		return nil, err
	}
	return o, nil
}

// closeOrder is shared by reject, cancel and timeout: it releases stock, marks
// items and the delivery task cancelled and records the reason.
func (a *App) closeOrder(ctx context.Context, tx pgx.Tx, o *lockedOrder, status, reason string, actor Actor) error {
	if err := releaseStock(ctx, tx, o.ID); err != nil {
		return err
	}
	if err := releasePromotion(ctx, tx, o.ID); err != nil {
		return err
	}
	if err := releaseCoupons(ctx, tx, o.ID); err != nil {
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
		if err := tripDrop(ctx, tx, o.ID, "removed"); err != nil {
			return err
		}
		if err := a.pushRider(ctx, tx, riderID, o.ID, "cancelled"); err != nil {
			return err
		}
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
	if actor.Type != "customer" {
		if err := a.notifyCustomer(ctx, tx, o.ID, status); err != nil {
			return err
		}
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
		return a.closeOrder(ctx, tx, o, "cancelled", "customer_cancelled", Actor{Type: "customer"})
	})
	if err != nil {
		return nil, wrap("customer cancel", err)
	}
	return loadOrder(ctx, a.DB, id)
}
