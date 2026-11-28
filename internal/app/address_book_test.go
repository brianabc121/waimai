package app_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func TestAddressBook(t *testing.T) {
	s := newStore(t, false)
	other := newStore(t, false)
	ctx := context.Background()
	a := testApp
	city := uniq("Makati")
	_, err := a.DB.Exec(ctx, `UPDATE branches SET country_code='PH', country='Philippines', province='Metro Manila', city=$2
		WHERE id=$1`, s.branchID, city)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE branches SET country_code='PH', country='Philippines', province='Metro Manila', city=$2
		WHERE id=$1`, other.branchID, uniq("Pasig"))
	noErr(t, err)

	lat, lng := 14.5571, 121.0233
	order := func(no int, phone, line string, pin bool) {
		t.Helper()
		m := app.MemberInput{No: no, Phone: phone, Source: "wechat", AddressLine: line}
		if pin {
			m.Lat, m.Lng = &lat, &lng
		}
		_, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), app.StaffOrderInput{Member: m, ServiceType: "delivery",
			ZoneID: &s.zoneID, Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}}})
		noErr(t, err)
	}
	// The same address written two ways is one entry, used twice; no pin, no entry.
	order(0, "+639170000001", "Ayala Tower One ,  6750 Ayala Ave.", true)
	order(0, "+639170000002", "ayala tower one，６７５０ ayala ave", true)
	order(0, "+639170000003", "88 Unpinned Rd", false)
	reg := city
	l, err := a.ListBook(ctx, app.BookFilter{City: &reg})
	noErr(t, err)
	if l.Total != 1 || l.Items[0].Line != "Ayala Tower One, 6750 Ayala Ave" || l.Items[0].Uses != 2 ||
		l.Items[0].Source != "staff" || l.Items[0].Lat != lat {
		t.Fatalf("book after orders: %+v", l.Items)
	}

	// The order desk finds it by words in any order, and by a typo; another phone
	// sees it as a book address. Stores in another city do not.
	for _, q := range []string{"6750 ayala", "AYALA TOWR ONE", "tower one"} {
		hs, err := a.AddressHints(ctx, s.branchID, 0, q, "+639179999999")
		noErr(t, err)
		if len(hs) == 0 || hs[0].Kind != "book" || hs[0].Line != "Ayala Tower One, 6750 Ayala Ave" || hs[0].Lat == nil {
			t.Fatalf("hints for %q: %+v", q, hs)
		}
	}
	hs, err := a.AddressHints(ctx, s.branchID, 0, "ayala", "+639170000001")
	noErr(t, err)
	if len(hs) != 1 || hs[0].Kind != "mine" {
		t.Fatalf("the phone's own address comes once, as its own: %+v", hs)
	}
	hs, err = a.AddressHints(ctx, s.branchID, 0, "unpinned", "")
	noErr(t, err)
	if len(hs) != 1 || hs[0].Kind != "store" {
		t.Fatalf("past delivery without a pin: %+v", hs)
	}
	hs, err = a.AddressHints(ctx, other.branchID, 0, "ayala", "")
	noErr(t, err)
	if len(hs) != 0 {
		t.Fatalf("another city's store: %+v", hs)
	}

	// The platform's page: regions, add, duplicate, correct, delete.
	regs, err := a.BookRegions(ctx)
	noErr(t, err)
	found := false
	for _, r := range regs {
		found = found || (r.City == city && r.Count == 1 && r.CountryCode == "PH")
	}
	if !found {
		t.Fatalf("regions: %+v", regs)
	}
	admin := adminActor(t)
	in := app.BookInput{AddrRegion: app.AddrRegion{CountryCode: "ph", Country: "Philippines", Province: "Metro Manila", City: city},
		Line: "Greenbelt 5", Lat: 14.5525, Lng: 121.0218}
	g, err := a.SaveBookAddress(ctx, admin, 0, in)
	noErr(t, err)
	if g.Source != "admin" || g.CountryCode != "PH" {
		t.Fatalf("added: %+v", g)
	}
	in.Line = "AYALA TOWER ONE 6750 AYALA AVE"
	_, err = a.SaveBookAddress(ctx, admin, 0, in)
	wantCode(t, err, "DUPLICATE_ADDRESS")
	_, err = a.SaveBookAddress(ctx, admin, g.ID, in)
	wantCode(t, err, "DUPLICATE_ADDRESS")
	in.Line, in.Lat = "Greenbelt 5, Legazpi St", 0
	in.Lng = 0
	_, err = a.SaveBookAddress(ctx, admin, g.ID, in)
	wantCode(t, err, "PIN_REQUIRED")
	in.Lat, in.Lng = 14.5526, 121.0219
	g, err = a.SaveBookAddress(ctx, admin, g.ID, in)
	noErr(t, err)
	if g.Line != "Greenbelt 5, Legazpi St" || g.Lat != 14.5526 {
		t.Fatalf("corrected: %+v", g)
	}
	l, err = a.ListBook(ctx, app.BookFilter{City: &reg, Q: "legazpi"})
	noErr(t, err)
	if l.Total != 1 || l.Items[0].ID != g.ID {
		t.Fatalf("search: %+v", l)
	}
	noErr(t, a.DeleteBookAddress(ctx, admin, g.ID))
	wantCode(t, a.DeleteBookAddress(ctx, admin, g.ID), "NOT_FOUND")
	if auditCount(t, "address.delete") == 0 {
		t.Fatal("no audit")
	}
}

// A place known by name (docs/ADDRESS_PLACE_DESIGN.md): the name and Google's
// id travel with the address from the order desk to the order, the member's
// addresses, the address book and the next order's hints; the line stays the
// whole address. Money is not touched, which checked() confirms.
func TestPlaceName(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	city := uniq("Makati")
	_, err := a.DB.Exec(ctx, `UPDATE branches SET country_code='PH', country='Philippines', province='Metro Manila', city=$2
		WHERE id=$1`, s.branchID, city)
	noErr(t, err)
	lat, lng := 14.5601, 121.0253
	place := func(phone string, m app.MemberInput) *app.Order {
		t.Helper()
		m.Phone, m.Source, m.Lat, m.Lng = phone, "wechat", &lat, &lng
		p, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), app.StaffOrderInput{Member: m, ServiceType: "delivery",
			ZoneID: &s.zoneID, Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}}})
		noErr(t, err)
		checked(t, "staff order to a named place", p.Order.ID)
		o, err := a.GetOrder(ctx, p.Order.ID)
		noErr(t, err)
		return o
	}
	name, line := "Trump Tower at Century City", "Trump Tower at Century City, Kalayaan Ave cor. Salamanca St, Makati"
	o := place("+639170001001", app.MemberInput{AddressLine: line, AddressUnit: "32F", PlaceName: name, PlaceID: "ChIJ-tower"})
	if o.AddressLine != line || o.PlaceName != name || o.PlaceID != "ChIJ-tower" {
		t.Fatalf("order: line %q name %q id %q", o.AddressLine, o.PlaceName, o.PlaceID)
	}

	// The same place chosen from a suggestion worded differently is the same
	// entry of the book; it keeps the line it was first written with.
	place("+639170001002", app.MemberInput{AddressLine: "Trump Tower at Century City, Kalayaan Avenue, Poblacion", PlaceName: name, PlaceID: "ChIJ-tower"})
	l, err := a.ListBook(ctx, app.BookFilter{City: &city})
	noErr(t, err)
	if l.Total != 1 || l.Items[0].Line != line || l.Items[0].PlaceName != name || l.Items[0].PlaceID != "ChIJ-tower" || l.Items[0].Uses != 2 {
		t.Fatalf("book: %+v", l.Items)
	}

	// The next order is offered it with its name, from the book and as the phone's own.
	hs, err := a.AddressHints(ctx, s.branchID, 0, "trump", "+639179999999")
	noErr(t, err)
	if len(hs) == 0 || hs[0].Kind != "book" || hs[0].PlaceName != name || hs[0].PlaceID != "ChIJ-tower" {
		t.Fatalf("book hint: %+v", hs)
	}
	hs, err = a.AddressHints(ctx, s.branchID, 0, "trump", "+639170001001")
	noErr(t, err)
	if len(hs) == 0 || hs[0].Kind != "mine" || hs[0].PlaceName != name || hs[0].Unit != "32F" {
		t.Fatalf("own hint: %+v", hs)
	}

	// A line rewritten by hand no longer starts with the name: name and id are dropped.
	o = place("+639170001003", app.MemberInput{AddressLine: "Gate 2, Kalayaan Ave", PlaceName: name, PlaceID: "ChIJ-tower"})
	if o.PlaceName != "" || o.PlaceID != "" {
		t.Fatalf("stale place kept: %q %q", o.PlaceName, o.PlaceID)
	}
	// An address typed with no place has none; an older entry of the book takes
	// the name of the first order that brings one.
	place("+639170001004", app.MemberInput{AddressLine: "One Ayala, Ayala Ave"})
	place("+639170001005", app.MemberInput{AddressLine: "One Ayala, Ayala Ave", PlaceName: "One Ayala", PlaceID: "ChIJ-one"})
	q := "one ayala"
	l, err = a.ListBook(ctx, app.BookFilter{City: &city, Q: q})
	noErr(t, err)
	if l.Total != 1 || l.Items[0].PlaceName != "One Ayala" || l.Items[0].PlaceID != "ChIJ-one" || l.Items[0].Uses != 2 {
		t.Fatalf("older entry: %+v", l.Items)
	}

	// The platform names an entry: a name the line does not start with goes in
	// front of it; taking the name away takes Google's id with it.
	admin := adminActor(t)
	e := l.Items[0]
	in := app.BookInput{AddrRegion: e.AddrRegion, Line: "Ayala Ave cor. Makati Ave", PlaceName: "One Ayala", Lat: e.Lat, Lng: e.Lng}
	g, err := a.SaveBookAddress(ctx, admin, e.ID, in)
	noErr(t, err)
	if g.Line != "One Ayala, Ayala Ave cor. Makati Ave" || g.PlaceName != "One Ayala" || g.PlaceID != "ChIJ-one" {
		t.Fatalf("named by the platform: %+v", g)
	}
	in.PlaceName = ""
	g, err = a.SaveBookAddress(ctx, admin, e.ID, in)
	noErr(t, err)
	if g.PlaceName != "" || g.PlaceID != "" {
		t.Fatalf("name removed: %+v", g)
	}
}

// One module answers staff and customers (SuggestAddresses). A customer is
// offered only the book's public places: those Google knows by name and
// those the platform wrote, never a line staff typed for one customer, and
// never anybody's unit, note or delivery area.
func TestPublicAddressSuggest(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	city := uniq("Pasig")
	_, err := a.DB.Exec(ctx, `UPDATE branches SET country_code='PH', country='Philippines', province='Metro Manila', city=$2
		WHERE id=$1`, s.branchID, city)
	noErr(t, err)
	// far from every other test's addresses, so "around a point" finds only these
	lat, lng := -41.2865, 174.7762
	place := func(phone string, m app.MemberInput) {
		t.Helper()
		m.Phone, m.Source, m.Lat, m.Lng = phone, "wechat", &lat, &lng
		p, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), app.StaffOrderInput{Member: m, ServiceType: "delivery",
			ZoneID: &s.zoneID, Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}}})
		noErr(t, err)
		checked(t, "staff order for the address book", p.Order.ID)
	}
	tag := uniq("Zephyr")
	name := tag + " Tower"
	place("+639170002001", app.MemberInput{AddressLine: name + ", Meralco Ave", AddressUnit: "18F Unit 1803", AddressNote: "call first",
		PlaceName: name, PlaceID: "ChIJ-" + tag})
	place("+639170002002", app.MemberInput{AddressLine: tag + " house, 12 Private Lane", AddressUnit: "back door"})
	_, err = a.SaveBookAddress(ctx, adminActor(t), 0, app.BookInput{AddrRegion: app.AddrRegion{CountryCode: "PH", Country: "Philippines",
		Province: "Metro Manila", City: city}, Line: tag + " Mall, Ortigas Ave", Lat: lat, Lng: lng})
	noErr(t, err)

	lines := func(hs []app.AddressHint) string {
		var out []string
		for _, h := range hs {
			out = append(out, h.Line)
		}
		sort.Strings(out)
		return strings.Join(out, " | ")
	}
	want := tag + " Mall, Ortigas Ave | " + name + ", Meralco Ave"

	// Staff see all three.
	hs, err := a.SuggestAddresses(ctx, app.AddressAsk{Q: tag, Staff: true, BranchID: s.branchID})
	noErr(t, err)
	if len(hs) != 3 {
		t.Fatalf("staff: %s", lines(hs))
	}
	// A customer at the store sees the place and the platform's entry, bare.
	for _, ask := range []app.AddressAsk{{Q: tag, Slug: s.slug}, {Q: tag, Near: &app.LatLng{lat + 0.01, lng - 0.01}}} {
		hs, err = a.SuggestAddresses(ctx, ask)
		noErr(t, err)
		if lines(hs) != want {
			t.Fatalf("customer %+v: %s", ask, lines(hs))
		}
		for _, h := range hs {
			if h.Unit != "" || h.Note != "" || h.Mine || h.Kind != "book" || h.ZoneName != "" || h.Fee != nil || h.Lat == nil {
				t.Fatalf("customer sees more than a place: %+v", h)
			}
		}
	}
	// Staff arguments mean nothing without Staff: the HTTP layer sets it only after checking the session.
	hs, err = a.SuggestAddresses(ctx, app.AddressAsk{Q: tag, BranchID: s.branchID, Phone: "+639170002002", Slug: s.slug})
	noErr(t, err)
	if lines(hs) != want {
		t.Fatalf("customer with staff arguments: %s", lines(hs))
	}
	// Nothing for one letter, for a point far away, or with neither store nor point.
	for _, ask := range []app.AddressAsk{{Q: "Z", Slug: s.slug}, {Q: tag, Near: &app.LatLng{lat + 2, lng}}, {Q: tag}, {Q: tag, Near: &app.LatLng{999, 0}}} {
		hs, err = a.SuggestAddresses(ctx, ask)
		noErr(t, err)
		if len(hs) != 0 {
			t.Fatalf("%+v: %s", ask, lines(hs))
		}
	}
	if _, err = a.SuggestAddresses(ctx, app.AddressAsk{Q: tag, Slug: uniq("no-such-store")}); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("unknown store: %v", err)
	}
}
