package httpapi

import (
	"net/http"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/gin-gonic/gin"
)

// A store's brands (docs/MULTI_BRAND_DESIGN.md §14).
func (s *Server) brandRoutes(auth *gin.RouterGroup) {
	auth.GET("/merchant/branches/:bid/brands", s.listBrands)
	auth.POST("/merchant/branches/:bid/brands", s.createBrand)
	auth.PUT("/merchant/branches/:bid/brands/:id", s.updateBrand)
	auth.GET("/merchant/branches/:bid/brand-code", s.suggestBrandCode)
	auth.PUT("/merchant/branches/:bid/staff/:id/brands", s.setStaffBrands)
}

var (
	pBrands      = need(app.PermBrandManage)
	errNotBrands = app.Err(http.StatusForbidden, "BRAND_FORBIDDEN", "you do not work on this brand")
)

// brandScope checks the route's permissions in the store and that the
// person works on the brand; brand 0 is the store's main brand.
func (s *Server) brandScope(c *gin.Context, perms []string, brand int64) (bid, brandID int64, ok bool) {
	if bid, ok = branchScope(c, perms); !ok {
		return
	}
	if brand == 0 {
		var err error
		if brand, err = s.App.MainBrandID(c, bid); err != nil {
			fail(c, err)
			return 0, 0, false
		}
	}
	if !s.mayUseBrand(c, bid, brand) {
		return 0, 0, false
	}
	return bid, brand, true
}

// mayUseBrand answers 403 when the person does not work on the brand.
func (s *Server) mayUseBrand(c *gin.Context, bid, brand int64) bool {
	u := currentUser(c)
	if u.AllBrands(bid) {
		return true
	}
	if brand == 0 {
		var err error
		if brand, err = s.App.MainBrandID(c, bid); err != nil {
			fail(c, err)
			return false
		}
	}
	if !u.MayUseBrand(bid, brand) {
		fail(c, errNotBrands)
		return false
	}
	return true
}

func (s *Server) listBrands(c *gin.Context) {
	if bid, ok := branchScope(c, pAny); ok {
		bs, err := s.App.ListBrands(c, bid)
		reply(c, gin.H{"brands": bs}, err)
	}
}

func (s *Server) createBrand(c *gin.Context) {
	bid, ok := branchScope(c, pBrands)
	var in app.BrandInput
	if !ok || !bind(c, &in) {
		return
	}
	b, err := s.App.CreateBrand(c, bid, staffActor(c), in)
	reply(c, b, err)
}

func (s *Server) updateBrand(c *gin.Context) {
	bid, id, ok := staffScope(c, pBrands)
	var in app.BrandInput
	if !ok || !bind(c, &in) {
		return
	}
	b, err := s.App.UpdateBrand(c, bid, id, staffActor(c), in)
	reply(c, b, err)
}

func (s *Server) suggestBrandCode(c *gin.Context) {
	if _, ok := branchScope(c, pBrands); ok {
		code, err := s.App.SuggestBrandCode(c, c.Query("name_en"))
		reply(c, gin.H{"code": code}, err)
	}
}

// setStaffBrands limits the brands a member of staff works on; an empty
// brand_ids (or null) means every brand.
func (s *Server) setStaffBrands(c *gin.Context) {
	bid, id, ok := staffScope(c, pStaffManage)
	var in struct {
		BrandIDs []int64 `json:"brand_ids"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	if len(in.BrandIDs) == 0 {
		in.BrandIDs = nil
	}
	reply(c, gin.H{"ok": true}, s.App.SetStaffBrands(c, bid, id, currentUser(c), in.BrandIDs))
}
