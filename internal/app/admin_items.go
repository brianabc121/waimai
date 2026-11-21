package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Menu management from the platform back office. The rules are the merchant's
// own use cases in menu.go; these wrappers only check that the branch is live
// and record who changed what.

func (a *App) liveBranch(ctx context.Context, branchID int64) error {
	var ok bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM branches b JOIN merchants m ON m.id = b.merchant_id
		WHERE b.id=$1 AND b.deleted_at IS NULL AND m.deleted_at IS NULL)`, branchID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

func (a *App) AdminSaveCategory(ctx context.Context, actor AdminActor, branchID, id int64, in CategoryInput) (int64, error) {
	if err := a.liveBranch(ctx, branchID); err != nil {
		return 0, err
	}
	action := "category.update"
	if id == 0 {
		action = "category.create"
	}
	id, err := a.SaveCategory(ctx, branchID, id, in)
	if err != nil {
		return 0, err
	}
	return id, adminAudit(ctx, a.DB, actor, action, "category", id, map[string]any{"branch_id": branchID, "name": in.Name})
}

// AdminArchiveCategory removes an empty category from the menu. Archived items
// may still point at it, so the row stays.
func (a *App) AdminArchiveCategory(ctx context.Context, actor AdminActor, branchID, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var name string
		var items int
		err := tx.QueryRow(ctx, `SELECT c.name, (SELECT count(*) FROM menu_items i WHERE i.category_id = c.id AND NOT i.archived)
			FROM menu_categories c JOIN branches b ON b.id = c.branch_id
			WHERE c.id=$1 AND c.branch_id=$2 AND NOT c.archived AND b.deleted_at IS NULL FOR UPDATE OF c`, id, branchID).
			Scan(&name, &items)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if items > 0 {
			return ErrInvalidState("move or delete the items in this category first")
		}
		if _, err := tx.Exec(ctx, `UPDATE menu_categories SET archived=true WHERE id=$1`, id); err != nil {
			return err
		}
		if err := branchChanged(ctx, tx, branchID); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "category.delete", "category", id, map[string]any{"branch_id": branchID, "name": name})
	})
}

func (a *App) AdminSaveItem(ctx context.Context, actor AdminActor, branchID, id int64, in ItemInput) (int64, error) {
	if err := a.liveBranch(ctx, branchID); err != nil {
		return 0, err
	}
	action := "item.update"
	if id == 0 {
		action = "item.create"
	}
	id, err := a.SaveItem(ctx, branchID, id, in)
	if err != nil {
		return 0, err
	}
	return id, adminAudit(ctx, a.DB, actor, action, "item", id, map[string]any{
		"branch_id": branchID, "name": in.Name, "price": in.Price, "published": in.Published})
}

func (a *App) AdminPatchItem(ctx context.Context, actor AdminActor, branchID, id int64, p ItemPatch) error {
	if err := a.liveBranch(ctx, branchID); err != nil {
		return err
	}
	if err := a.PatchItem(ctx, branchID, id, p); err != nil {
		return err
	}
	action := "item.update"
	if p.Archived != nil && *p.Archived {
		action = "item.delete"
	}
	return adminAudit(ctx, a.DB, actor, action, "item", id, map[string]any{"branch_id": branchID, "patch": p})
}

// AdminBulkItems applies one quick change to several items of a branch and
// returns how many changed. Action is publish, unpublish, sold_out, in_stock
// or archive.
func (a *App) AdminBulkItems(ctx context.Context, actor AdminActor, branchID int64, ids []int64, action string) (int, error) {
	set, ok := map[string]string{
		"publish":   "published = true",
		"unpublish": "published = false",
		"sold_out":  "sold_out = true",
		"in_stock":  "sold_out = false",
		"archive":   "archived = true",
	}[action]
	if !ok {
		return 0, ErrValidation("unknown action")
	}
	if len(ids) == 0 || len(ids) > 500 {
		return 0, ErrValidation("pick 1 to 500 items")
	}
	if err := a.liveBranch(ctx, branchID); err != nil {
		return 0, err
	}
	n := 0
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE menu_items SET `+set+`, updated_at = now()
			WHERE branch_id=$1 AND id = ANY($2) AND NOT archived`, branchID, ids)
		if err != nil {
			return err
		}
		n = int(tag.RowsAffected())
		if err := branchChanged(ctx, tx, branchID); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "item.bulk_"+action, "branch", branchID, map[string]any{"ids": ids, "changed": n})
	})
	return n, err
}

// AdminItemRow is one line of the cross-merchant item search.
type AdminItemRow struct {
	ID           int64     `json:"id"`
	BranchID     int64     `json:"branch_id"`
	BranchName   string    `json:"branch_name"`
	MerchantID   int64     `json:"merchant_id"`
	MerchantName string    `json:"merchant_name"`
	Currency     string    `json:"currency"`
	CategoryID   int64     `json:"category_id"`
	CategoryName string    `json:"category_name"`
	StationName  string    `json:"station_name"`
	Name         string    `json:"name"`
	NameEn       string    `json:"name_en"`
	ImageURL     string    `json:"image_url"`
	Price        int64     `json:"price"`
	Published    bool      `json:"published"`
	SoldOut      bool      `json:"sold_out"`
	StockQty     *int      `json:"stock_qty"`
	NeedsPrep    bool      `json:"needs_prep"`
	GroupCount   int       `json:"group_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ItemFilter struct {
	Q          string
	MerchantID int64
	BranchID   int64
	Status     string // "", on_sale, off, sold_out
	Page
}

type ItemList struct {
	Items  []AdminItemRow `json:"items"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
}

// SearchItems finds items across merchants. Sold out includes limited items
// whose stock ran out, the way customers see it.
func (a *App) SearchItems(ctx context.Context, f ItemFilter) (*ItemList, error) {
	switch f.Status {
	case "", "on_sale", "off", "sold_out":
	default:
		return nil, ErrValidation("unknown status")
	}
	const from = ` FROM menu_items i JOIN branches b ON b.id = i.branch_id JOIN merchants m ON m.id = b.merchant_id
		JOIN menu_categories c ON c.id = i.category_id LEFT JOIN kitchen_stations s ON s.id = i.station_id
		WHERE NOT i.archived AND b.deleted_at IS NULL AND m.deleted_at IS NULL
		AND ($1 = '' OR i.name ILIKE $1 OR i.name_en ILIKE $1)
		AND ($2::bigint = 0 OR m.id = $2) AND ($3::bigint = 0 OR b.id = $3)`
	const soldOut = `(i.sold_out OR coalesce(i.stock_qty, 1) <= 0)`
	out := &ItemList{Items: []AdminItemRow{}, Counts: map[string]int{}}
	pat := like(f.Q)
	var all, onSale, off, sold int
	if err := a.DB.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE i.published),
		count(*) FILTER (WHERE NOT i.published), count(*) FILTER (WHERE `+soldOut+`)`+from,
		pat, f.MerchantID, f.BranchID).Scan(&all, &onSale, &off, &sold); err != nil {
		return nil, err
	}
	out.Counts = map[string]int{"all": all, "on_sale": onSale, "off": off, "sold_out": sold}
	out.Total = out.Counts[f.Status]
	if f.Status == "" {
		out.Total = all
	}
	limit, offset := f.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT i.id, b.id, b.name, m.id, m.name, b.currency, c.id, c.name, coalesce(s.name, ''),
		i.name, i.name_en, i.image_url, i.price, i.published, i.sold_out, i.stock_qty, i.needs_prep,
		(SELECT count(*) FROM option_groups g WHERE g.item_id = i.id), i.updated_at`+from+`
		AND CASE $4 WHEN 'on_sale' THEN i.published WHEN 'off' THEN NOT i.published WHEN 'sold_out' THEN `+soldOut+` ELSE true END
		ORDER BY m.id, b.id, c.sort, c.id, i.sort, i.id LIMIT $5 OFFSET $6`,
		pat, f.MerchantID, f.BranchID, f.Status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r AdminItemRow
		if err := rows.Scan(&r.ID, &r.BranchID, &r.BranchName, &r.MerchantID, &r.MerchantName, &r.Currency,
			&r.CategoryID, &r.CategoryName, &r.StationName, &r.Name, &r.NameEn, &r.ImageURL, &r.Price, &r.Published,
			&r.SoldOut, &r.StockQty, &r.NeedsPrep, &r.GroupCount, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, r)
	}
	return out, rows.Err()
}
