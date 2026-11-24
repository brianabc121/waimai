package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Flavour note templates (docs/NOTE_TEMPLATE_DESIGN.md). A store writes tags
// such as "less sugar" or "no onion" once, in groups that allow one tag or
// several, and binds the template to many dishes. A tag never changes the
// price and is never required; the free-text note stays beside it.

const (
	maxNoteTemplates     = 50
	maxNoteGroups        = 5
	maxNoteTagsPerGroup  = 20
	maxNoteTagLen        = 20
	maxNoteTemplateLen   = 20
	maxItemNoteTemplates = 3
	maxLineNoteTags      = 30
)

type NoteTag struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	Active bool   `json:"active"`
	NameTr string `json:"name_tr,omitempty"`
}

type NoteGroup struct {
	ID     int64     `json:"id"`
	Name   string    `json:"name"`
	NameEn string    `json:"name_en"`
	Multi  bool      `json:"multi"`
	Tags   []NoteTag `json:"tags"`
	NameTr string    `json:"name_tr,omitempty"`
}

type NoteTemplate struct {
	ID     int64       `json:"id"`
	Name   string      `json:"name"`
	Icon   string      `json:"icon"`
	Sort   int         `json:"sort"`
	Groups []NoteGroup `json:"groups"`
	// ItemIDs are the dishes that show it.
	ItemIDs []int64 `json:"item_ids"`
}

// LineNoteTag is a chosen tag as an order line keeps it.
type LineNoteTag struct {
	ID      int64  `json:"id"`
	Group   string `json:"group"`
	Name    string `json:"name"`
	GroupTr string `json:"group_tr,omitempty"`
	NameTr  string `json:"name_tr,omitempty"`
}

// noteTagsJSON is what an order line stores: never null.
func noteTagsJSON(t []LineNoteTag) []byte {
	if t == nil {
		t = []LineNoteTag{}
	}
	b, _ := json.Marshal(t)
	return b
}

// loadNoteTemplates returns a store's templates in order, every tag included.
func loadNoteTemplates(ctx context.Context, q Querier, branchID int64) ([]NoteTemplate, error) {
	rows, err := q.Query(ctx, `SELECT id, name, icon, sort FROM note_templates WHERE branch_id=$1 ORDER BY sort, id`, branchID)
	if err != nil {
		return nil, err
	}
	out := []NoteTemplate{}
	idx := map[int64]int{}
	for rows.Next() {
		t := NoteTemplate{Groups: []NoteGroup{}, ItemIDs: []int64{}}
		if err := rows.Scan(&t.ID, &t.Name, &t.Icon, &t.Sort); err != nil {
			rows.Close()
			return nil, err
		}
		idx[t.ID] = len(out)
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	rows, err = q.Query(ctx, `SELECT g.id, g.template_id, g.name, g.name_en, g.multi FROM note_groups g
		JOIN note_templates t ON t.id = g.template_id WHERE t.branch_id=$1 ORDER BY g.sort, g.id`, branchID)
	if err != nil {
		return nil, err
	}
	type gref struct{ t, g int }
	groups := map[int64]gref{}
	for rows.Next() {
		g := NoteGroup{Tags: []NoteTag{}}
		var tid int64
		if err := rows.Scan(&g.ID, &tid, &g.Name, &g.NameEn, &g.Multi); err != nil {
			rows.Close()
			return nil, err
		}
		ti := idx[tid]
		out[ti].Groups = append(out[ti].Groups, g)
		groups[g.ID] = gref{ti, len(out[ti].Groups) - 1}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT x.id, x.group_id, x.name, x.name_en, x.active FROM note_tags x
		JOIN note_groups g ON g.id = x.group_id JOIN note_templates t ON t.id = g.template_id
		WHERE t.branch_id=$1 ORDER BY x.sort, x.id`, branchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x NoteTag
		var gid int64
		if err := rows.Scan(&x.ID, &gid, &x.Name, &x.NameEn, &x.Active); err != nil {
			rows.Close()
			return nil, err
		}
		if r, ok := groups[gid]; ok {
			out[r.t].Groups[r.g].Tags = append(out[r.t].Groups[r.g].Tags, x)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT b.template_id, b.item_id FROM menu_item_note_templates b
		JOIN menu_items i ON i.id = b.item_id JOIN note_templates t ON t.id = b.template_id
		WHERE t.branch_id=$1 AND NOT i.archived ORDER BY i.sort, i.id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tid, iid int64
		if err := rows.Scan(&tid, &iid); err != nil {
			return nil, err
		}
		out[idx[tid]].ItemIDs = append(out[idx[tid]].ItemIDs, iid)
	}
	return out, rows.Err()
}

// attachNotes fills in each dish's templates and the groups and tags it
// offers (the live ones only).
func attachNotes(ctx context.Context, q Querier, branchID int64, items []*Item) error {
	tpls, err := loadNoteTemplates(ctx, q, branchID)
	if err != nil || len(tpls) == 0 {
		return err
	}
	byID := map[int64]*NoteTemplate{}
	for i := range tpls {
		byID[tpls[i].ID] = &tpls[i]
	}
	rows, err := q.Query(ctx, `SELECT b.item_id, b.template_id FROM menu_item_note_templates b
		JOIN note_templates t ON t.id = b.template_id WHERE t.branch_id=$1 ORDER BY b.sort, t.sort, t.id`, branchID)
	if err != nil {
		return err
	}
	bound := map[int64][]int64{}
	for rows.Next() {
		var iid, tid int64
		if err := rows.Scan(&iid, &tid); err != nil {
			rows.Close()
			return err
		}
		bound[iid] = append(bound[iid], tid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range items {
		for _, tid := range bound[it.ID] {
			t := byID[tid]
			if t == nil {
				continue
			}
			it.NoteTemplateIDs = append(it.NoteTemplateIDs, tid)
			for _, g := range t.Groups {
				live := NoteGroup{ID: g.ID, Name: g.Name, NameEn: g.NameEn, Multi: g.Multi, Tags: []NoteTag{}}
				for _, x := range g.Tags {
					if x.Active {
						live.Tags = append(live.Tags, x)
					}
				}
				if len(live.Tags) > 0 {
					it.Notes = append(it.Notes, live)
				}
			}
		}
	}
	return nil
}

// pickNotes checks the tags chosen for one line of a dish and returns them in
// the dish's own order.
func pickNotes(it *Item, ids []int64) ([]LineNoteTag, error) {
	out := []LineNoteTag{}
	if len(ids) == 0 {
		return out, nil
	}
	bad := ErrD(http.StatusUnprocessableEntity, "NOTE_TAGS_INVALID", "a flavour note does not belong to this dish or is no longer offered",
		map[string]any{"item_id": it.ID})
	if len(ids) > maxLineNoteTags {
		return nil, bad
	}
	want := map[int64]bool{}
	for _, id := range ids {
		if want[id] {
			return nil, ErrValidation("duplicate flavour note")
		}
		want[id] = true
	}
	found := 0
	for _, g := range it.Notes {
		n := 0
		for _, x := range g.Tags {
			if want[x.ID] {
				n++
				found++
				out = append(out, LineNoteTag{ID: x.ID, Group: g.Name, Name: x.Name})
			}
		}
		if n > 1 && !g.Multi {
			return nil, ErrD(http.StatusUnprocessableEntity, "NOTE_TAGS_INVALID", "pick one flavour note in this group",
				map[string]any{"item_id": it.ID, "group_id": g.ID})
		}
	}
	if found != len(want) {
		return nil, bad
	}
	return out, nil
}

// ---- the store's template page ----

type NoteTagInput struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	// Active left out keeps a tag offered.
	Active *bool `json:"active"`
}

type NoteGroupInput struct {
	ID     int64          `json:"id"`
	Name   string         `json:"name"`
	NameEn string         `json:"name_en"`
	Multi  bool           `json:"multi"`
	Tags   []NoteTagInput `json:"tags"`
}

type NoteTemplateInput struct {
	Name   string           `json:"name"`
	Icon   string           `json:"icon"`
	Sort   *int             `json:"sort"`
	Groups []NoteGroupInput `json:"groups"`
}

func (a *App) NoteTemplates(ctx context.Context, branchID int64) ([]NoteTemplate, error) {
	return loadNoteTemplates(ctx, a.DB, branchID)
}

func (in *NoteTemplateInput) clean() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Icon = strings.TrimSpace(in.Icon)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > maxNoteTemplateLen {
		return ErrValidation(fmt.Sprintf("the template needs a name of at most %d characters", maxNoteTemplateLen))
	}
	if utf8.RuneCountInString(in.Icon) > 8 {
		return ErrValidation("invalid icon")
	}
	if len(in.Groups) == 0 || len(in.Groups) > maxNoteGroups {
		return ErrValidation(fmt.Sprintf("a template has 1 to %d groups", maxNoteGroups))
	}
	seen := map[string]bool{}
	for gi := range in.Groups {
		g := &in.Groups[gi]
		g.Name, g.NameEn = strings.TrimSpace(g.Name), strings.TrimSpace(g.NameEn)
		if utf8.RuneCountInString(g.Name) > maxNoteTagLen {
			return ErrValidation(fmt.Sprintf("a group name is at most %d characters", maxNoteTagLen))
		}
		tags := g.Tags[:0]
		inGroup := map[string]bool{}
		for _, x := range g.Tags {
			x.Name, x.NameEn = strings.TrimSpace(x.Name), strings.TrimSpace(x.NameEn)
			if x.Name == "" {
				continue
			}
			if utf8.RuneCountInString(x.Name) > maxNoteTagLen || len(x.NameEn) > 80 {
				return ErrValidation(fmt.Sprintf("a tag is at most %d characters", maxNoteTagLen))
			}
			if inGroup[x.Name] {
				return ErrD(http.StatusUnprocessableEntity, "NOTE_TAG_TWICE", "the same tag is twice in a group", map[string]any{"name": x.Name})
			}
			inGroup[x.Name] = true
			tags = append(tags, x)
		}
		g.Tags = tags
		if len(g.Tags) == 0 || len(g.Tags) > maxNoteTagsPerGroup {
			return ErrValidation(fmt.Sprintf("every group has 1 to %d tags", maxNoteTagsPerGroup))
		}
		if g.Name != "" && seen[g.Name] {
			return ErrValidation("two groups have the same name")
		}
		seen[g.Name] = true
	}
	return nil
}

// SaveNoteTemplate creates (id 0) or updates a template. Groups and tags keep
// their ids when the input names them, so translations and the tags in
// customers' carts stay good; those left out are deleted.
func (a *App) SaveNoteTemplate(ctx context.Context, branchID, id int64, in NoteTemplateInput) (int64, error) {
	if err := in.clean(); err != nil {
		return 0, err
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM note_templates WHERE branch_id=$1 AND name=$2 AND id<>$3)`,
			branchID, in.Name, id).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return Err(http.StatusConflict, "NOTE_TEMPLATE_TAKEN", "another template already has this name")
		}
		if id == 0 {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM note_templates WHERE branch_id=$1`, branchID).Scan(&n); err != nil {
				return err
			}
			if n >= maxNoteTemplates {
				return ErrValidation(fmt.Sprintf("a store has at most %d templates", maxNoteTemplates))
			}
			sort := n
			if in.Sort != nil {
				sort = *in.Sort
			}
			if err := tx.QueryRow(ctx, `INSERT INTO note_templates (branch_id, name, icon, sort) VALUES ($1,$2,$3,$4) RETURNING id`,
				branchID, in.Name, in.Icon, sort).Scan(&id); err != nil {
				return err
			}
		} else {
			err := tx.QueryRow(ctx, `UPDATE note_templates SET name=$3, icon=$4, sort=coalesce($5, sort), updated_at=now()
				WHERE id=$1 AND branch_id=$2 RETURNING id`, id, branchID, in.Name, in.Icon, in.Sort).Scan(&id)
			if isNoRows(err) {
				return ErrNotFound
			} else if err != nil {
				return err
			}
		}
		return saveNoteGroups(ctx, tx, id, in.Groups)
	})
	if err != nil {
		return 0, err
	}
	return id, branchChanged(ctx, a.DB, branchID)
}

func saveNoteGroups(ctx context.Context, tx pgx.Tx, tplID int64, groups []NoteGroupInput) error {
	ownGroups, err := idSet(ctx, tx, `SELECT id FROM note_groups WHERE template_id=$1`, tplID)
	if err != nil {
		return err
	}
	ownTags, err := idSet(ctx, tx, `SELECT x.id FROM note_tags x JOIN note_groups g ON g.id = x.group_id WHERE g.template_id=$1`, tplID)
	if err != nil {
		return err
	}
	keepGroups, keepTags := []int64{}, []int64{}
	for gi, g := range groups {
		gid := g.ID
		if ownGroups[gid] {
			if _, err := tx.Exec(ctx, `UPDATE note_groups SET name=$2, name_en=$3, multi=$4, sort=$5 WHERE id=$1`,
				gid, g.Name, g.NameEn, g.Multi, gi); err != nil {
				return err
			}
		} else if err := tx.QueryRow(ctx, `INSERT INTO note_groups (template_id, name, name_en, multi, sort)
			VALUES ($1,$2,$3,$4,$5) RETURNING id`, tplID, g.Name, g.NameEn, g.Multi, gi).Scan(&gid); err != nil {
			return err
		}
		keepGroups = append(keepGroups, gid)
		for xi, x := range g.Tags {
			active := x.Active == nil || *x.Active
			xid := x.ID
			// A tag may move to another group of the same template.
			if ownTags[xid] && !slices.Contains(keepTags, xid) {
				if _, err := tx.Exec(ctx, `UPDATE note_tags SET group_id=$2, name=$3, name_en=$4, active=$5, sort=$6 WHERE id=$1`,
					xid, gid, x.Name, x.NameEn, active, xi); err != nil {
					return err
				}
			} else if err := tx.QueryRow(ctx, `INSERT INTO note_tags (group_id, name, name_en, active, sort)
				VALUES ($1,$2,$3,$4,$5) RETURNING id`, gid, x.Name, x.NameEn, active, xi).Scan(&xid); err != nil {
				return err
			}
			keepTags = append(keepTags, xid)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM note_tags x USING note_groups g WHERE g.id = x.group_id AND g.template_id=$1
		AND NOT x.id = ANY($2)`, tplID, keepTags); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM note_groups WHERE template_id=$1 AND NOT id = ANY($2)`, tplID, keepGroups)
	return err
}

func idSet(ctx context.Context, q Querier, sql string, args ...any) (map[int64]bool, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// DeleteNoteTemplate removes a template; the dishes stop showing it. Orders
// keep the tags they were placed with.
func (a *App) DeleteNoteTemplate(ctx context.Context, branchID, id int64) error {
	tag, err := a.DB.Exec(ctx, `DELETE FROM note_templates WHERE id=$1 AND branch_id=$2`, id, branchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return branchChanged(ctx, a.DB, branchID)
}

// ReorderNoteTemplates sets the order of the template list.
func (a *App) ReorderNoteTemplates(ctx context.Context, branchID int64, ids []int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		for i, id := range ids {
			if _, err := tx.Exec(ctx, `UPDATE note_templates SET sort=$3 WHERE id=$1 AND branch_id=$2`, id, branchID, i); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetNoteTemplateItems makes exactly these dishes show the template (the
// "apply to dishes" dialog). A dish already showing three other templates is
// refused.
func (a *App) SetNoteTemplateItems(ctx context.Context, branchID, tplID int64, itemIDs []int64) error {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM note_templates WHERE id=$1 AND branch_id=$2)`, tplID, branchID).Scan(&ok); err != nil {
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
		if _, err := tx.Exec(ctx, `DELETE FROM menu_item_note_templates WHERE template_id=$1 AND NOT item_id = ANY($2)`,
			tplID, itemIDs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO menu_item_note_templates (item_id, template_id, sort)
			SELECT i, $1, (SELECT count(*) FROM menu_item_note_templates b WHERE b.item_id = i)
			FROM unnest($2::bigint[]) AS u(i) ON CONFLICT DO NOTHING`, tplID, itemIDs); err != nil {
			return err
		}
		var full []string
		rows, err := tx.Query(ctx, `SELECT i.name FROM menu_items i WHERE i.id = ANY($1)
			AND (SELECT count(*) FROM menu_item_note_templates b WHERE b.item_id = i.id) > $2 ORDER BY i.sort, i.id`,
			itemIDs, maxItemNoteTemplates)
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
			return ErrD(http.StatusUnprocessableEntity, "NOTE_TEMPLATES_FULL",
				fmt.Sprintf("a dish shows at most %d templates", maxItemNoteTemplates), map[string]any{"names": full})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return branchChanged(ctx, a.DB, branchID)
}

// setItemNoteTemplates replaces the templates of one dish (the dish form).
func setItemNoteTemplates(ctx context.Context, tx pgx.Tx, branchID, itemID int64, ids []int64) error {
	clean := []int64{}
	for _, id := range ids {
		if !slices.Contains(clean, id) {
			clean = append(clean, id)
		}
	}
	if len(clean) > maxItemNoteTemplates {
		return ErrValidation(fmt.Sprintf("a dish shows at most %d templates", maxItemNoteTemplates))
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM note_templates WHERE id = ANY($1) AND branch_id=$2`, clean, branchID).Scan(&n); err != nil {
		return err
	}
	if n != len(clean) {
		return ErrValidation("note template not found")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM menu_item_note_templates WHERE item_id=$1`, itemID); err != nil {
		return err
	}
	for i, id := range clean {
		if _, err := tx.Exec(ctx, `INSERT INTO menu_item_note_templates (item_id, template_id, sort) VALUES ($1,$2,$3)`,
			itemID, id, i); err != nil {
			return err
		}
	}
	return nil
}

// ---- ready-made templates ----

type starterText struct{ Zh, En string }

type starterGroup struct {
	Name  starterText
	Multi bool
	Tags  []starterText
}

type starterTemplate struct {
	Key    string
	Icon   string
	Name   starterText
	Groups []starterGroup
}

// StarterNoteTemplates are offered to a store with an "add common templates"
// button; they are added in the menu's language and can be changed after.
var StarterNoteTemplates = []starterTemplate{
	{"milk_tea", "🧋", starterText{"奶茶类", "Milk tea"}, []starterGroup{
		{starterText{"甜度", "Sweetness"}, false, []starterText{{"正常糖", "Normal sugar"}, {"少糖", "Less sugar"},
			{"半糖", "Half sugar"}, {"微糖", "Light sugar"}, {"无糖", "No sugar"}}},
		{starterText{"温度", "Temperature"}, false, []starterText{{"正常冰", "Normal ice"}, {"少冰", "Less ice"},
			{"去冰", "No ice"}, {"常温", "Room temperature"}, {"热", "Hot"}}},
	}},
	{"stir_fry", "🌶️", starterText{"炒菜类", "Stir-fry"}, []starterGroup{
		{starterText{"辣度", "Spiciness"}, false, []starterText{{"不辣", "Not spicy"}, {"微辣", "Mild"},
			{"中辣", "Medium spicy"}, {"特辣", "Extra spicy"}}},
		{starterText{"忌口", "Leave out"}, true, []starterText{{"少油", "Less oil"}, {"少盐", "Less salt"},
			{"不要葱", "No scallion"}, {"不要姜", "No ginger"}, {"不要蒜", "No garlic"}, {"不要香菜", "No coriander"}}},
	}},
	{"coffee", "☕", starterText{"咖啡类", "Coffee"}, []starterGroup{
		{starterText{"温度", "Temperature"}, false, []starterText{{"冰", "Iced"}, {"热", "Hot"}}},
		{starterText{"浓度", "Strength"}, false, []starterText{{"正常", "Regular"}, {"浓一点", "Stronger"}, {"淡一点", "Lighter"}}},
		{starterText{"奶和糖", "Milk and sugar"}, true, []starterText{{"不加奶", "No milk"}, {"燕麦奶", "Oat milk"},
			{"不加糖", "No sugar"}, {"少糖", "Less sugar"}}},
	}},
	{"noodles", "🍜", starterText{"粉面类", "Noodles"}, []starterGroup{
		{starterText{"软硬", "Texture"}, false, []starterText{{"正常", "Regular"}, {"软一点", "Softer"}, {"硬一点", "Firmer"}}},
		{starterText{"加减", "Adjust"}, true, []starterText{{"多汤", "More soup"}, {"少汤", "Less soup"},
			{"不要葱花", "No scallion"}, {"不要香菜", "No coriander"}, {"汤面分开", "Soup on the side"}}},
	}},
	{"general", "📦", starterText{"通用", "General"}, []starterGroup{
		{starterText{"其他", "Other"}, true, []starterText{{"打包分开装", "Pack separately"}, {"不要餐具", "No cutlery"},
			{"多给纸巾", "Extra napkins"}, {"汤和饭分开", "Soup and rice apart"}}},
	}},
}

// StarterNoteTemplate is a ready-made template as the dialog lists it, in the
// store's menu language.
type StarterNoteTemplate struct {
	Key    string      `json:"key"`
	Icon   string      `json:"icon"`
	Name   string      `json:"name"`
	Groups []NoteGroup `json:"groups"`
	// Added: the store has a template of this name already.
	Added bool `json:"added"`
}

func (s starterText) in(lang string) (text, en string) {
	switch lang {
	case "zh":
		return s.Zh, s.En
	case "zh-Hant":
		if t := convertChinese(s.Zh, "zh-Hant"); t != "" {
			return t, s.En
		}
		return s.Zh, s.En
	}
	// Other menu languages get the English, which the merchant can change.
	return s.En, ""
}

func (s starterTemplate) in(lang string) (NoteTemplateInput, StarterNoteTemplate) {
	name, _ := s.Name.in(lang)
	in := NoteTemplateInput{Name: name, Icon: s.Icon}
	view := StarterNoteTemplate{Key: s.Key, Icon: s.Icon, Name: name, Groups: []NoteGroup{}}
	for _, g := range s.Groups {
		gn, gen := g.Name.in(lang)
		gi := NoteGroupInput{Name: gn, NameEn: gen, Multi: g.Multi}
		gv := NoteGroup{Name: gn, NameEn: gen, Multi: g.Multi, Tags: []NoteTag{}}
		for _, x := range g.Tags {
			xn, xen := x.in(lang)
			gi.Tags = append(gi.Tags, NoteTagInput{Name: xn, NameEn: xen})
			gv.Tags = append(gv.Tags, NoteTag{Name: xn, NameEn: xen, Active: true})
		}
		in.Groups = append(in.Groups, gi)
		view.Groups = append(view.Groups, gv)
	}
	return in, view
}

func (a *App) menuLangOf(ctx context.Context, branchID int64) (string, error) {
	var lang string
	err := a.DB.QueryRow(ctx, `SELECT menu_lang FROM branches WHERE id=$1`, branchID).Scan(&lang)
	if isNoRows(err) {
		return "", ErrNotFound
	}
	return lang, err
}

// StarterNoteTemplates lists the ready-made templates for a store.
func (a *App) StarterNoteTemplates(ctx context.Context, branchID int64) ([]StarterNoteTemplate, error) {
	lang, err := a.menuLangOf(ctx, branchID)
	if err != nil {
		return nil, err
	}
	have, err := loadNoteTemplates(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	out := []StarterNoteTemplate{}
	for _, s := range StarterNoteTemplates {
		_, v := s.in(lang)
		v.Added = slices.ContainsFunc(have, func(t NoteTemplate) bool { return t.Name == v.Name })
		out = append(out, v)
	}
	return out, nil
}

// AddStarterNoteTemplates adds the chosen ready-made templates; ones the
// store already has (by name) are skipped.
func (a *App) AddStarterNoteTemplates(ctx context.Context, branchID int64, keys []string) ([]int64, error) {
	lang, err := a.menuLangOf(ctx, branchID)
	if err != nil {
		return nil, err
	}
	have, err := loadNoteTemplates(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, s := range StarterNoteTemplates {
		if !slices.Contains(keys, s.Key) {
			continue
		}
		in, v := s.in(lang)
		if slices.ContainsFunc(have, func(t NoteTemplate) bool { return t.Name == v.Name }) {
			continue
		}
		id, err := a.SaveNoteTemplate(ctx, branchID, 0, in)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// liveNoteTags maps each dish to the flavour notes it offers now.
func liveNoteTags(ctx context.Context, q Querier, itemIDs []int64) (map[int64]map[int64]bool, error) {
	rows, err := q.Query(ctx, `SELECT b.item_id, x.id FROM menu_item_note_templates b
		JOIN note_groups g ON g.template_id = b.template_id JOIN note_tags x ON x.group_id = g.id
		WHERE b.item_id = ANY($1) AND x.active`, itemIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[int64]bool{}
	for rows.Next() {
		var iid, xid int64
		if err := rows.Scan(&iid, &xid); err != nil {
			return nil, err
		}
		if out[iid] == nil {
			out[iid] = map[int64]bool{}
		}
		out[iid][xid] = true
	}
	return out, rows.Err()
}

// carryNotes takes an earlier line's flavour notes into a new cart line: the
// tags still offered by id, the others as words in the note.
func carryNotes(it OrderItem, live map[int64]bool) ([]int64, string) {
	ids := []int64{}
	words := []string{}
	for _, x := range it.NoteTags {
		if live[x.ID] {
			ids = append(ids, x.ID)
		} else {
			words = append(words, x.Name)
		}
	}
	note := it.Note
	if len(words) > 0 {
		note = strings.TrimSpace(strings.Join(words, "，") + " " + note)
		for len(note) > 200 { // the limit of a line's note
			_, n := utf8.DecodeLastRuneInString(note)
			note = note[:len(note)-n]
		}
	}
	return ids, note
}

// AdminNoteTemplateChange checks a store the platform edits the templates of
// and notes the change in the platform's audit log.
func (a *App) AdminNoteTemplateChange(ctx context.Context, actor AdminActor, branchID, tplID int64, action string) error {
	if err := a.liveBranch(ctx, branchID); err != nil {
		return err
	}
	if action == "" {
		return nil
	}
	return adminAudit(ctx, a.DB, actor, action, "note_template", tplID, map[string]any{"branch_id": branchID})
}
