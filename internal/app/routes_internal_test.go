package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecodePolyline(t *testing.T) {
	got := decodePolyline("_p~iF~ps|U_ulLnnqC_mqNvxq`@")
	want := [][2]float64{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("point %d: got %v, want %v", i, got[i], want[i])
		}
	}
	if p := decodePolyline("_p~iF~ps|U_ul"); len(p) != 1 {
		t.Fatalf("a cut-off line keeps its whole points: %v", p)
	}
}

// What is sent to Google and how the answer is read, against a stand-in server.
func TestGoogleRoutes(t *testing.T) {
	var asked []map[string]any
	refuseTwoWheeler := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Goog-Api-Key") != "k" || r.Header.Get("X-Goog-FieldMask") == "" || r.Method != "POST" {
			t.Errorf("headers: %v", r.Header)
		}
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		asked = append(asked, body)
		if refuseTwoWheeler && body["travelMode"] == "TWO_WHEELER" {
			http.Error(w, `{"error":{"status":"INVALID_ARGUMENT"}}`, 400)
			return
		}
		if body["travelMode"] == "DRIVE" && !refuseTwoWheeler {
			http.Error(w, `{"error":{"status":"PERMISSION_DENIED"}}`, 403)
			return
		}
		_, _ = w.Write([]byte(`{"routes":[{"distanceMeters":5200,"duration":"754s","polyline":{"encodedPolyline":"_p~iF~ps|U_ulLnnqC_mqNvxq` + "`" + `@"},
			"optimizedIntermediateWaypointIndex":[1,0],"legs":[{"distanceMeters":2000},{"distanceMeters":1100},{"distanceMeters":2100}]}]}`))
	}))
	defer srv.Close()
	g := GoogleRoutes{Key: "k", Endpoint: srv.URL}
	store, stops := LatLng{14.55, 121.02}, []LatLng{{14.559, 121.02}, {14.568, 121.02}}

	r, err := g.Route(context.Background(), store, stops)
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != "TWO_WHEELER" || r.DistanceM != 5200 || r.DurationS != 754 || r.ReturnM != 2100 || len(r.Path) != 3 ||
		len(r.Order) != 2 || r.Order[0] != 1 || r.Order[1] != 0 {
		t.Fatalf("route: %+v", r)
	}
	q := asked[0]
	if q["optimizeWaypointOrder"] != true || q["routingPreference"] != "TRAFFIC_AWARE" || len(q["intermediates"].([]any)) != 2 {
		t.Fatalf("asked: %v", q)
	}
	ll := q["origin"].(map[string]any)["location"].(map[string]any)["latLng"].(map[string]any)
	if ll["latitude"] != 14.55 || ll["longitude"] != 121.02 {
		t.Fatalf("origin: %v", ll)
	}

	// one stop: nothing to put in order
	asked = nil
	if _, err := g.Route(context.Background(), store, stops[:1]); err != nil {
		t.Fatal(err)
	}
	if _, set := asked[0]["optimizeWaypointOrder"]; set {
		t.Fatalf("one stop asks for no ordering: %v", asked[0])
	}

	// no motorcycle route for this request: a car route, and it says so
	asked, refuseTwoWheeler = nil, true
	r, err = g.Route(context.Background(), store, stops)
	if err != nil || r.Mode != "DRIVE" || len(asked) != 2 {
		t.Fatalf("car route: %+v %v (%d requests)", r, err, len(asked))
	}

	// a key that is refused is an error, and is not asked twice
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "denied", 403) }))
	defer bad.Close()
	if _, err := (GoogleRoutes{Key: "k", Endpoint: bad.URL}).Route(context.Background(), store, stops); err == nil {
		t.Fatal("a refused key must fail")
	}
	if _, err := g.Route(context.Background(), store, nil); err == nil {
		t.Fatal("no stops must fail")
	}
}

func TestStubRouter(t *testing.T) {
	store := LatLng{14.55, 121.02}
	r, _ := StubRouter{}.Route(context.Background(), store, []LatLng{{14.559, 121.03}})
	if r.Mode != "TWO_WHEELER" || len(r.Path) != 5 || r.DistanceM < 3900 || r.DistanceM > 4300 || r.ReturnM*2 != r.DistanceM && abs(r.ReturnM*2-r.DistanceM) > 2 {
		t.Fatalf("stub: %+v", r)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
