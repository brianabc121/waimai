package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

func (s *Server) repurchaseRule(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	r, err := s.App.RepurchaseRule(c, bid)
	reply(c, r, err)
}

func (s *Server) saveRepurchaseRule(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in app.RepurchaseInput
	if !bind(c, &in) {
		return
	}
	if err := s.App.SaveRepurchaseRule(c, bid, in, staffActor(c)); err != nil {
		fail(c, err)
		return
	}
	r, err := s.App.RepurchaseRule(c, bid)
	reply(c, r, err)
}
