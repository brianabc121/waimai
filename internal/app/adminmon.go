package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// What the back office shows about the worker and the job queue.

type WorkerInstance struct {
	Instance   string    `json:"instance"`
	Hostname   string    `json:"hostname"`
	Embedded   bool      `json:"embedded"`
	StartedAt  time.Time `json:"started_at"`
	LastBeatAt time.Time `json:"last_beat_at"`
	LastLoopMS int       `json:"last_loop_ms"`
	Online     bool      `json:"online"`
}

type WorkerTask struct {
	Task            string     `json:"task"`
	IntervalSeconds int        `json:"interval_seconds"`
	LastStartedAt   *time.Time `json:"last_started_at"`
	LastOKAt        *time.Time `json:"last_ok_at"`
	LastDurationMS  int        `json:"last_duration_ms"`
	LastCount       int        `json:"last_count"`
	LastError       string     `json:"last_error"`
	LastErrorAt     *time.Time `json:"last_error_at"`
	Runs            int64      `json:"runs"`
	Failures        int64      `json:"failures"`
	// OK is false when the most recent run failed. A task that never ran is OK.
	OK bool `json:"ok"`
}

type QueueKind struct {
	Kind    string `json:"kind"`
	Pending int    `json:"pending"`
	Running int    `json:"running"`
	Failed  int    `json:"failed"`
	Done24h int    `json:"done_24h"`
}

type QueueStats struct {
	Pending int `json:"pending"`
	Running int `json:"running"`
	Failed  int `json:"failed"`
	Done24h int `json:"done_24h"`
	// OldestWaitSeconds is how long the oldest due job has been waiting to start.
	OldestWaitSeconds int         `json:"oldest_wait_seconds"`
	ByKind            []QueueKind `json:"by_kind"`
}

type WorkerStatus struct {
	Now       time.Time        `json:"now"`
	Instances []WorkerInstance `json:"instances"`
	Tasks     []WorkerTask     `json:"tasks"`
	Queue     QueueStats       `json:"queue"`
}

// jobState is the status shown to administrators: a pending job whose lease
// is still valid is running.
const jobState = `CASE WHEN j.status = 'pending' AND j.locked_until > now() THEN 'running' ELSE j.status END`

func (a *App) WorkerStatus(ctx context.Context) (*WorkerStatus, error) {
	out := &WorkerStatus{Now: time.Now(), Instances: []WorkerInstance{}, Tasks: []WorkerTask{}, Queue: QueueStats{ByKind: []QueueKind{}}}

	rows, err := a.DB.Query(ctx, `SELECT instance, hostname, embedded, started_at, last_beat_at, last_loop_ms,
		last_beat_at > now() - make_interval(secs => $1) FROM worker_instances ORDER BY started_at DESC`, workerOffline.Seconds())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var w WorkerInstance
		if err := rows.Scan(&w.Instance, &w.Hostname, &w.Embedded, &w.StartedAt, &w.LastBeatAt, &w.LastLoopMS, &w.Online); err != nil {
			rows.Close()
			return nil, err
		}
		out.Instances = append(out.Instances, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	seen := map[string]WorkerTask{}
	rows, err = a.DB.Query(ctx, `SELECT task, last_started_at, last_ok_at, last_duration_ms, last_count, last_error,
		last_error_at, runs, failures FROM worker_tasks`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t WorkerTask
		if err := rows.Scan(&t.Task, &t.LastStartedAt, &t.LastOKAt, &t.LastDurationMS, &t.LastCount, &t.LastError,
			&t.LastErrorAt, &t.Runs, &t.Failures); err != nil {
			rows.Close()
			return nil, err
		}
		seen[t.Task] = t
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, def := range workerTasks {
		t := seen[def.Name]
		t.Task, t.IntervalSeconds = def.Name, int(def.Interval.Seconds())
		t.OK = t.LastErrorAt == nil || (t.LastOKAt != nil && t.LastOKAt.After(*t.LastErrorAt))
		if t.OK {
			t.LastError = ""
		}
		out.Tasks = append(out.Tasks, t)
	}

	rows, err = a.DB.Query(ctx, `SELECT j.kind,
		count(*) FILTER (WHERE `+jobState+` = 'pending'), count(*) FILTER (WHERE `+jobState+` = 'running'),
		count(*) FILTER (WHERE j.status = 'failed'),
		count(*) FILTER (WHERE j.status = 'done' AND j.finished_at > now() - interval '24 hours')
		FROM jobs j GROUP BY j.kind ORDER BY j.kind`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k QueueKind
		if err := rows.Scan(&k.Kind, &k.Pending, &k.Running, &k.Failed, &k.Done24h); err != nil {
			rows.Close()
			return nil, err
		}
		out.Queue.ByKind = append(out.Queue.ByKind, k)
		out.Queue.Pending += k.Pending
		out.Queue.Running += k.Running
		out.Queue.Failed += k.Failed
		out.Queue.Done24h += k.Done24h
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	err = a.DB.QueryRow(ctx, `SELECT coalesce(extract(epoch FROM now() - min(run_at)), 0)::int FROM jobs j
		WHERE j.status = 'pending' AND j.run_at <= now() AND (j.locked_until IS NULL OR j.locked_until < now())`).
		Scan(&out.Queue.OldestWaitSeconds)
	return out, err
}

type JobRow struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"` // pending, running, done, failed, discarded
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	Subject     string     `json:"subject"`
	RunAt       time.Time  `json:"run_at"`
	CreatedAt   time.Time  `json:"created_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	LastError   string     `json:"last_error"`
	// Message is the text of an SMS job. It is only filled in by GetJob.
	Message string `json:"message,omitempty"`
}

// maskPhone keeps the country code, the first digits and the last two:
// "+639171234589" becomes "+63917•••••89".
func maskPhone(p string) string {
	p = strings.TrimSpace(p)
	if len(p) < 9 {
		return "••••"
	}
	return p[:6] + strings.Repeat("•", len(p)-8) + p[len(p)-2:]
}

var orderLinkRe = regexp.MustCompile(`/o/[A-Za-z0-9_-]+`)

// describeJob fills in what a job is about without exposing personal data:
// phone numbers are masked and the customer's order link is cut.
func describeJob(j *JobRow, payload []byte, withMessage bool) {
	j.MaxAttempts = jobMaxAttempts
	switch j.Kind {
	case "sms":
		var p smsJob
		if json.Unmarshal(payload, &p) == nil {
			j.Subject = maskPhone(p.Phone)
			if withMessage {
				j.Message = orderLinkRe.ReplaceAllString(p.Message, "/o/…")
			}
		}
	case "menu_import":
		var p struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(payload, &p) == nil && p.ID != 0 {
			j.Subject = "#" + strconv.FormatInt(p.ID, 10)
		}
	}
}

type JobFilter struct {
	Status string // "", pending, running, done, failed, discarded
	Kind   string
	Page
}

type JobList struct {
	Items []JobRow `json:"items"`
	Total int      `json:"total"`
}

const jobCols = `j.id, j.kind, ` + jobState + `, j.attempts, j.run_at, j.created_at, j.finished_at, j.last_error, j.payload`

func scanJob(row interface{ Scan(...any) error }, withMessage bool) (*JobRow, error) {
	var j JobRow
	var payload []byte
	err := row.Scan(&j.ID, &j.Kind, &j.Status, &j.Attempts, &j.RunAt, &j.CreatedAt, &j.FinishedAt, &j.LastError, &payload)
	if isNoRows(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	describeJob(&j, payload, withMessage)
	return &j, nil
}

func (a *App) ListJobs(ctx context.Context, f JobFilter) (*JobList, error) {
	switch f.Status {
	case "", "pending", "running", "done", "failed", "discarded":
	default:
		return nil, ErrValidation("unknown status")
	}
	const where = ` FROM jobs j WHERE ($1 = '' OR ` + jobState + ` = $1) AND ($2 = '' OR j.kind = $2)`
	out := &JobList{Items: []JobRow{}}
	if err := a.DB.QueryRow(ctx, `SELECT count(*)`+where, f.Status, f.Kind).Scan(&out.Total); err != nil {
		return nil, err
	}
	limit, offset := f.limitOffset()
	rows, err := a.DB.Query(ctx, `SELECT `+jobCols+where+` ORDER BY j.id DESC LIMIT $3 OFFSET $4`, f.Status, f.Kind, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		j, err := scanJob(rows, false)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *j)
	}
	return out, rows.Err()
}

// GetJob returns one job with the text of its message.
func (a *App) GetJob(ctx context.Context, id int64) (*JobRow, error) {
	return scanJob(a.DB.QueryRow(ctx, `SELECT `+jobCols+` FROM jobs j WHERE j.id=$1`, id), true)
}

// RetryJob puts a failed or discarded job back in the queue with a fresh set of attempts.
func (a *App) RetryJob(ctx context.Context, actor AdminActor, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var kind, status string
		err := tx.QueryRow(ctx, `SELECT kind, status FROM jobs WHERE id=$1 FOR UPDATE`, id).Scan(&kind, &status)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "failed" && status != "discarded" {
			return ErrInvalidState("only failed or discarded jobs can be retried")
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET status='pending', attempts=0, run_at=now(), locked_until=NULL,
			lease_token=NULL, finished_at=NULL WHERE id=$1`, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "job.retry", "job", id, map[string]any{"kind": kind})
	})
}

// RetryFailedJobs retries every failed job, optionally of one kind, and returns how many.
func (a *App) RetryFailedJobs(ctx context.Context, actor AdminActor, kind string) (int, error) {
	n := 0
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE jobs SET status='pending', attempts=0, run_at=now(), locked_until=NULL,
			lease_token=NULL, finished_at=NULL WHERE status='failed' AND ($1 = '' OR kind = $1)`, kind)
		if err != nil {
			return err
		}
		if n = int(tag.RowsAffected()); n == 0 {
			return nil
		}
		return adminAudit(ctx, tx, actor, "job.retry_failed", "job", 0, map[string]any{"kind": kind, "count": n})
	})
	return n, err
}

// DiscardJob gives up on a job that failed or is still waiting. A job that a
// worker is running right now cannot be discarded.
func (a *App) DiscardJob(ctx context.Context, actor AdminActor, id int64) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var kind, state string
		err := tx.QueryRow(ctx, `SELECT j.kind, `+jobState+` FROM jobs j WHERE j.id=$1 FOR UPDATE`, id).Scan(&kind, &state)
		if isNoRows(err) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if state != "failed" && state != "pending" {
			return ErrInvalidState("only failed or waiting jobs can be discarded")
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET status='discarded', finished_at=now(), locked_until=NULL,
			lease_token=NULL WHERE id=$1`, id); err != nil {
			return err
		}
		return adminAudit(ctx, tx, actor, "job.discard", "job", id, map[string]any{"kind": kind})
	})
}
