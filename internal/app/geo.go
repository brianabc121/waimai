package app

import (
	"math"
)

// LatLng is one point as [latitude, longitude], the order used in the
// polygon column and in the API.
type LatLng [2]float64

// Polygon is a zone outline. The first point is not repeated at the end.
type Polygon []LatLng

const (
	minPolygonPoints = 3
	maxPolygonPoints = 200
	// maxZoneReachM keeps a stray click from creating a zone in another city.
	maxZoneReachM = 30000
	metresPerDeg  = 111320.0
)

type xy struct{ x, y float64 }

// planar projects points to metres on a plane around a reference latitude.
// Zones are a few kilometres across, where this is accurate to well under a
// metre, so plain 2D geometry is enough and PostGIS is not needed.
type planar struct{ lat0, lng0, kx float64 }

func newPlanar(ref LatLng) planar {
	return planar{lat0: ref[0], lng0: ref[1], kx: math.Cos(ref[0] * math.Pi / 180)}
}

func (p planar) pt(l LatLng) xy {
	return xy{(l[1] - p.lng0) * metresPerDeg * p.kx, (l[0] - p.lat0) * metresPerDeg}
}

func (p planar) ring(poly Polygon) []xy {
	out := make([]xy, len(poly))
	for i, l := range poly {
		out[i] = p.pt(l)
	}
	return out
}

func validLatLng(l LatLng) bool {
	return !math.IsNaN(l[0]) && !math.IsNaN(l[1]) && l[0] >= -90 && l[0] <= 90 && l[1] >= -180 && l[1] <= 180
}

// distSeg is the distance from p to the segment a-b.
func distSeg(p, a, b xy) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l2))
	}
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}

// ringDistance is the distance from p to the outline of the ring.
func ringDistance(p xy, ring []xy) float64 {
	d := math.Inf(1)
	for i := range ring {
		d = math.Min(d, distSeg(p, ring[i], ring[(i+1)%len(ring)]))
	}
	return d
}

// onEdgeM: a point this close to the outline counts as inside, so that a pin
// exactly on a shared border never falls between two zones.
const onEdgeM = 0.01

// ringContains reports whether p is inside the ring or on its outline.
func ringContains(p xy, ring []xy) bool {
	in := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if (a.y > p.y) != (b.y > p.y) && p.x < (b.x-a.x)*(p.y-a.y)/(b.y-a.y)+a.x {
			in = !in
		}
	}
	return in || ringDistance(p, ring) <= onEdgeM
}

func cross(o, a, b xy) float64 { return (a.x-o.x)*(b.y-o.y) - (a.y-o.y)*(b.x-o.x) }

// segmentsCross reports a proper crossing: the segments intersect at a point
// that is interior to both. Touching ends and collinear overlap do not count.
func segmentsCross(a, b, c, d xy) bool {
	d1, d2 := cross(c, d, a), cross(c, d, b)
	d3, d4 := cross(a, b, c), cross(a, b, d)
	return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))
}

func ringSelfIntersects(ring []xy) bool {
	n := len(ring)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if j == i+1 || (i == 0 && j == n-1) {
				continue // neighbours share a point
			}
			if segmentsCross(ring[i], ring[(i+1)%n], ring[j], ring[(j+1)%n]) {
				return true
			}
		}
	}
	return false
}

func ringArea(ring []xy) float64 {
	s := 0.0
	for i := range ring {
		j := (i + 1) % len(ring)
		s += ring[i].x*ring[j].y - ring[j].x*ring[i].y
	}
	return math.Abs(s) / 2
}

// checkPolygon validates an outline drawn by a merchant. store is nil while
// the branch has no location yet.
func checkPolygon(poly Polygon, store *LatLng) error {
	if len(poly) < minPolygonPoints || len(poly) > maxPolygonPoints {
		return ErrValidation("a zone outline needs 3 to 200 points")
	}
	for _, l := range poly {
		if !validLatLng(l) {
			return ErrValidation("zone outline has an invalid coordinate")
		}
	}
	ref := poly[0]
	if store != nil {
		ref = *store
	}
	pl := newPlanar(ref)
	ring := pl.ring(poly)
	if store != nil {
		for _, p := range ring {
			if math.Hypot(p.x, p.y) > maxZoneReachM {
				return ErrValidation("zone outline is more than 30 km from the store")
			}
		}
	}
	if ringSelfIntersects(ring) {
		return Err(422, "ZONE_SELF_INTERSECTS", "the zone outline crosses itself")
	}
	if ringArea(ring) < 1 {
		return ErrValidation("zone outline encloses no area")
	}
	return nil
}

// ZoneMatch is the zone a delivery point belongs to.
type ZoneMatch struct {
	Zone *Zone
	// DistanceM is 0 when the point is inside the zone, otherwise how far
	// outside its outline it is.
	DistanceM int
}

// matchZone picks the zone for a point. zones must be in priority order
// (sort, id). The first zone containing the point wins; a point outside all
// of them goes to the nearest zone within toleranceM, the earlier zone on a
// tie. Zones without an outline never match. nearest is the closest outlined
// zone even when it is out of tolerance, for the "x metres away" message.
func matchZone(zones []Zone, at LatLng, toleranceM int) (hit *ZoneMatch, nearest *ZoneMatch) {
	pl := newPlanar(at)
	p := xy{}
	best := math.Inf(1)
	for i := range zones {
		z := &zones[i]
		if len(z.Polygon) < minPolygonPoints {
			continue
		}
		ring := pl.ring(z.Polygon)
		if ringContains(p, ring) {
			m := &ZoneMatch{Zone: z}
			return m, m
		}
		if d := ringDistance(p, ring); d < best-1e-6 {
			best = d
			nearest = &ZoneMatch{Zone: z, DistanceM: int(math.Ceil(d))}
		}
	}
	if nearest != nil && best <= float64(toleranceM) {
		return nearest, nearest
	}
	return nil, nearest
}
