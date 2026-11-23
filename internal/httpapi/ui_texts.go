package httpapi

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// The platform's language page (docs/I18N_DESIGN.md §8).

func (s *Server) uiTextRoutes(api, admin *gin.RouterGroup) {
	// every browser loads these on top of the texts that ship with the app
	api.GET("/i18n/:lang", s.uiFixes)
	admin.GET("/i18n/:lang", s.adminUIFixes)
	admin.PUT("/i18n/:lang", s.saveUITexts)
	admin.POST("/i18n/:lang/fill", s.fillUITexts)
	admin.POST("/i18n/:lang/export", s.exportUITexts)
	admin.POST("/i18n/:lang/import", s.importUITexts)
}

func (s *Server) uiFixes(c *gin.Context) {
	f, err := s.App.UIFixes(c, c.Param("lang"))
	if err == nil {
		c.Header("Cache-Control", "public, max-age=60")
	}
	reply(c, f, err)
}

func (s *Server) adminUIFixes(c *gin.Context) {
	l, err := s.App.AdminUIFixes(c, c.Param("lang"))
	reply(c, gin.H{"fixes": l}, err)
}

func (s *Server) saveUITexts(c *gin.Context) {
	var in struct {
		Edits []app.UIEdit `json:"edits"`
	}
	if !bind(c, &in) {
		return
	}
	reply(c, gin.H{"ok": true}, s.App.SaveUITexts(c, adminActor(c), c.Param("lang"), in.Edits))
}

func (s *Server) fillUITexts(c *gin.Context) {
	var in struct {
		Texts []app.UISource `json:"texts"`
	}
	if !bind(c, &in) {
		return
	}
	m, err := s.App.FillUITexts(c, adminActor(c), c.Param("lang"), in.Texts)
	reply(c, gin.H{"texts": m}, err)
}

func (s *Server) exportUITexts(c *gin.Context) {
	var in struct {
		Rows []app.UIRow `json:"rows"`
	}
	if !bind(c, &in) {
		return
	}
	lang := c.Param("lang")
	b, err := app.UITextsSheet(lang, in.Rows)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="texts-`+lang+`-`+time.Now().Format("20060102")+`.xlsx"`)
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", b)
}

// importUITexts reads a spreadsheet back; the page shows what changed before saving.
func (s *Server) importUITexts(c *gin.Context) {
	body, ok := readImportFile(c)
	if !ok {
		return
	}
	e, err := app.ReadUITextsSheet(c.Param("lang"), body)
	reply(c, gin.H{"edits": e}, err)
}
