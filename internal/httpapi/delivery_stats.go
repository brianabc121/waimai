package httpapi

import (
	"github.com/brianabc121/waimai/internal/app"
	"github.com/gin-gonic/gin"
)

// Customer addresses and where deliveries go
// (docs/DELIVERY_ADDRESS_STATS_DESIGN.md): the store's own page and the
// platform's page over every store.
func (s *Server) deliveryStatsRoutes(auth *gin.RouterGroup) {
	auth.GET("/merchant/branches/:bid/customer-addresses", s.customerAddresses)
	auth.GET("/merchant/branches/:bid/customer-addresses/orders", s.customerAddressOrders)
	auth.GET("/merchant/branches/:bid/delivery-stats/top", s.deliveryTop)
	auth.GET("/merchant/branches/:bid/delivery-stats/top-detail", s.deliveryTopDetail)
	auth.GET("/merchant/branches/:bid/delivery-stats/points", s.deliveryPoints)
}

func (s *Server) adminDeliveryStatsRoutes(admin *gin.RouterGroup) {
	admin.GET("/delivery-addresses", s.customerAddresses)
	admin.GET("/delivery-addresses/orders", s.customerAddressOrders)
	admin.GET("/delivery-addresses/regions", s.deliveryRegions)
	admin.GET("/delivery-addresses/brands", s.deliveryBrands)
	admin.GET("/delivery-stats/top", s.deliveryTop)
	admin.GET("/delivery-stats/top-detail", s.deliveryTopDetail)
	admin.GET("/delivery-stats/points", s.deliveryPoints)
}

// deliveryScope is the store's own for its staff; the platform picks.
// Both read one brand (?brand=, docs/MULTI_BRAND_DESIGN.md §12) or every
// brand together.
func (s *Server) deliveryScope(c *gin.Context) (app.DeliveryScope, bool) {
	if c.Param("bid") == "" {
		sc := app.DeliveryScope{BranchID: queryInt(c, "branch_id"), MerchantID: queryInt(c, "merchant_id"),
			CountryCode: c.Query("country"), City: c.Query("city")}
		if br := queryInt(c, "brand"); br != 0 {
			sc.Brands = []int64{br}
		}
		return sc, true
	}
	bid, ok := branchScope(c, pMemberView)
	if !ok {
		return app.DeliveryScope{}, false
	}
	brands, ok := s.brandView(c, bid)
	return app.DeliveryScope{BranchID: bid, Brands: brands}, ok
}

func deliveryRange(c *gin.Context) app.DeliveryRange {
	return app.DeliveryRange{From: c.Query("from"), To: c.Query("to")}
}

func (s *Server) customerAddresses(c *gin.Context) {
	if sc, ok := s.deliveryScope(c); ok {
		l, err := s.App.CustomerAddresses(c, app.CustomerAddressFilter{DeliveryScope: sc, Q: c.Query("q"),
			Source: c.Query("source"), Sort: c.Query("sort"), Page: queryPage(c)})
		reply(c, l, err)
	}
}

func (s *Server) customerAddressOrders(c *gin.Context) {
	sc, ok := s.deliveryScope(c)
	if !ok {
		return
	}
	if sc.BranchID == 0 {
		fail(c, app.ErrNotFound)
		return
	}
	l, err := s.App.AddressOrders(c, sc.BranchID, sc.Brands, c.Query("cust"), c.Query("key"), c.Query("unit"), queryPage(c))
	reply(c, l, err)
}

func (s *Server) deliveryTop(c *gin.Context) {
	if sc, ok := s.deliveryScope(c); ok {
		l, err := s.App.TopAddresses(c, sc, deliveryRange(c))
		reply(c, l, err)
	}
}

func (s *Server) deliveryTopDetail(c *gin.Context) {
	if sc, ok := s.deliveryScope(c); ok {
		l, err := s.App.TopDetail(c, sc, deliveryRange(c), c.Query("key"))
		reply(c, gin.H{"items": l}, err)
	}
}

func (s *Server) deliveryPoints(c *gin.Context) {
	if sc, ok := s.deliveryScope(c); ok {
		m, err := s.App.DeliveryPoints(c, sc, deliveryRange(c))
		reply(c, m, err)
	}
}

func (s *Server) deliveryRegions(c *gin.Context) {
	l, err := s.App.StatsRegions(c)
	reply(c, gin.H{"regions": l}, err)
}

// deliveryBrands is one store's brands, for the platform's brand filter.
func (s *Server) deliveryBrands(c *gin.Context) {
	bid := queryInt(c, "branch_id")
	if bid == 0 {
		reply(c, gin.H{"brands": []any{}}, nil)
		return
	}
	bs, err := s.App.ListBrands(c, bid)
	reply(c, gin.H{"brands": bs}, err)
}
