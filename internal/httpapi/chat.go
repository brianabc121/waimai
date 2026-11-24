package httpapi

import (
	"crypto/subtle"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// Chat between customers and stores (internal/app/chat.go). The browsers talk
// to OpenIM; these routes hand out its sign-in tokens, serve the lists and the
// unread counts, and answer OpenIM's callbacks.

func (s *Server) chatRoutes(r *gin.Engine, api *gin.RouterGroup) {
	w := s.limit(s.publicWrites)
	api.GET("/customer/unread", s.customerUnread)
	api.GET("/customer/chats", s.customerChats)
	api.POST("/customer/chats/login", w, s.customerChatLogin)
	api.GET("/customer/chats/:slug", s.customerChat)
	api.POST("/customer/chats/:slug/read", w, s.customerChatRead)
	api.POST("/customer/chats/:slug/images", w, s.customerChatImage)
	api.GET("/chat-images/:token", s.chatImage)
	api.POST("/customer/chats/:slug/quiet", w, s.customerChatQuiet)
	api.POST("/customer/chats/:slug/hide", w, s.customerChatHide)
	api.POST("/customer/chats/:slug/report", w, s.customerChatReport)
	api.POST("/customer/devices", w, s.customerDevice)
	api.DELETE("/customer/devices/:id", w, s.customerDeviceRemove)
	api.GET("/customer/support", s.customerSupport)
	api.POST("/customer/support/read", w, s.customerSupportRead)
	api.POST("/customer/support/images", w, s.customerSupportImage)

	// OpenIM calls back here from inside the private network. Its callbacks
	// are not signed, so the address carries a key.
	r.POST("/im-hooks/:key/:command", s.chatHook)
}

func (s *Server) chatAdminRoutes(auth, admin *gin.RouterGroup) {
	auth.GET("/merchant/branches/:bid/chats", s.storeChats)
	auth.POST("/merchant/branches/:bid/chats/login", s.storeChatLogin)
	auth.GET("/merchant/branches/:bid/chats/:cid", s.storeChat)
	auth.POST("/merchant/branches/:bid/chats/:cid/read", s.storeChatRead)
	auth.POST("/merchant/branches/:bid/chats/:cid/images", s.storeChatImage)
	auth.GET("/merchant/branches/:bid/chat-order/:oid", s.chatOrderCustomer)
	auth.GET("/merchant/branches/:bid/chat-setup", s.chatSetup)
	auth.POST("/merchant/branches/:bid/chat-quick-replies", s.addQuickReply)
	auth.DELETE("/merchant/branches/:bid/chat-quick-replies/:id", s.deleteQuickReply)
	auth.PUT("/merchant/branches/:bid/chat-auto-reply", s.setChatAutoReply)

	auth.POST("/merchant/branches/:bid/chats/:cid/block", s.storeChatBlock)
	auth.POST("/merchant/branches/:bid/chats/:cid/report", s.storeChatReport)

	auth.GET("/merchant/branches/:bid/support", s.storeSupport)
	auth.POST("/merchant/branches/:bid/support/read", s.storeSupportRead)
	auth.POST("/merchant/branches/:bid/support/images", s.storeSupportImage)

	admin.GET("/support", s.adminSupport)
	admin.GET("/support/unread", s.adminSupportUnread)
	admin.POST("/support/login", s.adminSupportLogin)
	admin.GET("/support/:party", s.adminSupportChat)
	admin.POST("/support/:party/read", s.adminSupportRead)
	admin.POST("/support/:party/images", s.adminSupportImage)
	admin.POST("/support/:party/resolve", s.adminSupportResolve)

	admin.GET("/chats", s.adminChats)
	admin.POST("/chats/:id/reports/resolve", s.adminChatResolve)
	admin.GET("/chats/:id", s.adminChat)
	admin.POST("/chat-customers/:id/mute", s.adminChatMute)
	admin.POST("/chat-stores/:id", s.adminStoreChat)
}

// ---- customer ----

func (s *Server) customerUnread(c *gin.Context) {
	cu, err := s.customer(c)
	if err != nil { // nobody is signed in: nothing is unread
		c.JSON(200, app.Unread{})
		return
	}
	u, err := s.App.CustomerUnread(c, cu.ID)
	reply(c, u, err)
}

func (s *Server) customerChats(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		chats, err := s.App.CustomerChats(c, cu.ID)
		var support *app.ChatThread
		if err == nil {
			support, err = s.App.SupportThreadOf(c, app.CustomerParty(cu.ID))
		}
		reply(c, gin.H{"chats": chats, "support": support, "enabled": s.App.ChatOn()}, err)
	}
}

func (s *Server) customerChatLogin(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		l, err := s.App.CustomerChatLogin(c, cu.ID)
		reply(c, l, err)
	}
}

func (s *Server) customerChat(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		chat, err := s.App.CustomerChat(c, cu.ID, c.Param("slug"))
		reply(c, chat, err)
	}
}

func (s *Server) customerChatRead(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	chat, err := s.App.CustomerChat(c, cu.ID, c.Param("slug"))
	if err == nil {
		err = s.App.ReadChat(c, "customer", chat.BranchID, cu.ID)
	}
	reply(c, gin.H{"ok": true}, err)
}

// ---- store ----

func (s *Server) storeChats(c *gin.Context) {
	bid, ok := branchScope(c, pChat)
	if !ok {
		return
	}
	chats, unread, err := s.App.StoreChats(c, bid, app.ChatFilter{Q: c.Query("q"), Unread: c.Query("unread") == "1", Active: c.Query("active") == "1"})
	var support int
	if err == nil {
		support, err = s.App.SupportUnread(c, app.StoreParty(bid))
	}
	reply(c, gin.H{"chats": chats, "unread": unread, "support_unread": support, "enabled": s.App.ChatOn()}, err)
}

func (s *Server) storeChatLogin(c *gin.Context) {
	if bid, ok := branchScope(c, pChat); ok {
		l, err := s.App.StoreChatLogin(c, bid)
		reply(c, l, err)
	}
}

func (s *Server) storeChat(c *gin.Context) {
	bid, ok := branchScope(c, pChat)
	if !ok {
		return
	}
	if cid, ok := idParam(c, "cid"); ok {
		chat, err := s.App.StoreChatWith(c, bid, cid)
		reply(c, chat, err)
	}
}

func (s *Server) storeChatRead(c *gin.Context) {
	bid, ok := branchScope(c, pChat)
	if !ok {
		return
	}
	if cid, ok := idParam(c, "cid"); ok {
		reply(c, gin.H{"ok": true}, s.App.ReadChat(c, "store", bid, cid))
	}
}

// ---- reminders, blocks and reports (internal/app/chat_alerts.go) ----

func (s *Server) customerDevice(c *gin.Context) {
	cu, ok := s.me(c)
	var in struct {
		Token string `json:"token"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	id, err := s.App.AddCustomerDevice(c, cu.ID, in.Token)
	if err == nil {
		err = s.App.SetDeviceLang(c, false, id, reqLang(c))
	}
	reply(c, gin.H{"id": id}, err)
}

func (s *Server) customerDeviceRemove(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	if id, ok := idParam(c, "id"); ok {
		reply(c, gin.H{"ok": true}, s.App.RemoveCustomerDevice(c, cu.ID, id))
	}
}

// customerChatOf finds the store a customer's chat route is about.
func (s *Server) customerChatOf(c *gin.Context) (customerID, branchID int64, ok bool) {
	cu, ok := s.me(c)
	if !ok {
		return 0, 0, false
	}
	bid, err := s.App.ChatBranch(c, c.Param("slug"))
	if err != nil {
		fail(c, err)
		return 0, 0, false
	}
	return cu.ID, bid, true
}

func (s *Server) customerChatQuiet(c *gin.Context) {
	var in struct {
		Quiet bool `json:"quiet"`
	}
	if !bind(c, &in) {
		return
	}
	if cid, bid, ok := s.customerChatOf(c); ok {
		reply(c, gin.H{"ok": true}, s.App.SetChatQuiet(c, cid, bid, in.Quiet))
	}
}

func (s *Server) customerChatHide(c *gin.Context) {
	if cid, bid, ok := s.customerChatOf(c); ok {
		reply(c, gin.H{"ok": true}, s.App.HideChat(c, cid, bid))
	}
}

type chatReportIn struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

func (s *Server) customerChatReport(c *gin.Context) {
	var in chatReportIn
	if !bind(c, &in) {
		return
	}
	if cid, bid, ok := s.customerChatOf(c); ok {
		reply(c, gin.H{"ok": true}, s.App.ReportChat(c, "customer", bid, cid, 0, in.Reason, in.Detail))
	}
}

func (s *Server) storeChatBlock(c *gin.Context) {
	bid, ok := branchScope(c, pChat)
	var in struct {
		Blocked bool `json:"blocked"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	if cid, ok := idParam(c, "cid"); ok {
		reply(c, gin.H{"ok": true}, s.App.BlockChatCustomer(c, bid, cid, currentUser(c).ID, in.Blocked))
	}
}

func (s *Server) storeChatReport(c *gin.Context) {
	bid, ok := branchScope(c, pChat)
	var in chatReportIn
	if !ok || !bind(c, &in) {
		return
	}
	if cid, ok := idParam(c, "cid"); ok {
		reply(c, gin.H{"ok": true}, s.App.ReportChat(c, "store", bid, cid, currentUser(c).ID, in.Reason, in.Detail))
	}
}

func (s *Server) adminChatResolve(c *gin.Context) {
	if id, ok := idParam(c, "id"); ok {
		reply(c, gin.H{"ok": true}, s.App.AdminResolveChatReports(c, adminActor(c), id))
	}
}

// ---- platform ----

func (s *Server) adminChats(c *gin.Context) {
	l, err := s.App.AdminChats(c, app.ChatFilter{Q: c.Query("q"), Today: c.Query("today") == "1",
		Reported: c.Query("reported") == "1", BranchID: queryInt(c, "branch_id"), Page: queryPage(c)})
	reply(c, l, err)
}

func (s *Server) adminChat(c *gin.Context) {
	if id, ok := idParam(c, "id"); ok {
		chat, err := s.App.AdminChat(c, adminActor(c), id, queryInt(c, "before"))
		reply(c, chat, err)
	}
}

func (s *Server) adminChatMute(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in struct {
		Muted bool `json:"muted"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.AdminMuteCustomer(c, adminActor(c), id, in.Muted))
}

func (s *Server) adminStoreChat(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.AdminSetStoreChat(c, adminActor(c), id, in.Enabled))
}

// ---- OpenIM's callbacks ----

// hookRefusal is OpenIM's error number for "the callback said no". The reason
// goes in errMsg as this system's own code, which the chat pages translate.
const hookRefusal = 5001

func hookOK(c *gin.Context) {
	c.JSON(200, gin.H{"actionCode": 0, "errCode": 0, "errMsg": "", "errDlt": "", "nextCode": 0})
}

func hookNo(c *gin.Context, code, detail string) {
	c.JSON(200, gin.H{"actionCode": 0, "errCode": hookRefusal, "errMsg": code, "errDlt": detail, "nextCode": 1})
}

func (s *Server) chatHook(c *gin.Context) {
	key := s.Cfg.OpenIMHookKey
	if key == "" || subtle.ConstantTimeCompare([]byte(key), []byte(c.Param("key"))) != 1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	command := c.Param("command")
	switch command {
	case "callbackBeforeSendSingleMsgCommand", "callbackAfterSendSingleMsgCommand":
		var h app.ChatHook
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		if err := c.ShouldBindJSON(&h); err != nil {
			hookNo(c, "VALIDATION", "unreadable callback")
			return
		}
		if command == "callbackAfterSendSingleMsgCommand" {
			if err := s.App.ChatAfterSend(c, h); err != nil {
				log.Printf("chat: a message was not copied: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "not stored"})
				return
			}
			hookOK(c)
			return
		}
		err := s.App.ChatBeforeSend(c, h)
		var ae *app.Error
		switch {
		case err == nil:
			hookOK(c)
		case errors.As(err, &ae):
			hookNo(c, ae.Code, ae.Message)
		default:
			// this system cannot tell whether the message is allowed: it does not go out
			log.Printf("chat: before-send check failed: %v", err)
			hookNo(c, "CHAT_DOWN", "try again in a moment")
		}
	default:
		// Groups, friends and the rest of OpenIM are not part of this
		// system: whatever asks first is refused.
		if strings.HasPrefix(command, "callbackBefore") {
			hookNo(c, "FORBIDDEN", "not supported")
			return
		}
		hookOK(c)
	}
}

// ---- pictures, quick replies, the automatic answer ----

// pictureBody reads an uploaded JPEG (the browser has shrunk it).
func pictureBody(c *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, app.ImageMaxBytes+1))
	if err != nil || len(body) > app.ImageMaxBytes {
		fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the picture is larger than 1 MB"))
		return nil, false
	}
	return body, true
}

func (s *Server) customerChatImage(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	if body, ok := pictureBody(c); ok {
		token, err := s.App.CustomerChatImage(c, cu.ID, c.Param("slug"), body)
		reply(c, gin.H{"token": token}, err)
	}
}

func (s *Server) storeChatImage(c *gin.Context) {
	bid, ok := branchScope(c, pChat)
	if !ok {
		return
	}
	cid, ok := idParam(c, "cid")
	if !ok {
		return
	}
	if body, ok := pictureBody(c); ok {
		token, err := s.App.AddChatImage(c, "store", bid, cid, body)
		reply(c, gin.H{"token": token}, err)
	}
}

func (s *Server) chatImage(c *gin.Context) {
	image, err := s.App.ChatImage(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "private, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "image/jpeg", image)
}

func (s *Server) chatSetup(c *gin.Context) {
	if bid, ok := branchScope(c, pChat); ok {
		v, err := s.App.ChatSetup(c, bid)
		reply(c, v, err)
	}
}

func (s *Server) addQuickReply(c *gin.Context) {
	bid, ok := branchScope(c, pChat)
	if !ok {
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if bind(c, &in) {
		v, err := s.App.AddQuickReply(c, bid, in.Body)
		reply(c, v, err)
	}
}

func (s *Server) deleteQuickReply(c *gin.Context) {
	bid, ok := branchScope(c, pChat)
	if !ok {
		return
	}
	if id, ok := idParam(c, "id"); ok {
		reply(c, gin.H{"ok": true}, s.App.DeleteQuickReply(c, bid, id))
	}
}

func (s *Server) setChatAutoReply(c *gin.Context) {
	bid, ok := branchScope(c, pChatSettings)
	if !ok {
		return
	}
	var in struct {
		On   bool   `json:"on"`
		Text string `json:"text"`
	}
	if bind(c, &in) {
		reply(c, gin.H{"ok": true}, s.App.SetChatAutoReply(c, bid, in.On, in.Text))
	}
}

func (s *Server) chatOrderCustomer(c *gin.Context) {
	bid, ok := branchScope(c, pChat)
	if !ok {
		return
	}
	if oid, ok := idParam(c, "oid"); ok {
		id, err := s.App.ChatCustomerOfOrder(c, bid, oid)
		reply(c, gin.H{"customer_id": id}, err)
	}
}
