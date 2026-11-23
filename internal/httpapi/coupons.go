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

// ---- coupons a store lets customers claim ----

func (s *Server) listStoreBatches(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	list, err := s.App.ListStoreBatches(c, bid)
	reply(c, gin.H{"batches": list}, err)
}

func (s *Server) saveStoreBatch(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.CouponBatchInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SaveStoreBatch(c, bid, id, in, staffActor(c))
	reply(c, gin.H{"id": id}, err)
}

func (s *Server) setStoreBatchStatus(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !bind(c, &in) {
		return
	}
	err := s.App.SetStoreBatchStatus(c, bid, id, in.Status, staffActor(c))
	reply(c, gin.H{"ok": true}, err)
}

// storeCoupons is the claim strip on a store's page; anyone may look.
func (s *Server) storeCoupons(c *gin.Context) {
	var cid int64
	if cu, err := s.customer(c); err == nil {
		cid = cu.ID
	}
	list, err := s.App.StoreCoupons(c, c.Param("slug"), cid)
	reply(c, gin.H{"coupons": list}, err)
}

func (s *Server) claimCoupon(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	cu, ok := s.me(c)
	if !ok {
		return
	}
	cp, err := s.App.ClaimCoupon(c, c.Param("slug"), id, cu.ID)
	reply(c, cp, err)
}

// ---- platform coupons ----

func (s *Server) couponAdminRoutes(admin *gin.RouterGroup) {
	admin.GET("/coupons", s.adminCoupons)
	admin.POST("/coupons", s.adminSaveCoupon)
	admin.PUT("/coupons/:id", s.adminSaveCoupon)
	admin.POST("/coupons/:id/status", s.adminCouponStatus)
	admin.POST("/coupons/:id/issue", s.adminIssueCoupon)
}

func (s *Server) adminCoupons(c *gin.Context) {
	list, err := s.App.ListPlatformBatches(c)
	if err != nil {
		fail(c, err)
		return
	}
	sum, err := s.App.PlatformCouponSummary(c)
	if err != nil {
		fail(c, err)
		return
	}
	countries, err := s.App.CouponCountries(c)
	reply(c, gin.H{"batches": list, "summary": sum, "countries": countries}, err)
}

func (s *Server) adminSaveCoupon(c *gin.Context) {
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.CouponBatchInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SavePlatformBatch(c, adminActor(c), id, in)
	reply(c, gin.H{"id": id}, err)
}

func (s *Server) adminCouponStatus(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.SetPlatformBatchStatus(c, adminActor(c), id, in.Status))
}

func (s *Server) adminIssueCoupon(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in app.IssueInput
	if !bind(c, &in) {
		return
	}
	r, err := s.App.IssuePlatform(c, adminActor(c), id, in)
	reply(c, r, err)
}
