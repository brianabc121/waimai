package app

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/brianabc121/waimai/internal/pricing"
)

// Promotion rules (docs/PRICING_REVIEW.md §5.2). Each kind of promotion is
// one entry here: what the merchant must fill in, and what it takes off. A
// new kind of promotion is a new entry and its settings form; quotes,
// orders, add-ons, voids, refunds, bills and the ledger then follow by
// themselves, because they all price through the engine.
type promoRule struct {
	// dish rules take money off chosen dishes (pricing.StageItem), each
	// dish on its own, before any promotion on the whole order; several can
	// apply to one order. The others are on the whole order, one at most.
	dish bool
	// order is what a whole-order rule takes off sub of dishes and the
	// delivery fee.
	order func(p *Promotion, sub, fee int64) Discount
	// units is what a dish rule takes off one dish, given what each of its
	// units costs, dearest first.
	units func(p *Promotion, units []int64) int64
	// check checks the merchant's settings and clears what does not apply.
	check func(in *PromotionInput) error
}

var promoRules = map[string]promoRule{
	// The store's own offer on a delivery platform, entered with a platform
	// order (docs/ORDER_TYPES_DESIGN.md §9.2): a fixed amount off the whole
	// order, paid by the store, with no minimum. Never set up as a promotion.
	KindPlatformMerchantPromo: {
		order: func(p *Promotion, sub, _ int64) Discount { return Discount{Goods: min(p.Value, max(sub, 0))} },
		check: func(*PromotionInput) error { return ErrValidation("unknown promotion kind") },
	},
	"amount_off": {
		order: func(p *Promotion, sub, _ int64) Discount { return Discount{Goods: min(p.Value, sub)} },
		check: func(in *PromotionInput) error {
			if in.Value <= 0 {
				return ErrValidation("amount off must be greater than 0")
			}
			// "spend 100, get 150 off" is almost certainly a typo.
			if in.Threshold < in.Value {
				return ErrValidation("the minimum spend must be at least the amount off")
			}
			in.MaxDiscount = nil
			return nil
		},
	},
	"percent_off": {
		order: func(p *Promotion, sub, _ int64) Discount {
			d := mulDivRound(sub, p.Value, 10000)
			if p.MaxDiscount != nil {
				d = min(d, *p.MaxDiscount)
			}
			return Discount{Goods: min(d, sub)}
		},
		check: func(in *PromotionInput) error {
			if in.Value <= 0 || in.Value >= 10000 {
				return ErrValidation("percent off must be between 0 and 100")
			}
			if in.MaxDiscount != nil && *in.MaxDiscount <= 0 {
				return ErrValidation("the cap must be greater than 0")
			}
			return nil
		},
	},
	"free_delivery": {
		order: func(_ *Promotion, _, fee int64) Discount { return Discount{Delivery: fee} },
		check: func(in *PromotionInput) error {
			if !in.AppliesDelivery {
				return ErrValidation("free delivery only makes sense for delivery orders")
			}
			in.Value, in.MaxDiscount, in.AppliesPickup = 0, nil, false
			return nil
		},
	},
	// The chosen dishes at Value (bp) off: 2000 is 20% off.
	"dish_percent": {
		dish: true,
		units: func(p *Promotion, units []int64) int64 {
			var sum int64
			for _, u := range units {
				sum += u
			}
			return mulDivRound(sum, p.Value, 10000)
		},
		check: func(in *PromotionInput) error {
			if in.Value <= 0 || in.Value >= 10000 {
				return ErrValidation("percent off must be between 0 and 100")
			}
			return checkDishes(in)
		},
	},
	// Every second one of a chosen dish at Value (bp) off: 5000 is the
	// second at half price, 10000 buy one get one free. The cheaper units
	// are the ones that get it.
	"dish_second": {
		dish: true,
		units: func(p *Promotion, units []int64) int64 {
			var off int64
			for i := len(units) - len(units)/2; i < len(units); i++ { // the cheaper half
				off += mulDivRound(units[i], p.Value, 10000)
			}
			return off
		},
		check: func(in *PromotionInput) error {
			if in.Value <= 0 || in.Value > 10000 {
				return ErrValidation("the second one's discount must be between 0 and 100%")
			}
			return checkDishes(in)
		},
	},
}

// checkDishes checks a dish promotion's dishes. It counts on what the dishes
// cost after it, has no cap, and is not counted per customer.
func checkDishes(in *PromotionInput) error {
	if len(in.ItemIDs) == 0 {
		return ErrValidation("choose at least one dish")
	}
	if len(in.ItemIDs) > 500 {
		return ErrValidation("too many dishes")
	}
	slices.Sort(in.ItemIDs)
	in.ItemIDs = slices.Compact(in.ItemIDs)
	in.MaxDiscount, in.TotalQuota, in.PerPhoneLimit = nil, nil, nil
	return nil
}

func (p *Promotion) isDish() bool { return promoRules[p.Kind].dish }

// dishLine is a dish line as the dish rules see it.
type dishLine struct {
	Key    string
	ItemID int64
	// Unit is what one costs, options included; Qty how many.
	Unit int64
	Qty  int
}

// dishRule is a dish promotion as an order used it: its kind and settings
// then (order_discounts.rule).
type dishRule struct {
	Kind    string  `json:"kind"`
	Value   int64   `json:"value"`
	ItemIDs []int64 `json:"item_ids"`
	// BrandIDs are the brands it applied to then.
	BrandIDs []int64 `json:"brand_ids,omitempty"`
}

// dishPromo is a dish promotion applied to an order: the promotion and the
// adjustments it makes, one per dish.
type dishPromo struct {
	Promo *Promotion
	adj   []pricing.Adjustment
}

// dishAdjustments works out the dish promotions on lines: for each dish, the
// one that saves the most (the first of them on a tie), as one adjustment on
// that dish's lines. Promotions are taken in the order given.
func dishAdjustments(ps []*Promotion, lines []dishLine) []pricing.Adjustment {
	byItem := map[int64][]dishLine{}
	var items []int64
	for _, l := range lines {
		if l.ItemID == 0 || l.Qty <= 0 || l.Unit <= 0 {
			continue
		}
		if _, ok := byItem[l.ItemID]; !ok {
			items = append(items, l.ItemID)
		}
		byItem[l.ItemID] = append(byItem[l.ItemID], l)
	}
	var out []pricing.Adjustment
	for _, item := range items {
		ls := byItem[item]
		var units []int64
		var keys []string
		var cost int64
		for _, l := range ls {
			for range l.Qty {
				units = append(units, l.Unit)
			}
			keys = append(keys, l.Key)
			cost += l.Unit * int64(l.Qty)
		}
		slices.SortStableFunc(units, func(a, b int64) int { return cmp.Compare(b, a) })
		var best *Promotion
		var bestOff int64
		for _, p := range ps {
			r := promoRules[p.Kind]
			if !r.dish || !slices.Contains(p.ItemIDs, item) {
				continue
			}
			if off := min(r.units(p, units), cost); off > bestOff {
				best, bestOff = p, off
			}
		}
		if best != nil {
			out = append(out, pricing.Adjustment{Kind: "dish_promotion", Ref: best.ID, Name: best.Name, NameEn: best.NameEn,
				FundedBy: "store", Stage: pricing.StageItem, Amount: bestOff, Lines: keys})
		}
	}
	return out
}

// dishOff is what dish adjustments take off in all.
func dishOff(adj []pricing.Adjustment) int64 {
	var s int64
	for _, a := range adj {
		s += a.Amount
	}
	return s
}

// liveDishPromos are the dish promotions of b that apply at at to an order
// of service, with its dishes costing sub.
func liveDishPromos(ctx context.Context, q Querier, b *Branch, brandID int64, service string, at time.Time, sub int64) ([]*Promotion, error) {
	scope := "delivery"
	if service == "dine_in" {
		scope = "dine_in"
	}
	ps, err := loadPromotions(ctx, q, b.ID, "p.status='active' AND p.kind IN ('dish_percent','dish_second') AND p.scope="+
		scopeSQL(scope)+" AND "+brandPromoSQL(brandID), "")
	if err != nil {
		return nil, err
	}
	return dishOnly(ps, at, b, service, sub), nil
}

// dishOnly keeps the dish promotions of ps that apply.
func dishOnly(ps []Promotion, at time.Time, b *Branch, service string, sub int64) []*Promotion {
	var out []*Promotion
	for i := range ps {
		if ps[i].isDish() && ps[i].why(at, b.location(), service, sub) == "" {
			out = append(out, &ps[i])
		}
	}
	return out
}

// promoFromRule is a dish promotion rebuilt from what an order recorded.
func promoFromRule(id int64, name, nameEn string, raw []byte) (*Promotion, bool) {
	var r dishRule
	if json.Unmarshal(raw, &r) != nil || !promoRules[r.Kind].dish {
		return nil, false
	}
	return &Promotion{ID: id, Name: name, NameEn: nameEn, Kind: r.Kind, Value: r.Value, ItemIDs: r.ItemIDs,
		BrandIDs: r.BrandIDs, Status: "active"}, true
}

// ruleOf is what an order records of a dish promotion.
func ruleOf(p *Promotion) []byte {
	b, _ := json.Marshal(dishRule{Kind: p.Kind, Value: p.Value, ItemIDs: p.ItemIDs, BrandIDs: p.BrandIDs})
	return b
}

// keyOf is a line key for the engine.
func keyOf(i int64) string { return strconv.FormatInt(i, 10) }
