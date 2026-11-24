package app

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Payment types, online payment methods and discounts at checkout
// (docs/CHECKOUT_DISCOUNT_DESIGN.md §5, §6).

// ---- online payment methods (platform) ----

// PayChannel is one online payment method of a country (GCash, Alipay, a bank
// transfer). Choosing it at checkout only records how the guest paid.
type PayChannel struct {
	ID      int64             `json:"id"`
	Country string            `json:"country"`
	Name    string            `json:"name"`
	Names   map[string]string `json:"names"`
	IconURL string            `json:"icon_url"`
	Sort    int               `json:"sort"`
	Active  bool              `json:"active"`
	// For the platform page: stores of the country that take it, and
	// payments made with it in the last 30 days.
	Stores int `json:"stores"`
	Uses   int `json:"uses"`
}

const payChannelCols = `c.id, c.country, c.name, c.names, c.icon_url, c.sort, c.active`

func scanChannels(rows pgx.Rows, extra bool) ([]PayChannel, error) {
	defer rows.Close()
	out := []PayChannel{}
	for rows.Next() {
		var c PayChannel
		var names []byte
		dest := []any{&c.ID, &c.Country, &c.Name, &names, &c.IconURL, &c.Sort, &c.Active}
		if extra {
			dest = append(dest, &c.Stores, &c.Uses)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		c.Names = map[string]string{}
		_ = json.Unmarshal(names, &c.Names)
		out = append(out, c)
	}
	return out, rows.Err()
}

// PayCountry is a country on the platform's payment page.
type PayCountry struct {
	Country  string `json:"country"`
	Channels int    `json:"channels"`
	Stores   int    `json:"stores"`
}

// PayCountries lists every country that has payment methods or stores.
func (a *App) PayCountries(ctx context.Context) ([]PayCountry, error) {
	rows, err := a.DB.Query(ctx, `SELECT c, (SELECT count(*) FROM pay_channels p WHERE p.country = c),
		(SELECT count(*) FROM branches b WHERE upper(b.country_code) = c)
		FROM (SELECT country FROM pay_channels UNION SELECT upper(country_code) FROM branches WHERE country_code <> '') x(c)
		ORDER BY 3 DESC, 1`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PayCountry])
}

// ListPayChannels is the platform's list for one country, stopped ones included.
func (a *App) ListPayChannels(ctx context.Context, country string) ([]PayChannel, error) {
	country = strings.ToUpper(strings.TrimSpace(country))
	rows, err := a.DB.Query(ctx, `SELECT `+payChannelCols+`,
		(SELECT count(*) FROM branches b WHERE upper(b.country_code) = c.country
			AND NOT COALESCE(b.pay_settings->'off', '[]'::jsonb) @> to_jsonb(c.id)),
		(SELECT count(*) FROM orders o WHERE o.pay_channel_id = c.id AND o.created_at > now() - interval '30 days')
		FROM pay_channels c WHERE c.country=$1 ORDER BY c.sort, c.id`, country)
	if err != nil {
		return nil, err
	}
	return scanChannels(rows, true)
}

type PayChannelInput struct {
	Country string            `json:"country"`
	Name    string            `json:"name"`
	Names   map[string]string `json:"names"`
	IconURL string            `json:"icon_url"`
	Active  bool              `json:"active"`
}

// SavePayChannel adds (id 0) or edits a payment method. A method that was
// used is never deleted, only stopped, so old orders keep their name.
func (a *App) SavePayChannel(ctx context.Context, actor AdminActor, id int64, in PayChannelInput) (int64, error) {
	in.Country = strings.ToUpper(strings.TrimSpace(in.Country))
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Country) != 2 || in.Country[0] < 'A' || in.Country[0] > 'Z' || in.Country[1] < 'A' || in.Country[1] > 'Z' {
		return 0, ErrValidation("country must be a 2-letter code")
	}
	if in.Name == "" || len([]rune(in.Name)) > 40 || len(in.IconURL) > 500 {
		return 0, ErrValidation("name is required (at most 40 characters)")
	}
	names := map[string]string{}
	for k, v := range in.Names {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if len(k) > 10 || len([]rune(v)) > 40 {
			return 0, ErrValidation("names are at most 40 characters")
		}
		names[k] = v
	}
	raw, _ := json.Marshal(names)
	var err error
	if id == 0 {
		err = a.DB.QueryRow(ctx, `INSERT INTO pay_channels (country, name, names, icon_url, active, sort)
			VALUES ($1,$2,$3,$4,$5,(SELECT COALESCE(max(sort), 0) + 1 FROM pay_channels WHERE country=$1 AND sort < 99))
			RETURNING id`, in.Country, in.Name, raw, in.IconURL, in.Active).Scan(&id)
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, err = a.DB.Exec(ctx, `UPDATE pay_channels SET name=$2, names=$3, icon_url=$4, active=$5 WHERE id=$1 AND country=$6`,
			id, in.Name, raw, in.IconURL, in.Active, in.Country)
		if err == nil && tag.RowsAffected() == 0 {
			return 0, ErrNotFound
		}
	}
	if isUnique(err, "") {
		return 0, Err(http.StatusConflict, "DUPLICATE", "this country already has a method with that name")
	}
	if err != nil {
		return 0, err
	}
	return id, adminAudit(ctx, a.DB, actor, "pay_channel.saved", "pay_channel", id, in)
}

// SortPayChannels puts a country's methods in the given order.
func (a *App) SortPayChannels(ctx context.Context, actor AdminActor, country string, ids []int64) error {
	country = strings.ToUpper(country)
	return a.inTx(ctx, func(tx pgx.Tx) error {
		for i, id := range ids {
			if _, err := tx.Exec(ctx, `UPDATE pay_channels SET sort=$3 WHERE id=$1 AND country=$2`, id, country, i+1); err != nil {
				return err
			}
		}
		return adminAudit(ctx, tx, actor, "pay_channel.sorted", "pay_channel", 0, map[string]any{"country": country, "ids": ids})
	})
}

// ---- what a store accepts ----

// PaySettings is what a store accepts. Nil Cash or Card means accepted; Off
// lists the online methods of its country that it does not take.
type PaySettings struct {
	Cash *bool   `json:"cash"`
	Card *bool   `json:"card"`
	Off  []int64 `json:"off"`
}

// PayOptions is what a checkout screen offers.
type PayOptions struct {
	Cash bool `json:"cash"`
	Card bool `json:"card"`
	// Channels are the online methods the store takes, in the platform's order.
	Channels []PayChannel `json:"channels"`
	// All are every active method of the store's country (settings page).
	All []PayChannel `json:"all"`
	Off []int64      `json:"off"`
	// Reasons are the discount reasons to tap; nil means the built-in ones.
	Reasons []string `json:"reasons"`
	Country string   `json:"country"`
}

func loadPaySettings(ctx context.Context, q Querier, branchID int64) (PaySettings, []string, string, error) {
	var raw []byte
	var reasons []string
	var country string
	err := q.QueryRow(ctx, `SELECT pay_settings, discount_reasons, upper(country_code) FROM branches WHERE id=$1`, branchID).
		Scan(&raw, &reasons, &country)
	if isNoRows(err) {
		return PaySettings{}, nil, "", ErrNotFound
	}
	var ps PaySettings
	if err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &ps)
	}
	return ps, reasons, country, err
}

func (a *App) PayOptions(ctx context.Context, branchID int64) (*PayOptions, error) {
	return payOptions(ctx, a.DB, branchID)
}

func payOptions(ctx context.Context, q Querier, branchID int64) (*PayOptions, error) {
	ps, reasons, country, err := loadPaySettings(ctx, q, branchID)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+payChannelCols+` FROM pay_channels c WHERE c.country=$1 AND c.active
		ORDER BY c.sort, c.id`, country)
	if err != nil {
		return nil, err
	}
	all, err := scanChannels(rows, false)
	if err != nil {
		return nil, err
	}
	out := &PayOptions{Cash: ps.Cash == nil || *ps.Cash, Card: ps.Card == nil || *ps.Card, All: all,
		Channels: []PayChannel{}, Off: ps.Off, Reasons: reasons, Country: country}
	if out.Off == nil {
		out.Off = []int64{}
	}
	for _, c := range all {
		if !slices.Contains(ps.Off, c.ID) {
			out.Channels = append(out.Channels, c)
		}
	}
	return out, nil
}

type PaySettingsInput struct {
	Cash bool    `json:"cash"`
	Card bool    `json:"card"`
	Off  []int64 `json:"off"`
	// Reasons replaces the discount reasons; nil goes back to the built-in ones.
	Reasons []string `json:"reasons"`
}

func (a *App) SavePaySettings(ctx context.Context, branchID int64, in PaySettingsInput, actor Actor) error {
	var reasons []string
	if in.Reasons != nil {
		c, err := cleanTags(in.Reasons)
		if err != nil {
			return err
		}
		reasons = c
	}
	if in.Off == nil {
		in.Off = []int64{}
	}
	raw, _ := json.Marshal(PaySettings{Cash: &in.Cash, Card: &in.Card, Off: in.Off})
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE branches SET pay_settings=$2, discount_reasons=$3 WHERE id=$1`, branchID, raw, reasons)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := audit(ctx, tx, branchID, 0, actor, "pay_settings.saved", in); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID})
	})
}

// PayInput is how an order was paid, as the cashier tapped it.
type PayInput struct {
	Type      string `json:"type"` // cash, card or online
	ChannelID int64  `json:"channel_id"`
}

// resolvePay checks a payment against what the store takes. It returns the
// method's name for an online payment, which the order keeps.
func resolvePay(ctx context.Context, q Querier, branchID int64, in PayInput) (*int64, string, error) {
	opts, err := payOptions(ctx, q, branchID)
	if err != nil {
		return nil, "", err
	}
	switch in.Type {
	case "cash":
		if !opts.Cash {
			return nil, "", ErrValidation("this store does not take cash")
		}
		return nil, "", nil
	case "card":
		if !opts.Card {
			return nil, "", ErrValidation("this store does not take cards")
		}
		return nil, "", nil
	case "online":
		for _, c := range opts.Channels {
			if c.ID == in.ChannelID {
				id := c.ID
				return &id, c.Name, nil
			}
		}
		return nil, "", ErrValidation("choose one of the store's online payment methods")
	}
	return nil, "", ErrValidation("payment type must be cash, card or online")
}

// ---- discounts at checkout ----

// Adjust is the discount staff give at checkout (§5.1): a percentage off the
// dishes (PercentBP is what is still charged: 9000 = 10% off), and/or a
// round-off (Mode "round", Amount taken off the total), or the amount to pay
// typed in (Mode "set_total", Amount is that amount; on its own).
type Adjust struct {
	PercentBP int    `json:"percent_bp"`
	Mode      string `json:"mode"`
	Amount    int64  `json:"amount"`
	Reason    string `json:"reason"`
}

func (j *Adjust) empty() bool { return j == nil || (j.PercentBP == 0 && j.Mode == "") }

func (j *Adjust) validate() error {
	j.Reason = strings.TrimSpace(j.Reason)
	switch {
	case j.PercentBP < 0 || j.PercentBP >= 10000:
		return ErrValidation("the discount must be between 0% and 100%")
	case j.Mode != "" && j.Mode != "round" && j.Mode != "set_total":
		return ErrValidation("mode must be round or set_total")
	case j.Mode == "set_total" && j.PercentBP != 0:
		return ErrValidation("setting the amount to pay replaces the discount")
	case j.Mode == "set_total" && j.Reason == "":
		return ErrValidation("say why the amount to pay was changed")
	case j.Mode == "round" && j.Amount <= 0, j.Mode == "set_total" && j.Amount < 0:
		return ErrValidation("invalid amount")
	case len([]rune(j.Reason)) > 40:
		return ErrValidation("the reason is at most 40 characters")
	}
	if j.Mode == "" {
		j.Amount = 0
	}
	return nil
}

// adjustTotals prices an order with a discount at checkout on top of what it
// already had (§5.2): promotion and coupons first (d, and the platform's
// coupon platf), then the percentage off what the dishes still cost, which
// also lowers service charge and tax, then the round-off or the typed amount,
// which only lowers what is paid.
func adjustTotals(subtotal, fee int64, cfg TaxConfig, d Discount, platf int64, j Adjust) (Totals, error) {
	d.Goods = min(d.Goods, subtotal)
	staff := int64(0)
	if j.PercentBP > 0 {
		left := subtotal - d.Goods
		staff = left - mulDivRound(left, int64(j.PercentBP), 10000)
	}
	t := ComputeTotals(subtotal, fee, cfg, Discount{Goods: d.Goods + staff, Delivery: d.Delivery})
	t.Discount -= staff
	t.MerchantDiscount -= staff
	t.StaffDiscount, t.AdjPercentBP = staff, j.PercentBP
	t.applyPlatform(platf)
	switch j.Mode {
	case "round":
		if j.Amount > t.Total {
			return t, ErrValidation("the round-off is more than the amount to pay")
		}
		t.RoundOff = j.Amount
	case "set_total":
		if j.Amount > t.Total {
			return t, ErrValidation("the amount to pay cannot be more than the bill")
		}
		t.RoundOff = t.Total - j.Amount
	}
	t.AdjMode = j.Mode
	t.Total -= t.RoundOff
	return t, nil
}

// adjBase is what an order's discount at checkout is computed from.
type adjBase struct {
	subtotal, fee int64
	cfg           TaxConfig
	d             Discount
	platf         int64
	promoID       int64
	promoName     string
	promoNameEn   string
	promoAmount   int64
}

func (s *adjBase) price(j Adjust) (Totals, error) {
	t, err := adjustTotals(s.subtotal, s.fee, s.cfg, s.d, s.platf, j)
	t.PromotionID, t.PromotionName, t.PromotionNameEn, t.PromotionDiscount = s.promoID, s.promoName, s.promoNameEn, s.promoAmount
	return t, err
}

// orderAdjBase reads an order's amounts before any discount at checkout
// (discount never includes it; some older orders have no merchant_discount,
// so the store's part is discount less the platform's). The store-paid discount is split into what came off the dishes and what came off
// the delivery fee: coupons by their kind, the promotion by its kind.
func orderAdjBase(ctx context.Context, q Querier, b *Branch, orderID int64) (*adjBase, error) {
	var s adjBase
	var merchant int64
	var promoID *int64
	var promoKind string
	if err := q.QueryRow(ctx, `SELECT subtotal, delivery_fee, packaging_fee, tax_rate_bp, tax_inclusive, service_charge_bp,
		discount - platform_subsidy, platform_subsidy, promotion_id, promotion_kind, promotion_name, promotion_name_en
		FROM orders WHERE id=$1`, orderID).
		Scan(&s.subtotal, &s.fee, &s.cfg.PackagingFee, &s.cfg.TaxRateBP, &s.cfg.TaxInclusive, &s.cfg.ServiceChargeBP,
			&merchant, &s.platf, &promoID, &promoKind, &s.promoName, &s.promoNameEn); err != nil {
		return nil, err
	}
	var couponGoods, couponShip int64
	if err := q.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER (WHERE kind IN ('store_free','store_threshold')), 0),
		COALESCE(sum(amount) FILTER (WHERE kind = 'store_delivery'), 0) FROM order_discounts WHERE order_id=$1`, orderID).
		Scan(&couponGoods, &couponShip); err != nil {
		return nil, err
	}
	promo := max(0, merchant-couponGoods-couponShip)
	s.d = Discount{Goods: couponGoods, Delivery: couponShip}
	if promoID != nil {
		s.promoID, s.promoAmount = *promoID, promo
		// the kind the promotion had when the order took it, not its kind now
		p := promoOn(promoKind, promo)
		s.d.Goods += p.Goods
		s.d.Delivery += p.Delivery
	}
	return &s, nil
}

// writeAdjusted stores an order's amounts with its discount at checkout.
func writeAdjusted(ctx context.Context, tx pgx.Tx, orderID int64, t Totals, reason string, actor Actor) error {
	var by, approved *int64
	var at *time.Time
	if t.AdjPercentBP > 0 || t.AdjMode != "" {
		now := time.Now()
		at = &now
		if actor.ID != 0 && actor.Type == "staff" {
			by = &actor.ID
		}
		if actor.ApprovedBy != 0 {
			approved = &actor.ApprovedBy
		}
	} else {
		reason = ""
	}
	_, err := tx.Exec(ctx, `UPDATE orders SET service_charge=$2, tax_amount=$3, discount=$4, merchant_discount=$5,
		platform_subsidy=$6, total=$7, adj_percent_bp=$8, staff_discount=$9, adj_mode=$10, round_off=$11, adj_reason=$12,
		adj_by=$13, adj_approved_by=$14, adj_at=$15, version=version+1, updated_at=now() WHERE id=$1`,
		orderID, t.ServiceCharge, t.TaxAmount, t.Discount, t.MerchantDiscount, t.PlatformSubsidy, t.Total, t.AdjPercentBP,
		t.StaffDiscount, t.AdjMode, t.RoundOff, reason, by, approved, at)
	return err
}

// reapplyAdjust keeps an order's discount at checkout right after its dishes
// changed (an add-on, a void): the percentage is taken off the new amount,
// and a round-off or typed amount no longer fits, so it is dropped (§5.4).
// It reports whether a round-off was dropped.
func reapplyAdjust(ctx context.Context, tx pgx.Tx, b *Branch, orderID int64) (bool, error) {
	var bp int
	var mode, reason string
	if err := tx.QueryRow(ctx, `SELECT adj_percent_bp, adj_mode, adj_reason FROM orders WHERE id=$1`, orderID).
		Scan(&bp, &mode, &reason); err != nil {
		return false, err
	}
	if bp == 0 && mode == "" {
		return false, nil
	}
	base, err := orderAdjBase(ctx, tx, b, orderID)
	if err != nil {
		return false, err
	}
	t, err := base.price(Adjust{PercentBP: bp})
	if err != nil {
		return false, err
	}
	if t.AdjPercentBP == 0 {
		reason = ""
	}
	// who set it and when stay as they were
	if _, err := tx.Exec(ctx, `UPDATE orders SET service_charge=$2, tax_amount=$3, discount=$4, merchant_discount=$5,
		platform_subsidy=$6, total=$7, staff_discount=$8, adj_mode='', round_off=0, adj_reason=$9,
		adj_at = CASE WHEN $10 THEN adj_at END, adj_by = CASE WHEN $10 THEN adj_by END,
		adj_approved_by = CASE WHEN $10 THEN adj_approved_by END WHERE id=$1`,
		orderID, t.ServiceCharge, t.TaxAmount, t.Discount, t.MerchantDiscount, t.PlatformSubsidy, t.Total,
		t.StaffDiscount, reason, bp > 0); err != nil {
		return false, err
	}
	return mode != "", nil
}

// adjustable says whether staff may still change an order's discount at
// checkout: delivery and pickup orders until they leave the store (the same
// cut-off as an add-on), and not once they were paid.
func adjustable(o *lockedOrder) error {
	if open, why := addonOpen(o); !open {
		return ErrD(http.StatusConflict, "INVALID_STATE", "the order can no longer be changed", map[string]any{"why": why})
	}
	if o.Payment == "paid" {
		return ErrInvalidState("the order was already paid")
	}
	return nil
}

// PreviewOrderAdjust prices an order with a discount at checkout, without saving it.
func (a *App) PreviewOrderAdjust(ctx context.Context, orderID int64, j Adjust) (*Totals, error) {
	if err := j.validate(); err != nil {
		return nil, err
	}
	bid, err := a.OrderBranch(ctx, orderID)
	if err != nil {
		return nil, err
	}
	b, err := a.BranchByID(ctx, bid)
	if err != nil {
		return nil, err
	}
	base, err := orderAdjBase(ctx, a.DB, b, orderID)
	if err != nil {
		return nil, err
	}
	t, err := base.price(j)
	return &t, err
}

// SetOrderAdjust gives, changes or takes away a delivery or pickup order's
// discount at checkout from the order page.
func (a *App) SetOrderAdjust(ctx context.Context, orderID int64, expectedVersion int, j Adjust, actor Actor) (*Order, error) {
	if err := j.validate(); err != nil {
		return nil, err
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		o, err := lockOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if expectedVersion != 0 && expectedVersion != o.Version {
			return ErrVersion
		}
		if err := adjustable(o); err != nil {
			return err
		}
		b, err := scanBranch(tx.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, o.BranchID))
		if err != nil {
			return err
		}
		base, err := orderAdjBase(ctx, tx, b, o.ID)
		if err != nil {
			return err
		}
		t, err := base.price(j)
		if err != nil {
			return err
		}
		if err := writeAdjusted(ctx, tx, o.ID, t, j.Reason, actor); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.adjusted", map[string]any{"adjust": j,
			"total_before": o.Total, "total_after": t.Total, "staff_discount": t.StaffDiscount, "round_off": t.RoundOff}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
	if err != nil {
		return nil, err
	}
	return a.GetOrder(ctx, orderID)
}

// SetOrderPayment records or corrects how a paid delivery or pickup order was
// paid (a pickup paid by GCash at the counter).
func (a *App) SetOrderPayment(ctx context.Context, orderID int64, in PayInput, actor Actor) (*Order, error) {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		o, err := lockOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if o.Payment != "paid" {
			return ErrInvalidState("the order is not paid yet")
		}
		ch, name, err := resolvePay(ctx, tx, o.BranchID, in)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET pay_type=$2, pay_channel_id=$3, pay_method=$4, version=version+1,
			updated_at=now() WHERE id=$1`, o.ID, in.Type, ch, name); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.payment_recorded", map[string]any{"pay_type": in.Type, "method": name}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
	if err != nil {
		return nil, err
	}
	return a.GetOrder(ctx, orderID)
}
