package app

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Post kinds. A staff member can hold any number of them in a branch.
const (
	PostStation = "station" // one kitchen station
	PostPacking = "packing" // the packing counter
	PostRider   = "rider"   // may be assigned deliveries
)

// Post is one job a staff member does in a branch.
type Post struct {
	Kind          string `json:"kind"`
	StationID     *int64 `json:"station_id"`
	StationName   string `json:"station_name,omitempty"`
	StationNameEn string `json:"station_name_en,omitempty"`
	StationActive bool   `json:"station_active,omitempty"`
}

type PostInput struct {
	Kind      string `json:"kind"`
	StationID *int64 `json:"station_id"`
}

type postKey struct{ user, branch int64 }

// loadPosts reads posts matching where (aliases: p = staff_posts), grouped by
// user and branch, stations first in the kitchen's own order.
func loadPosts(ctx context.Context, q Querier, where string, args ...any) (map[postKey][]Post, error) {
	rows, err := q.Query(ctx, `SELECT p.user_id, p.branch_id, p.kind, p.station_id,
		coalesce(s.name,''), coalesce(s.name_en,''), coalesce(s.active,false)
		FROM staff_posts p LEFT JOIN kitchen_stations s ON s.id = p.station_id WHERE `+where+`
		ORDER BY (p.kind='rider'), (p.kind='packing'), s.sort, s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[postKey][]Post{}
	for rows.Next() {
		var k postKey
		var p Post
		if err := rows.Scan(&k.user, &k.branch, &p.Kind, &p.StationID, &p.StationName, &p.StationNameEn, &p.StationActive); err != nil {
			return nil, err
		}
		out[k] = append(out[k], p)
	}
	return out, rows.Err()
}

var errHasDeliveries = Err(http.StatusConflict, "STAFF_HAS_DELIVERIES",
	"this person still has deliveries in progress; reassign them first")

func openDeliveries(ctx context.Context, tx pgx.Tx, userID, branchID int64) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_tasks WHERE rider_id=$1 AND branch_id=$2
		AND status IN ('assigned','picked_up','arrived')`, userID, branchID).Scan(&n)
	return n, err
}

// setPosts replaces a person's posts in a branch. The rider post cannot be
// taken away while they still carry deliveries.
func setPosts(ctx context.Context, tx pgx.Tx, userID, branchID int64, posts []PostInput) error {
	var stations []int64
	packing, rider := false, false
	for _, p := range posts {
		switch p.Kind {
		case PostStation:
			if p.StationID == nil {
				return ErrValidation("a station post needs station_id")
			}
			if !contains(stations, *p.StationID) {
				stations = append(stations, *p.StationID)
			}
		case PostPacking:
			packing = true
		case PostRider:
			rider = true
		default:
			return ErrValidation("unknown post")
		}
	}
	if len(stations) > 0 {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM kitchen_stations WHERE branch_id=$1 AND id = ANY($2)`,
			branchID, stations).Scan(&n); err != nil {
			return err
		}
		if n != len(stations) {
			return ErrValidation("station not found in this branch")
		}
	}
	if !rider {
		var had bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM staff_posts WHERE user_id=$1 AND branch_id=$2 AND kind='rider')`,
			userID, branchID).Scan(&had); err != nil {
			return err
		}
		if had {
			if n, err := openDeliveries(ctx, tx, userID, branchID); err != nil {
				return err
			} else if n > 0 {
				return errHasDeliveries
			}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM staff_posts WHERE user_id=$1 AND branch_id=$2`, userID, branchID); err != nil {
		return err
	}
	if len(stations) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO staff_posts (user_id, branch_id, kind, station_id)
			SELECT $1, $2, 'station', unnest($3::bigint[])`, userID, branchID, stations); err != nil {
			return err
		}
	}
	for kind, on := range map[string]bool{PostPacking: packing, PostRider: rider} {
		if !on {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO staff_posts (user_id, branch_id, kind) VALUES ($1,$2,$3)`, userID, branchID, kind); err != nil {
			return err
		}
	}
	return nil
}

func contains(xs []int64, x int64) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

type StaffMember struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Phone       string `json:"phone"`
	Role        string `json:"role"`
	Active      bool   `json:"active"`
	Posts       []Post `json:"posts"`
	// What they have on their plate right now.
	OpenTasks        int `json:"open_tasks"`        // dishes waiting at their stations
	ActiveDeliveries int `json:"active_deliveries"` // deliveries not finished yet
	// OnDuty is set for riders who clocked on.
	OnDuty bool `json:"on_duty"`
}

// ListStaff returns everyone with a role in the branch, active people first.
func (a *App) ListStaff(ctx context.Context, branchID int64) ([]StaffMember, error) {
	return a.listStaff(ctx, branchID, "")
}

// ListRiders returns the active staff who hold the rider post.
func (a *App) ListRiders(ctx context.Context, branchID int64) ([]StaffMember, error) {
	return a.listStaff(ctx, branchID, ` AND u.active AND EXISTS (SELECT 1 FROM staff_posts p
		WHERE p.user_id = u.id AND p.branch_id = r.branch_id AND p.kind = 'rider')`)
}

func (a *App) listStaff(ctx context.Context, branchID int64, extra string) ([]StaffMember, error) {
	rows, err := a.DB.Query(ctx, `SELECT u.id, u.username, u.display_name, u.phone, r.role, u.active,
		(SELECT count(*) FROM order_items oi JOIN orders o ON o.id = oi.order_id
			JOIN staff_posts p ON p.station_id = oi.station_id AND p.user_id = u.id AND p.branch_id = r.branch_id
			WHERE oi.branch_id = r.branch_id AND oi.prep_status IN ('pending','preparing') AND o.acceptance_status = 'accepted'),
		(SELECT count(*) FROM delivery_tasks d WHERE d.rider_id = u.id AND d.branch_id = r.branch_id
			AND d.status IN ('assigned','picked_up','arrived')),
		coalesce((SELECT rs.on_duty FROM rider_status rs WHERE rs.user_id = u.id), false) AND EXISTS (SELECT 1
			FROM staff_posts p WHERE p.user_id = u.id AND p.branch_id = r.branch_id AND p.kind = 'rider')
		FROM user_branch_roles r JOIN users u ON u.id = r.user_id
		WHERE r.branch_id=$1 AND u.deleted_at IS NULL`+extra+` ORDER BY u.active DESC, u.id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StaffMember{}
	for rows.Next() {
		s := StaffMember{Posts: []Post{}}
		if err := rows.Scan(&s.ID, &s.Username, &s.DisplayName, &s.Phone, &s.Role, &s.Active, &s.OpenTasks, &s.ActiveDeliveries, &s.OnDuty); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	posts, err := loadPosts(ctx, a.DB, `p.branch_id=$1`, branchID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if ps := posts[postKey{out[i].ID, branchID}]; ps != nil {
			out[i].Posts = ps
		}
	}
	return out, nil
}

var errOwnerOnly = Err(http.StatusForbidden, "FORBIDDEN", "only an owner can manage an owner")

func staffRole(role string) bool {
	return role == "owner" || role == "manager" || role == "cashier" || role == "staff"
}

// staffChanged tells the branch's screens and the person's own workbench to
// re-read: posts decide what the workbench shows.
func staffChanged(ctx context.Context, tx pgx.Tx, branchID, userID int64) error {
	return notify(ctx, tx, Event{BranchID: branchID, RiderIDs: []int64{userID}})
}

// CreateStaff adds a person to the branch (seed, setup and tests).
func (a *App) CreateStaff(ctx context.Context, branchID int64, in NewUser) (int64, error) {
	return a.CreateStaffBy(ctx, branchID, in, nil)
}

// CreateStaffBy adds a person on behalf of a signed-in manager, who cannot
// create an owner; the change is audited.
func (a *App) CreateStaffBy(ctx context.Context, branchID int64, in NewUser, by *User) (int64, error) {
	if by != nil && in.Role == "owner" && by.RoleIn(branchID) != "owner" {
		return 0, errOwnerOnly
	}
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		if id, err = createUser(ctx, tx, in, branchID); err != nil {
			return err
		}
		if by != nil {
			if err := audit(ctx, tx, branchID, 0, Actor{Type: "staff", ID: by.ID}, "staff.create",
				map[string]any{"user_id": id, "username": strings.ToLower(strings.TrimSpace(in.Username)), "role": in.Role}); err != nil {
				return err
			}
		}
		return staffChanged(ctx, tx, branchID, id)
	})
	return id, err
}

// StaffPatch changes what is set; nil fields stay as they are.
type StaffPatch struct {
	Username    *string      `json:"username"`
	DisplayName *string      `json:"display_name"`
	Phone       *string      `json:"phone"`
	Role        *string      `json:"role"`
	Posts       *[]PostInput `json:"posts"`
	Active      *bool        `json:"active"`
}

type lockedStaff struct {
	role     string
	active   bool
	username string
}

// lockStaff locks the person's account row and returns their standing in the
// branch. People of other branches, and deleted ones, are not found.
func lockStaff(ctx context.Context, tx pgx.Tx, branchID, userID int64) (*lockedStaff, error) {
	s := &lockedStaff{}
	err := tx.QueryRow(ctx, `SELECT r.role, u.active, u.username FROM user_branch_roles r JOIN users u ON u.id = r.user_id
		WHERE r.branch_id=$1 AND r.user_id=$2 AND u.deleted_at IS NULL FOR UPDATE OF u`, branchID, userID).
		Scan(&s.role, &s.active, &s.username)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return s, err
}

// keepAnOwner refuses to leave the branch without an active owner.
func keepAnOwner(ctx context.Context, tx pgx.Tx, branchID, leaving int64) error {
	var others int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM user_branch_roles r JOIN users u ON u.id = r.user_id
		WHERE r.branch_id=$1 AND r.role='owner' AND u.active AND u.id <> $2`, branchID, leaving).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return Err(http.StatusConflict, "LAST_OWNER", "a store needs at least one active owner")
	}
	return nil
}

// UpdateStaff edits a person's details, back-office role, posts or active flag.
func (a *App) UpdateStaff(ctx context.Context, branchID, userID int64, in StaffPatch, by *User) error {
	byRole := by.RoleIn(branchID)
	return a.inTx(ctx, func(tx pgx.Tx) error {
		cur, err := lockStaff(ctx, tx, branchID, userID)
		if err != nil {
			return err
		}
		if cur.role == "owner" && byRole != "owner" {
			return errOwnerOnly
		}
		changed := map[string]any{"user_id": userID}
		if in.Role != nil && *in.Role != cur.role {
			if !staffRole(*in.Role) {
				return ErrValidation("unknown role")
			}
			if *in.Role == "owner" && byRole != "owner" {
				return errOwnerOnly
			}
			if userID == by.ID {
				return ErrValidation("you cannot change your own role")
			}
			if cur.role == "owner" {
				if err := keepAnOwner(ctx, tx, branchID, userID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE user_branch_roles SET role=$3 WHERE branch_id=$1 AND user_id=$2`,
				branchID, userID, *in.Role); err != nil {
				return err
			}
			changed["role"] = *in.Role
		}
		if in.Active != nil && *in.Active != cur.active {
			if !*in.Active {
				if userID == by.ID {
					return ErrValidation("you cannot disable your own account")
				}
				if cur.role == "owner" {
					if err := keepAnOwner(ctx, tx, branchID, userID); err != nil {
						return err
					}
				}
				if n, err := openDeliveries(ctx, tx, userID, branchID); err != nil {
					return err
				} else if n > 0 {
					return errHasDeliveries
				}
				if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET active=$2 WHERE id=$1`, userID, *in.Active); err != nil {
				return err
			}
			changed["active"] = *in.Active
		}
		if in.Username != nil {
			name := strings.ToLower(strings.TrimSpace(*in.Username))
			if name != cur.username {
				if !usernameRe.MatchString(name) {
					return ErrValidation("username must be 3-32 chars of a-z, 0-9, _ . -")
				}
				var taken bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE username=$1)`, name).Scan(&taken); err != nil {
					return err
				}
				if taken {
					return Err(http.StatusConflict, "USERNAME_TAKEN", "username already exists")
				}
				if _, err := tx.Exec(ctx, `UPDATE users SET username=$2 WHERE id=$1`, userID, name); err != nil {
					return err
				}
				changed["username"] = name
			}
		}
		if in.DisplayName != nil {
			name := strings.TrimSpace(*in.DisplayName)
			if name == "" {
				return ErrValidation("display_name is required")
			}
			if _, err := tx.Exec(ctx, `UPDATE users SET display_name=$2 WHERE id=$1`, userID, name); err != nil {
				return err
			}
			changed["display_name"] = name
		}
		if in.Phone != nil {
			if _, err := tx.Exec(ctx, `UPDATE users SET phone=$2 WHERE id=$1`, userID, strings.TrimSpace(*in.Phone)); err != nil {
				return err
			}
		}
		if in.Posts != nil {
			if err := setPosts(ctx, tx, userID, branchID, *in.Posts); err != nil {
				return err
			}
			changed["posts"] = *in.Posts
		}
		if err := audit(ctx, tx, branchID, 0, Actor{Type: "staff", ID: by.ID}, "staff.update", changed); err != nil {
			return err
		}
		return staffChanged(ctx, tx, branchID, userID)
	})
}

// ResetStaffPassword sets a new password and signs the person out everywhere.
func (a *App) ResetStaffPassword(ctx context.Context, branchID, userID int64, password string, by *User) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		cur, err := lockStaff(ctx, tx, branchID, userID)
		if err != nil {
			return err
		}
		if cur.role == "owner" && by.RoleIn(branchID) != "owner" {
			return errOwnerOnly
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, userID, hash); err != nil {
			return err
		}
		if userID != by.ID {
			if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
				return err
			}
		}
		return audit(ctx, tx, branchID, 0, Actor{Type: "staff", ID: by.ID}, "staff.password", map[string]any{"user_id": userID})
	})
}

// DeleteStaff removes a person from the branch. When that was their only
// branch the account is retired: it can no longer sign in and the username
// becomes free, while orders and the audit log keep their name.
func (a *App) DeleteStaff(ctx context.Context, branchID, userID int64, by *User) error {
	if userID == by.ID {
		return ErrValidation("you cannot delete your own account")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		cur, err := lockStaff(ctx, tx, branchID, userID)
		if err != nil {
			return err
		}
		if cur.role == "owner" {
			if by.RoleIn(branchID) != "owner" {
				return errOwnerOnly
			}
			if cur.active {
				if err := keepAnOwner(ctx, tx, branchID, userID); err != nil {
					return err
				}
			}
		}
		if n, err := openDeliveries(ctx, tx, userID, branchID); err != nil {
			return err
		} else if n > 0 {
			return errHasDeliveries
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_branch_roles WHERE branch_id=$1 AND user_id=$2`, branchID, userID); err != nil {
			return err
		}
		var elsewhere bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_branch_roles WHERE user_id=$1)`, userID).Scan(&elsewhere); err != nil {
			return err
		}
		if !elsewhere {
			// '~' is not allowed in usernames, so the retired name cannot clash.
			if _, err := tx.Exec(ctx, `UPDATE users SET active=false, deleted_at=now(), username = username || '~' || id
				WHERE id=$1`, userID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
				return err
			}
		}
		if err := audit(ctx, tx, branchID, 0, Actor{Type: "staff", ID: by.ID}, "staff.delete",
			map[string]any{"user_id": userID, "username": cur.username}); err != nil {
			return err
		}
		return staffChanged(ctx, tx, branchID, userID)
	})
}
