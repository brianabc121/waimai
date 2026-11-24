package app

import "github.com/brianabc121/waimai/internal/pricing"

// Money amounts are integers in the currency's minor unit.

// TaxConfig is the branch-level fee and tax configuration.
type TaxConfig struct {
	TaxRateBP       int
	TaxInclusive    bool
	ServiceChargeBP int
	PackagingFee    int64
}

// Totals is the fee breakdown shown to the customer and stored on the order.
type Totals struct {
	Subtotal      int64 `json:"subtotal"`
	PackagingFee  int64 `json:"packaging_fee"`
	DeliveryFee   int64 `json:"delivery_fee"`
	ServiceCharge int64 `json:"service_charge"`
	TaxAmount     int64 `json:"tax_amount"`
	TaxInclusive  bool  `json:"tax_inclusive"`
	TaxRateBP     int   `json:"tax_rate_bp"`
	Discount      int64 `json:"discount"`
	// The promotion behind Discount, if any (0 / empty when none applied).
	PromotionID     int64  `json:"promotion_id"`
	PromotionName   string `json:"promotion_name"`
	PromotionNameEn string `json:"promotion_name_en"`
	// PromotionDiscount is what the promotion alone took off (quotes only).
	PromotionDiscount int64 `json:"promotion_discount"`
	// MerchantDiscount is the part of Discount the store pays for (its
	// promotion and its coupons); PlatformSubsidy is the platform coupon,
	// which the platform pays back to the store. Discount is their sum.
	MerchantDiscount int64 `json:"merchant_discount"`
	PlatformSubsidy  int64 `json:"platform_subsidy"`
	// The discount staff gave at checkout (docs/CHECKOUT_DISCOUNT_DESIGN.md §5):
	// AdjPercentBP is the share of the dishes still charged (9000 = 10% off)
	// and StaffDiscount what it took off the dishes; AdjMode is "round" (a
	// round-off) or "set_total" (the amount to pay was typed in), and RoundOff
	// what that took off the total. Neither is part of Discount.
	AdjPercentBP  int    `json:"adj_percent_bp"`
	StaffDiscount int64  `json:"staff_discount"`
	AdjMode       string `json:"adj_mode"`
	RoundOff      int64  `json:"round_off"`
	// Total is what the customer pays.
	Total int64 `json:"total"`
}

// ComputeTotals implements docs/DESIGN.md §4.2 for one amount of dishes and
// a discount on them and on the delivery fee: the delivery fee is not taxed;
// a discount on dishes lowers what service charge and tax are worked out on.
// (Orders are priced by priceMoney in money.go; this is for the promotion
// trial and the tests.)
func ComputeTotals(subtotal, deliveryFee int64, cfg TaxConfig, d Discount) Totals {
	in := pricing.Input{Lines: []pricing.Line{{Key: "dishes", Amount: subtotal}},
		Fees: pricing.Fees{TaxRateBP: cfg.TaxRateBP, TaxInclusive: cfg.TaxInclusive, ServiceChargeBP: cfg.ServiceChargeBP,
			Packaging: cfg.PackagingFee, Delivery: deliveryFee},
		Adjustments: []pricing.Adjustment{{Kind: "discount", Stage: pricing.StageOrder, Amount: d.Goods},
			{Kind: "discount", Stage: pricing.StageDelivery, Amount: d.Delivery}}}
	r, _ := pricing.Price(in) // nothing here can fail
	tt := r.Totals
	t := Totals{Subtotal: tt.Subtotal, PackagingFee: tt.Packaging, DeliveryFee: tt.Delivery, ServiceCharge: tt.ServiceCharge,
		TaxAmount: tt.Tax, TaxInclusive: tt.TaxInclusive, TaxRateBP: tt.TaxRateBP,
		Discount: tt.GoodsDiscount + tt.DeliveryDiscount, Total: tt.Total}
	t.MerchantDiscount = t.Discount
	return t
}

// mulDivRound returns a*b/d rounded half up.
func mulDivRound(a, b, d int64) int64 { return pricing.MulDivRound(a, b, d) }
