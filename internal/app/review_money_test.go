package app_test

import (
	"context"
	"sync"
	"testing"

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
