package httpapi

// The store's own page and domains (docs/ORDER_TYPES_DESIGN.md §5).

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

func (s *Server) siteRoutes(r *gin.Engine, api, auth, admin *gin.RouterGroup) {
	api.GET("/site", s.siteInfo)
	// Caddy asks before it gets a certificate for a new host (on-demand TLS).
	r.GET("/internal/tls-ask", s.tlsAsk)
	auth.GET("/merchant/branches/:bid/site", s.siteLinks)
	auth.POST("/merchant/branches/:bid/domains", s.addDomain)
	auth.DELETE("/merchant/branches/:bid/domains/:id", s.deleteDomain)
	admin.GET("/domains", s.adminDomains)
	admin.PUT("/domains/:id", s.adminUpdateDomain)
}

// site is what the request's host serves: the mall or a store's own page.
func (s *Server) site(c *gin.Context) (*app.Site, bool) {
	site, err := s.App.SiteFor(c, c.Request.Host)
	if err != nil {
		fail(c, err)
		return nil, false
	}
	return site, true
}

// siteFor is site for a request on one store: on another store's own page
// that store is not there.
func (s *Server) siteFor(c *gin.Context, slug string) (*app.Site, bool) {
	site, ok := s.site(c)
	if ok && site.OwnSite() && site.Store.Slug != slug {
		fail(c, app.Err(http.StatusNotFound, "NOT_FOUND", "not found"))
		return nil, false
	}
	return site, ok
}

func (s *Server) siteInfo(c *gin.Context) {
	if site, ok := s.site(c); ok {
		c.JSON(200, site)
	}
}

func (s *Server) tlsAsk(c *gin.Context) {
	ok, err := s.App.TLSAllowed(c, c.Query("domain"))
	if err != nil {
		fail(c, err)
		return
	}
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	c.Status(http.StatusOK)
}

func (s *Server) siteLinks(c *gin.Context) {
	if bid, ok := branchScope(c, pSettings); ok {
		l, err := s.App.SiteLinks(c, bid)
		reply(c, l, err)
	}
}

func (s *Server) addDomain(c *gin.Context) {
	bid, ok := branchScope(c, pSettings)
	if !ok {
		return
	}
	var in app.DomainInput
	if !bind(c, &in) {
		return
	}
	d, err := s.App.AddDomain(c, bid, in)
	reply(c, d, err)
}

func (s *Server) deleteDomain(c *gin.Context) {
	bid, ok := branchScope(c, pSettings)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	reply(c, gin.H{"ok": true}, s.App.DeleteDomain(c, bid, id))
}

func (s *Server) adminDomains(c *gin.Context) {
	list, err := s.App.AdminDomains(c, c.Query("status"))
	reply(c, gin.H{"domains": list}, err)
}

func (s *Server) adminUpdateDomain(c *gin.Context) {
	var in app.AdminDomainInput
	if !bind(c, &in) {
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	d, err := s.App.AdminUpdateDomain(c, adminActor(c), id, in)
	reply(c, d, err)
}
