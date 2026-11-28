package app

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// One store, several brands (docs/MULTI_BRAND_DESIGN.md). The store is the
// kitchen, staff, cash drawer and delivery area; a brand is the shop customers
// see: its name, the code its staff orders start with, its members. Every
// store has a main brand that mirrors the store's own name, slug, code and
// store number (a trigger keeps them in step), so stores that never add a
// brand work exactly as before.

// MaxDeliveryBrands is how many active delivery brands one store may have
// (the platform can raise it per store, branches.brand_limit).
const MaxDeliveryBrands = 10

type Brand struct {
	ID          int64  `json:"id"`
	BranchID    int64  `json:"branch_id"`
	Name        string `json:"name"`
	NameEn      string `json:"name_en"`
	Code        string `json:"code"`
	StoreNo     int    `json:"store_no"`
	Slug        string `json:"slug"`
	DineIn      bool   `json:"dine_in"`
	Delivery    bool   `json:"delivery"`
	IsMain      bool   `json:"is_main"`
	Color       string `json:"color"`
	OpenTime    string `json:"open_time"`
	CloseTime   string `json:"close_time"`
	Paused      bool   `json:"paused"`
	MallVisible bool   `json:"mall_visible"`
	Status      string `json:"status"`
	Sort        int    `json:"sort"`
	// OwnMenu: the brand sells its own pick of the store's dishes, with its
	// own names and prices (brand_menu.go); otherwise the store's menu as it is.
	OwnMenu bool `json:"own_menu"`
	// LogoURL, CoverURL, Intro: what customers see of a brand; empty = the
	// store's (the main brand's are always the store's).
	LogoURL   string    `json:"logo_url"`
	CoverURL  string    `json:"cover_url"`
	Intro     string    `json:"intro"`
	CreatedAt time.Time `json:"created_at"`
	// Today is the brand's orders today (the brand list only).
	Today *BrandDay `json:"today,omitempty"`
}

// BrandDay is how a brand did on the store's current business day.
type BrandDay struct {
	Orders int   `json:"orders"`
	Total  int64 `json:"total"`
}

// BrandRef is what pickers and order tags need of a brand.
type BrandRef struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Code     string `json:"code"`
	Color    string `json:"color"`
	IsMain   bool   `json:"is_main"`
	DineIn   bool   `json:"dine_in"`
	Delivery bool   `json:"delivery"`
	Active   bool   `json:"active"`
}

func (b *Brand) Ref() BrandRef {
	return BrandRef{ID: b.ID, Name: b.Name, Code: b.Code, Color: b.Color, IsMain: b.IsMain,
		DineIn: b.DineIn, Delivery: b.Delivery, Active: b.Status == "active"}
}

const brandCols = `id, branch_id, name, name_en, code, store_no, slug, dine_in, delivery, is_main, color,
	open_time, close_time, paused, mall_visible, status, sort, own_menu, logo_url, cover_url, intro, created_at`

func scanBrand(row interface{ Scan(...any) error }) (*Brand, error) {
	var b Brand
	err := row.Scan(&b.ID, &b.BranchID, &b.Name, &b.NameEn, &b.Code, &b.StoreNo, &b.Slug, &b.DineIn, &b.Delivery,
		&b.IsMain, &b.Color, &b.OpenTime, &b.CloseTime, &b.Paused, &b.MallVisible, &b.Status, &b.Sort, &b.OwnMenu,
		&b.LogoURL, &b.CoverURL, &b.Intro, &b.CreatedAt)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// loadBrand reads one brand of the store; brandID 0 is the main brand.
func loadBrand(ctx context.Context, q Querier, branchID, brandID int64) (*Brand, error) {
	if brandID == 0 {
		return scanBrand(q.QueryRow(ctx, `SELECT `+brandCols+` FROM brands WHERE branch_id=$1 AND is_main`, branchID))
	}
	return scanBrand(q.QueryRow(ctx, `SELECT `+brandCols+` FROM brands WHERE branch_id=$1 AND id=$2`, branchID, brandID))
}

// BrandOf reads one brand of the store; brandID 0 is the main brand.
func (a *App) BrandOf(ctx context.Context, branchID, brandID int64) (*Brand, error) {
	return loadBrand(ctx, a.DB, branchID, brandID)
}

// MainBrandID is the store's main brand.
func (a *App) MainBrandID(ctx context.Context, branchID int64) (int64, error) {
	var id int64
	err := a.DB.QueryRow(ctx, `SELECT id FROM brands WHERE branch_id=$1 AND is_main`, branchID).Scan(&id)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return id, err
}

// brandBySlug is the brand a customer's address names: a brand's own slug,
// or the store's, which is its main brand's.
func brandBySlug(ctx context.Context, q Querier, branchID int64, slug string) (*Brand, error) {
	br, err := scanBrand(q.QueryRow(ctx, `SELECT `+brandCols+` FROM brands WHERE branch_id=$1 AND slug=$2`, branchID, slug))
	if err == ErrNotFound {
		return loadBrand(ctx, q, branchID, 0)
	}
	return br, err
}

// forBrand dresses the store as one of its brands for customers: the brand's
// name, address and number, its own hours when it has them, and its pause.
// The kitchen, delivery areas, fees and tax stay the store's.
func (b *Branch) forBrand(br *Brand) *Branch {
	if br == nil {
		return b
	}
	b.Paused = b.Paused || br.Paused || br.Status != "active"
	b.brandID, b.brandNamed, b.brandIntro = br.ID, !br.IsMain, br.Intro
	if br.OpenTime != "" && br.OpenTime != br.CloseTime {
		b.OpenTime, b.CloseTime = br.OpenTime, br.CloseTime
	}
	if !br.IsMain {
		b.Name, b.NameEn, b.Slug, b.StoreNo = br.Name, br.NameEn, br.Slug, br.StoreNo
	}
	return b
}

// orderBrand is the brand a staff member orders under: active, taking
// delivery or pickup orders.
func orderBrand(ctx context.Context, q Querier, branchID, brandID int64) (*Brand, error) {
	br, err := loadBrand(ctx, q, branchID, brandID)
	if err == ErrNotFound {
		return nil, Err(http.StatusUnprocessableEntity, "BRAND_UNKNOWN", "no such brand in this store")
	}
	if err != nil {
		return nil, err
	}
	if br.Status != "active" || !br.Delivery {
		return nil, Err(http.StatusUnprocessableEntity, "BRAND_CLOSED", "this brand does not take delivery orders")
	}
	return br, nil
}

// ListBrands lists the store's brands, main brand first, with today's orders.
func (a *App) ListBrands(ctx context.Context, branchID int64) ([]*Brand, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT `+brandCols+` FROM brands WHERE branch_id=$1
		ORDER BY is_main DESC, status, sort, id`, branchID)
	if err != nil {
		return nil, err
	}
	out := []*Brand{}
	byID := map[int64]*Brand{}
	for rows.Next() {
		br, err := scanBrand(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		br.Today = &BrandDay{}
		out = append(out, br)
		byID[br.ID] = br
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	biz := b.businessDay(time.Now()).Format("2006-01-02")
	rows, err = a.DB.Query(ctx, `SELECT brand_id, count(*), COALESCE(sum(total), 0) FROM orders
		WHERE branch_id=$1 AND biz_date=$2::date AND acceptance_status NOT IN ('rejected','cancelled')
		GROUP BY brand_id`, branchID, biz)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var d BrandDay
		if err := rows.Scan(&id, &d.Orders, &d.Total); err != nil {
			return nil, err
		}
		if br := byID[id]; br != nil {
			br.Today = &d
		}
	}
	return out, rows.Err()
}

// BrandInput is what the brand form sends.
type BrandInput struct {
	Name        string `json:"name"`
	NameEn      string `json:"name_en"`
	Code        string `json:"code"`
	DineIn      bool   `json:"dine_in"`
	Delivery    bool   `json:"delivery"`
	Color       string `json:"color"`
	OpenTime    string `json:"open_time"`
	CloseTime   string `json:"close_time"`
	Paused      bool   `json:"paused"`
	MallVisible bool   `json:"mall_visible"`
	Status      string `json:"status"`
	Sort        int    `json:"sort"`
	LogoURL     string `json:"logo_url"`
	CoverURL    string `json:"cover_url"`
	Intro       string `json:"intro"`
}

func (in *BrandInput) clean() error {
	in.Name, in.NameEn = strings.TrimSpace(in.Name), strings.TrimSpace(in.NameEn)
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.OpenTime, in.CloseTime = strings.TrimSpace(in.OpenTime), strings.TrimSpace(in.CloseTime)
	in.LogoURL, in.CoverURL, in.Intro = strings.TrimSpace(in.LogoURL), strings.TrimSpace(in.CoverURL), strings.TrimSpace(in.Intro)
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Color == "" {
		in.Color = "#a02c20"
	}
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 40 || len([]rune(in.NameEn)) > 60:
		return ErrValidation("the brand name must be 1 to 40 characters")
	case !storeCodeRe.MatchString(in.Code):
		return Err(http.StatusUnprocessableEntity, "BRAND_CODE_INVALID", "the brand code must be 2 to 8 letters or digits")
	case !colorRe.MatchString(in.Color):
		return ErrValidation("invalid colour")
	case (in.OpenTime == "") != (in.CloseTime == ""),
		in.OpenTime != "" && (!hhmmRe.MatchString(in.OpenTime) || !hhmmRe.MatchString(in.CloseTime)):
		return ErrValidation("opening hours are HH:MM")
	case len(in.LogoURL) > 500 || len(in.CoverURL) > 500 || len([]rune(in.Intro)) > 300:
		return ErrValidation("picture link or introduction too long")
	case in.Status != "active" && in.Status != "disabled":
		return ErrValidation("invalid status")
	case !in.DineIn && !in.Delivery:
		return ErrValidation("a brand takes dine-in or delivery orders")
	}
	return nil
}

// brandErr turns the database's refusals into answers the form can show.
func brandErr(err error) error {
	switch {
	case isUnique(err, "brands_code"), isUnique(err, "branches_store_code"):
		return Err(http.StatusConflict, "BRAND_CODE_TAKEN", "another brand already uses this code")
	case isUnique(err, "brands_one_dine"):
		return Err(http.StatusConflict, "DINE_BRAND_TAKEN", "the store already has a dine-in brand")
	}
	return err
}

// checkBrandLimit refuses more active delivery brands than the store may have.
func checkBrandLimit(ctx context.Context, tx pgx.Tx, branchID, brandID int64) error {
	var n, limit int
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE delivery AND status='active' AND id <> $2),
			(SELECT brand_limit FROM branches WHERE id=$1)
		FROM brands WHERE branch_id=$1`, branchID, brandID).Scan(&n, &limit); err != nil {
		return err
	}
	if n >= limit {
		return ErrD(http.StatusConflict, "BRAND_LIMIT", "the store has as many delivery brands as it may",
			map[string]any{"limit": limit})
	}
	return nil
}

// CreateBrand adds a brand to the store.
func (a *App) CreateBrand(ctx context.Context, branchID int64, actor Actor, in BrandInput) (*Brand, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	in.Status = "active"
	var br *Brand
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var slug string
		if err := tx.QueryRow(ctx, `SELECT slug FROM branches WHERE id=$1 FOR UPDATE`, branchID).Scan(&slug); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if in.Delivery {
			if err := checkBrandLimit(ctx, tx, branchID, 0); err != nil {
				return err
			}
		}
		base := slug + "-" + strings.ToLower(in.Code)
		var free string
		for i := 0; i < 20 && free == ""; i++ {
			c := base
			if i > 0 {
				c = fmt.Sprintf("%s-%d", base, i+1)
			}
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM brands WHERE slug=$1)
				OR EXISTS (SELECT 1 FROM branches WHERE slug=$1)`, c).Scan(&taken); err != nil {
				return err
			}
			if !taken {
				free = c
			}
		}
		if free == "" {
			return fmt.Errorf("no free slug for %s", base)
		}
		var err error
		br, err = scanBrand(tx.QueryRow(ctx, `INSERT INTO brands (branch_id, name, name_en, code, slug, dine_in,
				delivery, color, open_time, close_time, paused, mall_visible, logo_url, cover_url, intro, sort)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
				(SELECT COALESCE(max(sort), 0) + 1 FROM brands WHERE branch_id=$1))
			RETURNING `+brandCols, branchID, in.Name, in.NameEn, in.Code, free, in.DineIn, in.Delivery, in.Color,
			in.OpenTime, in.CloseTime, in.Paused, in.MallVisible, in.LogoURL, in.CoverURL, in.Intro))
		if err != nil {
			return brandErr(err)
		}
		return audit(ctx, tx, branchID, 0, actor, "brand.create", map[string]any{"brand_id": br.ID, "name": br.Name, "code": br.Code})
	})
	if err != nil {
		return nil, wrap("create brand", err)
	}
	return br, nil
}

// UpdateBrand changes a brand. The main brand's name and code are the
// store's: they are written there and the trigger copies them back. The main
// brand cannot be switched off.
func (a *App) UpdateBrand(ctx context.Context, branchID, brandID int64, actor Actor, in BrandInput) (*Brand, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	var br *Brand
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT id FROM branches WHERE id=$1 FOR UPDATE`, branchID); err != nil {
			return err
		}
		old, err := loadBrand(ctx, tx, branchID, brandID)
		if err != nil {
			return err
		}
		if old.IsMain && in.Status != "active" {
			return Err(http.StatusConflict, "MAIN_BRAND", "the main brand cannot be switched off")
		}
		if in.Delivery && in.Status == "active" && (!old.Delivery || old.Status != "active") {
			if err := checkBrandLimit(ctx, tx, branchID, brandID); err != nil {
				return err
			}
		}
		if old.IsMain {
			if _, err := tx.Exec(ctx, `UPDATE branches SET name=$2, name_en=$3, store_code=$4 WHERE id=$1`,
				branchID, in.Name, in.NameEn, in.Code); err != nil {
				return brandErr(err)
			}
		}
		if old.IsMain {
			// the main brand looks like the store: its page design has the pictures
			in.LogoURL, in.CoverURL, in.Intro = "", "", ""
		}
		br, err = scanBrand(tx.QueryRow(ctx, `UPDATE brands SET name=$3, name_en=$4, code=$5, dine_in=$6,
				delivery=$7, color=$8, open_time=$9, close_time=$10, paused=$11, mall_visible=$12, status=$13,
				logo_url=$14, cover_url=$15, intro=$16
			WHERE branch_id=$1 AND id=$2 RETURNING `+brandCols, branchID, brandID, in.Name, in.NameEn, in.Code,
			in.DineIn, in.Delivery, in.Color, in.OpenTime, in.CloseTime, in.Paused, in.MallVisible, in.Status,
			in.LogoURL, in.CoverURL, in.Intro))
		if err != nil {
			return brandErr(err)
		}
		return audit(ctx, tx, branchID, 0, actor, "brand.update", map[string]any{"brand_id": br.ID,
			"before": map[string]any{"name": old.Name, "code": old.Code, "status": old.Status},
			"after":  map[string]any{"name": br.Name, "code": br.Code, "status": br.Status}})
	})
	if err != nil {
		return nil, wrap("update brand", err)
	}
	return br, nil
}

// SuggestBrandCode offers a free code from the brand's English name: its
// first letters, then with a digit.
func (a *App) SuggestBrandCode(ctx context.Context, nameEn string) (string, error) {
	letters := []rune{}
	words := strings.Fields(strings.ToUpper(nameEn))
	for _, w := range words {
		for _, r := range w {
			if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				letters = append(letters, r)
				break
			}
		}
	}
	if len(words) == 1 || len(letters) < 2 {
		letters = letters[:0]
		for _, r := range strings.ToUpper(nameEn) {
			if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				letters = append(letters, r)
			}
		}
	}
	base := string(letters)
	if len(base) > 3 {
		base = base[:3]
	}
	if len(base) < 2 {
		base = "BR"
	}
	for i := 0; i < 100; i++ {
		c := base
		if i > 0 {
			c = fmt.Sprintf("%s%d", base, i+1)
		}
		var taken bool
		if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM brands WHERE code=$1)
			OR EXISTS (SELECT 1 FROM branches WHERE store_code=$1)`, c).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return c, nil
		}
	}
	return "", fmt.Errorf("no free brand code for %q", nameEn)
}

// MayUseBrand says whether the person works on this brand of the store.
func (u *User) MayUseBrand(branchID, brandID int64) bool {
	for _, r := range u.Roles {
		if r.BranchID == branchID {
			return r.BrandIDs == nil || slices.Contains(r.BrandIDs, brandID)
		}
	}
	// The platform looks at every store and every brand.
	return u.Entry == EntryAdmin
}

// BrandsOf is the brands of the store the person works on; nil is all of them.
func (u *User) BrandsOf(branchID int64) []int64 {
	for _, r := range u.Roles {
		if r.BranchID == branchID {
			return r.BrandIDs
		}
	}
	return nil
}

// AllBrands says whether the person works on every brand of the store.
func (u *User) AllBrands(branchID int64) bool {
	for _, r := range u.Roles {
		if r.BranchID == branchID {
			return r.BrandIDs == nil
		}
	}
	return true
}

// SetStaffBrands limits which brands a member of staff works on; nil = all.
func (a *App) SetStaffBrands(ctx context.Context, branchID, userID int64, actor *User, brandIDs []int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, `SELECT role FROM user_branch_roles WHERE branch_id=$1 AND user_id=$2 FOR UPDATE`,
			branchID, userID).Scan(&role); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if role == "owner" && brandIDs != nil {
			return Err(http.StatusConflict, "OWNER_ALL_BRANDS", "the owner works on every brand")
		}
		if brandIDs != nil {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM brands WHERE branch_id=$1 AND id = ANY($2)`,
				branchID, brandIDs).Scan(&n); err != nil {
				return err
			}
			slices.Sort(brandIDs)
			brandIDs = slices.Compact(brandIDs)
			if n != len(brandIDs) || n == 0 {
				return ErrValidation("pick at least one brand of this store")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE user_branch_roles SET brand_ids=$3 WHERE branch_id=$1 AND user_id=$2`,
			branchID, userID, brandIDs); err != nil {
			return err
		}
		return audit(ctx, tx, branchID, 0, Actor{Type: "staff", ID: actor.ID}, "staff.brands",
			map[string]any{"user_id": userID, "brand_ids": brandIDs})
	})
}

// roleBrands fills in the brands each role works on.
func roleBrands(ctx context.Context, q Querier, roles []BranchRole) error {
	if len(roles) == 0 {
		return nil
	}
	ids := make([]int64, len(roles))
	at := map[int64]int{}
	for i, r := range roles {
		ids[i] = r.BranchID
		at[r.BranchID] = i
	}
	rows, err := q.Query(ctx, `SELECT `+brandCols+` FROM brands WHERE branch_id = ANY($1)
		ORDER BY is_main DESC, status, sort, id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		br, err := scanBrand(rows)
		if err != nil {
			return err
		}
		r := &roles[at[br.BranchID]]
		if r.BrandIDs == nil || slices.Contains(r.BrandIDs, br.ID) {
			r.Brands = append(r.Brands, br.Ref())
		}
	}
	return rows.Err()
}
