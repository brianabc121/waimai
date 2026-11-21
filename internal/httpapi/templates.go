package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

func (s *Server) listTemplates(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	list, err := s.App.ListTemplates(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"channels": list})
}

func (s *Server) saveTemplateDraft(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in app.Template
	if !bind(c, &in) {
		return
	}
	if err := s.App.SaveTemplateDraft(c, bid, c.Param("channel"), in); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// templateAction publishes or discards one channel's draft.
func (s *Server) templateAction(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var err error
	switch c.Param("action") {
	case "publish":
		err = s.App.PublishTemplate(c, bid, c.Param("channel"), staffActor(c))
	case "discard":
		err = s.App.DiscardTemplateDraft(c, bid, c.Param("channel"))
	default:
		err = app.ErrNotFound
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
