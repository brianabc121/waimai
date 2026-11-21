package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/brianabc121/waimai/internal/app"
)

func TestQuantile(t *testing.T) {
	// buckets: ≤5, ≤10, ≤25, ≤50, ≤100, ≤250, ≤500, ≤1000, ≤2500, ≤5000, slower
	if got := app.Quantile(make([]int64, 11), 0.95, 0); got != 0 {
		t.Fatalf("no requests: %d", got)
	}
	// 100 requests between 10 and 25 ms: the median is in the middle of that bucket
	h := []int64{0, 0, 100, 0, 0, 0, 0, 0, 0, 0, 0}
	if got := app.Quantile(h, 0.5, 0); got < 17 || got > 18 {
		t.Fatalf("median inside one bucket: %d", got)
	}
	// 90 fast and 10 slow ones: P50 is fast, P95 lands among the slow ones
	h = []int64{90, 0, 0, 0, 0, 0, 0, 10, 0, 0, 0}
	if got := app.Quantile(h, 0.5, 900); got > 5 {
		t.Fatalf("p50: %d", got)
	}
	if got := app.Quantile(h, 0.95, 900); got <= 500 || got > 900 {
		t.Fatalf("p95 should be between 500 ms and the slowest request: %d", got)
	}
	// the open last bucket is bounded by the slowest request
	h = []int64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 10}
	if got := app.Quantile(h, 0.95, 8000); got <= 5000 || got > 8000 {
		t.Fatalf("p95 in the open bucket: %d", got)
	}
	// merging two processes is adding their histograms
	a, b := []int64{50, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, []int64{0, 0, 0, 0, 0, 0, 50, 0, 0, 0, 0}
	sum := make([]int64, 11)
	for i := range sum {
		sum[i] = a[i] + b[i]
	}
	if got := app.Quantile(sum, 0.95, 400); got <= 250 || got > 400 {
		t.Fatalf("merged p95: %d", got)
	}
}

func clearMetrics(t *testing.T) {
	t.Helper()
	_, err := testApp.DB.Exec(context.Background(), `TRUNCATE api_metrics, api_errors, api_instances`)
	noErr(t, err)
}

func TestAPIMetricsFlushAndMerge(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	clearMetrics(t)
	now := time.Now()
	m1, m2 := a.NewMetrics(t.Logf), a.NewMetrics(t.Logf)
	const orders, menu = "/api/v1/merchant/branches/:bid/orders", "/api/v1/stores/:slug"

	for i := 0; i < 40; i++ {
		m1.Observe(now, "GET", orders, 200, 8*time.Millisecond)
	}
	for i := 0; i < 40; i++ {
		m2.Observe(now, "GET", orders, 200, 300*time.Millisecond)
	}
	m2.Observe(now, "GET", orders, 500, 700*time.Millisecond)
	m2.Error(app.APIError{At: now, RequestID: "req-1", Method: "GET", Route: orders, Status: 500, Code: "INTERNAL", DurationMS: 700, Message: "boom"})
	m1.Observe(now, "POST", "/api/v1/auth/login", 429, time.Millisecond)
	m1.Observe(now, "POST", "/api/v1/auth/login", 401, 60*time.Millisecond)
	m1.Error(app.APIError{At: now, RequestID: "req-2", Method: "POST", Route: "/api/v1/auth/login", Status: 429, Code: "RATE_LIMITED"})
	m1.Observe(now.Add(-2*time.Hour), "GET", menu, 200, 20*time.Millisecond) // outside the last hour
	closeA := m1.StreamOpened("customer")
	closeB := m1.StreamOpened("customer")
	m2.StreamOpened("store")
	closeB()
	closeB() // closing twice counts once
	noErr(t, m1.Flush(ctx))
	noErr(t, m2.Flush(ctx))
	// the same minute is flushed again: the row grows instead of being replaced
	for i := 0; i < 20; i++ {
		m1.Observe(now, "GET", orders, 200, 8*time.Millisecond)
	}
	noErr(t, m1.Flush(ctx))
	noErr(t, m1.Flush(ctx)) // nothing new: nothing changes

	got, err := a.APIMetrics(ctx, "1h")
	noErr(t, err)
	if got.StepSeconds != 60 || len(got.Series) != 60 {
		t.Fatalf("1h is 60 points of a minute: step %d, %d points", got.StepSeconds, len(got.Series))
	}
	tot := got.Totals
	if tot.Requests != 103 || tot.C5 != 1 || tot.C4 != 2 || tot.Limited != 1 || tot.MaxMS != 700 {
		t.Fatalf("totals: %+v", tot)
	}
	// 60 requests at 8 ms and 41 slow ones: the median is fast, P95 is in the 250–500 ms bucket
	if tot.P50 > 10 || tot.P95 <= 250 || tot.P95 > 500 {
		t.Fatalf("percentiles over both processes: p50=%d p95=%d", tot.P50, tot.P95)
	}
	var sumOK, sumC4, sumC5 int64
	for _, p := range got.Series {
		sumOK, sumC4, sumC5 = sumOK+p.OK, sumC4+p.C4, sumC5+p.C5
	}
	if sumOK != 100 || sumC4 != 2 || sumC5 != 1 {
		t.Fatalf("series: ok=%d 4xx=%d 5xx=%d", sumOK, sumC4, sumC5)
	}
	if last := got.Series[59]; last.OK+got.Series[58].OK != 100 || last.T.After(now) {
		t.Fatalf("the traffic belongs to the latest minute: %+v", last)
	}
	if len(got.Routes) != 2 {
		t.Fatalf("routes in the last hour: %+v", got.Routes)
	}
	top := got.Routes[0]
	if top.Route != orders || top.Count != 101 || top.C5 != 1 || top.C4 != 0 || top.MaxMS != 700 || top.P50 > 10 || top.P95 <= 250 {
		t.Fatalf("busiest route, merged across processes: %+v", top)
	}
	if got.Streams.Instances != 2 || got.Streams.Customer != 1 || got.Streams.Store != 1 || got.Streams.Rider != 0 {
		t.Fatalf("streams: %+v", got.Streams)
	}

	// a wider range has wider points and includes the older request
	got, err = a.APIMetrics(ctx, "6h")
	noErr(t, err)
	if got.StepSeconds != 300 || len(got.Series) != 72 || got.Totals.Requests != 104 || len(got.Routes) != 3 {
		t.Fatalf("6h: step %d, %d points, %d requests, %d routes", got.StepSeconds, len(got.Series), got.Totals.Requests, len(got.Routes))
	}
	_, err = a.APIMetrics(ctx, "1y")
	wantCode(t, err, "VALIDATION")

	// errors
	errs, err := a.ListAPIErrors(ctx, app.APIErrorFilter{})
	noErr(t, err)
	if errs.Total != 2 {
		t.Fatalf("errors: %+v", errs)
	}
	errs, err = a.ListAPIErrors(ctx, app.APIErrorFilter{Status: 5, Since: "1h"})
	noErr(t, err)
	if errs.Total != 1 || errs.Items[0].RequestID != "req-1" || errs.Items[0].Message != "boom" || errs.Items[0].Code != "INTERNAL" {
		t.Fatalf("5xx: %+v", errs)
	}
	errs, err = a.ListAPIErrors(ctx, app.APIErrorFilter{Status: 429, Route: "/api/v1/auth/login"})
	noErr(t, err)
	if errs.Total != 1 || errs.Items[0].Code != "RATE_LIMITED" {
		t.Fatalf("429: %+v", errs)
	}

	// a process that stops writes what it still holds and signs off
	closeA()
	m2.Observe(now, "GET", menu, 200, time.Millisecond)
	m2.Stop()
	got, err = a.APIMetrics(ctx, "1h")
	noErr(t, err)
	if got.Totals.Requests != 104 || got.Streams.Instances != 1 || got.Streams.Store != 0 {
		t.Fatalf("after one process stopped: %+v %+v", got.Totals, got.Streams)
	}
	// one that died without signing off stops counting after 30 seconds
	_, err = a.DB.Exec(ctx, `UPDATE api_instances SET last_seen_at = now() - interval '40 seconds'`)
	noErr(t, err)
	got, err = a.APIMetrics(ctx, "1h")
	noErr(t, err)
	if got.Streams.Instances != 0 || got.Streams.Customer != 0 {
		t.Fatalf("silent process still counted: %+v", got.Streams)
	}

	// a burst of errors cannot fill the table: only so many are kept per flush
	for i := 0; i < 500; i++ {
		m1.Error(app.APIError{At: now, RequestID: "flood", Method: "POST", Route: "/api/v1/otp/request", Status: 429, Code: "RATE_LIMITED"})
	}
	noErr(t, m1.Flush(ctx))
	errs, err = a.ListAPIErrors(ctx, app.APIErrorFilter{Route: "/api/v1/otp/request"})
	noErr(t, err)
	if errs.Total != 200 {
		t.Fatalf("flood: %d errors kept", errs.Total)
	}
	m1.Stop()
}

func TestAPIMetricsRetention(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	clearMetrics(t)
	for _, age := range []string{"8 days", "6 days"} {
		_, err := a.DB.Exec(ctx, `INSERT INTO api_metrics (bucket, instance, method, route, status, count, sum_ms, max_ms, hist)
			VALUES (date_trunc('minute', now() - $1::interval), 'api-old', 'GET', '/api/v1/x', 2, 1, 1, 1, '{1,0,0,0,0,0,0,0,0,0,0}')`, age)
		noErr(t, err)
		_, err = a.DB.Exec(ctx, `INSERT INTO api_errors (at, request_id, method, route, status, code, duration_ms)
			VALUES (now() - $1::interval, 'r', 'GET', '/api/v1/x', 500, 'INTERNAL', 1)`, age)
		noErr(t, err)
	}
	_, err := a.DB.Exec(ctx, `INSERT INTO api_instances (instance, hostname, started_at, last_seen_at)
		VALUES ('api-old', 'h', now() - interval '3 days', now() - interval '2 days')`)
	noErr(t, err)
	noErr(t, a.Cleanup(ctx))
	var metrics, errs, instances int
	noErr(t, a.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM api_metrics), (SELECT count(*) FROM api_errors),
		(SELECT count(*) FROM api_instances)`).Scan(&metrics, &errs, &instances))
	if metrics != 1 || errs != 1 || instances != 0 {
		t.Fatalf("after cleanup: %d metric rows, %d errors, %d instances; want 1, 1, 0", metrics, errs, instances)
	}
	got, err := a.APIMetrics(ctx, "7d")
	noErr(t, err)
	if got.Totals.Requests != 1 || len(got.Series) != 84 {
		t.Fatalf("7d: %+v", got.Totals)
	}
}

func TestAdminOverview(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	s := newStore(t, true)
	clearMetrics(t)
	_, err := a.DB.Exec(ctx, `DELETE FROM jobs WHERE status IN ('pending','failed')`)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `DELETE FROM worker_instances`)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `DELETE FROM worker_tasks`)
	noErr(t, err)

	// nothing is running: the worker card is red, the rest is fine
	o, err := a.AdminOverview(ctx)
	noErr(t, err)
	if o.DB.State != "ok" || o.DB.Max == 0 || o.API.State != "ok" || o.Queue.State != "ok" || o.Worker.State != "bad" || o.Worker.Online != 0 {
		t.Fatalf("empty platform: db=%+v api=%+v queue=%+v worker=%+v", o.DB, o.API, o.Queue, o.Worker)
	}
	if len(o.Hour) != 60 || o.Errors == nil || o.StoresTotal == 0 {
		t.Fatalf("overview shape: %d points, errors %v, %d stores", len(o.Hour), o.Errors, o.StoresTotal)
	}
	ordersBefore := o.OrdersToday

	w := a.NewWorker(t.Logf, false)
	w.Tick(ctx)
	t.Cleanup(w.Stop)
	m := a.NewMetrics(t.Logf)
	now := time.Now()
	for i := 0; i < 97; i++ {
		m.Observe(now, "GET", "/api/v1/stores/:slug", 200, 12*time.Millisecond)
	}
	for i := 0; i < 3; i++ { // 3% of the requests fail
		m.Observe(now, "POST", "/api/v1/stores/:slug/orders", 500, 40*time.Millisecond)
		m.Error(app.APIError{At: now, RequestID: uniq("req"), Method: "POST", Route: "/api/v1/stores/:slug/orders", Status: 500, Code: "INTERNAL"})
	}
	m.Error(app.APIError{At: now, RequestID: "limited", Method: "POST", Route: "/api/v1/auth/login", Status: 429, Code: "RATE_LIMITED"})
	noErr(t, m.Flush(ctx))
	t.Cleanup(m.Stop)
	addJob(t, "+639170000555", "x", "failed", 5)
	s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})

	o, err = a.AdminOverview(ctx)
	noErr(t, err)
	if o.Worker.State != "ok" || o.Worker.Online != 1 || len(o.Worker.FailingTask) != 0 {
		t.Fatalf("worker: %+v", o.Worker)
	}
	if o.API.State != "warn" || o.API.Requests != 100 || o.API.ErrorRate < 0.029 || o.API.ErrorRate > 0.031 || o.API.Instances != 1 {
		t.Fatalf("api with 3%% errors: %+v", o.API)
	}
	if o.Queue.State != "warn" || o.Queue.Failed != 1 {
		t.Fatalf("queue with a failed job: %+v", o.Queue)
	}
	if o.Day.Requests != 100 || o.Day.C5 != 3 {
		t.Fatalf("day: %+v", o.Day)
	}
	// the error list shows server errors, not rate limiting
	if len(o.Errors) != 3 || o.Errors[0].Status != 500 {
		t.Fatalf("recent errors: %+v", o.Errors)
	}
	if o.OrdersToday != ordersBefore+1 {
		t.Fatalf("orders today: %d, was %d", o.OrdersToday, ordersBefore)
	}
	if o.StoresOpen == 0 || o.StoresOpen > o.StoresTotal {
		t.Fatalf("stores: %d of %d open", o.StoresOpen, o.StoresTotal)
	}

	// a failing task turns the worker card yellow
	_, err = a.DB.Exec(ctx, `UPDATE worker_tasks SET last_error='boom', last_error_at=now() + interval '1 second' WHERE task='cleanup'`)
	noErr(t, err)
	o, err = a.AdminOverview(ctx)
	noErr(t, err)
	if o.Worker.State != "warn" || len(o.Worker.FailingTask) != 1 || o.Worker.FailingTask[0] != "cleanup" {
		t.Fatalf("worker with a failing task: %+v", o.Worker)
	}
	_, err = a.DB.Exec(ctx, `DELETE FROM jobs WHERE status='failed'`)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `DELETE FROM worker_tasks`)
	noErr(t, err)
}
