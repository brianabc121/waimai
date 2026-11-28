package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

// docs/CODE_REVIEW_FIX_PLAN.md, batch 4: the money and balance findings.

// M05: one person's daily limit holds when two refunds into balances are
// sent at the same moment, on two orders. Limit 30.00 a day, two refunds of
// 20.00: one goes in, the other needs the owner.
func TestCreditDailyLimitUnderConcurrency(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)
	noErr(t, a.SaveCreditLimits(ctx, s.branchID, app.CreditLimits{ApproveOver: 100000, DailyLimit: 3000}, ownerOf(t, s.branchID)))
	for round := 0; round < 3; round++ {
		orders := make([]*app.Order, 2)
		for i := range orders {
			o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}}}, false, nil)
			noErr(t, err)
			orders[i] = finishPickup(t, o, s.staff)
		}
		// each round starts with nothing refunded today
		tamper(t, `UPDATE order_refunds SET created_at = created_at - interval '2 days' WHERE branch_id=$1`, s.branchID)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, o := range orders {
			wg.Add(1)
			go func(i int, o *app.Order) {
				defer wg.Done()
				_, _, errs[i] = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "chicken").ID, 1), Reason: "x",
					ToCredit: true, Consent: "in_person", Amount: i64(2000)}, s.staff)
			}(i, o)
		}
		wg.Wait()
		ok := 0
		for _, err := range errs {
			if err == nil {
				ok++
			} else {
				wantCode(t, err, "CREDIT_DAILY_LIMIT")
			}
		}
		if ok != 1 || creditAt(t, cust, s.branchID) != int64(2000*(round+1)) {
			t.Fatalf("round %d: %d refunds went in, balance %d: %v", round, ok, creditAt(t, cust, s.branchID), errs)
		}
		for _, o := range orders {
			checked(t, "limit", o.ID)
		}
	}
	walletClean(t)
}

// M06: the last place of a tier goes once when two customers' top-ups are
// confirmed at the same moment.
func TestTopupLastPlaceUnderConcurrency(t *testing.T) {
	s := newStoredStore(t)
	ctx, a := context.Background(), testApp
	one := 1
	_, err := a.SaveStoredCampaign(ctx, s.branchID, s.campaign, app.StoredCampaignInput{Name: "充值送", Tiers: []app.StoredTierInput{
		{ID: s.big, Pay: 100000, Bonus: 20000},
		{ID: s.first, Pay: 30000, Bonus: 3000, FirstOnly: true},
		{ID: s.large, Pay: 600000, Bonus: 100000},
		{Pay: 50000, Bonus: 8000, Quota: &one},
	}}, s.owner)
	noErr(t, err)
	var tier int64
	noErr(t, a.DB.QueryRow(ctx, `SELECT id FROM stored_tiers WHERE campaign_id=$1 AND pay=50000`, s.campaign).Scan(&tier))
	for round := 0; round < 3; round++ {
		// a place is free again
		tamper(t, `UPDATE topups SET tier_id=$2 WHERE tier_id=$1 AND status='paid'`, tier, s.big)
		custs, tps := make([]int64, 2), make([]*app.Topup, 2)
		for i := range tps {
			custs[i], _ = customer(t)
			tps[i], err = a.CreateTopup(ctx, s.slug, custs[i], app.TopupInput{TierID: tier})
			noErr(t, err)
		}
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range tps {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = a.ConfirmTopup(ctx, s.branchID, tps[i].ID, app.TopupConfirm{PayType: "cash"}, s.owner)
			}(i)
		}
		wg.Wait()
		ok, held := 0, int64(0)
		for i, err := range errs {
			if err == nil {
				ok++
			} else {
				wantCode(t, err, "STORED_TIER_CLOSED")
			}
			held += storedAt(t, custs[i], s.branchID).Balance
		}
		// one top-up of 500 + 80, the other customer holds nothing
		if ok != 1 || held != 58000 {
			t.Fatalf("round %d: %d top-ups confirmed, %d held: %v", round, ok, held, errs)
		}
	}
	walletClean(t)
}

// M07, sample B-S8: a customer who holds stored value only (1,000.00 paid +
// 200.00 given, no refund balance) pays an order the staff took by phone,
// with the code texted to them. Two chicken (mild) to pick up, no coupon:
// 338.00.
//
//	the code's message shows  1,000.00 + 200.00 = 1,200.00
//	the bonus pays ⌊33,800 × 20,000 ÷ 120,000⌋ = ⌊5,633.33⌋ = 56.33
//	the principal the rest: 338.00 − 56.33 = 281.67
//	left: 1,000.00 − 281.67 = 718.33 and 200.00 − 56.33 = 143.67
//	nothing to pay at the counter
func TestBalanceSampleStoredValueByCode(t *testing.T) {
	s := newStoredStore(t)
	plainStore(t, s.store)
	ctx, a := context.Background(), testApp
	cust, phone := customer(t)
	s.topupByCode(t, cust, s.big)
	place := func(phone string, no int) *app.Order {
		out, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), app.StaffOrderInput{
			Member:      app.MemberInput{No: no, Phone: phone, Source: "wechat"},
			ServiceType: "pickup", Accept: true, Items: []app.StaffLine{{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}}}})
		noErr(t, err)
		return out.Order
	}
	o := place(phone, 1)
	if o.Totals.Total != 33800 {
		t.Fatalf("total %d", o.Totals.Total)
	}
	sp, err := a.RequestCreditCode(ctx, o.ID, "", "spend", s.staff)
	noErr(t, err)
	if sp.Balance != 120000 {
		t.Fatalf("the code is for %d", sp.Balance)
	}
	o, err = a.SpendCreditByCode(ctx, o.ID, app.CreditVerify{ID: sp.ID, Code: sp.DevCode}, s.staff)
	noErr(t, err)
	if o.CreditPaid != 33800 || o.StoredPrincipalPaid != 28167 || o.StoredBonusPaid != 5633 || o.Due != 0 {
		t.Fatalf("paid %d = %d + %d, due %d", o.CreditPaid, o.StoredPrincipalPaid, o.StoredBonusPaid, o.Due)
	}
	if b := storedAt(t, cust, s.branchID); b.Principal != 71833 || b.Bonus != 14367 || b.Credit != 0 {
		t.Fatalf("left %+v", b)
	}
	checked(t, "B-S8 paid by code", o.ID)

	// frozen stored value is not offered, like anywhere else
	_, err = a.AdminFreezeBranchCredits(ctx, app.AdminActor{ID: 1}, s.branchID, true, "check")
	noErr(t, err)
	n := place(phone, 1)
	_, err = a.RequestCreditCode(ctx, n.ID, "", "spend", s.staff)
	wantCode(t, err, "CREDIT_NONE")
	_, err = a.AdminFreezeBranchCredits(ctx, app.AdminActor{ID: 1}, s.branchID, false, "check")
	noErr(t, err)
	// someone with nothing at the store
	_, other := customer(t)
	_, err = a.RequestCreditCode(ctx, place(other, 2).ID, "", "spend", s.staff)
	wantCode(t, err, "CREDIT_NONE")
	walletClean(t)
}

// M01, sample S15: an order keeps the promotion's terms it was placed under.
// Pickup, tax 12% on top, service 5%, packaging 10.00; four waters at 30.00
// with "spend 90, 15 off":
//
//	4 waters: 120.00 − 15.00 = 105.00, service 5.25, packaging 10.00,
//	          tax 120.25 × 12% = 14.43, to pay 134.68
//	3 waters: 90.00 − 15.00 = 75.00, service 3.75, packaging 10.00,
//	          tax 88.75 × 12% = 10.65, to pay 99.40
//	the customer returns one water: 134.68 − 99.40 = 35.28
//
// whatever the store did to the promotion after the order: took 5.00 off
// instead of 15.00 (which would give 3 waters 111.16 and a refund of 23.52),
// raised its threshold to 200.00, or ended it.
func TestPricingSampleS15_OrderKeepsItsPromotion(t *testing.T) {
	ctx, a := context.Background(), testApp
	was := app.PromotionInput{Name: "m15", Kind: "amount_off", Threshold: 9000, Value: 1500, AppliesDelivery: true, AppliesPickup: true}
	for _, c := range []struct {
		name   string
		change func(t *testing.T, s *store, id int64)
	}{
		{"没改", func(*testing.T, *store, int64) {}},
		{"改成减 5", func(t *testing.T, s *store, id int64) {
			in := was
			in.Value = 500
			_, err := a.SavePromotion(ctx, s.branchID, id, in, s.staff)
			noErr(t, err)
		}},
		{"门槛提到 200", func(t *testing.T, s *store, id int64) {
			in := was
			in.Threshold = 20000
			_, err := a.SavePromotion(ctx, s.branchID, id, in, s.staff)
			noErr(t, err)
		}},
		{"活动结束", func(t *testing.T, s *store, id int64) {
			noErr(t, a.SetPromotionStatus(ctx, s.branchID, id, "ended", s.staff))
		}},
		{"又建了一个更大的活动", func(t *testing.T, s *store, id int64) {
			s.promo(t, app.PromotionInput{Name: "m40", Kind: "amount_off", Threshold: 6000, Value: 4000})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t, false)
			id := s.promo(t, was)
			o := finishPickup(t, s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 4}}), s.staff)
			checked(t, "picked up", o.ID)
			if o.Totals.Total != 13468 || o.Totals.Discount != 1500 {
				t.Fatalf("order %+v", o.Totals)
			}
			c.change(t, s, id)
			q, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(o.Items[0].ID, 1), ReasonKind: "customer", Dry: true}, s.staff)
			noErr(t, err)
			if q.Suggested != 3528 {
				t.Fatalf("suggested %d, want 3528", q.Suggested)
			}
			_, o, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(o.Items[0].ID, 1), ReasonKind: "customer", Method: "现金", Reason: "x"}, s.staff)
			noErr(t, err)
			checked(t, "refund", o.ID)
			if o.Refunded != 3528 {
				t.Fatalf("refunded %d", o.Refunded)
			}
		})
	}
}

// M01 at a table: a bill paid under "20% off" gives back a water (30.00)
// as 30.00 less 20% = 24.00, plus its 5% service 1.20 = 25.20 (the sample of
// TestPricingFixDineRefundPromo), also when the store has since started a
// larger offer: a refund does not move a paid bill to another promotion.
func TestDineRefundStaysWithItsPromotion(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	s.promo(t, app.PromotionInput{Scope: "dine_in", Kind: "percent_off", Value: 2000})
	tbl, err := a.SaveTable(ctx, s.branchID, 0, app.TableInput{Name: "R1", Active: true})
	noErr(t, err)
	bill, err := a.OpenTable(ctx, tbl, 2, nil, s.staff)
	noErr(t, err)
	bill, err = a.AddBatch(ctx, bill.SessionID, "staff", uniq("k"),
		batch(app.QuoteLineInput{ItemID: s.chicken, Qty: 2, OptionIDs: []int64{s.spiceMild}},
			app.QuoteLineInput{ItemID: s.water, Qty: 1}), s.staff)
	noErr(t, err)
	bill = finishKitchen(t, bill)
	o := bill.Order
	bill, err = a.CloseTable(ctx, bill.SessionID, app.CloseInput{ExpectedManifest: o.ManifestVersion,
		AmountReceived: o.Totals.Total, PayType: "cash"}, s.staff)
	noErr(t, err)
	o = bill.Order
	checked(t, "paid", o.ID)
	if o.Totals.Total != 30912 {
		t.Fatalf("bill %d", o.Totals.Total)
	}
	s.promo(t, app.PromotionInput{Scope: "dine_in", Name: "half", Kind: "percent_off", Value: 5000})
	water := itemByName(o, "water").ID
	for _, kind := range []string{"customer", "store"} {
		rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(water, 1), ReasonKind: kind, Dry: true}, s.staff)
		noErr(t, err)
		if rq.Suggested != 2520 {
			t.Fatalf("%s: suggested %d, want 2520", kind, rq.Suggested)
		}
	}
	_, _, err = a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(water, 1), ReasonKind: "customer", Method: "现金", Reason: "x"}, s.staff)
	noErr(t, err)
	checked(t, "refund", o.ID)
}

// reconToday is today's reconciliation of the whole store.
func reconToday(t *testing.T, s *store) *app.Reconcile {
	t.Helper()
	ctx, a := context.Background(), testApp
	b, err := a.BranchByID(ctx, s.branchID)
	noErr(t, err)
	loc, err := time.LoadLocation(b.Timezone)
	noErr(t, err)
	day := time.Now().In(loc).Format("2006-01-02")
	rc, err := a.Reconciliation(ctx, s.branchID, nil, day, day)
	noErr(t, err)
	return rc
}

func reconRow(rc *app.Reconcile, payType string) int64 {
	for _, r := range rc.Rows {
		if r.PayType == payType {
			return r.Amount
		}
	}
	return 0
}

// M02, sample S16 (rule P1): an order paid in advance, then a dish is added.
// Tax 12% on top, service 5%, packaging 10.00, delivery 69.00 (untaxed).
//
//	tea delivered, paid online when the staff took it:
//	  69.00 + 3.45 + 10.00 + tax 82.45 × 12% = 9.89 + 69.00 = 161.34
//	a water is added:
//	  99.00 + 4.95 + 10.00 + tax 113.95 × 12% = 13.67 + 69.00 = 196.62
//	still to collect: 196.62 − 161.34 = 35.28, by the rider, in cash
//	the day's takings: online 161.34, cash 35.28, together 196.62
//
// The same to pick up: 92.34 paid, 127.62 with the water, 35.28 at the counter.
func TestPricingSampleS16_PrepaidOrderAddon(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	gcash := payPH(t, s)
	place := func(service, phone string) *app.Order {
		in := app.StaffOrderInput{Member: app.MemberInput{Phone: phone, Source: "wechat", AddressLine: "1 Main St"},
			ServiceType: service, Accept: true, Items: []app.StaffLine{{ItemID: s.tea, Qty: 1}},
			Pay: &app.PayInput{Type: "online", ChannelID: gcash}}
		if service == "delivery" {
			in.ZoneID = &s.zoneID
		}
		out, err := a.PlaceStaffOrder(ctx, s.branchID, s.staff, uniq("k"), in)
		noErr(t, err)
		return out.Order
	}
	addWater := func(o *app.Order) *app.Order {
		r, err := a.StaffAddon(ctx, o.ID, uniq("k"), app.StaffAddonInput{Items: []app.StaffLine{{ItemID: s.water, Qty: 1}}}, s.staff)
		noErr(t, err)
		return r.Order
	}
	ready := func(o *app.Order) *app.Order {
		var err error
		for _, it := range o.Items {
			if it.PrepStatus != "cancelled" {
				o, err = a.SetItemStatus(ctx, o.ID, it.ID, "ready", app.Actor{Type: "system"})
				noErr(t, err)
			}
		}
		o, err = a.Pack(ctx, o.ID, 1, o.ManifestVersion, s.staff)
		noErr(t, err)
		return o
	}

	// delivery
	o := place("delivery", "+639181110001")
	if o.Totals.Total != 16134 || o.PaymentStatus != "paid" {
		t.Fatalf("prepaid: %d %s", o.Totals.Total, o.PaymentStatus)
	}
	checked(t, "S16 prepaid", o.ID)
	o = addWater(o)
	if o.Totals.Total != 19662 || o.PaymentStatus != "unpaid" || o.Due != 3528 || *o.AmountReceived != 16134 {
		t.Fatalf("after the add-on: total %d %s due %d received %d", o.Totals.Total, o.PaymentStatus, o.Due, *o.AmountReceived)
	}
	checked(t, "S16 add-on", o.ID)
	o = ready(o)
	o, err := a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	_, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider1)
	noErr(t, err)
	o, err = a.RiderDeliver(ctx, o.Delivery.ID, s.rider1, 3528)
	noErr(t, err)
	if o.PaymentStatus != "paid" || *o.AmountReceived != 19662 || o.PayType != "online" {
		t.Fatalf("delivered: %s received %d by %s", o.PaymentStatus, *o.AmountReceived, o.PayType)
	}
	checked(t, "S16 delivered", o.ID)
	rc := reconToday(t, s)
	if reconRow(rc, "online") != 16134 || reconRow(rc, "cash") != 3528 || rc.Total != 19662 || rc.Cash.TopUp != 3528 ||
		rc.Cash.RiderCollected != 3528 || rc.Cash.RiderPending != 3528 || rc.Cash.Counter != 0 || rc.Cash.RiderShort != 0 {
		t.Fatalf("delivery takings: rows %+v total %d cash %+v", rc.Rows, rc.Total, rc.Cash)
	}
	// all of it can be given back: it was all received
	q, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: append(refundLines(o.Items[0].ID, 1), refundLines(o.Items[1].ID, 1)...),
		ReasonKind: "store", Dry: true}, s.staff)
	noErr(t, err)
	if q.Received != 19662 || q.Suggested != 19662 {
		t.Fatalf("refundable %+v", q)
	}

	// the added dish is taken off again before the order leaves: paid as before
	o = place("delivery", "+639181110002")
	o = addWater(o)
	if o.PaymentStatus != "unpaid" || o.Due != 3528 {
		t.Fatalf("second: %s due %d", o.PaymentStatus, o.Due)
	}
	_, err = a.VoidLine(ctx, itemByName(o, "water").ID, app.VoidInput{ReasonKind: "customer", Reason: "不要了", ExpectedVersion: o.Version}, s.staff, true)
	noErr(t, err)
	o, err = a.GetOrder(ctx, o.ID)
	noErr(t, err)
	if o.Totals.Total != 16134 || o.PaymentStatus != "paid" || *o.AmountReceived != 16134 {
		t.Fatalf("after the void: total %d %s received %d", o.Totals.Total, o.PaymentStatus, *o.AmountReceived)
	}
	checked(t, "S16 add-on voided", o.ID)

	// pickup: 92.34 in advance, 127.62 with the water, 35.28 at the counter
	p := place("pickup", "+639181110003")
	if p.Totals.Total != 9234 {
		t.Fatalf("pickup %d", p.Totals.Total)
	}
	p = addWater(p)
	if p.Totals.Total != 12762 || p.PaymentStatus != "unpaid" || p.Due != 3528 {
		t.Fatalf("pickup add-on: total %d %s due %d", p.Totals.Total, p.PaymentStatus, p.Due)
	}
	p = ready(p)
	p, err = a.StaffHandoff(ctx, p.ID, s.staff)
	noErr(t, err)
	if p.PaymentStatus != "paid" || *p.AmountReceived != 12762 || p.PayType != "online" {
		t.Fatalf("picked up: %s received %d by %s", p.PaymentStatus, *p.AmountReceived, p.PayType)
	}
	checked(t, "S16 picked up", p.ID)
	rc = reconToday(t, s)
	// online 161.34 × 2 + 92.34; cash 35.28 + 35.28, of which 35.28 at the counter
	if reconRow(rc, "online") != 16134*2+9234 || reconRow(rc, "cash") != 7056 || rc.Total != 16134*2+9234+7056 ||
		rc.Cash.TopUp != 7056 || rc.Cash.Counter != 3528 || rc.Cash.RiderCollected != 3528 {
		t.Fatalf("takings: rows %+v total %d cash %+v", rc.Rows, rc.Total, rc.Cash)
	}
}

// M04 and Q1, sample S17 (rules P2, P3): an order the rider collected too
// little for. A tea delivered, 161.34 to collect, the rider brings 100.00.
//
//	the day: riders took 100.00, 61.34 is short, no takings yet (the order is unpaid)
//	"the rest came in": takings 161.34 in cash, of which 61.34 at the counter;
//	   the drawer expects 61.34 + the rider's 100.00 once handed in = 161.34;
//	   all 161.34 can be given back
//	"let it go" (another such order): takings 100.00, 61.34 let go; at most
//	   100.00 can be given back
func TestPricingSampleS17_ShortCollectionSettled(t *testing.T) {
	s := newStore(t, false)
	ctx, a := context.Background(), testApp
	tea := []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}
	cashOf := func(o *app.Order) *app.CashCollection {
		t.Helper()
		l, err := a.ListCash(ctx, s.branchID)
		noErr(t, err)
		for _, c := range l {
			if c.OrderID == o.ID {
				return c
			}
		}
		t.Fatal("no cash collection")
		return nil
	}
	o := deliverCollect(t, s, s.place(t, "delivery", tea), 10000)
	c := cashOf(o)
	if o.PaymentStatus != "unpaid" || c.Short != 6134 {
		t.Fatalf("short: %s %+v", o.PaymentStatus, c)
	}
	rc := reconToday(t, s)
	if rc.Total != 0 || rc.Cash.RiderCollected != 10000 || rc.Cash.RiderShort != 6134 || rc.Cash.Expected != 0 {
		t.Fatalf("before: total %d cash %+v", rc.Total, rc.Cash)
	}
	_, err := a.SettleShort(ctx, c.ID, "waived", "", s.staff)
	wantCode(t, err, "VALIDATION")
	c, err = a.SettleShort(ctx, c.ID, "collected", "顾客转给了前台", s.staff)
	noErr(t, err)
	if c.Short != 0 || c.LateCash != 6134 {
		t.Fatalf("settled: %+v", c)
	}
	_, err = a.SettleShort(ctx, c.ID, "collected", "", s.staff)
	wantCode(t, err, "INVALID_STATE")
	checked(t, "S17 rest collected", o.ID)
	_, err = a.Reconcile(ctx, c.ID, 10000, "", s.staff)
	noErr(t, err)
	rc = reconToday(t, s)
	if reconRow(rc, "cash") != 16134 || rc.Total != 16134 || rc.Cash.Counter != 6134 || rc.Cash.LateCash != 6134 ||
		rc.Cash.RiderReturned != 10000 || rc.Cash.RiderShort != 0 || rc.Cash.Expected != 16134 {
		t.Fatalf("collected: rows %+v total %d cash %+v", rc.Rows, rc.Total, rc.Cash)
	}
	q, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(o.Items[0].ID, 1), ReasonKind: "store", Dry: true}, s.staff)
	noErr(t, err)
	if q.Received != 16134 || q.Suggested != 16134 {
		t.Fatalf("refundable after the rest came in: %+v", q)
	}

	// the store lets the rest go
	w := deliverCollect(t, s, s.place(t, "delivery", tea), 10000)
	c, err = a.SettleShort(ctx, cashOf(w).ID, "waived", "老顾客，下次补", s.staff)
	noErr(t, err)
	if c.Short != 0 || c.Waived != 6134 {
		t.Fatalf("waived: %+v", c)
	}
	checked(t, "S17 rest let go", w.ID)
	rc = reconToday(t, s)
	// cash takings 161.34 + 100.00; the drawer: 61.34 at the counter + 100.00 handed in (the second rider's 100.00 not yet)
	if reconRow(rc, "cash") != 26134 || rc.Total != 26134 || rc.Cash.Waived != 6134 || rc.Cash.Counter != 6134 ||
		rc.Cash.RiderCollected != 20000 || rc.Cash.RiderPending != 10000 || rc.Cash.Expected != 16134 {
		t.Fatalf("waived: rows %+v total %d cash %+v", rc.Rows, rc.Total, rc.Cash)
	}
	q, _, err = a.Refund(ctx, w.ID, app.RefundInput{Lines: refundLines(w.Items[0].ID, 1), ReasonKind: "store", Dry: true}, s.staff)
	noErr(t, err)
	if q.Received != 10000 || q.Suggested != 10000 {
		t.Fatalf("refundable after letting go: %+v", q)
	}
	// the brands add up to the view (rule R)
	var brands int64
	for _, b := range rc.Brands {
		brands += b.Amount
	}
	if brands != rc.Total {
		t.Fatalf("brands %d, total %d", brands, rc.Total)
	}
}

// M04, sample S18 (rule P3): a refund put into the customer's balance and
// paid in cash after all leaves the drawer on the day it was changed. Two
// days ago 20.00 of a pickup went into the balance; today it is paid in cash.
//
//	two days ago: cash given back 0
//	today: cash given back 20.00
func TestPricingSampleS18_RefundCashDay(t *testing.T) {
	s := newStore(t, true)
	ctx, a := context.Background(), testApp
	_, phone := customer(t)
	o, err := s.placeAs(t, phone, "pickup", []app.QuoteLineInput{{ItemID: s.chicken, Qty: 1, OptionIDs: []int64{s.spiceMild}}}, false, nil)
	noErr(t, err)
	o = finishPickup(t, o, s.staff)
	rq, _, err := a.Refund(ctx, o.ID, app.RefundInput{Lines: refundLines(itemByName(o, "chicken").ID, 1), Reason: "x",
		ToCredit: true, Consent: "in_person", Amount: i64(2000)}, ownerOf(t, s.branchID))
	noErr(t, err)
	tamper(t, `UPDATE order_refunds SET created_at = created_at - interval '2 days' WHERE id=$1`, rq.RefundID)
	noErr(t, a.RevokeCreditRefund(ctx, rq.RefundID, s.staff))
	checked(t, "S18 paid in cash after all", o.ID)
	b, err := a.BranchByID(ctx, s.branchID)
	noErr(t, err)
	loc, err := time.LoadLocation(b.Timezone)
	noErr(t, err)
	today := time.Now().In(loc)
	then := today.AddDate(0, 0, -2).Format("2006-01-02")
	rc, err := a.Reconciliation(ctx, s.branchID, nil, then, then)
	noErr(t, err)
	if rc.Cash.Refunded != 0 || rc.Refunds.Total != 2000 {
		t.Fatalf("two days ago: %+v %+v", rc.Cash, rc.Refunds)
	}
	rc = reconToday(t, s)
	if rc.Cash.Refunded != 2000 {
		t.Fatalf("today: %+v", rc.Cash)
	}
	walletClean(t)
}
