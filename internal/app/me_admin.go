package app

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// The platform's side of the customer pages: answering feedback, deciding
// applications, the wallet ledger and the texts about the platform.

type FeedbackList struct {
	Items []Feedback `json:"items"`
	Total int        `json:"total"`
	// Open counts feedback nobody has answered.
	Open int `json:"open"`
}

// AdminFeedback lists feedback, newest first. status is "", open or replied.
func (a *App) AdminFeedback(ctx context.Context, status string, p Page) (*FeedbackList, error) {
	const where = `($1 = '' OR ($1 = 'open') = (f.replied_at IS NULL))`
	if status != "" && status != "open" && status != "replied" {
		return nil, ErrValidation("status must be open or replied")
	}
	out := &FeedbackList{}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE `+where+`), count(*) FILTER (WHERE f.replied_at IS NULL)
		FROM customer_feedback f`, status).Scan(&out.Total, &out.Open); err != nil {
		return nil, err
	}
	limit, offset := p.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT f.id, f.kind, f.content, f.contact, f.reply, f.replied_at, f.created_at,
		c.id, c.phone FROM customer_feedback f JOIN customers c ON c.id = f.customer_id
		WHERE `+where+` ORDER BY f.id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	out.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Feedback, error) {
		var f Feedback
		err := r.Scan(&f.ID, &f.Kind, &f.Content, &f.Contact, &f.Reply, &f.RepliedAt, &f.CreatedAt, &f.CustomerID, &f.Phone)
		f.MemberNo = MemberNo(f.CustomerID)
		return f, err
	})
	if out.Items == nil {
		out.Items = []Feedback{}
	}
	return out, err
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// AdminReplyFeedback answers a piece of feedback; the customer gets a message.
func (a *App) AdminReplyFeedback(ctx context.Context, actor AdminActor, id int64, reply string) error {
	reply = strings.TrimSpace(reply)
	if reply == "" || utf8.RuneCountInString(reply) > 1000 {
		return ErrValidation("write a reply of up to 1000 characters")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var customerID int64
		var content string
		err := tx.QueryRow(ctx, `UPDATE customer_feedback SET reply=$2, replied_at=now() WHERE id=$1
			RETURNING customer_id, content`, id, reply).Scan(&customerID, &content)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if err := tell(ctx, tx, customerID, Message{Kind: "service", Title: "你的意见反馈有回复了", TitleEn: "We replied to your feedback", Key: "feedback_reply",
			Body: reply + "\n\n> " + cut(content, 60), BodyEn: reply + "\n\n> " + cut(content, 60), Link: "/me/feedback"}); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "feedback.reply", "feedback", id, nil)
	})
}

type ApplicationList struct {
	Items   []Application `json:"items"`
	Total   int           `json:"total"`
	Pending int           `json:"pending"`
}

func (a *App) AdminApplications(ctx context.Context, status string, p Page) (*ApplicationList, error) {
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		return nil, ErrValidation("status must be pending, approved or rejected")
	}
	out := &ApplicationList{}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE $1 = '' OR status = $1), count(*) FILTER (WHERE status = 'pending')
		FROM merchant_applications`, status).Scan(&out.Total, &out.Pending); err != nil {
		return nil, err
	}
	limit, offset := p.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT `+applicationCols+`, c.id, c.phone FROM merchant_applications p
		JOIN customers c ON c.id = p.customer_id WHERE ($1 = '' OR p.status = $1) ORDER BY p.id DESC LIMIT $2 OFFSET $3`,
		status, limit, offset)
	if err != nil {
		return nil, err
	}
	out.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Application, error) {
		var id int64
		var phone string
		p, err := scanApplication(r, &id, &phone)
		p.CustomerID, p.AccountPhone = id, phone
		return p, err
	})
	if out.Items == nil {
		out.Items = []Application{}
	}
	return out, err
}

// AdminDecideApplication approves or turns down an application and tells the
// applicant. Approving does not create the merchant.
func (a *App) AdminDecideApplication(ctx context.Context, actor AdminActor, id int64, status, reply string) error {
	reply = strings.TrimSpace(reply)
	if status != "approved" && status != "rejected" {
		return ErrValidation("status must be approved or rejected")
	}
	if utf8.RuneCountInString(reply) > 1000 || (status == "rejected" && reply == "") {
		return ErrValidation("say why, in up to 1000 characters")
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var customerID int64
		var store string
		err := tx.QueryRow(ctx, `UPDATE merchant_applications SET status=$2, reply=$3, decided_at=now()
			WHERE id=$1 AND status='pending' RETURNING customer_id, store_name`, id, status, reply).Scan(&customerID, &store)
		if isNoRows(err) {
			return Err(http.StatusConflict, "ALREADY_DECIDED", "this application is not waiting for a decision")
		} else if err != nil {
			return err
		}
		m := Message{Kind: "service", Body: reply, BodyEn: reply, Link: "/me/apply",
			Title: "商家入驻申请已通过：" + store, TitleEn: "Your application was approved: " + store,
			Key: "apply_approved", Args: map[string]string{"store": store}}
		if status == "rejected" {
			m.Title, m.TitleEn = "商家入驻申请未通过："+store, "Your application was not approved: "+store
		}
		if err := tell(ctx, tx, customerID, m); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "application."+status, "application", id, map[string]any{"store": store})
	})
}

// AdminCustomer is a customer as the platform lists them.
type AdminCustomer struct {
	ID          int64     `json:"id"`
	MemberNo    string    `json:"member_no"`
	Phone       string    `json:"phone"`
	Nickname    string    `json:"nickname"`
	Orders      int       `json:"orders"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
	Wallets     []Wallet  `json:"wallets"`
}

type CustomerList struct {
	Items []AdminCustomer `json:"items"`
	Total int             `json:"total"`
}

// AdminCustomers lists customers; q matches the phone, nickname or member number.
func (a *App) AdminCustomers(ctx context.Context, q string, p Page) (*CustomerList, error) {
	q = strings.TrimSpace(q)
	var byID int64
	if m := regexp.MustCompile(`(?i)^M-?0*(\d{1,15})$`).FindStringSubmatch(q); m != nil {
		for _, d := range m[1] {
			byID = byID*10 + int64(d-'0')
		}
	}
	const where = `($1 = '' OR c.phone ILIKE $1 OR c.nickname ILIKE $1 OR c.id = $2)`
	pat := ""
	if q != "" {
		pat = like(q)
	}
	out := &CustomerList{Items: []AdminCustomer{}}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM customers c WHERE `+where, pat, byID).Scan(&out.Total); err != nil {
		return nil, err
	}
	limit, offset := p.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT c.id, c.phone, c.nickname, c.created_at, c.last_login_at,
		(SELECT count(*) FROM orders o WHERE o.customer_id = c.id)
		FROM customers c WHERE `+where+` ORDER BY c.id DESC LIMIT $3 OFFSET $4`, pat, byID, limit, offset)
	if err != nil {
		return nil, err
	}
	out.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (AdminCustomer, error) {
		c := AdminCustomer{Wallets: []Wallet{}}
		err := r.Scan(&c.ID, &c.Phone, &c.Nickname, &c.CreatedAt, &c.LastLoginAt, &c.Orders)
		c.MemberNo = MemberNo(c.ID)
		return c, err
	})
	if err != nil {
		return nil, err
	}
	for i := range out.Items {
		w, err := wallets(ctx, a.DB, out.Items[i].ID)
		if err != nil {
			return nil, err
		}
		if w != nil {
			out.Items[i].Wallets = w
		}
	}
	return out, nil
}

type WalletAdjust struct {
	Account  string `json:"account"`
	Currency string `json:"currency"`
	// Amount is in minor units; negative takes away.
	Amount int64  `json:"amount"`
	Note   string `json:"note"`
}

var accountNames = map[string][2]string{"balance": {"余额", "balance"}, "bonus": {"奖励金", "rewards"}}

// AdminAdjustWallet writes one ledger entry. An account never goes below zero.
func (a *App) AdminAdjustWallet(ctx context.Context, actor AdminActor, customerID int64, in WalletAdjust) ([]Wallet, error) {
	in.Note, in.Currency = strings.TrimSpace(in.Note), strings.ToUpper(strings.TrimSpace(in.Currency))
	name, ok := accountNames[in.Account]
	switch {
	case !ok:
		return nil, ErrValidation("account must be balance or bonus")
	case !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(in.Currency):
		return nil, ErrValidation("currency must be a three-letter code")
	case in.Amount == 0 || in.Amount > 1e11 || in.Amount < -1e11:
		return nil, ErrValidation("enter an amount")
	case in.Note == "" || utf8.RuneCountInString(in.Note) > 200:
		return nil, ErrValidation("say what the change is for, in up to 200 characters")
	}
	var out []Wallet
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		// one adjustment at a time per customer
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT true FROM customers WHERE id=$1 FOR UPDATE`, customerID).Scan(&exists); isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var have int64
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(amount), 0)::bigint FROM customer_wallet_entries
			WHERE customer_id=$1 AND account=$2 AND currency=$3`, customerID, in.Account, in.Currency).Scan(&have); err != nil {
			return err
		}
		if have+in.Amount < 0 {
			return Err(http.StatusConflict, "WALLET_SHORT", "the account does not hold that much")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO customer_wallet_entries (customer_id, account, currency, amount, note, admin_id)
			VALUES ($1,$2,$3,$4,$5,$6)`, customerID, in.Account, in.Currency, in.Amount, in.Note, actor.ID); err != nil {
			return err
		}
		verb := [2]string{"增加", "added to"}
		if in.Amount < 0 {
			verb = [2]string{"减少", "taken from"}
		}
		if err := tell(ctx, tx, customerID, Message{Kind: "wallet", Body: in.Note, BodyEn: in.Note, Link: "/me/wallet/" + in.Account,
			Title: name[0] + verb[0] + "了", TitleEn: "Something was " + verb[1] + " your " + name[1],
			Key: map[bool]string{true: "wallet_added", false: "wallet_taken"}[in.Amount >= 0] + "." + in.Account}); err != nil {
			return err
		}
		if err := adminAudit(ctx, tx, actor, "customer.wallet", "customer", customerID,
			map[string]any{"account": in.Account, "currency": in.Currency, "amount": in.Amount, "note": in.Note}); err != nil {
			return err
		}
		var err error
		out, err = wallets(ctx, tx, customerID)
		return err
	})
	return out, err
}

// AdminSavePlatformInfo replaces the texts about the platform.
func (a *App) AdminSavePlatformInfo(ctx context.Context, actor AdminActor, values map[string]string) (map[string]string, error) {
	known := map[string]bool{}
	for _, k := range PlatformInfoKeys {
		known[k] = true
	}
	for k, v := range values {
		if !known[k] {
			return nil, ErrValidation("unknown field " + k)
		}
		if utf8.RuneCountInString(v) > 4000 {
			return nil, ErrValidation(k + " is too long")
		}
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		keys := []string{}
		for k, v := range values {
			if _, err := tx.Exec(ctx, `INSERT INTO platform_info (key, value) VALUES ($1,$2)
				ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, k, strings.TrimSpace(v)); err != nil {
				return err
			}
			keys = append(keys, k)
		}
		return adminAudit(ctx, tx, actor, "settings.platform_info", "settings", 0, map[string]any{"keys": keys})
	})
	if err != nil {
		return nil, err
	}
	return a.PlatformInfo(ctx)
}
