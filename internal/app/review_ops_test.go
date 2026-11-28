package app_test

import (
	"context"
	"fmt"
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

// Side boundary 1 of the review: a newer verification code ends the older
// ones (an old code does not work again after the new one was used), and the
// daily cap per address holds when many numbers ask at the same moment.
func TestOTPOldCodeAndAddressCap(t *testing.T) {
	a := need(t)
	ctx := context.Background()
	phone := fmt.Sprintf("+63917%07d", time.Now().UnixNano()%1e7)
	old, err := a.RequestOTP(ctx, phone, "")
	noErr(t, err)
	// the minute between two codes has passed
	_, err = a.DB.Exec(ctx, `UPDATE phone_verifications SET created_at = created_at - interval '2 minutes' WHERE phone=$1`, phone)
	noErr(t, err)
	fresh, err := a.RequestOTP(ctx, phone, "")
	noErr(t, err)
	if old == fresh {
		t.Skip("the two random codes happen to be equal")
	}
	_, _, err = a.VerifyOTP(ctx, phone, old)
	wantCode(t, err, "OTP_INVALID")
	_, _, err = a.VerifyOTP(ctx, phone, fresh)
	noErr(t, err)
	_, _, err = a.VerifyOTP(ctx, phone, old) // the new one is used: the old one stays dead
	wantCode(t, err, "OTP_INVALID")

	// 40 numbers at once from one address that has 3 codes left today
	echo := a.OTPDevEcho
	a.OTPDevEcho = false
	t.Cleanup(func() { a.OTPDevEcho = echo })
	ip := "198.51.100." + fmt.Sprint(time.Now().UnixNano()%250+1)
	_, err = a.DB.Exec(ctx, `DELETE FROM phone_verifications WHERE ip=$1`, ip)
	noErr(t, err)
	_, err = a.DB.Exec(ctx, `INSERT INTO phone_verifications (phone, code_hash, expires_at, ip, consumed_at)
		SELECT '+6390000' || lpad(g::text, 5, '0'), '\x00', now(), $1, now() FROM generate_series(1, $2::int) g`, ip, app.OTPDailyPerIP-3)
	noErr(t, err)
	var wg sync.WaitGroup
	var sent atomic.Int32
	base := time.Now().UnixNano() % 9e6
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.RequestOTP(ctx, fmt.Sprintf("+63918%07d", base+int64(i)), ip); err == nil {
				sent.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := sent.Load(); n != 3 {
		t.Fatalf("%d codes went out from an address with 3 left", n)
	}
}
