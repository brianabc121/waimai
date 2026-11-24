package app

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

// applyPlatform takes a platform coupon off what the customer pays. It is
// paid by the platform, so the store's tax and service charge do not change.
// It returns how much was taken off: never more than is left to pay.
func (t *Totals) applyPlatform(value int64) int64 {
	v := min(max(value, 0), t.Total)
	t.PlatformSubsidy = v
	t.Discount += v
	t.Total -= v
	return v
}

// mulDivRound returns a*b/d rounded half up.
func mulDivRound(a, b, d int64) int64 {
	return (a*b + d/2) / d
}

// ComputeTotals implements docs/DESIGN.md §4.2. The delivery fee is not taxed.
// A discount on goods lowers the amount that service charge and tax are
// computed on; a delivery discount only lowers the amount due.
func ComputeTotals(subtotal, deliveryFee int64, cfg TaxConfig, d Discount) Totals {
	d.Goods = min(d.Goods, subtotal)
	d.Delivery = min(d.Delivery, deliveryFee)
	goods := subtotal - d.Goods
	t := Totals{
		Subtotal:     subtotal,
		PackagingFee: cfg.PackagingFee,
		DeliveryFee:  deliveryFee,
		TaxInclusive: cfg.TaxInclusive,
		TaxRateBP:    cfg.TaxRateBP,
		Discount:     d.total(),
	}
	t.MerchantDiscount = t.Discount
	t.ServiceCharge = mulDivRound(goods, int64(cfg.ServiceChargeBP), 10000)
	base := goods + t.PackagingFee + t.ServiceCharge
	rate := int64(cfg.TaxRateBP)
	if cfg.TaxInclusive {
		t.TaxAmount = mulDivRound(base, rate, 10000+rate)
	} else {
		t.TaxAmount = mulDivRound(base, rate, 10000)
	}
	t.Total = subtotal + t.PackagingFee + t.DeliveryFee + t.ServiceCharge - t.Discount
	if !cfg.TaxInclusive {
		t.Total += t.TaxAmount
	}
	return t
}
