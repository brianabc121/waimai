package app

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/brianabc121/waimai/internal/pricing"
)

// Coupons (docs/COUPON_DESIGN.md). A customer holds coupons; at checkout one
// of each kind can be used, on top of the store's promotion. They are taken
// off in a fixed order: spend-threshold, then no-minimum, then delivery fee,
// all paid by the store; the platform coupon comes last and is paid by the
// platform.

const (
	CouponStoreFree      = "store_free"
	CouponStoreThreshold = "store_threshold"
	CouponStoreDelivery  = "store_delivery"
	CouponPlatformFree   = "platform_free"
)

// couponOrder is the order coupons are applied in.
var couponOrder = []string{CouponStoreThreshold, CouponStoreFree, CouponStoreDelivery, CouponPlatformFree}

func validCouponKind(k string) bool {
	for _, x := range couponOrder {
		if x == k {
			return true
		}
	}
	return false
}

// CustomerCoupon is one coupon a customer holds.
type CustomerCoupon struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Issuer      string `json:"issuer"`
	BranchID    *int64 `json:"branch_id"`
	StoreSlug   string `json:"store_slug"`
	StoreName   string `json:"store_name"`
	StoreNameEn string `json:"store_name_en"`
	Currency    string `json:"currency"`
	Name        string `json:"name"`
	NameEn      string `json:"name_en"`
	// Value is the amount off; 0 on a delivery coupon means the whole fee.
	Value     int64      `json:"value"`
	Threshold int64      `json:"threshold"`
	Source    string     `json:"source"`
	IssuedAt  time.Time  `json:"issued_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	Status    string     `json:"status"` // available, used, expired
	OrderID   *int64     `json:"-"`
	UsedAt    *time.Time `json:"used_at"`
	// Where a platform coupon works: a country, maybe one province of it.
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	Province    string `json:"province"`

	scope          string
	scopeBranchIDs []int64
}

// CouponChoice is a coupon as the checkout offers it: whether it can be used
// on this cart, why not, and what it would take off.
type CouponChoice struct {
	CustomerCoupon
	Usable bool `json:"usable"`
	// Reason is "" when usable, else below_threshold, delivery_only or no_saving.
	Reason string `json:"reason"`
	// Short is how much more food is needed when below the threshold.
	Short    int64 `json:"short"`
	Saves    int64 `json:"saves"`
	Selected bool  `json:"selected"`
}

// AppliedCoupon is a coupon used on a quote.
type AppliedCoupon struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	NameEn   string `json:"name_en"`
	Amount   int64  `json:"amount"`
	FundedBy string `json:"funded_by"`
}

// OrderDiscount is one thing taken off an order: the promotion or a coupon.
type OrderDiscount struct {
	Kind     string `json:"kind"`
	FundedBy string `json:"funded_by"`
	Name     string `json:"name"`
	NameEn   string `json:"name_en"`
	Amount   int64  `json:"amount"`
	CouponID *int64 `json:"coupon_id"`
}

func fundedBy(kind string) string {
	if kind == CouponPlatformFree {
		return "platform"
	}
	return "store"
}

const couponCols = `c.id, c.kind, c.issuer, c.branch_id, COALESCE(b.slug, ''), COALESCE(b.name, ''), COALESCE(b.name_en, ''),
	c.currency, c.name, c.name_en, c.value, c.threshold, c.source, c.issued_at, c.expires_at,
	CASE WHEN c.status = 'used' THEN 'used' WHEN c.status = 'void' OR c.expires_at <= now() THEN 'expired' ELSE 'available' END,
	c.order_id, c.used_at, c.country, c.country_code, c.province, c.scope, c.scope_branch_ids`

func queryCoupons(ctx context.Context, q Querier, where string, args ...any) ([]CustomerCoupon, error) {
	rows, err := q.Query(ctx, `SELECT `+couponCols+` FROM customer_coupons c LEFT JOIN branches b ON b.id = c.branch_id
		WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomerCoupon{}
	for rows.Next() {
		var c CustomerCoupon
		if err := rows.Scan(&c.ID, &c.Kind, &c.Issuer, &c.BranchID, &c.StoreSlug, &c.StoreName, &c.StoreNameEn,
			&c.Currency, &c.Name, &c.NameEn, &c.Value, &c.Threshold, &c.Source, &c.IssuedAt, &c.ExpiresAt, &c.Status,
			&c.OrderID, &c.UsedAt, &c.Country, &c.CountryCode, &c.Province, &c.scope, &c.scopeBranchIDs); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// worksAt says whether a platform coupon covers the store.
func (c *CustomerCoupon) worksAt(b *Branch) bool {
	if c.Issuer == "store" {
		return c.BranchID != nil && *c.BranchID == b.ID
	}
	if c.Currency != b.Currency {
		return false
	}
	if c.CountryCode != "" && !strings.EqualFold(c.CountryCode, b.CountryCode) ||
		c.Province != "" && !strings.EqualFold(c.Province, b.Province) {
		return false
	}
	in := false
	for _, id := range c.scopeBranchIDs {
		if id == b.ID {
			in = true
		}
	}
	switch c.scope {
	case "only":
		return in
	case "except":
		return !in
	}
	return true
}

// walletFor lists the customer's unused, unexpired coupons that work at the store.
func walletFor(ctx context.Context, q Querier, phone string, b *Branch) ([]CustomerCoupon, error) {
	if phone == "" {
		return nil, nil
	}
	cs, err := queryCoupons(ctx, q, `c.customer_id = (SELECT id FROM customers WHERE phone=$1) AND c.status='unused'
		AND c.expires_at > now() AND (c.branch_id = $2 OR (c.issuer = 'platform' AND c.currency = $3))
		ORDER BY c.expires_at, c.id`, phone, b.ID, b.Currency)
	if err != nil {
		return nil, err
	}
	out := cs[:0]
	for _, c := range cs {
		if c.worksAt(b) {
			out = append(out, c)
		}
	}
	return out, nil
}

// couponPick is the outcome of choosing coupons for a cart.
type couponPick struct {
	choices  []CouponChoice
	applied  []AppliedCoupon
	merchant Discount // what the store's coupons take off
	platform *CouponChoice
}

// pickCoupons decides which coupons a cart uses. want nil means "choose for
// me": in each kind the one that saves most, the one expiring first on a tie.
// Otherwise want lists the chosen ids; ids that cannot be used are dropped.
// Thresholds are checked against the original item subtotal; the amounts are
// limited by what the promotion and earlier coupons left.
func pickCoupons(wallet []CustomerCoupon, want []int64, auto bool, service string, subtotal, fee int64, promo Discount) (*couponPick, error) {
	wanted := map[int64]bool{}
	for _, id := range want {
		wanted[id] = true
	}
	if !auto {
		seen := map[string]bool{}
		for _, c := range wallet {
			if wanted[c.ID] {
				if seen[c.Kind] {
					return nil, ErrValidation("only one coupon of each kind can be used")
				}
				seen[c.Kind] = true
			}
		}
	}
	goods, ship := subtotal-promo.Goods, fee-promo.Delivery
	res := &couponPick{choices: []CouponChoice{}, applied: []AppliedCoupon{}}
	for _, kind := range couponOrder {
		var group []CouponChoice
		for _, c := range wallet {
			if c.Kind != kind {
				continue
			}
			ch := CouponChoice{CustomerCoupon: c}
			switch {
			case kind == CouponStoreDelivery && service != "delivery":
				ch.Reason = "delivery_only"
			case subtotal < c.Threshold:
				ch.Reason, ch.Short = "below_threshold", c.Threshold-subtotal
			}
			switch kind {
			case CouponStoreThreshold, CouponStoreFree:
				ch.Saves = min(c.Value, goods)
			case CouponStoreDelivery:
				ch.Saves = ship
				if c.Value > 0 {
					ch.Saves = min(c.Value, ship)
				}
			case CouponPlatformFree:
				ch.Saves = c.Value // limited to what is left to pay, later
			}
			if ch.Reason == "" && ch.Saves <= 0 {
				ch.Reason = "no_saving"
			}
			ch.Usable = ch.Reason == ""
			if !ch.Usable {
				ch.Saves = 0
			}
			group = append(group, ch)
		}
		// usable first, then what saves most, then what expires first
		sort.SliceStable(group, func(i, j int) bool {
			a, b := group[i], group[j]
			if a.Usable != b.Usable {
				return a.Usable
			}
			if a.Saves != b.Saves {
				return a.Saves > b.Saves
			}
			return a.ExpiresAt.Before(b.ExpiresAt)
		})
		sel := -1
		for i := range group {
			if group[i].Usable && (auto || wanted[group[i].ID]) {
				sel = i
				break
			}
		}
		if sel >= 0 {
			ch := &group[sel]
			ch.Selected = true
			switch kind {
			case CouponStoreThreshold, CouponStoreFree:
				goods -= ch.Saves
				res.merchant.Goods += ch.Saves
			case CouponStoreDelivery:
				ship -= ch.Saves
				res.merchant.Delivery += ch.Saves
			}
			if kind != CouponPlatformFree {
				res.applied = append(res.applied, AppliedCoupon{ID: ch.ID, Kind: kind, Name: ch.Name, NameEn: ch.NameEn,
					Amount: ch.Saves, FundedBy: "store"})
			}
		}
		res.choices = append(res.choices, group...)
		if sel >= 0 && kind == CouponPlatformFree {
			res.platform = &res.choices[len(res.choices)-len(group)+sel]
		}
	}
	return res, nil
}

// applyCoupons prices the cart with its promotion and coupons: the store's
// coupons after the promotion and before tax, the platform coupon off what is
// left to pay at the end (money.go).
func applyCoupons(quote *Quote, wallet []CustomerCoupon, cfg TaxConfig, promo *Promotion, fee int64, promoD Discount) error {
	in := quote.Input
	var sub int64
	lines := []pricing.Line{}
	for i, l := range quote.Lines {
		sub += l.LineTotal
		lines = append(lines, pricing.Line{Key: strconv.Itoa(i), Amount: l.LineTotal})
	}
	pick, err := pickCoupons(wallet, derefIDs(in.CouponIDs), in.CouponIDs == nil, in.ServiceType, sub, fee, promoD)
	if err != nil {
		return err
	}
	used := []orderCoupon{}
	for _, c := range pick.applied {
		id := c.ID
		used = append(used, orderCoupon{CouponID: &id, Kind: c.Kind, FundedBy: c.FundedBy, Name: c.Name, NameEn: c.NameEn,
			Amount: c.Amount})
	}
	if p := pick.platform; p != nil {
		id := p.ID
		used = append(used, orderCoupon{CouponID: &id, Kind: p.Kind, FundedBy: "platform", Name: p.Name, NameEn: p.NameEn,
			Amount: p.Value})
	}
	pr, err := priceMoney(moneyIn{Lines: lines, Fees: cfg, Delivery: fee, Promo: promo, PromoOff: promoD, Coupons: used})
	if err != nil {
		return err
	}
	quote.Totals, quote.promo = pr.Totals, pr.Promo
	quote.Coupons = []AppliedCoupon{}
	ids := []int64{}
	for _, c := range pr.Coupons {
		if c.FundedBy == "platform" {
			pick.platform.Saves = c.Amount
			if c.Amount == 0 {
				pick.platform.Selected = false
				continue
			}
		}
		quote.Coupons = append(quote.Coupons, AppliedCoupon{ID: *c.CouponID, Kind: c.Kind, Name: c.Name, NameEn: c.NameEn,
			Amount: c.Amount, FundedBy: c.FundedBy})
		ids = append(ids, *c.CouponID)
	}
	quote.CouponChoices = pick.choices
	quote.Input.CouponIDs = &ids // placing the order uses exactly these
	return nil
}

func derefIDs(p *[]int64) []int64 {
	if p == nil {
		return nil
	}
	return *p
}

// claimCoupons marks the quote's coupons used by the order and records every
// discount on it. A coupon used meanwhile (another phone, another order)
// fails the order with QUOTE_CHANGED.
func claimCoupons(ctx context.Context, tx pgx.Tx, b *Branch, orderID int64, phone string, q *Quote) error {
	for _, c := range q.Coupons {
		tag, err := tx.Exec(ctx, `UPDATE customer_coupons SET status='used', order_id=$2, used_at=now()
			WHERE id=$1 AND status='unused' AND expires_at > now()
				AND customer_id = (SELECT id FROM customers WHERE phone=$3)`, c.ID, orderID, phone)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return Err(http.StatusConflict, "QUOTE_CHANGED", "a coupon is no longer available, please confirm the new amount")
		}
	}
	return recordDiscounts(ctx, tx, b.ID, orderID, q)
}

// recordDiscounts writes a new order's discount rows (its promotion and
// coupons) and who pays for them.
func recordDiscounts(ctx context.Context, tx pgx.Tx, branchID, orderID int64, q *Quote) error {
	if err := saveDiscountRows(ctx, tx, branchID, orderID, quotePriced(q)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE orders SET merchant_discount=$2, platform_subsidy=$3 WHERE id=$1`,
		orderID, q.Totals.MerchantDiscount, q.Totals.PlatformSubsidy)
	return err
}

// quotePriced is a quote's amounts as saveMoney takes them.
func quotePriced(q *Quote) *priced {
	p := &priced{Totals: q.Totals, Promo: q.promo}
	if p.Promo == nil && q.Totals.PromotionID != 0 {
		p.Promo = &Promotion{ID: q.Totals.PromotionID}
	}
	for _, c := range q.Coupons {
		id := c.ID
		p.Coupons = append(p.Coupons, orderCoupon{CouponID: &id, Kind: c.Kind, FundedBy: c.FundedBy, Name: c.Name,
			NameEn: c.NameEn, Amount: c.Amount})
	}
	return p
}

// releaseCoupons gives a rejected or cancelled order's coupons back. They keep
// their expiry date; one that expired meanwhile comes back expired.
func releaseCoupons(ctx context.Context, tx pgx.Tx, orderID int64) error {
	_, err := tx.Exec(ctx, `UPDATE customer_coupons SET status='unused', order_id=NULL, used_at=NULL
		WHERE order_id=$1 AND status='used'`, orderID)
	return err
}

// loadDiscounts fills in each order's discount lines.
func loadDiscounts(ctx context.Context, q Querier, byID map[int64]*Order, ids []int64) error {
	rows, err := q.Query(ctx, `SELECT order_id, kind, funded_by, name, name_en, amount, coupon_id
		FROM order_discounts WHERE order_id = ANY($1) ORDER BY order_id,
			array_position(ARRAY['promotion','store_threshold','store_free','store_delivery','platform_free'], kind), id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var d OrderDiscount
		var orderID int64
		if err := rows.Scan(&orderID, &d.Kind, &d.FundedBy, &d.Name, &d.NameEn, &d.Amount, &d.CouponID); err != nil {
			return err
		}
		byID[orderID].Discounts = append(byID[orderID].Discounts, d)
	}
	return rows.Err()
}

// ---- issuing ----

// expiryFor is the end of the valid_days-th day (the day of issue is day 1)
// on the store's clock.
func expiryFor(now time.Time, loc *time.Location, days int) time.Time {
	l := now.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day()+days, 0, 0, 0, 0, loc)
}

// giftRepurchase gives the store's repurchase coupon when a customer's first
// order there is completed. It runs in the transaction that completes the
// order, so a rolled-back completion gives nothing.
func (a *App) giftRepurchase(ctx context.Context, tx pgx.Tx, orderID int64) error {
	var branchID, subtotal int64
	var customerID *int64
	var service string
	if err := tx.QueryRow(ctx, `SELECT branch_id, customer_id, subtotal, service_type FROM orders WHERE id=$1`, orderID).
		Scan(&branchID, &customerID, &subtotal, &service); err != nil {
		return err
	}
	if customerID == nil || (service != "delivery" && service != "pickup") {
		return nil
	}
	var bt CouponBatch
	err := tx.QueryRow(ctx, `SELECT `+batchCols+` FROM coupon_batches
		WHERE branch_id=$1 AND source='repurchase' AND status='active' FOR UPDATE`, branchID).Scan(bt.scan()...)
	if isNoRows(err) {
		return nil
	} else if err != nil {
		return err
	}
	if subtotal < bt.MinOrder {
		return nil
	}
	var before bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders WHERE customer_id=$1 AND branch_id=$2 AND id <> $3
		AND service_type IN ('delivery','pickup') AND fulfillment_status IN ('delivered','picked_up'))`,
		*customerID, branchID, orderID).Scan(&before); err != nil || before {
		return err
	}
	b, err := scanBranch(tx.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, branchID))
	if err != nil {
		return err
	}
	now := time.Now()
	if bt.MonthlyCap != nil {
		l := now.In(b.location())
		month := time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, b.location())
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM customer_coupons WHERE batch_id=$1 AND issued_at >= $2`,
			bt.ID, month).Scan(&n); err != nil {
			return err
		}
		if n >= *bt.MonthlyCap {
			return nil
		}
	}
	exp := expiryFor(now, b.location(), bt.ValidDays)
	var couponID int64
	err = tx.QueryRow(ctx, `INSERT INTO customer_coupons (batch_id, customer_id, issuer, kind, branch_id, currency, name,
		name_en, value, threshold, source, source_order_id, expires_at)
		VALUES ($1,$2,'store',$3,$4,$5,$6,$7,$8,$9,'repurchase',$10,$11)
		ON CONFLICT (batch_id, customer_id) WHERE source = 'repurchase' DO NOTHING RETURNING id`,
		bt.ID, *customerID, bt.Kind, branchID, b.Currency, bt.Name, bt.NameEn, bt.Value, bt.Threshold, orderID, exp).Scan(&couponID)
	if isNoRows(err) {
		return nil // already had it
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE coupon_batches SET issued_count = issued_count + 1 WHERE id=$1`, bt.ID); err != nil {
		return err
	}
	zh, en := couponWords(bt.Kind, bt.Value, bt.Threshold, b.Currency)
	day := exp.Add(-time.Second).In(b.location())
	return tell(ctx, tx, *customerID, Message{Kind: "wallet",
		Title:   b.Name + "送你一张" + zh,
		TitleEn: pickName(b.NameEn, b.Name) + " sent you a coupon: " + en,
		Body:    day.Format("1月2日") + "前在本店下单可用", BodyEn: "Use it at this store by " + day.Format("2 Jan"),
		Link: "/me/coupons", Key: "store_coupon", Args: map[string]string{"store": b.Name, "store_en": b.NameEn,
			"kind": bt.Kind, "value": strconv.FormatInt(bt.Value, 10), "threshold": strconv.FormatInt(bt.Threshold, 10),
			"currency": b.Currency, "day": day.Format("2006-01-02")}})
}

// couponWords describes a coupon in a few words, Chinese and English.
func couponWords(kind string, value, threshold int64, currency string) (string, string) {
	v, th := fmtMoney(value, currency), fmtMoney(threshold, currency)
	switch kind {
	case CouponStoreThreshold:
		return "满" + th + "减" + v + "券", th + " off-" + v + " coupon"
	case CouponStoreDelivery:
		if value == 0 {
			if threshold > 0 {
				return "满" + th + "免配送费券", "free delivery over " + th
			}
			return "免配送费券", "free delivery coupon"
		}
		if threshold > 0 {
			return "满" + th + "配送费减" + v + "券", v + " off delivery over " + th
		}
		return "配送费减" + v + "券", v + " off delivery"
	}
	return v + "无门槛券", v + " off, no minimum"
}

// RemindExpiringCoupons leaves one message for each coupon that runs out in
// the next three days and has not been used. Coupons given less than a day
// ago are left alone.
func (a *App) RemindExpiringCoupons(ctx context.Context) (int, error) {
	rows, err := a.DB.Query(ctx, `UPDATE customer_coupons c SET reminded_at = now()
		WHERE c.status='unused' AND c.reminded_at IS NULL AND c.expires_at > now()
			AND c.expires_at <= now() + interval '3 days' AND c.issued_at < now() - interval '1 day'
		RETURNING c.customer_id, c.kind, c.value, c.threshold, c.currency, c.expires_at,
			COALESCE((SELECT name FROM branches b WHERE b.id = c.branch_id), '')`)
	if err != nil {
		return 0, err
	}
	type due struct {
		cust             int64
		kind, cur, store string
		value, threshold int64
		exp              time.Time
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.cust, &d.kind, &d.value, &d.threshold, &d.cur, &d.exp, &d.store); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, d := range list {
		zh, en := couponWords(d.kind, d.value, d.threshold, d.cur)
		body, bodyEn := "全平台通用", "Any store on the platform"
		args := map[string]string{"kind": d.kind, "value": strconv.FormatInt(d.value, 10),
			"threshold": strconv.FormatInt(d.threshold, 10), "currency": d.cur, "_body": "msg.any_store"}
		if d.store != "" {
			body, bodyEn = d.store, d.store
			delete(args, "_body")
		}
		if err := tell(ctx, a.DB, d.cust, Message{Kind: "wallet", Title: "你的" + zh + "快过期了",
			TitleEn: "Your coupon expires soon: " + en, Body: body, BodyEn: bodyEn, Link: "/me/coupons",
			Key: "coupon_expiring", Args: args}); err != nil {
			return 0, err
		}
	}
	return len(list), nil
}

// MyCoupons lists a customer's coupons: available ones first, those running
// out soonest at the top; then used and expired ones, newest first.
func (a *App) MyCoupons(ctx context.Context, customerID int64) ([]CustomerCoupon, error) {
	return queryCoupons(ctx, a.DB, `c.customer_id=$1 ORDER BY
		(c.status='unused' AND c.expires_at > now()) DESC,
		CASE WHEN c.status='unused' AND c.expires_at > now() THEN c.expires_at END,
		COALESCE(c.used_at, c.expires_at) DESC, c.id DESC LIMIT 200`, customerID)
}

// orderGift is the coupon an order earned, if any.
func orderGift(ctx context.Context, q Querier, orderID int64) (*CustomerCoupon, error) {
	cs, err := queryCoupons(ctx, q, `c.source_order_id=$1 ORDER BY c.id LIMIT 1`, orderID)
	if err != nil || len(cs) == 0 {
		return nil, err
	}
	return &cs[0], nil
}
