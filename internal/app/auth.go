package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	SessionTTL         = 14 * 24 * time.Hour
	CustomerSessionTTL = 30 * 24 * time.Hour
	otpTTL             = 5 * time.Minute
	otpMaxAttempts     = 5
	otpResendInterval  = 60 * time.Second
)

type BranchRole struct {
	BranchID   int64  `json:"branch_id"`
	BranchName string `json:"branch_name"`
	Slug       string `json:"slug"`
	Currency   string `json:"currency"`
	Role       string `json:"role"`
}

type User struct {
	ID              int64        `json:"id"`
	Username        string       `json:"username"`
	DisplayName     string       `json:"display_name"`
	Phone           string       `json:"phone"`
	IsPlatformAdmin bool         `json:"is_platform_admin"`
	Roles           []BranchRole `json:"roles"`
}

// RoleIn returns the user's role in a branch, or "".
func (u *User) RoleIn(branchID int64) string {
	for _, r := range u.Roles {
		if r.BranchID == branchID {
			return r.Role
		}
	}
	return ""
}

func (a *App) loadUser(ctx context.Context, id int64) (*User, error) {
	u := &User{Roles: []BranchRole{}}
	err := a.DB.QueryRow(ctx, `SELECT id, username, display_name, phone, is_platform_admin FROM users
		WHERE id=$1 AND active`, id).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Phone, &u.IsPlatformAdmin)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT r.branch_id, b.name, b.slug, b.currency, r.role FROM user_branch_roles r
		JOIN branches b ON b.id = r.branch_id WHERE r.user_id=$1 ORDER BY r.branch_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r BranchRole
		if err := rows.Scan(&r.BranchID, &r.BranchName, &r.Slug, &r.Currency, &r.Role); err != nil {
			return nil, err
		}
		u.Roles = append(u.Roles, r)
	}
	return u, rows.Err()
}

var errBadLogin = Err(http.StatusUnauthorized, "BAD_CREDENTIALS", "wrong username or password")

// dummyHash keeps login timing similar when the username does not exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

// Login verifies credentials and creates a server-side session.
func (a *App) Login(ctx context.Context, username, password string) (token string, u *User, err error) {
	var id int64
	var hash string
	err = a.DB.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE username=$1 AND active`,
		strings.ToLower(strings.TrimSpace(username))).Scan(&id, &hash)
	if isNoRows(err) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", nil, errBadLogin
	} else if err != nil {
		return "", nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", nil, errBadLogin
	}
	token, th := newToken()
	if _, err = a.DB.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
		th, id, time.Now().Add(SessionTTL)); err != nil {
		return "", nil, err
	}
	u, err = a.loadUser(ctx, id)
	return token, u, err
}

func (a *App) Logout(ctx context.Context, token string) error {
	_, err := a.DB.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hashToken(token))
	return err
}

// UserBySession re-reads the user and roles on every request, so that a role
// change or deactivation takes effect immediately.
func (a *App) UserBySession(ctx context.Context, token string) (*User, error) {
	var id int64
	err := a.DB.QueryRow(ctx, `SELECT user_id FROM sessions WHERE token_hash=$1 AND expires_at > now()`,
		hashToken(token)).Scan(&id)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return a.loadUser(ctx, id)
}

func hashPassword(pw string) (string, error) {
	if len(pw) < 8 || len(pw) > 72 {
		return "", ErrValidation("password must be 8 to 72 characters")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

var usernameRe = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)

type NewUser struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Phone       string `json:"phone"`
	Role        string `json:"role"`
}

func createUser(ctx context.Context, tx pgx.Tx, in NewUser, branchID int64) (int64, error) {
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if !usernameRe.MatchString(in.Username) {
		return 0, ErrValidation("username must be 3-32 chars of a-z, 0-9, _ . -")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return 0, ErrValidation("display_name is required")
	}
	switch in.Role {
	case "owner", "manager", "cashier", "kitchen", "rider":
	default:
		return 0, ErrValidation("unknown role")
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO users (username, password_hash, display_name, phone)
		VALUES ($1,$2,$3,$4) ON CONFLICT (username) DO NOTHING RETURNING id`,
		in.Username, hash, strings.TrimSpace(in.DisplayName), in.Phone).Scan(&id)
	if isNoRows(err) {
		return 0, Err(http.StatusConflict, "USERNAME_TAKEN", "username already exists")
	} else if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_branch_roles (user_id, branch_id, role) VALUES ($1,$2,$3)`, id, branchID, in.Role)
	return id, err
}

type StaffMember struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Phone       string `json:"phone"`
	Role        string `json:"role"`
	Active      bool   `json:"active"`
}

func (a *App) ListStaff(ctx context.Context, branchID int64, role string) ([]StaffMember, error) {
	rows, err := a.DB.Query(ctx, `SELECT u.id, u.username, u.display_name, u.phone, r.role, u.active
		FROM user_branch_roles r JOIN users u ON u.id = r.user_id
		WHERE r.branch_id=$1 AND ($2 = '' OR (r.role = $2 AND u.active)) ORDER BY u.id`, branchID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StaffMember{}
	for rows.Next() {
		var s StaffMember
		if err := rows.Scan(&s.ID, &s.Username, &s.DisplayName, &s.Phone, &s.Role, &s.Active); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (a *App) CreateStaff(ctx context.Context, branchID int64, in NewUser) (int64, error) {
	var id int64
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var err error
		id, err = createUser(ctx, tx, in, branchID)
		return err
	})
	return id, err
}

// SetStaffActive enables or disables an account that has a role in this branch.
// Disabling also drops the user's sessions.
func (a *App) SetStaffActive(ctx context.Context, branchID, userID int64, active bool) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET active=$3 WHERE id=$2 AND EXISTS (
			SELECT 1 FROM user_branch_roles WHERE user_id=$2 AND branch_id=$1)`, branchID, userID, active)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if !active {
			_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID)
		}
		return err
	})
}

// ---- platform admin ----

type NewMerchant struct {
	MerchantName string  `json:"merchant_name"`
	BranchName   string  `json:"branch_name"`
	BranchNameEn string  `json:"branch_name_en"`
	Slug         string  `json:"slug"`
	Timezone     string  `json:"timezone"`
	Currency     string  `json:"currency"`
	Owner        NewUser `json:"owner"`
}

type MerchantSummary struct {
	MerchantID   int64  `json:"merchant_id"`
	MerchantName string `json:"merchant_name"`
	BranchID     int64  `json:"branch_id"`
	BranchName   string `json:"branch_name"`
	Slug         string `json:"slug"`
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)

// CreateMerchant creates a merchant, its first branch and the owner account.
func (a *App) CreateMerchant(ctx context.Context, in NewMerchant) (*MerchantSummary, error) {
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	if strings.TrimSpace(in.MerchantName) == "" || strings.TrimSpace(in.BranchName) == "" {
		return nil, ErrValidation("merchant_name and branch_name are required")
	}
	if !slugRe.MatchString(in.Slug) {
		return nil, ErrValidation("slug must be lowercase letters, digits and dashes")
	}
	if in.Timezone == "" {
		in.Timezone = "Asia/Manila"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return nil, ErrValidation("unknown timezone")
	}
	if in.Currency == "" {
		in.Currency = "PHP"
	}
	in.Owner.Role = "owner"
	out := &MerchantSummary{MerchantName: in.MerchantName, BranchName: in.BranchName, Slug: in.Slug}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO merchants (name) VALUES ($1) RETURNING id`, in.MerchantName).
			Scan(&out.MerchantID); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `INSERT INTO branches (merchant_id, slug, name, name_en, timezone, currency)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (slug) DO NOTHING RETURNING id`,
			out.MerchantID, in.Slug, in.BranchName, in.BranchNameEn, in.Timezone, in.Currency).Scan(&out.BranchID)
		if isNoRows(err) {
			return Err(http.StatusConflict, "SLUG_TAKEN", "this store link is already in use")
		} else if err != nil {
			return err
		}
		_, err = createUser(ctx, tx, in.Owner, out.BranchID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (a *App) ListMerchants(ctx context.Context) ([]MerchantSummary, error) {
	rows, err := a.DB.Query(ctx, `SELECT m.id, m.name, b.id, b.name, b.slug FROM merchants m
		JOIN branches b ON b.merchant_id = m.id ORDER BY m.id, b.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MerchantSummary{}
	for rows.Next() {
		var m MerchantSummary
		if err := rows.Scan(&m.MerchantID, &m.MerchantName, &m.BranchID, &m.BranchName, &m.Slug); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// EnsurePlatformAdmin creates or resets the platform admin account (used by seed and first-run setup).
func (a *App) EnsurePlatformAdmin(ctx context.Context, username, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = a.DB.Exec(ctx, `INSERT INTO users (username, password_hash, display_name, is_platform_admin)
		VALUES ($1,$2,'Platform Admin',true)
		ON CONFLICT (username) DO UPDATE SET password_hash=EXCLUDED.password_hash, is_platform_admin=true`,
		strings.ToLower(username), hash)
	return err
}

// ---- customer phone verification ----

var phoneRe = regexp.MustCompile(`^\+?[0-9]{7,15}$`)

func normalizePhone(p string) (string, error) {
	p = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(p)
	if !phoneRe.MatchString(p) {
		return "", ErrValidation("enter a valid phone number")
	}
	return p, nil
}

func (a *App) otpHash(phone, code string) []byte {
	m := hmac.New(sha256.New, a.Secret)
	m.Write([]byte("otp:" + phone + ":" + code))
	return m.Sum(nil)
}

// RequestOTP issues a 6-digit code for a phone number. The code is returned
// to the caller only when OTPDevEcho is on (development and demos).
func (a *App) RequestOTP(ctx context.Context, phone string) (devCode string, err error) {
	phone, err = normalizePhone(phone)
	if err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		// Serialise requests per phone so the resend interval cannot be raced.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "otp:"+phone); err != nil {
			return err
		}
		var recent bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM phone_verifications WHERE phone=$1
			AND created_at > now() - make_interval(secs => $2))`, phone, otpResendInterval.Seconds()).Scan(&recent); err != nil {
			return err
		}
		if recent {
			return Err(http.StatusTooManyRequests, "RATE_LIMITED", "please wait a minute before requesting another code")
		}
		_, err := tx.Exec(ctx, `INSERT INTO phone_verifications (phone, code_hash, expires_at) VALUES ($1,$2,$3)`,
			phone, a.otpHash(phone, code), time.Now().Add(otpTTL))
		return err
	})
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("验证码 %s，5 分钟内有效。Your verification code is %s, valid for 5 minutes.", code, code)
	if err := a.SMS.Send(ctx, phone, msg); err != nil {
		return "", wrap("send sms", err)
	}
	if a.OTPDevEcho {
		return code, nil
	}
	return "", nil
}

// VerifyOTP checks the latest code for the phone and, on success, creates a
// customer session. Five wrong attempts void the code.
func (a *App) VerifyOTP(ctx context.Context, phone, code string) (token string, normalized string, err error) {
	phone, err = normalizePhone(phone)
	if err != nil {
		return "", "", err
	}
	bad := Err(http.StatusUnprocessableEntity, "OTP_INVALID", "the code is wrong or expired")
	var result error
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		var id int64
		var hash []byte
		var attempts int
		err := tx.QueryRow(ctx, `SELECT id, code_hash, attempts FROM phone_verifications
			WHERE phone=$1 AND consumed_at IS NULL AND expires_at > now()
			ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, phone).Scan(&id, &hash, &attempts)
		if isNoRows(err) {
			result = bad
			return nil
		} else if err != nil {
			return err
		}
		if attempts >= otpMaxAttempts {
			result = bad
			return nil
		}
		if !hmac.Equal(hash, a.otpHash(phone, code)) {
			// Commit the attempt counter even though verification failed.
			_, err := tx.Exec(ctx, `UPDATE phone_verifications SET attempts = attempts + 1 WHERE id=$1`, id)
			result = bad
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE phone_verifications SET consumed_at = now() WHERE id=$1`, id); err != nil {
			return err
		}
		var th []byte
		token, th = newToken()
		_, err = tx.Exec(ctx, `INSERT INTO customer_sessions (token_hash, phone, expires_at) VALUES ($1,$2,$3)`,
			th, phone, time.Now().Add(CustomerSessionTTL))
		return err
	})
	if err != nil {
		return "", "", err
	}
	if result != nil {
		return "", "", result
	}
	return token, phone, nil
}

// CustomerPhone returns the verified phone for a customer session token.
func (a *App) CustomerPhone(ctx context.Context, token string) (string, error) {
	var phone string
	err := a.DB.QueryRow(ctx, `SELECT phone FROM customer_sessions WHERE token_hash=$1 AND expires_at > now()`,
		hashToken(token)).Scan(&phone)
	if isNoRows(err) {
		return "", Err(http.StatusForbidden, "PHONE_NOT_VERIFIED", "verify your phone number first")
	}
	return phone, err
}
