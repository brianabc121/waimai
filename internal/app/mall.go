package app

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The marketplace: the stores that deliver to where a customer is, on one
// page. A store's delivery areas stay with the merchant; a customer is told
// only what delivery to their spot costs and how long it takes.

const (
	// mallNearM is how far a store that does not deliver to the spot may be
	// and still be listed, after the ones that deliver, marked as such.
	mallNearM = 50000
	// mallReachDeg is a first cut around the customer, a little more than
	// mallNearM (which is more than maxZoneReachM).
	mallReachDeg = 0.46
	// mallMinReviews is how many reviews a store needs before its rating is shown.
	mallMinReviews = 5
	mallMaxStores  = 200
)

// MallCategory is a kind of store customers browse by.
type MallCategory struct {
	ID     int64  `json:"id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	Icon   string `json:"icon"`
	Sort   int    `json:"sort"`
	Active bool   `json:"active"`
	// NameTr is the name in the customer's language, where it differs.
	NameTr string `json:"name_tr,omitempty" db:"-"`
}

// MallBanner is a banner at the top of the marketplace.
type MallBanner struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	TitleEn    string `json:"title_en"`
	Subtitle   string `json:"subtitle"`
	SubtitleEn string `json:"subtitle_en"`
	LinkSlug   string `json:"link_slug"`
	LinkURL    string `json:"link_url"`
	ImageURL   string `json:"image_url"`
	Sort       int    `json:"sort"`
	Active     bool   `json:"active"`
	TitleTr    string `json:"title_tr,omitempty"`
	SubtitleTr string `json:"subtitle_tr,omitempty"`
}

// MallDelivery is what delivery to the customer's spot costs at one store.
type MallDelivery struct {
	Fee        int64 `json:"fee"`
	MinOrder   int64 `json:"min_order"`
	EtaMinutes *int  `json:"eta_minutes"`
}

// MallPromo is a running offer, as the tag on a store card.
type MallPromo struct {
	Kind        string `json:"kind"`
	Threshold   int64  `json:"threshold"`
	Value       int64  `json:"value"`
	MaxDiscount *int64 `json:"max_discount"`
	Name        string `json:"name"`
	NameEn      string `json:"name_en"`
}

// MallDish is a dish that matched a search.
type MallDish struct {
	Name     string `json:"name"`
	NameEn   string `json:"name_en"`
	NameTr   string `json:"name_tr,omitempty"`
	Price    int64  `json:"price"`
	ImageURL string `json:"image_url"`
}

// MallStore is one store card.
type MallStore struct {
	Slug       string   `json:"slug"`
	Name       string   `json:"name"`
	NameEn     string   `json:"name_en"`
	LogoURL    string   `json:"logo_url"`
	CoverURL   string   `json:"cover_url"`
	Tagline    string   `json:"tagline"`
	Categories []string `json:"categories"`
	Currency   string   `json:"currency"`
	City       string   `json:"city"`
	District   string   `json:"district"`
	// Rating is nil until the store has a handful of reviews.
	Rating       *float64 `json:"rating"`
	MonthlySales int      `json:"monthly_sales"`
	// Status is open, closed (outside opening hours) or paused.
	Status string `json:"status"`
	// OpensAt is when a closed store opens next ("10:00"), tomorrow or today.
	OpensAt       string `json:"opens_at"`
	OpensTomorrow bool   `json:"opens_tomorrow"`
	Scheduling    bool   `json:"scheduling"`
	// Delivery is nil when the store does not deliver to the spot, or when
	// stores are listed by city and no spot is known.
	Delivery *MallDelivery `json:"delivery"`
	// NoDelivery marks a store near the spot (within mallNearM) that does
	// not deliver there: listed after the others, to browse or pick up.
	NoDelivery bool `json:"no_delivery"`
	Pickup     bool `json:"pickup"`
	// MinOrderFrom is the store's lowest minimum order, shown when listing by city.
	MinOrderFrom int64       `json:"min_order_from"`
	DistanceM    *int        `json:"distance_m"`
	Promos       []MallPromo `json:"promos"`
	Dishes       []MallDish  `json:"dishes"`
	// NameTr is the store's name in the customer's language, when it differs.
	NameTr string `json:"name_tr,omitempty"`

	id       int64
	branch   *Branch
	lastUsed *time.Time
}

// MallRegionRef says where a point or a store is, in words.
type MallRegionRef struct {
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	Province    string `json:"province"`
	City        string `json:"city"`
	District    string `json:"district"`
}

// MallQuery is what the customer asks for.
type MallQuery struct {
	At *LatLng
	// Listing by city instead of around a point.
	CountryCode, City, District string
	Category                    string
	Q                           string
	Sort                        string // "", distance, rating, fee
	// Filters: open, promo, schedule, pickup.
	Filters []string
	// CustomerID, when someone is logged in, marks the stores they use.
	CustomerID int64
	// Lang names the dishes found in the customer's language.
	Lang string
}

// MallList is the answer: the stores that deliver to the spot, then the
// stores within mallNearM that do not (NoDelivery), so a place with few
// stores still has something to show.
type MallList struct {
	// Region is where the spot is, going by the nearest store.
	Region *MallRegionRef `json:"region"`
	Stores []MallStore    `json:"stores"`
	// Frequent are the slugs, among Stores, the customer keeps or ordered from.
	Frequent []string `json:"frequent"`
}

const mallStoreSQL = `SELECT b.id,
	coalesce(t.published->>'logo_url', ''), coalesce(t.published->>'cover_url', ''), b.tagline,
	coalesce((SELECT array_agg(c.code ORDER BY c.sort, c.id) FROM branch_categories bc JOIN store_categories c ON c.id = bc.category_id
		WHERE bc.branch_id = b.id AND c.active), '{}'),
	(SELECT count(*) FROM reviews r WHERE r.branch_id = b.id),
	coalesce((SELECT round(avg(r.rating), 1) FROM reviews r WHERE r.branch_id = b.id), 0)::float8,
	(SELECT count(*) FROM orders o WHERE o.branch_id = b.id AND o.acceptance_status = 'accepted' AND o.created_at > now() - interval '30 days')
	FROM branches b
	JOIN merchants m ON m.id = b.merchant_id
	LEFT JOIN channel_templates t ON t.branch_id = b.id AND t.channel = 'delivery'
	WHERE b.deleted_at IS NULL AND m.deleted_at IS NULL AND m.status = 'active'
		AND b.mall_listed AND NOT b.mall_hidden AND (b.delivery_enabled OR b.pickup_enabled) AND `

// mallCandidates loads the listed stores a condition selects, with what a card shows.
func (a *App) mallCandidates(ctx context.Context, where string, args ...any) ([]*MallStore, error) {
	rows, err := a.DB.Query(ctx, mallStoreSQL+where+` LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	type extra struct {
		logo, cover, tagline string
		cats                 []string
		reviews, sales       int
		rating               float64
	}
	byID := map[int64]extra{}
	ids := []int64{}
	for rows.Next() {
		var id int64
		var e extra
		if err := rows.Scan(&id, &e.logo, &e.cover, &e.tagline, &e.cats, &e.reviews, &e.rating, &e.sales); err != nil {
			rows.Close()
			return nil, err
		}
		byID[id] = e
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []*MallStore{}
	if len(ids) == 0 {
		return out, nil
	}
	brows, err := a.DB.Query(ctx, `SELECT `+branchCols+` FROM branches WHERE id = ANY($1) ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	defer brows.Close()
	now := time.Now()
	for brows.Next() {
		b, err := scanBranch(brows)
		if err != nil {
			return nil, err
		}
		e := byID[b.ID]
		s := &MallStore{id: b.ID, branch: b, Slug: b.Slug, Name: b.Name, NameEn: b.NameEn, LogoURL: e.logo, CoverURL: e.cover,
			Tagline: e.tagline, Categories: e.cats, Currency: b.Currency, City: b.City, District: b.District,
			MonthlySales: e.sales, Scheduling: b.SchedulingEnabled, Pickup: b.PickupEnabled,
			Promos: []MallPromo{}, Dishes: []MallDish{}, MinOrderFrom: b.MinOrderAmount}
		if s.Categories == nil {
			s.Categories = []string{}
		}
		if e.reviews >= mallMinReviews {
			r := e.rating
			s.Rating = &r
		}
		switch {
		case !b.takesOrders():
			s.Status = "paused"
		case b.isOpen(now):
			s.Status = "open"
		default:
			s.Status = "closed"
			s.OpensAt = b.OpenTime
			s.OpensTomorrow = now.In(b.location()).Format("15:04") >= b.OpenTime
		}
		out = append(out, s)
	}
	return out, brows.Err()
}

// mallZones loads the active delivery areas of the listed stores.
func (a *App) mallZones(ctx context.Context, ids []int64) (map[int64][]Zone, error) {
	out := map[int64][]Zone{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := a.DB.Query(ctx, `SELECT branch_id, fee, min_order, polygon, eta_minutes FROM delivery_zones
		WHERE branch_id = ANY($1) AND active ORDER BY branch_id, sort, id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var bid int64
		var z Zone
		var poly []byte
		if err := rows.Scan(&bid, &z.Fee, &z.MinOrder, &poly, &z.EtaMinutes); err != nil {
			return nil, err
		}
		if poly != nil {
			if err := json.Unmarshal(poly, &z.Polygon); err != nil {
				return nil, wrap("zone polygon", err)
			}
		}
		out[bid] = append(out[bid], z)
	}
	return out, rows.Err()
}

func storeIDs(list []*MallStore) []int64 {
	ids := make([]int64, len(list))
	for i, s := range list {
		ids[i] = s.id
	}
	return ids
}

// MallStores answers the marketplace page.
func (a *App) MallStores(ctx context.Context, q MallQuery) (*MallList, error) {
	out := &MallList{Stores: []MallStore{}, Frequent: []string{}}
	var delivers, listed []*MallStore
	if q.At != nil {
		at := *q.At
		if !validLatLng(at) {
			return nil, ErrValidation("invalid coordinates")
		}
		lngReach := mallReachDeg / math.Max(0.2, math.Cos(at[0]*math.Pi/180))
		cands, err := a.mallCandidates(ctx, `b.lat BETWEEN $1 AND $2 AND b.lng BETWEEN $3 AND $4`,
			at[0]-mallReachDeg, at[0]+mallReachDeg, at[1]-lngReach, at[1]+lngReach)
		if err != nil {
			return nil, err
		}
		zones, err := a.mallZones(ctx, storeIDs(cands))
		if err != nil {
			return nil, err
		}
		var nearest *MallStore
		best := math.Inf(1)
		for _, s := range cands {
			b := s.branch
			d := metresBetween(at, *b.latLng())
			dm := int(math.Round(d))
			s.DistanceM = &dm
			if d < best {
				best, nearest = d, s
			}
			var hit *ZoneMatch
			if b.DeliveryEnabled {
				hit, _ = matchZone(zones[b.ID], at, b.ZoneToleranceM)
			}
			if hit != nil {
				s.Delivery = &MallDelivery{Fee: hit.Zone.Fee, MinOrder: max(b.MinOrderAmount, hit.Zone.MinOrder), EtaMinutes: hit.Zone.EtaMinutes}
				delivers = append(delivers, s)
				listed = append(listed, s)
			} else if d <= mallNearM {
				s.NoDelivery = true
				listed = append(listed, s)
			}
		}
		if nearest != nil {
			b := nearest.branch
			out.Region = &MallRegionRef{CountryCode: b.CountryCode, Country: b.Country, Province: b.Province, City: b.City, District: b.District}
		}
	} else {
		if q.CountryCode == "" && q.City == "" {
			return nil, ErrValidation("a position or a city is required")
		}
		cands, err := a.mallCandidates(ctx, `b.lat IS NOT NULL AND ($1 = '' OR b.country_code = $1) AND ($2 = '' OR b.city = $2) AND ($3 = '' OR b.district = $3)`,
			strings.ToUpper(q.CountryCode), q.City, q.District)
		if err != nil {
			return nil, err
		}
		zones, err := a.mallZones(ctx, storeIDs(cands))
		if err != nil {
			return nil, err
		}
		for _, s := range cands {
			// the lowest minimum among the store's areas: "from ₱100"
			first := true
			for _, z := range zones[s.id] {
				if len(z.Polygon) < minPolygonPoints {
					continue
				}
				if m := max(s.branch.MinOrderAmount, z.MinOrder); first || m < s.MinOrderFrom {
					s.MinOrderFrom, first = m, false
				}
			}
		}
		delivers, listed = cands, cands
		out.Region = &MallRegionRef{CountryCode: strings.ToUpper(q.CountryCode), City: q.City, District: q.District}
	}

	all := listed
	if err := a.mallPromos(ctx, all); err != nil {
		return nil, err
	}
	if err := a.mallNames(ctx, all, q.Lang); err != nil {
		return nil, err
	}
	if text := strings.TrimSpace(q.Q); text != "" {
		if err := a.mallSearch(ctx, all, text, q.Lang); err != nil {
			return nil, err
		}
	}
	keep := func(s *MallStore) bool {
		if q.Category != "" && !slices.Contains(s.Categories, q.Category) {
			return false
		}
		if strings.TrimSpace(q.Q) != "" && len(s.Dishes) == 0 && !s.matched(q.Q) {
			return false
		}
		for _, f := range q.Filters {
			switch f {
			case "open":
				if s.Status != "open" {
					return false
				}
			case "promo":
				if len(s.Promos) == 0 {
					return false
				}
			case "schedule":
				if !s.Scheduling {
					return false
				}
			case "pickup":
				if !s.Pickup {
					return false
				}
			}
		}
		return true
	}
	if q.CustomerID != 0 && len(delivers) > 0 {
		if err := a.mallMine(ctx, q.CustomerID, delivers); err != nil {
			return nil, err
		}
		mine := []*MallStore{}
		for _, s := range delivers {
			if s.lastUsed != nil {
				mine = append(mine, s)
			}
		}
		sort.SliceStable(mine, func(i, j int) bool { return mine[i].lastUsed.After(*mine[j].lastUsed) })
		for i, s := range mine {
			if i < 8 {
				out.Frequent = append(out.Frequent, s.Slug)
			}
		}
	}
	for _, s := range mallSorted(listed, q.Sort) {
		if keep(s) && len(out.Stores) < mallMaxStores {
			out.Stores = append(out.Stores, *s)
		}
	}
	return out, nil
}

// matched says whether the store's own name answers a search.
func (s *MallStore) matched(q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	return strings.Contains(strings.ToLower(s.Name), q) || strings.Contains(strings.ToLower(s.NameEn), q) ||
		strings.Contains(strings.ToLower(s.NameTr), q)
}

// mallNames names the stores in the customer's language.
func (a *App) mallNames(ctx context.Context, list []*MallStore, lang string) error {
	bs := make([]*Branch, 0, len(list))
	for _, s := range list {
		if s.branch != nil {
			bs = append(bs, s.branch)
		}
	}
	t, err := storeTexts(ctx, a.DB, lang, bs)
	if err != nil {
		return err
	}
	for _, s := range list {
		s.NameTr = t[s.id].Name
	}
	return nil
}

// mallSorted puts the stores that deliver first, then the stores taking
// orders now; within that, by the order asked for: nearest first unless told
// otherwise.
func mallSorted(list []*MallStore, by string) []*MallStore {
	rank := func(s *MallStore) int {
		switch s.Status {
		case "open":
			return 0
		case "closed":
			return 1
		}
		return 2
	}
	dist := func(s *MallStore) int {
		if s.DistanceM == nil {
			return 0
		}
		return *s.DistanceM
	}
	out := append([]*MallStore{}, list...)
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.NoDelivery != y.NoDelivery {
			return !x.NoDelivery
		}
		if rank(x) != rank(y) {
			return rank(x) < rank(y)
		}
		switch by {
		case "rating":
			rx, ry := 0.0, 0.0
			if x.Rating != nil {
				rx = *x.Rating
			}
			if y.Rating != nil {
				ry = *y.Rating
			}
			if rx != ry {
				return rx > ry
			}
		case "fee":
			if x.Delivery != nil && y.Delivery != nil && x.Delivery.Fee != y.Delivery.Fee {
				return x.Delivery.Fee < y.Delivery.Fee
			}
		}
		if dist(x) != dist(y) {
			return dist(x) < dist(y)
		}
		if x.MonthlySales != y.MonthlySales {
			return x.MonthlySales > y.MonthlySales
		}
		return x.id < y.id
	})
	return out
}

// mallPromos adds each store's running offers, at most two a card.
func (a *App) mallPromos(ctx context.Context, list []*MallStore) error {
	if len(list) == 0 {
		return nil
	}
	byID := map[int64]*MallStore{}
	for _, s := range list {
		byID[s.id] = s
	}
	rows, err := a.DB.Query(ctx, `SELECT p.branch_id, `+promoCols+`, 0 FROM promotions p
		WHERE p.branch_id = ANY($1) AND p.status = 'active' ORDER BY p.branch_id, p.id`, storeIDs(list))
	if err != nil {
		return err
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var bid int64
		var p Promotion
		if err := rows.Scan(&bid, &p.ID, &p.Name, &p.NameEn, &p.Kind, &p.Threshold, &p.Value, &p.MaxDiscount,
			&p.AppliesDelivery, &p.AppliesPickup, &p.StartsAt, &p.EndsAt, &p.DailyStart, &p.DailyEnd,
			&p.TotalQuota, &p.PerPhoneLimit, &p.UsedCount, &p.Status, &p.phoneUsed); err != nil {
			return err
		}
		s := byID[bid]
		p.setLive(now, s.branch.location())
		if p.LiveNow && len(s.Promos) < 2 {
			s.Promos = append(s.Promos, MallPromo{Kind: p.Kind, Threshold: p.Threshold, Value: p.Value, MaxDiscount: p.MaxDiscount, Name: p.Name, NameEn: p.NameEn})
		}
	}
	return rows.Err()
}

// mallSearch adds the dishes whose name answers a search, at most three a
// store. The search looks in every language the menu is translated into, so a
// Thai customer typing Thai finds a menu written in Chinese.
func (a *App) mallSearch(ctx context.Context, list []*MallStore, q, lang string) error {
	if len(list) == 0 {
		return nil
	}
	byID := map[int64]*MallStore{}
	for _, s := range list {
		byID[s.id] = s
	}
	if !IsLang(lang) {
		lang = "en"
	}
	rows, err := a.DB.Query(ctx, `SELECT i.branch_id, i.name, i.name_en, i.price, i.image_url,
			CASE WHEN b.menu_lang = $3 THEN '' ELSE COALESCE(
				(SELECT t.text FROM content_translations t WHERE t.entity='item' AND t.entity_id=i.id AND t.field='name'
					AND t.lang = CASE WHEN $3 = ANY(b.menu_langs_off) THEN 'en' ELSE $3 END),
				(SELECT t.text FROM content_translations t WHERE t.entity='item' AND t.entity_id=i.id AND t.field='name'
					AND t.lang = 'en' AND b.menu_lang <> 'en'), '') END
		FROM menu_items i JOIN branches b ON b.id = i.branch_id
		WHERE i.branch_id = ANY($1) AND i.published AND NOT i.archived AND (i.delivery_available OR i.pickup_available)
		AND (i.name ILIKE $2 OR i.name_en ILIKE $2 OR EXISTS (SELECT 1 FROM content_translations x
			WHERE x.entity='item' AND x.entity_id=i.id AND x.field='name' AND x.text ILIKE $2))
		ORDER BY i.branch_id, i.sort, i.id`, storeIDs(list), like(q), lang)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var bid int64
		var d MallDish
		if err := rows.Scan(&bid, &d.Name, &d.NameEn, &d.Price, &d.ImageURL, &d.NameTr); err != nil {
			return err
		}
		if s := byID[bid]; len(s.Dishes) < 3 {
			s.Dishes = append(s.Dishes, d)
		}
	}
	return rows.Err()
}

// mallMine marks the stores a customer keeps or has ordered from.
func (a *App) mallMine(ctx context.Context, customerID int64, list []*MallStore) error {
	byID := map[int64]*MallStore{}
	for _, s := range list {
		byID[s.id] = s
	}
	rows, err := a.DB.Query(ctx, `SELECT branch_id, max(at) FROM (
			SELECT branch_id, created_at AS at FROM customer_favorites WHERE customer_id = $1 AND branch_id = ANY($2)
			UNION ALL
			SELECT branch_id, created_at FROM orders WHERE customer_id = $1 AND branch_id = ANY($2)) u GROUP BY 1`,
		customerID, storeIDs(list))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var bid int64
		var at time.Time
		if err := rows.Scan(&bid, &at); err != nil {
			return err
		}
		byID[bid].lastUsed = &at
	}
	return rows.Err()
}

// ---- the places that have stores ----

type MallDistrict struct {
	District string `json:"district"`
	Stores   int    `json:"stores"`
}
type MallCity struct {
	Province  string         `json:"province"`
	City      string         `json:"city"`
	Stores    int            `json:"stores"`
	Districts []MallDistrict `json:"districts"`
}
type MallCountry struct {
	CountryCode string     `json:"country_code"`
	Country     string     `json:"country"`
	Stores      int        `json:"stores"`
	Cities      []MallCity `json:"cities"`
}

// MallRegions lists the countries, cities and districts that have a listed store.
func (a *App) MallRegions(ctx context.Context) ([]MallCountry, error) {
	rows, err := a.DB.Query(ctx, `SELECT b.country_code, max(b.country), max(b.province), b.city, b.district, count(*)
		FROM branches b JOIN merchants m ON m.id = b.merchant_id
		WHERE b.deleted_at IS NULL AND m.deleted_at IS NULL AND m.status = 'active' AND b.mall_listed AND NOT b.mall_hidden
			AND (b.delivery_enabled OR b.pickup_enabled) AND b.lat IS NOT NULL AND b.country_code <> '' AND b.city <> ''
		GROUP BY b.country_code, b.city, b.district ORDER BY b.country_code, count(*) DESC, b.city, b.district`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MallCountry{}
	for rows.Next() {
		var code, country, province, city, district string
		var n int
		if err := rows.Scan(&code, &country, &province, &city, &district, &n); err != nil {
			return nil, err
		}
		var c *MallCountry
		for i := range out {
			if out[i].CountryCode == code {
				c = &out[i]
			}
		}
		if c == nil {
			out = append(out, MallCountry{CountryCode: code, Country: country, Cities: []MallCity{}})
			c = &out[len(out)-1]
		}
		c.Stores += n
		var ci *MallCity
		for i := range c.Cities {
			if c.Cities[i].City == city {
				ci = &c.Cities[i]
			}
		}
		if ci == nil {
			c.Cities = append(c.Cities, MallCity{Province: province, City: city, Districts: []MallDistrict{}})
			ci = &c.Cities[len(c.Cities)-1]
		}
		ci.Stores += n
		if district != "" {
			ci.Districts = append(ci.Districts, MallDistrict{District: district, Stores: n})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		sort.SliceStable(out[i].Cities, func(x, y int) bool { return out[i].Cities[x].Stores > out[i].Cities[y].Stores })
	}
	return out, nil
}

// ---- what the page starts with ----

type MallHome struct {
	Name       string         `json:"name"`
	Categories []MallCategory `json:"categories"`
	Banners    []MallBanner   `json:"banners"`
}

func mallCategories(ctx context.Context, q Querier, activeOnly bool) ([]MallCategory, error) {
	rows, err := q.Query(ctx, `SELECT id, code, name, name_en, icon, sort, active FROM store_categories
		WHERE active OR NOT $1 ORDER BY sort, id`, activeOnly)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[MallCategory])
	if out == nil {
		out = []MallCategory{}
	}
	return out, err
}

const mallBannerPath = "/api/v1/mall/banners/"

func mallBanners(ctx context.Context, q Querier, activeOnly bool) ([]MallBanner, error) {
	rows, err := q.Query(ctx, `SELECT id, title, title_en, subtitle, subtitle_en, link_slug, link_url, image_token, sort, active
		FROM mall_banners WHERE active OR NOT $1 ORDER BY sort, id`, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MallBanner{}
	for rows.Next() {
		var b MallBanner
		var token string
		if err := rows.Scan(&b.ID, &b.Title, &b.TitleEn, &b.Subtitle, &b.SubtitleEn, &b.LinkSlug, &b.LinkURL, &token, &b.Sort, &b.Active); err != nil {
			return nil, err
		}
		if token != "" {
			b.ImageURL = mallBannerPath + token
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// MallHome is what the marketplace shows before it knows where the customer is.
func (a *App) MallHome(ctx context.Context) (*MallHome, error) {
	info, err := a.PlatformInfo(ctx)
	if err != nil {
		return nil, err
	}
	cats, err := mallCategories(ctx, a.DB, true)
	if err != nil {
		return nil, err
	}
	banners, err := mallBanners(ctx, a.DB, true)
	if err != nil {
		return nil, err
	}
	return &MallHome{Name: info["name"], Categories: cats, Banners: banners}, nil
}

// MallBannerImage returns a banner's picture by the token in its address.
func (a *App) MallBannerImage(ctx context.Context, token string) ([]byte, error) {
	var image []byte
	err := a.DB.QueryRow(ctx, `SELECT image FROM mall_banners WHERE image_token=$1 AND image IS NOT NULL`, token).Scan(&image)
	if isNoRows(err) || (err == nil && token == "") {
		return nil, ErrNotFound
	}
	return image, err
}
