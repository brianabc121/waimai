package app

// Platform orders (docs/ORDER_TYPES_DESIGN.md §7, §9): an order a customer
// placed on Grab, foodpanda or another delivery platform, typed in by staff
// from the platform's tablet. It is numbered with the platform's prefix and
// its own number (Grab GF-123), priced at the platform's prices with tax
// included, carries at most two discounts (the store's own offer on the
// platform and the platform's), is paid on the platform and goes to the
// kitchen at once. What the store pays the platform (its commission and
// other charges) is kept apart from the order's amounts, in
// order_platform_fees.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/brianabc121/waimai/internal/pricing"
)

// The discount kinds of a platform order.
const (
	// KindPlatformMerchantPromo is the store's own offer on the platform: a
	// fixed amount off the whole order that the store pays (the order's
	// promotion, stage "order").
	KindPlatformMerchantPromo = "platform_merchant_promo"
	// KindPlatformPromo is the platform's own discount: after tax, paid by
	// the platform, it does not lower what the store is owed.
	KindPlatformPromo = "platform_promo"
)

// platformMerchantPromo is a platform order's own offer as a promotion, from
// its discount row: the amount typed in is its value.
func platformMerchantPromo(name, nameEn string, rule []byte, amount int64) *Promotion {
	var r dishRule
	_ = json.Unmarshal(rule, &r)
	if r.Value <= 0 {
		r.Value = amount
	}
	return &Promotion{Kind: KindPlatformMerchantPromo, Name: name, NameEn: nameEn, Value: r.Value, Status: "active"}
}

// platformPricer prices dishes on one platform for one store.
type platformPricer struct {
	PlatformID int64
	MarkupBP   int
	Step       int64
	// own are the prices the store set by hand, by dish.
	own map[int64]int64
}

// dish is a dish's platform price: the one set by hand, or the store price
// with the markup, rounded.
func (pp *platformPricer) dish(itemID, price int64) int64 {
	if v, ok := pp.own[itemID]; ok {
		return v
	}
	return pricing.PlatformPrice(price, pp.MarkupBP, pp.Step)
}

// option is an option's extra price on the platform, with the markup, rounded.
func (pp *platformPricer) option(delta int64) int64 {
	if delta < 0 {
		return -pricing.PlatformPrice(-delta, pp.MarkupBP, pp.Step)
	}
	return pricing.PlatformPrice(delta, pp.MarkupBP, pp.Step)
}

// The prices set by hand on a platform are the main brand's: another brand's
// platform prices are its own prices with its markup.
func loadPlatformPricer(ctx context.Context, q Querier, branchID, platformID int64, markup int, step int64, mainBrand bool) (*platformPricer, error) {
	pp := &platformPricer{PlatformID: platformID, MarkupBP: markup, Step: step, own: map[int64]int64{}}
	if !mainBrand {
		return pp, nil
	}
	rows, err := q.Query(ctx, `SELECT p.item_id, p.price FROM item_platform_prices p JOIN menu_items m ON m.id = p.item_id
		WHERE m.branch_id = $1 AND p.platform_id = $2`, branchID, platformID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, price int64
		if err := rows.Scan(&id, &price); err != nil {
			return nil, err
		}
		pp.own[id] = price
	}
	return pp, rows.Err()
}

// orderPlatformPricer is the pricer of a platform order's add-ons: the markup
// and rounding the order was placed with (a later change of the store's terms
// does not reach it), nil for other orders.
func orderPlatformPricer(ctx context.Context, q Querier, orderID int64) (*platformPricer, error) {
	var branchID int64
	var platformID *int64
	var markup int
	var step int64
	var main bool
	if err := q.QueryRow(ctx, `SELECT o.branch_id, o.platform_id, o.platform_markup_bp, o.platform_round_step,
			COALESCE((SELECT is_main FROM brands WHERE id = o.brand_id), true)
		FROM orders o WHERE o.id=$1`, orderID).Scan(&branchID, &platformID, &markup, &step, &main); err != nil {
		return nil, err
	}
	if platformID == nil {
		return nil, nil
	}
	return loadPlatformPricer(ctx, q, branchID, *platformID, markup, step, main)
}

// ---- the store's charges on an order ----

// PlatformFee is one charge of the platform on an order.
type PlatformFee struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"` // commission, other
	RateBP   int    `json:"rate_bp"`
	BaseKind string `json:"base_kind"`
	Base     int64  `json:"base"`
	Amount   int64  `json:"amount"`
	Manual   bool   `json:"manual"`
	Reason   string `json:"reason"`
}

// PlatformInfo is what a platform order shows besides its amounts.
type PlatformInfo struct {
	PlatformID int64             `json:"platform_id"`
	Name       string            `json:"name"`
	Names      map[string]string `json:"names"`
	Prefix     string            `json:"prefix"`
	Color      string            `json:"color"`
	LogoURL    string            `json:"logo_url"`
	OrderNo    string            `json:"order_no"`
	Pickup     string            `json:"pickup"` // rider, customer
	MarkupBP   int               `json:"markup_bp"`
	Fees       []PlatformFee     `json:"fees"`
	// Receivable is what the platform owes the store for the order: what the
	// customer paid and the platform's own discount, less what the platform
	// took back (Deducted). Payout is Receivable less the charges.
	Receivable int64 `json:"receivable"`
	Commission int64 `json:"commission"`
	OtherFees  int64 `json:"other_fees"`
	Deducted   int64 `json:"deducted"`
	Payout     int64 `json:"payout"`
}

// commissionBase is what a commission is counted on: the dishes after the
// store's discount, or before it.
func commissionBase(kind string, subtotal, merchantDiscount int64) int64 {
	if kind == "original" {
		return subtotal
	}
	return max(0, subtotal-merchantDiscount)
}

// refreshPlatformFees works a platform order's commission out again after its
// amounts changed (an add-on, a void): rate × base, unless staff typed it.
// Other orders are left alone.
func refreshPlatformFees(ctx context.Context, tx pgx.Tx, orderID int64) error {
	var sub, merch int64
	var isPlatform bool
	if err := tx.QueryRow(ctx, `SELECT subtotal, merchant_discount, order_type = 'platform' FROM orders WHERE id=$1`,
		orderID).Scan(&sub, &merch, &isPlatform); err != nil {
		return err
	}
	if !isPlatform {
		return nil
	}
	var id int64
	var rate int
	var kind string
	var manual bool
	err := tx.QueryRow(ctx, `SELECT id, rate_bp, base_kind, manual FROM order_platform_fees
		WHERE order_id=$1 AND kind='commission' FOR UPDATE`, orderID).Scan(&id, &rate, &kind, &manual)
	if isNoRows(err) {
		return nil
	} else if err != nil {
		return err
	}
	base := commissionBase(kind, sub, merch)
	if manual {
		_, err = tx.Exec(ctx, `UPDATE order_platform_fees SET base=$2, updated_at=now() WHERE id=$1`, id, base)
	} else {
		_, err = tx.Exec(ctx, `UPDATE order_platform_fees SET base=$2, amount=$3, updated_at=now() WHERE id=$1`,
			id, base, pricing.Commission(base, rate))
	}
	return err
}

// platformInfo reads what the orders of ids carry from their platform, by order.
func platformInfo(ctx context.Context, q Querier, ids []int64) (map[int64]*PlatformInfo, error) {
	out := map[int64]*PlatformInfo{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT o.id, p.id, p.name, p.names, p.prefix, p.color, p.logo_url, o.platform_order_no,
		o.platform_pickup, o.platform_markup_bp, o.total + o.platform_subsidy,
		COALESCE((SELECT sum(r.amount) FROM order_refunds r WHERE r.order_id = o.id), 0)
		FROM orders o JOIN delivery_platforms p ON p.id = o.platform_id WHERE o.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var x PlatformInfo
		var names []byte
		var owed int64
		if err := rows.Scan(&id, &x.PlatformID, &x.Name, &names, &x.Prefix, &x.Color, &x.LogoURL, &x.OrderNo, &x.Pickup,
			&x.MarkupBP, &owed, &x.Deducted); err != nil {
			rows.Close()
			return nil, err
		}
		x.Names = map[string]string{}
		_ = json.Unmarshal(names, &x.Names)
		x.Receivable = owed - x.Deducted
		x.Fees = []PlatformFee{}
		out[id] = &x
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT order_id, id, kind, rate_bp, base_kind, base, amount, manual, reason
		FROM order_platform_fees WHERE order_id = ANY($1) ORDER BY kind, id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var oid int64
		var f PlatformFee
		if err := rows.Scan(&oid, &f.ID, &f.Kind, &f.RateBP, &f.BaseKind, &f.Base, &f.Amount, &f.Manual, &f.Reason); err != nil {
			return nil, err
		}
		if x := out[oid]; x != nil {
			x.Fees = append(x.Fees, f)
			if f.Kind == "commission" {
				x.Commission += f.Amount
			} else {
				x.OtherFees += f.Amount
			}
		}
	}
	for _, x := range out {
		x.Payout = x.Receivable - x.Commission - x.OtherFees
	}
	return out, rows.Err()
}

// ---- entering an order ----

// PlatformOrderInput is a platform order as staff copy it from the platform.
type PlatformOrderInput struct {
	PlatformID int64 `json:"platform_id"`
	// BrandID is the brand whose shop on the platform took the order (0 = the main one).
	BrandID int64 `json:"brand_id"`
	// PlatformNo is the platform's own number of the order (GF-123).
	PlatformNo string `json:"platform_no"`
	// Pickup is who collects it: the platform's rider (default) or the customer.
	Pickup       string      `json:"pickup"`
	ScheduledFor *time.Time  `json:"scheduled_for"`
	CustomerName string      `json:"customer_name"`
	Note         string      `json:"note"`
	StaffNote    string      `json:"staff_note"`
	Items        []StaffLine `json:"items"`
	// PackagingFee is what the platform lets the store charge for packing.
	PackagingFee int64 `json:"packaging_fee"`
	// MerchantPromo is the store's own offer on the platform, PlatformPromo
	// the platform's; both are amounts off the whole order.
	MerchantPromo int64 `json:"merchant_promo"`
	PlatformPromo int64 `json:"platform_promo"`
	// Commission replaces the commission worked out from the rate, with a reason.
	Commission       *int64 `json:"commission"`
	CommissionReason string `json:"commission_reason"`
	// ExpectedTotal is the amount the staff member saw.
	ExpectedTotal *int64 `json:"expected_total"`
}

// PlatformQuote is a platform order priced, before it is placed.
type PlatformQuote struct {
	Currency string      `json:"currency"`
	Lines    []QuoteLine `json:"lines"`
	Totals   Totals      `json:"totals"`
	// Receivable is what the platform owes the store: the total and the
	// platform's discount. Payout is that less the commission.
	Receivable     int64  `json:"receivable"`
	CommissionRate int    `json:"commission_rate_bp"`
	CommissionBase string `json:"commission_base"`
	Commission     int64  `json:"commission"`
	CommissionAuto int64  `json:"commission_auto"`
	Payout         int64  `json:"payout"`
	// Duplicate is the order already entered with this platform number today.
	Duplicate *PlatformDuplicate `json:"duplicate,omitempty"`

	platform *BranchPlatform
	pr       *priced
	pricer   *platformPricer
	no       string
	brand    *Brand
}

// PlatformDuplicate is an order already entered with the same platform number.
type PlatformDuplicate struct {
	OrderID   int64     `json:"order_id"`
	PublicNo  string    `json:"public_no"`
	CreatedAt time.Time `json:"created_at"`
}

var platformNoRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}\-_#./ ]{0,39}$`)

func (in *PlatformOrderInput) check() error {
	in.PlatformNo = strings.TrimSpace(in.PlatformNo)
	in.CustomerName, in.Note, in.StaffNote = strings.TrimSpace(in.CustomerName), strings.TrimSpace(in.Note), strings.TrimSpace(in.StaffNote)
	in.CommissionReason = strings.TrimSpace(in.CommissionReason)
	if in.Pickup == "" {
		in.Pickup = "rider"
	}
	if in.Pickup != "rider" && in.Pickup != "customer" {
		return ErrValidation("pickup must be rider or customer")
	}
	if len([]rune(in.CustomerName)) > 60 || len([]rune(in.Note)) > 300 || len([]rune(in.StaffNote)) > 300 ||
		len([]rune(in.CommissionReason)) > 200 {
		return ErrValidation("text too long")
	}
	for _, v := range []int64{in.PackagingFee, in.MerchantPromo, in.PlatformPromo} {
		if v < 0 || v > 100_000_000 {
			return ErrValidation("amounts must be between 0 and 1,000,000")
		}
	}
	if in.Commission != nil {
		if *in.Commission < 0 || *in.Commission > 100_000_000 {
			return ErrValidation("invalid commission")
		}
		if in.CommissionReason == "" {
			return ErrValidation("say why the commission was changed")
		}
	}
	return nil
}

// checkNo checks the platform's number, needed to place the order.
func (in *PlatformOrderInput) checkNo() error {
	if !platformNoRe.MatchString(in.PlatformNo) {
		return ErrValidation("enter the platform's order number")
	}
	return nil
}

// platformQuote prices a platform order: the platform's prices, tax
// included, no service charge or delivery fee, the store's offer on the
// whole order, the platform's after tax; and the commission.
func platformQuote(ctx context.Context, q Querier, b *Branch, in PlatformOrderInput) (*PlatformQuote, error) {
	if in.PlatformID == 0 {
		return nil, ErrValidation("choose the platform")
	}
	// the brand's shop on the platform: its terms, its menu and prices
	brand, err := orderBrand(ctx, q, b.ID, in.BrandID)
	if err != nil {
		return nil, err
	}
	list, err := branchPlatforms(ctx, q, b.ID, brand.ID, true)
	if err != nil {
		return nil, err
	}
	var bp *BranchPlatform
	for i := range list {
		if list[i].ID == in.PlatformID && list[i].Active {
			bp = &list[i]
		}
	}
	if bp == nil {
		return nil, Err(http.StatusUnprocessableEntity, "PLATFORM_OFF", "the store does not sell on this platform")
	}
	pp, err := loadPlatformPricer(ctx, q, b.ID, bp.ID, bp.Markup, bp.RoundStep, brand.IsMain)
	if err != nil {
		return nil, err
	}
	service := "delivery"
	if in.Pickup == "customer" {
		service = "pickup"
	}
	items := []QuoteLineInput{}
	for _, l := range in.Items {
		items = append(items, QuoteLineInput{ItemID: l.ItemID, Qty: l.Qty, OptionIDs: l.OptionIDs,
			Note: strings.TrimSpace(l.Note), NoteTagIDs: l.NoteTagIDs, repeat: true})
	}
	lines, sub, err := priceLinesOn(ctx, q, b.ID, brand, service, items, pp)
	if err != nil {
		return nil, err
	}
	for i := range lines {
		lines[i].Basket = 1
	}
	if in.MerchantPromo > sub {
		return nil, ErrValidation("the store's offer is more than the dishes")
	}
	cfg := TaxConfig{TaxRateBP: b.TaxRateBP, TaxInclusive: true, PackagingFee: in.PackagingFee}
	mi := moneyIn{Fees: cfg}
	for i, l := range lines {
		mi.Lines = append(mi.Lines, pricing.Line{Key: strconv.Itoa(i), Amount: l.LineTotal})
	}
	name := platformOfferName(bp)
	if in.MerchantPromo > 0 {
		mi.Promo = &Promotion{Kind: KindPlatformMerchantPromo, Name: name, NameEn: bp.Name + " offer", Value: in.MerchantPromo,
			Status: "active"}
		mi.PromoOff = Discount{Goods: in.MerchantPromo}
	}
	if in.PlatformPromo > 0 {
		mi.Coupons = []orderCoupon{{Kind: KindPlatformPromo, FundedBy: "platform", Name: bp.Name,
			NameEn: bp.Name, Amount: in.PlatformPromo}}
	}
	p, err := priceMoney(mi)
	if err != nil {
		return nil, err
	}
	t := p.Totals
	if t.PlatformSubsidy != in.PlatformPromo {
		return nil, ErrValidation("the platform's discount is more than the order")
	}
	out := &PlatformQuote{Currency: b.Currency, Lines: lines, Totals: t, Receivable: t.Total + t.PlatformSubsidy,
		CommissionRate: bp.Commission, CommissionBase: bp.CommissionBase, platform: bp, pr: p, pricer: pp, no: in.PlatformNo,
		brand: brand}
	out.CommissionAuto = pricing.Commission(commissionBase(bp.CommissionBase, t.Subtotal, t.MerchantDiscount), bp.Commission)
	out.Commission = out.CommissionAuto
	if in.Commission != nil {
		out.Commission = *in.Commission
	}
	out.Payout = out.Receivable - out.Commission
	if in.PlatformNo != "" {
		if out.Duplicate, err = platformDuplicate(ctx, q, b, bp.ID, in.PlatformNo); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// platformOfferName is how the store's offer on a platform is named on the order.
func platformOfferName(bp *BranchPlatform) string { return bp.Name + " 商家活动" }

// platformDuplicate finds an order of this business day with the same
// platform number that was not cancelled.
func platformDuplicate(ctx context.Context, q Querier, b *Branch, platformID int64, no string) (*PlatformDuplicate, error) {
	var d PlatformDuplicate
	err := q.QueryRow(ctx, `SELECT id, public_no, created_at FROM orders WHERE branch_id=$1 AND platform_id=$2
		AND lower(platform_order_no) = lower($3) AND biz_date = $4::date AND acceptance_status NOT IN ('rejected','cancelled')
		ORDER BY id DESC LIMIT 1`, b.ID, platformID, no, b.businessDay(time.Now()).Format("2006-01-02")).
		Scan(&d.OrderID, &d.PublicNo, &d.CreatedAt)
	if isNoRows(err) {
		return nil, nil
	}
	return &d, err
}

// PlatformQuote prices what staff have entered so far.
func (a *App) PlatformQuote(ctx context.Context, branchID int64, in PlatformOrderInput) (*PlatformQuote, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	return platformQuote(ctx, a.DB, b, in)
}

// PlacePlatformOrder enters a platform order. It is accepted at once and goes
// to the kitchen; nothing is sent to the customer, who follows it on the platform.
func (a *App) PlacePlatformOrder(ctx context.Context, branchID int64, actor Actor, idemKey string, in PlatformOrderInput) (*Order, error) {
	if idemKey == "" || len(idemKey) > 100 {
		return nil, ErrValidation("Idempotency-Key header is required")
	}
	if err := in.check(); err != nil {
		return nil, err
	}
	if err := in.checkNo(); err != nil {
		return nil, err
	}
	var orderID int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		b, err := scanBranch(tx.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, branchID))
		if err != nil {
			return err
		}
		scope := fmt.Sprintf("platform-order:%d", b.ID)
		var rec staffPlaced
		replayed, err := idemBegin(ctx, tx, scope, idemKey, hashJSON(in), &rec)
		if err != nil {
			return err
		}
		if replayed {
			orderID = rec.OrderID
			return nil
		}
		// one number of a platform is entered once a day: two staff typing the same order wait for each other
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
			fmt.Sprintf("platform-no:%d:%d:%s", b.ID, in.PlatformID, strings.ToLower(in.PlatformNo))); err != nil {
			return err
		}
		cur, err := platformQuote(ctx, tx, b, in)
		if err != nil {
			return err
		}
		if d := cur.Duplicate; d != nil {
			return ErrD(http.StatusConflict, "DUPLICATE", "this platform order was entered already", d)
		}
		if in.ExpectedTotal != nil && *in.ExpectedTotal != cur.Totals.Total {
			return ErrD(http.StatusConflict, "QUOTE_CHANGED", "the amount changed, please confirm it again",
				map[string]any{"total": cur.Totals.Total})
		}
		if cur.Payout < 0 {
			return ErrValidation("the commission is more than the store receives for this order")
		}
		if err := reserveStock(ctx, tx, cur.Lines); err != nil {
			return err
		}
		bp := cur.platform
		biz := b.businessDay(time.Now()).Format("2006-01-02")
		no := orderNo{Type: TypePlatform, Public: bp.Prefix + " " + in.PlatformNo, Code: in.PlatformNo, BizDate: biz}
		nonce := randomBytes(16)
		if err := moneyEvent(ctx, tx, "placed", 0, actor); err != nil {
			return err
		}
		t := cur.Totals
		err = tx.QueryRow(ctx, `INSERT INTO orders (merchant_id, branch_id, public_no, pickup_code,
			access_token_hash, token_nonce, channel, service_type, customer_name, customer_phone, note, currency,
			subtotal, packaging_fee, delivery_fee, service_charge, service_charge_bp, tax_amount, tax_inclusive, tax_rate_bp,
			discount, total, payment_method, payment_status, pay_type, amount_received, accept_deadline,
			scheduled_for, release_at, placed_by, staff_note,
			platform_id, platform_order_no, platform_pickup, platform_markup_bp, platform_round_step, brand_id)
			VALUES ($1,$2,$3,$4,$5,$6,'staff','pickup',$7,'',$8,$9,$10,$11,0,0,0,$12,true,$13,$14,$15,'prepaid','paid','platform',$15,
				now() + make_interval(mins => $16), $17::timestamptz, $17::timestamptz - make_interval(mins => $18),
				NULLIF($19, 0), $20, $21, $22, $23, $24, $25, $26)
			RETURNING id`,
			b.MerchantID, b.ID, no.Public, no.Code, hashToken(a.orderToken(nonce)), nonce, in.CustomerName, in.Note,
			b.Currency, t.Subtotal, t.PackagingFee, t.TaxAmount, t.TaxRateBP, t.Discount, t.Total,
			b.AcceptTimeoutMinutes, in.ScheduledFor, b.PrepMinutes, actor.ID, in.StaffNote,
			bp.ID, in.PlatformNo, in.Pickup, cur.pricer.MarkupBP, cur.pricer.Step, cur.brand.ID).Scan(&orderID)
		if err != nil {
			return wrap("insert platform order", err)
		}
		if err := stampOrder(ctx, tx, orderID, no, ""); err != nil {
			return err
		}
		var batchID int64
		if err := tx.QueryRow(ctx, `INSERT INTO order_batches (order_id, seq) VALUES ($1, 1) RETURNING id`,
			orderID).Scan(&batchID); err != nil {
			return err
		}
		lineIDs := []int64{}
		for _, l := range cur.Lines {
			opts, _ := json.Marshal(l.Options)
			var lineID int64
			if err := tx.QueryRow(ctx, `INSERT INTO order_items (order_id, branch_id, batch_id, item_id, name,
				name_en, unit_price, qty, line_total, options, note, needs_prep, basket_no, name_customer, note_tags, base_name)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,'',$13,$14) RETURNING id`, orderID, b.ID, batchID, l.ItemID, l.Name,
				l.NameEn, l.UnitPrice, l.Qty, l.LineTotal, opts, l.Note, l.NeedsPrep, noteTagsJSON(l.NoteTags), l.BaseName).Scan(&lineID); err != nil {
				return wrap("insert order item", err)
			}
			lineIDs = append(lineIDs, lineID)
		}
		manual := in.Commission != nil
		var by *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO order_platform_fees (order_id, branch_id, platform_id, kind, rate_bp,
			base_kind, amount, manual, reason, created_by) VALUES ($1,$2,$3,'commission',$4,$5,$6,$7,$8,$9)`,
			orderID, b.ID, bp.ID, bp.Commission, bp.CommissionBase, cur.Commission, manual, in.CommissionReason, by); err != nil {
			return err
		}
		p := cur.pr
		p.ids = map[string]int64{}
		for i, id := range lineIDs {
			p.ids[strconv.Itoa(i)] = id
		}
		if err := saveMoney(ctx, tx, b, orderID, p, false); err != nil {
			return err
		}
		if err := audit(ctx, tx, b.ID, orderID, actor, "order.platform_placed", map[string]any{"platform": bp.Name,
			"platform_no": in.PlatformNo, "total": t.Total, "merchant_promo": in.MerchantPromo,
			"platform_promo": in.PlatformPromo, "commission": cur.Commission, "commission_manual": manual}); err != nil {
			return err
		}
		if err := idemSave(ctx, tx, scope, idemKey, staffPlaced{OrderID: orderID}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: b.ID, OrderID: orderID})
	})
	if err != nil {
		return nil, wrap("place platform order", err)
	}
	// Taken on the platform already: straight to the kitchen (§13, question 15).
	if _, err := a.Accept(ctx, orderID, 0, actor); err != nil {
		return nil, wrap("accept platform order", err)
	}
	return loadOrder(ctx, a.DB, orderID)
}

// ---- after the order ----

// SetPlatformCommission types the commission of a platform order (with a
// reason), or with amount nil works it out from the rate again.
func (a *App) SetPlatformCommission(ctx context.Context, orderID int64, amount *int64, reason string, actor Actor) (*Order, error) {
	reason = strings.TrimSpace(reason)
	if amount != nil && (*amount < 0 || *amount > 100_000_000) {
		return nil, ErrValidation("invalid commission")
	}
	if amount != nil && reason == "" {
		return nil, ErrValidation("say why the commission was changed")
	}
	if len([]rune(reason)) > 200 {
		return nil, ErrValidation("text too long")
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		var id, before int64
		err := tx.QueryRow(ctx, `SELECT id, amount FROM order_platform_fees WHERE order_id=$1 AND kind='commission' FOR UPDATE`,
			o.ID).Scan(&id, &before)
		if isNoRows(err) {
			return ErrInvalidState("not a platform order")
		} else if err != nil {
			return err
		}
		if amount != nil {
			_, err = tx.Exec(ctx, `UPDATE order_platform_fees SET amount=$2, manual=true, reason=$3, updated_at=now() WHERE id=$1`,
				id, *amount, reason)
		} else {
			_, err = tx.Exec(ctx, `UPDATE order_platform_fees SET manual=false, reason='', updated_at=now() WHERE id=$1`, id)
		}
		if err != nil {
			return err
		}
		if err := refreshPlatformFees(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := checkPayout(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		return audit(ctx, tx, o.BranchID, o.ID, actor, "order.platform_commission",
			map[string]any{"before": before, "amount": amount, "reason": reason})
	})
}

// checkPayout refuses charges that are more than the store gets for the order
// (D11); what the platform took back later is not counted.
func checkPayout(ctx context.Context, tx pgx.Tx, orderID int64) error {
	var payout int64
	if err := tx.QueryRow(ctx, `SELECT o.total + o.platform_subsidy
		- COALESCE((SELECT sum(f.amount) FROM order_platform_fees f WHERE f.order_id = o.id), 0)
		FROM orders o WHERE o.id=$1`, orderID).Scan(&payout); err != nil {
		return err
	}
	if payout < 0 {
		return ErrValidation("the charges are more than the store receives for this order")
	}
	return nil
}

// AddPlatformFee records another charge of the platform on an order (an
// advertising fee, a packaging charge).
func (a *App) AddPlatformFee(ctx context.Context, orderID int64, amount int64, reason string, actor Actor) (*Order, error) {
	reason = strings.TrimSpace(reason)
	if amount <= 0 || amount > 100_000_000 {
		return nil, ErrValidation("the amount must be more than 0")
	}
	if reason == "" || len([]rune(reason)) > 200 {
		return nil, ErrValidation("say what the charge is for")
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		var platformID *int64
		if err := tx.QueryRow(ctx, `SELECT platform_id FROM orders WHERE id=$1`, o.ID).Scan(&platformID); err != nil {
			return err
		}
		if platformID == nil {
			return ErrInvalidState("not a platform order")
		}
		var by *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO order_platform_fees (order_id, branch_id, platform_id, kind, base_kind,
			amount, manual, reason, created_by) VALUES ($1,$2,$3,'other','',$4,true,$5,$6)`,
			o.ID, o.BranchID, *platformID, amount, reason, by); err != nil {
			return err
		}
		if err := checkPayout(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		return audit(ctx, tx, o.BranchID, o.ID, actor, "order.platform_fee", map[string]any{"amount": amount, "reason": reason})
	})
}

// DeletePlatformFee takes back another charge entered by mistake.
func (a *App) DeletePlatformFee(ctx context.Context, orderID, feeID int64, actor Actor) (*Order, error) {
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		var amount int64
		err := tx.QueryRow(ctx, `DELETE FROM order_platform_fees WHERE id=$1 AND order_id=$2 AND kind='other' RETURNING amount`,
			feeID, o.ID).Scan(&amount)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		return audit(ctx, tx, o.BranchID, o.ID, actor, "order.platform_fee_deleted", map[string]any{"amount": amount})
	})
}

// RecordPlatformDeduction records money the platform took back from the store
// for an order (a complaint, a missing dish). The platform refunds the
// customer itself; the store only keeps the record, as a refund paid by the
// platform, so the reconciliation shows it (§7.4).
func (a *App) RecordPlatformDeduction(ctx context.Context, orderID int64, amount int64, reason string, actor Actor) (*Order, error) {
	reason = strings.TrimSpace(reason)
	if amount <= 0 {
		return nil, ErrValidation("the amount must be more than 0")
	}
	if reason == "" || len([]rune(reason)) > 200 {
		return nil, ErrValidation("say why the platform took it back")
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		var platformID *int64
		var refunded int64
		if err := tx.QueryRow(ctx, `SELECT platform_id, COALESCE((SELECT sum(amount) FROM order_refunds r WHERE r.order_id = o.id), 0)
			FROM orders o WHERE o.id=$1`, o.ID).Scan(&platformID, &refunded); err != nil {
			return err
		}
		if platformID == nil {
			return ErrInvalidState("not a platform order")
		}
		if o.Acceptance == "rejected" || o.Acceptance == "cancelled" {
			return ErrInvalidState("the order was cancelled")
		}
		if amount > o.Total-refunded {
			return ErrValidation("more than the order's amount")
		}
		var by, approvedBy *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if actor.ApprovedBy != 0 {
			approvedBy = &actor.ApprovedBy
		}
		if _, err := tx.Exec(ctx, `INSERT INTO order_refunds (order_id, branch_id, lines, amount, method, reason, reason_kind,
			refunded_by, approved_by, pay_type) VALUES ($1,$2,'[]',$3,'platform',$4,'store',$5,$6,'platform')`,
			o.ID, o.BranchID, amount, reason, by, approvedBy); err != nil {
			return err
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.platform_deduction",
			map[string]any{"amount": amount, "reason": reason}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
}
