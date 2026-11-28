package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// ---- customer ----

func (s *Server) storefront(c *gin.Context) {
	// ?brand= is one of the store's brands (docs/MULTI_BRAND_DESIGN.md §5):
	// staff ordering under it see its menu, names and prices
	sf, err := s.App.StorefrontOf(c, c.Param("slug"), queryInt(c, "brand"))
	if err != nil {
		fail(c, err)
		return
	}
	// How the store divides its delivery area is the store's business. Where
	// the pin decides, customers get no areas at all; where they choose from a
	// list, they get the list without the outlines.
	if sf.Branch.ZoneMode == "map" {
		sf.Zones = []app.Zone{}
	}
	for i := range sf.Zones {
		sf.Zones[i].Polygon, sf.Zones[i].Color = nil, ""
	}
	if err := s.App.LocalizeStorefront(c, sf, reqLang(c)); err != nil {
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
	code, err := s.App.RequestOTP(c, in.Phone, c.ClientIP())
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

// customer is the logged-in customer of this request.
func (s *Server) customer(c *gin.Context) (*app.Customer, error) {
	token, err := c.Cookie(customerCookie)
	if err != nil || token == "" {
		return nil, app.Err(http.StatusForbidden, "PHONE_NOT_VERIFIED", "verify your phone number first")
	}
	return s.App.CustomerBySession(c, token)
}

func (s *Server) customerSession(c *gin.Context) {
	cu, err := s.customer(c)
	if err != nil {
		c.JSON(200, gin.H{"verified": false})
		return
	}
	lang, _ := s.App.CustomerLang(c, cu.ID)
	c.JSON(200, gin.H{"verified": true, "phone": cu.Phone, "customer_id": cu.ID, "lang": lang})
}

// saveCustomerLang remembers the language a signed-in customer picked, so a
// new phone or browser opens in it too.
func (s *Server) saveCustomerLang(c *gin.Context) {
	cu, err := s.customer(c)
	if err != nil {
		fail(c, err)
		return
	}
	var in struct {
		Lang string `json:"lang"`
	}
	if !bind(c, &in) {
		return
	}
	if err := s.App.SaveCustomerLang(c, cu.ID, in.Lang); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"lang": in.Lang})
}

func (s *Server) customerLogout(c *gin.Context) {
	if token, err := c.Cookie(customerCookie); err == nil && token != "" {
		_ = s.App.CustomerLogout(c, token)
	}
	s.setCookie(c, customerCookie, "", -1)
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) createQuote(c *gin.Context) {
	var in app.QuoteInput
	if !bind(c, &in) {
		return
	}
	if !app.IsLang(in.Lang) {
		in.Lang = reqLang(c)
	}
	// The phone is optional here: a verified customer gets promotions checked
	// against their own usage; an anonymous one is checked again when ordering.
	phone, _ := s.customerPhone(c)
	site, ok := s.siteFor(c, c.Param("slug"))
	if !ok {
		return
	}
	in.OwnSite, in.BrandID = site.OwnSite(), 0 // the brand is the one the slug names (CreateQuote)
	q, err := s.App.CreateQuote(c, c.Param("slug"), in, phone)
	if err != nil {
		fail(c, hideZones(err))
		return
	}
	if q.ZoneDistanceM != nil || q.Input.Lat != nil || q.Input.AddressID != nil {
		// decided by the pin: the customer is not told which area it was
		q.ZoneID, q.ZoneName, q.ZoneDistanceM = nil, "", nil
	}
	c.JSON(200, q)
}

// hideZones drops what an error says about the store's delivery areas.
func hideZones(err error) error {
	var e *app.Error
	if errors.As(err, &e) && e.Code == "OUT_OF_ZONE" && e.Details != nil {
		return app.Err(e.Status, e.Code, e.Message)
	}
	return err
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
	site, ok := s.siteFor(c, c.Param("slug"))
	if !ok {
		return
	}
	in.OwnSite = site.OwnSite()
	out, err := s.App.PlaceOrder(c, c.Param("slug"), phone, c.GetHeader("Idempotency-Key"), in)
	if err != nil {
		fail(c, hideZones(err))
		return
	}
	c.JSON(200, out)
}

// customerView removes store-internal data from an order.
func customerView(o *app.Order) *app.Order {
	o.Cash = nil
	// which delivery area an address fell in is the store's business
	o.ZoneName, o.ZoneDistanceM = "", nil
	o.StaffNote, o.PlacedBy = "", ""
	o.AdjReason, o.AdjBy, o.AdjApprovedBy, o.PayCheck, o.PayCheckNote = "", "", "", "", ""
	// who in the store added a batch, and its kitchen tags, stay in the store
	for i := range o.Batches {
		o.Batches[i].By, o.Batches[i].RoundTags = "", []string{}
	}
	for i := range o.Items {
		o.Items[i].BatchBy, o.Items[i].RoundTags = "", []string{}
		o.Items[i].StartedBy, o.Items[i].ReadyBy = "", ""
	}
	// who voided or refunded, and the kitchen's side of it, too
	for i := range o.Voids {
		v := &o.Voids[i]
		v.By, v.ApprovedBy, v.AckBy, v.Wasted, v.WasStatus, v.NeedsAck, v.AckAt, v.StationID = "", "", "", false, "", false, nil, nil
	}
	for i := range o.Refunds {
		o.Refunds[i].By, o.Refunds[i].ApprovedBy = "", ""
	}
	o.VoidOpen, o.RefundOpen = false, false
	return o
}

// customerPage answers with an order as its customer's page shows it.
func (s *Server) customerPage(c *gin.Context, o *app.Order, err error) {
	if err == nil {
		err = s.App.CustomerOrderView(c, o)
	}
	if err == nil {
		err = s.App.LocalizeOrderStore(c, o, reqLang(c))
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, customerView(o))
}

func (s *Server) customerOrder(c *gin.Context) {
	o, err := s.App.OrderByToken(c, c.Param("token"))
	s.customerPage(c, o, err)
}

func (s *Server) customerCancel(c *gin.Context) {
	o, err := s.App.CustomerCancel(c, c.Param("token"))
	s.customerPage(c, o, err)
}

// customerOrders lists the logged-in customer's orders.
func (s *Server) customerOrders(c *gin.Context) {
	cu, err := s.customer(c)
	if err != nil {
		fail(c, err)
		return
	}
	before, _ := strconv.ParseInt(c.Query("before"), 10, 64)
	site, ok := s.site(c)
	if !ok {
		return
	}
	var store, brand int64
	if site.OwnSite() {
		store, brand = site.Store.ID, site.Store.BrandID // the brand's own page shows its orders only
	}
	page, err := s.App.CustomerOrdersAt(c, cu.ID, store, brand, c.Query("tab"), c.Query("q"), max(before, 0))
	if err == nil {
		err = s.App.LocalizeOrderCards(c, page, reqLang(c))
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, page)
}

func (s *Server) customerReorder(c *gin.Context) {
	r, err := s.App.Reorder(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, r)
}

func (s *Server) customerAftersale(c *gin.Context) {
	var in app.AftersaleInput
	if !bind(c, &in) {
		return
	}
	o, err := s.App.RequestAftersale(c, c.Param("token"), in)
	s.customerPage(c, o, err)
}

func (s *Server) handleAftersale(c *gin.Context) {
	id, ok := s.orderScope(c, pAftersale)
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
		Reply  string `json:"reply"`
	}
	if !bind(c, &in) {
		return
	}
	o, err := s.App.HandleAftersale(c, id, in.Status, in.Reply, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, o)
}

func (s *Server) storeReviews(c *gin.Context) {
	offset, _ := strconv.Atoi(c.Query("offset"))
	page, err := s.App.StoreReviews(c, c.Param("slug"), c.Query("filter"), offset)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, page)
}

func (s *Server) reportStore(c *gin.Context) {
	var in app.ReportInput
	if !bind(c, &in) {
		return
	}
	if err := s.App.ReportStore(c, c.Param("slug"), in); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) orderReview(c *gin.Context) {
	r, err := s.App.OrderReview(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, r)
}

func (s *Server) submitReview(c *gin.Context) {
	var in app.ReviewInput
	if !bind(c, &in) {
		return
	}
	r, err := s.App.SubmitReview(c, c.Param("token"), in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, r)
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

// login signs in to a store's back office ("office", the default) or the
// staff workbench ("work"). The platform signs in at adminLogin.
func (s *Server) login(c *gin.Context) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Entry    string `json:"entry"`
	}
	if !bind(c, &in) {
		return
	}
	if in.Entry == "" {
		in.Entry = app.EntryOffice
	}
	if in.Entry != app.EntryOffice && in.Entry != app.EntryWork {
		fail(c, app.ErrValidation("entry must be office or work"))
		return
	}
	token, u, err := s.App.LoginTo(c, in.Username, in.Password, in.Entry)
	if err != nil {
		fail(c, err)
		return
	}
	s.setCookie(c, staffCookie, token, int(app.SessionTTL.Seconds()))
	c.JSON(200, u)
}

// adminLogin is the platform's own sign-in, kept in its own cookie.
func (s *Server) adminLogin(c *gin.Context) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !bind(c, &in) {
		return
	}
	token, u, err := s.App.LoginTo(c, in.Username, in.Password, app.EntryAdmin)
	if err != nil {
		fail(c, err)
		return
	}
	s.setCookie(c, adminCookie, token, int(app.SessionTTL.Seconds()))
	c.JSON(200, u)
}

func (s *Server) adminLogout(c *gin.Context) {
	if token, _ := c.Cookie(adminCookie); token != "" {
		_ = s.App.Logout(c, token)
	}
	s.setCookie(c, adminCookie, "", -1)
	c.JSON(200, gin.H{"ok": true})
}

// switchEntry moves the sign-in between the back office and the workbench.
func (s *Server) switchEntry(c *gin.Context) {
	var in struct {
		Entry    string `json:"entry"`
		Password string `json:"password"`
	}
	if !bind(c, &in) {
		return
	}
	token, _ := sessionFor(c)
	u, err := s.App.SwitchEntry(c, token, in.Entry, in.Password)
	if err != nil {
		fail(c, err)
		return
	}
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
	bid, ok := branchScope(c, pOrderView)
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
	bid, ok := branchScope(c, pAny)
	if !ok {
		return
	}
	s.Hub.serve(c, &subscription{branch: bid})
}

func (s *Server) listRiders(c *gin.Context) {
	bid, ok := branchScope(c, pRiders)
	if !ok {
		return
	}
	riders, store, err := s.App.RiderBoard(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"riders": riders, "store": store})
}

// getOrder is the whole order, contact details and money included: for the
// desk's people, not for everyone with a role in the store. A station, the
// packing counter and a rider have their own, smaller, views
// (docs/CODE_REVIEW_FIX_PLAN.md H02).
func (s *Server) getOrder(c *gin.Context) {
	id, ok := s.orderScope(c, pOrderRead)
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
	c.JSON(200, forKitchen(c, o.BranchID, o))
}

// kitchenView leaves on an order what a station and the packing counter
// work with: the dishes, the notes, the table or the number, who carries
// it. Who the customer is, where they live and what was paid are taken off.
func kitchenView(o *app.Order) *app.Order {
	o.CustomerPhone, o.ContactPhone, o.ContactTitle = "", "", ""
	o.ContactWechat, o.ContactTelegram, o.ContactLine, o.ContactWhatsapp = "", "", "", ""
	o.AddressLine, o.AddressUnit, o.AddressNote, o.AddressPhoto = "", "", "", false
	o.DestLat, o.DestLng, o.ZoneDistanceM = nil, nil, nil
	o.Cash, o.Aftersale, o.MemberNo = nil, nil, nil
	o.Totals = app.Totals{}
	o.AmountReceived, o.PaymentNote, o.PayMethod, o.PayType, o.PayChannelID = nil, "", "", "", 0
	o.CreditPaid, o.StoredPrincipalPaid, o.StoredBonusPaid, o.Due = 0, 0, 0, 0
	o.PayCheck, o.PayCheckNote, o.AdjReason, o.AdjBy, o.AdjApprovedBy, o.AdjAt = "", "", "", "", "", nil
	o.Discounts, o.Refunds = nil, nil
	o.Platform = nil
	return o
}

// forKitchen answers an action a station or the packing counter may take
// with the kitchen's view of the order, unless the person may read orders
// at the desk anyway.
func forKitchen(c *gin.Context, bid int64, o *app.Order) *app.Order {
	if o == nil || permError(c, bid, pOrderRead) == nil {
		return o
	}
	return kitchenView(o)
}

func (s *Server) acceptOrder(c *gin.Context) {
	s.orderCmd(c, pHandle, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.Accept(c, id, in.ExpectedVersion, staffActor(c))
	})
}

func (s *Server) rejectOrder(c *gin.Context) {
	s.orderCmd(c, pHandle, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.Reject(c, id, in.ExpectedVersion, in.Reason, staffActor(c))
	})
}

func (s *Server) cancelOrder(c *gin.Context) {
	s.orderCmd(c, pCancel, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.StaffCancel(c, id, in.ExpectedVersion, in.Reason, staffActor(c))
	})
}

func (s *Server) assignRider(c *gin.Context) {
	s.orderCmd(c, pHandle, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.AssignRider(c, id, in.RiderID, staffActor(c))
	})
}

func (s *Server) handoffOrder(c *gin.Context) {
	s.orderCmd(c, pHandoff, func(id int64, _ orderCommand) (*app.Order, error) {
		return s.App.StaffHandoff(c, id, staffActor(c))
	})
}

// ---- merchant configuration ----

func (s *Server) adminMenu(c *gin.Context) {
	bid, ok := branchScope(c, pAny)
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
	bid, ok := branchScope(c, pMenuEdit)
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
	bid, ok := branchScope(c, pMenuEdit)
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
	bid, ok := branchScope(c, pSoldOut)
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
	if (in.Published != nil || in.Archived != nil) && !currentUser(c).Can(bid, app.PermMenuEdit) {
		fail(c, app.Err(http.StatusForbidden, "FORBIDDEN", "only people who may edit the menu can publish or remove items"))
		return
	}
	if err := s.App.PatchItem(c, bid, id, in); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) saveZone(c *gin.Context) {
	bid, ok := branchScope(c, pMap)
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

// saveZones changes several zones as one (see app.ZoneBatch).
func (s *Server) saveZones(c *gin.Context) {
	bid, ok := branchScope(c, pMap)
	if !ok {
		return
	}
	var in app.ZoneBatch
	if !bind(c, &in) {
		return
	}
	ids, err := s.App.SaveZones(c, bid, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ids": ids})
}

func (s *Server) deleteZone(c *gin.Context) {
	bid, ok := branchScope(c, pMap)
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

// reorderMenu stores the order of the categories and dishes dragged on the menu page.
func (s *Server) reorderMenu(c *gin.Context) {
	bid, ok := branchScope(c, pMenuEdit)
	if !ok {
		return
	}
	var in struct {
		Categories []app.MenuOrderCategory `json:"categories"`
	}
	if !bind(c, &in) {
		return
	}
	if err := s.App.ReorderMenu(c, bid, in.Categories); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) reorderZones(c *gin.Context) {
	bid, ok := branchScope(c, pMap)
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
	bid, ok := branchScope(c, pMap)
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
	bid, ok := branchScope(c, pMap)
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
	mapsKey, pushKey, appPush := s.App.PublicConfig(s.Cfg)
	c.JSON(200, gin.H{"maps_key": mapsKey, "push_key": pushKey, "app_push": appPush, "chat": s.App.ChatOn()})
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
	terms, err := s.App.DeliveryTermsAt(c, c.Param("slug"), *in.Lat, *in.Lng)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, terms)
}

// ---- the customer's address book ----

func (s *Server) listAddresses(c *gin.Context) {
	cu, err := s.customer(c)
	if err != nil {
		fail(c, err)
		return
	}
	list, err := s.App.ListAddresses(c, cu.ID, c.Query("store"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"addresses": list, "limit": app.MaxAddresses})
}

func (s *Server) saveAddress(c *gin.Context) {
	cu, err := s.customer(c)
	if err != nil {
		fail(c, err)
		return
	}
	var id int64
	if p := c.Param("id"); p != "" {
		if id, err = strconv.ParseInt(p, 10, 64); err != nil || id <= 0 {
			fail(c, app.Err(http.StatusNotFound, "ADDRESS_NOT_FOUND", "this address no longer exists"))
			return
		}
	}
	var in app.AddressInput
	if !bind(c, &in) {
		return
	}
	ad, err := s.App.SaveAddress(c, cu.ID, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, ad)
}

func (s *Server) deleteAddress(c *gin.Context) {
	cu, err := s.customer(c)
	if err != nil {
		fail(c, err)
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, app.Err(http.StatusNotFound, "ADDRESS_NOT_FOUND", "this address no longer exists"))
		return
	}
	if err := s.App.DeleteAddress(c, cu.ID, id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) saveSettings(c *gin.Context) {
	bid, ok := branchScope(c, pSettings)
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

func (s *Server) saveOrderNumbering(c *gin.Context) {
	bid, ok := branchScope(c, pSettings)
	if !ok {
		return
	}
	var in app.OrderNumbering
	if !bind(c, &in) {
		return
	}
	b, err := s.App.SaveOrderNumbering(c, bid, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, b)
}

func (s *Server) listStaff(c *gin.Context) {
	bid, ok := branchScope(c, pStaffView)
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
	bid, ok := branchScope(c, pStaffManage)
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

// staffScope resolves the branch and the staff member's id.
func staffScope(c *gin.Context, need []string) (bid, id int64, ok bool) {
	if bid, ok = branchScope(c, need); !ok {
		return
	}
	id, ok = idParam(c, "id")
	return
}

func (s *Server) patchStaff(c *gin.Context) {
	bid, id, ok := staffScope(c, pStaffManage)
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
	bid, id, ok := staffScope(c, pStaffManage)
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
	bid, id, ok := staffScope(c, pStaffManage)
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
	bid, ok := branchScope(c, pCash)
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
	if !allowed(c, bid, pCash) {
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
	bid, ok := branchScope(c, pKitchen)
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
		kitchenView(o)
	}
	// The timers turn yellow and red by the store's cooking target.
	target := 15
	if b, err := s.App.BranchByID(c, bid); err == nil {
		target = b.CookTargetMinutes
	}
	c.JSON(200, gin.H{"orders": orders, "target_minutes": target})
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
	if !allowed(c, bid, pKitchen) {
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
	c.JSON(200, forKitchen(c, bid, o))
}

func (s *Server) packOrder(c *gin.Context) {
	id, ok := s.orderScope(c, pKitchen)
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
	c.JSON(200, forKitchen(c, o.BranchID, o))
}

// ---- rider ----

// riderView gives the rider what the delivery needs and hides the customer's
// contact details once the task is over.
func riderView(o *app.Order) *app.Order {
	o.Cash = nil
	if o.Delivery != nil && (o.Delivery.Status == "delivered" || o.Delivery.Status == "failed") {
		o.CustomerPhone, o.AddressUnit, o.AddressNote = "", "", ""
		o.ContactPhone, o.ContactWechat, o.ContactTelegram, o.AddressPhoto = "", "", "", false
		o.ContactLine, o.ContactWhatsapp = "", ""
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
	if c.Param("action") == "proof" {
		s.riderProof(c, taskID)
		return
	}
	var in struct {
		CollectedAmount int64  `json:"collected_amount"`
		Reason          string `json:"reason"`
		// Pay is how the customer paid at the door, nil for cash.
		Pay *app.PayInput `json:"pay"`
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
		o, err = s.App.RiderDeliverPaid(c, taskID, riderID, in.CollectedAmount, in.Pay)
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
	s.orderCmd(c, pHandle, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.Redeliver(c, id, in.RiderID, staffActor(c))
	})
}

func (s *Server) closeFailedOrder(c *gin.Context) {
	s.orderCmd(c, pHandle, func(id int64, in orderCommand) (*app.Order, error) {
		return s.App.CloseFailed(c, id, in.Reason, staffActor(c))
	})
}

// ---- kitchen stations ----

func (s *Server) listStations(c *gin.Context) {
	bid, ok := branchScope(c, pAny)
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
	bid, ok := branchScope(c, pSettings)
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
	bid, ok := branchScope(c, pSettings)
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
	bid, ok := branchScope(c, pPromo)
	if !ok {
		return
	}
	ps, err := s.App.ListPromotions(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	// someone who works on some brands sees those brands' promotions
	mine := ps[:0]
	for _, p := range ps {
		if s.seesBrands(c, bid, p.BrandIDs) {
			mine = append(mine, p)
		}
	}
	c.JSON(200, gin.H{"promotions": mine})
}

func (s *Server) savePromotion(c *gin.Context) {
	bid, ok := branchScope(c, pPromo)
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
	// the brands it was for and the brands it will be for
	if !s.promotionMine(c, bid, id) || !s.mayUseBrands(c, bid, in.BrandIDs) {
		return
	}
	id, err := s.App.SavePromotion(c, bid, id, in, staffActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

// promotionMine checks that the person works on every brand of an existing
// promotion (id 0: a new one, nothing to check).
func (s *Server) promotionMine(c *gin.Context, bid, id int64) bool {
	if id == 0 || currentUser(c).AllBrands(bid) {
		return true
	}
	brands, err := s.App.PromotionBrands(c, bid, id)
	if err != nil {
		fail(c, err)
		return false
	}
	return s.mayUseBrands(c, bid, brands)
}

func (s *Server) setPromotionStatus(c *gin.Context) {
	bid, ok := branchScope(c, pPromo)
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
	if !s.promotionMine(c, bid, id) {
		return
	}
	if err := s.App.SetPromotionStatus(c, bid, id, in.Status, staffActor(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) trialPromotions(c *gin.Context) {
	bid, ok := branchScope(c, pPromo)
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
	bid, ok := branchScope(c, pMenuEdit)
	if !ok {
		return
	}
	sendMenuTemplate(c, s.App, bid)
}

// sendMenuTemplate sends the store's template; ?with=menu fills in every
// current dish so the file can be edited and imported back.
func sendMenuTemplate(c *gin.Context, a *app.App, bid int64) {
	withMenu := c.Query("with") == "menu"
	b, err := a.MenuTemplate(c, bid, withMenu)
	if err != nil {
		fail(c, err)
		return
	}
	name := "menu-template.xlsx"
	if withMenu {
		name = "menu-" + time.Now().Format("20060102") + ".xlsx"
	}
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b)
}

// readImportFile reads the .xlsx request body, capped before it is read.
func readImportFile(c *gin.Context) ([]byte, bool) {
	const limit = 2 << 20
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit+1))
	if err != nil || len(body) > limit {
		fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the file is larger than 2 MB"))
		return nil, false
	}
	return body, true
}

// uploadMenuImport takes the .xlsx as the raw request body; the file name
// comes in the query string. The body is capped before it is read.
func (s *Server) uploadMenuImport(c *gin.Context) {
	bid, ok := branchScope(c, pMenuEdit)
	if !ok {
		return
	}
	body, ok := readImportFile(c)
	if !ok {
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
	bid, ok := branchScope(c, pMenuEdit)
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
	bid, ok := branchScope(c, pMenuEdit)
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
	bid, ok := branchScope(c, pMenuEdit)
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
	if err == nil {
		err = s.App.SetDeviceLang(c, true, d.ID, reqLang(c))
	}
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

func (s *Server) riderSeen(c *gin.Context) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !bind(c, &in) {
		return
	}
	if err := s.App.MarkSeen(c, currentUser(c).ID, in.IDs); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) customerTracking(c *gin.Context) {
	t, err := s.App.OrderTracking(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, t)
}

// riderProof takes the photo as the raw request body, capped before it is read.
func (s *Server) riderProof(c *gin.Context, taskID int64) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, app.ProofMaxBytes+1))
	if err != nil || len(body) > app.ProofMaxBytes {
		fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the photo is larger than 600 KB"))
		return
	}
	o, err := s.App.SaveProof(c, taskID, currentUser(c).ID, body)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, riderView(o))
}

func sendPhoto(c *gin.Context, image []byte, err error) {
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "image/jpeg", image)
}

func (s *Server) riderProofPhoto(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	image, err := s.App.RiderProof(c, id, currentUser(c).ID)
	sendPhoto(c, image, err)
}

// ---- photo of the customer's door ----

func (s *Server) addressID(c *gin.Context) (cu *app.Customer, id int64, ok bool) {
	cu, err := s.customer(c)
	if err == nil {
		id, err = strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			err = app.Err(http.StatusNotFound, "ADDRESS_NOT_FOUND", "this address no longer exists")
		}
	}
	if err != nil {
		fail(c, err)
		return nil, 0, false
	}
	return cu, id, true
}

// saveAddressPhoto takes the photo as the raw request body, capped before it is read.
func (s *Server) saveAddressPhoto(c *gin.Context) {
	cu, id, ok := s.addressID(c)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, app.ProofMaxBytes+1))
	if err != nil || len(body) > app.ProofMaxBytes {
		fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the photo is larger than 600 KB"))
		return
	}
	ad, err := s.App.SaveAddressPhoto(c, cu.ID, id, body)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, ad)
}

func (s *Server) deleteAddressPhoto(c *gin.Context) {
	cu, id, ok := s.addressID(c)
	if !ok {
		return
	}
	if err := s.App.DeleteAddressPhoto(c, cu.ID, id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Server) addressPhoto(c *gin.Context) {
	cu, id, ok := s.addressID(c)
	if !ok {
		return
	}
	image, err := s.App.AddressPhoto(c, cu.ID, id)
	sendPhoto(c, image, err)
}

func (s *Server) orderAddressPhoto(c *gin.Context) {
	id, ok := s.orderScope(c, pOrderView)
	if !ok {
		return
	}
	image, err := s.App.OrderAddressPhoto(c, id)
	sendPhoto(c, image, err)
}

func (s *Server) riderAddressPhoto(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	image, err := s.App.RiderAddressPhoto(c, id, currentUser(c).ID)
	sendPhoto(c, image, err)
}

func (s *Server) orderProofPhoto(c *gin.Context) {
	id, ok := s.orderScope(c, pOrderView)
	if !ok {
		return
	}
	image, err := s.App.OrderProof(c, id)
	sendPhoto(c, image, err)
}
