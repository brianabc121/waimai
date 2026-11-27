package app

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Customer addresses and where deliveries go (docs/DELIVERY_ADDRESS_STATS_DESIGN.md):
// a store's customer address list, the addresses with the most deliveries in
// a period, and those addresses as points for a map. The store sees only its
// own; the platform sees every store, narrowed by merchant, store or region.
// Nothing is stored for this: it is all read off the orders.

// countedDelivery is a delivery that went out: not declined, cancelled or
// failed, and with an address.
const countedDelivery = `o.service_type = 'delivery' AND o.acceptance_status NOT IN ('rejected','cancelled')
	AND o.fulfillment_status <> 'delivery_failed' AND addr_key(o.address_line) <> ''`

// custKey tells customers apart: by phone, else by account, else the order.
const custKey = `COALESCE(NULLIF(o.customer_phone, ''), 'c' || o.customer_id::text, 'o' || o.id::text)`

// MaxStatsDays is the longest period the ranking and the map cover.
const MaxStatsDays = 31

// MaxMapPoints is the most addresses the map is sent at once.
const MaxMapPoints = 20000

// DeliveryScope is whose deliveries: one store (BranchID), one merchant's
// stores, or every store, optionally in one country or city.
type DeliveryScope struct {
	BranchID int64
	// Brands narrows a store's deliveries to some of its brands; nil is every
	// brand together (docs/MULTI_BRAND_DESIGN.md §12).
	Brands      []int64
	MerchantID  int64
	CountryCode string
	City        string
}

// DeliveryRange is a period of whole days, From to To inclusive, each in the
// store's own time zone ("2026-11-23").
type DeliveryRange struct {
	From string
	To   string
}

func (r DeliveryRange) days() (time.Time, time.Time, error) {
	from, err1 := time.Parse("2006-01-02", r.From)
	to, err2 := time.Parse("2006-01-02", r.To)
	if err1 != nil || err2 != nil {
		return from, to, Err(http.StatusBadRequest, "BAD_RANGE", "from and to must be dates (YYYY-MM-DD)")
	}
	if to.Before(from) {
		return from, to, Err(http.StatusBadRequest, "BAD_RANGE", "to is before from")
	}
	if to.Sub(from) >= MaxStatsDays*24*time.Hour {
		return from, to, ErrD(http.StatusBadRequest, "RANGE_TOO_LONG", "at most 31 days", map[string]int{"max_days": MaxStatsDays})
	}
	return from, to, nil
}

// sqlArgs collects the arguments of a query as it is built.
type sqlArgs []any

func (a *sqlArgs) add(v any) string {
	*a = append(*a, v)
	return "$" + strconv.Itoa(len(*a))
}

// where is the scope's part of a WHERE on orders o joined to branches b.
func (s DeliveryScope) where(args *sqlArgs) []string {
	w := []string{countedDelivery, "b.deleted_at IS NULL"}
	if s.BranchID != 0 {
		w = append(w, "o.branch_id = "+args.add(s.BranchID))
	}
	if s.Brands != nil {
		w = append(w, "o.brand_id = ANY("+args.add(s.Brands)+"::bigint[])")
	}
	if s.MerchantID != 0 {
		w = append(w, "b.merchant_id = "+args.add(s.MerchantID))
	}
	if s.CountryCode != "" {
		w = append(w, "b.country_code = "+args.add(strings.ToUpper(s.CountryCode)))
	}
	if s.City != "" {
		w = append(w, "b.city = "+args.add(s.City))
	}
	return w
}

// rangeWhere keeps orders placed in the period, by each store's clock. The
// first two conditions only let an index on created_at be used.
func (r DeliveryRange) where(args *sqlArgs) ([]string, error) {
	from, to, err := r.days()
	if err != nil {
		return nil, err
	}
	f, t := args.add(from.Format("2006-01-02")), args.add(to.Format("2006-01-02"))
	return []string{
		"o.created_at >= " + args.add(from.Add(-24*time.Hour)),
		"o.created_at < " + args.add(to.Add(72*time.Hour)),
		"o.created_at >= (" + f + "::date)::timestamp AT TIME ZONE b.timezone",
		"o.created_at < (" + t + "::date + 1)::timestamp AT TIME ZONE b.timezone",
	}, nil
}

// ---- the address list ----

// CustomerAddressFilter narrows the customer address list.
type CustomerAddressFilter struct {
	DeliveryScope
	Q string
	// Source is "staff" (orders placed by the store), "customer" or "" for both.
	Source string
	// Sort is "recent" (the default), "orders" or "first".
	Sort string
	Page
}

// CustomerAddress is one customer's one address (and unit) at one store,
// with how many deliveries went there.
type CustomerAddress struct {
	BranchID     int64     `json:"branch_id"`
	BranchName   string    `json:"branch_name"`
	MerchantName string    `json:"merchant_name"`
	Cust         string    `json:"cust"`
	Key          string    `json:"key"`
	UnitKey      string    `json:"unit_key"`
	Line         string    `json:"line"`
	Unit         string    `json:"unit"`
	Name         string    `json:"name"`
	Phone        string    `json:"phone"`
	MemberNo     *int      `json:"member_no"`
	ByStaff      bool      `json:"by_staff"`
	ByCustomer   bool      `json:"by_customer"`
	Orders       int       `json:"orders"`
	FirstAt      time.Time `json:"first_at"`
	LastAt       time.Time `json:"last_at"`
	Lat          *float64  `json:"lat"`
	Lng          *float64  `json:"lng"`
}

type CustomerAddressList struct {
	Items     []CustomerAddress `json:"items"`
	Total     int               `json:"total"`
	Customers int               `json:"customers"`
	Addresses int               `json:"addresses"`
}

// textMatch is what q finds: every word in the address, the name, or the
// phone's digits.
func textMatch(q string, args *sqlArgs) string {
	q = strings.TrimSpace(q)
	if len(q) > 100 {
		q = q[:100]
	}
	if q == "" {
		return ""
	}
	cond := []string{"o.customer_name ILIKE " + args.add(like(q))}
	if terms := searchTerms(q); len(terms) > 0 {
		n := args.add(terms)
		cond = append(cond, `NOT EXISTS (SELECT 1 FROM unnest(`+n+`::text[]) t
			WHERE addr_key(o.address_line || ' ' || o.address_unit) NOT LIKE '%' || addr_key(t) || '%')`)
	}
	if d := onlyDigits(strings.TrimLeft(strings.NewReplacer(" ", "", "-", "").Replace(q), "+0")); len(d) >= 3 {
		cond = append(cond, "o.customer_phone LIKE "+args.add("%"+d+"%"))
	}
	return "(" + strings.Join(cond, " OR ") + ")"
}

// CustomerAddresses pages through every address the scope has delivered to,
// whenever that was.
func (a *App) CustomerAddresses(ctx context.Context, f CustomerAddressFilter) (*CustomerAddressList, error) {
	args := sqlArgs{}
	w := f.where(&args)
	if m := textMatch(f.Q, &args); m != "" {
		w = append(w, m)
	}
	switch f.Source {
	case "staff":
		w = append(w, "o.channel = 'staff'")
	case "customer":
		w = append(w, "o.channel <> 'staff'")
	}
	order := "g.last_at DESC, g.last_id DESC"
	switch f.Sort {
	case "orders":
		order = "g.n DESC, g.last_at DESC, g.last_id DESC"
	case "first":
		order = "g.first_at, g.last_id"
	}
	limit, offset := f.limitOffset()
	out := &CustomerAddressList{Items: []CustomerAddress{}}
	groups := `WITH g AS (
		SELECT o.branch_id, ` + custKey + ` AS cust, addr_key(o.address_line) AS akey, lower(btrim(o.address_unit)) AS ukey,
			count(*) AS n, min(o.created_at) AS first_at, max(o.created_at) AS last_at,
			(array_agg(o.id ORDER BY o.created_at DESC, o.id DESC))[1] AS last_id,
			(array_agg(o.id ORDER BY o.created_at DESC, o.id DESC) FILTER (WHERE o.dest_lat IS NOT NULL))[1] AS pin_id,
			bool_or(o.channel = 'staff') AS by_staff, bool_or(o.channel <> 'staff') AS by_customer
		FROM orders o JOIN branches b ON b.id = o.branch_id
		WHERE ` + strings.Join(w, " AND ") + `
		GROUP BY 1, 2, 3, 4)`
	if err := a.DB.QueryRow(ctx, groups+` SELECT count(*), count(DISTINCT (branch_id, cust)), count(DISTINCT (branch_id, akey)) FROM g`,
		args...).Scan(&out.Total, &out.Customers, &out.Addresses); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, groups+`
		SELECT g.branch_id, b.name, mc.name, g.cust, g.akey, g.ukey, lo.address_line, lo.address_unit, lo.customer_name,
			lo.customer_phone, mb.member_no, g.by_staff, g.by_customer, g.n, g.first_at, g.last_at, po.dest_lat, po.dest_lng
		FROM g JOIN orders lo ON lo.id = g.last_id
			LEFT JOIN orders po ON po.id = g.pin_id
			JOIN branches b ON b.id = g.branch_id JOIN merchants mc ON mc.id = b.merchant_id
			LEFT JOIN LATERAL (SELECT sm.member_no FROM store_members sm
				WHERE sm.branch_id = g.branch_id AND (sm.status = 'active' OR sm.expires_at > now())
					AND (sm.id = lo.member_id OR (lo.customer_phone <> '' AND sm.phone = lo.customer_phone))
				ORDER BY (sm.id = lo.member_id) DESC NULLS LAST LIMIT 1) mb ON true
		ORDER BY `+order+` LIMIT `+args.add(limit)+` OFFSET `+args.add(offset), args...)
	if err != nil {
		return nil, err
	}
	out.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (CustomerAddress, error) {
		var c CustomerAddress
		err := r.Scan(&c.BranchID, &c.BranchName, &c.MerchantName, &c.Cust, &c.Key, &c.UnitKey, &c.Line, &c.Unit, &c.Name,
			&c.Phone, &c.MemberNo, &c.ByStaff, &c.ByCustomer, &c.Orders, &c.FirstAt, &c.LastAt, &c.Lat, &c.Lng)
		c.Line, c.Unit = strings.TrimSpace(c.Line), strings.TrimSpace(c.Unit)
		return c, err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AddressOrders lists one store's delivery orders for one customer at one
// address (and unit), newest first, whatever became of them; brands narrows
// them to some of the store's brands (nil: all).
func (a *App) AddressOrders(ctx context.Context, branchID int64, brands []int64, cust, key, unitKey string, p Page) (*MemberOrders, error) {
	const mine = `o.branch_id = $1 AND o.service_type = 'delivery' AND ` + custKey + ` = $2
		AND addr_key(o.address_line) = $3 AND lower(btrim(o.address_unit)) = $4
		AND ($5::bigint[] IS NULL OR o.brand_id = ANY($5))`
	out := &MemberOrders{Items: []MemberOrder{}}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM orders o WHERE `+mine, branchID, cust, key, unitKey, brands).
		Scan(&out.Total); err != nil {
		return nil, err
	}
	limit, offset := p.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.public_no, o.created_at, o.channel, o.order_source, o.service_type,
			o.acceptance_status, o.fulfillment_status, o.total, o.currency, o.address_line, o.member_id IS NOT NULL,
			COALESCE((SELECT string_agg(i.name || ' ×' || i.qty, '、' ORDER BY i.id) FROM order_items i
				WHERE i.order_id = o.id AND i.void_reason = ''), '')
		FROM orders o WHERE `+mine+` ORDER BY o.created_at DESC, o.id DESC LIMIT $6 OFFSET $7`,
		branchID, cust, key, unitKey, brands, limit, offset)
	if err != nil {
		return nil, err
	}
	out.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (MemberOrder, error) {
		var o MemberOrder
		err := r.Scan(&o.ID, &o.PublicNo, &o.CreatedAt, &o.Channel, &o.OrderSource, &o.ServiceType,
			&o.AcceptanceStatus, &o.FulfillmentStatus, &o.Total, &o.Currency, &o.AddressLine, &o.Linked, &o.Items)
		return o, err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---- the ranking ----

// TopAddress is one address (every unit and customer of it together) and
// what was delivered there in the period.
type TopAddress struct {
	Key       string `json:"key"`
	Line      string `json:"line"`
	City      string `json:"city"`
	Orders    int    `json:"orders"`
	Customers int    `json:"customers"`
	// Amount is the orders' total in Currency; nil when the stores counted
	// use more than one currency.
	Amount     *int64    `json:"amount"`
	Currency   string    `json:"currency"`
	LastAt     time.Time `json:"last_at"`
	BranchName string    `json:"branch_name"`
	Branches   int       `json:"branches"`
	Lat        *float64  `json:"lat"`
	Lng        *float64  `json:"lng"`
}

type DeliveryTop struct {
	Items []TopAddress `json:"items"`
	// Orders, Addresses and Unpinned are over the whole period, not only the
	// addresses listed.
	Orders    int `json:"orders"`
	Addresses int `json:"addresses"`
	Unpinned  int `json:"unpinned"`
}

// TopLimit is how many addresses the ranking lists.
const TopLimit = 100

// filtered is the period's counted deliveries in the scope, with each
// order's customer and address key.
func filtered(s DeliveryScope, r DeliveryRange, args *sqlArgs) (string, error) {
	w := s.where(args)
	rw, err := r.where(args)
	if err != nil {
		return "", err
	}
	w = append(w, rw...)
	return `f AS (SELECT o.id, o.branch_id, o.created_at, o.total, o.currency, o.dest_lat, o.address_unit, o.customer_name,
			o.customer_phone, ` + custKey + ` AS cust, addr_key(o.address_line) AS akey
		FROM orders o JOIN branches b ON b.id = o.branch_id WHERE ` + strings.Join(w, " AND ") + `)`, nil
}

// TopAddresses ranks the addresses by deliveries in the period.
func (a *App) TopAddresses(ctx context.Context, s DeliveryScope, r DeliveryRange) (*DeliveryTop, error) {
	args := sqlArgs{}
	f, err := filtered(s, r, &args)
	if err != nil {
		return nil, err
	}
	out := &DeliveryTop{Items: []TopAddress{}}
	if err := a.DB.QueryRow(ctx, `WITH `+f+`, g AS (SELECT akey, count(*) AS n, bool_or(dest_lat IS NOT NULL) AS pinned FROM f GROUP BY akey)
		SELECT COALESCE(sum(n), 0), count(*), count(*) FILTER (WHERE NOT pinned) FROM g`, args...).
		Scan(&out.Orders, &out.Addresses, &out.Unpinned); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `WITH `+f+`, g AS (
			SELECT akey, count(*) AS n, count(DISTINCT cust) AS c, sum(total) AS amt, count(DISTINCT currency) AS cur_n,
				min(currency) AS cur, max(created_at) AS last_at, count(DISTINCT branch_id) AS nb,
				(array_agg(id ORDER BY created_at DESC, id DESC))[1] AS last_id,
				(array_agg(id ORDER BY created_at DESC, id DESC) FILTER (WHERE dest_lat IS NOT NULL))[1] AS pin_id
			FROM f GROUP BY akey ORDER BY n DESC, last_at DESC LIMIT `+args.add(TopLimit)+`)
		SELECT g.akey, btrim(lo.address_line), b.city, g.n, g.c, CASE WHEN g.cur_n = 1 THEN g.amt END, g.cur, g.last_at,
			b.name, g.nb, po.dest_lat, po.dest_lng
		FROM g JOIN orders lo ON lo.id = g.last_id JOIN branches b ON b.id = lo.branch_id LEFT JOIN orders po ON po.id = g.pin_id
		ORDER BY g.n DESC, g.last_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	out.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (TopAddress, error) {
		var t TopAddress
		err := r.Scan(&t.Key, &t.Line, &t.City, &t.Orders, &t.Customers, &t.Amount, &t.Currency, &t.LastAt,
			&t.BranchName, &t.Branches, &t.Lat, &t.Lng)
		return t, err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TopUnit is one unit and customer of a ranked address.
type TopUnit struct {
	Unit       string `json:"unit"`
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Orders     int    `json:"orders"`
	BranchName string `json:"branch_name"`
}

// TopDetail splits one ranked address into its units and customers.
func (a *App) TopDetail(ctx context.Context, s DeliveryScope, r DeliveryRange, key string) ([]TopUnit, error) {
	args := sqlArgs{}
	f, err := filtered(s, r, &args)
	if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `WITH `+f+`, g AS (
			SELECT branch_id, lower(btrim(address_unit)) AS ukey, cust, count(*) AS n, max(created_at) AS last_at,
				(array_agg(id ORDER BY created_at DESC, id DESC))[1] AS last_id
			FROM f WHERE akey = `+args.add(key)+` GROUP BY 1, 2, 3)
		SELECT btrim(lo.address_unit), lo.customer_name, lo.customer_phone, g.n, b.name
		FROM g JOIN orders lo ON lo.id = g.last_id JOIN branches b ON b.id = g.branch_id
		ORDER BY g.n DESC, g.last_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (TopUnit, error) {
		var u TopUnit
		err := r.Scan(&u.Unit, &u.Name, &u.Phone, &u.Orders, &u.BranchName)
		return u, err
	})
}

// ---- the map ----

// MapPoint is an address on the map: N deliveries in the period.
type MapPoint struct {
	Key       string    `json:"key"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	N         int       `json:"n"`
	Customers int       `json:"c"`
	Line      string    `json:"line"`
	LastAt    time.Time `json:"last_at"`
}

// MapStore is a store drawn on the map.
type MapStore struct {
	ID   int64   `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

// MapZone is a delivery zone's outline (one store's map only).
type MapZone struct {
	ID      int64        `json:"id"`
	Name    string       `json:"name"`
	NameEn  string       `json:"name_en"`
	Color   string       `json:"color"`
	Polygon [][2]float64 `json:"polygon"`
}

type DeliveryMap struct {
	Points []MapPoint `json:"points"`
	// TooMany: more than MaxMapPoints addresses; Points is then empty.
	TooMany  bool       `json:"too_many"`
	Orders   int        `json:"orders"`
	Unpinned int        `json:"unpinned"`
	Stores   []MapStore `json:"stores"`
	Zones    []MapZone  `json:"zones"`
}

// DeliveryPoints is the period's addresses with a position, for the map.
func (a *App) DeliveryPoints(ctx context.Context, s DeliveryScope, r DeliveryRange) (*DeliveryMap, error) {
	args := sqlArgs{}
	f, err := filtered(s, r, &args)
	if err != nil {
		return nil, err
	}
	out := &DeliveryMap{Points: []MapPoint{}, Stores: []MapStore{}, Zones: []MapZone{}}
	rows, err := a.DB.Query(ctx, `WITH `+f+`, g AS (
			SELECT akey, count(*) AS n, count(DISTINCT cust) AS c, max(created_at) AS last_at,
				(array_agg(id ORDER BY created_at DESC, id DESC) FILTER (WHERE dest_lat IS NOT NULL))[1] AS pin_id
			FROM f GROUP BY akey)
		SELECT g.akey, po.dest_lat, po.dest_lng, g.n, g.c, btrim(po.address_line), g.last_at
		FROM g LEFT JOIN orders po ON po.id = g.pin_id
		ORDER BY g.n DESC, g.akey LIMIT `+args.add(MaxMapPoints*2), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p MapPoint
		var lat, lng *float64
		var line *string
		if err := rows.Scan(&p.Key, &lat, &lng, &p.N, &p.Customers, &line, &p.LastAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Orders += p.N
		if lat == nil || lng == nil {
			out.Unpinned++
			continue
		}
		p.Lat, p.Lng, p.Line = *lat, *lng, *line
		out.Points = append(out.Points, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Points) > MaxMapPoints {
		out.Points, out.TooMany = []MapPoint{}, true
	}

	sargs := sqlArgs{}
	sw := []string{"b.deleted_at IS NULL", "b.lat IS NOT NULL"}
	if s.BranchID != 0 {
		sw = append(sw, "b.id = "+sargs.add(s.BranchID))
	}
	if s.MerchantID != 0 {
		sw = append(sw, "b.merchant_id = "+sargs.add(s.MerchantID))
	}
	if s.CountryCode != "" {
		sw = append(sw, "b.country_code = "+sargs.add(strings.ToUpper(s.CountryCode)))
	}
	if s.City != "" {
		sw = append(sw, "b.city = "+sargs.add(s.City))
	}
	srows, err := a.DB.Query(ctx, `SELECT b.id, b.name, b.lat, b.lng FROM branches b WHERE `+strings.Join(sw, " AND ")+
		` ORDER BY b.id LIMIT 1000`, sargs...)
	if err != nil {
		return nil, err
	}
	if out.Stores, err = pgx.CollectRows(srows, pgx.RowToStructByPos[MapStore]); err != nil {
		return nil, err
	}
	if s.BranchID != 0 {
		zrows, err := a.DB.Query(ctx, `SELECT id, name, name_en, color, polygon FROM delivery_zones
			WHERE branch_id = $1 AND active AND polygon IS NOT NULL ORDER BY sort, id`, s.BranchID)
		if err != nil {
			return nil, err
		}
		if out.Zones, err = pgx.CollectRows(zrows, pgx.RowToStructByPos[MapZone]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---- the platform's filters ----

// StatsRegion is a country or city that has stores.
type StatsRegion struct {
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	City        string `json:"city"`
	Stores      int    `json:"stores"`
}

// StatsRegions lists the regions with stores, for the platform's filter.
func (a *App) StatsRegions(ctx context.Context) ([]StatsRegion, error) {
	rows, err := a.DB.Query(ctx, `SELECT country_code, max(country), city, count(*) FROM branches
		WHERE deleted_at IS NULL AND country_code <> '' GROUP BY country_code, city ORDER BY 1, 3`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[StatsRegion])
}
