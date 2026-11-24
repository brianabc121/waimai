package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

const quoteTTL = 10 * time.Minute

type QuoteLineInput struct {
	ItemID    int64   `json:"item_id"`
	Qty       int     `json:"qty"`
	OptionIDs []int64 `json:"option_ids"`
	Note      string  `json:"note"`
	// NoteTagIDs are the chosen flavour notes (note_templates.go).
	NoteTagIDs []int64 `json:"note_tag_ids,omitempty"`
	// Basket is 1 to 4; 0 means the first.
	Basket int `json:"basket,omitempty"`
	// repeat lets an option be named more than once, for as many of it
	// (two portions of extra beef). Only orders placed by staff use it.
	repeat bool
}

type QuoteInput struct {
	ServiceType string `json:"service_type"`
	ZoneID      *int64 `json:"zone_id"`
	// Lat/Lng is where the customer dropped the pin. Stores in map mode
	// decide the zone from it and ignore ZoneID.
	Lat *float64 `json:"lat"`
	Lng *float64 `json:"lng"`
	// AddressID is one of the customer's saved addresses. Its pin is used
	// instead of Lat/Lng, and the order is sent to it.
	AddressID *int64           `json:"address_id"`
	Items     []QuoteLineInput `json:"items"`
	// ScheduledFor is nil for "as soon as possible".
	ScheduledFor *time.Time `json:"scheduled_for"`
	// CouponIDs are the coupons to use. nil lets the system choose the best
	// ones; an empty list uses none. A stored quote always has the list.
	CouponIDs *[]int64 `json:"coupon_ids"`
	// Lang is the customer's language: the lines are named in it as well, and
	// the order keeps those names (docs/I18N_DESIGN.md §6.4).
	Lang string `json:"lang"`
	// feeOverride replaces the zone's delivery fee (an order placed by staff).
	feeOverride *int64
}

type LineOption struct {
	// OptionID is the menu option chosen (lines from before it was kept have 0).
	OptionID   int64  `json:"option_id,omitempty"`
	Group      string `json:"group"`
	GroupEn    string `json:"group_en"`
	Name       string `json:"name"`
	NameEn     string `json:"name_en"`
	PriceDelta int64  `json:"price_delta"`
	// Qty is how many of it, when more than one (orders placed by staff);
	// PriceDelta is then for all of them and the name says "× n".
	Qty int `json:"qty,omitempty"`
	// In the customer's language, when it differs.
	GroupTr string `json:"group_tr,omitempty"`
	NameTr  string `json:"name_tr,omitempty"`
}

type QuoteLine struct {
	ItemID    int64         `json:"item_id"`
	Name      string        `json:"name"`
	NameEn    string        `json:"name_en"`
	NameTr    string        `json:"name_tr,omitempty"`
	UnitPrice int64         `json:"unit_price"`
	Qty       int           `json:"qty"`
	LineTotal int64         `json:"line_total"`
	Options   []LineOption  `json:"options"`
	Note      string        `json:"note"`
	NoteTags  []LineNoteTag `json:"note_tags"`
	NeedsPrep bool          `json:"needs_prep"`
	Basket    int           `json:"basket"`
	// CategoryID is only used while pricing, to check required categories.
	CategoryID int64 `json:"-"`
}

type Quote struct {
	ID          string `json:"id"`
	BranchID    int64  `json:"branch_id"`
	Currency    string `json:"currency"`
	ServiceType string `json:"service_type"`
	ZoneID      *int64 `json:"zone_id"`
	ZoneName    string `json:"zone_name"`
	// ZoneDistanceM is set when the pin is outside the zone's outline and was
	// matched by tolerance.
	ZoneDistanceM *int        `json:"zone_distance_m"`
	ScheduledFor  *time.Time  `json:"scheduled_for"`
	Lines         []QuoteLine `json:"lines"`
	Totals        Totals      `json:"totals"`
	// Coupons are the coupons used; CouponChoices are all of the customer's
	// coupons that work at this store, for the picker.
	Coupons       []AppliedCoupon `json:"coupons"`
	CouponChoices []CouponChoice  `json:"coupon_choices"`
	ExpiresAt     time.Time       `json:"expires_at"`
	Input         QuoteInput      `json:"input"`
	// address is the saved address named by Input.AddressID, as it is now.
	address *Address
}

// computeQuote validates the cart against the live menu and prices it. It is
// used both when quoting and again, inside the order transaction, to detect
// changes since the quote was issued.
// phone is the verified customer when known; it only matters for promotions
// with a per-customer limit.
func computeQuote(ctx context.Context, q Querier, b *Branch, in QuoteInput, phone string) (*Quote, error) {
	if !b.takesOrders() {
		return nil, errStoreClosed
	}
	if in.ScheduledFor != nil {
		at := in.ScheduledFor.UTC().Truncate(time.Minute)
		in.ScheduledFor = &at
		// A scheduled order may be placed while the store is closed, as long
		// as it is open at the chosen time.
		if err := b.checkSchedule(time.Now(), *in.ScheduledFor); err != nil {
			return nil, err
		}
	} else if !b.isOpen(time.Now()) {
		return nil, Err(http.StatusConflict, "STORE_CLOSED", "the store is closed")
	}
	switch in.ServiceType {
	case "delivery":
		if !b.DeliveryEnabled {
			return nil, Err(http.StatusConflict, "SERVICE_UNAVAILABLE", "delivery is not available")
		}
	case "pickup":
		if !b.PickupEnabled {
			return nil, Err(http.StatusConflict, "SERVICE_UNAVAILABLE", "pickup is not available")
		}
	default:
		return nil, ErrValidation("service_type must be delivery or pickup")
	}

	quote := &Quote{BranchID: b.ID, Currency: b.Currency, ServiceType: in.ServiceType, Input: in, Lines: []QuoteLine{},
		ScheduledFor: in.ScheduledFor}
	var deliveryFee, minOrder int64
	if in.ServiceType == "delivery" {
		lat, lng := in.Lat, in.Lng
		if in.AddressID != nil {
			// The address is read again each time: it may have been moved or
			// deleted since the quote.
			ad, err := addressOf(ctx, q, phone, *in.AddressID)
			if err != nil {
				return nil, err
			}
			quote.address, lat, lng = ad, ad.Lat, ad.Lng
		}
		var z Zone
		if b.ZoneMode == "map" {
			// The zone comes from the pin, never from the browser's zone_id.
			if lat == nil || lng == nil {
				return nil, Err(http.StatusUnprocessableEntity, "OUT_OF_ZONE", "locate the delivery address on the map")
			}
			loc, err := locate(ctx, q, b, LatLng{*lat, *lng})
			if err != nil {
				return nil, err
			}
			if loc.Zone == nil && in.feeOverride == nil {
				return nil, ErrD(http.StatusUnprocessableEntity, "OUT_OF_ZONE", "this location is outside the delivery area",
					map[string]any{"nearest": loc.Nearest})
			}
			// Staff may still deliver outside the areas, at a fee they set.
			if loc.Zone != nil {
				z = *loc.Zone
			}
			if loc.DistanceM > 0 {
				quote.ZoneDistanceM = &loc.DistanceM
			}
		} else {
			if in.ZoneID == nil {
				return nil, Err(http.StatusUnprocessableEntity, "OUT_OF_ZONE", "choose a delivery area")
			}
			err := q.QueryRow(ctx, `SELECT id, name, name_en, fee, min_order FROM delivery_zones
				WHERE id=$1 AND branch_id=$2 AND active`, *in.ZoneID, b.ID).Scan(&z.ID, &z.Name, &z.NameEn, &z.Fee, &z.MinOrder)
			if isNoRows(err) {
				return nil, Err(http.StatusUnprocessableEntity, "OUT_OF_ZONE", "this area is not served")
			} else if err != nil {
				return nil, err
			}
		}
		if z.ID != 0 {
			quote.ZoneID, quote.ZoneName = &z.ID, z.Name
		}
		deliveryFee = z.Fee
		if in.feeOverride != nil {
			deliveryFee = *in.feeOverride
		}
		minOrder = max(b.MinOrderAmount, z.MinOrder)
	}

	lines, subtotal, err := priceLines(ctx, q, b.ID, in.ServiceType, in.Items)
	if err != nil {
		return nil, err
	}
	if !b.BasketsEnabled {
		for i := range lines {
			lines[i].Basket = 1
		}
	}
	if err := localizeLines(ctx, q, b, in.Lang, lines); err != nil {
		return nil, err
	}
	quote.Lines = lines
	if err := checkRequired(ctx, q, b.ID, in.ServiceType, lines); err != nil {
		return nil, err
	}
	if subtotal < minOrder {
		return nil, ErrD(http.StatusUnprocessableEntity, "BELOW_MINIMUM", "order is below the minimum amount",
			map[string]any{"minimum": minOrder, "subtotal": subtotal})
	}
	promos, err := loadPromotions(ctx, q, b.ID, "p.status='active' AND p.scope='delivery'", phone)
	if err != nil {
		return nil, err
	}
	promo, d := bestPromotion(promos, time.Now(), b.location(), in.ServiceType, subtotal, deliveryFee)
	var promoID int64
	var promoName, promoNameEn string
	if promo != nil {
		promoID, promoName, promoNameEn = promo.ID, promo.Name, promo.NameEn
	}
	wallet, err := walletFor(ctx, q, phone, b)
	if err != nil {
		return nil, err
	}
	if err := applyCoupons(quote, wallet, b.taxConfig(), promoID, promoName, promoNameEn, subtotal, deliveryFee, d); err != nil {
		return nil, err
	}
	return quote, nil
}

// Location is the answer to "can you deliver here?".
type Location struct {
	// Zone is nil when the point is out of range.
	Zone *Zone `json:"zone"`
	// DistanceM is 0 inside the zone, otherwise the metres outside its outline
	// (within the store's tolerance).
	DistanceM int `json:"distance_m"`
	// Nearest is set when out of range and at least one zone is drawn.
	Nearest *NearestZone `json:"nearest"`
}

type NearestZone struct {
	Name      string `json:"name"`
	NameEn    string `json:"name_en"`
	DistanceM int    `json:"distance_m"`
}

func locate(ctx context.Context, q Querier, b *Branch, at LatLng) (*Location, error) {
	if !validLatLng(at) {
		return nil, ErrValidation("invalid coordinates")
	}
	zones, err := loadZones(ctx, q, b.ID, true)
	if err != nil {
		return nil, err
	}
	hit, nearest := matchZone(zones, at, b.ZoneToleranceM)
	out := &Location{}
	if hit != nil {
		z := *hit.Zone
		z.Polygon = nil // the caller already has the outlines
		out.Zone, out.DistanceM = &z, hit.DistanceM
	} else if nearest != nil {
		out.Nearest = &NearestZone{Name: nearest.Zone.Name, NameEn: nearest.Zone.NameEn, DistanceM: nearest.DistanceM}
	}
	return out, nil
}

// Locate tells a customer which zone a pin falls in before they build a quote.
func (a *App) Locate(ctx context.Context, slug string, lat, lng float64) (*Location, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	return locate(ctx, a.DB, b, LatLng{lat, lng})
}

const maxBaskets = 4

// checkRequired refuses a cart that has nothing from a required category.
// A required category with nothing on sale right now is not enforced.
func checkRequired(ctx context.Context, q Querier, branchID int64, service string, lines []QuoteLine) error {
	rows, err := q.Query(ctx, `SELECT c.id, c.name, c.name_en FROM menu_categories c
		WHERE c.branch_id=$1 AND c.required AND NOT c.archived AND EXISTS (
			SELECT 1 FROM menu_items i WHERE i.category_id = c.id AND NOT i.archived AND i.published AND NOT i.sold_out
				AND (i.stock_qty IS NULL OR i.stock_qty > 0)
				AND CASE $2 WHEN 'delivery' THEN i.delivery_available WHEN 'dine_in' THEN i.dine_in_available ELSE i.pickup_available END)
		ORDER BY c.sort, c.id`, branchID, service)
	if err != nil {
		return err
	}
	defer rows.Close()
	have := map[int64]bool{}
	for _, l := range lines {
		have[l.CategoryID] = true
	}
	type missing struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		NameEn string `json:"name_en"`
	}
	var miss []missing
	for rows.Next() {
		var m missing
		if err := rows.Scan(&m.ID, &m.Name, &m.NameEn); err != nil {
			return err
		}
		if !have[m.ID] {
			miss = append(miss, m)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(miss) > 0 {
		return ErrD(http.StatusUnprocessableEntity, "REQUIRED_MISSING", "choose something from every required category",
			map[string]any{"categories": miss})
	}
	return nil
}

// priceLines validates cart lines against the live menu and prices them. It is
// shared by delivery/pickup quotes and dine-in batches.
func priceLines(ctx context.Context, q Querier, branchID int64, service string, in []QuoteLineInput) ([]QuoteLine, int64, error) {
	if len(in) == 0 {
		return nil, 0, ErrValidation("cart is empty")
	}
	if len(in) > 100 {
		return nil, 0, ErrValidation("too many lines")
	}
	lines := []QuoteLine{}
	items, err := loadItems(ctx, q, branchID, true)
	if err != nil {
		return nil, 0, err
	}
	byID := map[int64]*Item{}
	for _, it := range items {
		byID[it.ID] = it
	}

	var unavailable []int64
	wanted := map[int64]int{}
	var subtotal int64
	for _, line := range in {
		if line.Qty < 1 || line.Qty > 99 {
			return nil, 0, ErrValidation("qty must be between 1 and 99")
		}
		if len(line.Note) > 200 {
			return nil, 0, ErrValidation("note too long")
		}
		if line.Basket < 0 || line.Basket > maxBaskets {
			return nil, 0, ErrValidation("basket must be between 1 and 4")
		}
		it, ok := byID[line.ItemID]
		if !ok || !it.available(service) {
			unavailable = append(unavailable, line.ItemID)
			continue
		}
		wanted[it.ID] += line.Qty
		if it.StockQty != nil && wanted[it.ID] > *it.StockQty {
			unavailable = append(unavailable, line.ItemID)
			continue
		}
		ql := QuoteLine{ItemID: it.ID, Name: it.Name, NameEn: it.NameEn, Qty: line.Qty, Note: line.Note,
			NeedsPrep: it.NeedsPrep, Options: []LineOption{}, Basket: max(1, line.Basket), CategoryID: it.CategoryID}
		unit := it.Price
		chosen := map[int64]int{}
		for _, id := range line.OptionIDs {
			if chosen[id] > 0 && !line.repeat {
				return nil, 0, ErrValidation("duplicate option")
			}
			chosen[id]++
		}
		matched := 0
		optionGone := false
		for _, g := range it.Groups {
			n := 0
			for _, o := range g.Options {
				k := chosen[o.ID]
				if k == 0 {
					continue
				}
				matched++
				n += k
				if o.SoldOut {
					optionGone = true
				}
				unit += o.PriceDelta * int64(k)
				lo := LineOption{OptionID: o.ID, Group: g.Name, GroupEn: g.NameEn, Name: o.Name, NameEn: o.NameEn, PriceDelta: o.PriceDelta}
				if k > 1 {
					lo.Qty, lo.PriceDelta = k, o.PriceDelta*int64(k)
					lo.Name, lo.NameEn = fmt.Sprintf("%s ×%d", o.Name, k), ""
					if o.NameEn != "" {
						lo.NameEn = fmt.Sprintf("%s ×%d", o.NameEn, k)
					}
				}
				ql.Options = append(ql.Options, lo)
			}
			if n < g.MinSelect || n > g.MaxSelect {
				return nil, 0, ErrD(http.StatusUnprocessableEntity, "OPTIONS_INVALID",
					"required options are missing or too many were chosen",
					map[string]any{"item_id": it.ID, "group_id": g.ID})
			}
		}
		if matched != len(chosen) {
			return nil, 0, ErrD(http.StatusUnprocessableEntity, "OPTIONS_INVALID", "option does not belong to this item",
				map[string]any{"item_id": it.ID})
		}
		if optionGone {
			unavailable = append(unavailable, line.ItemID)
			continue
		}
		if ql.NoteTags, err = pickNotes(it, line.NoteTagIDs); err != nil {
			return nil, 0, err
		}
		ql.UnitPrice = unit
		ql.LineTotal = unit * int64(line.Qty)
		subtotal += ql.LineTotal
		lines = append(lines, ql)
	}
	if len(unavailable) > 0 {
		return nil, 0, ErrD(http.StatusConflict, "ITEM_UNAVAILABLE", "some items are no longer available",
			map[string]any{"item_ids": unavailable})
	}
	return lines, subtotal, nil
}

// CreateQuote prices a cart and stores the result for confirmation.
func (a *App) CreateQuote(ctx context.Context, slug string, in QuoteInput, phone string) (*Quote, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	quote, err := computeQuote(ctx, a.DB, a.forCustomer(b), in, phone)
	if err != nil {
		return nil, err
	}
	quote.ExpiresAt = time.Now().Add(quoteTTL).UTC().Truncate(time.Second)
	payload, _ := json.Marshal(quote)
	err = a.DB.QueryRow(ctx, `INSERT INTO quotes (id, branch_id, payload, total, expires_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4) RETURNING id::text`,
		b.ID, payload, quote.Totals.Total, quote.ExpiresAt).Scan(&quote.ID)
	if err != nil {
		return nil, wrap("insert quote", err)
	}
	return quote, nil
}

// sameQuote reports whether a re-priced quote still matches what the customer confirmed.
func sameQuote(old, cur *Quote) bool {
	if old.Totals != cur.Totals || len(old.Lines) != len(cur.Lines) || len(old.Coupons) != len(cur.Coupons) {
		return false
	}
	for i := range old.Coupons {
		if old.Coupons[i] != cur.Coupons[i] {
			return false
		}
	}
	for i := range old.Lines {
		if old.Lines[i].UnitPrice != cur.Lines[i].UnitPrice || old.Lines[i].Qty != cur.Lines[i].Qty ||
			old.Lines[i].ItemID != cur.Lines[i].ItemID || old.Lines[i].Basket != cur.Lines[i].Basket {
			return false
		}
	}
	return true
}

// reserveStock decrements limited stock for the quote's lines in item-id order
// (fixed lock order). A failed conditional update means someone else took the
// last portion.
func reserveStock(ctx context.Context, tx pgx.Tx, lines []QuoteLine) error {
	need := map[int64]int{}
	for _, l := range lines {
		need[l.ItemID] += l.Qty
	}
	ids := make([]int64, 0, len(need))
	for id := range need {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var gone []int64
	for _, id := range ids {
		tag, err := tx.Exec(ctx, `UPDATE menu_items SET stock_qty = stock_qty - $2
			WHERE id=$1 AND (stock_qty IS NULL OR stock_qty >= $2)`, id, need[id])
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			gone = append(gone, id)
		}
	}
	if len(gone) > 0 {
		return ErrD(http.StatusConflict, "ITEM_UNAVAILABLE", "some items just sold out", map[string]any{"item_ids": gone})
	}
	return nil
}

// releaseStock returns limited stock when an order is rejected or cancelled.
func releaseStock(ctx context.Context, tx pgx.Tx, orderID int64) error {
	_, err := tx.Exec(ctx, `UPDATE menu_items m SET stock_qty = m.stock_qty + s.qty
		FROM (SELECT item_id, sum(qty)::int AS qty FROM order_items WHERE order_id=$1 GROUP BY item_id) s
		WHERE m.id = s.item_id AND m.stock_qty IS NOT NULL`, orderID)
	return err
}
