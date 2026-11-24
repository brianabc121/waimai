package app

// Topping templates: extras with a price that a store writes once and many
// dishes share (麻辣烫: AC02 上海青 +20, AC03 油麦菜 +30). A dish that uses a
// template gets an ordinary option group built from it (option_groups.
// topping_template_id), kept in step whenever the template or the dish changes.
// Ordering, the price, the kitchen, receipts and translations then treat it
// like any other option, and the ids of its options stay the same across
// edits, so carts and translations survive.

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	maxToppingOptions       = 50
	maxItemToppingTemplates = 3
	defaultToppingTitle     = "加料"
)

type ToppingOption struct {
	ID      int64  `json:"id"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	NameEn  string `json:"name_en"`
	Price   int64  `json:"price"`
	SoldOut bool   `json:"sold_out"`
}

type ToppingTemplate struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Title     string          `json:"title"`
	MaxSelect int             `json:"max_select"`
	Options   []ToppingOption `json:"options"`
	ItemIDs   []int64         `json:"item_ids"`
	UpdatedAt string          `json:"updated_at"`
}

type ToppingOptionInput struct {
	ID      int64  `json:"id"` // 0 for a new one
	Code    string `json:"code"`
	Name    string `json:"name"`
	NameEn  string `json:"name_en"`
	Price   int64  `json:"price"`
	SoldOut bool   `json:"sold_out"`
}

type ToppingTemplateInput struct {
	Name      string               `json:"name"`
	Title     string               `json:"title"`
	MaxSelect int                  `json:"max_select"`
	Options   []ToppingOptionInput `json:"options"`
}

func (a *App) ToppingTemplates(ctx context.Context, branchID int64) ([]ToppingTemplate, error) {
	rows, err := a.DB.Query(ctx, `SELECT id, name, title, max_select, to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
		FROM topping_templates WHERE branch_id=$1 ORDER BY sort, id`, branchID)
	if err != nil {
		return nil, err
	}
	list := []ToppingTemplate{}
	byID := map[int64]int{}
	for rows.Next() {
		t := ToppingTemplate{Options: []ToppingOption{}, ItemIDs: []int64{}}
		if err := rows.Scan(&t.ID, &t.Name, &t.Title, &t.MaxSelect, &t.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		byID[t.ID] = len(list)
		list = append(list, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = a.DB.Query(ctx, `SELECT o.template_id, o.id, o.code, o.name, o.name_en, o.price, o.sold_out
		FROM topping_options o JOIN topping_templates t ON t.id = o.template_id WHERE t.branch_id=$1 ORDER BY o.sort, o.id`, branchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tid int64
		var o ToppingOption
		if err := rows.Scan(&tid, &o.ID, &o.Code, &o.Name, &o.NameEn, &o.Price, &o.SoldOut); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := byID[tid]; ok {
			list[i].Options = append(list[i].Options, o)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = a.DB.Query(ctx, `SELECT l.template_id, l.item_id FROM menu_item_topping_templates l
		JOIN menu_items i ON i.id = l.item_id WHERE i.branch_id=$1 AND NOT i.archived ORDER BY i.sort, i.id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tid, iid int64
		if err := rows.Scan(&tid, &iid); err != nil {
			return nil, err
		}
		if i, ok := byID[tid]; ok {
			list[i].ItemIDs = append(list[i].ItemIDs, iid)
		}
	}
	return list, rows.Err()
}

func (in *ToppingTemplateInput) clean() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Title = strings.TrimSpace(in.Title)
	if in.Name == "" || len([]rune(in.Name)) > 30 {
		return ErrValidation("the template needs a name of at most 30 characters")
	}
	if in.Title == "" {
		in.Title = defaultToppingTitle
	}
	if len([]rune(in.Title)) > 20 {
		return ErrValidation("the title must be at most 20 characters")
	}
	if len(in.Options) == 0 || len(in.Options) > maxToppingOptions {
		return ErrValidation(fmt.Sprintf("a template has 1 to %d toppings", maxToppingOptions))
	}
	if in.MaxSelect < 0 || in.MaxSelect > len(in.Options) {
		return ErrValidation("max_select is invalid")
	}
	codes := map[string]bool{}
	names := map[string]bool{}
	for i := range in.Options {
		o := &in.Options[i]
		o.Code = strings.ToUpper(strings.TrimSpace(o.Code))
		o.Name = strings.TrimSpace(o.Name)
		o.NameEn = strings.TrimSpace(o.NameEn)
		if len([]rune(o.Code)) > 12 || strings.ContainsAny(o.Code, " \t") {
			return ErrValidation("a topping code is at most 12 characters without spaces")
		}
		if o.Name == "" || len([]rune(o.Name)) > 30 {
			return ErrValidation("every topping needs a name of at most 30 characters")
		}
		if o.Price < 0 {
			return ErrValidation("a topping price must not be negative")
		}
		if o.Code != "" && codes[o.Code] {
			return ErrD(http.StatusUnprocessableEntity, "TOPPING_CODE_TWICE", "two toppings have the same code", map[string]any{"code": o.Code})
		}
		if names[o.Name] {
			return ErrD(http.StatusUnprocessableEntity, "TOPPING_NAME_TWICE", "two toppings have the same name", map[string]any{"name": o.Name})
		}
		codes[o.Code], names[o.Name] = o.Code != "", true
	}
	return nil
}

// SaveToppingTemplate creates (id 0) or replaces a template; every dish using
// it follows at once.
func (a *App) SaveToppingTemplate(ctx context.Context, branchID, id int64, in ToppingTemplateInput) (int64, error) {
	if err := in.clean(); err != nil {
		return 0, err
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var other int64
		err := tx.QueryRow(ctx, `SELECT id FROM topping_templates WHERE branch_id=$1 AND name=$2 AND id<>$3`, branchID, in.Name, id).Scan(&other)
		if err == nil {
			return ErrD(http.StatusConflict, "TOPPING_TEMPLATE_NAME_TAKEN", "another topping template has this name", nil)
		} else if !isNoRows(err) {
			return err
		}
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO topping_templates (branch_id, name, title, max_select, sort)
				VALUES ($1,$2,$3,$4,(SELECT count(*) FROM topping_templates WHERE branch_id=$1)) RETURNING id`,
				branchID, in.Name, in.Title, in.MaxSelect).Scan(&id)
		} else {
			err = tx.QueryRow(ctx, `UPDATE topping_templates SET name=$3, title=$4, max_select=$5, updated_at=now()
				WHERE id=$1 AND branch_id=$2 RETURNING id`, id, branchID, in.Name, in.Title, in.MaxSelect).Scan(&id)
		}
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		keep := []int64{}
		for i, o := range in.Options {
			if o.ID != 0 {
				tag, err := tx.Exec(ctx, `UPDATE topping_options SET code=$3, name=$4, name_en=$5, price=$6, sold_out=$7, sort=$8
					WHERE id=$1 AND template_id=$2`, o.ID, id, o.Code, o.Name, o.NameEn, o.Price, o.SoldOut, i)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 1 {
					keep = append(keep, o.ID)
					continue
				}
			}
			var oid int64
			if err := tx.QueryRow(ctx, `INSERT INTO topping_options (template_id, code, name, name_en, price, sold_out, sort)
				VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, id, o.Code, o.Name, o.NameEn, o.Price, o.SoldOut, i).Scan(&oid); err != nil {
				return err
			}
			keep = append(keep, oid)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM topping_options WHERE template_id=$1 AND NOT id = ANY($2)`, id, keep); err != nil {
			return err
		}
		if err := syncToppingTemplateItems(ctx, tx, id); err != nil {
			return err
		}
		return branchChanged(ctx, tx, branchID)
	})
	return id, err
}

func (a *App) DeleteToppingTemplate(ctx context.Context, branchID, id int64) error {
	// the dishes' option groups go with it (ON DELETE CASCADE)
	tag, err := a.DB.Exec(ctx, `DELETE FROM topping_templates WHERE id=$1 AND branch_id=$2`, id, branchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return branchChanged(ctx, a.DB, branchID)
}

func (a *App) ReorderToppingTemplates(ctx context.Context, branchID int64, ids []int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		for i, id := range ids {
			if _, err := tx.Exec(ctx, `UPDATE topping_templates SET sort=$3 WHERE id=$1 AND branch_id=$2`, id, branchID, i); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetToppingTemplateItems makes exactly these dishes use the template (the
// "apply to dishes" dialog). A dish already using three other templates is
// refused.
func (a *App) SetToppingTemplateItems(ctx context.Context, branchID, tplID int64, itemIDs []int64) error {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM topping_templates WHERE id=$1 AND branch_id=$2)`, tplID, branchID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		if itemIDs == nil {
			itemIDs = []int64{}
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM menu_items WHERE id = ANY($1) AND branch_id=$2 AND NOT archived`,
			itemIDs, branchID).Scan(&n); err != nil {
			return err
		}
		if n != len(itemIDs) {
			return ErrValidation("a dish was not found")
		}
		var gone []int64
		rows, err := tx.Query(ctx, `DELETE FROM menu_item_topping_templates WHERE template_id=$1 AND NOT item_id = ANY($2) RETURNING item_id`, tplID, itemIDs)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			gone = append(gone, id)
		}
		rows.Close()
		if _, err := tx.Exec(ctx, `INSERT INTO menu_item_topping_templates (item_id, template_id, sort)
			SELECT i, $1, (SELECT count(*) FROM menu_item_topping_templates b WHERE b.item_id = i)
			FROM unnest($2::bigint[]) AS u(i) ON CONFLICT DO NOTHING`, tplID, itemIDs); err != nil {
			return err
		}
		var full []string
		rows, err = tx.Query(ctx, `SELECT i.name FROM menu_items i WHERE i.id = ANY($1)
			AND (SELECT count(*) FROM menu_item_topping_templates b WHERE b.item_id = i.id) > $2 ORDER BY i.sort, i.id`,
			itemIDs, maxItemToppingTemplates)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				rows.Close()
				return err
			}
			full = append(full, s)
		}
		rows.Close()
		if len(full) > 0 {
			return ErrD(http.StatusUnprocessableEntity, "TOPPING_TEMPLATES_FULL",
				fmt.Sprintf("a dish uses at most %d topping templates", maxItemToppingTemplates), map[string]any{"names": full})
		}
		for _, id := range append(gone, itemIDs...) {
			if err := syncItemToppings(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return branchChanged(ctx, a.DB, branchID)
}

// setItemToppingTemplates replaces the templates of one dish (the dish form).
func setItemToppingTemplates(ctx context.Context, tx pgx.Tx, branchID, itemID int64, ids []int64) error {
	clean := []int64{}
	for _, id := range ids {
		if !slices.Contains(clean, id) {
			clean = append(clean, id)
		}
	}
	if len(clean) > maxItemToppingTemplates {
		return ErrValidation(fmt.Sprintf("a dish uses at most %d topping templates", maxItemToppingTemplates))
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM topping_templates WHERE id = ANY($1) AND branch_id=$2`, clean, branchID).Scan(&n); err != nil {
		return err
	}
	if n != len(clean) {
		return ErrValidation("topping template not found")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM menu_item_topping_templates WHERE item_id=$1`, itemID); err != nil {
		return err
	}
	for i, id := range clean {
		if _, err := tx.Exec(ctx, `INSERT INTO menu_item_topping_templates (item_id, template_id, sort) VALUES ($1,$2,$3)`,
			itemID, id, i); err != nil {
			return err
		}
	}
	return nil
}

func syncToppingTemplateItems(ctx context.Context, tx pgx.Tx, tplID int64) error {
	rows, err := tx.Query(ctx, `SELECT item_id FROM menu_item_topping_templates WHERE template_id=$1`, tplID)
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
		if err := syncItemToppings(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

// syncItemToppings makes a dish's template groups match its templates: one
// group per template, after the dish's own groups, with an option per topping.
// Existing groups and options are updated in place so their ids stay.
func syncItemToppings(ctx context.Context, tx pgx.Tx, itemID int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM option_groups g WHERE g.item_id=$1 AND g.topping_template_id IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM menu_item_topping_templates l WHERE l.item_id=$1 AND l.template_id=g.topping_template_id)`, itemID); err != nil {
		return err
	}
	type tpl struct {
		id, gid    int64
		title      string
		max, n     int
		linkSort   int
		groupFound bool
	}
	rows, err := tx.Query(ctx, `SELECT t.id, t.title, t.max_select, l.sort, (SELECT count(*) FROM topping_options o WHERE o.template_id=t.id),
		COALESCE((SELECT g.id FROM option_groups g WHERE g.item_id=$1 AND g.topping_template_id=t.id), 0)
		FROM menu_item_topping_templates l JOIN topping_templates t ON t.id = l.template_id WHERE l.item_id=$1 ORDER BY l.sort, t.id`, itemID)
	if err != nil {
		return err
	}
	var list []tpl
	for rows.Next() {
		var t tpl
		if err := rows.Scan(&t.id, &t.title, &t.max, &t.linkSort, &t.n, &t.gid); err != nil {
			rows.Close()
			return err
		}
		list = append(list, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i, t := range list {
		max := t.max
		if max == 0 || max > t.n {
			max = t.n
		}
		if max < 1 {
			max = 1
		}
		if t.gid == 0 {
			if err := tx.QueryRow(ctx, `INSERT INTO option_groups (item_id, name, name_en, min_select, max_select, sort, topping_template_id)
				VALUES ($1,$2,'',0,$3,$4,$5) RETURNING id`, itemID, t.title, max, 1000+i, t.id).Scan(&t.gid); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE option_groups SET name=$2, min_select=0, max_select=$3, sort=$4 WHERE id=$1`,
			t.gid, t.title, max, 1000+i); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE options o SET name=a.name, name_en=a.name_en, code=a.code, price_delta=a.price, sold_out=a.sold_out, sort=a.sort
			FROM topping_options a WHERE o.group_id=$1 AND o.topping_id=a.id`, t.gid); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO options (group_id, name, name_en, price_delta, sold_out, sort, code, topping_id)
			SELECT $1, a.name, a.name_en, a.price, a.sold_out, a.sort, a.code, a.id FROM topping_options a
			WHERE a.template_id=$2 AND NOT EXISTS (SELECT 1 FROM options o WHERE o.group_id=$1 AND o.topping_id=a.id)`, t.gid, t.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM options WHERE group_id=$1 AND (topping_id IS NULL OR topping_id NOT IN (SELECT id FROM topping_options WHERE template_id=$2))`,
			t.gid, t.id); err != nil {
			return err
		}
	}
	return nil
}
