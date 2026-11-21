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
	Total           int64  `json:"total"`
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
