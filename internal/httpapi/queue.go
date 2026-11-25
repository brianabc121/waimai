package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// The queue display (docs/QUEUE_DISPLAY_DESIGN.md): the staff's queue page,
// the guest's "take a number" pages and the paired screens.

const screenCookie = "qs" // paired queue screen: long-lived, read-only

var (
	pQueue       = need(app.PermQueueUse)
	pQueueJump   = need(app.PermQueueJump)
	pQueueManage = need(app.PermQueueManage)
)

func (s *Server) queueRoutes(api, auth *gin.RouterGroup) {
	// guests
	api.GET("/queue/:slug", s.publicQueue)
	api.POST("/queue/:slug/tickets", s.limit(s.publicWrites), s.takeTicketGuest)
	api.GET("/queue/tickets/:token", s.guestTicket)
	api.POST("/queue/tickets/:token/cancel", s.limit(s.publicWrites), s.cancelTicketGuest)
	api.GET("/queue/tickets/:token/events", s.guestTicketEvents)

	// screens
	api.POST("/queue-screens/pair", s.limit(s.loginLimit), s.pairScreen)
	api.GET("/queue-screens/current", s.currentScreen)
	api.GET("/queue-screens/current/events", s.screenEvents)

	// staff
	auth.GET("/merchant/branches/:bid/queue", s.withBranch(pQueue, s.queueDesk))
	auth.POST("/merchant/branches/:bid/queue/tickets", s.withBranch(pQueue, s.takeTicketDesk))
	auth.POST("/merchant/branches/:bid/queue/tickets/:id/:action", s.withBranch(pQueue, s.ticketAction))
	auth.GET("/merchant/branches/:bid/queue/log", s.withBranch(pQueue, s.queueLog))
	auth.POST("/merchant/branches/:bid/queue/pause", s.withBranch(pQueue, s.pauseQueue))
	auth.GET("/merchant/branches/:bid/queue/settings", s.withBranch(pQueue, s.queueSettings))
	auth.PUT("/merchant/branches/:bid/queue/settings", s.withBranch(pQueueManage, s.saveQueueSettings))
	auth.PUT("/merchant/branches/:bid/queue/groups", s.withBranch(pQueueManage, s.saveQueueGroups))
	auth.GET("/merchant/branches/:bid/queue/screens", s.withBranch(pQueueManage, s.queueScreens))
	auth.POST("/merchant/branches/:bid/queue/screens", s.withBranch(pQueueManage, s.createScreen))
	auth.PUT("/merchant/branches/:bid/queue/screens/:id", s.withBranch(pQueueManage, s.updateScreen))
	auth.POST("/merchant/branches/:bid/queue/screens/:id/revoke", s.withBranch(pQueueManage, s.revokeScreen))
	auth.GET("/merchant/branches/:bid/queue/pickup", s.withBranch(pQueue, s.pickupBoard))
	auth.POST("/merchant/branches/:bid/queue/pickup/:id/recall", s.withBranch(pQueue, s.recallPickup))
	auth.POST("/merchant/branches/:bid/queue/pickup/:id/done", s.withBranch(pQueue, s.handOverPickup))
}

// ---- guests ----

func (s *Server) publicQueue(c *gin.Context) {
	q, err := s.App.PublicQueue(c, c.Param("slug"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, q)
}

func (s *Server) takeTicketGuest(c *gin.Context) {
	var in app.TakeTicketInput
	if !bind(c, &in) {
		return
	}
	bid, err := s.App.BranchIDBySlug(c, c.Param("slug"))
	if err != nil {
		fail(c, err)
		return
	}
	in.Source, in.Priority, in.Reason = "qr", false, ""
	if in.Lang == "" {
		in.Lang, _ = c.Cookie("lang")
	}
	t, err := s.App.TakeTicket(c, bid, in, app.Actor{Type: "customer"})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"token": t.Token, "number": t.Number})
}

func (s *Server) guestTicket(c *gin.Context) {
	t, err := s.App.TicketByToken(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, t)
}

func (s *Server) cancelTicketGuest(c *gin.Context) {
	if err := s.App.CancelByGuest(c, c.Param("token")); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// guestTicketEvents follows the store: any step in the line can move the
// guest's place. Hints carry no data.
func (s *Server) guestTicketEvents(c *gin.Context) {
	t, err := s.App.TicketByToken(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	s.Hub.serve(c, &subscription{branch: t.BranchID})
}

// ---- screens ----

func (s *Server) pairScreen(c *gin.Context) {
	var in struct {
		Code string `json:"code"`
	}
	if !bind(c, &in) {
		return
	}
	token, err := s.App.PairQueueScreen(c, in.Code)
	if err != nil {
		fail(c, err)
		return
	}
	s.setCookie(c, screenCookie, token, int(app.DeviceTTL.Seconds()))
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) screenAccess(c *gin.Context) (*app.ScreenAccess, bool) {
	token, _ := c.Cookie(screenCookie)
	acc, err := s.App.ScreenByToken(c, token)
	if err != nil {
		fail(c, err)
		return nil, false
	}
	return acc, true
}

func (s *Server) currentScreen(c *gin.Context) {
	acc, ok := s.screenAccess(c)
	if !ok {
		return
	}
	v, err := s.App.ScreenView(c, acc)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) screenEvents(c *gin.Context) {
	acc, ok := s.screenAccess(c)
	if !ok {
		return
	}
	s.Hub.serve(c, &subscription{branch: acc.BranchID})
}

// ---- staff ----

func (s *Server) queueDesk(c *gin.Context, bid int64) {
	d, err := s.App.QueueDesk(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, d)
}

func (s *Server) takeTicketDesk(c *gin.Context, bid int64) {
	var in app.TakeTicketInput
	if !bind(c, &in) {
		return
	}
	if in.Source != "staff" {
		in.Source = "paper"
	}
	in.Client = ""
	if in.Priority && !allowed(c, bid, pQueueJump) {
		return
	}
	t, err := s.App.TakeTicket(c, bid, in, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, t)
}

func (s *Server) ticketAction(c *gin.Context, bid int64) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in app.QueueAction
	if c.Request.ContentLength != 0 && !bind(c, &in) {
		return
	}
	actor := staffActor(c)
	var t *app.QueueTicket
	var err error
	switch c.Param("action") {
	case "call":
		t, err = s.App.CallTicket(c, bid, id, in, actor)
	case "seat":
		var bill *app.Bill
		t, bill, err = s.App.SeatTicket(c, bid, id, in, actor)
		if err == nil {
			c.JSON(200, gin.H{"ticket": t, "session_id": bill.SessionID, "table_id": bill.TableID})
			return
		}
	case "skip":
		t, err = s.App.SkipTicket(c, bid, id, actor)
	case "requeue":
		t, err = s.App.RequeueTicket(c, bid, id, actor)
	case "jump":
		if !allowed(c, bid, pQueueJump) {
			return
		}
		t, err = s.App.JumpTicket(c, bid, id, in, actor)
	case "people":
		t, err = s.App.ChangePeople(c, bid, id, in, actor)
	case "cancel":
		t, err = s.App.CancelTicket(c, bid, id, actor)
	default:
		fail(c, app.ErrNotFound)
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ticket": t})
}

func (s *Server) queueLog(c *gin.Context, bid int64) {
	l, err := s.App.QueueLog(c, bid, c.Query("date"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"events": l})
}

func (s *Server) pauseQueue(c *gin.Context, bid int64) {
	var in struct {
		Paused bool `json:"paused"`
	}
	if !bind(c, &in) {
		return
	}
	set, err := s.App.SetQueuePaused(c, bid, in.Paused, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, set)
}

func (s *Server) queueSettings(c *gin.Context, bid int64) {
	set, err := s.App.QueueSettings(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, set)
}

func (s *Server) saveQueueSettings(c *gin.Context, bid int64) {
	var in app.QueueSettings
	if !bind(c, &in) {
		return
	}
	set, err := s.App.SaveQueueSettings(c, bid, in, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, set)
}

func (s *Server) saveQueueGroups(c *gin.Context, bid int64) {
	var in struct {
		Groups []app.QueueGroupInput `json:"groups"`
	}
	if !bind(c, &in) {
		return
	}
	g, err := s.App.SaveQueueGroups(c, bid, in.Groups, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"groups": g})
}

func (s *Server) queueScreens(c *gin.Context, bid int64) {
	l, err := s.App.QueueScreens(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"screens": l})
}

func (s *Server) createScreen(c *gin.Context, bid int64) {
	var in app.QueueScreenInput
	if !bind(c, &in) {
		return
	}
	sc, code, err := s.App.CreateQueueScreen(c, bid, in, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"screen": sc, "code": code, "expires_in_minutes": 10})
}

func (s *Server) updateScreen(c *gin.Context, bid int64) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in app.QueueScreenInput
	if !bind(c, &in) {
		return
	}
	sc, err := s.App.UpdateQueueScreen(c, bid, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"screen": sc})
}

func (s *Server) revokeScreen(c *gin.Context, bid int64) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := s.App.RevokeQueueScreen(c, bid, id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) pickupBoard(c *gin.Context, bid int64) {
	b, err := s.App.TeaBoard(c, bid, true)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, b)
}

func (s *Server) recallPickup(c *gin.Context, bid int64) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := s.App.RecallPickup(c, bid, id, staffActor(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) handOverPickup(c *gin.Context, bid int64) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := s.App.HandOverPickup(c, bid, id, staffActor(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
