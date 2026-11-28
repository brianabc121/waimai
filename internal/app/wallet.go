package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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

	// Stored value (docs/STORED_VALUE_DESIGN.md §7): what a customer topped
	// up and the store's bonus on it, and the store's side of both.
	acctPrincipal   = "stored_principal"   // a customer's own money topped up at a store
	acctBonus       = "stored_bonus"       // the store's bonus on it
	acctTopup       = "store_topup"        // what the store took in top-ups (negative: owed)
	acctBonusFund   = "store_bonus"        // what the store gave in bonuses
	acctStoredSales = "store_stored_sales" // what was paid from stored value at the store

	// Where stored value that left a customer went (docs/BALANCE_REVIEW.md
	// H1): paid back in money, used bonus kept back by the store, bonus made
	// void by a payback.
	acctCashout   = "store_cashout"
	acctClawback  = "store_clawback"
	acctBonusVoid = "store_bonus_void"

	// The platform's balance and rewards (no store; branch 0) and the
	// platform's side of them.
	acctPlatformBalance = "platform_balance"
	acctPlatformBonus   = "platform_bonus"
	acctPlatformIssued  = "platform_issued"
)

// customerAcct says whether an account kind belongs to a customer.
func customerAcct(kind string) bool {
	return kind == acctCredit || kind == acctPrincipal || kind == acctBonus ||
		kind == acctPlatformBalance || kind == acctPlatformBonus
}

type walletAcct struct {
	ID      int64
	Balance int64
	Frozen  bool
}

// walletAccount opens (when missing) and locks an account. A customer's
// accounts are locked after the customer's row (lockCustomer), so that two
// changes to one customer's balances queue there instead of locking each
// other's accounts in a different order (docs/BALANCE_REVIEW.md M4). The
// store's accounts are locked after them, in the order store_sales,
// store_refund, store_stored_sales (docs/BALANCE_CHECK.md §6). branchID 0
// is the platform's.
func walletAccount(ctx context.Context, tx pgx.Tx, kind string, branchID, customerID int64, currency string) (*walletAcct, error) {
	var cust *int64
	if customerAcct(kind) {
		if customerID == 0 {
			return nil, ErrValidation("a store balance belongs to a customer")
		}
		cust = &customerID
		if err := lockCustomer(ctx, tx, customerID); err != nil {
			return nil, err
		}
	}
	var branch *int64
	if branchID != 0 {
		branch = &branchID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO wallet_accounts (kind, branch_id, customer_id, currency) VALUES ($1,$2,$3,$4)
		ON CONFLICT DO NOTHING`, kind, branch, cust, currency); err != nil {
		return nil, wrap("open wallet account", err)
	}
	var w walletAcct
	err := tx.QueryRow(ctx, `SELECT id, balance, frozen FROM wallet_accounts
		WHERE kind=$1 AND COALESCE(branch_id, 0)=$2 AND COALESCE(customer_id, 0)=$3 AND currency=$4 FOR UPDATE`,
		kind, branchID, customerID, currency).Scan(&w.ID, &w.Balance, &w.Frozen)
	return &w, err
}

// lockCustomer queues every change to one customer's balances. NO KEY
// UPDATE leaves orders free to point at the customer meanwhile.
func lockCustomer(ctx context.Context, tx pgx.Tx, customerID int64) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM customers WHERE id=$1 FOR NO KEY UPDATE`, customerID).Scan(&one)
	if isNoRows(err) {
		return ErrNotFound
	}
	return err
}

type journal struct {
	Kind     string
	Idem     string
	BranchID int64
	Currency string
	OrderID  int64
	RefundID int64
	TopupID  int64
	Cashout  int64
	Actor    Actor
	Note     string
	Reverses int64
}

type posting struct {
	Account int64
	Amount  int64
}

// postJournal writes one journal and its postings through the database's
// only way into the books, wallet_post_journal (00085): it checks that they
// balance, that every account is of the journal's store and currency and of
// a kind the journal may post to, and extends each account's hash chain. A
// journal whose idem key was already written is not written again (fresh is
// false then).
func postJournal(ctx context.Context, tx pgx.Tx, j journal, ps ...posting) (id int64, fresh bool, err error) {
	var sum int64
	var accts, amts []int64
	for _, p := range ps {
		sum += p.Amount
		if p.Amount != 0 {
			accts, amts = append(accts, p.Account), append(amts, p.Amount)
		}
	}
	if sum != 0 || len(accts) < 2 {
		return 0, false, fmt.Errorf("wallet journal %s does not balance", j.Idem)
	}
	nz := func(v int64) *int64 {
		if v == 0 {
			return nil
		}
		return &v
	}
	err = tx.QueryRow(ctx, `SELECT o_id, o_fresh FROM wallet_post_journal($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		j.Kind, j.Idem, nz(j.BranchID), j.Currency, nz(j.OrderID), nz(j.RefundID), nz(j.TopupID), nz(j.Cashout),
		j.Actor.Type, nz(j.Actor.ID), nz(j.Actor.ApprovedBy), j.Note, nz(j.Reverses), accts, amts).Scan(&id, &fresh)
	if isCheck(err) {
		return 0, false, Err(http.StatusConflict, "CREDIT_SHORT", "the store balance does not hold that much")
	} else if err != nil {
		return 0, false, wrap("wallet journal", err)
	}
	return id, fresh, nil
}

// postNew is postJournal for a journal that must be new: its idem key is
// made from what the order holds now, so finding it written already means
// the same change would be counted twice (docs/BALANCE_CHECK.md W20).
func postNew(ctx context.Context, tx pgx.Tx, j journal, ps ...posting) error {
	_, fresh, err := postJournal(ctx, tx, j, ps...)
	if err == nil && !fresh {
		return Err(http.StatusConflict, "WALLET_DUPLICATE", "this balance change was already booked: look again")
	}
	return err
}

// orderCredit is what an order needs to know about its store balance.
type orderCredit struct {
	BranchID, CustomerID, CreditCustomer int64
	Currency, Type, Acceptance, Payment  string
	Total, CreditPaid                    int64
	// the part of CreditPaid that came from stored value
	StoredPrincipal, StoredBonus int64
}

// refundCredit is what the order paid from the customer's refund balance.
func (o *orderCredit) refundCredit() int64 { return o.CreditPaid - o.StoredPrincipal - o.StoredBonus }

func readOrderCredit(ctx context.Context, tx pgx.Tx, orderID int64) (*orderCredit, error) {
	var c orderCredit
	var cust, credit *int64
	err := tx.QueryRow(ctx, `SELECT branch_id, customer_id, credit_customer_id, currency, order_type, acceptance_status,
		payment_status, total, credit_paid, stored_principal_paid, stored_bonus_paid FROM orders WHERE id=$1`, orderID).
		Scan(&c.BranchID, &cust, &credit, &c.Currency, &c.Type, &c.Acceptance, &c.Payment, &c.Total, &c.CreditPaid,
			&c.StoredPrincipal, &c.StoredBonus)
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
	// customer accounts first, then the store's (the lock order)
	acct, err := walletAccount(ctx, tx, acctCredit, o.BranchID, customerID, o.Currency)
	if err != nil {
		return 0, err
	}
	prin, err := walletAccount(ctx, tx, acctPrincipal, o.BranchID, customerID, o.Currency)
	if err != nil {
		return 0, err
	}
	bonus, err := walletAccount(ctx, tx, acctBonus, o.BranchID, customerID, o.Currency)
	if err != nil {
		return 0, err
	}
	if acct.Frozen || (prin.Frozen && prin.Balance > 0) || (bonus.Frozen && bonus.Balance > 0) {
		return 0, Err(http.StatusConflict, "CREDIT_FROZEN", "this store balance is frozen")
	}
	// the refund balance first: it is money the customer already paid
	// (docs/STORED_VALUE_DESIGN.md §8.1), then stored value
	fromCredit := min(want, acct.Balance)
	p, b := storedTake(min(want-fromCredit, prin.Balance+bonus.Balance), prin.Balance, bonus.Balance)
	if fromCredit+p+b <= 0 {
		return 0, nil
	}
	if fromCredit > 0 {
		sales, err := walletAccount(ctx, tx, acctSales, o.BranchID, 0, o.Currency)
		if err != nil {
			return 0, err
		}
		n, err := journalSeq(ctx, tx, orderID, "spend")
		if err != nil {
			return 0, err
		}
		if err := postNew(ctx, tx, journal{Kind: "spend", Idem: fmt.Sprintf("spend:%d:%d", orderID, n),
			BranchID: o.BranchID, Currency: o.Currency, OrderID: orderID, Actor: actor},
			posting{acct.ID, -fromCredit}, posting{sales.ID, fromCredit}); err != nil {
			return 0, err
		}
	}
	if p+b > 0 {
		sales, err := walletAccount(ctx, tx, acctStoredSales, o.BranchID, 0, o.Currency)
		if err != nil {
			return 0, err
		}
		n, err := journalSeq(ctx, tx, orderID, "stored_spend")
		if err != nil {
			return 0, err
		}
		if err := postNew(ctx, tx, journal{Kind: "stored_spend", Idem: fmt.Sprintf("sspend:%d:%d", orderID, n),
			BranchID: o.BranchID, Currency: o.Currency, OrderID: orderID, Actor: actor},
			posting{prin.ID, -p}, posting{bonus.ID, -b}, posting{sales.ID, p + b}); err != nil {
			return 0, err
		}
	}
	amount := fromCredit + p + b
	if _, err := tx.Exec(ctx, `UPDATE orders SET credit_paid = credit_paid + $2, credit_customer_id = $3,
		stored_principal_paid = stored_principal_paid + $4, stored_bonus_paid = stored_bonus_paid + $5,
		version=version+1, updated_at=now() WHERE id=$1`, orderID, amount, customerID, p, b); err != nil {
		return 0, err
	}
	return amount, nil
}

// storedTake splits x taken from stored value holding principal and bonus in
// proportion (rule W9): the bonus share rounded down, so the cent left over
// is principal and what may be paid back in money is never a cent more.
func storedTake(x, principal, bonus int64) (p, b int64) {
	if x <= 0 || principal+bonus <= 0 {
		return 0, 0
	}
	x = min(x, principal+bonus)
	b = pricing.MulDivFloor(x, bonus, principal+bonus)
	return x - b, b
}

// creditGiveBack splits x given back of what an order paid from the refund
// balance c, stored principal p and bonus b in the shares it paid them
// (brian 2026-11-27: the cash balance, c + p, and the bonus each get back
// their share). The refund balance's and the principal's shares are rounded
// down, the bonus takes the cents left over, never more than it paid.
func creditGiveBack(x, c, p, b int64) (cc, pp, bb int64) {
	tot := c + p + b
	if x <= 0 || tot <= 0 {
		return 0, 0, 0
	}
	x = min(x, tot)
	pp = min(pricing.MulDivFloor(x, p, tot), p)
	cc = min(pricing.MulDivFloor(x, c, tot), c)
	bb = x - pp - cc
	if bb > b {
		over := bb - b
		bb = b
		more := min(over, c-cc)
		cc, over = cc+more, over-more
		pp += over
	}
	return cc, pp, bb
}

// giveCreditBack puts back into the balances up to amount of what the order
// paid from them (the order row locked), in the shares the order paid each
// (creditGiveBack; docs/STORED_VALUE_DESIGN.md §8.2).
func giveCreditBack(ctx context.Context, tx pgx.Tx, orderID, amount int64, actor Actor, note string) error {
	o, err := readOrderCredit(ctx, tx, orderID)
	if err != nil {
		return err
	}
	amount = min(amount, o.CreditPaid)
	if amount <= 0 {
		return nil
	}
	c, p, b := creditGiveBack(amount, o.refundCredit(), o.StoredPrincipal, o.StoredBonus)
	if c > 0 {
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
		if err := postNew(ctx, tx, journal{Kind: "return", Idem: fmt.Sprintf("return:%d:%d", orderID, n),
			BranchID: o.BranchID, Currency: o.Currency, OrderID: orderID, Actor: actor, Note: note},
			posting{acct.ID, c}, posting{sales.ID, -c}); err != nil {
			return err
		}
	}
	if p+b > 0 {
		prin, err := walletAccount(ctx, tx, acctPrincipal, o.BranchID, o.CreditCustomer, o.Currency)
		if err != nil {
			return err
		}
		bonus, err := walletAccount(ctx, tx, acctBonus, o.BranchID, o.CreditCustomer, o.Currency)
		if err != nil {
			return err
		}
		sales, err := walletAccount(ctx, tx, acctStoredSales, o.BranchID, 0, o.Currency)
		if err != nil {
			return err
		}
		n, err := journalSeq(ctx, tx, orderID, "stored_return")
		if err != nil {
			return err
		}
		if err := postNew(ctx, tx, journal{Kind: "stored_return", Idem: fmt.Sprintf("sreturn:%d:%d", orderID, n),
			BranchID: o.BranchID, Currency: o.Currency, OrderID: orderID, Actor: actor, Note: note},
			posting{prin.ID, p}, posting{bonus.ID, b}, posting{sales.ID, -(p + b)}); err != nil {
			return err
		}
	}
	back := c + p + b
	_, err = tx.Exec(ctx, `UPDATE orders SET credit_paid = credit_paid - $2,
		stored_principal_paid = stored_principal_paid - $3, stored_bonus_paid = stored_bonus_paid - $4,
		credit_customer_id = CASE WHEN credit_paid - $2 = 0 THEN NULL ELSE credit_customer_id END,
		pay_type = CASE WHEN pay_type = 'credit' AND credit_paid - $2 < total THEN '' ELSE pay_type END,
		version=version+1, updated_at=now() WHERE id=$1`, orderID, back, p, b)
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
// share is the part of it the order had paid from the refund balance: it
// goes back by reversing store_sales, as stored value's share reverses
// store_stored_sales; only the rest, cash the customer agreed to take as a
// balance, is new money the store put into balances (store_refund;
// docs/BALANCE_REVIEW.md M2, rule W16).
func creditRefund(ctx context.Context, tx pgx.Tx, orderID, refundID, customerID, amount, share int64, actor Actor) error {
	o, err := readOrderCredit(ctx, tx, orderID)
	if err != nil {
		return err
	}
	share = max(0, min(share, amount))
	acct, err := walletAccount(ctx, tx, acctCredit, o.BranchID, customerID, o.Currency)
	if err != nil {
		return err
	}
	sales, err := walletAccount(ctx, tx, acctSales, o.BranchID, 0, o.Currency)
	if err != nil {
		return err
	}
	store, err := walletAccount(ctx, tx, acctRefund, o.BranchID, 0, o.Currency)
	if err != nil {
		return err
	}
	if _, _, err := postJournal(ctx, tx, journal{Kind: "refund", Idem: fmt.Sprintf("refund:%d", refundID),
		BranchID: o.BranchID, Currency: o.Currency, OrderID: orderID, RefundID: refundID, Actor: actor},
		posting{acct.ID, amount}, posting{sales.ID, -share}, posting{store.ID, -(amount - share)}); err != nil {
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
	// One person's refunds into balances at a store go one at a time: the
	// second waits here and then counts the first in today's total.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		fmt.Sprintf("credit-refund-day:%d:%d", branchID, actor.ID)); err != nil {
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
	err := q.QueryRow(ctx, `SELECT COALESCE((SELECT sum(w.balance) FROM wallet_accounts w JOIN customers c ON c.id = w.customer_id
		WHERE w.kind IN ('store_credit', 'stored_principal', 'stored_bonus') AND w.branch_id=$1 AND c.phone=$2
			AND w.currency=$3 AND NOT w.frozen), 0)`,
		branchID, phone, currency).Scan(&bal)
	return bal, err
}

// storedParts is a refund's share of stored value.
type storedParts struct{ Principal, Bonus int64 }

// storedRefundSplit works out how much of a refund's balance part
// (creditPart, the refund less its cash part) goes back to stored principal
// and bonus (docs/STORED_VALUE_DESIGN.md §8.2, rule W12): each part's
// running share of the order is rounded down — principal, then the refund
// balance — and the bonus takes the cents left over, so neither of the
// parts that can become money again is ever a cent too much. A refund of
// everything gives each part back exactly.
func storedRefundSplit(total int64, oc *orderCredit, paidBack, cashBack, prinBack, bonusBack, amount, creditPart int64) storedParts {
	if total <= 0 || creditPart <= 0 || oc.StoredPrincipal+oc.StoredBonus == 0 {
		return storedParts{}
	}
	cum := paidBack + amount
	credit := oc.refundCredit()
	creditBack := max(0, paidBack-cashBack-prinBack-bonusBack)
	capP := max(0, oc.StoredPrincipal-prinBack)
	capB := max(0, oc.StoredBonus-bonusBack)
	capC := max(0, credit-creditBack)
	p := max(0, min(pricing.MulDivFloor(cum, oc.StoredPrincipal, total)-prinBack, capP, creditPart))
	c := max(0, min(pricing.MulDivFloor(cum, credit, total)-creditBack, capC, creditPart-p))
	b := creditPart - p - c
	if b > capB {
		over := b - capB
		b = capB
		more := min(over, capC-c)
		c, over = c+more, over-more
		more = min(over, capP-p)
		p += more
	}
	return storedParts{Principal: p, Bonus: b}
}

// storedRefund puts a refund's share of stored value back where it came
// from.
func storedRefund(ctx context.Context, tx pgx.Tx, orderID, refundID, customerID int64, parts storedParts, actor Actor) error {
	o, err := readOrderCredit(ctx, tx, orderID)
	if err != nil {
		return err
	}
	prin, err := walletAccount(ctx, tx, acctPrincipal, o.BranchID, customerID, o.Currency)
	if err != nil {
		return err
	}
	bonus, err := walletAccount(ctx, tx, acctBonus, o.BranchID, customerID, o.Currency)
	if err != nil {
		return err
	}
	sales, err := walletAccount(ctx, tx, acctStoredSales, o.BranchID, 0, o.Currency)
	if err != nil {
		return err
	}
	if _, _, err := postJournal(ctx, tx, journal{Kind: "stored_refund", Idem: fmt.Sprintf("srefund:%d", refundID),
		BranchID: o.BranchID, Currency: o.Currency, OrderID: orderID, RefundID: refundID, Actor: actor},
		posting{prin.ID, parts.Principal}, posting{bonus.ID, parts.Bonus},
		posting{sales.ID, -(parts.Principal + parts.Bonus)}); err != nil {
		return err
	}
	var storeName, publicNo string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(o.brand_name, ''), b.name), o.public_no FROM orders o
		JOIN branches b ON b.id = o.branch_id WHERE o.id=$1`, orderID).Scan(&storeName, &publicNo); err != nil {
		return err
	}
	amt := fmtMoney(parts.Principal+parts.Bonus, o.Currency)
	return tell(ctx, tx, customerID, Message{Kind: "wallet",
		Title:   fmt.Sprintf("%s 退款 %s 已退回你的储值余额", storeName, amt),
		TitleEn: fmt.Sprintf("%s refunded %s to your stored value", storeName, amt),
		Body:    "订单 " + publicNo, BodyEn: "Order " + publicNo,
		Link: fmt.Sprintf("/me/store-credit/%d", o.BranchID), Key: "stored_refund",
		Args: map[string]string{"store": storeName, "amount": amt, "no": publicNo}})
}
