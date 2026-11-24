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
