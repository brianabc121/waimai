package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The platform back office: merchants, branches, accounts and the admin audit
// trail. Every write takes the acting administrator and records what changed.

// AdminActor is the platform administrator performing a command.
type AdminActor struct {
	ID int64
	IP string
}

// restoreWindow is how long a deleted merchant can be brought back.
const restoreWindow = 30 * 24 * time.Hour

func adminAudit(ctx context.Context, q Querier, actor AdminActor, action, targetType string, targetID int64, detail any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO admin_audit_logs (admin_id, action, target_type, target_id, detail, ip)
		VALUES ($1,$2,$3,NULLIF($4::bigint, 0),$5,$6)`, actor.ID, action, targetType, targetID, b, actor.IP)
	return err
}

// Page is the paging part of a list request.
type Page struct {
	Page     int
	PageSize int
}

func (p Page) limitOffset() (int, int) {
	size := p.PageSize
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	page := p.Page
	if page < 1 {
		page = 1
	}
	return size, (page - 1) * size
}

// like turns free text into a case-insensitive substring pattern.
func like(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

// ---- merchants ----

type NewMerchant struct {
	MerchantName string  `json:"merchant_name"`
	ContactName  string  `json:"contact_name"`
	ContactPhone string  `json:"contact_phone"`
	Note         string  `json:"note"`
	BranchName   string  `json:"branch_name"`
	BranchNameEn string  `json:"branch_name_en"`
	Slug         string  `json:"slug"`
	Timezone     string  `json:"timezone"`
	Currency     string  `json:"currency"`
	Owner        NewUser `json:"owner"`
}

type MerchantSummary struct {
	MerchantID   int64  `json:"merchant_id"`
	MerchantName string `json:"merchant_name"`
	BranchID     int64  `json:"branch_id"`
	BranchName   string `json:"branch_name"`
	Slug         string `json:"slug"`
}

// BranchInput is what the platform edits on a branch. Everything else is the
// merchant's own settings page.
type BranchInput struct {
	Name     string `json:"name"`
	NameEn   string `json:"name_en"`
	Slug     string `json:"slug"`
	Timezone string `json:"timezone"`
	Currency string `json:"currency"`
	Address  string `json:"address"`
	Phone    string `json:"phone"`
}

func (in *BranchInput) normalize() error {
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	in.Name = strings.TrimSpace(in.Name)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Name == "" {
		return ErrValidation("branch name is required")
	}
	if !slugRe.MatchString(in.Slug) {
		return ErrValidation("slug must be lowercase letters, digits and dashes")
	}
	if in.Timezone == "" {
		in.Timezone = "Asia/Manila"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return ErrValidation("unknown timezone")
	}
	if in.Currency == "" {
		in.Currency = "PHP"
	}
	if len(in.Currency) != 3 {
		return ErrValidation("currency must be a 3-letter code")
	}
	return nil
}

var errSlugTaken = Err(http.StatusConflict, "SLUG_TAKEN", "this store link is already in use")

func insertBranch(ctx context.Context, tx pgx.Tx, merchantID int64, in BranchInput) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO branches (merchant_id, slug, name, name_en, timezone, currency, address, phone)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (slug) DO NOTHING RETURNING id`,
		merchantID, in.Slug, in.Name, in.NameEn, in.Timezone, in.Currency, in.Address, in.Phone).Scan(&id)
	if isNoRows(err) {
		return 0, errSlugTaken
	}
	return id, err
}

func createMerchant(ctx context.Context, tx pgx.Tx, in NewMerchant) (*MerchantSummary, error) {
	in.MerchantName = strings.TrimSpace(in.MerchantName)
	if in.MerchantName == "" {
		return nil, ErrValidation("merchant_name and branch_name are required")
	}
	br := BranchInput{Name: in.BranchName, NameEn: in.BranchNameEn, Slug: in.Slug, Timezone: in.Timezone, Currency: in.Currency}
	if err := br.normalize(); err != nil {
		return nil, err
	}
	in.Owner.Role = "owner"
	out := &MerchantSummary{MerchantName: in.MerchantName, BranchName: br.Name, Slug: br.Slug}
	if err := tx.QueryRow(ctx, `INSERT INTO merchants (name, contact_name, contact_phone, note) VALUES ($1,$2,$3,$4)
		RETURNING id`, in.MerchantName, strings.TrimSpace(in.ContactName), strings.TrimSpace(in.ContactPhone),
		strings.TrimSpace(in.Note)).Scan(&out.MerchantID); err != nil {
		return nil, err
	}
	var err error
	if out.BranchID, err = insertBranch(ctx, tx, out.MerchantID, br); err != nil {
		return nil, err
	}
	if _, err := createUser(ctx, tx, in.Owner, out.BranchID); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateMerchant creates a merchant, its first branch and the owner account.
// It is the unaudited form used by the seed command and tests.
func (a *App) CreateMerchant(ctx context.Context, in NewMerchant) (*MerchantSummary, error) {
	var out *MerchantSummary
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		out, err = createMerchant(ctx, tx, in)
		return err
	})
	return out, err
}

// AdminCreateMerchant is CreateMerchant performed by a platform administrator.
func (a *App) AdminCreateMerchant(ctx context.Context, actor AdminActor, in NewMerchant) (*MerchantSummary, error) {
	var out *MerchantSummary
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		if out, err = createMerchant(ctx, tx, in); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "merchant.create", "merchant", out.MerchantID, map[string]any{
			"name": out.MerchantName, "slug": out.Slug, "owner": strings.ToLower(strings.TrimSpace(in.Owner.Username))})
	})
	return out, err
}

// MerchantRow is one line of the merchant list. Status is active, suspended or deleted.
type MerchantRow struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	ContactName   string     `json:"contact_name"`
	ContactPhone  string     `json:"contact_phone"`
	Note          string     `json:"note"`
	SuspendReason string     `json:"suspend_reason"`
	SuspendedAt   *time.Time `json:"suspended_at"`
	DeletedAt     *time.Time `json:"deleted_at"`
	CreatedAt     time.Time  `json:"created_at"`
	BranchCount   int        `json:"branch_count"`
	Slugs         []string   `json:"slugs"`
	OrdersToday   int        `json:"orders_today"`
	Orders7d      int        `json:"orders_7d"`
}

type MerchantFilter struct {
	Q      string
	Status string // "", active, suspended, deleted
	Page
}

type MerchantList struct {
	Items  []MerchantRow  `json:"items"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
}

// merchantStatus is the status shown to administrators; deletion wins.
const merchantStatus = `CASE WHEN m.deleted_at IS NOT NULL THEN 'deleted' ELSE m.status END`

// localDayStart is the start of today in a branch's own time zone.
const localDayStart = `(date_trunc('day', now() AT TIME ZONE b.timezone) AT TIME ZONE b.timezone)`

const merchantRowCols = `m.id, m.name, ` + merchantStatus + `, m.contact_name, m.contact_phone, m.note, m.suspend_reason,
	m.suspended_at, m.deleted_at, m.created_at,
	(SELECT count(*) FROM branches b WHERE b.merchant_id = m.id AND b.deleted_at IS NULL),
	coalesce((SELECT array_agg(b.slug ORDER BY b.id) FROM branches b WHERE b.merchant_id = m.id AND b.deleted_at IS NULL), '{}'),
	(SELECT count(*) FROM orders o JOIN branches b ON b.id = o.branch_id
		WHERE o.merchant_id = m.id AND o.created_at >= ` + localDayStart + `),
	(SELECT count(*) FROM orders o WHERE o.merchant_id = m.id AND o.created_at >= now() - interval '7 days')`

func scanMerchantRow(row interface{ Scan(...any) error }) (*MerchantRow, error) {
	var r MerchantRow
	err := row.Scan(&r.ID, &r.Name, &r.Status, &r.ContactName, &r.ContactPhone, &r.Note, &r.SuspendReason,
		&r.SuspendedAt, &r.DeletedAt, &r.CreatedAt, &r.BranchCount, &r.Slugs, &r.OrdersToday, &r.Orders7d)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return &r, err
}

// ListMerchants searches merchants by name, branch name, link or contact
// phone. Deleted merchants only show under the "deleted" filter.
func (a *App) ListMerchants(ctx context.Context, f MerchantFilter) (*MerchantList, error) {
	switch f.Status {
	case "", "active", "suspended", "deleted":
	default:
		return nil, ErrValidation("unknown status")
	}
	const match = `($1 = '' OR m.name ILIKE $1 OR m.contact_name ILIKE $1 OR m.contact_phone ILIKE $1 OR EXISTS (
		SELECT 1 FROM branches b WHERE b.merchant_id = m.id AND (b.name ILIKE $1 OR b.name_en ILIKE $1 OR b.slug ILIKE $1)))`
	const statusIs = `(CASE WHEN $2 = '' THEN m.deleted_at IS NULL ELSE ` + merchantStatus + ` = $2 END)`
	out := &MerchantList{Items: []MerchantRow{}, Counts: map[string]int{"active": 0, "suspended": 0, "deleted": 0}}
	pat := like(f.Q)

	rows, err := a.DB.Query(ctx, `SELECT `+merchantStatus+`, count(*) FROM merchants m WHERE `+match+` GROUP BY 1`, pat)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out.Counts[s] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.Counts["all"] = out.Counts["active"] + out.Counts["suspended"]
	if f.Status == "" {
		out.Total = out.Counts["all"]
	} else {
		out.Total = out.Counts[f.Status]
	}

	limit, offset := f.limitOffset()
	rows, err = a.DB.Query(ctx, `SELECT `+merchantRowCols+` FROM merchants m WHERE `+match+` AND `+statusIs+`
		ORDER BY m.id DESC LIMIT $3 OFFSET $4`, pat, f.Status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanMerchantRow(rows)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *r)
	}
	return out, rows.Err()
}

// AdminBranch is a branch as the platform sees it.
type AdminBranch struct {
	ID           int64  `json:"id"`
	MerchantID   int64  `json:"merchant_id"`
	MerchantName string `json:"merchant_name"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	NameEn       string `json:"name_en"`
	Timezone     string `json:"timezone"`
	Currency     string `json:"currency"`
	// CountryCode is where the store's pin is; when its currency is known
	// the store's currency follows it and cannot be typed.
	CountryCode string     `json:"country_code"`
	Address     string     `json:"address"`
	Phone       string     `json:"phone"`
	Paused      bool       `json:"paused"`
	OpenNow     bool       `json:"open_now"`
	DeletedAt   *time.Time `json:"deleted_at"`
	OrdersToday int        `json:"orders_today"`
	ItemsOnSale int        `json:"items_on_sale"`
}

const adminBranchCols = `b.id, b.merchant_id, m.name, b.slug, b.name, b.name_en, b.timezone, b.currency, b.country_code, b.address, b.phone,
	b.paused, b.open_time, b.close_time, b.deleted_at,
	(SELECT count(*) FROM orders o WHERE o.branch_id = b.id AND o.created_at >= ` + localDayStart + `),
	(SELECT count(*) FROM menu_items i WHERE i.branch_id = b.id AND NOT i.archived AND i.published)`

func scanAdminBranch(row interface{ Scan(...any) error }) (*AdminBranch, error) {
	var b AdminBranch
	var open, closeAt string
	err := row.Scan(&b.ID, &b.MerchantID, &b.MerchantName, &b.Slug, &b.Name, &b.NameEn, &b.Timezone, &b.Currency, &b.CountryCode,
		&b.Address, &b.Phone, &b.Paused, &open, &closeAt, &b.DeletedAt, &b.OrdersToday, &b.ItemsOnSale)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	hours := Branch{Timezone: b.Timezone, OpenTime: open, CloseTime: closeAt}
	b.OpenNow = hours.isOpen(time.Now())
	return &b, nil
}

// AdminBranchByID returns a branch that is not deleted, for the platform.
func (a *App) AdminBranchByID(ctx context.Context, id int64) (*AdminBranch, error) {
	return scanAdminBranch(a.DB.QueryRow(ctx, `SELECT `+adminBranchCols+` FROM branches b
		JOIN merchants m ON m.id = b.merchant_id WHERE b.id=$1 AND b.deleted_at IS NULL AND m.deleted_at IS NULL`, id))
}

// ListBranchOptions lists every live branch, for the pickers of the back office.
func (a *App) ListBranchOptions(ctx context.Context) ([]AdminBranch, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+adminBranchCols+` FROM branches b JOIN merchants m ON m.id = b.merchant_id
		WHERE b.deleted_at IS NULL AND m.deleted_at IS NULL ORDER BY m.id, b.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminBranch{}
	for rows.Next() {
		b, err := scanAdminBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

type UserRole struct {
	BranchID   int64  `json:"branch_id"`
	BranchName string `json:"branch_name"`
	Role       string `json:"role"`
}

type MerchantUser struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Phone       string     `json:"phone"`
	Active      bool       `json:"active"`
	LastLoginAt *time.Time `json:"last_login_at"`
	Roles       []UserRole `json:"roles"`
}

type Revenue struct {
	Currency string `json:"currency"`
	Amount   int64  `json:"amount"`
}

type MerchantStats struct {
	OrdersToday map[string]int `json:"orders_today"` // delivery, pickup, dine_in
	Orders7d    int            `json:"orders_7d"`
	Completed7d int            `json:"completed_7d"`
	Revenue7d   []Revenue      `json:"revenue_7d"`
}

type MerchantDetail struct {
	Merchant *MerchantRow   `json:"merchant"`
	Stats    MerchantStats  `json:"stats"`
	Branches []AdminBranch  `json:"branches"`
	Users    []MerchantUser `json:"users"`
	// Reports are customers' complaints about this merchant's stores, newest first.
	Reports []StoreReport `json:"reports"`
}

func (a *App) merchantRow(ctx context.Context, q Querier, id int64) (*MerchantRow, error) {
	return scanMerchantRow(q.QueryRow(ctx, `SELECT `+merchantRowCols+` FROM merchants m WHERE m.id=$1`, id))
}

func (a *App) GetMerchant(ctx context.Context, id int64) (*MerchantDetail, error) {
	m, err := a.merchantRow(ctx, a.DB, id)
	if err != nil {
		return nil, err
	}
	d := &MerchantDetail{Merchant: m, Branches: []AdminBranch{}, Users: []MerchantUser{},
		Stats: MerchantStats{OrdersToday: map[string]int{"delivery": 0, "pickup": 0, "dine_in": 0}, Revenue7d: []Revenue{}}}

	rows, err := a.DB.Query(ctx, `SELECT `+adminBranchCols+` FROM branches b JOIN merchants m ON m.id = b.merchant_id
		WHERE b.merchant_id=$1 AND b.deleted_at IS NULL ORDER BY b.id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		b, err := scanAdminBranch(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		d.Branches = append(d.Branches, *b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = a.DB.Query(ctx, `SELECT CASE WHEN o.channel = 'dine_in' THEN 'dine_in' ELSE o.service_type END, count(*)
		FROM orders o JOIN branches b ON b.id = o.branch_id
		WHERE o.merchant_id=$1 AND o.created_at >= `+localDayStart+` GROUP BY 1`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return nil, err
		}
		d.Stats.OrdersToday[k] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := a.DB.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE completed_at IS NOT NULL) FROM orders
		WHERE merchant_id=$1 AND created_at >= now() - interval '7 days'`, id).
		Scan(&d.Stats.Orders7d, &d.Stats.Completed7d); err != nil {
		return nil, err
	}
	rows, err = a.DB.Query(ctx, `SELECT currency, sum(total) FROM orders
		WHERE merchant_id=$1 AND completed_at >= now() - interval '7 days' GROUP BY currency ORDER BY 2 DESC`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r Revenue
		if err := rows.Scan(&r.Currency, &r.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		d.Stats.Revenue7d = append(d.Stats.Revenue7d, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = a.DB.Query(ctx, `SELECT u.id, u.username, u.display_name, u.phone, u.active, u.last_login_at,
		b.id, b.name, r.role FROM user_branch_roles r JOIN users u ON u.id = r.user_id JOIN branches b ON b.id = r.branch_id
		WHERE b.merchant_id=$1 AND b.deleted_at IS NULL ORDER BY u.id, b.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u MerchantUser
		var r UserRole
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Phone, &u.Active, &u.LastLoginAt,
			&r.BranchID, &r.BranchName, &r.Role); err != nil {
			return nil, err
		}
		if n := len(d.Users); n > 0 && d.Users[n-1].ID == u.ID {
			d.Users[n-1].Roles = append(d.Users[n-1].Roles, r)
			continue
		}
		u.Roles = []UserRole{r}
		d.Users = append(d.Users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	d.Reports, err = a.MerchantReports(ctx, id)
	return d, err
}

type MerchantInput struct {
	Name         string `json:"name"`
	ContactName  string `json:"contact_name"`
	ContactPhone string `json:"contact_phone"`
	Note         string `json:"note"`
}

func (a *App) UpdateMerchant(ctx context.Context, actor AdminActor, id int64, in MerchantInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return ErrValidation("name is required")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var before MerchantInput
		err := tx.QueryRow(ctx, `SELECT name, contact_name, contact_phone, note FROM merchants
			WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).
			Scan(&before.Name, &before.ContactName, &before.ContactPhone, &before.Note)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE merchants SET name=$2, contact_name=$3, contact_phone=$4, note=$5,
			updated_at=now() WHERE id=$1`, id, in.Name, strings.TrimSpace(in.ContactName),
			strings.TrimSpace(in.ContactPhone), strings.TrimSpace(in.Note)); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "merchant.update", "merchant", id, map[string]any{"before": before, "after": in})
	})
}

// lockMerchant locks the merchant row and returns its state.
func lockMerchant(ctx context.Context, tx pgx.Tx, id int64) (name, status string, deletedAt *time.Time, err error) {
	err = tx.QueryRow(ctx, `SELECT name, status, deleted_at FROM merchants WHERE id=$1 FOR UPDATE`, id).
		Scan(&name, &status, &deletedAt)
	if isNoRows(err) {
		err = ErrNotFound
	}
	return
}

// merchantChanged tells every open screen of the merchant to re-read.
func merchantChanged(ctx context.Context, tx pgx.Tx, merchantID int64) error {
	rows, err := tx.Query(ctx, `SELECT id FROM branches WHERE merchant_id=$1`, merchantID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := branchChanged(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

// SuspendMerchant stops new customer orders for every branch of a merchant.
// Staff keep their access so that orders already in progress can be finished.
func (a *App) SuspendMerchant(ctx context.Context, actor AdminActor, id int64, reason string) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		_, status, deletedAt, err := lockMerchant(ctx, tx, id)
		if err != nil {
			return err
		}
		if deletedAt != nil {
			return ErrNotFound
		}
		if status == "suspended" {
			return nil
		}
		reason = strings.TrimSpace(reason)
		if _, err := tx.Exec(ctx, `UPDATE merchants SET status='suspended', suspend_reason=$2, suspended_at=now(),
			updated_at=now() WHERE id=$1`, id, reason); err != nil {
			return err
		}
		if err := merchantChanged(ctx, tx, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "merchant.suspend", "merchant", id, map[string]any{"reason": reason})
	})
}

func (a *App) ResumeMerchant(ctx context.Context, actor AdminActor, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		_, status, deletedAt, err := lockMerchant(ctx, tx, id)
		if err != nil {
			return err
		}
		if deletedAt != nil {
			return ErrNotFound
		}
		if status == "active" {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE merchants SET status='active', suspend_reason='', suspended_at=NULL,
			updated_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		if err := merchantChanged(ctx, tx, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "merchant.resume", "merchant", id, nil)
	})
}

// openWork counts what would be left unfinished: orders that are neither
// completed nor closed, and dine-in tables that are still open.
const openWork = `(SELECT count(*) FROM orders o WHERE o.branch_id = b.id AND o.completed_at IS NULL AND o.closed_at IS NULL)
	+ (SELECT count(*) FROM dining_sessions s WHERE s.branch_id = b.id AND s.status = 'open')`

var errOrdersInProgress = Err(http.StatusConflict, "ORDERS_IN_PROGRESS", "there are orders still in progress")

// dropSessions signs out everyone who no longer has a live branch after a delete.
func dropSessions(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `DELETE FROM sessions s USING users u WHERE u.id = s.user_id AND NOT u.is_platform_admin
		AND NOT EXISTS (SELECT 1 FROM user_branch_roles r JOIN branches b ON b.id = r.branch_id
			JOIN merchants m ON m.id = b.merchant_id
			WHERE r.user_id = u.id AND b.deleted_at IS NULL AND m.deleted_at IS NULL)`)
	return err
}

// DeleteMerchant soft-deletes a suspended merchant with nothing in progress.
// Store links stop working and its staff lose access; orders and cash records
// stay. confirmName must repeat the merchant's name.
func (a *App) DeleteMerchant(ctx context.Context, actor AdminActor, id int64, confirmName string) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		name, status, deletedAt, err := lockMerchant(ctx, tx, id)
		if err != nil {
			return err
		}
		if deletedAt != nil {
			return ErrNotFound
		}
		if strings.TrimSpace(confirmName) != name {
			return ErrValidation("type the merchant name to confirm")
		}
		if status != "suspended" {
			return ErrInvalidState("suspend the merchant before deleting it")
		}
		var open int
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(`+openWork+`), 0) FROM branches b WHERE b.merchant_id=$1`, id).
			Scan(&open); err != nil {
			return err
		}
		if open > 0 {
			return errOrdersInProgress
		}
		if _, err := tx.Exec(ctx, `UPDATE merchants SET deleted_at=now(), updated_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		if err := dropSessions(ctx, tx); err != nil {
			return err
		}
		if err := merchantChanged(ctx, tx, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "merchant.delete", "merchant", id, map[string]any{"name": name})
	})
}

// RestoreMerchant brings a deleted merchant back, still suspended.
func (a *App) RestoreMerchant(ctx context.Context, actor AdminActor, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		name, _, deletedAt, err := lockMerchant(ctx, tx, id)
		if err != nil {
			return err
		}
		if deletedAt == nil {
			return ErrInvalidState("this merchant is not deleted")
		}
		if time.Since(*deletedAt) > restoreWindow {
			return ErrInvalidState("this merchant was deleted more than 30 days ago")
		}
		if _, err := tx.Exec(ctx, `UPDATE merchants SET deleted_at=NULL, updated_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "merchant.restore", "merchant", id, map[string]any{"name": name})
	})
}

// ---- branches ----

// CreateBranch adds a branch to a merchant. The merchant's owners become
// owners of the new branch.
func (a *App) CreateBranch(ctx context.Context, actor AdminActor, merchantID int64, in BranchInput) (int64, error) {
	if err := in.normalize(); err != nil {
		return 0, err
	}
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		_, _, deletedAt, err := lockMerchant(ctx, tx, merchantID)
		if err != nil {
			return err
		}
		if deletedAt != nil {
			return ErrNotFound
		}
		if id, err = insertBranch(ctx, tx, merchantID, in); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_branch_roles (user_id, branch_id, role)
			SELECT DISTINCT r.user_id, $2::bigint, 'owner' FROM user_branch_roles r JOIN branches b ON b.id = r.branch_id
			WHERE b.merchant_id=$1 AND b.deleted_at IS NULL AND r.role='owner'`, merchantID, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "branch.create", "branch", id, in)
	})
	return id, err
}

func (a *App) UpdateBranch(ctx context.Context, actor AdminActor, id int64, in BranchInput) error {
	if err := in.normalize(); err != nil {
		return err
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var before BranchInput
		var country string
		err := tx.QueryRow(ctx, `SELECT b.name, b.name_en, b.slug, b.timezone, b.currency, b.address, b.phone, b.country_code
			FROM branches b JOIN merchants m ON m.id = b.merchant_id
			WHERE b.id=$1 AND b.deleted_at IS NULL AND m.deleted_at IS NULL FOR UPDATE OF b`, id).
			Scan(&before.Name, &before.NameEn, &before.Slug, &before.Timezone, &before.Currency, &before.Address, &before.Phone, &country)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if in.Slug != before.Slug {
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM branches WHERE slug=$1 AND id<>$2)`, in.Slug, id).
				Scan(&taken); err != nil {
				return err
			}
			if taken {
				return errSlugTaken
			}
		}
		// A store whose country is known pays in that country's money;
		// the currency typed here only counts until its location is set.
		if c := CurrencyOf(country); c != "" {
			in.Currency = c
		}
		if _, err := tx.Exec(ctx, `UPDATE branches SET name=$2, name_en=$3, slug=$4, timezone=$5,
			currency=$6, address=$7, phone=$8 WHERE id=$1`, id, in.Name, in.NameEn, in.Slug, in.Timezone, in.Currency,
			in.Address, in.Phone); err != nil {
			return err
		}
		if err := branchChanged(ctx, tx, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "branch.update", "branch", id, map[string]any{"before": before, "after": in})
	})
}

// DeleteBranch soft-deletes a branch that has nothing in progress.
func (a *App) DeleteBranch(ctx context.Context, actor AdminActor, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var name string
		var open int
		err := tx.QueryRow(ctx, `SELECT b.name, `+openWork+` FROM branches b
			WHERE b.id=$1 AND b.deleted_at IS NULL FOR UPDATE`, id).Scan(&name, &open)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if open > 0 {
			return errOrdersInProgress
		}
		if _, err := tx.Exec(ctx, `UPDATE branches SET deleted_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		if err := dropSessions(ctx, tx); err != nil {
			return err
		}
		if err := branchChanged(ctx, tx, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "branch.delete", "branch", id, map[string]any{"name": name})
	})
}

// ---- accounts ----

// AdminCreateUser creates a staff account in a branch on the merchant's behalf.
func (a *App) AdminCreateUser(ctx context.Context, actor AdminActor, branchID int64, in NewUser) (int64, error) {
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM branches b JOIN merchants m ON m.id = b.merchant_id
			WHERE b.id=$1 AND b.deleted_at IS NULL AND m.deleted_at IS NULL)`, branchID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		var err error
		if id, err = createUser(ctx, tx, in, branchID); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "user.create", "user", id, map[string]any{
			"username": strings.ToLower(strings.TrimSpace(in.Username)), "role": in.Role, "branch_id": branchID})
	})
	return id, err
}

// staffAccount locks a merchant staff account. Platform administrators are
// not managed from the back office.
func staffAccount(ctx context.Context, tx pgx.Tx, id int64) (username string, err error) {
	var admin bool
	err = tx.QueryRow(ctx, `SELECT username, is_platform_admin FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&username, &admin)
	if isNoRows(err) || (err == nil && admin) {
		return "", ErrNotFound
	}
	return username, err
}

// AdminSetUserActive enables or disables a staff account. Disabling signs it out.
func (a *App) AdminSetUserActive(ctx context.Context, actor AdminActor, id int64, active bool) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		username, err := staffAccount(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET active=$2 WHERE id=$1`, id, active); err != nil {
			return err
		}
		if !active {
			if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
				return err
			}
		}
		action := "user.enable"
		if !active {
			action = "user.disable"
		}
		return adminAudit(ctx, tx, actor, action, "user", id, map[string]any{"username": username})
	})
}

// passwordAlphabet leaves out characters that are easy to misread.
const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

func randomPassword(n int) string {
	raw := randomBytes(n)
	out := make([]byte, n)
	for i, b := range raw {
		out[i] = passwordAlphabet[int(b)%len(passwordAlphabet)]
	}
	return string(out)
}

// AdminResetPassword sets a random password, signs the account out everywhere
// and returns the new password once. It is never logged or audited.
func (a *App) AdminResetPassword(ctx context.Context, actor AdminActor, id int64) (string, error) {
	pw := randomPassword(14)
	hash, err := hashPassword(pw)
	if err != nil {
		return "", err
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		username, err := staffAccount(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, id, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "user.reset_password", "user", id, map[string]any{"username": username})
	})
	if err != nil {
		return "", err
	}
	return pw, nil
}

// ---- audit trail ----

type AdminAuditEntry struct {
	ID         int64           `json:"id"`
	AdminID    int64           `json:"admin_id"`
	AdminName  string          `json:"admin_name"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   *int64          `json:"target_id"`
	Detail     json.RawMessage `json:"detail"`
	IP         string          `json:"ip"`
	CreatedAt  time.Time       `json:"created_at"`
}

type AuditFilter struct {
	AdminID int64
	Action  string // prefix, for example "merchant." or "item.update"
	From    *time.Time
	To      *time.Time
	Page
}

type AuditList struct {
	Items []AdminAuditEntry `json:"items"`
	Total int               `json:"total"`
}

func (a *App) ListAdminAudit(ctx context.Context, f AuditFilter) (*AuditList, error) {
	const where = `($1::bigint = 0 OR l.admin_id = $1) AND ($2 = '' OR l.action LIKE $2)
		AND ($3::timestamptz IS NULL OR l.created_at >= $3) AND ($4::timestamptz IS NULL OR l.created_at < $4)`
	prefix := ""
	if s := strings.TrimSpace(f.Action); s != "" {
		prefix = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
	}
	out := &AuditList{Items: []AdminAuditEntry{}}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM admin_audit_logs l WHERE `+where,
		f.AdminID, prefix, f.From, f.To).Scan(&out.Total); err != nil {
		return nil, err
	}
	limit, offset := f.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT l.id, l.admin_id, u.username, l.action, l.target_type, l.target_id, l.detail,
		l.ip, l.created_at FROM admin_audit_logs l JOIN users u ON u.id = l.admin_id WHERE `+where+`
		ORDER BY l.id DESC LIMIT $5 OFFSET $6`, f.AdminID, prefix, f.From, f.To, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e AdminAuditEntry
		if err := rows.Scan(&e.ID, &e.AdminID, &e.AdminName, &e.Action, &e.TargetType, &e.TargetID, &e.Detail,
			&e.IP, &e.CreatedAt); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, e)
	}
	return out, rows.Err()
}
