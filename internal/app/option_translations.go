package app

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Saving an item writes its option groups and options anew, under new ids.
// Their translations follow them here, so that what the merchant corrected in
// the language window outlives the next save of the item, and unchanged names
// are not sent to the translation service again (docs/I18N_SIMPLE_DESIGN.md §4.4).

type optionNames struct {
	id      int64
	name    string
	options []optionNames
}

// itemOptionNames lists an item's own option groups (not the ones a topping
// template gives it) with their options, in order.
func itemOptionNames(ctx context.Context, tx pgx.Tx, itemID int64) ([]optionNames, error) {
	rows, err := tx.Query(ctx, `SELECT g.id, g.name, o.id, o.name FROM option_groups g LEFT JOIN options o ON o.group_id = g.id
		WHERE g.item_id=$1 AND g.topping_template_id IS NULL ORDER BY g.sort, g.id, o.sort, o.id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []optionNames
	for rows.Next() {
		var gid int64
		var gname string
		var oid *int64
		var oname *string
		if err := rows.Scan(&gid, &gname, &oid, &oname); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].id != gid {
			out = append(out, optionNames{id: gid, name: gname})
		}
		if oid != nil {
			g := &out[len(out)-1]
			g.options = append(g.options, optionNames{id: *oid, name: *oname})
		}
	}
	return out, rows.Err()
}

// pairNames matches the rows after a save with the rows before it: the same
// name first, then the same place in the list (a renamed row). It returns, for
// each new row, the index of the old one or -1.
func pairNames(before, after []optionNames) []int {
	pair := make([]int, len(after))
	used := make([]bool, len(before))
	for i := range pair {
		pair[i] = -1
	}
	for i, n := range after {
		for j, o := range before {
			if !used[j] && o.name == n.name {
				pair[i], used[j] = j, true
				break
			}
		}
	}
	for i := range after {
		if pair[i] < 0 && i < len(before) && !used[i] {
			pair[i], used[i] = i, true
		}
	}
	return pair
}

// carryOptionTranslations hands the translations of the option groups and
// options an item had before a save to the ones it has now, and drops those of
// rows that are gone.
func carryOptionTranslations(ctx context.Context, tx pgx.Tx, itemID int64, before []optionNames) error {
	if len(before) == 0 {
		return nil
	}
	after, err := itemOptionNames(ctx, tx, itemID)
	if err != nil {
		return err
	}
	move := func(entity string, from, to int64) error {
		_, err := tx.Exec(ctx, `UPDATE content_translations SET entity_id=$3 WHERE entity=$1 AND entity_id=$2`, entity, from, to)
		return err
	}
	var goneGroups, goneOptions []int64
	groupPair := pairNames(before, after)
	kept := make([]bool, len(before))
	for i, j := range groupPair {
		if j < 0 {
			continue
		}
		kept[j] = true
		if err := move("group", before[j].id, after[i].id); err != nil {
			return err
		}
		optPair := pairNames(before[j].options, after[i].options)
		keptOpt := make([]bool, len(before[j].options))
		for x, y := range optPair {
			if y < 0 {
				continue
			}
			keptOpt[y] = true
			if err := move("option", before[j].options[y].id, after[i].options[x].id); err != nil {
				return err
			}
		}
		for y, o := range before[j].options {
			if !keptOpt[y] {
				goneOptions = append(goneOptions, o.id)
			}
		}
	}
	for j, g := range before {
		if kept[j] {
			continue
		}
		goneGroups = append(goneGroups, g.id)
		for _, o := range g.options {
			goneOptions = append(goneOptions, o.id)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM content_translations WHERE (entity='group' AND entity_id = ANY($1))
		OR (entity='option' AND entity_id = ANY($2))`, goneGroups, goneOptions); err != nil {
		return err
	}
	return nil
}
