package app

import (
	"context"
	"encoding/hex"
	"os"
	"time"
)

// The background worker: a loop of periodic tasks. Every run of a task and
// every turn of the loop is recorded, so that the back office can tell
// whether the worker is alive and what it last did.

const (
	workerTick      = 5 * time.Second
	cleanupInterval = 10 * time.Minute
	// workerOffline is how long without a heartbeat before an instance counts
	// as gone: three missed turns of the loop.
	workerOffline = 20 * time.Second
)

// workerTasks lists the periodic tasks in the order they run, with how often.
var workerTasks = []struct {
	Name     string
	Interval time.Duration
}{
	{"expire_pending", workerTick},
	{"expire_addons", workerTick},
	{"release_scheduled", workerTick},
	{"jobs", workerTick},
	{"rider_trips", tripInterval},
	{"cleanup", cleanupInterval},
}

// Worker is one running worker loop.
type Worker struct {
	app         *App
	logf        func(string, ...any)
	instance    string
	hostname    string
	embedded    bool
	startedAt   time.Time
	lastCleanup time.Time
	lastTrips   time.Time
}

// NewWorker prepares a worker loop. embedded says that it runs inside the api process.
func (a *App) NewWorker(logf func(string, ...any), embedded bool) *Worker {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	return &Worker{app: a, logf: logf, embedded: embedded, hostname: host, startedAt: time.Now(),
		instance: "worker-" + hex.EncodeToString(randomBytes(4))}
}

// Instance is the id this worker reports its heartbeat under.
func (w *Worker) Instance() string { return w.instance }

// runTask runs one periodic task and records the outcome. Recording is best
// effort: a failure to write the status never stops the task loop.
func (w *Worker) runTask(ctx context.Context, name string, fn func(context.Context) (int, error)) {
	start := time.Now()
	n, err := fn(ctx)
	ms := int(time.Since(start).Milliseconds())
	if ctx.Err() != nil {
		return // shutting down: an interrupted run says nothing about the task
	}
	if err != nil {
		w.logf("worker: %s: %v", name, err)
		_, recErr := w.app.DB.Exec(ctx, `INSERT INTO worker_tasks (task, last_started_at, last_duration_ms, last_count,
				last_error, last_error_at, runs, failures) VALUES ($1,$2,$3,$4,$5,now(),1,1)
			ON CONFLICT (task) DO UPDATE SET last_started_at=$2, last_duration_ms=$3, last_count=$4, last_error=$5,
				last_error_at=now(), runs=worker_tasks.runs+1, failures=worker_tasks.failures+1`,
			name, start, ms, n, truncate(err.Error(), 500))
		if recErr != nil {
			w.logf("worker: record %s: %v", name, recErr)
		}
		return
	}
	if _, recErr := w.app.DB.Exec(ctx, `INSERT INTO worker_tasks (task, last_started_at, last_ok_at, last_duration_ms, last_count, runs)
			VALUES ($1,$2,now(),$3,$4,1)
		ON CONFLICT (task) DO UPDATE SET last_started_at=$2, last_ok_at=now(), last_duration_ms=$3, last_count=$4,
			runs=worker_tasks.runs+1`, name, start, ms, n); recErr != nil {
		w.logf("worker: record %s: %v", name, recErr)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Tick runs every task that is due once and then reports the heartbeat.
func (w *Worker) Tick(ctx context.Context) {
	start := time.Now()
	a := w.app
	w.runTask(ctx, "expire_pending", func(ctx context.Context) (int, error) {
		n, err := a.ExpirePendingOrders(ctx)
		if n > 0 {
			w.logf("worker: cancelled %d orders that were not accepted in time", n)
		}
		return n, err
	})
	w.runTask(ctx, "expire_addons", a.ExpireAddons)
	w.runTask(ctx, "release_scheduled", a.ReleaseScheduled)
	w.runTask(ctx, "jobs", a.runJobs)
	if time.Since(w.lastTrips) > tripInterval {
		w.runTask(ctx, "rider_trips", a.AdvanceTrips)
		w.lastTrips = time.Now()
	}
	if time.Since(w.lastCleanup) > cleanupInterval {
		w.runTask(ctx, "cleanup", func(ctx context.Context) (int, error) { return 0, a.Cleanup(ctx) })
		w.lastCleanup = time.Now()
	}
	if ctx.Err() != nil {
		return
	}
	if _, err := a.DB.Exec(ctx, `INSERT INTO worker_instances (instance, hostname, embedded, started_at, last_beat_at, last_loop_ms)
			VALUES ($1,$2,$3,$4,now(),$5)
		ON CONFLICT (instance) DO UPDATE SET last_beat_at=now(), last_loop_ms=$5`,
		w.instance, w.hostname, w.embedded, w.startedAt, int(time.Since(start).Milliseconds())); err != nil {
		w.logf("worker: heartbeat: %v", err)
	}
}

// Stop removes the heartbeat row, so that a worker that was shut down on
// purpose does not show as lost.
func (w *Worker) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := w.app.DB.Exec(ctx, `DELETE FROM worker_instances WHERE instance=$1`, w.instance); err != nil {
		w.logf("worker: sign off: %v", err)
	}
}

// RunWorker loops until ctx is cancelled.
func (a *App) RunWorker(ctx context.Context, logf func(string, ...any), embedded bool) {
	w := a.NewWorker(logf, embedded)
	defer w.Stop()
	tick := time.NewTicker(workerTick)
	defer tick.Stop()
	for {
		w.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
