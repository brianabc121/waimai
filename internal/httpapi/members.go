package httpapi

import (
	"strconv"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/gin-gonic/gin"
)

// Orders the store places for a customer, and the store's members
// (docs/MANUAL_ORDER_DESIGN.md).
func (s *Server) memberRoutes(auth *gin.RouterGroup) {
	auth.GET("/merchant/branches/:bid/members", s.searchMembers)
	auth.POST("/merchant/branches/:bid/members", s.newMember)
	auth.GET("/merchant/branches/:bid/members/:no", s.getMember)
	auth.PUT("/merchant/branches/:bid/members/:no", s.saveMember)
	auth.GET("/merchant/branches/:bid/address-hints", s.addressHints)
	auth.POST("/merchant/branches/:bid/staff-quotes", s.staffQuote)
	auth.POST("/merchant/branches/:bid/staff-orders", s.staffOrder)
}

func memberNo(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("no"))
	if err != nil || n < 0 {
		fail(c, app.ErrNotFound)
		return 0, false
	}
	return n, true
}

func (s *Server) searchMembers(c *gin.Context) {
	if bid, ok := branchScope(c, deskRoles); ok {
		ms, err := s.App.SearchMembers(c, bid, c.Query("q"))
		reply(c, gin.H{"members": ms}, err)
	}
}

func (s *Server) newMember(c *gin.Context) {
	if bid, ok := branchScope(c, deskRoles); ok {
		m, err := s.App.NewMember(c, bid, currentUser(c).ID)
		reply(c, m, err)
	}
}

func (s *Server) getMember(c *gin.Context) {
	bid, ok := branchScope(c, deskRoles)
	if !ok {
		return
	}
	if no, ok := memberNo(c); ok {
		m, err := s.App.GetMember(c, bid, no)
		reply(c, m, err)
	}
}

func (s *Server) saveMember(c *gin.Context) {
	bid, ok := branchScope(c, deskRoles)
	if !ok {
		return
	}
	no, ok := memberNo(c)
	var in app.MemberInput
	if !ok || !bind(c, &in) {
		return
	}
	in.No = no
	m, err := s.App.SaveMember(c, bid, currentUser(c).ID, in)
	reply(c, m, err)
}

func (s *Server) addressHints(c *gin.Context) {
	if bid, ok := branchScope(c, deskRoles); ok {
		hs, err := s.App.AddressHints(c, bid, c.Query("q"), c.Query("phone"))
		reply(c, gin.H{"addresses": hs}, err)
	}
}

func (s *Server) staffQuote(c *gin.Context) {
	bid, ok := branchScope(c, deskRoles)
	var in app.StaffOrderInput
	if !ok || !bind(c, &in) {
		return
	}
	q, err := s.App.StaffQuote(c, bid, in)
	reply(c, q, err)
}

func (s *Server) staffOrder(c *gin.Context) {
	bid, ok := branchScope(c, deskRoles)
	var in app.StaffOrderInput
	if !ok || !bind(c, &in) {
		return
	}
	out, err := s.App.PlaceStaffOrder(c, bid, staffActor(c), c.GetHeader("Idempotency-Key"), in)
	reply(c, out, err)
}
