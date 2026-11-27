package httpapi

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// Store credit (docs/REFUND_BALANCE_DESIGN.md): the customer's balances, the
// store's page of them, the texted code for orders the staff entered, and
// the platform's freeze.

var (
	pWalletRefund = need(app.PermWalletRefund)
	pWalletView   = need(app.PermWalletView)
)

func (s *Server) walletRoutes(api, auth, admin *gin.RouterGroup) {
	api.GET("/customer/store-credits", s.myStoreCredits)
	api.GET("/customer/store-credits/:bid", s.myStoreCreditEntries)

	auth.GET("/merchant/branches/:bid/credit", s.branchCredit)
	auth.PUT("/merchant/branches/:bid/credit-limits", s.saveCreditLimits)
	auth.POST("/merchant/refunds/:id/revoke-credit", s.revokeCreditRefund)
	auth.POST("/merchant/orders/:id/credit-code", s.requestCreditCode)
	auth.POST("/merchant/orders/:id/credit", s.spendCreditByCode)

	admin.GET("/customers/:id/store-credits", s.adminStoreCredits)
	admin.POST("/store-credits/:id/freeze", s.adminFreezeCredit)
	admin.POST("/branches/:bid/freeze-credits", s.adminFreezeBranchCredits)
	admin.GET("/wallet-check", s.adminWalletCheck)

	// the balance ledger and the closed days (docs/BALANCE_CHECK.md §5)
	auth.GET("/merchant/branches/:bid/wallet-ledger", s.branchWalletLedger)
	auth.GET("/merchant/branches/:bid/wallet-days", s.branchWalletDays)
	admin.GET("/wallet-ledger", s.adminWalletLedger)
	admin.GET("/wallet-days", s.adminWalletDays)
}

func ledgerFilter(c *gin.Context) app.LedgerFilter {
	f := app.LedgerFilter{Phone: c.Query("phone"), Order: c.Query("order"), Kind: c.Query("kind"),
		Account: c.Query("account"), From: c.Query("from"), To: c.Query("to")}
	f.Staff, _ = strconv.ParseInt(c.Query("staff"), 10, 64)
	f.Before, _ = strconv.ParseInt(c.Query("before"), 10, 64)
	return f
}

func (s *Server) branchWalletLedger(c *gin.Context) {
	bid, ok := branchScope(c, pWalletView)
	if !ok {
		return
	}
	f := ledgerFilter(c)
	f.BranchID, f.Mask = bid, true
	l, err := s.App.WalletLedger(c, f)
	reply(c, l, err)
}

func (s *Server) branchWalletDays(c *gin.Context) {
	bid, ok := branchScope(c, pWalletView)
	if !ok {
		return
	}
	t, err := s.App.WalletTrialBalance(c, bid, false, c.Query("day"))
	reply(c, t, err)
}

func (s *Server) adminWalletLedger(c *gin.Context) {
	f := ledgerFilter(c)
	f.BranchID, _ = strconv.ParseInt(c.Query("branch"), 10, 64)
	f.Platform = c.Query("platform") == "1"
	l, err := s.App.WalletLedger(c, f)
	if err == nil {
		l.Check, err = s.App.LastWalletCheck(c)
	}
	reply(c, l, err)
}

func (s *Server) adminWalletDays(c *gin.Context) {
	bid, _ := strconv.ParseInt(c.Query("branch"), 10, 64)
	t, err := s.App.WalletTrialBalance(c, bid, c.Query("all") == "1", c.Query("day"))
	reply(c, t, err)
}

func (s *Server) myStoreCredits(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	l, err := s.App.MyStoreCredits(c, cu.ID)
	reply(c, gin.H{"items": l}, err)
}

func (s *Server) myStoreCreditEntries(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	bid, ok := idParam(c, "bid")
	if !ok {
		return
	}
	l, err := s.App.MyStoreCreditEntries(c, cu.ID, bid)
	reply(c, gin.H{"items": l}, err)
}

func (s *Server) branchCredit(c *gin.Context) {
	bid, ok := branchScope(c, pWalletView)
	if !ok {
		return
	}
	brands, ok := s.brandView(c, bid)
	if !ok {
		return
	}
	staff, _ := strconv.ParseInt(c.Query("staff"), 10, 64)
	cust, _ := strconv.ParseInt(c.Query("customer"), 10, 64)
	r, err := s.App.BranchCredit(c, bid, app.CreditFilter{From: c.Query("from"), To: c.Query("to"), Brands: brands,
		StaffID: staff, Customer: cust})
	reply(c, r, err)
}

func (s *Server) saveCreditLimits(c *gin.Context) {
	bid, ok := branchScope(c, pOwner)
	var in app.CreditLimits
	if !ok || !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.SaveCreditLimits(c, bid, in, staffActor(c)))
}

func (s *Server) revokeCreditRefund(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	bid, err := s.App.RefundBranch(c, id)
	if err != nil {
		fail(c, err)
		return
	}
	if !allowed(c, bid, pWalletRefund) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.RevokeCreditRefund(c, id, staffActor(c)))
}

// requestCreditCode texts the customer of an order the staff entered a code
// for their store balance. Whoever may handle the order or its refunds asks.
func (s *Server) requestCreditCode(c *gin.Context) {
	var in struct {
		Phone   string `json:"phone"`
		Purpose string `json:"purpose"`
	}
	if !bind(c, &in) {
		return
	}
	perms := need(app.PermOrderHandle, app.PermTableUse, app.PermOrderManual)
	if in.Purpose == "refund" {
		perms = pWalletRefund
	}
	id, ok := s.orderScope(c, perms)
	if !ok {
		return
	}
	out, err := s.App.RequestCreditCode(c, id, in.Phone, in.Purpose, staffActor(c))
	if err == nil && !s.App.OTPDevEcho {
		out.DevCode = ""
	}
	reply(c, out, err)
}

func (s *Server) spendCreditByCode(c *gin.Context) {
	var in app.CreditVerify
	if !bind(c, &in) {
		return
	}
	id, ok := s.orderScope(c, need(app.PermOrderHandle, app.PermTableUse, app.PermOrderManual))
	if !ok {
		return
	}
	o, err := s.App.SpendCreditByCode(c, id, in, staffActor(c))
	reply(c, o, err)
}

func (s *Server) adminStoreCredits(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	l, err := s.App.AdminStoreCredits(c, id)
	reply(c, gin.H{"items": l}, err)
}

func (s *Server) adminFreezeCredit(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in struct {
		Frozen bool   `json:"frozen"`
		Note   string `json:"note"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.AdminFreezeCredit(c, adminActor(c), id, in.Frozen, in.Note))
}

func (s *Server) adminFreezeBranchCredits(c *gin.Context) {
	id, ok := idParam(c, "bid")
	var in struct {
		Frozen bool   `json:"frozen"`
		Note   string `json:"note"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	n, err := s.App.AdminFreezeBranchCredits(c, adminActor(c), id, in.Frozen, in.Note)
	reply(c, gin.H{"accounts": n}, err)
}

func (s *Server) adminWalletCheck(c *gin.Context) {
	r, err := s.App.LastWalletCheck(c)
	reply(c, gin.H{"last": r}, err)
}
