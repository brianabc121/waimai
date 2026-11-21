package app_test

import (
	"context"
	"math"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

var mapOrigin = app.LatLng{14.55, 121.02}

// m moves from the test origin by metres east and north.
func m(east, north float64) app.LatLng {
	const perDeg = 111320.0
	return app.LatLng{mapOrigin[0] + north/perDeg, mapOrigin[1] + east/(perDeg*math.Cos(mapOrigin[0]*math.Pi/180))}
}

func box(x0, y0, x1, y1 float64) app.Polygon {
	return app.Polygon{m(x0, y0), m(x1, y0), m(x1, y1), m(x0, y1)}
}

func (s *store) quoteAt(service string, p app.LatLng) (*app.Quote, error) {
	return testApp.CreateQuote(context.Background(), s.slug, app.QuoteInput{ServiceType: service, Lat: &p[0], Lng: &p[1],
		Items: s.cart()}, "+639170000001")
}

func TestMapZones(t *testing.T) {
	s := newStore(t, true)
	a, ctx := testApp, context.Background()

	// Map mode needs a store location and a drawn zone first.
	_, err := a.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "map", ZoneToleranceM: 200})
	wantCode(t, err, "MAP_NOT_READY")
	_, err = a.SaveLocation(ctx, s.branchID, app.LocationInput{Lat: 95, Lng: 0})
	wantCode(t, err, "VALIDATION")
	store := m(500, 500)
	b, err := a.SaveLocation(ctx, s.branchID, app.LocationInput{Lat: store[0], Lng: store[1], Address: "1 Map St"})
	noErr(t, err)
	if b.Lat == nil || *b.Lat != store[0] || b.Address != "1 Map St" || b.ZoneMode != "list" || b.ZoneToleranceM != 200 {
		t.Fatalf("branch after location: %+v", b)
	}
	_, err = a.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "map", ZoneToleranceM: 200})
	wantCode(t, err, "MAP_NOT_READY") // the only zone has no outline yet

	// Outlines are validated.
	eta := 25
	near := app.ZoneInput{Name: "near", Fee: 4900, MinOrder: 20000, Active: true, Polygon: box(0, 0, 1000, 1000),
		Color: "#2a78d6", EtaMinutes: &eta}
	bow := near
	bow.Polygon = app.Polygon{m(0, 0), m(1000, 1000), m(1000, 0), m(0, 1000)}
	_, err = a.SaveZone(ctx, s.branchID, 0, bow)
	wantCode(t, err, "ZONE_SELF_INTERSECTS")
	far := near
	far.Polygon = box(50000, 0, 51000, 1000)
	_, err = a.SaveZone(ctx, s.branchID, 0, far)
	wantCode(t, err, "VALIDATION")
	badColor := near
	badColor.Color = "blue"
	_, err = a.SaveZone(ctx, s.branchID, 0, badColor)
	wantCode(t, err, "VALIDATION")

	nearID, err := a.SaveZone(ctx, s.branchID, 0, near)
	noErr(t, err)
	// "wide" overlaps near's right half and leaves a 50 m gap to "east".
	wideID, err := a.SaveZone(ctx, s.branchID, 0, app.ZoneInput{Name: "wide", Fee: 9900, Active: true,
		Polygon: box(500, 0, 2000, 1000)})
	noErr(t, err)
	eastID, err := a.SaveZone(ctx, s.branchID, 0, app.ZoneInput{Name: "east", Fee: 12900, Active: true,
		Polygon: box(2050, 0, 3000, 1000)})
	noErr(t, err)

	// The priority order must name every zone exactly once.
	wantCode(t, a.ReorderZones(ctx, s.branchID, []int64{nearID, wideID}), "VALIDATION")
	wantCode(t, a.ReorderZones(ctx, s.branchID, []int64{nearID, nearID, wideID, eastID}), "VALIDATION")
	noErr(t, a.ReorderZones(ctx, s.branchID, []int64{nearID, wideID, eastID, s.zoneID}))

	menu, err := a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	if len(menu.Zones) != 4 || menu.Zones[0].ID != nearID || len(menu.Zones[0].Polygon) != 4 ||
		menu.Zones[0].Color != "#2a78d6" || *menu.Zones[0].EtaMinutes != 25 || menu.Zones[3].Polygon == nil ||
		len(menu.Zones[3].Polygon) != 0 {
		t.Fatalf("zones: %+v", menu.Zones)
	}

	// Still in list mode: the pin is ignored and the chosen zone is used.
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "delivery", ZoneID: &s.zoneID, Lat: &store[0],
		Lng: &store[1], Items: s.cart()}, "")
	noErr(t, err)
	if q.ZoneName != "Z" || q.Totals.DeliveryFee != 6900 {
		t.Fatalf("list mode quote: %+v", q)
	}

	b, err = a.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "map", ZoneToleranceM: 200})
	noErr(t, err)
	if b.ZoneMode != "map" {
		t.Fatalf("zone mode: %+v", b)
	}

	// Map mode: a zone id from the browser means nothing without a pin.
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "delivery", ZoneID: &s.zoneID, Items: s.cart()}, "")
	wantCode(t, err, "OUT_OF_ZONE")
	// ... and is ignored with one: the pin is inside "near", the id says "east".
	p := m(200, 500)
	q, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "delivery", ZoneID: &eastID, Lat: &p[0], Lng: &p[1],
		Items: s.cart()}, "")
	noErr(t, err)
	if q.ZoneName != "near" || q.Totals.DeliveryFee != 4900 || q.ZoneDistanceM != nil {
		t.Fatalf("pin inside near: %+v", q)
	}

	fee := func(name string, at app.LatLng, zone string, want int64, dist int) {
		t.Helper()
		q, err := s.quoteAt("delivery", at)
		noErr(t, err)
		got := 0
		if q.ZoneDistanceM != nil {
			got = *q.ZoneDistanceM
		}
		if q.ZoneName != zone || q.Totals.DeliveryFee != want || got != dist {
			t.Fatalf("%s: zone %q fee %d distance %d, want %q %d %d", name, q.ZoneName, q.Totals.DeliveryFee, got, zone, want, dist)
		}
	}
	fee("overlap goes to the earlier zone", m(750, 500), "near", 4900, 0)
	fee("only in wide", m(1500, 500), "wide", 9900, 0)
	fee("gap, nearer to wide", m(2010, 500), "wide", 9900, 10)
	fee("gap, nearer to east", m(2040, 500), "east", 12900, 10)
	fee("outside the outer edge, within tolerance", m(-150, 500), "near", 4900, 150)
	_, err = s.quoteAt("delivery", m(-400, 500))
	wantCode(t, err, "OUT_OF_ZONE")
	// Pickup needs no pin.
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: s.cart()}, "")
	noErr(t, err)

	loc, err := a.Locate(ctx, s.slug, m(-400, 500)[0], m(-400, 500)[1])
	noErr(t, err)
	if loc.Zone != nil || loc.Nearest == nil || loc.Nearest.Name != "near" || loc.Nearest.DistanceM != 400 {
		t.Fatalf("locate out of range: %+v %+v", loc, loc.Nearest)
	}
	loc, err = a.Locate(ctx, s.slug, m(2010, 500)[0], m(2010, 500)[1])
	noErr(t, err)
	if loc.Zone == nil || loc.Zone.Name != "wide" || loc.DistanceM != 10 {
		t.Fatalf("locate in the gap: %+v", loc)
	}

	// Reordering changes who owns the overlap; an old quote must be confirmed again.
	old, err := s.quoteAt("delivery", m(750, 500))
	noErr(t, err)
	noErr(t, a.ReorderZones(ctx, s.branchID, []int64{wideID, nearID, eastID, s.zoneID}))
	fee("overlap after reordering", m(750, 500), "wide", 9900, 0)
	in := app.PlaceOrderInput{QuoteID: old.ID, CustomerName: "Ann", AddressLine: "1 Main St"}
	_, err = a.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"), in)
	wantCode(t, err, "QUOTE_CHANGED")

	// A disabled zone steps aside: the zone below takes the overlap.
	_, err = a.SaveZone(ctx, s.branchID, wideID, app.ZoneInput{Name: "wide", Fee: 9900, Active: false,
		Polygon: box(500, 0, 2000, 1000)})
	noErr(t, err)
	fee("overlap with wide disabled", m(750, 500), "near", 4900, 0)

	// Zero tolerance: strictly inside or nothing.
	_, err = a.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "map", ZoneToleranceM: 0})
	noErr(t, err)
	_, err = s.quoteAt("delivery", m(-10, 500))
	wantCode(t, err, "OUT_OF_ZONE")
	_, err = a.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "map", ZoneToleranceM: 501})
	wantCode(t, err, "VALIDATION")
	_, err = a.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "map", ZoneToleranceM: 200})
	noErr(t, err)

	// The order remembers the pin and how far outside the outline it was.
	at := m(-150, 500)
	q, err = s.quoteAt("delivery", at)
	noErr(t, err)
	placed, err := a.PlaceOrder(ctx, s.slug, "+639170000001", uniq("k"),
		app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main St"})
	noErr(t, err)
	o, err := a.OrderByToken(ctx, placed.AccessToken)
	noErr(t, err)
	if o.ZoneName != "near" || o.DestLat == nil || *o.DestLat != at[0] || *o.DestLng != at[1] ||
		o.ZoneDistanceM == nil || *o.ZoneDistanceM != 150 {
		t.Fatalf("order location: %+v", o)
	}

	// Deleting a zone keeps history.
	noErr(t, a.DeleteZone(ctx, s.branchID, eastID))
	wantCode(t, a.DeleteZone(ctx, s.branchID, eastID), "NOT_FOUND")
	other := newStore(t, true)
	wantCode(t, a.DeleteZone(ctx, other.branchID, nearID), "NOT_FOUND")
	_, err = a.SaveZone(ctx, other.branchID, nearID, near)
	wantCode(t, err, "NOT_FOUND")
	o, err = a.OrderByToken(ctx, placed.AccessToken)
	noErr(t, err)
	if o.ZoneName != "near" {
		t.Fatalf("order after zone changes: %+v", o)
	}

	// Back to list mode: the list works again and outlines stay.
	_, err = a.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "list", ZoneToleranceM: 200})
	noErr(t, err)
	q = s.quote(t, "delivery", s.cart())
	if q.ZoneName != "Z" {
		t.Fatalf("list mode again: %+v", q)
	}
	menu, err = a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	if len(menu.Zones) != 3 || len(menu.Zones[0].Polygon) != 4 {
		t.Fatalf("zones after switching back: %+v", menu.Zones)
	}
}

// Without a platform map key customers cannot pin an address: a store that
// was switched to the map still sells by zone list.
func TestMapModeNeedsPlatformKey(t *testing.T) {
	s := newStore(t, true)
	a, ctx := testApp, context.Background()
	store := m(500, 500)
	_, err := a.SaveLocation(ctx, s.branchID, app.LocationInput{Lat: store[0], Lng: store[1]})
	noErr(t, err)
	id, err := a.SaveZone(ctx, s.branchID, 0, app.ZoneInput{Name: "drawn", Fee: 1000, Active: true, Polygon: box(0, 0, 1000, 1000)})
	noErr(t, err)
	_, err = a.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "map", ZoneToleranceM: 200})
	noErr(t, err)

	off := *a
	off.MapsOff = true
	sf, err := off.Storefront(ctx, s.slug)
	noErr(t, err)
	if sf.Branch.ZoneMode != "list" {
		t.Fatalf("storefront zone mode = %q, want list", sf.Branch.ZoneMode)
	}
	in := app.QuoteInput{ServiceType: "delivery", ZoneID: &id, Items: s.cart()}
	_, err = off.CreateQuote(ctx, s.slug, in, "+639170000001")
	noErr(t, err)
	_, err = a.CreateQuote(ctx, s.slug, in, "+639170000001")
	wantCode(t, err, "OUT_OF_ZONE") // with a key the pin is required

	_, err = a.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "list", ZoneToleranceM: 200})
	noErr(t, err)
	_, err = off.SaveZoneMode(ctx, s.branchID, app.ZoneModeInput{ZoneMode: "map", ZoneToleranceM: 200})
	wantCode(t, err, "MAPS_DISABLED")
}
