package pricing

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// The hand-worked samples of docs/PRICING_CHECK.md §4. Every number here was
// worked out by hand (and again in a separate script), not read off the
// engine: if one changes, the engine changed how money is worked out, and the
// document and brian must agree first.
//
// The store: tax 12% not included, service charge 5%, packaging 10.00,
// delivery 69.00; chicken ×2 = 338.00, water = 30.00.
// S9a platform prices, worked out by hand (PRICING_CHECK.md §4).
func TestPlatformPrice(t *testing.T) {
	for _, c := range []struct {
		price  int64
		markup int
		step   int64
		want   int64
	}{
		{16900, 2000, 100, 20300},  // 169.00 × 1.2 = 202.80 → 203.00
		{3000, 2000, 100, 3600},    // 30.00 × 1.2 = 36.00
		{16900, 2000, 0, 20280},    // not rounded: 202.80
		{16900, 2000, 500, 20500},  // to 5: 202.80 → 205.00
		{16900, 2000, 1000, 20000}, // to 10: 202.80 → 200.00
		{9999, 1250, 0, 11249},     // 99.99 × 1.125 = 112.48875 → 112.49
		{17999, 1250, 100, 20200},  // 179.99 × 1.125 = 202.48875 → 202.00 (once, not via 202.49)
		{12750, 0, 500, 13000},     // no markup, to 5: 127.50 → 130.00 (half up)
		{12740, 0, 500, 12500},     // 127.40 → 125.00
		{0, 3000, 100, 0},
		// S10a (docs/MULTI_BRAND_DESIGN.md §11.4): each brand's price with its own markup on Grab
		{5200, 1500, 100, 6000}, // 好吃党 Grab: 辣椒炒肉 52.00 × 1.15 = 59.80 → 60.00
		{300, 1500, 100, 300},   // 好吃党 Grab: 米饭 3.00 × 1.15 = 3.45 → 3.00
		{5800, 2000, 100, 7000}, // 一品湘 Grab: 小炒肉 58.00 × 1.2 = 69.60 → 70.00
		{300, 2000, 100, 400},   // 一品湘 Grab: 米饭 3.00 × 1.2 = 3.60 → 4.00
	} {
		if got := PlatformPrice(c.price, c.markup, c.step); got != c.want {
			t.Errorf("PlatformPrice(%d, %d, %d) = %d, want %d", c.price, c.markup, c.step, got, c.want)
		}
	}
}

// TestPlatformCommission: S9's commission (docs/ORDER_TYPES_DESIGN.md §10.3), hand worked.
func TestPlatformCommission(t *testing.T) {
	for _, c := range []struct {
		base int64
		rate int
		want int64
	}{
		{39200, 3000, 11760}, // 30% × 392.00 = 117.60; payout 392.00 − 117.60 = 274.40
		{36007, 3000, 10802}, // 30% × 360.07 = 108.021 → 108.02; payout 252.05
		{44200, 3000, 13260}, // on the original amount: 30% × 442.00 = 132.60
		{33333, 1550, 5167},  // 15.5% × 333.33 = 51.666 → 51.67
		{10001, 2250, 2250},  // 22.5% × 100.01 = 22.50225 → 22.50
		{100, 5000, 50},
		{0, 3000, 0},
		{50000, 0, 0},
		// S10a: 好吃党 Grab 25% of 126.00 = 31.50, payout 94.50; 一品湘 Grab 30% of 148.00 = 44.40, payout 103.60
		{12600, 2500, 3150},
		{14800, 3000, 4440},
	} {
		if got := Commission(c.base, c.rate); got != c.want {
			t.Errorf("Commission(%d, %d) = %d, want %d", c.base, c.rate, got, c.want)
		}
	}
}

func TestPricingCheckSamples(t *testing.T) {
	store := Fees{TaxRateBP: 1200, ServiceChargeBP: 500, Packaging: 1000, Delivery: 6900}
	two := []Line{{Key: "chicken", Amount: 33800}, {Key: "water", Amount: 3000}}
	pickup := Fees{TaxRateBP: 1200, ServiceChargeBP: 500, Packaging: 1000}
	type want struct {
		goods, staff, ship, svc, tax, platform, round, total int64
		disc, svcs, taxs                                     []int64 // per line
	}
	cases := []struct {
		name string
		in   Input
		want want
	}{
		{"S1 外卖无优惠", Input{Lines: two, Fees: store},
			want{svc: 1840, tax: 4757, total: 51297, disc: []int64{0, 0}, svcs: []int64{1690, 150}, taxs: []int64{4259, 378}}},
		{"S2 满减100 + 无门槛券50，分摊到每道菜", Input{Lines: two, Fees: store, Adjustments: []Adjustment{
			{Kind: "promotion", Stage: StageOrder, Amount: 10000},
			{Kind: "store_free", Stage: StageOrder, Amount: 5000}}},
			want{goods: 15000, svc: 1090, tax: 2867, total: 33657,
				disc: []int64{13777, 1223}, svcs: []int64{1001, 89}, taxs: []int64{2523, 224}}},
		{"S3 满减 + 9折 + 配送费券20 + 平台券10 + 抹零0.97", Input{Lines: two, Fees: store, Adjustments: []Adjustment{
			{Kind: "round_off", Stage: StageRound, Amount: 97}, // given out of order on purpose: stages decide
			{Kind: "platform_free", Stage: StagePlatform, Amount: 1000},
			{Kind: "staff_percent", Stage: StageStaff, PercentBP: 9000},
			{Kind: "store_delivery", Stage: StageDelivery, Amount: 2000},
			{Kind: "promotion", Stage: StageOrder, Amount: 10000}}},
			want{goods: 10000, staff: 2680, ship: 2000, svc: 1206, tax: 3159, platform: 1000, round: 97, total: 33288,
				disc: []int64{11647, 1033}, svcs: []int64{1108, 98}, taxs: []int64{2791, 248}}},
		{"S4 堂食含税 8折活动 服务费10%", Input{
			Lines:       []Line{{Key: "a", Amount: 16900}, {Key: "b", Amount: 16900}, {Key: "w", Amount: 3000}},
			Fees:        Fees{TaxRateBP: 1200, TaxInclusive: true, ServiceChargeBP: 1000},
			Adjustments: []Adjustment{{Kind: "promotion", Stage: StageOrder, PercentBP: 2000}}},
			want{goods: 7360, svc: 2944, tax: 3470, total: 32384,
				disc: []int64{3380, 3380, 600}, svcs: []int64{1352, 1352, 240}, taxs: []int64{1594, 1593, 283}}},
		{"S5 第二份半价只落在鸡上，再满减", Input{Lines: two, Fees: store, Adjustments: []Adjustment{
			{Kind: "dish_second", Stage: StageItem, Amount: 8450, Lines: []string{"chicken"}},
			{Kind: "promotion", Stage: StageOrder, Amount: 10000}}},
			want{goods: 18450, svc: 918, tax: 2432, total: 29600,
				disc: []int64{17392, 1058}, svcs: []int64{821, 97}, taxs: []int64{2067, 245}}},
		{"S6 改实收 300.00", Input{Lines: two, Fees: store, Adjustments: []Adjustment{
			{Kind: "promotion", Stage: StageOrder, Amount: 10000},
			{Kind: "set_total", Stage: StageRound, Amount: 30000}}},
			want{goods: 10000, svc: 1340, tax: 3497, round: 9537, total: 30000,
				disc: []int64{9185, 815}, svcs: []int64{1231, 109}, taxs: []int64{3102, 275}}},
		{"S7 含税 + 免配送费：配送费不计税", Input{Lines: []Line{{Key: "x", Amount: 10000}},
			Fees:        Fees{TaxRateBP: 1200, TaxInclusive: true, Packaging: 500, Delivery: 6900},
			Adjustments: []Adjustment{{Kind: "promotion", Stage: StageDelivery, Whole: true}}},
			want{ship: 6900, tax: 1125, total: 10500, disc: []int64{0}, svcs: []int64{0}, taxs: []int64{1071}}},
		// docs/ORDER_TYPES_DESIGN.md §10.3: a Grab order at platform prices (chicken 203.00 × 2, water 36.00),
		// tax included, the store's offer 50.00 on the whole order, Grab's own 20.00 after tax.
		{"S9 平台单：商家出资50 + 平台出资20，含税", Input{
			Lines: []Line{{Key: "chicken", Amount: 40600}, {Key: "water", Amount: 3600}},
			Fees:  Fees{TaxRateBP: 1200, TaxInclusive: true},
			Adjustments: []Adjustment{
				{Kind: "platform_promo", FundedBy: "platform", Stage: StagePlatform, Amount: 2000},
				{Kind: "platform_merchant_promo", FundedBy: "store", Stage: StageOrder, Amount: 5000}}},
			want{goods: 5000, tax: 4200, platform: 2000, total: 37200,
				disc: []int64{4593, 407}, svcs: []int64{0, 0}, taxs: []int64{3858, 342}}},
		// The water voided by the store: the store's offer keeps the chicken's share, 45.93.
		{"S9 店铺原因退一瓶水", Input{
			Lines: []Line{{Key: "chicken", Amount: 40600}},
			Fees:  Fees{TaxRateBP: 1200, TaxInclusive: true},
			Adjustments: []Adjustment{
				{Kind: "platform_merchant_promo", FundedBy: "store", Stage: StageOrder, Amount: 4593},
				{Kind: "platform_promo", FundedBy: "platform", Stage: StagePlatform, Amount: 2000}}},
			want{goods: 4593, tax: 3858, platform: 2000, total: 34007,
				disc: []int64{4593}, svcs: []int64{0}, taxs: []int64{3858}}},
		// docs/MULTI_BRAND_DESIGN.md §11.3: one cart (pork × 2, rice × 2) at two brands of one store,
		// 12% tax included, delivery 50.00. The engine sees only each brand's prices and promotion.
		{"S10 一品湘：库价 58.00，满100减15", Input{
			Lines:       []Line{{Key: "pork", Amount: 11600}, {Key: "rice", Amount: 600}},
			Fees:        Fees{TaxRateBP: 1200, TaxInclusive: true, Delivery: 5000},
			Adjustments: []Adjustment{{Kind: "promotion", Stage: StageOrder, Amount: 1500}}},
			want{goods: 1500, tax: 1146, total: 15700,
				disc: []int64{1426, 74}, svcs: []int64{0, 0}, taxs: []int64{1090, 56}}},
		{"S10 好吃党：品牌价 52.00，满100减10", Input{
			Lines:       []Line{{Key: "pork", Amount: 10400}, {Key: "rice", Amount: 600}},
			Fees:        Fees{TaxRateBP: 1200, TaxInclusive: true, Delivery: 5000},
			Adjustments: []Adjustment{{Kind: "promotion", Stage: StageOrder, Amount: 1000}}},
			want{goods: 1000, tax: 1071, total: 15000,
				disc: []int64{945, 55}, svcs: []int64{0, 0}, taxs: []int64{1013, 58}}},
		{"S10 好吃党没有自己的活动：一品湘的不跨品牌", Input{
			Lines: []Line{{Key: "pork", Amount: 10400}, {Key: "rice", Amount: 600}},
			Fees:  Fees{TaxRateBP: 1200, TaxInclusive: true, Delivery: 5000}},
			want{tax: 1179, total: 16000,
				disc: []int64{0, 0}, svcs: []int64{0, 0}, taxs: []int64{1115, 64}}},
		// S12, S13 (docs/CODE_REVIEW_FIX_PLAN.md H07): a pickup of three waters, 15.00 off, and
		// the order after one and after two of them are refunded.
		{"S12 自取三瓶水，第二份半价", Input{Lines: []Line{{Key: "water", Amount: 9000}}, Fees: pickup, Adjustments: []Adjustment{
			{Kind: "dish_second", Stage: StageItem, Amount: 1500, Lines: []string{"water"}}}},
			want{goods: 1500, svc: 375, tax: 1065, total: 9940, disc: []int64{1500}, svcs: []int64{375}, taxs: []int64{945}}},
		{"S12 退一瓶后：两瓶，第二份半价", Input{Lines: []Line{{Key: "water", Amount: 6000}}, Fees: pickup, Adjustments: []Adjustment{
			{Kind: "dish_second", Stage: StageItem, Amount: 1500, Lines: []string{"water"}}}},
			want{goods: 1500, svc: 225, tax: 687, total: 6412, disc: []int64{1500}, svcs: []int64{225}, taxs: []int64{567}}},
		{"S13 退一瓶后：两瓶，不到满减门槛", Input{Lines: []Line{{Key: "water", Amount: 6000}}, Fees: pickup},
			want{svc: 300, tax: 876, total: 8176, disc: []int64{0}, svcs: []int64{300}, taxs: []int64{756}}},
		{"S12 S13 退两瓶后：一瓶", Input{Lines: []Line{{Key: "water", Amount: 3000}}, Fees: pickup},
			want{svc: 150, tax: 498, total: 4648, disc: []int64{0}, svcs: []int64{150}, taxs: []int64{378}}},
		{"S13 店铺原因退一瓶后：满减留 10.00", Input{Lines: []Line{{Key: "water", Amount: 6000}}, Fees: pickup, Adjustments: []Adjustment{
			{Kind: "promotion", Stage: StageOrder, Amount: 1000}}},
			want{goods: 1000, svc: 250, tax: 750, total: 7000, disc: []int64{1000}, svcs: []int64{250}, taxs: []int64{630}}},
		{"S13 店铺原因退两瓶后：满减留 5.00", Input{Lines: []Line{{Key: "water", Amount: 3000}}, Fees: pickup, Adjustments: []Adjustment{
			{Kind: "promotion", Stage: StageOrder, Amount: 500}}},
			want{goods: 500, svc: 125, tax: 435, total: 4060, disc: []int64{500}, svcs: []int64{125}, taxs: []int64{315}}},
		// S15 (docs/CODE_REVIEW_FIX_PLAN.md M01): four waters, 15.00 off for spending 90.00.
		// 120.00 − 15.00 = 105.00, service 5.25, packaging 10.00, tax 120.25 × 12% = 14.43.
		// After one is returned the order is S13's three waters with the same 15.00 off: 99.40.
		{"S15 自取四瓶水，满 90 减 15", Input{Lines: []Line{{Key: "water", Amount: 12000}}, Fees: pickup, Adjustments: []Adjustment{
			{Kind: "promotion", Stage: StageOrder, Amount: 1500}}},
			want{goods: 1500, svc: 525, tax: 1443, total: 13468, disc: []int64{1500}, svcs: []int64{525}, taxs: []int64{1323}}},
		{"S15 退一瓶后：三瓶，仍按下单时的满 90 减 15", Input{Lines: []Line{{Key: "water", Amount: 9000}}, Fees: pickup, Adjustments: []Adjustment{
			{Kind: "promotion", Stage: StageOrder, Amount: 1500}}},
			want{goods: 1500, svc: 375, tax: 1065, total: 9940, disc: []int64{1500}, svcs: []int64{375}, taxs: []int64{945}}},
		// S16 (docs/CODE_REVIEW_FIX_PLAN.md M02): a tea delivered (161.34, sample S11 C) gets a water added.
		// 99.00, service 4.95, packaging 10.00, tax 113.95 × 12% = 13.67, delivery 69.00 untaxed: 196.62.
		// Each dish's tax: tea (69.00 + 3.45) × 12% = 8.69, water (30.00 + 1.50) × 12% = 3.78; packaging's 1.20.
		{"S16 外卖茶加一瓶水", Input{Lines: []Line{{Key: "tea", Amount: 6900}, {Key: "water", Amount: 3000}},
			Fees: Fees{TaxRateBP: 1200, ServiceChargeBP: 500, Packaging: 1000, Delivery: 6900}},
			want{svc: 495, tax: 1367, total: 19662, disc: []int64{0, 0}, svcs: []int64{345, 150}, taxs: []int64{869, 378}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Price(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if bad := Check(c.in, r); bad != nil {
				t.Fatal(strings.Join(bad, "\n"))
			}
			g := r.Totals
			got := want{goods: g.GoodsDiscount, staff: g.StaffDiscount, ship: g.DeliveryDiscount, svc: g.ServiceCharge,
				tax: g.Tax, platform: g.PlatformDiscount, round: g.RoundOff, total: g.Total}
			for _, l := range r.Lines {
				got.disc, got.svcs, got.taxs = append(got.disc, l.Discount), append(got.svcs, l.Service), append(got.taxs, l.Tax)
			}
			w := c.want
			if got.goods != w.goods || got.staff != w.staff || got.ship != w.ship || got.svc != w.svc || got.tax != w.tax ||
				got.platform != w.platform || got.round != w.round || got.total != w.total ||
				!slices.Equal(got.disc, w.disc) || !slices.Equal(got.svcs, w.svcs) || !slices.Equal(got.taxs, w.taxs) {
				t.Fatalf("\n got %+v\nwant %+v", got, w)
			}
		})
	}
}

// randomInput is an order with every kind of adjustment the engine knows, in
// a random order, with odd amounts (0, more than there is, below 0).
func randomInput(rng *rand.Rand) Input {
	n := 1 + rng.Intn(7)
	in := Input{Fees: Fees{TaxRateBP: []int{0, 600, 700, 1000, 1200}[rng.Intn(5)], TaxInclusive: rng.Intn(2) == 0,
		ServiceChargeBP: []int{0, 500, 1000}[rng.Intn(3)], Packaging: int64(rng.Intn(3)) * 499,
		Delivery: int64(rng.Intn(3)) * 3450}}
	keys := []string{}
	for i := 0; i < n; i++ {
		k := string(rune('a' + i))
		keys = append(keys, k)
		amt := int64(rng.Intn(60000))
		if rng.Intn(10) == 0 {
			amt = 0
		}
		in.Lines = append(in.Lines, Line{Key: k, Amount: amt})
	}
	amt := func(top int) int64 {
		switch rng.Intn(8) {
		case 0:
			return 0
		case 1:
			return -int64(rng.Intn(100))
		case 2:
			return int64(rng.Intn(top)) * 50 // can be more than there is
		}
		return int64(rng.Intn(top))
	}
	pick := func() []string {
		var ks []string
		for _, k := range keys {
			if rng.Intn(2) == 0 {
				ks = append(ks, k)
			}
		}
		return ks
	}
	for k := rng.Intn(9); k > 0; k-- {
		var a Adjustment
		switch rng.Intn(11) {
		case 0:
			a = Adjustment{Kind: "dish_percent", Stage: StageItem, PercentBP: 1 + rng.Intn(10000), Lines: pick()}
		case 1:
			a = Adjustment{Kind: "dish_second", Stage: StageItem, Amount: amt(20000), Lines: pick()}
		case 2:
			a = Adjustment{Kind: "promotion", Stage: StageOrder, Amount: amt(30000)}
		case 3:
			a = Adjustment{Kind: "promotion", Stage: StageOrder, PercentBP: 1 + rng.Intn(5000), Cap: int64(rng.Intn(3)) * 2000}
		case 4:
			a = Adjustment{Kind: "store_free", Stage: StageOrder, Amount: amt(8000)}
		case 5:
			a = Adjustment{Kind: "store_free", Stage: StageOrder, Whole: true}
		case 6:
			a = Adjustment{Kind: "staff_percent", Stage: StageStaff, PercentBP: []int{0, 9000, 8500, 9999, 1}[rng.Intn(5)]}
		case 7:
			a = Adjustment{Kind: "store_delivery", Stage: StageDelivery, Amount: amt(5000), Whole: rng.Intn(4) == 0}
		case 8:
			a = Adjustment{Kind: "platform_free", Stage: StagePlatform, Amount: amt(5000)}
		case 9:
			a = Adjustment{Kind: "round_off", Stage: StageRound, Amount: int64(rng.Intn(100))}
		case 10:
			a = Adjustment{Kind: "set_total", Stage: StageRound, Amount: int64(rng.Intn(40000))}
		}
		in.Adjustments = append(in.Adjustments, a)
	}
	return in
}

// The rules of §2 hold for any order.
func TestPricingCheckRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(20261125))
	errs := 0
	for k := 0; k < 50000; k++ {
		in := randomInput(rng)
		r, err := Price(in)
		if err != nil {
			errs++
			continue // a round-off or amount to pay above the bill is refused, nothing is saved
		}
		if bad := Check(in, r); bad != nil {
			t.Fatalf("case %d %+v:\n%s", k, in, strings.Join(bad, "\n"))
		}
	}
	if errs > 25000 {
		t.Fatalf("%d of 50000 refused: the cases test too little", errs)
	}
}

// §3 the answers do not depend on how the order is written down.
func TestPricingCheckMetamorphic(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for k := 0; k < 20000; k++ {
		in := randomInput(rng)
		r, err := Price(in)
		if err != nil {
			continue
		}
		// M1 the lines in another order: the same totals
		sh := in
		sh.Lines = slices.Clone(in.Lines)
		rng.Shuffle(len(sh.Lines), func(i, j int) { sh.Lines[i], sh.Lines[j] = sh.Lines[j], sh.Lines[i] })
		if r2, err := Price(sh); err != nil || r2.Totals != r.Totals {
			t.Fatalf("case %d: lines reordered %+v\n%+v", k, r2.Totals, r.Totals)
		}
		// M2 an adjustment that takes nothing off changes nothing
		z := in
		z.Adjustments = append(slices.Clone(in.Adjustments), Adjustment{Kind: "promotion", Stage: StageOrder})
		if r2, err := Price(z); err != nil || r2.Totals != r.Totals {
			t.Fatalf("case %d: a 0 promotion changed %+v\n%+v", k, r2.Totals, r.Totals)
		}
		// M3 a bigger discount on the dishes never makes the bill bigger
		// (before a typed amount to pay, which fixes it)
		if !slices.ContainsFunc(in.Adjustments, func(a Adjustment) bool { return a.Stage == StageRound }) {
			more := in
			more.Adjustments = append(slices.Clone(in.Adjustments),
				Adjustment{Kind: "store_free", Stage: StageOrder, Amount: 1 + int64(rng.Intn(5000))})
			if r2, err := Price(more); err != nil || r2.Totals.Total > r.Totals.Total {
				t.Fatalf("case %d: more off, total %d > %d", k, r2.Totals.Total, r.Totals.Total)
			}
		}
		// M4 tax not included costs exactly the tax more than tax included
		// at the same rate on the same order, before the platform and round-offs
		ex, inc := in, in
		ex.Fees.TaxInclusive, inc.Fees.TaxInclusive = false, true
		ex.Adjustments = withoutStages(in.Adjustments, StagePlatform, StageRound)
		inc.Adjustments = ex.Adjustments
		re, err1 := Price(ex)
		ri, err2 := Price(inc)
		if err1 != nil || err2 != nil || re.Totals.Total-ri.Totals.Total != re.Totals.Tax {
			t.Fatalf("case %d: tax excluded %d, included %d, tax %d", k, re.Totals.Total, ri.Totals.Total, re.Totals.Tax)
		}
	}
}

func withoutStages(as []Adjustment, ss ...Stage) []Adjustment {
	out := []Adjustment{}
	for _, a := range as {
		if !slices.Contains(ss, a.Stage) {
			out = append(out, a)
		}
	}
	return out
}

// Check itself catches a wrong price: each of these is a price made wrong by
// hand and must be reported.
func TestPricingCheckCatches(t *testing.T) {
	in := Input{Lines: []Line{{Key: "a", Amount: 33800}, {Key: "b", Amount: 3000}},
		Fees: Fees{TaxRateBP: 1200, ServiceChargeBP: 500, Packaging: 1000, Delivery: 6900},
		Adjustments: []Adjustment{{Kind: "promotion", Stage: StageOrder, Amount: 10000},
			{Kind: "staff_percent", Stage: StageStaff, PercentBP: 9000}}}
	good, err := Price(in)
	if err != nil || Check(in, good) != nil {
		t.Fatal(err, Check(in, good))
	}
	breaks := map[string]func(r *Result){
		"total off by one":           func(r *Result) { r.Totals.Total++ },
		"tax off by one":             func(r *Result) { r.Totals.Tax++ },
		"share moved between lines":  func(r *Result) { r.Applied[0].PerLine["a"]--; r.Applied[0].PerLine["b"]++ },
		"line discount not shared":   func(r *Result) { r.Lines[1].Discount++; r.Lines[1].Net-- },
		"service not shared whole":   func(r *Result) { r.Lines[0].Service-- },
		"promotion took more":        func(r *Result) { r.Applied[0].Off++; r.Applied[0].PerLine["a"]++ },
		"staff percent on the total": func(r *Result) { r.Applied[1].Off += 100 },
	}
	for name, br := range breaks {
		r, _ := Price(in)
		br(&r)
		if Check(in, r) == nil {
			t.Errorf("%s: not caught", name)
		}
	}
}

// W-S1, W-S2 (docs/PRICING_CHECK.md §4): the cash share of a refund from an
// order paid partly from a store balance is rounded down, worked out by hand.
// The whole split (running total, what was refunded before) is
// TestRefundSplitSamples in internal/app.
func TestMulDivFloorSamples(t *testing.T) {
	for _, c := range []struct{ a, b, d, want int64 }{
		{6000, 10000, 15000, 4000},   // W-S1: 60.00 × 100/150 = 40.00
		{9999, 24900, 29900, 8326},   // W-S2: 8326.93 → 8326
		{29900, 24900, 29900, 24900}, // W-S2, all of it: exactly the cash paid
		{1, 2, 3, 0},                 // 0.67 → 0
	} {
		if got := MulDivFloor(c.a, c.b, c.d); got != c.want {
			t.Errorf("MulDivFloor(%d, %d, %d) = %d, want %d", c.a, c.b, c.d, got, c.want)
		}
	}
}
