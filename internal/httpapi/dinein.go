package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

const (
	tableCookie  = "ts" // scanned phone: token of one table session
	deviceCookie = "td" // paired tablet: long-lived, bound to a table
)

// ---- staff: tables and sessions ----

func (s *Server) listTables(c *gin.Context) {
	bid, ok := branchScope(c, pTable)
	if !ok {
		return
	}
	tables, err := s.App.ListTables(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	recent, err := s.App.RecentBills(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"tables": tables, "recent": recent})
}

func (s *Server) saveTable(c *gin.Context) {
	bid, ok := branchScope(c, pTableManage)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.TableInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SaveTable(c, bid, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

// tableScope reads :id as a table and checks the role in its branch.
func (s *Server) tableScope(c *gin.Context, roles []string) (int64, bool) {
	id, ok := idParam(c, "id")
	if !ok {
		return 0, false
	}
	bid, err := s.App.TableBranch(c, id)
	if err != nil {
		fail(c, err)
		return 0, false
	}
	return id, allowed(c, bid, roles)
}

// sessionScope reads :id as a table session and checks the role in its branch.
func (s *Server) sessionScope(c *gin.Context, roles []string) (int64, bool) {
	id, ok := idParam(c, "id")
	if !ok {
		return 0, false
	}
	bid, err := s.App.SessionBranch(c, id)
	if err != nil {
		fail(c, err)
		return 0, false
	}
	return id, allowed(c, bid, roles)
}

func (s *Server) openTable(c *gin.Context) {
	id, ok := s.tableScope(c, pTable)
	if !ok {
		return
	}
	var in struct {
		Guests int `json:"guests"`
	}
	if !bind(c, &in) {
		return
	}
	bill, err := s.App.OpenTable(c, id, in.Guests, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, bill)
}

func (s *Server) getBill(c *gin.Context) {
	id, ok := s.sessionScope(c, pTable)
	if !ok {
		return
	}
	bill, err := s.App.GetBill(c, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, bill)
}

func (s *Server) staffBatch(c *gin.Context) {
	id, ok := s.sessionScope(c, pTable)
	if !ok {
		return
	}
	var in app.BatchInput
	if !bind(c, &in) {
		return
	}
	bill, err := s.App.AddBatch(c, id, "staff", c.GetHeader("Idempotency-Key"), in, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, bill)
}

func (s *Server) closeTable(c *gin.Context) {
	id, ok := s.sessionScope(c, pTable)
	if !ok {
		return
	}
	var in app.CloseInput
	if !bind(c, &in) {
		return
	}
	bill, err := s.App.CloseTable(c, id, in, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, bill)
}

// billItem handles serve (kitchen and desk) and void (desk only) for one line.
func (s *Server) billItem(c *gin.Context) {
	itemID, ok := idParam(c, "id")
	if !ok {
		return
	}
	_, bid, err := s.App.ItemOrder(c, itemID)
	if err != nil {
		fail(c, err)
		return
	}
	var bill *app.Bill
	switch c.Param("action") {
	case "serve":
		if !allowed(c, bid, pKitchen) {
			return
		}
		bill, err = s.App.ServeItem(c, itemID, staffActor(c))
	case "void":
		if !allowed(c, bid, pTable) {
			return
		}
		var in struct {
			Reason string `json:"reason"`
		}
		if !bind(c, &in) {
			return
		}
		bill, err = s.App.VoidItem(c, itemID, in.Reason, staffActor(c))
	default:
		if !allowed(c, bid, pKitchen) {
			return
		}
		err = app.ErrNotFound
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, bill)
}

func (s *Server) pairTableDevice(c *gin.Context) {
	id, ok := s.tableScope(c, pTable)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !bind(c, &in) {
		return
	}
	code, err := s.App.CreateDevicePairing(c, id, in.Name, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"code": code, "expires_in_minutes": 10})
}

func (s *Server) revokeTableDevice(c *gin.Context) {
	bid, ok := branchScope(c, pTable)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := s.App.RevokeDevice(c, bid, id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- guests: scanned phone or paired tablet ----

func (s *Server) exchangeTableCode(c *gin.Context) {
	var in struct {
		Code string `json:"code"`
	}
	if !bind(c, &in) {
		return
	}
	token, err := s.App.ExchangeTableCode(c, in.Code)
	if err != nil {
		// Whatever the phone held belonged to an earlier sitting.
		s.setCookie(c, tableCookie, "", -1)
		fail(c, err)
		return
	}
	s.setCookie(c, tableCookie, token, int(app.TableSessionTTL.Seconds()))
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) pairDevice(c *gin.Context) {
	var in struct {
		Code string `json:"code"`
	}
	if !bind(c, &in) {
		return
	}
	token, err := s.App.PairDevice(c, in.Code)
	if err != nil {
		fail(c, err)
		return
	}
	s.setCookie(c, deviceCookie, token, int(app.DeviceTTL.Seconds()))
	c.JSON(200, gin.H{"ok": true})
}

// tableAccess resolves the guest credential named by ?via= (device for a
// paired tablet, otherwise the scanned phone's session cookie).
func (s *Server) tableAccess(c *gin.Context) (*app.TableAccess, bool) {
	var acc *app.TableAccess
	var err error
	if c.Query("via") == "device" {
		token, _ := c.Cookie(deviceCookie)
		acc, err = s.App.AccessByDevice(c, token)
	} else {
		token, cerr := c.Cookie(tableCookie)
		if cerr != nil || token == "" {
			err = app.ErrSessionClosed
		} else {
			acc, err = s.App.AccessByToken(c, token)
		}
	}
	if err != nil {
		fail(c, err)
		return nil, false
	}
	return acc, true
}

func (s *Server) currentTable(c *gin.Context) {
	acc, ok := s.tableAccess(c)
	if !ok {
		return
	}
	var bill *app.Bill
	if acc.SessionID != 0 {
		var err error
		if bill, err = s.App.GuestBill(c, acc.SessionID); err != nil {
			fail(c, err)
			return
		}
	}
	c.JSON(200, gin.H{"access": acc, "bill": bill})
}

func (s *Server) guestBatch(c *gin.Context) {
	acc, ok := s.tableAccess(c)
	if !ok {
		return
	}
	if acc.SessionID == 0 {
		fail(c, app.ErrSessionClosed)
		return
	}
	var in app.BatchInput
	if !bind(c, &in) {
		return
	}
	bill, err := s.App.AddBatch(c, acc.SessionID, acc.Mode, c.GetHeader("Idempotency-Key"), in, app.Actor{Type: "guest"})
	if err != nil {
		fail(c, err)
		return
	}
	bill.Order.Cash, bill.Order.Delivery = nil, nil
	c.JSON(200, bill)
}

// tableEvents streams refresh hints. A phone follows its session's order; a
// tablet follows the branch, so it notices when its table is opened or closed.
// Hints carry no data: every screen re-reads what it is allowed to see.
func (s *Server) tableEvents(c *gin.Context) {
	acc, ok := s.tableAccess(c)
	if !ok {
		return
	}
	if acc.Mode == "ipad" {
		s.Hub.serve(c, &subscription{branch: acc.BranchID})
		return
	}
	s.Hub.serve(c, &subscription{order: acc.OrderID})
}
