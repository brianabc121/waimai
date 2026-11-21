package app

import (
	"errors"
	"testing"
)

// A square of about 1.1 km around a point in Makati, and helpers to move
// around it in metres.
var geoOrigin = LatLng{14.55, 121.02}

func at(eastM, northM float64) LatLng {
	pl := newPlanar(geoOrigin)
	return LatLng{geoOrigin[0] + northM/metresPerDeg, geoOrigin[1] + eastM/(metresPerDeg*pl.kx)}
}

func square(x0, y0, x1, y1 float64) Polygon {
	return Polygon{at(x0, y0), at(x1, y0), at(x1, y1), at(x0, y1)}
}

func TestMatchZone(t *testing.T) {
	// a (0..1000) and b (1050..2000) leave a 50 m gap; c overlaps a's right half.
	a := Zone{ID: 1, Name: "a", Polygon: square(0, 0, 1000, 1000)}
	b := Zone{ID: 2, Name: "b", Polygon: square(1050, 0, 2000, 1000)}
	c := Zone{ID: 3, Name: "c", Polygon: square(500, 0, 1000, 1000)}
	// a notch cut into the top edge: (400..600, 500..1000) is outside.
	notch := Zone{ID: 4, Name: "notch", Polygon: Polygon{at(0, 0), at(1000, 0), at(1000, 1000), at(600, 1000),
		at(600, 500), at(400, 500), at(400, 1000), at(0, 1000)}}
	undrawn := Zone{ID: 5, Name: "undrawn"}

	cases := []struct {
		name  string
		zones []Zone
		p     LatLng
		tol   int
		want  string // zone name, "" = out of range
		dist  int
	}{
		{"inside", []Zone{a, b}, at(500, 500), 200, "a", 0},
		{"on the edge", []Zone{a, b}, at(1000, 500), 0, "a", 0},
		{"on a corner", []Zone{a, b}, at(0, 0), 0, "a", 0},
		{"gap, nearer to a", []Zone{a, b}, at(1010, 500), 200, "a", 10},
		{"gap, nearer to b", []Zone{a, b}, at(1040, 500), 200, "b", 10},
		{"gap, same distance: the earlier zone", []Zone{a, b}, at(1025, 500), 200, "a", 25},
		{"gap, same distance, other order", []Zone{b, a}, at(1025, 500), 200, "b", 25},
		{"gap with zero tolerance", []Zone{a, b}, at(1025, 500), 0, "", 0},
		{"199 m outside", []Zone{a}, at(-199, 500), 200, "a", 199},
		{"201 m outside", []Zone{a}, at(-201, 500), 200, "", 0},
		{"overlap: first in order wins", []Zone{a, c}, at(750, 500), 200, "a", 0},
		{"overlap: reordered", []Zone{c, a}, at(750, 500), 200, "c", 0},
		{"inside beats a nearer earlier zone", []Zone{b, a}, at(990, 500), 200, "a", 0},
		{"inside a notch is outside", []Zone{notch}, at(500, 900), 0, "", 0},
		{"notch within tolerance", []Zone{notch}, at(500, 900), 200, "notch", 100},
		{"below the notch is inside", []Zone{notch}, at(500, 400), 0, "notch", 0},
		{"zones without an outline never match", []Zone{undrawn}, at(0, 0), 500, "", 0},
		{"no zones", nil, at(0, 0), 500, "", 0},
	}
	for _, tc := range cases {
		hit, nearest := matchZone(tc.zones, tc.p, tc.tol)
		got := ""
		if hit != nil {
			got = hit.Zone.Name
			if hit.DistanceM != tc.dist {
				t.Errorf("%s: distance %d, want %d", tc.name, hit.DistanceM, tc.dist)
			}
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		if hit == nil && len(tc.zones) > 0 && len(tc.zones[0].Polygon) > 0 && nearest == nil {
			t.Errorf("%s: nearest zone missing", tc.name)
		}
	}
	_, nearest := matchZone([]Zone{a, b}, at(-600, 500), 200)
	if nearest == nil || nearest.Zone.Name != "a" || nearest.DistanceM != 600 {
		t.Errorf("nearest out of range: %+v", nearest)
	}
}

func TestCheckPolygon(t *testing.T) {
	store := at(500, 500)
	ok := func(name string, p Polygon, s *LatLng) {
		t.Helper()
		if err := checkPolygon(p, s); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := func(name string, p Polygon, s *LatLng, code string) {
		t.Helper()
		var ae *Error
		if err := checkPolygon(p, s); !errors.As(err, &ae) || ae.Code != code {
			t.Errorf("%s: want %s, got %v", name, code, err)
		}
	}
	ok("square", square(0, 0, 1000, 1000), &store)
	ok("triangle without a store", Polygon{at(0, 0), at(100, 0), at(0, 100)}, nil)
	ok("concave", Polygon{at(0, 0), at(1000, 0), at(1000, 1000), at(500, 300), at(0, 1000)}, &store)
	bad("two points", Polygon{at(0, 0), at(100, 0)}, &store, "VALIDATION")
	bad("bow tie", Polygon{at(0, 0), at(1000, 1000), at(1000, 0), at(0, 1000)}, &store, "ZONE_SELF_INTERSECTS")
	bad("too far", square(40000, 0, 41000, 1000), &store, "VALIDATION")
	bad("no area", Polygon{at(0, 0), at(100, 0), at(200, 0)}, &store, "VALIDATION")
	bad("bad latitude", Polygon{{91, 121}, at(100, 0), at(0, 100)}, nil, "VALIDATION")
	big := Polygon{}
	for i := 0; i < 201; i++ {
		big = append(big, at(float64(i), float64(i*i)))
	}
	bad("201 points", big, nil, "VALIDATION")
}
