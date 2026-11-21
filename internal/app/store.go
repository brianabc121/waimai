package app

import (
	"context"
	"net/http"
	"time"
)

type Branch struct {
	ID                   int64  `json:"id"`
	MerchantID           int64  `json:"merchant_id"`
	Slug                 string `json:"slug"`
	Name                 string `json:"name"`
	NameEn               string `json:"name_en"`
	Address              string `json:"address"`
	Phone                string `json:"phone"`
	Timezone             string `json:"timezone"`
	Currency             string `json:"currency"`
	TaxRateBP            int    `json:"tax_rate_bp"`
	TaxInclusive         bool   `json:"tax_inclusive"`
	ServiceChargeBP      int    `json:"service_charge_bp"`
	PackagingFee         int64  `json:"packaging_fee"`
	MinOrderAmount       int64  `json:"min_order_amount"`
	AcceptTimeoutMinutes int    `json:"accept_timeout_minutes"`
	DeliveryEnabled      bool   `json:"delivery_enabled"`
	PickupEnabled        bool   `json:"pickup_enabled"`
	Paused               bool   `json:"paused"`
	OpenTime             string `json:"open_time"`
	CloseTime            string `json:"close_time"`
	NotifySMS            bool   `json:"notify_sms"`
	SchedulingEnabled    bool   `json:"scheduling_enabled"`
	PrepMinutes          int    `json:"prep_minutes"`
	OpenNow              bool   `json:"open_now"`
}

func (b *Branch) taxConfig() TaxConfig {
	return TaxConfig{TaxRateBP: b.TaxRateBP, TaxInclusive: b.TaxInclusive,
		ServiceChargeBP: b.ServiceChargeBP, PackagingFee: b.PackagingFee}
}

func (b *Branch) location() *time.Location {
	loc, err := time.LoadLocation(b.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// isOpen interprets open/close in the branch's local time. Equal values mean
// open all day; close < open means the window crosses midnight.
func (b *Branch) isOpen(now time.Time) bool {
	if b.OpenTime == b.CloseTime {
		return true
	}
	cur := now.In(b.location()).Format("15:04")
	if b.OpenTime < b.CloseTime {
		return cur >= b.OpenTime && cur < b.CloseTime
	}
	return cur >= b.OpenTime || cur < b.CloseTime
}

const slotStep = 15 * time.Minute

// scheduleLimit is the last moment an order can be scheduled for: the end of
// tomorrow in the branch's local time.
func (b *Branch) scheduleLimit(now time.Time) time.Time {
	l := now.In(b.location())
	return time.Date(l.Year(), l.Month(), l.Day()+2, 0, 0, 0, 0, l.Location())
}

// Slots lists the times a customer can schedule an order for: every 15
// minutes from "now plus preparation time" to the end of tomorrow, inside
// opening hours.
func (b *Branch) Slots(now time.Time) []time.Time {
	out := []time.Time{}
	if !b.SchedulingEnabled || b.Paused {
		return out
	}
	t := now.Add(time.Duration(b.PrepMinutes) * time.Minute).Truncate(slotStep).Add(slotStep)
	for limit := b.scheduleLimit(now); t.Before(limit); t = t.Add(slotStep) {
		if b.isOpen(t) {
			out = append(out, t.UTC())
		}
	}
	return out
}

// checkSchedule validates a requested time. The lead time is relaxed by the
// quote's lifetime so that a slot offered a few minutes ago is still accepted
// when the customer confirms.
func (b *Branch) checkSchedule(now, at time.Time) error {
	bad := func(msg string) error { return Err(http.StatusUnprocessableEntity, "SCHEDULE_INVALID", msg) }
	switch {
	case !b.SchedulingEnabled:
		return bad("this store does not take scheduled orders")
	case at.Before(now.Add(time.Duration(b.PrepMinutes)*time.Minute - quoteTTL)):
		return bad("that time is too soon, please pick a later one")
	case !at.Before(b.scheduleLimit(now)):
		return bad("orders can be scheduled up to the end of tomorrow")
	case !b.isOpen(at):
		return bad("the store is closed at that time")
	}
	return nil
}

const branchCols = `id, merchant_id, slug, name, name_en, address, phone, timezone, currency, tax_rate_bp,
	tax_inclusive, service_charge_bp, packaging_fee, min_order_amount, accept_timeout_minutes,
	delivery_enabled, pickup_enabled, paused, open_time, close_time, notify_sms, scheduling_enabled, prep_minutes`

func scanBranch(row interface{ Scan(...any) error }) (*Branch, error) {
	var b Branch
	err := row.Scan(&b.ID, &b.MerchantID, &b.Slug, &b.Name, &b.NameEn, &b.Address, &b.Phone, &b.Timezone,
		&b.Currency, &b.TaxRateBP, &b.TaxInclusive, &b.ServiceChargeBP, &b.PackagingFee, &b.MinOrderAmount,
		&b.AcceptTimeoutMinutes, &b.DeliveryEnabled, &b.PickupEnabled, &b.Paused, &b.OpenTime, &b.CloseTime, &b.NotifySMS,
		&b.SchedulingEnabled, &b.PrepMinutes)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	b.OpenNow = b.isOpen(time.Now())
	return &b, nil
}

func branchBySlug(ctx context.Context, q Querier, slug string) (*Branch, error) {
	return scanBranch(q.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE slug=$1`, slug))
}

func (a *App) BranchByID(ctx context.Context, id int64) (*Branch, error) {
	return scanBranch(a.DB.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, id))
}

type Zone struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	NameEn   string `json:"name_en"`
	Fee      int64  `json:"fee"`
	MinOrder int64  `json:"min_order"`
	Active   bool   `json:"active"`
	Sort     int    `json:"sort"`
}

type Option struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	NameEn     string `json:"name_en"`
	PriceDelta int64  `json:"price_delta"`
	SoldOut    bool   `json:"sold_out"`
}

type OptionGroup struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	NameEn    string   `json:"name_en"`
	MinSelect int      `json:"min_select"`
	MaxSelect int      `json:"max_select"`
	Options   []Option `json:"options"`
}

type Item struct {
	ID                int64         `json:"id"`
	CategoryID        int64         `json:"category_id"`
	Name              string        `json:"name"`
	NameEn            string        `json:"name_en"`
	Description       string        `json:"description"`
	ImageURL          string        `json:"image_url"`
	Price             int64         `json:"price"`
	Published         bool          `json:"published"`
	SoldOut           bool          `json:"sold_out"`
	StockQty          *int          `json:"stock_qty"`
	NeedsPrep         bool          `json:"needs_prep"`
	DeliveryAvailable bool          `json:"delivery_available"`
	PickupAvailable   bool          `json:"pickup_available"`
	Sort              int           `json:"sort"`
	StationID         *int64        `json:"station_id"`
	Groups            []OptionGroup `json:"groups"`
}

// available reports whether the item can be ordered right now for a service type.
func (it *Item) available(serviceType string) bool {
	if !it.Published || it.SoldOut {
		return false
	}
	if it.StockQty != nil && *it.StockQty <= 0 {
		return false
	}
	switch serviceType {
	case "delivery":
		return it.DeliveryAvailable
	case "dine_in":
		return true // everything on the published menu can be ordered at the table
	}
	return it.PickupAvailable
}

type Category struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	Sort   int    `json:"sort"`
	Items  []Item `json:"items"`
}

func loadZones(ctx context.Context, q Querier, branchID int64, activeOnly bool) ([]Zone, error) {
	rows, err := q.Query(ctx, `SELECT id, name, name_en, fee, min_order, active, sort FROM delivery_zones
		WHERE branch_id=$1 AND (active OR NOT $2) ORDER BY sort, id`, branchID, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	zones := []Zone{}
	for rows.Next() {
		var z Zone
		if err := rows.Scan(&z.ID, &z.Name, &z.NameEn, &z.Fee, &z.MinOrder, &z.Active, &z.Sort); err != nil {
			return nil, err
		}
		zones = append(zones, z)
	}
	return zones, rows.Err()
}

// loadItems returns non-archived items of a branch keyed by id, with option
// groups. publishedOnly hides drafts (customer view).
func loadItems(ctx context.Context, q Querier, branchID int64, publishedOnly bool) ([]*Item, error) {
	rows, err := q.Query(ctx, `SELECT id, category_id, name, name_en, description, image_url, price, published,
		sold_out, stock_qty, needs_prep, delivery_available, pickup_available, sort, station_id
		FROM menu_items WHERE branch_id=$1 AND NOT archived AND (published OR NOT $2) ORDER BY sort, id`,
		branchID, publishedOnly)
	if err != nil {
		return nil, err
	}
	items := []*Item{}
	byID := map[int64]*Item{}
	for rows.Next() {
		it := &Item{Groups: []OptionGroup{}}
		if err := rows.Scan(&it.ID, &it.CategoryID, &it.Name, &it.NameEn, &it.Description, &it.ImageURL,
			&it.Price, &it.Published, &it.SoldOut, &it.StockQty, &it.NeedsPrep, &it.DeliveryAvailable,
			&it.PickupAvailable, &it.Sort, &it.StationID); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, it)
		byID[it.ID] = it
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(ctx, `SELECT g.id, g.item_id, g.name, g.name_en, g.min_select, g.max_select
		FROM option_groups g JOIN menu_items i ON i.id = g.item_id
		WHERE i.branch_id=$1 AND NOT i.archived ORDER BY g.sort, g.id`, branchID)
	if err != nil {
		return nil, err
	}
	type gref struct {
		item *Item
		idx  int
	}
	groups := map[int64]gref{}
	for rows.Next() {
		var g OptionGroup
		var itemID int64
		if err := rows.Scan(&g.ID, &itemID, &g.Name, &g.NameEn, &g.MinSelect, &g.MaxSelect); err != nil {
			rows.Close()
			return nil, err
		}
		g.Options = []Option{}
		if it, ok := byID[itemID]; ok {
			it.Groups = append(it.Groups, g)
			groups[g.ID] = gref{it, len(it.Groups) - 1}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(ctx, `SELECT o.id, o.group_id, o.name, o.name_en, o.price_delta, o.sold_out
		FROM options o JOIN option_groups g ON g.id = o.group_id JOIN menu_items i ON i.id = g.item_id
		WHERE i.branch_id=$1 AND NOT i.archived ORDER BY o.sort, o.id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var o Option
		var groupID int64
		if err := rows.Scan(&o.ID, &groupID, &o.Name, &o.NameEn, &o.PriceDelta, &o.SoldOut); err != nil {
			return nil, err
		}
		if ref, ok := groups[groupID]; ok {
			ref.item.Groups[ref.idx].Options = append(ref.item.Groups[ref.idx].Options, o)
		}
	}
	return items, rows.Err()
}

func loadMenu(ctx context.Context, q Querier, branchID int64, publishedOnly bool) ([]Category, error) {
	rows, err := q.Query(ctx, `SELECT id, name, name_en, sort FROM menu_categories WHERE branch_id=$1 ORDER BY sort, id`, branchID)
	if err != nil {
		return nil, err
	}
	cats := []Category{}
	idx := map[int64]int{}
	for rows.Next() {
		c := Category{Items: []Item{}}
		if err := rows.Scan(&c.ID, &c.Name, &c.NameEn, &c.Sort); err != nil {
			rows.Close()
			return nil, err
		}
		idx[c.ID] = len(cats)
		cats = append(cats, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items, err := loadItems(ctx, q, branchID, publishedOnly)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if i, ok := idx[it.CategoryID]; ok {
			cats[i].Items = append(cats[i].Items, *it)
		}
	}
	return cats, nil
}

// Storefront is the anonymous, cacheable view of a branch.
type Storefront struct {
	Branch     *Branch     `json:"branch"`
	Zones      []Zone      `json:"zones"`
	Categories []Category  `json:"categories"`
	Promotions []Promotion `json:"promotions"`
	// Slots are the times an order can be scheduled for (empty when the store
	// does not take scheduled orders).
	Slots []time.Time `json:"slots"`
	// Templates is the published look of each ordering channel.
	Templates map[string]Template `json:"templates"`
}

func (a *App) Storefront(ctx context.Context, slug string) (*Storefront, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	zones, err := loadZones(ctx, a.DB, b.ID, true)
	if err != nil {
		return nil, err
	}
	cats, err := loadMenu(ctx, a.DB, b.ID, true)
	if err != nil {
		return nil, err
	}
	// Categories with nothing published (for example only drafts) are not shown.
	shown := cats[:0]
	for _, c := range cats {
		if len(c.Items) > 0 {
			shown = append(shown, c)
		}
	}
	cats = shown
	// Customers see whether a limited item is gone, not the exact stock.
	for i := range cats {
		for j := range cats[i].Items {
			it := &cats[i].Items[j]
			if it.StockQty != nil && *it.StockQty <= 0 {
				it.SoldOut = true
			}
			it.StockQty = nil
			it.StationID = nil
		}
	}
	promos, err := livePromotions(ctx, a.DB, b)
	if err != nil {
		return nil, err
	}
	tpl, err := a.PublishedTemplates(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	return &Storefront{Branch: b, Zones: zones, Categories: cats, Promotions: promos, Slots: b.Slots(time.Now()), Templates: tpl}, nil
}
