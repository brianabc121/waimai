package httpapi

import (
	"github.com/brianabc121/waimai/internal/app"
	"github.com/gin-gonic/gin"
)

// The platform's address book page (docs/ADDRESS_BOOK_DESIGN.md).
func (s *Server) addressBookRoutes(admin *gin.RouterGroup) {
	admin.GET("/address-regions", s.adminAddressRegions)
	admin.GET("/addresses", s.adminListAddresses)
	admin.POST("/addresses", s.adminSaveAddress)
	admin.PUT("/addresses/:id", s.adminSaveAddress)
	admin.DELETE("/addresses/:id", s.adminDeleteAddress)
}

func (s *Server) adminAddressRegions(c *gin.Context) {
	rs, err := s.App.BookRegions(c)
	reply(c, gin.H{"regions": rs}, err)
}

// queryOpt is a query parameter that may be present and empty ("" is the
// unknown region), or absent (not filtered on).
func queryOpt(c *gin.Context, name string) *string {
	if v, ok := c.GetQuery(name); ok {
		return &v
	}
	return nil
}

func (s *Server) adminListAddresses(c *gin.Context) {
	l, err := s.App.ListBook(c, app.BookFilter{CountryCode: queryOpt(c, "country_code"), Province: queryOpt(c, "province"),
		City: queryOpt(c, "city"), Q: c.Query("q"), Source: c.Query("source"), Sort: c.Query("sort"), Page: queryPage(c)})
	reply(c, l, err)
}

func (s *Server) adminSaveAddress(c *gin.Context) {
	var id int64
	if c.Param("id") != "" {
		var ok bool
		if id, ok = idParam(c, "id"); !ok {
			return
		}
	}
	var in app.BookInput
	if !bind(c, &in) {
		return
	}
	x, err := s.App.SaveBookAddress(c, adminActor(c), id, in)
	reply(c, x, err)
}

func (s *Server) adminDeleteAddress(c *gin.Context) {
	if id, ok := idParam(c, "id"); ok {
		reply(c, gin.H{"ok": true}, s.App.DeleteBookAddress(c, adminActor(c), id))
	}
}
