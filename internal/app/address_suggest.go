package app

import (
	"context"
	"math"
	"strings"
)

// Address suggestions, one module for everyone who types an address
// (docs/ADDRESS_BOOK_DESIGN.md section 11): staff at the order desk and
// customers choosing where to deliver. Both are offered the platform's
// address book first; the page asks Google only when nothing here matches
// or the person asks for more.
//
// What differs is what each may see. Staff see the addresses of their own
// store's customers. A customer sees only the book's public places (see
// publicBook): never another customer's unit, note, phone, or an address
// somebody typed by hand, and nothing about delivery areas.

// AddressAsk is one request for suggestions. Staff says who asks.
type AddressAsk struct {
	Q string
	// the order desk: the store, the brand, and the phone the order is for
	Staff    bool
	BranchID int64
	BrandID  int64
	Phone    string
	// a customer: the store being ordered from, else the point the map is at
	Slug string
	Near *LatLng
}

// SuggestAddresses answers an AddressAsk.
func (a *App) SuggestAddresses(ctx context.Context, ask AddressAsk) ([]AddressHint, error) {
	q := strings.TrimSpace(ask.Q)
	if r := []rune(q); len(r) > 100 {
		q = string(r[:100])
	}
	if ask.Staff {
		return a.deskAddresses(ctx, ask.BranchID, ask.BrandID, q, strings.TrimSpace(ask.Phone))
	}
	return a.publicAddresses(ctx, q, ask.Slug, ask.Near)
}

// AddressHints is SuggestAddresses for the order desk.
func (a *App) AddressHints(ctx context.Context, branchID, brandID int64, q, phone string) ([]AddressHint, error) {
	return a.SuggestAddresses(ctx, AddressAsk{Q: q, Staff: true, BranchID: branchID, BrandID: brandID, Phone: phone})
}

// publicAddresses lists the book's public places matching q: in the store's
// region when a store is named, else around a point. At least two letters,
// so the book cannot be read out a letter at a time.
func (a *App) publicAddresses(ctx context.Context, q, slug string, near *LatLng) ([]AddressHint, error) {
	out := []AddressHint{}
	if len([]rune(q)) < 2 {
		return out, nil
	}
	sc := bookScope{Public: true}
	if slug != "" {
		b, err := branchBySlug(ctx, a.DB, slug)
		if err != nil {
			return nil, err
		}
		r := branchRegion(b)
		sc.Region = &r
	} else if near != nil && math.Abs(near[0]) <= 90 && math.Abs(near[1]) <= 180 {
		sc.Near = near
	} else {
		return out, nil
	}
	book, err := searchBook(ctx, a.DB, sc, q, 6)
	if err != nil {
		return nil, err
	}
	for _, x := range book {
		lat, lng := x.Lat, x.Lng
		out = append(out, AddressHint{Line: x.Line, Lat: &lat, Lng: &lng, Kind: "book", PlaceName: x.PlaceName, PlaceID: x.PlaceID})
	}
	return out, nil
}

type AddressHint struct {
	Line string   `json:"line"`
	Unit string   `json:"unit"`
	Note string   `json:"note"`
	Lat  *float64 `json:"lat"`
	Lng  *float64 `json:"lng"`
	// PlaceName: the leading part of Line that names the place; PlaceID: Google's id of it.
	PlaceName string `json:"place_name"`
	PlaceID   string `json:"place_id"`
	// Kind: "mine" (this phone number), "book" (the address book) or "store".
	Kind string `json:"kind"`
	// Mine: this phone number used it before (Kind "mine").
	Mine bool `json:"mine"`
	// The delivery area and fee at the pin, when it has one.
	ZoneName string `json:"zone_name"`
	Fee      *int64 `json:"fee"`
}

// deskAddresses lists, for what staff typed: the phone's own addresses, then
// the address book's in the store's region, then the store's past deliveries
// not already among those.
func (a *App) deskAddresses(ctx context.Context, branchID, brandID int64, q, phone string) ([]AddressHint, error) {
	if q == "" && phone == "" {
		return []AddressHint{}, nil
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	// The phone's own addresses come from this brand's orders and members
	// only (brands do not share members); other deliveries of the store are
	// offered without saying whose they were.
	brandID, err = memberBrand(ctx, a.DB, branchID, brandID)
	if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT line, unit, note, lat, lng, mine, addr_key(line), pname, pid FROM (
			SELECT DISTINCT ON (addr_key(line)) line, unit, note, lat, lng, mine, at, pname, pid FROM (
				SELECT address_line AS line, address_unit AS unit, address_note AS note, dest_lat AS lat, dest_lng AS lng,
					($3 <> '' AND customer_phone = $3 AND brand_id = $4) AS mine, created_at AS at, place_name AS pname, place_id AS pid
				FROM orders WHERE branch_id=$1 AND service_type='delivery' AND address_line <> ''
					AND ($2 = '' OR address_line ILIKE '%' || $2 || '%' OR addr_key(address_line) LIKE '%' || addr_key($2) || '%')
				UNION ALL
				SELECT address_line, address_unit, address_note, lat, lng, ($3 <> '' AND phone = $3), updated_at, place_name, place_id
				FROM store_members WHERE brand_id=$4 AND address_line <> '' AND `+liveMember+`
					AND ($2 = '' OR address_line ILIKE '%' || $2 || '%' OR addr_key(address_line) LIKE '%' || addr_key($2) || '%')
				UNION ALL
				SELECT a.address_line, a.address_unit, a.address_note, a.lat, a.lng, ($3 <> '' AND m.phone = $3), a.last_used_at,
					a.place_name, a.place_id
				FROM store_member_addresses a JOIN store_members m ON m.id = a.member_id
				WHERE m.brand_id=$4 AND `+liveMember+` AND ($2 = '' OR a.address_line ILIKE '%' || $2 || '%'
					OR addr_key(a.address_line) LIKE '%' || addr_key($2) || '%')
			) x WHERE addr_key(line) <> '' AND ($2 <> '' OR mine)
			ORDER BY addr_key(line), mine DESC, at DESC
		) y ORDER BY mine DESC, at DESC LIMIT 12`, branchID, q, phone, brandID)
	if err != nil {
		return nil, err
	}
	var mine, store []AddressHint
	seen := map[string]bool{}
	var keys []string
	for rows.Next() {
		var h AddressHint
		var key string
		if err := rows.Scan(&h.Line, &h.Unit, &h.Note, &h.Lat, &h.Lng, &h.Mine, &key, &h.PlaceName, &h.PlaceID); err != nil {
			rows.Close()
			return nil, err
		}
		if h.Mine {
			h.Kind = "mine"
			seen[key] = true
			mine = append(mine, h)
		} else {
			h.Kind = "store"
			keys = append(keys, key)
			store = append(store, h)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(mine) > 4 {
		mine = mine[:4]
	}
	out := mine
	if q != "" {
		r := branchRegion(b)
		book, err := searchBook(ctx, a.DB, bookScope{Region: &r}, q, 6)
		if err != nil {
			return nil, err
		}
		for _, x := range book {
			if seen[x.key] {
				continue
			}
			seen[x.key] = true
			lat, lng := x.Lat, x.Lng
			out = append(out, AddressHint{Line: x.Line, Lat: &lat, Lng: &lng, Kind: "book", PlaceName: x.PlaceName, PlaceID: x.PlaceID})
		}
	}
	for i, h := range store {
		if len(out) >= 10 {
			break
		}
		if !seen[keys[i]] {
			seen[keys[i]] = true
			out = append(out, h)
		}
	}
	if out == nil {
		out = []AddressHint{}
	}
	zones, err := loadZones(ctx, a.DB, branchID, true)
	if err != nil {
		return nil, err
	}
	for i := range out {
		h := &out[i]
		if h.Lat == nil || h.Lng == nil {
			continue
		}
		if hit, _ := matchZone(zones, LatLng{*h.Lat, *h.Lng}, b.ZoneToleranceM); hit != nil {
			fee := hit.Zone.Fee
			h.ZoneName, h.Fee = hit.Zone.Name, &fee
		}
	}
	return out, nil
}
