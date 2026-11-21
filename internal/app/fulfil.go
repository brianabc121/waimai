package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ---- merchant order desk ----

// ListOrders returns a branch's orders. scope "active" is everything still in
// progress; "history" is the most recent finished or closed orders.
func (a *App) ListOrders(ctx context.Context, branchID int64, scope string) ([]*Order, error) {
	active := `o.acceptance_status = 'pending' OR (o.acceptance_status = 'accepted'
		AND o.fulfillment_status NOT IN ('delivered','picked_up'))`
	if scope == "history" {
		return queryOrders(ctx, a.DB, `o.branch_id=$1 AND NOT (`+active+`) ORDER BY o.created_at DESC LIMIT 100`, branchID)
	}
	return queryOrders(ctx, a.DB, `o.branch_id=$1 AND (`+active+`) ORDER BY o.created_at`, branchID)
}

// OrderBranch returns the branch an order belongs to, for permission checks.
func (a *App) OrderBranch(ctx context.Context, orderID int64) (int64, error) {
	var b int64
	err := a.DB.QueryRow(ctx, `SELECT branch_id FROM orders WHERE id=$1`, orderID).Scan(&b)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return b, err
}

func (a *App) GetOrder(ctx context.Context, orderID int64) (*Order, error) {
	return loadOrder(ctx, a.DB, orderID)
}

// command runs fn under the order lock and returns the fresh order view.
func (a *App) command(ctx context.Context, orderID int64, fn func(tx pgx.Tx, o *lockedOrder) error) (*Order, error) {
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		o, err := lockOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		return fn(tx, o)
	})
	if err != nil {
		return nil, err
	}
	return loadOrder(ctx, a.DB, orderID)
}

func checkVersion(o *lockedOrder, expected int) error {
	if expected != 0 && expected != o.Version {
		return ErrVersion
	}
	return nil
}

// recomputePacking moves packing between waiting_items and ready_to_pack
// according to whether every live item is ready.
func recomputePacking(ctx context.Context, tx pgx.Tx, orderID int64) error {
	_, err := tx.Exec(ctx, `UPDATE orders o SET packing_status = CASE WHEN NOT EXISTS (
			SELECT 1 FROM order_items i WHERE i.order_id = o.id AND i.prep_status IN ('pending','preparing'))
		THEN 'ready_to_pack' ELSE 'waiting_items' END
		WHERE o.id=$1 AND o.packing_status IN ('waiting_items','ready_to_pack')`, orderID)
	return err
}

// Accept moves a pending order to accepted and, in the same transaction,
// creates the kitchen work (items become visible on the board) and
// initialises packing.
func (a *App) Accept(ctx context.Context, orderID int64, expectedVersion int, actor Actor) (*Order, error) {
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		if o.Acceptance == "accepted" && expectedVersion == 0 {
			return nil
		}
		if o.Acceptance != "pending" {
			return ErrInvalidState("order is not pending")
		}
		if err := checkVersion(o, expectedVersion); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE order_items SET prep_status='ready' WHERE order_id=$1 AND NOT needs_prep`, o.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET acceptance_status='accepted', accepted_at=now(),
			packing_status='waiting_items', version=version+1, updated_at=now() WHERE id=$1`, o.ID); err != nil {
			return err
		}
		if err := recomputePacking(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.accepted", nil); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
}

func (a *App) Reject(ctx context.Context, orderID int64, expectedVersion int, reason string, actor Actor) (*Order, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrValidation("reason is required")
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		if o.Acceptance != "pending" {
			return ErrInvalidState("order is not pending")
		}
		if err := checkVersion(o, expectedVersion); err != nil {
			return err
		}
		return closeOrder(ctx, tx, o, "rejected", reason, actor)
	})
}

// StaffCancel cancels an accepted order before it left the store.
func (a *App) StaffCancel(ctx context.Context, orderID int64, expectedVersion int, reason string, actor Actor) (*Order, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrValidation("reason is required")
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		if o.Acceptance != "accepted" {
			return ErrInvalidState("only accepted orders can be cancelled; reject pending orders instead")
		}
		switch o.Fulfillment {
		case "none", "awaiting_handoff", "awaiting_pickup":
		default:
			return ErrInvalidState("the order already left the store")
		}
		if err := checkVersion(o, expectedVersion); err != nil {
			return err
		}
		return closeOrder(ctx, tx, o, "cancelled", reason, actor)
	})
}

// AssignRider assigns or reassigns a delivery order. Reassignment bumps
// assignment_version and changes rider_id, which locks the previous rider out.
func (a *App) AssignRider(ctx context.Context, orderID, riderID int64, actor Actor) (*Order, error) {
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		if o.ServiceType != "delivery" {
			return ErrInvalidState("pickup orders have no rider")
		}
		if o.Acceptance != "accepted" || (o.Fulfillment != "none" && o.Fulfillment != "awaiting_handoff") {
			return ErrInvalidState("a rider can only be assigned before handoff")
		}
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_branch_roles r JOIN users u ON u.id = r.user_id
			WHERE r.user_id=$1 AND r.branch_id=$2 AND r.role='rider' AND u.active)`, riderID, o.BranchID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrValidation("rider not found in this branch")
		}
		riders := []int64{riderID}
		var prev int64
		err := tx.QueryRow(ctx, `SELECT rider_id FROM delivery_tasks WHERE order_id=$1 FOR UPDATE`, o.ID).Scan(&prev)
		switch {
		case isNoRows(err):
			if _, err := tx.Exec(ctx, `INSERT INTO delivery_tasks (order_id, branch_id, rider_id) VALUES ($1,$2,$3)`,
				o.ID, o.BranchID, riderID); err != nil {
				return err
			}
		case err != nil:
			return err
		case prev == riderID:
			return nil
		default:
			riders = append(riders, prev)
			if _, err := tx.Exec(ctx, `UPDATE delivery_tasks SET rider_id=$2, assignment_version=assignment_version+1,
				status='assigned', assigned_at=now() WHERE order_id=$1`, o.ID, riderID); err != nil {
				return err
			}
		}
		if err := bump(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "delivery.assigned",
			map[string]any{"rider_id": riderID, "previous_rider_id": prev}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID, RiderIDs: riders})
	})
}

// handoff records the physical handover. Store staff and the assigned rider
// both call it; the first valid confirmation wins and the other side gets the
// current state back.
func handoff(ctx context.Context, tx pgx.Tx, o *lockedOrder, actor Actor) error {
	if o.Acceptance != "accepted" {
		return ErrInvalidState("order is not active")
	}
	if o.ServiceType == "pickup" {
		if o.Fulfillment == "picked_up" {
			return nil
		}
		if o.Fulfillment != "awaiting_pickup" {
			return Err(http.StatusConflict, "ITEMS_NOT_READY", "the order is not packed yet")
		}
		// Pay-at-pickup: handing over at the counter is also when cash is taken.
		if _, err := tx.Exec(ctx, `UPDATE orders SET fulfillment_status='picked_up', payment_status='paid',
			handed_off_at=now(), completed_at=now(), version=version+1, updated_at=now() WHERE id=$1`, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.picked_up", nil); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	}
	switch o.Fulfillment {
	case "out_for_delivery", "arrived", "delivered":
		return nil
	case "awaiting_handoff":
	default:
		return Err(http.StatusConflict, "ITEMS_NOT_READY", "the order is not packed yet")
	}
	var riderID int64
	err := tx.QueryRow(ctx, `UPDATE delivery_tasks SET status='picked_up', picked_up_at=now()
		WHERE order_id=$1 AND status='assigned' RETURNING rider_id`, o.ID).Scan(&riderID)
	if isNoRows(err) {
		return ErrInvalidState("assign a rider before handing off")
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET fulfillment_status='out_for_delivery', handed_off_at=now(),
		version=version+1, updated_at=now() WHERE id=$1`, o.ID); err != nil {
		return err
	}
	if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.handed_off", map[string]any{"rider_id": riderID}); err != nil {
		return err
	}
	return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID, RiderIDs: []int64{riderID}})
}

func (a *App) StaffHandoff(ctx context.Context, orderID int64, actor Actor) (*Order, error) {
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error { return handoff(ctx, tx, o, actor) })
}

// ---- kitchen ----

// KitchenBoard returns accepted orders that have not left the store, plus
// orders cancelled in the last 10 minutes after acceptance so that the kitchen
// sees a "cancelled" notice instead of a ticket silently disappearing.
func (a *App) KitchenBoard(ctx context.Context, branchID int64) ([]*Order, error) {
	return queryOrders(ctx, a.DB, `o.branch_id=$1 AND (
		(o.acceptance_status='accepted' AND o.fulfillment_status IN ('none','awaiting_handoff','awaiting_pickup'))
		OR (o.acceptance_status='cancelled' AND o.accepted_at IS NOT NULL AND o.closed_at > now() - interval '10 minutes'))
		ORDER BY o.accepted_at`, branchID)
}

// ItemOrder resolves an order item to its order and branch for permission checks.
func (a *App) ItemOrder(ctx context.Context, itemID int64) (orderID, branchID int64, err error) {
	err = a.DB.QueryRow(ctx, `SELECT order_id, branch_id FROM order_items WHERE id=$1`, itemID).Scan(&orderID, &branchID)
	if isNoRows(err) {
		err = ErrNotFound
	}
	return
}

// SetItemStatus applies a kitchen action to one order item: start, ready or undo.
func (a *App) SetItemStatus(ctx context.Context, orderID, itemID int64, action string, actor Actor) (*Order, error) {
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		if o.Acceptance != "accepted" {
			return ErrInvalidState("order is not active")
		}
		var cur string
		if err := tx.QueryRow(ctx, `SELECT prep_status FROM order_items WHERE id=$1 AND order_id=$2 FOR UPDATE`,
			itemID, o.ID).Scan(&cur); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		var next string
		switch action {
		case "start":
			if cur == "preparing" {
				return nil
			}
			if cur != "pending" {
				return ErrInvalidState("item is not pending")
			}
			next = "preparing"
		case "ready":
			if cur == "ready" {
				return nil
			}
			if cur != "pending" && cur != "preparing" {
				return ErrInvalidState("item cannot be marked ready")
			}
			next = "ready"
		case "undo":
			if cur == "preparing" {
				return nil
			}
			if cur != "ready" {
				return ErrInvalidState("item is not ready")
			}
			if o.Packing == "packed" {
				return ErrInvalidState("the order is already packed")
			}
			next = "preparing"
		default:
			return ErrValidation("unknown action")
		}
		if _, err := tx.Exec(ctx, `UPDATE order_items SET prep_status=$2 WHERE id=$1`, itemID, next); err != nil {
			return err
		}
		// Undo invalidates any packing check made against the old list.
		manifestInc := 0
		if action == "undo" {
			manifestInc = 1
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET version=version+1, manifest_version=manifest_version+$2,
			updated_at=now() WHERE id=$1`, o.ID, manifestInc); err != nil {
			return err
		}
		if err := recomputePacking(ctx, tx, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "item."+action, map[string]any{"item_id": itemID}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
}

// Pack confirms the bag check for the whole order. It needs every live item
// ready and the manifest version the packer was looking at.
func (a *App) Pack(ctx context.Context, orderID int64, bags, manifestVersion int, actor Actor) (*Order, error) {
	if bags < 1 || bags > 50 {
		return nil, ErrValidation("bags must be between 1 and 50")
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		if o.Acceptance != "accepted" {
			return ErrInvalidState("order is not active")
		}
		if o.Packing == "packed" {
			return nil
		}
		if manifestVersion != o.ManifestVersion {
			return Err(http.StatusConflict, "MANIFEST_CHANGED", "the order changed, check the list again")
		}
		if o.Packing != "ready_to_pack" {
			return Err(http.StatusConflict, "ITEMS_NOT_READY", "some items are not ready yet")
		}
		next := "awaiting_handoff"
		if o.ServiceType == "pickup" {
			next = "awaiting_pickup"
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET packing_status='packed', bags=$2, packed_at=now(),
			fulfillment_status=$3, version=version+1, updated_at=now() WHERE id=$1`, o.ID, bags, next); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "order.packed", map[string]any{"bags": bags}); err != nil {
			return err
		}
		riders := []int64{}
		var riderID int64
		if err := tx.QueryRow(ctx, `SELECT rider_id FROM delivery_tasks WHERE order_id=$1 AND status='assigned'`,
			o.ID).Scan(&riderID); err == nil {
			riders = append(riders, riderID)
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID, RiderIDs: riders})
	})
}

// ---- rider ----

// RiderTasks lists the rider's current assignments and what they finished in
// the last 12 hours.
func (a *App) RiderTasks(ctx context.Context, riderID int64) ([]*Order, error) {
	return queryOrders(ctx, a.DB, `o.id IN (SELECT order_id FROM delivery_tasks WHERE rider_id=$1 AND (
		status IN ('assigned','picked_up','arrived')
		OR (status IN ('delivered','failed') AND coalesce(delivered_at, assigned_at) > now() - interval '12 hours')))
		ORDER BY o.created_at`, riderID)
}

// riderCommand locks the order, then the task, and verifies the task still
// belongs to this rider. A reassigned rider gets NOT_ASSIGNED.
func (a *App) riderCommand(ctx context.Context, taskID, riderID int64, fn func(tx pgx.Tx, o *lockedOrder, status string) error) (*Order, error) {
	var orderID int64
	err := a.DB.QueryRow(ctx, `SELECT order_id FROM delivery_tasks WHERE id=$1 AND rider_id=$2`, taskID, riderID).Scan(&orderID)
	if isNoRows(err) {
		return nil, Err(http.StatusNotFound, "NOT_ASSIGNED", "this delivery is not assigned to you")
	} else if err != nil {
		return nil, err
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		var status string
		var owner int64
		if err := tx.QueryRow(ctx, `SELECT status, rider_id FROM delivery_tasks WHERE id=$1 FOR UPDATE`, taskID).
			Scan(&status, &owner); err != nil {
			return err
		}
		if owner != riderID || status == "cancelled" {
			return Err(http.StatusNotFound, "NOT_ASSIGNED", "this delivery is not assigned to you")
		}
		return fn(tx, o, status)
	})
}

func (a *App) RiderPickup(ctx context.Context, taskID, riderID int64) (*Order, error) {
	return a.riderCommand(ctx, taskID, riderID, func(tx pgx.Tx, o *lockedOrder, _ string) error {
		return handoff(ctx, tx, o, Actor{Type: "rider", ID: riderID})
	})
}

func (a *App) RiderArrive(ctx context.Context, taskID, riderID int64) (*Order, error) {
	return a.riderCommand(ctx, taskID, riderID, func(tx pgx.Tx, o *lockedOrder, status string) error {
		if status == "arrived" || status == "delivered" {
			return nil
		}
		if status != "picked_up" {
			return ErrInvalidState("confirm pickup first")
		}
		if _, err := tx.Exec(ctx, `UPDATE delivery_tasks SET status='arrived', arrived_at=now() WHERE id=$1`, taskID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET fulfillment_status='arrived', version=version+1, updated_at=now()
			WHERE id=$1`, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, Actor{Type: "rider", ID: riderID}, "delivery.arrived", nil); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID, RiderIDs: []int64{riderID}})
	})
}

// RiderDeliver records delivery and the cash actually collected in one
// transaction. A repeat call returns the current state and records nothing.
func (a *App) RiderDeliver(ctx context.Context, taskID, riderID, collected int64) (*Order, error) {
	if collected < 0 {
		return nil, ErrValidation("collected_amount must not be negative")
	}
	return a.riderCommand(ctx, taskID, riderID, func(tx pgx.Tx, o *lockedOrder, status string) error {
		if status == "delivered" {
			return nil
		}
		if status != "picked_up" && status != "arrived" {
			return ErrInvalidState("confirm pickup first")
		}
		if _, err := tx.Exec(ctx, `UPDATE delivery_tasks SET status='delivered', delivered_at=now() WHERE id=$1`, taskID); err != nil {
			return err
		}
		pay := o.Payment
		if o.Payment == "unpaid" {
			if _, err := tx.Exec(ctx, `INSERT INTO cash_collections (order_id, branch_id, rider_id, amount_due,
				amount_collected) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (order_id) DO NOTHING`,
				o.ID, o.BranchID, riderID, o.Total, collected); err != nil {
				return err
			}
			if collected == o.Total {
				pay = "paid"
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET fulfillment_status='delivered', payment_status=$2,
			completed_at=now(), version=version+1, updated_at=now() WHERE id=$1`, o.ID, pay); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, Actor{Type: "rider", ID: riderID}, "delivery.delivered",
			map[string]any{"amount_due": o.Total, "amount_collected": collected}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID, RiderIDs: []int64{riderID}})
	})
}

func (a *App) RiderFail(ctx context.Context, taskID, riderID int64, reason string) (*Order, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrValidation("reason is required")
	}
	return a.riderCommand(ctx, taskID, riderID, func(tx pgx.Tx, o *lockedOrder, status string) error {
		if status == "failed" {
			return nil
		}
		if status != "picked_up" && status != "arrived" {
			return ErrInvalidState("only an order you are carrying can be reported")
		}
		if _, err := tx.Exec(ctx, `UPDATE delivery_tasks SET status='failed', fail_reason=$2 WHERE id=$1`, taskID, reason); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET fulfillment_status='delivery_failed', version=version+1,
			updated_at=now() WHERE id=$1`, o.ID); err != nil {
			return err
		}
		if err := audit(ctx, tx, o.BranchID, o.ID, Actor{Type: "rider", ID: riderID}, "delivery.failed",
			map[string]any{"reason": reason}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID, RiderIDs: []int64{riderID}})
	})
}

// ---- cash reconciliation ----

func queryCash(ctx context.Context, q Querier, where string, args ...any) ([]*CashCollection, error) {
	rows, err := q.Query(ctx, `SELECT c.id, c.order_id, o.public_no, c.rider_id, u.display_name, c.amount_due,
		c.amount_collected, c.reconcile_status, c.amount_returned, c.reconciled_at, c.note, c.created_at
		FROM cash_collections c JOIN users u ON u.id = c.rider_id JOIN orders o ON o.id = c.order_id
		WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*CashCollection{}
	for rows.Next() {
		c := &CashCollection{}
		if err := rows.Scan(&c.ID, &c.OrderID, &c.PublicNo, &c.RiderID, &c.RiderName, &c.AmountDue,
			&c.AmountCollected, &c.ReconcileStatus, &c.AmountReturned, &c.ReconciledAt, &c.Note, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *App) ListCash(ctx context.Context, branchID int64) ([]*CashCollection, error) {
	return queryCash(ctx, a.DB, `c.branch_id=$1 ORDER BY (c.reconcile_status='pending') DESC, c.created_at DESC LIMIT 200`, branchID)
}

func (a *App) CashBranch(ctx context.Context, id int64) (int64, error) {
	var b int64
	err := a.DB.QueryRow(ctx, `SELECT branch_id FROM cash_collections WHERE id=$1`, id).Scan(&b)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return b, err
}

// Reconcile records the cash the rider actually handed back to the store.
func (a *App) Reconcile(ctx context.Context, id, amountReturned int64, note string, actor Actor) (*CashCollection, error) {
	if amountReturned < 0 {
		return nil, ErrValidation("amount_returned must not be negative")
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var status string
		var collected, branchID, orderID int64
		if err := tx.QueryRow(ctx, `SELECT reconcile_status, amount_collected, branch_id, order_id
			FROM cash_collections WHERE id=$1 FOR UPDATE`, id).Scan(&status, &collected, &branchID, &orderID); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if status != "pending" {
			return ErrInvalidState("already reconciled")
		}
		next := "reconciled"
		if amountReturned != collected {
			next = "discrepancy"
		}
		if _, err := tx.Exec(ctx, `UPDATE cash_collections SET reconcile_status=$2, amount_returned=$3,
			reconciled_by=$4, reconciled_at=now(), note=$5 WHERE id=$1`, id, next, amountReturned, actor.ID, note); err != nil {
			return err
		}
		if err := audit(ctx, tx, branchID, orderID, actor, "cash."+next,
			map[string]any{"amount_collected": collected, "amount_returned": amountReturned}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID, OrderID: orderID})
	})
	if err != nil {
		return nil, err
	}
	out, err := queryCash(ctx, a.DB, `c.id=$1`, id)
	if err != nil || len(out) == 0 {
		return nil, wrap("load cash", err)
	}
	return out[0], nil
}
