package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The balance ledger (docs/BALANCE_REVIEW.md L4, docs/BALANCE_CHECK.md §5):
// every movement of every balance, one row per posting, with the business
// record it belongs to, who did it and the other side of the journal; and
// the closed days of the books. The platform sees everything, a store its
// own accounts.

// LedgerCounter is the other side of a posting: the rest of its journal.
type LedgerCounter struct {
	AccountKind string `json:"account_kind"`
	Amount      int64  `json:"amount"`
}

// LedgerRow is one posting.
type LedgerRow struct {
	PostingID   int64           `json:"posting_id"`
	JournalID   int64           `json:"journal_id"`
	At          time.Time       `json:"at"`
	Kind        string          `json:"kind"`
	AccountID   int64           `json:"account_id"`
	AccountKind string          `json:"account_kind"`
	Seq         int64           `json:"seq"`
	BranchID    int64           `json:"branch_id"`
	StoreName   string          `json:"store_name"`
	CustomerID  int64           `json:"customer_id"`
	Phone       string          `json:"phone"`
	Currency    string          `json:"currency"`
	Amount      int64           `json:"amount"`
	Balance     int64           `json:"balance"`
	OrderID     int64           `json:"order_id"`
	PublicNo    string          `json:"public_no"`
	RefundID    int64           `json:"refund_id"`
	TopupID     int64           `json:"topup_id"`
	CashoutID   int64           `json:"cashout_id"`
	ActorType   string          `json:"actor_type"`
	By          string          `json:"by"`
	ApprovedBy  string          `json:"approved_by"`
	Note        string          `json:"note"`
	Reverses    int64           `json:"reverses"`
	Counter     []LedgerCounter `json:"counter"`
}

// LedgerFilter narrows the ledger. Days are in the store's time (UTC for
// the platform's accounts); empty means the last 30 days.
type LedgerFilter struct {
	BranchID int64  // 0: every store (the platform only)
	Platform bool   // the platform's own balance and rewards only
	Phone    string // a customer's phone, or its last digits
	Order    string // an order's public number or id
	Kind     string // a journal kind
	Account  string // an account kind
	Staff    int64  // who did it
	From, To string
	Before   int64 // posting id, for the next page
	Mask     bool  // show phones masked
}

// Ledger is a page of the ledger and what it adds up to by account kind.
type Ledger struct {
	Rows   []LedgerRow     `json:"rows"`
	Totals []LedgerTotal   `json:"totals"`
	More   bool            `json:"more"`
	Check  *WalletCheckRun `json:"check,omitempty"`
}

type LedgerTotal struct {
	AccountKind string `json:"account_kind"`
	Currency    string `json:"currency"`
	In          int64  `json:"in"`
	Out         int64  `json:"out"`
	Count       int    `json:"count"`
}

const ledgerPage = 300

func (f *LedgerFilter) where() (string, []any, error) {
	if f.From == "" && f.To == "" {
		now := time.Now()
		f.From, f.To = now.AddDate(0, 0, -30).Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02")
	}
	if err := billDays(f.From, f.To); err != nil {
		return "", nil, err
	}
	args := []any{f.From, f.To}
	conds := []string{`(j.created_at AT TIME ZONE COALESCE(b.timezone, 'UTC'))::date BETWEEN $1::date AND $2::date`}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	switch {
	case f.Platform:
		conds = append(conds, `w.branch_id IS NULL`)
	case f.BranchID != 0:
		add(`w.branch_id = ?`, f.BranchID)
	}
	if p := strings.TrimSpace(f.Phone); p != "" {
		add(`w.customer_id IN (SELECT id FROM customers WHERE phone LIKE '%' || ?)`, p)
	}
	if o := strings.TrimSpace(f.Order); o != "" {
		add(`j.order_id IN (SELECT id FROM orders WHERE public_no = ? OR id::text = ?)`, o)
	}
	if f.Kind != "" {
		add(`j.kind = ?`, f.Kind)
	}
	if f.Account != "" {
		add(`w.kind = ?`, f.Account)
	}
	if f.Staff != 0 {
		add(`j.actor_type IN ('staff', 'admin') AND j.actor_id = ?`, f.Staff)
	}
	return strings.Join(conds, " AND "), args, nil
}

// WalletLedger is a page of the ledger, newest first.
func (a *App) WalletLedger(ctx context.Context, f LedgerFilter) (*Ledger, error) {
	where, args, err := f.where()
	if err != nil {
		return nil, err
	}
	out := &Ledger{Rows: []LedgerRow{}, Totals: []LedgerTotal{}}
	page := where
	pargs := append([]any{}, args...)
	if f.Before > 0 {
		pargs = append(pargs, f.Before)
		page += " AND p.id < $" + strconv.Itoa(len(pargs))
	}
	const from = ` FROM wallet_postings p JOIN wallet_accounts w ON w.id = p.account_id
		JOIN wallet_journals j ON j.id = p.journal_id LEFT JOIN branches b ON b.id = w.branch_id `
	rows, err := a.DB.Query(ctx, `SELECT p.id, j.id, j.created_at, j.kind, w.id, w.kind, p.seq, COALESCE(w.branch_id, 0),
			COALESCE(b.name, ''), COALESCE(w.customer_id, 0), COALESCE(c.phone, ''), w.currency, p.amount, p.balance_after,
			COALESCE(j.order_id, 0), COALESCE(o.public_no, ''), COALESCE(j.refund_id, 0), COALESCE(j.topup_id, 0),
			COALESCE(j.cashout_id, 0), j.actor_type, COALESCE(u.display_name, ''), COALESCE(ua.display_name, ''), j.note,
			COALESCE(j.reverses, 0),
			COALESCE((SELECT jsonb_agg(jsonb_build_object('account_kind', w2.kind, 'amount', p2.amount) ORDER BY p2.id)
				FROM wallet_postings p2 JOIN wallet_accounts w2 ON w2.id = p2.account_id
				WHERE p2.journal_id = j.id AND p2.id <> p.id), '[]')`+from+`
		LEFT JOIN customers c ON c.id = w.customer_id LEFT JOIN orders o ON o.id = j.order_id
		LEFT JOIN users u ON j.actor_type IN ('staff', 'admin') AND u.id = j.actor_id
		LEFT JOIN users ua ON ua.id = j.approved_by
		WHERE `+page+` ORDER BY p.id DESC LIMIT `+strconv.Itoa(ledgerPage+1), pargs...)
	if err != nil {
		return nil, err
	}
	out.Rows, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (LedgerRow, error) {
		var l LedgerRow
		err := r.Scan(&l.PostingID, &l.JournalID, &l.At, &l.Kind, &l.AccountID, &l.AccountKind, &l.Seq, &l.BranchID,
			&l.StoreName, &l.CustomerID, &l.Phone, &l.Currency, &l.Amount, &l.Balance, &l.OrderID, &l.PublicNo,
			&l.RefundID, &l.TopupID, &l.CashoutID, &l.ActorType, &l.By, &l.ApprovedBy, &l.Note, &l.Reverses, &l.Counter)
		if f.Mask && l.Phone != "" {
			l.Phone = maskPhone(l.Phone)
		}
		return l, err
	})
	if err != nil {
		return nil, err
	}
	if len(out.Rows) > ledgerPage {
		out.Rows, out.More = out.Rows[:ledgerPage], true
	}
	if out.Rows == nil {
		out.Rows = []LedgerRow{}
	}
	rows, err = a.DB.Query(ctx, `SELECT w.kind, w.currency, COALESCE(sum(p.amount) FILTER (WHERE p.amount > 0), 0),
			-COALESCE(sum(p.amount) FILTER (WHERE p.amount < 0), 0), count(*)`+from+`
		WHERE `+where+` GROUP BY w.kind, w.currency ORDER BY w.currency, w.kind`, args...)
	if err != nil {
		return nil, err
	}
	out.Totals, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (LedgerTotal, error) {
		var t LedgerTotal
		return t, r.Scan(&t.AccountKind, &t.Currency, &t.In, &t.Out, &t.Count)
	})
	if out.Totals == nil {
		out.Totals = []LedgerTotal{}
	}
	return out, err
}

// TrialRow is one kind of account on a closed day: what it opened with,
// what came in and went out, and what it closed with.
type TrialRow struct {
	AccountKind string `json:"account_kind"`
	Currency    string `json:"currency"`
	Accounts    int    `json:"accounts"`
	Opening     int64  `json:"opening"`
	Credits     int64  `json:"credits"`
	Debits      int64  `json:"debits"`
	Closing     int64  `json:"closing"`
}

// TrialBalance is the books of a store (or the platform, BranchID 0, or
// every store with All) on a closed day. Each currency's closings add up
// to 0 (rule W19).
type TrialBalance struct {
	Day       string     `json:"day"`
	LastDay   string     `json:"last_day"`
	Rows      []TrialRow `json:"rows"`
	Balanced  bool       `json:"balanced"`
	Unclosed  bool       `json:"unclosed"`
	Timezone  string     `json:"timezone"`
	Generated time.Time  `json:"generated"`
}

// WalletTrialBalance is the trial balance of a closed day; an empty day is
// the last one closed.
func (a *App) WalletTrialBalance(ctx context.Context, branchID int64, all bool, day string) (*TrialBalance, error) {
	out := &TrialBalance{Rows: []TrialRow{}, Timezone: "UTC", Generated: time.Now()}
	if branchID != 0 {
		if err := a.DB.QueryRow(ctx, `SELECT timezone FROM branches WHERE id=$1`, branchID).Scan(&out.Timezone); err != nil {
			if isNoRows(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	var last *time.Time
	if err := a.DB.QueryRow(ctx, `SELECT max(day) FROM wallet_days_closed WHERE $2 OR scope_id = $1`, branchID, all).Scan(&last); err != nil {
		return nil, err
	}
	if last == nil {
		out.Unclosed = true
		out.Balanced = true
		return out, nil
	}
	out.LastDay = last.Format("2006-01-02")
	if day == "" {
		day = out.LastDay
	}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return nil, ErrValidation("day: YYYY-MM-DD")
	}
	out.Day = day
	out.Unclosed = day > out.LastDay
	rows, err := a.DB.Query(ctx, `SELECT w.kind, w.currency, count(*)::int, sum(d.opening), sum(d.credits), sum(d.debits), sum(d.closing)
		FROM wallet_daily_balances d JOIN wallet_accounts w ON w.id = d.account_id
		WHERE d.day = $1::date AND ($3 OR COALESCE(w.branch_id, 0) = $2)
		GROUP BY w.currency, w.kind ORDER BY w.currency, w.kind`, day, branchID, all)
	if err != nil {
		return nil, err
	}
	out.Rows, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (TrialRow, error) {
		var t TrialRow
		return t, r.Scan(&t.AccountKind, &t.Currency, &t.Accounts, &t.Opening, &t.Credits, &t.Debits, &t.Closing)
	})
	if err != nil {
		return nil, err
	}
	if out.Rows == nil {
		out.Rows = []TrialRow{}
	}
	sums := map[string]int64{}
	for _, r := range out.Rows {
		sums[r.Currency] += r.Closing
	}
	out.Balanced = true
	for _, v := range sums {
		out.Balanced = out.Balanced && v == 0
	}
	return out, nil
}
