package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Chat between a customer and a store (docs/CHAT_DESIGN.md). The browsers
// talk to OpenIM directly. This file is everything around that: who may sign
// in to it as whom, the rules OpenIM asks about before it passes a message
// on, and the copy of each message that the lists, the unread badges and the
// platform's "all conversations" page read.

var (
	ErrChatOff     = Err(http.StatusServiceUnavailable, "CHAT_OFF", "chat is not available")
	errChatClosed  = Err(http.StatusForbidden, "CHAT_CLOSED", "this store's chat is closed")
	errChatMuted   = Err(http.StatusForbidden, "CHAT_MUTED", "you cannot send messages")
	errChatNoOrder = Err(http.StatusForbidden, "CHAT_NO_ORDER", "a store can only write to a customer who ordered in the last 7 days")
)

const (
	chatTextMax = 1000
	// chatRate is how many messages one side may send in a conversation per minute.
	chatRate = 20
	// chatNewStores is how many stores a customer may write to for the first time per day.
	chatNewStores = 20
	// chatReachDays is how long after an order the store may write first.
	chatReachDays = 7
	chatPreview   = 120
)

// OpenIM's message types that this system lets through.
// OpenIM's numbers for a text and for a message whose content is this
// system's own (a picture or an order card, see chatCard).
const (
	imText   = 101
	imCustom = 110
)

func customerIM(id int64) string { return "c_" + strconv.FormatInt(id, 10) }
func branchIM(id int64) string   { return "b_" + strconv.FormatInt(id, 10) }

// imParty reads c_<id> or b_<id>.
func imParty(userID string) (kind string, id int64) {
	if len(userID) < 3 || userID[1] != '_' {
		return "", 0
	}
	n, err := strconv.ParseInt(userID[2:], 10, 64)
	if err != nil || n <= 0 {
		return "", 0
	}
	switch userID[0] {
	case 'c':
		return "customer", n
	case 'b':
		return "store", n
	}
	return "", 0
}

// ChatOn reports whether a chat server is configured.
func (a *App) ChatOn() bool { return a.IM != nil }

// ChatLogin is what a browser needs to sign in to the chat server.
type ChatLogin struct {
	UserID     string `json:"user_id"`
	Token      string `json:"token"`
	WSURL      string `json:"ws_url"`
	APIURL     string `json:"api_url"`
	PlatformID int    `json:"platform_id"`
}

// chatAccount makes sure the account exists in OpenIM. nickname is what
// OpenIM shows other parties: a store's name, and no more than "customer" for
// a customer (OpenIM refuses an empty one).
func (a *App) chatAccount(ctx context.Context, userID, nickname string) error {
	if a.IM == nil {
		return ErrChatOff
	}
	var known bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_accounts WHERE im_user_id=$1)`, userID).Scan(&known); err != nil || known {
		return err
	}
	if err := a.IM.EnsureUser(ctx, userID, nickname); err != nil {
		return chatDown(err)
	}
	_, err := a.DB.Exec(ctx, `INSERT INTO chat_accounts (im_user_id) VALUES ($1) ON CONFLICT DO NOTHING`, userID)
	return err
}

func (a *App) customerAccount(ctx context.Context, customerID int64) error {
	return a.chatAccount(ctx, customerIM(customerID), "customer")
}

func (a *App) storeAccount(ctx context.Context, branchID int64) error {
	var name string
	if err := a.DB.QueryRow(ctx, `SELECT name FROM branches WHERE id=$1`, branchID).Scan(&name); err != nil {
		return err
	}
	return a.chatAccount(ctx, branchIM(branchID), name)
}

// chatLogin returns a token for an account that exists.
func (a *App) chatLogin(ctx context.Context, userID string) (*ChatLogin, error) {
	token, err := a.IM.UserToken(ctx, userID)
	if err != nil {
		return nil, chatDown(err)
	}
	return &ChatLogin{UserID: userID, Token: token, WSURL: a.base.OpenIMPublicWS, APIURL: a.base.OpenIMPublicAPI, PlatformID: imPlatformWeb}, nil
}

func chatDown(err error) error {
	return Err(http.StatusBadGateway, "CHAT_DOWN", "the chat server did not answer: "+err.Error())
}

func (a *App) CustomerChatLogin(ctx context.Context, customerID int64) (*ChatLogin, error) {
	if err := a.customerAccount(ctx, customerID); err != nil {
		return nil, err
	}
	return a.chatLogin(ctx, customerIM(customerID))
}

func (a *App) StoreChatLogin(ctx context.Context, branchID int64) (*ChatLogin, error) {
	if err := a.storeAccount(ctx, branchID); err != nil {
		return nil, err
	}
	return a.chatLogin(ctx, branchIM(branchID))
}

// ---- lists ----

// OrderTag is the order a conversation is about right now.
type OrderTag struct {
	ID       int64  `json:"id"`
	PublicNo string `json:"public_no"`
	Status   string `json:"status"`
}

// ChatThread is one conversation in a list. Which fields are set depends on
// who is looking: a customer sees the store, a store the customer, the
// platform both.
type ChatThread struct {
	ID         int64      `json:"id"`
	BranchID   int64      `json:"branch_id"`
	Slug       string     `json:"slug,omitempty"`
	StoreName  string     `json:"store_name,omitempty"`
	CustomerID int64      `json:"customer_id,omitempty"`
	Customer   string     `json:"customer,omitempty"`
	PhoneTail  string     `json:"phone_tail,omitempty"`
	Peer       string     `json:"peer"` // the other side's OpenIM account
	LastKind   string     `json:"last_kind"`
	LastText   string     `json:"last_text"`
	LastSender string     `json:"last_sender"`
	LastAt     time.Time  `json:"last_at"`
	Unread     int        `json:"unread"`
	Count      int        `json:"count"`
	PeerReadAt *time.Time `json:"peer_read_at"`
	Order      *OrderTag  `json:"order"`
}

// activeOrder is the lateral join that finds a conversation's open order.
const activeOrder = `LEFT JOIN LATERAL (SELECT o.id, o.public_no, o.acceptance_status, o.fulfillment_status FROM orders o
	WHERE o.branch_id=t.branch_id AND o.customer_id=t.customer_id AND o.closed_at IS NULL AND o.channel <> 'dine_in'
	  AND o.acceptance_status IN ('pending','accepted')
	ORDER BY o.id DESC LIMIT 1) ao ON true`

// customerName is how a store and the platform call a customer: the nickname,
// else the name on the latest order in that store.
const customerName = `COALESCE(NULLIF(c.nickname,''), (SELECT o.customer_name FROM orders o
	WHERE o.branch_id=t.branch_id AND o.customer_id=t.customer_id ORDER BY o.id DESC LIMIT 1), '')`

func orderTag(id *int64, no, acceptance, fulfillment *string) *OrderTag {
	if id == nil {
		return nil
	}
	return &OrderTag{ID: *id, PublicNo: *no, Status: DisplayStatus(*acceptance, *fulfillment)}
}

// CustomerChats lists the stores a customer has talked with.
func (a *App) CustomerChats(ctx context.Context, customerID int64) ([]ChatThread, error) {
	rows, err := a.DB.Query(ctx, `SELECT t.id, t.branch_id, b.slug, b.name, t.last_kind, t.last_text, t.last_sender, t.last_at,
			t.customer_unread, t.msg_count, t.store_read_at, ao.id, ao.public_no, ao.acceptance_status, ao.fulfillment_status
		FROM chat_threads t JOIN branches b ON b.id=t.branch_id `+activeOrder+`
		WHERE t.customer_id=$1 AND b.deleted_at IS NULL ORDER BY t.last_at DESC LIMIT 200`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatThread{}
	for rows.Next() {
		var t ChatThread
		var oid *int64
		var no, acc, ful *string
		if err := rows.Scan(&t.ID, &t.BranchID, &t.Slug, &t.StoreName, &t.LastKind, &t.LastText, &t.LastSender, &t.LastAt,
			&t.Unread, &t.Count, &t.PeerReadAt, &oid, &no, &acc, &ful); err != nil {
			return nil, err
		}
		t.Peer, t.Order = branchIM(t.BranchID), orderTag(oid, no, acc, ful)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Unread is what the message button and the "my messages" row count.
type Unread struct {
	Chats   int `json:"chats"`
	Notices int `json:"notices"`
	Total   int `json:"total"`
}

func (a *App) CustomerUnread(ctx context.Context, customerID int64) (*Unread, error) {
	var u Unread
	err := a.DB.QueryRow(ctx, `SELECT
		(SELECT COALESCE(sum(customer_unread),0) FROM chat_threads WHERE customer_id=$1),
		(SELECT count(*) FROM customer_messages WHERE customer_id=$1 AND read_at IS NULL)`, customerID).Scan(&u.Chats, &u.Notices)
	u.Total = u.Chats + u.Notices
	return &u, err
}

// ChatFilter narrows a store's or the platform's list.
type ChatFilter struct {
	Q        string
	Unread   bool
	Active   bool // has an open order
	Reported bool
	Today    bool
	BranchID int64
	Page
}

func scanStoreThread(rows pgx.Rows) (ChatThread, error) {
	var t ChatThread
	var oid *int64
	var no, acc, ful *string
	err := rows.Scan(&t.ID, &t.BranchID, &t.CustomerID, &t.Customer, &t.PhoneTail, &t.LastKind, &t.LastText, &t.LastSender, &t.LastAt,
		&t.Unread, &t.Count, &t.PeerReadAt, &oid, &no, &acc, &ful)
	t.Peer, t.Order = customerIM(t.CustomerID), orderTag(oid, no, acc, ful)
	return t, err
}

const storeThreadCols = `t.id, t.branch_id, t.customer_id, ` + customerName + `, right(c.phone, 4), t.last_kind, t.last_text,
	t.last_sender, t.last_at, t.store_unread, t.msg_count, t.customer_read_at, ao.id, ao.public_no, ao.acceptance_status, ao.fulfillment_status`

// StoreChats lists a store's conversations: the ones with an open order
// first, then the latest.
func (a *App) StoreChats(ctx context.Context, branchID int64, f ChatFilter) ([]ChatThread, int, error) {
	where, args := `t.branch_id=$1`, []any{branchID}
	if f.Unread {
		where += ` AND t.store_unread > 0`
	}
	if f.Active {
		where += ` AND ao.id IS NOT NULL`
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		args = append(args, like(q))
		where += fmt.Sprintf(` AND (%s ILIKE $%d OR c.phone ILIKE $%d OR EXISTS (SELECT 1 FROM orders o
			WHERE o.branch_id=t.branch_id AND o.customer_id=t.customer_id AND o.public_no ILIKE $%d))`, customerName, len(args), len(args), len(args))
	}
	rows, err := a.DB.Query(ctx, `SELECT `+storeThreadCols+` FROM chat_threads t JOIN customers c ON c.id=t.customer_id `+activeOrder+`
		WHERE `+where+` ORDER BY (ao.id IS NOT NULL) DESC, t.last_at DESC LIMIT 300`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ChatThread{}
	for rows.Next() {
		t, err := scanStoreThread(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	unread, err := a.StoreUnread(ctx, branchID)
	return out, unread, err
}

// StoreUnread is the number on the store's "messages" entry.
func (a *App) StoreUnread(ctx context.Context, branchID int64) (int, error) {
	var n int
	err := a.DB.QueryRow(ctx, `SELECT COALESCE(sum(store_unread),0) FROM chat_threads WHERE branch_id=$1`, branchID).Scan(&n)
	return n, err
}

// ---- one conversation ----

// ChatOrder is an order shown beside a conversation.
type ChatOrder struct {
	ID        int64     `json:"id"`
	PublicNo  string    `json:"public_no"`
	Status    string    `json:"status"`
	Service   string    `json:"service_type"`
	Total     int64     `json:"total"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"created_at"`
	Open      bool      `json:"open"`
	// Token opens the order's page; only the customer's side gets it.
	Token string `json:"token,omitempty"`
	nonce []byte
}

func chatOrders(ctx context.Context, q Querier, branchID, customerID int64, limit int) ([]ChatOrder, error) {
	rows, err := q.Query(ctx, `SELECT id, public_no, acceptance_status, fulfillment_status, service_type, total, currency, created_at,
			closed_at IS NULL AND acceptance_status IN ('pending','accepted'), token_nonce
		FROM orders WHERE branch_id=$1 AND customer_id=$2 AND channel <> 'dine_in' ORDER BY id DESC LIMIT $3`, branchID, customerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatOrder{}
	for rows.Next() {
		var o ChatOrder
		var acc, ful string
		if err := rows.Scan(&o.ID, &o.PublicNo, &acc, &ful, &o.Service, &o.Total, &o.Currency, &o.CreatedAt, &o.Open, &o.nonce); err != nil {
			return nil, err
		}
		o.Status = DisplayStatus(acc, ful)
		out = append(out, o)
	}
	return out, rows.Err()
}

// CustomerChat is what the customer's chat page with one store starts from.
type CustomerChat struct {
	BranchID    int64      `json:"branch_id"`
	Slug        string     `json:"slug"`
	StoreName   string     `json:"store_name"`
	StoreNameEn string     `json:"store_name_en"`
	Phone       string     `json:"phone"`
	OpenNow     bool       `json:"open_now"`
	OpenTime    string     `json:"open_time"`
	Peer        string     `json:"peer"`
	Enabled     bool       `json:"enabled"` // false: the platform closed this store's chat
	Muted       bool       `json:"muted"`   // the platform stopped this customer from writing
	ThreadID    int64      `json:"thread_id"`
	PeerReadAt  *time.Time `json:"peer_read_at"`
	// the customer's orders here: what an order card shows, and what can be sent as one
	Orders []ChatOrder `json:"orders"`
}

func (a *App) CustomerChat(ctx context.Context, customerID int64, slug string) (*CustomerChat, error) {
	b, err := branchBySlug(ctx, a.DB, slug)
	if err != nil {
		return nil, err
	}
	out := &CustomerChat{BranchID: b.ID, Slug: b.Slug, StoreName: b.Name, StoreNameEn: b.NameEn, Phone: b.Phone,
		OpenNow: b.takesOrders() && b.isOpen(time.Now()), OpenTime: b.OpenTime, Peer: branchIM(b.ID)}
	if err := a.DB.QueryRow(ctx, `SELECT chat_enabled FROM branches WHERE id=$1`, b.ID).Scan(&out.Enabled); err != nil {
		return nil, err
	}
	if err := a.DB.QueryRow(ctx, `SELECT chat_muted_at IS NOT NULL FROM customers WHERE id=$1`, customerID).Scan(&out.Muted); err != nil {
		return nil, err
	}
	if out.Orders, err = chatOrders(ctx, a.DB, b.ID, customerID, chatOrdersShown); err != nil {
		return nil, err
	}
	for i := range out.Orders {
		out.Orders[i].Token = a.orderToken(out.Orders[i].nonce)
	}
	// the store may never have opened its chat: its account must exist to be written to
	if err := a.storeAccount(ctx, b.ID); err != nil {
		return nil, err
	}
	err = a.DB.QueryRow(ctx, `SELECT id, store_read_at FROM chat_threads WHERE customer_id=$1 AND branch_id=$2`, customerID, b.ID).
		Scan(&out.ThreadID, &out.PeerReadAt)
	if err != nil && !isNoRows(err) {
		return nil, err
	}
	return out, nil
}

// StoreChat is what the store's chat pane with one customer starts from.
type StoreChat struct {
	ChatThread
	Phone  string      `json:"phone_masked"`
	Orders []ChatOrder `json:"orders"`
	Total  int         `json:"order_count"`
}

// StoreChatWith opens the conversation with a customer. There may be no
// messages yet: the store can write first to someone who ordered recently.
func (a *App) StoreChatWith(ctx context.Context, branchID, customerID int64) (*StoreChat, error) {
	out := &StoreChat{}
	var phone string
	err := a.DB.QueryRow(ctx, `SELECT c.phone, COALESCE(NULLIF(c.nickname,''), (SELECT o.customer_name FROM orders o
			WHERE o.branch_id=$1 AND o.customer_id=c.id ORDER BY o.id DESC LIMIT 1), ''),
			(SELECT count(*) FROM orders o WHERE o.branch_id=$1 AND o.customer_id=c.id AND o.channel <> 'dine_in')
		FROM customers c WHERE c.id=$2`, branchID, customerID).Scan(&phone, &out.Customer, &out.Total)
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.BranchID, out.CustomerID, out.Peer = branchID, customerID, customerIM(customerID)
	out.Phone, out.PhoneTail = "•••• "+last4(phone), last4(phone)
	err = a.DB.QueryRow(ctx, `SELECT id, last_kind, last_text, last_sender, last_at, store_unread, msg_count, customer_read_at
		FROM chat_threads WHERE branch_id=$1 AND customer_id=$2`, branchID, customerID).
		Scan(&out.ID, &out.LastKind, &out.LastText, &out.LastSender, &out.LastAt, &out.Unread, &out.Count, &out.PeerReadAt)
	if err != nil && !isNoRows(err) {
		return nil, err
	}
	if out.ID == 0 && out.Total == 0 {
		return nil, ErrNotFound // neither talked nor ordered here: not this store's customer
	}
	if err := a.customerAccount(ctx, customerID); err != nil {
		return nil, err
	}
	if out.Orders, err = chatOrders(ctx, a.DB, branchID, customerID, chatOrdersShown); err != nil {
		return nil, err
	}
	for _, o := range out.Orders {
		if o.Open {
			out.Order = &OrderTag{ID: o.ID, PublicNo: o.PublicNo, Status: o.Status}
			break
		}
	}
	return out, nil
}

// ReadChat marks a conversation read by one side ("customer" or "store") and
// tells the store's screens and the chat server.
func (a *App) ReadChat(ctx context.Context, side string, branchID, customerID int64) error {
	col := "customer"
	if side == "store" {
		col = "store"
	}
	tag, err := a.DB.Exec(ctx, `UPDATE chat_threads SET `+col+`_unread=0, `+col+`_read_at=now()
		WHERE branch_id=$1 AND customer_id=$2`, branchID, customerID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	// the chat server tells the other side's open pages, which then show "read"
	if a.IM != nil {
		me, peer := customerIM(customerID), branchIM(branchID)
		if side == "store" {
			me, peer = peer, me
		}
		if err := a.IM.MarkRead(ctx, me, peer); err != nil {
			log.Printf("chat: mark read %s: %v", me, err)
		}
	}
	return notify(ctx, a.DB, Event{BranchID: branchID})
}

// ---- OpenIM's callbacks ----

// ChatHook is the part of a callback's body this system reads.
type ChatHook struct {
	SendID      string `json:"sendID"`
	RecvID      string `json:"recvID"`
	ContentType int    `json:"contentType"`
	SessionType int    `json:"sessionType"`
	Content     string `json:"content"`
	Ex          string `json:"ex"`
	ServerMsgID string `json:"serverMsgID"`
	ClientMsgID string `json:"clientMsgID"`
	SendTime    int64  `json:"sendTime"`
}

// chatMsg is a callback's message once it is understood.
type chatMsg struct {
	sender     string // customer or store
	customerID int64
	branchID   int64
	staffID    int64
	kind       string
	body       string // the text; a picture's token
	orderID    int64
	auto       bool // the store's own answer while closed, sent by this system
}

// read works out who is writing to whom and what. Only a customer and a store
// can talk, one to one, in the kinds of message this system knows.
func (h *ChatHook) read() (*chatMsg, error) {
	fromKind, fromID := imParty(h.SendID)
	toKind, toID := imParty(h.RecvID)
	m := &chatMsg{sender: fromKind}
	switch {
	case h.SessionType != 1:
		return nil, ErrValidation("only one-to-one chat is supported")
	case fromKind == "customer" && toKind == "store":
		m.customerID, m.branchID = fromID, toID
	case fromKind == "store" && toKind == "customer":
		m.customerID, m.branchID = toID, fromID
	default:
		return nil, ErrValidation("a chat is between a customer and a store")
	}
	switch h.ContentType {
	case imText:
		var c struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(h.Content), &c); err != nil {
			return nil, ErrValidation("unreadable message")
		}
		m.kind, m.body = "text", strings.TrimSpace(c.Content)
		if m.body == "" {
			return nil, ErrValidation("the message is empty")
		}
		if utf8.RuneCountInString(m.body) > chatTextMax {
			return nil, ErrValidation(fmt.Sprintf("a message can have %d characters at most", chatTextMax))
		}
	case imCustom:
		// {"data": "<chatCard as JSON>"}: a picture uploaded here before, or an order
		var c struct {
			Data string `json:"data"`
		}
		var card chatCard
		if json.Unmarshal([]byte(h.Content), &c) != nil || json.Unmarshal([]byte(c.Data), &card) != nil {
			return nil, ErrValidation("unreadable message")
		}
		switch {
		case card.T == "image" && card.Token != "":
			m.kind, m.body = "image", card.Token
		case card.T == "order" && card.Order > 0:
			m.kind, m.orderID = "order", card.Order
		default:
			return nil, ErrValidation("this kind of message is not supported")
		}
	default:
		return nil, ErrValidation("this kind of message is not supported")
	}
	if m.sender == "store" {
		var ex struct {
			UID  int64 `json:"uid"`
			Auto bool  `json:"auto"`
		}
		_ = json.Unmarshal([]byte(h.Ex), &ex)
		m.staffID, m.auto = ex.UID, ex.Auto && ex.UID == 0 && m.kind == "text"
	}
	return m, nil
}

// ChatBeforeSend answers OpenIM's question "may this message go out?". An
// error is the reason it may not.
func (a *App) ChatBeforeSend(ctx context.Context, h ChatHook) error {
	m, err := h.read()
	if err != nil {
		return err
	}
	var enabled, deleted, muted, exists bool
	err = a.DB.QueryRow(ctx, `SELECT b.chat_enabled, b.deleted_at IS NOT NULL, c.chat_muted_at IS NOT NULL,
			EXISTS (SELECT 1 FROM chat_threads t WHERE t.branch_id=b.id AND t.customer_id=c.id)
		FROM branches b, customers c WHERE b.id=$1 AND c.id=$2`, m.branchID, m.customerID).Scan(&enabled, &deleted, &muted, &exists)
	if isNoRows(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !enabled || deleted {
		return errChatClosed
	}
	if m.sender == "customer" {
		if muted {
			return errChatMuted
		}
		if !exists {
			var n int
			if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM chat_threads WHERE customer_id=$1 AND created_at > now() - interval '1 day'`,
				m.customerID).Scan(&n); err != nil {
				return err
			}
			if n >= chatNewStores {
				return Err(http.StatusTooManyRequests, "CHAT_TOO_MANY", "too many new conversations today")
			}
		}
	} else if !m.auto {
		// the browser says which member of staff is writing; it must be one of this store's desk
		var ok bool
		if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_branch_roles r JOIN users u ON u.id=r.user_id
			WHERE r.branch_id=$1 AND r.user_id=$2 AND u.active AND r.role IN ('owner','manager','cashier'))`, m.branchID, m.staffID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return Err(http.StatusForbidden, "FORBIDDEN", "only the store's desk can answer")
		}
		if !exists {
			// a store writes first only to someone who ordered from it lately
			if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders WHERE branch_id=$1 AND customer_id=$2
				AND created_at > now() - make_interval(days => $3))`, m.branchID, m.customerID, chatReachDays).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return errChatNoOrder
			}
		}
	}
	// a picture must be one this side uploaded for this conversation, an
	// order one this customer placed with this store
	var ok bool
	switch m.kind {
	case "image":
		err = a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_images WHERE token=$1 AND branch_id=$2 AND customer_id=$3 AND sender=$4)`,
			m.body, m.branchID, m.customerID, m.sender).Scan(&ok)
	case "order":
		err = a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders WHERE id=$1 AND branch_id=$2 AND customer_id=$3)`,
			m.orderID, m.branchID, m.customerID).Scan(&ok)
	default:
		ok = true
	}
	if err != nil {
		return err
	}
	if !ok {
		return ErrValidation("the picture or the order does not belong to this conversation")
	}
	var recent int
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM chat_messages m JOIN chat_threads t ON t.id=m.thread_id
		WHERE t.branch_id=$1 AND t.customer_id=$2 AND m.sender=$3 AND m.sent_at > now() - interval '1 minute'`,
		m.branchID, m.customerID, m.sender).Scan(&recent); err != nil {
		return err
	}
	if recent >= chatRate {
		return Err(http.StatusTooManyRequests, "CHAT_TOO_FAST", "too many messages, wait a moment")
	}
	return nil
}

func preview(kind, body string) string {
	if kind != "text" && kind != "system" {
		return ""
	}
	body = strings.Join(strings.Fields(body), " ")
	if r := []rune(body); len(r) > chatPreview {
		return string(r[:chatPreview]) + "…"
	}
	return body
}

// ChatAfterSend keeps this system's copy of a message OpenIM has passed on.
// A callback that comes twice is stored once.
func (a *App) ChatAfterSend(ctx context.Context, h ChatHook) error {
	m, err := h.read()
	if err != nil {
		return nil // nothing this system keeps
	}
	if h.ServerMsgID == "" {
		return ErrValidation("serverMsgID is required")
	}
	at := time.Now()
	if h.SendTime > 0 {
		at = time.UnixMilli(h.SendTime)
	}
	var fresh bool // false: a callback that was repeated
	err = a.inTx(ctx, func(tx pgx.Tx) error {
		var threadID int64
		err := tx.QueryRow(ctx, `INSERT INTO chat_threads (customer_id, branch_id, last_at) VALUES ($1,$2,$3)
			ON CONFLICT (customer_id, branch_id) DO UPDATE SET customer_id=EXCLUDED.customer_id RETURNING id`,
			m.customerID, m.branchID, at).Scan(&threadID)
		if err != nil {
			return err
		}
		var staff *int64
		if m.sender == "store" && m.staffID != 0 {
			staff = &m.staffID
		}
		var order *int64
		if m.orderID != 0 {
			order = &m.orderID
		}
		tag, err := tx.Exec(ctx, `INSERT INTO chat_messages (thread_id, sender, staff_user_id, kind, body, order_id, im_msg_id, sent_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (im_msg_id) DO NOTHING`, threadID, m.sender, staff, m.kind, m.body, order, h.ServerMsgID, at)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		fresh = true
		// The sender has, by writing, read what came before. The other side
		// may have the conversation open and have read this message before
		// this callback arrived: then it does not count as unread.
		unread := `store_unread = store_unread + CASE WHEN store_read_at >= $5 THEN 0 ELSE 1 END,
			customer_unread = 0, customer_read_at = GREATEST(customer_read_at, $5)`
		if m.sender == "store" {
			unread = `customer_unread = customer_unread + CASE WHEN customer_read_at >= $5 THEN 0 ELSE 1 END,
				store_unread = 0, store_read_at = GREATEST(store_read_at, $5)`
		}
		if _, err := tx.Exec(ctx, `UPDATE chat_threads SET last_kind=$2, last_text=$3, last_sender=$4, last_at=$5,
			msg_count = msg_count + 1, `+unread+` WHERE id=$1`, threadID, m.kind, preview(m.kind, m.body), m.sender, at); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: m.branchID})
	})
	if err == nil && fresh && m.sender == "customer" {
		a.chatAutoReply(ctx, m.branchID, m.customerID)
	}
	return err
}

// ---- the platform ----

// AdminChatRow is one conversation on the platform's "all conversations" page.
type AdminChatRow struct {
	ChatThread
	Phone       string `json:"phone"`
	Merchant    string `json:"merchant"`
	Muted       bool   `json:"muted"`
	ChatEnabled bool   `json:"chat_enabled"`
}

type AdminChatList struct {
	Chats []AdminChatRow `json:"chats"`
	Total int            `json:"total"`
	Today int            `json:"today"`
}

const adminChatCols = `t.id, t.branch_id, b.slug, b.name, m.name, t.customer_id, ` + customerName + `, c.phone, t.last_kind, t.last_text,
	t.last_sender, t.last_at, t.msg_count, ao.id, ao.public_no, ao.acceptance_status, ao.fulfillment_status,
	c.chat_muted_at IS NOT NULL, b.chat_enabled`

const adminChatFrom = ` FROM chat_threads t JOIN customers c ON c.id=t.customer_id JOIN branches b ON b.id=t.branch_id
	JOIN merchants m ON m.id=b.merchant_id ` + activeOrder

func scanAdminChat(row interface{ Scan(...any) error }) (AdminChatRow, error) {
	var r AdminChatRow
	var oid *int64
	var no, acc, ful *string
	err := row.Scan(&r.ID, &r.BranchID, &r.Slug, &r.StoreName, &r.Merchant, &r.CustomerID, &r.Customer, &r.Phone, &r.LastKind, &r.LastText,
		&r.LastSender, &r.LastAt, &r.Count, &oid, &no, &acc, &ful, &r.Muted, &r.ChatEnabled)
	r.PhoneTail, r.Order = last4(r.Phone), orderTag(oid, no, acc, ful)
	return r, err
}

// AdminChats lists every conversation on the platform, latest first. q looks
// in store names, customer phones, order numbers and the messages themselves.
func (a *App) AdminChats(ctx context.Context, f ChatFilter) (*AdminChatList, error) {
	where, args := `true`, []any{}
	if f.BranchID != 0 {
		args = append(args, f.BranchID)
		where += fmt.Sprintf(` AND t.branch_id=$%d`, len(args))
	}
	if f.Today {
		where += ` AND t.last_at > now() - interval '1 day'`
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		args = append(args, like(q))
		n := len(args)
		where += fmt.Sprintf(` AND (b.name ILIKE $%d OR c.phone ILIKE $%d OR %s ILIKE $%d
			OR EXISTS (SELECT 1 FROM orders o WHERE o.branch_id=t.branch_id AND o.customer_id=t.customer_id AND o.public_no ILIKE $%d)
			OR EXISTS (SELECT 1 FROM chat_messages cm WHERE cm.thread_id=t.id AND cm.body ILIKE $%d))`, n, n, customerName, n, n, n)
	}
	out := &AdminChatList{Chats: []AdminChatRow{}}
	if err := a.DB.QueryRow(ctx, `SELECT count(*)`+adminChatFrom+` WHERE `+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	if err := a.DB.QueryRow(ctx, `SELECT count(*) FROM chat_threads WHERE last_at > now() - interval '1 day'`).Scan(&out.Today); err != nil {
		return nil, err
	}
	limit, offset := f.limitOffset()
	args = append(args, limit, offset)
	rows, err := a.DB.Query(ctx, `SELECT `+adminChatCols+adminChatFrom+` WHERE `+where+
		fmt.Sprintf(` ORDER BY t.last_at DESC, t.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanAdminChat(rows)
		if err != nil {
			return nil, err
		}
		out.Chats = append(out.Chats, r)
	}
	return out, rows.Err()
}

// ChatLine is one message of this system's copy.
type ChatLine struct {
	ID      int64     `json:"id"`
	Sender  string    `json:"sender"`
	Staff   string    `json:"staff,omitempty"`
	Kind    string    `json:"kind"`
	Body    string    `json:"body"`     // the text; the address of a picture
	OrderNo string    `json:"order_no"` // of an order card
	SentAt  time.Time `json:"sent_at"`
}

type AdminChat struct {
	AdminChatRow
	StartedAt time.Time   `json:"started_at"`
	Messages  []ChatLine  `json:"messages"`
	Orders    []ChatOrder `json:"orders"`
}

// adminChatPage is how many messages the platform's page loads at a time.
const adminChatPage = 200

// AdminChat opens one conversation for the platform, read-only, and writes
// down who looked. before pages back through older messages.
func (a *App) AdminChat(ctx context.Context, actor AdminActor, id, before int64) (*AdminChat, error) {
	row, err := scanAdminChat(a.DB.QueryRow(ctx, `SELECT `+adminChatCols+adminChatFrom+` WHERE t.id=$1`, id))
	if isNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out := &AdminChat{AdminChatRow: row, Messages: []ChatLine{}}
	if err := a.DB.QueryRow(ctx, `SELECT created_at FROM chat_threads WHERE id=$1`, id).Scan(&out.StartedAt); err != nil {
		return nil, err
	}
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := a.DB.Query(ctx, `SELECT m.id, m.sender, COALESCE(NULLIF(u.display_name,''), u.username, ''), m.kind, m.body,
			COALESCE(o.public_no, ''), m.sent_at
		FROM chat_messages m LEFT JOIN users u ON u.id=m.staff_user_id LEFT JOIN orders o ON o.id=m.order_id
		WHERE m.thread_id=$1 AND m.id < $2 ORDER BY m.id DESC LIMIT $3`, id, before, adminChatPage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l ChatLine
		if err := rows.Scan(&l.ID, &l.Sender, &l.Staff, &l.Kind, &l.Body, &l.OrderNo, &l.SentAt); err != nil {
			return nil, err
		}
		if l.Kind == "image" {
			l.Body = chatImagePath + l.Body
		}
		out.Messages = append(out.Messages, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out.Messages)-1; i < j; i, j = i+1, j-1 { // oldest first
		out.Messages[i], out.Messages[j] = out.Messages[j], out.Messages[i]
	}
	if out.Orders, err = chatOrders(ctx, a.DB, row.BranchID, row.CustomerID, 5); err != nil {
		return nil, err
	}
	if before == 1<<62 { // opening it, not paging further back
		if err := adminAudit(ctx, a.DB, actor, "chat.view", "chat", id, map[string]any{"branch_id": row.BranchID, "customer_id": row.CustomerID}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AdminMuteCustomer stops a customer from writing in any chat, or lets them again.
func (a *App) AdminMuteCustomer(ctx context.Context, actor AdminActor, customerID int64, muted bool) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE customers SET chat_muted_at = CASE WHEN $2 THEN COALESCE(chat_muted_at, now()) END WHERE id=$1`, customerID, muted)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return adminAudit(ctx, tx, actor, map[bool]string{true: "chat.mute", false: "chat.unmute"}[muted], "customer", customerID, nil)
	})
}

// AdminSetStoreChat closes or reopens a store's chat.
func (a *App) AdminSetStoreChat(ctx context.Context, actor AdminActor, branchID int64, enabled bool) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE branches SET chat_enabled=$2 WHERE id=$1`, branchID, enabled)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return adminAudit(ctx, tx, actor, map[bool]string{true: "chat.store_on", false: "chat.store_off"}[enabled], "branch", branchID, nil)
	})
}
