package app

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// Manager approval (docs/STAFF_ACCESS_DESIGN.md §7): at the counter an order
// taker meets something their role may not do, such as cancelling an accepted
// order. A manager or the owner who may do it enters their approval code, and
// the one action goes ahead; the log says who approved it.

// ApprovablePerms are the permissions an approval can stand in for.
var ApprovablePerms = map[string]bool{PermOrderCancel: true, PermOrderAftersale: true, PermCheckoutDiscount: true}

const (
	approvalTTL      = 5 * time.Minute
	approveMaxFails  = 5
	approveLockSpell = 15 * time.Minute
)

var pinRe = regexp.MustCompile(`^[0-9]{6}$`)

// SetApprovePin sets the person's own approval code. The sign-in password is
// asked for, so that a back office left open does not let someone else set it.
func (a *App) SetApprovePin(ctx context.Context, userID int64, password, pin string) error {
	if !pinRe.MatchString(pin) {
		return ErrValidation("the approval code is 6 digits")
	}
	var hash string
	if err := a.DB.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1 AND active`, userID).Scan(&hash); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return Err(http.StatusUnauthorized, "BAD_PASSWORD", "wrong password")
	}
	ph, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = a.DB.Exec(ctx, `UPDATE users SET approve_pin_hash=$2, approve_fails=0, approve_locked_until=NULL WHERE id=$1`, userID, string(ph))
	return err
}

// Approver is someone who can approve an action in a store.
type Approver struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

// Approvers lists the store's owners and managers who may do perm themselves
// and have set an approval code.
func (a *App) Approvers(ctx context.Context, branchID int64, perm string) ([]Approver, error) {
	if !ApprovablePerms[perm] {
		return nil, ErrValidation("this cannot be approved")
	}
	conf, err := branchPerms(ctx, a.DB, []int64{branchID})
	if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT u.id, u.display_name, r.role FROM user_branch_roles r JOIN users u ON u.id = r.user_id
		WHERE r.branch_id=$1 AND r.role IN ('owner', 'manager') AND u.active AND u.approve_pin_hash IS NOT NULL
		ORDER BY r.role DESC, u.display_name`, branchID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Approver])
	if err != nil {
		return nil, err
	}
	list := []Approver{}
	for _, ap := range out {
		if hasPerm(permsFor(conf, branchID, ap.Role), perm) {
			list = append(list, ap)
		}
	}
	return list, nil
}

func hasPerm(perms []string, p string) bool {
	for _, x := range perms {
		if x == p {
			return true
		}
	}
	return false
}

// Approval is a one-time permission for one action.
type Approval struct {
	Token        string    `json:"token"`
	ExpiresAt    time.Time `json:"expires_at"`
	ApproverName string    `json:"approver_name"`
}

// Approve checks the approver's code and issues an approval for requester to
// do perm once in the store. Five wrong codes lock the approver's code for 15
// minutes.
func (a *App) Approve(ctx context.Context, requester *User, branchID, approverID int64, perm, pin string) (*Approval, error) {
	if !ApprovablePerms[perm] {
		return nil, ErrValidation("this cannot be approved")
	}
	if approverID == requester.ID {
		return nil, ErrValidation("ask someone else to approve")
	}
	approver, err := a.loadUser(ctx, approverID)
	if errors.Is(err, ErrNotFound) || err == nil && (!approver.Can(branchID, perm) || !senior(approver.RoleIn(branchID))) {
		return nil, Err(http.StatusForbidden, "NOT_APPROVER", "this person cannot approve it")
	} else if err != nil {
		return nil, err
	}
	var out *Approval
	var refuse error
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		var hash *string
		var fails int
		var locked *time.Time
		if err := tx.QueryRow(ctx, `SELECT approve_pin_hash, approve_fails, approve_locked_until FROM users WHERE id=$1 FOR UPDATE`,
			approverID).Scan(&hash, &fails, &locked); err != nil {
			return err
		}
		switch {
		case hash == nil:
			refuse = Err(http.StatusForbidden, "NOT_APPROVER", "this person has no approval code")
			return nil
		case locked != nil && locked.After(time.Now()):
			refuse = ErrD(http.StatusTooManyRequests, "APPROVAL_LOCKED", "too many wrong codes", map[string]any{"until": *locked})
			return nil
		}
		if bcrypt.CompareHashAndPassword([]byte(*hash), []byte(pin)) != nil {
			fails++
			if fails >= approveMaxFails {
				until := time.Now().Add(approveLockSpell)
				refuse = ErrD(http.StatusTooManyRequests, "APPROVAL_LOCKED", "too many wrong codes", map[string]any{"until": until})
				_, err := tx.Exec(ctx, `UPDATE users SET approve_fails=0, approve_locked_until=$2 WHERE id=$1`, approverID, until)
				return err
			}
			refuse = ErrD(http.StatusUnauthorized, "BAD_PIN", "wrong approval code", map[string]any{"left": approveMaxFails - fails})
			_, err := tx.Exec(ctx, `UPDATE users SET approve_fails=$2 WHERE id=$1`, approverID, fails)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET approve_fails=0, approve_locked_until=NULL WHERE id=$1`, approverID); err != nil {
			return err
		}
		token, th := newToken()
		expires := time.Now().Add(approvalTTL)
		if _, err := tx.Exec(ctx, `INSERT INTO approvals (token_hash, branch_id, perm, approver_id, requester_id, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6)`, th, branchID, perm, approverID, requester.ID, expires); err != nil {
			return err
		}
		out = &Approval{Token: token, ExpiresAt: expires, ApproverName: approver.DisplayName}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if refuse != nil {
		return nil, refuse
	}
	return out, nil
}

// UseApproval spends an approval on the action at hand: it must be for this
// person, this store and one of the permissions the action needs.
func (a *App) UseApproval(ctx context.Context, token string, branchID, requesterID int64, perms []string) (int64, error) {
	var by int64
	err := a.DB.QueryRow(ctx, `UPDATE approvals SET used_at=now() WHERE token_hash=$1 AND branch_id=$2 AND requester_id=$3
		AND perm = ANY($4) AND used_at IS NULL AND expires_at > now() RETURNING approver_id`,
		hashToken(token), branchID, requesterID, perms).Scan(&by)
	if isNoRows(err) {
		return 0, Err(http.StatusForbidden, "APPROVAL_INVALID", "the approval was used or has expired; ask again")
	}
	return by, err
}
