package app

import (
	"context"
	"encoding/json"
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
}

type QuoteInput struct {
	ServiceType string           `json:"service_type"`
	ZoneID      *int64           `json:"zone_id"`
	Items       []QuoteLineInput `json:"items"`
}

type LineOption struct {
	Group      string `json:"group"`
	GroupEn    string `json:"group_en"`
	Name       string `json:"name"`
	NameEn     string `json:"name_en"`
	PriceDelta int64  `json:"price_delta"`
}

type QuoteLine struct {
	ItemID    int64        `json:"item_id"`
	Name      string       `json:"name"`
	NameEn    string       `json:"name_en"`
	UnitPrice int64        `json:"unit_price"`
	Qty       int          `json:"qty"`
	LineTotal int64        `json:"line_total"`
	Options   []LineOption `json:"options"`
	Note      string       `json:"note"`
	NeedsPrep bool         `json:"needs_prep"`
}

type Quote struct {
	ID          string      `json:"id"`
	BranchID    int64       `json:"branch_id"`
	Currency    string      `json:"currency"`
	ServiceType string      `json:"service_type"`
	ZoneID      *int64      `json:"zone_id"`
	ZoneName    string      `json:"zone_name"`
	Lines       []QuoteLine `json:"lines"`
	Totals      Totals      `json:"totals"`
	ExpiresAt   time.Time   `json:"expires_at"`
	Input       QuoteInput  `json:"input"`
}

// computeQuote validates the cart against the live menu and prices it. It is
// used both when quoting and again, inside the order transaction, to detect
// changes since the quote was issued.
func computeQuote(ctx context.Context, q Querier, b *Branch, in QuoteInput) (*Quote, error) {
	if b.Paused {
		return nil, Err(http.StatusConflict, "STORE_CLOSED", "the store is not taking orders right now")
	}
	if !b.isOpen(time.Now()) {
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
	if len(in.Items) == 0 {
		return nil, ErrValidation("cart is empty")
	}
	if len(in.Items) > 100 {
		return nil, ErrValidation("too many lines")
	}

	quote := &Quote{BranchID: b.ID, Currency: b.Currency, ServiceType: in.ServiceType, Input: in, Lines: []QuoteLine{}}
	var deliveryFee, minOrder int64
	if in.ServiceType == "delivery" {
		if in.ZoneID == nil {
			return nil, Err(http.StatusUnprocessableEntity, "OUT_OF_ZONE", "choose a delivery area")
		}
		var z Zone
		err := q.QueryRow(ctx, `SELECT id, name, name_en, fee, min_order FROM delivery_zones
			WHERE id=$1 AND branch_id=$2 AND active`, *in.ZoneID, b.ID).Scan(&z.ID, &z.Name, &z.NameEn, &z.Fee, &z.MinOrder)
		if isNoRows(err) {
			return nil, Err(http.StatusUnprocessableEntity, "OUT_OF_ZONE", "this area is not served")
		} else if err != nil {
			return nil, err
		}
		quote.ZoneID, quote.ZoneName = &z.ID, z.Name
		deliveryFee = z.Fee
		minOrder = max(b.MinOrderAmount, z.MinOrder)
	}

	items, err := loadItems(ctx, q, b.ID, true)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*Item{}
	for _, it := range items {
		byID[it.ID] = it
	}

	var unavailable []int64
	wanted := map[int64]int{}
	var subtotal int64
	for _, line := range in.Items {
		if line.Qty < 1 || line.Qty > 99 {
			return nil, ErrValidation("qty must be between 1 and 99")
		}
		if len(line.Note) > 200 {
			return nil, ErrValidation("note too long")
		}
		it, ok := byID[line.ItemID]
		if !ok || !it.available(in.ServiceType) {
			unavailable = append(unavailable, line.ItemID)
			continue
		}
		wanted[it.ID] += line.Qty
		if it.StockQty != nil && wanted[it.ID] > *it.StockQty {
			unavailable = append(unavailable, line.ItemID)
			continue
		}
		ql := QuoteLine{ItemID: it.ID, Name: it.Name, NameEn: it.NameEn, Qty: line.Qty, Note: line.Note,
			NeedsPrep: it.NeedsPrep, Options: []LineOption{}}
		unit := it.Price
		chosen := map[int64]bool{}
		for _, id := range line.OptionIDs {
			if chosen[id] {
				return nil, ErrValidation("duplicate option")
			}
			chosen[id] = true
		}
		matched := 0
		optionGone := false
		for _, g := range it.Groups {
			n := 0
			for _, o := range g.Options {
				if !chosen[o.ID] {
					continue
				}
				matched++
				n++
				if o.SoldOut {
					optionGone = true
				}
				unit += o.PriceDelta
				ql.Options = append(ql.Options, LineOption{Group: g.Name, GroupEn: g.NameEn, Name: o.Name,
					NameEn: o.NameEn, PriceDelta: o.PriceDelta})
			}
			if n < g.MinSelect || n > g.MaxSelect {
				return nil, ErrD(http.StatusUnprocessableEntity, "OPTIONS_INVALID",
					"required options are missing or too many were chosen",
					map[string]any{"item_id": it.ID, "group_id": g.ID})
			}
		}
		if matched != len(chosen) {
			return nil, ErrD(http.StatusUnprocessableEntity, "OPTIONS_INVALID", "option does not belong to this item",
				map[string]any{"item_id": it.ID})
		}
		if optionGone {
			unavailable = append(unavailable, line.ItemID)
			continue
		}
		ql.UnitPrice = unit
		ql.LineTotal = unit * int64(line.Qty)
		subtotal += ql.LineTotal
		quote.Lines = append(quote.Lines, ql)
	}
	if len(unavailable) > 0 {
		return nil, ErrD(http.StatusConflict, "ITEM_UNAVAILABLE", "some items are no longer available",
			map[string]any{"item_ids": unavailable})
	}
	if subtotal < minOrder {
		return nil, ErrD(http.StatusUnprocessableEntity, "BELOW_MINIMUM", "order is below the minimum amount",
			map[string]any{"minimum": minOrder, "subtotal": subtotal})
	}
	quote.Totals = ComputeTotals(subtotal, deliveryFee, b.taxConfig())
	return quote, nil
}

// CreateQuote prices a cart and stores the result for confirmation.
func (a *App) CreateQuote(ctx context.Context, slug string, in QuoteInput) (*Quote, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	quote, err := computeQuote(ctx, a.DB, b, in)
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
	if old.Totals != cur.Totals || len(old.Lines) != len(cur.Lines) {
		return false
	}
	for i := range old.Lines {
		if old.Lines[i].UnitPrice != cur.Lines[i].UnitPrice || old.Lines[i].Qty != cur.Lines[i].Qty ||
			old.Lines[i].ItemID != cur.Lines[i].ItemID {
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
