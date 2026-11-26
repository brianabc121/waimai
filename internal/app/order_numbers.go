package app

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Order types (docs/ORDER_TYPES_DESIGN.md §3). The number an order shows
// everywhere (public_no) is built per type by nextOrderNo.
const (
	TypeDineIn   = "dine_in"  // DineIn-1, every business day
	TypeStaff    = "staff"    // store code + member number: YPX000123, YPX000123-2
	TypePlatform = "platform" // the platform's name and its own number: Grab GF-123
	TypeMall     = "mall"     // Hippo-1, every calendar day
	TypeWeb      = "web"      // Online-1, every business day
)

// OrderTypes lists the types in the order the pages show them.
var OrderTypes = []string{TypeDineIn, TypeStaff, TypePlatform, TypeMall, TypeWeb}

var typePrefix = map[string]string{TypeDineIn: "DineIn-", TypeMall: "Hippo-", TypeWeb: "Online-"}

// orderNo is what a new order is numbered and stamped with.
type orderNo struct {
	Type    string
	Public  string
	Code    string // the number part: what fits on a pickup slip or a short display
	BizDate string // YYYY-MM-DD
}

// businessDay is the business day at t: before the store's reset time it is
// still the day before.
func (b *Branch) businessDay(t time.Time) time.Time {
	local := t.In(b.location())
	h, m := 4, 0
	if hhmmRe.MatchString(b.DayResetTime) {
		fmt.Sscanf(b.DayResetTime, "%d:%d", &h, &m)
	}
	return local.Add(-time.Duration(h)*time.Hour - time.Duration(m)*time.Minute)
}

// counter takes the next number of a store's running number for kind in period.
func counter(ctx context.Context, tx pgx.Tx, branchID int64, kind, period string) (int, error) {
	var seq int
	err := tx.QueryRow(ctx, `INSERT INTO order_counters (branch_id, kind, period, seq) VALUES ($1, $2, $3::date, 1)
		ON CONFLICT (branch_id, kind, period) DO UPDATE SET seq = order_counters.seq + 1 RETURNING seq`,
		branchID, kind, period).Scan(&seq)
	return seq, err
}

// nextOrderNo numbers a new dine-in, mall or online order. A brand other
// than the main one has its own running numbers (docs/MULTI_BRAND_DESIGN.md §6).
func nextOrderNo(ctx context.Context, tx pgx.Tx, b *Branch, brand *Brand, typ string) (orderNo, error) {
	now := time.Now()
	biz := b.businessDay(now).Format("2006-01-02")
	period := biz
	if typ == TypeMall {
		period = now.In(b.location()).Format("2006-01-02") // midnight, as brian asked
	}
	prefix, ok := typePrefix[typ]
	if !ok {
		return orderNo{}, fmt.Errorf("nextOrderNo: no running number for %q", typ)
	}
	kind := typ
	if brand != nil && !brand.IsMain {
		kind = fmt.Sprintf("%s:%d", typ, brand.ID)
	}
	seq, err := counter(ctx, tx, b.ID, kind, period)
	if err != nil {
		return orderNo{}, err
	}
	code := fmt.Sprint(seq)
	return orderNo{Type: typ, Public: prefix + code, Code: code, BizDate: biz}, nil
}

// staffOrderNo numbers an order staff place for a member: the brand's code
// and the member number, and -2, -3 for the member's next orders that
// business day (docs/MULTI_BRAND_DESIGN.md §6). The main brand's code is the
// store code.
func staffOrderNo(ctx context.Context, tx pgx.Tx, b *Branch, brand *Brand, memberNo int) (orderNo, error) {
	code, kind := brand.Code, "staff:"
	if brand.IsMain {
		var err error
		if code, err = ensureStoreCode(ctx, tx, b); err != nil {
			return orderNo{}, err
		}
	} else {
		kind = fmt.Sprintf("staff:%d:", brand.ID)
	}
	biz := b.businessDay(time.Now()).Format("2006-01-02")
	no := fmt.Sprintf("%06d", memberNo)
	n, err := counter(ctx, tx, b.ID, kind+no, biz)
	if err != nil {
		return orderNo{}, err
	}
	public := code + no
	if n > 1 {
		public += fmt.Sprintf("-%d", n)
	}
	short := no
	if len(short) > 4 {
		short = short[len(short)-4:]
	}
	return orderNo{Type: TypeStaff, Public: public, Code: short, BizDate: biz}, nil
}

// stampOrder writes the type, the business day and, for dine-in, who opened it.
func stampOrder(ctx context.Context, tx pgx.Tx, orderID int64, no orderNo, dineSource string) error {
	_, err := tx.Exec(ctx, `UPDATE orders SET order_type=$2, biz_date=$3::date, dine_source=$4 WHERE id=$1`,
		orderID, no.Type, no.BizDate, dineSource)
	return err
}

var storeCodeRe = regexp.MustCompile(`^[A-Z0-9]{2,8}$`)

// ensureStoreCode gives the store a code from its slug the first time one is needed.
func ensureStoreCode(ctx context.Context, tx pgx.Tx, b *Branch) (string, error) {
	if b.StoreCode != "" {
		return b.StoreCode, nil
	}
	var code string
	err := tx.QueryRow(ctx, `SELECT store_code FROM branches WHERE id=$1 FOR UPDATE`, b.ID).Scan(&code)
	if err != nil || code != "" {
		b.StoreCode = code
		return code, err
	}
	base := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return -1
	}, b.Slug)
	if len(base) > 3 {
		base = base[:3]
	}
	if len(base) < 2 {
		base = "S" + base
	}
	for _, c := range []string{base, fmt.Sprintf("%s%d", base, b.ID)} {
		tag, err := tx.Exec(ctx, `UPDATE branches SET store_code=$2 WHERE id=$1 AND NOT EXISTS
			(SELECT 1 FROM branches o WHERE o.store_code=$2 AND o.id<>$1)`, b.ID, c)
		if err != nil {
			return "", err
		}
		if tag.RowsAffected() == 1 {
			b.StoreCode = c
			return c, nil
		}
	}
	return "", fmt.Errorf("no free store code for branch %d", b.ID)
}

// OrderNumbering is the store's setting for order numbers.
type OrderNumbering struct {
	StoreCode    string `json:"store_code"`
	DayResetTime string `json:"day_reset_time"`
}

// SaveOrderNumbering sets the store code and the daily reset time.
func (a *App) SaveOrderNumbering(ctx context.Context, branchID int64, in OrderNumbering) (*Branch, error) {
	in.StoreCode = strings.ToUpper(strings.TrimSpace(in.StoreCode))
	if !storeCodeRe.MatchString(in.StoreCode) {
		return nil, ErrValidation("the store code must be 2 to 8 letters or digits")
	}
	if !hhmmRe.MatchString(in.DayResetTime) || in.DayResetTime > "08:00" {
		return nil, ErrValidation("the reset time must be between 00:00 and 08:00")
	}
	tag, err := a.DB.Exec(ctx, `UPDATE branches SET store_code=$2, day_reset_time=$3 WHERE id=$1`,
		branchID, in.StoreCode, in.DayResetTime)
	if isUnique(err, "branches_store_code") || isUnique(err, "brands_code") {
		return nil, Err(http.StatusConflict, "STORE_CODE_TAKEN", "another store already uses this code")
	}
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return a.BranchByID(ctx, branchID)
}
