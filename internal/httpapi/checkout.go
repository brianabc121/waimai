package httpapi

import (
	"github.com/brianabc121/waimai/internal/app"
	"github.com/gin-gonic/gin"
)

// Payment methods, discounts at checkout (docs/CHECKOUT_DISCOUNT_DESIGN.md §5, §6).

func (s *Server) payChannelRoutes(admin *gin.RouterGroup) {
	admin.GET("/pay-countries", s.adminPayCountries)
	admin.GET("/pay-channels", s.adminPayChannels)
	admin.POST("/pay-channels", s.adminSavePayChannel)
	admin.PUT("/pay-channels/:id", s.adminSavePayChannel)
	admin.POST("/pay-channels/sort", s.adminSortPayChannels)
}

func (s *Server) checkoutRoutes(auth *gin.RouterGroup) {
	auth.GET("/merchant/branches/:bid/pay-options", s.withBranch(pAny, s.payOptions))
	auth.PUT("/merchant/branches/:bid/pay-settings", s.withBranch(pSettings, s.savePaySettings))
	auth.POST("/merchant/orders/:id/adjust-preview", s.previewAdjust)
	auth.PUT("/merchant/orders/:id/adjust", s.setAdjust)
	auth.PUT("/merchant/orders/:id/payment", s.setPayment)
}

func (s *Server) adminPayCountries(c *gin.Context) {
	cs, err := s.App.PayCountries(c)
	reply(c, gin.H{"countries": cs}, err)
}

func (s *Server) adminPayChannels(c *gin.Context) {
	cs, err := s.App.ListPayChannels(c, c.Query("country"))
	reply(c, gin.H{"channels": cs}, err)
}

func (s *Server) adminSavePayChannel(c *gin.Context) {
	var id int64
	if c.Param("id") != "" {
		var ok bool
		if id, ok = idParam(c, "id"); !ok {
			return
		}
	}
	var in app.PayChannelInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SavePayChannel(c, adminActor(c), id, in)
	reply(c, gin.H{"id": id}, err)
}

func (s *Server) adminSortPayChannels(c *gin.Context) {
	var in struct {
		Country string  `json:"country"`
		IDs     []int64 `json:"ids"`
	}
	if !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.SortPayChannels(c, adminActor(c), in.Country, in.IDs))
}

func (s *Server) payOptions(c *gin.Context, bid int64) {
	o, err := s.App.PayOptions(c, bid)
	reply(c, o, err)
}

func (s *Server) savePaySettings(c *gin.Context, bid int64) {
	var in app.PaySettingsInput
	if !bind(c, &in) {
		return
	}
	if err := s.App.SavePaySettings(c, bid, in, staffActor(c)); err != nil {
		fail(c, err)
		return
	}
	s.payOptions(c, bid)
}

// discountOK checks that the person may give a discount at checkout, or
// spends a manager's approval for it (§5.3).
func (s *Server) discountOK(c *gin.Context, bid int64) bool {
	err := permError(c, bid, []string{app.PermCheckoutDiscount})
	if err == errForbidden && c.GetHeader("X-Approval") != "" {
		if aerr := s.useApproval(c, bid, []string{app.PermCheckoutDiscount}); aerr != nil {
			fail(c, aerr)
			return false
		}
		return true
	}
	if err != nil {
		fail(c, err)
		return false
	}
	return true
}

func (s *Server) previewAdjust(c *gin.Context) {
	id, ok := s.orderScope(c, need(app.PermOrderView, app.PermTableUse, app.PermOrderManual))
	var in app.Adjust
	if !ok || !bind(c, &in) {
		return
	}
	t, err := s.App.PreviewOrderAdjust(c, id, in)
	reply(c, t, err)
}

func (s *Server) setAdjust(c *gin.Context) {
	id, ok := s.orderScope(c, pHandle)
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int        `json:"expected_version"`
		Adjust          app.Adjust `json:"adjust"`
	}
	if !bind(c, &in) {
		return
	}
	bid, err := s.App.OrderBranch(c, id)
	if err != nil {
		fail(c, err)
		return
	}
	if !s.discountOK(c, bid) {
		return
	}
	o, err := s.App.SetOrderAdjust(c, id, in.ExpectedVersion, in.Adjust, staffActor(c))
	reply(c, o, err)
}

func (s *Server) setPayment(c *gin.Context) {
	id, ok := s.orderScope(c, pHandle)
	var in app.PayInput
	if !ok || !bind(c, &in) {
		return
	}
	o, err := s.App.SetOrderPayment(c, id, in, staffActor(c))
	reply(c, o, err)
}
