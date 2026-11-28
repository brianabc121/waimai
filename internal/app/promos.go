package app

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Promotion is a store-level offer. At most one applies to an order.
type Promotion struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	NameEn          string     `json:"name_en"`
	NameTr          string     `json:"name_tr,omitempty"`
	Kind            string     `json:"kind"` // a key of promoRules (rules.go)
	Threshold       int64      `json:"threshold"`
	Value           int64      `json:"value"`
	MaxDiscount     *int64     `json:"max_discount"`
	AppliesDelivery bool       `json:"applies_delivery"`
	AppliesPickup   bool       `json:"applies_pickup"`
	StartsAt        *time.Time `json:"starts_at"`
	EndsAt          *time.Time `json:"ends_at"`
	DailyStart      string     `json:"daily_start"`
	DailyEnd        string     `json:"daily_end"`
	TotalQuota      *int       `json:"total_quota"`
	PerPhoneLimit   *int       `json:"per_phone_limit"`
	UsedCount       int        `json:"used_count"`
	Status          string     `json:"status"`
	// Scope is delivery (delivery and pickup orders) or dine_in (table
	// bills). The two sets never mix.
	Scope string `json:"scope"`
	// LiveNow says whether the promotion would apply to an order placed now
	// (active, inside its dates and daily window, quota left).
	LiveNow bool `json:"live_now"`
	// ItemIDs are the dishes a dish promotion is on.
	ItemIDs []int64 `json:"item_ids"`
	// BrandIDs are the store's brands it applies to (docs/MULTI_BRAND_DESIGN.md §10).
	BrandIDs []int64 `json:"brand_ids"`

	phoneUsed int // redemptions by the quoting customer, when known
}

// Discount is what a promotion takes off: Goods reduces the taxable item
// amount, Delivery reduces the (untaxed) delivery fee.
type Discount struct {
	Goods    int64
	Delivery int64
}

func (d Discount) total() int64 { return d.Goods + d.Delivery }

// inWindow checks dates and the daily window in the branch's local time.
func (p *Promotion) inWindow(now time.Time, loc *time.Location) bool {
	if p.StartsAt != nil && now.Before(*p.StartsAt) {
		return false
	}
	if p.EndsAt != nil && !now.Before(*p.EndsAt) {
		return false
	}
	if p.DailyStart == p.DailyEnd {
		return true
	}
	cur := now.In(loc).Format("15:04")
	if p.DailyStart < p.DailyEnd {
		return cur >= p.DailyStart && cur < p.DailyEnd
	}
	return cur >= p.DailyStart || cur < p.DailyEnd // crosses midnight
}

// why returns "" when the promotion applies to the cart, or the reason it does not.
func (p *Promotion) why(now time.Time, loc *time.Location, service string, subtotal int64) string {
	switch {
	case p.Status != "active":
		return "not_active"
	case !p.inWindow(now, loc):
		return "outside_time"
	case (service == "dine_in") != (p.Scope == "dine_in"):
		return "service_type"
	case service == "delivery" && !p.AppliesDelivery, service == "pickup" && !p.AppliesPickup:
		return "service_type"
	case p.Kind == "free_delivery" && service != "delivery":
		return "service_type"
	case subtotal < p.Threshold:
		return "below_threshold"
	case p.TotalQuota != nil && p.UsedCount >= *p.TotalQuota:
		return "quota_used_up"
	case p.PerPhoneLimit != nil && p.phoneUsed >= *p.PerPhoneLimit:
		return "customer_limit"
	}
	return ""
}

// discount computes what a whole-order promotion saves, ignoring
// eligibility; a dish promotion saves nothing here (see dishAdjustments).
func (p *Promotion) discount(subtotal, deliveryFee int64) Discount {
	if r := promoRules[p.Kind]; r.order != nil {
		return r.order(p, subtotal, deliveryFee)
	}
	return Discount{}
}

// bestPromotion picks the applicable promotion that saves the most. Ties go
// to the oldest promotion, so the choice is stable between quote and order.
func bestPromotion(ps []Promotion, now time.Time, loc *time.Location, service string, subtotal, deliveryFee int64) (*Promotion, Discount) {
	var best *Promotion
	var bestD Discount
	for i := range ps {
		p := &ps[i]
		if p.isDish() || p.why(now, loc, service, subtotal) != "" {
			continue
		}
		d := p.discount(subtotal, deliveryFee)
		if d.total() > bestD.total() {
			best, bestD = p, d
		}
	}
	return best, bestD
}

const promoCols = `p.id, p.name, p.name_en, p.kind, p.threshold, p.value, p.max_discount, p.applies_delivery,
	p.applies_pickup, p.starts_at, p.ends_at, p.daily_start, p.daily_end, p.total_quota, p.per_phone_limit,
	p.used_count, p.status, p.scope, ARRAY(SELECT pi.item_id FROM promotion_items pi WHERE pi.promotion_id = p.id ORDER BY pi.item_id),
	p.brand_ids`

// scanDest is where a row of promoCols followed by the phone-use count goes.
// Every query built on promoCols scans through it, so adding a column to
// promoCols means adding it here too and nowhere else.
func (p *Promotion) scanDest(before ...any) []any {
	return append(before, &p.ID, &p.Name, &p.NameEn, &p.Kind, &p.Threshold, &p.Value, &p.MaxDiscount,
		&p.AppliesDelivery, &p.AppliesPickup, &p.StartsAt, &p.EndsAt, &p.DailyStart, &p.DailyEnd,
		&p.TotalQuota, &p.PerPhoneLimit, &p.UsedCount, &p.Status, &p.Scope, &p.ItemIDs, &p.BrandIDs, &p.phoneUsed)
}

// loadPromotions returns a branch's promotions, oldest first. With a phone,
// each one also carries how often that customer has used it.
func loadPromotions(ctx context.Context, q Querier, branchID int64, where, phone string) ([]Promotion, error) {
	rows, err := q.Query(ctx, `SELECT `+promoCols+`,
		(SELECT count(*) FROM promotion_redemptions r WHERE r.promotion_id = p.id AND r.status='applied'
			AND $2 <> '' AND r.phone = $2)
		FROM promotions p WHERE p.branch_id=$1 AND `+where+` ORDER BY p.id`, branchID, phone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Promotion{}
	for rows.Next() {
		var p Promotion
		if err := rows.Scan(p.scanDest()...); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (p *Promotion) setLive(now time.Time, loc *time.Location) {
	p.LiveNow = p.Status == "active" && p.inWindow(now, loc) && (p.TotalQuota == nil || p.UsedCount < *p.TotalQuota)
}

// ListPromotions is the merchant view, including drafts and ended ones.
func (a *App) ListPromotions(ctx context.Context, branchID int64) ([]Promotion, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	ps, err := loadPromotions(ctx, a.DB, branchID, "true", "")
	if err != nil {
		return nil, err
	}
	for i := range ps {
		ps[i].setLive(time.Now(), b.location())
	}
	return ps, nil
}

// livePromotions is what the storefront advertises: promotions of one scope
// (delivery or dine_in) that apply right now, at a brand (0 = the main one).
func livePromotions(ctx context.Context, q Querier, b *Branch, brandID int64, scope string) ([]Promotion, error) {
	ps, err := loadPromotions(ctx, q, b.ID, "p.status='active' AND "+notLimitSQL+" AND p.scope="+scopeSQL(scope)+" AND "+brandPromoSQL(brandID), "")
	if err != nil {
		return nil, err
	}
	out := []Promotion{}
	for _, p := range ps {
		p.setLive(time.Now(), b.location())
		if p.LiveNow {
			p.UsedCount, p.TotalQuota, p.PerPhoneLimit = 0, nil, nil // not for customers
			out = append(out, p)
		}
	}
	return out, nil
}

// scopeSQL is a promotion scope as an SQL literal; anything unknown is delivery.
func scopeSQL(scope string) string {
	if scope == "dine_in" {
		return "'dine_in'"
	}
	return "'delivery'"
}

type PromotionInput struct {
	// Scope is set when the promotion is created and never changes.
	Scope           string     `json:"scope"`
	Name            string     `json:"name"`
	NameEn          string     `json:"name_en"`
	Kind            string     `json:"kind"`
	Threshold       int64      `json:"threshold"`
	Value           int64      `json:"value"`
	MaxDiscount     *int64     `json:"max_discount"`
	AppliesDelivery bool       `json:"applies_delivery"`
	AppliesPickup   bool       `json:"applies_pickup"`
	StartsAt        *time.Time `json:"starts_at"`
	EndsAt          *time.Time `json:"ends_at"`
	DailyStart      string     `json:"daily_start"`
	DailyEnd        string     `json:"daily_end"`
	TotalQuota      *int       `json:"total_quota"`
	PerPhoneLimit   *int       `json:"per_phone_limit"`
	// ItemIDs are the dishes of a dish promotion.
	ItemIDs []int64 `json:"item_ids"`
	// BrandIDs are the brands it applies to; none = the main brand.
	BrandIDs []int64 `json:"brand_ids"`
}

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (in *PromotionInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.NameEn = strings.TrimSpace(in.NameEn)
	if in.DailyStart == "" {
		in.DailyStart = "00:00"
	}
	if in.DailyEnd == "" {
		in.DailyEnd = "00:00"
	}
	switch in.Scope {
	case "", "delivery":
		in.Scope = "delivery"
		// a purchase limit holds for delivery and pickup alike
		if promoRules[in.Kind].limit != "" {
			in.AppliesDelivery, in.AppliesPickup = true, true
		}
	case "dine_in":
		// Dine-in bills have no delivery fee, and guests rarely leave a phone.
		if in.Kind == "free_delivery" {
			return ErrValidation("dine-in promotions cannot be free delivery")
		}
		in.AppliesDelivery, in.AppliesPickup, in.PerPhoneLimit = false, false, nil
	default:
		return ErrValidation("scope must be delivery or dine_in")
	}
	switch {
	case in.Name == "" || len(in.Name) > 80 || len(in.NameEn) > 80:
		return ErrValidation("name is required")
	case in.Threshold < 0:
		return ErrValidation("threshold cannot be negative")
	case in.Scope == "delivery" && !in.AppliesDelivery && !in.AppliesPickup:
		return ErrValidation("choose at least one service type")
	case !hhmm.MatchString(in.DailyStart) || !hhmm.MatchString(in.DailyEnd):
		return ErrValidation("daily times must be HH:MM")
	case in.StartsAt != nil && in.EndsAt != nil && !in.EndsAt.After(*in.StartsAt):
		return ErrValidation("end must be after start")
	case in.TotalQuota != nil && *in.TotalQuota < 1, in.PerPhoneLimit != nil && *in.PerPhoneLimit < 1:
		return ErrValidation("limits must be at least 1")
	}
	r, ok := promoRules[in.Kind]
	if !ok {
		return ErrValidation("unknown kind of promotion")
	}
	if !r.dish && r.limit == "" {
		in.ItemIDs = nil
	}
	return r.check(in)
}

// SavePromotion creates (id 0, as a draft) or edits a promotion. Orders that
// already used it keep their own snapshot. A quota cannot drop below what is used.
func (a *App) SavePromotion(ctx context.Context, branchID, id int64, in PromotionInput, actor Actor) (int64, error) {
	if err := in.validate(); err != nil {
		return 0, err
	}
	if in.BrandIDs == nil {
		in.BrandIDs = []int64{}
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		// the brands: this store's own (none = the main brand)
		if len(in.BrandIDs) > 0 {
			slices.Sort(in.BrandIDs)
			in.BrandIDs = slices.Compact(in.BrandIDs)
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM brands WHERE branch_id=$1 AND id = ANY($2)`,
				branchID, in.BrandIDs).Scan(&n); err != nil {
				return err
			}
			if n != len(in.BrandIDs) {
				return Err(http.StatusUnprocessableEntity, "BRAND_UNKNOWN", "no such brand in this store")
			}
		}
		if id == 0 {
			if err := tx.QueryRow(ctx, `INSERT INTO promotions (branch_id, name, name_en, kind, threshold, value,
				max_discount, applies_delivery, applies_pickup, starts_at, ends_at, daily_start, daily_end,
				total_quota, per_phone_limit, scope, brand_ids) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
				branchID, in.Name, in.NameEn, in.Kind, in.Threshold, in.Value, in.MaxDiscount, in.AppliesDelivery,
				in.AppliesPickup, in.StartsAt, in.EndsAt, in.DailyStart, in.DailyEnd, in.TotalQuota,
				in.PerPhoneLimit, in.Scope, in.BrandIDs).Scan(&id); err != nil {
				return err
			}
		} else {
			var status, scope string
			var used int
			err := tx.QueryRow(ctx, `SELECT status, used_count, scope FROM promotions WHERE id=$1 AND branch_id=$2 FOR UPDATE`,
				id, branchID).Scan(&status, &used, &scope)
			if isNoRows(err) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			if scope != in.Scope {
				return ErrValidation("a promotion cannot move between delivery and dine-in; create a new one")
			}
			if status == "ended" {
				return ErrInvalidState("an ended promotion cannot be edited")
			}
			if in.TotalQuota != nil && *in.TotalQuota < used {
				return ErrValidation("the quota cannot be lower than what was already used")
			}
			if _, err := tx.Exec(ctx, `UPDATE promotions SET name=$3, name_en=$4, kind=$5, threshold=$6, value=$7,
				max_discount=$8, applies_delivery=$9, applies_pickup=$10, starts_at=$11, ends_at=$12, daily_start=$13,
				daily_end=$14, total_quota=$15, per_phone_limit=$16, brand_ids=$17, updated_at=now() WHERE id=$1 AND branch_id=$2`,
				id, branchID, in.Name, in.NameEn, in.Kind, in.Threshold, in.Value, in.MaxDiscount, in.AppliesDelivery,
				in.AppliesPickup, in.StartsAt, in.EndsAt, in.DailyStart, in.DailyEnd, in.TotalQuota,
				in.PerPhoneLimit, in.BrandIDs); err != nil {
				return err
			}
		}
		// the dishes of a dish promotion: this store's own, not archived
		if _, err := tx.Exec(ctx, `DELETE FROM promotion_items WHERE promotion_id=$1`, id); err != nil {
			return err
		}
		if len(in.ItemIDs) > 0 {
			tag, err := tx.Exec(ctx, `INSERT INTO promotion_items (promotion_id, item_id)
				SELECT $1, m.id FROM menu_items m WHERE m.id = ANY($2) AND m.branch_id = $3 AND NOT m.archived`,
				id, in.ItemIDs, branchID)
			if err != nil {
				return err
			}
			if int(tag.RowsAffected()) != len(in.ItemIDs) {
				return ErrValidation("a chosen dish is not on this store's menu")
			}
		}
		if err := audit(ctx, tx, branchID, 0, actor, "promotion.saved", map[string]any{"promotion_id": id, "input": in}); err != nil {
			return err
		}
		return branchChanged(ctx, tx, branchID)
	})
	return id, err
}

// SetPromotionStatus publishes, pauses, resumes or ends a promotion.
// draft -> active; active <-> paused; anything -> ended (final).
// PromotionBrands is the brands a promotion applies to, for checking that
// the person changing it works on all of them.
func (a *App) PromotionBrands(ctx context.Context, branchID, id int64) ([]int64, error) {
	var ids []int64
	err := a.DB.QueryRow(ctx, `SELECT brand_ids FROM promotions WHERE id=$1 AND branch_id=$2`, id, branchID).Scan(&ids)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return ids, err
}

func (a *App) SetPromotionStatus(ctx context.Context, branchID, id int64, to string, actor Actor) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var from string
		err := tx.QueryRow(ctx, `SELECT status FROM promotions WHERE id=$1 AND branch_id=$2 FOR UPDATE`, id, branchID).Scan(&from)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if from == to {
			return nil
		}
		ok := false
		switch to {
		case "active":
			ok = from == "draft" || from == "paused"
		case "paused":
			ok = from == "active"
		case "ended":
			ok = true
		default:
			return ErrValidation("status must be active, paused or ended")
		}
		if !ok {
			return ErrInvalidState("cannot change a " + from + " promotion to " + to)
		}
		if _, err := tx.Exec(ctx, `UPDATE promotions SET status=$2, updated_at=now() WHERE id=$1`, id, to); err != nil {
			return err
		}
		if err := audit(ctx, tx, branchID, 0, actor, "promotion."+to, map[string]any{"promotion_id": id, "from": from}); err != nil {
			return err
		}
		return branchChanged(ctx, tx, branchID)
	})
}

// PromotionTrial is one row of a trial calculation.
type PromotionTrial struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Discount int64  `json:"discount"`
	Reason   string `json:"reason"` // "" when it would apply
	Wins     bool   `json:"wins"`
}

type TrialInput struct {
	ServiceType string     `json:"service_type"`
	Subtotal    int64      `json:"subtotal"`
	DeliveryFee int64      `json:"delivery_fee"`
	At          *time.Time `json:"at"` // defaults to now
	// BrandID tries the promotions of one brand; 0 = the main brand.
	BrandID int64 `json:"brand_id"`
}

type TrialResult struct {
	Rows   []PromotionTrial `json:"rows"`
	Totals Totals           `json:"totals"`
}

// TrialPromotions shows the merchant what a cart of the given size would get.
// Drafts and paused promotions are evaluated as if they were active, so an
// offer can be checked before it is published; ended ones are left out.
func (a *App) TrialPromotions(ctx context.Context, branchID int64, in TrialInput) (*TrialResult, error) {
	if in.ServiceType != "delivery" && in.ServiceType != "pickup" && in.ServiceType != "dine_in" {
		return nil, ErrValidation("service_type must be delivery, pickup or dine_in")
	}
	if in.Subtotal < 0 || in.DeliveryFee < 0 {
		return nil, ErrValidation("amounts cannot be negative")
	}
	if in.ServiceType != "delivery" {
		in.DeliveryFee = 0
	}
	scope := "delivery"
	if in.ServiceType == "dine_in" {
		scope = "dine_in"
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	// an amount of dishes says nothing of which dishes: the trial is for
	// promotions on the whole order
	ps, err := loadPromotions(ctx, a.DB, branchID, "p.status <> 'ended' AND p.kind NOT IN ('dish_percent','dish_second') AND "+notLimitSQL+" AND p.scope="+
		scopeSQL(scope)+" AND "+brandPromoSQL(in.BrandID), "")
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if in.At != nil {
		now = *in.At
	}
	res := &TrialResult{Rows: []PromotionTrial{}}
	status := make([]string, len(ps))
	for i := range ps {
		status[i] = ps[i].Status
		ps[i].Status = "active"
	}
	best, d := bestPromotion(ps, now, b.location(), in.ServiceType, in.Subtotal, in.DeliveryFee)
	for i := range ps {
		p := &ps[i]
		row := PromotionTrial{ID: p.ID, Name: p.Name, Status: status[i],
			Reason: p.why(now, b.location(), in.ServiceType, in.Subtotal)}
		if row.Reason == "" {
			row.Discount = p.discount(in.Subtotal, in.DeliveryFee).total()
		}
		row.Wins = best != nil && best.ID == p.ID
		res.Rows = append(res.Rows, row)
	}
	cfg := b.taxConfig()
	if scope == "dine_in" {
		cfg = b.dineInTax()
	}
	res.Totals = ComputeTotals(in.Subtotal, in.DeliveryFee, cfg, d)
	return res, nil
}

// claimPromotion takes one use of the promotion for a new order. The UPDATE
// locks the promotion row, so concurrent orders are counted one at a time and
// the quota and the per-customer limit cannot be exceeded.
func claimPromotion(ctx context.Context, tx pgx.Tx, branchID, promoID, orderID int64, phone string, amount int64) error {
	changed := Err(http.StatusConflict, "QUOTE_CHANGED", "the promotion is no longer available, please confirm the new amount")
	var limit *int
	err := tx.QueryRow(ctx, `UPDATE promotions SET used_count = used_count + 1
		WHERE id=$1 AND branch_id=$2 AND status='active' AND (total_quota IS NULL OR used_count < total_quota)
		RETURNING per_phone_limit`, promoID, branchID).Scan(&limit)
	if isNoRows(err) {
		return changed
	} else if err != nil {
		return err
	}
	if limit != nil {
		var used int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM promotion_redemptions
			WHERE promotion_id=$1 AND phone=$2 AND status='applied'`, promoID, phone).Scan(&used); err != nil {
			return err
		}
		if used >= *limit {
			return changed
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO promotion_redemptions (promotion_id, branch_id, order_id, phone, amount)
		VALUES ($1,$2,$3,$4,$5)`, promoID, branchID, orderID, phone, amount)
	return err
}

// releasePromotion gives the use back when an order is rejected, cancelled or
// times out. It is a no-op for orders without a promotion or already released.
func releasePromotion(ctx context.Context, tx pgx.Tx, orderID int64) error {
	var promoID int64
	err := tx.QueryRow(ctx, `UPDATE promotion_redemptions SET status='released', released_at=now()
		WHERE order_id=$1 AND status='applied' RETURNING promotion_id`, orderID).Scan(&promoID)
	if isNoRows(err) {
		return nil
	} else if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE promotions SET used_count = used_count - 1 WHERE id=$1 AND used_count > 0`, promoID)
	return err
}

// brandPromoSQL limits promotions (aliased p) to the ones that apply to a
// brand (docs/MULTI_BRAND_DESIGN.md §10).
// brandID 0 is the store's main brand.
func brandPromoSQL(brandID int64) string {
	if brandID == 0 {
		return "EXISTS (SELECT 1 FROM brands br WHERE br.branch_id = p.branch_id AND br.is_main AND br.id = ANY(p.brand_ids))"
	}
	return strconv.FormatInt(brandID, 10) + " = ANY(p.brand_ids)"
}
