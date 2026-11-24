package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strings"

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
	// Required is left as it is when nil.
	Required *bool `json:"required"`
}

// SaveCategory creates (id == 0) or updates a category.
func (a *App) SaveCategory(ctx context.Context, branchID, id int64, in CategoryInput) (int64, error) {
	if strings.TrimSpace(in.Name) == "" {
		return 0, ErrValidation("name is required")
	}
	var err error
	if id == 0 {
		err = a.DB.QueryRow(ctx, `INSERT INTO menu_categories (branch_id, name, name_en, sort, required)
			VALUES ($1,$2,$3,$4,coalesce($5, false)) RETURNING id`, branchID, in.Name, in.NameEn, in.Sort, in.Required).Scan(&id)
	} else {
		err = a.DB.QueryRow(ctx, `UPDATE menu_categories SET name=$3, name_en=$4, sort=$5, required=coalesce($6, required)
			WHERE id=$1 AND branch_id=$2 AND NOT archived RETURNING id`, id, branchID, in.Name, in.NameEn, in.Sort, in.Required).Scan(&id)
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
	CategoryID  int64  `json:"category_id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	NameEn      string `json:"name_en"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
	// Photos are more pictures for the dish page, after the main one.
	Photos []string `json:"photos"`
	// Details is the longer text on the dish page.
	Details           string `json:"details"`
	Price             int64  `json:"price"`
	Published         bool   `json:"published"`
	SoldOut           bool   `json:"sold_out"`
	StockQty          *int   `json:"stock_qty"`
	NeedsPrep         bool   `json:"needs_prep"`
	DeliveryAvailable bool   `json:"delivery_available"`
	PickupAvailable   bool   `json:"pickup_available"`
	// DineInAvailable left out keeps the dish as it is (offered, for a new one).
	DineInAvailable *bool              `json:"dine_in_available"`
	Sort            int                `json:"sort"`
	StationID       *int64             `json:"station_id"`
	Groups          []OptionGroupInput `json:"groups"`
	// NoteTemplateIDs are the flavour note templates to show; nil keeps them.
	NoteTemplateIDs *[]int64 `json:"note_template_ids"`
}

// The dish page holds the main picture and up to this many more.
const (
	maxItemPhotos  = 8
	maxItemDetails = 2000
)

// SaveItem creates (id == 0) or updates an item and replaces its option
// groups. Editing never touches past orders: they keep their own snapshot.
func (a *App) SaveItem(ctx context.Context, branchID, id int64, in ItemInput) (int64, error) {
	if strings.TrimSpace(in.Name) == "" {
		return 0, ErrValidation("name is required")
	}
	if in.Price < 0 {
		return 0, ErrValidation("price must not be negative")
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	if len([]rune(in.Code)) > 12 || strings.ContainsAny(in.Code, " \t") {
		return 0, ErrValidation("the code must be at most 12 characters without spaces")
	}
	if in.StockQty != nil && *in.StockQty < 0 {
		return 0, ErrValidation("stock_qty must not be negative")
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(in.ImageURL)), "javascript:") {
		return 0, ErrValidation("invalid image_url")
	}
	photos := []string{}
	for _, p := range in.Photos {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(p), "javascript:") || len(p) > 1000 {
			return 0, ErrValidation("invalid photo")
		}
		photos = append(photos, p)
	}
	if len(photos) > maxItemPhotos {
		return 0, ErrValidation(fmt.Sprintf("at most %d more photos", maxItemPhotos))
	}
	if len([]rune(in.Details)) > maxItemDetails {
		return 0, ErrValidation(fmt.Sprintf("the details must be at most %d characters", maxItemDetails))
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
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM menu_categories WHERE id=$1 AND branch_id=$2 AND NOT archived)`,
			in.CategoryID, branchID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrValidation("category not found")
		}
		if in.Code != "" {
			var taken string
			err := tx.QueryRow(ctx, `SELECT name FROM menu_items WHERE branch_id=$1 AND upper(code)=$2 AND id<>$3 AND NOT archived LIMIT 1`,
				branchID, in.Code, id).Scan(&taken)
			if err == nil {
				return ErrD(http.StatusConflict, "CODE_TAKEN", "another dish already has this code", map[string]any{"name": taken})
			} else if !isNoRows(err) {
				return err
			}
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
				image_url, price, published, sold_out, stock_qty, needs_prep, delivery_available, pickup_available, sort, station_id, photos, details, dine_in_available, code)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,COALESCE($18, true),$19) RETURNING id`,
				branchID, in.CategoryID, in.Name, in.NameEn, in.Description, in.ImageURL, in.Price, in.Published,
				in.SoldOut, in.StockQty, in.NeedsPrep, in.DeliveryAvailable, in.PickupAvailable, in.Sort, in.StationID, photos, in.Details,
				in.DineInAvailable, in.Code).Scan(&id)
		} else {
			err = tx.QueryRow(ctx, `UPDATE menu_items SET category_id=$3, name=$4, name_en=$5, description=$6,
				image_url=$7, price=$8, published=$9, sold_out=$10, stock_qty=$11, needs_prep=$12,
				delivery_available=$13, pickup_available=$14, sort=$15, station_id=$16, photos=$17, details=$18,
				dine_in_available=COALESCE($19, dine_in_available), code=$20, updated_at=now()
				WHERE id=$1 AND branch_id=$2 AND NOT archived RETURNING id`,
				id, branchID, in.CategoryID, in.Name, in.NameEn, in.Description, in.ImageURL, in.Price, in.Published,
				in.SoldOut, in.StockQty, in.NeedsPrep, in.DeliveryAvailable, in.PickupAvailable, in.Sort, in.StationID, photos, in.Details,
				in.DineInAvailable, in.Code).Scan(&id)
		}
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM option_groups WHERE item_id=$1`, id); err != nil {
			return err
		}
		if in.NoteTemplateIDs != nil {
			if err := setItemNoteTemplates(ctx, tx, branchID, id, *in.NoteTemplateIDs); err != nil {
				return err
			}
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
	// Polygon nil keeps a zone without an outline (list mode only).
	Polygon    Polygon `json:"polygon"`
	Color      string  `json:"color"`
	EtaMinutes *int    `json:"eta_minutes"`
}

var zoneColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// SaveZone creates (id 0) or replaces a delivery zone. A new zone goes to the
// end of the priority order unless Sort says otherwise; ReorderZones moves it.
func (a *App) SaveZone(ctx context.Context, branchID, id int64, in ZoneInput) (int64, error) {
	return saveZone(ctx, a.DB, branchID, id, in)
}

// ZoneChange is one zone in a ZoneBatch: ID 0 creates a zone.
type ZoneChange struct {
	ID int64 `json:"id"`
	ZoneInput
}

// ZoneBatch changes several zones as one. Cutting an overlap out of a zone
// reshapes two or more zones at once (and may split one in two); they must
// all be saved or none, or the map would be left with a hole or an overlap.
type ZoneBatch struct {
	Zones  []ZoneChange `json:"zones"`
	Delete []int64      `json:"delete"`
}

// SaveZones applies a batch in one transaction and returns the zones' ids in
// the order they were given.
func (a *App) SaveZones(ctx context.Context, branchID int64, in ZoneBatch) ([]int64, error) {
	if len(in.Zones)+len(in.Delete) == 0 || len(in.Zones)+len(in.Delete) > 50 {
		return nil, ErrValidation("a batch changes 1 to 50 zones")
	}
	ids := make([]int64, 0, len(in.Zones))
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		for _, z := range in.Zones {
			id, err := saveZone(ctx, tx, branchID, z.ID, z.ZoneInput)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		for _, id := range in.Delete {
			tag, err := tx.Exec(ctx, `DELETE FROM delivery_zones WHERE id=$1 AND branch_id=$2`, id, branchID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func saveZone(ctx context.Context, q Querier, branchID, id int64, in ZoneInput) (int64, error) {
	if strings.TrimSpace(in.Name) == "" || in.Fee < 0 || in.MinOrder < 0 {
		return 0, ErrValidation("zone needs a name and non-negative amounts")
	}
	if in.Color != "" && !zoneColorRe.MatchString(in.Color) {
		return 0, ErrValidation("color must look like #2a78d6")
	}
	if in.EtaMinutes != nil && (*in.EtaMinutes < 1 || *in.EtaMinutes > 600) {
		return 0, ErrValidation("estimated minutes must be 1 to 600")
	}
	var poly any // SQL NULL when the zone has no outline
	if len(in.Polygon) > 0 {
		b, err := scanBranch(q.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, branchID))
		if err != nil {
			return 0, err
		}
		if err := checkPolygon(in.Polygon, b.latLng()); err != nil {
			return 0, err
		}
		raw, _ := json.Marshal(in.Polygon)
		poly = string(raw)
	}
	var err error
	if id == 0 {
		err = q.QueryRow(ctx, `INSERT INTO delivery_zones (branch_id, name, name_en, fee, min_order, active, sort,
			polygon, color, eta_minutes) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10) RETURNING id`,
			branchID, in.Name, in.NameEn, in.Fee, in.MinOrder, in.Active, in.Sort, poly, in.Color,
			in.EtaMinutes).Scan(&id)
	} else {
		err = q.QueryRow(ctx, `UPDATE delivery_zones SET name=$3, name_en=$4, fee=$5, min_order=$6, active=$7, sort=$8,
			polygon=$9::jsonb, color=$10, eta_minutes=$11 WHERE id=$1 AND branch_id=$2 RETURNING id`,
			id, branchID, in.Name, in.NameEn, in.Fee, in.MinOrder, in.Active, in.Sort, poly, in.Color,
			in.EtaMinutes).Scan(&id)
	}
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return id, err
}

// DeleteZone removes a zone. Orders keep the zone's name and fee as they were
// when placed, so history is unaffected.
func (a *App) DeleteZone(ctx context.Context, branchID, id int64) error {
	tag, err := a.DB.Exec(ctx, `DELETE FROM delivery_zones WHERE id=$1 AND branch_id=$2`, id, branchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderZones sets the priority order: where outlines overlap, the earlier
// zone wins. ids must be exactly the branch's zones.
func (a *App) ReorderZones(ctx context.Context, branchID int64, ids []int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_zones WHERE branch_id=$1`, branchID).Scan(&n); err != nil {
			return err
		}
		seen := map[int64]bool{}
		for _, id := range ids {
			seen[id] = true
		}
		if len(ids) != n || len(seen) != n {
			return ErrValidation("ids must list every zone of the store exactly once")
		}
		for i, id := range ids {
			tag, err := tx.Exec(ctx, `UPDATE delivery_zones SET sort=$3 WHERE id=$1 AND branch_id=$2`, id, branchID, i)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrValidation("ids must list every zone of the store exactly once")
			}
		}
		return nil
	})
}

// Region is where a point is, in words, as the map's geocoder tells it.
type Region struct {
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	Province    string `json:"province"`
	City        string `json:"city"`
	District    string `json:"district"`
	PostalCode  string `json:"postal_code"`
}

type LocationInput struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Address string  `json:"address"`
	// Region is what the merchant's map made of the point. Without it the
	// stored one is kept while the store stays put, and dropped when it moves.
	Region *Region `json:"region"`
}

// regionStaysM: a location saved without a region this close to the old one
// keeps the old one's region.
const regionStaysM = 50

func (r *Region) clean() error {
	r.CountryCode = strings.ToUpper(strings.TrimSpace(r.CountryCode))
	if r.CountryCode != "" && (len(r.CountryCode) != 2 || strings.Trim(r.CountryCode, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "") {
		return ErrValidation("country_code must be two letters")
	}
	for _, f := range []*string{&r.Country, &r.Province, &r.City, &r.District, &r.PostalCode} {
		*f = strings.TrimSpace(*f)
		if len(*f) > 120 {
			return ErrValidation("region name is too long")
		}
	}
	return nil
}

// SaveLocation sets where the store is. Zone outlines do not move with it.
func (a *App) SaveLocation(ctx context.Context, branchID int64, in LocationInput) (*Branch, error) {
	if !validLatLng(LatLng{in.Lat, in.Lng}) {
		return nil, ErrValidation("invalid coordinates")
	}
	in.Address = strings.TrimSpace(in.Address)
	if len(in.Address) > 300 {
		return nil, ErrValidation("address is too long")
	}
	old, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	reg := Region{}
	if in.Region != nil {
		reg = *in.Region
		if err := reg.clean(); err != nil {
			return nil, err
		}
	} else if at := old.latLng(); at != nil {
		if p := newPlanar(*at).pt(LatLng{in.Lat, in.Lng}); math.Hypot(p.x, p.y) <= regionStaysM {
			reg = Region{old.CountryCode, old.Country, old.Province, old.City, old.District, old.PostalCode}
		}
	}
	tag, err := a.DB.Exec(ctx, `UPDATE branches SET lat=$2, lng=$3, address=CASE WHEN $4 = '' THEN address ELSE $4 END,
		country_code=$5, country=$6, province=$7, city=$8, district=$9, postal_code=$10,
		currency=CASE WHEN $11 = '' THEN currency ELSE $11 END
		WHERE id=$1`, branchID, in.Lat, in.Lng, in.Address,
		reg.CountryCode, reg.Country, reg.Province, reg.City, reg.District, reg.PostalCode, CurrencyOf(reg.CountryCode))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	// The currency may have followed the country: open pages reload.
	if err := branchChanged(ctx, a.DB, branchID); err != nil {
		return nil, err
	}
	return a.BranchByID(ctx, branchID)
}

type ZoneModeInput struct {
	ZoneMode       string `json:"zone_mode"`
	ZoneToleranceM int    `json:"zone_tolerance_m"`
}

// SaveZoneMode sets the tolerance outside the outlines. The area of a
// delivery always follows the address's pin now; "list" (the customer picks
// an area by name) can no longer be chosen. It is only what a platform
// without a map key falls back to (forCustomer).
func (a *App) SaveZoneMode(ctx context.Context, branchID int64, in ZoneModeInput) (*Branch, error) {
	if in.ZoneMode != "" && in.ZoneMode != "map" {
		return nil, ErrValidation("zone_mode can only be map")
	}
	in.ZoneMode = "map"
	if in.ZoneToleranceM < 0 || in.ZoneToleranceM > 500 {
		return nil, ErrValidation("tolerance must be 0 to 500 metres")
	}
	tag, err := a.DB.Exec(ctx, `UPDATE branches SET zone_mode=$2, zone_tolerance_m=$3 WHERE id=$1`,
		branchID, in.ZoneMode, in.ZoneToleranceM)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return a.BranchByID(ctx, branchID)
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
	SchedulingEnabled    bool   `json:"scheduling_enabled"`
	PrepMinutes          int    `json:"prep_minutes"` // 0 = keep the default of 30
	// 0 keeps what is set for these three.
	KitchenSlowMinutes int `json:"kitchen_slow_minutes"`
	DetourPercent      int `json:"detour_percent"`
	DetourMeters       int `json:"detour_meters"`
	// The store page's notice, introduction and baskets; nil keeps what is set.
	Notice         *string `json:"notice"`
	Intro          *string `json:"intro"`
	BasketsEnabled *bool   `json:"baskets_enabled"`
	// Which of ContactFields the address form asks for; nil keeps what is set.
	ContactFields *[]string `json:"contact_fields"`
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
	if in.PrepMinutes == 0 {
		in.PrepMinutes = 30
	}
	if in.PrepMinutes < 5 || in.PrepMinutes > 240 {
		return nil, ErrValidation("preparation time must be 5 to 240 minutes")
	}
	if in.KitchenSlowMinutes != 0 && (in.KitchenSlowMinutes < 1 || in.KitchenSlowMinutes > 120) {
		return nil, ErrValidation("a slow dish must be 1 to 120 minutes")
	}
	if in.DetourPercent != 0 && (in.DetourPercent < 5 || in.DetourPercent > 300) {
		return nil, ErrValidation("the detour share must be 5% to 300%")
	}
	if in.DetourMeters != 0 && (in.DetourMeters < 100 || in.DetourMeters > 20000) {
		return nil, ErrValidation("the detour distance must be 0.1 to 20 km")
	}
	for _, t := range []*string{in.Notice, in.Intro} {
		if t != nil {
			*t = strings.TrimSpace(*t)
		}
	}
	if in.Notice != nil && len([]rune(*in.Notice)) > 300 {
		return nil, ErrValidation("the notice must be at most 300 characters")
	}
	if in.Intro != nil && len([]rune(*in.Intro)) > 500 {
		return nil, ErrValidation("the introduction must be at most 500 characters")
	}
	var contactFields any
	if in.ContactFields != nil {
		asked := []string{}
		for _, f := range ContactFields {
			if slices.Contains(*in.ContactFields, f) {
				asked = append(asked, f)
			}
		}
		for _, f := range *in.ContactFields {
			if !slices.Contains(ContactFields, f) {
				return nil, ErrValidation("contact_fields can hold wechat, telegram, line and whatsapp")
			}
		}
		contactFields = asked
	}
	tag, err := a.DB.Exec(ctx, `UPDATE branches SET name=$2, name_en=$3, address=$4, phone=$5, tax_rate_bp=$6,
		tax_inclusive=$7, service_charge_bp=$8, packaging_fee=$9, min_order_amount=$10, accept_timeout_minutes=$11,
		delivery_enabled=$12, pickup_enabled=$13, paused=$14, open_time=$15, close_time=$16, notify_sms=$17,
		scheduling_enabled=$18, prep_minutes=$19, kitchen_slow_minutes=coalesce(nullif($20, 0), kitchen_slow_minutes),
		detour_percent=coalesce(nullif($21, 0), detour_percent), detour_meters=coalesce(nullif($22, 0), detour_meters),
		notice=coalesce($23, notice), intro=coalesce($24, intro), baskets_enabled=coalesce($25, baskets_enabled),
		contact_fields=coalesce($26::text[], contact_fields) WHERE id=$1`,
		branchID, in.Name, in.NameEn, in.Address, in.Phone, in.TaxRateBP, in.TaxInclusive, in.ServiceChargeBP,
		in.PackagingFee, in.MinOrderAmount, in.AcceptTimeoutMinutes, in.DeliveryEnabled, in.PickupEnabled,
		in.Paused, in.OpenTime, in.CloseTime, in.NotifySMS, in.SchedulingEnabled, in.PrepMinutes,
		in.KitchenSlowMinutes, in.DetourPercent, in.DetourMeters, in.Notice, in.Intro, in.BasketsEnabled, contactFields)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return a.BranchByID(ctx, branchID)
}

// ---- background jobs (run by cmd/worker) ----

// ReleaseScheduled tells kitchen screens that a scheduled order has reached
// its preparation time. The kitchen board itself filters by time, so a missed
// run only delays the push, never the order.
func (a *App) ReleaseScheduled(ctx context.Context) (int, error) {
	n := 0
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE orders SET release_notified = true
			WHERE release_at <= now() AND NOT release_notified AND acceptance_status='accepted'
			RETURNING id, branch_id`)
		if err != nil {
			return err
		}
		var evs []Event
		for rows.Next() {
			var e Event
			if err := rows.Scan(&e.OrderID, &e.BranchID); err != nil {
				rows.Close()
				return err
			}
			evs = append(evs, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, e := range evs {
			if err := notify(ctx, tx, e); err != nil {
				return err
			}
		}
		n = len(evs)
		return nil
	})
	return n, err
}

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
	for _, q := range append([]string{
		`DELETE FROM sessions WHERE expires_at < now()`,
		`DELETE FROM customer_sessions WHERE expires_at < now()`,
		`DELETE FROM phone_verifications WHERE created_at < now() - interval '1 day'`,
		`DELETE FROM idempotency_records WHERE created_at < now() - interval '48 hours'`,
		`DELETE FROM quotes WHERE used_order_id IS NULL AND expires_at < now() - interval '1 hour'`,
		`DELETE FROM jobs WHERE status <> 'pending' AND finished_at < now() - interval '14 days'`,
		`DELETE FROM worker_instances WHERE last_beat_at < now() - interval '1 day'`,
		`DELETE FROM api_instances WHERE last_seen_at < now() - interval '1 day'`,
		`DELETE FROM api_metrics WHERE bucket < now() - interval '7 days'`,
		`DELETE FROM api_errors WHERE at < now() - interval '7 days'`,
		`DELETE FROM rider_tracks WHERE at < now() - interval '7 days'`,
		`DELETE FROM delivery_proofs WHERE created_at < now() - interval '30 days'`,
		// door photos of addresses the customer deleted a month ago
		`DELETE FROM customer_address_photos p USING customer_addresses ad
			WHERE ad.id = p.address_id AND ad.deleted_at < now() - interval '30 days'`,
		purgeUnusedImages,
	}, append(chatCleanup, supportCleanup...)...) {
		if _, err := a.DB.Exec(ctx, q); err != nil {
			return err
		}
	}
	if err := a.forgetOldTripPaths(ctx); err != nil {
		return err
	}
	if err := recycleMembers(ctx, a.DB, 0); err != nil {
		return err
	}
	if _, err := a.RemindExpiringCoupons(ctx); err != nil {
		return err
	}
	if _, err := a.MakeSettlements(ctx); err != nil {
		return err
	}
	return a.endLongShifts(ctx)
}
