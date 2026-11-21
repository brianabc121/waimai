package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// What the back office shows about API traffic, read from api_metrics.

// histSums selects the latency histogram summed over the grouped rows, one
// column per bucket.
var histSums = func() string {
	parts := make([]string, histSize)
	for i := range parts {
		parts[i] = fmt.Sprintf("coalesce(sum(hist[%d]), 0)::bigint", i+1)
	}
	return strings.Join(parts, ", ")
}()

// histDest returns scan targets for histSums.
func histDest(h *[histSize]int64) []any {
	out := make([]any, histSize)
	for i := range h {
		out[i] = &h[i]
	}
	return out
}

func addHist(dst *[histSize]int64, src [histSize]int64) {
	for i, n := range src {
		dst[i] += n
	}
}

type APIPoint struct {
	T   time.Time `json:"t"`
	OK  int64     `json:"ok"` // 2xx and 3xx
	C4  int64     `json:"c4"`
	C5  int64     `json:"c5"`
	P50 int       `json:"p50"`
	P95 int       `json:"p95"`
}

type APITotals struct {
	Requests int64 `json:"requests"`
	C4       int64 `json:"c4"`
	C5       int64 `json:"c5"`
	Limited  int64 `json:"limited"`
	P50      int   `json:"p50"`
	P95      int   `json:"p95"`
	MaxMS    int   `json:"max_ms"`
}

type APIRoute struct {
	Method string `json:"method"`
	Route  string `json:"route"`
	Count  int64  `json:"count"`
	C4     int64  `json:"c4"`
	C5     int64  `json:"c5"`
	P50    int    `json:"p50"`
	P95    int    `json:"p95"`
	MaxMS  int    `json:"max_ms"`
}

type APIStreams struct {
	Instances int `json:"instances"` // api processes seen in the last 30 seconds
	Customer  int `json:"customer"`
	Table     int `json:"table"`
	Store     int `json:"store"`
	Rider     int `json:"rider"`
}

type APIMetrics struct {
	Range       string     `json:"range"`
	StepSeconds int        `json:"step_seconds"`
	Series      []APIPoint `json:"series"`
	Totals      APITotals  `json:"totals"`
	Routes      []APIRoute `json:"routes"`
	Streams     APIStreams `json:"streams"`
}

// apiRanges maps a range to the width of one point and how many there are.
var apiRanges = map[string]struct {
	step time.Duration
	n    int
}{
	"1h":  {time.Minute, 60},
	"6h":  {5 * time.Minute, 72},
	"24h": {15 * time.Minute, 96},
	"7d":  {2 * time.Hour, 84},
}

// apiSeries returns n points of the given width ending with the current one,
// and the totals over them. Points without traffic are zero.
func (a *App) apiSeries(ctx context.Context, step time.Duration, n int) ([]APIPoint, APITotals, error) {
	var tot APITotals
	stepS := int64(step.Seconds())
	start := time.Unix(time.Now().Unix()/stepS*stepS-int64(n-1)*stepS, 0)
	points := make([]APIPoint, n)
	hists := make([][histSize]int64, n)
	maxes := make([]int, n)
	for i := range points {
		points[i].T = start.Add(time.Duration(i) * step)
	}
	rows, err := a.DB.Query(ctx, `SELECT floor((extract(epoch FROM bucket) - $2) / $3)::int, status, sum(count)::bigint,
		sum(limited)::bigint, max(max_ms), `+histSums+` FROM api_metrics WHERE bucket >= $1 GROUP BY 1, 2`,
		start, start.Unix(), stepS)
	if err != nil {
		return nil, tot, err
	}
	defer rows.Close()
	var all [histSize]int64
	for rows.Next() {
		var i, class, maxMS int
		var count, limited int64
		var h [histSize]int64
		if err := rows.Scan(append([]any{&i, &class, &count, &limited, &maxMS}, histDest(&h)...)...); err != nil {
			return nil, tot, err
		}
		if i < 0 || i >= n {
			continue
		}
		switch class {
		case 4:
			points[i].C4 += count
			tot.C4 += count
		case 5:
			points[i].C5 += count
			tot.C5 += count
		default:
			points[i].OK += count
		}
		tot.Requests += count
		tot.Limited += limited
		addHist(&hists[i], h)
		addHist(&all, h)
		if maxMS > maxes[i] {
			maxes[i] = maxMS
		}
		if maxMS > tot.MaxMS {
			tot.MaxMS = maxMS
		}
	}
	if err := rows.Err(); err != nil {
		return nil, tot, err
	}
	for i := range points {
		points[i].P50 = Quantile(hists[i][:], 0.5, maxes[i])
		points[i].P95 = Quantile(hists[i][:], 0.95, maxes[i])
	}
	tot.P50 = Quantile(all[:], 0.5, tot.MaxMS)
	tot.P95 = Quantile(all[:], 0.95, tot.MaxMS)
	return points, tot, nil
}

func (a *App) apiStreams(ctx context.Context) (APIStreams, error) {
	var s APIStreams
	err := a.DB.QueryRow(ctx, `SELECT count(*), coalesce(sum(sse_customer), 0), coalesce(sum(sse_table), 0),
		coalesce(sum(sse_store), 0), coalesce(sum(sse_rider), 0) FROM api_instances
		WHERE last_seen_at > now() - make_interval(secs => $1)`, apiOffline.Seconds()).
		Scan(&s.Instances, &s.Customer, &s.Table, &s.Store, &s.Rider)
	return s, err
}

// APIMetrics returns the traffic of the last hour, 6 hours, 24 hours or 7 days.
func (a *App) APIMetrics(ctx context.Context, rng string) (*APIMetrics, error) {
	r, found := apiRanges[rng]
	if !found {
		return nil, ErrValidation("unknown range")
	}
	out := &APIMetrics{Range: rng, StepSeconds: int(r.step.Seconds()), Routes: []APIRoute{}}
	var err error
	if out.Series, out.Totals, err = a.apiSeries(ctx, r.step, r.n); err != nil {
		return nil, err
	}
	rows, err := a.DB.Query(ctx, `SELECT method, route, sum(count)::bigint, coalesce(sum(count) FILTER (WHERE status = 4), 0)::bigint,
		coalesce(sum(count) FILTER (WHERE status = 5), 0)::bigint, max(max_ms), `+histSums+`
		FROM api_metrics WHERE bucket >= $1 GROUP BY method, route ORDER BY 3 DESC`, out.Series[0].T)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rt APIRoute
		var h [histSize]int64
		if err := rows.Scan(append([]any{&rt.Method, &rt.Route, &rt.Count, &rt.C4, &rt.C5, &rt.MaxMS}, histDest(&h)...)...); err != nil {
			return nil, err
		}
		rt.P50, rt.P95 = Quantile(h[:], 0.5, rt.MaxMS), Quantile(h[:], 0.95, rt.MaxMS)
		out.Routes = append(out.Routes, rt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.Streams, err = a.apiStreams(ctx)
	return out, err
}

type APIErrorFilter struct {
	Route  string
	Status int    // 0 for any; 5 for every 5xx; otherwise the exact code
	Since  string // one of the ranges of APIMetrics, or "" for everything kept
	Page
}

type APIErrorList struct {
	Items []APIError `json:"items"`
	Total int        `json:"total"`
}

// ListAPIErrors returns the recorded 5xx and 429 responses, newest first.
func (a *App) ListAPIErrors(ctx context.Context, f APIErrorFilter) (*APIErrorList, error) {
	since := time.Time{}
	if f.Since != "" {
		r, found := apiRanges[f.Since]
		if !found {
			return nil, ErrValidation("unknown range")
		}
		since = time.Now().Add(-r.step * time.Duration(r.n))
	}
	const where = ` FROM api_errors WHERE ($1 = '' OR route = $1) AND at >= $3
		AND ($2 = 0 OR status = $2 OR ($2 = 5 AND status >= 500))`
	out := &APIErrorList{Items: []APIError{}}
	if err := a.DB.QueryRow(ctx, `SELECT count(*)`+where, f.Route, f.Status, since).Scan(&out.Total); err != nil {
		return nil, err
	}
	limit, offset := f.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT id, at, request_id, method, route, status, code, duration_ms, user_id, message`+where+`
		ORDER BY at DESC, id DESC LIMIT $4 OFFSET $5`, f.Route, f.Status, since, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e APIError
		if err := rows.Scan(&e.ID, &e.At, &e.RequestID, &e.Method, &e.Route, &e.Status, &e.Code, &e.DurationMS, &e.UserID, &e.Message); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, e)
	}
	return out, rows.Err()
}

// ---- overview ----

// Thresholds of the four health cards. "Warn" is the level from which a card
// turns yellow, "Bad" the level from which it turns red.
const (
	apiErrorRateWarn = 0.01
	apiErrorRateBad  = 0.05
	apiP95WarnMS     = 1000
	apiP95BadMS      = 3000
	queueWaitWarn    = 60  // seconds
	queueWaitBad     = 600 // seconds
)

type HealthAPI struct {
	State     string  `json:"state"` // ok, warn, bad
	Requests  int64   `json:"requests"`
	ErrorRate float64 `json:"error_rate"`
	P95       int     `json:"p95"`
	Instances int     `json:"instances"`
}

type HealthWorker struct {
	State       string   `json:"state"`
	Online      int      `json:"online"`
	FailingTask []string `json:"failing_tasks"`
}

type HealthDB struct {
	State  string `json:"state"`
	PingMS int    `json:"ping_ms"`
	InUse  int    `json:"in_use"`
	Max    int    `json:"max"`
}

type HealthQueue struct {
	State             string `json:"state"`
	Pending           int    `json:"pending"`
	Failed            int    `json:"failed"`
	OldestWaitSeconds int    `json:"oldest_wait_seconds"`
}

type Overview struct {
	Now    time.Time    `json:"now"`
	API    HealthAPI    `json:"api"`
	Worker HealthWorker `json:"worker"`
	DB     HealthDB     `json:"db"`
	Queue  HealthQueue  `json:"queue"`
	// Day is the traffic of the last 24 hours.
	Day         APITotals  `json:"day"`
	OrdersToday int        `json:"orders_today"` // by each store's own calendar day
	StoresOpen  int        `json:"stores_open"`
	StoresTotal int        `json:"stores_total"`
	Hour        []APIPoint `json:"hour"` // the last 60 minutes, one point a minute
	Errors      []APIError `json:"errors"`
}

func worse(a, b string) string {
	rank := map[string]int{"ok": 0, "warn": 1, "bad": 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// apiHealth judges the last five minutes of traffic.
func apiHealth(requests, c5 int64, p95 int) (state string, rate float64) {
	state = "ok"
	if requests > 0 {
		rate = float64(c5) / float64(requests)
	}
	switch {
	case rate > apiErrorRateBad:
		state = "bad"
	case rate >= apiErrorRateWarn:
		state = "warn"
	}
	switch {
	case p95 > apiP95BadMS:
		state = "bad"
	case p95 >= apiP95WarnMS:
		state = worse(state, "warn")
	}
	return state, rate
}

func queueHealth(failed, oldestWait int) string {
	switch {
	case oldestWait > queueWaitBad:
		return "bad"
	case oldestWait >= queueWaitWarn || failed > 0:
		return "warn"
	}
	return "ok"
}

// AdminOverview is the front page of the back office: is everything healthy,
// and how busy is the platform.
func (a *App) AdminOverview(ctx context.Context) (*Overview, error) {
	out := &Overview{Now: time.Now(), Errors: []APIError{}, Hour: []APIPoint{}, Worker: HealthWorker{FailingTask: []string{}}}
	var err error

	// database: the ping also tells whether a connection could be had at all
	start := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	pingErr := a.DB.Ping(pingCtx)
	cancel()
	st := a.DB.Stat()
	out.DB = HealthDB{State: "ok", PingMS: int(time.Since(start).Milliseconds()), InUse: int(st.AcquiredConns()), Max: int(st.MaxConns())}
	if pingErr != nil {
		out.DB.State = "bad"
		out.API.State, out.Worker.State, out.Queue.State = "bad", "bad", "bad"
		return out, nil // nothing else can be read
	} else if st.AcquiredConns() >= st.MaxConns() {
		out.DB.State = "warn"
	}

	// api: the last five minutes
	_, tot5, err := a.apiSeries(ctx, time.Minute, 5)
	if err != nil {
		return nil, err
	}
	streams, err := a.apiStreams(ctx)
	if err != nil {
		return nil, err
	}
	out.API = HealthAPI{Requests: tot5.Requests, P95: tot5.P95, Instances: streams.Instances}
	out.API.State, out.API.ErrorRate = apiHealth(tot5.Requests, tot5.C5, tot5.P95)

	// worker and queue
	ws, err := a.WorkerStatus(ctx)
	if err != nil {
		return nil, err
	}
	out.Worker = HealthWorker{State: "ok", FailingTask: []string{}}
	for _, w := range ws.Instances {
		if w.Online {
			out.Worker.Online++
		}
	}
	for _, task := range ws.Tasks {
		if !task.OK {
			out.Worker.FailingTask = append(out.Worker.FailingTask, task.Task)
		}
	}
	if out.Worker.Online == 0 {
		out.Worker.State = "bad"
	} else if len(out.Worker.FailingTask) > 0 {
		out.Worker.State = "warn"
	}
	out.Queue = HealthQueue{Pending: ws.Queue.Pending, Failed: ws.Queue.Failed, OldestWaitSeconds: ws.Queue.OldestWaitSeconds,
		State: queueHealth(ws.Queue.Failed, ws.Queue.OldestWaitSeconds)}

	// traffic
	if out.Hour, _, err = a.apiSeries(ctx, time.Minute, 60); err != nil {
		return nil, err
	}
	if _, out.Day, err = a.apiSeries(ctx, 15*time.Minute, 96); err != nil {
		return nil, err
	}
	errs, err := a.ListAPIErrors(ctx, APIErrorFilter{Status: 5, Page: Page{Page: 1, PageSize: 5}})
	if err != nil {
		return nil, err
	}
	out.Errors = errs.Items

	// business
	rows, err := a.DB.Query(ctx, `SELECT b.timezone, b.open_time, b.close_time, b.paused OR m.status <> 'active',
		(SELECT count(*) FROM orders o WHERE o.branch_id = b.id AND o.created_at >= `+localDayStart+`)
		FROM branches b JOIN merchants m ON m.id = b.merchant_id WHERE b.deleted_at IS NULL AND m.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b Branch
		var closed bool
		var orders int
		if err := rows.Scan(&b.Timezone, &b.OpenTime, &b.CloseTime, &closed, &orders); err != nil {
			return nil, err
		}
		out.StoresTotal++
		out.OrdersToday += orders
		if !closed && b.isOpen(out.Now) {
			out.StoresOpen++
		}
	}
	return out, rows.Err()
}
