package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// Order types and numbers (docs/ORDER_TYPES_DESIGN.md §3–4).
func TestOrderTypesAndNumbers(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp

	// The store code: letters and digits, unique, upper-cased.
	_, err := a.SaveOrderNumbering(ctx, s.branchID, app.OrderNumbering{StoreCode: "a", DayResetTime: "04:00"})
	wantCode(t, err, "VALIDATION")
	_, err = a.SaveOrderNumbering(ctx, s.branchID, app.OrderNumbering{StoreCode: "YP-X", DayResetTime: "04:00"})
	wantCode(t, err, "VALIDATION")
	_, err = a.SaveOrderNumbering(ctx, s.branchID, app.OrderNumbering{StoreCode: "YPX1", DayResetTime: "09:00"})
	wantCode(t, err, "VALIDATION")
	code := fmt.Sprintf("T%07d", time.Now().UnixNano()%1e7)
	b, err := a.SaveOrderNumbering(ctx, s.branchID, app.OrderNumbering{StoreCode: code, DayResetTime: "03:00"})
	noErr(t, err)
	if b.StoreCode != code || b.DayResetTime != "03:00" {
		t.Fatalf("saved numbering: %q %q", b.StoreCode, b.DayResetTime)
	}
	other := newStore(t, false)
	_, err = a.SaveOrderNumbering(ctx, other.branchID, app.OrderNumbering{StoreCode: code, DayResetTime: "04:00"})
	wantCode(t, err, "STORE_CODE_TAKEN")

	// Customers' own orders: Hippo-1, Hippo-2, each with its type.
	o1 := s.place(t, "pickup", s.cart())
	o2 := s.place(t, "delivery", s.cart())
	if o1.PublicNo != "Hippo-1" || o2.PublicNo != "Hippo-2" || o1.OrderType != app.TypeMall || o1.PickupCode != "1" {
		t.Fatalf("mall numbers: %s %s %s %s", o1.PublicNo, o2.PublicNo, o1.OrderType, o1.PickupCode)
	}
	checked(t, "mall order", o1.ID)

	// Staff orders: the store code and the member number, -2 for the member's next one today.
	in := app.StaffOrderInput{
		Member:      app.MemberInput{No: 123, Phone: "+639450001234", Source: "wechat"},
		ServiceType: "pickup", Accept: true,
		Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}},
	}
	p1, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	p2, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
	noErr(t, err)
	if p1.Order.PublicNo != code+"000123" || p2.Order.PublicNo != code+"000123-2" || p1.Order.OrderType != app.TypeStaff ||
		p1.Order.PickupCode != "0123" {
		t.Fatalf("staff numbers: %s %s %s", p1.Order.PublicNo, p2.Order.PublicNo, p1.Order.PickupCode)
	}
	checked(t, "staff order", p2.Order.ID)

	// Dine-in: DineIn-1 opened by staff stays staff; a table whose first round
	// came from the customer's phone is self-ordered.
	t1, _ := s.table(t, "A1")
	t2, qr := s.table(t, "A2")
	bill1, err := a.OpenTable(ctx, t1, 2, nil, s.staff)
	noErr(t, err)
	bill2, err := a.OpenTable(ctx, t2, 2, nil, s.staff)
	noErr(t, err)
	_, err = a.AddBatch(ctx, bill1.SessionID, "staff", uniq("k"), batch(app.QuoteLineInput{ItemID: s.tea, Qty: 1}), s.staff)
	noErr(t, err)
	token, err := a.ExchangeTableCode(ctx, qr)
	noErr(t, err)
	phone, err := a.AccessByToken(ctx, token)
	noErr(t, err)
	_, err = a.AddBatch(ctx, phone.SessionID, "qr", uniq("k"), batch(app.QuoteLineInput{ItemID: s.tea, Qty: 1}), app.Actor{Type: "guest"})
	noErr(t, err)
	d1, err := a.GetOrder(ctx, bill1.Order.ID)
	noErr(t, err)
	d2, err := a.GetOrder(ctx, bill2.Order.ID)
	noErr(t, err)
	if d1.PublicNo != "DineIn-1" || d2.PublicNo != "DineIn-2" || d1.OrderType != app.TypeDineIn ||
		d1.DineSource != "staff" || d2.DineSource != "self" {
		t.Fatalf("dine-in: %s/%s %s/%s %s", d1.PublicNo, d1.DineSource, d2.PublicNo, d2.DineSource, d1.OrderType)
	}
	checked(t, "dine-in round", d2.ID)

	// Every order has its business day.
	var missing int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM orders WHERE branch_id=$1 AND biz_date IS NULL`, s.branchID).Scan(&missing))
	if missing != 0 {
		t.Fatalf("%d orders without a business day", missing)
	}
	// The other store numbers its own orders from 1.
	if o := other.place(t, "pickup", other.cart()); o.PublicNo != "Hippo-1" {
		t.Fatalf("other store: %s", o.PublicNo)
	}
}
