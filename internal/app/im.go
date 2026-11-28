package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// IM is the chat server behind the chat pages: OpenIM in production, a
// stand-in in tests. Messages themselves travel between the browsers and that
// server; this system only makes accounts, hands out sign-in tokens and
// answers the server's callbacks (chat.go).
type IM interface {
	// EnsureUser creates the account when it does not exist yet.
	EnsureUser(ctx context.Context, userID, nickname string) error
	// UserToken signs a browser in as userID.
	UserToken(ctx context.Context, userID string) (string, error)
	// MarkRead marks everything in the conversation between userID and peer
	// as read by userID; the server then tells peer's screens.
	MarkRead(ctx context.Context, userID, peer string) error
	// SendText sends a text for an account; ex travels with the message.
	SendText(ctx context.Context, from, to, text, ex string) error
	// Check reports whether the server answers.
	Check(ctx context.Context) error
	// Logout ends every browser sign-in of userID: tokens handed out before
	// stop working, and a page that may still chat signs in again.
	Logout(ctx context.Context, userID string) error
}

// imPlatformWeb is OpenIM's number for a browser.
const imPlatformWeb = 5

// OpenIM talks to an OpenIM server's REST API with the administrator's token.
type OpenIM struct {
	API     string // e.g. http://openim-server:10002
	Secret  string
	AdminID string
	Client  *http.Client

	mu      sync.Mutex
	token   string
	tokenAt time.Time

	// one MarkRead at a time: see there
	reading sync.Mutex
}

// adminTokenAge is how long an administrator token is reused; OpenIM's own
// last far longer.
const adminTokenAge = 6 * time.Hour

func operationID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// call posts one request. token "" is for the call that gets the token.
func (o *OpenIM) call(ctx context.Context, path, token string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.API+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("operationID", operationID())
	if token != "" {
		req.Header.Set("token", token)
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("openim %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("openim %s: %w", path, err)
	}
	var env struct {
		ErrCode int             `json:"errCode"`
		ErrMsg  string          `json:"errMsg"`
		ErrDlt  string          `json:"errDlt"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("openim %s: status %d, not JSON", path, resp.StatusCode)
	}
	if env.ErrCode != 0 {
		return &imError{Path: path, Code: env.ErrCode, Msg: env.ErrMsg + " " + env.ErrDlt}
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

type imError struct {
	Path string
	Code int
	Msg  string
}

func (e *imError) Error() string { return fmt.Sprintf("openim %s: %d %s", e.Path, e.Code, e.Msg) }

// OpenIM's codes for a token that is no longer accepted.
func (e *imError) tokenRejected() bool { return e.Code >= 1501 && e.Code <= 1507 }

func (o *OpenIM) adminToken(ctx context.Context, fresh bool) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !fresh && o.token != "" && time.Since(o.tokenAt) < adminTokenAge {
		return o.token, nil
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := o.call(ctx, "/auth/get_admin_token", "", map[string]any{"secret": o.Secret, "userID": o.AdminID}, &out); err != nil {
		return "", err
	}
	o.token, o.tokenAt = out.Token, time.Now()
	return o.token, nil
}

// admin calls with the administrator's token and gets a new one once when
// the server no longer accepts it (it was restarted with another secret).
func (o *OpenIM) admin(ctx context.Context, path string, in, out any) error {
	token, err := o.adminToken(ctx, false)
	if err != nil {
		return err
	}
	err = o.call(ctx, path, token, in, out)
	if ie, ok := err.(*imError); ok && ie.tokenRejected() {
		if token, err = o.adminToken(ctx, true); err != nil {
			return err
		}
		err = o.call(ctx, path, token, in, out)
	}
	return err
}

func (o *OpenIM) EnsureUser(ctx context.Context, userID, nickname string) error {
	var found struct {
		UsersInfo []struct {
			UserID   string `json:"userID"`
			Nickname string `json:"nickname"`
		} `json:"usersInfo"`
	}
	if err := o.admin(ctx, "/user/get_users_info", map[string]any{"userIDs": []string{userID}}, &found); err != nil {
		return err
	}
	if len(found.UsersInfo) > 0 {
		if found.UsersInfo[0].Nickname == nickname {
			return nil
		}
		return o.admin(ctx, "/user/update_user_info_ex", map[string]any{"userInfo": map[string]any{"userID": userID, "nickname": nickname}}, nil)
	}
	return o.admin(ctx, "/user/user_register", map[string]any{
		"users": []map[string]string{{"userID": userID, "nickname": nickname, "faceURL": ""}},
	}, nil)
}

func (o *OpenIM) UserToken(ctx context.Context, userID string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	err := o.admin(ctx, "/auth/get_user_token", map[string]any{"platformID": imPlatformWeb, "userID": userID}, &out)
	return out.Token, err
}

// conversationID is OpenIM's id of the one-to-one conversation of two accounts.
func conversationID(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return "si_" + a + "_" + b
}

// MarkRead asks what is unread first and marks only when something is: a
// second mark with nothing new makes OpenIM send a receipt without messages,
// which its web SDK (3.8.3) cannot take. Two pages reading at the same moment
// must therefore not both get past the question, hence the lock.
func (o *OpenIM) MarkRead(ctx context.Context, userID, peer string) error {
	o.reading.Lock()
	defer o.reading.Unlock()
	id := conversationID(userID, peer)
	var out struct {
		Seqs map[string]struct {
			MaxSeq     int64 `json:"maxSeq"`
			HasReadSeq int64 `json:"hasReadSeq"`
		} `json:"seqs"`
	}
	if err := o.admin(ctx, "/msg/get_conversations_has_read_and_max_seq", map[string]any{"userID": userID, "conversationIDs": []string{id}}, &out); err != nil {
		return err
	}
	seq := out.Seqs[id]
	if seq.HasReadSeq >= seq.MaxSeq {
		return nil
	}
	return o.admin(ctx, "/msg/mark_conversation_as_read", map[string]any{
		"userID": userID, "conversationID": id, "hasReadSeq": seq.MaxSeq, "seqs": []int64{},
	}, nil)
}

func (o *OpenIM) SendText(ctx context.Context, from, to, text, ex string) error {
	return o.admin(ctx, "/msg/send_msg", map[string]any{
		"sendID": from, "recvID": to, "senderPlatformID": imPlatformWeb,
		"content": map[string]any{"content": text}, "contentType": 101, "sessionType": 1,
		"isOnlineOnly": false, "notOfflinePush": false, "ex": ex,
	}, nil)
}

// Logout uses OpenIM's force_logout, which kicks the account's tokens for
// the web platform.
func (o *OpenIM) Logout(ctx context.Context, userID string) error {
	return o.admin(ctx, "/auth/force_logout", map[string]any{"platformID": imPlatformWeb, "userID": userID}, nil)
}

func (o *OpenIM) Check(ctx context.Context) error {
	_, err := o.adminToken(ctx, true)
	return err
}
