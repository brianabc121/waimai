package app

import (
	"testing"
	"time"
)

func TestBuildTrack(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	type fix struct {
		east, north float64
		sec         int
		acc         float32
	}
	build := func(in []fix) Track {
		pts := make([]trackPoint, len(in))
		acc := make([]*float32, len(in))
		for i, f := range in {
			pts[i] = trackPoint{at(f.east, f.north), t0.Add(time.Duration(f.sec) * time.Second)}
			if f.acc > 0 {
				a := f.acc
				acc[i] = &a
			}
		}
		return buildTrack(cleanFixes(pts, acc))
	}
	near := func(got, want, tol int) bool { return got >= want-tol && got <= want+tol }

	if tr := build(nil); tr.DistanceM != 0 || tr.Fixes != 0 || len(tr.Points) != 0 {
		t.Fatalf("empty: %+v", tr)
	}

	// A straight kilometre north in ten steps: the points in between add no shape.
	var line []fix
	for i := 0; i <= 10; i++ {
		line = append(line, fix{0, float64(i) * 100, i * 15, 10})
	}
	tr := build(line)
	if !near(tr.DistanceM, 1000, 5) || len(tr.Points) != 2 || len(tr.Gaps) != 0 || tr.Fixes != 11 {
		t.Fatalf("line: %+v", tr)
	}

	// Standing still for ten minutes with the phone jittering adds nothing,
	// and is not a gap: fixes kept coming.
	still := []fix{{0, 0, 0, 10}}
	for i := 1; i <= 10; i++ {
		still = append(still, fix{float64(i%3) * 8, float64(i%2) * 9, i * 60, 10})
	}
	still = append(still, fix{0, 400, 660, 10})
	tr = build(still)
	if !near(tr.DistanceM, 400, 5) || len(tr.Gaps) != 0 || tr.GapSeconds != 0 {
		t.Fatalf("standing still: %+v", tr)
	}
	// the path says how long they stood there
	if len(tr.Points) != 3 || tr.Points[1].At.Sub(t0) != 600*time.Second {
		t.Fatalf("standing still path: %+v", tr.Points)
	}

	// A poor fix and a fix 5 km away within 15 seconds are thrown out.
	tr = build([]fix{{0, 0, 0, 10}, {0, 100, 15, 10}, {900, 900, 30, 500}, {5000, 0, 45, 10}, {0, 200, 60, 10}})
	if !near(tr.DistanceM, 200, 5) || tr.Fixes != 3 {
		t.Fatalf("strays: %+v", tr)
	}
	// ...but when the fixes keep disagreeing with one, that one was the stray.
	tr = build([]fix{{5000, 0, 0, 10}, {0, 0, 15, 10}, {0, 100, 30, 10}, {0, 200, 45, 10}, {0, 300, 60, 10}})
	if !near(tr.DistanceM, 100, 5) || tr.Fixes != 2 {
		t.Fatalf("stray first: %+v", tr)
	}

	// Five minutes without a fix and a kilometre on: a gap, measured straight.
	tr = build([]fix{{0, 0, 0, 10}, {0, 100, 15, 10}, {0, 200, 30, 10}, {1000, 200, 330, 10}, {1000, 300, 345, 10}})
	if !near(tr.DistanceM, 1300, 5) || len(tr.Gaps) != 1 || tr.GapSeconds != 300 {
		t.Fatalf("gap: %+v", tr)
	}
	if g := tr.Gaps[0]; metresBetween(tr.Points[g].Pos, tr.Points[g+1].Pos) < 990 {
		t.Fatalf("the gap is not where the fixes stopped: %+v", tr)
	}
	// A slow five minutes over 100 m is waiting at a gate, not a gap.
	tr = build([]fix{{0, 0, 0, 10}, {0, 100, 300, 10}})
	if len(tr.Gaps) != 0 {
		t.Fatalf("slow: %+v", tr)
	}

	// A turn keeps its corner.
	tr = build([]fix{{0, 0, 0, 10}, {0, 250, 30, 10}, {0, 500, 60, 10}, {250, 500, 90, 10}, {500, 500, 120, 10}})
	if len(tr.Points) != 3 || !near(tr.DistanceM, 1000, 5) {
		t.Fatalf("corner: %+v", tr)
	}
}
