package httpapi

import (
	"net/http"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/gin-gonic/gin"
)

// A store's brands (docs/MULTI_BRAND_DESIGN.md §14).
func (s *Server) brandRoutes(auth *gin.RouterGroup) {
	auth.GET("/merchant/branches/:bid/brands", s.listBrands)
	auth.POST("/merchant/branches/:bid/brands", s.menuChanged, s.createBrand)
	auth.PUT("/merchant/branches/:bid/brands/:id", s.menuChanged, s.updateBrand)
	auth.GET("/merchant/branches/:bid/brand-code", s.suggestBrandCode)
	auth.PUT("/merchant/branches/:bid/staff/:id/brands", s.setStaffBrands)
	auth.GET("/merchant/branches/:bid/brands/:id/menu", s.brandMenu)
	auth.PUT("/merchant/branches/:bid/brands/:id/menu", s.menuChanged, s.saveBrandMenu)
}

// A brand's own menu (docs/MULTI_BRAND_DESIGN.md §5).
func (s *Server) brandMenu(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if bid, brand, ok := s.brandScope(c, pMenuEdit, id); ok {
		m, err := s.App.GetBrandMenu(c, bid, brand)
		reply(c, m, err)
	}
}

func (s *Server) saveBrandMenu(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in app.BrandMenuInput
	if !ok || !bind(c, &in) {
		return
	}
	if bid, brand, ok := s.brandScope(c, pMenuEdit, id); ok {
		m, err := s.App.SaveBrandMenu(c, bid, brand, staffActor(c), in)
		reply(c, m, err)
	}
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

// brandView is the brands a page that is read together or brand by brand
// is read for (docs/MULTI_BRAND_DESIGN.md §12): ?brand=N is one brand, and
// none is every brand the person works on, which is nil (the whole store,
// cash drawer and all) when they work on every brand.
func (s *Server) brandView(c *gin.Context, bid int64) ([]int64, bool) {
	if brand := queryInt(c, "brand"); brand != 0 {
		if !s.mayUseBrand(c, bid, brand) {
			return nil, false
		}
		if _, err := s.App.BrandOf(c, bid, brand); err != nil {
			fail(c, err)
			return nil, false
		}
		return []int64{brand}, true
	}
	u := currentUser(c)
	if u.AllBrands(bid) {
		return nil, true
	}
	if ids := u.BrandsOf(bid); len(ids) > 0 {
		return ids, true
	}
	return []int64{}, true
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

// mayUseBrands answers 403 unless the person works on every one of the
// brands; none is the store's main brand. A promotion or a coupon batch is
// changed only by someone who works on all the brands it is for
// (docs/MULTI_BRAND_DESIGN.md §13, docs/CODE_REVIEW_FIX_PLAN.md M09).
func (s *Server) mayUseBrands(c *gin.Context, bid int64, brands []int64) bool {
	if currentUser(c).AllBrands(bid) {
		return true
	}
	if len(brands) == 0 {
		brands = []int64{0}
	}
	for _, b := range brands {
		if !s.mayUseBrand(c, bid, b) {
			return false
		}
	}
	return true
}

// seesBrands says, without answering, whether any of the brands is one the
// person works on; none is the main brand.
func (s *Server) seesBrands(c *gin.Context, bid int64, brands []int64) bool {
	u := currentUser(c)
	if u.AllBrands(bid) {
		return true
	}
	if len(brands) == 0 {
		main, err := s.App.MainBrandID(c, bid)
		return err == nil && u.MayUseBrand(bid, main)
	}
	for _, b := range brands {
		if u.MayUseBrand(bid, b) {
			return true
		}
	}
	return false
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
	// Only the owner decides who works on which brand: a manager limited to
	// one brand could otherwise lift the limit, their own included
	// (docs/CODE_REVIEW_FIX_PLAN.md H03).
	bid, id, ok := staffScope(c, pOwner)
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
