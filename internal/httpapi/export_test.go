package httpapi

import "github.com/gin-gonic/gin"

// Test hooks: the pieces a test needs to build a small router of its own
// around the metrics middleware.
var (
	RequestID = requestID
	Fail      = fail
)

func (s *Server) MetricsMiddleware() gin.HandlerFunc { return s.metrics }
