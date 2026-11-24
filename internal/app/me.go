package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// The customer's own pages: profile, orders, favourite stores, reviews,
// messages, feedback, applications to open a store and the wallet.

// MemberNo is the number a customer can quote to the platform.
func MemberNo(id int64) string { return fmt.Sprintf("M-%08d", id) }

// Wallet is what a customer holds in one currency.
type Wallet struct {
	Currency string `json:"currency"`
	Balance  int64  `json:"balance"`
	Bonus    int64  `json:"bonus"`
}

// Profile is the customer as their own page shows them.
type Profile struct {
	ID       int64      `json:"id"`
	MemberNo string     `json:"member_no"`
	Phone    string     `json:"phone"`
	Nickname string     `json:"nickname"`
	AvatarAt *time.Time `json:"avatar_at"`
	Wallets  []Wallet   `json:"wallets"`
	// Unread counts messages not opened yet; Favorites the stores kept.
	Unread    int `json:"unread"`
	Favorites int `json:"favorites"`
	// ChatUnread counts chat messages from stores not read yet.
	ChatUnread int `json:"chat_unread"`
}

func wallets(ctx context.Context, q Querier, customerID int64) ([]Wallet, error) {
	rows, err := q.Query(ctx, `SELECT currency, coalesce(sum(amount) FILTER (WHERE account='balance'), 0)::bigint,
		coalesce(sum(amount) FILTER (WHERE account='bonus'), 0)::bigint
		FROM customer_wallet_entries WHERE customer_id=$1 GROUP BY currency ORDER BY currency`, customerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Wallet, error) {
		var w Wallet
		return w, r.Scan(&w.Currency, &w.Balance, &w.Bonus)
	})
}

func (a *App) CustomerProfile(ctx context.Context, customerID int64) (*Profile, error) {
	p := &Profile{ID: customerID, MemberNo: MemberNo(customerID)}
	err := a.DB.QueryRow(ctx, `SELECT phone, nickname, avatar_at,
		(SELECT count(*) FROM customer_messages m WHERE m.customer_id = c.id AND m.read_at IS NULL),
		(SELECT count(*) FROM customer_favorites f JOIN branches b ON b.id = f.branch_id
			WHERE f.customer_id = c.id AND b.deleted_at IS NULL),
		(SELECT COALESCE(sum(t.customer_unread), 0) FROM chat_threads t WHERE t.customer_id = c.id)
		+ (SELECT COALESCE(sum(s.party_unread), 0) FROM support_threads s WHERE s.customer_id = c.id)
		FROM customers c WHERE id=$1`, customerID).Scan(&p.Phone, &p.Nickname, &p.AvatarAt, &p.Unread, &p.Favorites, &p.ChatUnread)
	if err != nil {
		return nil, err
	}
	p.Wallets, err = wallets(ctx, a.DB, customerID)
	if p.Wallets == nil {
		p.Wallets = []Wallet{}
	}
	return p, err
}

// SaveNickname sets the name the customer is greeted by. Empty goes back to
// showing the phone number.
func (a *App) SaveNickname(ctx context.Context, customerID int64, nickname string) (*Profile, error) {
	nickname = strings.Join(strings.Fields(nickname), " ")
	if utf8.RuneCountInString(nickname) > 20 {
		return nil, ErrValidation("the nickname is at most 20 characters")
	}
	if _, err := a.DB.Exec(ctx, `UPDATE customers SET nickname=$2 WHERE id=$1`, customerID, nickname); err != nil {
		return nil, err
	}
	return a.CustomerProfile(ctx, customerID)
}

// SaveAvatar stores the customer's picture; nil takes it away.
func (a *App) SaveAvatar(ctx context.Context, customerID int64, image []byte) (*Profile, error) {
	if image != nil && !isJPEG(image, ProofMaxBytes) {
		return nil, ErrValidation("the picture must be a JPEG of at most 600 KB")
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if image == nil {
			if _, err := tx.Exec(ctx, `DELETE FROM customer_avatars WHERE customer_id=$1`, customerID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE customers SET avatar_at=NULL WHERE id=$1`, customerID)
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO customer_avatars (customer_id, image) VALUES ($1,$2)
			ON CONFLICT (customer_id) DO UPDATE SET image=EXCLUDED.image, created_at=now()`, customerID, image); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE customers SET avatar_at=now() WHERE id=$1`, customerID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return a.CustomerProfile(ctx, customerID)
}

func (a *App) Avatar(ctx context.Context, customerID int64) ([]byte, error) {
	var image []byte
	err := a.DB.QueryRow(ctx, `SELECT image FROM customer_avatars WHERE customer_id=$1`, customerID).Scan(&image)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return image, err
}

// StoreRef names a store on the customer's pages.
type StoreRef struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	NameEn  string `json:"name_en"`
	LogoURL string `json:"logo_url"`
}

const storeRefCols = `b.slug, b.name, b.name_en, coalesce((SELECT t.published->>'logo_url' FROM channel_templates t
	WHERE t.branch_id = b.id AND t.channel = 'delivery'), '')`

const myPageSize = 20

// orderItems reads the dishes of the listed orders.
func orderItems(ctx context.Context, q Querier, ids []int64, add func(orderID int64, it ReviewItem, qty int)) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `SELECT order_id, name, name_en, qty FROM order_items
		WHERE order_id = ANY($1) AND prep_status <> 'cancelled' ORDER BY id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var it ReviewItem
		var qty int
		if err := rows.Scan(&id, &it.Name, &it.NameEn, &qty); err != nil {
			return err
		}
		add(id, it, qty)
	}
	return rows.Err()
}

// ---- favourite stores and the stores a customer knows ----

// MyStore is a store the customer keeps or has ordered from.
type MyStore struct {
	StoreRef
	Phone       string     `json:"phone"`
	Address     string     `json:"address"`
	Currency    string     `json:"currency"`
	Favorite    bool       `json:"favorite"`
	LastOrderAt *time.Time `json:"last_order_at"`
}

// MyStores lists the stores the customer keeps and those they ordered from,
// the most recently used first. With favoritesOnly, only the kept ones.
func (a *App) MyStores(ctx context.Context, customerID int64, favoritesOnly bool) ([]MyStore, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+storeRefCols+`, b.phone, b.address, b.currency, f.customer_id IS NOT NULL, lo.at
		FROM branches b
		LEFT JOIN customer_favorites f ON f.branch_id = b.id AND f.customer_id = $1
		LEFT JOIN (SELECT branch_id, max(created_at) AS at FROM orders WHERE customer_id = $1 GROUP BY 1) lo ON lo.branch_id = b.id
		WHERE b.deleted_at IS NULL AND EXISTS (SELECT 1 FROM merchants m WHERE m.id = b.merchant_id AND m.deleted_at IS NULL)
			AND (f.customer_id IS NOT NULL OR (lo.at IS NOT NULL AND NOT $2))
		ORDER BY greatest(lo.at, f.created_at) DESC NULLS LAST LIMIT 50`, customerID, favoritesOnly)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MyStore, error) {
		var s MyStore
		return s, r.Scan(&s.Slug, &s.Name, &s.NameEn, &s.LogoURL, &s.Phone, &s.Address, &s.Currency, &s.Favorite, &s.LastOrderAt)
	})
	if out == nil {
		out = []MyStore{}
	}
	return out, err
}

func (a *App) IsFavorite(ctx context.Context, customerID int64, slug string) (bool, error) {
	var on bool
	err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customer_favorites f JOIN branches b ON b.id = f.branch_id
		WHERE f.customer_id=$1 AND b.slug=$2)`, customerID, slug).Scan(&on)
	return on, err
}

// SetFavorite keeps a store or lets it go.
func (a *App) SetFavorite(ctx context.Context, customerID int64, slug string, on bool) error {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return err
	}
	if on {
		_, err = a.DB.Exec(ctx, `INSERT INTO customer_favorites (customer_id, branch_id) VALUES ($1,$2)
			ON CONFLICT DO NOTHING`, customerID, b.ID)
	} else {
		_, err = a.DB.Exec(ctx, `DELETE FROM customer_favorites WHERE customer_id=$1 AND branch_id=$2`, customerID, b.ID)
	}
	return err
}

// StoreOffers is what one store takes off orders right now.
type StoreOffers struct {
	Store      StoreRef    `json:"store"`
	Currency   string      `json:"currency"`
	Promotions []Promotion `json:"promotions"`
}

// MyOffers lists the running promotions of the customer's stores and of the
// store they are looking at. Promotions apply by themselves at checkout;
// there is nothing to collect.
func (a *App) MyOffers(ctx context.Context, customerID int64, slug string) ([]StoreOffers, error) {
	stores, err := a.MyStores(ctx, customerID, false)
	if err != nil {
		return nil, err
	}
	slugs := []string{}
	if slug != "" {
		slugs = append(slugs, slug)
	}
	refs := map[string]StoreRef{}
	for _, s := range stores {
		refs[s.Slug] = s.StoreRef
		if s.Slug != slug && len(slugs) < 20 {
			slugs = append(slugs, s.Slug)
		}
	}
	out := []StoreOffers{}
	for _, sl := range slugs {
		b, err := branchBySlug(ctx, a.DB, sl)
		if err != nil {
			continue // the store is gone
		}
		ps, err := livePromotions(ctx, a.DB, b, "delivery")
		if err != nil {
			return nil, err
		}
		if len(ps) == 0 {
			continue
		}
		ref, ok := refs[sl]
		if !ok {
			ref = StoreRef{Slug: b.Slug, Name: b.Name, NameEn: b.NameEn}
		}
		out = append(out, StoreOffers{Store: ref, Currency: b.Currency, Promotions: ps})
	}
	return out, nil
}

// ---- the customer's reviews ----

type MyReview struct {
	Review
	Store StoreRef `json:"store"`
	Token string   `json:"token"`
}

func (a *App) MyReviews(ctx context.Context, customerID int64) ([]MyReview, error) {
	rows, err := a.DB.Query(ctx, `SELECT r.id, r.order_id, r.display_name, r.rating, r.taste, r.packaging, r.delivery,
		r.comment, r.created_at, o.token_nonce, `+storeRefCols+`
		FROM reviews r JOIN orders o ON o.id = r.order_id JOIN branches b ON b.id = r.branch_id
		WHERE o.customer_id=$1 ORDER BY r.id DESC LIMIT 100`, customerID)
	if err != nil {
		return nil, err
	}
	out := []MyReview{}
	idx := map[int64]int{}
	ids := []int64{}
	for rows.Next() {
		r := MyReview{Review: Review{Items: []ReviewItem{}}}
		var orderID int64
		var nonce []byte
		if err := rows.Scan(&r.ID, &orderID, &r.DisplayName, &r.Rating, &r.Taste, &r.Packaging, &r.Delivery, &r.Comment,
			&r.CreatedAt, &nonce, &r.Store.Slug, &r.Store.Name, &r.Store.NameEn, &r.Store.LogoURL); err != nil {
			rows.Close()
			return nil, err
		}
		r.Token = a.orderToken(nonce)
		idx[orderID] = len(out)
		ids = append(ids, orderID)
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	err = orderItems(ctx, a.DB, ids, func(id int64, it ReviewItem, _ int) {
		r := &out[idx[id]]
		for _, x := range r.Items {
			if x.Name == it.Name {
				return
			}
		}
		r.Items = append(r.Items, it)
	})
	return out, err
}

// ---- messages ----

type Message struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	TitleEn   string    `json:"title_en"`
	Body      string    `json:"body"`
	BodyEn    string    `json:"body_en"`
	Link      string    `json:"link"`
	CreatedAt time.Time `json:"created_at"`
	Read      bool      `json:"read"`
	// Key names the text in the customer's language ("msg." + Key in the web
	// app) and Args fills it in; Title and Body are the Chinese and English
	// fallback.
	Key  string            `json:"key,omitempty"`
	Args map[string]string `json:"args,omitempty"`
}

// tell leaves a message for a customer, in the caller's transaction.
func tell(ctx context.Context, q Querier, customerID int64, m Message) error {
	args := m.Args
	if args == nil {
		args = map[string]string{}
	}
	_, err := q.Exec(ctx, `INSERT INTO customer_messages (customer_id, kind, title, title_en, body, body_en, link, msg_key, msg_args)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, customerID, m.Kind, m.Title, m.TitleEn, m.Body, m.BodyEn, m.Link, m.Key, args)
	return err
}

// tellOrder leaves a message about an order for the customer who placed it.
func (a *App) tellOrder(ctx context.Context, tx pgx.Tx, orderID int64, key, zh, en string) error {
	var customerID *int64
	var store, publicNo string
	var nonce []byte
	if err := tx.QueryRow(ctx, `SELECT o.customer_id, b.name, o.public_no, o.token_nonce
		FROM orders o JOIN branches b ON b.id = o.branch_id WHERE o.id=$1`, orderID).
		Scan(&customerID, &store, &publicNo, &nonce); err != nil || customerID == nil {
		return err
	}
	body := store + " · " + publicNo
	return tell(ctx, tx, *customerID, Message{Kind: "order", Title: zh, TitleEn: en, Body: body, BodyEn: body,
		Link: "/o/" + a.orderToken(nonce), Key: key, Args: map[string]string{"store": store, "no": publicNo}})
}

func (a *App) MyMessages(ctx context.Context, customerID int64, offset int) ([]Message, bool, error) {
	if offset < 0 || offset > 10000 {
		return nil, false, ErrValidation("offset out of range")
	}
	rows, err := a.DB.Query(ctx, `SELECT id, kind, title, title_en, body, body_en, link, created_at, read_at IS NOT NULL,
			msg_key, msg_args
		FROM customer_messages WHERE customer_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, customerID, myPageSize+1, offset)
	if err != nil {
		return nil, false, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) {
		var m Message
		return m, r.Scan(&m.ID, &m.Kind, &m.Title, &m.TitleEn, &m.Body, &m.BodyEn, &m.Link, &m.CreatedAt, &m.Read, &m.Key, &m.Args)
	})
	if err != nil {
		return nil, false, err
	}
	if out == nil {
		out = []Message{}
	}
	if len(out) > myPageSize {
		return out[:myPageSize], true, nil
	}
	return out, false, nil
}

// ReadMessages marks one message read, or all of them when id is 0.
func (a *App) ReadMessages(ctx context.Context, customerID, id int64) error {
	_, err := a.DB.Exec(ctx, `UPDATE customer_messages SET read_at=now()
		WHERE customer_id=$1 AND read_at IS NULL AND ($2::bigint = 0 OR id = $2)`, customerID, id)
	return err
}

// ---- feedback ----

type Feedback struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	Content   string     `json:"content"`
	Contact   string     `json:"contact"`
	Reply     string     `json:"reply"`
	RepliedAt *time.Time `json:"replied_at"`
	CreatedAt time.Time  `json:"created_at"`
	// set for the platform only
	CustomerID int64  `json:"customer_id,omitempty"`
	Phone      string `json:"phone,omitempty"`
	MemberNo   string `json:"member_no,omitempty"`
}

type FeedbackInput struct {
	Kind    string `json:"kind"`
	Content string `json:"content"`
	Contact string `json:"contact"`
}

var feedbackKinds = map[string]bool{"suggestion": true, "problem": true, "complaint": true, "account": true, "other": true}

// tooMany refuses a customer who has sent more than limit rows to a table today.
func tooMany(ctx context.Context, q Querier, table string, customerID int64, limit int) error {
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE customer_id=$1 AND created_at > now() - interval '1 day'`,
		customerID).Scan(&n); err != nil {
		return err
	}
	if n >= limit {
		return Err(http.StatusTooManyRequests, "TOO_MANY_TODAY", "you have sent several today already; please try again tomorrow")
	}
	return nil
}

func (a *App) SubmitFeedback(ctx context.Context, customerID int64, in FeedbackInput) (*Feedback, error) {
	in.Content, in.Contact = strings.TrimSpace(in.Content), strings.TrimSpace(in.Contact)
	switch {
	case !feedbackKinds[in.Kind]:
		return nil, ErrValidation("choose what the feedback is about")
	case in.Content == "" || utf8.RuneCountInString(in.Content) > 1000:
		return nil, ErrValidation("write up to 1000 characters")
	case utf8.RuneCountInString(in.Contact) > 100:
		return nil, ErrValidation("the contact is too long")
	}
	if err := tooMany(ctx, a.DB, "customer_feedback", customerID, 10); err != nil {
		return nil, err
	}
	f := &Feedback{Kind: in.Kind, Content: in.Content, Contact: in.Contact}
	err := a.DB.QueryRow(ctx, `INSERT INTO customer_feedback (customer_id, kind, content, contact) VALUES ($1,$2,$3,$4)
		RETURNING id, created_at`, customerID, in.Kind, in.Content, in.Contact).Scan(&f.ID, &f.CreatedAt)
	return f, err
}

func (a *App) MyFeedback(ctx context.Context, customerID int64) ([]Feedback, error) {
	rows, err := a.DB.Query(ctx, `SELECT id, kind, content, contact, reply, replied_at, created_at
		FROM customer_feedback WHERE customer_id=$1 ORDER BY id DESC LIMIT 50`, customerID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Feedback, error) {
		var f Feedback
		return f, r.Scan(&f.ID, &f.Kind, &f.Content, &f.Contact, &f.Reply, &f.RepliedAt, &f.CreatedAt)
	})
	if out == nil {
		out = []Feedback{}
	}
	return out, err
}

// ---- applications to open a store ----

type Application struct {
	ID          int64  `json:"id"`
	StoreName   string `json:"store_name"`
	ContactName string `json:"contact_name"`
	Phone       string `json:"phone"`
	City        string `json:"city"`
	Cuisine     string `json:"cuisine"`
	Note        string `json:"note"`
	ApplicationPlace
	Status    string     `json:"status"`
	Reply     string     `json:"reply"`
	DecidedAt *time.Time `json:"decided_at"`
	CreatedAt time.Time  `json:"created_at"`
	// set for the platform only
	CustomerID   int64  `json:"customer_id,omitempty"`
	AccountPhone string `json:"account_phone,omitempty"`
}

// ApplicationPlace is where the store is: a point picked on the map, the
// country and region read off the map there (in English), the place and
// street the applicant chose, and the floor or unit they typed.
type ApplicationPlace struct {
	CountryCode  string   `json:"country_code"`
	Country      string   `json:"country"`
	Province     string   `json:"province"`
	District     string   `json:"district"`
	PostalCode   string   `json:"postal_code"`
	PlaceName    string   `json:"place_name"`
	PlaceAddress string   `json:"place_address"`
	Unit         string   `json:"unit"`
	Lat          *float64 `json:"lat"`
	Lng          *float64 `json:"lng"`
}

type ApplicationInput struct {
	StoreName   string `json:"store_name"`
	ContactName string `json:"contact_name"`
	Phone       string `json:"phone"`
	City        string `json:"city"`
	Cuisine     string `json:"cuisine"`
	Note        string `json:"note"`
	ApplicationPlace
}

const applicationCols = `p.id, p.store_name, p.contact_name, p.phone, p.city, p.cuisine, p.note,
	p.country_code, p.country, p.province, p.district, p.postal_code, p.place_name, p.place_address, p.unit, p.lat, p.lng,
	p.status, p.reply, p.decided_at, p.created_at`

func scanApplication(r pgx.Row, extra ...any) (Application, error) {
	var p Application
	l := &p.ApplicationPlace
	dest := append([]any{&p.ID, &p.StoreName, &p.ContactName, &p.Phone, &p.City, &p.Cuisine, &p.Note,
		&l.CountryCode, &l.Country, &l.Province, &l.District, &l.PostalCode, &l.PlaceName, &l.PlaceAddress, &l.Unit, &l.Lat, &l.Lng,
		&p.Status, &p.Reply, &p.DecidedAt, &p.CreatedAt}, extra...)
	return p, r.Scan(dest...)
}

func (a *App) SubmitApplication(ctx context.Context, customerID int64, in ApplicationInput) (*Application, error) {
	l := &in.ApplicationPlace
	for _, s := range []*string{&in.StoreName, &in.ContactName, &in.Phone, &in.City, &in.Cuisine, &in.Note,
		&l.CountryCode, &l.Country, &l.Province, &l.District, &l.PostalCode, &l.PlaceName, &l.PlaceAddress, &l.Unit} {
		*s = strings.TrimSpace(*s)
	}
	l.CountryCode = strings.ToUpper(l.CountryCode)
	n := utf8.RuneCountInString
	pinned := l.Lat != nil && l.Lng != nil
	switch {
	case in.StoreName == "" || n(in.StoreName) > 100:
		return nil, ErrValidation("enter the name of the store")
	case in.ContactName == "" || n(in.ContactName) > 80:
		return nil, ErrValidation("enter who to contact")
	case in.Phone == "" || n(in.Phone) > 40:
		return nil, ErrValidation("enter a phone number")
	case (l.Lat == nil) != (l.Lng == nil) || pinned && !validLatLng(LatLng{*l.Lat, *l.Lng}):
		return nil, ErrValidation("pick where the store is on the map")
	case !pinned && in.City == "":
		return nil, ErrValidation("say where the store is")
	case n(in.City) > 100 || n(in.Cuisine) > 100 || n(in.Note) > 1000 || n(l.CountryCode) > 2 || n(l.Country) > 100 ||
		n(l.Province) > 100 || n(l.District) > 100 || n(l.PostalCode) > 20 || n(l.PlaceName) > 200 || n(l.PlaceAddress) > 300 || n(l.Unit) > 100:
		return nil, ErrValidation("one of the fields is too long")
	}
	var pending bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM merchant_applications WHERE customer_id=$1 AND status='pending')`,
		customerID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending {
		return nil, Err(http.StatusConflict, "APPLICATION_PENDING", "your earlier application is still being looked at")
	}
	if err := tooMany(ctx, a.DB, "merchant_applications", customerID, 3); err != nil {
		return nil, err
	}
	p, err := scanApplication(a.DB.QueryRow(ctx, `INSERT INTO merchant_applications AS p
		(customer_id, store_name, contact_name, phone, city, cuisine, note,
		 country_code, country, province, district, postal_code, place_name, place_address, unit, lat, lng)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING `+applicationCols, customerID, in.StoreName, in.ContactName, in.Phone, in.City, in.Cuisine, in.Note,
		l.CountryCode, l.Country, l.Province, l.District, l.PostalCode, l.PlaceName, l.PlaceAddress, l.Unit, l.Lat, l.Lng))
	return &p, err
}

func (a *App) MyApplications(ctx context.Context, customerID int64) ([]Application, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+applicationCols+` FROM merchant_applications p
		WHERE p.customer_id=$1 ORDER BY p.id DESC LIMIT 20`, customerID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Application, error) { return scanApplication(r) })
	if out == nil {
		out = []Application{}
	}
	return out, err
}

// ---- wallet ----

type WalletEntry struct {
	ID        int64     `json:"id"`
	Account   string    `json:"account"`
	Currency  string    `json:"currency"`
	Amount    int64     `json:"amount"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

func (a *App) WalletEntries(ctx context.Context, customerID int64, account string) ([]WalletEntry, error) {
	if account != "balance" && account != "bonus" {
		return nil, ErrValidation("account must be balance or bonus")
	}
	rows, err := a.DB.Query(ctx, `SELECT id, account, currency, amount, note, created_at FROM customer_wallet_entries
		WHERE customer_id=$1 AND account=$2 ORDER BY id DESC LIMIT 100`, customerID, account)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (WalletEntry, error) {
		var e WalletEntry
		return e, r.Scan(&e.ID, &e.Account, &e.Currency, &e.Amount, &e.Note, &e.CreatedAt)
	})
	if out == nil {
		out = []WalletEntry{}
	}
	return out, err
}

// ---- what the platform says about itself ----

// PlatformInfoKeys are the texts the platform can set for the customer pages.
var PlatformInfoKeys = []string{"name", "about", "about_en", "support_phone", "support_email", "support_hours", "support_chat"}

func (a *App) PlatformInfo(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range PlatformInfoKeys {
		out[k] = ""
	}
	rows, err := a.DB.Query(ctx, `SELECT key, value FROM platform_info`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if _, ok := out[k]; ok {
			out[k] = v
		}
	}
	return out, rows.Err()
}
