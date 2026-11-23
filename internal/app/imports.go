package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
)

const (
	importMaxBytes = 2 << 20
	importMaxRows  = 2000
)

// ImportRow is one spreadsheet line after parsing. Errors is empty for a row
// that can be written. For an existing dish a blank cell keeps what the dish
// has; the *Set flags say a cell had a value ("无" clears a picture, the
// details or the options).
type ImportRow struct {
	Line        int                `json:"line"` // row number in the sheet, as Excel shows it
	Category    string             `json:"category"`
	Name        string             `json:"name"`
	NameEn      string             `json:"name_en"`
	Description string             `json:"description"`
	Price       int64              `json:"price"`
	Station     string             `json:"station"`
	NeedsPrep   *bool              `json:"needs_prep"`
	Stock       *int               `json:"stock"`
	StockSet    bool               `json:"stock_set"` // the stock cell had a value ("不限" clears the limit)
	Delivery    *bool              `json:"delivery"`
	Pickup      *bool              `json:"pickup"`
	DineIn      *bool              `json:"dine_in"`
	Published   *bool              `json:"published"`
	ImageURL    string             `json:"image_url"`
	ImageSet    bool               `json:"image_set"`
	Photos      []string           `json:"photos"`
	PhotosSet   bool               `json:"photos_set"`
	Details     string             `json:"details"`
	DetailsSet  bool               `json:"details_set"`
	Specs       []OptionGroupInput `json:"specs"`
	SpecsSet    bool               `json:"specs_set"`
	Action      string             `json:"action"` // create or update; empty when the row has errors
	OldPrice    *int64             `json:"old_price,omitempty"`
	NewCategory bool               `json:"new_category,omitempty"` // the category does not exist yet and will be made
	Errors      []string           `json:"errors"`
}

// MenuImport is an uploaded spreadsheet and its parse result.
type MenuImport struct {
	ID           int64       `json:"id"`
	Filename     string      `json:"filename"`
	Status       string      `json:"status"` // pending, parsed, failed, applied
	Error        string      `json:"error"`
	TotalRows    int         `json:"total_rows"`
	ValidRows    int         `json:"valid_rows"`
	ErrorRows    int         `json:"error_rows"`
	CreatedCount int         `json:"created_count"`
	UpdatedCount int         `json:"updated_count"`
	CreatedAt    time.Time   `json:"created_at"`
	AppliedAt    *time.Time  `json:"applied_at"`
	Rows         []ImportRow `json:"rows"`
}

// Column headers, Chinese or English. Only the first three are required.
// Headers are compared by normHeader, so "价格（PHP）*" is still 价格.
var importColumns = map[string][]string{
	"category":    {"分类", "category"},
	"name":        {"菜名", "中文名", "商品名称", "name"},
	"price":       {"价格", "price"},
	"name_en":     {"英文名", "englishname", "name_en"},
	"description": {"描述", "简介", "description"},
	"specs":       {"规格", "specs", "options"},
	"image":       {"主图", "主图链接", "图片", "image", "imageurl"},
	"photos":      {"更多图片", "更多图片链接", "photos"},
	"details":     {"商品详情", "详情", "details"},
	"published":   {"上架", "published"},
	"dine_in":     {"堂食", "dinein"},
	"delivery":    {"外卖", "delivery"},
	"pickup":      {"自取", "pickup"},
	"needs_prep":  {"需要制作", "needsprep"},
	"station":     {"工作台", "station"},
	"stock":       {"限量库存", "库存", "stock"},
}

// normHeader drops spaces, "*" and anything in brackets, and lowercases.
func normHeader(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '（', '[', '【':
			depth++
		case ')', '）', ']', '】':
			if depth > 0 {
				depth--
			}
		case ' ', '*', '＊', '\t', '\n', '\r', '　':
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return strings.ToLower(b.String())
}

// A cell holding one of these clears the picture, details or options of an
// existing dish (a blank cell keeps them).
func isClear(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "无", "-", "none", "清空":
		return true
	}
	return false
}

// CreateMenuImport stores an uploaded .xlsx and queues it for parsing.
func (a *App) CreateMenuImport(ctx context.Context, branchID int64, filename string, content []byte, actor Actor) (int64, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || len(filename) > 200 || !utf8.ValidString(filename) {
		filename = "menu.xlsx"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".xlsx") {
		return 0, ErrValidation("please upload an .xlsx file")
	}
	if len(content) == 0 {
		return 0, ErrValidation("the file is empty")
	}
	if len(content) > importMaxBytes {
		return 0, ErrValidation("the file is larger than 2 MB")
	}
	// .xlsx is a zip archive; anything else is refused before it is stored.
	if !bytes.HasPrefix(content, []byte("PK\x03\x04")) {
		return 0, ErrValidation("this is not an .xlsx file")
	}
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var uploader *int64
		if actor.ID != 0 {
			uploader = &actor.ID
		}
		if err := tx.QueryRow(ctx, `INSERT INTO menu_imports (branch_id, uploaded_by, filename, content)
			VALUES ($1,$2,$3,$4) RETURNING id`, branchID, uploader, filename, content).Scan(&id); err != nil {
			return err
		}
		return enqueue(ctx, tx, "menu_import", map[string]int64{"id": id})
	})
	return id, err
}

const importCols = `id, filename, status, error, total_rows, valid_rows, error_rows, created_count, updated_count,
	created_at, applied_at`

// GetMenuImport returns one import with its rows.
func (a *App) GetMenuImport(ctx context.Context, branchID, id int64) (*MenuImport, error) {
	var m MenuImport
	var rows []byte
	err := a.DB.QueryRow(ctx, `SELECT `+importCols+`, rows FROM menu_imports WHERE id=$1 AND branch_id=$2`, id, branchID).
		Scan(&m.ID, &m.Filename, &m.Status, &m.Error, &m.TotalRows, &m.ValidRows, &m.ErrorRows, &m.CreatedCount,
			&m.UpdatedCount, &m.CreatedAt, &m.AppliedAt, &rows)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rows, &m.Rows); err != nil {
		return nil, err
	}
	for i := range m.Rows {
		if m.Rows[i].Photos == nil {
			m.Rows[i].Photos = []string{}
		}
		if m.Rows[i].Specs == nil {
			m.Rows[i].Specs = []OptionGroupInput{}
		}
	}
	return &m, nil
}

// ListMenuImports returns the branch's recent imports without their rows.
func (a *App) ListMenuImports(ctx context.Context, branchID int64) ([]MenuImport, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+importCols+` FROM menu_imports WHERE branch_id=$1 ORDER BY id DESC LIMIT 10`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MenuImport{}
	for rows.Next() {
		m := MenuImport{Rows: []ImportRow{}}
		if err := rows.Scan(&m.ID, &m.Filename, &m.Status, &m.Error, &m.TotalRows, &m.ValidRows, &m.ErrorRows,
			&m.CreatedCount, &m.UpdatedCount, &m.CreatedAt, &m.AppliedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func parseYesNo(s string) (*bool, bool) {
	t, f := true, false
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return nil, true
	case "是", "y", "yes", "true", "1", "对", "√", "✓":
		return &t, true
	case "否", "n", "no", "false", "0", "不", "×", "✗":
		return &f, true
	}
	return nil, false
}

// parsePrice turns "169", "169.5" or "1,299.00" into minor units. Raw cell
// values of numeric cells can carry float noise (169.90000000000001), so the
// value is rounded to cents and must be within a hair of a whole cent.
func parsePrice(s string) (int64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 10_000_000 {
		return 0, false
	}
	cents := math.Round(v * 100)
	if math.Abs(v*100-cents) > 1e-6 {
		return 0, false
	}
	return int64(cents), true
}

// validImageRef accepts a web link or a picture already on this site.
func validImageRef(s string) bool {
	if len(s) > 1000 || strings.ContainsAny(s, " \t\r\n\"'<>") {
		return false
	}
	l := strings.ToLower(s)
	if strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") {
		return len(s) > len("https://")
	}
	return strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//")
}

// splitPhotos reads the "more pictures" cell: links one per line, or
// separated by commas, semicolons, "|" or spaces.
func splitPhotos(s string) []string {
	out := []string{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == '，' || r == ';' || r == '；' || r == '|' || r == ' ' || r == '\t'
	}) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseMenuSheet reads the first sheet. A file-level problem (not a
// spreadsheet, missing header) is returned as an error; row problems go into
// each row's Errors.
func parseMenuSheet(content []byte) ([]ImportRow, error) {
	f, err := excelize.OpenReader(bytes.NewReader(content), excelize.Options{
		RawCellValue: true, UnzipSizeLimit: 32 << 20, UnzipXMLSizeLimit: 16 << 20})
	if err != nil {
		return nil, fmt.Errorf("the file could not be read as a spreadsheet")
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("the workbook has no sheets")
	}
	it, err := f.Rows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("the first sheet could not be read")
	}
	defer it.Close()

	names := map[string]string{} // normalised header -> column key
	for key, list := range importColumns {
		for _, n := range list {
			names[normHeader(n)] = key
		}
	}
	col := map[string]int{}
	out := []ImportRow{}
	seen := map[string]int{}
	line := 0
	header := false
	for it.Next() {
		line++
		cells, err := it.Columns()
		if err != nil {
			return nil, fmt.Errorf("row %d could not be read", line)
		}
		blank := true
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
			if cells[i] != "" {
				blank = false
			}
		}
		if blank {
			continue
		}
		if !header {
			for i, c := range cells {
				if key, ok := names[normHeader(c)]; ok {
					if _, dup := col[key]; !dup {
						col[key] = i
					}
				}
			}
			for _, need := range []string{"category", "name", "price"} {
				if _, ok := col[need]; !ok {
					return nil, fmt.Errorf("the first row must have the columns 分类, 菜名 and 价格")
				}
			}
			header = true
			continue
		}
		if len(out) >= importMaxRows {
			return nil, fmt.Errorf("the sheet has more than %d rows", importMaxRows)
		}
		get := func(key string) string {
			i, ok := col[key]
			if !ok || i >= len(cells) {
				return ""
			}
			return cells[i]
		}
		r := ImportRow{Line: line, Category: get("category"), Name: get("name"), NameEn: get("name_en"),
			Description: get("description"), Station: get("station"), Errors: []string{},
			Photos: []string{}, Specs: []OptionGroupInput{}}
		bad := func(code string) { r.Errors = append(r.Errors, code) }
		if r.Category == "" {
			bad("category_required")
		}
		if r.Name == "" {
			bad("name_required")
		}
		if utf8.RuneCountInString(r.Category) > 40 || utf8.RuneCountInString(r.Name) > 80 ||
			utf8.RuneCountInString(r.NameEn) > 80 || utf8.RuneCountInString(r.Description) > 300 {
			bad("text_too_long")
		}
		if p := get("price"); p == "" {
			bad("price_required")
		} else if v, ok := parsePrice(p); !ok {
			bad("price_invalid")
		} else {
			r.Price = v
		}
		yesNo := false
		for _, c := range []struct {
			key string
			dst **bool
		}{{"needs_prep", &r.NeedsPrep}, {"delivery", &r.Delivery}, {"pickup", &r.Pickup},
			{"dine_in", &r.DineIn}, {"published", &r.Published}} {
			v, ok := parseYesNo(get(c.key))
			if !ok {
				yesNo = true
			}
			*c.dst = v
		}
		if yesNo {
			bad("yes_no_invalid")
		}
		if s := get("stock"); s != "" {
			r.StockSet = true
			if s != "不限" && !strings.EqualFold(s, "unlimited") {
				n, err := strconv.ParseFloat(s, 64)
				if err != nil || n < 0 || n > 100000 || n != math.Trunc(n) {
					bad("stock_invalid")
				} else {
					v := int(n)
					r.Stock = &v
				}
			}
		}
		if s := get("image"); s != "" {
			r.ImageSet = true
			if !isClear(s) {
				if validImageRef(s) {
					r.ImageURL = s
				} else {
					bad("image_invalid")
				}
			}
		}
		if s := get("photos"); s != "" {
			r.PhotosSet = true
			if !isClear(s) {
				ps := splitPhotos(s)
				for _, p := range ps {
					if !validImageRef(p) {
						bad("image_invalid")
						break
					}
				}
				if len(ps) > maxItemPhotos {
					bad(fmt.Sprintf("photos_too_many:%d", maxItemPhotos))
				}
				r.Photos = ps
			}
		}
		if s := get("details"); s != "" {
			r.DetailsSet = true
			if !isClear(s) {
				r.Details = s
				if utf8.RuneCountInString(s) > maxItemDetails {
					bad(fmt.Sprintf("details_too_long:%d", maxItemDetails))
				}
			}
		}
		if s := get("specs"); s != "" {
			r.SpecsSet = true
			if !isClear(s) {
				groups, code := parseSpecs(s)
				if code != "" {
					bad(code)
				} else {
					r.Specs = groups
				}
			}
		}
		if r.Category != "" && r.Name != "" {
			key := r.Category + "\x00" + r.Name
			if first, dup := seen[key]; dup {
				r.Errors = append(r.Errors, fmt.Sprintf("duplicate_of_line:%d", first))
			} else {
				seen[key] = line
			}
		}
		out = append(out, r)
	}
	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("the sheet could not be read")
	}
	if !header {
		return nil, fmt.Errorf("the sheet is empty")
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the sheet has a header but no rows")
	}
	return out, nil
}

// importContext is the branch's current menu, used to decide create/update
// and to resolve station names.
type importContext struct {
	cats     map[string]int64  // category name -> id
	items    map[string]int64  // category id + name -> item id
	prices   map[int64]int64   // item id -> price
	stations map[string]*int64 // active station name -> id
}

func loadImportContext(ctx context.Context, q Querier, branchID int64) (*importContext, error) {
	ic := &importContext{cats: map[string]int64{}, items: map[string]int64{}, prices: map[int64]int64{}, stations: map[string]*int64{}}
	rows, err := q.Query(ctx, `SELECT id, name FROM menu_categories WHERE branch_id=$1 AND NOT archived ORDER BY id DESC`, branchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		ic.cats[name] = id // oldest wins when names repeat
	}
	rows.Close()
	rows, err = q.Query(ctx, `SELECT id, category_id, name, price FROM menu_items WHERE branch_id=$1 AND NOT archived ORDER BY id DESC`, branchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, cat, price int64
		var name string
		if err := rows.Scan(&id, &cat, &name, &price); err != nil {
			rows.Close()
			return nil, err
		}
		ic.items[fmt.Sprintf("%d\x00%s", cat, name)] = id
		ic.prices[id] = price
	}
	rows.Close()
	rows, err = q.Query(ctx, `SELECT id, name FROM kitchen_stations WHERE branch_id=$1 AND active`, branchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		ic.stations[name] = &id
	}
	rows.Close()
	return ic, rows.Err()
}

// resolve fills Action and OldPrice and adds errors that depend on the
// current menu. It returns the existing item id for an update.
func (ic *importContext) resolve(r *ImportRow) int64 {
	if r.Station != "" {
		if _, ok := ic.stations[r.Station]; !ok {
			r.Errors = append(r.Errors, "station_unknown")
		}
	}
	r.Action, r.OldPrice, r.NewCategory = "", nil, false
	if len(r.Errors) > 0 {
		return 0
	}
	r.Action = "create"
	cat, ok := ic.cats[r.Category]
	if !ok {
		r.NewCategory = true
		return 0
	}
	if id, ok := ic.items[fmt.Sprintf("%d\x00%s", cat, r.Name)]; ok {
		old := ic.prices[id]
		r.Action, r.OldPrice = "update", &old
		return id
	}
	return 0
}

// parseMenuImport is the background job: parse the stored file and save the
// row-level result for review. Nothing in the menu changes yet.
func (a *App) parseMenuImport(ctx context.Context, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var branchID int64
		var status string
		var content []byte
		err := tx.QueryRow(ctx, `SELECT branch_id, status, content FROM menu_imports WHERE id=$1 FOR UPDATE`, id).
			Scan(&branchID, &status, &content)
		if isNoRows(err) || (err == nil && status != "pending") {
			return nil // gone or already handled: nothing to retry
		} else if err != nil {
			return err
		}
		rows, perr := parseMenuSheet(content)
		if perr != nil {
			_, err := tx.Exec(ctx, `UPDATE menu_imports SET status='failed', error=$2, content=NULL WHERE id=$1`, id, perr.Error())
			if err != nil {
				return err
			}
			return branchChanged(ctx, tx, branchID)
		}
		ic, err := loadImportContext(ctx, tx, branchID)
		if err != nil {
			return err
		}
		valid := 0
		for i := range rows {
			ic.resolve(&rows[i])
			if len(rows[i].Errors) == 0 {
				valid++
			}
		}
		b, _ := json.Marshal(rows)
		if _, err := tx.Exec(ctx, `UPDATE menu_imports SET status='parsed', rows=$2, total_rows=$3, valid_rows=$4,
			error_rows=$5, content=NULL WHERE id=$1`, id, b, len(rows), valid, len(rows)-valid); err != nil {
			return err
		}
		return branchChanged(ctx, tx, branchID)
	})
}

// writeGroups replaces a dish's option groups.
func writeGroups(ctx context.Context, tx pgx.Tx, itemID int64, groups []OptionGroupInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM option_groups WHERE item_id=$1`, itemID); err != nil {
		return err
	}
	for gi, g := range groups {
		var gid int64
		if err := tx.QueryRow(ctx, `INSERT INTO option_groups (item_id, name, name_en, min_select, max_select, sort)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, itemID, g.Name, g.NameEn, g.MinSelect, g.MaxSelect, gi).Scan(&gid); err != nil {
			return err
		}
		for oi, o := range g.Options {
			if _, err := tx.Exec(ctx, `INSERT INTO options (group_id, name, name_en, price_delta, sold_out, sort)
				VALUES ($1,$2,$3,$4,$5,$6)`, gid, o.Name, o.NameEn, o.PriceDelta, o.SoldOut, oi); err != nil {
				return err
			}
		}
	}
	return nil
}

// ApplyMenuImport writes the valid rows of a parsed import. New dishes are
// created as drafts unless the 上架 column says 是; rows that match an
// existing dish by category and name update it in place, and a blank cell
// keeps what the dish has. Rows with errors are skipped. The import can be
// applied once.
func (a *App) ApplyMenuImport(ctx context.Context, branchID, id int64, actor Actor) (*MenuImport, error) {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var status string
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT status, rows FROM menu_imports WHERE id=$1 AND branch_id=$2 FOR UPDATE`, id, branchID).
			Scan(&status, &raw)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "parsed" {
			return ErrInvalidState("this import cannot be applied (status: " + status + ")")
		}
		var rows []ImportRow
		if err := json.Unmarshal(raw, &rows); err != nil {
			return err
		}
		// The menu may have changed since the preview: decide again now.
		ic, err := loadImportContext(ctx, tx, branchID)
		if err != nil {
			return err
		}
		created, updated, valid, newCats := 0, 0, 0, 0
		for i := range rows {
			r := &rows[i]
			kept := r.Errors[:0]
			for _, e := range r.Errors {
				if e != "station_unknown" {
					kept = append(kept, e)
				}
			}
			r.Errors = kept
			itemID := ic.resolve(r)
			if len(r.Errors) > 0 {
				continue
			}
			valid++
			catID, ok := ic.cats[r.Category]
			if !ok {
				if err := tx.QueryRow(ctx, `INSERT INTO menu_categories (branch_id, name, sort)
					VALUES ($1, $2, (SELECT COALESCE(max(sort), -1) + 1 FROM menu_categories WHERE branch_id=$1))
					RETURNING id`, branchID, r.Category).Scan(&catID); err != nil {
					return err
				}
				ic.cats[r.Category] = catID
				newCats++
			}
			var station *int64
			if r.Station != "" {
				station = ic.stations[r.Station]
			}
			photos := r.Photos
			if photos == nil {
				photos = []string{}
			}
			if itemID == 0 {
				needsPrep := r.NeedsPrep == nil || *r.NeedsPrep
				if !needsPrep {
					station = nil
				}
				if err := tx.QueryRow(ctx, `INSERT INTO menu_items (branch_id, category_id, name, name_en, description,
					price, published, stock_qty, needs_prep, delivery_available, pickup_available, dine_in_available,
					station_id, image_url, photos, details, sort)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
						(SELECT COALESCE(max(sort), -1) + 1 FROM menu_items WHERE category_id=$2))
					RETURNING id`, branchID, catID, r.Name, r.NameEn, r.Description, r.Price,
					r.Published != nil && *r.Published, r.Stock, needsPrep,
					r.Delivery == nil || *r.Delivery, r.Pickup == nil || *r.Pickup, r.DineIn == nil || *r.DineIn,
					station, r.ImageURL, photos, r.Details).Scan(&itemID); err != nil {
					return err
				}
				if len(r.Specs) > 0 {
					if err := writeGroups(ctx, tx, itemID, r.Specs); err != nil {
						return err
					}
				}
				ic.items[fmt.Sprintf("%d\x00%s", catID, r.Name)] = itemID
				created++
				continue
			}
			// Update: a blank cell keeps the current value.
			if _, err := tx.Exec(ctx, `UPDATE menu_items SET price=$2,
				name_en = CASE WHEN $3 <> '' THEN $3 ELSE name_en END,
				description = CASE WHEN $4 <> '' THEN $4 ELSE description END,
				needs_prep = COALESCE($5, needs_prep),
				delivery_available = COALESCE($6, delivery_available),
				pickup_available = COALESCE($7, pickup_available),
				stock_qty = CASE WHEN $8 THEN $9 ELSE stock_qty END,
				station_id = CASE WHEN $10 <> '' THEN $11 ELSE station_id END,
				dine_in_available = COALESCE($13, dine_in_available),
				published = COALESCE($14, published),
				image_url = CASE WHEN $15 THEN $16 ELSE image_url END,
				photos = CASE WHEN $17 THEN $18::jsonb ELSE photos END,
				details = CASE WHEN $19 THEN $20 ELSE details END,
				updated_at = now() WHERE id=$1 AND branch_id=$12`,
				itemID, r.Price, r.NameEn, r.Description, r.NeedsPrep, r.Delivery, r.Pickup, r.StockSet, r.Stock,
				r.Station, station, branchID, r.DineIn, r.Published, r.ImageSet, r.ImageURL, r.PhotosSet, photos,
				r.DetailsSet, r.Details); err != nil {
				return err
			}
			if r.SpecsSet {
				if err := writeGroups(ctx, tx, itemID, r.Specs); err != nil {
					return err
				}
			}
			updated++
		}
		b, _ := json.Marshal(rows)
		if _, err := tx.Exec(ctx, `UPDATE menu_imports SET status='applied', applied_at=now(), rows=$2, valid_rows=$3,
			error_rows=$4, created_count=$5, updated_count=$6 WHERE id=$1`, id, b, valid, len(rows)-valid, created, updated); err != nil {
			return err
		}
		if err := audit(ctx, tx, branchID, 0, actor, "menu.imported",
			map[string]any{"import_id": id, "created": created, "updated": updated, "skipped": len(rows) - valid,
				"new_categories": newCats}); err != nil {
			return err
		}
		return branchChanged(ctx, tx, branchID)
	})
	if err != nil {
		return nil, err
	}
	return a.GetMenuImport(ctx, branchID, id)
}

// ---- platform admins import for a store ----

func (a *App) AdminCreateMenuImport(ctx context.Context, actor AdminActor, branchID int64, filename string, content []byte) (int64, error) {
	if err := a.liveBranch(ctx, branchID); err != nil {
		return 0, err
	}
	return a.CreateMenuImport(ctx, branchID, filename, content, Actor{Type: "admin", ID: actor.ID})
}

func (a *App) AdminApplyMenuImport(ctx context.Context, actor AdminActor, branchID, id int64) (*MenuImport, error) {
	if err := a.liveBranch(ctx, branchID); err != nil {
		return nil, err
	}
	m, err := a.ApplyMenuImport(ctx, branchID, id, Actor{Type: "admin", ID: actor.ID})
	if err != nil {
		return nil, err
	}
	return m, adminAudit(ctx, a.DB, actor, "menu.import", "branch", branchID, map[string]any{
		"import_id": id, "filename": m.Filename, "created": m.CreatedCount, "updated": m.UpdatedCount,
		"skipped": m.TotalRows - m.ValidRows})
}
