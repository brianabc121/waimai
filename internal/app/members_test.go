package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func TestStoreMembersAndStaffOrders(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp

	// "New member" takes the next number and holds it.
	m1, err := a.NewMember(ctx, s.branchID, 0)
	noErr(t, err)
	m2, err := a.NewMember(ctx, s.branchID, 0)
	noErr(t, err)
	if m1.No != 1 || m2.No != 2 || m1.Status != "temp" || m1.ExpiresAt == nil {
		t.Fatalf("new members: %+v %+v", m1, m2)
	}
	// An hour later, the unused number at the top comes back.
	_, err = a.DB.Exec(ctx, `UPDATE store_members SET expires_at = now() - interval '1 minute' WHERE id=$1`, m2.ID)
	noErr(t, err)
	_, err = a.GetMember(ctx, s.branchID, 2)
	wantCode(t, err, "NOT_FOUND")
	m3, err := a.NewMember(ctx, s.branchID, 0)
	noErr(t, err)
	if m3.No != 2 {
		t.Fatalf("recycled number: %d", m3.No)
	}

	// Filling in the details: phone and where they asked from are required, the name is not.
	_, err = a.SaveMember(ctx, s.branchID, 0, app.MemberInput{No: 1, Source: "wechat"})
	wantCode(t, err, "PHONE_REQUIRED")
	_, err = a.SaveMember(ctx, s.branchID, 0, app.MemberInput{No: 1, Phone: "+63 917 555 2046"})
	wantCode(t, err, "SOURCE_REQUIRED")
	saved, err := a.SaveMember(ctx, s.branchID, 0, app.MemberInput{No: 1, Phone: "+63 917 555 2046", Source: "wechat", Contact: "chen"})
	noErr(t, err)
	if saved.Status != "active" || saved.ExpiresAt != nil || saved.Phone != "+639175552046" {
		t.Fatalf("saved member: %+v", saved)
	}
	found, err := a.SearchMembers(ctx, s.branchID, "0000")
	noErr(t, err)
	if len(found) != 2 {
		t.Fatalf("search by number: %d", len(found))
	}
	found, err = a.SearchMembers(ctx, s.branchID, "5552046")
	noErr(t, err)
	if len(found) != 1 || found[0].No != 1 {
		t.Fatalf("search by phone: %+v", found)
	}

	// Ordering for member 2 (still temporary) with two extra portions of an option.
	in := app.StaffOrderInput{
		Member:      app.MemberInput{No: 2, Phone: "+639450001111", Source: "whatsapp", Contact: "+639450001111", AddressLine: "2621 Zamora St"},
		ServiceType: "delivery", ZoneID: &s.zoneID, Accept: true, StaffNote: "asked twice",
		Items: []app.StaffLine{{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.tea, Qty: 2, Note: "less ice"}},
	}
	q, err := a.StaffQuote(ctx, s.branchID, in)
	noErr(t, err)
	if q.Totals.DeliveryFee != 6900 || len(q.Coupons) != 0 {
		t.Fatalf("quote: %+v", q.Totals)
	}
	fee := int64(2000)
	in.DeliveryFee = &fee
	_, err = a.PlaceStaffOrder(ctx, s.branchID, s.staff, "", in)
	wantCode(t, err, "VALIDATION")
	key := uniq("k")
	out, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, key, in)
	noErr(t, err)
	o := out.Order
	if o.Channel != "staff" || o.OrderSource != "whatsapp" || o.MemberNo == nil || *o.MemberNo != 2 ||
		o.AcceptanceStatus != "accepted" || o.Totals.DeliveryFee != 2000 || o.ContactWhatsapp != "+639450001111" ||
		o.StaffNote != "asked twice" || o.CustomerName != "" {
		t.Fatalf("order: %+v", o)
	}
	if out.Member.Status != "active" || out.Member.AddressLine != "2621 Zamora St" {
		t.Fatalf("member after the order: %+v", out.Member)
	}
	// A double press places one order.
	again, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, key, in)
	noErr(t, err)
	if again.Order.ID != o.ID {
		t.Fatal("the same key placed a second order")
	}

	// A member number nobody has is created; changed details are saved.
	in.Member = app.MemberInput{No: 7, Phone: "+639450002222", Source: "line", Name: "Mark"}
	in.ServiceType, in.ZoneID, in.DeliveryFee = "pickup", nil, nil
	out, err = a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	if out.Member.No != 7 || out.Order.AddressLine != "" {
		t.Fatalf("new member order: %+v %+v", out.Member, out.Order)
	}
	in.Member.Name = "Mark T."
	_, err = a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	m, err := a.GetMember(ctx, s.branchID, 7)
	noErr(t, err)
	if m.Name != "Mark T." || m.Stats.Orders != 2 || m.Stats.Week != 2 || len(m.Stats.Favorites) == 0 {
		t.Fatalf("member 7: %+v %+v", m, m.Stats)
	}
	// The next new number follows the highest one.
	m8, err := a.NewMember(ctx, s.branchID, 0)
	noErr(t, err)
	if m8.No != 8 {
		t.Fatalf("next number: %d", m8.No)
	}
	// A delivery needs an address.
	in.ServiceType, in.ZoneID = "delivery", &s.zoneID
	_, err = a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	wantCode(t, err, "ADDRESS_REQUIRED")

	hints, err := a.AddressHints(ctx, s.branchID, "zamora", "")
	noErr(t, err)
	if len(hints) != 1 || hints[0].Line != "2621 Zamora St" {
		t.Fatalf("address hints: %+v", hints)
	}
}

func TestStaffOrderOptionQuantities(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	cat, err := a.SaveCategory(ctx, s.branchID, 0, app.CategoryInput{Name: "noodles"})
	noErr(t, err)
	id, err := a.SaveItem(ctx, s.branchID, 0, app.ItemInput{CategoryID: cat, Code: " b2 ", Name: "noodle", Price: 22000,
		Published: true, DeliveryAvailable: true, PickupAvailable: true,
		Groups: []app.OptionGroupInput{{Name: "add", MinSelect: 0, MaxSelect: 5,
			Options: []app.OptionInput{{Name: "kelp", PriceDelta: 4000}, {Name: "egg", PriceDelta: 1500}}}}})
	noErr(t, err)
	_, err = a.SaveItem(ctx, s.branchID, 0, app.ItemInput{CategoryID: cat, Code: "B2", Name: "other", Price: 100})
	wantCode(t, err, "CODE_TAKEN")
	menu, err := a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	var kelp, egg int64
	for _, c := range menu.Categories {
		for _, it := range c.Items {
			if it.ID == id {
				if it.Code != "B2" {
					t.Fatalf("code = %q", it.Code)
				}
				kelp, egg = it.Groups[0].Options[0].ID, it.Groups[0].Options[1].ID
			}
		}
	}
	in := app.StaffOrderInput{Member: app.MemberInput{Phone: "+639450003333", Source: "phone"}, ServiceType: "pickup",
		Items: []app.StaffLine{{ItemID: id, Qty: 1, OptionIDs: []int64{kelp, kelp, egg}}}}
	out, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	it := out.Order.Items[0]
	if it.UnitPrice != 22000+8000+1500 || len(it.Options) != 2 || it.Options[0].Qty != 2 || it.Options[0].Name != "kelp ×2" {
		t.Fatalf("line: %+v", it)
	}
	if out.Member.No != 1 || out.Order.AcceptanceStatus != "pending" {
		t.Fatalf("member %d, accepted %v", out.Member.No, out.Order.AcceptanceStatus)
	}
	// The customer's own cart still refuses the same option twice.
	_, err = a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup",
		Items: []app.QuoteLineInput{{ItemID: id, Qty: 1, OptionIDs: []int64{kelp, kelp}}}}, "")
	wantCode(t, err, "VALIDATION")
	// Too many in a group is still too many.
	in.Items[0].OptionIDs = []int64{kelp, kelp, kelp, egg, egg, egg}
	_, err = a.StaffQuote(ctx, s.branchID, in)
	wantCode(t, err, "OPTIONS_INVALID")
}

func TestMemberPages(t *testing.T) {
	s := newStore(t, false)
	other := newStore(t, false)
	ctx := context.Background()
	a := testApp

	// Member 1 orders twice, member 2 is filled in but never orders, member 3 is temporary.
	in := app.StaffOrderInput{Member: app.MemberInput{Phone: "+639171110001", Source: "wechat", Name: "Chen"},
		ServiceType: "pickup", Items: []app.StaffLine{{ItemID: s.tea, Qty: 2}}}
	o1, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	in.Member.No = o1.Member.No
	_, err = a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	_, err = a.SaveMember(ctx, s.branchID, 0, app.MemberInput{Phone: "+639171110002", Source: "line", Name: "Lina", AddressLine: "12 Rizal Ave"})
	noErr(t, err)
	tmp, err := a.NewMember(ctx, s.branchID, 0)
	noErr(t, err)
	elsewhere := uniq("elsewhere")
	_, err = a.SaveMember(ctx, other.branchID, 0, app.MemberInput{Phone: "+639171110009", Source: "phone", Name: elsewhere})
	noErr(t, err)

	// A store sees its own members only, newest first, with what they ordered.
	l, err := a.ListMembers(ctx, app.MemberFilter{BranchID: s.branchID})
	noErr(t, err)
	if l.Total != 3 || l.Temp != 1 || len(l.Items) != 3 || l.Items[0].ID != tmp.ID {
		t.Fatalf("list: total %d temp %d %+v", l.Total, l.Temp, l.Items)
	}
	chen := l.Items[2]
	if chen.Name != "Chen" || chen.Orders != 2 || chen.Spent != o1.Order.Totals.Total*2 || chen.LastAt == nil || chen.BranchID != s.branchID {
		t.Fatalf("chen: %+v", chen)
	}
	for _, c := range []struct {
		f    app.MemberFilter
		want int
	}{
		{app.MemberFilter{Q: "lina"}, 1}, {app.MemberFilter{Q: "1110001"}, 1}, {app.MemberFilter{Q: "000002"}, 1},
		{app.MemberFilter{Q: "rizal"}, 1}, {app.MemberFilter{Status: "temp"}, 1}, {app.MemberFilter{Status: "active"}, 2},
		{app.MemberFilter{Source: "line"}, 1}, {app.MemberFilter{Q: elsewhere}, 0},
	} {
		c.f.BranchID = s.branchID
		l, err := a.ListMembers(ctx, c.f)
		noErr(t, err)
		if l.Total != c.want || len(l.Items) != c.want {
			t.Fatalf("%+v: %d", c.f, l.Total)
		}
	}
	l, err = a.ListMembers(ctx, app.MemberFilter{BranchID: s.branchID, Sort: "spent", Page: app.Page{PageSize: 1}})
	noErr(t, err)
	if l.Total != 3 || len(l.Items) != 1 || l.Items[0].Name != "Chen" {
		t.Fatalf("by spent: %+v", l.Items)
	}

	// The platform sees every store, and can narrow to one.
	l, err = a.ListMembers(ctx, app.MemberFilter{Q: elsewhere})
	noErr(t, err)
	if l.Total != 1 || l.Items[0].BranchID != other.branchID || l.Items[0].BranchName == "" || l.Items[0].MerchantName == "" {
		t.Fatalf("platform: %+v", l.Items)
	}

	// Details and order history.
	d, err := a.MemberByNo(ctx, s.branchID, chen.No)
	noErr(t, err)
	if d.Stats == nil || d.Stats.Orders != 2 || d.Orders != 2 {
		t.Fatalf("detail: %+v", d)
	}
	h, err := a.MemberOrderHistory(ctx, chen.ID, app.Page{})
	noErr(t, err)
	if h.Total != 2 || !h.Items[0].Linked || h.Items[0].Items == "" || h.Items[0].OrderSource != "wechat" {
		t.Fatalf("history: %+v", h)
	}
	if d2, _ := a.MemberByNo(ctx, other.branchID, chen.No); d2 != nil && d2.ID == chen.ID {
		t.Fatal("another store found this store's member")
	}

	// The platform edits a member; a temporary one becomes a member.
	admin := adminActor(t)
	_, err = a.AdminSaveMember(ctx, admin, tmp.ID, app.MemberInput{Phone: "12"})
	wantCode(t, err, "PHONE_REQUIRED")
	saved, err := a.AdminSaveMember(ctx, admin, tmp.ID, app.MemberInput{Phone: "+639171110003", Source: "whatsapp", Name: "Tess"})
	noErr(t, err)
	if saved.Status != "active" || saved.No != tmp.No || saved.Name != "Tess" || saved.BranchID != s.branchID {
		t.Fatalf("admin save: %+v", saved)
	}
	_, err = a.AdminSaveMember(ctx, admin, 999999999, app.MemberInput{Phone: "+639171110003", Source: "whatsapp"})
	wantCode(t, err, "NOT_FOUND")
}

func TestMemberAddressBook(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	order := func(no int, line, unit string) *app.StaffOrder {
		t.Helper()
		out, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), app.StaffOrderInput{
			Member:      app.MemberInput{No: no, Phone: "+639181112222", Source: "wechat", AddressLine: line, AddressUnit: unit},
			ServiceType: "delivery", ZoneID: &s.zoneID, Items: []app.StaffLine{{ItemID: s.tea, Qty: 2}}})
		noErr(t, err)
		return out
	}
	first := order(0, "12 Rizal Ave", "5F")
	no := first.Member.No
	order(no, " 12 rizal ave ", "5f") // the same address, typed differently
	order(no, "88 Ayala Ave", "")     // a new one
	order(no, "12 Rizal Ave", "7F")   // another unit is another address
	d, err := a.MemberByNo(ctx, s.branchID, no)
	noErr(t, err)
	if len(d.Addresses) != 3 {
		t.Fatalf("addresses: %+v", d.Addresses)
	}
	if !d.Addresses[0].Current || d.Addresses[0].Unit != "7F" {
		t.Fatalf("the last one is current: %+v", d.Addresses[0])
	}
	var rizal5 app.MemberAddress
	for _, ad := range d.Addresses {
		if ad.Unit == "5F" {
			rizal5 = ad
		}
	}
	if rizal5.Uses != 2 {
		t.Fatalf("uses of the repeated address: %+v", rizal5)
	}
	// Ordering for pickup does not add an address; saving details does.
	_, err = a.SaveMember(ctx, s.branchID, 0, app.MemberInput{No: no, Phone: "+639181112222", Source: "wechat", AddressLine: "1 Bonifacio St"})
	noErr(t, err)
	d, err = a.MemberByNo(ctx, s.branchID, no)
	noErr(t, err)
	if len(d.Addresses) != 4 || d.Addresses[0].Line != "1 Bonifacio St" || d.Addresses[0].Uses != 0 {
		t.Fatalf("saved address: %+v", d.Addresses)
	}
	// The address book shows up when typing an address for this phone.
	hints, err := a.AddressHints(ctx, s.branchID, "", "+639181112222")
	noErr(t, err)
	if len(hints) < 3 {
		t.Fatalf("hints: %+v", hints)
	}
	// Any address but the current one can be removed.
	wantCode(t, a.DeleteMemberAddress(ctx, d.ID, d.Addresses[0].ID), "NOT_FOUND")
	noErr(t, a.DeleteMemberAddress(ctx, d.ID, rizal5.ID))
	d, err = a.MemberByNo(ctx, s.branchID, no)
	noErr(t, err)
	if len(d.Addresses) != 3 {
		t.Fatalf("after delete: %+v", d.Addresses)
	}
}
