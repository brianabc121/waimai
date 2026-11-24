package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// Role permissions: only the owner of a store sees and changes them
// (docs/STAFF_ACCESS_DESIGN.md §6.1); the platform can look.
func (s *Server) permRoutes(auth *gin.RouterGroup) {
	auth.GET("/merchant/branches/:bid/role-perms", s.withBranch(pOwner, s.rolePerms))
	auth.PUT("/merchant/branches/:bid/role-perms", s.withBranch(pOwner, s.saveRolePerms))
	auth.POST("/merchant/branches/:bid/role-perms/copy", s.withBranch(pOwner, s.copyRolePerms))

	// Manager approval (§7): each manager sets a code of their own; the order
	// taker asks one of them to approve a single action.
	auth.PUT("/auth/approve-pin", s.limit(s.loginLimit), s.setApprovePin)
	auth.GET("/merchant/branches/:bid/approvers", s.withBranch(pAny, s.approvers))
	auth.POST("/merchant/branches/:bid/approvals", s.limit(s.approveLimit), s.withBranch(pAny, s.approve))
}

func (s *Server) setApprovePin(c *gin.Context) {
	var in struct {
		Password string `json:"password"`
		Pin      string `json:"pin"`
	}
	if !bind(c, &in) {
		return
	}
	if err := s.App.SetApprovePin(c, currentUser(c).ID, in.Password, in.Pin); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) approvers(c *gin.Context, bid int64) {
	list, err := s.App.Approvers(c, bid, c.Query("perm"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"approvers": list})
}

func (s *Server) approve(c *gin.Context, bid int64) {
	var in struct {
		ApproverID int64  `json:"approver_id"`
		Pin        string `json:"pin"`
		Perm       string `json:"perm"`
	}
	if !bind(c, &in) {
		return
	}
	out, err := s.App.Approve(c, currentUser(c), bid, in.ApproverID, in.Perm, in.Pin)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

func (s *Server) rolePerms(c *gin.Context, bid int64) {
	out, err := s.App.RolePerms(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

func (s *Server) saveRolePerms(c *gin.Context, bid int64) {
	var in app.RolePermsInput
	if !bind(c, &in) {
		return
	}
	out, err := s.App.SaveRolePerms(c, currentUser(c).ID, bid, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

func (s *Server) copyRolePerms(c *gin.Context, bid int64) {
	var in struct {
		To []int64 `json:"to"`
	}
	if !bind(c, &in) {
		return
	}
	if err := s.App.CopyRolePerms(c, currentUser(c), bid, in.To); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
