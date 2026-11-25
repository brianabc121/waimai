package pricing

import (
	"fmt"
	"sort"
)

// Check verifies a priced order against the rules every price must keep
// (docs/PRICING_CHECK.md §2). It works the amounts out again on its own, from
// in and from what r says each adjustment took off, and returns one line per
// broken rule (nil when all hold).
//
// It is meant for tests and for checking stored orders: any change to how an
// order's money is worked out must keep Check empty.
func Check(in Input, r Result) []string {
	var bad []string
	fail := func(f string, a ...any) { bad = append(bad, fmt.Sprintf(f, a...)) }
	t := r.Totals
	n := len(in.Lines)

	// C1 the lines are the input's lines, each with sensible parts
	if len(r.Lines) != n {
		fail("C1 %d lines priced, %d given", len(r.Lines), n)
		return bad
	}
	var sub, disc, net, svc, tax int64
	for j, l := range r.Lines {
		if l.Key != in.Lines[j].Key || l.Amount != max(in.Lines[j].Amount, 0) {
			fail("C1 line %d is %+v, given %+v", j, l, in.Lines[j])
		}
		if l.Discount < 0 || l.Discount > l.Amount || l.Net != l.Amount-l.Discount || l.Service < 0 || l.Tax < 0 {
			fail("C1 line %s: %+v", l.Key, l)
		}
		sub += l.Amount
		disc += l.Discount
		net += l.Net
		svc += l.Service
		tax += l.Tax
	}
	// C2 subtotal is the dishes
	if t.Subtotal != sub {
		fail("C2 subtotal %d, lines %d", t.Subtotal, sub)
	}
	if len(r.Applied) != len(in.Adjustments) {
		fail("C3 %d adjustments applied, %d given", len(r.Applied), len(in.Adjustments))
		return bad
	}

	// C3–C5 replay the dish stages: each adjustment takes what its rule says
	// off what is left, shared in proportion, never more than a line has
	left := make([]int64, n)
	for j := range r.Lines {
		left[j] = r.Lines[j].Amount
	}
	idx := map[string]int{}
	for j, l := range in.Lines {
		idx[l.Key] = j
	}
	perLine := make([]int64, n)
	var goods, staff, ship, platf, round int64
	order := make([]int, len(in.Adjustments))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return in.Adjustments[order[a]].Stage < in.Adjustments[order[b]].Stage })
	shipLeft := max(in.Fees.Delivery, 0)
	for _, i := range order {
		a, ap := in.Adjustments[i], r.Applied[i]
		if ap.Off < 0 {
			fail("C3 %s took off %d", a.Kind, ap.Off)
		}
		switch a.Stage {
		case StageItem, StageOrder, StageStaff:
			cover := make([]bool, n)
			var base int64
			for j, l := range in.Lines {
				if a.Stage != StageItem || len(a.Lines) == 0 || contains(a.Lines, l.Key) {
					cover[j] = true
					base += left[j]
				}
			}
			want := wantOff(a, base)
			if ap.Off != want {
				fail("C3 %s (stage %d) took off %d, its rule gives %d on %d", a.Kind, a.Stage, ap.Off, want, base)
			}
			var shared int64
			for k, v := range ap.PerLine {
				j, ok := idx[k]
				switch {
				case !ok || !cover[j]:
					fail("C4 %s shared %d to line %s it does not cover", a.Kind, v, k)
					continue
				case v < 0 || v > left[j]:
					fail("C4 %s gave line %s %d of the %d left", a.Kind, k, v, left[j])
				case base > 0 && abs(v*base-ap.Off*left[j]) >= base:
					// largest remainder: within one hundredth of the exact share
					fail("C4 %s gave line %s %d, its share is %d×%d/%d", a.Kind, k, v, ap.Off, left[j], base)
				}
				shared += v
				left[j] -= v
				perLine[j] += v
			}
			if shared != ap.Off {
				fail("C4 %s shared out %d of %d", a.Kind, shared, ap.Off)
			}
			if a.Stage == StageStaff {
				staff += ap.Off
			} else {
				goods += ap.Off
			}
		case StageDelivery:
			want := min(max(a.Amount, 0), shipLeft)
			if a.Whole {
				want = shipLeft
			}
			if ap.Off != want {
				fail("C3 %s took %d off the delivery fee, its rule gives %d", a.Kind, ap.Off, want)
			}
			shipLeft -= ap.Off
			ship += ap.Off
		case StagePlatform:
			platf += ap.Off
		case StageRound:
			round += ap.Off
		}
	}
	for j, l := range r.Lines {
		if perLine[j] != l.Discount {
			fail("C4 line %s discount %d, its shares add up to %d", l.Key, l.Discount, perLine[j])
		}
	}
	// C5 the totals are the sums of the adjustments
	if t.GoodsDiscount != goods || t.StaffDiscount != staff || t.DeliveryDiscount != ship ||
		t.PlatformDiscount != platf || t.RoundOff != round {
		fail("C5 totals goods %d staff %d delivery %d platform %d round %d; adjustments %d %d %d %d %d",
			t.GoodsDiscount, t.StaffDiscount, t.DeliveryDiscount, t.PlatformDiscount, t.RoundOff,
			goods, staff, ship, platf, round)
	}
	if disc != goods+staff {
		fail("C5 line discounts %d, goods %d + staff %d", disc, goods, staff)
	}
	if t.DeliveryDiscount > t.Delivery {
		fail("C5 delivery discount %d over the fee %d", t.DeliveryDiscount, t.Delivery)
	}

	// C6 service charge on what the dishes cost after discounts, shared out whole
	if want := MulDivRound(net, int64(in.Fees.ServiceChargeBP), 10000); t.ServiceCharge != want {
		fail("C6 service charge %d, %d×%dbp gives %d", t.ServiceCharge, net, in.Fees.ServiceChargeBP, want)
	}
	if svc != t.ServiceCharge {
		fail("C6 line service %d, service charge %d", svc, t.ServiceCharge)
	}
	// C7 tax on dishes + packaging + service charge (never the delivery fee),
	// rounded once; the lines carry all of it but the packaging's part
	base, rate := net+t.Packaging+t.ServiceCharge, int64(in.Fees.TaxRateBP)
	wantTax := MulDivRound(base, rate, 10000)
	if in.Fees.TaxInclusive {
		wantTax = MulDivRound(base, rate, 10000+rate)
	}
	if t.Tax != wantTax {
		fail("C7 tax %d, %d at %dbp (inclusive %v) gives %d", t.Tax, base, rate, in.Fees.TaxInclusive, wantTax)
	}
	if tax > t.Tax || (t.Packaging == 0 && tax != t.Tax) {
		fail("C7 line tax %d, tax %d (packaging %d)", tax, t.Tax, t.Packaging)
	}

	// C8 what is paid is the sum of the parts, never below 0
	beforePlatform := t.Subtotal + t.Packaging + t.Delivery + t.ServiceCharge - t.GoodsDiscount - t.StaffDiscount - t.DeliveryDiscount
	if !t.TaxInclusive {
		beforePlatform += t.Tax
	}
	if t.PlatformDiscount > beforePlatform {
		fail("C8 platform pays %d of %d", t.PlatformDiscount, beforePlatform)
	}
	if want := beforePlatform - t.PlatformDiscount - t.RoundOff; t.Total != want || t.Total < 0 {
		fail("C8 total %d, parts give %d", t.Total, want)
	}
	// C9 a typed amount to pay (the last word on the total) is what is paid
	last := -1
	for i, a := range in.Adjustments {
		if a.Stage == StageRound {
			last = i
		}
	}
	if last >= 0 && in.Adjustments[last].Kind == "set_total" && t.Total != max(in.Adjustments[last].Amount, 0) {
		fail("C9 amount to pay typed %d, total %d", in.Adjustments[last].Amount, t.Total)
	}
	return bad
}

// wantOff is what a dish-stage adjustment takes off base by its rule.
func wantOff(a Adjustment, base int64) int64 {
	var off int64
	switch {
	case a.Stage == StageStaff:
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
	return min(max(off, 0), base)
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
