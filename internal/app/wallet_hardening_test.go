package app_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// The hardening of the books (migration 00085, docs/BALANCE_REVIEW.md,
// rules W16–W20 of docs/BALANCE_CHECK.md).

// storeAcct is the balance of one of a store's own accounts (0: none yet).
func storeAcct(t *testing.T, branchID int64, kind string) int64 {
	t.Helper()
	var bal int64
	noErr(t, testApp.DB.QueryRow(context.Background(), `SELECT COALESCE(sum(balance), 0) FROM wallet_accounts
		WHERE branch_id=$1 AND kind=$2 AND customer_id IS NULL`, branchID, kind).Scan(&bal))
	return bal
}

func acctID(t *testing.T, branchID int64, kind string, customerID int64) int64 {
	t.Helper()
	var id int64
	noErr(t, testApp.DB.QueryRow(context.Background(), `SELECT id FROM wallet_accounts
		WHERE COALESCE(branch_id, 0)=$1 AND kind=$2 AND COALESCE(customer_id, 0)=$3`, branchID, kind, customerID).Scan(&id))
	return id
}

func wantDBError(t *testing.T, err error, part string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), part) {
		t.Fatalf("want an error with %q, got %v", part, err)
	}
}

// H2, M1: the books have one way in, and it checks where money goes.
func TestBooksGuards(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp
	cust, _ := customer(t)
	s.topupPaid(t, cust, s.first, "cash") // 300 + 30
	other := newStoredStore(t)
	c2, _ := customer(t)
	other.topupPaid(t, c2, other.first, "cash")
	prin := acctID(t, s.branchID, "stored_principal", cust)
	topup := acctID(t, s.branchID, "store_topup", 0)
	otherTopup := acctID(t, other.branchID, "store_topup", 0)
	bonusFund := acctID(t, s.branchID, "store_bonus", 0)

	// a session setting opened the door before 00085
	_, err := a.DB.Exec(ctx, `DO $$ BEGIN PERFORM set_config('waimai.wallet_posting', 'on', true);
		UPDATE wallet_accounts SET balance = balance + 100000 WHERE id = `+strconv.FormatInt(prin, 10)+`; END $$`)
	wantDBError(t, err, "only changes by a posting")

	post := func(kind, idem string, branch int64, accts, amts []int64) error {
		_, err := a.DB.Exec(ctx, `SELECT wallet_post_journal($1, $2, $3, 'PHP', NULL, NULL, NULL, NULL, 'system', NULL, NULL, '', NULL, $4, $5)`,
			kind, idem, branch, accts, amts)
		return err
	}
	// another store's account in a journal
	wantDBError(t, post("topup", uniq("x"), s.branchID, []int64{prin, otherTopup}, []int64{1, -1}), "another store")
	// an account the kind of journal may not touch: spending into the bonus fund
	wantDBError(t, post("stored_spend", uniq("x"), s.branchID, []int64{prin, bonusFund}, []int64{-1, 1}), "may not post")
	// not balanced
	wantDBError(t, post("topup", uniq("x"), s.branchID, []int64{prin, topup}, []int64{2, -1}), "does not balance")
	// below zero
	err = post("topup_revoke", uniq("x"), s.branchID, []int64{prin, topup}, []int64{-40000, 40000})
	wantDBError(t, err, "wallet_accounts_not_negative")
	// the old platform wallet table is closed
	_, err = a.DB.Exec(ctx, `INSERT INTO customer_wallet_entries (customer_id, account, currency, amount) VALUES ($1, 'balance', 'PHP', 100)`, cust)
	wantDBError(t, err, "closed")
	walletClean(t)

	// someone with the owner's rights switches the triggers off and edits a
	// posting: the hash chain and the sums find it (W18, W2)
	tamper(t, `UPDATE wallet_postings SET amount = amount + 1 WHERE account_id=$1 AND seq=1`, prin)
	bad, err := a.CheckWallet(ctx)
	noErr(t, err)
	if !hasPrefix(bad, "W18") || !hasPrefix(bad, "W2") {
		t.Fatalf("an edited posting was not found: %v", bad)
	}
	tamper(t, `UPDATE wallet_postings SET amount = amount - 1 WHERE account_id=$1 AND seq=1`, prin)
	walletClean(t)
}

func hasPrefix(l []string, p string) bool {
	for _, s := range l {
		if strings.HasPrefix(s, p) || strings.Contains(s, ": "+p) {
			return true
		}
	}
	return false
}

// H2: the app's own role reads the books and writes them only through
// wallet_post_journal.
func TestAppRoleCannotWriteBooks(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp
	cust, _ := customer(t)
	s.topupPaid(t, cust, s.first, "cash")
	prin := acctID(t, s.branchID, "stored_principal", cust)
	topup := acctID(t, s.branchID, "store_topup", 0)
	var exists bool
	noErr(t, a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='waimai_app')`).Scan(&exists))
	if !exists {
		t.Skip("the test database's user may not create roles")
	}
	try := func(sql string, args ...any) error {
		tx, err := a.DB.Begin(ctx)
		noErr(t, err)
		defer tx.Rollback(ctx) //nolint:errcheck
		_, err = tx.Exec(ctx, `SET LOCAL ROLE waimai_app`)
		noErr(t, err)
		_, err = tx.Exec(ctx, sql, args...)
		return err
	}
	wantDBError(t, try(`UPDATE wallet_accounts SET balance = balance + 1 WHERE id=$1`, prin), "permission denied")
	wantDBError(t, try(`INSERT INTO wallet_postings (journal_id, account_id, amount) SELECT max(id), $1, 1 FROM wallet_journals`, prin), "permission denied")
	wantDBError(t, try(`DELETE FROM wallet_journals WHERE id = 0`), "permission denied")
	wantDBError(t, try(`INSERT INTO wallet_daily_balances (account_id, day, opening, credits, debits, closing, postings)
		VALUES ($1, '2000-01-01', 0, 0, 0, 0, 0)`, prin), "permission denied")
	noErr(t, try(`UPDATE wallet_accounts SET frozen = true WHERE id=$1`, prin))
	noErr(t, try(`SELECT 1 FROM wallet_accounts WHERE id=$1 FOR UPDATE`, prin))
	noErr(t, try(`SELECT wallet_post_journal('topup_revoke', $3, $1, 'PHP', NULL, NULL, NULL, NULL, 'system', NULL, NULL, '', NULL,
		ARRAY[$2::bigint, $4::bigint], ARRAY[-1::bigint, 1::bigint])`, s.branchID, prin, uniq("role"), topup))
	walletClean(t)
}

// H1: paying stored value back books where the money went; a retry with
// the same key pays back once (W14, W20).
func TestCashoutBooksWhereItWent(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)
	s.topupByCode(t, cust, s.big) // 1,000 + 200
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}, true, nil)
	noErr(t, err)
	checked(t, "spent", o.ID)
	in := app.CashoutInput{Reason: "customer", PayType: "cash", Consent: "in_person", IdemKey: uniq("cash")}
	q, err := a.StoredCashout(ctx, s.branchID, cust, in, s.owner)
	noErr(t, err)
	again, err := a.StoredCashout(ctx, s.branchID, cust, in, s.owner)
	noErr(t, err)
	if *again != *q {
		t.Fatalf("a retry answered %+v, first %+v", again, q)
	}
	var n int
	noErr(t, a.DB.QueryRow(ctx, `SELECT count(*) FROM stored_cashouts WHERE branch_id=$1`, s.branchID).Scan(&n))
	if n != 1 {
		t.Fatalf("%d paybacks", n)
	}
	if storeAcct(t, s.branchID, "store_cashout") != q.PaidOut || storeAcct(t, s.branchID, "store_clawback") != q.Clawback ||
		storeAcct(t, s.branchID, "store_bonus_void") != q.Bonus || q.Clawback == 0 {
		t.Fatalf("books: paid %d kept %d void %d, quote %+v", storeAcct(t, s.branchID, "store_cashout"),
			storeAcct(t, s.branchID, "store_clawback"), storeAcct(t, s.branchID, "store_bonus_void"), q)
	}
	// top-ups less what went back = what is still owed
	if -storeAcct(t, s.branchID, "store_topup") != 100000 || -storeAcct(t, s.branchID, "store_bonus") != 20000 {
		t.Fatalf("taken in: %d + %d", storeAcct(t, s.branchID, "store_topup"), storeAcct(t, s.branchID, "store_bonus"))
	}
	walletClean(t)
}

// M2: a refund's share that was paid from the refund balance reverses
// balance sales; only cash kept as a balance is new money (W16).
func TestRefundBalanceShareReverses(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	_, phone := customer(t)
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 3, OptionIDs: []int64{s.spiceMild}}}, false, nil)
	noErr(t, err)
	o = finishPickup(t, o, s.staff)
	chicken := itemByName(o, "chicken")
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(chicken.ID, 3), Reason: "x", Method: "现金", ToCredit: true,
		Consent: "in_person"}, ownerOf(t, s.branchID))
	noErr(t, err)
	in := -storeAcct(t, s.branchID, "store_refund")
	if in <= 0 {
		t.Fatalf("nothing went into the balance")
	}
	b, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}, true, nil)
	noErr(t, err)
	if b.CreditPaid != b.Totals.Total {
		t.Fatalf("paid from the balance: %+v", b)
	}
	b = finishPickup(t, b, s.staff)
	sales := storeAcct(t, s.branchID, "store_sales")
	rq, _, err := a.Refund(ctx, b.ID, app.RefundInput{Lines: refundLines(itemByName(b, "tea").ID, 1), Reason: "x", Method: "现金"}, ownerOf(t, s.branchID))
	noErr(t, err)
	if rq.CreditPart == 0 || storeAcct(t, s.branchID, "store_sales") != sales-rq.CreditPart ||
		-storeAcct(t, s.branchID, "store_refund") != in {
		t.Fatalf("refund %+v: sales %d → %d, put in %d → %d", rq, sales, storeAcct(t, s.branchID, "store_sales"),
			in, -storeAcct(t, s.branchID, "store_refund"))
	}
	checked(t, "refunded from the balance", b.ID)
	walletClean(t)
}

// H3: the platform's balance and rewards are in the books.
func TestPlatformWalletBooks(t *testing.T) {
	ctx, a := context.Background(), testApp
	cust, _ := customer(t)
	admin := app.AdminActor{ID: 1}
	key := uniq("adj")
	w, err := a.AdminAdjustWallet(ctx, admin, cust, app.WalletAdjust{Account: "balance", Currency: "PHP", Amount: 50000, Note: "补偿", IdemKey: key})
	noErr(t, err)
	again, err := a.AdminAdjustWallet(ctx, admin, cust, app.WalletAdjust{Account: "balance", Currency: "PHP", Amount: 50000, Note: "补偿", IdemKey: key})
	noErr(t, err)
	if len(w) != 1 || w[0].Balance != 50000 || len(again) != 1 || again[0].Balance != 50000 {
		t.Fatalf("adjusted: %+v, again %+v", w, again)
	}
	_, err = a.AdminAdjustWallet(ctx, admin, cust, app.WalletAdjust{Account: "bonus", Currency: "PHP", Amount: 3000, Note: "活动"})
	noErr(t, err)
	_, err = a.AdminAdjustWallet(ctx, admin, cust, app.WalletAdjust{Account: "balance", Currency: "PHP", Amount: -60000, Note: "x"})
	wantCode(t, err, "WALLET_SHORT")
	w, err = a.AdminAdjustWallet(ctx, admin, cust, app.WalletAdjust{Account: "balance", Currency: "PHP", Amount: -20000, Note: "退回"})
	noErr(t, err)
	if w[0].Balance != 30000 || w[0].Bonus != 3000 {
		t.Fatalf("wallet %+v", w)
	}
	es, err := a.WalletEntries(ctx, cust, "balance")
	noErr(t, err)
	if len(es) != 2 || es[0].Amount != -20000 || es[1].Amount != 50000 || es[1].Note != "补偿" {
		t.Fatalf("entries %+v", es)
	}
	p, err := a.CustomerProfile(ctx, cust)
	noErr(t, err)
	if len(p.Wallets) != 1 || p.Wallets[0].Balance != 30000 {
		t.Fatalf("profile wallets %+v", p.Wallets)
	}
	walletClean(t)
}

// M5: closed days add up, and the hourly check raises the alarm.
func TestWalletDaysAndAlarm(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp
	cust, _ := customer(t)
	s.topupPaid(t, cust, s.big, "cash")
	var tz string
	noErr(t, a.DB.QueryRow(ctx, `SELECT timezone FROM branches WHERE id=$1`, s.branchID).Scan(&tz))
	loc, err := time.LoadLocation(tz)
	noErr(t, err)
	day := time.Now().In(loc).Format("2006-01-02")
	var rows int
	noErr(t, a.DB.QueryRow(ctx, `SELECT wallet_close_day($1, $2::date)`, s.branchID, day).Scan(&rows))
	var closing, opening, credits int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT opening, credits, closing FROM wallet_daily_balances
		WHERE account_id=$1 AND day=$2::date`, acctID(t, s.branchID, "stored_principal", cust), day).Scan(&opening, &credits, &closing))
	if rows < 4 || opening != 0 || credits != 100000 || closing != 100000 {
		t.Fatalf("closed %d rows: %d + %d = %d", rows, opening, credits, closing)
	}
	// a day is closed once
	noErr(t, a.DB.QueryRow(ctx, `SELECT wallet_close_day($1, $2::date)`, s.branchID, day).Scan(&rows))
	if rows != 0 {
		t.Fatalf("closed again: %d", rows)
	}
	_, err = a.CloseWalletDays(ctx)
	noErr(t, err)
	walletClean(t)

	// the alarm: an edited posting found by the hourly check is posted to
	// the webhook once
	var mu sync.Mutex
	var got []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, string(b))
		mu.Unlock()
	}))
	defer hook.Close()
	t.Setenv("WALLET_ALERT_WEBHOOK", hook.URL)
	_, err = a.DB.Exec(ctx, `DELETE FROM wallet_check_runs`)
	noErr(t, err)
	_, err = a.WalletCheckDue(ctx) // a full check first: all clean
	noErr(t, err)
	prin := acctID(t, s.branchID, "stored_principal", cust)
	tamper(t, `UPDATE wallet_postings SET amount = amount + 1 WHERE account_id=$1 AND seq=1`, prin)
	defer tamper(t, `UPDATE wallet_postings SET amount = amount - 1 WHERE account_id=$1 AND seq=1`, prin)
	_, err = a.DB.Exec(ctx, `UPDATE wallet_check_runs SET ran_at = ran_at - interval '2 hours', last_posting_id = 0`)
	noErr(t, err)
	n, err := a.WalletCheckDue(ctx)
	noErr(t, err)
	last, err := a.LastWalletCheck(ctx)
	noErr(t, err)
	mu.Lock()
	defer mu.Unlock()
	if n == 0 || len(got) != 1 || !strings.Contains(got[0], "W18") || len(last.Problems) == 0 || last.Digest == "" {
		t.Fatalf("alarm: %d problems, webhook %v, last %+v", n, got, last)
	}
}

// M6: undoing a top-up voids the coupons that came with it, found by the
// top-up and not by the time; another top-up's coupons stay.
func TestTopupRevokeVoidsItsCoupons(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp
	cust, _ := customer(t)
	keep := s.topupByCode(t, cust, s.big)
	undo := s.topupByCode(t, cust, s.big)
	noErr(t, a.RevokeTopup(ctx, s.branchID, undo.ID, "收错了", s.owner))
	count := func(topup int64) (gave, live int) {
		noErr(t, a.DB.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status = 'unused') FROM customer_coupons
			WHERE topup_id=$1`, topup).Scan(&gave, &live))
		return
	}
	if g, l := count(undo.ID); g != 2 || l != 0 {
		t.Fatalf("the undone top-up's coupons: %d given, %d usable", g, l)
	}
	if g, l := count(keep.ID); g != 2 || l != 2 {
		t.Fatalf("the other top-up's coupons: %d given, %d usable", g, l)
	}
	walletClean(t)
}
