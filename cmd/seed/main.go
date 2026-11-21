// Command seed loads repeatable demo data: a platform admin, two merchants
// (to demonstrate tenant isolation), a bilingual menu, delivery areas and
// staff accounts. It is safe to run more than once.
package main

import (
	"context"
	"errors"
	"log"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/db"
)

const demoPassword = "demo1234"

func main() {
	cfg := config.Load()
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()
	a := app.New(pool, cfg.AppSecret)

	must(a.EnsurePlatformAdmin(ctx, "admin", demoPassword))
	seedStore(ctx, a, "makati", "马卡蒂小馆", "Makati Kitchen", "owner1", true)
	seedStore(ctx, a, "bgc", "BGC 面馆", "BGC Noodle House", "owner2", false)
	log.Printf("seed done. Demo password for every account: %s", demoPassword)
	log.Print("accounts: admin · owner1 cashier1 kitchen1 rider1 rider2 (store makati) · owner2 (store bgc)")
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func intp(v int) *int { return &v }

func seedStore(ctx context.Context, a *app.App, slug, name, nameEn, owner string, full bool) {
	m, err := a.CreateMerchant(ctx, app.NewMerchant{
		MerchantName: name, BranchName: name, BranchNameEn: nameEn, Slug: slug,
		Timezone: "Asia/Manila", Currency: "PHP",
		Owner: app.NewUser{Username: owner, Password: demoPassword, DisplayName: "店长 " + owner},
	})
	var ae *app.Error
	if errors.As(err, &ae) && (ae.Code == "SLUG_TAKEN" || ae.Code == "USERNAME_TAKEN") {
		log.Printf("store %s already exists, skipping", slug)
		return
	}
	must(err)
	bid := m.BranchID

	_, err = a.SaveSettings(ctx, bid, app.SettingsInput{
		Name: name, NameEn: nameEn, Address: "123 Ayala Ave, Makati", Phone: "+63 2 8123 4567",
		TaxRateBP: 1200, TaxInclusive: true, ServiceChargeBP: 0, PackagingFee: 1000,
		AcceptTimeoutMinutes: 10, DeliveryEnabled: true, PickupEnabled: true,
		OpenTime: "00:00", CloseTime: "00:00", NotifySMS: true,
	})
	must(err)

	for i, z := range []app.ZoneInput{
		{Name: "马卡蒂 CBD", NameEn: "Makati CBD", Fee: 4900, Active: true},
		{Name: "萨尔塞多村", NameEn: "Salcedo Village", Fee: 6900, Active: true},
		{Name: "BGC", NameEn: "BGC", Fee: 9900, MinOrder: 50000, Active: true},
	} {
		z.Sort = i
		_, err := a.SaveZone(ctx, bid, 0, z)
		must(err)
	}

	cat := func(name, en string, sort int) int64 {
		id, err := a.SaveCategory(ctx, bid, 0, app.CategoryInput{Name: name, NameEn: en, Sort: sort})
		must(err)
		return id
	}
	item := func(in app.ItemInput) {
		in.Published, in.DeliveryAvailable, in.PickupAvailable = true, true, true
		_, err := a.SaveItem(ctx, bid, 0, in)
		must(err)
	}
	spice := app.OptionGroupInput{Name: "辣度", NameEn: "Spice level", MinSelect: 1, MaxSelect: 1,
		Options: []app.OptionInput{
			{Name: "不辣", NameEn: "Not spicy"}, {Name: "微辣", NameEn: "Mild"},
			{Name: "中辣", NameEn: "Medium"}, {Name: "大辣", NameEn: "Hot"}}}

	station := func(name, en string, sort int) *int64 {
		id, err := a.SaveStation(ctx, bid, 0, app.StationInput{Name: name, NameEn: en, Active: true, Sort: sort})
		must(err)
		return &id
	}
	fryer, wok, bar := station("炸台", "Fryer", 0), station("热厨", "Hot kitchen", 1), station("饮品台", "Drinks", 2)

	fried := cat("炸物", "Fried", 0)
	item(app.ItemInput{CategoryID: fried, Name: "盐酥鸡", NameEn: "Popcorn Chicken", Price: 16900, NeedsPrep: true, StationID: fryer,
		Description: "现炸台式盐酥鸡", Groups: []app.OptionGroupInput{spice,
			{Name: "加料", NameEn: "Add-ons", MinSelect: 0, MaxSelect: 2, Options: []app.OptionInput{
				{Name: "加九层塔", NameEn: "Extra basil", PriceDelta: 1000},
				{Name: "加蒜", NameEn: "Extra garlic", PriceDelta: 500}}}}})
	item(app.ItemInput{CategoryID: fried, Name: "无盐地瓜条", NameEn: "Sweet Potato Fries", Price: 9900, NeedsPrep: true, StationID: fryer})
	if !full {
		return
	}
	item(app.ItemInput{CategoryID: fried, Name: "黄金鸡排", NameEn: "Crispy Chicken Cutlet", Price: 18900,
		NeedsPrep: true, StationID: fryer, Groups: []app.OptionGroupInput{spice}})

	mains := cat("主食", "Mains", 1)
	item(app.ItemInput{CategoryID: mains, Name: "卤肉饭", NameEn: "Braised Pork Rice", Price: 15900, NeedsPrep: true, StationID: wok})
	item(app.ItemInput{CategoryID: mains, Name: "牛肉面", NameEn: "Beef Noodle Soup", Price: 24900, NeedsPrep: true, StationID: wok,
		Groups: []app.OptionGroupInput{spice}})
	item(app.ItemInput{CategoryID: mains, Name: "今日限量便当", NameEn: "Daily Bento (limited)", Price: 21900,
		NeedsPrep: true, StationID: wok, StockQty: intp(5), Description: "每日限量 5 份"})

	combos := cat("套餐", "Combos", 2)
	item(app.ItemInput{CategoryID: combos, Name: "双人炸物套餐", NameEn: "Fried Combo for Two", Price: 39900,
		NeedsPrep: true, StationID: fryer, Description: "任选两款炸物 + 一款饮品",
		Groups: []app.OptionGroupInput{
			{Name: "任选两款炸物", NameEn: "Choose two fried items", MinSelect: 2, MaxSelect: 2, Options: []app.OptionInput{
				{Name: "盐酥鸡", NameEn: "Popcorn Chicken"}, {Name: "地瓜条", NameEn: "Sweet Potato Fries"},
				{Name: "鸡排", NameEn: "Chicken Cutlet", PriceDelta: 2000}}},
			{Name: "饮品", NameEn: "Drink", MinSelect: 1, MaxSelect: 1, Options: []app.OptionInput{
				{Name: "冰柠檬茶", NameEn: "Iced Lemon Tea"}, {Name: "珍珠奶茶", NameEn: "Bubble Milk Tea", PriceDelta: 3000}}}}})

	drinks := cat("饮品", "Drinks", 3)
	item(app.ItemInput{CategoryID: drinks, Name: "冰柠檬茶", NameEn: "Iced Lemon Tea", Price: 6900, NeedsPrep: true, StationID: bar})
	item(app.ItemInput{CategoryID: drinks, Name: "珍珠奶茶", NameEn: "Bubble Milk Tea", Price: 11900, NeedsPrep: true, StationID: bar})
	item(app.ItemInput{CategoryID: drinks, Name: "瓶装水", NameEn: "Bottled Water", Price: 3000, NeedsPrep: false})

	i64 := func(v int64) *int64 { return &v }
	for _, p := range []app.PromotionInput{
		{Name: "满200减20", NameEn: "Save 20 over 200", Kind: "amount_off", Threshold: 20000, Value: 2000,
			AppliesDelivery: true, AppliesPickup: true},
		{Name: "满500免配送费", NameEn: "Free delivery over 500", Kind: "free_delivery", Threshold: 50000, AppliesDelivery: true},
		{Name: "夜宵九折", NameEn: "Late-night 10% off", Kind: "percent_off", Value: 1000, MaxDiscount: i64(10000),
			DailyStart: "22:00", DailyEnd: "02:00", AppliesDelivery: true, AppliesPickup: true},
	} {
		id, err := a.SavePromotion(ctx, bid, 0, p, app.Actor{Type: "system"})
		must(err)
		must(a.SetPromotionStatus(ctx, bid, id, "active", app.Actor{Type: "system"}))
	}

	for _, u := range []app.NewUser{
		{Username: "cashier1", DisplayName: "收银 小陈", Role: "cashier"},
		{Username: "kitchen1", DisplayName: "后厨 阿明", Role: "kitchen"},
		{Username: "rider1", DisplayName: "骑手 Juan", Role: "rider", Phone: "+639171234567"},
		{Username: "rider2", DisplayName: "骑手 Maria", Role: "rider", Phone: "+639181234567"},
	} {
		u.Password = demoPassword
		_, err := a.CreateStaff(ctx, bid, u)
		must(err)
	}
}
