package app_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// brandStore is S10's store (docs/PRICING_CHECK.md §4): 12% tax included,
// delivery 50.00, no packaging or service charge; pork 58.00 and rice 3.00.
// 一品湘 is the main brand with "100 off 15"; 好吃党 sells pork as 辣椒炒肉
// at 52.00 with "100 off 10".
type brandStore struct {
	*store
	pork, rice       int64
	main, hcd        int64
	mainPromo, hPrmo int64
}

func newBrandStore(t *testing.T) *brandStore {
	t.Helper()
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	_, err := a.SaveSettings(ctx, s.branchID, app.SettingsInput{Name: "一品湘", TaxRateBP: 1200, TaxInclusive: true,
		AcceptTimeoutMinutes: 10, DeliveryEnabled: true, PickupEnabled: true, OpenTime: "00:00", CloseTime: "00:00"})
	noErr(t, err)
	_, err = a.SaveZone(ctx, s.branchID, s.zoneID, app.ZoneInput{Name: "Z", Fee: 5000, Active: true})
	noErr(t, err)
	cat, err := a.SaveCategory(ctx, s.branchID, 0, app.CategoryInput{Name: "湘菜"})
	noErr(t, err)
	mk := func(name string, price int64) int64 {
		id, err := a.SaveItem(ctx, s.branchID, 0, app.ItemInput{Name: name, Price: price, CategoryID: cat, Published: true,
			DeliveryAvailable: true, PickupAvailable: true})
		noErr(t, err)
		return id
	}
	b := &brandStore{store: s, pork: mk("小炒肉", 5800), rice: mk("米饭", 300)}
	b.main, err = a.MainBrandID(ctx, s.branchID)
	noErr(t, err)
	hcd, err := a.CreateBrand(ctx, s.branchID, s.staff, app.BrandInput{Name: "好吃党", Code: brandCode("HD"), Delivery: true, MallVisible: true})
	noErr(t, err)
	b.hcd = hcd.ID
	var in app.BrandMenuInput
	noErr(t, jsonInto(`{"own_menu": true, "categories": [{"key": "a", "name": "招牌"}],
		"items": [{"item_id": `+itoa(b.pork)+`, "category_key": "a", "name": "辣椒炒肉", "price": 5200, "published": true},
			{"item_id": `+itoa(b.rice)+`, "published": true}]}`, &in))
	_, err = a.SaveBrandMenu(ctx, s.branchID, b.hcd, s.staff, in)
	noErr(t, err)
	promo := func(value int64, brands []int64) int64 {
		id, err := a.SavePromotion(ctx, s.branchID, 0, app.PromotionInput{Name: "满100减", Kind: "amount_off", Threshold: 10000,
			Value: value, AppliesDelivery: true, AppliesPickup: true, BrandIDs: brands}, s.staff)
		noErr(t, err)
		noErr(t, a.SetPromotionStatus(ctx, s.branchID, id, "active", s.staff))
		return id
	}
	b.mainPromo = promo(1500, nil) // none = the main brand
	b.hPrmo = promo(1000, []int64{b.hcd})
	return b
}

func (b *brandStore) staffOrder(t *testing.T, brand int64, no int) *app.StaffOrder {
	t.Helper()
	out, err := testApp.PlaceStaffOrder(context.Background(), b.branchID, b.staff, uniq("k"), app.StaffOrderInput{
		BrandID:     brand,
		Member:      app.MemberInput{No: no, Phone: "+639450008888", Source: "wechat", AddressLine: "1 Main St"},
		ServiceType: "delivery", ZoneID: &b.zoneID, Accept: true,
		Items: []app.StaffLine{{ItemID: b.pork, Qty: 2}, {ItemID: b.rice, Qty: 2}},
	})
	noErr(t, err)
	return out
}

// S10: one cart at two brands, each at its own prices and promotion.
func TestBrandPricing(t *testing.T) {
	b := newBrandStore(t)
	ctx, a := context.Background(), testApp

	o := b.staffOrder(t, 0, 1).Order
	checked(t, "一品湘", o.ID)
	if o.Totals.Subtotal != 12200 || o.Totals.Discount != 1500 || o.Totals.TaxAmount != 1146 || o.Totals.Total != 15700 {
		t.Fatalf("一品湘: %+v", o.Totals)
	}
	h := b.staffOrder(t, b.hcd, 1).Order
	checked(t, "好吃党", h.ID)
	if h.Totals.Subtotal != 11000 || h.Totals.Discount != 1000 || h.Totals.TaxAmount != 1071 || h.Totals.Total != 15000 {
		t.Fatalf("好吃党: %+v", h.Totals)
	}
	var pork *app.OrderItem
	for i := range h.Items {
		if h.Items[i].ItemID == b.pork {
			pork = &h.Items[i]
		}
	}
	if pork == nil || pork.Name != "辣椒炒肉" || pork.BaseName != "小炒肉" || pork.UnitPrice != 5200 {
		t.Fatalf("brand line: %+v", pork)
	}
	var base string
	noErr(t, a.DB.QueryRow(ctx, `SELECT base_name FROM order_items WHERE id=$1`, pork.ID).Scan(&base))
	if base != "小炒肉" {
		t.Fatalf("kitchen name: %q", base)
	}

	// Without its own promotion, 好吃党 does not take 一品湘's.
	noErr(t, a.SetPromotionStatus(ctx, b.branchID, b.hPrmo, "paused", b.staff))
	n := b.staffOrder(t, b.hcd, 2).Order
	checked(t, "好吃党无活动", n.ID)
	if n.Totals.Discount != 0 || n.Totals.TaxAmount != 1179 || n.Totals.Total != 16000 {
		t.Fatalf("no promotion: %+v", n.Totals)
	}

	// The brand's storefront: its menu, names, prices and promotions.
	noErr(t, a.SetPromotionStatus(ctx, b.branchID, b.hPrmo, "active", b.staff))
	sf, err := a.StorefrontOf(ctx, b.slug, b.hcd)
	noErr(t, err)
	if sf.Brand == nil || sf.Brand.ID != b.hcd || len(sf.Categories) != 2 || sf.Categories[0].Name != "招牌" ||
		sf.Categories[0].Items[0].Name != "辣椒炒肉" || sf.Categories[0].Items[0].Price != 5200 ||
		sf.Categories[1].Items[0].ID != b.rice || len(sf.Promotions) != 1 || sf.Promotions[0].ID != b.hPrmo {
		t.Fatalf("brand storefront: %+v %+v", sf.Categories, sf.Promotions)
	}
	sf, err = a.Storefront(ctx, b.slug)
	noErr(t, err)
	if len(sf.Promotions) != 1 || sf.Promotions[0].ID != b.mainPromo {
		t.Fatalf("main storefront promotions: %+v", sf.Promotions)
	}

	// A brand menu only takes the store's own dishes.
	other := newStore(t, false)
	var in app.BrandMenuInput
	noErr(t, jsonInto(`{"own_menu": true, "items": [{"item_id": `+itoa(other.tea)+`, "published": true}]}`, &in))
	_, err = a.SaveBrandMenu(ctx, b.branchID, b.hcd, b.staff, in)
	wantCode(t, err, "ITEM_UNKNOWN")
	noErr(t, jsonInto(`{"own_menu": true}`, &in))
	in.Items = nil
	_, err = a.SaveBrandMenu(ctx, b.branchID, b.hcd, b.staff, in)
	wantCode(t, err, "BRAND_MENU_EMPTY")
	_, err = a.SavePromotion(ctx, b.branchID, 0, app.PromotionInput{Name: "x", Kind: "amount_off", Threshold: 100, Value: 10,
		AppliesDelivery: true, BrandIDs: []int64{other.branchID * 1000}}, b.staff)
	wantCode(t, err, "BRAND_UNKNOWN")
}

// Promotions and store coupons do not cross brands (D13).
func TestBrandPromoCouponScope(t *testing.T) {
	b := newBrandStore(t)
	ctx, a := context.Background(), testApp
	cid, phone := customer(t)
	hc := b.give(t, cid, app.CouponStoreThreshold, 2000, 10000, 30)
	_, err := a.DB.Exec(ctx, `UPDATE customer_coupons SET brand_id=$2 WHERE id=$1`, hc, b.hcd)
	noErr(t, err)

	cart := []app.QuoteLineInput{{ItemID: b.pork, Qty: 2}, {ItemID: b.rice, Qty: 2}}
	want := []int64{hc}
	q, err := a.CreateQuote(ctx, b.slug, app.QuoteInput{ServiceType: "delivery", ZoneID: &b.zoneID, Items: cart, CouponIDs: &want}, phone)
	noErr(t, err)
	if q.Totals.Total != 15700 || len(q.Coupons) != 0 || len(q.CouponChoices) != 0 {
		t.Fatalf("好吃党's coupon at 一品湘: total %d coupons %+v choices %+v", q.Totals.Total, q.Coupons, q.CouponChoices)
	}
	// the main brand's own coupon works
	mc := b.give(t, cid, app.CouponStoreThreshold, 2000, 10000, 30)
	want = []int64{mc}
	q, err = a.CreateQuote(ctx, b.slug, app.QuoteInput{ServiceType: "delivery", ZoneID: &b.zoneID, Items: cart, CouponIDs: &want}, phone)
	noErr(t, err)
	if q.Totals.Total != 13700 || len(q.Coupons) != 1 {
		t.Fatalf("own coupon: total %d coupons %+v", q.Totals.Total, q.Coupons)
	}
	p, err := a.PlaceOrder(ctx, b.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann", AddressLine: "1 Main St"})
	noErr(t, err)
	o, err := a.OrderByToken(ctx, p.AccessToken)
	noErr(t, err)
	checked(t, "一品湘 with its coupon", o.ID)
	if o.BrandID != b.main {
		t.Fatalf("order brand %d", o.BrandID)
	}

	// D13 notices a promotion or a coupon of another brand.
	h := b.staffOrder(t, b.hcd, 5).Order
	_, err = a.DB.Exec(ctx, `UPDATE order_discounts SET brand_ids=$2 WHERE order_id=$1 AND kind='promotion'`, h.ID, []int64{b.main})
	noErr(t, err)
	bad, err := a.CheckOrderMoney(ctx, h.ID)
	noErr(t, err)
	if len(bad) != 1 || !strings.HasPrefix(bad[0], "D13") {
		t.Fatalf("D13 promotion: %v", bad)
	}
	_, err = a.DB.Exec(ctx, `UPDATE order_discounts SET brand_ids=$2 WHERE order_id=$1 AND kind='promotion'`, h.ID, []int64{b.hcd})
	noErr(t, err)
	checked(t, "好吃党 restored", h.ID)
	_, err = a.DB.Exec(ctx, `UPDATE customer_coupons SET brand_id=$2 WHERE id=$1`, mc, b.hcd)
	noErr(t, err)
	bad, err = a.CheckOrderMoney(ctx, o.ID)
	noErr(t, err)
	if len(bad) != 1 || !strings.HasPrefix(bad[0], "D13") {
		t.Fatalf("D13 coupon: %v", bad)
	}
	_, err = a.DB.Exec(ctx, `UPDATE customer_coupons SET brand_id=$2 WHERE id=$1`, mc, b.main)
	noErr(t, err)

	// A promotion moved off the brand later: the order keeps what it took.
	_, err = a.SavePromotion(ctx, b.branchID, b.hPrmo, app.PromotionInput{Name: "满100减", Kind: "amount_off", Threshold: 10000,
		Value: 1000, AppliesDelivery: true, AppliesPickup: true, BrandIDs: []int64{b.main}}, b.staff)
	noErr(t, err)
	h2, err := a.StaffAddon(ctx, h.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: b.rice, Qty: 1}}}, b.staff)
	noErr(t, err)
	checked(t, "好吃党 add-on after its promotion moved", h2.Order.ID)
	if h2.Order.Totals.Discount < 1000 {
		t.Fatalf("held promotion: %+v", h2.Order.Totals)
	}
}

// Dishes added to an order come at its brand's prices.
func TestBrandAddonPrice(t *testing.T) {
	b := newBrandStore(t)
	ctx, a := context.Background(), testApp
	for _, c := range []struct {
		brand int64
		name  string
		unit  int64
	}{{0, "小炒肉", 5800}, {b.hcd, "辣椒炒肉", 5200}} {
		o := b.staffOrder(t, c.brand, 7).Order
		r, err := a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: b.pork, Qty: 1}}}, b.staff)
		noErr(t, err)
		checked(t, "add-on at "+c.name, o.ID)
		last := r.Order.Items[len(r.Order.Items)-1]
		if last.Name != c.name || last.UnitPrice != c.unit || r.Order.Totals.Subtotal != 2*c.unit+600+c.unit {
			t.Fatalf("%s add-on: %+v %+v", c.name, last, r.Order.Totals)
		}
	}
}

func jsonInto(s string, v any) error { return json.Unmarshal([]byte(s), v) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
