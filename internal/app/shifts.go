package app

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Attendance. A shift is one stretch of work: from clocking on to clocking
// off. A person is on at most one shift at a time; riders and in-store staff
// use the same switch. rider_status stays the live state of riders (on duty,
// last position) and is switched together with the shift.

const (
	// shiftAwayMeters marks a clock-in that was not made at the store.
	shiftAwayMeters = 200
	shiftMaxSpan    = 24 * time.Hour
	attendanceDays  = 62
)

var (
	errNotStaff     = Err(http.StatusForbidden, "FORBIDDEN", "you do not work in this store")
	errShiftOverlap = Err(http.StatusConflict, "SHIFT_OVERLAP", "this person already has a shift at that time")
)

// ShiftState is whether a person is at work right now.
type ShiftState struct {
	OnDuty    bool       `json:"on_duty"`
	DutySince *time.Time `json:"duty_since"`
	BranchID  *int64     `json:"branch_id"`
}

func shiftState(ctx context.Context, q Querier, userID int64) (*ShiftState, error) {
	var s ShiftState
	err := q.QueryRow(ctx, `SELECT started_at, branch_id FROM staff_shifts WHERE user_id=$1 AND ended_at IS NULL AND NOT voided`,
		userID).Scan(&s.DutySince, &s.BranchID)
	if isNoRows(err) {
		return &s, nil
	}
	s.OnDuty = err == nil
	return &s, err
}

// ShiftState returns the shift the person is on, if any.
func (a *App) ShiftState(ctx context.Context, userID int64) (*ShiftState, error) {
	return shiftState(ctx, a.DB, userID)
}

// distanceToStore is how far pos is from the store, nil when either is unknown.
func distanceToStore(ctx context.Context, q Querier, branchID int64, pos *LatLng) (*int, error) {
	if pos == nil {
		return nil, nil
	}
	var lat, lng *float64
	if err := q.QueryRow(ctx, `SELECT lat, lng FROM branches WHERE id=$1`, branchID).Scan(&lat, &lng); err != nil {
		return nil, err
	}
	if lat == nil || lng == nil {
		return nil, nil
	}
	d := int(math.Round(metresBetween(LatLng{*lat, *lng}, *pos)))
	return &d, nil
}

func latLngArgs(pos *LatLng) (lat, lng *float64) {
	if pos == nil {
		return nil, nil
	}
	return &pos[0], &pos[1]
}

func isRiderIn(ctx context.Context, q Querier, userID, branchID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM staff_posts WHERE user_id=$1 AND branch_id=$2 AND kind='rider')`,
		userID, branchID).Scan(&ok)
	return ok, err
}

// clockOn opens a shift unless the person is already on one. source is
// "manual" or "auto". It reports whether a shift was opened.
func clockOn(ctx context.Context, tx pgx.Tx, userID, branchID int64, source string, session []byte, pos *LatLng) (bool, error) {
	dist, err := distanceToStore(ctx, tx, branchID, pos)
	if err != nil {
		return false, err
	}
	lat, lng := latLngArgs(pos)
	tag, err := tx.Exec(ctx, `INSERT INTO staff_shifts (user_id, branch_id, started_at, start_source, session,
			start_lat, start_lng, start_distance_m) VALUES ($1,$2,now(),$3,$4,$5,$6,$7)
		ON CONFLICT (user_id) WHERE ended_at IS NULL AND NOT voided DO NOTHING`,
		userID, branchID, source, session, lat, lng, dist)
	if err != nil {
		return false, err
	}
	opened := tag.RowsAffected() == 1
	if !opened && source == "manual" && session != nil {
		// Clocking on again from another sign-in moves the shift there.
		if _, err := tx.Exec(ctx, `UPDATE staff_shifts SET session=$2 WHERE user_id=$1 AND ended_at IS NULL AND NOT voided`,
			userID, session); err != nil {
			return false, err
		}
	}
	var shiftBranch int64
	if err := tx.QueryRow(ctx, `SELECT branch_id FROM staff_shifts WHERE user_id=$1 AND ended_at IS NULL AND NOT voided`,
		userID).Scan(&shiftBranch); err != nil {
		return false, err
	}
	rider, err := isRiderIn(ctx, tx, userID, shiftBranch)
	if err != nil {
		return false, err
	}
	if rider && (opened || source == "manual") {
		if err := setDuty(ctx, tx, userID, true, session); err != nil {
			return false, err
		}
	}
	if !opened {
		return false, nil
	}
	return true, notify(ctx, tx, Event{BranchID: branchID, RiderIDs: []int64{userID}})
}

// autoClockOn is called when someone starts working without clocking on, so
// that the hours are not lost and nobody is held up by the switch.
func autoClockOn(ctx context.Context, tx pgx.Tx, userID, branchID int64) error {
	if userID == 0 {
		return nil
	}
	_, err := clockOn(ctx, tx, userID, branchID, "auto", nil, nil)
	return err
}

// clockOff ends the person's shift at the given time and takes a rider off
// duty. The last known position is cleared, so the store never shows where
// someone is after work.
func clockOff(ctx context.Context, tx pgx.Tx, userID int64, reason string, at time.Time, pos *LatLng) error {
	var id, branchID int64
	var started time.Time
	err := tx.QueryRow(ctx, `SELECT id, branch_id, started_at FROM staff_shifts
		WHERE user_id=$1 AND ended_at IS NULL AND NOT voided FOR UPDATE`, userID).Scan(&id, &branchID, &started)
	if err != nil && !isNoRows(err) {
		return err
	}
	if err := setDuty(ctx, tx, userID, false, nil); err != nil {
		return err
	}
	if err := tripOffDuty(ctx, tx, userID, at); err != nil {
		return err
	}
	if isNoRows(err) {
		return nil
	}
	if at.Before(started) {
		at = started
	}
	dist, err := distanceToStore(ctx, tx, branchID, pos)
	if err != nil {
		return err
	}
	lat, lng := latLngArgs(pos)
	if _, err := tx.Exec(ctx, `UPDATE staff_shifts SET ended_at=$2, end_reason=$3, end_lat=$4, end_lng=$5, end_distance_m=$6,
		session=NULL WHERE id=$1`, id, at, reason, lat, lng, dist); err != nil {
		return err
	}
	return notify(ctx, tx, Event{BranchID: branchID, RiderIDs: []int64{userID}})
}

// SetShift clocks a person on or off in a branch they work in. session is
// the sign-in the shift is started from: signing out there ends it.
func (a *App) SetShift(ctx context.Context, u *User, branchID int64, on bool, session string, pos *LatLng) (*ShiftState, error) {
	if pos != nil && !validLatLng(*pos) {
		pos = nil
	}
	var out *ShiftState
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if on {
			if u.RoleIn(branchID) == "" {
				return errNotStaff
			}
			var sh []byte
			if session != "" {
				sh = hashToken(session)
			}
			if _, err := clockOn(ctx, tx, u.ID, branchID, "manual", sh, pos); err != nil {
				return err
			}
		} else if err := clockOff(ctx, tx, u.ID, "manual", time.Now(), pos); err != nil {
			return err
		}
		var err error
		out, err = shiftState(ctx, tx, u.ID)
		return err
	})
	return out, err
}

// endLongShifts clocks off people who never ended their shift. The shift is
// closed at the limit and flagged, for a manager to put right.
func (a *App) endLongShifts(ctx context.Context) error {
	rows, err := a.DB.Query(ctx, `SELECT user_id, started_at FROM staff_shifts
		WHERE ended_at IS NULL AND NOT voided AND started_at < now() - make_interval(secs => $1)
		UNION SELECT user_id, duty_since FROM rider_status
		WHERE on_duty AND duty_since < now() - make_interval(secs => $1)`, dutyMax.Seconds())
	if err != nil {
		return err
	}
	type late struct {
		user  int64
		since time.Time
	}
	lates, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (late, error) {
		var l late
		return l, r.Scan(&l.user, &l.since)
	})
	if err != nil {
		return err
	}
	for _, l := range lates {
		if err := a.inTx(ctx, func(tx pgx.Tx) error {
			return clockOff(ctx, tx, l.user, "auto", l.since.Add(dutyMax), nil)
		}); err != nil {
			return err
		}
	}
	return nil
}

// ---- the manager's attendance page ----

// Shift is one stretch of work as the attendance page shows it.
type Shift struct {
	ID     int64 `json:"id"`
	UserID int64 `json:"user_id"`
	// Day is the store's calendar day the shift started on.
	Day       string     `json:"day"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	// Minutes worked; a shift still running counts up to now.
	Minutes        int        `json:"minutes"`
	StartSource    string     `json:"start_source"`
	EndReason      string     `json:"end_reason"`
	StartDistanceM *int       `json:"start_distance_m"`
	EndDistanceM   *int       `json:"end_distance_m"`
	OrigStartedAt  *time.Time `json:"orig_started_at"`
	OrigEndedAt    *time.Time `json:"orig_ended_at"`
	Edited         bool       `json:"edited"`
	EditedBy       string     `json:"edited_by"`
	EditedAt       *time.Time `json:"edited_at"`
	EditReason     string     `json:"edit_reason"`
	Voided         bool       `json:"voided"`
	// LastActionAt is the last thing the person did during a shift the
	// system had to end, to help a manager decide when it really ended.
	LastActionAt *time.Time `json:"last_action_at"`
}

// AttendanceStaff is a person on the attendance page. Gone marks someone who
// worked in the period but is no longer on the staff list.
type AttendanceStaff struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	Active      bool   `json:"active"`
	Gone        bool   `json:"gone"`
	Posts       []Post `json:"posts"`
}

type Attendance struct {
	From       string            `json:"from"`
	To         string            `json:"to"`
	Timezone   string            `json:"timezone"`
	Now        time.Time         `json:"now"`
	AwayMeters int               `json:"away_meters"`
	Staff      []AttendanceStaff `json:"staff"`
	Shifts     []Shift           `json:"shifts"`
}

// dayRange turns two calendar days in the store's time zone into instants:
// from the start of the first day to the end of the last.
func dayRange(loc *time.Location, from, to string, maxDays int) (time.Time, time.Time, error) {
	f, err1 := time.ParseInLocation("2006-01-02", from, loc)
	t, err2 := time.ParseInLocation("2006-01-02", to, loc)
	if err1 != nil || err2 != nil {
		return f, t, ErrValidation("from and to must be dates like 2026-11-21")
	}
	if t.Before(f) {
		return f, t, ErrValidation("to is before from")
	}
	if t.Sub(f) > time.Duration(maxDays)*24*time.Hour {
		return f, t, ErrValidation("the period is too long")
	}
	return f, t.AddDate(0, 0, 1), nil
}

const shiftCols = `s.id, s.user_id, s.started_at, s.ended_at, s.start_source, s.end_reason, s.start_distance_m,
	s.end_distance_m, s.orig_started_at, s.orig_ended_at, coalesce(e.display_name,''), s.edited_at, s.edit_reason, s.voided`

func scanShift(row pgx.CollectableRow, loc *time.Location, now time.Time) (Shift, error) {
	var s Shift
	err := row.Scan(&s.ID, &s.UserID, &s.StartedAt, &s.EndedAt, &s.StartSource, &s.EndReason, &s.StartDistanceM,
		&s.EndDistanceM, &s.OrigStartedAt, &s.OrigEndedAt, &s.EditedBy, &s.EditedAt, &s.EditReason, &s.Voided)
	s.Edited = s.EditedAt != nil
	s.Day = s.StartedAt.In(loc).Format("2006-01-02")
	end := now
	if s.EndedAt != nil {
		end = *s.EndedAt
	}
	if m := int(end.Sub(s.StartedAt).Minutes()); m > 0 {
		s.Minutes = m
	}
	return s, err
}

// Attendance lists the shifts that started in the period, with everyone who
// could have worked in it.
func (a *App) Attendance(ctx context.Context, branchID int64, from, to string) (*Attendance, error) {
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	loc := b.location()
	start, end, err := dayRange(loc, from, to, attendanceDays)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := &Attendance{From: from, To: to, Timezone: b.Timezone, Now: now, AwayMeters: shiftAwayMeters, Staff: []AttendanceStaff{}}
	rows, err := a.DB.Query(ctx, `SELECT `+shiftCols+` FROM staff_shifts s LEFT JOIN users e ON e.id = s.edited_by
		WHERE s.branch_id=$1 AND s.started_at >= $2 AND s.started_at < $3 ORDER BY s.started_at, s.id`, branchID, start, end)
	if err != nil {
		return nil, err
	}
	out.Shifts, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Shift, error) { return scanShift(r, loc, now) })
	if err != nil {
		return nil, err
	}
	if out.Shifts == nil {
		out.Shifts = []Shift{}
	}
	for i := range out.Shifts {
		s := &out.Shifts[i]
		if s.EndReason != "auto" || s.Edited || s.EndedAt == nil {
			continue
		}
		var last *time.Time
		if err := a.DB.QueryRow(ctx, `SELECT max(created_at) FROM audit_logs WHERE branch_id=$1 AND actor_id=$2
			AND actor_type IN ('staff','rider') AND created_at >= $3 AND created_at <= $4`,
			branchID, s.UserID, s.StartedAt, *s.EndedAt).Scan(&last); err != nil {
			return nil, err
		}
		s.LastActionAt = last
	}
	staff, err := a.ListStaff(ctx, branchID)
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	for _, m := range staff {
		seen[m.ID] = true
		out.Staff = append(out.Staff, AttendanceStaff{ID: m.ID, DisplayName: m.DisplayName, Username: m.Username,
			Role: m.Role, Active: m.Active, Posts: m.Posts})
	}
	var gone []int64
	for _, s := range out.Shifts {
		if !seen[s.UserID] {
			seen[s.UserID] = true
			gone = append(gone, s.UserID)
		}
	}
	if len(gone) > 0 {
		rows, err := a.DB.Query(ctx, `SELECT id, display_name, username FROM users WHERE id = ANY($1) ORDER BY id`, gone)
		if err != nil {
			return nil, err
		}
		more, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (AttendanceStaff, error) {
			m := AttendanceStaff{Gone: true, Role: "staff", Posts: []Post{}}
			return m, r.Scan(&m.ID, &m.DisplayName, &m.Username)
		})
		if err != nil {
			return nil, err
		}
		out.Staff = append(out.Staff, more...)
	}
	sort.SliceStable(out.Staff, func(i, j int) bool { return out.Staff[i].Active && !out.Staff[j].Active })
	return out, nil
}

// ShiftInput is what a manager sends to add or correct a shift. Times left
// out stay as they are; Void strikes the shift from the record.
type ShiftInput struct {
	UserID    int64      `json:"user_id"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Reason    string     `json:"reason"`
	Void      bool       `json:"void"`
}

func checkShiftTimes(start time.Time, end *time.Time, now time.Time) error {
	if start.After(now.Add(time.Minute)) {
		return ErrValidation("a shift cannot start in the future")
	}
	if end == nil {
		return nil
	}
	if !end.After(start) {
		return ErrValidation("the shift must end after it starts")
	}
	if end.After(now.Add(time.Minute)) {
		return ErrValidation("a shift cannot end in the future")
	}
	if end.Sub(start) > shiftMaxSpan {
		return ErrValidation("a shift cannot be longer than 24 hours")
	}
	return nil
}

func shiftOverlaps(ctx context.Context, tx pgx.Tx, userID, exceptID int64, start time.Time, end *time.Time) error {
	var clash bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM staff_shifts WHERE user_id=$1 AND id<>$2 AND NOT voided
		AND started_at < coalesce($4::timestamptz, now()) AND coalesce(ended_at, now()) > $3)`,
		userID, exceptID, start, end).Scan(&clash); err != nil {
		return err
	}
	if clash {
		return errShiftOverlap
	}
	return nil
}

func (a *App) loadShift(ctx context.Context, id int64) (*Shift, error) {
	var branchID int64
	if err := a.DB.QueryRow(ctx, `SELECT branch_id FROM staff_shifts WHERE id=$1`, id).Scan(&branchID); err != nil {
		return nil, err
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT `+shiftCols+` FROM staff_shifts s LEFT JOIN users e ON e.id = s.edited_by WHERE s.id=$1`, id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	s, err := pgx.CollectExactlyOneRow(rows, func(r pgx.CollectableRow) (Shift, error) { return scanShift(r, b.location(), now) })
	return &s, err
}

// AddShift records a finished shift nobody clocked, for example when the
// person had no phone that day.
func (a *App) AddShift(ctx context.Context, branchID int64, in ShiftInput, by *User) (*Shift, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, ErrValidation("a reason is required")
	}
	if in.StartedAt == nil || in.EndedAt == nil {
		return nil, ErrValidation("started_at and ended_at are required")
	}
	if err := checkShiftTimes(*in.StartedAt, in.EndedAt, time.Now()); err != nil {
		return nil, err
	}
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var member bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_branch_roles r JOIN users u ON u.id = r.user_id
			WHERE r.user_id=$1 AND r.branch_id=$2 AND u.deleted_at IS NULL)`, in.UserID, branchID).Scan(&member); err != nil {
			return err
		}
		if !member {
			return ErrNotFound
		}
		// Serialise changes to one person's shifts.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id=$1 FOR UPDATE`, in.UserID); err != nil {
			return err
		}
		if err := shiftOverlaps(ctx, tx, in.UserID, 0, *in.StartedAt, in.EndedAt); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO staff_shifts (user_id, branch_id, started_at, ended_at, start_source,
			end_reason, edited_by, edited_at, edit_reason) VALUES ($1,$2,$3,$4,'manager','manager',$5,now(),$6) RETURNING id`,
			in.UserID, branchID, *in.StartedAt, *in.EndedAt, by.ID, reason).Scan(&id); err != nil {
			return err
		}
		if err := audit(ctx, tx, branchID, 0, Actor{Type: "staff", ID: by.ID}, "shift.add", map[string]any{
			"shift_id": id, "user_id": in.UserID, "started_at": in.StartedAt, "ended_at": in.EndedAt, "reason": reason}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID, RiderIDs: []int64{in.UserID}})
	})
	if err != nil {
		return nil, err
	}
	return a.loadShift(ctx, id)
}

// EditShift corrects or voids a shift. The times as they were first recorded
// are kept, and a reason is required: the person can see both.
func (a *App) EditShift(ctx context.Context, branchID, shiftID int64, in ShiftInput, by *User) (*Shift, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, ErrValidation("a reason is required")
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var userID int64
		var start time.Time
		var end *time.Time
		var voided bool
		err := tx.QueryRow(ctx, `SELECT user_id, started_at, ended_at, voided FROM staff_shifts
			WHERE id=$1 AND branch_id=$2 FOR UPDATE`, shiftID, branchID).Scan(&userID, &start, &end, &voided)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if voided {
			return ErrInvalidState("this shift was voided")
		}
		now := time.Now()
		wasOpen := end == nil
		newStart, newEnd := start, end
		if in.StartedAt != nil {
			newStart = *in.StartedAt
		}
		if in.EndedAt != nil {
			newEnd = in.EndedAt
		}
		if in.Void && wasOpen {
			newEnd = &now
		}
		if !in.Void {
			if err := checkShiftTimes(newStart, newEnd, now); err != nil {
				return err
			}
			if err := shiftOverlaps(ctx, tx, userID, shiftID, newStart, newEnd); err != nil {
				return err
			}
		}
		// A shift that was still running ends here: the rider goes off duty.
		if wasOpen && newEnd != nil {
			if err := setDuty(ctx, tx, userID, false, nil); err != nil {
				return err
			}
		}
		endReason := ""
		if wasOpen && newEnd != nil {
			endReason = "manager"
		}
		if _, err := tx.Exec(ctx, `UPDATE staff_shifts SET
				orig_started_at = coalesce(orig_started_at, started_at),
				orig_ended_at = CASE WHEN edited_at IS NULL THEN ended_at ELSE orig_ended_at END,
				started_at=$2, ended_at=$3, voided=$4, edited_by=$5, edited_at=now(), edit_reason=$6,
				end_reason = CASE WHEN $7 <> '' THEN $7 ELSE end_reason END,
				session = CASE WHEN $3::timestamptz IS NULL THEN session ELSE NULL END
			WHERE id=$1`, shiftID, newStart, newEnd, in.Void, by.ID, reason, endReason); err != nil {
			return err
		}
		if err := audit(ctx, tx, branchID, 0, Actor{Type: "staff", ID: by.ID}, "shift.edit", map[string]any{
			"shift_id": shiftID, "user_id": userID, "from": []any{start, end}, "to": []any{newStart, newEnd},
			"void": in.Void, "reason": reason}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID, RiderIDs: []int64{userID}})
	})
	if err != nil {
		return nil, err
	}
	return a.loadShift(ctx, shiftID)
}
