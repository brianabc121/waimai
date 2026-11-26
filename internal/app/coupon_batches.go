package app

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Batches of coupons other than the welcome-back gift: coupons a store lets
// customers claim on its page, and platform coupons, given to new customers
// or by hand (docs/COUPON_DESIGN.md §3.5, §7).

// BatchStats says what became of a batch's coupons.
type BatchStats struct {
	Issued int   `json:"issued"`
	Used   int   `json:"used"`
	Open   int   `json:"open"`
	Spent  int64 `json:"spent"` // what used coupons took off, on orders that were not cancelled
}

type BatchRow struct {
	CouponBatch
	Stats BatchStats `json:"stats"`
	// StoreNames names the stores in Scope (platform batches).
	StoreNames []string `json:"store_names,omitempty"`
}

type CouponBatchInput struct {
	Name        string  `json:"name"`
	NameEn      string  `json:"name_en"`
	Kind        string  `json:"kind"`
	Value       int64   `json:"value"`
	Threshold   int64   `json:"threshold"`
	ValidDays   int     `json:"valid_days"`
	TotalQuota  *int    `json:"total_quota"`
	PerCustomer *int    `json:"per_customer"`
	Budget      *int64  `json:"budget"`
	Currency    string  `json:"currency"`
	Source      string  `json:"source"`
	Scope       string  `json:"scope"`
	ScopeIDs    []int64 `json:"scope_branch_ids"`
	// CountryCode is where a platform batch works (required); Province keeps
	// it to one province, "" being the whole country.
	CountryCode string `json:"country_code"`
	Province    string `json:"province"`
	// BrandID is the brand a store batch gives coupons of (0 = the main
	// brand); set when it is created.
	BrandID int64 `json:"brand_id"`

	country string // filled from the stores in that country
}

func (in *CouponBatchInput) validate(platform bool) error {
	in.Name, in.NameEn = strings.TrimSpace(in.Name), strings.TrimSpace(in.NameEn)
	if len(in.Name) > 60 || len(in.NameEn) > 60 {
		return ErrValidation("the name is too long")
	}
	if in.ValidDays < 1 || in.ValidDays > 365 {
		return ErrValidation("the coupon must last 1 to 365 days")
	}
	if in.TotalQuota != nil && *in.TotalQuota < 1 || in.PerCustomer != nil && *in.PerCustomer < 1 {
		return ErrValidation("limits must be at least 1")
	}
	if in.Threshold < 0 || in.Value < 0 {
		return ErrValidation("amounts cannot be negative")
	}
	if platform {
		in.Kind, in.Threshold, in.TotalQuota, in.PerCustomer = CouponPlatformFree, 0, nil, nil
		if in.Value <= 0 {
			return ErrValidation("the amount off must be greater than 0")
		}
		if in.Name == "" {
			return ErrValidation("name is required")
		}
		if in.Source != "new_customer" && in.Source != "manual" {
			return ErrValidation("source must be new_customer or manual")
		}
		in.CountryCode, in.Province = strings.ToUpper(strings.TrimSpace(in.CountryCode)), strings.TrimSpace(in.Province)
		if len(in.CountryCode) != 2 {
			return ErrValidation("choose the country")
		}
		if in.Budget != nil && *in.Budget < in.Value {
			return ErrValidation("the budget must cover at least one coupon")
		}
		switch in.Scope {
		case "", "all":
			in.Scope, in.ScopeIDs = "all", []int64{}
		case "only", "except":
			if len(in.ScopeIDs) == 0 {
				return ErrValidation("choose the stores")
			}
		default:
			return ErrValidation("scope must be all, only or except")
		}
		return nil
	}
	in.Budget, in.Source, in.Scope, in.ScopeIDs = nil, "claim", "all", []int64{}
	if in.PerCustomer == nil {
		one := 1
		in.PerCustomer = &one
	}
	r := RepurchaseInput{Kind: in.Kind, Value: in.Value, Threshold: in.Threshold, ValidDays: 1}
	return r.validate()
}

const batchStatsSQL = `SELECT b.id, count(c.id),
		count(c.id) FILTER (WHERE c.status='used'),
		count(c.id) FILTER (WHERE c.status='unused' AND c.expires_at > now()),
		COALESCE(sum(d.amount) FILTER (WHERE c.status='used'), 0)
	FROM coupon_batches b LEFT JOIN customer_coupons c ON c.batch_id = b.id
	LEFT JOIN order_discounts d ON d.coupon_id = c.id AND c.status='used'
	WHERE b.id = ANY($1) GROUP BY b.id`

func listBatches(ctx context.Context, q Querier, where string, args ...any) ([]BatchRow, error) {
	rows, err := q.Query(ctx, `SELECT `+batchCols+` FROM coupon_batches WHERE `+where+` ORDER BY status='ended', id DESC`, args...)
	if err != nil {
		return nil, err
	}
	out := []BatchRow{}
	byID := map[int64]*BatchRow{}
	ids := []int64{}
	for rows.Next() {
		var r BatchRow
		if err := rows.Scan(r.scan()...); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
		ids = append(ids, r.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	rows, err = q.Query(ctx, batchStatsSQL, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var s BatchStats
		if err := rows.Scan(&id, &s.Issued, &s.Used, &s.Open, &s.Spent); err != nil {
			return nil, err
		}
		byID[id].Stats = s
	}
	return out, rows.Err()
}

// ---- issuing ----

// issueCoupon gives one coupon of a batch to a customer. It takes the batch's
// quota and budget atomically and keeps to its per-customer limit (platform
// coupons: one each). It returns 0 when nothing may be given.
func issueCoupon(ctx context.Context, tx pgx.Tx, batchID, customerID int64) (int64, error) {
	// one customer at a time, so two quick taps do not both pass the limit
	if _, err := tx.Exec(ctx, `SELECT 1 FROM customers WHERE id=$1 FOR UPDATE`, customerID); err != nil {
		return 0, err
	}
	var bt CouponBatch
	err := tx.QueryRow(ctx, `SELECT `+batchCols+` FROM coupon_batches WHERE id=$1`, batchID).Scan(bt.scan()...)
	if isNoRows(err) {
		return 0, ErrNotFound
	} else if err != nil {
		return 0, err
	}
	limit := 1
	if bt.PerCustomer != nil {
		limit = *bt.PerCustomer
	}
	var had int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM customer_coupons WHERE batch_id=$1 AND customer_id=$2`,
		batchID, customerID).Scan(&had); err != nil {
		return 0, err
	}
	if had >= limit {
		return 0, nil
	}
	err = tx.QueryRow(ctx, `UPDATE coupon_batches SET issued_count = issued_count + 1 WHERE id=$1 AND status='active'
		AND (total_quota IS NULL OR issued_count < total_quota)
		AND (budget IS NULL OR (issued_count + 1) * value <= budget) RETURNING issued_count`, batchID).Scan(new(int))
	if isNoRows(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	now := time.Now()
	exp := now.Add(time.Duration(bt.ValidDays) * 24 * time.Hour)
	if bt.BranchID != nil {
		b, err := scanBranch(tx.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, *bt.BranchID))
		if err != nil {
			return 0, err
		}
		exp = expiryFor(now, b.location(), bt.ValidDays)
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO customer_coupons (batch_id, customer_id, issuer, kind, branch_id, currency, name,
		name_en, value, threshold, scope, scope_branch_ids, source, expires_at, country_code, country, province)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
		bt.ID, customerID, bt.Issuer, bt.Kind, bt.BranchID, bt.Currency, bt.Name, bt.NameEn, bt.Value, bt.Threshold,
		bt.Scope, bt.ScopeIDs, bt.Source, exp, bt.CountryCode, bt.Country, bt.Province).Scan(&id)
	return id, err
}

// giftNewCustomer gives a customer who just signed up every running
// new-customer platform coupon of the country of their phone number.
func giftNewCustomer(ctx context.Context, tx pgx.Tx, customerID int64) error {
	var phone string
	if err := tx.QueryRow(ctx, `SELECT phone FROM customers WHERE id=$1`, customerID).Scan(&phone); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id, value, currency, country_code FROM coupon_batches
		WHERE issuer='platform' AND source='new_customer' AND status='active' ORDER BY id`)
	if err != nil {
		return err
	}
	type b struct {
		id, value int64
		cur       string
	}
	var list []b
	for rows.Next() {
		var x b
		var cc string
		if err := rows.Scan(&x.id, &x.value, &x.cur, &cc); err != nil {
			rows.Close()
			return err
		}
		if phoneIn(phone, cc) {
			list = append(list, x)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, x := range list {
		id, err := issueCoupon(ctx, tx, x.id, customerID)
		if err != nil {
			return err
		}
		if id != 0 {
			if err := tell(ctx, tx, customerID, Message{Kind: "wallet", Title: "新人礼：" + fmtMoney(x.value, x.cur) + "无门槛券已放入你的账户",
				TitleEn: "Welcome gift: " + fmtMoney(x.value, x.cur) + " off your first orders",
				Body:    "全平台通用，下单时自动使用", BodyEn: "Any store; it applies by itself at checkout", Link: "/me/coupons",
				Key: "welcome_gift", Args: map[string]string{"value": strconv.FormatInt(x.value, 10), "currency": x.cur}}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- the store's claimable coupons ----

func (a *App) ListStoreBatches(ctx context.Context, branchID int64) ([]BatchRow, error) {
	return listBatches(ctx, a.DB, `branch_id=$1 AND source='claim'`, branchID)
}

func (a *App) SaveStoreBatch(ctx context.Context, branchID, id int64, in CouponBatchInput, actor Actor) (int64, error) {
	if err := in.validate(false); err != nil {
		return 0, err
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return 0, err
	}
	brand, err := loadBrand(ctx, a.DB, branchID, in.BrandID)
	if err == ErrNotFound {
		return 0, Err(http.StatusUnprocessableEntity, "BRAND_UNKNOWN", "no such brand in this store")
	} else if err != nil {
		return 0, err
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		if id == 0 {
			if err := tx.QueryRow(ctx, `INSERT INTO coupon_batches (issuer, branch_id, kind, source, name, name_en, currency,
				value, threshold, valid_days, total_quota, per_customer, brand_id) VALUES ('store',$1,$2,'claim',$3,$4,$5,$6,$7,$8,$9,$10,$11)
				RETURNING id`, branchID, in.Kind, in.Name, in.NameEn, b.Currency, in.Value, in.Threshold, in.ValidDays,
				in.TotalQuota, in.PerCustomer, brand.ID).Scan(&id); err != nil {
				return err
			}
		} else {
			var status string
			var issued int
			err := tx.QueryRow(ctx, `SELECT status, issued_count FROM coupon_batches WHERE id=$1 AND branch_id=$2
				AND source='claim' FOR UPDATE`, id, branchID).Scan(&status, &issued)
			if isNoRows(err) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			if status == "ended" {
				return ErrInvalidState("an ended batch cannot be edited")
			}
			if in.TotalQuota != nil && *in.TotalQuota < issued {
				return ErrValidation("the number cannot be lower than what was already given out")
			}
			if _, err := tx.Exec(ctx, `UPDATE coupon_batches SET kind=$2, name=$3, name_en=$4, value=$5, threshold=$6,
				valid_days=$7, total_quota=$8, per_customer=$9, updated_at=now() WHERE id=$1`, id, in.Kind, in.Name,
				in.NameEn, in.Value, in.Threshold, in.ValidDays, in.TotalQuota, in.PerCustomer); err != nil {
				return err
			}
		}
		if err := audit(ctx, tx, branchID, 0, actor, "coupon.batch_saved", map[string]any{"batch_id": id, "input": in}); err != nil {
			return err
		}
		return branchChanged(ctx, tx, branchID)
	})
	return id, err
}

// setBatchStatus pauses, resumes or ends a batch. Coupons already given stay usable.
func setBatchStatus(ctx context.Context, tx pgx.Tx, where string, id int64, to string, args ...any) (string, error) {
	var from string
	err := tx.QueryRow(ctx, `SELECT status FROM coupon_batches WHERE id=$1 AND `+where+` FOR UPDATE`,
		append([]any{id}, args...)...).Scan(&from)
	if isNoRows(err) {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	switch {
	case from == to:
		return from, nil
	case to != "active" && to != "paused" && to != "ended":
		return "", ErrValidation("status must be active, paused or ended")
	case from == "ended":
		return "", ErrInvalidState("an ended batch cannot be changed")
	}
	_, err = tx.Exec(ctx, `UPDATE coupon_batches SET status=$2, updated_at=now() WHERE id=$1`, id, to)
	return from, err
}

func (a *App) SetStoreBatchStatus(ctx context.Context, branchID, id int64, to string, actor Actor) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		from, err := setBatchStatus(ctx, tx, `branch_id=$2 AND source='claim'`, id, to, branchID)
		if err != nil || from == to {
			return err
		}
		if err := audit(ctx, tx, branchID, 0, actor, "coupon.batch_"+to, map[string]any{"batch_id": id, "from": from}); err != nil {
			return err
		}
		return branchChanged(ctx, tx, branchID)
	})
}

// ClaimableCoupon is a batch on the store page, with whether this customer
// may still claim one.
type ClaimableCoupon struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	NameEn    string `json:"name_en"`
	Value     int64  `json:"value"`
	Threshold int64  `json:"threshold"`
	ValidDays int    `json:"valid_days"`
	// Claimed says the customer reached their limit; Gone that none are left.
	Claimed bool `json:"claimed"`
	Gone    bool `json:"gone"`
}

// StoreCoupons lists what a store's page offers to claim. customerID 0 is
// someone not logged in.
func (a *App) StoreCoupons(ctx context.Context, slug string, customerID int64) ([]ClaimableCoupon, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT id, kind, name, name_en, value, threshold, valid_days,
			(SELECT count(*) FROM customer_coupons c WHERE c.batch_id = b.id AND c.customer_id = $2) >= COALESCE(per_customer, 1),
			total_quota IS NOT NULL AND issued_count >= total_quota
		FROM coupon_batches b WHERE branch_id=$1 AND source='claim' AND status='active'
		ORDER BY value DESC, id`, b.ID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ClaimableCoupon{}
	for rows.Next() {
		var c ClaimableCoupon
		if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &c.NameEn, &c.Value, &c.Threshold, &c.ValidDays, &c.Claimed, &c.Gone); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClaimCoupon gives the customer one coupon of a store's batch.
func (a *App) ClaimCoupon(ctx context.Context, slug string, batchID, customerID int64) (*CustomerCoupon, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	var id int64
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM coupon_batches WHERE id=$1 AND branch_id=$2 AND source='claim')`,
			batchID, b.ID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		id, err = issueCoupon(ctx, tx, batchID, customerID)
		if err != nil {
			return err
		}
		if id == 0 {
			return Err(http.StatusConflict, "COUPON_UNAVAILABLE", "this coupon is no longer available to you")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	cs, err := queryCoupons(ctx, a.DB, `c.id=$1`, id)
	if err != nil || len(cs) == 0 {
		return nil, err
	}
	return &cs[0], nil
}

// ---- platform coupons ----

func (a *App) ListPlatformBatches(ctx context.Context) ([]BatchRow, error) {
	list, err := listBatches(ctx, a.DB, `issuer='platform'`)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if len(list[i].ScopeIDs) == 0 {
			continue
		}
		rows, err := a.DB.Query(ctx, `SELECT name FROM branches WHERE id = ANY($1) ORDER BY id`, list[i].ScopeIDs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return nil, err
			}
			list[i].StoreNames = append(list[i].StoreNames, n)
		}
		rows.Close()
	}
	return list, nil
}

func (a *App) SavePlatformBatch(ctx context.Context, actor AdminActor, id int64, in CouponBatchInput) (int64, error) {
	if err := in.validate(true); err != nil {
		return 0, err
	}
	// the money of the country is the money of its stores
	err := a.DB.QueryRow(ctx, `SELECT currency, max(country) FROM branches WHERE deleted_at IS NULL AND country_code=$1
		GROUP BY currency ORDER BY count(*) DESC LIMIT 1`, in.CountryCode).Scan(&in.Currency, &in.country)
	if isNoRows(err) {
		return 0, ErrValidation("there is no store in that country yet")
	} else if err != nil {
		return 0, err
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		if id == 0 {
			if err := tx.QueryRow(ctx, `INSERT INTO coupon_batches (issuer, kind, source, name, name_en, currency, value,
				valid_days, budget, scope, scope_branch_ids, country_code, country, province)
				VALUES ('platform','platform_free',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
				RETURNING id`, in.Source, in.Name, in.NameEn, in.Currency, in.Value, in.ValidDays, in.Budget, in.Scope,
				in.ScopeIDs, in.CountryCode, in.country, in.Province).Scan(&id); err != nil {
				return err
			}
		} else {
			var status string
			var issued int
			var value int64
			err := tx.QueryRow(ctx, `SELECT status, issued_count, value FROM coupon_batches WHERE id=$1 AND issuer='platform'
				FOR UPDATE`, id).Scan(&status, &issued, &value)
			if isNoRows(err) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
			if status == "ended" {
				return ErrInvalidState("an ended batch cannot be edited")
			}
			if in.Budget != nil && *in.Budget < int64(issued)*in.Value {
				return ErrValidation("the budget cannot be lower than what was already given out")
			}
			if _, err := tx.Exec(ctx, `UPDATE coupon_batches SET source=$2, name=$3, name_en=$4, currency=$5, value=$6,
				valid_days=$7, budget=$8, scope=$9, scope_branch_ids=$10, country_code=$11, country=$12, province=$13,
				updated_at=now() WHERE id=$1`, id, in.Source, in.Name, in.NameEn, in.Currency, in.Value, in.ValidDays,
				in.Budget, in.Scope, in.ScopeIDs, in.CountryCode, in.country, in.Province); err != nil {
				return err
			}
		}
		return adminAudit(ctx, tx, actor, "coupon.platform_saved", "coupon_batch", id, in)
	})
	return id, err
}

func (a *App) SetPlatformBatchStatus(ctx context.Context, actor AdminActor, id int64, to string) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		from, err := setBatchStatus(ctx, tx, `issuer='platform'`, id, to)
		if err != nil || from == to {
			return err
		}
		return adminAudit(ctx, tx, actor, "coupon.platform_"+to, "coupon_batch", id, map[string]any{"from": from})
	})
}

type IssueInput struct {
	// All gives one to every customer; otherwise Phones lists who gets one.
	All    bool     `json:"all"`
	Phones []string `json:"phones"`
}

type IssueResult struct {
	Given    int      `json:"given"`
	Skipped  int      `json:"skipped"` // already had one, or the budget ran out
	NotFound []string `json:"not_found"`
}

// IssuePlatform hands out a manual platform batch.
func (a *App) IssuePlatform(ctx context.Context, actor AdminActor, id int64, in IssueInput) (*IssueResult, error) {
	res := &IssueResult{NotFound: []string{}}
	if !in.All && len(in.Phones) == 0 {
		return nil, ErrValidation("give a list of phone numbers, or choose everyone")
	}
	if len(in.Phones) > 5000 {
		return nil, ErrValidation("at most 5000 numbers at a time")
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var source, status, name, cc, where string
		var value int64
		var cur string
		err := tx.QueryRow(ctx, `SELECT source, status, name, value, currency, country_code, country || CASE WHEN province <> ''
			THEN ' ' || province ELSE '' END FROM coupon_batches WHERE id=$1 AND issuer='platform'`, id).
			Scan(&source, &status, &name, &value, &cur, &cc, &where)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "active" {
			return ErrInvalidState("start the batch before giving it out")
		}
		var ids []int64
		if in.All {
			// everyone with a phone number of the batch's country
			dial, ok := dialCodes[cc]
			if !ok {
				return ErrValidation("the batch has no country")
			}
			rows, err := tx.Query(ctx, `SELECT id FROM customers WHERE phone LIKE $1 ORDER BY id`, "+"+dial+"%")
			if err != nil {
				return err
			}
			for rows.Next() {
				var x int64
				if err := rows.Scan(&x); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, x)
			}
			rows.Close()
		} else {
			seen := map[string]bool{}
			for _, p := range in.Phones {
				p = strings.TrimSpace(p)
				if p == "" || seen[p] {
					continue
				}
				seen[p] = true
				var x int64
				err := tx.QueryRow(ctx, `SELECT id FROM customers WHERE phone=$1 OR phone = '+' || $1`, strings.TrimPrefix(p, "+")).Scan(&x)
				if isNoRows(err) {
					res.NotFound = append(res.NotFound, p)
					continue
				} else if err != nil {
					return err
				}
				ids = append(ids, x)
			}
		}
		for _, cid := range ids {
			cp, err := issueCoupon(ctx, tx, id, cid)
			if err != nil {
				return err
			}
			if cp == 0 {
				res.Skipped++
				continue
			}
			res.Given++
			if err := tell(ctx, tx, cid, Message{Kind: "wallet", Title: "平台送你一张" + fmtMoney(value, cur) + "无门槛券",
				TitleEn: "A " + fmtMoney(value, cur) + " coupon from us", Body: name + " · 限" + where + "店铺使用", BodyEn: name + " · stores in " + where,
				Link: "/me/coupons", Key: "platform_coupon", Args: map[string]string{"value": strconv.FormatInt(value, 10), "currency": cur}}); err != nil {
				return err
			}
		}
		return adminAudit(ctx, tx, actor, "coupon.platform_issued", "coupon_batch", id,
			map[string]any{"all": in.All, "given": res.Given, "skipped": res.Skipped})
	})
	return res, err
}

// PlatformCouponSummary is the top of the platform coupon page, one row per
// currency, since amounts of different money do not add up.
type PlatformCouponSummary struct {
	Currency    string `json:"currency"`
	MonthSpent  int64  `json:"month_spent"` // platform coupons on completed orders this month
	MonthOrders int    `json:"month_orders"`
	OpenCount   int    `json:"open_count"` // given, not yet used nor expired
	OpenValue   int64  `json:"open_value"`
}

func (a *App) PlatformCouponSummary(ctx context.Context) ([]PlatformCouponSummary, error) {
	rows, err := a.DB.Query(ctx, `WITH spent AS (
			SELECT o.currency, sum(o.platform_subsidy) AS amt, count(*) AS n FROM orders o
			WHERE o.platform_subsidy > 0 AND o.fulfillment_status IN ('delivered','picked_up')
				AND o.completed_at >= date_trunc('month', now()) GROUP BY o.currency),
		open AS (
			SELECT currency, count(*) AS n, sum(value) AS amt FROM customer_coupons
			WHERE issuer='platform' AND status='unused' AND expires_at > now() GROUP BY currency)
		SELECT COALESCE(s.currency, o.currency), COALESCE(s.amt, 0), COALESCE(s.n, 0), COALESCE(o.n, 0), COALESCE(o.amt, 0)
		FROM spent s FULL JOIN open o ON o.currency = s.currency ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlatformCouponSummary{}
	for rows.Next() {
		var x PlatformCouponSummary
		if err := rows.Scan(&x.Currency, &x.MonthSpent, &x.MonthOrders, &x.OpenCount, &x.OpenValue); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// CouponCountry is a country with stores, for choosing where a platform
// batch works.
type CouponCountry struct {
	CountryCode string   `json:"country_code"`
	Country     string   `json:"country"`
	Currency    string   `json:"currency"`
	Stores      int      `json:"stores"`
	Provinces   []string `json:"provinces"`
}

func (a *App) CouponCountries(ctx context.Context) ([]CouponCountry, error) {
	rows, err := a.DB.Query(ctx, `SELECT country_code, max(country), mode() WITHIN GROUP (ORDER BY currency), count(*),
			COALESCE(array_agg(DISTINCT province ORDER BY province) FILTER (WHERE province <> ''), '{}')
		FROM branches WHERE deleted_at IS NULL AND country_code <> ''
		GROUP BY country_code ORDER BY count(*) DESC, country_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CouponCountry{}
	for rows.Next() {
		var c CouponCountry
		if err := rows.Scan(&c.CountryCode, &c.Country, &c.Currency, &c.Stores, &c.Provinces); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
