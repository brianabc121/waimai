package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"strings"
	"time"
	"unicode/utf8"
)

// Chat, second part: pictures, order cards, quick replies and the answer a
// closed store gives by itself.

// chatCard is the content of a message that is not a text. It travels through
// OpenIM as a "custom" message; the callbacks check it (ChatHook.read).
type chatCard struct {
	T     string `json:"t"`               // image or order
	Token string `json:"token,omitempty"` // of a picture uploaded with AddChatImage
	Order int64  `json:"order,omitempty"`
}

const (
	chatImagePath = "/api/v1/chat-images/"
	// chatOrdersShown is how many of a customer's orders a conversation knows.
	chatOrdersShown = 10
	// chatImagesPerHour caps the pictures one side uploads to one conversation.
	chatImagesPerHour = 30
	chatQuickMax      = 20
	chatQuickLen      = 200
	// chatAutoEvery is how often one conversation gets the closed store's answer.
	chatAutoEvery = 6 * time.Hour
)

// AddChatImage keeps a picture one side ("customer" or "store") is about to
// send, and returns its token. The browser shrinks it to a JPEG first.
func (a *App) AddChatImage(ctx context.Context, side string, branchID, customerID int64, image []byte) (string, error) {
	if !isJPEG(image, ImageMaxBytes) {
		return "", ErrValidation("the picture must be a JPEG of at most 1 MB")
	}
	var n int
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM chat_images WHERE branch_id=$1 AND customer_id=$2 AND sender=$3
		AND created_at > now() - interval '1 hour'`, branchID, customerID, side).Scan(&n); err != nil {
		return "", err
	}
	if n >= chatImagesPerHour {
		return "", Err(429, "CHAT_TOO_FAST", "too many pictures, wait a while")
	}
	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tok)
	tag, err := a.DB.Exec(ctx, `INSERT INTO chat_images (token, customer_id, branch_id, sender, content)
		SELECT $1, c.id, b.id, $4, $5 FROM customers c, branches b WHERE c.id=$2 AND b.id=$3 AND b.deleted_at IS NULL`,
		token, customerID, branchID, side, image)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return token, nil
}

// CustomerChatImage is AddChatImage for a customer, who names the store by its slug.
func (a *App) CustomerChatImage(ctx context.Context, customerID int64, slug string, image []byte) (string, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return "", err
	}
	return a.AddChatImage(ctx, "customer", b.ID, customerID, image)
}

// ChatImage returns a picture by its token. The token is the permission: only
// the two sides of the conversation (and the platform) ever see it.
func (a *App) ChatImage(ctx context.Context, token string) ([]byte, error) {
	var image []byte
	err := a.DB.QueryRow(ctx, `SELECT content FROM chat_images WHERE token=$1`, token).Scan(&image)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	return image, err
}

// ChatCustomerOfOrder says which customer an order of the store belongs to,
// for "message the customer" on the order desk. An order placed without an
// account has nobody to write to.
func (a *App) ChatCustomerOfOrder(ctx context.Context, branchID, orderID int64) (int64, error) {
	var id *int64
	err := a.DB.QueryRow(ctx, `SELECT customer_id FROM orders WHERE id=$1 AND branch_id=$2`, orderID, branchID).Scan(&id)
	if isNoRows(err) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if id == nil {
		return 0, Err(409, "CHAT_NO_ACCOUNT", "this order was placed without an account; call the customer instead")
	}
	return *id, nil
}

// ---- the store's own settings ----

type QuickReply struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// ChatSetup is what the store's message page keeps beside the conversations.
type ChatSetup struct {
	QuickReplies []QuickReply `json:"quick_replies"`
	AutoReplyOn  bool         `json:"auto_reply_on"`
	AutoReply    string       `json:"auto_reply"` // "": the standard sentence
}

func (a *App) ChatSetup(ctx context.Context, branchID int64) (*ChatSetup, error) {
	out := &ChatSetup{QuickReplies: []QuickReply{}}
	if err := a.DB.QueryRow(ctx, `SELECT chat_auto_reply_on, chat_auto_reply FROM branches WHERE id=$1`, branchID).
		Scan(&out.AutoReplyOn, &out.AutoReply); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT id, body FROM chat_quick_replies WHERE branch_id=$1 ORDER BY id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var q QuickReply
		if err := rows.Scan(&q.ID, &q.Body); err != nil {
			return nil, err
		}
		out.QuickReplies = append(out.QuickReplies, q)
	}
	return out, rows.Err()
}

func (a *App) AddQuickReply(ctx context.Context, branchID int64, body string) (*QuickReply, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > chatQuickLen {
		return nil, ErrValidation("a quick reply has 1 to 200 characters")
	}
	var n int
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM chat_quick_replies WHERE branch_id=$1`, branchID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= chatQuickMax {
		return nil, ErrValidation("a store keeps 20 quick replies at most")
	}
	q := &QuickReply{Body: body}
	err := a.DB.QueryRow(ctx, `INSERT INTO chat_quick_replies (branch_id, body) VALUES ($1,$2) RETURNING id`, branchID, body).Scan(&q.ID)
	return q, err
}

func (a *App) DeleteQuickReply(ctx context.Context, branchID, id int64) error {
	tag, err := a.DB.Exec(ctx, `DELETE FROM chat_quick_replies WHERE id=$1 AND branch_id=$2`, id, branchID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (a *App) SetChatAutoReply(ctx context.Context, branchID int64, on bool, text string) error {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > chatQuickLen {
		return ErrValidation("the automatic answer has 200 characters at most")
	}
	_, err := a.DB.Exec(ctx, `UPDATE branches SET chat_auto_reply_on=$2, chat_auto_reply=$3 WHERE id=$1`, branchID, on, text)
	return err
}

// chatAutoReply answers for a store that is closed, once in a while, after a
// customer wrote. It never fails the callback it is part of.
func (a *App) chatAutoReply(ctx context.Context, branchID, customerID int64) {
	if a.IM == nil {
		return
	}
	b, err := a.BranchByID(ctx, branchID)
	if err != nil || (b.takesOrders() && b.isOpen(time.Now())) {
		return
	}
	var text string
	// the UPDATE both decides and remembers, so two callbacks cannot both answer
	err = a.DB.QueryRow(ctx, `UPDATE chat_threads t SET auto_replied_at=now() FROM branches b
		WHERE t.branch_id=$1 AND t.customer_id=$2 AND b.id=t.branch_id AND b.chat_auto_reply_on
			AND (t.auto_replied_at IS NULL OR t.auto_replied_at < now() - make_interval(secs => $3))
		RETURNING b.chat_auto_reply`, branchID, customerID, chatAutoEvery.Seconds()).Scan(&text)
	if err != nil {
		if !isNoRows(err) {
			log.Printf("chat: automatic answer of store %d: %v", branchID, err)
		}
		return
	}
	if text == "" {
		text = "现在不在营业时间，营业后会尽快回复您。We are closed now and will answer when we open."
	}
	if err := a.IM.SendText(ctx, branchIM(branchID), customerIM(customerID), text, `{"auto":true}`); err != nil {
		log.Printf("chat: automatic answer of store %d: %v", branchID, err)
	}
}
