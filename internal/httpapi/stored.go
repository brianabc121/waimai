package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// Stored value, "top up X, get Y free" (docs/STORED_VALUE_DESIGN.md): the
// store's marketing page and counter, the customer's offers page and top-up
// code, and the platform's switch per country.

var (
	pStoredTopup   = need(app.PermStoredTopup)
	pStoredCashout = need(app.PermStoredCashout)
)

func (s *Server) storedRoutes(api, auth, admin *gin.RouterGroup) {
	api.GET("/stores/:slug/stored", s.storeStored)
	api.POST("/stores/:slug/topups", s.limit(s.publicWrites), s.createTopup)
	api.GET("/customer/topups/:id", s.myTopup)
	api.POST("/customer/topups/:id/cancel", s.cancelMyTopup)
	api.GET("/customer/store-credits/:bid/stored", s.myStoredEntries)

	auth.GET("/merchant/branches/:bid/marketing", s.storedOverview)
	auth.POST("/merchant/branches/:bid/marketing/terms", s.acceptStoredTerms)
	auth.POST("/merchant/branches/:bid/stored-campaigns", s.saveStoredCampaign)
	auth.PUT("/merchant/branches/:bid/stored-campaigns/:id", s.saveStoredCampaign)
	auth.POST("/merchant/branches/:bid/stored-campaigns/:id/status", s.setStoredCampaignStatus)
	auth.GET("/merchant/branches/:bid/topups", s.branchTopups)
	auth.POST("/merchant/branches/:bid/topups", s.startStaffTopup)
	auth.POST("/merchant/branches/:bid/topups/:id/confirm", s.confirmTopup)
	auth.POST("/merchant/branches/:bid/topups/:id/revoke", s.revokeTopup)
	auth.GET("/merchant/branches/:bid/stored", s.branchStored)
	auth.GET("/merchant/branches/:bid/stored/:cid/cashout", s.cashoutQuote)
	auth.POST("/merchant/branches/:bid/stored/:cid/cashout", s.storedCashout)

	admin.GET("/stored/countries", s.adminStoredCountries)
	admin.PUT("/stored/countries", s.adminSaveStoredCountry)
	admin.GET("/stored/branches", s.adminStoredBranches)
}

// branchScopeApproving is branchScope where a manager's approval may stand in
// for what the role may not do (docs/STAFF_ACCESS_DESIGN.md §7), and where a
// role that may do it can still bring one for the store's limits.
func (s *Server) branchScopeApproving(c *gin.Context, perms, approve []string) (int64, bool) {
	bid, ok := idParam(c, "bid")
	if !ok {
		return 0, false
	}
	err := permError(c, bid, perms)
	if tok := c.GetHeader("X-Approval"); tok != "" && (err == nil || err == errForbidden) {
		need := approve
		if err != nil {
			need = perms
		}
		if aerr := s.useApproval(c, bid, need); aerr != nil {
			fail(c, aerr)
			return 0, false
		}
		return bid, true
	}
	if err != nil {
		fail(c, err)
		return 0, false
	}
	return bid, true
}

func (s *Server) storeStored(c *gin.Context) {
	var cid int64
	if cu, err := s.customer(c); err == nil {
		cid = cu.ID
	}
	r, err := s.App.StoredOffers(c, c.Param("slug"), cid)
	reply(c, r, err)
}

func (s *Server) createTopup(c *gin.Context) {
	cu, ok := s.me(c)
	var in app.TopupInput
	if !ok || !bind(c, &in) {
		return
	}
	t, err := s.App.CreateTopup(c, c.Param("slug"), cu.ID, in)
	reply(c, t, err)
}

func (s *Server) myTopup(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	t, err := s.App.MyTopup(c, cu.ID, id)
	reply(c, t, err)
}

func (s *Server) cancelMyTopup(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.CancelMyTopup(c, cu.ID, id))
}

func (s *Server) myStoredEntries(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	bid, ok := idParam(c, "bid")
	if !ok {
		return
	}
	l, err := s.App.MyStoredEntries(c, cu.ID, bid)
	reply(c, gin.H{"items": l}, err)
}

func (s *Server) storedOverview(c *gin.Context) {
	// the counter reads the running offer too
	bid, ok := branchScope(c, need(app.PermPromo, app.PermWalletView, app.PermStoredTopup))
	if !ok {
		return
	}
	r, err := s.App.StoredOverview(c, bid)
	reply(c, r, err)
}

func (s *Server) acceptStoredTerms(c *gin.Context) {
	bid, ok := branchScope(c, pOwner)
	if !ok {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.AcceptStoredTerms(c, bid, staffActor(c)))
}

func (s *Server) saveStoredCampaign(c *gin.Context) {
	bid, ok := branchScope(c, pPromo)
	var in app.StoredCampaignInput
	if !ok || !bind(c, &in) {
		return
	}
	var id int64
	if c.Param("id") != "" {
		if id, ok = idParam(c, "id"); !ok {
			return
		}
	}
	// only the owner changes the limit above which a manager approves
	if in.ApproveOver != nil && permError(c, bid, pOwner) != nil {
		in.ApproveOver = nil
	}
	id, err := s.App.SaveStoredCampaign(c, bid, id, in, staffActor(c))
	reply(c, gin.H{"id": id}, err)
}

func (s *Server) setStoredCampaignStatus(c *gin.Context) {
	bid, ok := branchScope(c, pPromo)
	var in struct {
		Status string `json:"status"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.SetStoredCampaignStatus(c, bid, id, in.Status, staffActor(c)))
}

func (s *Server) branchTopups(c *gin.Context) {
	bid, ok := branchScope(c, need(app.PermStoredTopup, app.PermWalletView))
	if !ok {
		return
	}
	l, err := s.App.BranchTopups(c, bid, c.Query("status"), c.Query("code"))
	reply(c, gin.H{"items": l}, err)
}

func (s *Server) startStaffTopup(c *gin.Context) {
	bid, ok := branchScope(c, pStoredTopup)
	var in app.TopupInput
	if !ok || !bind(c, &in) {
		return
	}
	r, err := s.App.StartStaffTopup(c, bid, in, staffActor(c))
	if err == nil && !s.App.OTPDevEcho {
		r.Verify.DevCode = ""
	}
	reply(c, r, err)
}

func (s *Server) confirmTopup(c *gin.Context) {
	replayable(c)
	bid, ok := s.branchScopeApproving(c, pStoredTopup, pStoredCashout)
	var in app.TopupConfirm
	if !ok || !bind(c, &in) {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	in.IdemKey = c.GetHeader("Idempotency-Key")
	t, err := s.App.ConfirmTopup(c, bid, id, in, staffActor(c))
	reply(c, t, err)
}

func (s *Server) revokeTopup(c *gin.Context) {
	bid, ok := s.branchScopeApproving(c, pStoredCashout, pStoredCashout)
	var in struct {
		Note string `json:"note"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.RevokeTopup(c, bid, id, in.Note, staffActor(c)))
}

func (s *Server) branchStored(c *gin.Context) {
	bid, ok := branchScope(c, pWalletView)
	if !ok {
		return
	}
	cust, _ := strconv.ParseInt(c.Query("customer"), 10, 64)
	r, err := s.App.BranchStored(c, bid, cust)
	reply(c, r, err)
}

func (s *Server) cashoutQuote(c *gin.Context) {
	bid, ok := branchScope(c, need(app.PermStoredCashout, app.PermWalletView))
	if !ok {
		return
	}
	cid, ok := idParam(c, "cid")
	if !ok {
		return
	}
	q, err := s.App.StoredCashoutQuote(c, bid, cid, c.DefaultQuery("reason", "customer"))
	reply(c, q, err)
}

func (s *Server) storedCashout(c *gin.Context) {
	replayable(c)
	bid, ok := s.branchScopeApproving(c, pStoredCashout, pStoredCashout)
	var in app.CashoutInput
	if !ok || !bind(c, &in) {
		return
	}
	cid, ok := idParam(c, "cid")
	if !ok {
		return
	}
	in.IdemKey = c.GetHeader("Idempotency-Key")
	q, err := s.App.StoredCashout(c, bid, cid, in, staffActor(c))
	reply(c, q, err)
}

func (s *Server) adminStoredCountries(c *gin.Context) {
	l, err := s.App.StoredCountries(c)
	reply(c, gin.H{"items": l}, err)
}

func (s *Server) adminSaveStoredCountry(c *gin.Context) {
	var in app.StoredCountry
	if !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.SaveStoredCountry(c, adminActor(c), in))
}

func (s *Server) adminStoredBranches(c *gin.Context) {
	l, err := s.App.AdminStoredBranches(c)
	reply(c, gin.H{"items": l}, err)
}
