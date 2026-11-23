package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/config"
)

// After an update an open page asks for an older build's file name: that
// must be a 404 (so the page knows to reload), not index.html.
func TestStaticCaching(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>page</html>"), 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "OrderView-new.js"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "sw.js"), []byte("sw"), 0o644)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	(&Server{Cfg: config.Config{StaticDir: dir}}).static(r)
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		return w
	}
	for _, c := range []struct {
		path, cache string
		code        int
	}{
		{"/o/abc", "no-cache", 200},
		{"/", "no-cache", 200},
		{"/sw.js", "no-cache", 200},
		{"/assets/OrderView-new.js", "public, max-age=31536000, immutable", 200},
		{"/assets/OrderView-old.js", "", http.StatusNotFound},
	} {
		w := get(c.path)
		if w.Code != c.code || w.Header().Get("Cache-Control") != c.cache {
			t.Errorf("%s: %d %q", c.path, w.Code, w.Header().Get("Cache-Control"))
		}
	}
}
