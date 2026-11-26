package app

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Role permissions (docs/STAFF_ACCESS_DESIGN.md §6): the owner of a store
// decides what its managers and order takers may do in the back office. The
// owner can do everything; staff without a back-office role can do none of it.
const (
	PermOrderView     = "order.view"
	PermOrderProgress = "order.progress"
	PermOrderHandle   = "order.handle"
	PermOrderManual   = "order.manual"
	// PermOrderPlatform enters orders taken on a delivery platform
	// (docs/ORDER_TYPES_DESIGN.md §7.1).
	PermOrderPlatform  = "order.platform"
	PermOrderCancel    = "order.cancel"
	PermOrderAftersale = "order.aftersale"
	// PermCheckoutDiscount gives a discount, a round-off or a set amount at
	// checkout (docs/CHECKOUT_DISCOUNT_DESIGN.md §5.3).
	PermCheckoutDiscount = "checkout.discount"
	PermOrderVoid        = "order.void"
	PermTableUse         = "table.use"
	PermTableManage      = "table.manage"
	// The queue display (docs/QUEUE_DISPLAY_DESIGN.md §10).
	PermQueueUse      = "queue.use"
	PermQueueJump     = "queue.jump"
	PermQueueManage   = "queue.manage"
	PermMemberView    = "member.view"
	PermMemberEdit    = "member.edit"
	PermChatReply     = "chat.reply"
	PermChatSettings  = "chat.settings"
	PermSupport       = "support"
	PermMenuSoldOut   = "menu.soldout"
	PermMenuEdit      = "menu.edit"
	PermMenuTranslate = "menu.translate"
	PermStoreDesign   = "store.design"
	PermPromo         = "promo"
	PermMall          = "mall"
	PermCash          = "cash"
	PermBills         = "bills"
	PermReconcile     = "reconcile"
	PermRiderView     = "rider.view"
	PermDeliveryMap   = "delivery.map"
	PermStaffView     = "staff.view"
	PermStaffManage   = "staff.manage"
	PermStaffAttend   = "staff.attendance"
	PermStaffReports  = "staff.reports"
	PermSettings      = "settings"
	PermKitchenScreen = "kitchen.screen"
)

// PermDef is one line of the role permissions table.
type PermDef struct {
	Code  string `json:"code"`
	Group string `json:"group"`
	// Defaults for an order taker (cashier) and a manager: what the role could
	// do before permissions were configurable, so that nobody's access changed.
	Cashier bool `json:"cashier"`
	Manager bool `json:"manager"`
	// Locked permissions are always on for the role and cannot be taken away.
	LockedCashier bool `json:"locked_cashier,omitempty"`
	// Requires lists permissions this one needs: ticking it ticks them,
	// unticking one of them unticks this.
	Requires []string `json:"requires,omitempty"`
}

var PermCatalog = []PermDef{
	{Code: PermOrderView, Group: "order", Cashier: true, Manager: true, LockedCashier: true},
	{Code: PermOrderProgress, Group: "order", Cashier: true, Manager: true, Requires: []string{PermOrderView}},
	{Code: PermOrderHandle, Group: "order", Cashier: true, Manager: true, Requires: []string{PermOrderView}},
	{Code: PermOrderManual, Group: "order", Cashier: true, Manager: true, Requires: []string{PermOrderView}},
	{Code: PermOrderPlatform, Group: "order", Cashier: true, Manager: true, Requires: []string{PermOrderView}},
	{Code: PermOrderCancel, Group: "order", Cashier: true, Manager: true, Requires: []string{PermOrderView}},
	{Code: PermOrderAftersale, Group: "order", Cashier: true, Manager: true, Requires: []string{PermOrderView}},
	{Code: PermOrderVoid, Group: "order", Cashier: false, Manager: true, Requires: []string{PermOrderView}},
	{Code: PermCheckoutDiscount, Group: "order", Cashier: false, Manager: true},
	{Code: PermTableUse, Group: "table", Cashier: true, Manager: true},
	{Code: PermTableManage, Group: "table", Cashier: false, Manager: true, Requires: []string{PermTableUse}},
	{Code: PermQueueUse, Group: "queue", Cashier: true, Manager: true},
	{Code: PermQueueJump, Group: "queue", Cashier: false, Manager: true, Requires: []string{PermQueueUse}},
	{Code: PermQueueManage, Group: "queue", Cashier: false, Manager: true, Requires: []string{PermQueueUse}},
	{Code: PermMemberView, Group: "customer", Cashier: true, Manager: true},
	{Code: PermMemberEdit, Group: "customer", Cashier: true, Manager: true, Requires: []string{PermMemberView}},
	{Code: PermChatReply, Group: "customer", Cashier: true, Manager: true},
	{Code: PermChatSettings, Group: "customer", Cashier: false, Manager: true, Requires: []string{PermChatReply}},
	{Code: PermSupport, Group: "customer", Cashier: true, Manager: true},
	{Code: PermMenuSoldOut, Group: "menu", Cashier: true, Manager: true},
	{Code: PermMenuEdit, Group: "menu", Cashier: false, Manager: true, Requires: []string{PermMenuSoldOut}},
	{Code: PermMenuTranslate, Group: "menu", Cashier: false, Manager: true},
	{Code: PermStoreDesign, Group: "store", Cashier: false, Manager: true},
	{Code: PermPromo, Group: "store", Cashier: false, Manager: true},
	{Code: PermMall, Group: "store", Cashier: false, Manager: true},
	{Code: PermCash, Group: "money", Cashier: false, Manager: true},
	{Code: PermBills, Group: "money", Cashier: false, Manager: true},
	{Code: PermReconcile, Group: "money", Cashier: false, Manager: true},
	{Code: PermRiderView, Group: "delivery", Cashier: true, Manager: true},
	{Code: PermDeliveryMap, Group: "delivery", Cashier: false, Manager: true},
	{Code: PermStaffView, Group: "staff", Cashier: false, Manager: true},
	{Code: PermStaffManage, Group: "staff", Cashier: false, Manager: true, Requires: []string{PermStaffView}},
	{Code: PermStaffAttend, Group: "staff", Cashier: false, Manager: true, Requires: []string{PermStaffView}},
	{Code: PermStaffReports, Group: "staff", Cashier: false, Manager: true, Requires: []string{PermStaffView}},
	{Code: PermSettings, Group: "branch", Cashier: false, Manager: true},
	{Code: PermKitchenScreen, Group: "branch", Cashier: true, Manager: true},
}

func permDef(code string) (PermDef, bool) {
	for _, d := range PermCatalog {
		if d.Code == code {
			return d, true
		}
	}
	return PermDef{}, false
}

// AllPerms is the owner's list.
func AllPerms() []string {
	out := make([]string, len(PermCatalog))
	for i, d := range PermCatalog {
		out[i] = d.Code
	}
	return out
}

// DefaultPerms is what a role can do in a store whose owner changed nothing.
func DefaultPerms(role string) []string {
	switch role {
	case "owner":
		return AllPerms()
	case "manager", "cashier":
		out := []string{}
		for _, d := range PermCatalog {
			if (role == "manager" && d.Manager) || (role == "cashier" && d.Cashier) {
				out = append(out, d.Code)
			}
		}
		return out
	}
	return []string{}
}

// normalizePerms keeps known codes in catalog order, adds what they require
// and the role's locked permissions.
func normalizePerms(role string, in []string) []string {
	want := map[string]bool{}
	var add func(code string)
	add = func(code string) {
		d, ok := permDef(code)
		if !ok || want[code] {
			return
		}
		want[code] = true
		for _, r := range d.Requires {
			add(r)
		}
	}
	for _, c := range in {
		add(c)
	}
	out := []string{}
	for _, d := range PermCatalog {
		if want[d.Code] || (role == "cashier" && d.LockedCashier) {
			out = append(out, d.Code)
		}
	}
	return out
}

// Can reports whether the user may do something in a branch.
func (u *User) Can(branchID int64, perm string) bool {
	for _, r := range u.Roles {
		if r.BranchID == branchID {
			return slices.Contains(r.Perms, perm)
		}
	}
	return false
}

type permRow struct {
	branch int64
	role   string
}

// branchPerms reads the configured permissions of these branches.
func branchPerms(ctx context.Context, q Querier, branchIDs []int64) (map[permRow][]string, error) {
	out := map[permRow][]string{}
	if len(branchIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT branch_id, role, perms FROM branch_role_perms WHERE branch_id = ANY($1)`, branchIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k permRow
		var p []string
		if err := rows.Scan(&k.branch, &k.role, &p); err != nil {
			return nil, err
		}
		out[k] = normalizePerms(k.role, p)
	}
	return out, rows.Err()
}

func permsFor(conf map[permRow][]string, branchID int64, role string) []string {
	if role == "manager" || role == "cashier" {
		if p, ok := conf[permRow{branchID, role}]; ok {
			return p
		}
	}
	return DefaultPerms(role)
}

// RolePerms is the table on the "role permissions" page.
type RolePerms struct {
	Catalog []PermDef `json:"catalog"`
	Cashier []string  `json:"cashier"`
	Manager []string  `json:"manager"`
	// Counts of people holding each role in the store.
	CashierCount int `json:"cashier_count"`
	ManagerCount int `json:"manager_count"`
	// Who changed it last; empty while the store uses the defaults.
	UpdatedBy string     `json:"updated_by,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

func (a *App) RolePerms(ctx context.Context, branchID int64) (*RolePerms, error) {
	conf, err := branchPerms(ctx, a.DB, []int64{branchID})
	if err != nil {
		return nil, err
	}
	out := &RolePerms{Catalog: PermCatalog, Cashier: permsFor(conf, branchID, "cashier"), Manager: permsFor(conf, branchID, "manager")}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE r.role='cashier'), count(*) FILTER (WHERE r.role='manager')
		FROM user_branch_roles r JOIN users u ON u.id = r.user_id WHERE r.branch_id=$1 AND u.active AND u.deleted_at IS NULL`,
		branchID).Scan(&out.CashierCount, &out.ManagerCount); err != nil {
		return nil, err
	}
	var by string
	var at time.Time
	err = a.DB.QueryRow(ctx, `SELECT coalesce(u.display_name, ''), p.updated_at FROM branch_role_perms p
		LEFT JOIN users u ON u.id = p.updated_by WHERE p.branch_id=$1 ORDER BY p.updated_at DESC LIMIT 1`, branchID).Scan(&by, &at)
	if err == nil {
		out.UpdatedBy, out.UpdatedAt = by, &at
	} else if !isNoRows(err) {
		return nil, err
	}
	return out, nil
}

type RolePermsInput struct {
	Cashier []string `json:"cashier"`
	Manager []string `json:"manager"`
}

// SaveRolePerms replaces the table of a store. Only the owner calls it (the
// HTTP layer checks). Every change is written to the store's staff log.
func (a *App) SaveRolePerms(ctx context.Context, actor int64, branchID int64, in RolePermsInput) (*RolePerms, error) {
	if in.Cashier == nil || in.Manager == nil {
		return nil, ErrValidation("both roles are needed")
	}
	for _, c := range append(slices.Clone(in.Cashier), in.Manager...) {
		if _, ok := permDef(c); !ok {
			return nil, ErrValidation("unknown permission " + c)
		}
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		conf, err := branchPerms(ctx, tx, []int64{branchID})
		if err != nil {
			return err
		}
		for _, role := range []string{"cashier", "manager"} {
			next := normalizePerms(role, in.Cashier)
			if role == "manager" {
				next = normalizePerms(role, in.Manager)
			}
			prev := permsFor(conf, branchID, role)
			if slices.Equal(prev, next) {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO branch_role_perms (branch_id, role, perms, updated_by, updated_at)
				VALUES ($1,$2,$3,$4,now()) ON CONFLICT (branch_id, role) DO UPDATE SET perms=EXCLUDED.perms,
				updated_by=EXCLUDED.updated_by, updated_at=now()`, branchID, role, next, actor); err != nil {
				return err
			}
			added, removed := diffPerms(prev, next)
			if _, err := tx.Exec(ctx, `INSERT INTO role_perm_log (branch_id, role, actor_id, added, removed) VALUES ($1,$2,$3,$4,$5)`,
				branchID, role, actor, added, removed); err != nil {
				return err
			}
		}
		// open back offices refresh their menus
		return notify(ctx, tx, Event{BranchID: branchID})
	})
	if err != nil {
		return nil, err
	}
	return a.RolePerms(ctx, branchID)
}

func diffPerms(prev, next []string) (added, removed []string) {
	added, removed = []string{}, []string{}
	for _, c := range next {
		if !slices.Contains(prev, c) {
			added = append(added, c)
		}
	}
	for _, c := range prev {
		if !slices.Contains(next, c) {
			removed = append(removed, c)
		}
	}
	return
}

// CopyRolePerms applies one store's table to other stores the same person owns.
func (a *App) CopyRolePerms(ctx context.Context, u *User, from int64, to []int64) error {
	if u.RoleIn(from) != "owner" {
		return ErrNotFound
	}
	for _, b := range to {
		if u.RoleIn(b) != "owner" {
			return Err(http.StatusForbidden, "FORBIDDEN", "you are not the owner of every store you picked")
		}
	}
	src, err := a.RolePerms(ctx, from)
	if err != nil {
		return err
	}
	for _, b := range to {
		if b == from {
			continue
		}
		if _, err := a.SaveRolePerms(ctx, u.ID, b, RolePermsInput{Cashier: src.Cashier, Manager: src.Manager}); err != nil {
			return err
		}
	}
	return nil
}
