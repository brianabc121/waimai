package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The staff dine-in desk (docs/DINE_IN_ORDER_DESIGN.md): tags on a sitting
// and on a round, changing guests, moving to a free table, rushing a dish,
// the dishes eaten in most often, and the desk's preset choices.

const (
	maxTags   = 10
	maxTagLen = 20
)

// cleanTags trims, drops blanks and repeats, and checks the limits.
func cleanTags(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		if len([]rune(t)) > maxTagLen {
			return nil, ErrValidation("a tag is up to 20 characters")
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > maxTags {
		return nil, ErrValidation("up to 10 tags")
	}
	return out, nil
}

type SessionPatch struct {
	Guests *int      `json:"guests"`
	Tags   *[]string `json:"tags"`
}

// UpdateSession changes the guest count or the sitting's tags of an open table.
func (a *App) UpdateSession(ctx context.Context, sessionID int64, in SessionPatch, actor Actor) (*Bill, error) {
	if in.Guests != nil && (*in.Guests < 0 || *in.Guests > 99) {
		return nil, ErrValidation("guests must be between 0 and 99")
	}
	var tags []string
	if in.Tags != nil {
		var err error
		if tags, err = cleanTags(*in.Tags); err != nil {
			return nil, err
		}
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		s, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		if s.Status != "open" {
			return ErrSessionClosed
		}
		if in.Guests != nil {
			if _, err := tx.Exec(ctx, `UPDATE dining_sessions SET guests=$2 WHERE id=$1`, s.ID, *in.Guests); err != nil {
				return err
			}
		}
		if in.Tags != nil {
			if _, err := tx.Exec(ctx, `UPDATE dining_sessions SET tags=$2 WHERE id=$1`, s.ID, tags); err != nil {
				return err
			}
		}
		if err := bump(ctx, tx, s.OrderID); err != nil {
			return err
		}
		if err := audit(ctx, tx, s.BranchID, s.OrderID, actor, "table.updated",
			map[string]any{"table": s.TableName, "guests": in.Guests, "tags": tags}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: s.BranchID, OrderID: s.OrderID})
	})
	if err != nil {
		return nil, err
	}
	return loadBill(ctx, a.DB, sessionID)
}

// MoveTable moves an open sitting, bill and all, to a free table of the same
// branch. QR phones and tablets follow the table, so the old table's code
// stops reaching this bill and the new table's code starts to.
func (a *App) MoveTable(ctx context.Context, sessionID, toTableID int64, actor Actor) (*Bill, error) {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		s, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		if s.Status != "open" {
			return ErrSessionClosed
		}
		if toTableID == s.TableID {
			return nil
		}
		var branchID int64
		var name string
		var active bool
		err = tx.QueryRow(ctx, `SELECT branch_id, name, active FROM dining_tables WHERE id=$1 FOR UPDATE`, toTableID).
			Scan(&branchID, &name, &active)
		if isNoRows(err) || (err == nil && branchID != s.BranchID) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if !active {
			return ErrInvalidState("this table is disabled")
		}
		var busy bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dining_sessions WHERE table_id=$1 AND status='open')`,
			toTableID).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return Err(http.StatusConflict, "TABLE_OCCUPIED", "that table is already open")
		}
		if _, err := tx.Exec(ctx, `UPDATE dining_sessions SET table_id=$2, moved_from=$3 WHERE id=$1`,
			s.ID, toTableID, s.TableName); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET table_name=$2, version=version+1, updated_at=now() WHERE id=$1`,
			s.OrderID, name); err != nil {
			return err
		}
		if err := audit(ctx, tx, s.BranchID, s.OrderID, actor, "table.moved",
			map[string]any{"from": s.TableName, "to": name}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: s.BranchID, OrderID: s.OrderID})
	})
	if isUnique(err, "dining_sessions_one_open") {
		return nil, Err(http.StatusConflict, "TABLE_OCCUPIED", "that table is already open")
	}
	if err != nil {
		return nil, err
	}
	return loadBill(ctx, a.DB, sessionID)
}

// RushItem asks the kitchen to hurry a dish that is still being made.
func (a *App) RushItem(ctx context.Context, itemID int64, actor Actor) (*Bill, error) {
	var sessionID int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		s, o, err := a.dineItem(ctx, tx, itemID)
		if err != nil {
			return err
		}
		sessionID = s.ID
		if s.Status != "open" {
			return ErrSessionClosed
		}
		var status, name string
		if err := tx.QueryRow(ctx, `SELECT prep_status, name FROM order_items WHERE id=$1 FOR UPDATE`, itemID).
			Scan(&status, &name); err != nil {
			return err
		}
		if status != "pending" && status != "preparing" {
			return ErrInvalidState("only a dish still being made can be rushed")
		}
		if _, err := tx.Exec(ctx, `UPDATE order_items SET rush_at = now() WHERE id=$1`, itemID); err != nil {
			return err
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "table.item_rushed",
			map[string]any{"item_id": itemID, "name": name}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
	if err != nil {
		return nil, err
	}
	return loadBill(ctx, a.DB, sessionID)
}

// DineTop lists the menu items eaten in most over the last 30 days, best first.
func (a *App) DineTop(ctx context.Context, branchID int64) ([]int64, error) {
	rows, err := a.DB.Query(ctx, `SELECT i.item_id FROM order_items i JOIN orders o ON o.id = i.order_id
		WHERE o.branch_id=$1 AND o.service_type='dine_in' AND i.prep_status <> 'cancelled'
		AND o.created_at > now() - interval '30 days'
		GROUP BY i.item_id ORDER BY sum(i.qty) DESC, i.item_id LIMIT 20`, branchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// DinePresets are the desk's tap-to-choose lists. A nil list means the
// built-in defaults, which the page shows in the staff member's language.
type DinePresets struct {
	TableTags   []string `json:"table_tags"`
	RoundTags   []string `json:"round_tags"`
	VoidReasons []string `json:"void_reasons"`
	PayMethods  []string `json:"pay_methods"`
	// LongMinutes marks a table red after this long; 0 means 90.
	LongMinutes int `json:"long_minutes"`
	// StayAfterSend keeps the order screen on the table after sending a round.
	StayAfterSend bool `json:"stay_after_send"`
	// ShowPhotos false shows text-only dish tiles; nil means photos.
	ShowPhotos *bool `json:"show_photos"`
}

func (a *App) GetDinePresets(ctx context.Context, branchID int64) (*DinePresets, error) {
	var raw []byte
	err := a.DB.QueryRow(ctx, `SELECT dine_presets FROM branches WHERE id=$1`, branchID).Scan(&raw)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	p := &DinePresets{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, p)
	}
	return p, nil
}

func (a *App) SaveDinePresets(ctx context.Context, branchID int64, in DinePresets) error {
	for _, l := range []*[]string{&in.TableTags, &in.RoundTags, &in.VoidReasons, &in.PayMethods} {
		if *l == nil {
			continue
		}
		c, err := cleanTags(*l)
		if err != nil {
			return err
		}
		*l = c
	}
	if in.LongMinutes < 0 || in.LongMinutes > 600 {
		return ErrValidation("minutes must be between 0 and 600")
	}
	raw, _ := json.Marshal(in)
	tag, err := a.DB.Exec(ctx, `UPDATE branches SET dine_presets=$2 WHERE id=$1`, branchID, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return notify(ctx, a.DB, Event{BranchID: branchID})
}

// ReleaseTable frees a table that was opened but never ordered from: the
// sitting ends and its empty bill is cancelled, so no sale is recorded. A
// table that has ordered anything, even dishes voided since, is refused; it
// leaves through the bill (void, then close) like every other.
func (a *App) ReleaseTable(ctx context.Context, sessionID int64, actor Actor) (*Bill, error) {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		s, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		o, err := lockOrder(ctx, tx, s.OrderID)
		if err != nil {
			return err
		}
		if s.Status != "open" {
			if o.Acceptance == "cancelled" {
				return nil // released already: a second tap changes nothing
			}
			return ErrSessionClosed
		}
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM order_items WHERE order_id=$1)
			OR EXISTS (SELECT 1 FROM order_batches WHERE order_id=$1)
			OR (SELECT total <> 0 OR credit_paid <> 0 FROM orders WHERE id=$1)`, o.ID).Scan(&used); err != nil {
			return err
		}
		if used {
			return Err(http.StatusConflict, "TABLE_HAS_ORDER", "this table has ordered; void the items or close the bill instead")
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET acceptance_status='cancelled', close_reason='退桌',
			closed_at=now(), version=version+1, updated_at=now() WHERE id=$1`, o.ID); err != nil {
			return err
		}
		var closer *int64
		if actor.ID != 0 {
			closer = &actor.ID
		}
		if _, err := tx.Exec(ctx, `UPDATE dining_sessions SET status='closed', closed_at=now(), closed_by=$2 WHERE id=$1`,
			s.ID, closer); err != nil {
			return err
		}
		if err := audit(ctx, tx, s.BranchID, o.ID, actor, "table.released", map[string]any{"table": s.TableName}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: s.BranchID, OrderID: o.ID})
	})
	if err != nil {
		return nil, err
	}
	return loadBill(ctx, a.DB, sessionID)
}
