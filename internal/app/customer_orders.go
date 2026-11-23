package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The logged-in customer's own orders: the list, ordering the same again,
// and after-sales requests.

// OrderStore is the store of an order as its customer sees it.
type OrderStore struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	NameEn  string `json:"name_en"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
	LogoURL string `json:"logo_url"`
}

// OrderCard is one order in the customer's list.
type OrderCard struct {
	Token         string     `json:"token"`
	PublicNo      string     `json:"public_no"`
	Store         OrderStore `json:"store"`
	ServiceType   string     `json:"service_type"`
	DisplayStatus string     `json:"display_status"`
	CreatedAt     time.Time  `json:"created_at"`
	Timezone      string     `json:"timezone"`
	Total         int64      `json:"total"`
	Currency      string     `json:"currency"`
	// ItemCount is how many dishes, counting quantities.
	ItemCount int        `json:"item_count"`
	Items     []CardItem `json:"items"`
	CanReview bool       `json:"can_review"`
	Reviewed  bool       `json:"reviewed"`
	// Aftersale is the state of the after-sales request: "", open, resolved or declined.
	Aftersale string `json:"aftersale"`
	id        int64
}

type CardItem struct {
	Name     string `json:"name"`
	NameEn   string `json:"name_en"`
	NameTr   string `json:"name_tr,omitempty"`
	Qty      int    `json:"qty"`
	ImageURL string `json:"image_url"`
}

type OrderCardPage struct {
	Orders []OrderCard `json:"orders"`
	// Next is the cursor of the following page, 0 when this is the last.
	Next int64 `json:"next"`
}

const orderPageSize = 20

const inProgress = `(o.acceptance_status = 'pending' OR (o.acceptance_status = 'accepted'
	AND o.fulfillment_status NOT IN ('delivered','picked_up')))`

// CustomerOrders lists a customer's delivery and pickup orders, newest first.
// tab is all, active (still on the way), review (finished, not reviewed yet)
// or aftersale; search matches the store's or a dish's name; before is the
// cursor a previous page returned.
func (a *App) CustomerOrders(ctx context.Context, customerID int64, tab, search string, before int64) (*OrderCardPage, error) {
	cond := "true"
	switch tab {
	case "", "all":
	case "active":
		cond = inProgress
	case "review":
		cond = `o.acceptance_status = 'accepted' AND o.fulfillment_status IN ('delivered','picked_up')
			AND o.completed_at > now() - interval '30 days' AND NOT EXISTS (SELECT 1 FROM reviews r WHERE r.order_id = o.id)`
	case "aftersale":
		cond = `s.id IS NOT NULL`
	default:
		return nil, ErrValidation("tab must be all, active, review or aftersale")
	}
	search = strings.TrimSpace(search)
	if len([]rune(search)) > 50 {
		return nil, ErrValidation("search is too long")
	}
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search) + "%"
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.token_nonce, o.public_no, o.service_type, o.acceptance_status,
		o.fulfillment_status, o.release_at, o.completed_at, o.created_at, o.total, o.currency,
		b.slug, b.name, b.name_en, b.phone, b.address, b.timezone, COALESCE(ct.published->>'logo_url', ''),
		EXISTS (SELECT 1 FROM reviews r WHERE r.order_id = o.id), COALESCE(s.status, '')
		FROM orders o JOIN branches b ON b.id = o.branch_id
		LEFT JOIN channel_templates ct ON ct.branch_id = b.id AND ct.channel = 'delivery'
		LEFT JOIN order_aftersales s ON s.order_id = o.id
		WHERE o.customer_id = $1 AND o.service_type <> 'dine_in' AND (`+cond+`)
		AND ($2 = '' OR b.name ILIKE $3 OR b.name_en ILIKE $3 OR o.public_no ILIKE $3 OR EXISTS (SELECT 1 FROM order_items i
			WHERE i.order_id = o.id AND (i.name ILIKE $3 OR i.name_en ILIKE $3)))
		AND ($4 = 0 OR o.id < $4) ORDER BY o.id DESC LIMIT $5`, customerID, search, like, before, orderPageSize+1)
	if err != nil {
		return nil, wrap("customer orders", err)
	}
	out := &OrderCardPage{Orders: []OrderCard{}}
	idx := map[int64]int{}
	ids := []int64{}
	for rows.Next() {
		c := OrderCard{Items: []CardItem{}}
		var nonce []byte
		var acceptance, fulfillment string
		var releaseAt, completedAt *time.Time
		if err := rows.Scan(&c.id, &nonce, &c.PublicNo, &c.ServiceType, &acceptance, &fulfillment, &releaseAt,
			&completedAt, &c.CreatedAt, &c.Total, &c.Currency, &c.Store.Slug, &c.Store.Name, &c.Store.NameEn,
			&c.Store.Phone, &c.Store.Address, &c.Timezone, &c.Store.LogoURL, &c.Reviewed, &c.Aftersale); err != nil {
			rows.Close()
			return nil, err
		}
		if len(out.Orders) == orderPageSize {
			out.Next = out.Orders[orderPageSize-1].id
			continue
		}
		c.Token = a.orderToken(nonce)
		c.DisplayStatus = DisplayStatus(acceptance, fulfillment)
		if c.DisplayStatus == "preparing" && releaseAt != nil && releaseAt.After(time.Now()) {
			c.DisplayStatus = "scheduled"
		}
		c.CanReview = !c.Reviewed && (c.DisplayStatus == "delivered" || c.DisplayStatus == "picked_up") &&
			completedAt != nil && time.Since(*completedAt) < reviewWindow
		idx[c.id] = len(out.Orders)
		ids = append(ids, c.id)
		out.Orders = append(out.Orders, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err = a.DB.Query(ctx, `SELECT i.order_id, i.name, i.name_en, i.qty, COALESCE(m.image_url, ''), i.name_customer
		FROM order_items i LEFT JOIN menu_items m ON m.id = i.item_id
		WHERE i.order_id = ANY($1) AND i.void_reason = '' ORDER BY i.id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var orderID int64
		var it CardItem
		if err := rows.Scan(&orderID, &it.Name, &it.NameEn, &it.Qty, &it.ImageURL, &it.NameTr); err != nil {
			return nil, err
		}
		c := &out.Orders[idx[orderID]]
		c.ItemCount += it.Qty
		c.Items = append(c.Items, it)
	}
	return out, rows.Err()
}

// CustomerOrderView adds to an order what only its customer's page shows:
// the store and the dishes' pictures.
func (a *App) CustomerOrderView(ctx context.Context, o *Order) error {
	st := OrderStore{}
	err := a.DB.QueryRow(ctx, `SELECT b.slug, b.name, b.name_en, b.phone, b.address, COALESCE(ct.published->>'logo_url', '')
		FROM branches b LEFT JOIN channel_templates ct ON ct.branch_id = b.id AND ct.channel = 'delivery'
		WHERE b.id = $1`, o.BranchID).Scan(&st.Slug, &st.Name, &st.NameEn, &st.Phone, &st.Address, &st.LogoURL)
	if err != nil {
		return err
	}
	o.Store = &st
	ids := make([]int64, len(o.Items))
	for i, it := range o.Items {
		ids[i] = it.ItemID
	}
	rows, err := a.DB.Query(ctx, `SELECT id, image_url FROM menu_items WHERE id = ANY($1) AND image_url <> ''`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	img := map[int64]string{}
	for rows.Next() {
		var id int64
		var u string
		if err := rows.Scan(&id, &u); err != nil {
			return err
		}
		img[id] = u
	}
	for i := range o.Items {
		o.Items[i].ImageURL = img[o.Items[i].ItemID]
	}
	return rows.Err()
}

// ---- order the same again ----

// ReorderLine is one line for the cart, written against today's menu.
type ReorderLine struct {
	ItemID    int64   `json:"item_id"`
	Qty       int     `json:"qty"`
	OptionIDs []int64 `json:"option_ids"`
	Note      string  `json:"note"`
	Basket    int     `json:"basket"`
}

type Reorder struct {
	Slug        string        `json:"slug"`
	ServiceType string        `json:"service_type"`
	Lines       []ReorderLine `json:"lines"`
	// Missing are the dishes that are no longer sold, or whose options changed.
	Missing []ReviewItem `json:"missing"`
}

// Reorder turns an earlier order into cart lines. The order kept the names of
// the chosen options, not their ids, so they are matched by name against the
// dish as it is sold now; a dish that cannot be matched is reported as missing.
func (a *App) Reorder(ctx context.Context, token string) (*Reorder, error) {
	o, err := a.OrderByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if o.ServiceType == "dine_in" {
		return nil, ErrNotFound
	}
	out := &Reorder{ServiceType: o.ServiceType, Lines: []ReorderLine{}, Missing: []ReviewItem{}}
	if err := a.DB.QueryRow(ctx, `SELECT slug FROM branches WHERE id=$1`, o.BranchID).Scan(&out.Slug); err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, it := range o.Items {
		ids = append(ids, it.ItemID)
	}
	rows, err := a.DB.Query(ctx, `SELECT m.id, g.name, p.name, p.id FROM menu_items m
		LEFT JOIN option_groups g ON g.item_id = m.id LEFT JOIN options p ON p.group_id = g.id
		WHERE m.id = ANY($1) AND m.branch_id = $2 AND m.published`, ids, o.BranchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type key struct{ group, name string }
	menu := map[int64]map[key]int64{}
	for rows.Next() {
		var id int64
		var group, name *string
		var optID *int64
		if err := rows.Scan(&id, &group, &name, &optID); err != nil {
			return nil, err
		}
		if menu[id] == nil {
			menu[id] = map[key]int64{}
		}
		if optID != nil {
			menu[id][key{*group, *name}] = *optID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
lines:
	for _, it := range o.Items {
		if it.VoidReason != "" {
			continue
		}
		opts, ok := menu[it.ItemID]
		gone := ReviewItem{Name: it.Name, NameEn: it.NameEn}
		if !ok {
			out.Missing = append(out.Missing, gone)
			continue
		}
		l := ReorderLine{ItemID: it.ItemID, Qty: it.Qty, OptionIDs: []int64{}, Note: it.Note, Basket: max(it.Basket, 1)}
		for _, op := range it.Options {
			id, ok := opts[key{op.Group, op.Name}]
			if !ok {
				out.Missing = append(out.Missing, gone)
				continue lines
			}
			l.OptionIDs = append(l.OptionIDs, id)
		}
		out.Lines = append(out.Lines, l)
	}
	return out, nil
}

// ---- after-sales ----

type Aftersale struct {
	ID        int64      `json:"id"`
	Reason    string     `json:"reason"`
	Detail    string     `json:"detail"`
	Status    string     `json:"status"`
	Reply     string     `json:"reply"`
	CreatedAt time.Time  `json:"created_at"`
	HandledAt *time.Time `json:"handled_at"`
}

const aftersaleCols = `id, reason, detail, status, reply, created_at, handled_at`

type AftersaleInput struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

var aftersaleReasons = map[string]bool{"wrong_item": true, "missing_item": true, "quality": true,
	"late": true, "not_received": true, "changed_mind": true, "other": true}

// aftersaleWindow is how long after ordering a request can still be made.
const aftersaleWindow = 7 * 24 * time.Hour

// RequestAftersale records the customer's request on an order the store has
// accepted. An order still waiting for the store is simply cancelled instead.
func (a *App) RequestAftersale(ctx context.Context, token string, in AftersaleInput) (*Order, error) {
	in.Detail = strings.TrimSpace(in.Detail)
	if !aftersaleReasons[in.Reason] {
		return nil, ErrValidation("unknown reason")
	}
	if len([]rune(in.Detail)) > 500 {
		return nil, ErrValidation("the description must be at most 500 characters")
	}
	if in.Reason == "other" && in.Detail == "" {
		return nil, ErrValidation("describe what happened")
	}
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var created time.Time
		var service string
		if err := tx.QueryRow(ctx, `SELECT id, created_at, service_type FROM orders WHERE access_token_hash=$1`,
			hashToken(token)).Scan(&id, &created, &service); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		o, err := lockOrder(ctx, tx, id)
		if err != nil {
			return err
		}
		if o.Acceptance != "accepted" || service == "dine_in" {
			return Err(http.StatusConflict, "AFTERSALE_NOT_ALLOWED", "this order cannot have an after-sales request")
		}
		if time.Since(created) > aftersaleWindow {
			return Err(http.StatusConflict, "AFTERSALE_TOO_LATE", "after-sales requests are taken for 7 days after ordering")
		}
		tag, err := tx.Exec(ctx, `INSERT INTO order_aftersales (order_id, branch_id, reason, detail) VALUES ($1,$2,$3,$4)
			ON CONFLICT (order_id) DO NOTHING`, id, o.BranchID, in.Reason, in.Detail)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return Err(http.StatusConflict, "AFTERSALE_EXISTS", "this order already has an after-sales request")
		}
		if err := bump(ctx, tx, id); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, id, Actor{Type: "customer"}, "order.aftersale_requested",
			map[string]any{"reason": in.Reason}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: id})
	})
	if err != nil {
		return nil, wrap("request aftersale", err)
	}
	return loadOrder(ctx, a.DB, id)
}

// HandleAftersale records how the store settled a request: resolved (agreed
// and dealt with) or declined, with a word to the customer.
func (a *App) HandleAftersale(ctx context.Context, orderID int64, status, reply string, actor Actor) (*Order, error) {
	reply = strings.TrimSpace(reply)
	if status != "resolved" && status != "declined" {
		return nil, ErrValidation("status must be resolved or declined")
	}
	if len([]rune(reply)) > 500 {
		return nil, ErrValidation("the reply must be at most 500 characters")
	}
	if status == "declined" && reply == "" {
		return nil, ErrValidation("say why the request is declined")
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		tag, err := tx.Exec(ctx, `UPDATE order_aftersales SET status=$2, reply=$3, handled_by=NULLIF($4::bigint, 0),
			handled_at=now() WHERE order_id=$1 AND status='open'`, orderID, status, reply, actor.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrInvalidState("there is no open after-sales request on this order")
		}
		if err := bump(ctx, tx, orderID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, orderID, actor, "order.aftersale_"+status, nil); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: orderID})
	})
}
