package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PlannedRoute is a round trip from the store through the drops and back.
type PlannedRoute struct {
	// Order lists the stops, as indexes into what was asked, in the order to visit them.
	Order     []int
	DistanceM int
	DurationS int
	// ReturnM is the last leg, from the last drop back to the store.
	ReturnM int
	Path    [][2]float64
	// Mode is how the route is travelled: TWO_WHEELER or DRIVE.
	Mode string
}

// Router plans the round trip of a rider on real roads.
type Router interface {
	Route(ctx context.Context, store LatLng, stops []LatLng) (*PlannedRoute, error)
}

// routeMaxStops is the most drops one request may carry.
const routeMaxStops = 25

// GoogleRoutes asks the Routes API of Google Maps Platform. One request per
// trip: store, the drops in the best order, store.
type GoogleRoutes struct {
	Key string
	// Endpoint is replaced in tests.
	Endpoint string
	Client   *http.Client
}

const googleRoutesURL = "https://routes.googleapis.com/directions/v2:computeRoutes"

type googleWaypoint struct {
	Location struct {
		LatLng struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"latLng"`
	} `json:"location"`
}

func googlePoint(p LatLng) googleWaypoint {
	var w googleWaypoint
	w.Location.LatLng.Latitude, w.Location.LatLng.Longitude = p[0], p[1]
	return w
}

// Route asks for a motorcycle route first. Where Google has none for the
// request, it asks again for a car and says so in Mode.
func (g GoogleRoutes) Route(ctx context.Context, store LatLng, stops []LatLng) (*PlannedRoute, error) {
	if len(stops) == 0 || len(stops) > routeMaxStops {
		return nil, fmt.Errorf("routes: %d stops", len(stops))
	}
	r, err := g.ask(ctx, store, stops, "TWO_WHEELER")
	if bad, ok := err.(routeRefused); ok && bad.status == http.StatusBadRequest {
		r, err = g.ask(ctx, store, stops, "DRIVE")
	}
	return r, err
}

// routeRefused is an answer other than 200.
type routeRefused struct {
	status int
	body   string
}

func (e routeRefused) Error() string { return fmt.Sprintf("routes: HTTP %d: %s", e.status, e.body) }

func (g GoogleRoutes) ask(ctx context.Context, store LatLng, stops []LatLng, mode string) (*PlannedRoute, error) {
	body := map[string]any{
		"origin": googlePoint(store), "destination": googlePoint(store),
		"travelMode": mode, "routingPreference": "TRAFFIC_AWARE",
	}
	mid := make([]googleWaypoint, len(stops))
	for i, s := range stops {
		mid[i] = googlePoint(s)
	}
	body["intermediates"] = mid
	if len(stops) > 1 {
		body["optimizeWaypointOrder"] = true
	}
	payload, _ := json.Marshal(body)
	url := g.Endpoint
	if url == "" {
		url = googleRoutesURL
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", g.Key)
	req.Header.Set("X-Goog-FieldMask", "routes.distanceMeters,routes.duration,routes.polyline.encodedPolyline,"+
		"routes.optimizedIntermediateWaypointIndex,routes.legs.distanceMeters")
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		if len(raw) > 300 {
			raw = raw[:300]
		}
		return nil, routeRefused{resp.StatusCode, string(raw)}
	}
	var out struct {
		Routes []struct {
			DistanceMeters int    `json:"distanceMeters"`
			Duration       string `json:"duration"`
			Polyline       struct {
				EncodedPolyline string `json:"encodedPolyline"`
			} `json:"polyline"`
			Order []int `json:"optimizedIntermediateWaypointIndex"`
			Legs  []struct {
				DistanceMeters int `json:"distanceMeters"`
			} `json:"legs"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("routes: %w", err)
	}
	if len(out.Routes) == 0 {
		return nil, fmt.Errorf("routes: no route found")
	}
	rt := out.Routes[0]
	r := &PlannedRoute{DistanceM: rt.DistanceMeters, Mode: mode, Path: decodePolyline(rt.Polyline.EncodedPolyline)}
	if s, err := strconv.ParseFloat(strings.TrimSuffix(rt.Duration, "s"), 64); err == nil {
		r.DurationS = int(math.Round(s))
	}
	if n := len(rt.Legs); n == len(stops)+1 {
		r.ReturnM = rt.Legs[n-1].DistanceMeters
	}
	// Without optimisation, or with one stop, the order is as asked.
	r.Order = make([]int, len(stops))
	for i := range r.Order {
		r.Order[i] = i
	}
	if len(rt.Order) == len(stops) {
		seen := make([]bool, len(stops))
		ok := true
		for _, i := range rt.Order {
			if i < 0 || i >= len(stops) || seen[i] {
				ok = false
				break
			}
			seen[i] = true
		}
		if ok {
			copy(r.Order, rt.Order)
		}
	}
	if len(r.Path) < 2 {
		return nil, fmt.Errorf("routes: the route has no shape")
	}
	return r, nil
}

// decodePolyline reads Google's encoded polyline into [lat, lng] points.
func decodePolyline(s string) [][2]float64 {
	var out [][2]float64
	lat, lng, i := 0, 0, 0
	next := func() (int, bool) {
		result, shift := 0, 0
		for {
			if i >= len(s) {
				return 0, false
			}
			b := int(s[i]) - 63
			i++
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				break
			}
		}
		if result&1 != 0 {
			return ^(result >> 1), true
		}
		return result >> 1, true
	}
	for i < len(s) {
		dlat, ok1 := next()
		dlng, ok2 := next()
		if !ok1 || !ok2 {
			break
		}
		lat, lng = lat+dlat, lng+dlng
		out = append(out, [2]float64{float64(lat) / 1e5, float64(lng) / 1e5})
	}
	return out
}

// StubRouter stands in for Google in tests and demos (GOOGLE_ROUTES_API_KEY=stub):
// the drops in straight-line order, along "streets" that run north-south and
// east-west, so the route is longer than the straight line, as on real roads.
type StubRouter struct{}

func (StubRouter) Route(_ context.Context, store LatLng, stops []LatLng) (*PlannedRoute, error) {
	order, _ := bestRound(store, stops)
	r := &PlannedRoute{Order: order, Mode: "TWO_WHEELER", Path: [][2]float64{{store[0], store[1]}}}
	at := store
	leg := func(to LatLng) int {
		corner := LatLng{to[0], at[1]}
		d := metresBetween(at, corner) + metresBetween(corner, to)
		r.Path = append(r.Path, [2]float64{corner[0], corner[1]}, [2]float64{to[0], to[1]})
		at = to
		return int(math.Round(d))
	}
	for _, i := range order {
		r.DistanceM += leg(stops[i])
	}
	r.ReturnM = leg(store)
	r.DistanceM += r.ReturnM
	r.DurationS = r.DistanceM * 3600 / 25000 // 25 km/h
	return r, nil
}
