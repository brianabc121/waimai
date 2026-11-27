package app_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// Stored value, "top up X, get Y free" (docs/STORED_VALUE_DESIGN.md,
// docs/PRICING_CHECK.md §3 row 20): every step that moves money is checked
// against the rules W1–W15.

// storedAt is what the customer holds at the store: refund balance,
// principal and bonus.
func storedAt(t *testing.T, customerID, branchID int64) app.StoreCredit {
	t.Helper()
	l, err := testApp.MyStoreCredits(context.Background(), customerID)
	noErr(t, err)
	for _, c := range l {
		if c.BranchID == branchID {
			return c
		}
	}
	return app.StoreCredit{}
}

// storedStore is a store in the Philippines that took the terms, with one
// campaign: pay 1,000 get 200 (and 2 coupons of 50), pay 300 get 30 for a
// first top-up only, and pay 6,000 get 1,000 (above the manager's limit).
type storedStore struct {
	*store
	owner             app.Actor
	big, first, large int64
	campaign          int64
}

func newStoredStore(t *testing.T) *storedStore {
	t.Helper()
	s := &storedStore{store: newStore(t, false)}
	ctx, a := context.Background(), testApp
	_, err := a.DB.Exec(ctx, `UPDATE branches SET country_code='PH', currency='PHP' WHERE id=$1`, s.branchID)
	noErr(t, err)
	noErr(t, a.SaveStoredCountry(ctx, app.AdminActor{ID: 1}, app.StoredCountry{CountryCode: "PH", Enabled: true, Cap: 5000000}))
	s.owner = ownerOf(t, s.branchID)
	in := app.StoredCampaignInput{Name: "充值送", Tiers: []app.StoredTierInput{
		{Pay: 100000, Bonus: 20000, CouponKind: app.CouponStoreFree, CouponValue: 5000, CouponQty: 2, CouponDays: 30},
		{Pay: 30000, Bonus: 3000, FirstOnly: true},
		{Pay: 600000, Bonus: 100000},
	}}
	_, err = a.SaveStoredCampaign(ctx, s.branchID, 0, in, s.owner)
	wantCode(t, err, "STORED_TERMS")
	noErr(t, a.AcceptStoredTerms(ctx, s.branchID, s.owner))
	s.campaign, err = a.SaveStoredCampaign(ctx, s.branchID, 0, in, s.owner)
	noErr(t, err)
	// a second campaign running at the same time is refused
	_, err = a.SaveStoredCampaign(ctx, s.branchID, 0, app.StoredCampaignInput{Name: "另一个",
		Tiers: []app.StoredTierInput{{Pay: 50000, Bonus: 5000}}}, s.owner)
	wantCode(t, err, "STORED_CAMPAIGN_OVERLAP")
	ov, err := a.StoredOverview(ctx, s.branchID)
	noErr(t, err)
	for _, tr := range ov.Campaigns[0].Tiers {
		switch tr.Pay {
		case 100000:
			s.big = tr.ID
		case 30000:
			s.first = tr.ID
		case 600000:
			s.large = tr.ID
		}
	}
	if s.big == 0 || s.first == 0 || s.large == 0 {
		t.Fatalf("tiers: %+v", ov.Campaigns[0].Tiers)
	}
	return s
}

// topupByCode is the customer's own code read out at the counter, paid in
// cash.
func (s *storedStore) topupByCode(t *testing.T, cust, tier int64) *app.Topup {
	t.Helper()
	return s.topupPaid(t, cust, tier, "cash")
}

func (s *storedStore) topupPaid(t *testing.T, cust, tier int64, payType string) *app.Topup {
	t.Helper()
	ctx, a := context.Background(), testApp
	tp, err := a.CreateTopup(ctx, s.slug, cust, app.TopupInput{TierID: tier})
	noErr(t, err)
	l, err := a.BranchTopups(ctx, s.branchID, "pending", tp.Code)
	noErr(t, err)
	if len(l) != 1 || l[0].ID != tp.ID {
		t.Fatalf("the counter finds the code: %+v", l)
	}
	tp, err = a.ConfirmTopup(ctx, s.branchID, tp.ID, app.TopupConfirm{PayType: payType}, s.owner)
	noErr(t, err)
	if tp.Status != "paid" {
		t.Fatalf("top-up %+v", tp)
	}
	return tp
}

// Row 20: top-ups both ways, spending with the refund balance and cash,
// add-on, void, discount, refunds, cancel.
func TestStoredValueFlow(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)

	// a refund balance of 50 first (docs/REFUND_BALANCE_DESIGN.md)
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}}}, false, nil)
	noErr(t, err)
	o = finishPickup(t, o, s.staff)
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "chicken").ID, 1), Reason: "x",
		ToCredit: true, Consent: "in_person", Amount: i64(5000)}, s.staff)
	noErr(t, err)
	checked(t, "refund balance", o.ID)

	// 1. the customer's own code: pay 300 get 30, first top-up only
	tp := s.topupByCode(t, cust, s.first)
	if tp.Pay != 30000 || tp.Bonus != 3000 {
		t.Fatalf("first top-up: %+v", tp)
	}
	_, err = a.ConfirmTopup(ctx, s.branchID, tp.ID, app.TopupConfirm{PayType: "cash"}, s.owner)
	wantCode(t, err, "TOPUP_NOT_PENDING")
	_, err = a.CreateTopup(ctx, s.slug, cust, app.TopupInput{TierID: s.first})
	wantCode(t, err, "STORED_TIER_CLOSED")
	if b := storedAt(t, cust, s.branchID); b.Credit != 5000 || b.Principal != 30000 || b.Bonus != 3000 || b.Balance != 38000 {
		t.Fatalf("after the first top-up: %+v", b)
	}
	walletClean(t)

	// 2. the staff by phone: pay 1,000 get 200 and 2 coupons, the
	// customer reads out the texted code
	st, err := a.StartStaffTopup(ctx, s.branchID, app.TopupInput{TierID: s.big, Phone: phone}, s.staff)
	noErr(t, err)
	if st.Balance.Principal != 30000 || st.Balance.Bonus != 3000 {
		t.Fatalf("the staff see %+v", st.Balance)
	}
	bad := "000000"
	if st.Verify.DevCode == bad {
		bad = "000001"
	}
	_, err = a.ConfirmTopup(ctx, s.branchID, st.Topup.ID, app.TopupConfirm{PayType: "cash",
		Verify: &app.CreditVerify{ID: st.Verify.ID, Code: bad}}, s.staff)
	wantCode(t, err, "OTP_INVALID")
	_, err = a.ConfirmTopup(ctx, s.branchID, st.Topup.ID, app.TopupConfirm{PayType: "cash",
		Verify: &app.CreditVerify{ID: st.Verify.ID, Code: st.Verify.DevCode}}, s.staff)
	noErr(t, err)
	_, err = a.ConfirmTopup(ctx, s.branchID, st.Topup.ID, app.TopupConfirm{PayType: "cash",
		Verify: &app.CreditVerify{ID: st.Verify.ID, Code: st.Verify.DevCode}}, s.staff)
	wantCode(t, err, "TOPUP_NOT_PENDING")
	cs, err := a.MyCoupons(ctx, cust)
	noErr(t, err)
	var given int
	for _, c := range cs {
		if c.BranchID != nil && *c.BranchID == s.branchID && c.Value == 5000 {
			given++
		}
	}
	if given != 2 {
		t.Fatalf("coupons given with the top-up: %d", given)
	}
	if b := storedAt(t, cust, s.branchID); b.Principal != 130000 || b.Bonus != 23000 || b.Balance != 158000 {
		t.Fatalf("after the second top-up: %+v", b)
	}

	// 3. above the manager's limit: the cashier needs an approval
	st, err = a.StartStaffTopup(ctx, s.branchID, app.TopupInput{TierID: s.large, Phone: phone}, s.staff)
	noErr(t, err)
	v := &app.CreditVerify{ID: st.Verify.ID, Code: st.Verify.DevCode}
	cashierID, err := a.CreateStaff(ctx, s.branchID, app.NewUser{Username: uniq("c"), Password: "password1", DisplayName: "C", Role: "cashier"})
	noErr(t, err)
	cashier := app.Actor{Type: "staff", ID: cashierID}
	_, err = a.ConfirmTopup(ctx, s.branchID, st.Topup.ID, app.TopupConfirm{PayType: "cash", Verify: v}, cashier)
	wantCode(t, err, "STORED_NEEDS_APPROVAL")
	// the country's cap: 50,000 a customer at a store
	noErr(t, a.SaveStoredCountry(ctx, app.AdminActor{ID: 1}, app.StoredCountry{CountryCode: "PH", Enabled: true, Cap: 700000}))
	approved := cashier
	approved.ApprovedBy = s.owner.ID
	_, err = a.ConfirmTopup(ctx, s.branchID, st.Topup.ID, app.TopupConfirm{PayType: "cash", Verify: v}, approved)
	wantCode(t, err, "STORED_CAP")
	noErr(t, a.SaveStoredCountry(ctx, app.AdminActor{ID: 1}, app.StoredCountry{CountryCode: "PH", Enabled: true, Cap: 5000000}))
	big, err := a.ConfirmTopup(ctx, s.branchID, st.Topup.ID, app.TopupConfirm{PayType: "cash", Verify: v}, approved)
	noErr(t, err)
	// 4. paid by mistake: undone the same day while unspent, once
	noErr(t, a.RevokeTopup(ctx, s.branchID, big.ID, "收错了", s.owner))
	wantCode(t, a.RevokeTopup(ctx, s.branchID, big.ID, "", s.owner), "TOPUP_NOT_REVOCABLE")
	if b := storedAt(t, cust, s.branchID); b.Principal != 130000 || b.Bonus != 23000 {
		t.Fatalf("after the revoke: %+v", b)
	}
	walletClean(t)

	// 5. spend: the refund balance first, then stored value split by its
	// share, bonus = floor(x × 23000 / 153000) (rule W9)
	lines := []app.QuoteLineInput{{ItemID: s.chicken, Qty: 4, OptionIDs: []int64{s.spiceMild}}, {ItemID: s.tea, Qty: 2}}
	q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: lines}, phone)
	noErr(t, err)
	if q.StoreCredit != 158000 || q.Credit != q.Totals.Total {
		t.Fatalf("checkout shows %d, uses %d of %d", q.StoreCredit, q.Credit, q.Totals.Total)
	}
	b, err := s.placeAs(t, phone, "pickup", lines, true, i64(q.Credit))
	noErr(t, err)
	T := b.Totals.Total
	if T >= 158000 {
		t.Fatalf("the order is meant to be paid from balances alone: %d", T)
	}
	x := T - 5000
	wantB := x * 23000 / 153000
	if b.CreditPaid != T || b.Due != 0 || b.StoredBonusPaid != wantB || b.StoredPrincipalPaid != x-wantB {
		t.Fatalf("placed: total %d paid %d principal %d bonus %d (bonus want %d)", T, b.CreditPaid, b.StoredPrincipalPaid,
			b.StoredBonusPaid, wantB)
	}
	if bal := storedAt(t, cust, s.branchID); bal.Credit != 0 || bal.Principal != 130000-(x-wantB) || bal.Bonus != 23000-wantB {
		t.Fatalf("left: %+v", bal)
	}
	checked(t, "placed with stored value", b.ID)
	b, err = a.Accept(ctx, b.ID, 0, s.staff)
	noErr(t, err)

	// 6. add-on: the balances paid stay, the rest is due
	res, err := a.StaffAddon(ctx, b.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}}, s.staff)
	noErr(t, err)
	b = res.Order
	if b.CreditPaid != T || b.Due != b.Totals.Total-T {
		t.Fatalf("add-on: %+v", b)
	}
	checked(t, "add-on", b.ID)

	// 7. a void and a discount shrink it below what the balances paid:
	// each balance gets back its share of what the order paid (brian
	// 2026-11-27): principal and refund balance rounded down, the bonus
	// takes the cents left over
	for _, step := range []string{"void", "discount"} {
		before := storedAt(t, cust, s.branchID)
		paid, pP, pB := b.CreditPaid, b.StoredPrincipalPaid, b.StoredBonusPaid
		pC := paid - pP - pB
		if step == "void" {
			r, err := a.VoidLine(ctx, itemByName(b, "tea").ID, app.VoidInput{Qty: 2, Reason: "不要了", ReasonKind: "customer"}, s.staff, false)
			noErr(t, err)
			b = r.Order
		} else {
			b, err = a.SetOrderAdjust(ctx, b.ID, 0, app.Adjust{PercentBP: 8000, Reason: "熟客"}, s.staff)
			noErr(t, err)
		}
		after := storedAt(t, cust, s.branchID)
		back := paid - b.CreditPaid
		if b.Totals.Total >= paid || b.CreditPaid != b.Totals.Total || b.Due != 0 || back <= 0 {
			t.Fatalf("%s: total %d paid %d (was %d)", step, b.Totals.Total, b.CreditPaid, paid)
		}
		wantP, wantC := back*pP/paid, back*pC/paid
		wantB := back - wantP - wantC
		if wantB > pB {
			t.Fatalf("%s: the bonus share %d is over what it paid %d", step, wantB, pB)
		}
		if after.Credit != before.Credit+wantC || after.Principal != before.Principal+wantP || after.Bonus != before.Bonus+wantB ||
			b.StoredPrincipalPaid != pP-wantP || b.StoredBonusPaid != pB-wantB {
			t.Fatalf("%s gave back %d (want %d + %d + %d): %+v -> %+v, order %+v", step, back, wantC, wantP, wantB, before, after, b)
		}
		checked(t, step, b.ID)
	}
	b = finishPickup(t, b, s.staff)
	if b.PaymentStatus != "paid" || b.PayType != "credit" {
		t.Fatalf("picked up: %s %s", b.PaymentStatus, b.PayType)
	}
	checked(t, "picked up", b.ID)

	// 8. refunds: one chicken in cash, then the rest into the balance. All
	// the stored value comes back exactly (W12), the refund balance's 50
	// too
	sp, sb := b.StoredPrincipalPaid, b.StoredBonusPaid
	chicken := itemByName(b, "chicken").ID
	rq, _, err := a.Refund(ctx, b.ID, app.RefundInput{Lines: refundLines(chicken, 1), Reason: "x", Method: "现金"}, s.staff)
	noErr(t, err)
	if rq.CashPart != 0 || rq.StoredPart <= 0 {
		t.Fatalf("a refund of an order paid from balances: %+v", rq)
	}
	checked(t, "refund one", b.ID)
	rest := append(refundLines(chicken, 3), refundLines(itemByName(b, "water").ID, 1)...)
	_, _, err = a.Refund(ctx, b.ID, app.RefundInput{Lines: rest, Reason: "x", ToCredit: true, Consent: "chat"}, s.staff)
	noErr(t, err)
	checked(t, "refund the rest", b.ID)
	if bal := storedAt(t, cust, s.branchID); bal.Principal != 130000 || bal.Bonus != 23000 || bal.Credit != 5000 {
		t.Fatalf("everything back (stored %d+%d): %+v", sp, sb, bal)
	}
	var prinBack, bonusBack int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT sum(principal_part), sum(bonus_part) FROM order_refunds WHERE order_id=$1`, b.ID).
		Scan(&prinBack, &bonusBack))
	if prinBack != sp || bonusBack != sb {
		t.Fatalf("refunded principal %d bonus %d of %d %d", prinBack, bonusBack, sp, sb)
	}
	walletClean(t)

	// 9. mixed with cash: an order bigger than all balances
	big2 := []app.QuoteLineInput{{ItemID: s.chicken, Qty: 9, OptionIDs: []int64{s.spiceMild}}}
	c, err := s.placeAs(t, phone, "pickup", big2, true, nil)
	noErr(t, err)
	if c.Totals.Total <= 158000 || c.CreditPaid != 158000 || c.StoredPrincipalPaid != 130000 || c.StoredBonusPaid != 23000 ||
		c.Due != c.Totals.Total-158000 {
		t.Fatalf("mixed: %+v", c)
	}
	checked(t, "mixed with cash", c.ID)
	// 10. cancelled: everything goes back where it came from
	c, err = a.Accept(ctx, c.ID, 0, s.staff)
	noErr(t, err)
	c, err = a.StaffCancel(ctx, c.ID, c.Version, "沽清", s.staff)
	noErr(t, err)
	if bal := storedAt(t, cust, s.branchID); bal.Principal != 130000 || bal.Bonus != 23000 || bal.Credit != 5000 {
		t.Fatalf("cancelled: %+v", bal)
	}
	checked(t, "cancelled", c.ID)

	// 11. once spent, a top-up cannot be undone
	d, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}}, true, nil)
	noErr(t, err)
	checked(t, "spent", d.ID)
	wantCode(t, a.RevokeTopup(ctx, s.branchID, tp.ID, "", s.owner), "TOPUP_SPENT")

	// another store, another currency: nothing to spend
	s2 := newStore(t, false)
	q2, err := a.CreateQuote(ctx, s2.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s2.tea, Qty: 1}}}, phone)
	noErr(t, err)
	if q2.StoreCredit != 0 {
		t.Fatalf("another store sees stored value: %d", q2.StoreCredit)
	}
	_, err = a.DB.Exec(ctx, `UPDATE branches SET currency='USD' WHERE id=$1`, s.branchID)
	noErr(t, err)
	qu, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s.water, Qty: 1}}}, phone)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE branches SET currency='PHP' WHERE id=$1`, s.branchID)
	noErr(t, err)
	if qu.StoreCredit != 0 {
		t.Fatalf("another currency sees stored value: %d", qu.StoreCredit)
	}

	// the country switched off: no more top-ups, what is held still spends
	noErr(t, a.SaveStoredCountry(ctx, app.AdminActor{ID: 1}, app.StoredCountry{CountryCode: "PH", Enabled: false, Cap: 5000000}))
	_, err = a.CreateTopup(ctx, s.slug, cust, app.TopupInput{TierID: s.big})
	noErr(t, a.SaveStoredCountry(ctx, app.AdminActor{ID: 1}, app.StoredCountry{CountryCode: "PH", Enabled: true, Cap: 5000000}))
	wantCode(t, err, "STORED_OFF")
	walletClean(t)
}

// §8.3, rule W14: paying stored value back. For the customer's own reasons
// the bonus used comes off; for the store's, all the principal left.
func TestStoredValueCashout(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp

	for _, reason := range []string{"customer", "store"} {
		cust, phone := customer(t)
		s.topupByCode(t, cust, s.big) // 1,000 + 200
		o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}, true, nil)
		noErr(t, err)
		checked(t, "spent", o.ID)
		x := o.CreditPaid
		bonus := x * 20000 / 120000
		if o.StoredBonusPaid != bonus || o.StoredPrincipalPaid != x-bonus {
			t.Fatalf("spent %d: %+v", x, o)
		}
		q, err := a.StoredCashoutQuote(ctx, s.branchID, cust, reason)
		noErr(t, err)
		left := 100000 - (x - bonus)
		// customer: what was left of the principal less the bonus used,
		// which is the 1,000 paid less all that was spent
		want := left
		if reason == "customer" {
			want = 100000 - x
		}
		if q.Principal != left || q.Bonus != 20000-bonus || q.BonusUsed != bonus || q.PaidOut != want || q.Clawback != left-want {
			t.Fatalf("%s quote: %+v (spent %d)", reason, q, x)
		}
		_, err = a.StoredCashout(ctx, s.branchID, cust, app.CashoutInput{Reason: reason, PayType: "cash", Consent: "in_person",
			Expect: i64(want + 1)}, s.owner)
		wantCode(t, err, "STORED_CHANGED")
		done, err := a.StoredCashout(ctx, s.branchID, cust, app.CashoutInput{Reason: reason, PayType: "cash", Consent: "in_person",
			Expect: i64(want)}, s.owner)
		noErr(t, err)
		if done.PaidOut != want {
			t.Fatalf("paid out %d, want %d", done.PaidOut, want)
		}
		if bal := storedAt(t, cust, s.branchID); bal.Principal != 0 || bal.Bonus != 0 {
			t.Fatalf("after paying back: %+v", bal)
		}
		_, err = a.StoredCashout(ctx, s.branchID, cust, app.CashoutInput{Reason: reason, PayType: "cash", Consent: "in_person"}, s.owner)
		wantCode(t, err, "STORED_NONE")
		// the order paid before is cancelled after the payback: what it
		// paid goes back into stored value, as it came (W12)
		o, err = a.Accept(ctx, o.ID, 0, s.staff)
		noErr(t, err)
		o, err = a.StaffCancel(ctx, o.ID, o.Version, "x", s.staff)
		noErr(t, err)
		if bal := storedAt(t, cust, s.branchID); bal.Principal != x-bonus || bal.Bonus != bonus {
			t.Fatalf("cancelled after paying back: %+v", bal)
		}
		checked(t, "cancelled after the payback", o.ID)
		walletClean(t)
	}
}

// Many orders at once on one stored balance: it is spent once.
func TestStoredValueConcurrentSpend(t *testing.T) {
	s := newStoredStore(t)
	ctx := context.Background()
	cust, phone := customer(t)
	s.topupByCode(t, cust, s.first) // 300 + 30
	var wg sync.WaitGroup
	got := make([]*app.Order, 5)
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
	var spent, p, b int64
	for _, o := range got {
		if o == nil {
			t.Fatal("an order failed")
		}
		spent += o.CreditPaid
		p += o.StoredPrincipalPaid
		b += o.StoredBonusPaid
		checked(t, "concurrent", o.ID)
	}
	if spent != 33000 || p != 30000 || b != 3000 || storedAt(t, cust, s.branchID).Balance != 0 {
		t.Fatalf("spent %d (%d + %d) of 33000", spent, p, b)
	}
	_ = ctx
	walletClean(t)
}

// §10: the reconciliation page and the bill. Top-ups are money taken in
// advance: in the drawer, not takings; what stored value paid is takings,
// its bonus the store's cost; paying back takes cash out of the drawer.
func TestStoredValueReconcile(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp
	c1, phone1 := customer(t)
	c2, _ := customer(t)
	c3, _ := customer(t)
	s.topupPaid(t, c1, s.big, "cash")   // 1,000 + 200
	s.topupPaid(t, c2, s.first, "card") // 300 + 30
	undone := s.topupPaid(t, c3, s.first, "cash")
	noErr(t, a.RevokeTopup(ctx, s.branchID, undone.ID, "收错了", s.owner))

	o, err := s.placeAs(t, phone1, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}, true, nil)
	noErr(t, err)
	x := o.Totals.Total
	b := x * 20000 / 120000
	if o.CreditPaid != x || o.StoredBonusPaid != b || o.Due != 0 {
		t.Fatalf("paid from stored value: %+v", o)
	}
	o = finishPickup(t, o, s.staff)
	checked(t, "picked up", o.ID)
	q, err := a.StoredCashout(ctx, s.branchID, c1, app.CashoutInput{Reason: "store", PayType: "cash", Consent: "in_person"}, s.owner)
	noErr(t, err)
	if q.PaidOut != 100000-(x-b) {
		t.Fatalf("paid back %d", q.PaidOut)
	}

	d := today()
	r, err := a.Reconciliation(ctx, s.branchID, nil, d.From, d.To)
	noErr(t, err)
	want := app.ReconStored{Count: 3, Paid: 160000, Bonus: 26000, Cash: 130000, Card: 30000, Revoked: 30000, RevokedCash: 30000,
		RevokedBonus: 3000, SpentPrincipal: x - b, SpentBonus: b, Cashouts: 1, PaidOut: q.PaidOut, CashoutCash: q.PaidOut}
	if r.Stored != want {
		t.Fatalf("stored block:\n got %+v\nwant %+v", r.Stored, want)
	}
	if r.Cash.StoredIn != 100000 || r.Cash.StoredOut != q.PaidOut || r.Cash.Expected != r.Cash.Counter+100000-q.PaidOut {
		t.Fatalf("drawer: %+v", r.Cash)
	}
	var row *app.ReconRow
	for i := range r.Rows {
		if r.Rows[i].PayType == "stored_value" {
			row = &r.Rows[i]
		}
		if r.Rows[i].PayType == "store_credit" {
			t.Fatalf("a refund balance row with none spent: %+v", r.Rows[i])
		}
	}
	if row == nil || row.Amount != x || row.Principal != x-b || row.Bonus != b || r.Total != x {
		t.Fatalf("takings: %+v, total %d", row, r.Total)
	}
	bs, err := a.BillSummary(ctx, s.branchID, nil, d.From, d.To)
	noErr(t, err)
	if bs.PaidByCredit != x || bs.StoredPrincipal != x-b || bs.StoredBonus != b || bs.StoredTopups != 130000 || bs.StoredGiven != 23000 {
		t.Fatalf("bill: %+v", bs)
	}
	walletClean(t)
}

// Broken numbers are caught: W11 (the order against the books), W13 (a
// refund against the books) and W8 (a top-up against the books).
func TestStoredValueCheckCatchesTampering(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)
	tp := s.topupByCode(t, cust, s.big)
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}, true, nil)
	noErr(t, err)
	if o.StoredPrincipalPaid == 0 || o.StoredBonusPaid == 0 {
		t.Fatalf("not paid from stored value: %+v", o)
	}
	o = finishPickup(t, o, s.staff)
	rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "chicken").ID, 1), Reason: "x", Method: "现金"}, s.staff)
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
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := a.DB.Exec(ctx, sql, args...)
		noErr(t, err)
	}
	exec(`UPDATE orders SET stored_principal_paid = stored_principal_paid - 1, stored_bonus_paid = stored_bonus_paid + 1 WHERE id=$1`, o.ID)
	want("W11")
	exec(`UPDATE orders SET stored_principal_paid = stored_principal_paid + 1, stored_bonus_paid = stored_bonus_paid - 1 WHERE id=$1`, o.ID)
	if _, err := a.DB.Exec(ctx, `UPDATE order_refunds SET principal_part = principal_part - 1 WHERE id=$1`, rq.RefundID); err == nil {
		t.Fatal("a refund's parts were changed")
	}
	tamper(t, `UPDATE order_refunds SET principal_part = principal_part - 1, bonus_part = bonus_part + 1 WHERE id=$1`, rq.RefundID)
	want("W13")
	tamper(t, `UPDATE order_refunds SET principal_part = principal_part + 1, bonus_part = bonus_part - 1 WHERE id=$1`, rq.RefundID)
	checked(t, "put back", o.ID)

	exec(`UPDATE topups SET bonus = bonus + 1 WHERE id=$1`, tp.ID)
	bad, err := a.CheckWallet(ctx)
	noErr(t, err)
	found := false
	for _, b := range bad {
		found = found || strings.HasPrefix(b, "W8")
	}
	exec(`UPDATE topups SET bonus = bonus - 1 WHERE id=$1`, tp.ID)
	if !found {
		t.Fatalf("W8 not caught: %v", bad)
	}
	walletClean(t)
}
