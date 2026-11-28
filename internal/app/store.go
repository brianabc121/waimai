package app

import (
	"context"
	"encoding/json"
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
	// AddonConfirmMinutes is how long the store has to confirm a customer's topping.
	AddonConfirmMinutes int `json:"addon_confirm_minutes"`
	// CookTargetMinutes is how long an order should take in the kitchen.
	CookTargetMinutes int `json:"cook_target_minutes"`
	// What the work summaries count as a slow dish and as a detour.
	KitchenSlowMinutes int  `json:"kitchen_slow_minutes"`
	DetourPercent      int  `json:"detour_percent"`
	DetourMeters       int  `json:"detour_meters"`
	OpenNow            bool `json:"open_now"`
	// Lat/Lng is where the store is, set on the delivery map page.
	Lat *float64 `json:"lat"`
	Lng *float64 `json:"lng"`
	// Where that is in words, read off the map when the location is saved.
	// CountryCode is ISO 3166-1 alpha-2; all are empty until then.
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	Province    string `json:"province"`
	City        string `json:"city"`
	District    string `json:"district"`
	PostalCode  string `json:"postal_code"`
	// ZoneMode is "map": the delivery address's pin decides the zone
	// (matchZone). "list" (the customer picks a zone by name) is what
	// customers get only where the platform has no map key.
	ZoneMode       string `json:"zone_mode"`
	ZoneToleranceM int    `json:"zone_tolerance_m"`
	// Notice scrolls under the store's name; Intro is "about the store".
	Notice string `json:"notice"`
	Intro  string `json:"intro"`
	// BasketsEnabled lets a customer split one order into baskets.
	BasketsEnabled bool `json:"baskets_enabled"`
	// ContactFields are the other ways to reach a customer that the address
	// form asks for here (see ContactFields); none unless the store set them.
	ContactFields []string `json:"contact_fields"`
	// Suspended is set by the platform on the whole merchant. Customers cannot
	// order; staff can still sign in and finish the orders already in progress.
	Suspended bool `json:"suspended"`
	// MenuLang is the language the merchant writes the menu in; customers
	// reading one of MenuLangsOff get the English menu (docs/I18N_DESIGN.md §6).
	MenuLang     string   `json:"menu_lang"`
	MenuLangsOff []string `json:"menu_langs_off"`
	// The name, notice and introduction in the customer's language, when they
	// differ (customer pages only).
	NameTr   string `json:"name_tr,omitempty"`
	NoticeTr string `json:"notice_tr,omitempty"`
	IntroTr  string `json:"intro_tr,omitempty"`
	// StoreCode starts staff order numbers (YPX000123); DayResetTime is when
	// DineIn- and Online- numbers start from 1 again (docs/ORDER_TYPES_DESIGN.md §4).
	StoreCode string `json:"store_code"`
	// StoreNo names the store's own page: <store_no>.<site domain>.
	StoreNo      int    `json:"store_no"`
	DayResetTime string `json:"day_reset_time"`
}

var errStoreClosed = Err(http.StatusConflict, "STORE_CLOSED", "the store is not taking orders right now")

// takesOrders is false while the store paused itself or the platform suspended the merchant.
func (b *Branch) takesOrders() bool { return !b.Paused && !b.Suspended }

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
	if !b.SchedulingEnabled || !b.takesOrders() {
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
	delivery_enabled, pickup_enabled, paused, open_time, close_time, notify_sms, scheduling_enabled, prep_minutes,
	lat, lng, zone_mode, zone_tolerance_m, kitchen_slow_minutes, detour_percent, detour_meters,
	notice, intro, baskets_enabled, country_code, country, province, city, district, postal_code, contact_fields,
	menu_lang, menu_langs_off, addon_confirm_minutes, cook_target_minutes, (SELECT m.status = 'suspended' FROM merchants m WHERE m.id = branches.merchant_id),
	store_code, day_reset_time, store_no`

func scanBranch(row interface{ Scan(...any) error }) (*Branch, error) {
	var b Branch
	err := row.Scan(&b.ID, &b.MerchantID, &b.Slug, &b.Name, &b.NameEn, &b.Address, &b.Phone, &b.Timezone,
		&b.Currency, &b.TaxRateBP, &b.TaxInclusive, &b.ServiceChargeBP, &b.PackagingFee, &b.MinOrderAmount,
		&b.AcceptTimeoutMinutes, &b.DeliveryEnabled, &b.PickupEnabled, &b.Paused, &b.OpenTime, &b.CloseTime, &b.NotifySMS,
		&b.SchedulingEnabled, &b.PrepMinutes, &b.Lat, &b.Lng, &b.ZoneMode, &b.ZoneToleranceM,
		&b.KitchenSlowMinutes, &b.DetourPercent, &b.DetourMeters, &b.Notice, &b.Intro, &b.BasketsEnabled,
		&b.CountryCode, &b.Country, &b.Province, &b.City, &b.District, &b.PostalCode, &b.ContactFields, &b.MenuLang, &b.MenuLangsOff, &b.AddonConfirmMinutes, &b.CookTargetMinutes, &b.Suspended, &b.StoreCode, &b.DayResetTime, &b.StoreNo)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	b.OpenNow = b.isOpen(time.Now())
	return &b, nil
}

// latLng is the store's position, nil until the merchant has set it.
func (b *Branch) latLng() *LatLng {
	if b.Lat == nil || b.Lng == nil {
		return nil
	}
	return &LatLng{*b.Lat, *b.Lng}
}

func branchBySlug(ctx context.Context, q Querier, slug string) (*Branch, error) {
	// a brand's slug names its store too (the brand is read with brandBySlug)
	return scanBranch(q.QueryRow(ctx, `SELECT `+branchCols+` FROM branches
		WHERE (slug=$1 OR id = (SELECT br.branch_id FROM brands br WHERE br.slug=$1 AND br.status='active'))
		AND deleted_at IS NULL AND EXISTS (SELECT 1 FROM merchants m WHERE m.id = branches.merchant_id AND m.deleted_at IS NULL)`, slug))
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
	// Polygon is the outline drawn on the map; empty while not drawn.
	Polygon    Polygon `json:"polygon"`
	Color      string  `json:"color"`
	EtaMinutes *int    `json:"eta_minutes"`
}

type Option struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	NameEn     string `json:"name_en"`
	PriceDelta int64  `json:"price_delta"`
	SoldOut    bool   `json:"sold_out"`
	// NameTr is the name in the customer's language, when it differs.
	NameTr string `json:"name_tr,omitempty"`
	// Code is the topping's code (AC02), for options from a topping template.
	Code string `json:"code,omitempty"`
}

type OptionGroup struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	NameEn    string   `json:"name_en"`
	MinSelect int      `json:"min_select"`
	MaxSelect int      `json:"max_select"`
	Options   []Option `json:"options"`
	NameTr    string   `json:"name_tr,omitempty"`
	// ToppingTemplateID is set when the group comes from a topping template:
	// the dish form does not edit it, the template does.
	ToppingTemplateID *int64 `json:"topping_template_id,omitempty"`
}

type Item struct {
	ID         int64 `json:"id"`
	CategoryID int64 `json:"category_id"`
	// Code is the short code staff type to find the dish (B1); may be empty.
	Code              string   `json:"code"`
	Name              string   `json:"name"`
	NameEn            string   `json:"name_en"`
	Description       string   `json:"description"`
	ImageURL          string   `json:"image_url"`
	Photos            []string `json:"photos"`
	Details           string   `json:"details"`
	Price             int64    `json:"price"`
	Published         bool     `json:"published"`
	SoldOut           bool     `json:"sold_out"`
	StockQty          *int     `json:"stock_qty"`
	NeedsPrep         bool     `json:"needs_prep"`
	DeliveryAvailable bool     `json:"delivery_available"`
	PickupAvailable   bool     `json:"pickup_available"`
	DineInAvailable   bool     `json:"dine_in_available"`
	Sort              int      `json:"sort"`
	// BaseName is the store's own name of a dish a brand renamed.
	BaseName  string        `json:"base_name,omitempty"`
	StationID *int64        `json:"station_id"`
	Groups    []OptionGroup `json:"groups"`
	// NoteTemplateIDs are the flavour note templates the dish shows; Notes
	// are their groups with the tags on offer (note_templates.go).
	NoteTemplateIDs []int64 `json:"note_template_ids"`
	// ToppingTemplateIDs are the topping templates the dish uses; their groups
	// are among Groups.
	ToppingTemplateIDs []int64     `json:"topping_template_ids"`
	Notes              []NoteGroup `json:"notes"`
	// PlatformPrices are the prices set by hand on delivery platforms, by
	// platform id; only the back office's menu fills them in.
	PlatformPrices map[int64]int64 `json:"platform_prices,omitempty"`
	// PlatformAuto is the price on each platform the store sells on when
	// none is set by hand: the store price with the markup, rounded.
	PlatformAuto map[int64]int64 `json:"platform_auto,omitempty"`
	// Sold is how many were ordered so far; only the storefront fills it in.
	Sold int `json:"sold"`
	// The name, description and details in the customer's language, when
	// they differ from the merchant's (menu_translate.go).
	NameTr        string `json:"name_tr,omitempty"`
	DescriptionTr string `json:"description_tr,omitempty"`
	DetailsTr     string `json:"details_tr,omitempty"`
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
		return it.DineInAvailable
	}
	return it.PickupAvailable
}

type Category struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	Sort   int    `json:"sort"`
	// Required: an order must contain something from this category.
	Required bool   `json:"required"`
	Items    []Item `json:"items"`
	NameTr   string `json:"name_tr,omitempty"`
	// brand: a category of a brand's own menu (brand_menu.go), not the store's.
	brand bool
}

func loadZones(ctx context.Context, q Querier, branchID int64, activeOnly bool) ([]Zone, error) {
	rows, err := q.Query(ctx, `SELECT id, name, name_en, fee, min_order, active, sort, polygon, color, eta_minutes
		FROM delivery_zones
		WHERE branch_id=$1 AND (active OR NOT $2) ORDER BY sort, id`, branchID, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	zones := []Zone{}
	for rows.Next() {
		var z Zone
		var poly []byte
		if err := rows.Scan(&z.ID, &z.Name, &z.NameEn, &z.Fee, &z.MinOrder, &z.Active, &z.Sort, &poly, &z.Color,
			&z.EtaMinutes); err != nil {
			return nil, err
		}
		z.Polygon = Polygon{}
		if poly != nil {
			if err := json.Unmarshal(poly, &z.Polygon); err != nil {
				return nil, wrap("zone polygon", err)
			}
		}
		zones = append(zones, z)
	}
	return zones, rows.Err()
}

// loadItems returns non-archived items of a branch keyed by id, with option
// groups. publishedOnly hides drafts (customer view).
func loadItems(ctx context.Context, q Querier, branchID int64, publishedOnly bool) ([]*Item, error) {
	rows, err := q.Query(ctx, `SELECT id, category_id, name, name_en, description, image_url, price, published,
		sold_out, stock_qty, needs_prep, delivery_available, pickup_available, sort, station_id, photos, details, dine_in_available, code
		FROM menu_items WHERE branch_id=$1 AND NOT archived AND (published OR NOT $2) ORDER BY sort, id`,
		branchID, publishedOnly)
	if err != nil {
		return nil, err
	}
	items := []*Item{}
	byID := map[int64]*Item{}
	for rows.Next() {
		it := &Item{Groups: []OptionGroup{}, NoteTemplateIDs: []int64{}, Notes: []NoteGroup{}, ToppingTemplateIDs: []int64{}}
		if err := rows.Scan(&it.ID, &it.CategoryID, &it.Name, &it.NameEn, &it.Description, &it.ImageURL,
			&it.Price, &it.Published, &it.SoldOut, &it.StockQty, &it.NeedsPrep, &it.DeliveryAvailable,
			&it.PickupAvailable, &it.Sort, &it.StationID, &it.Photos, &it.Details, &it.DineInAvailable, &it.Code); err != nil {
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

	rows, err = q.Query(ctx, `SELECT g.id, g.item_id, g.name, g.name_en, g.min_select, g.max_select, g.topping_template_id
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
		if err := rows.Scan(&g.ID, &itemID, &g.Name, &g.NameEn, &g.MinSelect, &g.MaxSelect, &g.ToppingTemplateID); err != nil {
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

	rows, err = q.Query(ctx, `SELECT o.id, o.group_id, o.name, o.name_en, o.price_delta, o.sold_out, o.code
		FROM options o JOIN option_groups g ON g.id = o.group_id JOIN menu_items i ON i.id = g.item_id
		WHERE i.branch_id=$1 AND NOT i.archived ORDER BY o.sort, o.id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var o Option
		var groupID int64
		if err := rows.Scan(&o.ID, &groupID, &o.Name, &o.NameEn, &o.PriceDelta, &o.SoldOut, &o.Code); err != nil {
			return nil, err
		}
		if ref, ok := groups[groupID]; ok {
			ref.item.Groups[ref.idx].Options = append(ref.item.Groups[ref.idx].Options, o)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := attachNotes(ctx, q, branchID, items); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT l.item_id, l.template_id FROM menu_item_topping_templates l JOIN menu_items i ON i.id = l.item_id
		WHERE i.branch_id=$1 AND NOT i.archived ORDER BY l.sort, l.template_id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var iid, tid int64
		if err := rows.Scan(&iid, &tid); err != nil {
			return nil, err
		}
		if it, ok := byID[iid]; ok {
			it.ToppingTemplateIDs = append(it.ToppingTemplateIDs, tid)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func loadMenu(ctx context.Context, q Querier, branchID int64, publishedOnly bool) ([]Category, error) {
	rows, err := q.Query(ctx, `SELECT id, name, name_en, sort, required FROM menu_categories WHERE branch_id=$1 AND NOT archived ORDER BY sort, id`, branchID)
	if err != nil {
		return nil, err
	}
	cats := []Category{}
	idx := map[int64]int{}
	for rows.Next() {
		c := Category{Items: []Item{}}
		if err := rows.Scan(&c.ID, &c.Name, &c.NameEn, &c.Sort, &c.Required); err != nil {
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
	Branch *Branch `json:"branch"`
	// Brand is the brand whose menu and promotions these are.
	Brand      *BrandRef   `json:"brand"`
	Zones      []Zone      `json:"zones"`
	Categories []Category  `json:"categories"`
	Promotions []Promotion `json:"promotions"`
	// DinePromotions are the dine-in offers running now, shown on the table's
	// QR menu; the delivery storefront never shows them.
	DinePromotions []Promotion `json:"dine_promotions"`
	// ItemLimits are the purchase limits on items running now (item_limits.go)
	// for delivery and pickup orders; DineItemLimits those of table bills.
	ItemLimits     []ItemLimit `json:"item_limits"`
	DineItemLimits []ItemLimit `json:"dine_item_limits"`
	// Slots are the times an order can be scheduled for (empty when the store
	// does not take scheduled orders).
	Slots []time.Time `json:"slots"`
	// Templates is the published look of each ordering channel.
	Templates map[string]Template `json:"templates"`
	// Sales is the number of orders the store has taken; Rating sums up its reviews.
	Sales  int           `json:"sales"`
	Rating ReviewSummary `json:"rating"`
	// Media are the promotional pictures and videos at the top of the page.
	Media []Media `json:"media"`
	// MachineTranslated: some of the menu is shown machine translated.
	MachineTranslated bool `json:"machine_translated"`
}

func (a *App) Storefront(ctx context.Context, slug string) (*Storefront, error) {
	return a.StorefrontOf(ctx, slug, 0)
}

// StorefrontOf is the storefront of one of the store's brands (0 = the one
// the slug names, the main one for the store's own slug): its menu, names and
// prices, and its promotions. A brand other than the main one looks like a
// shop of its own (docs/MULTI_BRAND_DESIGN.md §9).
func (a *App) StorefrontOf(ctx context.Context, slug string, brandID int64) (*Storefront, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	var brand *Brand
	if brandID == 0 {
		brand, err = brandBySlug(ctx, a.DB, b.ID, slug)
	} else {
		brand, err = loadBrand(ctx, a.DB, b.ID, brandID)
	}
	if err == ErrNotFound {
		return nil, Err(http.StatusNotFound, "BRAND_UNKNOWN", "no such brand in this store")
	}
	if err != nil {
		return nil, err
	}
	// The storefront shows a suspended merchant the same way as a paused store.
	b.forBrand(brand)
	b.Paused = !b.takesOrders()
	a.forCustomer(b)
	zones, err := loadZones(ctx, a.DB, b.ID, true)
	if err != nil {
		return nil, err
	}
	cats, err := brandMenuCategories(ctx, a.DB, b.ID, brand, true)
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
	sold, sales, err := salesCounts(ctx, a.DB, b.ID, brand.ID)
	if err != nil {
		return nil, err
	}
	for i := range cats {
		for j := range cats[i].Items {
			it := &cats[i].Items[j]
			if it.StockQty != nil && *it.StockQty <= 0 {
				it.SoldOut = true
			}
			it.StationID = nil
			it.Sold = sold[it.ID]
		}
	}
	rating, err := reviewSummary(ctx, a.DB, b.ID, brand.ID)
	if err != nil {
		return nil, err
	}
	promos, err := livePromotions(ctx, a.DB, b, brand.ID, "delivery")
	if err != nil {
		return nil, err
	}
	dinePromos, err := livePromotions(ctx, a.DB, b, brand.ID, "dine_in")
	if err != nil {
		return nil, err
	}
	limits, err := storeLimits(ctx, a.DB, b, brand.ID, "delivery")
	if err != nil {
		return nil, err
	}
	dineLimits, err := storeLimits(ctx, a.DB, b, brand.ID, "dine_in")
	if err != nil {
		return nil, err
	}
	tpl, err := a.PublishedTemplates(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	media, err := listMedia(ctx, a.DB, b.ID, true)
	if err != nil {
		return nil, err
	}
	if !brand.IsMain {
		// the store's pictures are of its main brand
		media = []Media{}
		for ch, t := range tpl {
			if brand.LogoURL != "" {
				t.LogoURL = brand.LogoURL
			}
			if brand.CoverURL != "" {
				t.CoverURL = brand.CoverURL
			}
			tpl[ch] = t
		}
	}
	ref := brand.Ref()
	return &Storefront{Brand: &ref, Branch: b, Zones: zones, Categories: cats, Promotions: promos, DinePromotions: dinePromos, ItemLimits: limits, DineItemLimits: dineLimits, Slots: b.Slots(time.Now()), Templates: tpl,
		Sales: sales, Rating: *rating, Media: media}, nil
}
