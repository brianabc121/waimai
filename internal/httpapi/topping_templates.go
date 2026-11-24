package httpapi

import (
	"github.com/brianabc121/waimai/internal/app"

	"github.com/gin-gonic/gin"
)

// Topping templates: extras with a price that many dishes share. Reading
// needs any back-office role, changing needs menu editing.
func (s *Server) toppingTemplateRoutes(auth *gin.RouterGroup) {
	base := "/merchant/branches/:bid"
	auth.GET(base+"/topping-templates", s.toppingTemplates)
	auth.POST(base+"/topping-templates", s.menuChanged, s.saveToppingTemplate)
	auth.PUT(base+"/topping-templates/:id", s.menuChanged, s.saveToppingTemplate)
	auth.DELETE(base+"/topping-templates/:id", s.menuChanged, s.deleteToppingTemplate)
	auth.PUT(base+"/topping-template-order", s.reorderToppingTemplates)
	auth.PUT(base+"/topping-templates/:id/items", s.menuChanged, s.toppingTemplateItems)
}

func (s *Server) toppingTemplates(c *gin.Context) {
	bid, ok := branchScope(c, pAny)
	if !ok {
		return
	}
	l, err := s.App.ToppingTemplates(c, bid)
	reply(c, gin.H{"templates": l}, err)
}

func (s *Server) saveToppingTemplate(c *gin.Context) {
	bid, ok := branchScope(c, pMenuEdit)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.ToppingTemplateInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.SaveToppingTemplate(c, bid, id, in)
	reply(c, gin.H{"id": id}, err)
}

func (s *Server) deleteToppingTemplate(c *gin.Context) {
	bid, ok := branchScope(c, pMenuEdit)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	ok2(c, s.App.DeleteToppingTemplate(c, bid, id))
}

func (s *Server) reorderToppingTemplates(c *gin.Context) {
	bid, ok := branchScope(c, pMenuEdit)
	if !ok {
		return
	}
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !bind(c, &in) {
		return
	}
	ok2(c, s.App.ReorderToppingTemplates(c, bid, in.IDs))
}

func (s *Server) toppingTemplateItems(c *gin.Context) {
	bid, ok := branchScope(c, pMenuEdit)
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in struct {
		ItemIDs []int64 `json:"item_ids"`
	}
	if !bind(c, &in) {
		return
	}
	ok2(c, s.App.SetToppingTemplateItems(c, bid, id, in.ItemIDs))
}
