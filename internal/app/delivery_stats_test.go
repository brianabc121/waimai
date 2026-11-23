package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func TestDeliveryStats(t *testing.T) {
	s := newStore(t, false)
	other := newStore(t, false)
	ctx := context.Background()
	a := testApp
	city := uniq("Makati")
	_, err := a.DB.Exec(ctx, `UPDATE branches SET country_code='PH', country='Philippines', city=$2, lat=14.55, lng=121.02,
		timezone='Asia/Manila' WHERE id=$1`, s.branchID, city)
	noErr(t, err)

	lat, lng := 14.5571, 121.0233
	// order places a delivery and then sets what the test needs: who, when
	// (Manila time) and what became of it.
	order := func(st *store, phone, line, unit string, pin bool, at, acceptance, fulfillment string) int64 {
		t.Helper()
		m := app.MemberInput{Phone: phone, Source: "wechat", AddressLine: line, AddressUnit: unit}
		if pin {
			m.Lat, m.Lng = &lat, &lng
		}
		o, err := a.PlaceStaffOrder(ctx, st.branchID, st.staff, uniq("k"), app.StaffOrderInput{Member: m, ServiceType: "delivery",
			ZoneID: &st.zoneID, Items: []app.StaffLine{{ItemID: st.tea, Qty: 1}}})
		noErr(t, err)
		_, err = a.DB.Exec(ctx, `UPDATE orders SET created_at = ($2::timestamp AT TIME ZONE 'Asia/Manila'),
			acceptance_status=$3, fulfillment_status=$4, customer_phone=$5,
			dest_lat = CASE WHEN $6 THEN $7::float8 END, dest_lng = CASE WHEN $6 THEN $8::float8 END WHERE id=$1`,
			o.Order.ID, at, acceptance, fulfillment, phone, pin, lat, lng)
		noErr(t, err)
		return o.Order.ID
	}
	p1, p2, p3 := "+639175550001", "+639175550002", "+639175550003"
	order(s, p1, "Ayala Tower One, 6750 Ayala Ave", "23F", true, "2026-11-10 10:00", "accepted", "delivered")
	order(s, p2, "ayala tower one，６７５０ ayala ave.", "18F", true, "2026-11-11 12:00", "accepted", "delivered")
	order(s, p1, "Ayala Tower One, 6750 Ayala Ave", "23f ", true, "2026-11-12 12:00", "accepted", "delivered")
	order(s, p3, "88 Unpinned Rd", "", false, "2026-11-12 13:00", "accepted", "delivered")
	order(s, p3, "1 Declined St", "", true, "2026-11-12 13:00", "rejected", "none")
	order(s, p3, "2 Failed St", "", true, "2026-11-12 13:00", "accepted", "delivery_failed")
	// The last minutes of the 20th in Manila count for the 20th; the first of the 21st do not.
	order(s, p1, "Ayala Tower One, 6750 Ayala Ave", "23F", true, "2026-11-20 23:30", "accepted", "out_for_delivery")
	order(s, p1, "Ayala Tower One, 6750 Ayala Ave", "23F", true, "2026-11-21 00:30", "accepted", "delivered")
	order(other, p1, "Ayala Tower One, 6750 Ayala Ave", "23F", true, "2026-11-12 12:00", "accepted", "delivered")

	own := app.DeliveryScope{BranchID: s.branchID}
	nov := app.DeliveryRange{From: "2026-11-01", To: "2026-11-20"}

	// The ranking: one address for every way it was written, its units together.
	top, err := a.TopAddresses(ctx, own, nov)
	noErr(t, err)
	if top.Orders != 5 || top.Addresses != 2 || top.Unpinned != 1 || len(top.Items) != 2 {
		t.Fatalf("top summary: %+v", top)
	}
	first := top.Items[0]
	if first.Orders != 4 || first.Customers != 2 || first.Line != "Ayala Tower One, 6750 Ayala Ave" || first.Lat == nil ||
		first.Amount == nil || first.Currency == "" || first.Branches != 1 || first.City != city {
		t.Fatalf("top address: %+v", first)
	}
	if top.Items[1].Orders != 1 || top.Items[1].Lat != nil {
		t.Fatalf("unpinned address: %+v", top.Items[1])
	}
	units, err := a.TopDetail(ctx, own, nov, first.Key)
	noErr(t, err)
	if len(units) != 2 || units[0].Orders != 3 || units[0].Phone != p1 || units[1].Orders != 1 || units[1].Unit != "18F" {
		t.Fatalf("units: %+v", units)
	}

	// At most 31 days.
	_, err = a.TopAddresses(ctx, own, app.DeliveryRange{From: "2026-11-01", To: "2026-12-01"})
	noErr(t, err)
	_, err = a.TopAddresses(ctx, own, app.DeliveryRange{From: "2026-11-01", To: "2026-12-02"})
	wantCode(t, err, "RANGE_TOO_LONG")
	_, err = a.DeliveryPoints(ctx, own, app.DeliveryRange{From: "2026-11-20", To: "2026-11-01"})
	wantCode(t, err, "BAD_RANGE")
	_, err = a.TopAddresses(ctx, own, app.DeliveryRange{From: "11/01", To: "2026-11-20"})
	wantCode(t, err, "BAD_RANGE")

	// The map: pinned addresses only, the rest counted; the store is on it.
	m, err := a.DeliveryPoints(ctx, own, nov)
	noErr(t, err)
	if len(m.Points) != 1 || m.Points[0].N != 4 || m.Points[0].Lat != lat || m.Unpinned != 1 || m.Orders != 5 ||
		len(m.Stores) != 1 || m.TooMany {
		t.Fatalf("map: %+v", m)
	}

	// The address list: every customer's every address and unit, any time.
	l, err := a.CustomerAddresses(ctx, app.CustomerAddressFilter{DeliveryScope: own})
	noErr(t, err)
	if l.Total != 3 || l.Customers != 3 || l.Addresses != 2 {
		t.Fatalf("list: %+v", l)
	}
	row := l.Items[0]
	if row.Phone != p1 || row.Orders != 4 || row.Unit != "23F" || !row.ByStaff || row.ByCustomer || row.Lat == nil || row.MemberNo == nil {
		t.Fatalf("latest row: %+v", row)
	}
	for q, want := range map[string]int{"6750 ayala": 2, "TOWER one": 2, "5550003": 1, "unpinned": 1, "nowhere": 0} {
		l, err := a.CustomerAddresses(ctx, app.CustomerAddressFilter{DeliveryScope: own, Q: q})
		noErr(t, err)
		if l.Total != want {
			t.Fatalf("search %q: %d, want %d", q, l.Total, want)
		}
	}
	l, err = a.CustomerAddresses(ctx, app.CustomerAddressFilter{DeliveryScope: own, Source: "customer"})
	noErr(t, err)
	if l.Total != 0 {
		t.Fatalf("customers' own orders: %+v", l)
	}
	l, err = a.CustomerAddresses(ctx, app.CustomerAddressFilter{DeliveryScope: own, Sort: "orders"})
	noErr(t, err)
	if l.Items[0].Orders != 4 {
		t.Fatalf("by orders: %+v", l.Items)
	}
	h, err := a.AddressOrders(ctx, s.branchID, row.Cust, row.Key, row.UnitKey, app.Page{})
	noErr(t, err)
	if h.Total != 4 {
		t.Fatalf("orders at the address: %+v", h)
	}

	// The platform: by country and city, across stores.
	all, err := a.TopAddresses(ctx, app.DeliveryScope{CountryCode: "ph", City: city}, nov)
	noErr(t, err)
	if all.Orders != 5 {
		t.Fatalf("by city: %+v", all)
	}
	both, err := a.CustomerAddresses(ctx, app.CustomerAddressFilter{Q: "+639175550001"})
	noErr(t, err)
	if both.Total < 2 {
		t.Fatalf("every store: %+v", both)
	}
	regs, err := a.StatsRegions(ctx)
	noErr(t, err)
	found := false
	for _, r := range regs {
		found = found || (r.City == city && r.CountryCode == "PH" && r.Stores == 1)
	}
	if !found {
		t.Fatalf("regions: %+v", regs)
	}
}
