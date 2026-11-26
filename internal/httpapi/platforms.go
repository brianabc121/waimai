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
	// platform orders (§7)
	auth.GET("/merchant/branches/:bid/platform-orders/platforms", s.orderPlatforms)
	auth.POST("/merchant/branches/:bid/platform-orders/quote", s.platformQuote)
	auth.POST("/merchant/branches/:bid/platform-orders", s.platformOrder)
	auth.PUT("/merchant/orders/:id/platform-commission", s.platformCommission)
	auth.POST("/merchant/orders/:id/platform-fees", s.addPlatformFee)
	auth.DELETE("/merchant/orders/:id/platform-fees/:fid", s.deletePlatformFee)
	auth.POST("/merchant/orders/:id/platform-deductions", s.platformDeduction)
}

// orderPlatforms are the platforms the store is on, for the page that enters
// their orders: each brand's shops (一品湘 Grab, 好吃党 Grab), those of the
// brands the person works on; platforms are the main brand's.
func (s *Server) orderPlatforms(c *gin.Context) {
	bid, ok := branchScope(c, pPlatform)
	if !ok {
		return
	}
	brands, err := s.App.ListBrands(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	type shops struct {
		Brand     app.BrandRef         `json:"brand"`
		Platforms []app.BranchPlatform `json:"platforms"`
	}
	out := []shops{}
	main := []app.BranchPlatform{}
	u := currentUser(c)
	for _, br := range brands {
		if br.Status != "active" || !br.Delivery || !(u.AllBrands(bid) || u.MayUseBrand(bid, br.ID)) {
			continue
		}
		list, err := s.App.BranchPlatforms(c, s.App.DB, bid, br.ID, true)
		if err != nil {
			fail(c, err)
			return
		}
		if br.IsMain {
			main = list
		}
		if len(list) > 0 {
			out = append(out, shops{Brand: br.Ref(), Platforms: list})
		}
	}
	c.JSON(200, gin.H{"platforms": main, "shops": out})
}

func (s *Server) platformQuote(c *gin.Context) {
	bid, ok := branchScope(c, pPlatform)
	var in app.PlatformOrderInput
	if !ok || !bind(c, &in) {
		return
	}
	if !s.mayUseBrand(c, bid, in.BrandID) {
		return
	}
	q, err := s.App.PlatformQuote(c, bid, in)
	reply(c, q, err)
}

func (s *Server) platformOrder(c *gin.Context) {
	bid, ok := branchScope(c, pPlatform)
	var in app.PlatformOrderInput
	if !ok || !bind(c, &in) {
		return
	}
	if !s.mayUseBrand(c, bid, in.BrandID) {
		return
	}
	o, err := s.App.PlacePlatformOrder(c, bid, staffActor(c), c.GetHeader("Idempotency-Key"), in)
	reply(c, gin.H{"order": o}, err)
}

type platformMoney struct {
	Amount *int64 `json:"amount"`
	Reason string `json:"reason"`
}

func (s *Server) platformCommission(c *gin.Context) {
	id, ok := s.orderScope(c, pPlatform)
	var in platformMoney
	if !ok || !bind(c, &in) {
		return
	}
	o, err := s.App.SetPlatformCommission(c, id, in.Amount, in.Reason, staffActor(c))
	reply(c, o, err)
}

func (s *Server) addPlatformFee(c *gin.Context) {
	id, ok := s.orderScope(c, pPlatform)
	var in platformMoney
	if !ok || !bind(c, &in) {
		return
	}
	if in.Amount == nil {
		fail(c, app.ErrValidation("amount is required"))
		return
	}
	o, err := s.App.AddPlatformFee(c, id, *in.Amount, in.Reason, staffActor(c))
	reply(c, o, err)
}

func (s *Server) deletePlatformFee(c *gin.Context) {
	id, ok := s.orderScope(c, pPlatform)
	if !ok {
		return
	}
	fid, ok := idParam(c, "fid")
	if !ok {
		return
	}
	o, err := s.App.DeletePlatformFee(c, id, fid, staffActor(c))
	reply(c, o, err)
}

func (s *Server) platformDeduction(c *gin.Context) {
	id, ok := s.orderScope(c, pPlatform)
	var in platformMoney
	if !ok || !bind(c, &in) {
		return
	}
	if in.Amount == nil {
		fail(c, app.ErrValidation("amount is required"))
		return
	}
	o, err := s.App.RecordPlatformDeduction(c, id, *in.Amount, in.Reason, staffActor(c))
	reply(c, o, err)
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

// branchPlatforms are a brand's shops on the platforms (?brand=, none = the main brand).
func (s *Server) branchPlatforms(c *gin.Context) {
	brand, _ := strconv.ParseInt(c.Query("brand"), 10, 64)
	if bid, brand, ok := s.brandScope(c, pSettings, brand); ok {
		list, err := s.App.BranchPlatforms(c, s.App.DB, bid, brand, false)
		reply(c, gin.H{"platforms": list}, err)
	}
}

func (s *Server) saveBranchPlatforms(c *gin.Context) {
	var in struct {
		BrandID   int64                     `json:"brand_id"`
		Platforms []app.BranchPlatformInput `json:"platforms"`
	}
	if !bind(c, &in) {
		return
	}
	bid, brand, ok := s.brandScope(c, pSettings, in.BrandID)
	if !ok {
		return
	}
	list, err := s.App.SaveBranchPlatforms(c, bid, brand, in.Platforms)
	reply(c, gin.H{"platforms": list}, err)
}
