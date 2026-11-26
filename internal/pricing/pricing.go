// Package pricing is the one place an order's money is worked out
// (docs/PRICING_REVIEW.md §5). It is a pure function: it reads nothing and
// writes nothing, so the same input always gives the same amounts, and every
// screen that shows or changes an order (quote, order, add-on, void, discount
// at checkout, table bill, refund) prices it here.
//
// Amounts are integers in hundredths of the currency. Rates are basis points
// (1200 = 12%). Rounding is half up, once per amount.
//
// The order of work is fixed (§5.1):
//
//  1. dishes at their price
//  2. discounts on chosen dishes (StageItem)
//  3. discounts on all the dishes: promotions, store coupons (StageOrder)
//  4. the percentage off given at checkout (StageStaff)
//  5. service charge, packaging and delivery fees; discounts on the delivery fee
//  6. tax
//  7. what the platform pays for (StagePlatform)
//  8. a round-off or a typed amount to pay (StageRound)
//
// Every discount on dishes is shared out to the dishes it covers, in
// proportion to what each still costs, so that a dish can later be refunded
// for exactly what was paid for it.
package pricing

import (
	"errors"
	"math/bits"
	"sort"
)

// Stage is where in the order of work an adjustment is taken off.
type Stage int

const (
	StageItem     Stage = iota + 1 // chosen dishes (second at half price, a dish at 20% off)
	StageOrder                     // all the dishes (spend X save Y, store coupons)
	StageStaff                     // the percentage off given at checkout
	StageDelivery                  // the delivery fee (free delivery, delivery coupons)
	StagePlatform                  // paid by the platform, off what is left to pay
	StageRound                     // a round-off or the amount to pay typed in
)

// Line is one dish line of the order.
type Line struct {
	Key    string // the caller's id for the line
	Amount int64  // unit price × quantity, options included
}

// Fees are the rates and fees the order was priced with.
type Fees struct {
	TaxRateBP       int
	TaxInclusive    bool
	ServiceChargeBP int
	Packaging       int64
	Delivery        int64
}

// Adjustment is one thing taken off: a promotion, a coupon, a discount at
// checkout or a round-off. Exactly how much is worked out from Amount,
// PercentBP, Cap and Keep and limited to what is left at its stage.
type Adjustment struct {
	Kind     string // promotion, store_threshold, store_free, store_delivery, platform_free, staff_percent, round_off, set_total, ...
	Ref      int64  // the promotion's or coupon's id
	Name     string
	NameEn   string
	FundedBy string // store or platform
	Stage    Stage
	// Amount is a fixed amount off (Stage* except StageRound); for
	// set_total it is the amount to pay, for round_off the amount taken off.
	Amount int64
	// PercentBP takes this share off what is left at the stage (2000 =
	// 20% off). For StageStaff it is instead the share still charged
	// (9000 = 10% off), as the checkout screen sends it.
	PercentBP int
	// Cap limits a percentage (0 = none).
	Cap int64
	// Whole takes all that is left (free delivery, a delivery coupon of 0).
	Whole bool
	// Lines limits an item discount to these lines' keys (StageItem only).
	Lines []string
}

// Applied is an adjustment with what it actually took off.
type Applied struct {
	Adjustment
	Off int64 `json:"off"`
	// PerLine is how Off is shared out over the dish lines (dish stages only).
	PerLine map[string]int64 `json:"per_line,omitempty"`
}

// LineResult is a dish line with its share of everything.
type LineResult struct {
	Key      string `json:"key"`
	Amount   int64  `json:"amount"`
	Discount int64  `json:"discount"` // store-paid discounts on it (item, order, staff stages)
	Net      int64  `json:"net"`      // Amount − Discount
	Service  int64  `json:"service"`  // its share of the service charge
	Tax      int64  `json:"tax"`      // its share of the tax
}

// Totals is the order's bill.
type Totals struct {
	Subtotal         int64
	Packaging        int64
	Delivery         int64
	ServiceCharge    int64
	Tax              int64
	TaxInclusive     bool
	TaxRateBP        int
	GoodsDiscount    int64 // item and order stages
	DeliveryDiscount int64
	StaffDiscount    int64
	PlatformDiscount int64
	RoundOff         int64
	Total            int64
}

// Input is everything needed to price an order.
type Input struct {
	Lines       []Line
	Fees        Fees
	Adjustments []Adjustment
}

// Result is the priced order.
type Result struct {
	Lines   []LineResult
	Applied []Applied
	Totals  Totals
}

// Errors for a round-off or a typed amount that does not fit.
var (
	ErrRoundTooBig = errors.New("the round-off is more than the amount to pay")
	ErrSetTooBig   = errors.New("the amount to pay cannot be more than the bill")
)

// MulDivRound returns a*b/d rounded half up (a, b ≥ 0, d > 0).
func MulDivRound(a, b, d int64) int64 {
	return (a*b + d/2) / d
}

// Price works an order out. Adjustments of one stage are taken in the order
// given; stages always go in their fixed order.
func Price(in Input) (Result, error) {
	n := len(in.Lines)
	left := make([]int64, n) // what each line still costs
	res := Result{Lines: make([]LineResult, n)}
	var sub int64
	for i, l := range in.Lines {
		left[i] = max(l.Amount, 0)
		sub += left[i]
		res.Lines[i] = LineResult{Key: l.Key, Amount: left[i]}
	}
	t := Totals{Subtotal: sub, Packaging: in.Fees.Packaging, Delivery: in.Fees.Delivery,
		TaxInclusive: in.Fees.TaxInclusive, TaxRateBP: in.Fees.TaxRateBP}

	byStage := func(s Stage) []int {
		var idx []int
		for i, a := range in.Adjustments {
			if a.Stage == s {
				idx = append(idx, i)
			}
		}
		return idx
	}
	applied := make([]Applied, len(in.Adjustments))
	for i, a := range in.Adjustments {
		applied[i] = Applied{Adjustment: a}
	}

	// stages 2 to 4: the dishes
	for _, s := range []Stage{StageItem, StageOrder, StageStaff} {
		for _, i := range byStage(s) {
			a := in.Adjustments[i]
			cover := make([]bool, n)
			var base int64
			for j := range in.Lines {
				if s != StageItem || len(a.Lines) == 0 || contains(a.Lines, in.Lines[j].Key) {
					cover[j] = true
					base += left[j]
				}
			}
			var off int64
			switch {
			case s == StageStaff:
				if a.PercentBP > 0 {
					off = base - MulDivRound(base, int64(a.PercentBP), 10000)
				}
			case a.Whole:
				off = base
			case a.PercentBP > 0:
				off = MulDivRound(base, int64(a.PercentBP), 10000)
				if a.Cap > 0 {
					off = min(off, a.Cap)
				}
			default:
				off = a.Amount
			}
			off = min(max(off, 0), base)
			per := Share(off, left, cover)
			applied[i].Off, applied[i].PerLine = off, map[string]int64{}
			for j, v := range per {
				if v != 0 {
					left[j] -= v
					res.Lines[j].Discount += v
					applied[i].PerLine[in.Lines[j].Key] += v
				}
			}
			if s == StageStaff {
				t.StaffDiscount += off
			} else {
				t.GoodsDiscount += off
			}
		}
	}

	// stage 5: delivery fee discounts
	shipLeft := max(in.Fees.Delivery, 0)
	for _, i := range byStage(StageDelivery) {
		a := in.Adjustments[i]
		off := a.Amount
		if a.Whole {
			off = shipLeft
		}
		off = min(max(off, 0), shipLeft)
		shipLeft -= off
		applied[i].Off = off
		t.DeliveryDiscount += off
	}

	// stages 5 and 6: service charge and tax, worked out on the whole and
	// then shared out to the lines
	var goods int64
	for j := range left {
		goods += left[j]
		res.Lines[j].Net = left[j]
	}
	t.ServiceCharge = MulDivRound(goods, int64(in.Fees.ServiceChargeBP), 10000)
	base := goods + t.Packaging + t.ServiceCharge
	rate := int64(in.Fees.TaxRateBP)
	if in.Fees.TaxInclusive {
		t.Tax = MulDivRound(base, rate, 10000+rate)
	} else {
		t.Tax = MulDivRound(base, rate, 10000)
	}
	all := make([]bool, n)
	for j := range all {
		all[j] = true
	}
	for j, v := range Share(t.ServiceCharge, left, all) {
		res.Lines[j].Service = v
	}
	// the tax is shared over the lines (with their service) and the packaging
	w := make([]int64, n+1)
	for j := range left {
		w[j] = left[j] + res.Lines[j].Service
	}
	w[n] = t.Packaging
	cov := append(append([]bool{}, all...), true)
	for j, v := range Share(t.Tax, w, cov)[:n] {
		res.Lines[j].Tax = v
	}

	t.Total = sub + t.Packaging + t.Delivery + t.ServiceCharge - t.GoodsDiscount - t.StaffDiscount - t.DeliveryDiscount
	if !in.Fees.TaxInclusive {
		t.Total += t.Tax
	}

	// stage 7: the platform's part
	for _, i := range byStage(StagePlatform) {
		off := min(max(in.Adjustments[i].Amount, 0), t.Total)
		applied[i].Off = off
		t.PlatformDiscount += off
		t.Total -= off
	}

	// stage 8: round-off or the amount to pay
	for _, i := range byStage(StageRound) {
		a := in.Adjustments[i]
		var off int64
		if a.Kind == "set_total" {
			if a.Amount > t.Total {
				return res, ErrSetTooBig
			}
			off = t.Total - max(a.Amount, 0)
		} else {
			if a.Amount > t.Total {
				return res, ErrRoundTooBig
			}
			off = max(a.Amount, 0)
		}
		applied[i].Off = off
		t.RoundOff += off
		t.Total -= off
	}

	res.Applied = applied
	res.Totals = t
	return res, nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Share splits amount over the covered weights in proportion, by the largest
// remainder, so that the parts always add up to amount and none is more than
// its weight. Uncovered or zero weights get nothing.
func Share(amount int64, weights []int64, cover []bool) []int64 {
	out := make([]int64, len(weights))
	var total int64
	for i, w := range weights {
		if cover[i] && w > 0 {
			total += w
		}
	}
	if amount <= 0 || total <= 0 {
		return out
	}
	amount = min(amount, total)
	type rem struct {
		i int
		r int64
	}
	var rems []rem
	var given int64
	for i, w := range weights {
		if !cover[i] || w <= 0 {
			continue
		}
		q, r := mulDiv(amount, w, total)
		out[i] = q
		given += q
		rems = append(rems, rem{i, r})
	}
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].r > rems[b].r })
	for k := 0; given < amount; k++ {
		i := rems[k%len(rems)].i
		if out[i] < weights[i] {
			out[i]++
			given++
		}
	}
	return out
}

// mulDiv returns a*b/d and its remainder without overflowing (a, b ≥ 0, d > 0, a ≤ d).
func mulDiv(a, b, d int64) (int64, int64) {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	q, r := bits.Div64(hi, lo, uint64(d))
	return int64(q), int64(r)
}

// PlatformPrice is a dish's price on a delivery platform worked out from the
// store's own price (docs/ORDER_TYPES_DESIGN.md §6): the price with the
// markup, rounded half up to the step (cents; 0 rounds to the cent). It is
// rounded once, from the exact amount, so 202.495 with a step of 1.00 is
// 202.00, not 202.50 and then 203.00.
func PlatformPrice(price int64, markupBP int, step int64) int64 {
	if step <= 0 {
		step = 1
	}
	return MulDivRound(price, int64(10000+markupBP), 10000*step) * step
}
