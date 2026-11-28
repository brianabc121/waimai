package app

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Purchase limits on chosen items (商品营销: 每单限购、每日限购). A limit is
// set up like an item promotion (a row of promotions with its items, dates,
// daily hours, scope and brands) but takes no money off: it only refuses
// lines that would put more of an item on an order, or in a customer's day,
// than the store allows. Nothing of it is recorded on the order, so pricing,
// voids and refunds never read it.
const (
	KindLimitOrder = "dish_limit_order"
	KindLimitDay   = "dish_limit_day"

	limitKindsSQL = "('dish_limit_order','dish_limit_day')"
	// notLimitSQL keeps limits out of the promotions that are priced or
	// advertised as offers.
	notLimitSQL = "p.kind NOT IN " + limitKindsSQL

	maxItemLimit = 99 // a line holds at most 99
)

// checkLimit checks a limit's settings: how many, and on which items.
func checkLimit(in *PromotionInput) error {
	if in.Value < 1 || in.Value > maxItemLimit {
		return ErrValidation("the limit must be between 1 and 99")
	}
	if err := checkDishes(in); err != nil {
		return err
	}
	// a limit holds whatever the order is worth
	in.Threshold = 0
	return nil
}

func (p *Promotion) isLimit() bool { return promoRules[p.Kind].limit != "" }

// ItemLimit is how many of an item may be bought: on one order, and by one
// customer in a day. 0 is no limit.
type ItemLimit struct {
	ItemID   int64 `json:"item_id"`
	PerOrder int   `json:"per_order,omitempty"`
	PerDay   int   `json:"per_day,omitempty"`
}

// liveLimits are the limits of b's brand that hold at at for an order of
// service, by item; where several are on one item, the strictest of each kind.
func liveLimits(ctx context.Context, q Querier, b *Branch, brandID int64, service string, at time.Time) (map[int64]*ItemLimit, error) {
	scope := "delivery"
	if service == "dine_in" {
		scope = "dine_in"
	}
	ps, err := loadPromotions(ctx, q, b.ID, "p.status='active' AND p.kind IN "+limitKindsSQL+" AND p.scope="+
		scopeSQL(scope)+" AND "+brandPromoSQL(brandID), "")
	if err != nil {
		return nil, err
	}
	out := map[int64]*ItemLimit{}
	for i := range ps {
		p := &ps[i]
		if p.why(at, b.location(), service, 0) != "" {
			continue
		}
		for _, id := range p.ItemIDs {
			l := out[id]
			if l == nil {
				l = &ItemLimit{ItemID: id}
				out[id] = l
			}
			n := int(p.Value)
			switch promoRules[p.Kind].limit {
			case "order":
				if l.PerOrder == 0 || n < l.PerOrder {
					l.PerOrder = n
				}
			case "day":
				if l.PerDay == 0 || n < l.PerDay {
					l.PerDay = n
				}
			}
		}
	}
	return out, nil
}

// storeLimits is liveLimits as the storefront shows it, in item order.
func storeLimits(ctx context.Context, q Querier, b *Branch, brandID int64, service string) ([]ItemLimit, error) {
	m, err := liveLimits(ctx, q, b, brandID, service, time.Now())
	if err != nil {
		return nil, err
	}
	out := make([]ItemLimit, 0, len(m))
	for _, l := range m {
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID < out[j].ItemID })
	return out, nil
}

// liveLineSQL is an order line that still counts: not voided, on an order
// that was not rejected or cancelled.
const liveLineSQL = `oi.void_id IS NULL AND oi.void_reason = '' AND oi.prep_status <> 'cancelled'
	AND o.acceptance_status IN ('pending','accepted')`

// checkItemLimits refuses lines that go over a limit. orderID is the order
// they are added to (0 for a new one): what it already holds counts toward
// the limit per order. phone is the customer's; without one (a table with no
// member) only the limit per order can be checked. Inside a transaction the
// customer's day is locked, so two orders at once cannot both take the last
// ones.
func checkItemLimits(ctx context.Context, q Querier, b *Branch, brandID int64, service, phone string, orderID int64, lines []QuoteLine) error {
	limits, err := liveLimits(ctx, q, b, brandID, service, time.Now())
	if err != nil || len(limits) == 0 {
		return err
	}
	want := map[int64]int{}
	names := map[int64]string{}
	var ids []int64
	daily := false
	for _, l := range lines {
		lim := limits[l.ItemID]
		if lim == nil {
			continue
		}
		if _, seen := want[l.ItemID]; !seen {
			ids = append(ids, l.ItemID)
			names[l.ItemID] = l.Name
		}
		want[l.ItemID] += l.Qty
		daily = daily || lim.PerDay > 0
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	over := func(id int64, kind string, limit, bought int) error {
		return ErrD(http.StatusUnprocessableEntity, "ITEM_LIMIT", "more of an item than the store allows", map[string]any{
			"item_id": id, "name": names[id], "kind": kind, "limit": limit, "bought": bought, "left": max(limit-bought, 0)})
	}
	onOrder := map[int64]int{}
	if orderID != 0 {
		if onOrder, err = boughtQty(ctx, q, `oi.order_id = $2`, ids, orderID); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if n := limits[id].PerOrder; n > 0 && onOrder[id]+want[id] > n {
			return over(id, "order", n, onOrder[id])
		}
	}
	if !daily || phone == "" {
		return nil
	}
	if tx, ok := q.(pgx.Tx); ok {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "item_limit:"+keyOf(b.ID)+":"+phone); err != nil {
			return err
		}
	}
	now := time.Now().In(b.location())
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today, err := boughtQty(ctx, q, `o.branch_id = $2 AND o.customer_phone = $3 AND ob.created_at >= $4`, ids, b.ID, phone, day)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if n := limits[id].PerDay; n > 0 && today[id]+want[id] > n {
			return over(id, "day", n, today[id])
		}
	}
	return nil
}

// boughtQty is how many of each of items the live lines matching where hold.
func boughtQty(ctx context.Context, q Querier, where string, items []int64, args ...any) (map[int64]int, error) {
	rows, err := q.Query(ctx, `SELECT oi.item_id, sum(oi.qty)::int FROM order_items oi
		JOIN orders o ON o.id = oi.order_id JOIN order_batches ob ON ob.id = oi.batch_id
		WHERE oi.item_id = ANY($1) AND `+liveLineSQL+` AND `+where+` GROUP BY 1`, append([]any{items}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// checkOrderLimits is checkItemLimits for lines added to an existing order,
// under the order's own brand and customer.
func checkOrderLimits(ctx context.Context, q Querier, b *Branch, orderID int64, service string, lines []QuoteLine) error {
	var brandID int64
	var phone string
	if err := q.QueryRow(ctx, `SELECT brand_id, customer_phone FROM orders WHERE id=$1`, orderID).Scan(&brandID, &phone); err != nil {
		return err
	}
	return checkItemLimits(ctx, q, b, brandID, service, phone, orderID, lines)
}
