package pricing

import (
	"math/rand"
	"testing"
)

// legacy is the arithmetic of app.ComputeTotals, adjustTotals and
// applyPlatform before the engine (PR #114), kept to prove the engine gives
// the same amounts.
func legacy(sub, fee int64, f Fees, goods, ship int64, staffBP int, platf int64, round int64) (Totals, error) {
	goods = min(goods, sub)
	ship = min(ship, fee)
	staff := int64(0)
	if staffBP > 0 {
		left := sub - goods
		staff = left - MulDivRound(left, int64(staffBP), 10000)
	}
	g := sub - goods - staff
	t := Totals{Subtotal: sub, Packaging: f.Packaging, Delivery: fee, TaxInclusive: f.TaxInclusive, TaxRateBP: f.TaxRateBP,
		GoodsDiscount: goods, DeliveryDiscount: ship, StaffDiscount: staff}
	t.ServiceCharge = MulDivRound(g, int64(f.ServiceChargeBP), 10000)
	base := g + f.Packaging + t.ServiceCharge
	r := int64(f.TaxRateBP)
	if f.TaxInclusive {
		t.Tax = MulDivRound(base, r, 10000+r)
	} else {
		t.Tax = MulDivRound(base, r, 10000)
	}
	t.Total = sub + f.Packaging + fee + t.ServiceCharge - goods - staff - ship
	if !f.TaxInclusive {
		t.Total += t.Tax
	}
	p := min(max(platf, 0), t.Total)
	t.PlatformDiscount = p
	t.Total -= p
	if round > t.Total {
		return t, ErrRoundTooBig
	}
	t.RoundOff = round
	t.Total -= round
	return t, nil
}

func TestPriceMatchesLegacyAndAddsUp(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for k := 0; k < 20000; k++ {
		n := 1 + rng.Intn(6)
		in := Input{Fees: Fees{TaxRateBP: rng.Intn(3) * 600, TaxInclusive: rng.Intn(2) == 0,
			ServiceChargeBP: rng.Intn(3) * 500, Packaging: int64(rng.Intn(3)) * 500}}
		var sub int64
		for i := 0; i < n; i++ {
			a := int64(rng.Intn(50000))
			sub += a
			in.Lines = append(in.Lines, Line{Key: string(rune('a' + i)), Amount: a})
		}
		in.Fees.Delivery = int64(rng.Intn(3)) * 3450
		promo := int64(rng.Intn(20000))
		coupon := int64(rng.Intn(8000))
		ship := int64(rng.Intn(5000))
		staffBP := []int{0, 9000, 8500, 9999}[rng.Intn(4)]
		platf := int64(rng.Intn(3000))
		round := int64(rng.Intn(3)) * 7
		in.Adjustments = []Adjustment{
			{Kind: "promotion", Stage: StageOrder, Amount: promo, FundedBy: "store"},
			{Kind: "store_free", Stage: StageOrder, Amount: coupon, FundedBy: "store"},
			{Kind: "store_delivery", Stage: StageDelivery, Amount: ship, FundedBy: "store"},
			{Kind: "staff_percent", Stage: StageStaff, PercentBP: staffBP},
			{Kind: "platform_free", Stage: StagePlatform, Amount: platf, FundedBy: "platform"},
			{Kind: "round_off", Stage: StageRound, Amount: round},
		}
		got, err := Price(in)
		want, werr := legacy(sub, in.Fees.Delivery, in.Fees, promo+coupon, ship, staffBP, platf, round)
		if (err == nil) != (werr == nil) {
			t.Fatalf("case %d: err %v, legacy %v", k, err, werr)
		}
		if err != nil {
			continue
		}
		if got.Totals != want {
			t.Fatalf("case %d:\n got %+v\nwant %+v", k, got.Totals, want)
		}
		// everything shared out adds up
		var disc, net, svc, tax, amt int64
		for _, l := range got.Lines {
			if l.Net != l.Amount-l.Discount || l.Net < 0 || l.Service < 0 || l.Tax < 0 {
				t.Fatalf("case %d: line %+v", k, l)
			}
			amt += l.Amount
			disc += l.Discount
			net += l.Net
			svc += l.Service
			tax += l.Tax
		}
		if amt != sub || disc != got.Totals.GoodsDiscount+got.Totals.StaffDiscount || svc != got.Totals.ServiceCharge || tax > got.Totals.Tax {
			t.Fatalf("case %d: shares disc=%d svc=%d tax=%d of %+v", k, disc, svc, tax, got.Totals)
		}
		for _, a := range got.Applied {
			var s int64
			for _, v := range a.PerLine {
				s += v
			}
			if a.PerLine != nil && s != a.Off {
				t.Fatalf("case %d: %s shared %d of %d", k, a.Kind, s, a.Off)
			}
		}
	}
}

func TestItemDiscountOnlyOnItsLines(t *testing.T) {
	r, err := Price(Input{Lines: []Line{{Key: "beef", Amount: 20000}, {Key: "tea", Amount: 5000}},
		Adjustments: []Adjustment{
			{Kind: "item_percent", Stage: StageItem, PercentBP: 5000, Lines: []string{"beef"}},
			{Kind: "promotion", Stage: StageOrder, Amount: 3000},
		}})
	if err != nil {
		t.Fatal(err)
	}
	// half off the beef (100.00), then 30.00 over 100.00 + 50.00: 20.00 and 10.00
	if r.Lines[0].Discount != 12000 || r.Lines[1].Discount != 1000 || r.Totals.Total != 12000 {
		t.Fatalf("%+v %+v", r.Lines, r.Totals)
	}
}

func TestShare(t *testing.T) {
	got := Share(100, []int64{1, 1, 1}, []bool{true, true, true})
	if got[0]+got[1]+got[2] != 3 && got[0]+got[1]+got[2] != 100 {
		t.Fatal(got)
	}
	got = Share(10, []int64{3, 3, 3}, []bool{true, true, true})
	if got[0]+got[1]+got[2] != 9 { // never more than the weights
		t.Fatal(got)
	}
	got = Share(7, []int64{100, 200, 0, 400}, []bool{true, true, true, false})
	if got[2] != 0 || got[3] != 0 || got[0]+got[1] != 7 {
		t.Fatal(got)
	}
}
