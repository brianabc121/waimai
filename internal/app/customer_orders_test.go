package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

// A customer's own list of orders, ordering again, and the after-sales request.
func TestCustomerOrdersReorderAftersale(t *testing.T) {
	s := newStore(t, false)
	ctx := context.Background()
	a := testApp
	phone := freshPhone()
	code, err := a.RequestOTP(ctx, phone, "")
	noErr(t, err)
	session, _, err := a.VerifyOTP(ctx, phone, code)
	noErr(t, err)
	cu, err := a.CustomerBySession(ctx, session)
	noErr(t, err)

	place := func(lines []app.QuoteLineInput) (*app.Order, string) {
		t.Helper()
		q, err := a.CreateQuote(ctx, s.slug, app.QuoteInput{ServiceType: "pickup", Items: lines}, phone)
		noErr(t, err)
		p, err := a.PlaceOrder(ctx, s.slug, phone, uniq("k"), app.PlaceOrderInput{QuoteID: q.ID, CustomerName: "Ann"})
		noErr(t, err)
		o, err := a.OrderByToken(ctx, p.AccessToken)
		noErr(t, err)
		return o, p.AccessToken
	}
	list := func(tab, search string) []app.OrderCard {
		t.Helper()
		p, err := a.CustomerOrders(ctx, cu.ID, tab, search, 0)
		noErr(t, err)
		return p.Orders
	}

	done, doneToken := place(s.cart())
	waiting, waitingToken := place([]app.QuoteLineInput{{ItemID: s.water, Qty: 3}})

	// finish the first order
	done, err = a.Accept(ctx, done.ID, 0, s.staff)
	noErr(t, err)
	for _, it := range done.Items {
		if it.NeedsPrep {
			_, err = a.SetItemStatus(ctx, done.ID, it.ID, "ready", s.staff)
			noErr(t, err)
		}
	}
	done, err = a.GetOrder(ctx, done.ID)
	noErr(t, err)
	_, err = a.Pack(ctx, done.ID, 1, done.ManifestVersion, s.staff)
	noErr(t, err)
	_, err = a.StaffHandoff(ctx, done.ID, s.staff)
	noErr(t, err)

	all := list("all", "")
	if len(all) != 2 || all[0].Token != waitingToken || all[1].Token != doneToken {
		t.Fatalf("all: %+v", all)
	}
	if all[1].DisplayStatus != "picked_up" || !all[1].CanReview || all[1].ItemCount != 4 || all[1].Store.Slug != s.slug {
		t.Fatalf("finished card: %+v", all[1])
	}
	if got := list("active", ""); len(got) != 1 || got[0].Token != waitingToken || got[0].DisplayStatus != "pending_acceptance" {
		t.Fatalf("active: %+v", got)
	}
	if got := list("review", ""); len(got) != 1 || got[0].Token != doneToken {
		t.Fatalf("review: %+v", got)
	}
	if got := list("all", "chick"); len(got) != 1 || got[0].Token != doneToken {
		t.Fatalf("search by dish: %+v", got)
	}
	if got := list("all", "100%_no"); len(got) != 0 {
		t.Fatalf("search took the wildcard literally: %+v", got)
	}
	_, err = a.CustomerOrders(ctx, cu.ID, "paid", "", 0)
	wantCode(t, err, "VALIDATION")
	// nobody else sees them
	if p, err := a.CustomerOrders(ctx, cu.ID+100000, "all", "", 0); err != nil || len(p.Orders) != 0 {
		t.Fatalf("another customer: %+v %v", p, err)
	}

	// once reviewed it leaves the "to review" tab
	_, err = a.SubmitReview(ctx, doneToken, app.ReviewInput{Rating: 5, Taste: 5, Packaging: 4})
	noErr(t, err)
	if got := list("review", ""); len(got) != 0 {
		t.Fatalf("review after reviewing: %+v", got)
	}

	// order again: options come back by name; a dish no longer sold is reported
	_, err = a.DB.Exec(ctx, `UPDATE menu_items SET published=false WHERE id=$1`, s.tea)
	noErr(t, err)
	re, err := a.Reorder(ctx, doneToken)
	noErr(t, err)
	if re.Slug != s.slug || len(re.Lines) != 2 || len(re.Missing) != 1 || re.Missing[0].Name != "tea" {
		t.Fatalf("reorder: %+v", re)
	}
	if l := re.Lines[0]; l.ItemID != s.chicken || l.Qty != 2 || len(l.OptionIDs) != 1 || l.OptionIDs[0] != s.spiceMild {
		t.Fatalf("reorder line: %+v", l)
	}

	// after-sales: not before the store accepted, once per order, the store answers
	_, err = a.RequestAftersale(ctx, waitingToken, app.AftersaleInput{Reason: "late"})
	wantCode(t, err, "AFTERSALE_NOT_ALLOWED")
	_, err = a.RequestAftersale(ctx, doneToken, app.AftersaleInput{Reason: "nope"})
	wantCode(t, err, "VALIDATION")
	_, err = a.RequestAftersale(ctx, doneToken, app.AftersaleInput{Reason: "other"})
	wantCode(t, err, "VALIDATION")
	o, err := a.RequestAftersale(ctx, doneToken, app.AftersaleInput{Reason: "missing_item", Detail: " no tea in the bag "})
	noErr(t, err)
	if o.Aftersale == nil || o.Aftersale.Status != "open" || o.Aftersale.Detail != "no tea in the bag" {
		t.Fatalf("aftersale: %+v", o.Aftersale)
	}
	_, err = a.RequestAftersale(ctx, doneToken, app.AftersaleInput{Reason: "late"})
	wantCode(t, err, "AFTERSALE_EXISTS")
	if got := list("aftersale", ""); len(got) != 1 || got[0].Aftersale != "open" {
		t.Fatalf("aftersale tab: %+v", got)
	}
	// an unanswered request keeps the finished order on the store's desk
	desk, err := a.ListOrders(ctx, s.branchID, "active")
	noErr(t, err)
	found := false
	for _, d := range desk {
		found = found || d.ID == done.ID
	}
	if !found {
		t.Fatal("the order with an open request is not on the desk")
	}
	_, err = a.HandleAftersale(ctx, done.ID, "declined", "", s.staff)
	wantCode(t, err, "VALIDATION")
	_, err = a.HandleAftersale(ctx, waiting.ID, "resolved", "", s.staff)
	wantCode(t, err, "INVALID_STATE")
	o, err = a.HandleAftersale(ctx, done.ID, "resolved", "tea money returned in cash", s.staff)
	noErr(t, err)
	if o.Aftersale.Status != "resolved" || o.Aftersale.HandledAt == nil || o.Aftersale.Reply == "" {
		t.Fatalf("handled: %+v", o.Aftersale)
	}
	_, err = a.HandleAftersale(ctx, done.ID, "resolved", "", s.staff)
	wantCode(t, err, "INVALID_STATE")
	desk, err = a.ListOrders(ctx, s.branchID, "history")
	noErr(t, err)
	found = false
	for _, d := range desk {
		found = found || d.ID == done.ID
	}
	if !found {
		t.Fatal("the answered order did not go back to history")
	}
}
