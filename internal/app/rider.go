package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// TokenSessionTTL is how long the app stays signed in.
	TokenSessionTTL = 30 * 24 * time.Hour
	// dutyMax ends a shift nobody ended.
	dutyMax = 12 * time.Hour
	// A phone that was offline sends what it saved; older points are dropped.
	locationMaxAge   = 10 * time.Minute
	locationMaxBatch = 100
	trackKeep        = 7 * 24 * time.Hour
)

// RiderStatus is what the store knows about a rider right now.
type RiderStatus struct {
	OnDuty    bool       `json:"on_duty"`
	DutySince *time.Time `json:"duty_since"`
	Lat       *float64   `json:"lat"`
	Lng       *float64   `json:"lng"`
	Accuracy  *float32   `json:"accuracy"`
	Battery   *int16     `json:"battery"`
	LocatedAt *time.Time `json:"located_at"`
}

func riderStatus(ctx context.Context, q Querier, userID int64) (*RiderStatus, error) {
	var s RiderStatus
	err := q.QueryRow(ctx, `SELECT on_duty, duty_since, lat, lng, accuracy, battery, located_at FROM rider_status
		WHERE user_id=$1`, userID).Scan(&s.OnDuty, &s.DutySince, &s.Lat, &s.Lng, &s.Accuracy, &s.Battery, &s.LocatedAt)
	if isNoRows(err) {
		return &s, nil
	}
	return &s, err
}

func (a *App) RiderStatus(ctx context.Context, userID int64) (*RiderStatus, error) {
	return riderStatus(ctx, a.DB, userID)
}

// riderBranches lists the branches where the user holds the rider post.
func riderBranches(ctx context.Context, q Querier, userID int64) ([]int64, error) {
	rows, err := q.Query(ctx, `SELECT branch_id FROM staff_posts WHERE user_id=$1 AND kind='rider' ORDER BY branch_id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

var errNotRider = Err(http.StatusForbidden, "NOT_RIDER", "only a rider can do this")

// SetDuty clocks a rider on or off. The last known position is cleared when
// the shift ends, so the store never shows where someone is after work.
// session is the sign-in the shift is started from: signing out there ends it.
func (a *App) SetDuty(ctx context.Context, userID int64, on bool, session string) (*RiderStatus, error) {
	var out *RiderStatus
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		branches, err := riderBranches(ctx, tx, userID)
		if err != nil {
			return err
		}
		if len(branches) == 0 {
			if on {
				return errNotRider
			}
		}
		var sh []byte
		if session != "" {
			sh = hashToken(session)
		}
		if err := setDuty(ctx, tx, userID, on, sh); err != nil {
			return err
		}
		for _, b := range branches {
			if err := notify(ctx, tx, Event{BranchID: b, RiderIDs: []int64{userID}}); err != nil {
				return err
			}
		}
		out, err = riderStatus(ctx, tx, userID)
		return err
	})
	return out, err
}

func setDuty(ctx context.Context, q Querier, userID int64, on bool, session []byte) error {
	if on {
		_, err := q.Exec(ctx, `INSERT INTO rider_status (user_id, on_duty, duty_since, duty_session) VALUES ($1, true, now(), $2)
			ON CONFLICT (user_id) DO UPDATE SET on_duty=true, updated_at=now(), duty_session=$2,
				duty_since = CASE WHEN rider_status.on_duty THEN rider_status.duty_since ELSE now() END`, userID, session)
		return err
	}
	_, err := q.Exec(ctx, `UPDATE rider_status SET on_duty=false, duty_since=NULL, duty_session=NULL, lat=NULL, lng=NULL,
		accuracy=NULL, located_at=NULL, updated_at=now() WHERE user_id=$1 AND on_duty`, userID)
	return err
}

// endLongShifts clocks off riders who never ended their shift.
func (a *App) endLongShifts(ctx context.Context) error {
	_, err := a.DB.Exec(ctx, `UPDATE rider_status SET on_duty=false, duty_since=NULL, duty_session=NULL, lat=NULL, lng=NULL,
		accuracy=NULL, located_at=NULL, updated_at=now() WHERE on_duty AND duty_since < now() - make_interval(secs => $1)`, dutyMax.Seconds())
	return err
}

// LocationPoint is one position fix from the rider's phone.
type LocationPoint struct {
	Lat      float64   `json:"lat"`
	Lng      float64   `json:"lng"`
	Accuracy *float32  `json:"accuracy"`
	Battery  *int16    `json:"battery"`
	At       time.Time `json:"at"`
}

// ReportLocations stores a batch of fixes. Positions are only taken from a
// rider who is on duty. It returns how many points were kept.
func (a *App) ReportLocations(ctx context.Context, userID int64, points []LocationPoint) (int, error) {
	if len(points) == 0 || len(points) > locationMaxBatch {
		return 0, ErrValidation(fmt.Sprintf("send between 1 and %d points", locationMaxBatch))
	}
	now := time.Now()
	kept := points[:0:0]
	for _, p := range points {
		if p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 || (p.Lat == 0 && p.Lng == 0) {
			return 0, ErrValidation("invalid coordinates")
		}
		if p.At.IsZero() || p.At.After(now.Add(time.Minute)) {
			p.At = now
		}
		if now.Sub(p.At) > locationMaxAge {
			continue
		}
		kept = append(kept, p)
	}
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var on bool
		err := tx.QueryRow(ctx, `SELECT on_duty FROM rider_status WHERE user_id=$1 FOR UPDATE`, userID).Scan(&on)
		if err != nil && !isNoRows(err) {
			return err
		}
		if !on {
			return Err(http.StatusConflict, "OFF_DUTY", "positions are only taken while on duty")
		}
		for _, p := range kept {
			if _, err := tx.Exec(ctx, `INSERT INTO rider_tracks (user_id, lat, lng, accuracy, at) VALUES ($1,$2,$3,$4,$5)`,
				userID, p.Lat, p.Lng, p.Accuracy, p.At); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE rider_status SET lat=$2, lng=$3, accuracy=$4, battery=coalesce($5, battery),
				located_at=$6, updated_at=now() WHERE user_id=$1 AND (located_at IS NULL OR located_at < $6)`,
				userID, p.Lat, p.Lng, p.Accuracy, p.Battery, p.At); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(kept), nil
}

// ---- devices and push ----

// Device is a phone or browser that receives push messages for a user.
type Device struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"-"`
	Platform string `json:"platform"`
	Token    string `json:"-"`
}

// RegisterDevice records where to push for this user. A token that was
// registered by someone else on the same phone moves to the new user.
func (a *App) RegisterDevice(ctx context.Context, userID int64, platform, token, version, session string) (*Device, error) {
	token = strings.TrimSpace(token)
	if platform != "android" && platform != "ios" && platform != "web" {
		return nil, ErrValidation("platform must be android, ios or web")
	}
	if token == "" || len(token) > 4096 || len(version) > 40 {
		return nil, ErrValidation("invalid device token")
	}
	if platform == "web" {
		var sub struct {
			Endpoint string `json:"endpoint"`
		}
		if json.Unmarshal([]byte(token), &sub) != nil || !strings.HasPrefix(sub.Endpoint, "http") {
			return nil, ErrValidation("invalid push subscription")
		}
	}
	var sh []byte
	if session != "" {
		sh = hashToken(session)
	}
	d := &Device{UserID: userID, Platform: platform, Token: token}
	err := a.DB.QueryRow(ctx, `INSERT INTO rider_devices (user_id, platform, token, app_version, session_hash)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (token) DO UPDATE SET user_id=$1, platform=$2, app_version=$4,
			session_hash=$5, last_seen_at=now() RETURNING id`, userID, platform, token, version, sh).Scan(&d.ID)
	return d, err
}

func (a *App) RemoveDevice(ctx context.Context, userID, id int64) error {
	_, err := a.DB.Exec(ctx, `DELETE FROM rider_devices WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

// PushMessage is a short reminder. It never carries the task itself: the app
// reloads from the server when it is opened.
type PushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ErrPushGone tells the caller that the device no longer exists.
var ErrPushGone = errors.New("push: device is gone")

// PushSender delivers a message to one device.
type PushSender interface {
	Send(ctx context.Context, d Device, m PushMessage) error
}

type pushJob struct {
	UserID  int64       `json:"user_id"`
	Message PushMessage `json:"message"`
}

// riderPushTexts are the reminders sent to riders, Chinese then English.
var riderPushTexts = map[string][2]string{
	"assigned":   {"新配送单", "New delivery"},
	"reassigned": {"配送单已改派给别人", "Delivery reassigned to someone else"},
	"cancelled":  {"配送单已取消", "Delivery cancelled"},
	"packed":     {"可以取餐了", "Ready to collect"},
}

// pushRider enqueues a reminder for a rider in the caller's transaction, so
// that it is only sent if the change commits.
func (a *App) pushRider(ctx context.Context, tx pgx.Tx, riderID, orderID int64, event string) error {
	text, ok := riderPushTexts[event]
	if !ok || riderID == 0 {
		return nil
	}
	var has bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM rider_devices WHERE user_id=$1)`, riderID).Scan(&has); err != nil || !has {
		return err
	}
	var no, line, unit string
	if err := tx.QueryRow(ctx, `SELECT public_no, address_line, address_unit FROM orders WHERE id=$1`, orderID).
		Scan(&no, &line, &unit); err != nil {
		return err
	}
	body := strings.TrimSpace(no + " " + strings.TrimSpace(line+" "+unit))
	return enqueue(ctx, tx, "push", pushJob{UserID: riderID, Message: PushMessage{
		Title: text[0] + " " + text[1], Body: body, Tag: fmt.Sprintf("order-%d", orderID), URL: "/work"}})
}

// sendPush delivers one queued reminder to every device of the user. Devices
// the push service no longer knows are forgotten. It fails, and is retried,
// only when no device could be reached.
func (a *App) sendPush(ctx context.Context, p pushJob) error {
	rows, err := a.DB.Query(ctx, `SELECT d.id, d.user_id, d.platform, d.token FROM rider_devices d
		JOIN users u ON u.id = d.user_id WHERE d.user_id=$1 AND u.active ORDER BY d.id`, p.UserID)
	if err != nil {
		return err
	}
	devices, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Device, error) {
		var d Device
		return d, r.Scan(&d.ID, &d.UserID, &d.Platform, &d.Token)
	})
	if err != nil {
		return err
	}
	var sent int
	var last error
	for _, d := range devices {
		sender := a.Push[d.Platform]
		if sender == nil {
			continue // this platform is not set up on this server
		}
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := sender.Send(sendCtx, d, p.Message)
		cancel()
		switch {
		case err == nil:
			sent++
		case errors.Is(err, ErrPushGone):
			if _, err := a.DB.Exec(ctx, `DELETE FROM rider_devices WHERE id=$1`, d.ID); err != nil {
				return err
			}
		default:
			last = err
		}
	}
	if sent == 0 {
		return last
	}
	return nil
}

// ---- what the store and the customer see of a rider ----

// MarkSeen records that the rider has these assignments on screen. Only the
// rider's own, still waiting assignments count; the order desk is told.
func (a *App) MarkSeen(ctx context.Context, riderID int64, taskIDs []int64) error {
	if len(taskIDs) == 0 || len(taskIDs) > 100 {
		return nil
	}
	return a.inTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE delivery_tasks SET seen_at=now() WHERE id = ANY($1) AND rider_id=$2
			AND seen_at IS NULL AND status='assigned' RETURNING branch_id, order_id`, taskIDs, riderID)
		if err != nil {
			return err
		}
		type hit struct{ branch, order int64 }
		hits, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (hit, error) {
			var h hit
			return h, r.Scan(&h.branch, &h.order)
		})
		if err != nil {
			return err
		}
		for _, h := range hits {
			if err := bump(ctx, tx, h.order); err != nil {
				return err
			}
			if err := notify(ctx, tx, Event{BranchID: h.branch, OrderID: h.order}); err != nil {
				return err
			}
		}
		return nil
	})
}

// positionFresh is how old a position may be before the rider counts as
// out of contact.
const positionFresh = 5 * time.Minute

// RiderInfo is one rider as the order desk sees them when assigning.
type RiderInfo struct {
	StaffMember
	Lat       *float64   `json:"lat"`
	Lng       *float64   `json:"lng"`
	Accuracy  *float32   `json:"accuracy"`
	Battery   *int16     `json:"battery"`
	LocatedAt *time.Time `json:"located_at"`
	// Contact is "ok", "none" (on duty, never reported a position), "lost"
	// (on duty, nothing for five minutes) or "" when off duty.
	Contact string `json:"contact"`
	// DistanceM is the straight-line distance to the store, when both are known.
	DistanceM *int `json:"distance_m"`
}

// RiderBoard lists the branch's riders for assigning: on duty first, then
// the nearest to the store, then whoever carries the least.
// It also returns where the store is, when that is set.
func (a *App) RiderBoard(ctx context.Context, branchID int64) ([]RiderInfo, *LatLng, error) {
	staff, err := a.ListRiders(ctx, branchID)
	if err != nil {
		return nil, nil, err
	}
	var store *LatLng
	var lat, lng *float64
	if err := a.DB.QueryRow(ctx, `SELECT lat, lng FROM branches WHERE id=$1`, branchID).Scan(&lat, &lng); err != nil {
		return nil, nil, err
	}
	if lat != nil && lng != nil {
		store = &LatLng{*lat, *lng}
	}
	out := make([]RiderInfo, 0, len(staff))
	now := time.Now()
	for _, m := range staff {
		r := RiderInfo{StaffMember: m}
		if m.OnDuty {
			st, err := riderStatus(ctx, a.DB, m.ID)
			if err != nil {
				return nil, nil, err
			}
			r.Lat, r.Lng, r.Accuracy, r.Battery, r.LocatedAt = st.Lat, st.Lng, st.Accuracy, st.Battery, st.LocatedAt
			switch {
			case st.LocatedAt == nil:
				r.Contact = "none"
			case now.Sub(*st.LocatedAt) > positionFresh:
				r.Contact = "lost"
			default:
				r.Contact = "ok"
			}
			if store != nil && st.Lat != nil && st.Lng != nil {
				d := int(metresBetween(*store, LatLng{*st.Lat, *st.Lng}))
				r.DistanceM = &d
			}
		}
		out = append(out, r)
	}
	rank := func(r RiderInfo) int {
		switch {
		case !r.OnDuty:
			return 3
		case r.Contact == "ok":
			return 0
		case r.Contact == "none":
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if (a.DistanceM != nil) != (b.DistanceM != nil) {
			return a.DistanceM != nil
		}
		// Within 200 m riders are equally close; the one carrying less goes first.
		if a.DistanceM != nil && *a.DistanceM/200 != *b.DistanceM/200 {
			return *a.DistanceM < *b.DistanceM
		}
		return a.ActiveDeliveries < b.ActiveDeliveries
	})
	return out, store, nil
}

// metresBetween is the great-circle distance.
func metresBetween(a, b LatLng) float64 {
	const r = 6371000.0
	rad := math.Pi / 180
	dLat, dLng := (b[0]-a[0])*rad, (b[1]-a[1])*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a[0]*rad)*math.Cos(b[0]*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Tracking is what a customer may know about the rider carrying their order.
type Tracking struct {
	Active     bool       `json:"active"` // the order is on its way
	RiderName  string     `json:"rider_name,omitempty"`
	RiderPhone string     `json:"rider_phone,omitempty"`
	Lat        *float64   `json:"lat"`
	Lng        *float64   `json:"lng"`
	LocatedAt  *time.Time `json:"located_at"`
	DestLat    *float64   `json:"dest_lat"`
	DestLng    *float64   `json:"dest_lng"`
	// DistanceM is the straight line from the rider to the customer's pin.
	DistanceM *int `json:"distance_m"`
}

// OrderTracking returns the rider's position for the customer of this order,
// only between pickup and delivery and only while the position is recent.
func (a *App) OrderTracking(ctx context.Context, token string) (*Tracking, error) {
	o, err := a.OrderByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	t := &Tracking{DestLat: o.DestLat, DestLng: o.DestLng}
	d := o.Delivery
	if d == nil || o.AcceptanceStatus != "accepted" || (d.Status != "picked_up" && d.Status != "arrived") {
		return t, nil
	}
	t.Active, t.RiderName, t.RiderPhone = true, d.RiderName, d.RiderPhone
	st, err := riderStatus(ctx, a.DB, d.RiderID)
	if err != nil {
		return nil, err
	}
	if st.OnDuty && st.LocatedAt != nil && time.Since(*st.LocatedAt) <= locationMaxAge {
		t.Lat, t.Lng, t.LocatedAt = st.Lat, st.Lng, st.LocatedAt
		if o.DestLat != nil && o.DestLng != nil {
			m := int(metresBetween(LatLng{*st.Lat, *st.Lng}, LatLng{*o.DestLat, *o.DestLng}))
			t.DistanceM = &m
		}
	}
	return t, nil
}
