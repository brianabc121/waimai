package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Reviews and reports on the customer's store page. A review belongs to one
// finished delivery or pickup order and is written by whoever holds that
// order's link, so it needs no customer account.

const reviewWindow = 30 * 24 * time.Hour

// ReviewSummary is the store's score. The averages are 0 while Count is 0.
type ReviewSummary struct {
	Count     int     `json:"count"`
	Average   float64 `json:"average"`
	Taste     float64 `json:"taste"`
	Packaging float64 `json:"packaging"`
	Delivery  float64 `json:"delivery"`
}

type Review struct {
	ID          int64     `json:"id"`
	DisplayName string    `json:"display_name"`
	Rating      int       `json:"rating"`
	Taste       int       `json:"taste"`
	Packaging   int       `json:"packaging"`
	Delivery    *int      `json:"delivery"`
	Comment     string    `json:"comment"`
	CreatedAt   time.Time `json:"created_at"`
	// Items are the dishes of the reviewed order.
	Items []ReviewItem `json:"items"`
}

type ReviewItem struct {
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
}

type ReviewInput struct {
	Rating    int    `json:"rating"`
	Taste     int    `json:"taste"`
	Packaging int    `json:"packaging"`
	Delivery  int    `json:"delivery"`
	Comment   string `json:"comment"`
}

// salesCounts returns how many of each item were ordered and how many orders
// the store has taken. Rejected, cancelled and unanswered orders do not count.
// With a brand, only its orders count (the brand's page shows its own sales).
func salesCounts(ctx context.Context, q Querier, branchID, brandID int64) (map[int64]int, int, error) {
	rows, err := q.Query(ctx, `SELECT oi.item_id, sum(oi.qty)::int FROM order_items oi JOIN orders o ON o.id = oi.order_id
		WHERE oi.branch_id=$1 AND o.acceptance_status='accepted' AND oi.prep_status <> 'cancelled'
			AND ($2 = 0 OR o.brand_id = $2) GROUP BY 1`, branchID, brandID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	sold := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, 0, err
		}
		sold[id] = n
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var orders int
	err = q.QueryRow(ctx, `SELECT count(*) FROM orders WHERE branch_id=$1 AND acceptance_status='accepted'
		AND ($2 = 0 OR brand_id = $2)`, branchID, brandID).Scan(&orders)
	return sold, orders, err
}

// reviewSummary sums up the reviews of one of the store's brands (0 = all of them).
func reviewSummary(ctx context.Context, q Querier, branchID, brandID int64) (*ReviewSummary, error) {
	var s ReviewSummary
	err := q.QueryRow(ctx, `SELECT count(*), coalesce(round(avg(rating), 1), 0)::float8,
		coalesce(round(avg(taste), 1), 0)::float8, coalesce(round(avg(packaging), 1), 0)::float8,
		coalesce(round(avg(delivery), 1), 0)::float8 FROM reviews WHERE branch_id=$1 AND ($2 = 0 OR brand_id = $2)`, branchID, brandID).
		Scan(&s.Count, &s.Average, &s.Taste, &s.Packaging, &s.Delivery)
	return &s, err
}

// ReviewPage is one page of a store's reviews.
type ReviewPage struct {
	Summary ReviewSummary `json:"summary"`
	Reviews []Review      `json:"reviews"`
	More    bool          `json:"more"`
}

const reviewPageSize = 20

// StoreReviews lists reviews, newest first. filter is all, latest (the last
// 30 days), good (4 or 5 stars) or bad (1 or 2 stars).
func (a *App) StoreReviews(ctx context.Context, slug, filter string, offset int) (*ReviewPage, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	cond := "true"
	switch filter {
	case "", "all":
	case "latest":
		cond = "r.created_at >= now() - interval '30 days'"
	case "good":
		cond = "r.rating >= 4"
	case "bad":
		cond = "r.rating <= 2"
	default:
		return nil, ErrValidation("filter must be all, latest, good or bad")
	}
	if offset < 0 || offset > 10000 {
		return nil, ErrValidation("offset out of range")
	}
	br, err := brandBySlug(ctx, a.DB, b.ID, slug)
	if err != nil {
		return nil, err
	}
	sum, err := reviewSummary(ctx, a.DB, b.ID, br.ID)
	if err != nil {
		return nil, err
	}
	out := &ReviewPage{Summary: *sum, Reviews: []Review{}}
	rows, err := a.DB.Query(ctx, `SELECT r.id, r.order_id, r.display_name, r.rating, r.taste, r.packaging, r.delivery,
		r.comment, r.created_at FROM reviews r WHERE r.branch_id=$1 AND r.brand_id=$4 AND `+cond+`
		ORDER BY r.created_at DESC, r.id DESC LIMIT $2 OFFSET $3`, b.ID, reviewPageSize+1, offset, br.ID)
	if err != nil {
		return nil, err
	}
	idx := map[int64]int{}
	ids := []int64{}
	for rows.Next() {
		r := Review{Items: []ReviewItem{}}
		var orderID int64
		if err := rows.Scan(&r.ID, &orderID, &r.DisplayName, &r.Rating, &r.Taste, &r.Packaging, &r.Delivery,
			&r.Comment, &r.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if len(out.Reviews) == reviewPageSize {
			out.More = true
			continue
		}
		idx[orderID] = len(out.Reviews)
		ids = append(ids, orderID)
		out.Reviews = append(out.Reviews, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err = a.DB.Query(ctx, `SELECT order_id, name, name_en FROM order_items
		WHERE order_id = ANY($1) AND prep_status <> 'cancelled' ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var orderID int64
		var it ReviewItem
		if err := rows.Scan(&orderID, &it.Name, &it.NameEn); err != nil {
			return nil, err
		}
		r := &out.Reviews[idx[orderID]]
		dup := false
		for _, x := range r.Items {
			dup = dup || x.Name == it.Name
		}
		if !dup {
			r.Items = append(r.Items, it)
		}
	}
	return out, rows.Err()
}

// OrderReview is what the order page needs to offer or show a review.
type OrderReview struct {
	CanReview bool    `json:"can_review"`
	Review    *Review `json:"review"`
}

func reviewable(o *Order) bool {
	return (o.DisplayStatus == "delivered" || o.DisplayStatus == "picked_up") &&
		o.CompletedAt != nil && time.Since(*o.CompletedAt) < reviewWindow
}

func (a *App) OrderReview(ctx context.Context, token string) (*OrderReview, error) {
	o, err := a.OrderByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	out := &OrderReview{}
	r := Review{Items: []ReviewItem{}}
	err = a.DB.QueryRow(ctx, `SELECT id, display_name, rating, taste, packaging, delivery, comment, created_at
		FROM reviews WHERE order_id=$1`, o.ID).Scan(&r.ID, &r.DisplayName, &r.Rating, &r.Taste, &r.Packaging,
		&r.Delivery, &r.Comment, &r.CreatedAt)
	switch {
	case isNoRows(err):
		out.CanReview = reviewable(o)
	case err != nil:
		return nil, err
	default:
		out.Review = &r
	}
	return out, nil
}

// reviewerName is a stable alias for a phone number that does not reveal it.
func (a *App) reviewerName(phone string) string {
	m := hmac.New(sha256.New, a.Secret)
	m.Write([]byte("reviewer:" + phone))
	return fmt.Sprintf("用户-%06d", binary.BigEndian.Uint32(m.Sum(nil))%1000000)
}

// SubmitReview stores the customer's one review of a finished order.
func (a *App) SubmitReview(ctx context.Context, token string, in ReviewInput) (*OrderReview, error) {
	o, err := a.OrderByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if !reviewable(o) {
		return nil, Err(http.StatusConflict, "NOT_REVIEWABLE", "only a finished order of the last 30 days can be reviewed")
	}
	in.Comment = strings.TrimSpace(in.Comment)
	if len([]rune(in.Comment)) > 500 {
		return nil, ErrValidation("the comment must be at most 500 characters")
	}
	var delivery *int
	scores := []int{in.Rating, in.Taste, in.Packaging}
	if o.ServiceType == "delivery" {
		delivery = &in.Delivery
		scores = append(scores, in.Delivery)
	}
	for _, v := range scores {
		if v < 1 || v > 5 {
			return nil, ErrValidation("every score must be 1 to 5 stars")
		}
	}
	tag, err := a.DB.Exec(ctx, `INSERT INTO reviews (branch_id, order_id, rating, taste, packaging, delivery, comment, display_name)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (order_id) DO NOTHING`,
		o.BranchID, o.ID, in.Rating, in.Taste, in.Packaging, delivery, in.Comment, a.reviewerName(o.CustomerPhone))
	if err != nil {
		return nil, wrap("insert review", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, Err(http.StatusConflict, "ALREADY_REVIEWED", "this order was already reviewed")
	}
	return a.OrderReview(ctx, token)
}

// ---- reports ----

var reportReasons = map[string]bool{"food_safety": true, "wrong_info": true, "bad_service": true, "fraud": true, "other": true}

type ReportInput struct {
	Reason  string `json:"reason"`
	Detail  string `json:"detail"`
	Contact string `json:"contact"`
}

// ReportStore files a customer's complaint about a store for the platform to read.
func (a *App) ReportStore(ctx context.Context, slug string, in ReportInput) error {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return err
	}
	in.Detail, in.Contact = strings.TrimSpace(in.Detail), strings.TrimSpace(in.Contact)
	if !reportReasons[in.Reason] {
		return ErrValidation("choose a reason")
	}
	if len([]rune(in.Detail)) > 500 || len([]rune(in.Contact)) > 80 {
		return ErrValidation("text too long")
	}
	if in.Reason == "other" && in.Detail == "" {
		return ErrValidation("describe the problem")
	}
	_, err = a.DB.Exec(ctx, `INSERT INTO store_reports (branch_id, reason, detail, contact) VALUES ($1,$2,$3,$4)`,
		b.ID, in.Reason, in.Detail, in.Contact)
	return err
}

type StoreReport struct {
	ID         int64     `json:"id"`
	BranchID   int64     `json:"branch_id"`
	BranchName string    `json:"branch_name"`
	Reason     string    `json:"reason"`
	Detail     string    `json:"detail"`
	Contact    string    `json:"contact"`
	CreatedAt  time.Time `json:"created_at"`
}

// MerchantReports lists the latest reports against a merchant's stores.
func (a *App) MerchantReports(ctx context.Context, merchantID int64) ([]StoreReport, error) {
	rows, err := a.DB.Query(ctx, `SELECT r.id, r.branch_id, b.name, r.reason, r.detail, r.contact, r.created_at
		FROM store_reports r JOIN branches b ON b.id = r.branch_id WHERE b.merchant_id=$1
		ORDER BY r.created_at DESC, r.id DESC LIMIT 100`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreReport{}
	for rows.Next() {
		var r StoreReport
		if err := rows.Scan(&r.ID, &r.BranchID, &r.BranchName, &r.Reason, &r.Detail, &r.Contact, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
