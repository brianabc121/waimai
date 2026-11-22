// Package app holds the business use cases. HTTP handlers and the worker call
// into it; it never sees gin.Context.
package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brianabc121/waimai/internal/config"
)

// NotifyChannel is the PostgreSQL NOTIFY channel used for refresh hints.
const NotifyChannel = "waimai_events"

// SMSSender delivers a text message. Implementations for real providers live
// in internal/sms; the default only logs that a message was produced.
type SMSSender interface {
	Send(ctx context.Context, phone, message string) error
}

type LogSMS struct{}

func (LogSMS) Send(_ context.Context, phone, _ string) error {
	log.Printf("sms: message for phone ending %s not sent (no SMS provider configured)", last4(phone))
	return nil
}

func last4(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[len(s)-4:]
}

type App struct {
	DB         *pgxpool.Pool
	Secret     []byte
	SMS        SMSSender // read with smsSender()
	OTPDevEcho bool
	// Push delivers reminders to riders, by device platform (android, ios,
	// web). A platform with no sender is skipped.
	Push map[string]PushSender
	// PublicBaseURL is the customer-facing origin, used for order links in SMS.
	PublicBaseURL string
	// MapsOff is set when the platform has no map key. Customers cannot pin
	// an address then, so every store behaves as "pick a zone from a list".
	MapsOff bool
	// Router plans a rider's round trip on real roads. Without one the
	// planned route of a trip is a straight-line estimate.
	Router Router
	// IM is the chat server (OpenIM). Nil turns chat off.
	IM IM

	// The four fields above are rebuilt when an administrator saves a key
	// (settings.go); mu guards them. base is the configuration from the
	// environment and effective the one in use, with the saved keys over it.
	mu          *sync.RWMutex
	base        config.Config
	effective   *config.Config
	settingsSig string
}

// forCustomer applies platform-wide limits to a store as customers use it.
func (a *App) forCustomer(b *Branch) *Branch {
	if a.mapsOff() {
		b.ZoneMode = "list"
	}
	return b
}

func New(pool *pgxpool.Pool, secret string) *App {
	return &App{DB: pool, Secret: []byte(secret), SMS: LogSMS{}, mu: &sync.RWMutex{}}
}

// Error is a business error with a stable code that the HTTP layer maps 1:1.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func Err(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func ErrD(status int, code, msg string, details any) *Error {
	return &Error{Status: status, Code: code, Message: msg, Details: details}
}

var (
	ErrNotFound     = Err(http.StatusNotFound, "NOT_FOUND", "resource not found")
	ErrInvalidState = func(msg string) *Error { return Err(http.StatusConflict, "INVALID_STATE", msg) }
	ErrValidation   = func(msg string) *Error { return Err(http.StatusUnprocessableEntity, "VALIDATION", msg) }
	ErrVersion      = Err(http.StatusConflict, "VERSION_CONFLICT", "the order changed, reload and retry")
)

// Querier is satisfied by both the pool and a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// inTx runs fn in a transaction and commits when fn returns nil.
func (a *App) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Actor identifies who performed a command, for the audit log.
type Actor struct {
	Type string // customer, staff, rider, system
	ID   int64
	// Via is where a kitchen action was tapped: "work" is the person's own
	// workbench, "board" the shared kitchen screen, "" unknown. Only what is
	// done on the workbench is certain to be this person's own work.
	Via string
}

func audit(ctx context.Context, tx pgx.Tx, branchID, orderID int64, actor Actor, action string, detail any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	b, _ := json.Marshal(detail)
	var actorID *int64
	if actor.ID != 0 {
		actorID = &actor.ID
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs (branch_id, order_id, actor_type, actor_id, action, detail)
		VALUES ($1,NULLIF($2::bigint, 0),$3,$4,$5,$6)`, branchID, orderID, actor.Type, actorID, action, b)
	return err
}

// Event is the NOTIFY payload. It carries identifiers only; screens re-read
// the snapshot they are allowed to see.
type Event struct {
	BranchID int64   `json:"b"`
	OrderID  int64   `json:"o,omitempty"`
	RiderIDs []int64 `json:"r,omitempty"`
}

func notify(ctx context.Context, q Querier, ev Event) error {
	b, _ := json.Marshal(ev)
	_, err := q.Exec(ctx, `SELECT pg_notify($1, $2)`, NotifyChannel, string(b))
	return err
}

// ---- idempotency ----

// idemBegin claims (scope,key). When the key was already used with the same
// request it unmarshals the stored response into out and returns replayed=true.
// A concurrent holder of the same key blocks this insert until it commits.
func idemBegin(ctx context.Context, tx pgx.Tx, scope, key string, reqHash []byte, out any) (bool, error) {
	if key == "" {
		return false, nil
	}
	tag, err := tx.Exec(ctx, `INSERT INTO idempotency_records (scope, key, request_hash)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, scope, key, reqHash)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return false, nil
	}
	var storedHash []byte
	var resp []byte
	if err := tx.QueryRow(ctx, `SELECT request_hash, response FROM idempotency_records
		WHERE scope=$1 AND key=$2`, scope, key).Scan(&storedHash, &resp); err != nil {
		return false, err
	}
	if !hmac.Equal(storedHash, reqHash) {
		return false, Err(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "idempotency key reused with a different request")
	}
	if resp == nil {
		return false, Err(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "original request has no stored result")
	}
	return true, json.Unmarshal(resp, out)
}

func idemSave(ctx context.Context, tx pgx.Tx, scope, key string, resp any) error {
	if key == "" {
		return nil
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE idempotency_records SET response=$3 WHERE scope=$1 AND key=$2`, scope, key, b)
	return err
}

func hashJSON(v any) []byte {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return h[:]
}

// ---- tokens ----

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func newToken() (token string, hash []byte) {
	token = base64.RawURLEncoding.EncodeToString(randomBytes(32))
	return token, hashToken(token)
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// orderToken derives the customer's order access token from a per-order nonce
// and the server secret, so it can be re-issued on an idempotent replay while
// the database only stores its hash.
func (a *App) orderToken(nonce []byte) string {
	return a.derive("order-access:", nonce)
}

// derive computes a purpose-bound token from a stored nonce and the server secret.
func (a *App) derive(purpose string, nonce []byte) string {
	m := hmac.New(sha256.New, a.Secret)
	m.Write([]byte(purpose))
	m.Write(nonce)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	var ae *Error
	if errors.As(err, &ae) {
		return err
	}
	return fmt.Errorf("%s: %w", op, err)
}
