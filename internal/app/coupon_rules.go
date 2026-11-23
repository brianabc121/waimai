package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// CouponBatch is how one kind of coupon is given out.
type CouponBatch struct {
	ID          int64   `json:"id"`
	Issuer      string  `json:"issuer"`
	BranchID    *int64  `json:"branch_id"`
	Kind        string  `json:"kind"`
	Source      string  `json:"source"`
	Name        string  `json:"name"`
	NameEn      string  `json:"name_en"`
	Currency    string  `json:"currency"`
	Value       int64   `json:"value"`
	Threshold   int64   `json:"threshold"`
	ValidDays   int     `json:"valid_days"`
	MinOrder    int64   `json:"min_order"`
	MonthlyCap  *int    `json:"monthly_cap"`
	TotalQuota  *int    `json:"total_quota"`
	PerCustomer *int    `json:"per_customer"`
	Budget      *int64  `json:"budget"`
	Scope       string  `json:"scope"`
	ScopeIDs    []int64 `json:"scope_branch_ids"`
	// CountryCode, Country and Province keep a platform batch to a place.
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	Province    string `json:"province"`
	IssuedCount int    `json:"issued_count"`
	Status      string `json:"status"`
}

const batchCols = `id, issuer, branch_id, kind, source, name, name_en, currency, value, threshold, valid_days, min_order,
	monthly_cap, total_quota, per_customer, budget, scope, scope_branch_ids, country_code, country, province, issued_count, status`

func (b *CouponBatch) scan() []any {
	return []any{&b.ID, &b.Issuer, &b.BranchID, &b.Kind, &b.Source, &b.Name, &b.NameEn, &b.Currency, &b.Value,
		&b.Threshold, &b.ValidDays, &b.MinOrder, &b.MonthlyCap, &b.TotalQuota, &b.PerCustomer, &b.Budget, &b.Scope,
		&b.ScopeIDs, &b.CountryCode, &b.Country, &b.Province, &b.IssuedCount, &b.Status}
}

func pickName(en, zh string) string {
	if en != "" {
		return en
	}
	return zh
}

var currencySymbols = map[string]string{"PHP": "₱", "CNY": "¥", "USD": "$", "SGD": "S$", "MYR": "RM", "THB": "฿",
	"HKD": "HK$", "TWD": "NT$", "IDR": "Rp", "VND": "₫", "JPY": "¥", "KRW": "₩", "EUR": "€", "GBP": "£", "AUD": "A$"}

// fmtMoney writes a minor-unit amount the short way: ₱20, ₱12.50.
func fmtMoney(v int64, currency string) string {
	sym, ok := currencySymbols[currency]
	if !ok {
		sym = currency + " "
	}
	s := strconv.FormatInt(v/100, 10)
	if r := v % 100; r != 0 {
		s += "." + strings.TrimRight(strconv.FormatInt(100+r, 10)[1:], "0")
	}
	return sym + s
}

// RepurchaseRule is the store's "gift after the first order" setting with how
// it has done this month.
type RepurchaseRule struct {
	Enabled    bool   `json:"enabled"`
	Kind       string `json:"kind"`
	Value      int64  `json:"value"`
	Threshold  int64  `json:"threshold"`
	ValidDays  int    `json:"valid_days"`
	MinOrder   int64  `json:"min_order"`
	MonthlyCap *int   `json:"monthly_cap"`
	Currency   string `json:"currency"`
	Stats      struct {
		// Coupons given this month (store's clock), and what became of them.
		Issued  int   `json:"issued"`
		Used    int   `json:"used"`
		Open    int   `json:"open"`
		Expired int   `json:"expired"`
		Cost    int64 `json:"cost"`  // what the used ones took off
		Sales   int64 `json:"sales"` // item subtotal of the orders they were used on
	} `json:"stats"`
}

// RepurchaseRule returns the store's setting; a store that never set it gets
// the recommended one (₱20-style no-minimum coupon, 30 days), switched off.
func (a *App) RepurchaseRule(ctx context.Context, branchID int64) (*RepurchaseRule, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	r := &RepurchaseRule{Kind: CouponStoreFree, Value: 2000, ValidDays: 30, Currency: b.Currency}
	var bt CouponBatch
	err = a.DB.QueryRow(ctx, `SELECT `+batchCols+` FROM coupon_batches WHERE branch_id=$1 AND source='repurchase'`, branchID).
		Scan(bt.scan()...)
	if isNoRows(err) {
		return r, nil
	} else if err != nil {
		return nil, err
	}
	r.Enabled, r.Kind, r.Value, r.Threshold = bt.Status == "active", bt.Kind, bt.Value, bt.Threshold
	r.ValidDays, r.MinOrder, r.MonthlyCap = bt.ValidDays, bt.MinOrder, bt.MonthlyCap
	l := time.Now().In(b.location())
	month := time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, b.location())
	err = a.DB.QueryRow(ctx, `SELECT count(*),
			count(*) FILTER (WHERE c.status='used'),
			count(*) FILTER (WHERE c.status='unused' AND c.expires_at > now()),
			count(*) FILTER (WHERE c.status='void' OR (c.status='unused' AND c.expires_at <= now())),
			COALESCE(sum(d.amount) FILTER (WHERE c.status='used'), 0),
			COALESCE(sum(o.subtotal) FILTER (WHERE c.status='used'), 0)
		FROM customer_coupons c
		LEFT JOIN order_discounts d ON d.coupon_id = c.id
		LEFT JOIN orders o ON o.id = c.order_id
		WHERE c.batch_id=$1 AND c.issued_at >= $2`, bt.ID, month).
		Scan(&r.Stats.Issued, &r.Stats.Used, &r.Stats.Open, &r.Stats.Expired, &r.Stats.Cost, &r.Stats.Sales)
	return r, err
}

type RepurchaseInput struct {
	Enabled    bool   `json:"enabled"`
	Kind       string `json:"kind"`
	Value      int64  `json:"value"`
	Threshold  int64  `json:"threshold"`
	ValidDays  int    `json:"valid_days"`
	MinOrder   int64  `json:"min_order"`
	MonthlyCap *int   `json:"monthly_cap"`
}

func (in *RepurchaseInput) validate() error {
	switch in.Kind {
	case CouponStoreFree:
		in.Threshold = 0
		if in.Value <= 0 {
			return ErrValidation("the amount off must be greater than 0")
		}
	case CouponStoreThreshold:
		if in.Value <= 0 {
			return ErrValidation("the amount off must be greater than 0")
		}
		if in.Threshold < in.Value {
			return ErrValidation("the minimum spend must be at least the amount off")
		}
	case CouponStoreDelivery:
		if in.Value < 0 {
			return ErrValidation("the amount off cannot be negative")
		}
	default:
		return ErrValidation("kind must be store_free, store_threshold or store_delivery")
	}
	switch {
	case in.Threshold < 0, in.MinOrder < 0:
		return ErrValidation("amounts cannot be negative")
	case in.ValidDays < 1 || in.ValidDays > 90:
		return ErrValidation("the coupon must last 1 to 90 days")
	case in.MonthlyCap != nil && *in.MonthlyCap < 1:
		return ErrValidation("the monthly limit must be at least 1")
	}
	return nil
}

// SaveRepurchaseRule stores the setting. Coupons already given keep their own
// amounts and dates.
func (a *App) SaveRepurchaseRule(ctx context.Context, branchID int64, in RepurchaseInput, actor Actor) error {
	if err := in.validate(); err != nil {
		return err
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return err
	}
	status := "paused"
	if in.Enabled {
		status = "active"
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO coupon_batches (issuer, branch_id, kind, source, name, name_en, currency,
				value, threshold, valid_days, min_order, monthly_cap, status)
			VALUES ('store',$1,$2,'repurchase',$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (branch_id) WHERE source = 'repurchase' DO UPDATE SET kind=$2, name=$3, name_en=$4, currency=$5,
				value=$6, threshold=$7, valid_days=$8, min_order=$9, monthly_cap=$10, status=$11, updated_at=now()`,
			branchID, in.Kind, "复购礼券", "Welcome-back coupon", b.Currency, in.Value, in.Threshold,
			in.ValidDays, in.MinOrder, in.MonthlyCap, status); err != nil {
			return err
		}
		return audit(ctx, tx, branchID, 0, actor, "coupon.repurchase_saved", map[string]any{"input": in})
	})
}
