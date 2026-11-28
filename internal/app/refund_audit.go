package app

import (
	"context"
	"encoding/json"
	"time"
)

// RefundAuditRow is an order whose refunds are worth a look
// (docs/CODE_REVIEW_FIX_PLAN.md H01, H07). The audit only reads.
type RefundAuditRow struct {
	OrderID  int64
	BranchID int64
	PublicNo string
	Currency string
	Total    int64
	// Why is "unpaid" (more refunded than the order took in) or "split"
	// (refunded in several goes while it had offers).
	Why      string
	Refunds  int
	Refunded int64
	// Should is what the rules of 2026-11-28 give: what was received
	// (unpaid), or one refund of the same dishes (split).
	Should int64
}

// RefundAudit lists the orders created since then that were refunded more
// than they took in, and those refunded dish by dish, in several goes and
// not in full, while a promotion or coupon was on them: until 2026-11-28
// each of those refunds was priced against the whole order. A split order
// whose staff gave back less than suggested shows as well; the list is for a
// person to read.
func (a *App) RefundAudit(ctx context.Context, since time.Time) ([]RefundAuditRow, error) {
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.branch_id, o.public_no, o.currency, o.total, o.discount, o.service_type,
			count(r.id), sum(r.amount), (array_agg(r.reason_kind ORDER BY r.id DESC))[1],
			CASE WHEN o.payment_status = 'paid' THEN o.total ELSE LEAST(o.total, o.credit_paid + COALESCE(
				(SELECT c.amount_collected FROM cash_collections c WHERE c.order_id = o.id), 0)) END
		FROM orders o JOIN order_refunds r ON r.order_id = o.id
		WHERE o.created_at >= $1 GROUP BY o.id ORDER BY o.id`, since)
	if err != nil {
		return nil, err
	}
	type cand struct {
		RefundAuditRow
		discount int64
		service  string
		kind     string
		received int64
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.OrderID, &c.BranchID, &c.PublicNo, &c.Currency, &c.Total, &c.discount, &c.service,
			&c.Refunds, &c.Refunded, &c.kind, &c.received); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []RefundAuditRow{}
	for _, c := range cands {
		if c.Refunded > c.received {
			r := c.RefundAuditRow
			r.Why, r.Should = "unpaid", c.received
			out = append(out, r)
			continue
		}
		if c.Refunds < 2 || c.discount == 0 {
			continue
		}
		// every dish refunded so far, as one refund
		gone := map[int64]int{}
		rr, err := a.DB.Query(ctx, `SELECT lines FROM order_refunds WHERE order_id=$1`, c.OrderID)
		if err != nil {
			return nil, err
		}
		for rr.Next() {
			var raw []byte
			if err := rr.Scan(&raw); err != nil {
				rr.Close()
				return nil, err
			}
			var ls []RefundLine
			_ = json.Unmarshal(raw, &ls)
			for _, l := range ls {
				gone[l.ItemID] += l.Qty
			}
		}
		rr.Close()
		m, err := loadMoney(ctx, a.DB, c.OrderID)
		if err != nil {
			return nil, err
		}
		less := map[int64]int64{}
		left := false
		for _, l := range m.Lines {
			less[l.ID] = l.Unit * int64(gone[l.ID])
			if less[l.ID] < l.Amount {
				left = true
			}
		}
		if !left {
			continue // refunded in full: the last refund gave what was left
		}
		b, err := readBranch(ctx, a.DB, c.BranchID)
		if err != nil {
			return nil, err
		}
		pol := voidPolicy(m, c.kind, false)
		if c.service == "dine_in" {
			pol = dinePolicy(m, c.kind == "store")
		}
		p, _, err := m.reprice(ctx, a.DB, b, m.without(less), pol)
		if err != nil {
			return nil, err
		}
		should := max(0, min(c.Total-max(0, p.Totals.Total-m.RoundOff), c.received))
		if should != c.Refunded {
			r := c.RefundAuditRow
			r.Why, r.Should = "split", should
			out = append(out, r)
		}
	}
	return out, nil
}
