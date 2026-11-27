package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// Voids and refunds by dish (docs/VOID_ITEM_DESIGN.md).

var pVoid = need(app.PermOrderVoid)

func (s *Server) voidRoutes(auth *gin.RouterGroup) {
	auth.POST("/merchant/order-items/:id/void-quote", s.voidQuote)
	auth.POST("/merchant/order-items/:id/void", s.voidItem)
	auth.POST("/kitchen/voids/:id/ack", s.ackVoid)
	auth.POST("/merchant/orders/:id/refunds", s.refundOrder)
	auth.GET("/merchant/branches/:bid/voids", s.voidLog)
	auth.GET("/merchant/orders/:id/money-events", s.moneyEvents)
}

// voidScope checks who may void a line: the table's people for a dine-in
// line, the order handlers otherwise. It also says whether they may void a
// dish the kitchen already started: with "void dishes already made", or once
// with a manager's approval (X-Approval).
func (s *Server) voidScope(c *gin.Context) (itemID int64, canMade bool, ok bool) {
	itemID, ok = idParam(c, "id")
	if !ok {
		return 0, false, false
	}
	_, bid, err := s.App.ItemOrder(c, itemID)
	if err != nil {
		fail(c, err)
		return 0, false, false
	}
	dine, err := s.App.ItemDineIn(c, itemID)
	if err != nil {
		fail(c, err)
		return 0, false, false
	}
	base := pHandle
	if dine {
		base = pTable
	}
	if !allowed(c, bid, base) {
		return 0, false, false
	}
	if permError(c, bid, pVoid) == nil {
		return itemID, true, true
	}
	if tok := c.GetHeader("X-Approval"); tok != "" {
		by, err := s.App.UseApproval(c, tok, bid, currentUser(c).ID, pVoid)
		if err != nil {
			fail(c, err)
			return 0, false, false
		}
		c.Set("approved_by", by)
		return itemID, true, true
	}
	return itemID, false, true
}

func (s *Server) voidQuote(c *gin.Context) {
	id, canMade, ok := s.voidScope(c)
	var in app.VoidInput
	if !ok || !bind(c, &in) {
		return
	}
	q, err := s.App.QuoteVoid(c, id, in, canMade)
	reply(c, q, err)
}

func (s *Server) voidItem(c *gin.Context) {
	id, canMade, ok := s.voidScope(c)
	var in app.VoidInput
	if !ok || !bind(c, &in) {
		return
	}
	r, err := s.App.VoidLine(c, id, in, staffActor(c), canMade)
	reply(c, r, err)
}

func (s *Server) ackVoid(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	bid, err := s.App.VoidBranch(c, id)
	if err != nil {
		fail(c, err)
		return
	}
	if !allowed(c, bid, pKitchen) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.AckVoid(c, id, staffActor(c)))
}

func (s *Server) refundOrder(c *gin.Context) {
	var in app.RefundInput
	if !bind(c, &in) {
		return
	}
	// A dry run only works the amount out: it spends no approval. Cash put
	// into a store balance needs its own permission (which includes after-sales),
	// and a manager's (or the owner's) approval above the store's limits
	// (docs/REFUND_BALANCE_DESIGN.md §6.4): one approval covers both.
	perms := pAftersale
	if in.Dry {
		perms = pOrderView
	} else if in.ToCredit {
		perms = pWalletRefund
	}
	id, ok := s.orderScope(c, perms)
	if !ok {
		return
	}
	if in.ToCredit && !in.Dry && c.GetInt64("approved_by") == 0 {
		if tok := c.GetHeader("X-Approval"); tok != "" {
			bid, err := s.App.OrderBranch(c, id)
			if err != nil {
				fail(c, err)
				return
			}
			by, err := s.App.UseApproval(c, tok, bid, currentUser(c).ID, pWalletRefund)
			if err != nil {
				fail(c, err)
				return
			}
			c.Set("approved_by", by)
		}
	}
	in.IdemKey = c.GetHeader("Idempotency-Key")
	q, o, err := s.App.Refund(c, id, in, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"quote": q, "order": o})
}

func (s *Server) voidLog(c *gin.Context) {
	bid, ok := branchScope(c, pVoid)
	if !ok {
		return
	}
	l, err := s.App.VoidLog(c, bid, c.Query("date"))
	reply(c, l, err)
}

// moneyEvents is the order's money ledger: every change to its amounts.
func (s *Server) moneyEvents(c *gin.Context) {
	id, ok := s.orderScope(c, pOrderView)
	if !ok {
		return
	}
	l, err := s.App.OrderMoneyEvents(c, id)
	reply(c, l, err)
}
