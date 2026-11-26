package app

// Delivery platforms (GrabFood, foodpanda...) and the stores' prices on them
// (docs/ORDER_TYPES_DESIGN.md §6). The platform administrator keeps the list;
// a store turns on the ones it sells on, with its own commission, markup and
// rounding, and may set a dish's platform price by hand.

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/brianabc121/waimai/internal/pricing"
)

type DeliveryPlatform struct {
	ID             int64             `json:"id"`
	Name           string            `json:"name"`
	Names          map[string]string `json:"names"`
	Prefix         string            `json:"prefix"`
	Countries      []string          `json:"countries"`
	Color          string            `json:"color"`
	LogoURL        string            `json:"logo_url"`
	NoHint         string            `json:"no_hint"`
	NoPattern      string            `json:"no_pattern"`
	CommissionBP   int               `json:"commission_bp"`
	CommissionBase string            `json:"commission_base"` // after_merchant, original
	MarkupBP       int               `json:"markup_bp"`
	Active         bool              `json:"active"`
	Sort           int               `json:"sort"`
	// for the platform's page: stores that turned it on
	Stores int `json:"stores"`
}

const platformCols = `p.id, p.name, p.names, p.prefix, p.countries, p.color, p.logo_url, p.no_hint, p.no_pattern,
	p.commission_bp, p.commission_base, p.markup_bp, p.active, p.sort`

func scanPlatform(row interface{ Scan(...any) error }, extra ...any) (*DeliveryPlatform, error) {
	var p DeliveryPlatform
	var names []byte
	dest := append([]any{&p.ID, &p.Name, &names, &p.Prefix, &p.Countries, &p.Color, &p.LogoURL, &p.NoHint, &p.NoPattern,
		&p.CommissionBP, &p.CommissionBase, &p.MarkupBP, &p.Active, &p.Sort}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	p.Names = map[string]string{}
	_ = json.Unmarshal(names, &p.Names)
	return &p, nil
}

// AdminPlatforms is the platform's list, stopped ones included.
func (a *App) AdminPlatforms(ctx context.Context) ([]DeliveryPlatform, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+platformCols+`,
		(SELECT count(*) FROM branch_platforms bp WHERE bp.platform_id = p.id AND bp.enabled)
		FROM delivery_platforms p ORDER BY p.sort, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryPlatform{}
	for rows.Next() {
		var p *DeliveryPlatform
		var stores int
		if p, err = scanPlatform(rows, &stores); err != nil {
			return nil, err
		}
		p.Stores = stores
		out = append(out, *p)
	}
	return out, rows.Err()
}

type PlatformInput struct {
	Name           string            `json:"name"`
	Names          map[string]string `json:"names"`
	Prefix         string            `json:"prefix"`
	Countries      []string          `json:"countries"`
	Color          string            `json:"color"`
	LogoURL        string            `json:"logo_url"`
	NoHint         string            `json:"no_hint"`
	NoPattern      string            `json:"no_pattern"`
	CommissionBP   int               `json:"commission_bp"`
	CommissionBase string            `json:"commission_base"`
	MarkupBP       int               `json:"markup_bp"`
	Active         bool              `json:"active"`
	Sort           int               `json:"sort"`
}

var (
	prefixRe = regexp.MustCompile(`^[A-Za-z0-9]{1,10}$`)
	colorRe  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// SavePlatform adds (id 0) or edits a delivery platform. A platform is never
// deleted, only stopped: old orders keep pointing at it.
func (a *App) SavePlatform(ctx context.Context, actor AdminActor, id int64, in PlatformInput) (int64, error) {
	in.Name, in.Prefix = strings.TrimSpace(in.Name), strings.TrimSpace(in.Prefix)
	in.NoHint, in.NoPattern = strings.TrimSpace(in.NoHint), strings.TrimSpace(in.NoPattern)
	in.LogoURL, in.Color = strings.TrimSpace(in.LogoURL), strings.TrimSpace(in.Color)
	if in.Name == "" || len([]rune(in.Name)) > 40 {
		return 0, ErrValidation("name is required (at most 40 characters)")
	}
	if !prefixRe.MatchString(in.Prefix) {
		return 0, ErrValidation("the prefix is 1 to 10 letters or digits")
	}
	if in.Color == "" {
		in.Color = "#00b14f"
	}
	if !colorRe.MatchString(in.Color) {
		return 0, ErrValidation("the colour is like #00b14f")
	}
	if len(in.LogoURL) > 500 || len([]rune(in.NoHint)) > 60 || len(in.NoPattern) > 200 {
		return 0, ErrValidation("text too long")
	}
	if in.NoPattern != "" {
		if _, err := regexp.Compile(in.NoPattern); err != nil {
			return 0, ErrValidation("the order number pattern is not a valid regular expression")
		}
	}
	if in.CommissionBP < 0 || in.CommissionBP > 10000 || in.MarkupBP < 0 || in.MarkupBP > 10000 {
		return 0, ErrValidation("rates are between 0% and 100%")
	}
	if in.CommissionBase == "" {
		in.CommissionBase = "after_merchant"
	}
	if in.CommissionBase != "after_merchant" && in.CommissionBase != "original" {
		return 0, ErrValidation("commission_base must be after_merchant or original")
	}
	countries := []string{}
	for _, c := range in.Countries {
		c = strings.ToUpper(strings.TrimSpace(c))
		if !countryRe.MatchString(c) {
			return 0, ErrValidation("countries are 2-letter codes")
		}
		if !slices.Contains(countries, c) {
			countries = append(countries, c)
		}
	}
	names := map[string]string{}
	for k, v := range in.Names {
		if v = strings.TrimSpace(v); v == "" {
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
		err = a.DB.QueryRow(ctx, `INSERT INTO delivery_platforms (name, names, prefix, countries, color, logo_url, no_hint,
			no_pattern, commission_bp, commission_base, markup_bp, active, sort)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, COALESCE(NULLIF($13, 0), (SELECT COALESCE(max(sort), 0) + 1 FROM delivery_platforms)))
			RETURNING id`, in.Name, raw, in.Prefix, countries, in.Color, in.LogoURL, in.NoHint, in.NoPattern,
			in.CommissionBP, in.CommissionBase, in.MarkupBP, in.Active, in.Sort).Scan(&id)
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, err = a.DB.Exec(ctx, `UPDATE delivery_platforms SET name=$2, names=$3, prefix=$4, countries=$5, color=$6,
			logo_url=$7, no_hint=$8, no_pattern=$9, commission_bp=$10, commission_base=$11, markup_bp=$12, active=$13, sort=$14
			WHERE id=$1`, id, in.Name, raw, in.Prefix, countries, in.Color, in.LogoURL, in.NoHint, in.NoPattern,
			in.CommissionBP, in.CommissionBase, in.MarkupBP, in.Active, in.Sort)
		if err == nil && tag.RowsAffected() == 0 {
			return 0, ErrNotFound
		}
	}
	if isUnique(err, "delivery_platforms_prefix") {
		return 0, Err(http.StatusConflict, "DUPLICATE", "another platform already uses this prefix")
	}
	if err != nil {
		return 0, err
	}
	return id, adminAudit(ctx, a.DB, actor, "platform.saved", "delivery_platform", id, in)
}

// ---- the store's platforms ----

// BranchPlatform is a platform the store may sell on (it covers the store's
// country), with the store's own terms. The effective fields are what is
// used: the store's value, or the platform's when the store left it empty.
type BranchPlatform struct {
	DeliveryPlatform
	Enabled       bool   `json:"enabled"`
	ShopRef       string `json:"shop_ref"`
	OwnCommission *int   `json:"own_commission_bp"`
	OwnMarkup     *int   `json:"own_markup_bp"`
	RoundStep     int64  `json:"round_step"`
	// Effective terms.
	Commission int `json:"effective_commission_bp"`
	Markup     int `json:"effective_markup_bp"`
}

// BranchPlatforms lists the active platforms of the store's country, those it
// sells on first. enabledOnly keeps those.
func (a *App) BranchPlatforms(ctx context.Context, q Querier, branchID int64, enabledOnly bool) ([]BranchPlatform, error) {
	rows, err := q.Query(ctx, `SELECT `+platformCols+`, COALESCE(bp.enabled, false), COALESCE(bp.shop_ref, ''),
		bp.commission_bp, bp.markup_bp, COALESCE(bp.round_step, 100)
		FROM delivery_platforms p JOIN branches b ON b.id = $1
		LEFT JOIN branch_platforms bp ON bp.platform_id = p.id AND bp.branch_id = b.id
		WHERE (p.active AND (cardinality(p.countries) = 0 OR b.country_code = '' OR upper(b.country_code) = ANY (p.countries)))
			OR bp.enabled
		ORDER BY COALESCE(bp.enabled, false) DESC, p.sort, p.id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BranchPlatform{}
	for rows.Next() {
		var bp BranchPlatform
		p, err := scanPlatform(rows, &bp.Enabled, &bp.ShopRef, &bp.OwnCommission, &bp.OwnMarkup, &bp.RoundStep)
		if err != nil {
			return nil, err
		}
		bp.DeliveryPlatform = *p
		bp.Commission, bp.Markup = p.CommissionBP, p.MarkupBP
		if bp.OwnCommission != nil {
			bp.Commission = *bp.OwnCommission
		}
		if bp.OwnMarkup != nil {
			bp.Markup = *bp.OwnMarkup
		}
		if !enabledOnly || bp.Enabled {
			out = append(out, bp)
		}
	}
	return out, rows.Err()
}

type BranchPlatformInput struct {
	PlatformID    int64  `json:"platform_id"`
	Enabled       bool   `json:"enabled"`
	ShopRef       string `json:"shop_ref"`
	OwnCommission *int   `json:"own_commission_bp"`
	OwnMarkup     *int   `json:"own_markup_bp"`
	RoundStep     int64  `json:"round_step"`
}

// SaveBranchPlatforms stores the store's terms on the platforms listed; the
// others stay as they are.
func (a *App) SaveBranchPlatforms(ctx context.Context, branchID int64, in []BranchPlatformInput) ([]BranchPlatform, error) {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		offered, err := a.BranchPlatforms(ctx, tx, branchID, false)
		if err != nil {
			return err
		}
		for _, x := range in {
			ok := false
			for _, p := range offered {
				ok = ok || p.ID == x.PlatformID
			}
			if !ok {
				return ErrValidation("this platform is not offered in the store's country")
			}
			x.ShopRef = strings.TrimSpace(x.ShopRef)
			if len([]rune(x.ShopRef)) > 60 {
				return ErrValidation("the shop id is at most 60 characters")
			}
			for _, r := range []*int{x.OwnCommission, x.OwnMarkup} {
				if r != nil && (*r < 0 || *r > 10000) {
					return ErrValidation("rates are between 0% and 100%")
				}
			}
			if !slices.Contains([]int64{0, 100, 500, 1000}, x.RoundStep) {
				return ErrValidation("round_step must be 0, 100, 500 or 1000")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO branch_platforms (branch_id, platform_id, enabled, shop_ref, commission_bp, markup_bp, round_step)
				VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (branch_id, platform_id) DO UPDATE SET enabled=$3, shop_ref=$4,
				commission_bp=$5, markup_bp=$6, round_step=$7`,
				branchID, x.PlatformID, x.Enabled, x.ShopRef, x.OwnCommission, x.OwnMarkup, x.RoundStep); err != nil {
				return err
			}
		}
		return branchChanged(ctx, tx, branchID)
	})
	if err != nil {
		return nil, err
	}
	return a.BranchPlatforms(ctx, a.DB, branchID, false)
}

// ---- dish prices on the platforms ----

// PlatformPrice is the dish's price on the platform: the one the store set,
// or the store price with the markup, rounded.
func (bp *BranchPlatform) PlatformPrice(storePrice int64, own *int64) int64 {
	if own != nil {
		return *own
	}
	return pricing.PlatformPrice(storePrice, bp.Markup, bp.RoundStep)
}

// itemPlatformPrices reads the prices the store set by hand, by dish and platform.
func itemPlatformPrices(ctx context.Context, q Querier, branchID int64) (map[int64]map[int64]int64, error) {
	rows, err := q.Query(ctx, `SELECT pp.item_id, pp.platform_id, pp.price FROM item_platform_prices pp
		JOIN menu_items i ON i.id = pp.item_id WHERE i.branch_id = $1`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[int64]int64{}
	for rows.Next() {
		var item, platform, price int64
		if err := rows.Scan(&item, &platform, &price); err != nil {
			return nil, err
		}
		if out[item] == nil {
			out[item] = map[int64]int64{}
		}
		out[item][platform] = price
	}
	return out, rows.Err()
}

// setItemPlatformPrices replaces the dish's hand-set platform prices.
func setItemPlatformPrices(ctx context.Context, tx pgx.Tx, itemID int64, prices map[int64]int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM item_platform_prices WHERE item_id=$1`, itemID); err != nil {
		return err
	}
	for pid, price := range prices {
		if price < 0 || price > 100_000_000 {
			return ErrValidation("platform price out of range")
		}
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM delivery_platforms WHERE id=$1)`, pid).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrValidation("unknown platform")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO item_platform_prices (item_id, platform_id, price) VALUES ($1,$2,$3)`,
			itemID, pid, price); err != nil {
			return err
		}
	}
	return nil
}
