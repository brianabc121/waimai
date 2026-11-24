package app

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Cooking progress in the back office (docs/COOKING_PROGRESS_DESIGN.md): which
// dishes of every open order are waiting, being made or ready, and how long
// today's orders took in the kitchen. The page only reads; the kitchen screen
// and the workbench still start and finish dishes.

// Progress is what the "cooking progress" page shows live.
type Progress struct {
	// Orders still in the kitchen or waiting to be handed over, including
	// scheduled ones that are not due yet and orders cancelled in the last
	// ten minutes (the page tells the kitchen to stop).
	Orders []*Order `json:"orders"`
	// TargetMinutes is the store's cooking target.
	TargetMinutes int    `json:"target_minutes"`
	Timezone      string `json:"timezone"`
	// Today is the summary of what was finished today.
	Today DoneSummary `json:"today"`
}

// DoneBatch is one order or one of its add-ons (a dine-in round) whose
// dishes are all ready.
type DoneBatch struct {
	OrderID     int64     `json:"order_id"`
	PublicNo    string    `json:"public_no"`
	ServiceType string    `json:"service_type"`
	TableName   string    `json:"table_name"`
	BatchSeq    int       `json:"batch_seq"`
	Batches     int       `json:"batches"`
	Qty         int       `json:"qty"`
	KitchenAt   time.Time `json:"kitchen_at"`
	DoneAt      time.Time `json:"done_at"`
	Minutes     int       `json:"minutes"`
	Late        bool      `json:"late"`
	Cooks       []string  `json:"cooks"`
	// The order now, for the "now" column.
	AcceptanceStatus  string     `json:"acceptance_status"`
	FulfillmentStatus string     `json:"fulfillment_status"`
	PackingStatus     string     `json:"packing_status"`
	RiderName         string     `json:"rider_name"`
	PickedUpAt        *time.Time `json:"picked_up_at"`
	Served            bool       `json:"served"`
}

type DoneSummary struct {
	Count     int `json:"count"`
	AvgMinute int `json:"avg_minutes"`
	// MedianMinutes: half the batches were done within this.
	MedianMinutes int `json:"median_minutes"`
	Late          int `json:"late"`
}

// DoneDay is the "finished" tab of the page.
type DoneDay struct {
	Date          string      `json:"date"`
	Timezone      string      `json:"timezone"`
	TargetMinutes int         `json:"target_minutes"`
	Summary       DoneSummary `json:"summary"`
	Batches       []DoneBatch `json:"batches"`
}

// Progress loads the live page.
func (a *App) Progress(ctx context.Context, branchID int64) (*Progress, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	orders, err := queryOrders(ctx, a.DB, `o.branch_id=$1 AND (
		(o.acceptance_status='accepted' AND o.fulfillment_status IN ('none','awaiting_handoff','awaiting_pickup'))
		OR (o.acceptance_status='cancelled' AND o.accepted_at IS NOT NULL AND o.closed_at > now() - interval '10 minutes'
			AND (o.release_at IS NULL OR o.release_at <= o.closed_at)))
		ORDER BY o.accepted_at`, branchID)
	if err != nil {
		return nil, err
	}
	day, err := a.doneDay(ctx, b, time.Now())
	if err != nil {
		return nil, err
	}
	return &Progress{Orders: orders, TargetMinutes: b.CookTargetMinutes, Timezone: b.Timezone, Today: day.Summary}, nil
}

// ProgressDone lists the batches finished on a day (YYYY-MM-DD on the store's
// clock; empty for today).
func (a *App) ProgressDone(ctx context.Context, branchID int64, date string) (*DoneDay, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(b.Timezone)
	if err != nil {
		loc = time.UTC
	}
	at := time.Now()
	if date != "" {
		d, err := time.ParseInLocation("2006-01-02", date, loc)
		if err != nil {
			return nil, ErrValidation("date must be YYYY-MM-DD")
		}
		at = d.Add(12 * time.Hour)
	}
	return a.doneDay(ctx, b, at)
}

func (a *App) doneDay(ctx context.Context, b *Branch, at time.Time) (*DoneDay, error) {
	loc, err := time.LoadLocation(b.Timezone)
	if err != nil {
		loc = time.UTC
	}
	at = at.In(loc)
	from := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 1)
	orders, err := queryOrders(ctx, a.DB, `o.branch_id=$1 AND EXISTS (SELECT 1 FROM order_items i
		WHERE i.order_id = o.id AND i.ready_at >= $2 AND i.ready_at < $3) ORDER BY o.id`, b.ID, from, to)
	if err != nil {
		return nil, err
	}
	out := &DoneDay{Date: from.Format("2006-01-02"), Timezone: b.Timezone, TargetMinutes: b.CookTargetMinutes, Batches: []DoneBatch{}}
	for _, o := range orders {
		out.Batches = append(out.Batches, doneBatches(o, b.CookTargetMinutes, from, to)...)
	}
	sort.Slice(out.Batches, func(i, j int) bool { return out.Batches[i].DoneAt.After(out.Batches[j].DoneAt) })
	out.Summary = summarize(out.Batches)
	return out, nil
}

// doneBatches splits an order into its batches and keeps those whose dishes
// for the kitchen are all ready, the last of them within [from, to).
func doneBatches(o *Order, target int, from, to time.Time) []DoneBatch {
	type acc struct {
		seq     int
		qty     int
		open    bool
		served  bool
		kitchen *time.Time
		done    time.Time
		cooks   []string
	}
	bySeq := map[int]*acc{}
	seqs := []int{}
	maxSeq := 0
	for _, it := range o.Items {
		maxSeq = max(maxSeq, it.BatchSeq)
		if !it.NeedsPrep || it.PrepStatus == "cancelled" {
			continue
		}
		b := bySeq[it.BatchSeq]
		if b == nil {
			b = &acc{seq: it.BatchSeq, served: true, kitchen: it.KitchenAt}
			bySeq[it.BatchSeq] = b
			seqs = append(seqs, it.BatchSeq)
		}
		b.qty += it.Qty
		if it.PrepStatus != "ready" || it.ReadyAt == nil {
			b.open = true
			continue
		}
		if it.ServedAt == nil {
			b.served = false
		}
		if it.ReadyAt.After(b.done) {
			b.done = *it.ReadyAt
		}
		if it.ReadyBy != "" && !slices.Contains(b.cooks, it.ReadyBy) {
			b.cooks = append(b.cooks, it.ReadyBy)
		}
	}
	out := []DoneBatch{}
	for _, s := range seqs {
		b := bySeq[s]
		if b.open || b.kitchen == nil || b.done.Before(from) || !b.done.Before(to) {
			continue
		}
		mins := int(b.done.Sub(*b.kitchen).Round(time.Minute) / time.Minute)
		mins = max(mins, 0)
		d := DoneBatch{OrderID: o.ID, PublicNo: o.PublicNo, ServiceType: o.ServiceType, TableName: o.TableName,
			BatchSeq: s, Batches: maxSeq, Qty: b.qty, KitchenAt: *b.kitchen, DoneAt: b.done, Minutes: mins,
			Late: mins > target, Cooks: b.cooks, AcceptanceStatus: o.AcceptanceStatus,
			FulfillmentStatus: o.FulfillmentStatus, PackingStatus: o.PackingStatus,
			Served: o.ServiceType == "dine_in" && b.served}
		if d.Cooks == nil {
			d.Cooks = []string{}
		}
		if o.Delivery != nil {
			d.RiderName, d.PickedUpAt = o.Delivery.RiderName, o.Delivery.PickedUpAt
		}
		out = append(out, d)
	}
	return out
}

func summarize(batches []DoneBatch) DoneSummary {
	s := DoneSummary{Count: len(batches)}
	if len(batches) == 0 {
		return s
	}
	mins := make([]int, len(batches))
	total := 0
	for i, b := range batches {
		mins[i] = b.Minutes
		total += b.Minutes
		if b.Late {
			s.Late++
		}
	}
	sort.Ints(mins)
	s.AvgMinute = (total + len(mins)/2) / len(mins)
	s.MedianMinutes = mins[(len(mins)-1)/2]
	return s
}

// RushDish asks the kitchen to hurry a dish of any open order (delivery,
// pickup or dine-in) that is not ready yet.
func (a *App) RushDish(ctx context.Context, itemID int64, actor Actor) (*Order, error) {
	orderID, _, err := a.ItemOrder(ctx, itemID)
	if err != nil {
		return nil, err
	}
	return a.command(ctx, orderID, func(tx pgx.Tx, o *lockedOrder) error {
		if o.Acceptance != "accepted" || o.Fulfillment == "dined" {
			return ErrInvalidState("order is not active")
		}
		var status, name string
		if err := tx.QueryRow(ctx, `SELECT prep_status, name FROM order_items WHERE id=$1 AND order_id=$2 FOR UPDATE`,
			itemID, o.ID).Scan(&status, &name); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
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
		if err := audit(ctx, tx, o.BranchID, o.ID, actor, "item.rushed",
			map[string]any{"item_id": itemID, "name": name}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: o.BranchID, OrderID: o.ID})
	})
}
