package app_test

import (
	"context"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func staffOf(t *testing.T, branchID, id int64) app.StaffMember {
	t.Helper()
	list, err := testApp.ListStaff(context.Background(), branchID)
	noErr(t, err)
	for _, s := range list {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("staff %d not listed", id)
	return app.StaffMember{}
}

func userByLogin(t *testing.T, username string) *app.User {
	t.Helper()
	_, u, err := testApp.Login(context.Background(), username, "password1")
	noErr(t, err)
	return u
}

// A person can hold several posts; the old kitchen and rider roles map onto them.
func TestStaffPosts(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	fryer, err := a.SaveStation(ctx, s.branchID, 0, app.StationInput{Name: "Fryer", Active: true})
	noErr(t, err)
	wok, err := a.SaveStation(ctx, s.branchID, 0, app.StationInput{Name: "Wok", Active: true})
	noErr(t, err)

	name := uniq("z")
	id, err := a.CreateStaff(ctx, s.branchID, app.NewUser{Username: name, Password: "password1", DisplayName: "Zhang", Role: "staff",
		Posts: []app.PostInput{{Kind: app.PostStation, StationID: &fryer}, {Kind: app.PostStation, StationID: &wok},
			{Kind: app.PostStation, StationID: &wok}, {Kind: app.PostRider}}})
	noErr(t, err)
	u := userByLogin(t, name)
	if u.RoleIn(s.branchID) != "staff" || !u.InStore(s.branchID) || !u.HasPost(s.branchID, app.PostRider) || len(u.Roles[0].Posts) != 3 {
		t.Fatalf("posts not loaded: %+v", u.Roles)
	}
	if p := u.Roles[0].Posts[0]; p.Kind != app.PostStation || p.StationName != "Fryer" {
		t.Fatalf("stations should come first with their names: %+v", u.Roles[0].Posts)
	}

	// Legacy roles keep what they could do.
	kname := uniq("k")
	_, err = a.CreateStaff(ctx, s.branchID, app.NewUser{Username: kname, Password: "password1", DisplayName: "K", Role: "kitchen"})
	noErr(t, err)
	k := userByLogin(t, kname)
	if k.RoleIn(s.branchID) != "staff" || !k.HasPost(s.branchID, app.PostPacking) || !k.HasPost(s.branchID, app.PostStation) || k.HasPost(s.branchID, app.PostRider) {
		t.Fatalf("kitchen role should become staff with stations and packing: %+v", k.Roles)
	}
	if r := staffOf(t, s.branchID, s.rider1); r.Role != "staff" || len(r.Posts) != 1 || r.Posts[0].Kind != app.PostRider {
		t.Fatalf("rider role should become staff with the rider post: %+v", r)
	}

	// Riders are whoever holds the post, not a role.
	riders, err := a.ListRiders(ctx, s.branchID)
	noErr(t, err)
	got := map[int64]bool{}
	for _, r := range riders {
		got[r.ID] = true
	}
	if !got[id] || !got[s.rider1] || got[k.ID] {
		t.Fatalf("riders = %v", got)
	}

	// A station of another branch, or an unknown kind, is refused.
	other := newStore(t, true)
	foreign, err := a.SaveStation(ctx, other.branchID, 0, app.StationInput{Name: "X", Active: true})
	noErr(t, err)
	owner := userByLogin(t, s.ownerName)
	err = a.UpdateStaff(ctx, s.branchID, id, app.StaffPatch{Posts: &[]app.PostInput{{Kind: app.PostStation, StationID: &foreign}}}, owner)
	wantCode(t, err, "VALIDATION")
	err = a.UpdateStaff(ctx, s.branchID, id, app.StaffPatch{Posts: &[]app.PostInput{{Kind: "boss"}}}, owner)
	wantCode(t, err, "VALIDATION")
	if len(staffOf(t, s.branchID, id).Posts) != 3 {
		t.Fatal("a refused change must leave the posts alone")
	}
}

// Only a rider can be assigned and deliver, and the post stays while they carry an order.
func TestRiderPostGuardsDelivery(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	owner := userByLogin(t, s.ownerName)
	packer, err := a.CreateStaff(ctx, s.branchID, app.NewUser{Username: uniq("p"), Password: "password1", DisplayName: "P", Role: "staff",
		Posts: []app.PostInput{{Kind: app.PostPacking}}})
	noErr(t, err)

	o := s.place(t, "delivery", []app.QuoteLineInput{{ItemID: s.tea, Qty: 1}})
	_, err = a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	_, err = a.AssignRider(ctx, o.ID, packer, s.staff)
	wantCode(t, err, "VALIDATION")
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)

	// With a delivery in hand the post cannot be removed, nor the account disabled or deleted.
	none := []app.PostInput{}
	off := false
	wantCode(t, a.UpdateStaff(ctx, s.branchID, s.rider1, app.StaffPatch{Posts: &none}, owner), "STAFF_HAS_DELIVERIES")
	wantCode(t, a.UpdateStaff(ctx, s.branchID, s.rider1, app.StaffPatch{Active: &off}, owner), "STAFF_HAS_DELIVERIES")
	wantCode(t, a.DeleteStaff(ctx, s.branchID, s.rider1, owner), "STAFF_HAS_DELIVERIES")
	if m := staffOf(t, s.branchID, s.rider1); m.ActiveDeliveries != 1 {
		t.Fatalf("active deliveries = %d", m.ActiveDeliveries)
	}

	// After reassigning, the post can go, and the old rider can no longer act.
	_, err = a.AssignRider(ctx, o.ID, s.rider2, s.staff)
	noErr(t, err)
	noErr(t, a.UpdateStaff(ctx, s.branchID, s.rider1, app.StaffPatch{Posts: &none}, owner))
	_, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	wantCode(t, err, "VALIDATION")
}

func TestStaffEditRules(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	owner := userByLogin(t, s.ownerName)
	mname := uniq("m")
	mid, err := a.CreateStaff(ctx, s.branchID, app.NewUser{Username: mname, Password: "password1", DisplayName: "M", Role: "manager"})
	noErr(t, err)
	manager := userByLogin(t, mname)
	str := func(v string) *string { return &v }
	off := false

	// A manager cannot touch an owner or make one.
	wantCode(t, a.UpdateStaff(ctx, s.branchID, owner.ID, app.StaffPatch{DisplayName: str("x")}, manager), "OWNER_ONLY")
	wantCode(t, a.ResetStaffPassword(ctx, s.branchID, owner.ID, "password2", manager), "OWNER_ONLY")
	wantCode(t, a.DeleteStaff(ctx, s.branchID, owner.ID, manager), "OWNER_ONLY")
	wantCode(t, a.UpdateStaff(ctx, s.branchID, s.rider1, app.StaffPatch{Role: str("owner")}, manager), "OWNER_ONLY")
	_, err = a.CreateStaffBy(ctx, s.branchID, app.NewUser{Username: uniq("o"), Password: "password1", DisplayName: "O", Role: "owner"}, manager)
	wantCode(t, err, "OWNER_ONLY")

	// Nobody locks themselves out, and the store keeps an owner.
	wantCode(t, a.UpdateStaff(ctx, s.branchID, owner.ID, app.StaffPatch{Role: str("manager")}, owner), "VALIDATION")
	wantCode(t, a.UpdateStaff(ctx, s.branchID, owner.ID, app.StaffPatch{Active: &off}, owner), "VALIDATION")
	wantCode(t, a.DeleteStaff(ctx, s.branchID, owner.ID, owner), "VALIDATION")
	noErr(t, a.UpdateStaff(ctx, s.branchID, mid, app.StaffPatch{Role: str("owner")}, owner))
	second := userByLogin(t, mname)
	wantCode(t, a.UpdateStaff(ctx, s.branchID, owner.ID, app.StaffPatch{Role: str("bogus")}, second), "VALIDATION")
	noErr(t, a.UpdateStaff(ctx, s.branchID, owner.ID, app.StaffPatch{Role: str("manager")}, second))
	wantCode(t, a.UpdateStaff(ctx, s.branchID, mid, app.StaffPatch{Role: str("cashier")}, second), "VALIDATION")

	// Details, username clash, and a password reset that signs the person out.
	rname := uniq("r")
	noErr(t, a.UpdateStaff(ctx, s.branchID, s.rider1, app.StaffPatch{Username: str(rname), DisplayName: str("Juan"), Phone: str("0917")}, second))
	wantCode(t, a.UpdateStaff(ctx, s.branchID, s.rider2, app.StaffPatch{Username: str(rname)}, second), "USERNAME_TAKEN")
	wantCode(t, a.UpdateStaff(ctx, s.branchID, s.rider2, app.StaffPatch{DisplayName: str(" ")}, second), "VALIDATION")
	if m := staffOf(t, s.branchID, s.rider1); m.Username != rname || m.DisplayName != "Juan" || m.Phone != "0917" {
		t.Fatalf("details not saved: %+v", m)
	}
	tok, _, err := a.Login(ctx, rname, "password1")
	noErr(t, err)
	noErr(t, a.ResetStaffPassword(ctx, s.branchID, s.rider1, "password9", second))
	if _, err := a.UserBySession(ctx, tok); err == nil {
		t.Fatal("a reset should end the person's sessions")
	}
	_, _, err = a.Login(ctx, rname, "password1")
	wantCode(t, err, "BAD_CREDENTIALS")
	_, _, err = a.Login(ctx, rname, "password9")
	noErr(t, err)

	// Deleting hides the person, frees the username and keeps other stores' people out of reach.
	noErr(t, a.DeleteStaff(ctx, s.branchID, s.rider1, second))
	_, _, err = a.Login(ctx, rname, "password9")
	wantCode(t, err, "BAD_CREDENTIALS")
	list, err := a.ListStaff(ctx, s.branchID)
	noErr(t, err)
	for _, m := range list {
		if m.ID == s.rider1 {
			t.Fatal("deleted staff still listed")
		}
	}
	_, err = a.CreateStaff(ctx, s.branchID, app.NewUser{Username: rname, Password: "password1", DisplayName: "New", Role: "staff"})
	noErr(t, err)
	other := newStore(t, true)
	wantCode(t, a.UpdateStaff(ctx, s.branchID, other.rider1, app.StaffPatch{DisplayName: str("x")}, second), "NOT_FOUND")
	wantCode(t, a.DeleteStaff(ctx, s.branchID, other.rider1, second), "NOT_FOUND")
}
