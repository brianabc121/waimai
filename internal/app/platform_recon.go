package app

// Platform orders and takings by order type on the reconciliation page
// (docs/ORDER_TYPES_DESIGN.md §9.5): what each delivery platform owes the
// store for a period, what it charged, what it took back, the payout to
// expect and what it actually paid; and the takings split by the five order
// types. Amounts are read from the orders, order_platform_fees and
// order_refunds; nothing is worked out again here.

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// receivableSQL is what an order brought the store: what the customer paid,
// and on a platform order the platform's own discount, which the platform
// pays the store back.
const receivableSQL = `(o.total + CASE WHEN o.order_type = 'platform' THEN o.platform_subsidy ELSE 0 END)`

// ReconPlatform is one shop's orders on a platform in the period: a brand
// on a platform ("一品湘 Grab"), since each brand is a shop of its own there.
type ReconPlatform struct {
	BrandID    int64             `json:"brand_id"`
	BrandName  string            `json:"brand_name"`
	PlatformID int64             `json:"platform_id"`
	Name       string            `json:"name"`
	Names      map[string]string `json:"names"`
	Prefix     string            `json:"prefix"`
	Color      string            `json:"color"`
	Count      int               `json:"count"`
	// Subtotal is the dishes at platform prices; Merchant the store's own
	// offer on the platform; Subsidy the platform's discount; Paid what the
	// customers paid; Receivable Paid + Subsidy.
	Subtotal   int64 `json:"subtotal"`
	Merchant   int64 `json:"merchant"`
	Subsidy    int64 `json:"subsidy"`
	Paid       int64 `json:"paid"`
	Receivable int64 `json:"receivable"`
	Commission int64 `json:"commission"`
	OtherFees  int64 `json:"other_fees"`
	// Deducted is what the platform took back in the period (recorded deductions).
	Deducted int64 `json:"deducted"`
	// Payout = Receivable − Commission − OtherFees − Deducted.
	Payout int64 `json:"payout"`
	// Actual is what the platform paid for statements inside the period, as
	// typed in; Diff is Actual − Payout. Both are absent when nothing was typed.
	Actual  *int64           `json:"actual"`
	Diff    *int64           `json:"diff"`
	Payouts []PlatformPayout `json:"payouts"`
}

// PlatformPayout is one statement of a platform: what it paid for a period.
type PlatformPayout struct {
	ID         int64     `json:"id"`
	BrandID    int64     `json:"brand_id"`
	PlatformID int64     `json:"platform_id"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Amount     int64     `json:"amount"`
	Note       string    `json:"note"`
	By         string    `json:"by"`
	At         time.Time `json:"at"`
}

// ReconType is the takings of one order type (dine-in by staff or scanned,
// staff, each platform, mall, own link).
type ReconType struct {
	Type       string            `json:"type"`
	Source     string            `json:"source"` // dine-in: staff or self
	PlatformID int64             `json:"platform_id"`
	Name       string            `json:"name"`
	Names      map[string]string `json:"names"`
	Color      string            `json:"color"`
	Count      int               `json:"count"`
	Amount     int64             `json:"amount"`
}

// platformRecon fills the platform block of the reconciliation, one row a
// brand's shop on a platform.
func platformRecon(ctx context.Context, q Querier, branchID int64, brands []int64, from, to string) ([]ReconPlatform, error) {
	out := []ReconPlatform{}
	type key struct{ brand, platform int64 }
	at := map[key]int{}
	row := func(brand int64, brandName string, id int64, name, names, prefix, color string) *ReconPlatform {
		if i, ok := at[key{brand, id}]; ok {
			return &out[i]
		}
		at[key{brand, id}] = len(out)
		out = append(out, ReconPlatform{BrandID: brand, BrandName: brandName, PlatformID: id, Name: name, Names: jsonNames(names),
			Prefix: prefix, Color: color, Payouts: []PlatformPayout{}})
		return &out[len(out)-1]
	}
	rows, err := q.Query(ctx, `SELECT br.id, br.name, p.id, p.name, p.names::text, p.prefix, p.color, count(*), COALESCE(sum(o.subtotal), 0),
			COALESCE(sum(d.amount), 0), COALESCE(sum(o.platform_subsidy), 0), COALESCE(sum(o.total), 0),
			COALESCE(sum(f.commission), 0), COALESCE(sum(f.other), 0)
		FROM orders o JOIN branches b ON b.id = o.branch_id JOIN delivery_platforms p ON p.id = o.platform_id
			JOIN brands br ON br.id = o.brand_id
			LEFT JOIN LATERAL (SELECT sum(x.amount) AS amount FROM order_discounts x
				WHERE x.order_id = o.id AND x.kind = 'platform_merchant_promo') d ON true
			LEFT JOIN LATERAL (SELECT sum(x.amount) FILTER (WHERE x.kind = 'commission') AS commission,
				sum(x.amount) FILTER (WHERE x.kind = 'other') AS other FROM order_platform_fees x WHERE x.order_id = o.id) f ON true
		WHERE `+paidInPeriod+` AND o.order_type = 'platform'
		GROUP BY br.id, p.id ORDER BY NOT br.is_main, br.sort, br.id, p.sort, p.id`, branchID, from, to, brands)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var brand, id int64
		var brandName, name, names, prefix, color string
		var r ReconPlatform
		if err := rows.Scan(&brand, &brandName, &id, &name, &names, &prefix, &color, &r.Count, &r.Subtotal, &r.Merchant, &r.Subsidy, &r.Paid,
			&r.Commission, &r.OtherFees); err != nil {
			rows.Close()
			return nil, err
		}
		x := row(brand, brandName, id, name, names, prefix, color)
		x.Count, x.Subtotal, x.Merchant, x.Subsidy, x.Paid = r.Count, r.Subtotal, r.Merchant, r.Subsidy, r.Paid
		x.Commission, x.OtherFees = r.Commission, r.OtherFees
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// what the platforms took back in the period, whenever the order was paid
	rows, err = q.Query(ctx, `SELECT br.id, br.name, p.id, p.name, p.names::text, p.prefix, p.color, COALESCE(sum(r.amount), 0)
		FROM order_refunds r JOIN orders o ON o.id = r.order_id JOIN branches b ON b.id = r.branch_id
			JOIN delivery_platforms p ON p.id = o.platform_id JOIN brands br ON br.id = o.brand_id
		WHERE r.branch_id = $1 AND r.pay_type = 'platform'
			AND (r.created_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date
			AND ($4::bigint[] IS NULL OR o.brand_id = ANY($4))
		GROUP BY br.id, p.id`, branchID, from, to, brands)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var brand, id, amt int64
		var brandName, name, names, prefix, color string
		if err := rows.Scan(&brand, &brandName, &id, &name, &names, &prefix, &color, &amt); err != nil {
			rows.Close()
			return nil, err
		}
		row(brand, brandName, id, name, names, prefix, color).Deducted = amt
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// statements typed in for periods inside this one
	rows, err = q.Query(ctx, `SELECT x.id, br.id, br.name, p.id, p.name, p.names::text, p.prefix, p.color, to_char(x.period_from, 'YYYY-MM-DD'),
			to_char(x.period_to, 'YYYY-MM-DD'), x.amount, x.note, COALESCE(u.display_name, ''), x.updated_at
		FROM platform_payouts x JOIN delivery_platforms p ON p.id = x.platform_id JOIN brands br ON br.id = x.brand_id
			LEFT JOIN users u ON u.id = x.created_by
		WHERE x.branch_id = $1 AND x.period_from >= $2::date AND x.period_to <= $3::date
			AND ($4::bigint[] IS NULL OR x.brand_id = ANY($4))
		ORDER BY x.period_from, x.id`, branchID, from, to, brands)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p PlatformPayout
		var brandName, name, names, prefix, color string
		if err := rows.Scan(&p.ID, &p.BrandID, &brandName, &p.PlatformID, &name, &names, &prefix, &color, &p.From, &p.To, &p.Amount,
			&p.Note, &p.By, &p.At); err != nil {
			rows.Close()
			return nil, err
		}
		x := row(p.BrandID, brandName, p.PlatformID, name, names, prefix, color)
		x.Payouts = append(x.Payouts, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		x := &out[i]
		x.Receivable = x.Paid + x.Subsidy
		x.Payout = x.Receivable - x.Commission - x.OtherFees - x.Deducted
		if len(x.Payouts) > 0 {
			var actual int64
			for _, p := range x.Payouts {
				actual += p.Amount
			}
			diff := actual - x.Payout
			x.Actual, x.Diff = &actual, &diff
		}
	}
	return out, nil
}

// brandRecon is the takings by brand, of the brands in the view. Amounts
// are the same sums as the rest of the page, only grouped by brand.
func brandRecon(ctx context.Context, q Querier, branchID int64, brands []int64, from, to string) ([]ReconBrand, error) {
	rows, err := q.Query(ctx, `WITH paid AS (
			SELECT o.brand_id, count(*) AS n, sum(`+receivableSQL+`) AS amount,
				COALESCE(sum(`+receivableSQL+`) FILTER (WHERE o.order_type = 'platform'), 0) AS platform,
				COALESCE(sum((SELECT sum(x.amount) FROM order_platform_fees x WHERE x.order_id = o.id AND x.kind = 'commission')), 0) AS commission
			FROM orders o JOIN branches b ON b.id = o.branch_id WHERE `+paidInPeriod+` GROUP BY o.brand_id),
		back AS (
			SELECT o.brand_id, sum(r.amount) AS refunds
			FROM order_refunds r JOIN orders o ON o.id = r.order_id JOIN branches b ON b.id = r.branch_id
			WHERE r.branch_id = $1 AND (r.created_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date
				AND ($4::bigint[] IS NULL OR o.brand_id = ANY($4))
			GROUP BY o.brand_id)
		SELECT br.id, br.name, br.code, COALESCE(p.n, 0), COALESCE(p.amount, 0), COALESCE(k.refunds, 0),
			COALESCE(p.platform, 0), COALESCE(p.commission, 0)
		FROM brands br LEFT JOIN paid p ON p.brand_id = br.id LEFT JOIN back k ON k.brand_id = br.id
		WHERE br.branch_id = $1 AND ($4::bigint[] IS NULL OR br.id = ANY($4)) AND (p.n IS NOT NULL OR k.refunds IS NOT NULL OR br.status = 'active')
		ORDER BY NOT br.is_main, br.sort, br.id`, branchID, from, to, brands)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReconBrand{}
	for rows.Next() {
		var r ReconBrand
		if err := rows.Scan(&r.BrandID, &r.Name, &r.Code, &r.Count, &r.Amount, &r.Refunds, &r.Platform, &r.Commission); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// typeRecon splits the period's takings by order type.
func typeRecon(ctx context.Context, q Querier, branchID int64, brands []int64, from, to string) ([]ReconType, error) {
	rows, err := q.Query(ctx, `SELECT COALESCE(NULLIF(o.order_type, ''), 'web'),
			CASE WHEN o.order_type = 'dine_in' THEN COALESCE(o.dine_source, '') ELSE '' END,
			COALESCE(o.platform_id, 0), COALESCE(max(p.name), ''), COALESCE(max(p.names::text), '{}'), COALESCE(max(p.color), ''),
			count(*), COALESCE(sum(`+receivableSQL+`), 0)
		FROM orders o JOIN branches b ON b.id = o.branch_id LEFT JOIN delivery_platforms p ON p.id = o.platform_id
		WHERE `+paidInPeriod+`
		GROUP BY 1, 2, 3
		ORDER BY array_position(ARRAY['dine_in','staff','platform','mall','web'], COALESCE(NULLIF(o.order_type, ''), 'web')), 2 DESC, 3`,
		branchID, from, to, brands)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReconType{}
	for rows.Next() {
		var r ReconType
		var names string
		if err := rows.Scan(&r.Type, &r.Source, &r.PlatformID, &r.Name, &names, &r.Color, &r.Count, &r.Amount); err != nil {
			return nil, err
		}
		r.Names = jsonNames(names)
		out = append(out, r)
	}
	return out, rows.Err()
}

// PlatformReconOrder is one platform order of the period, for the rows under a platform.
type PlatformReconOrder struct {
	OrderID    int64     `json:"order_id"`
	BrandID    int64     `json:"brand_id"`
	PublicNo   string    `json:"public_no"`
	PlatformID int64     `json:"platform_id"`
	PaidAt     time.Time `json:"paid_at"`
	Status     string    `json:"status"`
	Subtotal   int64     `json:"subtotal"`
	Merchant   int64     `json:"merchant"`
	Subsidy    int64     `json:"subsidy"`
	Paid       int64     `json:"paid"`
	Receivable int64     `json:"receivable"`
	Commission int64     `json:"commission"`
	OtherFees  int64     `json:"other_fees"`
	// Deducted is everything the platform took back on the order, whenever.
	Deducted int64 `json:"deducted"`
	Payout   int64 `json:"payout"`
}

// PlatformReconOrders lists the platform orders paid in the period, of one
// platform or all (platformID 0), of the brands in the view.
func (a *App) PlatformReconOrders(ctx context.Context, branchID int64, brands []int64, from, to string, platformID int64) ([]PlatformReconOrder, error) {
	if err := billDays(from, to); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.brand_id, o.public_no, o.platform_id, o.paid_at, o.acceptance_status, o.subtotal,
			COALESCE((SELECT sum(x.amount) FROM order_discounts x WHERE x.order_id = o.id AND x.kind = 'platform_merchant_promo'), 0),
			o.platform_subsidy, o.total,
			COALESCE((SELECT sum(x.amount) FROM order_platform_fees x WHERE x.order_id = o.id AND x.kind = 'commission'), 0),
			COALESCE((SELECT sum(x.amount) FROM order_platform_fees x WHERE x.order_id = o.id AND x.kind = 'other'), 0),
			COALESCE((SELECT sum(x.amount) FROM order_refunds x WHERE x.order_id = o.id AND x.pay_type = 'platform'), 0)
		FROM orders o JOIN branches b ON b.id = o.branch_id
		WHERE `+paidInPeriod+` AND o.order_type = 'platform' AND ($5::bigint = 0 OR o.platform_id = $5)
		ORDER BY o.paid_at, o.id LIMIT 5000`, branchID, from, to, brands, platformID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlatformReconOrder{}
	for rows.Next() {
		var r PlatformReconOrder
		if err := rows.Scan(&r.OrderID, &r.BrandID, &r.PublicNo, &r.PlatformID, &r.PaidAt, &r.Status, &r.Subtotal, &r.Merchant, &r.Subsidy,
			&r.Paid, &r.Commission, &r.OtherFees, &r.Deducted); err != nil {
			return nil, err
		}
		r.Receivable = r.Paid + r.Subsidy
		r.Payout = r.Receivable - r.Commission - r.OtherFees - r.Deducted
		out = append(out, r)
	}
	return out, rows.Err()
}

// PlatformPayoutInput is a platform's statement as staff type it in.
type PlatformPayoutInput struct {
	// BrandID is the brand whose shop the statement is of; 0 is the main brand.
	BrandID    int64  `json:"brand_id"`
	PlatformID int64  `json:"platform_id"`
	From       string `json:"from"`
	To         string `json:"to"`
	Amount     *int64 `json:"amount"`
	Note       string `json:"note"`
}

// SavePlatformPayout records what a platform paid a brand's shop for a
// period; the same shop and period again replaces it.
func (a *App) SavePlatformPayout(ctx context.Context, branchID int64, in PlatformPayoutInput, actor Actor) error {
	if err := billDays(in.From, in.To); err != nil {
		return err
	}
	if in.Amount == nil || *in.Amount < -100000000 || *in.Amount > 100000000 {
		return ErrValidation("enter the amount the platform paid")
	}
	in.Note = strings.TrimSpace(in.Note)
	if len([]rune(in.Note)) > 200 {
		return ErrValidation("text too long")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM delivery_platforms WHERE id = $1)`, in.PlatformID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrValidation("choose the platform")
		}
		brand, err := loadBrand(ctx, tx, branchID, in.BrandID)
		if err == ErrNotFound {
			return ErrValidation("choose the brand")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO platform_payouts (branch_id, brand_id, platform_id, period_from, period_to, amount, note, created_by)
			VALUES ($1, $2, $3, $4::date, $5::date, $6, $7, NULLIF($8::bigint, 0))
			ON CONFLICT (branch_id, brand_id, platform_id, period_from, period_to)
			DO UPDATE SET amount = EXCLUDED.amount, note = EXCLUDED.note, created_by = EXCLUDED.created_by, updated_at = now()`,
			branchID, brand.ID, in.PlatformID, in.From, in.To, *in.Amount, in.Note, actor.ID); err != nil {
			return err
		}
		return audit(ctx, tx, branchID, 0, actor, "platform.payout_saved",
			map[string]any{"brand_id": brand.ID, "platform_id": in.PlatformID, "from": in.From, "to": in.To, "amount": *in.Amount})
	})
}

// DeletePlatformPayout removes a statement typed in by mistake.
func (a *App) DeletePlatformPayout(ctx context.Context, branchID, id int64, actor Actor) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM platform_payouts WHERE id = $1 AND branch_id = $2`, id, branchID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit(ctx, tx, branchID, 0, actor, "platform.payout_deleted", map[string]any{"id": id})
	})
}
