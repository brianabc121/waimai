package app

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// The platform's address book (docs/ADDRESS_BOOK_DESIGN.md). Every address
// staff deliver to with a pin goes in, once per region (country, province,
// city of the store); the order desk offers these first while staff type an
// address. Two addresses are the same when addr_key (a SQL function, see
// migration 00051) says so: NFKC, lower case, no spaces or punctuation.
// Matching uses PostgreSQL's pg_trgm, not a separate search server.

// AddrRegion is where an address is, as the store's own region is written.
type AddrRegion struct {
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	Province    string `json:"province"`
	City        string `json:"city"`
}

func branchRegion(b *Branch) AddrRegion {
	return AddrRegion{CountryCode: b.CountryCode, Country: b.Country, Province: b.Province, City: b.City}
}

type BookAddress struct {
	ID int64 `json:"id"`
	AddrRegion
	Line       string    `json:"line"`
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	Source     string    `json:"source"`
	BranchID   *int64    `json:"branch_id"`
	BranchName string    `json:"branch_name"`
	Uses       int       `json:"uses"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	key        string
}

const bookCols = `a.id, a.country_code, a.country, a.province, a.city, a.line, a.lat, a.lng, a.source, a.branch_id,
	COALESCE(b.name, ''), a.uses, a.created_at, a.updated_at, a.last_used_at, a.line_key`

func scanBook(row interface{ Scan(...any) error }) (*BookAddress, error) {
	x := &BookAddress{}
	err := row.Scan(&x.ID, &x.CountryCode, &x.Country, &x.Province, &x.City, &x.Line, &x.Lat, &x.Lng, &x.Source,
		&x.BranchID, &x.BranchName, &x.Uses, &x.CreatedAt, &x.UpdatedAt, &x.LastUsedAt, &x.key)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return x, err
}

// rememberAddress puts an address staff delivered to into the book, or counts
// one more use of the one already there. Without a pin there is nothing to keep.
func rememberAddress(ctx context.Context, q Querier, b *Branch, line string, lat, lng *float64) error {
	if lat == nil || lng == nil || strings.TrimSpace(line) == "" {
		return nil
	}
	r := branchRegion(b)
	_, err := q.Exec(ctx, `INSERT INTO address_book (country_code, country, province, city, line, lat, lng, source, branch_id)
		SELECT $1, $2, $3, $4, left(addr_line($5), 300), $6, $7, 'staff', $8 WHERE addr_key($5) <> ''
		ON CONFLICT (country_code, province, city, line_key) DO UPDATE SET uses = address_book.uses + 1, last_used_at = now()`,
		r.CountryCode, r.Country, r.Province, r.City, line, *lat, *lng, b.ID)
	return wrap("remember address", err)
}

// searchTerms splits what was typed into words; each must appear in the address.
func searchTerms(q string) []string {
	out := strings.FieldsFunc(q, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) })
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// bookMatch is the WHERE part that finds q ($n) in the book: every word of it
// somewhere in the address, or (for a typo) close enough by trigrams.
func bookMatch(n string) string {
	return `(NOT EXISTS (SELECT 1 FROM unnest(` + n + `::text[]) t WHERE a.line_key NOT LIKE '%' || addr_key(t) || '%')
		OR (length(addr_key(array_to_string(` + n + `::text[], ''))) >= 4 AND addr_key(array_to_string(` + n + `::text[], '')) <% a.line_key))`
}

// searchBook lists the book's addresses in one region matching q, the closest first.
func searchBook(ctx context.Context, q Querier, r AddrRegion, text string, limit int) ([]BookAddress, error) {
	terms := searchTerms(text)
	if len(terms) == 0 {
		return []BookAddress{}, nil
	}
	rows, err := q.Query(ctx, `SELECT `+bookCols+` FROM address_book a LEFT JOIN branches b ON b.id = a.branch_id
		WHERE a.country_code = $1 AND a.province = $2 AND a.city = $3 AND `+bookMatch("$4")+`
		ORDER BY (a.line_key LIKE addr_key(array_to_string($4::text[], '')) || '%') DESC,
			similarity(a.line_key, addr_key(array_to_string($4::text[], ''))) DESC, a.uses DESC, a.last_used_at DESC
		LIMIT $5`, r.CountryCode, r.Province, r.City, terms, limit)
	if err != nil {
		return nil, err
	}
	return collectBook(rows)
}

func collectBook(rows pgx.Rows) ([]BookAddress, error) {
	defer rows.Close()
	out := []BookAddress{}
	for rows.Next() {
		x, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// ---- the platform's address page ----

// BookRegion is a region with how many addresses the book has there. Regions
// of stores show even while they have none.
type BookRegion struct {
	AddrRegion
	Count int `json:"count"`
}

func (a *App) BookRegions(ctx context.Context) ([]BookRegion, error) {
	rows, err := a.DB.Query(ctx, `SELECT country_code, max(country), province, city, sum(n)::int FROM (
			SELECT country_code, country, province, city, 1 AS n FROM address_book
			UNION ALL
			SELECT country_code, country, province, city, 0 FROM branches WHERE deleted_at IS NULL
		) x GROUP BY country_code, province, city
		ORDER BY max(country) = '', max(country), province, city`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BookRegion{}
	for rows.Next() {
		var r BookRegion
		if err := rows.Scan(&r.CountryCode, &r.Country, &r.Province, &r.City, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BookFilter narrows the address page. A region part that is nil is not
// filtered on; "" is the unknown region.
type BookFilter struct {
	CountryCode, Province, City *string
	Q                           string
	Source                      string
	// Sort: "recent" (last used, the default), "uses", "new" or "line".
	Sort string
	Page
}

type BookList struct {
	Items []BookAddress `json:"items"`
	Total int           `json:"total"`
}

func (a *App) ListBook(ctx context.Context, f BookFilter) (*BookList, error) {
	where, args := []string{"true"}, []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.CountryCode != nil {
		where = append(where, "a.country_code = "+arg(*f.CountryCode))
	}
	if f.Province != nil {
		where = append(where, "a.province = "+arg(*f.Province))
	}
	if f.City != nil {
		where = append(where, "a.city = "+arg(*f.City))
	}
	if f.Source != "" {
		where = append(where, "a.source = "+arg(f.Source))
	}
	order := "a.last_used_at DESC, a.id DESC"
	switch f.Sort {
	case "uses":
		order = "a.uses DESC, a.last_used_at DESC"
	case "new":
		order = "a.id DESC"
	case "line":
		order = "a.line_key, a.id"
	}
	if terms := searchTerms(f.Q); len(terms) > 0 {
		n := arg(terms)
		where = append(where, bookMatch(n))
		if f.Sort == "" {
			order = `similarity(a.line_key, addr_key(array_to_string(` + n + `::text[], ''))) DESC, ` + order
		}
	}
	w := strings.Join(where, " AND ")
	l := &BookList{}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM address_book a WHERE `+w, args...).Scan(&l.Total); err != nil {
		return nil, err
	}
	size, off := f.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT `+bookCols+` FROM address_book a LEFT JOIN branches b ON b.id = a.branch_id
		WHERE `+w+` ORDER BY `+order+` LIMIT `+strconv.Itoa(size)+` OFFSET `+strconv.Itoa(off), args...)
	if err != nil {
		return nil, err
	}
	l.Items, err = collectBook(rows)
	return l, err
}

// BookInput is an address put in or corrected on the platform's page.
type BookInput struct {
	AddrRegion
	Line string  `json:"line"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

func (in *BookInput) clean() error {
	in.Line = strings.TrimSpace(in.Line)
	in.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
	in.Country, in.Province, in.City = strings.TrimSpace(in.Country), strings.TrimSpace(in.Province), strings.TrimSpace(in.City)
	if in.Line == "" || len([]rune(in.Line)) > 300 {
		return Err(http.StatusUnprocessableEntity, "ADDRESS_REQUIRED", "an address of at most 300 characters is required")
	}
	if !validLatLng(LatLng{in.Lat, in.Lng}) || (in.Lat == 0 && in.Lng == 0) {
		return Err(http.StatusUnprocessableEntity, "PIN_REQUIRED", "the address needs a position")
	}
	if len(in.CountryCode) > 2 || len([]rune(in.Country)) > 80 || len([]rune(in.Province)) > 120 || len([]rune(in.City)) > 120 {
		return ErrValidation("invalid region")
	}
	return nil
}

var errDuplicateAddress = Err(http.StatusConflict, "DUPLICATE_ADDRESS", "this address is already in the book for this region")

// SaveBookAddress adds an address (id 0) or corrects one. An address that
// reads the same as another in the region is refused.
func (a *App) SaveBookAddress(ctx context.Context, actor AdminActor, id int64, in BookInput) (*BookAddress, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	action := "address.update"
	if id == 0 {
		action = "address.create"
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO address_book (country_code, country, province, city, line, lat, lng, source, uses)
				VALUES ($1,$2,$3,$4,left(addr_line($5), 300),$6,$7,'admin',0) RETURNING id`,
				in.CountryCode, in.Country, in.Province, in.City, in.Line, in.Lat, in.Lng).Scan(&id)
		} else {
			var tag interface{ RowsAffected() int64 }
			tag, err = tx.Exec(ctx, `UPDATE address_book SET country_code=$2, country=$3, province=$4, city=$5,
				line=left(addr_line($6), 300), lat=$7, lng=$8, updated_at=now() WHERE id=$1`,
				id, in.CountryCode, in.Country, in.Province, in.City, in.Line, in.Lat, in.Lng)
			if err == nil && tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		if isUnique(err, "address_book_uniq") {
			return errDuplicateAddress
		}
		if err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, action, "address", id, map[string]any{"line": in.Line, "lat": in.Lat, "lng": in.Lng,
			"region": strings.Join([]string{in.CountryCode, in.Province, in.City}, " / ")})
	})
	if err != nil {
		return nil, err
	}
	return scanBook(a.DB.QueryRow(ctx, `SELECT `+bookCols+` FROM address_book a LEFT JOIN branches b ON b.id = a.branch_id WHERE a.id=$1`, id))
}

func (a *App) DeleteBookAddress(ctx context.Context, actor AdminActor, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var line string
		err := tx.QueryRow(ctx, `DELETE FROM address_book WHERE id=$1 RETURNING line`, id).Scan(&line)
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "address.delete", "address", id, map[string]any{"line": line})
	})
}
