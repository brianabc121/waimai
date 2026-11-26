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
	} {
		if got := PlatformPrice(c.price, c.markup, c.step); got != c.want {
			t.Errorf("PlatformPrice(%d, %d, %d) = %d, want %d", c.price, c.markup, c.step, got, c.want)
		}
	}
}

func TestPricingCheckSamples(t *testing.T) {
	store := Fees{TaxRateBP: 1200, ServiceChargeBP: 500, Packaging: 1000, Delivery: 6900}
	two := []Line{{Key: "chicken", Amount: 33800}, {Key: "water", Amount: 3000}}
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
