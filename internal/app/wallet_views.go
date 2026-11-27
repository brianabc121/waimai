package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// What customers, stores and the platform see of store balances
// (docs/REFUND_BALANCE_DESIGN.md §10), and the store taking a refund out of
// a balance again when it paid the customer in cash after all (§7.2).

// CreditEntry is one change to a customer's store balance.
type CreditEntry struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"` // refund, spend, return, revoke
	Amount    int64     `json:"amount"`
	Balance   int64     `json:"balance"`
	OrderID   int64     `json:"order_id"`
	PublicNo  string    `json:"public_no"`
	BrandName string    `json:"brand_name"`
	RefundID  int64     `json:"refund_id"`
	// store's view only
	CustomerID int64  `json:"customer_id,omitempty"`
	Phone      string `json:"phone,omitempty"`
	By         string `json:"by,omitempty"`
	Note       string `json:"note,omitempty"`
	// Revocable: a refund of cash into the balance the store may turn into
	// a cash refund again, while the balance still holds it.
	Revocable bool `json:"revocable,omitempty"`
}

const creditEntryCols = `j.id, j.created_at, j.kind, p.amount, p.balance_after, COALESCE(j.order_id, 0),
	COALESCE(o.public_no, ''), COALESCE(NULLIF(o.brand_name, ''), ''), COALESCE(j.refund_id, 0)`

func scanCreditEntry(rows pgx.Rows, extra ...any) (CreditEntry, error) {
	var e CreditEntry
	dest := append([]any{&e.ID, &e.At, &e.Kind, &e.Amount, &e.Balance, &e.OrderID, &e.PublicNo, &e.BrandName, &e.RefundID}, extra...)
	return e, rows.Scan(dest...)
}

// StoreCredit is a customer's balance at one store: the refund balance and
// stored value (docs/STORED_VALUE_DESIGN.md §3); Balance is all of it.
type StoreCredit struct {
	AccountID int64  `json:"account_id"`
	BranchID  int64  `json:"branch_id"`
	StoreName string `json:"store_name"`
	Slug      string `json:"slug"`
	Currency  string `json:"currency"`
	Balance   int64  `json:"balance"`
	Credit    int64  `json:"credit"`
	Principal int64  `json:"principal"`
	Bonus     int64  `json:"bonus"`
	Frozen    bool   `json:"frozen"`
	// Closed: the store no longer takes orders (§7.3).
	Closed bool `json:"closed"`
}

func storeCredits(ctx context.Context, q Querier, customerID int64, all bool) ([]StoreCredit, error) {
	rows, err := q.Query(ctx, `SELECT min(w.id), w.branch_id, b.name, b.slug, w.currency, sum(w.balance),
			COALESCE(sum(w.balance) FILTER (WHERE w.kind = 'store_credit'), 0),
			COALESCE(sum(w.balance) FILTER (WHERE w.kind = 'stored_principal'), 0),
			COALESCE(sum(w.balance) FILTER (WHERE w.kind = 'stored_bonus'), 0), bool_or(w.frozen),
			b.deleted_at IS NOT NULL OR EXISTS (SELECT 1 FROM merchants m WHERE m.id = b.merchant_id AND (m.deleted_at IS NOT NULL OR m.status = 'suspended'))
		FROM wallet_accounts w JOIN branches b ON b.id = w.branch_id
		WHERE w.kind IN ('store_credit', 'stored_principal', 'stored_bonus') AND w.customer_id=$1
		GROUP BY w.branch_id, b.id, w.currency HAVING $2 OR sum(w.balance) <> 0
		ORDER BY sum(w.balance) DESC, min(w.id)`, customerID, all)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (StoreCredit, error) {
		var s StoreCredit
		return s, r.Scan(&s.AccountID, &s.BranchID, &s.StoreName, &s.Slug, &s.Currency, &s.Balance, &s.Credit, &s.Principal,
			&s.Bonus, &s.Frozen, &s.Closed)
	})
	if out == nil {
		out = []StoreCredit{}
	}
	return out, err
}

// MyStoreCredits is the customer's balances at stores, largest first.
func (a *App) MyStoreCredits(ctx context.Context, customerID int64) ([]StoreCredit, error) {
	return storeCredits(ctx, a.DB, customerID, false)
}

// MyStoreCreditEntries is how the customer's balance at a store changed.
func (a *App) MyStoreCreditEntries(ctx context.Context, customerID, branchID int64) ([]CreditEntry, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+creditEntryCols+`
		FROM wallet_postings p JOIN wallet_accounts w ON w.id = p.account_id JOIN wallet_journals j ON j.id = p.journal_id
		LEFT JOIN orders o ON o.id = j.order_id
		WHERE w.kind='store_credit' AND w.customer_id=$1 AND w.branch_id=$2 ORDER BY p.id DESC LIMIT 200`, customerID, branchID)
	if err != nil {
		return nil, err
	}
	out := []CreditEntry{}
	defer rows.Close()
	for rows.Next() {
		e, err := scanCreditEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreditAlert is something the owner should look at (§9).
type CreditAlert struct {
	Kind     string `json:"kind"` // customer_often, quick_refund, staff_often
	Phone    string `json:"phone,omitempty"`
	By       string `json:"by,omitempty"`
	PublicNo string `json:"public_no,omitempty"`
	OrderID  int64  `json:"order_id,omitempty"`
	Count    int    `json:"count,omitempty"`
	Amount   int64  `json:"amount,omitempty"`
}

// BranchCredit is the store's page of balances (§10.2).
type BranchCredit struct {
	Currency string `json:"currency"`
	// Owed is what the store owes customers in balances now; Customers how
	// many customers hold some.
	Owed      int64 `json:"owed"`
	Customers int   `json:"customers"`
	// In the period: put into balances by refunds, paid from them (net of
	// what went back), taken out again.
	In      int64 `json:"in"`
	Used    int64 `json:"used"`
	Revoked int64 `json:"revoked"`
	// Holders are the customers with a balance, largest first.
	Holders []CreditHolder `json:"holders"`
	Entries []CreditEntry  `json:"entries"`
	Alerts  []CreditAlert  `json:"alerts"`
	// Limits are the owner's (§6.4).
	ApproveOver int64 `json:"approve_over"`
	DailyLimit  int64 `json:"daily_limit"`
}

// CreditHolder is one customer's balance at the store.
type CreditHolder struct {
	CustomerID int64     `json:"customer_id"`
	Phone      string    `json:"phone"`
	Balance    int64     `json:"balance"`
	Frozen     bool      `json:"frozen"`
	Since      time.Time `json:"since"`
}

// CreditFilter narrows the store's page.
type CreditFilter struct {
	From, To string
	Brands   []int64
	StaffID  int64
	Customer int64
}

// BranchCredit is the store's balances, and their changes in the period.
func (a *App) BranchCredit(ctx context.Context, branchID int64, f CreditFilter) (*BranchCredit, error) {
	if f.From == "" && f.To == "" {
		// the last 30 days, in the store's time
		var tz string
		if err := a.DB.QueryRow(ctx, `SELECT timezone FROM branches WHERE id=$1`, branchID).Scan(&tz); err != nil {
			if isNoRows(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		f.From, f.To = now.AddDate(0, 0, -29).Format("2006-01-02"), now.Format("2006-01-02")
	}
	if err := billDays(f.From, f.To); err != nil {
		return nil, err
	}
	out := &BranchCredit{Holders: []CreditHolder{}, Entries: []CreditEntry{}, Alerts: []CreditAlert{}}
	if err := a.DB.QueryRow(ctx, `SELECT currency, credit_approve_over, credit_daily_limit FROM branches WHERE id=$1`, branchID).
		Scan(&out.Currency, &out.ApproveOver, &out.DailyLimit); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT w.customer_id, c.phone, w.balance, w.frozen, w.created_at
		FROM wallet_accounts w JOIN customers c ON c.id = w.customer_id
		WHERE w.kind='store_credit' AND w.branch_id=$1 AND w.currency=$2 AND w.balance > 0 ORDER BY w.balance DESC, w.id LIMIT 500`,
		branchID, out.Currency)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h CreditHolder
		if err := rows.Scan(&h.CustomerID, &h.Phone, &h.Balance, &h.Frozen, &h.Since); err != nil {
			rows.Close()
			return nil, err
		}
		h.Phone = maskPhone(h.Phone)
		out.Owed += h.Balance
		out.Customers++
		out.Holders = append(out.Holders, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// the owed total over every holder, not only those listed
	if err := a.DB.QueryRow(ctx, `SELECT COALESCE(sum(balance), 0), count(*) FILTER (WHERE balance > 0) FROM wallet_accounts
		WHERE kind='store_credit' AND branch_id=$1 AND currency=$2`, branchID, out.Currency).Scan(&out.Owed, &out.Customers); err != nil {
		return nil, err
	}
	rows, err = a.DB.Query(ctx, `SELECT `+creditEntryCols+`, w.customer_id, c.phone,
			COALESCE((SELECT u.display_name FROM users u WHERE u.id = j.actor_id AND j.actor_type = 'staff'), ''), j.note,
			j.kind = 'refund' AND r.id IS NOT NULL AND r.to_credit > r.amount - r.cash_part
				AND NOT EXISTS (SELECT 1 FROM wallet_journals x WHERE x.reverses = j.id)
		FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id JOIN wallet_accounts w ON w.id = p.account_id
			JOIN customers c ON c.id = w.customer_id JOIN branches b ON b.id = j.branch_id
			LEFT JOIN orders o ON o.id = j.order_id LEFT JOIN order_refunds r ON r.id = j.refund_id
		WHERE j.branch_id=$1 AND w.kind='store_credit'
			AND (j.created_at AT TIME ZONE b.timezone)::date BETWEEN $2::date AND $3::date
			AND ($4::bigint[] IS NULL OR o.brand_id = ANY($4))
			AND ($5 = 0 OR (j.actor_type = 'staff' AND j.actor_id = $5))
			AND ($6 = 0 OR w.customer_id = $6)
		ORDER BY j.id DESC LIMIT 1000`, branchID, f.From, f.To, f.Brands, f.StaffID, f.Customer)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var phone, by, note string
		var cust int64
		var revocable bool
		e, err := scanCreditEntry(rows, &cust, &phone, &by, &note, &revocable)
		if err != nil {
			rows.Close()
			return nil, err
		}
		e.CustomerID, e.Phone, e.By, e.Note, e.Revocable = cust, maskPhone(phone), by, note, revocable
		switch e.Kind {
		case "refund":
			out.In += e.Amount
		case "spend", "return":
			out.Used -= e.Amount
		case "revoke":
			out.Revoked -= e.Amount
		}
		out.Entries = append(out.Entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out.Alerts, err = creditAlerts(ctx, a.DB, branchID); err != nil {
		return nil, err
	}
	return out, nil
}

// creditAlerts are the patterns worth the owner's look in the last 30 days
// (§9): one customer getting refunds into a balance again and again, a
// refund into a balance soon after the order was placed, and one person
// refunding into balances far more than the others.
func creditAlerts(ctx context.Context, q Querier, branchID int64) ([]CreditAlert, error) {
	out := []CreditAlert{}
	rows, err := q.Query(ctx, `SELECT c.phone, count(*), sum(p.amount)
		FROM wallet_journals j JOIN wallet_postings p ON p.journal_id = j.id JOIN wallet_accounts w ON w.id = p.account_id
			JOIN customers c ON c.id = w.customer_id
		WHERE j.branch_id=$1 AND j.kind='refund' AND w.kind='store_credit' AND j.created_at > now() - interval '30 days'
		GROUP BY c.phone HAVING count(*) >= 3 ORDER BY count(*) DESC LIMIT 20`, branchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		al := CreditAlert{Kind: "customer_often"}
		if err := rows.Scan(&al.Phone, &al.Count, &al.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		al.Phone = maskPhone(al.Phone)
		out = append(out, al)
	}
	rows.Close()
	rows, err = q.Query(ctx, `SELECT o.id, o.public_no, r.to_credit
		FROM order_refunds r JOIN orders o ON o.id = r.order_id
		WHERE r.branch_id=$1 AND r.to_credit > r.amount - r.cash_part AND r.created_at > now() - interval '30 days'
			AND r.created_at < o.created_at + interval '30 minutes'
		ORDER BY r.id DESC LIMIT 20`, branchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		al := CreditAlert{Kind: "quick_refund"}
		if err := rows.Scan(&al.OrderID, &al.PublicNo, &al.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, al)
	}
	rows.Close()
	rows, err = q.Query(ctx, `WITH per AS (
			SELECT r.refunded_by AS uid, count(*) AS n, sum(r.to_credit - (r.amount - r.cash_part)) AS amt
			FROM order_refunds r WHERE r.branch_id=$1 AND r.refunded_by IS NOT NULL
				AND r.to_credit > r.amount - r.cash_part AND r.created_at > now() - interval '30 days'
			GROUP BY r.refunded_by)
		SELECT u.display_name, per.n, per.amt FROM per JOIN users u ON u.id = per.uid
		WHERE per.n >= 3 AND per.n > 2 * (SELECT avg(n) FROM per) AND (SELECT count(*) FROM per) > 1
		ORDER BY per.n DESC`, branchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		al := CreditAlert{Kind: "staff_often"}
		if err := rows.Scan(&al.By, &al.Count, &al.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, al)
	}
	rows.Close()
	return out, rows.Err()
}

// CreditLimits are the owner's limits on refunds into balances.
type CreditLimits struct {
	ApproveOver int64 `json:"approve_over"`
	DailyLimit  int64 `json:"daily_limit"`
}

// SaveCreditLimits sets them; only the owner does.
func (a *App) SaveCreditLimits(ctx context.Context, branchID int64, in CreditLimits, actor Actor) error {
	if in.ApproveOver < 0 || in.DailyLimit < 0 {
		return ErrValidation("limits are not negative")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		owner, err := isOwner(ctx, tx, actor.ID, branchID)
		if err != nil {
			return err
		}
		if !owner {
			return Err(http.StatusForbidden, "FORBIDDEN", "only the owner sets these limits")
		}
		if _, err := tx.Exec(ctx, `UPDATE branches SET credit_approve_over=$2, credit_daily_limit=$3 WHERE id=$1`,
			branchID, in.ApproveOver, in.DailyLimit); err != nil {
			return err
		}
		return audit(ctx, tx, branchID, 0, actor, "wallet.limits", in)
	})
}

// RefundBranch is the store of a refund.
func (a *App) RefundBranch(ctx context.Context, refundID int64) (int64, error) {
	var bid int64
	err := a.DB.QueryRow(ctx, `SELECT branch_id FROM order_refunds WHERE id=$1`, refundID).Scan(&bid)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	return bid, err
}

// RevokeCreditRefund takes back out of the balance the cash part of a refund
// the store had put there, when the store pays it in cash after all (§7.2):
// only while the balance still holds it. The refund then counts as cash.
func (a *App) RevokeCreditRefund(ctx context.Context, refundID int64, actor Actor) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var orderID int64
		if err := tx.QueryRow(ctx, `SELECT order_id FROM order_refunds WHERE id=$1`, refundID).Scan(&orderID); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		o, err := lockOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		var amount, cashPart, toCredit int64
		if err := tx.QueryRow(ctx, `SELECT amount, cash_part, to_credit FROM order_refunds WHERE id=$1 FOR UPDATE`, refundID).
			Scan(&amount, &cashPart, &toCredit); err != nil {
			return err
		}
		back := toCredit - (amount - cashPart)
		if back <= 0 {
			return ErrInvalidState("this refund did not put cash into a balance")
		}
		var journalID, acctID, customerID int64
		var currency string
		if err := tx.QueryRow(ctx, `SELECT j.id, w.id, w.customer_id, j.currency FROM wallet_journals j
			JOIN wallet_postings p ON p.journal_id = j.id JOIN wallet_accounts w ON w.id = p.account_id
			WHERE j.idem_key=$1 AND w.kind='store_credit'`, fmt.Sprintf("refund:%d", refundID)).
			Scan(&journalID, &acctID, &customerID, &currency); err != nil {
			return err
		}
		acct, err := walletAccount(ctx, tx, acctCredit, o.BranchID, customerID, currency)
		if err != nil {
			return err
		}
		if acct.Balance < back {
			return Err(http.StatusConflict, "CREDIT_SPENT", "the customer already spent part of it")
		}
		store, err := walletAccount(ctx, tx, acctRefund, o.BranchID, 0, currency)
		if err != nil {
			return err
		}
		if _, fresh, err := postJournal(ctx, tx, journal{Kind: "revoke", Idem: fmt.Sprintf("revoke:%d", refundID),
			BranchID: o.BranchID, Currency: currency, OrderID: o.ID, RefundID: refundID, Actor: actor,
			Note: "paid in cash instead", Reverses: journalID},
			posting{acct.ID, -back}, posting{store.ID, back}); err != nil || !fresh {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE order_refunds SET to_credit = to_credit - $2, pay_type = 'cash',
			method = '现金' WHERE id=$1`, refundID, back); err != nil {
			return err
		}
		amt := fmtMoney(back, currency)
		if err := tell(ctx, tx, customerID, Message{Kind: "wallet",
			Title: "店铺余额 " + amt + " 已改为现金退款", TitleEn: amt + " of your store balance was refunded in cash instead",
			Link: fmt.Sprintf("/me/store-credit/%d", o.BranchID), Key: "store_credit_revoked",
			Args: map[string]string{"amount": amt}}); err != nil {
			return err
		}
		return audit(ctx, tx, o.BranchID, o.ID, actor, "wallet.revoke", map[string]any{"refund_id": refundID, "amount": back})
	})
}

// ---- the platform ----

// AdminStoreCredits is a customer's balances at every store, for the
// platform's customer page.
func (a *App) AdminStoreCredits(ctx context.Context, customerID int64) ([]StoreCredit, error) {
	return storeCredits(ctx, a.DB, customerID, true)
}

// AdminFreezeCredit freezes or frees one store balance: a frozen one cannot
// be spent (account taken over, a dispute, a store that closed).
func (a *App) AdminFreezeCredit(ctx context.Context, actor AdminActor, accountID int64, frozen bool, note string) error {
	if len([]rune(note)) > 200 {
		return ErrValidation("note too long")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		// the refund balance and stored value of the customer at the store go together
		tag, err := tx.Exec(ctx, `UPDATE wallet_accounts w SET frozen=$2, frozen_note=$3, updated_at=now()
			FROM wallet_accounts a WHERE a.id=$1 AND a.kind IN ('store_credit', 'stored_principal', 'stored_bonus')
			AND w.customer_id = a.customer_id AND w.branch_id = a.branch_id
			AND w.kind IN ('store_credit', 'stored_principal', 'stored_bonus')`, accountID, frozen, note)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return adminAudit(ctx, tx, actor, map[bool]string{true: "wallet.freeze", false: "wallet.unfreeze"}[frozen],
			"wallet_account", accountID, map[string]any{"note": note})
	})
}

// AdminFreezeBranchCredits freezes every balance at a store, for a store
// that closed (§7.3).
func (a *App) AdminFreezeBranchCredits(ctx context.Context, actor AdminActor, branchID int64, frozen bool, note string) (int64, error) {
	var n int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE wallet_accounts SET frozen=$2, frozen_note=$3, updated_at=now()
			WHERE branch_id=$1 AND kind IN ('store_credit', 'stored_principal', 'stored_bonus') AND (balance > 0 OR frozen)`, branchID, frozen, note)
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		return adminAudit(ctx, tx, actor, map[bool]string{true: "wallet.freeze_store", false: "wallet.unfreeze_store"}[frozen],
			"branch", branchID, map[string]any{"note": note, "accounts": n})
	})
	return n, err
}

// branchCreditOwed is what a store owes customers in balances.
func branchCreditOwed(ctx context.Context, q Querier, branchID int64) (int64, error) {
	var owed int64
	err := q.QueryRow(ctx, `SELECT COALESCE(sum(balance), 0) FROM wallet_accounts
		WHERE kind IN ('store_credit', 'stored_principal', 'stored_bonus') AND branch_id=$1`,
		branchID).Scan(&owed)
	return owed, err
}

func errCreditOwed(owed int64) error {
	return ErrD(http.StatusConflict, "CREDIT_OWED", "customers still hold store balances here: settle them first",
		map[string]any{"owed": owed})
}
