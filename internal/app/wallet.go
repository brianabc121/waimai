package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/brianabc121/waimai/internal/pricing"
)

// Store credit (docs/REFUND_BALANCE_DESIGN.md): money a store refunds into a
// customer's balance, which can only be spent at that store. The books are
// double-entry (§4): every change is a journal whose postings add up to zero,
// written by postJournal only; the database keeps journals and postings
// append-only, moves balances only through postings and refuses a journal
// that does not balance.
//
// A balance is a way to pay, not a discount (§0, §5.5): it never touches the
// pricing engine. An order keeps credit_paid, what it paid from a balance;
// the rest (total - credit_paid) is what is collected in cash or otherwise.

const (
	acctCredit = "store_credit" // a customer's balance at a store
	acctRefund = "store_refund" // what the store put into balances
	acctSales  = "store_sales"  // what was paid from balances at the store
)

type walletAcct struct {
	ID      int64
	Balance int64
	Frozen  bool
}

// walletAccount opens (when missing) and locks an account. Customer accounts
// are locked before the store's, in that order everywhere.
func walletAccount(ctx context.Context, tx pgx.Tx, kind string, branchID, customerID int64, currency string) (*walletAcct, error) {
	var cust *int64
	if kind == acctCredit {
		if customerID == 0 {
			return nil, ErrValidation("a store balance belongs to a customer")
		}
		cust = &customerID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO wallet_accounts (kind, branch_id, customer_id, currency) VALUES ($1,$2,$3,$4)
		ON CONFLICT DO NOTHING`, kind, branchID, cust, currency); err != nil {
		return nil, wrap("open wallet account", err)
	}
	var w walletAcct
	err := tx.QueryRow(ctx, `SELECT id, balance, frozen FROM wallet_accounts
		WHERE kind=$1 AND branch_id=$2 AND COALESCE(customer_id, 0)=$3 AND currency=$4 FOR UPDATE`,
		kind, branchID, customerID, currency).Scan(&w.ID, &w.Balance, &w.Frozen)
	return &w, err
}

type journal struct {
	Kind     string
	Idem     string
	BranchID int64
	Currency string
	OrderID  int64
	RefundID int64
	Actor    Actor
	Note     string
	Reverses int64
}

type posting struct {
	Account int64
	Amount  int64
}

// postJournal writes one journal and its postings. A journal whose idem key
// was already written is not written again (fresh is false then).
func postJournal(ctx context.Context, tx pgx.Tx, j journal, ps ...posting) (id int64, fresh bool, err error) {
	var sum int64
	var live []posting
	for _, p := range ps {
		sum += p.Amount
		if p.Amount != 0 {
			live = append(live, p)
		}
	}
	if sum != 0 || len(live) < 2 {
		return 0, false, fmt.Errorf("wallet journal %s does not balance", j.Idem)
	}
	nz := func(v int64) *int64 {
		if v == 0 {
			return nil
		}
		return &v
	}
	err = tx.QueryRow(ctx, `INSERT INTO wallet_journals (kind, idem_key, branch_id, currency, order_id, refund_id,
		actor_type, actor_id, approved_by, note, reverses) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,left($10, 200),$11)
		ON CONFLICT (idem_key) DO NOTHING RETURNING id`, j.Kind, j.Idem, j.BranchID, j.Currency, nz(j.OrderID),
		nz(j.RefundID), j.Actor.Type, nz(j.Actor.ID), nz(j.Actor.ApprovedBy), j.Note, nz(j.Reverses)).Scan(&id)
	if isNoRows(err) {
		err = tx.QueryRow(ctx, `SELECT id FROM wallet_journals WHERE idem_key=$1`, j.Idem).Scan(&id)
		return id, false, err
	} else if err != nil {
		return 0, false, wrap("wallet journal", err)
	}
	sort.Slice(live, func(a, b int) bool { return live[a].Account < live[b].Account })
	for _, p := range live {
		if _, err := tx.Exec(ctx, `INSERT INTO wallet_postings (journal_id, account_id, amount) VALUES ($1,$2,$3)`,
			id, p.Account, p.Amount); err != nil {
			if isCheck(err) {
				return 0, false, Err(http.StatusConflict, "CREDIT_SHORT", "the store balance does not hold that much")
			}
			return 0, false, wrap("wallet posting", err)
		}
	}
	return id, true, nil
}

// orderCredit is what an order needs to know about its store balance.
type orderCredit struct {
	BranchID, CustomerID, CreditCustomer int64
	Currency, Type, Acceptance, Payment  string
	Total, CreditPaid                    int64
}

func readOrderCredit(ctx context.Context, tx pgx.Tx, orderID int64) (*orderCredit, error) {
	var c orderCredit
	var cust, credit *int64
	err := tx.QueryRow(ctx, `SELECT branch_id, customer_id, credit_customer_id, currency, order_type, acceptance_status,
		payment_status, total, credit_paid FROM orders WHERE id=$1`, orderID).Scan(&c.BranchID, &cust, &credit, &c.Currency,
		&c.Type, &c.Acceptance, &c.Payment, &c.Total, &c.CreditPaid)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if cust != nil {
		c.CustomerID = *cust
	}
	if credit != nil {
		c.CreditCustomer = *credit
	}
	return &c, nil
}

// journalSeq numbers an order's journals of one kind, for their idem keys.
// The order row is locked, so the count cannot race.
func journalSeq(ctx context.Context, tx pgx.Tx, orderID int64, kind string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM wallet_journals WHERE order_id=$1 AND kind=$2`, orderID, kind).Scan(&n)
	return n + 1, err
}

// useCredit pays up to want of the order (the order row locked) from the
// customer's balance at its store: never more than the balance holds or the
// order still has to be paid. It returns how much was taken.
func useCredit(ctx context.Context, tx pgx.Tx, orderID, customerID, want int64, actor Actor) (int64, error) {
	o, err := readOrderCredit(ctx, tx, orderID)
	if err != nil {
		return 0, err
	}
	if o.Type == TypePlatform {
		return 0, ErrInvalidState("a delivery platform's order is paid on the platform")
	}
	if o.Acceptance == "rejected" || o.Acceptance == "cancelled" || o.Payment == "paid" {
		return 0, ErrInvalidState("the order is closed or already paid")
	}
	if o.CreditCustomer != 0 && o.CreditCustomer != customerID {
		return 0, Err(http.StatusConflict, "CREDIT_OTHER_CUSTOMER", "the order is already paid from another customer's balance")
	}
	want = min(want, o.Total-o.CreditPaid)
	if want <= 0 {
		return 0, nil
	}
	acct, err := walletAccount(ctx, tx, acctCredit, o.BranchID, customerID, o.Currency)
	if err != nil {
		return 0, err
	}
	if acct.Frozen {
		return 0, Err(http.StatusConflict, "CREDIT_FROZEN", "this store balance is frozen")
	}
	amount := min(want, acct.Balance)
	if amount <= 0 {
		return 0, nil
	}
	sales, err := walletAccount(ctx, tx, acctSales, o.BranchID, 0, o.Currency)
	if err != nil {
		return 0, err
	}
	n, err := journalSeq(ctx, tx, orderID, "spend")
	if err != nil {
		return 0, err
	}
	if _, _, err := postJournal(ctx, tx, journal{Kind: "spend", Idem: fmt.Sprintf("spend:%d:%d", orderID, n),
		BranchID: o.BranchID, Currency: o.Currency, OrderID: orderID, Actor: actor},
		posting{acct.ID, -amount}, posting{sales.ID, amount}); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET credit_paid = credit_paid + $2, credit_customer_id = $3,
		version=version+1, updated_at=now() WHERE id=$1`, orderID, amount, customerID); err != nil {
		return 0, err
	}
	return amount, nil
}

// giveCreditBack puts back into the balance up to amount of what the order
// paid from it (the order row locked).
func giveCreditBack(ctx context.Context, tx pgx.Tx, orderID, amount int64, actor Actor, note string) error {
	o, err := readOrderCredit(ctx, tx, orderID)
	if err != nil {
		return err
	}
	amount = min(amount, o.CreditPaid)
	if amount <= 0 {
		return nil
	}
	acct, err := walletAccount(ctx, tx, acctCredit, o.BranchID, o.CreditCustomer, o.Currency)
	if err != nil {
		return err
	}
	sales, err := walletAccount(ctx, tx, acctSales, o.BranchID, 0, o.Currency)
	if err != nil {
		return err
	}
	n, err := journalSeq(ctx, tx, orderID, "return")
	if err != nil {
		return err
	}
	if _, _, err := postJournal(ctx, tx, journal{Kind: "return", Idem: fmt.Sprintf("return:%d:%d", orderID, n),
		BranchID: o.BranchID, Currency: o.Currency, OrderID: orderID, Actor: actor, Note: note},
		posting{acct.ID, amount}, posting{sales.ID, -amount}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE orders SET credit_paid = credit_paid - $2,
		credit_customer_id = CASE WHEN credit_paid - $2 = 0 THEN NULL ELSE credit_customer_id END,
		pay_type = CASE WHEN pay_type = 'credit' AND credit_paid - $2 < total THEN '' ELSE pay_type END,
		version=version+1, updated_at=now() WHERE id=$1`, orderID, amount)
	return err
}

// settleTenders keeps what an order paid from a balance within what it comes
// to (rule W4) after its amounts changed: the cash still to collect goes
// down first, then the balance gets the rest back (§5.4).
func settleTenders(ctx context.Context, tx pgx.Tx, orderID int64) error {
	var total, paid int64
	if err := tx.QueryRow(ctx, `SELECT total, credit_paid FROM orders WHERE id=$1`, orderID).Scan(&total, &paid); err != nil {
		return err
	}
	if paid <= total {
		return nil
	}
	return giveCreditBack(ctx, tx, orderID, paid-total, Actor{Type: "system"}, "order amount went down")
}

// creditDue is what is still to be collected on an order besides its store
// balance.
const creditDue = `(o.total - o.credit_paid)`

// isOwner says whether the person owns the store.
func isOwner(ctx context.Context, q Querier, userID, branchID int64) (bool, error) {
	if userID == 0 {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_branch_roles WHERE user_id=$1 AND branch_id=$2 AND role='owner')`,
		userID, branchID).Scan(&ok)
	return ok, err
}

// refundSplit works out how much of a refund came from the cash the customer
// paid, in proportion (§6.2): the running share of the cash part is rounded
// down, so cash is never refunded a cent too much and a refund of everything
// gives each part back exactly.
func refundSplit(total, creditPaid, refundedBefore, cashPartBefore, amount int64) int64 {
	if total <= 0 {
		return 0
	}
	cash := total - creditPaid
	target := pricing.MulDivFloor(refundedBefore+amount, cash, total)
	part := target - cashPartBefore
	return max(0, min(part, amount, cash-cashPartBefore))
}

// isCheck says whether err is a broken CHECK constraint (a balance that
// would go below zero).
func isCheck(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23514"
}

// creditRefund puts amount of a refund into the customer's store balance.
func creditRefund(ctx context.Context, tx pgx.Tx, orderID, refundID, customerID, amount int64, actor Actor) error {
	o, err := readOrderCredit(ctx, tx, orderID)
	if err != nil {
		return err
	}
	acct, err := walletAccount(ctx, tx, acctCredit, o.BranchID, customerID, o.Currency)
	if err != nil {
		return err
	}
	store, err := walletAccount(ctx, tx, acctRefund, o.BranchID, 0, o.Currency)
	if err != nil {
		return err
	}
	if _, _, err := postJournal(ctx, tx, journal{Kind: "refund", Idem: fmt.Sprintf("refund:%d", refundID),
		BranchID: o.BranchID, Currency: o.Currency, OrderID: orderID, RefundID: refundID, Actor: actor},
		posting{acct.ID, amount}, posting{store.ID, -amount}); err != nil {
		return err
	}
	var storeName, publicNo string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(o.brand_name, ''), b.name), o.public_no FROM orders o
		JOIN branches b ON b.id = o.branch_id WHERE o.id=$1`, orderID).Scan(&storeName, &publicNo); err != nil {
		return err
	}
	amt := fmtMoney(amount, o.Currency)
	return tell(ctx, tx, customerID, Message{Kind: "wallet",
		Title:   fmt.Sprintf("%s 退款 %s 已存入你在本店的余额", storeName, amt),
		TitleEn: fmt.Sprintf("%s refunded %s into your balance at the store", storeName, amt),
		Body:    "只能在本店使用。订单 " + publicNo, BodyEn: "It can only be spent at this store. Order " + publicNo,
		Link: fmt.Sprintf("/me/store-credit/%d", o.BranchID), Key: "store_credit_refund",
		Args: map[string]string{"store": storeName, "amount": amt, "no": publicNo}})
}

// creditRefundLimits holds back a refund of cash into a store balance above
// the owner's limits (§6.4): one above the single limit needs a manager's
// approval, a person's total for the day above the daily limit the owner's.
// The owner is not limited.
func creditRefundLimits(ctx context.Context, tx pgx.Tx, branchID, amount int64, actor Actor) error {
	owner, err := isOwner(ctx, tx, actor.ID, branchID)
	if err != nil || owner {
		return err
	}
	var over, daily, today int64
	if err := tx.QueryRow(ctx, `SELECT b.credit_approve_over, b.credit_daily_limit,
		COALESCE((SELECT sum(r.to_credit - (r.amount - r.cash_part)) FROM order_refunds r
			WHERE r.branch_id = b.id AND r.refunded_by = $2
			AND (r.created_at AT TIME ZONE b.timezone)::date = (now() AT TIME ZONE b.timezone)::date), 0)
		FROM branches b WHERE b.id=$1`, branchID, actor.ID).Scan(&over, &daily, &today); err != nil {
		return err
	}
	if today+amount > daily {
		ok, err := isOwner(ctx, tx, actor.ApprovedBy, branchID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrD(http.StatusForbidden, "CREDIT_DAILY_LIMIT", "today's refunds into balances are over the store's limit: the owner has to approve",
				map[string]any{"perm": PermWalletRefund, "limit": daily, "today": today, "owner": true})
		}
		return nil
	}
	if amount > over && actor.ApprovedBy == 0 {
		return ErrD(http.StatusForbidden, "CREDIT_NEEDS_APPROVAL", "a refund this large into a balance needs a manager",
			map[string]any{"perm": PermWalletRefund, "limit": over})
	}
	return nil
}

// storeCreditOf is what the customer with this phone can spend at the store.
func storeCreditOf(ctx context.Context, q Querier, branchID int64, phone, currency string) (int64, error) {
	if phone == "" {
		return 0, nil
	}
	var bal int64
	err := q.QueryRow(ctx, `SELECT COALESCE((SELECT w.balance FROM wallet_accounts w JOIN customers c ON c.id = w.customer_id
		WHERE w.kind='store_credit' AND w.branch_id=$1 AND c.phone=$2 AND w.currency=$3 AND NOT w.frozen), 0)`,
		branchID, phone, currency).Scan(&bal)
	return bal, err
}
