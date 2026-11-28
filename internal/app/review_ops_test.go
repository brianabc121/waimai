package app_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brianabc121/waimai/internal/app"
)

// slowSMS takes a while to send, like a long translation job does.
type slowSMS struct {
	took  time.Duration
	sends atomic.Int32
}

func (s *slowSMS) Send(ctx context.Context, _, _ string) error {
	s.sends.Add(1)
	select {
	case <-time.After(s.took):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// M10: a job that runs longer than its lease keeps renewing it, so a second
// worker does not take it for abandoned and run it again.
func TestLongJobKeepsItsLease(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	_, err := a.DB.Exec(ctx, `DELETE FROM jobs WHERE status='pending'`)
	noErr(t, err)
	slow := &slowSMS{took: 2500 * time.Millisecond}
	old := a.SMS
	a.SMS, a.JobLease, a.JobRenewEvery = slow, time.Second, 300*time.Millisecond
	t.Cleanup(func() { a.SMS, a.JobLease, a.JobRenewEvery = old, 0, 0 })
	id := addJob(t, "+639171230010", "long", "pending", 0)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.RunJobs(ctx); err != nil {
			t.Errorf("first worker: %v", err)
		}
	}()
	// well past the one-second lease: without renewal the job is up for grabs
	time.Sleep(1600 * time.Millisecond)
	noErr(t, a.RunJobs(ctx))
	wg.Wait()
	if n := slow.sends.Load(); n != 1 {
		t.Fatalf("the long job ran %d times, want once", n)
	}
	var status string
	var attempts int
	noErr(t, a.DB.QueryRow(ctx, `SELECT status, attempts FROM jobs WHERE id=$1`, id).Scan(&status, &attempts))
	if status != "done" || attempts != 1 {
		t.Fatalf("job: %s after %d attempts", status, attempts)
	}
}

// M11: with every connection of the pool held by a transaction and the cache
// of system texts cold, accepting orders still goes through: the text is read
// on the transaction's own connection.
func TestNotifyInsideTransactionNeedsNoSecondConnection(t *testing.T) {
	need(t)
	s := newStore(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := testApp.SaveSettings(ctx, s.branchID, app.SettingsInput{Name: "T", TaxRateBP: 1200, TaxInclusive: true,
		AcceptTimeoutMinutes: 10, DeliveryEnabled: true, PickupEnabled: true, OpenTime: "00:00", CloseTime: "00:00", NotifySMS: true})
	noErr(t, err)
	o1 := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})
	o2 := s.place(t, "pickup", []app.QuoteLineInput{{ItemID: s.water, Qty: 1}})

	url := os.Getenv("TEST_DATABASE_URL")
	if strings.Contains(url, "?") {
		url += "&pool_max_conns=2"
	} else {
		url += "?pool_max_conns=2"
	}
	pool, err := pgxpool.New(ctx, url)
	noErr(t, err)
	defer pool.Close()
	small := app.New(pool, "test-secret") // a fresh App: its text cache is cold

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []int64{o1.ID, o2.ID} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = small.Accept(ctx, id, 0, s.staff)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("accept %d with a pool of two: %v", i, err)
		}
	}
	var n int
	noErr(t, testApp.DB.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='sms' AND created_at > now() - interval '1 minute'
		AND payload->>'phone' = $1`, o1.CustomerPhone).Scan(&n))
	if n < 2 {
		t.Fatalf("texts queued: %d, want one per accepted order", n)
	}
}
