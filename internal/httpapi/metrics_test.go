package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/httpapi"
)

func routeStats(t *testing.T, out map[string]any) map[string]map[string]any {
	t.Helper()
	rows := map[string]map[string]any{}
	for _, r := range out["routes"].([]any) {
		row := r.(map[string]any)
		rows[row["method"].(string)+" "+row["route"].(string)] = row
	}
	return rows
}

func TestAPIMetricsOverHTTP(t *testing.T) {
	f := newFixture(t)
	admin, owner := adminClient(t), login(t, f.owner)
	ctx := context.Background()
	if _, err := a.DB.Exec(ctx, `TRUNCATE api_metrics, api_errors`); err != nil {
		t.Fatal(err)
	}
	if err := metrics.Flush(ctx); err != nil { // drop what earlier tests left in memory
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(ctx, `TRUNCATE api_metrics, api_errors`); err != nil {
		t.Fatal(err)
	}

	for _, r := range []string{"/admin/overview", "/admin/metrics/api", "/admin/metrics/errors"} {
		owner.want(403, "FORBIDDEN", "GET", r, nil)
		newClient(t).want(401, "UNAUTHENTICATED", "GET", r, nil)
	}

	visitor := newClient(t)
	for i := 0; i < 5; i++ {
		visitor.want(200, "", "GET", "/stores/"+f.slug, nil)
	}
	visitor.want(404, "NOT_FOUND", "GET", "/stores/no-such-store", nil)
	visitor.want(404, "NOT_FOUND", "GET", "/no/such/route", nil)
	// health checks are not API traffic
	for _, p := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(server.URL + p)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v", p, err)
		}
		resp.Body.Close()
	}
	// hammer the login until the limiter answers
	guesser := newClient(t)
	limited := 0
	for i := 0; i < 30 && limited < 2; i++ {
		if status, _ := guesser.do("POST", "/auth/login", map[string]any{"username": f.owner, "password": "wrong-guess"}); status == 429 {
			limited++
		}
	}
	if limited < 2 {
		t.Fatal("the login limiter never answered 429")
	}
	if err := metrics.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	out := admin.want(200, "", "GET", "/admin/metrics/api?range=1h", nil)
	if len(out["series"].([]any)) != 60 || out["step_seconds"].(float64) != 60 {
		t.Fatalf("series: %v points", len(out["series"].([]any)))
	}
	rows := routeStats(t, out)
	menu := rows["GET /api/v1/stores/:slug"]
	if menu == nil || menu["count"].(float64) != 6 || menu["c4"].(float64) != 1 || menu["c5"].(float64) != 0 {
		t.Fatalf("the menu route, by template: %v", menu)
	}
	if un := rows["GET unmatched"]; un == nil || un["count"].(float64) != 1 {
		t.Fatalf("unknown paths are counted together: %v", rows)
	}
	for key := range rows {
		if strings.Contains(key, f.slug) || strings.Contains(key, "no-such") || strings.Contains(key, "healthz") ||
			strings.Contains(key, "readyz") || strings.HasSuffix(key, "/events") || strings.Contains(key, "/admin/metrics") {
			t.Fatalf("route %q should not be in the metrics", key)
		}
	}
	totals := out["totals"].(map[string]any)
	if totals["limited"].(float64) != 2 || totals["c5"].(float64) != 0 || totals["requests"].(float64) < 9 {
		t.Fatalf("totals: %v", totals)
	}
	if out["streams"].(map[string]any)["instances"].(float64) < 1 {
		t.Fatalf("this process should be counted: %v", out["streams"])
	}
	admin.want(422, "VALIDATION", "GET", "/admin/metrics/api?range=forever", nil)
	// watching the monitoring pages does not show up as traffic, other admin work does
	admin.want(200, "", "GET", "/admin/merchants", nil)
	if err := metrics.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	rows = routeStats(t, admin.want(200, "", "GET", "/admin/metrics/api", nil))
	if rows["GET /api/v1/admin/metrics/api"] != nil || rows["GET /api/v1/admin/merchants"] == nil {
		t.Fatalf("admin routes in the metrics: %v", rows)
	}

	// rate limiting is listed with the request id the client was given
	out = admin.want(200, "", "GET", "/admin/metrics/errors?status=429", nil)
	if out["total"].(float64) != 2 {
		t.Fatalf("rate limited: %v", out)
	}
	e := out["items"].([]any)[0].(map[string]any)
	if e["code"] != "RATE_LIMITED" || e["route"] != "/api/v1/auth/login" || len(e["request_id"].(string)) != 16 {
		t.Fatalf("error row: %v", e)
	}
	out = admin.want(200, "", "GET", "/admin/metrics/errors?status=5", nil)
	if out["total"].(float64) != 0 {
		t.Fatalf("no server errors happened: %v", out)
	}

	out = admin.want(200, "", "GET", "/admin/overview", nil)
	if out["db"].(map[string]any)["state"] != "ok" || out["api"].(map[string]any)["state"] != "ok" || len(out["hour"].([]any)) != 60 {
		t.Fatalf("overview: %v", out)
	}
}

// TestMetricsRecordsServerErrors drives the middleware with handlers that
// fail in the two ways a handler can: an unexpected error and a panic.
func TestMetricsRecordsServerErrors(t *testing.T) {
	if a == nil {
		t.Skip("no database")
	}
	ctx := context.Background()
	m := a.NewMetrics(t.Logf)
	s := httpapi.NewServer(a, config.Config{}, httpapi.NewHub())
	s.Metrics = m
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), httpapi.RequestID, s.MetricsMiddleware())
	r.GET("/api/v1/t/:id/broken", func(c *gin.Context) { httpapi.Fail(c, errors.New("disk on fire")) })
	r.GET("/api/v1/t/:id/panic", func(c *gin.Context) { panic("nil map") })
	r.GET("/api/v1/t/:id/fine", func(c *gin.Context) { c.JSON(200, gin.H{}) })
	r.GET("/assets/app.js", func(c *gin.Context) { c.String(200, "x") })
	ts := httptest.NewServer(r)
	defer ts.Close()
	route := "/api/v1/t/:id/"
	ids := map[string]string{}
	for path, want := range map[string]int{"broken": 500, "panic": 500, "fine": 200} {
		resp, err := http.Get(ts.URL + "/api/v1/t/secret-token-77/" + path)
		if err != nil || resp.StatusCode != want {
			t.Fatalf("%s: %v %v", path, err, resp)
		}
		ids[path] = resp.Header.Get("X-Request-Id")
		resp.Body.Close()
	}
	resp, err := http.Get(ts.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := m.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)

	for path, code := range map[string]string{"broken": "INTERNAL", "panic": "PANIC"} {
		l, err := a.ListAPIErrors(ctx, app.APIErrorFilter{Route: route + path})
		if err != nil {
			t.Fatal(err)
		}
		if l.Total != 1 || l.Items[0].Status != 500 || l.Items[0].Code != code || l.Items[0].RequestID != ids[path] {
			t.Fatalf("%s: %+v (request id %s)", path, l.Items, ids[path])
		}
		if path == "broken" && l.Items[0].Message != "disk on fire" {
			t.Fatalf("the internal message is kept for the administrator: %+v", l.Items[0])
		}
	}
	var counted, fives, static int
	if err := a.DB.QueryRow(ctx, `SELECT coalesce(sum(count), 0), coalesce(sum(count) FILTER (WHERE status = 5), 0),
		(SELECT count(*) FROM api_metrics WHERE route LIKE '/assets/%' OR route LIKE '%secret-token%')
		FROM api_metrics WHERE instance = $1`, m.Instance()).Scan(&counted, &fives, &static); err != nil {
		t.Fatal(err)
	}
	if counted != 3 || fives != 2 || static != 0 {
		t.Fatalf("counted %d requests, %d of them 5xx, %d rows that should not exist", counted, fives, static)
	}
}
