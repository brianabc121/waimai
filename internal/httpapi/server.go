// Package httpapi is the HTTP layer: authentication, binding, error mapping
// and SSE. Business rules live in internal/app.
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/config"
)

const (
	staffCookie    = "sid"
	customerCookie = "cs"
)

type Server struct {
	App *app.App
	Cfg config.Config
	Hub *Hub
	// Metrics, when set, counts every API request for the back office.
	Metrics *app.Metrics

	publicWrites *limiter // quotes, orders
	reportLimit  *limiter // reports against a store
	mallLimit    *limiter // marketplace listings
	otpLimit     *limiter
	loginLimit   *limiter
	// Guests at the tables usually share the restaurant's one public address,
	// so the dine-in limit is per address but much wider.
	tableLimit *limiter
}

func NewServer(a *app.App, cfg config.Config, hub *Hub) *Server {
	return &Server{App: a, Cfg: cfg, Hub: hub,
		publicWrites: newLimiter(60, 30),
		reportLimit:  newLimiter(3, 3),
		mallLimit:    newLimiter(120, 60),
		// Everyone who orders logs in, and many customers can share one
		// address; the daily caps in RequestOTP are what limit the cost.
		otpLimit:   newLimiter(20, 20),
		loginLimit: newLimiter(10, 10),
		tableLimit: newLimiter(600, 200),
	}
}

// ---- helpers ----

func fail(c *gin.Context, err error) {
	rid := c.GetString("request_id")
	var ae *app.Error
	if errors.As(err, &ae) {
		c.Set(errorCodeKey, ae.Code)
		c.AbortWithStatusJSON(ae.Status, gin.H{"error": gin.H{
			"code": ae.Code, "message": ae.Message, "details": ae.Details, "request_id": rid}})
		return
	}
	log.Printf("request %s %s %s failed: %v", rid, c.Request.Method, c.FullPath(), err)
	c.Set(errorCodeKey, "INTERNAL")
	c.Set(errorMessageKey, err.Error())
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{
		"code": "INTERNAL", "message": "internal error", "request_id": rid}})
}

func bind(c *gin.Context, v any) bool {
	// No JSON command needs more than this; larger bodies are refused unread.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := c.ShouldBindJSON(v); err != nil {
		fail(c, app.Err(http.StatusUnprocessableEntity, "VALIDATION", "invalid JSON body"))
		return false
	}
	return true
}

func idParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		fail(c, app.ErrNotFound)
		return 0, false
	}
	return id, true
}

func (s *Server) setCookie(c *gin.Context, name, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
}

func currentUser(c *gin.Context) *app.User {
	u, _ := c.Get("user")
	user, _ := u.(*app.User)
	return user
}

func staffActor(c *gin.Context) app.Actor {
	a := app.Actor{Type: "staff", ID: currentUser(c).ID}
	// The workbench and the kitchen screen say which of them is asking.
	if via := c.Query("via"); via == "work" || via == "board" {
		a.Via = via
	}
	return a
}

var (
	deskRoles    = []string{"owner", "manager", "cashier"}
	managerRoles = []string{"owner", "manager"}
	kitchenRoles = []string{"owner", "manager", "cashier", "kitchen"}
	allStaff     = []string{"owner", "manager", "cashier", "kitchen"}
)

// allowed checks the user's role in a branch. A branch the user has no role
// in is reported as not found, so that ids of other merchants leak nothing.
func allowed(c *gin.Context, branchID int64, roles []string) bool {
	u := currentUser(c)
	role := u.RoleIn(branchID)
	if role == "" && u.IsPlatformAdmin {
		// The platform can look at any store's back office but not act in it:
		// accepting orders or reconciling cash stays with the merchant.
		if c.Request.Method == http.MethodGet {
			return true
		}
		fail(c, app.Err(http.StatusForbidden, "READ_ONLY", "the platform view of a store is read-only"))
		return false
	}
	if role == "" {
		fail(c, app.ErrNotFound)
		return false
	}
	// "kitchen" in a role list stands for anyone who works a station or the
	// packing counter, whatever their back-office role is.
	inStore := u.InStore(branchID)
	for _, r := range roles {
		if r == role || (r == "kitchen" && inStore) {
			return true
		}
	}
	fail(c, app.Err(http.StatusForbidden, "FORBIDDEN", "your role cannot do this"))
	return false
}

// branchScope reads :bid and checks the role.
func branchScope(c *gin.Context, roles []string) (int64, bool) {
	bid, ok := idParam(c, "bid")
	if !ok {
		return 0, false
	}
	return bid, allowed(c, bid, roles)
}

// orderScope reads :id, resolves the order's branch and checks the role.
func (s *Server) orderScope(c *gin.Context, roles []string) (int64, bool) {
	id, ok := idParam(c, "id")
	if !ok {
		return 0, false
	}
	bid, err := s.App.OrderBranch(c, id)
	if err != nil {
		fail(c, err)
		return 0, false
	}
	return id, allowed(c, bid, roles)
}

// ---- middleware ----

func requestID(c *gin.Context) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	c.Set("request_id", id)
	c.Header("X-Request-Id", id)
	c.Next()
}

// csrf requires a custom header on every state-changing API call. Browsers do
// not let cross-site forms set it, and the API serves no CORS headers.
func csrf(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		if c.GetHeader("X-Requested-With") == "" {
			fail(c, app.Err(http.StatusForbidden, "CSRF", "missing X-Requested-With header"))
			return
		}
	}
	c.Next()
}

func (s *Server) limit(l *limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(c.ClientIP()) {
			fail(c, app.Err(http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, slow down"))
			return
		}
		c.Next()
	}
}

// sessionToken reads the staff session: the app sends it in the Authorization
// header, a browser in its cookie.
func sessionToken(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	token, _ := c.Cookie(staffCookie)
	return token
}

func (s *Server) requireUser(c *gin.Context) {
	token := sessionToken(c)
	if token == "" {
		fail(c, app.Err(http.StatusUnauthorized, "UNAUTHENTICATED", "please sign in"))
		return
	}
	u, err := s.App.UserBySession(c, token)
	if err != nil {
		fail(c, app.Err(http.StatusUnauthorized, "UNAUTHENTICATED", "please sign in"))
		return
	}
	c.Set("user", u)
	c.Next()
}

func requireAdmin(c *gin.Context) {
	if !currentUser(c).IsPlatformAdmin {
		fail(c, app.Err(http.StatusForbidden, "FORBIDDEN", "platform admin only"))
		return
	}
	c.Next()
}

// ---- routes ----

func (s *Server) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.ContextWithFallback = true // lets handlers pass *gin.Context as the request context
	r.Use(gin.Recovery(), requestID, s.metrics)
	_ = r.SetTrustedProxies([]string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})

	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.GET("/readyz", func(c *gin.Context) {
		if err := s.App.DB.Ping(c); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	api := r.Group("/api/v1", csrf, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})

	// customer
	api.GET("/config", s.publicConfig)
	api.GET("/stores/:slug", s.storefront)
	api.POST("/stores/:slug/locate", s.limit(s.publicWrites), s.locate)
	api.POST("/otp/request", s.limit(s.otpLimit), s.otpRequest)
	api.POST("/otp/verify", s.limit(s.otpLimit), s.otpVerify)
	api.GET("/customer/session", s.customerSession)
	api.POST("/customer/logout", s.customerLogout)
	api.PUT("/customer/lang", s.limit(s.publicWrites), s.saveCustomerLang)
	api.GET("/customer/addresses", s.listAddresses)
	api.POST("/customer/addresses", s.limit(s.publicWrites), s.saveAddress)
	api.PUT("/customer/addresses/:id", s.limit(s.publicWrites), s.saveAddress)
	api.DELETE("/customer/addresses/:id", s.limit(s.publicWrites), s.deleteAddress)
	api.GET("/customer/addresses/:id/photo", s.addressPhoto)
	api.POST("/customer/addresses/:id/photo", s.limit(s.publicWrites), s.saveAddressPhoto)
	api.DELETE("/customer/addresses/:id/photo", s.limit(s.publicWrites), s.deleteAddressPhoto)
	api.POST("/stores/:slug/quotes", s.limit(s.publicWrites), s.createQuote)
	api.POST("/stores/:slug/orders", s.limit(s.publicWrites), s.placeOrder)
	api.GET("/customer/orders", s.customerOrders)
	api.GET("/customer/orders/:token", s.customerOrder)
	api.GET("/customer/orders/:token/reorder", s.customerReorder)
	api.POST("/customer/orders/:token/aftersale", s.limit(s.publicWrites), s.customerAftersale)
	api.POST("/customer/orders/:token/cancel", s.limit(s.publicWrites), s.customerCancel)
	api.GET("/customer/orders/:token/events", s.customerEvents)
	api.GET("/customer/orders/:token/tracking", s.customerTracking)
	api.GET("/images/:token", s.image)
	api.GET("/media/:token", s.mediaFile)
	api.GET("/media/:token/poster", s.mediaPoster)
	api.GET("/stores/:slug/reviews", s.storeReviews)
	api.GET("/stores/:slug/coupons", s.storeCoupons)
	api.POST("/stores/:slug/coupons/:id/claim", s.limit(s.publicWrites), s.claimCoupon)
	api.POST("/stores/:slug/reports", s.limit(s.reportLimit), s.reportStore)
	api.GET("/customer/orders/:token/review", s.orderReview)
	api.POST("/customer/orders/:token/review", s.limit(s.publicWrites), s.submitReview)
	s.meRoutes(api)
	s.chatRoutes(r, api)
	s.mallRoutes(api)

	// dine-in guests: scanned table code or paired tablet
	api.POST("/table-sessions/exchange", s.limit(s.tableLimit), s.exchangeTableCode)
	api.POST("/table-devices/pair", s.limit(s.loginLimit), s.pairDevice)
	api.GET("/table-sessions/current", s.currentTable)
	api.POST("/table-sessions/current/batches", s.limit(s.tableLimit), s.guestBatch)
	api.GET("/table-sessions/current/events", s.tableEvents)

	// auth
	api.POST("/auth/login", s.limit(s.loginLimit), s.login)
	api.POST("/auth/token", s.limit(s.loginLimit), s.tokenLogin)
	api.POST("/auth/logout", s.logout)

	auth := api.Group("", s.requireUser)
	auth.GET("/auth/me", func(c *gin.Context) { c.JSON(200, currentUser(c)) })

	// merchant order desk
	auth.GET("/merchant/branches/:bid/orders", s.listOrders)
	auth.GET("/merchant/branches/:bid/events", s.branchEvents)
	auth.GET("/merchant/branches/:bid/riders", s.listRiders)
	auth.GET("/merchant/orders/:id", s.getOrder)
	auth.GET("/merchant/orders/:id/proof", s.orderProofPhoto)
	auth.GET("/merchant/orders/:id/address-photo", s.orderAddressPhoto)
	auth.POST("/merchant/orders/:id/accept", s.acceptOrder)
	auth.POST("/merchant/orders/:id/reject", s.rejectOrder)
	auth.POST("/merchant/orders/:id/cancel", s.cancelOrder)
	auth.POST("/merchant/orders/:id/aftersale", s.handleAftersale)
	auth.POST("/merchant/orders/:id/assign-rider", s.assignRider)
	auth.POST("/merchant/orders/:id/handoff", s.handoffOrder)
	auth.POST("/merchant/orders/:id/redeliver", s.redeliverOrder)
	auth.POST("/merchant/orders/:id/close-failed", s.closeFailedOrder)

	// dine-in: tables, sessions, bills
	auth.GET("/merchant/branches/:bid/tables", s.listTables)
	auth.POST("/merchant/branches/:bid/tables", s.saveTable)
	auth.PUT("/merchant/branches/:bid/tables/:id", s.saveTable)
	auth.POST("/merchant/branches/:bid/table-devices/:id/revoke", s.revokeTableDevice)
	auth.POST("/merchant/tables/:id/open", s.openTable)
	auth.POST("/merchant/tables/:id/devices", s.pairTableDevice)
	auth.GET("/merchant/table-sessions/:id", s.getBill)
	auth.POST("/merchant/table-sessions/:id/batches", s.staffBatch)
	auth.POST("/merchant/table-sessions/:id/close", s.closeTable)
	auth.POST("/merchant/bill-items/:id/:action", s.billItem)

	// merchant configuration
	auth.GET("/merchant/branches/:bid/menu", s.adminMenu)
	auth.POST("/merchant/branches/:bid/categories", s.menuChanged, s.saveCategory)
	auth.PUT("/merchant/branches/:bid/categories/:id", s.menuChanged, s.saveCategory)
	auth.POST("/merchant/branches/:bid/items", s.menuChanged, s.saveItem)
	auth.PUT("/merchant/branches/:bid/items/:id", s.menuChanged, s.saveItem)
	auth.PATCH("/merchant/branches/:bid/items/:id", s.menuChanged, s.patchItem)
	auth.POST("/merchant/branches/:bid/zones", s.saveZone)
	auth.PUT("/merchant/branches/:bid/zones/:id", s.saveZone)
	auth.DELETE("/merchant/branches/:bid/zones/:id", s.deleteZone)
	auth.PUT("/merchant/branches/:bid/zone-order", s.reorderZones)
	auth.PUT("/merchant/branches/:bid/zone-batch", s.saveZones)
	auth.PUT("/merchant/branches/:bid/location", s.saveLocation)
	auth.PUT("/merchant/branches/:bid/zone-mode", s.saveZoneMode)
	auth.PUT("/merchant/branches/:bid/settings", s.menuChanged, s.saveSettings)
	auth.GET("/merchant/branches/:bid/mall", s.mallListing)
	auth.PUT("/merchant/branches/:bid/mall", s.saveMallListing)
	auth.POST("/merchant/branches/:bid/images", s.uploadImage)
	auth.GET("/merchant/branches/:bid/media", s.listMedia)
	auth.POST("/merchant/branches/:bid/media", s.uploadMedia)
	auth.PATCH("/merchant/branches/:bid/media/:id", s.updateMedia)
	auth.DELETE("/merchant/branches/:bid/media/:id", s.deleteMedia)
	auth.POST("/merchant/branches/:bid/media/:id/poster", s.uploadMediaPoster)
	auth.PUT("/merchant/branches/:bid/media-order", s.reorderMedia)
	auth.GET("/merchant/branches/:bid/templates", s.listTemplates)
	auth.PUT("/merchant/branches/:bid/templates/:channel", s.saveTemplateDraft)
	auth.POST("/merchant/branches/:bid/templates/:channel/:action", s.templateAction)
	auth.GET("/merchant/branches/:bid/stations", s.listStations)
	auth.POST("/merchant/branches/:bid/stations", s.saveStation)
	auth.PUT("/merchant/branches/:bid/stations/:id", s.saveStation)
	auth.POST("/merchant/branches/:bid/station-assignments", s.assignStation)
	auth.GET("/merchant/branches/:bid/menu-imports", s.listMenuImports)
	auth.POST("/merchant/branches/:bid/menu-imports", s.uploadMenuImport)
	auth.GET("/merchant/branches/:bid/menu-imports/template", s.menuImportTemplate)
	auth.GET("/merchant/branches/:bid/menu-imports/:id", s.getMenuImport)
	auth.POST("/merchant/branches/:bid/menu-imports/:id/apply", s.menuChanged, s.applyMenuImport)
	auth.GET("/merchant/branches/:bid/promotions", s.listPromotions)
	auth.POST("/merchant/branches/:bid/promotions", s.menuChanged, s.savePromotion)
	auth.PUT("/merchant/branches/:bid/promotions/:id", s.menuChanged, s.savePromotion)
	auth.POST("/merchant/branches/:bid/promotions/:id/status", s.menuChanged, s.setPromotionStatus)
	auth.GET("/merchant/branches/:bid/coupons/repurchase", s.repurchaseRule)
	auth.PUT("/merchant/branches/:bid/coupons/repurchase", s.saveRepurchaseRule)
	auth.GET("/merchant/branches/:bid/coupons/batches", s.listStoreBatches)
	auth.POST("/merchant/branches/:bid/coupons/batches", s.saveStoreBatch)
	auth.PUT("/merchant/branches/:bid/coupons/batches/:id", s.saveStoreBatch)
	auth.POST("/merchant/branches/:bid/coupons/batches/:id/status", s.setStoreBatchStatus)
	auth.POST("/merchant/branches/:bid/promotion-trial", s.trialPromotions)
	auth.GET("/merchant/branches/:bid/staff", s.listStaff)
	auth.POST("/merchant/branches/:bid/staff", s.createStaff)
	auth.PATCH("/merchant/branches/:bid/staff/:id", s.patchStaff)
	auth.DELETE("/merchant/branches/:bid/staff/:id", s.deleteStaff)
	auth.POST("/merchant/branches/:bid/staff/:id/password", s.resetStaffPassword)
	auth.GET("/merchant/branches/:bid/attendance", s.attendance)
	auth.POST("/merchant/branches/:bid/attendance", s.addShift)
	auth.PATCH("/merchant/branches/:bid/attendance/:id", s.editShift)
	auth.GET("/merchant/branches/:bid/reports/kitchen", s.kitchenReport)
	auth.GET("/merchant/branches/:bid/reports/kitchen/:id", s.kitchenDetail)
	auth.GET("/merchant/branches/:bid/reports/riders", s.riderReport)
	auth.GET("/merchant/branches/:bid/reports/riders/:id", s.riderDetail)
	auth.GET("/merchant/branches/:bid/trips/:id", s.tripDetail)
	auth.GET("/merchant/branches/:bid/cash-collections", s.listCash)
	auth.POST("/merchant/cash-collections/:id/reconcile", s.reconcile)

	// kitchen and packing
	auth.GET("/kitchen/branches/:bid/board", s.kitchenBoard)
	auth.POST("/kitchen/items/:id/:action", s.itemAction)
	auth.POST("/kitchen/orders/:id/pack", s.packOrder)

	// clocking on and off, for everyone who works in a store
	auth.GET("/work/shift", s.shiftState)
	auth.POST("/work/shift", s.setShift)
	auth.GET("/work/attendance", s.myAttendance)

	// rider
	auth.GET("/rider/tasks", s.riderTasks)
	auth.GET("/rider/events", s.riderEvents)
	auth.POST("/rider/tasks/:id/:action", s.riderAction)
	auth.GET("/rider/tasks/:id/proof", s.riderProofPhoto)
	auth.GET("/rider/tasks/:id/address-photo", s.riderAddressPhoto)
	auth.POST("/rider/seen", s.riderSeen)
	auth.GET("/rider/duty", s.riderDuty)
	auth.POST("/rider/duty", s.setRiderDuty)
	auth.POST("/rider/locations", s.riderLocations)
	auth.POST("/rider/devices", s.registerDevice)
	auth.DELETE("/rider/devices/:id", s.removeDevice)

	// platform admin
	admin := auth.Group("/admin", requireAdmin)
	s.adminRoutes(admin)
	s.meAdminRoutes(admin)
	s.mallAdminRoutes(admin)
	s.couponAdminRoutes(admin)
	s.billRoutes(auth, admin)
	s.translationRoutes(auth, admin)
	s.uiTextRoutes(api, admin)
	s.chatAdminRoutes(auth, admin)

	s.static(r)
	return r
}

// static serves the built Vue app with history-mode fallback when STATIC_DIR
// is set (single-binary demos). In production Nginx serves the files.
func (s *Server) static(r *gin.Engine) {
	dir := s.Cfg.StaticDir
	r.NoRoute(func(c *gin.Context) {
		if dir == "" || strings.HasPrefix(c.Request.URL.Path, "/api/") {
			fail(c, app.ErrNotFound)
			return
		}
		path := filepath.Clean("/" + c.Request.URL.Path)
		clean := filepath.Join(dir, path)
		if st, err := os.Stat(clean); err == nil && !st.IsDir() {
			if strings.HasPrefix(path, "/assets/") {
				// built files carry a hash in their name and never change
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				c.Header("Cache-Control", "no-cache")
			}
			c.File(clean)
			return
		}
		// A built file that is gone (an older build's name) must answer 404,
		// not the page: the browser would otherwise try to run HTML as a
		// script, and the page could not tell that it needs a reload.
		if strings.HasPrefix(path, "/assets/") {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.File(filepath.Join(dir, "index.html"))
	})
}
