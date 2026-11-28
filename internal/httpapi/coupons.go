package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

func (s *Server) repurchaseRule(c *gin.Context) {
	if bid, brand, ok := s.brandScope(c, pPromo, queryInt(c, "brand")); ok {
		r, err := s.App.RepurchaseRuleOf(c, bid, brand)
		reply(c, r, err)
	}
}

func (s *Server) saveRepurchaseRule(c *gin.Context) {
	var in app.RepurchaseInput
	if !bind(c, &in) {
		return
	}
	bid, brand, ok := s.brandScope(c, pPromo, in.BrandID)
	if !ok {
		return
	}
	in.BrandID = brand
	if err := s.App.SaveRepurchaseRule(c, bid, in, staffActor(c)); err != nil {
		fail(c, err)
		return
	}
	r, err := s.App.RepurchaseRuleOf(c, bid, brand)
	reply(c, r, err)
}

// ---- coupons a store lets customers claim ----

func (s *Server) listStoreBatches(c *gin.Context) {
	bid, ok := branchScope(c, pPromo)
	if !ok {
		return
	}
	list, err := s.App.ListStoreBatches(c, bid)
	mine := list[:0]
	for _, b := range list {
		var brand []int64
		if b.BrandID != nil {
			brand = []int64{*b.BrandID}
		}
		if s.seesBrands(c, bid, brand) {
			mine = append(mine, b)
		}
	}
	reply(c, gin.H{"batches": mine}, err)
}

func (s *Server) saveStoreBatch(c *gin.Context) {
	bid, ok := branchScope(c, pPromo)
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
	if !s.batchMine(c, bid, id) || !s.mayUseBrand(c, bid, in.BrandID) {
		return
	}
	id, err := s.App.SaveStoreBatch(c, bid, id, in, staffActor(c))
	reply(c, gin.H{"id": id}, err)
}

// batchMine checks that the person works on the brand of an existing batch.
func (s *Server) batchMine(c *gin.Context, bid, id int64) bool {
	if id == 0 || currentUser(c).AllBrands(bid) {
		return true
	}
	brand, err := s.App.StoreBatchBrand(c, bid, id)
	if err != nil {
		fail(c, err)
		return false
	}
	return s.mayUseBrand(c, bid, brand)
}

func (s *Server) setStoreBatchStatus(c *gin.Context) {
	bid, ok := branchScope(c, pPromo)
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
	if !s.batchMine(c, bid, id) {
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
