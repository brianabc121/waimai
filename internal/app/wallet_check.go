package app

import (
	"context"
	"fmt"
	"log"
	"time"
)

// The store balance rules of docs/REFUND_BALANCE_DESIGN.md §11 (W1–W7),
// checked by make pricing-check, cmd/pricecheck and every night (§10.1).

// checkOrderCredit checks W4–W7 on one order; an error reading is reported
// as a broken rule, so that nothing is skipped silently.
func checkOrderCredit(ctx context.Context, q Querier, orderID int64) []string {
	var bad []string
	fail := func(f string, v ...any) { bad = append(bad, fmt.Sprintf(f, v...)) }
	var total, paid, spent, refunded, cashPart, cashOut, toCredit, posted, branch, acctBranch int64
	var status, currency, acctCurrency string
	var others int
	err := q.QueryRow(ctx, `SELECT o.total, o.credit_paid, o.acceptance_status, o.branch_id, o.currency,
		-COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			JOIN wallet_accounts w ON w.id = p.account_id
			WHERE j.order_id = o.id AND j.kind IN ('spend', 'return') AND w.kind = 'store_credit'), 0),
		COALESCE((SELECT sum(r.amount) FROM order_refunds r WHERE r.order_id = o.id), 0),
		COALESCE((SELECT sum(r.cash_part) FROM order_refunds r WHERE r.order_id = o.id), 0),
		COALESCE((SELECT sum(r.amount - r.to_credit) FROM order_refunds r WHERE r.order_id = o.id), 0),
		COALESCE((SELECT sum(r.to_credit) FROM order_refunds r WHERE r.order_id = o.id), 0),
		COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			JOIN wallet_accounts w ON w.id = p.account_id
			WHERE j.order_id = o.id AND j.kind IN ('refund', 'revoke') AND w.kind = 'store_credit'), 0),
		COALESCE((SELECT min(w.branch_id) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			JOIN wallet_accounts w ON w.id = p.account_id WHERE j.order_id = o.id AND w.kind = 'store_credit'), o.branch_id),
		COALESCE((SELECT min(w.currency) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			JOIN wallet_accounts w ON w.id = p.account_id WHERE j.order_id = o.id AND w.kind = 'store_credit'), o.currency),
		(SELECT count(*) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			JOIN wallet_accounts w ON w.id = p.account_id
			WHERE j.order_id = o.id AND w.kind = 'store_credit' AND (w.branch_id <> o.branch_id OR w.currency <> o.currency))
		FROM orders o WHERE o.id = $1`, orderID).Scan(&total, &paid, &status, &branch, &currency, &spent, &refunded,
		&cashPart, &cashOut, &toCredit, &posted, &acctBranch, &acctCurrency, &others)
	if err != nil {
		return []string{"W read: " + err.Error()}
	}
	// W3 a balance is only used and filled at its own store, in its currency
	if others > 0 {
		fail("W3 %d postings on a balance of another store or currency", others)
	}
	// W4 the balance never pays more than the order; a closed order paid nothing from it
	if paid < 0 || paid > total {
		fail("W4 paid %d from the balance, total %d", paid, total)
	}
	if (status == "rejected" || status == "cancelled") && paid != 0 {
		fail("W4 closed order still paid %d from the balance", paid)
	}
	if spent != paid {
		fail("W4 the books took %d from the balance, the order says %d", spent, paid)
	}
	// W5 refunds of every kind never exceed what was paid (D7 counts them too)
	if refunded > total {
		fail("W5 refunded %d, total %d", refunded, total)
	}
	// W6 the cash part follows the cash paid, and no more cash goes back than came in
	if cashPart > total-paid {
		fail("W6 cash part of refunds %d, cash paid %d", cashPart, total-paid)
	}
	if cashOut > total-paid {
		fail("W6 %d given back in cash, %d paid in cash", cashOut, total-paid)
	}
	if refunded-cashPart > toCredit {
		fail("W6 %d of refunds came from the balance but only %d went back to it", refunded-cashPart, toCredit)
	}
	// W7 every refund into the balance is in the books, to the cent
	if posted != toCredit {
		fail("W7 refunds say %d went into the balance, the books %d", toCredit, posted)
	}
	return bad
}

// CheckWallet checks the books as a whole: every journal balances in one
// currency (W1), every balance is the sum of its postings and no customer's
// is below zero (W2), and each store's side mirrors its customers' (§4.2).
func (a *App) CheckWallet(ctx context.Context) ([]string, error) {
	var bad []string
	fail := func(f string, v ...any) { bad = append(bad, fmt.Sprintf(f, v...)) }
	rows, err := a.DB.Query(ctx, `SELECT j.id, count(p.id), COALESCE(sum(p.amount), 0),
			count(*) FILTER (WHERE w.currency <> j.currency OR w.branch_id <> j.branch_id)
		FROM wallet_journals j LEFT JOIN wallet_postings p ON p.journal_id = j.id LEFT JOIN wallet_accounts w ON w.id = p.account_id
		GROUP BY j.id HAVING count(p.id) < 2 OR COALESCE(sum(p.amount), 0) <> 0
			OR count(*) FILTER (WHERE w.currency <> j.currency OR w.branch_id <> j.branch_id) > 0`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, n, sum, other int64
		if err := rows.Scan(&id, &n, &sum, &other); err != nil {
			rows.Close()
			return nil, err
		}
		fail("W1 journal %d: %d postings, sum %d, %d of another store or currency", id, n, sum, other)
	}
	rows.Close()
	rows, err = a.DB.Query(ctx, `SELECT w.id, w.kind, w.balance, COALESCE(sum(p.amount), 0),
			COALESCE((SELECT p2.balance_after FROM wallet_postings p2 WHERE p2.account_id = w.id ORDER BY p2.id DESC LIMIT 1), 0)
		FROM wallet_accounts w LEFT JOIN wallet_postings p ON p.account_id = w.id
		GROUP BY w.id HAVING w.balance <> COALESCE(sum(p.amount), 0) OR (w.kind = 'store_credit' AND w.balance < 0)
			OR w.balance <> COALESCE((SELECT p2.balance_after FROM wallet_postings p2 WHERE p2.account_id = w.id ORDER BY p2.id DESC LIMIT 1), 0)`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, bal, sum, last int64
		var kind string
		if err := rows.Scan(&id, &kind, &bal, &sum, &last); err != nil {
			rows.Close()
			return nil, err
		}
		fail("W2 account %d (%s): balance %d, postings add up to %d, last posting left %d", id, kind, bal, sum, last)
	}
	rows.Close()
	rows, err = a.DB.Query(ctx, `SELECT branch_id, currency,
			COALESCE(sum(balance) FILTER (WHERE kind = 'store_credit'), 0),
			COALESCE(sum(balance) FILTER (WHERE kind <> 'store_credit'), 0)
		FROM wallet_accounts GROUP BY branch_id, currency
		HAVING COALESCE(sum(balance), 0) <> 0`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var branch, cust, store int64
		var cur string
		if err := rows.Scan(&branch, &cur, &cust, &store); err != nil {
			rows.Close()
			return nil, err
		}
		fail("W1 store %d (%s): customers hold %d, the store's side is %d", branch, cur, cust, store)
	}
	rows.Close()
	return bad, rows.Err()
}

const walletCheckInterval = 24 * time.Hour

// WalletCheckDue runs CheckWallet once a day (whichever worker gets there
// first) and keeps what it found for the platform's monitor page (§10.1).
// It returns how many problems it found.
func (a *App) WalletCheckDue(ctx context.Context) (int, error) {
	var due bool
	if err := a.DB.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM wallet_check_runs
		WHERE ran_at > now() - make_interval(secs => $1))`, (walletCheckInterval - time.Hour).Seconds()).Scan(&due); err != nil || !due {
		return 0, err
	}
	bad, err := a.CheckWallet(ctx)
	if err != nil {
		return 0, err
	}
	if bad == nil {
		bad = []string{}
	}
	if len(bad) > 0 {
		log.Printf("wallet check: %d problems: %v", len(bad), bad)
	}
	_, err = a.DB.Exec(ctx, `INSERT INTO wallet_check_runs (problems) VALUES ($1)`, bad)
	return len(bad), err
}

// WalletCheckRun is the last nightly check.
type WalletCheckRun struct {
	RanAt    time.Time `json:"ran_at"`
	Problems []string  `json:"problems"`
}

// LastWalletCheck is the latest nightly check, nil before the first.
func (a *App) LastWalletCheck(ctx context.Context) (*WalletCheckRun, error) {
	var r WalletCheckRun
	err := a.DB.QueryRow(ctx, `SELECT ran_at, problems FROM wallet_check_runs ORDER BY id DESC LIMIT 1`).Scan(&r.RanAt, &r.Problems)
	if isNoRows(err) {
		return nil, nil
	}
	return &r, err
}
