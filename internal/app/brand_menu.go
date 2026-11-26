package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Brand menus (docs/MULTI_BRAND_DESIGN.md §5). The store's dishes are one
// list, the kitchen's: options, toppings, notes, stations, stock and sold-out
// all stay on the dish. A brand with its own menu picks dishes from that list
// into its own categories and may give each its own name, picture and price.
// The brand price is only what a line starts from: the order keeps it in its
// own snapshot (order_items.unit_price) like any price, so the pricing engine
// does not change.

// BrandCategory is a category of a brand's own menu.
type BrandCategory struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	Sort   int    `json:"sort"`
}

// BrandItem is a dish on a brand's own menu; empty name, picture and a nil
// price mean the dish's own.
type BrandItem struct {
	ItemID     int64  `json:"item_id"`
	CategoryID *int64 `json:"category_id"`
	Name       string `json:"name"`
	NameEn     string `json:"name_en"`
	ImageURL   string `json:"image_url"`
	Price      *int64 `json:"price"`
	Published  bool   `json:"published"`
	Sort       int    `json:"sort"`
}

// BrandMenu is a brand's own menu as the back office edits it.
type BrandMenu struct {
	BrandID    int64           `json:"brand_id"`
	OwnMenu    bool            `json:"own_menu"`
	Categories []BrandCategory `json:"categories"`
	Items      []BrandItem     `json:"items"`
}

func loadBrandMenu(ctx context.Context, q Querier, brandID int64) (*BrandMenu, error) {
	m := &BrandMenu{BrandID: brandID, Categories: []BrandCategory{}, Items: []BrandItem{}}
	if err := q.QueryRow(ctx, `SELECT own_menu FROM brands WHERE id=$1`, brandID).Scan(&m.OwnMenu); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT id, name, name_en, sort FROM brand_categories WHERE brand_id=$1 ORDER BY sort, id`, brandID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c BrandCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.NameEn, &c.Sort); err != nil {
			rows.Close()
			return nil, err
		}
		m.Categories = append(m.Categories, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT bi.item_id, bi.category_id, bi.name, bi.name_en, bi.image_url, bi.price, bi.published, bi.sort
		FROM brand_items bi JOIN menu_items i ON i.id = bi.item_id
		WHERE bi.brand_id=$1 AND NOT i.archived ORDER BY bi.sort, bi.item_id`, brandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it BrandItem
		if err := rows.Scan(&it.ItemID, &it.CategoryID, &it.Name, &it.NameEn, &it.ImageURL, &it.Price, &it.Published, &it.Sort); err != nil {
			return nil, err
		}
		m.Items = append(m.Items, it)
	}
	return m, rows.Err()
}

// GetBrandMenu is a brand's own menu for the back office.
func (a *App) GetBrandMenu(ctx context.Context, branchID, brandID int64) (*BrandMenu, error) {
	if _, err := loadBrand(ctx, a.DB, branchID, brandID); err != nil {
		return nil, err
	}
	return loadBrandMenu(ctx, a.DB, brandID)
}

// BrandMenuInput replaces a brand's own menu. Categories are matched to
// items by Key (a new category has none yet).
type BrandMenuInput struct {
	OwnMenu    bool `json:"own_menu"`
	Categories []struct {
		Key    string `json:"key"`
		Name   string `json:"name"`
		NameEn string `json:"name_en"`
	} `json:"categories"`
	Items []struct {
		ItemID      int64  `json:"item_id"`
		CategoryKey string `json:"category_key"`
		Name        string `json:"name"`
		NameEn      string `json:"name_en"`
		ImageURL    string `json:"image_url"`
		Price       *int64 `json:"price"`
		Published   bool   `json:"published"`
	} `json:"items"`
}

// SaveBrandMenu replaces a brand's own menu: its categories, in order, and
// its dishes, in order.
func (a *App) SaveBrandMenu(ctx context.Context, branchID, brandID int64, actor Actor, in BrandMenuInput) (*BrandMenu, error) {
	if len(in.Categories) > 100 || len(in.Items) > 2000 {
		return nil, ErrValidation("too many categories or dishes")
	}
	keys := map[string]bool{}
	for i := range in.Categories {
		c := &in.Categories[i]
		c.Name, c.NameEn = strings.TrimSpace(c.Name), strings.TrimSpace(c.NameEn)
		if c.Name == "" || len([]rune(c.Name)) > 40 || len(c.NameEn) > 80 {
			return nil, ErrValidation("a category needs a name of up to 40 characters")
		}
		if c.Key == "" || keys[c.Key] {
			return nil, ErrValidation("each category needs its own key")
		}
		keys[c.Key] = true
	}
	seen := map[int64]bool{}
	for i := range in.Items {
		it := &in.Items[i]
		it.Name, it.NameEn = strings.TrimSpace(it.Name), strings.TrimSpace(it.NameEn)
		if seen[it.ItemID] {
			return nil, ErrValidation("a dish is on the menu twice")
		}
		seen[it.ItemID] = true
		if len([]rune(it.Name)) > 80 || len(it.NameEn) > 160 || len(it.ImageURL) > 500 {
			return nil, ErrValidation("dish name or picture too long")
		}
		if it.Price != nil && (*it.Price < 0 || *it.Price > 100_000_000) {
			return nil, ErrValidation("price out of range")
		}
		if it.CategoryKey != "" && !keys[it.CategoryKey] {
			return nil, ErrValidation("unknown category")
		}
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT id FROM brands WHERE branch_id=$1 AND id=$2 FOR UPDATE`, branchID, brandID); err != nil {
			return err
		}
		if _, err := loadBrand(ctx, tx, branchID, brandID); err != nil {
			return err
		}
		ids := make([]int64, 0, len(in.Items))
		for _, it := range in.Items {
			ids = append(ids, it.ItemID)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM menu_items WHERE branch_id=$1 AND id = ANY($2) AND NOT archived`,
			branchID, ids).Scan(&n); err != nil {
			return err
		}
		if n != len(ids) {
			return Err(http.StatusUnprocessableEntity, "ITEM_UNKNOWN", "a dish is not on the store's menu")
		}
		if in.OwnMenu && len(ids) == 0 {
			return Err(http.StatusUnprocessableEntity, "BRAND_MENU_EMPTY", "pick at least one dish")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM brand_items WHERE brand_id=$1`, brandID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM brand_categories WHERE brand_id=$1`, brandID); err != nil {
			return err
		}
		catID := map[string]int64{}
		for i, c := range in.Categories {
			var id int64
			if err := tx.QueryRow(ctx, `INSERT INTO brand_categories (brand_id, name, name_en, sort) VALUES ($1,$2,$3,$4) RETURNING id`,
				brandID, c.Name, c.NameEn, i).Scan(&id); err != nil {
				return err
			}
			catID[c.Key] = id
		}
		for i, it := range in.Items {
			var cat *int64
			if id, ok := catID[it.CategoryKey]; ok {
				cat = &id
			}
			if _, err := tx.Exec(ctx, `INSERT INTO brand_items (brand_id, item_id, category_id, name, name_en, image_url, price, published, sort)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, brandID, it.ItemID, cat, it.Name, it.NameEn, it.ImageURL, it.Price, it.Published, i); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE brands SET own_menu=$2 WHERE id=$1`, brandID, in.OwnMenu); err != nil {
			return err
		}
		return audit(ctx, tx, branchID, 0, actor, "brand.menu", map[string]any{"brand_id": brandID, "own_menu": in.OwnMenu,
			"categories": len(in.Categories), "items": len(in.Items)})
	})
	if err != nil {
		return nil, wrap("save brand menu", err)
	}
	return loadBrandMenu(ctx, a.DB, brandID)
}

// brandItems is the store's dishes as the brand sells them: all of them as
// they are when the brand has no menu of its own, else the ones it picked
// (published ones only when publishedOnly), under its names and prices.
func brandItems(ctx context.Context, q Querier, branchID int64, brand *Brand, publishedOnly bool) ([]*Item, map[int64]*int64, error) {
	items, err := loadItems(ctx, q, branchID, publishedOnly)
	if err != nil || brand == nil || !brand.OwnMenu {
		return items, nil, err
	}
	m, err := loadBrandMenu(ctx, q, brand.ID)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[int64]*Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	out := make([]*Item, 0, len(m.Items))
	cats := map[int64]*int64{}
	for _, bi := range m.Items {
		it, ok := byID[bi.ItemID]
		if !ok || (publishedOnly && !bi.Published) {
			continue
		}
		if bi.Name != "" && bi.Name != it.Name {
			it.BaseName = it.Name
			it.Name, it.NameEn = bi.Name, bi.NameEn
			// the store's translations are of the store's name
			it.NameTr = ""
		} else if bi.NameEn != "" {
			it.NameEn = bi.NameEn
		}
		if bi.ImageURL != "" {
			it.ImageURL = bi.ImageURL
		}
		if bi.Price != nil {
			it.Price = *bi.Price
		}
		it.Published = it.Published && bi.Published
		it.Sort = bi.Sort
		cats[it.ID] = bi.CategoryID
		out = append(out, it)
	}
	return out, cats, nil
}

// brandMenuCategories is the menu a brand shows: the store's own when the
// brand has none, else the brand's categories, then "More" for dishes it put
// in none.
func brandMenuCategories(ctx context.Context, q Querier, branchID int64, brand *Brand, publishedOnly bool) ([]Category, error) {
	if brand == nil || !brand.OwnMenu {
		return loadMenu(ctx, q, branchID, publishedOnly)
	}
	items, catOf, err := brandItems(ctx, q, branchID, brand, publishedOnly)
	if err != nil {
		return nil, err
	}
	m, err := loadBrandMenu(ctx, q, brand.ID)
	if err != nil {
		return nil, err
	}
	cats := make([]Category, 0, len(m.Categories)+1)
	idx := map[int64]int{}
	for _, c := range m.Categories {
		idx[c.ID] = len(cats)
		cats = append(cats, Category{ID: c.ID, Name: c.Name, NameEn: c.NameEn, Sort: c.Sort, Items: []Item{}, brand: true})
	}
	more := -1
	for _, it := range items {
		if c := catOf[it.ID]; c != nil {
			if i, ok := idx[*c]; ok {
				cats[i].Items = append(cats[i].Items, *it)
				continue
			}
		}
		if more < 0 {
			more = len(cats)
			cats = append(cats, Category{ID: -1, Name: "其他", NameEn: "More", Sort: len(cats), Items: []Item{}, brand: true})
		}
		cats[more].Items = append(cats[more].Items, *it)
	}
	return cats, nil
}

// brandOfOrder is the brand an order was placed under.
func brandOfOrder(ctx context.Context, q Querier, orderID int64) (*Brand, error) {
	return scanBrand(q.QueryRow(ctx, `SELECT `+brandCols+` FROM brands WHERE id = (SELECT brand_id FROM orders WHERE id=$1)`, orderID))
}
