package httpapi

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// The customer's own pages ("me") and the platform's side of them.

// me is the logged-in customer; the request has failed when ok is false.
func (s *Server) me(c *gin.Context) (cu *app.Customer, ok bool) {
	cu, err := s.customer(c)
	if err != nil {
		fail(c, err)
		return nil, false
	}
	return cu, true
}

// reply writes the result of an app call.
func reply(c *gin.Context, v any, err error) {
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, v)
}

func offsetOf(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("offset"))
	return n
}

func (s *Server) meRoutes(api *gin.RouterGroup) {
	w := s.limit(s.publicWrites)
	api.GET("/platform/info", s.platformInfo)
	api.GET("/customer/me", s.myProfile)
	api.PUT("/customer/me", w, s.saveMyProfile)
	api.GET("/customer/avatar", s.myAvatar)
	api.POST("/customer/avatar", w, s.saveMyAvatar)
	api.DELETE("/customer/avatar", w, s.deleteMyAvatar)
	api.GET("/customer/stores", s.myStores)
	api.GET("/customer/favorites/:slug", s.myFavorite)
	api.PUT("/customer/favorites/:slug", w, s.setMyFavorite)
	api.DELETE("/customer/favorites/:slug", w, s.setMyFavorite)
	api.GET("/customer/offers", s.myOffers)
	api.GET("/customer/coupons", s.myCoupons)
	api.GET("/customer/reviews", s.myReviews)
	api.GET("/customer/messages", s.myMessages)
	api.POST("/customer/messages/read", w, s.readMyMessages)
	api.GET("/customer/feedback", s.myFeedback)
	api.POST("/customer/feedback", w, s.submitFeedback)
	api.GET("/customer/applications", s.myApplications)
	api.POST("/customer/applications", w, s.submitApplication)
	api.GET("/customer/wallet/:account", s.myWallet)
}

func (s *Server) meAdminRoutes(admin *gin.RouterGroup) {
	admin.GET("/feedback", s.adminFeedback)
	admin.POST("/feedback/:id/reply", s.adminReplyFeedback)
	admin.GET("/applications", s.adminApplications)
	admin.POST("/applications/:id/decide", s.adminDecideApplication)
	admin.GET("/customers", s.adminCustomers)
	admin.POST("/customers/:id/wallet", s.adminAdjustWallet)
	admin.GET("/platform-info", s.platformInfo)
	admin.PUT("/platform-info", s.adminSavePlatformInfo)
}

func (s *Server) platformInfo(c *gin.Context) {
	info, err := s.App.PlatformInfo(c)
	reply(c, info, err)
}

func (s *Server) myProfile(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		p, err := s.App.CustomerProfile(c, cu.ID)
		reply(c, p, err)
	}
}

func (s *Server) saveMyProfile(c *gin.Context) {
	cu, ok := s.me(c)
	var in struct {
		Nickname string `json:"nickname"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	p, err := s.App.SaveNickname(c, cu.ID, in.Nickname)
	reply(c, p, err)
}

func (s *Server) myAvatar(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		image, err := s.App.Avatar(c, cu.ID)
		sendPhoto(c, image, err)
	}
}

// saveMyAvatar takes the picture as the raw request body, capped before it is read.
func (s *Server) saveMyAvatar(c *gin.Context) {
	cu, ok := s.me(c)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, app.ProofMaxBytes+1))
	if err != nil || len(body) > app.ProofMaxBytes {
		fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the picture is larger than 600 KB"))
		return
	}
	p, err := s.App.SaveAvatar(c, cu.ID, body)
	reply(c, p, err)
}

func (s *Server) deleteMyAvatar(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		p, err := s.App.SaveAvatar(c, cu.ID, nil)
		reply(c, p, err)
	}
}

func (s *Server) myStores(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		list, err := s.App.MyStores(c, cu.ID, c.Query("favorites") == "1")
		reply(c, gin.H{"stores": list}, err)
	}
}

// myFavorite answers for anyone: someone who is not logged in keeps no store.
func (s *Server) myFavorite(c *gin.Context) {
	cu, err := s.customer(c)
	if err != nil {
		c.JSON(200, gin.H{"favorite": false})
		return
	}
	on, err := s.App.IsFavorite(c, cu.ID, c.Param("slug"))
	reply(c, gin.H{"favorite": on}, err)
}

func (s *Server) setMyFavorite(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		on := c.Request.Method == http.MethodPut
		reply(c, gin.H{"favorite": on}, s.App.SetFavorite(c, cu.ID, c.Param("slug"), on))
	}
}

func (s *Server) myOffers(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		list, err := s.App.MyOffers(c, cu.ID, c.Query("store"))
		reply(c, gin.H{"offers": list}, err)
	}
}

func (s *Server) myCoupons(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		list, err := s.App.MyCoupons(c, cu.ID)
		reply(c, gin.H{"coupons": list}, err)
	}
}

func (s *Server) myReviews(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		list, err := s.App.MyReviews(c, cu.ID)
		reply(c, gin.H{"reviews": list}, err)
	}
}

func (s *Server) myMessages(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		list, more, err := s.App.MyMessages(c, cu.ID, offsetOf(c))
		reply(c, gin.H{"messages": list, "more": more}, err)
	}
}

func (s *Server) readMyMessages(c *gin.Context) {
	cu, ok := s.me(c)
	var in struct {
		ID int64 `json:"id"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.ReadMessages(c, cu.ID, in.ID))
}

func (s *Server) myFeedback(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		list, err := s.App.MyFeedback(c, cu.ID)
		reply(c, gin.H{"feedback": list}, err)
	}
}

func (s *Server) submitFeedback(c *gin.Context) {
	cu, ok := s.me(c)
	var in app.FeedbackInput
	if !ok || !bind(c, &in) {
		return
	}
	f, err := s.App.SubmitFeedback(c, cu.ID, in)
	reply(c, f, err)
}

func (s *Server) myApplications(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		list, err := s.App.MyApplications(c, cu.ID)
		reply(c, gin.H{"applications": list}, err)
	}
}

func (s *Server) submitApplication(c *gin.Context) {
	cu, ok := s.me(c)
	var in app.ApplicationInput
	if !ok || !bind(c, &in) {
		return
	}
	p, err := s.App.SubmitApplication(c, cu.ID, in)
	reply(c, p, err)
}

func (s *Server) myWallet(c *gin.Context) {
	if cu, ok := s.me(c); ok {
		list, err := s.App.WalletEntries(c, cu.ID, c.Param("account"))
		reply(c, gin.H{"entries": list}, err)
	}
}

// ---- platform admin ----

func (s *Server) adminFeedback(c *gin.Context) {
	l, err := s.App.AdminFeedback(c, c.Query("status"), queryPage(c))
	reply(c, l, err)
}

func (s *Server) adminReplyFeedback(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in struct {
		Reply string `json:"reply"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.AdminReplyFeedback(c, adminActor(c), id, in.Reply))
}

func (s *Server) adminApplications(c *gin.Context) {
	l, err := s.App.AdminApplications(c, c.Query("status"), queryPage(c))
	reply(c, l, err)
}

func (s *Server) adminDecideApplication(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in struct {
		Status string `json:"status"`
		Reply  string `json:"reply"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.AdminDecideApplication(c, adminActor(c), id, in.Status, in.Reply))
}

func (s *Server) adminCustomers(c *gin.Context) {
	l, err := s.App.AdminCustomers(c, c.Query("q"), queryPage(c))
	reply(c, l, err)
}

func (s *Server) adminAdjustWallet(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in app.WalletAdjust
	if !ok || !bind(c, &in) {
		return
	}
	w, err := s.App.AdminAdjustWallet(c, adminActor(c), id, in)
	reply(c, gin.H{"wallets": w}, err)
}

func (s *Server) adminSavePlatformInfo(c *gin.Context) {
	var in map[string]string
	if !bind(c, &in) {
		return
	}
	info, err := s.App.AdminSavePlatformInfo(c, adminActor(c), in)
	reply(c, info, err)
}
