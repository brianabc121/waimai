package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Stored value, "top up X, get Y free" (docs/STORED_VALUE_DESIGN.md). The
// customer pays the store at the counter and the staff confirm it; the money
// becomes stored principal, the store's gift stored bonus, both in the
// double-entry books of wallet.go. Spending takes them in proportion
// (storedTake), refunds give them back the same way, and a customer may have
// what is left of their principal paid back (§8.3).
//
// Like a store balance it is a way to pay, never a discount: the pricing
// engine does not see it (§8.4).

const (
	PermStoredTopup   = "stored.topup"
	PermStoredCashout = "stored.cashout"

	topupTTL      = 30 * time.Minute
	maxTiers      = 6
	storedJournal = "topup"
)

// ---- the platform's switch per country (§9, Q9) ----

type StoredCountry struct {
	CountryCode string `json:"country_code"`
	Enabled     bool   `json:"enabled"`
	Cap         int64  `json:"cap"`
	Note        string `json:"note"`
	// what the platform sees of the country's stores
	Currency  string `json:"currency"`
	Stores    int    `json:"stores"`
	Principal int64  `json:"principal"`
	Bonus     int64  `json:"bonus"`
}

// storedRules is whether stored value is on for a store's country and the
// most one customer may hold there.
func storedRules(ctx context.Context, q Querier, b *Branch) (bool, int64, error) {
	var on bool
	var cap int64
	err := q.QueryRow(ctx, `SELECT enabled, cap FROM stored_countries WHERE country_code = upper($1)`, b.CountryCode).Scan(&on, &cap)
	if isNoRows(err) {
		return false, 0, nil
	}
	return on, cap, err
}

func errStoredOff() error {
	return Err(http.StatusConflict, "STORED_OFF", "stored value is not available in this store's country")
}

func (a *App) StoredCountries(ctx context.Context) ([]StoredCountry, error) {
	rows, err := a.DB.Query(ctx, `SELECT c.country_code, c.enabled, c.cap, c.note,
			COALESCE((SELECT min(b.currency) FROM branches b WHERE upper(b.country_code) = c.country_code AND b.deleted_at IS NULL), ''),
			(SELECT count(DISTINCT t.branch_id) FROM topups t JOIN branches b ON b.id = t.branch_id
				WHERE upper(b.country_code) = c.country_code AND t.status = 'paid')::int,
			COALESCE((SELECT sum(w.balance) FROM wallet_accounts w JOIN branches b ON b.id = w.branch_id
				WHERE upper(b.country_code) = c.country_code AND w.kind = 'stored_principal'), 0),
			COALESCE((SELECT sum(w.balance) FROM wallet_accounts w JOIN branches b ON b.id = w.branch_id
				WHERE upper(b.country_code) = c.country_code AND w.kind = 'stored_bonus'), 0)
		FROM stored_countries c ORDER BY c.enabled DESC, c.country_code`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (StoredCountry, error) {
		var c StoredCountry
		return c, r.Scan(&c.CountryCode, &c.Enabled, &c.Cap, &c.Note, &c.Currency, &c.Stores, &c.Principal, &c.Bonus)
	})
}

func (a *App) SaveStoredCountry(ctx context.Context, actor AdminActor, in StoredCountry) error {
	in.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
	if len(in.CountryCode) != 2 || in.Cap < 0 || len(in.Note) > 200 {
		return ErrValidation("a two-letter country, a cap of 0 or more and a short note")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO stored_countries (country_code, enabled, cap, note) VALUES ($1,$2,$3,$4)
			ON CONFLICT (country_code) DO UPDATE SET enabled=$2, cap=$3, note=$4, updated_at=now()`,
			in.CountryCode, in.Enabled, in.Cap, in.Note); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "stored.country", "country", 0, in)
	})
}

// ---- campaigns (§4) ----

type StoredTier struct {
	ID          int64  `json:"id"`
	Pay         int64  `json:"pay"`
	Bonus       int64  `json:"bonus"`
	CouponKind  string `json:"coupon_kind"`
	CouponValue int64  `json:"coupon_value"`
	CouponMin   int64  `json:"coupon_min"`
	CouponDays  int    `json:"coupon_days"`
	CouponQty   int    `json:"coupon_qty"`
	Label       string `json:"label"`
	PerCustomer *int   `json:"per_customer"`
	FirstOnly   bool   `json:"first_only"`
	Quota       *int   `json:"quota"`
	// Taken is how many top-ups used it; for a customer, Mine is theirs and
	// Closed why they may not take it now ("first_only", "per_customer",
	// "quota").
	Taken  int    `json:"taken"`
	Mine   int    `json:"mine,omitempty"`
	Closed string `json:"closed,omitempty"`
}

type StoredCampaign struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	NameEn    string       `json:"name_en"`
	StartsAt  *time.Time   `json:"starts_at"`
	EndsAt    *time.Time   `json:"ends_at"`
	Status    string       `json:"status"` // active, paused
	State     string       `json:"state"`  // running, upcoming, ended, paused
	CustomOK  bool         `json:"custom_ok"`
	CustomMin int64        `json:"custom_min"`
	BrandIDs  []int64      `json:"brand_ids"`
	Note      string       `json:"note"`
	Tiers     []StoredTier `json:"tiers"`
	// what it brought in
	Count int   `json:"count"`
	Paid  int64 `json:"paid"`
	Given int64 `json:"given"`
}

const campaignCols = `c.id, c.name, COALESCE(c.names->>'en', ''), c.starts_at, c.ends_at, c.status,
	CASE WHEN c.status = 'paused' THEN 'paused' WHEN c.ends_at IS NOT NULL AND c.ends_at <= now() THEN 'ended'
		WHEN c.starts_at IS NOT NULL AND c.starts_at > now() THEN 'upcoming' ELSE 'running' END,
	c.custom_ok, c.custom_min, c.brand_ids, c.note,
	(SELECT count(*) FROM topups t WHERE t.campaign_id = c.id AND t.status = 'paid')::int,
	COALESCE((SELECT sum(t.pay) FROM topups t WHERE t.campaign_id = c.id AND t.status = 'paid'), 0),
	COALESCE((SELECT sum(t.bonus) FROM topups t WHERE t.campaign_id = c.id AND t.status = 'paid'), 0)`

func scanCampaign(r pgx.CollectableRow) (*StoredCampaign, error) {
	var c StoredCampaign
	err := r.Scan(&c.ID, &c.Name, &c.NameEn, &c.StartsAt, &c.EndsAt, &c.Status, &c.State, &c.CustomOK, &c.CustomMin,
		&c.BrandIDs, &c.Note, &c.Count, &c.Paid, &c.Given)
	if c.BrandIDs == nil {
		c.BrandIDs = []int64{}
	}
	return &c, err
}

func loadCampaigns(ctx context.Context, q Querier, where string, args ...any) ([]*StoredCampaign, error) {
	rows, err := q.Query(ctx, `SELECT `+campaignCols+` FROM stored_campaigns c WHERE `+where+` ORDER BY c.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	cs, err := pgx.CollectRows(rows, scanCampaign)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		trows, err := q.Query(ctx, `SELECT t.id, t.pay, t.bonus, t.coupon_kind, t.coupon_value, t.coupon_min, t.coupon_days,
				t.coupon_qty, t.label, t.per_customer, t.first_only, t.quota,
				(SELECT count(*) FROM topups x WHERE x.tier_id = t.id AND x.status = 'paid')::int
			FROM stored_tiers t WHERE t.campaign_id=$1 AND NOT t.removed ORDER BY t.sort, t.id`, c.ID)
		if err != nil {
			return nil, err
		}
		c.Tiers, err = pgx.CollectRows(trows, func(r pgx.CollectableRow) (StoredTier, error) {
			var t StoredTier
			return t, r.Scan(&t.ID, &t.Pay, &t.Bonus, &t.CouponKind, &t.CouponValue, &t.CouponMin, &t.CouponDays,
				&t.CouponQty, &t.Label, &t.PerCustomer, &t.FirstOnly, &t.Quota, &t.Taken)
		})
		if err != nil {
			return nil, err
		}
		if c.Tiers == nil {
			c.Tiers = []StoredTier{}
		}
	}
	if cs == nil {
		cs = []*StoredCampaign{}
	}
	return cs, nil
}

// StoredOverview is the store's marketing page (§4.2).
type StoredOverview struct {
	Currency    string            `json:"currency"`
	Enabled     bool              `json:"enabled"`
	Cap         int64             `json:"cap"`
	TermsAt     *time.Time        `json:"terms_at"`
	ApproveOver int64             `json:"approve_over"`
	Principal   int64             `json:"principal"`
	Bonus       int64             `json:"bonus"`
	Holders     int               `json:"holders"`
	NewHolders  int               `json:"new_holders"`
	MonthCount  int               `json:"month_count"`
	MonthPaid   int64             `json:"month_paid"`
	MonthGiven  int64             `json:"month_given"`
	MonthSpentP int64             `json:"month_spent_principal"`
	MonthSpentB int64             `json:"month_spent_bonus"`
	Campaigns   []*StoredCampaign `json:"campaigns"`
}

func (a *App) StoredOverview(ctx context.Context, branchID int64) (*StoredOverview, error) {
	b, err := readBranch(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	out := &StoredOverview{Currency: b.Currency}
	if out.Enabled, out.Cap, err = storedRules(ctx, a.DB, b); err != nil {
		return nil, err
	}
	month := `(j.created_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone)`
	if err := a.DB.QueryRow(ctx, `SELECT b.stored_terms_at, b.topup_approve_over,
			COALESCE((SELECT sum(balance) FROM wallet_accounts WHERE branch_id = b.id AND kind = 'stored_principal'), 0),
			COALESCE((SELECT sum(balance) FROM wallet_accounts WHERE branch_id = b.id AND kind = 'stored_bonus'), 0),
			(SELECT count(DISTINCT customer_id) FROM wallet_accounts WHERE branch_id = b.id
				AND kind IN ('stored_principal', 'stored_bonus') AND balance > 0)::int,
			(SELECT count(DISTINCT t.customer_id) FROM topups t WHERE t.branch_id = b.id AND t.status = 'paid'
				AND (t.paid_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone)
				AND NOT EXISTS (SELECT 1 FROM topups e WHERE e.branch_id = b.id AND e.customer_id = t.customer_id
					AND e.status = 'paid' AND e.paid_at < date_trunc('month', now() AT TIME ZONE b.timezone) AT TIME ZONE b.timezone))::int,
			(SELECT count(*) FROM topups t WHERE t.branch_id = b.id AND t.status = 'paid'
				AND (t.paid_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone))::int,
			COALESCE((SELECT sum(t.pay) FROM topups t WHERE t.branch_id = b.id AND t.status = 'paid'
				AND (t.paid_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone)), 0),
			COALESCE((SELECT sum(t.bonus) FROM topups t WHERE t.branch_id = b.id AND t.status = 'paid'
				AND (t.paid_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone)), 0),
			COALESCE((SELECT -sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
				JOIN wallet_accounts w ON w.id = p.account_id WHERE j.branch_id = b.id AND w.kind = 'stored_principal'
				AND j.kind IN ('stored_spend', 'stored_return', 'stored_refund') AND `+month+`), 0),
			COALESCE((SELECT -sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
				JOIN wallet_accounts w ON w.id = p.account_id WHERE j.branch_id = b.id AND w.kind = 'stored_bonus'
				AND j.kind IN ('stored_spend', 'stored_return', 'stored_refund') AND `+month+`), 0)
		FROM branches b WHERE b.id=$1`, branchID).Scan(&out.TermsAt, &out.ApproveOver, &out.Principal, &out.Bonus,
		&out.Holders, &out.NewHolders, &out.MonthCount, &out.MonthPaid, &out.MonthGiven, &out.MonthSpentP, &out.MonthSpentB); err != nil {
		return nil, err
	}
	out.Campaigns, err = loadCampaigns(ctx, a.DB, `c.branch_id=$1`, branchID)
	return out, err
}

// AcceptStoredTerms is the owner agreeing to the store's terms for stored
// value (§4.3): the store issues it, takes the money and pays it back.
func (a *App) AcceptStoredTerms(ctx context.Context, branchID int64, actor Actor) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE branches SET stored_terms_at = COALESCE(stored_terms_at, now()) WHERE id=$1`, branchID); err != nil {
			return err
		}
		return audit(ctx, tx, branchID, 0, actor, "stored.terms", nil)
	})
}

type StoredTierInput struct {
	ID          int64  `json:"id"`
	Pay         int64  `json:"pay"`
	Bonus       int64  `json:"bonus"`
	CouponKind  string `json:"coupon_kind"`
	CouponValue int64  `json:"coupon_value"`
	CouponMin   int64  `json:"coupon_min"`
	CouponDays  int    `json:"coupon_days"`
	CouponQty   int    `json:"coupon_qty"`
	Label       string `json:"label"`
	PerCustomer *int   `json:"per_customer"`
	FirstOnly   bool   `json:"first_only"`
	Quota       *int   `json:"quota"`
}

type StoredCampaignInput struct {
	Name        string            `json:"name"`
	NameEn      string            `json:"name_en"`
	StartsAt    *time.Time        `json:"starts_at"`
	EndsAt      *time.Time        `json:"ends_at"`
	CustomOK    bool              `json:"custom_ok"`
	CustomMin   int64             `json:"custom_min"`
	BrandIDs    []int64           `json:"brand_ids"`
	Note        string            `json:"note"`
	ApproveOver *int64            `json:"approve_over"`
	Tiers       []StoredTierInput `json:"tiers"`
}

func (in *StoredCampaignInput) validate() error {
	in.Name, in.NameEn, in.Note = strings.TrimSpace(in.Name), strings.TrimSpace(in.NameEn), strings.TrimSpace(in.Note)
	if in.Name == "" || len([]rune(in.Name)) > 60 || len([]rune(in.NameEn)) > 60 || len([]rune(in.Note)) > 500 {
		return ErrValidation("a name of up to 60 characters and a note of up to 500")
	}
	if in.StartsAt != nil && in.EndsAt != nil && !in.EndsAt.After(*in.StartsAt) {
		return ErrValidation("the campaign has to end after it starts")
	}
	if len(in.Tiers) == 0 && !in.CustomOK {
		return ErrValidation("add at least one tier")
	}
	if len(in.Tiers) > maxTiers {
		return ErrValidation(fmt.Sprintf("at most %d tiers", maxTiers))
	}
	if in.CustomMin < 0 || (in.CustomOK && in.CustomMin <= 0) {
		return ErrValidation("a top-up of any amount needs a minimum above 0")
	}
	seen := map[int64]bool{}
	for i := range in.Tiers {
		t := &in.Tiers[i]
		t.Label = strings.TrimSpace(t.Label)
		if t.Pay <= 0 || t.Bonus < 0 || t.Bonus > t.Pay {
			return ErrValidation("each tier pays more than 0 and gives a bonus of at most what it pays")
		}
		if seen[t.Pay] {
			return ErrValidation("two tiers pay the same amount")
		}
		seen[t.Pay] = true
		if len([]rune(t.Label)) > 20 || (t.PerCustomer != nil && *t.PerCustomer <= 0) || (t.Quota != nil && *t.Quota <= 0) {
			return ErrValidation("a short label, and limits above 0")
		}
		if t.CouponQty == 0 {
			t.CouponKind, t.CouponValue, t.CouponMin = "", 0, 0
		}
		if t.CouponDays == 0 {
			t.CouponDays = 30
		}
		if t.CouponQty < 0 || t.CouponQty > 10 || t.CouponDays < 1 || t.CouponDays > 365 {
			return ErrValidation("up to 10 coupons, valid 1 to 365 days")
		}
		if t.CouponQty > 0 {
			if t.CouponKind != CouponStoreFree && t.CouponKind != CouponStoreThreshold {
				return ErrValidation("the coupon is one with or without a minimum")
			}
			if t.CouponValue <= 0 || t.CouponMin < 0 || (t.CouponKind == CouponStoreThreshold && t.CouponMin <= t.CouponValue) {
				return ErrValidation("the coupon takes off more than 0, and less than its minimum")
			}
			if t.CouponKind == CouponStoreFree {
				t.CouponMin = 0
			}
		}
	}
	return nil
}

// SaveStoredCampaign writes a campaign and its tiers. A tier dropped from
// the list stays for the top-ups that used it.
func (a *App) SaveStoredCampaign(ctx context.Context, branchID, id int64, in StoredCampaignInput, actor Actor) (int64, error) {
	if err := in.validate(); err != nil {
		return 0, err
	}
	b, err := readBranch(ctx, a.DB, branchID)
	if err != nil {
		return 0, err
	}
	on, _, err := storedRules(ctx, a.DB, b)
	if err != nil {
		return 0, err
	}
	if !on {
		return 0, errStoredOff()
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		var terms *time.Time
		if err := tx.QueryRow(ctx, `SELECT stored_terms_at FROM branches WHERE id=$1 FOR UPDATE`, branchID).Scan(&terms); err != nil {
			return err
		}
		if terms == nil {
			return Err(http.StatusConflict, "STORED_TERMS", "agree to the stored value terms first")
		}
		if in.BrandIDs == nil {
			in.BrandIDs = []int64{}
		}
		var bad int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM unnest($2::bigint[]) x(id)
			WHERE NOT EXISTS (SELECT 1 FROM brands WHERE id = x.id AND branch_id = $1)`, branchID, in.BrandIDs).Scan(&bad); err != nil {
			return err
		}
		if bad > 0 {
			return ErrValidation("a brand of another store")
		}
		names := map[string]string{}
		if in.NameEn != "" {
			names["en"] = in.NameEn
		}
		if id == 0 {
			if err := tx.QueryRow(ctx, `INSERT INTO stored_campaigns (branch_id, name, names, starts_at, ends_at, custom_ok,
				custom_min, brand_ids, note) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, branchID, in.Name, names,
				in.StartsAt, in.EndsAt, in.CustomOK, in.CustomMin, in.BrandIDs, in.Note).Scan(&id); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE stored_campaigns SET name=$3, names=$4, starts_at=$5, ends_at=$6, custom_ok=$7,
				custom_min=$8, brand_ids=$9, note=$10, updated_at=now() WHERE id=$1 AND branch_id=$2`, id, branchID, in.Name,
				names, in.StartsAt, in.EndsAt, in.CustomOK, in.CustomMin, in.BrandIDs, in.Note)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		if err := oneRunningCampaign(ctx, tx, branchID, id); err != nil {
			return err
		}
		keep := []int64{}
		for i, t := range in.Tiers {
			if t.ID != 0 {
				tag, err := tx.Exec(ctx, `UPDATE stored_tiers SET pay=$3, bonus=$4, coupon_kind=$5, coupon_value=$6, coupon_min=$7,
					coupon_days=$8, coupon_qty=$9, label=$10, per_customer=$11, first_only=$12, quota=$13, sort=$14
					WHERE id=$1 AND campaign_id=$2 AND NOT removed`, t.ID, id, t.Pay, t.Bonus, t.CouponKind, t.CouponValue,
					t.CouponMin, t.CouponDays, t.CouponQty, t.Label, t.PerCustomer, t.FirstOnly, t.Quota, i)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 1 {
					keep = append(keep, t.ID)
					continue
				}
			}
			var tid int64
			if err := tx.QueryRow(ctx, `INSERT INTO stored_tiers (campaign_id, pay, bonus, coupon_kind, coupon_value, coupon_min,
				coupon_days, coupon_qty, label, per_customer, first_only, quota, sort) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
				RETURNING id`, id, t.Pay, t.Bonus, t.CouponKind, t.CouponValue, t.CouponMin, t.CouponDays, t.CouponQty, t.Label,
				t.PerCustomer, t.FirstOnly, t.Quota, i).Scan(&tid); err != nil {
				return err
			}
			keep = append(keep, tid)
		}
		if _, err := tx.Exec(ctx, `UPDATE stored_tiers SET removed=true WHERE campaign_id=$1 AND NOT (id = ANY($2))`, id, keep); err != nil {
			return err
		}
		if in.ApproveOver != nil && *in.ApproveOver >= 0 {
			if _, err := tx.Exec(ctx, `UPDATE branches SET topup_approve_over=$2 WHERE id=$1`, branchID, *in.ApproveOver); err != nil {
				return err
			}
		}
		return audit(ctx, tx, branchID, 0, actor, "stored.campaign_saved", map[string]any{"id": id, "name": in.Name, "tiers": in.Tiers})
	})
	return id, err
}

// oneRunningCampaign keeps campaigns that are on from overlapping in time:
// customers would not know which tiers to pick (§4.3).
func oneRunningCampaign(ctx context.Context, tx pgx.Tx, branchID, id int64) error {
	var clash string
	err := tx.QueryRow(ctx, `SELECT o.name FROM stored_campaigns c JOIN stored_campaigns o ON o.branch_id = c.branch_id AND o.id <> c.id
		WHERE c.id=$1 AND c.branch_id=$2 AND c.status='active' AND o.status='active'
			AND (o.ends_at IS NULL OR o.ends_at > now())
			AND COALESCE(c.starts_at, '-infinity') < COALESCE(o.ends_at, 'infinity')
			AND COALESCE(o.starts_at, '-infinity') < COALESCE(c.ends_at, 'infinity')
		LIMIT 1`, id, branchID).Scan(&clash)
	if isNoRows(err) {
		return nil
	} else if err != nil {
		return err
	}
	return ErrD(http.StatusConflict, "STORED_CAMPAIGN_OVERLAP", "another campaign is on at the same time: stop it or change the dates",
		map[string]any{"name": clash})
}

func (a *App) SetStoredCampaignStatus(ctx context.Context, branchID, id int64, status string, actor Actor) error {
	if status != "active" && status != "paused" {
		return ErrValidation("status is active or paused")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE stored_campaigns SET status=$3, updated_at=now() WHERE id=$1 AND branch_id=$2`, id, branchID, status)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if status == "active" {
			if err := oneRunningCampaign(ctx, tx, branchID, id); err != nil {
				return err
			}
		}
		return audit(ctx, tx, branchID, 0, actor, "stored.campaign_"+status, map[string]any{"id": id})
	})
}

// runningCampaign is the campaign on now at the store, shown on the brand's
// page (nil when there is none).
func runningCampaign(ctx context.Context, q Querier, branchID, brandID int64) (*StoredCampaign, error) {
	cs, err := loadCampaigns(ctx, q, `c.branch_id=$1 AND c.status='active' AND (c.starts_at IS NULL OR c.starts_at <= now())
		AND (c.ends_at IS NULL OR c.ends_at > now()) AND (c.brand_ids = '{}' OR $2 = ANY(c.brand_ids))`, branchID, brandID)
	if err != nil || len(cs) == 0 {
		return nil, err
	}
	return cs[0], nil
}

// ---- balances ----

// StoredBalance is what a customer holds at a store: the refund balance and
// stored value (principal and bonus).
type StoredBalance struct {
	Credit    int64 `json:"credit"`
	Principal int64 `json:"principal"`
	Bonus     int64 `json:"bonus"`
	Total     int64 `json:"total"`
	Frozen    bool  `json:"frozen"`
}

func customerBalance(ctx context.Context, q Querier, branchID, customerID int64, currency string) (StoredBalance, error) {
	var s StoredBalance
	err := q.QueryRow(ctx, `SELECT
			COALESCE(sum(balance) FILTER (WHERE kind='store_credit'), 0),
			COALESCE(sum(balance) FILTER (WHERE kind='stored_principal'), 0),
			COALESCE(sum(balance) FILTER (WHERE kind='stored_bonus'), 0),
			COALESCE(bool_or(frozen AND balance > 0), false)
		FROM wallet_accounts WHERE branch_id=$1 AND customer_id=$2 AND currency=$3`, branchID, customerID, currency).
		Scan(&s.Credit, &s.Principal, &s.Bonus, &s.Frozen)
	s.Total = s.Credit + s.Principal + s.Bonus
	return s, err
}

// ---- the customer's page (§5) ----

// StoredOffers is the stored value part of a store's offers page; the
// coupons and promotions come from the store's own endpoints.
type StoredOffers struct {
	Currency string          `json:"currency"`
	Enabled  bool            `json:"enabled"`
	Campaign *StoredCampaign `json:"campaign"`
	Balance  *StoredBalance  `json:"balance"`
	// Pending is the customer's top-up waiting at the counter, if any.
	Pending *Topup `json:"pending"`
}

func (a *App) StoredOffers(ctx context.Context, slug string, customerID int64) (*StoredOffers, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	br, err := brandBySlug(ctx, a.DB, b.ID, slug)
	if err != nil {
		return nil, err
	}
	out := &StoredOffers{Currency: b.Currency}
	if out.Enabled, _, err = storedRules(ctx, a.DB, b); err != nil {
		return nil, err
	}
	var terms *time.Time
	if err := a.DB.QueryRow(ctx, `SELECT stored_terms_at FROM branches WHERE id=$1`, b.ID).Scan(&terms); err != nil {
		return nil, err
	}
	if out.Enabled && terms != nil {
		if out.Campaign, err = runningCampaign(ctx, a.DB, b.ID, br.ID); err != nil {
			return nil, err
		}
	}
	if customerID != 0 {
		bal, err := customerBalance(ctx, a.DB, b.ID, customerID, b.Currency)
		if err != nil {
			return nil, err
		}
		out.Balance = &bal
		if out.Campaign != nil {
			for i := range out.Campaign.Tiers {
				t := &out.Campaign.Tiers[i]
				t.Mine, t.Closed, err = tierOpen(ctx, a.DB, b.ID, customerID, t, 0)
				if err != nil {
					return nil, err
				}
			}
		}
		ts, err := listTopups(ctx, a.DB, `t.branch_id=$1 AND t.customer_id=$2 AND t.status='pending' AND t.expires_at > now()`, b.ID, customerID)
		if err != nil {
			return nil, err
		}
		if len(ts) > 0 {
			out.Pending = &ts[0]
		}
	}
	return out, nil
}

// tierOpen says whether the customer may take a tier now (except is a
// top-up not to count: the one being confirmed).
func tierOpen(ctx context.Context, q Querier, branchID, customerID int64, t *StoredTier, except int64) (mine int, closed string, err error) {
	var before int
	if err = q.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM topups WHERE tier_id=$1 AND customer_id=$2 AND id <> $4
				AND (status='paid' OR (status='pending' AND expires_at > now())))::int,
			(SELECT count(*) FROM topups WHERE branch_id=$3 AND customer_id=$2 AND status='paid' AND id <> $4)::int`,
		t.ID, customerID, branchID, except).Scan(&mine, &before); err != nil {
		return
	}
	switch {
	case t.FirstOnly && before > 0:
		closed = "first_only"
	case t.PerCustomer != nil && mine >= *t.PerCustomer:
		closed = "per_customer"
	case t.Quota != nil && t.Taken >= *t.Quota:
		closed = "quota"
	}
	return
}

// ---- top-ups (§5.3, §6) ----

type Topup struct {
	ID          int64      `json:"id"`
	BranchID    int64      `json:"branch_id"`
	StoreName   string     `json:"store_name"`
	Slug        string     `json:"slug"`
	BrandName   string     `json:"brand_name"`
	CustomerID  int64      `json:"customer_id"`
	Phone       string     `json:"phone"`
	Currency    string     `json:"currency"`
	Pay         int64      `json:"pay"`
	Bonus       int64      `json:"bonus"`
	CouponKind  string     `json:"coupon_kind"`
	CouponValue int64      `json:"coupon_value"`
	CouponMin   int64      `json:"coupon_min"`
	CouponQty   int        `json:"coupon_qty"`
	Source      string     `json:"source"`
	Status      string     `json:"status"` // pending, paid, cancelled, revoked, expired
	Code        string     `json:"code,omitempty"`
	Token       string     `json:"token,omitempty"`
	ExpiresAt   time.Time  `json:"expires_at"`
	PayType     string     `json:"pay_type"`
	PayMethod   string     `json:"pay_method"`
	ReceivedBy  string     `json:"received_by"`
	PaidAt      *time.Time `json:"paid_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
	// Revocable: paid today and nothing of it spent (§6).
	Revocable bool `json:"revocable"`
}

const topupCols = `t.id, t.branch_id, b.name, b.slug, COALESCE(br.name, ''), t.customer_id, c.phone, t.currency, t.pay, t.bonus,
	t.coupon_kind, t.coupon_value, t.coupon_min, t.coupon_qty, t.source,
	CASE WHEN t.status = 'pending' AND t.expires_at <= now() THEN 'expired' ELSE t.status END,
	t.code, t.token, t.expires_at, t.pay_type, t.pay_method, COALESCE(u.display_name, ''), t.paid_at, t.revoked_at, t.created_at,
	t.status = 'paid' AND (t.paid_at AT TIME ZONE b.timezone)::date = (now() AT TIME ZONE b.timezone)::date`

func listTopups(ctx context.Context, q Querier, where string, args ...any) ([]Topup, error) {
	rows, err := q.Query(ctx, `SELECT `+topupCols+` FROM topups t JOIN branches b ON b.id = t.branch_id
		JOIN customers c ON c.id = t.customer_id LEFT JOIN brands br ON br.id = t.brand_id LEFT JOIN users u ON u.id = t.received_by
		WHERE `+where+` ORDER BY t.id DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Topup, error) {
		var t Topup
		return t, r.Scan(&t.ID, &t.BranchID, &t.StoreName, &t.Slug, &t.BrandName, &t.CustomerID, &t.Phone, &t.Currency, &t.Pay,
			&t.Bonus, &t.CouponKind, &t.CouponValue, &t.CouponMin, &t.CouponQty, &t.Source, &t.Status, &t.Code, &t.Token,
			&t.ExpiresAt, &t.PayType, &t.PayMethod, &t.ReceivedBy, &t.PaidAt, &t.RevokedAt, &t.CreatedAt, &t.Revocable)
	})
	if out == nil {
		out = []Topup{}
	}
	return out, err
}

func randomDigits(n int) (string, error) {
	max := big.NewInt(1)
	for range n {
		max.Mul(max, big.NewInt(10))
	}
	v, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", n, v.Int64()), nil
}

// TopupInput is what a top-up is asked for: a tier of the running campaign,
// or any amount when the campaign allows it (no bonus).
type TopupInput struct {
	TierID int64 `json:"tier_id"`
	Amount int64 `json:"amount"`
	// staff only
	Phone   string `json:"phone"`
	BrandID int64  `json:"brand_id"`
}

// newTopup writes a pending top-up for the customer (the customer row
// locked by the caller), checking the campaign, the tier's limits and the
// country's cap.
func newTopup(ctx context.Context, tx pgx.Tx, b *Branch, brandID, customerID int64, in TopupInput, source string, actor Actor) (*Topup, error) {
	on, cap, err := storedRules(ctx, tx, b)
	if err != nil {
		return nil, err
	}
	if !on {
		return nil, errStoredOff()
	}
	camp, err := runningCampaign(ctx, tx, b.ID, brandID)
	if err != nil {
		return nil, err
	}
	if camp == nil {
		return nil, Err(http.StatusConflict, "STORED_NO_CAMPAIGN", "the store has no top-up offer on now")
	}
	var tier *StoredTier
	pay, bonus := in.Amount, int64(0)
	if in.TierID != 0 {
		for i := range camp.Tiers {
			if camp.Tiers[i].ID == in.TierID {
				tier = &camp.Tiers[i]
			}
		}
		if tier == nil {
			return nil, Err(http.StatusConflict, "STORED_TIER_GONE", "that tier is no longer on offer")
		}
		_, closed, err := tierOpen(ctx, tx, b.ID, customerID, tier, 0)
		if err != nil {
			return nil, err
		}
		if closed != "" {
			return nil, ErrD(http.StatusConflict, "STORED_TIER_CLOSED", "you cannot take this tier", map[string]any{"why": closed})
		}
		pay, bonus = tier.Pay, tier.Bonus
	} else if !camp.CustomOK || pay < camp.CustomMin {
		return nil, ErrD(http.StatusUnprocessableEntity, "STORED_AMOUNT", "pick a tier, or an amount of at least the minimum",
			map[string]any{"min": camp.CustomMin})
	}
	if cap > 0 {
		bal, err := customerBalance(ctx, tx, b.ID, customerID, b.Currency)
		if err != nil {
			return nil, err
		}
		if bal.Principal+bal.Bonus+pay+bonus > cap {
			return nil, ErrD(http.StatusConflict, "STORED_CAP", "this would hold more than the most one customer may hold at a store",
				map[string]any{"cap": cap, "holds": bal.Principal + bal.Bonus})
		}
	}
	// one waiting top-up per customer at a store: a new one replaces it
	if _, err := tx.Exec(ctx, `UPDATE topups SET status='cancelled' WHERE branch_id=$1 AND customer_id=$2 AND status='pending'`,
		b.ID, customerID); err != nil {
		return nil, err
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}
	var brand *int64
	if brandID != 0 {
		brand = &brandID
	}
	var by *int64
	if actor.Type == "staff" && actor.ID != 0 {
		by = &actor.ID
	}
	var id int64
	for range 20 {
		code, err := randomDigits(6)
		if err != nil {
			return nil, err
		}
		var tierID, campID *int64
		t := StoredTier{}
		if tier != nil {
			tierID, t = &tier.ID, *tier
		}
		campID = &camp.ID
		err = tx.QueryRow(ctx, `INSERT INTO topups (branch_id, brand_id, customer_id, campaign_id, tier_id, currency, pay, bonus,
			coupon_kind, coupon_value, coupon_min, coupon_days, coupon_qty, source, code, token, expires_at, requested_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
			ON CONFLICT (branch_id, code) WHERE status = 'pending' DO NOTHING RETURNING id`,
			b.ID, brand, customerID, campID, tierID, b.Currency, pay, bonus, t.CouponKind, t.CouponValue, t.CouponMin,
			max(t.CouponDays, 1), t.CouponQty, source, code, hex.EncodeToString(tok), time.Now().Add(topupTTL), by).Scan(&id)
		if err == nil {
			break
		}
		if !isNoRows(err) {
			return nil, err
		}
	}
	if id == 0 {
		return nil, fmt.Errorf("no free top-up code")
	}
	ts, err := listTopups(ctx, tx, `t.id=$1`, id)
	if err != nil {
		return nil, err
	}
	return &ts[0], nil
}

// CreateTopup is the customer asking to top up at a store: a code to show
// at the counter (§5.3).
func (a *App) CreateTopup(ctx context.Context, slug string, customerID int64, in TopupInput) (*Topup, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	br, err := brandBySlug(ctx, a.DB, b.ID, slug)
	if err != nil {
		return nil, err
	}
	var out *Topup
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM customers WHERE id=$1 FOR UPDATE`, customerID); err != nil {
			return err
		}
		out, err = newTopup(ctx, tx, b, br.ID, customerID, in, "customer", Actor{Type: "customer", ID: customerID})
		return err
	})
	return out, err
}

// MyTopup is one of the customer's top-ups.
func (a *App) MyTopup(ctx context.Context, customerID, id int64) (*Topup, error) {
	ts, err := listTopups(ctx, a.DB, `t.id=$1 AND t.customer_id=$2`, id, customerID)
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, ErrNotFound
	}
	return &ts[0], nil
}

func (a *App) CancelMyTopup(ctx context.Context, customerID, id int64) error {
	tag, err := a.DB.Exec(ctx, `UPDATE topups SET status='cancelled' WHERE id=$1 AND customer_id=$2 AND status='pending'`, id, customerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidState("the top-up is no longer waiting")
	}
	return nil
}

// BranchTopups is a store's top-ups: waiting at the counter, or the record.
func (a *App) BranchTopups(ctx context.Context, branchID int64, status, code string) ([]Topup, error) {
	switch {
	case code != "":
		return listTopups(ctx, a.DB, `t.branch_id=$1 AND t.code=$2 AND t.status='pending' AND t.expires_at > now()`, branchID, code)
	case status == "pending":
		return listTopups(ctx, a.DB, `t.branch_id=$1 AND t.status='pending' AND t.expires_at > now()`, branchID)
	}
	return listTopups(ctx, a.DB, `t.branch_id=$1 AND t.status IN ('paid', 'revoked')`, branchID)
}

// TopupStarted is a top-up the staff started, and the code texted to the
// customer to confirm it.
type TopupStarted struct {
	Topup   *Topup         `json:"topup"`
	Verify  CreditCodeSent `json:"verify"`
	Balance StoredBalance  `json:"balance"`
}

// StartStaffTopup is the staff topping up a customer by phone (§6): the
// customer gets a texted code they read out, so nobody tops up an account
// whose owner does not know.
func (a *App) StartStaffTopup(ctx context.Context, branchID int64, in TopupInput, actor Actor) (*TopupStarted, error) {
	phone, err := normalizeCustomerPhone(in.Phone)
	if err != nil {
		return nil, Err(http.StatusUnprocessableEntity, "CREDIT_PHONE", "enter the customer's mobile number")
	}
	b, err := readBranch(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	code, err := randomDigits(6)
	if err != nil {
		return nil, err
	}
	out := &TopupStarted{}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		brandID := in.BrandID
		if brandID != 0 {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM brands WHERE id=$1 AND branch_id=$2)`, brandID, branchID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return ErrValidation("a brand of another store")
			}
		} else {
			main, err := loadBrand(ctx, tx, branchID, 0)
			if err != nil {
				return err
			}
			brandID = main.ID
		}
		var cust int64
		if err := tx.QueryRow(ctx, `INSERT INTO customers (phone) VALUES ($1)
			ON CONFLICT (phone) DO UPDATE SET phone = EXCLUDED.phone RETURNING id`, phone).Scan(&cust); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM customers WHERE id=$1 FOR UPDATE`, cust); err != nil {
			return err
		}
		t, err := newTopup(ctx, tx, b, brandID, cust, in, "staff", actor)
		if err != nil {
			return err
		}
		out.Topup = t
		if out.Balance, err = customerBalance(ctx, tx, branchID, cust, b.Currency); err != nil {
			return err
		}
		var by *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if err := tx.QueryRow(ctx, `INSERT INTO wallet_verifications (topup_id, phone, purpose, code_hash, requested_by, expires_at)
			VALUES ($1,$2,'topup','\x00',$3,$4) RETURNING id`, t.ID, phone, by, time.Now().Add(otpTTL)).Scan(&out.Verify.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE wallet_verifications SET code_hash=$2 WHERE id=$1`, out.Verify.ID,
			a.creditCodeHash(out.Verify.ID, phone, code))
		return err
	})
	if err != nil {
		return nil, err
	}
	out.Verify.Phone = maskPhone(phone)
	amt := fmtMoney(out.Topup.Pay, b.Currency)
	msg := fmt.Sprintf("%s 要给你的账户充值 %s，验证码 %s，5 分钟内有效，只告诉本店店员。%s is topping up %s for you: code %s, valid 5 minutes. Only tell the store's staff.",
		b.Name, amt, code, b.Name, amt, code)
	if err := a.smsSender().Send(ctx, phone, msg); err != nil {
		return nil, Err(http.StatusBadGateway, "SMS_FAILED", "the code could not be sent")
	}
	if a.OTPDevEcho {
		out.Verify.DevCode = code
	}
	return out, nil
}

// checkTopupCode checks the customer's texted code for a top-up the staff
// started.
func (a *App) checkTopupCode(ctx context.Context, tx pgx.Tx, topupID int64, v *CreditVerify) error {
	bad := Err(http.StatusUnprocessableEntity, "OTP_INVALID", "the code is wrong or expired")
	if v == nil {
		return bad
	}
	var phone string
	var hash []byte
	var attempts int
	err := tx.QueryRow(ctx, `SELECT phone, code_hash, attempts FROM wallet_verifications WHERE id=$1 AND topup_id=$2
		AND purpose='topup' AND consumed_at IS NULL AND expires_at > now()`, v.ID, topupID).Scan(&phone, &hash, &attempts)
	if isNoRows(err) || (err == nil && attempts >= otpMaxAttempts) {
		return bad
	} else if err != nil {
		return err
	}
	if !hmac.Equal(hash, a.creditCodeHash(v.ID, phone, v.Code)) {
		if _, err := a.DB.Exec(ctx, `UPDATE wallet_verifications SET attempts = attempts + 1 WHERE id=$1`, v.ID); err != nil {
			return err
		}
		return bad
	}
	tag, err := tx.Exec(ctx, `UPDATE wallet_verifications SET consumed_at=now() WHERE id=$1 AND consumed_at IS NULL`, v.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return bad
	}
	return nil
}

// TopupConfirm is the staff saying they took the money.
type TopupConfirm struct {
	PayType   string        `json:"pay_type"`
	ChannelID int64         `json:"pay_channel_id"`
	Verify    *CreditVerify `json:"verify"`
}

// isManager says whether the person owns or manages the store.
func isManager(ctx context.Context, q Querier, userID, branchID int64) (bool, error) {
	if userID == 0 {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_branch_roles WHERE user_id=$1 AND branch_id=$2
		AND role IN ('owner', 'manager'))`, userID, branchID).Scan(&ok)
	return ok, err
}

// ConfirmTopup books a waiting top-up as paid: the principal and the bonus
// go into the customer's stored value, the coupons that come with it are
// given, and the money is the store's (its drawer, or its online methods).
func (a *App) ConfirmTopup(ctx context.Context, branchID, id int64, in TopupConfirm, actor Actor) (*Topup, error) {
	b, err := readBranch(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		var custID, pay, bonus int64
		var status, source, currency, cKind string
		var cValue, cMin int64
		var cDays, cQty int
		var tierID, brandID *int64
		var expires time.Time
		err := tx.QueryRow(ctx, `SELECT customer_id, pay, bonus, status, source, currency, coupon_kind, coupon_value, coupon_min,
				coupon_days, coupon_qty, tier_id, brand_id, expires_at
			FROM topups WHERE id=$1 AND branch_id=$2 FOR UPDATE`, id, branchID).Scan(&custID, &pay, &bonus, &status, &source,
			&currency, &cKind, &cValue, &cMin, &cDays, &cQty, &tierID, &brandID, &expires)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "pending" || !expires.After(time.Now()) {
			return Err(http.StatusConflict, "TOPUP_NOT_PENDING", "this top-up was already paid, cancelled or has expired")
		}
		if currency != b.Currency {
			return ErrInvalidState("the store's currency changed")
		}
		on, cap, err := storedRules(ctx, tx, b)
		if err != nil {
			return err
		}
		if !on {
			return errStoredOff()
		}
		if source == "staff" {
			if err := a.checkTopupCode(ctx, tx, id, in.Verify); err != nil {
				return err
			}
		}
		var approveOver int64
		if err := tx.QueryRow(ctx, `SELECT topup_approve_over FROM branches WHERE id=$1`, branchID).Scan(&approveOver); err != nil {
			return err
		}
		if pay > approveOver {
			mgr, err := isManager(ctx, tx, actor.ID, branchID)
			if err != nil {
				return err
			}
			if !mgr && actor.ApprovedBy == 0 {
				return ErrD(http.StatusForbidden, "STORED_NEEDS_APPROVAL", "a top-up this large needs a manager",
					map[string]any{"perm": PermStoredCashout, "limit": approveOver})
			}
		}
		channel, method, err := resolvePay(ctx, tx, branchID, PayInput{Type: in.PayType, ChannelID: in.ChannelID})
		if err != nil {
			return err
		}
		if in.PayType == "cash" {
			method = "cash"
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM customers WHERE id=$1 FOR UPDATE`, custID); err != nil {
			return err
		}
		// the tier's limits again: two counters may confirm two codes at once
		if tierID != nil {
			var t StoredTier
			if err := tx.QueryRow(ctx, `SELECT id, per_customer, first_only, quota,
					(SELECT count(*) FROM topups x WHERE x.tier_id = t.id AND x.status = 'paid')::int
				FROM stored_tiers t WHERE id=$1 FOR UPDATE`, *tierID).Scan(&t.ID, &t.PerCustomer, &t.FirstOnly, &t.Quota, &t.Taken); err != nil {
				return err
			}
			if _, closed, err := tierOpen(ctx, tx, branchID, custID, &t, id); err != nil {
				return err
			} else if closed != "" {
				return ErrD(http.StatusConflict, "STORED_TIER_CLOSED", "the customer cannot take this tier", map[string]any{"why": closed})
			}
		}
		prin, err := walletAccount(ctx, tx, acctPrincipal, branchID, custID, currency)
		if err != nil {
			return err
		}
		bon, err := walletAccount(ctx, tx, acctBonus, branchID, custID, currency)
		if err != nil {
			return err
		}
		if prin.Frozen || bon.Frozen {
			return Err(http.StatusConflict, "CREDIT_FROZEN", "this store balance is frozen")
		}
		if cap > 0 && prin.Balance+bon.Balance+pay+bonus > cap {
			return ErrD(http.StatusConflict, "STORED_CAP", "this would hold more than the most one customer may hold at a store",
				map[string]any{"cap": cap, "holds": prin.Balance + bon.Balance})
		}
		topAcct, err := walletAccount(ctx, tx, acctTopup, branchID, 0, currency)
		if err != nil {
			return err
		}
		fund, err := walletAccount(ctx, tx, acctBonusFund, branchID, 0, currency)
		if err != nil {
			return err
		}
		if _, _, err := postJournal(ctx, tx, journal{Kind: storedJournal, Idem: fmt.Sprintf("topup:%d", id), BranchID: branchID,
			Currency: currency, TopupID: id, Actor: actor},
			posting{prin.ID, pay}, posting{topAcct.ID, -pay}, posting{bon.ID, bonus}, posting{fund.ID, -bonus}); err != nil {
			return err
		}
		var by, approved *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if actor.ApprovedBy != 0 {
			approved = &actor.ApprovedBy
		}
		if _, err := tx.Exec(ctx, `UPDATE topups SET status='paid', pay_type=$2, pay_channel_id=$3, pay_method=$4, received_by=$5,
			approved_by=$6, paid_at=now() WHERE id=$1`, id, in.PayType, channel, method, by, approved); err != nil {
			return err
		}
		if cQty > 0 {
			if err := giveTopupCoupons(ctx, tx, b, brandID, custID, cKind, cValue, cMin, cDays, cQty); err != nil {
				return err
			}
		}
		if err := audit(ctx, tx, branchID, 0, actor, "stored.topup", map[string]any{"topup": id, "pay": pay, "bonus": bonus,
			"pay_type": in.PayType, "method": method}); err != nil {
			return err
		}
		amt, gift := fmtMoney(pay, currency), fmtMoney(bonus, currency)
		return tell(ctx, tx, custID, Message{Kind: "wallet",
			Title:   fmt.Sprintf("%s 充值 %s 成功，赠送 %s", b.Name, amt, gift),
			TitleEn: fmt.Sprintf("Topped up %s at %s, with %s free", amt, b.Name, gift),
			Body:    "只能在本店使用。", BodyEn: "It can only be spent at this store.",
			Link: fmt.Sprintf("/me/store-credit/%d", branchID), Key: "stored_topup",
			Args: map[string]string{"store": b.Name, "amount": amt, "bonus": gift}})
	})
	if err != nil {
		return nil, err
	}
	ts, err := listTopups(ctx, a.DB, `t.id=$1`, id)
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	return &ts[0], nil
}

// giveTopupCoupons gives the coupons that come with a tier. They work at the
// brand the customer topped up at.
func giveTopupCoupons(ctx context.Context, tx pgx.Tx, b *Branch, brandID *int64, customerID int64, kind string, value, min int64, days, qty int) error {
	if brandID == nil {
		main, err := loadBrand(ctx, tx, b.ID, 0)
		if err != nil {
			return err
		}
		brandID = &main.ID
	}
	zh, en := couponWords(kind, value, min, b.Currency)
	var batch int64
	err := tx.QueryRow(ctx, `SELECT id FROM coupon_batches WHERE branch_id=$1 AND brand_id=$2 AND source='topup' AND kind=$3
		AND value=$4 AND threshold=$5 AND valid_days=$6 AND currency=$7 LIMIT 1`, b.ID, *brandID, kind, value, min, days, b.Currency).Scan(&batch)
	if isNoRows(err) {
		err = tx.QueryRow(ctx, `INSERT INTO coupon_batches (issuer, branch_id, brand_id, kind, source, name, name_en, currency, value,
			threshold, valid_days) VALUES ('store',$1,$2,$3,'topup',$4,$5,$6,$7,$8,$9) RETURNING id`,
			b.ID, *brandID, kind, zh, en, b.Currency, value, min, days).Scan(&batch)
	}
	if err != nil {
		return err
	}
	exp := expiryFor(time.Now(), b.location(), days)
	for range qty {
		if _, err := tx.Exec(ctx, `INSERT INTO customer_coupons (batch_id, customer_id, issuer, kind, branch_id, currency, name,
			name_en, value, threshold, source, expires_at, brand_id) VALUES ($1,$2,'store',$3,$4,$5,$6,$7,$8,$9,'topup',$10,$11)`,
			batch, customerID, kind, b.ID, b.Currency, zh, en, value, min, exp, *brandID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE coupon_batches SET issued_count = issued_count + $2 WHERE id=$1`, batch, qty)
	return err
}

// RevokeTopup undoes a top-up booked by mistake (§6): only on the day it was
// paid and while none of it was spent; the money goes back out of the drawer.
func (a *App) RevokeTopup(ctx context.Context, branchID, id int64, note string, actor Actor) error {
	note = strings.TrimSpace(note)
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var custID, pay, bonus int64
		var status, currency string
		var today bool
		err := tx.QueryRow(ctx, `SELECT t.customer_id, t.pay, t.bonus, t.status, t.currency,
				(t.paid_at AT TIME ZONE b.timezone)::date = (now() AT TIME ZONE b.timezone)::date
			FROM topups t JOIN branches b ON b.id = t.branch_id WHERE t.id=$1 AND t.branch_id=$2 FOR UPDATE OF t`, id, branchID).
			Scan(&custID, &pay, &bonus, &status, &currency, &today)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "paid" || !today {
			return Err(http.StatusConflict, "TOPUP_NOT_REVOCABLE", "only a top-up paid today can be undone")
		}
		prin, err := walletAccount(ctx, tx, acctPrincipal, branchID, custID, currency)
		if err != nil {
			return err
		}
		bon, err := walletAccount(ctx, tx, acctBonus, branchID, custID, currency)
		if err != nil {
			return err
		}
		var spent bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			WHERE p.account_id IN ($1, $2) AND p.amount < 0 AND j.id > (SELECT id FROM wallet_journals WHERE topup_id=$3 AND kind='topup'))`,
			prin.ID, bon.ID, id).Scan(&spent); err != nil {
			return err
		}
		if spent || prin.Balance < pay || bon.Balance < bonus {
			return Err(http.StatusConflict, "TOPUP_SPENT", "some of this top-up was already spent: pay stored value back instead")
		}
		var orig int64
		if err := tx.QueryRow(ctx, `SELECT id FROM wallet_journals WHERE topup_id=$1 AND kind='topup'`, id).Scan(&orig); err != nil {
			return err
		}
		topAcct, err := walletAccount(ctx, tx, acctTopup, branchID, 0, currency)
		if err != nil {
			return err
		}
		fund, err := walletAccount(ctx, tx, acctBonusFund, branchID, 0, currency)
		if err != nil {
			return err
		}
		if _, _, err := postJournal(ctx, tx, journal{Kind: "topup_revoke", Idem: fmt.Sprintf("topup_revoke:%d", id),
			BranchID: branchID, Currency: currency, TopupID: id, Actor: actor, Note: note, Reverses: orig},
			posting{prin.ID, -pay}, posting{topAcct.ID, pay}, posting{bon.ID, -bonus}, posting{fund.ID, bonus}); err != nil {
			return err
		}
		var by *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if _, err := tx.Exec(ctx, `UPDATE topups SET status='revoked', revoked_by=$2, revoked_at=now(), revoke_note=left($3, 200)
			WHERE id=$1`, id, by, note); err != nil {
			return err
		}
		// coupons that came with it and were not used go too
		if _, err := tx.Exec(ctx, `UPDATE customer_coupons c SET status='void' FROM coupon_batches cb, topups t
			WHERE t.id=$1 AND cb.id = c.batch_id AND cb.source='topup' AND c.customer_id = t.customer_id AND c.status='unused'
			AND c.issued_at >= t.paid_at - interval '1 second' AND c.issued_at <= t.paid_at + interval '1 second'`, id); err != nil {
			return err
		}
		return audit(ctx, tx, branchID, 0, actor, "stored.topup_revoked", map[string]any{"topup": id, "pay": pay, "bonus": bonus, "note": note})
	})
}

// ---- paying stored value back (§8.3) ----

type CashoutQuote struct {
	CustomerID int64  `json:"customer_id"`
	Phone      string `json:"phone"`
	Currency   string `json:"currency"`
	Reason     string `json:"reason"`
	ToppedUp   int64  `json:"topped_up"`   // principal topped up since the last payback
	BonusGiven int64  `json:"bonus_given"` // bonus given since the last payback
	Principal  int64  `json:"principal"`   // left
	Bonus      int64  `json:"bonus"`       // left, made void
	BonusUsed  int64  `json:"bonus_used"`
	Clawback   int64  `json:"clawback"`
	PaidOut    int64  `json:"paid_out"`
}

// cashoutQuote works out the payback (rule W14): for the customer's own
// reasons, what is left of the principal less the bonus already used; for
// the store's, all that is left of the principal. The bonus left is void.
func cashoutQuote(ctx context.Context, tx pgx.Tx, branchID, customerID int64, currency, reason string) (*CashoutQuote, error) {
	if reason != "customer" && reason != "store" {
		return nil, ErrValidation("the reason is the customer's or the store's")
	}
	q := &CashoutQuote{CustomerID: customerID, Currency: currency, Reason: reason}
	err := tx.QueryRow(ctx, `WITH last AS (SELECT COALESCE(max(j.id), 0) AS id FROM wallet_journals j
			JOIN wallet_postings p ON p.journal_id = j.id JOIN wallet_accounts w ON w.id = p.account_id
			WHERE j.kind = 'stored_cashout' AND w.branch_id = $1 AND w.customer_id = $2)
		SELECT c.phone,
			COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
				JOIN wallet_accounts w ON w.id = p.account_id, last
				WHERE j.kind IN ('topup', 'topup_revoke') AND j.id > last.id AND w.kind = 'stored_principal'
				AND w.branch_id = $1 AND w.customer_id = $2 AND w.currency = $3), 0),
			COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
				JOIN wallet_accounts w ON w.id = p.account_id, last
				WHERE j.kind IN ('topup', 'topup_revoke') AND j.id > last.id AND w.kind = 'stored_bonus'
				AND w.branch_id = $1 AND w.customer_id = $2 AND w.currency = $3), 0)
		FROM customers c WHERE c.id = $2`, branchID, customerID, currency).Scan(&q.Phone, &q.ToppedUp, &q.BonusGiven)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	bal, err := customerBalance(ctx, tx, branchID, customerID, currency)
	if err != nil {
		return nil, err
	}
	q.Principal, q.Bonus = bal.Principal, bal.Bonus
	q.BonusUsed = max(0, q.BonusGiven-q.Bonus)
	q.PaidOut = q.Principal
	if reason == "customer" {
		q.PaidOut = max(0, q.Principal-q.BonusUsed)
	}
	q.Clawback = q.Principal - q.PaidOut
	q.Phone = maskPhone(q.Phone)
	return q, nil
}

func (a *App) StoredCashoutQuote(ctx context.Context, branchID, customerID int64, reason string) (*CashoutQuote, error) {
	b, err := readBranch(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	var q *CashoutQuote
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		q, err = cashoutQuote(ctx, tx, branchID, customerID, b.Currency, reason)
		return err
	})
	return q, err
}

type CashoutInput struct {
	Reason  string `json:"reason"`
	PayType string `json:"pay_type"`
	Consent string `json:"consent"`
	Note    string `json:"note"`
	// Expect is what the staff saw: the payback is refused if it changed.
	Expect *int64 `json:"expect"`
}

// storedCashoutApproval: the owner is not limited; anyone else paying back
// more than this needs the owner's code.
const storedCashoutApproval = 200000

func (a *App) StoredCashout(ctx context.Context, branchID, customerID int64, in CashoutInput, actor Actor) (*CashoutQuote, error) {
	switch in.PayType {
	case "cash", "transfer", "online", "other":
	default:
		return nil, ErrValidation("how the money was paid back: cash, transfer, online or other")
	}
	switch in.Consent {
	case "in_person", "phone", "chat":
	default:
		return nil, ErrValidation("say how the customer asked for it")
	}
	b, err := readBranch(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	var q *CashoutQuote
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM customers WHERE id=$1 FOR UPDATE`, customerID); err != nil {
			return err
		}
		prin, err := walletAccount(ctx, tx, acctPrincipal, branchID, customerID, b.Currency)
		if err != nil {
			return err
		}
		bon, err := walletAccount(ctx, tx, acctBonus, branchID, customerID, b.Currency)
		if err != nil {
			return err
		}
		if q, err = cashoutQuote(ctx, tx, branchID, customerID, b.Currency, in.Reason); err != nil {
			return err
		}
		if q.Principal+q.Bonus == 0 {
			return Err(http.StatusConflict, "STORED_NONE", "the customer holds no stored value here")
		}
		if in.Expect != nil && *in.Expect != q.PaidOut {
			return Err(http.StatusConflict, "STORED_CHANGED", "the amount changed: look again")
		}
		if q.PaidOut > storedCashoutApproval {
			owner, err := isOwner(ctx, tx, actor.ID, branchID)
			if err != nil {
				return err
			}
			byOwner, err := isOwner(ctx, tx, actor.ApprovedBy, branchID)
			if err != nil {
				return err
			}
			if !owner && !byOwner {
				return ErrD(http.StatusForbidden, "STORED_CASHOUT_OWNER", "paying back this much needs the owner",
					map[string]any{"perm": PermStoredCashout, "limit": storedCashoutApproval, "owner": true})
			}
		}
		var by, approved *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if actor.ApprovedBy != 0 {
			approved = &actor.ApprovedBy
		}
		var cid int64
		if err := tx.QueryRow(ctx, `INSERT INTO stored_cashouts (branch_id, customer_id, currency, reason, principal, bonus, clawback,
			paid_out, pay_type, consent, note, done_by, approved_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,left($11, 200),$12,$13)
			RETURNING id`, branchID, customerID, b.Currency, in.Reason, q.Principal, q.Bonus, q.Clawback, q.PaidOut, in.PayType,
			in.Consent, strings.TrimSpace(in.Note), by, approved).Scan(&cid); err != nil {
			return err
		}
		topAcct, err := walletAccount(ctx, tx, acctTopup, branchID, 0, b.Currency)
		if err != nil {
			return err
		}
		fund, err := walletAccount(ctx, tx, acctBonusFund, branchID, 0, b.Currency)
		if err != nil {
			return err
		}
		if _, _, err := postJournal(ctx, tx, journal{Kind: "stored_cashout", Idem: fmt.Sprintf("cashout:%d", cid),
			BranchID: branchID, Currency: b.Currency, Cashout: cid, Actor: actor, Note: in.Reason},
			posting{prin.ID, -q.Principal}, posting{topAcct.ID, q.Principal},
			posting{bon.ID, -q.Bonus}, posting{fund.ID, q.Bonus}); err != nil {
			return err
		}
		if err := audit(ctx, tx, branchID, 0, actor, "stored.cashout", map[string]any{"cashout": cid, "customer": customerID,
			"reason": in.Reason, "paid_out": q.PaidOut, "clawback": q.Clawback, "bonus_void": q.Bonus}); err != nil {
			return err
		}
		amt := fmtMoney(q.PaidOut, b.Currency)
		return tell(ctx, tx, customerID, Message{Kind: "wallet",
			Title:   fmt.Sprintf("%s 已退还你的储值 %s", b.Name, amt),
			TitleEn: fmt.Sprintf("%s paid back %s of your stored value", b.Name, amt),
			Body:    "你在本店的储值已清零。", BodyEn: "Your stored value at the store is now 0.",
			Link: fmt.Sprintf("/me/store-credit/%d", branchID), Key: "stored_cashout",
			Args: map[string]string{"store": b.Name, "amount": amt}})
	})
	return q, err
}

// ---- the store's stored value page (§10) ----

type StoredHolder struct {
	CustomerID int64      `json:"customer_id"`
	Phone      string     `json:"phone"`
	Principal  int64      `json:"principal"`
	Bonus      int64      `json:"bonus"`
	Frozen     bool       `json:"frozen"`
	LastTopup  *time.Time `json:"last_topup"`
}

type StoredEntry struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	Principal int64     `json:"principal"`
	Bonus     int64     `json:"bonus"`
	OrderID   int64     `json:"order_id"`
	PublicNo  string    `json:"public_no"`
	TopupID   int64     `json:"topup_id"`
	Phone     string    `json:"phone,omitempty"`
	By        string    `json:"by,omitempty"`
	Note      string    `json:"note,omitempty"`
}

type BranchStored struct {
	Currency      string         `json:"currency"`
	Principal     int64          `json:"principal"`
	Bonus         int64          `json:"bonus"`
	MonthCashouts int            `json:"month_cashouts"`
	MonthPaidOut  int64          `json:"month_paid_out"`
	MonthVoid     int64          `json:"month_void"` // bonus made void and bonus kept back
	Holders       []StoredHolder `json:"holders"`
	Entries       []StoredEntry  `json:"entries"`
}

const storedEntrySQL = `SELECT j.id, j.created_at, j.kind,
		COALESCE(sum(p.amount) FILTER (WHERE w.kind = 'stored_principal'), 0),
		COALESCE(sum(p.amount) FILTER (WHERE w.kind = 'stored_bonus'), 0),
		COALESCE(j.order_id, 0), COALESCE(o.public_no, ''), COALESCE(j.topup_id, 0), min(c.phone),
		COALESCE(min(u.display_name), ''), j.note
	FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id JOIN wallet_accounts w ON w.id = p.account_id
	LEFT JOIN customers c ON c.id = w.customer_id LEFT JOIN orders o ON o.id = j.order_id
	LEFT JOIN users u ON j.actor_type = 'staff' AND u.id = j.actor_id
	WHERE w.kind IN ('stored_principal', 'stored_bonus') AND `

func scanStoredEntries(rows pgx.Rows, mask bool) ([]StoredEntry, error) {
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (StoredEntry, error) {
		var e StoredEntry
		var phone *string
		err := r.Scan(&e.ID, &e.At, &e.Kind, &e.Principal, &e.Bonus, &e.OrderID, &e.PublicNo, &e.TopupID, &phone, &e.By, &e.Note)
		if phone != nil && mask {
			e.Phone = maskPhone(*phone)
		}
		return e, err
	})
	if out == nil {
		out = []StoredEntry{}
	}
	return out, err
}

func (a *App) BranchStored(ctx context.Context, branchID, customerID int64) (*BranchStored, error) {
	b, err := readBranch(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	out := &BranchStored{Currency: b.Currency}
	if err := a.DB.QueryRow(ctx, `SELECT
			COALESCE((SELECT sum(balance) FROM wallet_accounts WHERE branch_id = b.id AND kind = 'stored_principal'), 0),
			COALESCE((SELECT sum(balance) FROM wallet_accounts WHERE branch_id = b.id AND kind = 'stored_bonus'), 0),
			(SELECT count(*) FROM stored_cashouts s WHERE s.branch_id = b.id
				AND (s.created_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone))::int,
			COALESCE((SELECT sum(s.paid_out) FROM stored_cashouts s WHERE s.branch_id = b.id
				AND (s.created_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone)), 0),
			COALESCE((SELECT sum(s.bonus + s.clawback) FROM stored_cashouts s WHERE s.branch_id = b.id
				AND (s.created_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone)), 0)
		FROM branches b WHERE b.id=$1`, branchID).Scan(&out.Principal, &out.Bonus, &out.MonthCashouts, &out.MonthPaidOut,
		&out.MonthVoid); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT w.customer_id, c.phone,
			COALESCE(sum(w.balance) FILTER (WHERE w.kind = 'stored_principal'), 0),
			COALESCE(sum(w.balance) FILTER (WHERE w.kind = 'stored_bonus'), 0), bool_or(w.frozen),
			(SELECT max(t.paid_at) FROM topups t WHERE t.branch_id = $1 AND t.customer_id = w.customer_id AND t.status = 'paid')
		FROM wallet_accounts w JOIN customers c ON c.id = w.customer_id
		WHERE w.branch_id = $1 AND w.kind IN ('stored_principal', 'stored_bonus') AND ($2 = 0 OR w.customer_id = $2)
		GROUP BY w.customer_id, c.phone HAVING sum(w.balance) > 0 OR $2 <> 0
		ORDER BY sum(w.balance) DESC LIMIT 500`, branchID, customerID)
	if err != nil {
		return nil, err
	}
	out.Holders, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (StoredHolder, error) {
		var h StoredHolder
		err := r.Scan(&h.CustomerID, &h.Phone, &h.Principal, &h.Bonus, &h.Frozen, &h.LastTopup)
		h.Phone = maskPhone(h.Phone)
		return h, err
	})
	if err != nil {
		return nil, err
	}
	if out.Holders == nil {
		out.Holders = []StoredHolder{}
	}
	rows, err = a.DB.Query(ctx, storedEntrySQL+`w.branch_id = $1 AND ($2 = 0 OR w.customer_id = $2)
		GROUP BY j.id, o.public_no ORDER BY j.id DESC LIMIT 300`, branchID, customerID)
	if err != nil {
		return nil, err
	}
	out.Entries, err = scanStoredEntries(rows, true)
	return out, err
}

// MyStoredEntries is how the customer's stored value at a store changed.
func (a *App) MyStoredEntries(ctx context.Context, customerID, branchID int64) ([]StoredEntry, error) {
	rows, err := a.DB.Query(ctx, storedEntrySQL+`w.customer_id = $1 AND w.branch_id = $2
		GROUP BY j.id, o.public_no ORDER BY j.id DESC LIMIT 200`, customerID, branchID)
	if err != nil {
		return nil, err
	}
	out, err := scanStoredEntries(rows, false)
	for i := range out {
		out[i].By, out[i].Phone = "", ""
	}
	return out, err
}

// ---- the platform (§9) ----

type AdminStoredBranch struct {
	BranchID     int64  `json:"branch_id"`
	Name         string `json:"name"`
	CountryCode  string `json:"country_code"`
	Currency     string `json:"currency"`
	Principal    int64  `json:"principal"`
	Bonus        int64  `json:"bonus"`
	MonthTopup   int64  `json:"month_topup"`
	MonthPaidOut int64  `json:"month_paid_out"`
	Frozen       bool   `json:"frozen"`
	Closed       bool   `json:"closed"`
	// OverCaps: what customers hold here is more than 100 times the
	// country's cap, a sign to look (§9 #8).
	Watch bool `json:"watch"`
}

func (a *App) AdminStoredBranches(ctx context.Context) ([]AdminStoredBranch, error) {
	rows, err := a.DB.Query(ctx, `SELECT b.id, b.name, upper(b.country_code), b.currency,
			COALESCE(sum(w.balance) FILTER (WHERE w.kind = 'stored_principal'), 0),
			COALESCE(sum(w.balance) FILTER (WHERE w.kind = 'stored_bonus'), 0),
			COALESCE((SELECT sum(t.pay) FROM topups t WHERE t.branch_id = b.id AND t.status = 'paid'
				AND (t.paid_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone)), 0),
			COALESCE((SELECT sum(s.paid_out) FROM stored_cashouts s WHERE s.branch_id = b.id
				AND (s.created_at AT TIME ZONE b.timezone) >= date_trunc('month', now() AT TIME ZONE b.timezone)), 0),
			COALESCE(bool_or(w.frozen AND w.balance > 0), false),
			b.deleted_at IS NOT NULL OR EXISTS (SELECT 1 FROM merchants m WHERE m.id = b.merchant_id AND (m.deleted_at IS NOT NULL OR m.status = 'suspended')),
			COALESCE(sum(w.balance), 0) > 100 * COALESCE((SELECT NULLIF(cap, 0) FROM stored_countries WHERE country_code = upper(b.country_code)), 0)
				AND EXISTS (SELECT 1 FROM stored_countries WHERE country_code = upper(b.country_code) AND cap > 0)
		FROM branches b JOIN wallet_accounts w ON w.branch_id = b.id AND w.kind IN ('stored_principal', 'stored_bonus')
		GROUP BY b.id ORDER BY COALESCE(sum(w.balance), 0) DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AdminStoredBranch, error) {
		var s AdminStoredBranch
		return s, r.Scan(&s.BranchID, &s.Name, &s.CountryCode, &s.Currency, &s.Principal, &s.Bonus, &s.MonthTopup,
			&s.MonthPaidOut, &s.Frozen, &s.Closed, &s.Watch)
	})
	if out == nil {
		out = []AdminStoredBranch{}
	}
	return out, err
}
