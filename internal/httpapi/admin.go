package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

// Platform back office. Every route here sits behind requireUser and requireAdmin.

func adminActor(c *gin.Context) app.AdminActor {
	return app.AdminActor{ID: currentUser(c).ID, IP: c.ClientIP()}
}

func queryInt(c *gin.Context, name string) int64 {
	n, _ := strconv.ParseInt(c.Query(name), 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

func queryPage(c *gin.Context) app.Page {
	return app.Page{Page: int(queryInt(c, "page")), PageSize: int(queryInt(c, "page_size"))}
}

// queryTime reads an RFC 3339 time or a plain date; a missing or malformed value is ignored.
func queryTime(c *gin.Context, name string) *time.Time {
	v := c.Query(name)
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return &t
		}
	}
	return nil
}

func ok(c *gin.Context, err error) {
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) adminRoutes(admin *gin.RouterGroup) {
	admin.GET("/merchants", s.listMerchants)
	admin.POST("/merchants", s.createMerchant)
	admin.GET("/merchants/:id", s.getMerchant)
	admin.PUT("/merchants/:id", s.updateMerchant)
	admin.POST("/merchants/:id/:action", s.merchantAction) // suspend, resume, restore
	admin.DELETE("/merchants/:id", s.deleteMerchant)
	admin.POST("/merchants/:id/branches", s.adminCreateBranch)

	admin.GET("/branches", s.adminListBranches)
	admin.GET("/branches/:bid", s.adminGetBranch)
	admin.PUT("/branches/:bid", s.adminUpdateBranch)
	admin.DELETE("/branches/:bid", s.adminDeleteBranch)
	admin.POST("/branches/:bid/users", s.adminCreateUser)
	admin.PATCH("/users/:id", s.adminPatchUser)
	admin.POST("/users/:id/reset-password", s.adminResetPassword)

	admin.GET("/items", s.adminSearchItems)
	admin.GET("/branches/:bid/menu", s.adminBranchMenu)
	admin.POST("/branches/:bid/categories", s.adminSaveCategory)
	admin.PUT("/branches/:bid/categories/:id", s.adminSaveCategory)
	admin.DELETE("/branches/:bid/categories/:id", s.adminDeleteCategory)
	admin.POST("/branches/:bid/items", s.adminSaveItem)
	admin.POST("/branches/:bid/items/bulk", s.adminBulkItems)
	admin.PUT("/branches/:bid/items/:id", s.adminSaveItem)
	admin.PATCH("/branches/:bid/items/:id", s.adminPatchItem)

	admin.GET("/audit", s.adminAudit)
}

// ---- merchants ----

func (s *Server) listMerchants(c *gin.Context) {
	m, err := s.App.ListMerchants(c, app.MerchantFilter{Q: c.Query("q"), Status: c.Query("status"), Page: queryPage(c)})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

func (s *Server) createMerchant(c *gin.Context) {
	var in app.NewMerchant
	if !bind(c, &in) {
		return
	}
	m, err := s.App.AdminCreateMerchant(c, adminActor(c), in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

func (s *Server) getMerchant(c *gin.Context) {
	id, found := idParam(c, "id")
	if !found {
		return
	}
	m, err := s.App.GetMerchant(c, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

func (s *Server) updateMerchant(c *gin.Context) {
	id, found := idParam(c, "id")
	if !found {
		return
	}
	var in app.MerchantInput
	if !bind(c, &in) {
		return
	}
	ok(c, s.App.UpdateMerchant(c, adminActor(c), id, in))
}

func (s *Server) merchantAction(c *gin.Context) {
	id, found := idParam(c, "id")
	if !found {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if c.Request.ContentLength != 0 && !bind(c, &in) {
		return
	}
	switch c.Param("action") {
	case "suspend":
		ok(c, s.App.SuspendMerchant(c, adminActor(c), id, in.Reason))
	case "resume":
		ok(c, s.App.ResumeMerchant(c, adminActor(c), id))
	case "restore":
		ok(c, s.App.RestoreMerchant(c, adminActor(c), id))
	default:
		fail(c, app.ErrNotFound)
	}
}

func (s *Server) deleteMerchant(c *gin.Context) {
	id, found := idParam(c, "id")
	if !found {
		return
	}
	var in struct {
		ConfirmName string `json:"confirm_name"`
	}
	if !bind(c, &in) {
		return
	}
	ok(c, s.App.DeleteMerchant(c, adminActor(c), id, in.ConfirmName))
}

// ---- branches and accounts ----

func (s *Server) adminListBranches(c *gin.Context) {
	b, err := s.App.ListBranchOptions(c)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"branches": b})
}

func (s *Server) adminGetBranch(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	b, err := s.App.AdminBranchByID(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, b)
}

func (s *Server) adminCreateBranch(c *gin.Context) {
	id, found := idParam(c, "id")
	if !found {
		return
	}
	var in app.BranchInput
	if !bind(c, &in) {
		return
	}
	bid, err := s.App.CreateBranch(c, adminActor(c), id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": bid})
}

func (s *Server) adminUpdateBranch(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	var in app.BranchInput
	if !bind(c, &in) {
		return
	}
	ok(c, s.App.UpdateBranch(c, adminActor(c), bid, in))
}

func (s *Server) adminDeleteBranch(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	ok(c, s.App.DeleteBranch(c, adminActor(c), bid))
}

func (s *Server) adminCreateUser(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	var in app.NewUser
	if !bind(c, &in) {
		return
	}
	id, err := s.App.AdminCreateUser(c, adminActor(c), bid, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

func (s *Server) adminPatchUser(c *gin.Context) {
	id, found := idParam(c, "id")
	if !found {
		return
	}
	var in struct {
		Active *bool `json:"active"`
	}
	if !bind(c, &in) {
		return
	}
	if in.Active == nil {
		fail(c, app.ErrValidation("active is required"))
		return
	}
	ok(c, s.App.AdminSetUserActive(c, adminActor(c), id, *in.Active))
}

func (s *Server) adminResetPassword(c *gin.Context) {
	id, found := idParam(c, "id")
	if !found {
		return
	}
	pw, err := s.App.AdminResetPassword(c, adminActor(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"password": pw})
}

// ---- menu ----

func (s *Server) adminSearchItems(c *gin.Context) {
	l, err := s.App.SearchItems(c, app.ItemFilter{Q: c.Query("q"), MerchantID: queryInt(c, "merchant_id"),
		BranchID: queryInt(c, "branch_id"), Status: c.Query("status"), Page: queryPage(c)})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, l)
}

func (s *Server) adminBranchMenu(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	if _, err := s.App.AdminBranchByID(c, bid); err != nil {
		fail(c, err)
		return
	}
	m, err := s.App.AdminMenu(c, bid)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, m)
}

func (s *Server) adminSaveCategory(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	id, found := optionalID(c)
	if !found {
		return
	}
	var in app.CategoryInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.AdminSaveCategory(c, adminActor(c), bid, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

func (s *Server) adminDeleteCategory(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	id, found := idParam(c, "id")
	if !found {
		return
	}
	ok(c, s.App.AdminArchiveCategory(c, adminActor(c), bid, id))
}

func (s *Server) adminSaveItem(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	id, found := optionalID(c)
	if !found {
		return
	}
	var in app.ItemInput
	if !bind(c, &in) {
		return
	}
	id, err := s.App.AdminSaveItem(c, adminActor(c), bid, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id})
}

func (s *Server) adminPatchItem(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	id, found := idParam(c, "id")
	if !found {
		return
	}
	var in app.ItemPatch
	if !bind(c, &in) {
		return
	}
	ok(c, s.App.AdminPatchItem(c, adminActor(c), bid, id, in))
}

func (s *Server) adminBulkItems(c *gin.Context) {
	bid, found := idParam(c, "bid")
	if !found {
		return
	}
	var in struct {
		IDs    []int64 `json:"ids"`
		Action string  `json:"action"`
	}
	if !bind(c, &in) {
		return
	}
	n, err := s.App.AdminBulkItems(c, adminActor(c), bid, in.IDs, in.Action)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"changed": n})
}

// ---- audit trail ----

func (s *Server) adminAudit(c *gin.Context) {
	l, err := s.App.ListAdminAudit(c, app.AuditFilter{AdminID: queryInt(c, "admin_id"), Action: c.Query("action"),
		From: queryTime(c, "from"), To: queryTime(c, "to"), Page: queryPage(c)})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, l)
}
