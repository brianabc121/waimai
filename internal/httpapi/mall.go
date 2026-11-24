package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// The marketplace: what customers see, and how the merchant and the platform set it up.

func (s *Server) mallRoutes(api *gin.RouterGroup) {
	api.GET("/mall/home", s.mallHome)
	api.GET("/mall/stores", s.limit(s.mallLimit), s.mallStores)
	api.GET("/mall/regions", s.mallRegions)
	api.GET("/mall/banners/:token", s.mallBannerImage)
}

func (s *Server) mallHome(c *gin.Context) {
	h, err := s.App.MallHome(c)
	if err == nil {
		err = s.App.LocalizeMallHome(c, h, reqLang(c))
	}
	reply(c, h, err)
}

func (s *Server) mallStores(c *gin.Context) {
	q := app.MallQuery{CountryCode: c.Query("country"), City: c.Query("city"), District: c.Query("district"),
		Category: c.Query("category"), Q: c.Query("q"), Sort: c.Query("sort"), Lang: reqLang(c)}
	if len(q.Q) > 60 {
		q.Q = q.Q[:60]
	}
	if f := c.Query("filter"); f != "" {
		q.Filters = strings.Split(f, ",")
	}
	if c.Query("lat") != "" || c.Query("lng") != "" {
		lat, err1 := strconv.ParseFloat(c.Query("lat"), 64)
		lng, err2 := strconv.ParseFloat(c.Query("lng"), 64)
		if err1 != nil || err2 != nil {
			fail(c, app.ErrValidation("invalid coordinates"))
			return
		}
		q.At = &app.LatLng{lat, lng}
	}
	// Someone logged in also gets the stores they use; anyone else just the list.
	if cu, err := s.customer(c); err == nil {
		q.CustomerID = cu.ID
	}
	l, err := s.App.MallStores(c, q)
	reply(c, l, err)
}

func (s *Server) mallRegions(c *gin.Context) {
	l, err := s.App.MallRegions(c)
	reply(c, gin.H{"countries": l}, err)
}

func (s *Server) mallBannerImage(c *gin.Context) {
	image, err := s.App.MallBannerImage(c, c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(200, "image/jpeg", image)
}

// ---- merchant ----

func (s *Server) mallListing(c *gin.Context) {
	if bid, ok := branchScope(c, pMall); ok {
		l, err := s.App.MallListing(c, bid)
		reply(c, l, err)
	}
}

func (s *Server) saveMallListing(c *gin.Context) {
	bid, ok := branchScope(c, pMall)
	var in app.MallListingInput
	if !ok || !bind(c, &in) {
		return
	}
	l, err := s.App.SaveMallListing(c, bid, in)
	reply(c, l, err)
}

// ---- platform ----

func (s *Server) mallAdminRoutes(admin *gin.RouterGroup) {
	admin.GET("/mall", s.adminMall)
	admin.PUT("/mall/stores/:id", s.adminHideStore)
	admin.PUT("/mall/categories/:id", s.platformChanged, s.adminSaveMallCategory)
	admin.POST("/mall/banners", s.platformChanged, s.adminSaveBanner)
	admin.PUT("/mall/banners/:id", s.platformChanged, s.adminSaveBanner)
	admin.DELETE("/mall/banners/:id", s.platformChanged, s.adminDeleteBanner)
	admin.POST("/mall/banners/:id/image", s.adminBannerImage)
	admin.DELETE("/mall/banners/:id/image", s.adminBannerImage)
}

func (s *Server) adminMall(c *gin.Context) {
	m, err := s.App.AdminMall(c)
	reply(c, m, err)
}

func (s *Server) adminHideStore(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in struct {
		Hidden bool `json:"hidden"`
	}
	if !ok || !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.AdminHideStore(c, adminActor(c), id, in.Hidden))
}

func (s *Server) adminSaveMallCategory(c *gin.Context) {
	id, ok := idParam(c, "id")
	var in app.MallCategoryInput
	if !ok || !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.AdminSaveMallCategory(c, adminActor(c), id, in))
}

func (s *Server) adminSaveBanner(c *gin.Context) {
	var id int64
	if c.Param("id") != "" {
		var ok bool
		if id, ok = idParam(c, "id"); !ok {
			return
		}
	}
	var in app.MallBannerInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.AdminSaveBanner(c, adminActor(c), id, in)
	reply(c, gin.H{"id": id}, err)
}

func (s *Server) adminDeleteBanner(c *gin.Context) {
	if id, ok := idParam(c, "id"); ok {
		reply(c, gin.H{"ok": true}, s.App.AdminDeleteBanner(c, adminActor(c), id))
	}
}

// adminBannerImage takes the picture as the raw request body; DELETE removes it.
func (s *Server) adminBannerImage(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var image []byte
	if c.Request.Method == http.MethodPost {
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, app.ImageMaxBytes+1))
		if err != nil || len(body) > app.ImageMaxBytes {
			fail(c, app.Err(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "the picture is larger than 1 MB"))
			return
		}
		image = body
	}
	reply(c, gin.H{"ok": true}, s.App.AdminSetBannerImage(c, adminActor(c), id, image))
}
