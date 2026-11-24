package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// Platform support (internal/app/support.go): customers and stores write to
// the platform, whose administrators answer from /admin/support.

// ---- customer ----

func (s *Server) customerSupport(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		v, err := s.App.SupportOf(c, app.CustomerParty(cu.ID), true)
		reply(c, v, err)
	}
}

func (s *Server) customerSupportRead(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		reply(c, gin.H{"ok": true}, s.App.ReadSupport(c, app.CustomerParty(cu.ID), false))
	}
}

func (s *Server) customerSupportImage(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	if body, ok := pictureBody(c); ok {
		token, err := s.App.AddSupportImage(c, app.CustomerParty(cu.ID), false, body)
		reply(c, gin.H{"token": token}, err)
	}
}

// ---- store ----

func (s *Server) storeSupport(c *gin.Context) {
	if bid, ok := branchScope(c, pSupport); ok {
		v, err := s.App.SupportOf(c, app.StoreParty(bid), false)
		reply(c, v, err)
	}
}

func (s *Server) storeSupportRead(c *gin.Context) {
	if bid, ok := branchScope(c, pSupport); ok {
		reply(c, gin.H{"ok": true}, s.App.ReadSupport(c, app.StoreParty(bid), false))
	}
}

func (s *Server) storeSupportImage(c *gin.Context) {
	bid, ok := branchScope(c, pSupport)
	if !ok {
		return
	}
	if body, ok := pictureBody(c); ok {
		token, err := s.App.AddSupportImage(c, app.StoreParty(bid), false, body)
		reply(c, gin.H{"token": token}, err)
	}
}

// ---- platform ----

func (s *Server) adminSupport(c *gin.Context) {
	l, err := s.App.AdminSupport(c, app.SupportFilter{Q: c.Query("q"), Who: c.Query("who"), Unread: c.Query("unread") == "1",
		Open: c.Query("open") == "1", Page: queryPage(c)})
	reply(c, gin.H{"list": l, "enabled": s.App.ChatOn()}, err)
}

func (s *Server) adminSupportUnread(c *gin.Context) {
	n, err := s.App.AdminSupportUnread(c)
	reply(c, gin.H{"unread": n}, err)
}

func (s *Server) adminSupportLogin(c *gin.Context) {
	l, err := s.App.PlatformChatLogin(c)
	reply(c, l, err)
}

func (s *Server) adminSupportChat(c *gin.Context) {
	v, err := s.App.AdminSupportChat(c, c.Param("party"))
	reply(c, v, err)
}

func (s *Server) adminSupportRead(c *gin.Context) {
	reply(c, gin.H{"ok": true}, s.App.ReadSupport(c, c.Param("party"), true))
}

func (s *Server) adminSupportImage(c *gin.Context) {
	if body, ok := pictureBody(c); ok {
		token, err := s.App.AddSupportImage(c, c.Param("party"), true, body)
		reply(c, gin.H{"token": token}, err)
	}
}

func (s *Server) adminSupportResolve(c *gin.Context) {
	var in struct {
		Resolved bool `json:"resolved"`
	}
	if bind(c, &in) {
		reply(c, gin.H{"ok": true}, s.App.AdminResolveSupport(c, adminActor(c), c.Param("party"), in.Resolved))
	}
}
