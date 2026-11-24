package app_test

import (
	"context"
	"slices"
	"testing"
)

// Runs after the other tests of the package (files go in name order), over
// the orders they made: the money ledger and the dish lines' shares must
// agree with the orders (docs/PRICING_REVIEW.md §5.3).
func TestMoneyLedgerOverAllOrders(t *testing.T) {
	need(t)
	ctx := context.Background()
	rows, err := testApp.DB.Query(ctx, `SELECT o.id, o.total,
		(SELECT COALESCE(sum(e.delta_total), 0) FROM order_money_events e WHERE e.order_id = o.id),
		o.service_charge, o.tax_amount, o.packaging_fee, o.staff_discount,
		(SELECT COALESCE(sum(i.service_share), 0) FROM order_items i WHERE i.order_id = o.id),
		(SELECT COALESCE(sum(i.tax_share), 0) FROM order_items i WHERE i.order_id = o.id),
		(SELECT COALESCE(sum(i.discount_share), 0) FROM order_items i WHERE i.order_id = o.id),
		(SELECT COALESCE(sum(v.value::bigint), 0) FROM order_discounts d, jsonb_each_text(d.per_line) v
			WHERE d.order_id = o.id),
		(SELECT COALESCE(sum(i.service_share + i.tax_share + i.discount_share), 0) FROM order_items i
			WHERE i.order_id = o.id AND i.void_id IS NOT NULL)
		FROM orders o WHERE o.created_at > now() - interval '15 minutes'`)
	noErr(t, err)
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, total, ledger, svc, tax, pack, staff, lsvc, ltax, ldisc, perLine, voided int64
		noErr(t, rows.Scan(&id, &total, &ledger, &svc, &tax, &pack, &staff, &lsvc, &ltax, &ldisc, &perLine, &voided))
		n++
		if ledger != total {
			t.Errorf("order %d: ledger %d, total %d", id, ledger, total)
		}
		if lsvc != svc {
			t.Errorf("order %d: line service %d, service charge %d", id, lsvc, svc)
		}
		if ltax > tax || (pack == 0 && ltax != tax) {
			t.Errorf("order %d: line tax %d, tax %d (packaging %d)", id, ltax, tax, pack)
		}
		if ldisc != perLine+staff {
			t.Errorf("order %d: line discounts %d, discount rows %d + staff %d", id, ldisc, perLine, staff)
		}
		if voided != 0 {
			t.Errorf("order %d: voided lines keep shares %d", id, voided)
		}
	}
	noErr(t, rows.Err())
	t.Logf("%d orders checked", n)

	// every kind of change was recorded under its own name
	var kinds []string
	noErr(t, testApp.DB.QueryRow(ctx, `SELECT array_agg(DISTINCT kind ORDER BY kind) FROM order_money_events
		WHERE created_at > now() - interval '15 minutes'`).Scan(&kinds))
	for _, k := range []string{"placed", "addon", "void", "refund", "adjust", "table", "close"} {
		if !slices.Contains(kinds, k) {
			t.Errorf("no %q rows in the ledger (have %v)", k, kinds)
		}
	}

	// the history an order page shows: one row per change, ending at the total
	var orderID, total int64
	noErr(t, testApp.DB.QueryRow(ctx, `SELECT o.id, o.total FROM orders o WHERE EXISTS
		(SELECT 1 FROM order_refunds r WHERE r.order_id = o.id) ORDER BY o.id DESC LIMIT 1`).Scan(&orderID, &total))
	evs, err := testApp.OrderMoneyEvents(ctx, orderID)
	noErr(t, err)
	var sum int64
	refunds := 0
	for _, e := range evs {
		sum += e.DeltaTotal
		if e.Kind == "refund" {
			refunds++
		}
	}
	if sum != total || refunds == 0 || evs[len(evs)-1].Total != total {
		t.Errorf("order %d history %+v, total %d", orderID, evs, total)
	}

	// the ledger cannot be changed
	if _, err := testApp.DB.Exec(ctx, `UPDATE order_money_events SET total = 0
		WHERE id = (SELECT max(id) FROM order_money_events)`); err == nil {
		t.Error("ledger row updated")
	}
	if _, err := testApp.DB.Exec(ctx, `DELETE FROM order_money_events
		WHERE id = (SELECT max(id) FROM order_money_events)`); err == nil {
		t.Error("ledger row deleted")
	}
}
