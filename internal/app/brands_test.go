package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// brandCode returns a code nobody on the platform has yet.
func brandCode(p string) string {
	return fmt.Sprintf("%s%05d", p, (time.Now().UnixNano()/1000+seq.Add(1))%100000)
}

// One store, several brands (docs/MULTI_BRAND_DESIGN.md): each brand has its
// own code for staff order numbers and its own members.
func TestBrands(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp

	// Every store starts with its main brand.
	list, err := a.ListBrands(ctx, s.branchID)
	noErr(t, err)
	if len(list) != 1 || !list[0].IsMain || !list[0].DineIn || list[0].Status != "active" {
		t.Fatalf("main brand: %+v", list)
	}
	main := list[0]

	// A delivery brand with its own code; a bad or taken code is refused.
	_, err = a.CreateBrand(ctx, s.branchID, s.staff, app.BrandInput{Name: "好吃党", Code: "h-d", Delivery: true})
	wantCode(t, err, "BRAND_CODE_INVALID")
	code := brandCode("HC")
	hcd, err := a.CreateBrand(ctx, s.branchID, s.staff, app.BrandInput{Name: "好吃党", NameEn: "Haochidang", Code: code, Delivery: true, MallVisible: true})
	noErr(t, err)
	if hcd.IsMain || hcd.DineIn || hcd.Code != code || hcd.Slug == "" || hcd.StoreNo == 0 {
		t.Fatalf("new brand: %+v", hcd)
	}
	other := newStore(t, false)
	_, err = a.CreateBrand(ctx, other.branchID, other.staff, app.BrandInput{Name: "X", Code: code, Delivery: true})
	wantCode(t, err, "BRAND_CODE_TAKEN")
	// Only one dine-in brand per store.
	_, err = a.CreateBrand(ctx, s.branchID, s.staff, app.BrandInput{Name: "二号店", Code: brandCode("EH"), DineIn: true})
	wantCode(t, err, "DINE_BRAND_TAKEN")
	// The main brand stays on.
	_, err = a.UpdateBrand(ctx, s.branchID, main.ID, s.staff, app.BrandInput{Name: main.Name, Code: brandCode("MN"), DineIn: true, Delivery: true, Status: "disabled"})
	wantCode(t, err, "MAIN_BRAND")
	// The store's brand limit counts active delivery brands.
	_, err = a.DB.Exec(ctx, `UPDATE branches SET brand_limit=2 WHERE id=$1`, s.branchID)
	noErr(t, err)
	_, err = a.CreateBrand(ctx, s.branchID, s.staff, app.BrandInput{Name: "江西小吃", Code: brandCode("JX"), Delivery: true})
	wantCode(t, err, "BRAND_LIMIT")

	// Members are kept per brand: the same number and phone are two members.
	in := app.StaffOrderInput{
		Member:      app.MemberInput{No: 223, Phone: "+639450007777", Source: "wechat", Name: "Ann"},
		ServiceType: "pickup", Accept: true,
		Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}},
	}
	p1, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	in.BrandID = hcd.ID
	in.Member.Name = "Ann H."
	p2, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	p3, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	if p1.Order.BrandID != main.ID || p2.Order.BrandID != hcd.ID || p2.Order.BrandName != "好吃党" {
		t.Fatalf("order brands: %d %d %q", p1.Order.BrandID, p2.Order.BrandID, p2.Order.BrandName)
	}
	if p2.Order.PublicNo != code+"000223" || p3.Order.PublicNo != code+"000223-2" {
		t.Fatalf("brand numbers: %s %s", p2.Order.PublicNo, p3.Order.PublicNo)
	}
	if p1.Member.ID == p2.Member.ID || p2.Member.ID != p3.Member.ID {
		t.Fatalf("members: %d %d %d", p1.Member.ID, p2.Member.ID, p3.Member.ID)
	}
	checked(t, "brand staff order", p2.Order.ID)
	// D15 notices an order moved to another brand than its member's.
	_, err = a.DB.Exec(ctx, `UPDATE orders SET brand_id=$2 WHERE id=$1`, p3.Order.ID, main.ID)
	noErr(t, err)
	bad, err := a.CheckOrderMoney(ctx, p3.Order.ID)
	noErr(t, err)
	if len(bad) != 1 || !strings.HasPrefix(bad[0], "D15") {
		t.Fatalf("D15: %v", bad)
	}
	_, err = a.DB.Exec(ctx, `UPDATE orders SET brand_id=$2 WHERE id=$1`, p3.Order.ID, hcd.ID)
	noErr(t, err)
	checked(t, "brand staff order, second", p3.Order.ID)
	m, err := a.GetMember(ctx, s.branchID, 0, 223)
	noErr(t, err)
	if m.Name != "Ann" || m.Stats.Orders != 1 {
		t.Fatalf("main brand member: %+v %+v", m, m.Stats)
	}
	m, err = a.GetMember(ctx, s.branchID, hcd.ID, 223)
	noErr(t, err)
	if m.Name != "Ann H." || m.Stats.Orders != 2 {
		t.Fatalf("brand member: %+v %+v", m, m.Stats)
	}
	found, err := a.SearchMembers(ctx, s.branchID, hcd.ID, "0007777")
	noErr(t, err)
	if len(found) != 1 || found[0].BrandID != hcd.ID {
		t.Fatalf("brand search: %+v", found)
	}
	// Another store's brand is not this store's.
	_, err = a.GetMember(ctx, other.branchID, hcd.ID, 223)
	wantCode(t, err, "BRAND_UNKNOWN")

	// A switched-off brand takes no new orders; its orders and members stay.
	hcd, err = a.UpdateBrand(ctx, s.branchID, hcd.ID, s.staff, app.BrandInput{Name: hcd.Name, NameEn: hcd.NameEn, Code: code,
		Delivery: true, Color: hcd.Color, MallVisible: true, Status: "disabled"})
	noErr(t, err)
	_, err = a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	wantCode(t, err, "BRAND_CLOSED")
	_, err = a.GetMember(ctx, s.branchID, hcd.ID, 223)
	noErr(t, err)
	l, err := a.ListBrands(ctx, s.branchID)
	noErr(t, err)
	if len(l) != 2 || l[1].Status != "disabled" || l[1].Today == nil || l[1].Today.Orders != 2 {
		t.Fatalf("brand list: %+v %+v", l, l[1].Today)
	}

	// The main brand follows the store's code and name.
	mc := brandCode("YP")
	_, err = a.UpdateBrand(ctx, s.branchID, main.ID, s.staff, app.BrandInput{Name: "一品湘", Code: mc, DineIn: true, Delivery: true, Status: "active"})
	noErr(t, err)
	var name, storeCode string
	noErr(t, a.DB.QueryRow(ctx, `SELECT name, store_code FROM branches WHERE id=$1`, s.branchID).Scan(&name, &storeCode))
	if name != "一品湘" || storeCode != mc {
		t.Fatalf("store after main brand edit: %q %q", name, storeCode)
	}
}
