package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log"
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
	// Codes are texted at a price per message; these stop one phone or one
	// network address from running up the bill.
	OTPDailyPerPhone = 10
	OTPDailyPerIP    = 30
)

type BranchRole struct {
	BranchID   int64  `json:"branch_id"`
	BranchName string `json:"branch_name"`
	Slug       string `json:"slug"`
	Currency   string `json:"currency"`
	// Role is back-office access: owner, manager, cashier or staff (none).
	// What the person works on is in Posts.
	Role  string `json:"role"`
	Posts []Post `json:"posts"`
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

// HasPost reports whether the user holds a post of this kind in the branch.
func (u *User) HasPost(branchID int64, kind string) bool {
	for _, r := range u.Roles {
		if r.BranchID != branchID {
			continue
		}
		for _, p := range r.Posts {
			if p.Kind == kind {
				return true
			}
		}
	}
	return false
}

// InStore reports whether the user works a station or the packing counter.
func (u *User) InStore(branchID int64) bool {
	return u.HasPost(branchID, PostStation) || u.HasPost(branchID, PostPacking)
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
		JOIN branches b ON b.id = r.branch_id JOIN merchants m ON m.id = b.merchant_id
		WHERE r.user_id=$1 AND b.deleted_at IS NULL AND m.deleted_at IS NULL ORDER BY r.branch_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r := BranchRole{Posts: []Post{}}
		if err := rows.Scan(&r.BranchID, &r.BranchName, &r.Slug, &r.Currency, &r.Role); err != nil {
			return nil, err
		}
		u.Roles = append(u.Roles, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	posts, err := loadPosts(ctx, a.DB, `p.user_id=$1`, id)
	if err != nil {
		return nil, err
	}
	for i := range u.Roles {
		if ps := posts[postKey{id, u.Roles[i].BranchID}]; ps != nil {
			u.Roles[i].Posts = ps
		}
	}
	return u, nil
}

var errBadLogin = Err(http.StatusUnauthorized, "BAD_CREDENTIALS", "wrong username or password")

// dummyHash keeps login timing similar when the username does not exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

// Login verifies credentials and creates a server-side session.
func (a *App) Login(ctx context.Context, username, password string) (token string, u *User, err error) {
	return a.login(ctx, username, password, "cookie", SessionTTL)
}

// TokenLogin signs the app in. The app keeps the token itself and sends it in
// a header, so the session lasts longer than a browser's.
func (a *App) TokenLogin(ctx context.Context, username, password string) (token string, u *User, expires time.Time, err error) {
	token, u, err = a.login(ctx, username, password, "token", TokenSessionTTL)
	return token, u, time.Now().Add(TokenSessionTTL), err
}

func (a *App) login(ctx context.Context, username, password, kind string, ttl time.Duration) (token string, u *User, err error) {
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
	if _, err = a.DB.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at, kind) VALUES ($1,$2,$3,$4)`,
		th, id, time.Now().Add(ttl), kind); err != nil {
		return "", nil, err
	}
	if _, err = a.DB.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id=$1`, id); err != nil {
		return "", nil, err
	}
	u, err = a.loadUser(ctx, id)
	return token, u, err
}

// Logout ends the session and forgets the devices registered in it. A shift
// that was started from this sign-in ends with it: a signed-out rider is not
// tracked.
func (a *App) Logout(ctx context.Context, token string) error {
	th := hashToken(token)
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var userID int64
		err := tx.QueryRow(ctx, `DELETE FROM sessions WHERE token_hash=$1 RETURNING user_id`, th).Scan(&userID)
		if isNoRows(err) {
			return nil
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM rider_devices WHERE session_hash=$1`, th); err != nil {
			return err
		}
		var mine bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM rider_status WHERE user_id=$1 AND on_duty AND duty_session=$2)
			OR EXISTS (SELECT 1 FROM staff_shifts WHERE user_id=$1 AND ended_at IS NULL AND NOT voided AND session=$2)`,
			userID, th).Scan(&mine); err != nil || !mine {
			return err
		}
		if err := clockOff(ctx, tx, userID, "logout", time.Now(), nil); err != nil {
			return err
		}
		branches, err := riderBranches(ctx, tx, userID)
		if err != nil {
			return err
		}
		for _, b := range branches {
			if err := notify(ctx, tx, Event{BranchID: b, RiderIDs: []int64{userID}}); err != nil {
				return err
			}
		}
		return nil
	})
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
	// Role is owner, manager, cashier or staff. The former "kitchen" and
	// "rider" roles are still accepted and become staff with matching posts.
	Role  string      `json:"role"`
	Posts []PostInput `json:"posts"`
}

func createUser(ctx context.Context, tx pgx.Tx, in NewUser, branchID int64) (int64, error) {
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if !usernameRe.MatchString(in.Username) {
		return 0, ErrValidation("username must be 3-32 chars of a-z, 0-9, _ . -")
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return 0, ErrValidation("display_name is required")
	}
	legacy := in.Role
	switch in.Role {
	case "owner", "manager", "cashier", "staff":
	case "kitchen", "rider":
		in.Role = "staff"
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
	if _, err = tx.Exec(ctx, `INSERT INTO user_branch_roles (user_id, branch_id, role) VALUES ($1,$2,$3)`, id, branchID, in.Role); err != nil {
		return 0, err
	}
	switch legacy {
	case "kitchen":
		if _, err = tx.Exec(ctx, `INSERT INTO staff_posts (user_id, branch_id, kind, station_id)
			SELECT $1, $2, 'station', id FROM kitchen_stations WHERE branch_id=$2 AND active`, id, branchID); err != nil {
			return 0, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO staff_posts (user_id, branch_id, kind) VALUES ($1,$2,'packing')`, id, branchID)
	case "rider":
		_, err = tx.Exec(ctx, `INSERT INTO staff_posts (user_id, branch_id, kind) VALUES ($1,$2,'rider')`, id, branchID)
	default:
		err = setPosts(ctx, tx, id, branchID, in.Posts)
	}
	return id, err
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)

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

// Customers log in with a mobile number of one country for now.
const customerCountryCode = "63"

var customerPhoneRe = regexp.MustCompile(`^9[0-9]{9}$`)

// normalizeCustomerPhone accepts the ways people write a Philippine mobile
// number (+63 917 555 0123, 0917 555 0123, 917 555 0123) and returns +639XXXXXXXXX.
func normalizeCustomerPhone(p string) (string, error) {
	p = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(p)
	local := p
	switch {
	case strings.HasPrefix(p, "+"+customerCountryCode):
		local = p[1+len(customerCountryCode):]
	case strings.HasPrefix(p, customerCountryCode) && len(p) == len(customerCountryCode)+10:
		local = p[len(customerCountryCode):]
	case strings.HasPrefix(p, "0"):
		local = p[1:]
	}
	if !customerPhoneRe.MatchString(local) {
		return "", Err(http.StatusUnprocessableEntity, "PHONE_INVALID", "enter a valid +63 mobile number")
	}
	return "+" + customerCountryCode + local, nil
}

var contactPhoneRe = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// normalizeContactPhone is for the number a rider calls at an address. It
// need not be in the country customers log in from: a number written with
// another country's code (+86 138 0013 8000) is kept as it is. Anything else
// is read as a number of the login country.
func normalizeContactPhone(p string) (string, error) {
	p = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(p)
	if strings.HasPrefix(p, "+") && !strings.HasPrefix(p, "+"+customerCountryCode) {
		if !contactPhoneRe.MatchString(p) {
			return "", Err(http.StatusUnprocessableEntity, "PHONE_INVALID", "enter a valid phone number with its country code")
		}
		return p, nil
	}
	return normalizeCustomerPhone(p)
}

func (a *App) otpHash(phone, code string) []byte {
	m := hmac.New(sha256.New, a.Secret)
	m.Write([]byte("otp:" + phone + ":" + code))
	return m.Sum(nil)
}

// RequestOTP texts a 6-digit code to a phone number. ip is the caller's
// network address, for the daily cap; it is not enforced when OTPDevEcho is
// on, where every test comes from one address. The code is returned to the
// caller only when OTPDevEcho is on (development and demos).
func (a *App) RequestOTP(ctx context.Context, phone, ip string) (devCode string, err error) {
	phone, err = normalizeCustomerPhone(phone)
	if err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		// Serialise requests per phone so the limits cannot be raced.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "otp:"+phone); err != nil {
			return err
		}
		var recent bool
		var today, fromIP int
		if err := tx.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM phone_verifications WHERE phone=$1 AND created_at > now() - make_interval(secs => $2)),
			(SELECT count(*) FROM phone_verifications WHERE phone=$1 AND created_at > now() - interval '24 hours'),
			(SELECT count(*) FROM phone_verifications WHERE ip=$3 AND ip <> '' AND created_at > now() - interval '24 hours')`,
			phone, otpResendInterval.Seconds(), ip).Scan(&recent, &today, &fromIP); err != nil {
			return err
		}
		if recent {
			return Err(http.StatusTooManyRequests, "OTP_WAIT", "please wait a minute before requesting another code")
		}
		if today >= OTPDailyPerPhone || (!a.OTPDevEcho && fromIP >= OTPDailyPerIP) {
			return Err(http.StatusTooManyRequests, "OTP_DAILY_LIMIT", "too many codes today, please try again tomorrow")
		}
		_, err := tx.Exec(ctx, `INSERT INTO phone_verifications (phone, code_hash, expires_at, ip) VALUES ($1,$2,$3,$4)`,
			phone, a.otpHash(phone, code), time.Now().Add(otpTTL), ip)
		return err
	})
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("验证码 %s，5 分钟内有效。Your verification code is %s, valid for 5 minutes.", code, code)
	if err := a.smsSender().Send(ctx, phone, msg); err != nil {
		log.Printf("verification code: send: %v", err)
		return "", Err(http.StatusBadGateway, "SMS_FAILED", "the code could not be sent")
	}
	if a.OTPDevEcho {
		return code, nil
	}
	return "", nil
}

// OTPSentToday is how many verification codes were texted in the last 24 hours.
func (a *App) OTPSentToday(ctx context.Context) (n int, err error) {
	err = a.DB.QueryRow(ctx, `SELECT count(*) FROM phone_verifications WHERE created_at > now() - interval '24 hours'`).Scan(&n)
	return n, err
}

// VerifyOTP checks the latest code for the phone and, on success, logs the
// customer in: the account is created the first time, and earlier orders of
// that phone join it. Five wrong attempts void the code.
func (a *App) VerifyOTP(ctx context.Context, phone, code string) (token string, normalized string, err error) {
	phone, err = normalizeCustomerPhone(phone)
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
		var customerID int64
		if err := tx.QueryRow(ctx, `INSERT INTO customers (phone) VALUES ($1)
			ON CONFLICT (phone) DO UPDATE SET last_login_at = now() RETURNING id`, phone).Scan(&customerID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE orders SET customer_id=$1 WHERE customer_phone=$2 AND customer_id IS NULL`,
			customerID, phone); err != nil {
			return err
		}
		var th []byte
		token, th = newToken()
		_, err = tx.Exec(ctx, `INSERT INTO customer_sessions (token_hash, phone, customer_id, expires_at) VALUES ($1,$2,$3,$4)`,
			th, phone, customerID, time.Now().Add(CustomerSessionTTL))
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

// Customer is a logged-in customer.
type Customer struct {
	ID    int64  `json:"id"`
	Phone string `json:"phone"`
}

// CustomerBySession returns the customer a session token belongs to. Sessions
// from before accounts existed get their account here.
func (a *App) CustomerBySession(ctx context.Context, token string) (*Customer, error) {
	var c Customer
	var id *int64
	err := a.DB.QueryRow(ctx, `SELECT customer_id, phone FROM customer_sessions WHERE token_hash=$1 AND expires_at > now()`,
		hashToken(token)).Scan(&id, &c.Phone)
	if isNoRows(err) {
		return nil, Err(http.StatusForbidden, "PHONE_NOT_VERIFIED", "verify your phone number first")
	} else if err != nil {
		return nil, err
	}
	if id != nil {
		c.ID = *id
		return &c, nil
	}
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO customers (phone) VALUES ($1)
			ON CONFLICT (phone) DO UPDATE SET phone = EXCLUDED.phone RETURNING id`, c.Phone).Scan(&c.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE customer_sessions SET customer_id=$1 WHERE token_hash=$2`, c.ID, hashToken(token))
		return err
	})
	return &c, err
}

// CustomerPhone returns the verified phone for a customer session token.
func (a *App) CustomerPhone(ctx context.Context, token string) (string, error) {
	c, err := a.CustomerBySession(ctx, token)
	if err != nil {
		return "", err
	}
	return c.Phone, nil
}

// CustomerLogout ends one customer session.
func (a *App) CustomerLogout(ctx context.Context, token string) error {
	_, err := a.DB.Exec(ctx, `DELETE FROM customer_sessions WHERE token_hash=$1`, hashToken(token))
	return err
}

// touchCustomerSessions keeps a customer who orders logged in for another 30 days.
func touchCustomerSessions(ctx context.Context, tx pgx.Tx, phone string) error {
	_, err := tx.Exec(ctx, `UPDATE customer_sessions SET expires_at=$2 WHERE phone=$1 AND expires_at > now()`,
		phone, time.Now().Add(CustomerSessionTTL))
	return err
}
