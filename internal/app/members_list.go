package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The member pages: a store's own members in the back office, and every
// store's members in the platform's back office.

// MemberFilter narrows the member list. BranchID 0 is every store (the
// platform only).
type MemberFilter struct {
	BranchID int64
	// BrandID narrows to one brand's members; 0 is every brand (the platform
	// only: a store's page always names a brand).
	BrandID    int64
	MerchantID int64
	Q          string
	// Status is "active", "temp" or "" for both.
	Status string
	Source string
	// Sort is "new" (newest first, the default), "recent" (last order),
	// "orders" or "spent".
	Sort string
	Page
}

// MemberRow is a member with their store and what they have ordered there:
// orders kept against the member and orders from the same phone number,
// leaving out declined and cancelled ones.
type MemberRow struct {
	*Member
	BranchID     int64      `json:"branch_id"`
	BranchName   string     `json:"branch_name"`
	BrandName    string     `json:"brand_name"`
	BrandCode    string     `json:"brand_code"`
	MerchantName string     `json:"merchant_name"`
	Currency     string     `json:"currency"`
	Orders       int        `json:"orders"`
	Spent        int64      `json:"spent"`
	LastAt       *time.Time `json:"last_at"`
	AddressCount int        `json:"address_count"`
	// Addresses is only filled in when one member is looked up.
	Addresses []MemberAddress `json:"addresses,omitempty"`
}

type MemberList struct {
	Items []MemberRow `json:"items"`
	Total int         `json:"total"`
	// Temp counts the temporary members among Total's, whatever Status says.
	Temp int `json:"temp"`
}

// memberColsM is memberCols on the alias m.
var memberColsM = func() string {
	parts := strings.Split(memberCols, ",")
	for i, p := range parts {
		parts[i] = "m." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}()

const memberRowFrom = `FROM store_members m
	JOIN branches b ON b.id = m.branch_id
	JOIN brands br ON br.id = m.brand_id
	JOIN merchants mc ON mc.id = b.merchant_id
	LEFT JOIN LATERAL (SELECT count(*) AS n, COALESCE(sum(o.total), 0) AS spent, max(o.created_at) AS last
		FROM orders o WHERE o.branch_id = m.branch_id AND o.brand_id = m.brand_id
			AND (o.member_id = m.id OR (m.phone <> '' AND o.customer_phone = m.phone))
			AND o.acceptance_status NOT IN ('rejected','cancelled')) s ON true`

const memberRowCols = `, b.id, b.name, br.name, br.code, mc.name, b.currency, s.n, s.spent, s.last,
	(SELECT count(*) FROM store_member_addresses ad WHERE ad.member_id = m.id)`

func scanMemberRow(row interface{ Scan(...any) error }) (*MemberRow, error) {
	m := &Member{}
	r := &MemberRow{Member: m}
	err := row.Scan(&m.ID, &m.BrandID, &m.No, &m.Status, &m.Phone, &m.Name, &m.Source, &m.Contact, &m.AddressLine,
		&m.AddressUnit, &m.AddressNote, &m.Lat, &m.Lng, &m.Note, &m.ExpiresAt, &m.CreatedAt, &m.PlaceName, &m.PlaceID,
		&r.BranchID, &r.BranchName, &r.BrandName, &r.BrandCode, &r.MerchantName, &r.Currency, &r.Orders, &r.Spent, &r.LastAt, &r.AddressCount)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return r, err
}

// ListMembers pages through live members (temporary ones only while their
// hour lasts).
func (a *App) ListMembers(ctx context.Context, f MemberFilter) (*MemberList, error) {
	where := []string{`(m.status = 'active' OR m.expires_at > now())`, `b.deleted_at IS NULL`}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.BranchID != 0 {
		where = append(where, "m.branch_id = "+arg(f.BranchID))
	}
	if f.MerchantID != 0 {
		where = append(where, "b.merchant_id = "+arg(f.MerchantID))
	}
	if f.BrandID != 0 {
		where = append(where, "m.brand_id = "+arg(f.BrandID))
	}
	if f.Source != "" {
		where = append(where, "m.source = "+arg(f.Source))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		if len(q) > 100 {
			q = q[:100]
		}
		cond := []string{"m.name ILIKE " + arg(like(q)), "m.contact ILIKE $" + strconv.Itoa(len(args)),
			"m.address_line ILIKE $" + strconv.Itoa(len(args)), "m.note ILIKE $" + strconv.Itoa(len(args))}
		if n, err := strconv.Atoi(strings.TrimLeft(q, "0")); err == nil && n > 0 && n <= 99999999 {
			cond = append(cond, "m.member_no = "+arg(n))
		}
		if d := onlyDigits(strings.TrimLeft(strings.NewReplacer(" ", "", "-", "").Replace(q), "+0")); d != "" {
			cond = append(cond, "m.phone LIKE "+arg("%"+d+"%"))
		}
		where = append(where, "("+strings.Join(cond, " OR ")+")")
	}
	base := strings.Join(where, " AND ")
	out := &MemberList{Items: []MemberRow{}}
	if err := a.DB.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE m.status = 'temp')
		FROM store_members m JOIN branches b ON b.id = m.branch_id WHERE `+base, args...).
		Scan(&out.Total, &out.Temp); err != nil {
		return nil, err
	}
	switch f.Status {
	case "active", "temp":
		base += " AND m.status = " + arg(f.Status)
		if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM store_members m JOIN branches b ON b.id = m.branch_id
			WHERE `+base, args...).Scan(&out.Total); err != nil {
			return nil, err
		}
	}
	order := "m.created_at DESC, m.id DESC"
	switch f.Sort {
	case "recent":
		order = "s.last DESC NULLS LAST, m.id DESC"
	case "orders":
		order = "s.n DESC, m.id DESC"
	case "spent":
		order = "s.spent DESC, m.id DESC"
	case "no":
		order = "b.id, m.member_no"
	}
	limit, offset := f.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT `+memberColsM+memberRowCols+` `+memberRowFrom+`
		WHERE `+base+` ORDER BY `+order+` LIMIT `+arg(limit)+` OFFSET `+arg(offset), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanMemberRow(rows)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *r)
	}
	return out, rows.Err()
}

// MemberDetail is one member with their store, counts and favourite dishes.
func (a *App) MemberDetail(ctx context.Context, id int64) (*MemberRow, error) {
	r, err := scanMemberRow(a.DB.QueryRow(ctx, `SELECT `+memberColsM+memberRowCols+` `+memberRowFrom+`
		WHERE m.id = $1 AND (m.status = 'active' OR m.expires_at > now())`, id))
	if err != nil {
		return nil, err
	}
	if r.Stats, err = memberStats(ctx, a.DB, r.BranchID, r.Member); err != nil {
		return nil, err
	}
	r.Addresses, err = memberAddresses(ctx, a.DB, r.Member)
	return r, err
}

// MemberByNo is MemberDetail for a store's member number.
func (a *App) MemberByNo(ctx context.Context, branchID, brandID int64, no int) (*MemberRow, error) {
	id, err := a.MemberIDByNo(ctx, branchID, brandID, no)
	if err != nil {
		return nil, err
	}
	return a.MemberDetail(ctx, id)
}

// MemberOrder is a line of a member's order history.
type MemberOrder struct {
	ID                int64     `json:"id"`
	PublicNo          string    `json:"public_no"`
	CreatedAt         time.Time `json:"created_at"`
	Channel           string    `json:"channel"`
	OrderSource       string    `json:"order_source"`
	ServiceType       string    `json:"service_type"`
	AcceptanceStatus  string    `json:"acceptance_status"`
	FulfillmentStatus string    `json:"fulfillment_status"`
	Total             int64     `json:"total"`
	Currency          string    `json:"currency"`
	Items             string    `json:"items"`
	AddressLine       string    `json:"address_line"`
	// Linked: kept against the member; otherwise it only shares the phone.
	Linked bool `json:"linked"`
}

type MemberOrders struct {
	Items []MemberOrder `json:"items"`
	Total int           `json:"total"`
}

// MemberOrderHistory lists a member's orders at their store, newest first,
// with declined and cancelled ones too.
func (a *App) MemberOrderHistory(ctx context.Context, memberID int64, p Page) (*MemberOrders, error) {
	var branchID, brandID int64
	var phone string
	err := a.DB.QueryRow(ctx, `SELECT branch_id, brand_id, phone FROM store_members WHERE id=$1`, memberID).
		Scan(&branchID, &brandID, &phone)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	mine := `o.branch_id=$1 AND (o.member_id=$2 OR ($3 <> '' AND o.customer_phone=$3)) AND o.brand_id=` +
		strconv.FormatInt(brandID, 10)
	out := &MemberOrders{Items: []MemberOrder{}}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM orders o WHERE `+mine, branchID, memberID, phone).
		Scan(&out.Total); err != nil {
		return nil, err
	}
	limit, offset := p.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.public_no, o.created_at, o.channel, o.order_source, o.service_type,
			o.acceptance_status, o.fulfillment_status, o.total, o.currency, o.address_line,
			COALESCE(o.member_id = $2, false),
			COALESCE((SELECT string_agg(i.name || ' ×' || i.qty, '、' ORDER BY i.id) FROM order_items i
				WHERE i.order_id = o.id AND i.void_reason = ''), '')
		FROM orders o WHERE `+mine+` ORDER BY o.created_at DESC, o.id DESC LIMIT $4 OFFSET $5`,
		branchID, memberID, phone, limit, offset)
	if err != nil {
		return nil, err
	}
	out.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (MemberOrder, error) {
		var o MemberOrder
		err := r.Scan(&o.ID, &o.PublicNo, &o.CreatedAt, &o.Channel, &o.OrderSource, &o.ServiceType,
			&o.AcceptanceStatus, &o.FulfillmentStatus, &o.Total, &o.Currency, &o.AddressLine, &o.Linked, &o.Items)
		return o, err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MemberIDByNo finds a live member of the store.
func (a *App) MemberIDByNo(ctx context.Context, branchID, brandID int64, no int) (int64, error) {
	brandID, err := memberBrand(ctx, a.DB, branchID, brandID)
	if err != nil {
		return 0, err
	}
	var id int64
	err = a.DB.QueryRow(ctx, `SELECT id FROM store_members WHERE brand_id=$1 AND member_no=$2 AND `+liveMember,
		brandID, no).Scan(&id)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return id, err
}

// AdminSaveMember changes a member's details from the platform's back
// office. Like the store's own form, it makes a temporary member a member.
func (a *App) AdminSaveMember(ctx context.Context, actor AdminActor, id int64, in MemberInput) (*MemberRow, error) {
	var branchID, brandID int64
	var no int
	err := a.DB.QueryRow(ctx, `SELECT branch_id, brand_id, member_no FROM store_members WHERE id=$1 AND `+liveMember, id).
		Scan(&branchID, &brandID, &no)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	in.No = no
	if err := in.clean(false); err != nil {
		return nil, err
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := upsertMember(ctx, tx, branchID, brandID, 0, in); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "member.update", "member", id,
			map[string]any{"branch_id": branchID, "member_no": no, "phone": in.Phone, "name": in.Name})
	})
	if err != nil {
		return nil, wrap("save member", err)
	}
	return a.MemberDetail(ctx, id)
}

// MemberAddress is one of the addresses a member has had orders delivered
// to, or that staff saved for them.
type MemberAddress struct {
	ID         int64     `json:"id"`
	Line       string    `json:"line"`
	Unit       string    `json:"unit"`
	Note       string    `json:"note"`
	Lat        *float64  `json:"lat"`
	Lng        *float64  `json:"lng"`
	Uses       int       `json:"uses"`
	LastUsedAt time.Time `json:"last_used_at"`
	// Current: the address now on the member (the one used last).
	Current bool `json:"current"`
	// PlaceName: the leading part of Line that names the place; PlaceID: Google's id of it.
	PlaceName string `json:"place_name"`
	PlaceID   string `json:"place_id"`
}

// keepMemberAddress saves the member's address in their address book unless
// it is there already (same address and unit, ignoring case and spaces); a
// known one takes the newer note and pin. used counts a delivery to it.
func keepMemberAddress(ctx context.Context, q Querier, m *Member, used bool) error {
	if strings.TrimSpace(m.AddressLine) == "" {
		return nil
	}
	uses := 0
	if used {
		uses = 1
	}
	_, err := q.Exec(ctx, `INSERT INTO store_member_addresses (member_id, address_line, address_unit, address_note, lat, lng, uses,
			place_name, place_id)
		VALUES ($1, btrim($2), btrim($3), $4, $5, $6, $7, $8, $9)
		ON CONFLICT (member_id, lower(btrim(address_line)), lower(btrim(address_unit))) DO UPDATE SET
			address_note = CASE WHEN EXCLUDED.address_note <> '' THEN EXCLUDED.address_note ELSE store_member_addresses.address_note END,
			lat = COALESCE(EXCLUDED.lat, store_member_addresses.lat), lng = COALESCE(EXCLUDED.lng, store_member_addresses.lng),
			place_name = CASE WHEN EXCLUDED.place_name <> '' THEN EXCLUDED.place_name ELSE store_member_addresses.place_name END,
			place_id = CASE WHEN EXCLUDED.place_id <> '' THEN EXCLUDED.place_id ELSE store_member_addresses.place_id END,
			uses = store_member_addresses.uses + EXCLUDED.uses, last_used_at = now()`,
		m.ID, m.AddressLine, m.AddressUnit, m.AddressNote, m.Lat, m.Lng, uses, m.PlaceName, m.PlaceID)
	return err
}

func memberAddresses(ctx context.Context, q Querier, m *Member) ([]MemberAddress, error) {
	rows, err := q.Query(ctx, `SELECT id, address_line, address_unit, address_note, lat, lng, uses, last_used_at,
			lower(btrim(address_line)) = lower(btrim($2)) AND lower(btrim(address_unit)) = lower(btrim($3)), place_name, place_id
		FROM store_member_addresses WHERE member_id=$1 ORDER BY last_used_at DESC, id DESC`, m.ID, m.AddressLine, m.AddressUnit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MemberAddress, error) {
		var a MemberAddress
		err := r.Scan(&a.ID, &a.Line, &a.Unit, &a.Note, &a.Lat, &a.Lng, &a.Uses, &a.LastUsedAt, &a.Current, &a.PlaceName, &a.PlaceID)
		return a, err
	})
}

// DeleteMemberAddress removes an address from a member's address book. The
// address now on the member stays: it is the one the next order fills in.
func (a *App) DeleteMemberAddress(ctx context.Context, memberID, addressID int64) error {
	tag, err := a.DB.Exec(ctx, `DELETE FROM store_member_addresses ad USING store_members m
		WHERE ad.id=$2 AND ad.member_id=$1 AND m.id=ad.member_id
			AND NOT (lower(btrim(ad.address_line)) = lower(btrim(m.address_line))
				AND lower(btrim(ad.address_unit)) = lower(btrim(m.address_unit)))`, memberID, addressID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
