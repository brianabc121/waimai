package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Station is a kitchen workstation defined by the branch (fryer, drinks, ...).
type Station struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	NameEn    string `json:"name_en"`
	Active    bool   `json:"active"`
	Sort      int    `json:"sort"`
	ItemCount int    `json:"item_count"` // menu items routed here
	OpenTasks int    `json:"open_tasks"` // order items still to prepare here
}

func (a *App) ListStations(ctx context.Context, branchID int64) ([]Station, error) {
	rows, err := a.DB.Query(ctx, `SELECT s.id, s.name, s.name_en, s.active, s.sort,
		(SELECT count(*) FROM menu_items m WHERE m.station_id = s.id AND NOT m.archived),
		(SELECT count(*) FROM order_items oi JOIN orders o ON o.id = oi.order_id
			WHERE oi.station_id = s.id AND oi.prep_status IN ('pending','preparing') AND o.acceptance_status = 'accepted')
		FROM kitchen_stations s WHERE s.branch_id=$1 ORDER BY s.active DESC, s.sort, s.id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Station{}
	for rows.Next() {
		var s Station
		if err := rows.Scan(&s.ID, &s.Name, &s.NameEn, &s.Active, &s.Sort, &s.ItemCount, &s.OpenTasks); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type StationInput struct {
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
	Active bool   `json:"active"`
	Sort   int    `json:"sort"`
}

// SaveStation creates (id == 0) or updates a station. Deactivating a station
// stops new dispatch to it; items already in the kitchen keep their station
// and can still be finished or explicitly moved.
func (a *App) SaveStation(ctx context.Context, branchID, id int64, in StationInput) (int64, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return 0, ErrValidation("name is required")
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if in.Active {
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kitchen_stations
				WHERE branch_id=$1 AND name=$2 AND active AND id <> $3)`, branchID, in.Name, id).Scan(&taken); err != nil {
				return err
			}
			if taken {
				return Err(http.StatusConflict, "STATION_NAME_TAKEN", "an active station already has this name")
			}
		}
		var err error
		if id == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO kitchen_stations (branch_id, name, name_en, active, sort)
				VALUES ($1,$2,$3,$4,$5) RETURNING id`, branchID, in.Name, in.NameEn, in.Active, in.Sort).Scan(&id)
		} else {
			err = tx.QueryRow(ctx, `UPDATE kitchen_stations SET name=$3, name_en=$4, active=$5, sort=$6
				WHERE id=$1 AND branch_id=$2 RETURNING id`, id, branchID, in.Name, in.NameEn, in.Active, in.Sort).Scan(&id)
		}
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		return branchChanged(ctx, tx, branchID)
	})
	return id, err
}

// AssignStation routes menu items to a station (nil clears the routing). It
// only affects orders accepted from now on.
func (a *App) AssignStation(ctx context.Context, branchID int64, itemIDs []int64, stationID *int64) (int, error) {
	if len(itemIDs) == 0 || len(itemIDs) > 500 {
		return 0, ErrValidation("choose between 1 and 500 items")
	}
	n := 0
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if stationID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kitchen_stations WHERE id=$1 AND branch_id=$2 AND active)`,
				*stationID, branchID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return ErrValidation("station not found or inactive")
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE menu_items SET station_id=$3, updated_at=now()
			WHERE branch_id=$1 AND id = ANY($2) AND NOT archived`, branchID, itemIDs, stationID)
		if err != nil {
			return err
		}
		n = int(tag.RowsAffected())
		return branchChanged(ctx, tx, branchID)
	})
	return n, err
}

// MoveItemStation moves an order item that is already in the kitchen to
// another station (nil = unassigned). The move is audited with both stations.
func (a *App) MoveItemStation(ctx context.Context, orderID, itemID int64, stationID *int64, reason string, actor Actor) (*Order, error) {
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		if o.Acceptance != "accepted" {
			return ErrInvalidState("order is not active")
		}
		var from *int64
		var status string
		if err := tx.QueryRow(ctx, `SELECT station_id, prep_status FROM order_items WHERE id=$1 AND order_id=$2 FOR UPDATE`,
			itemID, o.ID).Scan(&from, &status); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if status != "pending" && status != "preparing" {
			return ErrInvalidState("only items still being prepared can be moved")
		}
		name := ""
		if stationID != nil {
			// A deactivated station cannot receive work, but may keep what it has.
			err := tx.QueryRow(ctx, `SELECT name FROM kitchen_stations WHERE id=$1 AND branch_id=$2 AND active`,
				*stationID, o.BranchID).Scan(&name)
			if isNoRows(err) {
				return ErrValidation("station not found or inactive")
			} else if err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE order_items SET station_id=$2, station_name=$3 WHERE id=$1`, itemID, stationID, name); err != nil {
			return err
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "item.station_moved",
			map[string]any{"item_id": itemID, "from": from, "to": stationID, "reason": reason}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
}
