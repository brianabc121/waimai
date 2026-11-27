package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The store balance rules of docs/REFUND_BALANCE_DESIGN.md §11 (W1–W7),
// checked by make pricing-check, cmd/pricecheck and every night (§10.1).

// checkOrderCredit checks W4–W7 on one order; an error reading is reported
// as a broken rule, so that nothing is skipped silently.
func checkOrderCredit(ctx context.Context, q Querier, orderID int64) []string {
	var bad []string
	fail := func(f string, v ...any) { bad = append(bad, fmt.Sprintf(f, v...)) }
	var total, paid, spent, refunded, cashPart, cashOut, toCredit, posted, branch, acctBranch int64
	var sp, sb, spentP, spentB, partP, partB, postedP, postedB int64
	var status, currency, acctCurrency string
	var others int
	err := q.QueryRow(ctx, `SELECT o.total, o.credit_paid, o.stored_principal_paid, o.stored_bonus_paid,
		-COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			JOIN wallet_accounts w ON w.id = p.account_id
			WHERE j.order_id = o.id AND j.kind IN ('stored_spend', 'stored_return') AND w.kind = 'stored_principal'), 0),
		-COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			JOIN wallet_accounts w ON w.id = p.account_id
			WHERE j.order_id = o.id AND j.kind IN ('stored_spend', 'stored_return') AND w.kind = 'stored_bonus'), 0),
		COALESCE((SELECT sum(r.principal_part) FROM order_refunds r WHERE r.order_id = o.id), 0),
		COALESCE((SELECT sum(r.bonus_part) FROM order_refunds r WHERE r.order_id = o.id), 0),
		COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			JOIN wallet_accounts w ON w.id = p.account_id
			WHERE j.order_id = o.id AND j.kind = 'stored_refund' AND w.kind = 'stored_principal'), 0),
		COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
			JOIN wallet_accounts w ON w.id = p.account_id
			WHERE j.order_id = o.id AND j.kind = 'stored_refund' AND w.kind = 'stored_bonus'), 0),
		o.acceptance_status, o.branch_id, o.currency,
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
			WHERE j.order_id = o.id AND (w.branch_id <> o.branch_id OR w.currency <> o.currency))
		FROM orders o WHERE o.id = $1`, orderID).Scan(&total, &paid, &sp, &sb, &spentP, &spentB, &partP, &partB, &postedP, &postedB,
		&status, &branch, &currency, &spent, &refunded,
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
	if spent != paid-sp-sb {
		fail("W4 the books took %d from the refund balance, the order says %d", spent, paid-sp-sb)
	}
	// W10/W11 stored value is part of what balances paid, and the books agree
	if sp < 0 || sb < 0 || sp+sb > paid {
		fail("W10 stored value paid %d + %d, balances %d", sp, sb, paid)
	}
	if spentP != sp || spentB != sb {
		fail("W11 the books took %d + %d from stored value, the order says %d + %d", spentP, spentB, sp, sb)
	}
	// W12/W13 refunds give stored value back to where it came from, never more
	if partP > sp || partB > sb {
		fail("W12 refunds gave back %d + %d of stored value, it paid %d + %d", partP, partB, sp, sb)
	}
	if refunded == total && total > 0 && (partP != sp || partB != sb) {
		fail("W12 refunded in full but stored value got back %d + %d of %d + %d", partP, partB, sp, sb)
	}
	if postedP != partP || postedB != partB {
		fail("W13 refunds say %d + %d went back to stored value, the books %d + %d", partP, partB, postedP, postedB)
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
	if posted+postedP+postedB != toCredit {
		fail("W7 refunds say %d went into balances, the books %d", toCredit, posted+postedP+postedB)
	}
	return bad
}

// CheckWallet checks the whole books (docs/BALANCE_CHECK.md §3): every
// journal balances, stays at one store and currency and posts only to the
// accounts its kind may (W1); every balance is the sum of its postings, its
// postings are numbered without a gap and none of a customer's is below
// zero (W2); each store's side mirrors its customers' (W15); every
// account's hash chain is whole (W18); refunds and paybacks are booked to
// the right store accounts (W16, W14); top-ups and paybacks agree with the
// books (W8, W14); the closed days add up (W19); and every order that
// touched a balance keeps W3–W13.
func (a *App) CheckWallet(ctx context.Context) ([]string, error) {
	return a.checkWalletSince(ctx, 0, 0)
}

// rowsEach runs a query and calls fn for every row.
func rowsEach(ctx context.Context, q Querier, sql string, args []any, fn func(pgx.Rows) error) error {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// checkWalletSince checks what was written after journal sinceJ and posting
// sinceP; 0 and 0 is the whole books.
func (a *App) checkWalletSince(ctx context.Context, sinceJ, sinceP int64) ([]string, error) {
	var bad []string
	fail := func(f string, v ...any) { bad = append(bad, fmt.Sprintf(f, v...)) }
	full := sinceJ == 0 && sinceP == 0
	// W1
	if err := rowsEach(ctx, a.DB, `SELECT j.id, count(p.id), COALESCE(sum(p.amount), 0),
			count(p.id) FILTER (WHERE w.currency <> j.currency OR w.branch_id IS DISTINCT FROM j.branch_id),
			count(p.id) FILTER (WHERE NOT EXISTS (SELECT 1 FROM wallet_journal_rules r
				WHERE r.journal_kind = j.kind AND r.account_kind = w.kind))
		FROM wallet_journals j LEFT JOIN wallet_postings p ON p.journal_id = j.id LEFT JOIN wallet_accounts w ON w.id = p.account_id
		WHERE j.id > $1 GROUP BY j.id
		HAVING count(p.id) < 2 OR COALESCE(sum(p.amount), 0) <> 0
			OR count(p.id) FILTER (WHERE w.currency <> j.currency OR w.branch_id IS DISTINCT FROM j.branch_id) > 0
			OR count(p.id) FILTER (WHERE NOT EXISTS (SELECT 1 FROM wallet_journal_rules r
				WHERE r.journal_kind = j.kind AND r.account_kind = w.kind)) > 0`, []any{sinceJ}, func(r pgx.Rows) error {
		var id, n, sum, other, rule int64
		if err := r.Scan(&id, &n, &sum, &other, &rule); err != nil {
			return err
		}
		fail("W1 journal %d: %d postings, sum %d, %d of another store or currency, %d on an account its kind may not touch",
			id, n, sum, other, rule)
		return nil
	}); err != nil {
		return nil, err
	}
	// W2
	if err := rowsEach(ctx, a.DB, `SELECT w.id, w.kind, w.balance, COALESCE(sum(p.amount), 0), w.posting_seq, count(p.id)
		FROM wallet_accounts w LEFT JOIN wallet_postings p ON p.account_id = w.id
		WHERE $1 = 0 OR EXISTS (SELECT 1 FROM wallet_postings x WHERE x.account_id = w.id AND x.id > $1)
		GROUP BY w.id HAVING w.balance <> COALESCE(sum(p.amount), 0) OR (w.customer_id IS NOT NULL AND w.balance < 0)
			OR w.posting_seq <> count(p.id)`, []any{sinceP}, func(r pgx.Rows) error {
		var id, bal, sum, seq, n int64
		var kind string
		if err := r.Scan(&id, &kind, &bal, &sum, &seq, &n); err != nil {
			return err
		}
		fail("W2 account %d (%s): balance %d, postings add up to %d, numbered %d of %d", id, kind, bal, sum, seq, n)
		return nil
	}); err != nil {
		return nil, err
	}
	// W15 (the platform's accounts are the group without a store)
	if err := rowsEach(ctx, a.DB, `SELECT COALESCE(branch_id, 0), currency,
			COALESCE(sum(balance) FILTER (WHERE customer_id IS NOT NULL), 0),
			COALESCE(sum(balance) FILTER (WHERE customer_id IS NULL), 0)
		FROM wallet_accounts GROUP BY branch_id, currency HAVING COALESCE(sum(balance), 0) <> 0`, nil, func(r pgx.Rows) error {
		var branch, cust, store int64
		var cur string
		if err := r.Scan(&branch, &cur, &cust, &store); err != nil {
			return err
		}
		fail("W15 store %d (%s): customers hold %d, the other side is %d", branch, cur, cust, store)
		return nil
	}); err != nil {
		return nil, err
	}
	// W18
	if err := rowsEach(ctx, a.DB, `SELECT b_account, b_seq, b_problem FROM wallet_chain_breaks($1)`, []any{sinceP},
		func(r pgx.Rows) error {
			var acct, seq int64
			var problem string
			if err := r.Scan(&acct, &seq, &problem); err != nil {
				return err
			}
			fail("W18 account %d posting %d: %s", acct, seq, problem)
			return nil
		}); err != nil {
		return nil, err
	}
	// W16 a refund's balance share reverses balance sales; only cash the
	// customer agreed to keep as a balance is new money (store_refund)
	if err := rowsEach(ctx, a.DB, `SELECT r.id, r.amount - r.cash_part - r.principal_part - r.bonus_part,
			GREATEST(r.to_credit - (r.amount - r.cash_part), 0),
			-COALESCE(sum(p.amount) FILTER (WHERE w.kind = 'store_sales'), 0),
			-COALESCE(sum(p.amount) FILTER (WHERE w.kind = 'store_refund'), 0)
		FROM order_refunds r JOIN wallet_journals j ON j.refund_id = r.id
		JOIN wallet_postings p ON p.journal_id = j.id JOIN wallet_accounts w ON w.id = p.account_id
		WHERE j.kind IN ('refund', 'revoke', 'reclass')
			AND ($1 = 0 OR r.id IN (SELECT x.refund_id FROM wallet_journals x WHERE x.id > $1 AND x.refund_id IS NOT NULL))
		GROUP BY r.id
		HAVING -COALESCE(sum(p.amount) FILTER (WHERE w.kind = 'store_sales'), 0) <> r.amount - r.cash_part - r.principal_part - r.bonus_part
			OR -COALESCE(sum(p.amount) FILTER (WHERE w.kind = 'store_refund'), 0) <> GREATEST(r.to_credit - (r.amount - r.cash_part), 0)`,
		[]any{sinceJ}, func(r pgx.Rows) error {
			var id, share, cash, sales, refund int64
			if err := r.Scan(&id, &share, &cash, &sales, &refund); err != nil {
				return err
			}
			fail("W16 refund %d: balance share %d reversed %d of balance sales; cash kept as a balance %d, booked %d",
				id, share, sales, cash, refund)
			return nil
		}); err != nil {
		return nil, err
	}
	// W3–W13 on every order the checked journals touched
	var orders []int64
	if err := rowsEach(ctx, a.DB, `SELECT DISTINCT order_id FROM wallet_journals WHERE id > $1 AND order_id IS NOT NULL ORDER BY 1`,
		[]any{sinceJ}, func(r pgx.Rows) error {
			var id int64
			orders = append(orders, id)
			return r.Scan(&orders[len(orders)-1])
		}); err != nil {
		return nil, err
	}
	for _, id := range orders {
		for _, p := range checkOrderCredit(ctx, a.DB, id) {
			fail("order %d: %s", id, p)
		}
	}
	if !full {
		return bad, nil
	}
	more, err := checkStored(ctx, a.DB)
	if err != nil {
		return nil, err
	}
	bad = append(bad, more...)
	more, err = checkWalletDays(ctx, a.DB)
	return append(bad, more...), err
}

// checkWalletDays checks the closed days (W19): on each store's last closed
// day its accounts add up to zero in every currency, the closing balance
// plus what was posted after it is the balance now, and every day opens
// with the day before's closing.
func checkWalletDays(ctx context.Context, q Querier) ([]string, error) {
	var bad []string
	fail := func(f string, v ...any) { bad = append(bad, fmt.Sprintf(f, v...)) }
	err := rowsEach(ctx, q, `SELECT c.scope_id, c.day, w.currency, sum(d.closing)
		FROM wallet_days_closed c JOIN wallet_accounts w ON COALESCE(w.branch_id, 0) = c.scope_id
		JOIN wallet_daily_balances d ON d.account_id = w.id AND d.day = c.day
		GROUP BY c.scope_id, c.day, w.currency HAVING sum(d.closing) <> 0`, nil, func(r pgx.Rows) error {
		var scope, sum int64
		var day time.Time
		var cur string
		if err := r.Scan(&scope, &day, &cur, &sum); err != nil {
			return err
		}
		fail("W19 store %d (%s) closed %s adding up to %d", scope, cur, day.Format("2006-01-02"), sum)
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = rowsEach(ctx, q, `SELECT w.id, c.day, w.balance, COALESCE(d.closing, 0) + COALESCE((SELECT sum(p.amount)
			FROM wallet_postings p JOIN wallet_journals j ON j.id = p.journal_id
			WHERE p.account_id = w.id AND (j.created_at AT TIME ZONE COALESCE(b.timezone, 'UTC'))::date > c.day), 0)
		FROM wallet_accounts w JOIN wallet_days_closed c ON c.scope_id = COALESCE(w.branch_id, 0)
		LEFT JOIN branches b ON b.id = w.branch_id
		LEFT JOIN wallet_daily_balances d ON d.account_id = w.id AND d.day = c.day`, nil, func(r pgx.Rows) error {
		var id, bal, want int64
		var day time.Time
		if err := r.Scan(&id, &day, &bal, &want); err != nil {
			return err
		}
		if bal != want {
			fail("W19 account %d: closed %s, closing plus what came after is %d, the balance is %d", id, day.Format("2006-01-02"), want, bal)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = rowsEach(ctx, q, `SELECT account_id, day, opening, prev FROM (SELECT account_id, day, opening,
			lag(closing) OVER (PARTITION BY account_id ORDER BY day) AS prev FROM wallet_daily_balances) x
		WHERE opening <> COALESCE(prev, 0)`, nil, func(r pgx.Rows) error {
		var id, open, prev int64
		var day time.Time
		if err := r.Scan(&id, &day, &open, &prev); err != nil {
			return err
		}
		fail("W19 account %d opened %s with %d, the day before closed with %d", id, day.Format("2006-01-02"), open, prev)
		return nil
	})
	return bad, err
}

// walletDigest fingerprints the books: every account's last hash. Written
// down somewhere else each day, it shows later that nothing before was
// rewritten (docs/BALANCE_CHECK.md §6).
func walletDigest(ctx context.Context, q Querier) (string, error) {
	var d string
	err := q.QueryRow(ctx, `SELECT encode(sha256(convert_to(COALESCE(string_agg(id || ':' || posting_seq || ':' ||
		COALESCE(encode(last_hash, 'hex'), ''), ',' ORDER BY id), ''), 'UTF8')), 'hex') FROM wallet_accounts`).Scan(&d)
	return d, err
}

// walletWarnings are what is not wrong in the books but leaves them less
// safe: the app connected as a superuser or as the books' owner, which can
// switch the database's protections off.
func walletWarnings(ctx context.Context, q Querier) ([]string, error) {
	var super, owner bool
	if err := q.QueryRow(ctx, `SELECT r.rolsuper, pg_has_role(current_user, t.tableowner, 'MEMBER')
		FROM pg_roles r, pg_tables t WHERE r.rolname = current_user AND t.schemaname = 'public' AND t.tablename = 'wallet_postings'`).
		Scan(&super, &owner); err != nil {
		return nil, err
	}
	if super || owner {
		return []string{"APP_ROLE the app connects to the database as a superuser or as the owner of the books: connect as waimai_app (docs/DEPLOY.md)"}, nil
	}
	return []string{}, nil
}

// walletCheckInterval is how often the books are checked: what was written
// since the last check every hour, everything once a day.
const (
	walletCheckInterval = time.Hour
	walletFullInterval  = 24 * time.Hour
	// a day is closed this long after it ends, for transactions that began
	// before midnight to commit
	walletDayGrace = 15 * time.Minute
)

// CloseWalletDays writes the daily balances of every day that has ended
// and is not closed yet, store by store (the platform as store 0).
func (a *App) CloseWalletDays(ctx context.Context) (int, error) {
	type scope struct {
		id   int64
		tz   string
		last *time.Time
		from *time.Time
	}
	var scopes []scope
	err := rowsEach(ctx, a.DB, `SELECT s.id, s.tz, c.day,
			(SELECT min((j.created_at AT TIME ZONE s.tz)::date) FROM wallet_journals j WHERE COALESCE(j.branch_id, 0) = s.id)
		FROM (SELECT DISTINCT COALESCE(w.branch_id, 0) AS id, COALESCE(b.timezone, 'UTC') AS tz
			FROM wallet_accounts w LEFT JOIN branches b ON b.id = w.branch_id) s
		LEFT JOIN wallet_days_closed c ON c.scope_id = s.id ORDER BY s.id`, nil, func(r pgx.Rows) error {
		var sc scope
		if err := r.Scan(&sc.id, &sc.tz, &sc.last, &sc.from); err != nil {
			return err
		}
		scopes = append(scopes, sc)
		return nil
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, sc := range scopes {
		loc, err := time.LoadLocation(sc.tz)
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().Add(-walletDayGrace).In(loc)
		lastEnded := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		var day time.Time
		switch {
		case sc.last != nil:
			day = sc.last.AddDate(0, 0, 1)
		case sc.from != nil:
			day = *sc.from
		default:
			continue
		}
		for ; !day.After(lastEnded); day = day.AddDate(0, 0, 1) {
			var rows int
			if err := a.DB.QueryRow(ctx, `SELECT wallet_close_day($1, $2)`, sc.id, day).Scan(&rows); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// WalletCheckDue checks the books every hour (whichever worker gets there
// first): what was written since the last check, and once a day
// everything. It closes the days that ended first, keeps what it found for
// the platform's monitor page and raises the alarm on a problem. It returns
// how many problems it found.
func (a *App) WalletCheckDue(ctx context.Context) (int, error) {
	var due, fullDue bool
	var lastJ, lastP int64
	if err := a.DB.QueryRow(ctx, `SELECT
			NOT EXISTS (SELECT 1 FROM wallet_check_runs WHERE ran_at > now() - make_interval(secs => $1)),
			NOT EXISTS (SELECT 1 FROM wallet_check_runs WHERE full_check AND ran_at > now() - make_interval(secs => $2)),
			COALESCE((SELECT last_journal_id FROM wallet_check_runs ORDER BY id DESC LIMIT 1), 0),
			COALESCE((SELECT last_posting_id FROM wallet_check_runs ORDER BY id DESC LIMIT 1), 0)`,
		(walletCheckInterval-5*time.Minute).Seconds(), (walletFullInterval-time.Hour).Seconds()).
		Scan(&due, &fullDue, &lastJ, &lastP); err != nil || !due {
		return 0, err
	}
	if _, err := a.CloseWalletDays(ctx); err != nil {
		return 0, err
	}
	var upJ, upP int64
	if err := a.DB.QueryRow(ctx, `SELECT COALESCE((SELECT max(id) FROM wallet_journals), 0),
		COALESCE((SELECT max(id) FROM wallet_postings), 0)`).Scan(&upJ, &upP); err != nil {
		return 0, err
	}
	sinceJ, sinceP := lastJ, lastP
	if fullDue {
		sinceJ, sinceP = 0, 0
	}
	bad, err := a.checkWalletSince(ctx, sinceJ, sinceP)
	if err != nil {
		return 0, err
	}
	if bad == nil {
		bad = []string{}
	}
	warn, err := walletWarnings(ctx, a.DB)
	if err != nil {
		return 0, err
	}
	digest, err := walletDigest(ctx, a.DB)
	if err != nil {
		return 0, err
	}
	if len(bad) > 0 {
		log.Printf("wallet check: %d problems: %v", len(bad), bad)
	}
	var runID int64
	if err := a.DB.QueryRow(ctx, `INSERT INTO wallet_check_runs (problems, full_check, last_journal_id, last_posting_id, digest, warnings)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, bad, fullDue, upJ, upP, digest, warn).Scan(&runID); err != nil {
		return 0, err
	}
	if len(bad) > 0 {
		a.walletAlarm(ctx, runID, bad)
	}
	return len(bad), nil
}

// walletAlarm tells the platform that the books have a problem: a JSON POST
// to WALLET_ALERT_WEBHOOK (at most once every 6 hours while the problem
// lasts) and the red banner on every platform page (LastWalletCheck).
func (a *App) walletAlarm(ctx context.Context, runID int64, bad []string) {
	url := a.walletAlertURL()
	if url == "" {
		return
	}
	var recent bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wallet_check_runs WHERE alerted_at > now() - interval '6 hours')`).
		Scan(&recent); err != nil || recent {
		return
	}
	shown := bad
	if len(shown) > 10 {
		shown = shown[:10]
	}
	text := fmt.Sprintf("河马点餐：余额账本核对发现 %d 个问题（第 %d 次核对）\n%s", len(bad), runID, strings.Join(shown, "\n"))
	body, _ := json.Marshal(map[string]any{"text": text, "content": text, "problems": bad, "run": runID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("wallet alarm: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		log.Printf("wallet alarm: %v", err)
		return
	}
	res.Body.Close()
	if res.StatusCode >= 300 {
		log.Printf("wallet alarm: webhook answered %d", res.StatusCode)
		return
	}
	if _, err := a.DB.Exec(ctx, `UPDATE wallet_check_runs SET alerted_at = now() WHERE id=$1`, runID); err != nil {
		log.Printf("wallet alarm: %v", err)
	}
}

func (a *App) walletAlertURL() string {
	a.mu.RLock()
	cfg := a.effective
	a.mu.RUnlock()
	if cfg != nil && cfg.WalletAlertWebhook != "" {
		return cfg.WalletAlertWebhook
	}
	return os.Getenv("WALLET_ALERT_WEBHOOK")
}

// WalletCheckRun is a check of the books.
type WalletCheckRun struct {
	RanAt    time.Time `json:"ran_at"`
	Full     bool      `json:"full"`
	Problems []string  `json:"problems"`
	Warnings []string  `json:"warnings"`
	Digest   string    `json:"digest"`
	// LastFull is when the whole books were last checked.
	LastFull *time.Time `json:"last_full"`
}

// LastWalletCheck is the latest check, nil before the first. A problem
// stays shown until a full check finds none.
func (a *App) LastWalletCheck(ctx context.Context) (*WalletCheckRun, error) {
	var r WalletCheckRun
	err := a.DB.QueryRow(ctx, `SELECT ran_at, full_check, problems, warnings, digest,
			(SELECT max(ran_at) FROM wallet_check_runs WHERE full_check)
		FROM wallet_check_runs ORDER BY id DESC LIMIT 1`).Scan(&r.RanAt, &r.Full, &r.Problems, &r.Warnings, &r.Digest, &r.LastFull)
	if isNoRows(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if len(r.Problems) == 0 && !r.Full {
		// an hourly check sees only what is new: problems found before stay
		// until a full check clears them
		var earlier []string
		err := a.DB.QueryRow(ctx, `SELECT problems FROM wallet_check_runs WHERE jsonb_array_length(problems) > 0
			AND id > COALESCE((SELECT max(id) FROM wallet_check_runs WHERE full_check AND jsonb_array_length(problems) = 0), 0)
			ORDER BY id DESC LIMIT 1`).Scan(&earlier)
		if err != nil && !isNoRows(err) {
			return nil, err
		}
		r.Problems = earlier
	}
	if r.Problems == nil {
		r.Problems = []string{}
	}
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	return &r, nil
}

// checkStored checks the top-ups and paybacks against the books (rules W8
// and W14, docs/STORED_VALUE_DESIGN.md §11).
func checkStored(ctx context.Context, q Querier) ([]string, error) {
	var bad []string
	rows, err := q.Query(ctx, `SELECT t.id, t.status, t.pay, t.bonus,
			COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
				JOIN wallet_accounts w ON w.id = p.account_id WHERE j.topup_id = t.id AND w.kind = 'stored_principal' AND j.kind = 'topup'), 0),
			COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
				JOIN wallet_accounts w ON w.id = p.account_id WHERE j.topup_id = t.id AND w.kind = 'stored_bonus' AND j.kind = 'topup'), 0),
			COALESCE((SELECT sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
				JOIN wallet_accounts w ON w.id = p.account_id WHERE j.topup_id = t.id AND w.customer_id IS NOT NULL), 0)
		FROM topups t`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, pay, bonus, inP, inB, net int64
		var status string
		if err := rows.Scan(&id, &status, &pay, &bonus, &inP, &inB, &net); err != nil {
			rows.Close()
			return nil, err
		}
		booked := status == "paid" || status == "revoked"
		switch {
		case booked && (inP != pay || inB != bonus):
			bad = append(bad, fmt.Sprintf("W8 top-up %d (%s): booked %d + %d, it was %d + %d", id, status, inP, inB, pay, bonus))
		case !booked && (inP != 0 || inB != 0):
			bad = append(bad, fmt.Sprintf("W8 top-up %d (%s) is in the books", id, status))
		case status == "revoked" && net != 0:
			bad = append(bad, fmt.Sprintf("W8 revoked top-up %d still holds %d", id, net))
		case status == "paid" && net != pay+bonus:
			bad = append(bad, fmt.Sprintf("W8 top-up %d: the books hold %d, it was %d", id, net, pay+bonus))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `SELECT s.id, s.reason, s.principal, s.bonus, s.clawback, s.paid_out,
			COALESCE((SELECT -sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
				JOIN wallet_accounts w ON w.id = p.account_id WHERE j.cashout_id = s.id AND w.kind = 'stored_principal'), 0),
			COALESCE((SELECT -sum(p.amount) FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id
				JOIN wallet_accounts w ON w.id = p.account_id WHERE j.cashout_id = s.id AND w.kind = 'stored_bonus'), 0)
		FROM stored_cashouts s`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, p, b, claw, out, outP, outB int64
		var reason string
		if err := rows.Scan(&id, &reason, &p, &b, &claw, &out, &outP, &outB); err != nil {
			return nil, err
		}
		if outP != p || outB != b {
			bad = append(bad, fmt.Sprintf("W14 payback %d: took %d + %d from the books, it says %d + %d", id, outP, outB, p, b))
		}
		if out+claw != p || (reason == "store" && claw != 0) {
			bad = append(bad, fmt.Sprintf("W14 payback %d (%s): paid %d, kept %d, of %d", id, reason, out, claw, p))
		}
	}
	return bad, rows.Err()
}
