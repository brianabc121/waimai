package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/brianabc121/waimai/internal/app"
)

func addJob(t *testing.T, phone, msg, status string, attempts int) int64 {
	t.Helper()
	var id int64
	noErr(t, testApp.DB.QueryRow(context.Background(), `INSERT INTO jobs (kind, payload, status, attempts, last_error, finished_at)
		VALUES ('sms', jsonb_build_object('phone', $1::text, 'message', $2::text), $3, $4,
			CASE WHEN $3 = 'failed' THEN 'provider down' ELSE '' END, CASE WHEN $3 = 'pending' THEN NULL ELSE now() END)
		RETURNING id`, phone, msg, status, attempts).Scan(&id))
	return id
}

func TestWorkerHeartbeatAndTasks(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	fake := &fakeSMS{}
	old := a.SMS
	a.SMS = fake
	t.Cleanup(func() { a.SMS = old })
	_, err := a.DB.Exec(ctx, `DELETE FROM jobs WHERE status='pending'`)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `DELETE FROM worker_tasks`)
	noErr(t, err)

	// before the first run every task is listed and counts as fine
	st, err := a.WorkerStatus(ctx)
	noErr(t, err)
	if len(st.Tasks) != 6 || st.Tasks[0].Task != "expire_pending" || st.Tasks[5].IntervalSeconds != 600 {
		t.Fatalf("tasks: %+v", st.Tasks)
	}
	for _, task := range st.Tasks {
		if !task.OK || task.Runs != 0 || task.LastStartedAt != nil {
			t.Fatalf("task before first run: %+v", task)
		}
	}

	phone := "+639171234589"
	addJob(t, phone, "hello", "pending", 0)
	w := a.NewWorker(t.Logf, false)
	w.Tick(ctx)
	if n := len(fake.forPhone(phone)); n != 1 {
		t.Fatalf("the tick should have sent the message, sent %d", n)
	}
	st, err = a.WorkerStatus(ctx)
	noErr(t, err)
	find := func(st *app.WorkerStatus, name string) app.WorkerTask {
		for _, task := range st.Tasks {
			if task.Task == name {
				return task
			}
		}
		t.Fatalf("no task %s", name)
		return app.WorkerTask{}
	}
	jobs := find(st, "jobs")
	if !jobs.OK || jobs.Runs != 1 || jobs.LastCount != 1 || jobs.LastOKAt == nil || jobs.Failures != 0 {
		t.Fatalf("jobs task after one tick: %+v", jobs)
	}
	if c := find(st, "cleanup"); c.Runs != 1 {
		t.Fatalf("cleanup runs on the first tick: %+v", c)
	}
	inst := func(st *app.WorkerStatus) *app.WorkerInstance {
		for i := range st.Instances {
			if st.Instances[i].Instance == w.Instance() {
				return &st.Instances[i]
			}
		}
		return nil
	}
	if me := inst(st); me == nil || !me.Online || me.Embedded {
		t.Fatalf("heartbeat: %+v", me)
	}

	// cleanup is not due again on the next tick, the others are
	w.Tick(ctx)
	st, err = a.WorkerStatus(ctx)
	noErr(t, err)
	if c, j := find(st, "cleanup"), find(st, "jobs"); c.Runs != 1 || j.Runs != 2 || j.LastCount != 0 {
		t.Fatalf("second tick: cleanup %+v jobs %+v", c, j)
	}

	// a failing task is reported until it succeeds again
	_, err = a.DB.Exec(ctx, `UPDATE worker_tasks SET last_error='boom', last_error_at=now() + interval '1 second',
		failures=1 WHERE task='jobs'`)
	noErr(t, err)
	st, err = a.WorkerStatus(ctx)
	noErr(t, err)
	if j := find(st, "jobs"); j.OK || j.LastError != "boom" {
		t.Fatalf("failed task: %+v", j)
	}
	_, err = a.DB.Exec(ctx, `UPDATE worker_tasks SET last_ok_at=now() + interval '2 seconds' WHERE task='jobs'`)
	noErr(t, err)
	st, err = a.WorkerStatus(ctx)
	noErr(t, err)
	if j := find(st, "jobs"); !j.OK || j.LastError != "" || j.Failures != 1 {
		t.Fatalf("recovered task: %+v", j)
	}

	// a worker that stopped beating shows as offline, one that signs off disappears
	_, err = a.DB.Exec(ctx, `UPDATE worker_instances SET last_beat_at = now() - interval '30 seconds' WHERE instance=$1`, w.Instance())
	noErr(t, err)
	st, err = a.WorkerStatus(ctx)
	noErr(t, err)
	if me := inst(st); me == nil || me.Online {
		t.Fatalf("silent worker should be offline: %+v", me)
	}
	w.Stop()
	st, err = a.WorkerStatus(ctx)
	noErr(t, err)
	if me := inst(st); me != nil {
		t.Fatalf("signed-off worker still listed: %+v", me)
	}
}

func TestAdminJobs(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	actor := adminActor(t)
	fake := &fakeSMS{}
	old := a.SMS
	a.SMS = fake
	t.Cleanup(func() { a.SMS = old })
	_, err := a.DB.Exec(ctx, `DELETE FROM jobs WHERE status IN ('pending','failed')`)
	noErr(t, err)

	phone := "+639171234589"
	failed := addJob(t, phone, "【T】A001 商家已接单 https://example.test/o/abcDEF_123-x", "failed", 5)
	waiting := addJob(t, "+639170000777", "later", "pending", 0)
	_, err = a.DB.Exec(ctx, `UPDATE jobs SET run_at = now() + interval '1 hour' WHERE id=$1`, waiting)
	noErr(t, err)
	running := addJob(t, "+639170000888", "now", "pending", 1)
	_, err = a.DB.Exec(ctx, `UPDATE jobs SET locked_until = now() + interval '1 minute', lease_token = gen_random_uuid() WHERE id=$1`, running)
	noErr(t, err)

	st, err := a.WorkerStatus(ctx)
	noErr(t, err)
	if st.Queue.Failed != 1 || st.Queue.Pending != 1 || st.Queue.Running != 1 || st.Queue.OldestWaitSeconds != 0 {
		t.Fatalf("queue: %+v", st.Queue)
	}

	// lists never carry the full number or the message
	list, err := a.ListJobs(ctx, app.JobFilter{Status: "failed", Kind: "sms"})
	noErr(t, err)
	if list.Total != 1 || list.Items[0].ID != failed || list.Items[0].Subject != "+63917•••••89" ||
		list.Items[0].Message != "" || list.Items[0].LastError != "provider down" || list.Items[0].MaxAttempts != 5 {
		t.Fatalf("failed list: %+v", list)
	}
	list, err = a.ListJobs(ctx, app.JobFilter{Status: "running"})
	noErr(t, err)
	if list.Total != 1 || list.Items[0].ID != running {
		t.Fatalf("running list: %+v", list)
	}
	_, err = a.ListJobs(ctx, app.JobFilter{Status: "nope"})
	wantCode(t, err, "VALIDATION")
	// the detail shows the text with the customer's order link cut
	j, err := a.GetJob(ctx, failed)
	noErr(t, err)
	if !strings.Contains(j.Message, "商家已接单") || strings.Contains(j.Message, "abcDEF") || !strings.Contains(j.Message, "/o/…") {
		t.Fatalf("message: %q", j.Message)
	}
	_, err = a.GetJob(ctx, 999999999)
	wantCode(t, err, "NOT_FOUND")

	// only failed or waiting jobs can be discarded, only failed or discarded ones retried
	wantCode(t, a.DiscardJob(ctx, actor, running), "INVALID_STATE")
	wantCode(t, a.RetryJob(ctx, actor, waiting), "INVALID_STATE")
	wantCode(t, a.RetryJob(ctx, actor, 999999999), "NOT_FOUND")

	// a discarded job is never sent
	before := auditCount(t, "job.discard")
	noErr(t, a.DiscardJob(ctx, actor, waiting))
	_, err = a.DB.Exec(ctx, `UPDATE jobs SET run_at = now() WHERE id=$1`, waiting)
	noErr(t, err)
	noErr(t, a.RunJobs(ctx))
	if n := len(fake.forPhone("+639170000777")); n != 0 {
		t.Fatalf("discarded job was sent %d times", n)
	}
	if auditCount(t, "job.discard") != before+1 {
		t.Fatal("discard was not audited")
	}

	// retrying starts over and the worker picks the job up
	noErr(t, a.RetryJob(ctx, actor, failed))
	j, err = a.GetJob(ctx, failed)
	noErr(t, err)
	if j.Status != "pending" || j.Attempts != 0 || j.FinishedAt != nil {
		t.Fatalf("retried job: %+v", j)
	}
	noErr(t, a.RunJobs(ctx))
	if n := len(fake.forPhone(phone)); n != 1 {
		t.Fatalf("retried job sent %d times", n)
	}
	j, err = a.GetJob(ctx, failed)
	noErr(t, err)
	if j.Status != "done" || j.Attempts != 1 {
		t.Fatalf("after retry: %+v", j)
	}

	// a discarded job can be brought back; retry-all takes every failed one
	noErr(t, a.RetryJob(ctx, actor, waiting))
	noErr(t, a.DiscardJob(ctx, actor, waiting))
	f1 := addJob(t, "+639170000901", "x", "failed", 5)
	f2 := addJob(t, "+639170000902", "y", "failed", 5)
	n, err := a.RetryFailedJobs(ctx, actor, "menu_import")
	noErr(t, err)
	if n != 0 {
		t.Fatalf("no failed imports, retried %d", n)
	}
	n, err = a.RetryFailedJobs(ctx, actor, "")
	noErr(t, err)
	if n != 2 {
		t.Fatalf("retry all: %d", n)
	}
	noErr(t, a.RunJobs(ctx))
	for _, id := range []int64{f1, f2} {
		j, err = a.GetJob(ctx, id)
		noErr(t, err)
		if j.Status != "done" {
			t.Fatalf("job %d after retry-all: %s", id, j.Status)
		}
	}
	_, err = a.DB.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, running)
	noErr(t, err)
}
