package app

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

// Orders the store places for a customer who sent their order over WeChat,
// LINE, WhatsApp or the phone, and the store's own members those orders are
// kept against (docs/MANUAL_ORDER_DESIGN.md).

// memberHold is how long a number taken with "new member" stays reserved
// while nobody fills it in or orders for it.
const memberHold = time.Hour

// OrderSources are where a customer asked the store for the order.
var OrderSources = []string{"wechat", "line", "whatsapp", "phone", "other"}

type Member struct {
	ID          int64      `json:"id"`
	No          int        `json:"no"`
	Status      string     `json:"status"`
	Phone       string     `json:"phone"`
	Name        string     `json:"name"`
	Source      string     `json:"source"`
	Contact     string     `json:"contact"`
	AddressLine string     `json:"address_line"`
	AddressUnit string     `json:"address_unit"`
	AddressNote string     `json:"address_note"`
	Lat         *float64   `json:"lat"`
	Lng         *float64   `json:"lng"`
	Note        string     `json:"note"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	// Stats is only filled in when one member is looked up.
	Stats *MemberStats `json:"stats,omitempty"`
}

// MemberStats counts the orders of a member, and of the same phone number at
// this store: this week, this month (on the store's clock) and the last three
// months.
type MemberStats struct {
	Week      int        `json:"week"`
	Month     int        `json:"month"`
	Quarter   int        `json:"quarter"`
	Orders    int        `json:"orders"`
	Spent     int64      `json:"spent"`
	LastAt    *time.Time `json:"last_at"`
	Favorites []string   `json:"favorites"`
}

// MemberInput is what staff fill in. No is the member number; 0 takes a new one.
type MemberInput struct {
	No          int      `json:"no"`
	Phone       string   `json:"phone"`
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	Contact     string   `json:"contact"`
	AddressLine string   `json:"address_line"`
	AddressUnit string   `json:"address_unit"`
	AddressNote string   `json:"address_note"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	Note        string   `json:"note"`
}

const memberCols = `id, member_no, status, phone, name, source, contact, address_line, address_unit, address_note,
	lat, lng, note, expires_at, created_at`

func scanMember(row interface{ Scan(...any) error }) (*Member, error) {
	m := &Member{}
	err := row.Scan(&m.ID, &m.No, &m.Status, &m.Phone, &m.Name, &m.Source, &m.Contact, &m.AddressLine,
		&m.AddressUnit, &m.AddressNote, &m.Lat, &m.Lng, &m.Note, &m.ExpiresAt, &m.CreatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return m, err
}

// A live member: active, or a temporary one still within its hour.
const liveMember = `(status = 'active' OR expires_at > now())`

var memberPhoneRe = regexp.MustCompile(`^\+?[0-9]{6,15}$`)

func (in *MemberInput) clean(delivery bool) error {
	in.Phone = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(in.Phone))
	in.Name, in.Contact = strings.TrimSpace(in.Name), strings.TrimSpace(in.Contact)
	in.AddressLine, in.AddressUnit = strings.TrimSpace(in.AddressLine), strings.TrimSpace(in.AddressUnit)
	in.AddressNote, in.Note = strings.TrimSpace(in.AddressNote), strings.TrimSpace(in.Note)
	if in.No < 0 || in.No > 99999999 {
		return ErrValidation("invalid member number")
	}
	if !memberPhoneRe.MatchString(in.Phone) {
		return Err(http.StatusUnprocessableEntity, "PHONE_REQUIRED", "a phone number is required")
	}
	known := false
	for _, s := range OrderSources {
		known = known || s == in.Source
	}
	if !known {
		return Err(http.StatusUnprocessableEntity, "SOURCE_REQUIRED", "choose where the customer asked from")
	}
	if delivery && in.AddressLine == "" {
		return Err(http.StatusUnprocessableEntity, "ADDRESS_REQUIRED", "an address is required for delivery")
	}
	if (in.Lat == nil) != (in.Lng == nil) || (in.Lat != nil && !validLatLng(LatLng{*in.Lat, *in.Lng})) {
		return ErrValidation("invalid coordinates")
	}
	if len([]rune(in.Name)) > 80 || len([]rune(in.Contact)) > 80 || len([]rune(in.AddressLine)) > 300 ||
		len([]rune(in.AddressUnit)) > 100 || len([]rune(in.AddressNote)) > 300 || len([]rune(in.Note)) > 300 {
		return ErrValidation("text too long")
	}
	return nil
}

// recycleMembers removes temporary members nobody filled in within the hour,
// so that "new member" pressed by mistake does not leave numbers lying about.
func recycleMembers(ctx context.Context, q Querier, branchID int64) error {
	where := `status = 'temp' AND expires_at <= now()
		AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.member_id = store_members.id)`
	args := []any{}
	if branchID != 0 {
		where += ` AND branch_id = $1`
		args = append(args, branchID)
	}
	_, err := q.Exec(ctx, `DELETE FROM store_members WHERE `+where, args...)
	return err
}

// NewMember takes the store's next member number and holds it for an hour
// as a temporary member.
func (a *App) NewMember(ctx context.Context, branchID, staffID int64) (*Member, error) {
	var m *Member
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		// One number at a time per store.
		if _, err := tx.Exec(ctx, `SELECT id FROM branches WHERE id=$1 FOR UPDATE`, branchID); err != nil {
			return err
		}
		if err := recycleMembers(ctx, tx, branchID); err != nil {
			return err
		}
		var err error
		m, err = scanMember(tx.QueryRow(ctx, `INSERT INTO store_members (branch_id, member_no, status, expires_at, created_by)
			VALUES ($1, (SELECT COALESCE(MAX(member_no), 0) + 1 FROM store_members WHERE branch_id=$1), 'temp',
				now() + make_interval(secs => $2), NULLIF($3, 0))
			RETURNING `+memberCols, branchID, memberHold.Seconds(), staffID))
		return err
	})
	if err != nil {
		return nil, wrap("new member", err)
	}
	return m, nil
}

// SearchMembers finds live members of the store by number, phone or name.
func (a *App) SearchMembers(ctx context.Context, branchID int64, q string) ([]*Member, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []*Member{}, nil
	}
	digits := strings.TrimLeft(q, "+0")
	n, _ := strconv.Atoi(strings.TrimLeft(q, "0"))
	rows, err := a.DB.Query(ctx, `SELECT `+memberCols+` FROM store_members
		WHERE branch_id=$1 AND `+liveMember+` AND (
			lpad(member_no::text, 6, '0') LIKE $2 || '%' OR member_no = $3
			OR ($4 <> '' AND phone LIKE '%' || $4 || '%') OR name ILIKE '%' || $2 || '%')
		ORDER BY (member_no = $3) DESC, updated_at DESC LIMIT 10`, branchID, q, n, onlyDigits(digits))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func onlyDigits(s string) string {
	if len(s) < 3 {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}

// GetMember looks one member up by number, with their order counts.
func (a *App) GetMember(ctx context.Context, branchID int64, no int) (*Member, error) {
	m, err := scanMember(a.DB.QueryRow(ctx, `SELECT `+memberCols+` FROM store_members
		WHERE branch_id=$1 AND member_no=$2 AND `+liveMember, branchID, no))
	if err != nil {
		return nil, err
	}
	m.Stats, err = memberStats(ctx, a.DB, branchID, m)
	return m, err
}

func memberStats(ctx context.Context, q Querier, branchID int64, m *Member) (*MemberStats, error) {
	st := &MemberStats{Favorites: []string{}}
	const mine = `o.branch_id=$1 AND (o.member_id=$2 OR ($3 <> '' AND o.customer_phone=$3))
		AND o.acceptance_status NOT IN ('rejected','cancelled')`
	err := q.QueryRow(ctx, `SELECT
			count(*) FILTER (WHERE o.created_at >= date_trunc('week', now() AT TIME ZONE b.timezone) AT TIME ZONE b.timezone),
			count(*) FILTER (WHERE o.created_at >= date_trunc('month', now() AT TIME ZONE b.timezone) AT TIME ZONE b.timezone),
			count(*) FILTER (WHERE o.created_at >= now() - interval '3 months'),
			count(*), COALESCE(sum(o.total), 0), max(o.created_at)
		FROM orders o JOIN branches b ON b.id = o.branch_id WHERE `+mine, branchID, m.ID, m.Phone).
		Scan(&st.Week, &st.Month, &st.Quarter, &st.Orders, &st.Spent, &st.LastAt)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT i.name FROM order_items i JOIN orders o ON o.id = i.order_id
		WHERE `+mine+` AND i.void_reason = '' GROUP BY i.name ORDER BY sum(i.qty) DESC, max(o.created_at) DESC LIMIT 3`,
		branchID, m.ID, m.Phone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		st.Favorites = append(st.Favorites, name)
	}
	return st, rows.Err()
}

// SaveMember fills in a member's details: a temporary member becomes a
// member, and a number nobody has yet is created.
func (a *App) SaveMember(ctx context.Context, branchID, staffID int64, in MemberInput) (*Member, error) {
	if err := in.clean(false); err != nil {
		return nil, err
	}
	var m *Member
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		m, err = upsertMember(ctx, tx, branchID, staffID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// upsertMember saves what staff filled in against the member number: the
// member is created when the number is free (a new number when 0), and
// changed details replace the old ones.
func upsertMember(ctx context.Context, tx pgx.Tx, branchID, staffID int64, in MemberInput) (*Member, error) {
	if _, err := tx.Exec(ctx, `SELECT id FROM branches WHERE id=$1 FOR UPDATE`, branchID); err != nil {
		return nil, err
	}
	if err := recycleMembers(ctx, tx, branchID); err != nil {
		return nil, err
	}
	if in.No == 0 {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(member_no), 0) + 1 FROM store_members WHERE branch_id=$1`,
			branchID).Scan(&in.No); err != nil {
			return nil, err
		}
	}
	m, err := scanMember(tx.QueryRow(ctx, `INSERT INTO store_members (branch_id, member_no, status, phone, name, source,
			contact, address_line, address_unit, address_note, lat, lng, note, created_by)
		VALUES ($1,$2,'active',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13, 0))
		ON CONFLICT (branch_id, member_no) DO UPDATE SET status='active', expires_at=NULL, phone=EXCLUDED.phone,
			name=EXCLUDED.name, source=EXCLUDED.source, contact=EXCLUDED.contact,
			address_line=EXCLUDED.address_line, address_unit=EXCLUDED.address_unit,
			address_note=EXCLUDED.address_note, lat=EXCLUDED.lat, lng=EXCLUDED.lng, note=EXCLUDED.note,
			updated_at=now()
		RETURNING `+memberCols, branchID, in.No, in.Phone, in.Name, in.Source, in.Contact, in.AddressLine,
		in.AddressUnit, in.AddressNote, in.Lat, in.Lng, in.Note, staffID))
	if err != nil {
		return nil, err
	}
	if err := keepMemberAddress(ctx, tx, m, false); err != nil {
		return nil, err
	}
	return m, nil
}

// AddressHint is an address offered while staff type one in: one this
// phone number used before, one from the platform's address book (in the
// store's region), or one the store delivered to before.
type AddressHint struct {
	Line string   `json:"line"`
	Unit string   `json:"unit"`
	Note string   `json:"note"`
	Lat  *float64 `json:"lat"`
	Lng  *float64 `json:"lng"`
	// Kind: "mine" (this phone number), "book" (the address book) or "store".
	Kind string `json:"kind"`
	// Mine: this phone number used it before (Kind "mine").
	Mine bool `json:"mine"`
	// The delivery area and fee at the pin, when it has one.
	ZoneName string `json:"zone_name"`
	Fee      *int64 `json:"fee"`
}

// AddressHints lists, for what was typed: the phone's own addresses, then the
// address book's in the store's region, then the store's past deliveries
// not already among those.
func (a *App) AddressHints(ctx context.Context, branchID int64, q, phone string) ([]AddressHint, error) {
	q, phone = strings.TrimSpace(q), strings.TrimSpace(phone)
	if q == "" && phone == "" {
		return []AddressHint{}, nil
	}
	if len(q) > 100 {
		q = q[:100]
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT line, unit, note, lat, lng, mine, addr_key(line) FROM (
			SELECT DISTINCT ON (addr_key(line)) line, unit, note, lat, lng, mine, at FROM (
				SELECT address_line AS line, address_unit AS unit, address_note AS note, dest_lat AS lat, dest_lng AS lng,
					($3 <> '' AND customer_phone = $3) AS mine, created_at AS at
				FROM orders WHERE branch_id=$1 AND service_type='delivery' AND address_line <> ''
					AND ($2 = '' OR address_line ILIKE '%' || $2 || '%' OR addr_key(address_line) LIKE '%' || addr_key($2) || '%')
				UNION ALL
				SELECT address_line, address_unit, address_note, lat, lng, ($3 <> '' AND phone = $3), updated_at
				FROM store_members WHERE branch_id=$1 AND address_line <> '' AND `+liveMember+`
					AND ($2 = '' OR address_line ILIKE '%' || $2 || '%' OR addr_key(address_line) LIKE '%' || addr_key($2) || '%')
				UNION ALL
				SELECT a.address_line, a.address_unit, a.address_note, a.lat, a.lng, ($3 <> '' AND m.phone = $3), a.last_used_at
				FROM store_member_addresses a JOIN store_members m ON m.id = a.member_id
				WHERE m.branch_id=$1 AND `+liveMember+` AND ($2 = '' OR a.address_line ILIKE '%' || $2 || '%'
					OR addr_key(a.address_line) LIKE '%' || addr_key($2) || '%')
			) x WHERE addr_key(line) <> '' AND ($2 <> '' OR mine)
			ORDER BY addr_key(line), mine DESC, at DESC
		) y ORDER BY mine DESC, at DESC LIMIT 12`, branchID, q, phone)
	if err != nil {
		return nil, err
	}
	var mine, store []AddressHint
	seen := map[string]bool{}
	var keys []string
	for rows.Next() {
		var h AddressHint
		var key string
		if err := rows.Scan(&h.Line, &h.Unit, &h.Note, &h.Lat, &h.Lng, &h.Mine, &key); err != nil {
			rows.Close()
			return nil, err
		}
		if h.Mine {
			h.Kind = "mine"
			seen[key] = true
			mine = append(mine, h)
		} else {
			h.Kind = "store"
			keys = append(keys, key)
			store = append(store, h)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(mine) > 4 {
		mine = mine[:4]
	}
	out := mine
	if q != "" {
		book, err := searchBook(ctx, a.DB, branchRegion(b), q, 6)
		if err != nil {
			return nil, err
		}
		for _, x := range book {
			if seen[x.key] {
				continue
			}
			seen[x.key] = true
			lat, lng := x.Lat, x.Lng
			out = append(out, AddressHint{Line: x.Line, Lat: &lat, Lng: &lng, Kind: "book"})
		}
	}
	for i, h := range store {
		if len(out) >= 10 {
			break
		}
		if !seen[keys[i]] {
			seen[keys[i]] = true
			out = append(out, h)
		}
	}
	if out == nil {
		out = []AddressHint{}
	}
	zones, err := loadZones(ctx, a.DB, branchID, true)
	if err != nil {
		return nil, err
	}
	for i := range out {
		h := &out[i]
		if h.Lat == nil || h.Lng == nil {
			continue
		}
		if hit, _ := matchZone(zones, LatLng{*h.Lat, *h.Lng}, b.ZoneToleranceM); hit != nil {
			fee := hit.Zone.Fee
			h.ZoneName, h.Fee = hit.Zone.Name, &fee
		}
	}
	return out, nil
}

// ---- the order ----

// StaffLine is a dish as staff put it in: OptionIDs may name an option more
// than once for more of it.
type StaffLine struct {
	ItemID    int64   `json:"item_id"`
	Qty       int     `json:"qty"`
	OptionIDs []int64 `json:"option_ids"`
	Note      string  `json:"note"`
	// NoteTagIDs are the chosen flavour notes.
	NoteTagIDs []int64 `json:"note_tag_ids"`
}

type StaffOrderInput struct {
	Member      MemberInput `json:"member"`
	ServiceType string      `json:"service_type"`
	// ZoneID is the delivery area, for a store that picks areas from a list.
	ZoneID       *int64      `json:"zone_id"`
	Items        []StaffLine `json:"items"`
	ScheduledFor *time.Time  `json:"scheduled_for"`
	// DeliveryFee replaces the area's fee when set ("adjust delivery fee").
	DeliveryFee *int64 `json:"delivery_fee"`
	Note        string `json:"note"`
	StaffNote   string `json:"staff_note"`
	// Accept accepts the order at once, as it was taken by the store itself.
	Accept bool `json:"accept"`
	// Adjust is the discount at checkout (docs/CHECKOUT_DISCOUNT_DESIGN.md §5);
	// the caller checked who may give it.
	Adjust *Adjust `json:"adjust"`
	// Pay is set when the customer already paid online; nil means cash on
	// delivery or at the counter.
	Pay *PayInput `json:"pay"`
	// ExpectedTotal is the amount the staff member saw; another amount means
	// prices or the discount changed meanwhile.
	ExpectedTotal *int64 `json:"expected_total"`
}

// HasAdjust says whether the order comes with a discount at checkout.
func (in *StaffOrderInput) HasAdjust() bool { return !in.Adjust.empty() }

// staffAdjust adds the discount at checkout to a staff quote. Staff orders use
// no coupons, so the store's discount is the promotion alone.
func staffAdjust(ctx context.Context, q Querier, b *Branch, quote *Quote, j *Adjust) error {
	if j.empty() {
		return nil
	}
	t := quote.Totals
	cfg := b.taxConfig()
	cfg.PackagingFee = t.PackagingFee
	in := moneyIn{Fees: cfg, Delivery: t.DeliveryFee, StaffBP: j.PercentBP}
	for i, l := range quote.Lines {
		in.Lines = append(in.Lines, pricing.Line{Key: strconv.Itoa(i), Amount: l.LineTotal})
	}
	if quote.promo != nil {
		in.Promo, in.PromoOff = quote.promo, promoOn(quote.promo.Kind, t.PromotionDiscount)
	}
	if quote.pr != nil {
		in.Dish, in.DishPromos = quote.pr.dishIn, quote.pr.dishPromos
	}
	if j.Mode != "" {
		in.Round = j
	}
	p, err := priceMoney(in)
	if err != nil {
		return err
	}
	quote.Totals, quote.pr = p.Totals, p
	quote.DishDiscounts = p.dishDiscounts()
	return nil
}

func (in *StaffOrderInput) quoteInput() QuoteInput {
	none := []int64{}
	qi := QuoteInput{ServiceType: in.ServiceType, ScheduledFor: in.ScheduledFor, CouponIDs: &none, ZoneID: in.ZoneID,
		Lat: in.Member.Lat, Lng: in.Member.Lng, feeOverride: in.DeliveryFee}
	for _, l := range in.Items {
		qi.Items = append(qi.Items, QuoteLineInput{ItemID: l.ItemID, Qty: l.Qty, OptionIDs: l.OptionIDs,
			Note: strings.TrimSpace(l.Note), NoteTagIDs: l.NoteTagIDs, repeat: true})
	}
	return qi
}

func (in *StaffOrderInput) check() error {
	if in.DeliveryFee != nil && (*in.DeliveryFee < 0 || *in.DeliveryFee > 100000000) {
		return ErrValidation("invalid delivery fee")
	}
	if in.Adjust != nil {
		if err := in.Adjust.validate(); err != nil {
			return err
		}
	}
	if in.Pay != nil && in.Pay.Type != "online" {
		return ErrValidation("only an online payment can be recorded when ordering")
	}
	if len([]rune(in.Note)) > 300 || len([]rune(in.StaffNote)) > 300 {
		return ErrValidation("text too long")
	}
	return nil
}

// StaffQuote prices what staff have put in so far. No coupons are used: the
// customer's wallet is theirs to spend when they order themselves.
func (a *App) StaffQuote(ctx context.Context, branchID int64, in StaffOrderInput) (*Quote, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	phone := strings.TrimSpace(in.Member.Phone)
	q, err := computeQuote(ctx, a.DB, a.forCustomer(b), in.quoteInput(), phone)
	if err != nil {
		return nil, err
	}
	if err := staffAdjust(ctx, a.DB, b, q, in.Adjust); err != nil {
		return nil, err
	}
	return q, nil
}

// StaffOrder is the answer to placing an order for a customer.
type StaffOrder struct {
	Order       *Order  `json:"order"`
	Member      *Member `json:"member"`
	AccessToken string  `json:"access_token"`
}

type staffPlaced struct {
	OrderID int64 `json:"order_id"`
}

// PlaceStaffOrder saves the member (creating or updating them) and places the
// order for them, in one transaction. The idempotency key makes a double
// press place one order.
func (a *App) PlaceStaffOrder(ctx context.Context, branchID int64, actor Actor, idemKey string, in StaffOrderInput) (*StaffOrder, error) {
	if idemKey == "" || len(idemKey) > 100 {
		return nil, ErrValidation("Idempotency-Key header is required")
	}
	if err := in.check(); err != nil {
		return nil, err
	}
	if err := in.Member.clean(in.ServiceType == "delivery"); err != nil {
		return nil, err
	}
	var orderID, memberID int64
	var token string
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		b, err := scanBranch(tx.QueryRow(ctx, `SELECT `+branchCols+` FROM branches WHERE id=$1`, branchID))
		if err != nil {
			return err
		}
		scope := fmt.Sprintf("staff-order:%d", b.ID)
		var rec staffPlaced
		replayed, err := idemBegin(ctx, tx, scope, idemKey, hashJSON(in), &rec)
		if err != nil {
			return err
		}
		if replayed {
			var nonce []byte
			var mid *int64
			if err := tx.QueryRow(ctx, `SELECT token_nonce, member_id FROM orders WHERE id=$1`, rec.OrderID).Scan(&nonce, &mid); err != nil {
				return err
			}
			orderID, token = rec.OrderID, a.orderToken(nonce)
			if mid != nil {
				memberID = *mid
			}
			return nil
		}
		phone := in.Member.Phone
		cur, err := computeQuote(ctx, tx, a.forCustomer(b), in.quoteInput(), phone)
		if err != nil {
			return err
		}
		if err := staffAdjust(ctx, tx, b, cur, in.Adjust); err != nil {
			return err
		}
		if in.ExpectedTotal != nil && *in.ExpectedTotal != cur.Totals.Total {
			return ErrD(http.StatusConflict, "QUOTE_CHANGED", "the amount changed, please confirm it again",
				map[string]any{"total": cur.Totals.Total})
		}
		var payChannel *int64
		var payName string
		if in.Pay != nil {
			if payChannel, payName, err = resolvePay(ctx, tx, b.ID, *in.Pay); err != nil {
				return err
			}
		}
		if err := reserveStock(ctx, tx, cur.Lines); err != nil {
			return err
		}
		m, err := upsertMember(ctx, tx, b.ID, actor.ID, in.Member)
		if err != nil {
			return err
		}
		memberID = m.ID
		publicNo, pickupCode, err := nextOrderNo(ctx, tx, b, "W")
		if err != nil {
			return err
		}
		nonce := randomBytes(16)
		token = a.orderToken(nonce)
		payMethod := "cod"
		if cur.ServiceType == "pickup" {
			payMethod = "pay_at_pickup"
		}
		if payChannel != nil {
			payMethod = "prepaid"
		}
		var wechat, line, whatsapp string
		switch m.Source {
		case "wechat":
			wechat = m.Contact
		case "line":
			line = m.Contact
		case "whatsapp":
			whatsapp = m.Contact
		}
		addrLine, addrUnit, addrNote := m.AddressLine, m.AddressUnit, m.AddressNote
		var lat, lng *float64
		if cur.ServiceType == "delivery" {
			lat, lng = m.Lat, m.Lng
		} else {
			addrLine, addrUnit, addrNote = "", "", ""
		}
		if err := moneyEvent(ctx, tx, "placed", 0, actor); err != nil {
			return err
		}
		t := cur.Totals
		err = tx.QueryRow(ctx, `INSERT INTO orders (merchant_id, branch_id, public_no, pickup_code,
			access_token_hash, token_nonce, channel, service_type, customer_name, customer_phone, zone_id, zone_name,
			address_line, address_unit, address_note, note, currency, subtotal, packaging_fee, delivery_fee,
			service_charge, tax_amount, tax_inclusive, tax_rate_bp, discount, total, payment_method, accept_deadline,
			promotion_id, promotion_name, promotion_name_en, scheduled_for, release_at, dest_lat, dest_lng,
			zone_distance_m, contact_phone, contact_wechat, contact_line, contact_whatsapp,
			member_id, order_source, placed_by, staff_note)
			VALUES ($1,$2,$3,$4,$5,$6,'staff',$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,
				now() + make_interval(mins => $27), NULLIF($28, 0), $29, $30,
				$31::timestamptz, $31::timestamptz - make_interval(mins => $32), $33, $34, $35, $36, $37, $38, $39,
				$40, $41, NULLIF($42, 0), $43)
			RETURNING id`,
			b.MerchantID, b.ID, publicNo, pickupCode, hashToken(token), nonce, cur.ServiceType, m.Name, phone,
			cur.ZoneID, cur.ZoneName, addrLine, addrUnit, addrNote, strings.TrimSpace(in.Note), b.Currency,
			t.Subtotal, t.PackagingFee, t.DeliveryFee, t.ServiceCharge, t.TaxAmount, t.TaxInclusive, t.TaxRateBP,
			t.Discount, t.Total, payMethod, b.AcceptTimeoutMinutes, t.PromotionID, t.PromotionName,
			t.PromotionNameEn, cur.ScheduledFor, b.PrepMinutes, lat, lng, cur.ZoneDistanceM,
			phone, wechat, line, whatsapp, m.ID, m.Source, actor.ID, strings.TrimSpace(in.StaffNote)).Scan(&orderID)
		if err != nil {
			return wrap("insert order", err)
		}
		// A customer with an account at the platform sees the order in "My orders".
		if _, err := tx.Exec(ctx, `UPDATE orders o SET customer_id = c.id FROM customers c WHERE o.id=$1 AND c.phone=$2`,
			orderID, phone); err != nil {
			return wrap("link customer", err)
		}
		if cur.ServiceType == "delivery" {
			if err := keepMemberAddress(ctx, tx, m, true); err != nil {
				return err
			}
			// A new address with a pin goes into the platform's address book.
			if err := rememberAddress(ctx, tx, b, m.AddressLine, lat, lng); err != nil {
				return err
			}
		}
		if t.PromotionID != 0 {
			if err := claimPromotion(ctx, tx, b.ID, t.PromotionID, orderID, phone, t.PromotionDiscount); err != nil {
				return err
			}
		}
		if !in.Adjust.empty() {
			if err := writeAdjusted(ctx, tx, orderID, t, in.Adjust.Reason, actor); err != nil {
				return err
			}
			if err := audit(ctx, tx, b.ID, orderID, actor, "order.adjusted", map[string]any{"adjust": in.Adjust,
				"total_after": t.Total, "staff_discount": t.StaffDiscount, "round_off": t.RoundOff}); err != nil {
				return err
			}
		}
		if payChannel != nil {
			// Paid before it left: the rider collects nothing.
			if _, err := tx.Exec(ctx, `UPDATE orders SET payment_status='paid', pay_type='online', pay_channel_id=$2,
				pay_method=$3, amount_received=total WHERE id=$1`, orderID, *payChannel, payName); err != nil {
				return err
			}
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
				name_en, unit_price, qty, line_total, options, note, needs_prep, basket_no, name_customer, note_tags)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,'',$13) RETURNING id`, orderID, b.ID, batchID, l.ItemID, l.Name,
				l.NameEn, l.UnitPrice, l.Qty, l.LineTotal, opts, l.Note, l.NeedsPrep, noteTagsJSON(l.NoteTags)).Scan(&lineID); err != nil {
				return wrap("insert order item", err)
			}
			lineIDs = append(lineIDs, lineID)
		}
		// the amounts with the discount at checkout, the promotion's row, each line's share
		if err := saveMoney(ctx, tx, b, orderID, quotePriced(cur, lineIDs), false); err != nil {
			return err
		}
		if err := audit(ctx, tx, b.ID, orderID, actor, "order.placed_by_staff",
			map[string]any{"total": t.Total, "service_type": cur.ServiceType, "member_no": m.No, "source": m.Source}); err != nil {
			return err
		}
		if err := idemSave(ctx, tx, scope, idemKey, staffPlaced{OrderID: orderID}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: b.ID, OrderID: orderID})
	})
	if err != nil {
		return nil, wrap("place staff order", err)
	}
	if in.Accept {
		if _, err := a.Accept(ctx, orderID, 0, actor); err != nil {
			return nil, wrap("accept staff order", err)
		}
	}
	o, err := loadOrder(ctx, a.DB, orderID)
	if err != nil {
		return nil, err
	}
	m, err := scanMember(a.DB.QueryRow(ctx, `SELECT `+memberCols+` FROM store_members WHERE id=$1`, memberID))
	if err != nil && err != ErrNotFound {
		return nil, err
	}
	return &StaffOrder{Order: o, Member: m, AccessToken: token}, nil
}
