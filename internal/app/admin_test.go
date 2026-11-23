package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func adminActor(t *testing.T) app.AdminActor {
	t.Helper()
	ctx := context.Background()
	noErr(t, testApp.EnsurePlatformAdmin(ctx, "admin-apptest", "password1"))
	_, u, err := testApp.Login(ctx, "admin-apptest", "password1")
	noErr(t, err)
	return app.AdminActor{ID: u.ID, IP: "203.0.113.9"}
}

func merchantOf(t *testing.T, branchID int64) (id int64, name string) {
	t.Helper()
	b, err := testApp.AdminBranchByID(context.Background(), branchID)
	noErr(t, err)
	return b.MerchantID, b.MerchantName
}

func auditCount(t *testing.T, action string) int {
	t.Helper()
	l, err := testApp.ListAdminAudit(context.Background(), app.AuditFilter{Action: action})
	noErr(t, err)
	return l.Total
}

func TestAdminSuspendDeleteRestore(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	actor := adminActor(t)
	mid, name := merchantOf(t, s.branchID)
	pending := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	tea := app.QuoteInput{ServiceType: "pickup", Items: []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}}}

	// an active merchant cannot be deleted
	wantCode(t, a.DeleteMerchant(ctx, actor, mid, name), "INVALID_STATE")

	// suspended: the store is still visible but takes no orders
	noErr(t, a.SuspendMerchant(ctx, actor, mid, "papers missing"))
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if !sf.Branch.Paused || !sf.Branch.Suspended {
		t.Fatal("a suspended merchant's storefront should show as not taking orders")
	}
	_, err = a.CreateQuote(ctx, s.slug, tea, "")
	wantCode(t, err, "STORE_CLOSED")
	// the store's own pause switch is untouched, so resuming restores it exactly
	b, err := a.BranchByID(ctx, s.branchID)
	noErr(t, err)
	if b.Paused {
		t.Fatal("suspending must not flip the store's own pause switch")
	}
	// staff can still finish what is in progress
	_, err = a.Accept(ctx, pending.ID, pending.Version, s.staff)
	noErr(t, err)

	// delete: the name must match and nothing may be in progress
	wantCode(t, a.DeleteMerchant(ctx, actor, mid, "wrong name"), "VALIDATION")
	wantCode(t, a.DeleteMerchant(ctx, actor, mid, name), "ORDERS_IN_PROGRESS")
	o, err := a.GetOrder(ctx, pending.ID)
	noErr(t, err)
	_, err = a.StaffCancel(ctx, o.ID, o.Version, "test", s.staff)
	noErr(t, err)
	owner := uniq("own")
	_, err = a.CreateStaff(ctx, s.branchID, app.NewUser{Username: owner, Password: "password1", DisplayName: "O", Role: "manager"})
	noErr(t, err)
	token, _, err := a.Login(ctx, owner, "password1")
	noErr(t, err)
	noErr(t, a.DeleteMerchant(ctx, actor, mid, name))

	_, err = a.Storefront(ctx, s.slug)
	wantCode(t, err, "NOT_FOUND")
	_, err = a.CreateQuote(ctx, s.slug, tea, "")
	wantCode(t, err, "NOT_FOUND")
	if _, err := a.UserBySession(ctx, token); err == nil {
		t.Fatal("staff of a deleted merchant should be signed out")
	}
	_, u, err := a.Login(ctx, owner, "password1")
	noErr(t, err)
	if len(u.Roles) != 0 {
		t.Fatalf("staff of a deleted merchant should have no store, got %v", u.Roles)
	}
	// the slug stays taken
	_, err = a.CreateMerchant(ctx, app.NewMerchant{MerchantName: "X", BranchName: "X", Slug: s.slug,
		Owner: app.NewUser{Username: uniq("o"), Password: "password1", DisplayName: "O"}})
	wantCode(t, err, "SLUG_TAKEN")
	l, err := a.ListMerchants(ctx, app.MerchantFilter{Q: s.slug})
	noErr(t, err)
	if l.Total != 0 || l.Counts["deleted"] != 1 {
		t.Fatalf("a deleted merchant only shows under the deleted filter: %+v", l)
	}
	l, err = a.ListMerchants(ctx, app.MerchantFilter{Q: s.slug, Status: "deleted"})
	noErr(t, err)
	if len(l.Items) != 1 || l.Items[0].ID != mid || l.Items[0].Status != "deleted" {
		t.Fatalf("deleted filter: %+v", l)
	}

	// restore comes back suspended; resume reopens the store
	noErr(t, a.RestoreMerchant(ctx, actor, mid))
	wantCode(t, a.RestoreMerchant(ctx, actor, mid), "INVALID_STATE")
	_, err = a.CreateQuote(ctx, s.slug, tea, "")
	wantCode(t, err, "STORE_CLOSED")
	noErr(t, a.ResumeMerchant(ctx, actor, mid))
	_, err = a.CreateQuote(ctx, s.slug, tea, "")
	noErr(t, err)
	_, u, err = a.Login(ctx, owner, "password1")
	noErr(t, err)
	if u.RoleIn(s.branchID) != "manager" {
		t.Fatal("restoring a merchant should give its staff their store back")
	}

	d, err := a.GetMerchant(ctx, mid)
	noErr(t, err)
	if d.Merchant.Status != "active" || len(d.Branches) != 1 || d.Stats.Orders7d != 1 || d.Stats.OrdersToday["pickup"] != 1 {
		t.Fatalf("merchant detail: %+v %+v", d.Merchant, d.Stats)
	}
}

func TestAdminBranchesAndAccounts(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	actor := adminActor(t)
	mid, _ := merchantOf(t, s.branchID)
	d, err := a.GetMerchant(ctx, mid)
	noErr(t, err)
	var ownerID int64
	var ownerName string
	for _, u := range d.Users {
		if u.Roles[0].Role == "owner" {
			ownerID, ownerName = u.ID, u.Username
		}
	}

	// a new branch: the slug is unique and the owner gets it
	slug2 := uniq("b")
	_, err = a.CreateBranch(ctx, actor, mid, app.BranchInput{Name: "Second", Slug: s.slug})
	wantCode(t, err, "SLUG_TAKEN")
	_, err = a.CreateBranch(ctx, actor, mid, app.BranchInput{Name: "Second", Slug: "Bad Slug"})
	wantCode(t, err, "VALIDATION")
	b2, err := a.CreateBranch(ctx, actor, mid, app.BranchInput{Name: "Second", Slug: slug2})
	noErr(t, err)
	_, u, err := a.Login(ctx, ownerName, "password1")
	noErr(t, err)
	if u.RoleIn(b2) != "owner" {
		t.Fatal("the merchant's owner should own a new branch")
	}

	// renaming the link: the old one stops working
	slug3 := uniq("c")
	wantCode(t, a.UpdateBranch(ctx, actor, b2, app.BranchInput{Name: "Second", Slug: s.slug}), "SLUG_TAKEN")
	noErr(t, a.UpdateBranch(ctx, actor, b2, app.BranchInput{Name: "Second", Slug: slug3, Timezone: "Asia/Singapore", Currency: "sgd"}))
	_, err = a.Storefront(ctx, slug2)
	wantCode(t, err, "NOT_FOUND")
	sf, err := a.Storefront(ctx, slug3)
	noErr(t, err)
	if sf.Branch.Currency != "SGD" || sf.Branch.Timezone != "Asia/Singapore" {
		t.Fatalf("branch update not applied: %+v", sf.Branch)
	}
	// once the store's country is known, its currency follows the country
	_, err = a.SaveLocation(ctx, b2, app.LocationInput{Lat: 14.55, Lng: 121.02, Region: &app.Region{CountryCode: "PH"}})
	noErr(t, err)
	noErr(t, a.UpdateBranch(ctx, actor, b2, app.BranchInput{Name: "Second", Slug: slug3, Timezone: "Asia/Singapore", Currency: "USD"}))
	sf, err = a.Storefront(ctx, slug3)
	noErr(t, err)
	if sf.Branch.Currency != "PHP" {
		t.Fatalf("typed currency won over the store's country: %q", sf.Branch.Currency)
	}

	// deleting a branch hides it and takes it off the owner's list
	noErr(t, a.DeleteBranch(ctx, actor, b2))
	_, err = a.Storefront(ctx, slug3)
	wantCode(t, err, "NOT_FOUND")
	wantCode(t, a.DeleteBranch(ctx, actor, b2), "NOT_FOUND")
	_, u, err = a.Login(ctx, ownerName, "password1")
	noErr(t, err)
	if u.RoleIn(b2) != "" || u.RoleIn(s.branchID) != "owner" {
		t.Fatalf("roles after deleting a branch: %+v", u.Roles)
	}
	// a branch with an order in progress stays
	s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	wantCode(t, a.DeleteBranch(ctx, actor, s.branchID), "ORDERS_IN_PROGRESS")

	// reset password: new one works, old one and old sessions do not
	token, _, err := a.Login(ctx, ownerName, "password1")
	noErr(t, err)
	pw, err := a.AdminResetPassword(ctx, actor, ownerID)
	noErr(t, err)
	if len(pw) < 12 {
		t.Fatalf("generated password is too short: %q", pw)
	}
	if _, err := a.UserBySession(ctx, token); err == nil {
		t.Fatal("a password reset should sign the account out")
	}
	_, _, err = a.Login(ctx, ownerName, "password1")
	wantCode(t, err, "BAD_CREDENTIALS")
	_, _, err = a.Login(ctx, ownerName, pw)
	noErr(t, err)

	// disable and enable
	noErr(t, a.AdminSetUserActive(ctx, actor, ownerID, false))
	_, _, err = a.Login(ctx, ownerName, pw)
	wantCode(t, err, "BAD_CREDENTIALS")
	noErr(t, a.AdminSetUserActive(ctx, actor, ownerID, true))
	_, _, err = a.Login(ctx, ownerName, pw)
	noErr(t, err)

	// a new account through the back office
	mgr := uniq("m")
	_, err = a.AdminCreateUser(ctx, actor, s.branchID, app.NewUser{Username: mgr, Password: "password1", DisplayName: "M", Role: "manager"})
	noErr(t, err)
	_, err = a.AdminCreateUser(ctx, actor, b2, app.NewUser{Username: uniq("m"), Password: "password1", DisplayName: "M", Role: "manager"})
	wantCode(t, err, "NOT_FOUND")

	// platform administrators are not managed from here
	wantCode(t, a.AdminSetUserActive(ctx, actor, actor.ID, false), "NOT_FOUND")
	_, err = a.AdminResetPassword(ctx, actor, actor.ID)
	wantCode(t, err, "NOT_FOUND")
}

func TestAdminMenu(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	actor := adminActor(t)
	mid, _ := merchantOf(t, s.branchID)
	before := auditCount(t, "item.")

	l, err := a.SearchItems(ctx, app.ItemFilter{MerchantID: mid})
	noErr(t, err)
	if l.Total != 4 || l.Counts["on_sale"] != 4 || l.Counts["sold_out"] != 0 || len(l.Items) != 4 {
		t.Fatalf("search by merchant: %+v", l.Counts)
	}
	l, err = a.SearchItems(ctx, app.ItemFilter{MerchantID: mid, Q: "CHICK"})
	noErr(t, err)
	if l.Total != 1 || l.Items[0].ID != s.chicken || l.Items[0].GroupCount != 1 {
		t.Fatalf("search by name: %+v", l.Items)
	}
	// a literal % is not a wildcard
	l, err = a.SearchItems(ctx, app.ItemFilter{MerchantID: mid, Q: "%"})
	noErr(t, err)
	if l.Total != 0 {
		t.Fatalf("%% should match nothing, got %d", l.Total)
	}

	// edits go through the merchant's own rules and are recorded
	menu, err := a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	catID := menu.Categories[0].ID
	_, err = a.AdminSaveItem(ctx, actor, s.branchID, s.tea, app.ItemInput{CategoryID: catID, Name: "tea", Price: -1})
	wantCode(t, err, "VALIDATION")
	_, err = a.AdminSaveItem(ctx, actor, s.branchID, s.tea, app.ItemInput{CategoryID: catID, Name: "milk tea", Price: 7900,
		Published: true, NeedsPrep: true, DeliveryAvailable: true, PickupAvailable: true})
	noErr(t, err)
	sold := true
	noErr(t, a.AdminPatchItem(ctx, actor, s.branchID, s.water, app.ItemPatch{SoldOut: &sold}))
	n, err := a.AdminBulkItems(ctx, actor, s.branchID, []int64{s.chicken, s.limited, 999999999}, "unpublish")
	noErr(t, err)
	if n != 2 {
		t.Fatalf("bulk unpublish changed %d items, want 2", n)
	}
	_, err = a.AdminBulkItems(ctx, actor, s.branchID, []int64{s.chicken}, "explode")
	wantCode(t, err, "VALIDATION")
	l, err = a.SearchItems(ctx, app.ItemFilter{BranchID: s.branchID, Status: "off"})
	noErr(t, err)
	if l.Total != 2 || l.Counts["sold_out"] != 1 || l.Counts["on_sale"] != 2 {
		t.Fatalf("after edits: %+v", l.Counts)
	}
	sf, err := a.Storefront(ctx, s.slug)
	noErr(t, err)
	if len(sf.Categories[0].Items) != 2 || sf.Categories[0].Items[0].Name != "milk tea" {
		t.Fatalf("customers should see the edited menu: %+v", sf.Categories[0].Items)
	}
	// another merchant's item cannot be reached through this branch
	other := newStore(t, true)
	wantCode(t, a.AdminPatchItem(ctx, actor, s.branchID, other.tea, app.ItemPatch{SoldOut: &sold}), "NOT_FOUND")

	// a category can go once it is empty
	wantCode(t, a.AdminArchiveCategory(ctx, actor, s.branchID, catID), "INVALID_STATE")
	_, err = a.AdminBulkItems(ctx, actor, s.branchID, []int64{s.chicken, s.tea, s.water, s.limited}, "archive")
	noErr(t, err)
	noErr(t, a.AdminArchiveCategory(ctx, actor, s.branchID, catID))
	wantCode(t, a.AdminArchiveCategory(ctx, actor, s.branchID, catID), "NOT_FOUND")
	menu, err = a.AdminMenu(ctx, s.branchID)
	noErr(t, err)
	if len(menu.Categories) != 0 {
		t.Fatal("an archived category should leave the menu")
	}
	_, err = a.AdminSaveItem(ctx, actor, s.branchID, 0, app.ItemInput{CategoryID: catID, Name: "x", Price: 100})
	wantCode(t, err, "VALIDATION")

	// item.update x2 (save, patch), item.bulk_unpublish, item.bulk_archive
	if got := auditCount(t, "item.") - before; got != 4 {
		t.Fatalf("want 4 audit entries for item changes, got %d", got)
	}
	al, err := a.ListAdminAudit(ctx, app.AuditFilter{AdminID: actor.ID, Action: "category.delete", Page: app.Page{PageSize: 1}})
	noErr(t, err)
	if len(al.Items) != 1 || al.Items[0].AdminName != "admin-apptest" || al.Items[0].IP != "203.0.113.9" || *al.Items[0].TargetID != catID {
		t.Fatalf("audit entry: %+v", al.Items)
	}
}
