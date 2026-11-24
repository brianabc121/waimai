package httpapi

import (
	"github.com/brianabc121/waimai/internal/app"

	"github.com/gin-gonic/gin"
)

// Flavour note templates (docs/NOTE_TEMPLATE_DESIGN.md): the store's own
// routes and the platform's, on the same handlers.

func (s *Server) noteTemplateRoutes(auth, admin *gin.RouterGroup) {
	for _, r := range []struct {
		g     *gin.RouterGroup
		base  string
		admin bool
	}{{auth, "/merchant/branches/:bid", false}, {admin, "/branches/:bid", true}} {
		h := noteHandlers{s, r.admin}
		r.g.GET(r.base+"/note-templates", h.list)
		r.g.GET(r.base+"/note-templates/starters", h.starters)
		r.g.POST(r.base+"/note-templates/starters", s.menuChanged, h.addStarters)
		r.g.POST(r.base+"/note-templates", s.menuChanged, h.save)
		r.g.PUT(r.base+"/note-templates/:id", s.menuChanged, h.save)
		r.g.DELETE(r.base+"/note-templates/:id", s.menuChanged, h.remove)
		r.g.PUT(r.base+"/note-template-order", h.reorder)
		r.g.PUT(r.base+"/note-templates/:id/items", s.menuChanged, h.items)
	}
}

type noteHandlers struct {
	s     *Server
	admin bool
}

// scope reads the store and checks who may do this: on the store's side,
// reading needs any back-office role and changing needs menu editing.
func (h noteHandlers) scope(c *gin.Context, write bool, action string) (int64, bool) {
	if !h.admin {
		perms := pAny
		if write {
			perms = pMenuEdit
		}
		return branchScope(c, perms)
	}
	bid, ok := idParam(c, "bid")
	if !ok {
		return 0, false
	}
	var tid int64
	if c.Param("id") != "" {
		if tid, ok = idParam(c, "id"); !ok {
			return 0, false
		}
	}
	if err := h.s.App.AdminNoteTemplateChange(c, adminActor(c), bid, tid, action); err != nil {
		fail(c, err)
		return 0, false
	}
	return bid, true
}

func (h noteHandlers) list(c *gin.Context) {
	bid, ok := h.scope(c, false, "")
	if !ok {
		return
	}
	l, err := h.s.App.NoteTemplates(c, bid)
	reply(c, gin.H{"templates": l}, err)
}

func (h noteHandlers) starters(c *gin.Context) {
	bid, ok := h.scope(c, false, "")
	if !ok {
		return
	}
	l, err := h.s.App.StarterNoteTemplates(c, bid)
	reply(c, gin.H{"starters": l}, err)
}

func (h noteHandlers) addStarters(c *gin.Context) {
	bid, ok := h.scope(c, true, "note_template.starters")
	if !ok {
		return
	}
	var in struct {
		Keys []string `json:"keys"`
	}
	if !bind(c, &in) {
		return
	}
	ids, err := h.s.App.AddStarterNoteTemplates(c, bid, in.Keys)
	reply(c, gin.H{"ids": ids}, err)
}

func (h noteHandlers) save(c *gin.Context) {
	action := "note_template.create"
	if c.Param("id") != "" {
		action = "note_template.update"
	}
	bid, ok := h.scope(c, true, action)
	if !ok {
		return
	}
	id, ok := optionalID(c)
	if !ok {
		return
	}
	var in app.NoteTemplateInput
	if !bind(c, &in) {
		return
	}
	id, err := h.s.App.SaveNoteTemplate(c, bid, id, in)
	reply(c, gin.H{"id": id}, err)
}

func (h noteHandlers) remove(c *gin.Context) {
	bid, ok := h.scope(c, true, "note_template.delete")
	if !ok {
		return
	}
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	ok2(c, h.s.App.DeleteNoteTemplate(c, bid, id))
}

func (h noteHandlers) reorder(c *gin.Context) {
	bid, ok := h.scope(c, true, "")
	if !ok {
		return
	}
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !bind(c, &in) {
		return
	}
	ok2(c, h.s.App.ReorderNoteTemplates(c, bid, in.IDs))
}

func (h noteHandlers) items(c *gin.Context) {
	bid, ok := h.scope(c, true, "note_template.items")
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
	ok2(c, h.s.App.SetNoteTemplateItems(c, bid, id, in.ItemIDs))
}

// ok2 is ok() for handlers whose own variable is named ok.
func ok2(c *gin.Context, err error) { ok(c, err) }
