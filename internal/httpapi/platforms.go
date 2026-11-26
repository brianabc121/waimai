package httpapi

// Delivery platforms (docs/ORDER_TYPES_DESIGN.md §6).

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

func (s *Server) platformRoutes(auth, admin *gin.RouterGroup) {
	admin.GET("/platforms", s.adminPlatforms)
	admin.POST("/platforms", s.savePlatform)
	admin.PUT("/platforms/:id", s.savePlatform)
	auth.GET("/merchant/branches/:bid/platforms", s.branchPlatforms)
	auth.PUT("/merchant/branches/:bid/platforms", s.saveBranchPlatforms)
}

func (s *Server) adminPlatforms(c *gin.Context) {
	list, err := s.App.AdminPlatforms(c)
	reply(c, gin.H{"platforms": list}, err)
}

func (s *Server) savePlatform(c *gin.Context) {
	var in app.PlatformInput
	if !bind(c, &in) {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	id, err := s.App.SavePlatform(c, adminActor(c), id, in)
	reply(c, gin.H{"id": id}, err)
}

func (s *Server) branchPlatforms(c *gin.Context) {
	if bid, ok := branchScope(c, pSettings); ok {
		list, err := s.App.BranchPlatforms(c, s.App.DB, bid, false)
		reply(c, gin.H{"platforms": list}, err)
	}
}

func (s *Server) saveBranchPlatforms(c *gin.Context) {
	bid, ok := branchScope(c, pSettings)
	if !ok {
		return
	}
	var in struct {
		Platforms []app.BranchPlatformInput `json:"platforms"`
	}
	if !bind(c, &in) {
		return
	}
	list, err := s.App.SaveBranchPlatforms(c, bid, in.Platforms)
	reply(c, gin.H{"platforms": list}, err)
}
