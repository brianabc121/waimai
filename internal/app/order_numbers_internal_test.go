package app

import (
	"testing"
	"time"
)

// The business day turns at the store's reset time, in the store's time zone.
func TestBusinessDay(t *testing.T) {
	b := &Branch{Timezone: "Asia/Manila", DayResetTime: "04:00"}
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct{ at, reset, want string }{
		{"2026-11-26T01:30:00+08:00", "04:00", "2026-11-25"}, // after midnight, before the reset
		{"2026-11-26T04:00:00+08:00", "04:00", "2026-11-26"},
		{"2026-11-26T23:59:00+08:00", "04:00", "2026-11-26"},
		{"2026-11-26T00:10:00+08:00", "00:00", "2026-11-26"},
		{"2026-11-25T17:30:00Z", "02:00", "2026-11-25"}, // 01:30 on the 26th in Manila, before 02:00
		{"2026-11-26T07:59:00+08:00", "08:00", "2026-11-25"},
		{"2026-11-26T05:00:00+08:00", "", "2026-11-26"}, // unset: 04:00
	} {
		b.DayResetTime = c.reset
		if got := b.businessDay(at(c.at)).Format("2006-01-02"); got != c.want {
			t.Errorf("%s reset %s: got %s, want %s", c.at, c.reset, got, c.want)
		}
	}
}
