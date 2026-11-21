package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
)

const (
	errorCodeKey    = "error_code"
	errorMessageKey = "error_message"
)

// streamKind names the kind of event stream a route serves, or "" for an
// ordinary request.
func streamKind(route string) string {
	if !strings.HasSuffix(route, "/events") {
		return ""
	}
	switch {
	case strings.Contains(route, "/customer/"):
		return "customer"
	case strings.Contains(route, "/table-sessions/"):
		return "table"
	case strings.Contains(route, "/rider/"):
		return "rider"
	}
	return "store"
}

// selfMonitoring reports the endpoints the back office polls every few
// seconds while it is open. Counting them would make an idle platform look
// busy just because somebody is watching it.
func selfMonitoring(route string) bool {
	for _, p := range []string{"/api/v1/admin/overview", "/api/v1/admin/metrics/", "/api/v1/admin/worker"} {
		if strings.HasPrefix(route, p) {
			return true
		}
	}
	return false
}

// metrics counts every API request by route template, so that neither order
// numbers nor tokens end up in the numbers. Health checks and static files
// are not counted, nor is the polling of the monitoring pages themselves. Event streams stay open for minutes: they are counted as
// open connections, not as requests, or they would drown every latency figure.
func (s *Server) metrics(c *gin.Context) {
	m := s.Metrics
	if m == nil {
		c.Next()
		return
	}
	route := c.FullPath()
	if route == "" && strings.HasPrefix(c.Request.URL.Path, "/api/") {
		route = "unmatched"
	}
	if !strings.HasPrefix(route, "/api/") && route != "unmatched" {
		c.Next()
		return
	}
	start := time.Now()
	stream := streamKind(route)
	counted := stream == "" && !selfMonitoring(route)
	finished := false
	defer func() {
		status := c.Writer.Status()
		code := c.GetString(errorCodeKey)
		if !finished { // a panic is on its way up to gin.Recovery, which answers 500
			status, code = http.StatusInternalServerError, "PANIC"
		}
		d := time.Since(start)
		if counted {
			m.Observe(start, c.Request.Method, route, status, d)
		}
		if status >= 500 || status == http.StatusTooManyRequests {
			e := app.APIError{At: start, RequestID: c.GetString("request_id"), Method: c.Request.Method, Route: route,
				Status: status, Code: code, DurationMS: int(d.Milliseconds()), Message: c.GetString(errorMessageKey)}
			if u := currentUser(c); u != nil {
				e.UserID = &u.ID
			}
			m.Error(e)
		}
	}()
	if stream != "" {
		defer m.StreamOpened(stream)()
	}
	c.Next()
	finished = true
}
