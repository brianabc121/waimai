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
	auth.GET("/merchant/branches/:bid/members/:no/orders", s.memberOrders)
	auth.GET("/merchant/branches/:bid/member-list", s.listMembers)
	auth.DELETE("/merchant/branches/:bid/members/:no/addresses/:aid", s.deleteMemberAddress)
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
		m, err := s.App.MemberByNo(c, bid, no)
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

// memberFilter reads the member list's query string.
func memberFilter(c *gin.Context) app.MemberFilter {
	return app.MemberFilter{Q: c.Query("q"), Status: c.Query("status"), Source: c.Query("source"),
		Sort: c.Query("sort"), Page: queryPage(c)}
}

func (s *Server) listMembers(c *gin.Context) {
	if bid, ok := branchScope(c, deskRoles); ok {
		f := memberFilter(c)
		f.BranchID = bid
		l, err := s.App.ListMembers(c, f)
		reply(c, l, err)
	}
}

func (s *Server) memberOrders(c *gin.Context) {
	bid, ok := branchScope(c, deskRoles)
	if !ok {
		return
	}
	if no, ok := memberNo(c); ok {
		id, err := s.App.MemberIDByNo(c, bid, no)
		if err != nil {
			fail(c, err)
			return
		}
		l, err := s.App.MemberOrderHistory(c, id, queryPage(c))
		reply(c, l, err)
	}
}

// The platform's member page: every store's members.
func (s *Server) adminListMembers(c *gin.Context) {
	f := memberFilter(c)
	f.BranchID, f.MerchantID = queryInt(c, "branch_id"), queryInt(c, "merchant_id")
	l, err := s.App.ListMembers(c, f)
	reply(c, l, err)
}

func (s *Server) adminGetMember(c *gin.Context) {
	if id, ok := idParam(c, "id"); ok {
		m, err := s.App.MemberDetail(c, id)
		reply(c, m, err)
	}
}

func (s *Server) adminMemberOrders(c *gin.Context) {
	if id, ok := idParam(c, "id"); ok {
		l, err := s.App.MemberOrderHistory(c, id, queryPage(c))
		reply(c, l, err)
	}
}

func (s *Server) adminSaveMember(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in app.MemberInput
	if !ok || !bind(c, &in) {
		return
	}
	m, err := s.App.AdminSaveMember(c, adminActor(c), id, in)
	reply(c, m, err)
}

func (s *Server) deleteMemberAddress(c *gin.Context) {
	bid, ok := branchScope(c, deskRoles)
	if !ok {
		return
	}
	no, ok := memberNo(c)
	if !ok {
		return
	}
	aid, ok := idParam(c, "aid")
	if !ok {
		return
	}
	id, err := s.App.MemberIDByNo(c, bid, no)
	if err == nil {
		err = s.App.DeleteMemberAddress(c, id, aid)
	}
	reply(c, gin.H{"ok": true}, err)
}

func (s *Server) adminDeleteMemberAddress(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if aid, ok := idParam(c, "aid"); ok {
		reply(c, gin.H{"ok": true}, s.App.DeleteMemberAddress(c, id, aid))
	}
}
