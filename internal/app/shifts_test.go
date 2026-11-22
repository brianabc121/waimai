package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

func shiftsOf(t *testing.T, branchID, userID int64) []app.Shift {
	t.Helper()
	day := time.Now().In(time.FixedZone("", 8*3600))
	at, err := testApp.Attendance(context.Background(), branchID, day.AddDate(0, 0, -3).Format("2006-01-02"), day.AddDate(0, 0, 1).Format("2006-01-02"))
	noErr(t, err)
	var out []app.Shift
	for _, s := range at.Shifts {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	return out
}

// Everyone clocks on and off with the same switch; each time is kept. A
// rider's switch and the general one are the same shift.
func TestShiftsClockOnAndOff(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	_, err := a.SaveLocation(ctx, s.branchID, app.LocationInput{Lat: 14.5547, Lng: 121.0244})
	noErr(t, err)
	cookName := uniq("cook")
	cook, err := a.CreateStaff(ctx, s.branchID, app.NewUser{Username: cookName, Password: "password1", DisplayName: "Cook", Role: "staff",
		Posts: []app.PostInput{{Kind: app.PostPacking}}})
	noErr(t, err)
	cookUser := userByLogin(t, cookName)

	// someone from another store cannot clock on here
	other := newStore(t, true)
	_, err = a.SetShift(ctx, cookUser, other.branchID, true, "", nil)
	wantCode(t, err, "FORBIDDEN")

	// in-store staff clock on at the store: the distance is a few metres
	st, err := a.SetShift(ctx, cookUser, s.branchID, true, "tok", &app.LatLng{14.5548, 121.0244})
	noErr(t, err)
	if !st.OnDuty || st.BranchID == nil || *st.BranchID != s.branchID {
		t.Fatalf("clock on: %+v", st)
	}
	// a second tap changes nothing
	_, err = a.SetShift(ctx, cookUser, s.branchID, true, "tok", nil)
	noErr(t, err)
	sh := shiftsOf(t, s.branchID, cook)
	if len(sh) != 1 || sh[0].EndedAt != nil || sh[0].StartSource != "manual" || sh[0].StartDistanceM == nil || *sh[0].StartDistanceM > 30 {
		t.Fatalf("open shift: %+v", sh)
	}
	if m := staffOf(t, s.branchID, cook); !m.OnDuty {
		t.Fatal("the staff list should show the cook at work")
	}
	// they are not a rider: no rider status appears
	if rs, _ := a.RiderStatus(ctx, cook); rs.OnDuty {
		t.Fatal("a packer must not show as a rider on duty")
	}
	// clocking off two kilometres away is recorded, not refused
	st, err = a.SetShift(ctx, cookUser, s.branchID, false, "tok", &app.LatLng{14.5727, 121.0244})
	noErr(t, err)
	sh = shiftsOf(t, s.branchID, cook)
	if st.OnDuty || len(sh) != 1 || sh[0].EndedAt == nil || sh[0].EndReason != "manual" || *sh[0].EndDistanceM < 1900 {
		t.Fatalf("closed shift: %+v %+v", st, sh)
	}
	if m := staffOf(t, s.branchID, cook); m.OnDuty {
		t.Fatal("the cook left")
	}

	// the rider's own switch opens and closes a shift too
	rs, err := a.SetDuty(ctx, s.rider1, true, "")
	noErr(t, err)
	if !rs.OnDuty {
		t.Fatal("rider on duty")
	}
	if st, _ := a.ShiftState(ctx, s.rider1); !st.OnDuty {
		t.Fatal("the rider's shift should be open")
	}
	_, err = a.SetDuty(ctx, s.rider1, false, "")
	noErr(t, err)
	sh = shiftsOf(t, s.branchID, s.rider1)
	if len(sh) != 1 || sh[0].EndedAt == nil {
		t.Fatalf("rider shift: %+v", sh)
	}
	// and the general switch puts a rider on duty
	riders, err := a.ListRiders(ctx, s.branchID)
	noErr(t, err)
	var r2name string
	for _, r := range riders {
		if r.ID == s.rider2 {
			r2name = r.Username
		}
	}
	_, err = a.SetShift(ctx, userByLogin(t, r2name), s.branchID, true, "", nil)
	noErr(t, err)
	if rs, _ := a.RiderStatus(ctx, s.rider2); !rs.OnDuty {
		t.Fatal("clocking on should put the rider on duty")
	}
	_, err = a.ReportLocations(ctx, s.rider2, []app.LocationPoint{{Lat: 14.55, Lng: 121.02, At: time.Now()}})
	noErr(t, err)

	// a shift nobody ended is closed at twelve hours and flagged
	_, err = a.DB.Exec(ctx, `UPDATE staff_shifts SET started_at = now() - interval '13 hours' WHERE user_id=$1 AND ended_at IS NULL`, s.rider2)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE rider_status SET duty_since = now() - interval '13 hours' WHERE user_id=$1`, s.rider2)
	noErr(t, err)
	noErr(t, a.Cleanup(ctx))
	sh = shiftsOf(t, s.branchID, s.rider2)
	if len(sh) != 1 || sh[0].EndReason != "auto" || sh[0].Minutes != 12*60 {
		t.Fatalf("auto-ended shift: %+v", sh)
	}
	if rs, _ := a.RiderStatus(ctx, s.rider2); rs.OnDuty || rs.Lat != nil {
		t.Fatalf("the rider should be off duty with no position: %+v", rs)
	}
}

// Working without clocking on opens a shift, but only for work done on the
// person's own workbench: the shared kitchen screen is nobody in particular.
// The dish records who made it.
func TestAutoClockOnAndWhoCooked(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	cook, err := a.CreateStaff(ctx, s.branchID, app.NewUser{Username: uniq("cook"), Password: "password1", DisplayName: "Cook", Role: "staff",
		Posts: []app.PostInput{{Kind: app.PostPacking}}})
	noErr(t, err)
	board := app.Actor{Type: "staff", ID: cook, Via: "board"}
	work := app.Actor{Type: "staff", ID: cook, Via: "work"}

	o := s.place(t, "delivery", s.cart())
	o, err = a.Accept(ctx, o.ID, o.Version, s.staff)
	noErr(t, err)
	chicken, tea := itemByName(o, "chicken"), itemByName(o, "tea")

	_, err = a.SetItemStatus(ctx, o.ID, chicken.ID, "start", board)
	noErr(t, err)
	if st, _ := a.ShiftState(ctx, cook); st.OnDuty {
		t.Fatal("the kitchen screen must not clock anyone on")
	}
	_, err = a.SetItemStatus(ctx, o.ID, chicken.ID, "ready", work)
	noErr(t, err)
	sh := shiftsOf(t, s.branchID, cook)
	if len(sh) != 1 || sh[0].StartSource != "auto" || sh[0].EndedAt != nil {
		t.Fatalf("auto shift: %+v", sh)
	}

	type made struct {
		startedBy, readyBy *int64
		readyAt            *time.Time
		via                string
		undo               int
	}
	read := func(id int64) made {
		var m made
		noErr(t, a.DB.QueryRow(ctx, `SELECT prep_started_by, ready_by, ready_at, ready_via, undo_count FROM order_items WHERE id=$1`, id).
			Scan(&m.startedBy, &m.readyBy, &m.readyAt, &m.via, &m.undo))
		return m
	}
	if m := read(chicken.ID); m.startedBy == nil || *m.startedBy != cook || m.readyBy == nil || *m.readyBy != cook || m.via != "work" || m.readyAt == nil {
		t.Fatalf("chicken: %+v", m)
	}
	// taken back: it counts for nobody until someone marks it ready again
	_, err = a.SetItemStatus(ctx, o.ID, chicken.ID, "undo", work)
	noErr(t, err)
	if m := read(chicken.ID); m.readyBy != nil || m.readyAt != nil || m.undo != 1 || m.startedBy == nil {
		t.Fatalf("after undo: %+v", m)
	}
	_, err = a.SetItemStatus(ctx, o.ID, chicken.ID, "ready", board)
	noErr(t, err)
	if m := read(chicken.ID); m.readyBy == nil || m.via != "board" || m.undo != 1 {
		t.Fatalf("ready on the board: %+v", m)
	}
	// water needs no cooking: nobody made it
	if m := read(itemByName(o, "water").ID); m.readyBy != nil {
		t.Fatalf("water: %+v", m)
	}
	o, err = a.SetItemStatus(ctx, o.ID, tea.ID, "ready", work)
	noErr(t, err)
	_, err = a.Pack(ctx, o.ID, 2, o.ManifestVersion, work)
	noErr(t, err)
	var packedBy *int64
	var packedVia string
	noErr(t, a.DB.QueryRow(ctx, `SELECT packed_by, packed_via FROM orders WHERE id=$1`, o.ID).Scan(&packedBy, &packedVia))
	if packedBy == nil || *packedBy != cook || packedVia != "work" {
		t.Fatalf("packed by %v via %q", packedBy, packedVia)
	}

	// a rider who collects the order without clocking on is at work
	o, err = a.AssignRider(ctx, o.ID, s.rider1, s.staff)
	noErr(t, err)
	if st, _ := a.ShiftState(ctx, s.rider1); st.OnDuty {
		t.Fatal("being assigned an order is not clocking on")
	}
	_, err = a.RiderPickup(ctx, o.Delivery.ID, s.rider1)
	noErr(t, err)
	if rs, _ := a.RiderStatus(ctx, s.rider1); !rs.OnDuty {
		t.Fatal("collecting an order should put the rider on duty")
	}
	if sh := shiftsOf(t, s.branchID, s.rider1); len(sh) != 1 || sh[0].StartSource != "auto" {
		t.Fatalf("rider auto shift: %+v", sh)
	}
}

// A manager corrects, adds and voids shifts. A reason is required and the
// first recorded times are kept.
func TestManagerEditsShifts(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	owner := userByLogin(t, s.ownerName)
	now := time.Now().Truncate(time.Second)
	ago := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }

	// add a shift nobody clocked
	_, err := a.AddShift(ctx, s.branchID, app.ShiftInput{UserID: s.rider1, StartedAt: ago(5 * time.Hour), EndedAt: ago(3 * time.Hour)}, owner)
	wantCode(t, err, "VALIDATION") // no reason
	_, err = a.AddShift(ctx, s.branchID, app.ShiftInput{UserID: s.rider1, StartedAt: ago(3 * time.Hour), EndedAt: ago(5 * time.Hour), Reason: "x"}, owner)
	wantCode(t, err, "VALIDATION") // ends before it starts
	_, err = a.AddShift(ctx, s.branchID, app.ShiftInput{UserID: s.rider1, StartedAt: ago(30 * time.Hour), EndedAt: ago(1 * time.Hour), Reason: "x"}, owner)
	wantCode(t, err, "VALIDATION") // longer than a day
	other := newStore(t, true)
	_, err = a.AddShift(ctx, s.branchID, app.ShiftInput{UserID: other.rider1, StartedAt: ago(5 * time.Hour), EndedAt: ago(3 * time.Hour), Reason: "x"}, owner)
	wantCode(t, err, "NOT_FOUND") // not our staff
	added, err := a.AddShift(ctx, s.branchID, app.ShiftInput{UserID: s.rider1, StartedAt: ago(5 * time.Hour), EndedAt: ago(3 * time.Hour), Reason: "phone was broken"}, owner)
	noErr(t, err)
	if added.StartSource != "manager" || added.Minutes != 120 || !added.Edited || added.EditedBy != "Owner" {
		t.Fatalf("added: %+v", added)
	}
	_, err = a.AddShift(ctx, s.branchID, app.ShiftInput{UserID: s.rider1, StartedAt: ago(4 * time.Hour), EndedAt: ago(2 * time.Hour), Reason: "x"}, owner)
	wantCode(t, err, "SHIFT_OVERLAP")

	// correct a shift the system had to end
	_, err = a.SetDuty(ctx, s.rider2, true, "")
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `UPDATE staff_shifts SET started_at = now() - interval '13 hours' WHERE user_id=$1 AND ended_at IS NULL`, s.rider2)
	noErr(t, err)
	noErr(t, a.Cleanup(ctx))
	long := shiftsOf(t, s.branchID, s.rider2)[0]
	_, err = a.EditShift(ctx, s.branchID, long.ID, app.ShiftInput{EndedAt: ago(11 * time.Hour)}, owner)
	wantCode(t, err, "VALIDATION")
	_, err = a.EditShift(ctx, other.branchID, long.ID, app.ShiftInput{EndedAt: ago(11 * time.Hour), Reason: "x"}, owner)
	wantCode(t, err, "NOT_FOUND") // another store's id
	fixed, err := a.EditShift(ctx, s.branchID, long.ID, app.ShiftInput{EndedAt: ago(11 * time.Hour), Reason: "forgot to clock off"}, owner)
	noErr(t, err)
	if fixed.Minutes < 119 || fixed.Minutes > 121 || fixed.OrigEndedAt == nil || !fixed.OrigEndedAt.Equal(*long.EndedAt) ||
		fixed.OrigStartedAt == nil || fixed.EditReason != "forgot to clock off" || fixed.EndReason != "auto" {
		t.Fatalf("fixed: %+v", fixed)
	}
	// a second correction keeps the very first times
	again, err := a.EditShift(ctx, s.branchID, long.ID, app.ShiftInput{EndedAt: ago(10 * time.Hour), Reason: "checked the camera"}, owner)
	noErr(t, err)
	if !again.OrigEndedAt.Equal(*long.EndedAt) {
		t.Fatalf("original end lost: %+v", again)
	}

	// a manager ends a shift that is still running: the rider goes off duty
	_, err = a.SetDuty(ctx, s.rider2, true, "")
	noErr(t, err)
	var open app.Shift
	for _, sh := range shiftsOf(t, s.branchID, s.rider2) {
		if sh.EndedAt == nil {
			open = sh
		}
	}
	_, err = a.DB.Exec(ctx, `UPDATE staff_shifts SET started_at = now() - interval '2 hours' WHERE id=$1`, open.ID)
	noErr(t, err)
	closed, err := a.EditShift(ctx, s.branchID, open.ID, app.ShiftInput{EndedAt: ago(time.Hour), Reason: "went home"}, owner)
	noErr(t, err)
	if closed.EndReason != "manager" || closed.EndedAt == nil {
		t.Fatalf("closed: %+v", closed)
	}
	if rs, _ := a.RiderStatus(ctx, s.rider2); rs.OnDuty {
		t.Fatal("the rider should be off duty")
	}

	// void: struck from the record, and it no longer blocks that time
	voided, err := a.EditShift(ctx, s.branchID, added.ID, app.ShiftInput{Void: true, Reason: "entered by mistake"}, owner)
	noErr(t, err)
	if !voided.Voided {
		t.Fatalf("voided: %+v", voided)
	}
	_, err = a.EditShift(ctx, s.branchID, added.ID, app.ShiftInput{EndedAt: ago(time.Hour), Reason: "x"}, owner)
	wantCode(t, err, "INVALID_STATE")
	_, err = a.AddShift(ctx, s.branchID, app.ShiftInput{UserID: s.rider1, StartedAt: ago(4 * time.Hour), EndedAt: ago(2 * time.Hour), Reason: "right times"}, owner)
	noErr(t, err)

	// the period is checked
	_, err = a.Attendance(ctx, s.branchID, "2026-01-01", "2026-06-01")
	wantCode(t, err, "VALIDATION")
	_, err = a.Attendance(ctx, s.branchID, "yesterday", "today")
	wantCode(t, err, "VALIDATION")
}
