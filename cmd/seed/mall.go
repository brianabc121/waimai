package main

import (
	"context"
	"errors"
	"log"

	"github.com/brianabc121/waimai/internal/app"
)

// The marketplace needs more than two stores to look like one. These are
// small: a couple of dishes each, a place on the map and one delivery area.

type mallDish struct {
	name, en, pic string
	price         int64
}

type mallStore struct {
	slug, name, en, owner string
	lat, lng              float64
	district, city        string
	cats                  []string
	tagline, pic          string
	// one delivery area: a box this many degrees around the store
	reach       float64
	fee, min    int64
	eta         int
	open, close string
	pickupOnly  bool
	dishes      []mallDish
	promo       *app.PromotionInput
	// what the store has behind it: orders in the last weeks and what people said
	sold  int
	stars []int
}

var mallStores = []mallStore{
	{slug: "tea", sold: 96, stars: []int{5, 5, 5, 5, 5, 5, 5, 4}, name: "茶屿奶茶", en: "Tea Isle", owner: "owner3", lat: 14.5560, lng: 121.0210, district: "Bel-Air", city: "Makati",
		cats: []string{"drinks"}, tagline: "现泡茶底，每天现煮珍珠", pic: "bubble-milk-tea", reach: 0.012, fee: 2900, min: 10000, eta: 20,
		dishes: []mallDish{{"珍珠奶茶", "Bubble Milk Tea", "bubble-milk-tea", 11900}, {"冰柠檬茶", "Iced Lemon Tea", "iced-lemon-tea", 6900}},
		promo:  &app.PromotionInput{Name: "满200减20", NameEn: "Save 20 over 200", Kind: "amount_off", Threshold: 20000, Value: 2000, AppliesDelivery: true, AppliesPickup: true}},
	{slug: "chicken", sold: 41, stars: []int{5, 5, 5, 4, 4, 5, 4}, name: "炸鸡研究所", en: "Fried Chicken Lab", owner: "owner4", lat: 14.5585, lng: 121.0265, district: "Bel-Air", city: "Makati",
		cats: []string{"fastfood", "snacks"}, tagline: "现炸现卖，外酥里嫩", pic: "popcorn-chicken", reach: 0.014, fee: 3900, min: 15000, eta: 25,
		dishes: []mallDish{{"盐酥鸡", "Popcorn Chicken", "popcorn-chicken", 16900}, {"双人炸物套餐", "Fried Combo for Two", "fried-combo", 39900}},
		promo:  &app.PromotionInput{Name: "满300免配送费", NameEn: "Free delivery over 300", Kind: "free_delivery", Threshold: 30000, AppliesDelivery: true}},
	{slug: "bento", sold: 35, stars: []int{5, 5, 4, 5, 5, 4}, name: "台北便当", en: "Taipei Bento", owner: "owner5", lat: 14.5650, lng: 121.0300, district: "Poblacion", city: "Makati",
		cats: []string{"bento"}, tagline: "三菜一饭，分量足", pic: "bento", reach: 0.022, fee: 5900, min: 18000, eta: 35,
		dishes: []mallDish{{"招牌便当", "House Bento", "bento", 21900}, {"台式牛肉面", "Taiwanese Beef Noodles", "beef-noodle-soup", 24000}}},
	{slug: "cutlet", sold: 26, stars: []int{5, 4, 5, 4, 5, 4}, name: "金黄鸡排", en: "Golden Cutlet", owner: "owner6", lat: 14.5510, lng: 121.0290, district: "San Lorenzo", city: "Makati",
		cats: []string{"snacks"}, tagline: "比脸大的鸡排", pic: "chicken-cutlet", reach: 0.016, fee: 4900, min: 15000, eta: 30,
		dishes: []mallDish{{"黄金鸡排", "Crispy Chicken Cutlet", "chicken-cutlet", 18900}, {"无盐地瓜条", "Sweet Potato Fries", "sweet-potato-fries", 9900}},
		promo:  &app.PromotionInput{Name: "满300减30", NameEn: "Save 30 over 300", Kind: "amount_off", Threshold: 30000, Value: 3000, AppliesDelivery: true, AppliesPickup: true}},
	// Open for a few minutes after midnight: at any other time the marketplace shows it as closed.
	{slug: "luwei", sold: 19, stars: []int{5, 4, 4, 5, 5, 4}, name: "卤味一家", en: "Braised House", owner: "owner7", lat: 14.5600, lng: 121.0180, district: "Bel-Air", city: "Makati",
		cats: []string{"chinese"}, tagline: "古早味卤肉饭", pic: "braised-pork-rice", reach: 0.02, fee: 6900, min: 25000, eta: 40, open: "00:00", close: "00:05",
		dishes: []mallDish{{"卤肉饭", "Braised Pork Rice", "braised-pork-rice", 15900}}},
	{slug: "fries", sold: 12, name: "薯薯小食", en: "Sweet Fries", owner: "owner8", lat: 14.5520, lng: 121.0190, district: "Legazpi Village", city: "Makati",
		cats: []string{"snacks", "dessert"}, tagline: "只做到店自取", pic: "sweet-potato-fries", pickupOnly: true,
		dishes: []mallDish{{"无盐地瓜条", "Sweet Potato Fries", "sweet-potato-fries", 9900}}},
	{slug: "pasay", sold: 21, stars: []int{5, 5, 4, 5, 5}, name: "帕赛烧腊", en: "Pasay Roast", owner: "owner9", lat: 14.5378, lng: 121.0014, district: "Barangay 76", city: "Pasay",
		cats: []string{"chinese", "bento"}, tagline: "烧腊饭，港式做法", pic: "braised-pork-rice", reach: 0.008, fee: 4900, min: 15000, eta: 30,
		dishes: []mallDish{{"烧腊双拼饭", "Two-Roast Rice", "braised-pork-rice", 19900}}},
}

func seedMall(ctx context.Context, a *app.App) {
	system := app.Actor{Type: "system"}
	codes := map[string]int64{}
	rows, err := a.DB.Query(ctx, `SELECT code, id FROM store_categories`)
	must(err)
	for rows.Next() {
		var code string
		var id int64
		must(rows.Scan(&code, &id))
		codes[code] = id
	}
	rows.Close()
	ids := func(cats []string) []int64 {
		out := []int64{}
		for _, c := range cats {
			if id, ok := codes[c]; ok {
				out = append(out, id)
			}
		}
		return out
	}

	for _, s := range mallStores {
		m, err := a.CreateMerchant(ctx, app.NewMerchant{MerchantName: s.name, BranchName: s.name, BranchNameEn: s.en, Slug: s.slug,
			Timezone: "Asia/Manila", Currency: "PHP", Owner: app.NewUser{Username: s.owner, Password: demoPassword, DisplayName: "店长 " + s.owner}})
		var ae *app.Error
		if errors.As(err, &ae) && (ae.Code == "SLUG_TAKEN" || ae.Code == "USERNAME_TAKEN") {
			continue
		}
		must(err)
		bid := m.BranchID
		open, cls := s.open, s.close
		if open == "" {
			open, cls = "00:00", "00:00"
		}
		address := s.district + ", " + s.city
		_, err = a.SaveSettings(ctx, bid, app.SettingsInput{Name: s.name, NameEn: s.en, Address: address, Phone: "+63 2 8123 4567",
			TaxRateBP: 1200, TaxInclusive: true, PackagingFee: 500, AcceptTimeoutMinutes: 10,
			DeliveryEnabled: !s.pickupOnly, PickupEnabled: true, OpenTime: open, CloseTime: cls, NotifySMS: true,
			SchedulingEnabled: s.open != "", PrepMinutes: 30, Intro: strp(s.tagline)})
		must(err)
		_, err = a.SaveLocation(ctx, bid, app.LocationInput{Lat: s.lat, Lng: s.lng, Address: address,
			Region: &app.Region{CountryCode: "PH", Country: "Philippines", Province: "Metro Manila", City: s.city, District: s.district}})
		must(err)
		if s.reach > 0 {
			_, err = a.SaveZone(ctx, bid, 0, app.ZoneInput{Name: "配送范围", NameEn: "Delivery area", Fee: s.fee, MinOrder: s.min, Active: true, Color: "#2563eb",
				EtaMinutes: intp(s.eta), Polygon: app.Polygon{{s.lat + s.reach, s.lng - s.reach}, {s.lat + s.reach, s.lng + s.reach},
					{s.lat - s.reach, s.lng + s.reach}, {s.lat - s.reach, s.lng - s.reach}}})
			must(err)
		}
		cat, err := a.SaveCategory(ctx, bid, 0, app.CategoryInput{Name: "招牌", NameEn: "Favourites"})
		must(err)
		for _, d := range s.dishes {
			_, err := a.SaveItem(ctx, bid, 0, app.ItemInput{CategoryID: cat, Name: d.name, NameEn: d.en, Price: d.price, ImageURL: "/demo/" + d.pic + ".jpg",
				NeedsPrep: true, Published: true, DeliveryAvailable: true, PickupAvailable: true})
			must(err)
		}
		if s.promo != nil {
			id, err := a.SavePromotion(ctx, bid, 0, *s.promo, system)
			must(err)
			must(a.SetPromotionStatus(ctx, bid, id, "active", system))
		}
		pic := "/demo/" + s.pic + ".jpg"
		must(a.SaveTemplateDraft(ctx, bid, "delivery", app.Template{Layout: "list", ThemeColor: "#d9480f", LogoURL: pic, CoverURL: pic}))
		must(a.PublishTemplate(ctx, bid, "delivery", system))
		_, err = a.SaveMallListing(ctx, bid, app.MallListingInput{Listed: true, Tagline: s.tagline, CategoryIDs: ids(s.cats)})
		must(err)
		seedMallHistory(ctx, a, m.MerchantID, bid, s)
	}

	// The two older demo stores get categories and a cover, unless someone chose them already.
	for slug, cats := range map[string][]string{"makati": {"chinese", "snacks"}, "bgc": {"chinese"}} {
		var bid int64
		var n int
		if err := a.DB.QueryRow(ctx, `SELECT b.id, (SELECT count(*) FROM branch_categories c WHERE c.branch_id = b.id)
			FROM branches b WHERE b.slug=$1 AND b.deleted_at IS NULL`, slug).Scan(&bid, &n); err != nil {
			continue
		}
		if n == 0 {
			l, err := a.MallListing(ctx, bid)
			must(err)
			_, err = a.SaveMallListing(ctx, bid, app.MallListingInput{Listed: l.Listed, Tagline: l.Tagline, CategoryIDs: ids(cats)})
			must(err)
		}
		_, err := a.DB.Exec(ctx, `UPDATE channel_templates SET published = jsonb_set(published, '{cover_url}', '"/demo/beef-noodle-soup.jpg"')
			WHERE branch_id=$1 AND channel='delivery' AND coalesce(published->>'cover_url', '') = ''`, bid)
		must(err)
	}

	var banners int
	must(a.DB.QueryRow(ctx, `SELECT count(*) FROM mall_banners`).Scan(&banners))
	if banners == 0 {
		// the banners go on record as the demo administrator's
		var admin app.AdminActor
		must(a.DB.QueryRow(ctx, `SELECT id FROM users WHERE username='admin'`).Scan(&admin.ID))
		id, err := a.AdminSaveBanner(ctx, admin, 0, app.MallBannerInput{Title: "新店开业 · 免配送费", TitleEn: "New store · free delivery",
			Subtitle: "炸鸡研究所，满 ₱300 免配送费", SubtitleEn: "Fried Chicken Lab: free delivery over ₱300", LinkSlug: "chicken", Active: true})
		must(err)
		img, err := assets.ReadFile("assets/fried-chicken-cover.jpg")
		must(err)
		must(a.AdminSetBannerImage(ctx, admin, id, img))
		_, err = a.AdminSaveBanner(ctx, admin, 0, app.MallBannerInput{Title: "下午茶 · 满200减20", TitleEn: "Tea time · save 20 over 200",
			Subtitle: "茶屿奶茶", SubtitleEn: "Tea Isle", LinkSlug: "tea", Sort: 1, Active: true})
		must(err)
	}
	log.Print("marketplace: stores tea chicken bento cutlet luwei fries pasay (owner3 to owner9)")
}

// seedMallHistory gives a demo store a past: finished pickup orders spread
// over the last weeks (what "sold this month" counts) and a few reviews.
func seedMallHistory(ctx context.Context, a *app.App, merchantID, bid int64, s mallStore) {
	if s.sold == 0 {
		return
	}
	price := s.dishes[0].price
	_, err := a.DB.Exec(ctx, `INSERT INTO orders (merchant_id, branch_id, public_no, access_token_hash, token_nonce, pickup_code, service_type,
			customer_name, customer_phone, currency, subtotal, packaging_fee, delivery_fee, service_charge, tax_amount, tax_inclusive, tax_rate_bp, total,
			payment_method, accept_deadline, acceptance_status, packing_status, fulfillment_status, payment_status, created_at, accepted_at, completed_at, closed_at)
		SELECT $1, $2::bigint, 'M' || lpad(g::text, 4, '0'), sha256(('demo-' || $2::bigint::text || '-' || g)::bytea), decode(md5('demo' || $2::bigint::text || g), 'hex'), lpad(g::text, 4, '0'), 'pickup',
			'演示顾客', '+639170000000', 'PHP', $3::bigint, 0, 0, 0, 0, true, 1200, $3::bigint,
			'pay_at_pickup', x.t, 'accepted', 'packed', 'picked_up', 'paid', x.t, x.t, x.t + interval '40 minutes', x.t + interval '40 minutes'
		FROM generate_series(1, $4::int) g, LATERAL (SELECT now() - interval '3 hours' - g * interval '25 days' / $4::int AS t) x`,
		merchantID, bid, price, s.sold)
	must(err)
	for i, n := range s.stars {
		_, err := a.DB.Exec(ctx, `INSERT INTO reviews (branch_id, order_id, rating, taste, packaging, comment, display_name, created_at)
			SELECT $1, o.id, $2, $2, $2, '', '顾客 ' || $3::int::text, o.completed_at FROM orders o
			WHERE o.branch_id=$1 ORDER BY o.id OFFSET $3::int - 1 LIMIT 1`, bid, n, i+1)
		must(err)
	}
}
