package httpapi

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// ---- customer ----

func (s *Server) storefront(c *gin.Context) {
	sf, err := s.App.Storefront(c, c.Param("slug"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, sf)
}

func (s *Server) otpRequest(c *gin.Context) {
	var in struct {
		Phone string `json:"phone"`
	}
	if !bind(c, &in) {
		return
	}
	code, err := s.App.RequestOTP(c, in.Phone)
	if err != nil {
		fail(c, err)
		return
	}
	out := gin.H{"sent": true}
	if code != "" {
		out["dev_code"] = code // only when OTP_DEV_ECHO=true
	}
	c.JSON(200, out)
}

func (s *Server) otpVerify(c *gin.Context) {
	var in struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if !bind(c, &in) {
		return
	}
	token, phone, err := s.App.VerifyOTP(c, in.Phone, in.Code)
	if err != nil {
		fail(c, err)
		return
	}
	s.setCookie(c, customerCookie, token, int(app.CustomerSessionTTL.Seconds()))
	c.JSON(200, gin.H{"verified": true, "phone": phone})
}

func (s *Server) customerPhone(c *gin.Context) (string, error) {
	token, err := c.Cookie(customerCookie)
	if err != nil || token == "" {
		return "", app.Err(http.StatusForbidden, "PHONE_NOT_VERIFIED", "verify your phone number first")
	}
	return s.App.CustomerPhone(c, token)
}

func (s *Server) customerSession(c *gin.Context) {
	phone, err := s.customerPhone(c)
	if err != nil {
		c.JSON(200, gin.H{"verified": false})
		return
	}
	c.JSON(200, gin.H{"verified": true, "phone": phone})
}

func (s *Server) createQuote(c *gin.Context) {
	var in app.QuoteInput
	if !bind(c, &in) {
		return
	}
	// The phone is optional here: a verified customer gets promotions checked
	// against their own usage; an anonymous one is checked again when ordering.
	phone, _ := s.customerPhone(c)
	q, err := s.App.CreateQuote(c, c.Param("slug"), in, phone)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, q)
}

func (s *Server) placeOrder(c *gin.Context) {
	phone, err := s.customerPhone(c)
	if err != nil {
		fail(c, err)
		return
	}
	var in app.PlaceOrderInput
	if !bind(c, &in) {
		return
	}
	out, err := s.App.PlaceOrder(c, c.Param("slug"), phone, c.GetHeader("Idempotency-Key"), in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

// customerView removes store-internal data from an order.
func customerView(o *app.Order) *app.Order {
	o.Cash = nil
	return o
}

func (s *Server) customerOrder(c *gin.Context) {
	o, err := s.App.OrderByToken(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, customerView(o))
}

func (s *Server) customerCancel(c *gin.Context) {
	o, err := s.App.CustomerCancel(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, customerView(o))
}

func (s *Server) customerEvents(c *gin.Context) {
	o, err := s.App.OrderByToken(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	s.Hub.serve(c, &subscription{order: o.ID})
}

// ---- auth ----

func (s *Server) login(c *gin.Context) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !bind(c, &in) {
		return
	}
	token, u, err := s.App.Login(c, in.Username, in.Password)
	if err != nil {
		fail(c, err)
		return
	}
	s.setCookie(c, staffCookie, token, int(app.SessionTTL.Seconds()))
	c.JSON(200, u)
}

// tokenLogin is the app's sign-in: the session token comes back in the body
// and no cookie is set.
func (s *Server) tokenLogin(c *gin.Context) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !bind(c, &in) {
		return
	}
	token, u, expires, err := s.App.TokenLogin(c, in.Username, in.Password)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"token": token, "expires_at": expires, "user": u})
}

func (s *Server) logout(c *gin.Context) {
	if token := sessionToken(c); token != "" {
		_ = s.App.Logout(c, token)
	}
	s.setCookie(c, staffCookie, "", -1)
	c.JSON(200, gin.H{"ok": true})
}

// ---- merchant order desk ----

func (s *Server) listOrders(c *gin.Context) {
	bid, ok := branchScope(c, deskRoles)
	if !ok {
		return
	}
	orders, err := s.App.ListOrders(c, bid, c.Query("scope"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"orders": orders})
}

func (s *Server) branchEvents(c *gin.Context) {
	bid, ok := branchScope(c, allStaff)
	if !ok {
		return
	}
	s.Hub.serve(c, &subscription{branch: bid})
}

func (s *Server) listRiders(c *gin.Context) {
	bid, ok := branchScope(c, deskRoles)
	if !ok {
		return
	}
	riders, err := s.App.ListRiders(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"riders": riders})
}

func (s *Server) getOrder(c *gin.Context) {
	id, ok := s.orderScope(c, allStaff)
	if !ok {
		return
	}
	o, err := s.App.GetOrder(c, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, o)
}

type orderCommand struct {
	ExpectedVersion int    `json:"expected_version"`
	Reason          string `json:"reason"`
	RiderID         int64  `json:"rider_id"`
}

func (s *Server) orderCmd(c *gin.Context, roles []string, fn func(id int64, in orderCommand) (*app.Order, error)) {
	id, ok := s.orderScope(c, roles)
	if !ok {
		return
	}
	var in orderCommand
	if c.Request.ContentLength != 0 && !bind(c, &in) {
		return
	}
	o, err := fn(id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, o)
}

func (s *Server) acceptOrder(c *gin.Context) {
	s.orderCmd(c, deskRoles, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.Accept(c, id, in.ExpectedVersion, staffActor(c))
	})
}

func (s *Server) rejectOrder(c *gin.Context) {
	s.orderCmd(c, deskRoles, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.Reject(c, id, in.ExpectedVersion, in.Reason, staffActor(c))
	})
}

func (s *Server) cancelOrder(c *gin.Context) {
	s.orderCmd(c, deskRoles, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.StaffCancel(c, id, in.ExpectedVersion, in.Reason, staffActor(c))
	})
}

func (s *Server) assignRider(c *gin.Context) {
	s.orderCmd(c, deskRoles, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.AssignRider(c, id, in.RiderID, staffActor(c))
	})
}

func (s *Server) handoffOrder(c *gin.Context) {
	s.orderCmd(c, kitchenRoles, func(id int64, _ orderCommand) (*app.Order, error) {
		return s.App.StaffHandoff(c, id, staffActor(c))
	})
}

// ---- merchant configuration ----

func (s *Server) adminMenu(c *gin.Context) {
	bid, ok := branchScope(c, allStaff)
	if !ok {
		return
	}
	m, err := s.App.AdminMenu(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

// optionalID reads :id when the route has one (PUT) and returns 0 otherwise (POST).
func optionalID(c *gin.Context) (int64, bool) {
	if c.Param("id") == "" {
		return 0, true
	}
	return idParam(c, "id")
}

func (s *Server) saveCategory(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.CategoryInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SaveCategory(c, bid, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

func (s *Server) saveItem(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.ItemInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SaveItem(c, bid, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

// patchItem covers the quick sold-out toggle, which kitchen staff may also use.
func (s *Server) patchItem(c *gin.Context) {
	bid, ok := branchScope(c, allStaff)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in app.ItemPatch
	if !bind(c, &in) {
		return
	}
	role := currentUser(c).RoleIn(bid)
	if (in.Published != nil || in.Archived != nil) && role != "owner" && role != "manager" {
		fail(c, app.Err(http.StatusForbidden, "FORBIDDEN", "only managers can publish or remove items"))
		return
	}
	if err := s.App.PatchItem(c, bid, id, in); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) saveZone(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.ZoneInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SaveZone(c, bid, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

func (s *Server) deleteZone(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := s.App.DeleteZone(c, bid, id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) reorderZones(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !bind(c, &in) {
		return
	}
	if err := s.App.ReorderZones(c, bid, in.IDs); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) saveLocation(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in app.LocationInput
	if !bind(c, &in) {
		return
	}
	b, err := s.App.SaveLocation(c, bid, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, b)
}

func (s *Server) saveZoneMode(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in app.ZoneModeInput
	if !bind(c, &in) {
		return
	}
	b, err := s.App.SaveZoneMode(c, bid, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, b)
}

// publicConfig is what every page may know before anyone signs in.
func (s *Server) publicConfig(c *gin.Context) {
	c.JSON(200, gin.H{"maps_key": s.Cfg.GoogleMapsKey, "push_key": s.Cfg.VAPIDPublicKey})
}

func (s *Server) locate(c *gin.Context) {
	var in struct {
		Lat *float64 `json:"lat"`
		Lng *float64 `json:"lng"`
	}
	if !bind(c, &in) {
		return
	}
	if in.Lat == nil || in.Lng == nil {
		fail(c, app.ErrValidation("lat and lng are required"))
		return
	}
	loc, err := s.App.Locate(c, c.Param("slug"), *in.Lat, *in.Lng)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, loc)
}

func (s *Server) saveSettings(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in app.SettingsInput
	if !bind(c, &in) {
		return
	}
	b, err := s.App.SaveSettings(c, bid, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, b)
}

func (s *Server) listStaff(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	staff, err := s.App.ListStaff(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"staff": staff})
}

func (s *Server) createStaff(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in app.NewUser
	if !bind(c, &in) {
		return
	}
	id, err := s.App.CreateStaffBy(c, bid, in, currentUser(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

// staffScope resolves the branch (managers only) and the staff member's id.
func staffScope(c *gin.Context) (bid, id int64, ok bool) {
	if bid, ok = branchScope(c, managerRoles); !ok {
		return
	}
	id, ok = idParam(c, "id")
	return
}

func (s *Server) patchStaff(c *gin.Context) {
	bid, id, ok := staffScope(c)
	if !ok {
		return
	}
	var in app.StaffPatch
	if !bind(c, &in) {
		return
	}
	if err := s.App.UpdateStaff(c, bid, id, in, currentUser(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) deleteStaff(c *gin.Context) {
	bid, id, ok := staffScope(c)
	if !ok {
		return
	}
	if err := s.App.DeleteStaff(c, bid, id, currentUser(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) resetStaffPassword(c *gin.Context) {
	bid, id, ok := staffScope(c)
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !bind(c, &in) {
		return
	}
	if err := s.App.ResetStaffPassword(c, bid, id, in.Password, currentUser(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) listCash(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	cash, err := s.App.ListCash(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"collections": cash})
}

func (s *Server) reconcile(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	bid, err := s.App.CashBranch(c, id)
	if err != nil {
		fail(c, err)
		return
	}
	if !allowed(c, bid, managerRoles) {
		return
	}
	var in struct {
		AmountReturned int64  `json:"amount_returned"`
		Note           string `json:"note"`
	}
	if !bind(c, &in) {
		return
	}
	out, err := s.App.Reconcile(c, id, in.AmountReturned, in.Note, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, out)
}

// ---- kitchen ----

func (s *Server) kitchenBoard(c *gin.Context) {
	bid, ok := branchScope(c, kitchenRoles)
	if !ok {
		return
	}
	orders, err := s.App.KitchenBoard(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	// The kitchen needs dishes and notes, not the customer's contact details.
	for _, o := range orders {
		o.CustomerPhone, o.AddressLine, o.AddressUnit, o.AddressNote, o.Cash = "", "", "", "", nil
	}
	c.JSON(200, gin.H{"orders": orders})
}

func (s *Server) itemAction(c *gin.Context) {
	itemID, ok := idParam(c, "id")
	if !ok {
		return
	}
	orderID, bid, err := s.App.ItemOrder(c, itemID)
	if err != nil {
		fail(c, err)
		return
	}
	if !allowed(c, bid, kitchenRoles) {
		return
	}
	var o *app.Order
	if c.Param("action") == "station" {
		var in struct {
			StationID *int64 `json:"station_id"`
			Reason    string `json:"reason"`
		}
		if !bind(c, &in) {
			return
		}
		o, err = s.App.MoveItemStation(c, orderID, itemID, in.StationID, in.Reason, staffActor(c))
	} else {
		o, err = s.App.SetItemStatus(c, orderID, itemID, c.Param("action"), staffActor(c))
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, o)
}

func (s *Server) packOrder(c *gin.Context) {
	id, ok := s.orderScope(c, kitchenRoles)
	if !ok {
		return
	}
	var in struct {
		Bags            int `json:"bags"`
		ManifestVersion int `json:"manifest_version"`
	}
	if !bind(c, &in) {
		return
	}
	o, err := s.App.Pack(c, id, in.Bags, in.ManifestVersion, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, o)
}

// ---- rider ----

// riderView gives the rider what the delivery needs and hides the customer's
// contact details once the task is over.
func riderView(o *app.Order) *app.Order {
	o.Cash = nil
	if o.Delivery != nil && (o.Delivery.Status == "delivered" || o.Delivery.Status == "failed") {
		o.CustomerPhone, o.AddressUnit, o.AddressNote = "", "", ""
	}
	return o
}

func (s *Server) riderTasks(c *gin.Context) {
	orders, err := s.App.RiderTasks(c, currentUser(c).ID)
	if err != nil {
		fail(c, err)
		return
	}
	for _, o := range orders {
		riderView(o)
	}
	c.JSON(200, gin.H{"tasks": orders})
}

func (s *Server) riderEvents(c *gin.Context) {
	s.Hub.serve(c, &subscription{rider: currentUser(c).ID})
}

func (s *Server) riderAction(c *gin.Context) {
	taskID, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in struct {
		CollectedAmount int64  `json:"collected_amount"`
		Reason          string `json:"reason"`
	}
	if c.Request.ContentLength != 0 && !bind(c, &in) {
		return
	}
	riderID := currentUser(c).ID
	var o *app.Order
	var err error
	switch c.Param("action") {
	case "pickup":
		o, err = s.App.RiderPickup(c, taskID, riderID)
	case "arrive":
		o, err = s.App.RiderArrive(c, taskID, riderID)
	case "deliver":
		o, err = s.App.RiderDeliver(c, taskID, riderID, in.CollectedAmount)
	case "fail":
		o, err = s.App.RiderFail(c, taskID, riderID, in.Reason)
	default:
		err = app.ErrNotFound
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, riderView(o))
}

// ---- failed deliveries ----

func (s *Server) redeliverOrder(c *gin.Context) {
	s.orderCmd(c, deskRoles, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.Redeliver(c, id, in.RiderID, staffActor(c))
	})
}

func (s *Server) closeFailedOrder(c *gin.Context) {
	s.orderCmd(c, deskRoles, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.CloseFailed(c, id, in.Reason, staffActor(c))
	})
}

// ---- kitchen stations ----

func (s *Server) listStations(c *gin.Context) {
	bid, ok := branchScope(c, allStaff)
	if !ok {
		return
	}
	st, err := s.App.ListStations(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"stations": st})
}

func (s *Server) saveStation(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.StationInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SaveStation(c, bid, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

func (s *Server) assignStation(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in struct {
		ItemIDs   []int64 `json:"item_ids"`
		StationID *int64  `json:"station_id"`
	}
	if !bind(c, &in) {
		return
	}
	n, err := s.App.AssignStation(c, bid, in.ItemIDs, in.StationID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"updated": n})
}

// ---- promotions ----

func (s *Server) listPromotions(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	ps, err := s.App.ListPromotions(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"promotions": ps})
}

func (s *Server) savePromotion(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.PromotionInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SavePromotion(c, bid, id, in, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

func (s *Server) setPromotionStatus(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !bind(c, &in) {
		return
	}
	if err := s.App.SetPromotionStatus(c, bid, id, in.Status, staffActor(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) trialPromotions(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	var in app.TrialInput
	if !bind(c, &in) {
		return
	}
	res, err := s.App.TrialPromotions(c, bid, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, res)
}

// ---- menu import (Excel) ----

func (s *Server) menuImportTemplate(c *gin.Context) {
	if _, ok := branchScope(c, managerRoles); !ok {
		return
	}
	b, err := app.MenuImportTemplate()
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="menu-template.xlsx"`)
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b)
}

// uploadMenuImport takes the .xlsx as the raw request body; the file name
// comes in the query string. The body is capped before it is read.
func (s *Server) uploadMenuImport(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	const limit = 2 << 20
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit+1))
	if err != nil || len(body) > limit {
		fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the file is larger than 2 MB"))
		return
	}
	id, err := s.App.CreateMenuImport(c, bid, c.Query("filename"), body, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

func (s *Server) listMenuImports(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	list, err := s.App.ListMenuImports(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"imports": list})
}

func (s *Server) getMenuImport(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	m, err := s.App.GetMenuImport(c, bid, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

func (s *Server) applyMenuImport(c *gin.Context) {
	bid, ok := branchScope(c, managerRoles)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	m, err := s.App.ApplyMenuImport(c, bid, id, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

// ---- rider duty, position and devices ----

func (s *Server) riderDuty(c *gin.Context) {
	st, err := s.App.RiderStatus(c, currentUser(c).ID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, st)
}

func (s *Server) setRiderDuty(c *gin.Context) {
	var in struct {
		OnDuty *bool `json:"on_duty"`
	}
	if !bind(c, &in) {
		return
	}
	if in.OnDuty == nil {
		fail(c, app.ErrValidation("on_duty is required"))
		return
	}
	st, err := s.App.SetDuty(c, currentUser(c).ID, *in.OnDuty, sessionToken(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, st)
}

func (s *Server) riderLocations(c *gin.Context) {
	var in struct {
		Points []app.LocationPoint `json:"points"`
	}
	if !bind(c, &in) {
		return
	}
	n, err := s.App.ReportLocations(c, currentUser(c).ID, in.Points)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"accepted": n})
}

func (s *Server) registerDevice(c *gin.Context) {
	var in struct {
		Platform   string `json:"platform"`
		Token      string `json:"token"`
		AppVersion string `json:"app_version"`
	}
	if !bind(c, &in) {
		return
	}
	d, err := s.App.RegisterDevice(c, currentUser(c).ID, in.Platform, in.Token, in.AppVersion, sessionToken(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, d)
}

func (s *Server) removeDevice(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := s.App.RemoveDevice(c, currentUser(c).ID, id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
