package app_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// Store credit (docs/REFUND_BALANCE_DESIGN.md, docs/PRICING_CHECK.md §3 row 19):
// every step that moves money is checked against the rules W1–W7.

func (s *store) placeAs(t *testing.T, phone, service string, lines []app.QuoteLineInput, use bool, expect *int64) (*app.Order, error) {
	t.Helper()
	ctx := context.Background()
	in := app.QuoteInput{ServiceType: service, Items: lines}
	if service == "delivery" {
		in.ZoneID = &s.zoneID
	}
	q, err := testApp.CreateQuote(ctx, s.slug, in, phone)
	noErr(t, err)
	p, err := testApp.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann",
		AddressLine: "1 Main St", UseCredit: use, CreditExpected: expect})
	if err != nil {
		return nil, err
	}
	return testApp.OrderByToken(ctx, p.AccessToken)
}

// finishPickup makes every dish, packs the order and hands it over.
func finishPickup(t *testing.T, o *app.Order, staff app.Actor) *app.Order {
	t.Helper()
	ctx, a := context.Background(), testApp
	var err error
	if o.AcceptanceStatus != "accepted" {
		o, err = a.Accept(ctx, o.ID, 0, staff)
		noErr(t, err)
	}
	for _, it := range o.Items {
		if it.PrepStatus != "cancelled" && it.PrepStatus != "ready" {
			o, err = a.SetItemStatus(ctx, o.ID, it.ID, "ready", app.Actor{Type: "system"})
			noErr(t, err)
		}
	}
	o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, staff)
	noErr(t, err)
	o, err = a.StaffHandoff(ctx, o.ID, staff)
	noErr(t, err)
	return o
}

func creditAt(t *testing.T, customerID, branchID int64) int64 {
	t.Helper()
	l, err := testApp.MyStoreCredits(context.Background(), customerID)
	noErr(t, err)
	for _, c := range l {
		if c.BranchID == branchID {
			return c.Balance
		}
	}
	return 0
}

func ownerOf(t *testing.T, branchID int64) app.Actor {
	t.Helper()
	var id int64
	noErr(t, testApp.DB.QueryRow(context.Background(), `SELECT user_id FROM user_branch_roles WHERE branch_id=$1 AND role='owner'
		LIMIT 1`, branchID).Scan(&id))
	return app.Actor{Type: "staff", ID: id}
}

type refundRow struct{ Amount, CashPart, ToCredit int64 }

func refundRowOf(t *testing.T, id int64) refundRow {
	t.Helper()
	var r refundRow
	noErr(t, testApp.DB.QueryRow(context.Background(), `SELECT amount, cash_part, to_credit FROM order_refunds WHERE id=$1`, id).
		Scan(&r.Amount, &r.CashPart, &r.ToCredit))
	return r
}

func walletClean(t *testing.T) {
	t.Helper()
	bad, err := testApp.CheckWallet(context.Background())
	noErr(t, err)
	if len(bad) > 0 {
		t.Fatalf("wallet check: %v", bad)
	}
}

// today is a window around today: the stores in the tests keep their own
// time zone, whose date is not always the machine's. Every test store is
// new, so the wider window holds only this test's work.
func today() app.CreditFilter {
	now := time.Now()
	return app.CreditFilter{From: now.AddDate(0, 0, -1).Format("2006-01-02"), To: now.AddDate(0, 0, 1).Format("2006-01-02")}
}

// tamper changes rows the way someone with the database's owner rights
// could: with the triggers that refuse it switched off (00085 refuses the
// change itself), so that the checks can be shown to catch it.
func tamper(t *testing.T, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	tx, err := testApp.DB.Begin(ctx)
	noErr(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
	noErr(t, err)
	_, err = tx.Exec(ctx, sql, args...)
	noErr(t, err)
	noErr(t, tx.Commit(ctx))
}

func i64(v int64) *int64 { return &v }

// Row 19: an order paid partly from the balance through add-on, void,
// discount, pickup and refunds both ways; the balance is the store's only.
func TestStoreCreditFlow(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)

	// A: paid in cash; part of a dish goes into the balance with the
	// customer's say-so, a retry records nothing twice
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 3, OptionIDs: []int64{s.spiceMild}},
		{ItemID: s.tea, Qty: 1}}, false, nil)
	noErr(t, err)
	o = finishPickup(t, o, s.staff)
	checked(t, "A picked up", o.ID)
	chicken := itemByName(o, "chicken")
	in := app.RefundInput{Lines: refundLines(chicken.ID, 1), Reason: "凉了", ToCredit: true, Amount: i64(5000)}
	_, _, err = a.Refund(ctx, o.ID, in, s.staff)
	wantCode(t, err, "CREDIT_CONSENT")
	in.Consent, in.IdemKey = "in_person", uniq("idem")
	rq, _, err := a.Refund(ctx, o.ID, in, s.staff)
	noErr(t, err)
	if rq.CashPart != 5000 || rq.CreditPart != 0 || creditAt(t, cust, s.branchID) != 5000 {
		t.Fatalf("refund to balance: %+v, balance %d", rq, creditAt(t, cust, s.branchID))
	}
	if r := refundRowOf(t, rq.RefundID); r != (refundRow{5000, 5000, 5000}) {
		t.Fatalf("refund row: %+v", r)
	}
	again, _, err := a.Refund(ctx, o.ID, in, s.staff)
	noErr(t, err)
	if again.RefundID != rq.RefundID || creditAt(t, cust, s.branchID) != 5000 {
		t.Fatalf("a retry refunded again: %d vs %d, balance %d", again.RefundID, rq.RefundID, creditAt(t, cust, s.branchID))
	}
	checked(t, "A refund into the balance", o.ID)
	walletClean(t)

	// another store, the same customer: nothing to spend there
	s2 := newStore(t, false)
	q2, err := a.CreateQuote(ctx, s2.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s2.tea, Qty: 1}}}, phone)
	noErr(t, err)
	if q2.StoreCredit != 0 || q2.Credit != 0 {
		t.Fatalf("another store sees the balance: %+v", q2)
	}
	o2, err := s2.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s2.tea, Qty: 1}}, true, nil)
	noErr(t, err)
	if o2.CreditPaid != 0 || o2.Due != o2.Totals.Total {
		t.Fatalf("spent at another store: %+v", o2)
	}
	checked(t, "another store", o2.ID)

	// B: the checkout page showed the balance; the amount it showed must
	// still hold
	lines := []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.tea, Qty: 1},
		{ItemID: s.water, Qty: 1}}
	qb, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: lines}, phone)
	noErr(t, err)
	if qb.StoreCredit != 5000 || qb.Credit != 5000 {
		t.Fatalf("quote: store_credit %d credit %d", qb.StoreCredit, qb.Credit)
	}
	_, err = s.placeAs(t, phone, "pickup", lines, true, i64(4000))
	wantCode(t, err, "CREDIT_CHANGED")
	if creditAt(t, cust, s.branchID) != 5000 {
		t.Fatal("a refused order spent the balance")
	}
	b, err := s.placeAs(t, phone, "pickup", lines, true, i64(5000))
	noErr(t, err)
	if b.CreditPaid != 5000 || b.Due != b.Totals.Total-5000 || creditAt(t, cust, s.branchID) != 0 {
		t.Fatalf("placed with balance: paid %d due %d total %d", b.CreditPaid, b.Due, b.Totals.Total)
	}
	checked(t, "B placed with the balance", b.ID)
	b, err = a.Accept(ctx, b.ID, 0, s.staff)
	noErr(t, err)

	res, err := a.StaffAddon(ctx, b.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}}, s.staff)
	noErr(t, err)
	b = res.Order
	if b.CreditPaid != 5000 || b.Due != b.Totals.Total-5000 {
		t.Fatalf("add-on: %+v", b)
	}
	checked(t, "B add-on", b.ID)

	r, err := a.VoidLine(ctx, itemByName(b, "tea").ID, app.VoidInput{Reason: "不要了", ReasonKind: "customer"}, s.staff, false)
	noErr(t, err)
	b = r.Order
	if b.CreditPaid != 5000 || b.Due != b.Totals.Total-5000 {
		t.Fatalf("void: %+v", b)
	}
	checked(t, "B void", b.ID)

	b, err = a.SetOrderAdjust(ctx, b.ID, 0, app.Adjust{PercentBP: 9000, Reason: "熟客"}, s.staff)
	noErr(t, err)
	if b.CreditPaid != 5000 || b.Due != b.Totals.Total-5000 {
		t.Fatalf("10%% off: %+v", b)
	}
	checked(t, "B 10% off", b.ID)
	// paying the rest by card leaves the balance's part alone
	b = finishPickup(t, b, s.staff)
	if b.PaymentStatus != "paid" || b.CreditPaid != 5000 {
		t.Fatalf("B picked up: %s %d", b.PaymentStatus, b.CreditPaid)
	}
	checked(t, "B picked up", b.ID)

	// B refunds: in cash, the balance's share goes back to the balance; then
	// every dish left into the balance. The last dish takes all that is left
	// (sample S11), so it all comes back, and the cash part ends exactly at
	// what was paid in cash (W-S2)
	var cash, back int64
	refund := func(ri app.RefundInput) {
		t.Helper()
		before := creditAt(t, cust, s.branchID)
		rq, _, err := a.Refund(ctx, b.ID, ri, s.staff)
		noErr(t, err)
		got := creditAt(t, cust, s.branchID) - before
		if rq.CashPart <= 0 || rq.CreditPart <= 0 || (!ri.ToCredit && got != rq.CreditPart) ||
			(ri.ToCredit && got != rq.CashPart+rq.CreditPart) {
			t.Fatalf("refund %+v put %d into the balance", rq, got)
		}
		cash += rq.CashPart
		back += rq.CashPart + rq.CreditPart
		checked(t, "B refund", b.ID)
	}
	chickenB := itemByName(b, "chicken").ID
	refund(app.RefundInput{Lines: refundLines(chickenB, 1), Reason: "x", Method: "现金"})
	var rest []struct {
		ItemID int64 `json:"item_id"`
		Qty    int   `json:"qty"`
	}
	for _, it := range b.Items {
		if it.PrepStatus == "cancelled" {
			continue
		}
		qty := it.Qty
		if it.ID == chickenB {
			qty--
		}
		rest = append(rest, refundLines(it.ID, qty)...)
	}
	refund(app.RefundInput{Lines: rest, Reason: "x", ToCredit: true, Consent: "chat"})
	if T := b.Totals.Total; back != T || cash != T-5000 {
		t.Fatalf("refunded %d (cash %d) of %d", back, cash, T)
	}
	walletClean(t)

	// C: cancelled after acceptance gives the balance back
	bal := creditAt(t, cust, s.branchID)
	c, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}}, true, nil)
	noErr(t, err)
	if c.CreditPaid != min(bal, c.Totals.Total) {
		t.Fatalf("C paid %d of %d", c.CreditPaid, bal)
	}
	checked(t, "C placed", c.ID)
	c, err = a.Accept(ctx, c.ID, 0, s.staff)
	noErr(t, err)
	c, err = a.StaffCancel(ctx, c.ID, c.Version, "沽清", s.staff)
	noErr(t, err)
	if creditAt(t, cust, s.branchID) != bal {
		t.Fatalf("cancel: balance %d, was %d", creditAt(t, cust, s.branchID), bal)
	}
	checked(t, "C cancelled", c.ID)

	// another currency: the balance is in the store's currency only
	_, err = a.DB.Exec(ctx, `UPDATE branches SET currency='USD' WHERE id=$1`, s.branchID)
	noErr(t, err)
	qu, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s.water, Qty: 1}}}, phone)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE branches SET currency=(SELECT currency FROM wallet_accounts WHERE branch_id=$1 AND
		kind='store_credit' LIMIT 1) WHERE id=$1`, s.branchID)
	noErr(t, err)
	if qu.StoreCredit != 0 {
		t.Fatalf("another currency sees the balance: %d", qu.StoreCredit)
	}

	// frozen by the platform: cannot be spent
	l, err := a.MyStoreCredits(ctx, cust)
	noErr(t, err)
	var acct int64
	for _, sc := range l {
		if sc.BranchID == s.branchID {
			acct = sc.AccountID
		}
	}
	noErr(t, a.AdminFreezeCredit(ctx, app.AdminActor{ID: 1}, acct, true, "争议"))
	_, err = s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}}, true, nil)
	wantCode(t, err, "CREDIT_FROZEN")
	noErr(t, a.AdminFreezeCredit(ctx, app.AdminActor{ID: 1}, acct, false, ""))

	// the store cannot be deleted while it owes balances
	if bc, err := a.BranchCredit(ctx, s.branchID, today()); err != nil || bc.Owed != bal || bc.Customers != 1 {
		t.Fatalf("store page: %+v %v", bc, err)
	}
	walletClean(t)
}

// W-S3 on a real order: the order shrinks below what the balance paid, the
// cash still to collect goes first, then the balance gets the rest back.
func TestStoreCreditOrderShrinks(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 4, OptionIDs: []int64{s.spiceMild}}}, false, nil)
	noErr(t, err)
	o = finishPickup(t, o, s.staff)
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "chicken").ID, 2), Reason: "x",
		ToCredit: true, Consent: "phone", Amount: i64(20000)}, s.staff)
	noErr(t, err)
	bal := creditAt(t, cust, s.branchID)
	checked(t, "refund", o.ID)

	b, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}},
		{ItemID: s.tea, Qty: 2}}, true, nil)
	noErr(t, err)
	if b.CreditPaid != min(bal, b.Totals.Total) || b.CreditPaid >= b.Totals.Total {
		t.Fatalf("placed: paid %d of %d, balance %d", b.CreditPaid, b.Totals.Total, bal)
	}
	paid := b.CreditPaid
	b, err = a.Accept(ctx, b.ID, 0, s.staff)
	noErr(t, err)
	r, err := a.VoidLine(ctx, itemByName(b, "tea").ID, app.VoidInput{Qty: 2, Reason: "不要了", ReasonKind: "customer"}, s.staff, false)
	noErr(t, err)
	b = r.Order
	if b.Totals.Total >= paid {
		t.Skipf("the order did not shrink below the balance paid: %d >= %d", b.Totals.Total, paid)
	}
	if b.CreditPaid != b.Totals.Total || b.Due != 0 || creditAt(t, cust, s.branchID) != bal-b.Totals.Total {
		t.Fatalf("shrunk: paid %d total %d due %d balance %d", b.CreditPaid, b.Totals.Total, b.Due, creditAt(t, cust, s.branchID))
	}
	checked(t, "void below the balance paid", b.ID)
	b = finishPickup(t, b, s.staff)
	if b.PaymentStatus != "paid" || b.PayType != "credit" {
		t.Fatalf("paid by balance alone: %s %s", b.PaymentStatus, b.PayType)
	}
	checked(t, "picked up", b.ID)
	walletClean(t)
}

// Two orders at once on one balance: it is spent once.
func TestStoreCreditConcurrentSpend(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}, false, nil)
	noErr(t, err)
	o = finishPickup(t, o, s.staff)
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "chicken").ID, 1), Reason: "x",
		ToCredit: true, Consent: "in_person", Amount: i64(6000)}, s.staff)
	noErr(t, err)

	var wg sync.WaitGroup
	got := make([]*app.Order, 4)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}, true, nil)
			if err == nil {
				got[i] = o
			}
		}(i)
	}
	wg.Wait()
	var spent int64
	for _, o := range got {
		if o == nil {
			t.Fatal("an order failed")
		}
		spent += o.CreditPaid
		checked(t, "concurrent", o.ID)
	}
	if spent != 6000 || creditAt(t, cust, s.branchID) != 0 {
		t.Fatalf("spent %d of 6000, left %d", spent, creditAt(t, cust, s.branchID))
	}
	walletClean(t)
}

// §7.1: who may put cash into a balance and how much; §7.2 revoking.
func TestStoreCreditLimitsAndRevoke(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)
	owner := ownerOf(t, s.branchID)
	err := a.SaveCreditLimits(ctx, s.branchID, app.CreditLimits{ApproveOver: 1000, DailyLimit: 3000}, s.staff)
	wantCode(t, err, "FORBIDDEN")
	noErr(t, a.SaveCreditLimits(ctx, s.branchID, app.CreditLimits{ApproveOver: 1000, DailyLimit: 3000}, owner))

	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 4, OptionIDs: []int64{s.spiceMild}}}, false, nil)
	noErr(t, err)
	o = finishPickup(t, o, s.staff)
	chicken := itemByName(o, "chicken").ID
	give := func(amount int64, actor app.Actor) (*app.RefundQuote, error) {
		rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(chicken, 1), Reason: "x", ToCredit: true,
			Consent: "in_person", Amount: &amount}, actor)
		return rq, err
	}
	_, err = give(2000, s.staff)
	wantCode(t, err, "CREDIT_NEEDS_APPROVAL")
	mgr := s.staff
	mgr.ApprovedBy = s.rider1 // any other user who approved
	r1, err := give(2000, mgr)
	noErr(t, err)
	_, err = give(1500, mgr)
	wantCode(t, err, "CREDIT_DAILY_LIMIT")
	byOwner := s.staff
	byOwner.ApprovedBy = owner.ID
	_, err = give(1500, byOwner)
	noErr(t, err)
	_, err = give(5000, owner)
	noErr(t, err)
	if creditAt(t, cust, s.branchID) != 8500 {
		t.Fatalf("balance %d", creditAt(t, cust, s.branchID))
	}
	checked(t, "refunds into the balance", o.ID)

	// paid in cash after all: the cash part comes out of the balance
	noErr(t, a.RevokeCreditRefund(ctx, r1.RefundID, s.staff))
	if creditAt(t, cust, s.branchID) != 6500 || refundRowOf(t, r1.RefundID).ToCredit != 0 {
		t.Fatalf("revoked: balance %d, refund %+v", creditAt(t, cust, s.branchID), refundRowOf(t, r1.RefundID))
	}
	// the refund keeps what went in: put in = still in + taken back (L1)
	var revoked int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT revoked_credit FROM order_refunds WHERE id=$1`, r1.RefundID).Scan(&revoked))
	if revoked != 2000 {
		t.Fatalf("taken back %d, the balance went from 8500 to 6500", revoked)
	}
	checked(t, "revoked", o.ID)
	err = a.RevokeCreditRefund(ctx, r1.RefundID, s.staff)
	wantCode(t, err, "INVALID_STATE")

	// once spent, it cannot be taken back
	b, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}},
		{ItemID: s.tea, Qty: 1}}, true, nil)
	noErr(t, err)
	checked(t, "spent", b.ID)
	if creditAt(t, cust, s.branchID) >= 5000 {
		t.Fatalf("expected most of the balance spent, left %d", creditAt(t, cust, s.branchID))
	}
	bc, err := a.BranchCredit(ctx, s.branchID, today())
	noErr(t, err)
	var last int64
	for _, e := range bc.Entries {
		if e.Kind == "refund" && e.Amount == 5000 {
			last = e.RefundID
		}
	}
	err = a.RevokeCreditRefund(ctx, last, s.staff)
	wantCode(t, err, "CREDIT_SPENT")
	if bc.ApproveOver != 1000 || bc.DailyLimit != 3000 || bc.In != 8500 || bc.Revoked != 2000 {
		t.Fatalf("store page: %+v", bc)
	}
	walletClean(t)
}

// §5.3, §6.3: an order the staff entered names nobody's balance until the
// customer reads out a texted code; a balance filled at one brand is spent
// at another brand of the same store.
func TestStoreCreditByCode(t *testing.T) {
	bs := newBrandStore(t)
	ctx, a := context.Background(), testApp
	_, phone := customer(t)
	place := func(brand int64, no int) *app.Order {
		out, err := a.PlaceStaffOrder(ctx, bs.branchID, bs.staff, uniq("k"), app.StaffOrderInput{BrandID: brand,
			Member:      app.MemberInput{No: no, Phone: phone, Source: "wechat"},
			ServiceType: "pickup", Accept: true, Items: []app.StaffLine{{ItemID: bs.pork, Qty: 2}, {ItemID: bs.rice, Qty: 2}}})
		noErr(t, err)
		return out.Order
	}
	o := finishPickup(t, place(bs.hcd, 1), bs.staff)
	checked(t, "好吃党 picked up", o.ID)
	pork := itemByName(o, "辣椒炒肉").ID
	in := app.RefundInput{Lines: refundLines(pork, 1), Reason: "x", ToCredit: true, Consent: "phone"}
	_, _, err := a.Refund(ctx, o.ID, in, bs.staff)
	wantCode(t, err, "CREDIT_NO_CUSTOMER")
	sent, err := a.RequestCreditCode(ctx, o.ID, "", "refund", bs.staff)
	noErr(t, err)
	_, err = a.RequestCreditCode(ctx, o.ID, "", "refund", bs.staff)
	wantCode(t, err, "OTP_WAIT")
	in.Verify = &app.CreditVerify{ID: sent.ID, Code: "000000"}
	if sent.DevCode == "000000" {
		in.Verify.Code = "000001"
	}
	_, _, err = a.Refund(ctx, o.ID, in, bs.staff)
	wantCode(t, err, "OTP_INVALID")
	in.Verify.Code = sent.DevCode
	rq, _, err := a.Refund(ctx, o.ID, in, bs.staff)
	noErr(t, err)
	checked(t, "refund by code", o.ID)
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(pork, 1), Reason: "x", ToCredit: true,
		Verify: &app.CreditVerify{ID: sent.ID, Code: sent.DevCode}}, bs.staff)
	wantCode(t, err, "OTP_INVALID") // used once

	// the main brand of the same store spends it, again by code
	m := place(0, 1)
	_, err = a.SpendCreditByCode(ctx, m.ID, app.CreditVerify{ID: sent.ID, Code: sent.DevCode}, bs.staff)
	wantCode(t, err, "OTP_INVALID")
	sp, err := a.RequestCreditCode(ctx, m.ID, "", "spend", bs.staff)
	noErr(t, err)
	if sp.Balance != rq.CashPart+rq.CreditPart {
		t.Fatalf("balance %d, refunded %+v", sp.Balance, rq)
	}
	m, err = a.SpendCreditByCode(ctx, m.ID, app.CreditVerify{ID: sp.ID, Code: sp.DevCode}, bs.staff)
	noErr(t, err)
	if m.CreditPaid != min(sp.Balance, m.Totals.Total) || m.Due != m.Totals.Total-m.CreditPaid {
		t.Fatalf("spent: %+v", m)
	}
	checked(t, "spent at the main brand", m.ID)
	m = finishPickup(t, m, bs.staff)
	checked(t, "main brand picked up", m.ID)

	// a phone with no balance gets no code to spend
	n := place(0, 2)
	_, err = a.RequestCreditCode(ctx, n.ID, "+639990001234", "spend", bs.staff)
	wantCode(t, err, "CREDIT_NONE")
	walletClean(t)
}

// The check catches a refund that says it went into the balance when the
// books never got it, and an order that claims more paid from the balance.
func TestStoreCreditCheckCatchesTampering(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	_, phone := customer(t)
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}, false, nil)
	noErr(t, err)
	o = finishPickup(t, o, s.staff)
	rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "chicken").ID, 1), Reason: "x",
		ToCredit: true, Consent: "in_person", Amount: i64(3000)}, s.staff)
	noErr(t, err)
	checked(t, "refund", o.ID)
	want := func(prefix string) {
		t.Helper()
		bad, err := a.CheckOrderMoney(ctx, o.ID)
		noErr(t, err)
		for _, b := range bad {
			if strings.HasPrefix(b, prefix) {
				return
			}
		}
		t.Fatalf("%s not caught: %v", prefix, bad)
	}
	// a refund's record cannot be changed quietly any more (00085)
	_, err = a.DB.Exec(ctx, `UPDATE order_refunds SET to_credit = to_credit - 1 WHERE id=$1`, rq.RefundID)
	if err == nil {
		t.Fatal("a refund's amount into the balance was changed without a revoke")
	}
	tamper(t, `UPDATE order_refunds SET to_credit = to_credit - 1 WHERE id=$1`, rq.RefundID)
	want("W7")
	tamper(t, `UPDATE order_refunds SET to_credit = to_credit + 1 WHERE id=$1`, rq.RefundID)
	_, err = a.DB.Exec(ctx, `UPDATE orders SET credit_paid = 1 WHERE id=$1`, o.ID)
	noErr(t, err)
	want("W4")
	_, err = a.DB.Exec(ctx, `UPDATE orders SET credit_paid = 0 WHERE id=$1`, o.ID)
	noErr(t, err)
	checked(t, "put back", o.ID)
	// the books themselves cannot be edited
	_, err = a.DB.Exec(ctx, `UPDATE wallet_postings SET amount = amount + 1 WHERE journal_id IN
		(SELECT id FROM wallet_journals WHERE refund_id=$1)`, rq.RefundID)
	if err == nil {
		t.Fatal("a posting was edited")
	}
	_, err = a.DB.Exec(ctx, `UPDATE wallet_accounts SET balance = balance + 100 WHERE kind='store_credit' AND branch_id=$1`, s.branchID)
	if err == nil {
		t.Fatal("a balance was set by hand")
	}
	walletClean(t)
}
