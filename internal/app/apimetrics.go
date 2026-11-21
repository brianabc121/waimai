package app

import (
	"context"
	"encoding/hex"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Request metrics. Every api process counts its requests in memory and writes
// what it counted to the database every few seconds; nothing is written on
// the request path.

// LatencyBuckets are the upper bounds, in milliseconds, of the latency
// histogram. One more bucket holds everything slower than the last bound.
var LatencyBuckets = [...]int{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

const (
	histSize        = len(LatencyBuckets) + 1
	metricsFlush    = 10 * time.Second
	apiOffline      = 30 * time.Second // three missed flushes
	maxErrorsBuffer = 200              // per flush, so that a flood of 429s cannot fill the table
)

type metricKey struct {
	minute int64 // unix time of the start of the minute
	method string
	route  string
	class  int
}

type metricCell struct {
	count, limited int
	sumMS          int64
	maxMS          int
	hist           [histSize]int
}

// APIError is one failed request worth looking at: a 5xx or a 429.
type APIError struct {
	ID         int64     `json:"id"`
	At         time.Time `json:"at"`
	RequestID  string    `json:"request_id"`
	Method     string    `json:"method"`
	Route      string    `json:"route"`
	Status     int       `json:"status"`
	Code       string    `json:"code"`
	DurationMS int       `json:"duration_ms"`
	UserID     *int64    `json:"user_id"`
	Message    string    `json:"message"`
}

// Metrics collects the request metrics of one api process.
type Metrics struct {
	app       *App
	logf      func(string, ...any)
	instance  string
	hostname  string
	startedAt time.Time

	mu     sync.Mutex
	cells  map[metricKey]*metricCell
	errors []APIError
	sse    map[string]int // customer, table, store, rider
}

func (a *App) NewMetrics(logf func(string, ...any)) *Metrics {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	return &Metrics{app: a, logf: logf, hostname: host, startedAt: time.Now(),
		instance: "api-" + hex.EncodeToString(randomBytes(4)),
		cells:    map[metricKey]*metricCell{}, sse: map[string]int{}}
}

func (m *Metrics) Instance() string { return m.instance }

func histIndex(ms int) int {
	for i, bound := range LatencyBuckets {
		if ms <= bound {
			return i
		}
	}
	return histSize - 1
}

// Observe counts one finished request.
func (m *Metrics) Observe(at time.Time, method, route string, status int, d time.Duration) {
	ms := int(d.Milliseconds())
	k := metricKey{minute: at.Truncate(time.Minute).Unix(), method: method, route: route, class: status / 100}
	m.mu.Lock()
	c := m.cells[k]
	if c == nil {
		c = &metricCell{}
		m.cells[k] = c
	}
	c.count++
	if status == 429 {
		c.limited++
	}
	c.sumMS += int64(ms)
	if ms > c.maxMS {
		c.maxMS = ms
	}
	c.hist[histIndex(ms)]++
	m.mu.Unlock()
}

// Error keeps one failed request for the error list.
func (m *Metrics) Error(e APIError) {
	m.mu.Lock()
	if len(m.errors) < maxErrorsBuffer {
		m.errors = append(m.errors, e)
	}
	m.mu.Unlock()
}

// StreamOpened counts an event stream of the given kind (customer, table,
// store or rider) as open until the returned function is called.
func (m *Metrics) StreamOpened(kind string) (closed func()) {
	m.mu.Lock()
	m.sse[kind]++
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			m.sse[kind]--
			m.mu.Unlock()
		})
	}
}

// Flush writes what was counted since the last flush, and the heartbeat of
// this process. What cannot be written is dropped: metrics never get in the
// way of serving requests.
func (m *Metrics) Flush(ctx context.Context) error {
	m.mu.Lock()
	cells, errs := m.cells, m.errors
	m.cells, m.errors = map[metricKey]*metricCell{}, nil
	sse := [4]int{m.sse["customer"], m.sse["table"], m.sse["store"], m.sse["rider"]}
	m.mu.Unlock()

	b := &pgx.Batch{}
	for k, c := range cells {
		hist := make([]int32, histSize)
		for i, n := range c.hist {
			hist[i] = int32(n)
		}
		// The same minute is flushed several times; later parts add to the row.
		b.Queue(`INSERT INTO api_metrics AS t (bucket, instance, method, route, status, count, limited, sum_ms, max_ms, hist)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (bucket, instance, method, route, status) DO UPDATE SET count = t.count + excluded.count,
				limited = t.limited + excluded.limited, sum_ms = t.sum_ms + excluded.sum_ms,
				max_ms = greatest(t.max_ms, excluded.max_ms),
				hist = ARRAY(SELECT x + y FROM unnest(t.hist, excluded.hist) AS u(x, y))`,
			time.Unix(k.minute, 0), m.instance, k.method, k.route, k.class, c.count, c.limited, c.sumMS, c.maxMS, hist)
	}
	for _, e := range errs {
		b.Queue(`INSERT INTO api_errors (at, request_id, method, route, status, code, duration_ms, user_id, message)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, e.At, e.RequestID, e.Method, e.Route, e.Status, e.Code, e.DurationMS,
			e.UserID, truncate(e.Message, 500))
	}
	b.Queue(`INSERT INTO api_instances (instance, hostname, started_at, last_seen_at, sse_customer, sse_table, sse_store, sse_rider)
		VALUES ($1,$2,$3,now(),$4,$5,$6,$7)
		ON CONFLICT (instance) DO UPDATE SET last_seen_at = now(), sse_customer=$4, sse_table=$5, sse_store=$6, sse_rider=$7`,
		m.instance, m.hostname, m.startedAt, sse[0], sse[1], sse[2], sse[3])
	return m.app.DB.SendBatch(ctx, b).Close()
}

// Run flushes until ctx is cancelled, then once more, and signs the process off.
func (m *Metrics) Run(ctx context.Context) {
	tick := time.NewTicker(metricsFlush)
	defer tick.Stop()
	for {
		if err := m.Flush(ctx); err != nil && ctx.Err() == nil {
			m.logf("metrics: flush: %v", err)
		}
		select {
		case <-ctx.Done():
			m.Stop()
			return
		case <-tick.C:
		}
	}
}

// Stop writes the last counts and removes the heartbeat of this process.
func (m *Metrics) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.Flush(ctx); err != nil {
		m.logf("metrics: last flush: %v", err)
	}
	if _, err := m.app.DB.Exec(ctx, `DELETE FROM api_instances WHERE instance=$1`, m.instance); err != nil {
		m.logf("metrics: sign off: %v", err)
	}
}

// Quantile estimates a percentile (q between 0 and 1) from a latency
// histogram by interpolating inside the bucket it falls in. maxMS, when
// known, bounds the open last bucket.
func Quantile(hist []int64, q float64, maxMS int) int {
	var total int64
	for _, n := range hist {
		total += n
	}
	if total == 0 {
		return 0
	}
	rank := q * float64(total)
	var seen float64
	for i, n := range hist {
		if n == 0 {
			continue
		}
		if seen+float64(n) >= rank {
			lo, hi := 0.0, 0.0
			if i > 0 {
				lo = float64(LatencyBuckets[i-1])
			}
			if i < len(LatencyBuckets) {
				hi = float64(LatencyBuckets[i])
			} else {
				hi = lo * 2
				if maxMS > int(lo) {
					hi = float64(maxMS)
				}
			}
			if maxMS > 0 && float64(maxMS) < hi && float64(maxMS) >= lo {
				hi = float64(maxMS) // nothing was slower than the slowest request
			}
			return int(lo + (hi-lo)*(rank-seen)/float64(n) + 0.5)
		}
		seen += float64(n)
	}
	return maxMS
}
