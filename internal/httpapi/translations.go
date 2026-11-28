package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// reqLang is the language a customer reads the site in: ?lang=, then the
// lang cookie the page sets, then the browser's languages.
func reqLang(c *gin.Context) string {
	if l := c.Query("lang"); app.IsLang(l) {
		return l
	}
	if l, err := c.Cookie("lang"); err == nil && app.IsLang(l) {
		return l
	}
	if l := app.LangFromHeader(c.GetHeader("Accept-Language")); l != "" {
		return l
	}
	return "en"
}

// menuChanged queues a translation of the store's menu after a request that
// changed it went through.
func (s *Server) menuChanged(c *gin.Context) {
	c.Next()
	if c.Request.Method == http.MethodGet || c.Writer.Status() >= 300 {
		return
	}
	if bid, ok := idValue(c.Param("bid")); ok {
		_ = s.App.QueueTranslate(c, bid, false)
	}
}

// platformChanged queues translating the mall's categories and banners after
// the platform changes them.
func (s *Server) platformChanged(c *gin.Context) {
	c.Next()
	if c.Writer.Status() < 300 {
		_ = s.App.QueueTranslatePlatform(c)
	}
}

func (s *Server) translationRoutes(auth, admin *gin.RouterGroup) {
	// the merchant's own store
	auth.GET("/merchant/branches/:bid/translations", s.withBranch(pTranslate, s.getTranslations))
	auth.PUT("/merchant/branches/:bid/translations", s.withBranch(pTranslate, s.saveTranslation))
	auth.PUT("/merchant/branches/:bid/menu-langs", s.withBranch(pTranslate, s.saveMenuLangs))
	auth.POST("/merchant/branches/:bid/translations/retranslate", s.withBranch(pTranslate, s.retranslate))
	// the platform, for any store
	admin.GET("/branches/:bid/translations", s.withAnyBranch(s.getTranslations))
	admin.PUT("/branches/:bid/translations", s.withAnyBranch(s.saveTranslation))
	admin.PUT("/branches/:bid/menu-langs", s.withAnyBranch(s.saveMenuLangs))
	admin.POST("/branches/:bid/translations/retranslate", s.withAnyBranch(s.retranslate))
}

func (s *Server) withBranch(roles []string, h func(*gin.Context, int64)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if bid, ok := branchScope(c, roles); ok {
			h(c, bid)
		}
	}
}

func (s *Server) withAnyBranch(h func(*gin.Context, int64)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if bid, ok := idParam(c, "bid"); ok {
			h(c, bid)
		}
	}
}

func (s *Server) getTranslations(c *gin.Context, bid int64) {
	// ?scope=item/12 is one record's language window, "none" only the settings
	v, err := s.App.MenuTranslations(c, bid, c.Query("scope"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, v)
}

func (s *Server) saveTranslation(c *gin.Context, bid int64) {
	// one translation, or everything changed in a language window ("edits")
	var in struct {
		app.TranslationEdit
		Edits []app.TranslationEdit `json:"edits"`
	}
	if !bind(c, &in) {
		return
	}
	if in.Edits == nil {
		in.Edits = []app.TranslationEdit{in.TranslationEdit}
	}
	if err := s.App.SaveTranslations(c, bid, in.Edits, staffActor(c)); err != nil {
		fail(c, err)
		return
	}
	s.getTranslations(c, bid)
}

func (s *Server) saveMenuLangs(c *gin.Context, bid int64) {
	var in struct {
		MenuLang     string   `json:"menu_lang"`
		MenuLangsOff []string `json:"menu_langs_off"`
	}
	if !bind(c, &in) {
		return
	}
	if err := s.App.SaveMenuLangs(c, bid, in.MenuLang, in.MenuLangsOff); err != nil {
		fail(c, err)
		return
	}
	s.getTranslations(c, bid)
}

func (s *Server) retranslate(c *gin.Context, bid int64) {
	if err := s.App.Retranslate(c, bid); err != nil {
		fail(c, err)
		return
	}
	s.getTranslations(c, bid)
}

func idValue(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}
