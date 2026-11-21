package app

import "testing"

func TestHistIndex(t *testing.T) {
	for ms, want := range map[int]int{0: 0, 5: 0, 6: 1, 10: 1, 11: 2, 100: 4, 101: 5, 5000: 9, 5001: 10, 90000: 10} {
		if got := histIndex(ms); got != want {
			t.Errorf("histIndex(%d) = %d, want %d", ms, got, want)
		}
	}
}

func TestHealthRules(t *testing.T) {
	api := []struct {
		requests, c5 int64
		p95          int
		want         string
	}{
		{0, 0, 0, "ok"},          // no traffic is not a problem
		{1000, 9, 400, "ok"},     // 0.9% errors
		{1000, 10, 400, "warn"},  // 1%
		{1000, 50, 400, "warn"},  // exactly 5% is still a warning
		{1000, 51, 400, "bad"},   // above 5%
		{1000, 0, 999, "ok"},     // just under a second
		{1000, 0, 1000, "warn"},  // a second
		{1000, 0, 3000, "warn"},  // exactly three seconds
		{1000, 0, 3001, "bad"},   // above
		{1000, 60, 1500, "bad"},  // the worse of the two wins
		{1000, 20, 3500, "bad"},  // either can make it bad
		{1000, 20, 1500, "warn"}, // both warn
	}
	for _, c := range api {
		if got, _ := apiHealth(c.requests, c.c5, c.p95); got != c.want {
			t.Errorf("apiHealth(%d, %d, %d) = %s, want %s", c.requests, c.c5, c.p95, got, c.want)
		}
	}
	queue := []struct {
		failed, wait int
		want         string
	}{{0, 0, "ok"}, {0, 59, "ok"}, {0, 60, "warn"}, {1, 0, "warn"}, {0, 600, "warn"}, {0, 601, "bad"}, {3, 601, "bad"}}
	for _, c := range queue {
		if got := queueHealth(c.failed, c.wait); got != c.want {
			t.Errorf("queueHealth(%d, %d) = %s, want %s", c.failed, c.wait, got, c.want)
		}
	}
}
