package httpapi

import (
	"github.com/gin-gonic/gin"
)

// Cooking progress in the back office (docs/COOKING_PROGRESS_DESIGN.md).

func (s *Server) progress(c *gin.Context) {
	bid, ok := branchScope(c, pProgress)
	if !ok {
		return
	}
	p, err := s.App.Progress(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	// The page needs dishes and timing, not how to reach the customer.
	for _, o := range p.Orders {
		o.CustomerPhone, o.AddressUnit, o.AddressNote, o.Cash = "", "", "", nil
		o.ContactPhone, o.ContactWechat, o.ContactTelegram, o.ContactLine, o.ContactWhatsapp = "", "", "", "", ""
	}
	c.JSON(200, p)
}

func (s *Server) progressDone(c *gin.Context) {
	bid, ok := branchScope(c, pProgress)
	if !ok {
		return
	}
	d, err := s.App.ProgressDone(c, bid, c.Query("date"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, d)
}

// rushDish marks a dish as rushed on the kitchen screen; any service type.
func (s *Server) rushDish(c *gin.Context) {
	itemID, ok := idParam(c, "id")
	if !ok {
		return
	}
	_, bid, err := s.App.ItemOrder(c, itemID)
	if err != nil {
		fail(c, err)
		return
	}
	if !allowed(c, bid, pHandle) {
		return
	}
	o, err := s.App.RushDish(c, itemID, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, o)
}
