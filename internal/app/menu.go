package app

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// AdminMenu is the back-office view: drafts, stock and inactive zones included.
type AdminMenu struct {
	Branch     *Branch    `json:"branch"`
	Zones      []Zone     `json:"zones"`
	Stations   []Station  `json:"stations"`
	Categories []Category `json:"categories"`
}

func (a *App) AdminMenu(ctx context.Context, branchID int64) (*AdminMenu, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	zones, err := loadZones(ctx, a.DB, branchID, false)
	if err != nil {
		return nil, err
	}
	cats, err := loadMenu(ctx, a.DB, branchID, false)
	if err != nil {
		return nil, err
	}
	stations, err := a.ListStations(ctx, branchID)
	if err != nil {
		return nil, err
	}
	return &AdminMenu{Branch: b, Zones: zones, Stations: stations, Categories: cats}, nil
}

func branchChanged(ctx context.Context, q Querier, branchID int64) error {
	return notify(ctx, q, Event{BranchID: branchID})
}

type CategoryInput struct {
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	Sort   int    `json:"sort"`
}

// SaveCategory creates (id == 0) or updates a category.
func (a *App) SaveCategory(ctx context.Context, branchID, id int64, in CategoryInput) (int64, error) {
	if strings.TrimSpace(in.Name) == "" {
		return 0, ErrValidation("name is required")
	}
	var err error
	if id == 0 {
		err = a.DB.QueryRow(ctx, `INSERT INTO menu_categories (branch_id, name, name_en, sort) VALUES ($1,$2,$3,$4)
			RETURNING id`, branchID, in.Name, in.NameEn, in.Sort).Scan(&id)
	} else {
		err = a.DB.QueryRow(ctx, `UPDATE menu_categories SET name=$3, name_en=$4, sort=$5
			WHERE id=$1 AND branch_id=$2 RETURNING id`, id, branchID, in.Name, in.NameEn, in.Sort).Scan(&id)
	}
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return id, err
}

type OptionInput struct {
	Name       string `json:"name"`
	NameEn     string `json:"name_en"`
	PriceDelta int64  `json:"price_delta"`
	SoldOut    bool   `json:"sold_out"`
}

type OptionGroupInput struct {
	Name      string        `json:"name"`
	NameEn    string        `json:"name_en"`
	MinSelect int           `json:"min_select"`
	MaxSelect int           `json:"max_select"`
	Options   []OptionInput `json:"options"`
}

type ItemInput struct {
	CategoryID        int64              `json:"category_id"`
	Name              string             `json:"name"`
	NameEn            string             `json:"name_en"`
	Description       string             `json:"description"`
	ImageURL          string             `json:"image_url"`
	Price             int64              `json:"price"`
	Published         bool               `json:"published"`
	SoldOut           bool               `json:"sold_out"`
	StockQty          *int               `json:"stock_qty"`
	NeedsPrep         bool               `json:"needs_prep"`
	DeliveryAvailable bool               `json:"delivery_available"`
	PickupAvailable   bool               `json:"pickup_available"`
	Sort              int                `json:"sort"`
	StationID         *int64             `json:"station_id"`
	Groups            []OptionGroupInput `json:"groups"`
}

// SaveItem creates (id == 0) or updates an item and replaces its option
// groups. Editing never touches past orders: they keep their own snapshot.
func (a *App) SaveItem(ctx context.Context, branchID, id int64, in ItemInput) (int64, error) {
	if strings.TrimSpace(in.Name) == "" {
		return 0, ErrValidation("name is required")
	}
	if in.Price < 0 {
		return 0, ErrValidation("price must not be negative")
	}
	if in.StockQty != nil && *in.StockQty < 0 {
		return 0, ErrValidation("stock_qty must not be negative")
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(in.ImageURL)), "javascript:") {
		return 0, ErrValidation("invalid image_url")
	}
	for _, g := range in.Groups {
		if strings.TrimSpace(g.Name) == "" || len(g.Options) == 0 {
			return 0, ErrValidation("every option group needs a name and at least one option")
		}
		if g.MinSelect < 0 || g.MaxSelect < 1 || g.MinSelect > g.MaxSelect || g.MinSelect > len(g.Options) {
			return 0, ErrValidation("option group min/max selection is invalid")
		}
		for _, o := range g.Options {
			if strings.TrimSpace(o.Name) == "" || o.PriceDelta < 0 {
				return 0, ErrValidation("every option needs a name and a non-negative price")
			}
		}
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM menu_categories WHERE id=$1 AND branch_id=$2)`,
			in.CategoryID, branchID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrValidation("category not found")
		}
		if in.StationID != nil {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kitchen_stations WHERE id=$1 AND branch_id=$2 AND active)`,
				*in.StationID, branchID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return ErrValidation("station not found or inactive")
			}
		}
		var err error
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO menu_items (branch_id, category_id, name, name_en, description,
				image_url, price, published, sold_out, stock_qty, needs_prep, delivery_available, pickup_available, sort, station_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`,
				branchID, in.CategoryID, in.Name, in.NameEn, in.Description, in.ImageURL, in.Price, in.Published,
				in.SoldOut, in.StockQty, in.NeedsPrep, in.DeliveryAvailable, in.PickupAvailable, in.Sort, in.StationID).Scan(&id)
		} else {
			err = tx.QueryRow(ctx, `UPDATE menu_items SET category_id=$3, name=$4, name_en=$5, description=$6,
				image_url=$7, price=$8, published=$9, sold_out=$10, stock_qty=$11, needs_prep=$12,
				delivery_available=$13, pickup_available=$14, sort=$15, station_id=$16, updated_at=now()
				WHERE id=$1 AND branch_id=$2 AND NOT archived RETURNING id`,
				id, branchID, in.CategoryID, in.Name, in.NameEn, in.Description, in.ImageURL, in.Price, in.Published,
				in.SoldOut, in.StockQty, in.NeedsPrep, in.DeliveryAvailable, in.PickupAvailable, in.Sort, in.StationID).Scan(&id)
		}
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM option_groups WHERE item_id=$1`, id); err != nil {
			return err
		}
		for gi, g := range in.Groups {
			var gid int64
			if err := tx.QueryRow(ctx, `INSERT INTO option_groups (item_id, name, name_en, min_select, max_select, sort)
				VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, id, g.Name, g.NameEn, g.MinSelect, g.MaxSelect, gi).Scan(&gid); err != nil {
				return err
			}
			for oi, o := range g.Options {
				if _, err := tx.Exec(ctx, `INSERT INTO options (group_id, name, name_en, price_delta, sold_out, sort)
					VALUES ($1,$2,$3,$4,$5,$6)`, gid, o.Name, o.NameEn, o.PriceDelta, o.SoldOut, oi); err != nil {
					return err
				}
			}
		}
		return branchChanged(ctx, tx, branchID)
	})
	return id, err
}

// ItemPatch is the quick toggle used from the menu list.
type ItemPatch struct {
	SoldOut   *bool `json:"sold_out"`
	Published *bool `json:"published"`
	Archived  *bool `json:"archived"`
	// StockSet distinguishes "leave stock alone" from "set to unlimited (null)".
	StockSet bool `json:"stock_set"`
	StockQty *int `json:"stock_qty"`
}

func (a *App) PatchItem(ctx context.Context, branchID, id int64, p ItemPatch) error {
	if p.StockSet && p.StockQty != nil && *p.StockQty < 0 {
		return ErrValidation("stock_qty must not be negative")
	}
	tag, err := a.DB.Exec(ctx, `UPDATE menu_items SET
		sold_out  = coalesce($3::bool, sold_out),
		published = coalesce($4::bool, published),
		archived  = coalesce($5::bool, archived),
		stock_qty = CASE WHEN $6::bool THEN $7::int ELSE stock_qty END,
		updated_at = now()
		WHERE id=$1 AND branch_id=$2`, id, branchID, p.SoldOut, p.Published, p.Archived, p.StockSet, p.StockQty)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return branchChanged(ctx, a.DB, branchID)
}

type ZoneInput struct {
	Name     string `json:"name"`
	NameEn   string `json:"name_en"`
	Fee      int64  `json:"fee"`
	MinOrder int64  `json:"min_order"`
	Active   bool   `json:"active"`
	Sort     int    `json:"sort"`
}

func (a *App) SaveZone(ctx context.Context, branchID, id int64, in ZoneInput) (int64, error) {
	if strings.TrimSpace(in.Name) == "" || in.Fee < 0 || in.MinOrder < 0 {
		return 0, ErrValidation("zone needs a name and non-negative amounts")
	}
	var err error
	if id == 0 {
		err = a.DB.QueryRow(ctx, `INSERT INTO delivery_zones (branch_id, name, name_en, fee, min_order, active, sort)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, branchID, in.Name, in.NameEn, in.Fee, in.MinOrder,
			in.Active, in.Sort).Scan(&id)
	} else {
		err = a.DB.QueryRow(ctx, `UPDATE delivery_zones SET name=$3, name_en=$4, fee=$5, min_order=$6, active=$7, sort=$8
			WHERE id=$1 AND branch_id=$2 RETURNING id`, id, branchID, in.Name, in.NameEn, in.Fee, in.MinOrder,
			in.Active, in.Sort).Scan(&id)
	}
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return id, err
}

type SettingsInput struct {
	Name                 string `json:"name"`
	NameEn               string `json:"name_en"`
	Address              string `json:"address"`
	Phone                string `json:"phone"`
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
}

var hhmmRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (a *App) SaveSettings(ctx context.Context, branchID int64, in SettingsInput) (*Branch, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrValidation("name is required")
	}
	if in.TaxRateBP < 0 || in.TaxRateBP > 5000 || in.ServiceChargeBP < 0 || in.ServiceChargeBP > 5000 {
		return nil, ErrValidation("tax and service charge must be between 0% and 50%")
	}
	if in.PackagingFee < 0 || in.MinOrderAmount < 0 {
		return nil, ErrValidation("amounts must not be negative")
	}
	if in.AcceptTimeoutMinutes < 1 || in.AcceptTimeoutMinutes > 120 {
		return nil, ErrValidation("accept timeout must be 1 to 120 minutes")
	}
	if !hhmmRe.MatchString(in.OpenTime) || !hhmmRe.MatchString(in.CloseTime) {
		return nil, ErrValidation("opening hours must be HH:MM")
	}
	tag, err := a.DB.Exec(ctx, `UPDATE branches SET name=$2, name_en=$3, address=$4, phone=$5, tax_rate_bp=$6,
		tax_inclusive=$7, service_charge_bp=$8, packaging_fee=$9, min_order_amount=$10, accept_timeout_minutes=$11,
		delivery_enabled=$12, pickup_enabled=$13, paused=$14, open_time=$15, close_time=$16, notify_sms=$17 WHERE id=$1`,
		branchID, in.Name, in.NameEn, in.Address, in.Phone, in.TaxRateBP, in.TaxInclusive, in.ServiceChargeBP,
		in.PackagingFee, in.MinOrderAmount, in.AcceptTimeoutMinutes, in.DeliveryEnabled, in.PickupEnabled,
		in.Paused, in.OpenTime, in.CloseTime, in.NotifySMS)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return a.BranchByID(ctx, branchID)
}

// ---- background jobs (run by cmd/worker) ----

// ExpirePendingOrders cancels orders the store did not accept in time and
// returns how many were cancelled. SKIP LOCKED keeps it from waiting behind a
// clerk who is accepting the same order right now.
func (a *App) ExpirePendingOrders(ctx context.Context) (int, error) {
	n := 0
	for {
		done := false
		err := a.inTx(ctx, func(tx pgx.Tx) error {
			var id int64
			err := tx.QueryRow(ctx, `SELECT id FROM orders WHERE acceptance_status='pending' AND accept_deadline < now()
				ORDER BY accept_deadline LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id)
			if isNoRows(err) {
				done = true
				return nil
			} else if err != nil {
				return err
			}
			o, err := lockOrder(ctx, tx, id)
			if err != nil {
				return err
			}
			if o.Acceptance != "pending" {
				return nil
			}
			n++
			return a.closeOrder(ctx, tx, o, "cancelled", "accept_timeout", Actor{Type: "system"})
		})
		if err != nil {
			return n, err
		}
		if done {
			return n, nil
		}
	}
}

// Cleanup removes expired sessions, verification codes, quotes and old
// idempotency records.
func (a *App) Cleanup(ctx context.Context) error {
	for _, q := range []string{
		`DELETE FROM sessions WHERE expires_at < now()`,
		`DELETE FROM customer_sessions WHERE expires_at < now()`,
		`DELETE FROM phone_verifications WHERE created_at < now() - interval '1 day'`,
		`DELETE FROM idempotency_records WHERE created_at < now() - interval '48 hours'`,
		`DELETE FROM quotes WHERE used_order_id IS NULL AND expires_at < now() - interval '1 hour'`,
		`DELETE FROM jobs WHERE status <> 'pending' AND finished_at < now() - interval '14 days'`,
	} {
		if _, err := a.DB.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// RunWorker loops until ctx is cancelled.
func (a *App) RunWorker(ctx context.Context, logf func(string, ...any)) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	lastCleanup := time.Time{}
	for {
		if n, err := a.ExpirePendingOrders(ctx); err != nil {
			logf("worker: expire pending orders: %v", err)
		} else if n > 0 {
			logf("worker: cancelled %d orders that were not accepted in time", n)
		}
		if err := a.RunJobs(ctx); err != nil {
			logf("worker: jobs: %v", err)
		}
		if time.Since(lastCleanup) > 10*time.Minute {
			if err := a.Cleanup(ctx); err != nil {
				logf("worker: cleanup: %v", err)
			}
			lastCleanup = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
