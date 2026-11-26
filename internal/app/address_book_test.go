package app_test

import (
	"context"
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
