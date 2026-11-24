package httpapi

import (
	"strconv"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/gin-gonic/gin"
)

// Adding dishes to a delivery or pickup order (docs/ADD_ON_ORDER_DESIGN.md).

func (s *Server) staffAddonQuote(c *gin.Context) {
	id, ok := s.orderScope(c, pManual)
	var in app.StaffAddonInput
	if !ok || !bind(c, &in) {
		return
	}
	q, err := s.App.StaffAddonQuote(c, id, in)
	reply(c, q, err)
}

func (s *Server) staffAddon(c *gin.Context) {
	id, ok := s.orderScope(c, pManual)
	var in app.StaffAddonInput
	if !ok || !bind(c, &in) {
		return
	}
	o, err := s.App.StaffAddon(c, id, c.GetHeader("Idempotency-Key"), in, staffActor(c))
	reply(c, o, err)
}

func batchSeq(c *gin.Context) (int, bool) {
	seq, err := strconv.Atoi(c.Param("seq"))
	if err != nil || seq < 1 {
		fail(c, app.ErrNotFound)
		return 0, false
	}
	return seq, true
}

func (s *Server) acceptAddon(c *gin.Context) {
	id, ok := s.orderScope(c, pHandle)
	if !ok {
		return
	}
	seq, ok := batchSeq(c)
	if !ok {
		return
	}
	o, err := s.App.AcceptAddon(c, id, seq, staffActor(c))
	reply(c, o, err)
}

func (s *Server) rejectAddon(c *gin.Context) {
	id, ok := s.orderScope(c, pHandle)
	if !ok {
		return
	}
	seq, ok := batchSeq(c)
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if c.Request.ContentLength != 0 && !bind(c, &in) {
		return
	}
	o, err := s.App.RejectAddon(c, id, seq, in.Reason, staffActor(c))
	reply(c, o, err)
}

func (s *Server) customerAddonQuote(c *gin.Context) {
	var in app.AddonInput
	if !bind(c, &in) {
		return
	}
	in.Lang = reqLang(c)
	q, err := s.App.CustomerAddonQuote(c, c.Param("token"), in)
	reply(c, q, err)
}

func (s *Server) customerAddon(c *gin.Context) {
	var in app.AddonInput
	if !bind(c, &in) {
		return
	}
	in.Lang = reqLang(c)
	o, err := s.App.CustomerAddon(c, c.Param("token"), c.GetHeader("Idempotency-Key"), in)
	s.customerPage(c, o, err)
}

func (s *Server) customerAddonCancel(c *gin.Context) {
	seq, ok := batchSeq(c)
	if !ok {
		return
	}
	o, err := s.App.CancelCustomerAddon(c, c.Param("token"), seq)
	s.customerPage(c, o, err)
}
