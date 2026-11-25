package app

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The queue display (docs/QUEUE_DISPLAY_DESIGN.md): a screen in the store
// that customers look at. "Tea" mode lists pickup numbers by how far the
// kitchen got (§5); "restaurant" mode is the waiting list for tables (§6, in
// queue.go). This file has the store's queue settings, the paired screens
// and the tea board.

// QueueSettings are the store's choices for the queue pages (§12.1). The
// zero value is never used: settings are read through queueSettings, which
// fills in the defaults.
type QueueSettings struct {
	// Which modes the store uses.
	Tea        bool `json:"tea"`
	Restaurant bool `json:"restaurant"`
	// Paused stops new tickets; numbers already taken are still called.
	Paused bool `json:"paused"`
	// UpgradeMinutes: how long the head of a smaller group must have waited
	// before a bigger table with nobody waiting for it is offered to them.
	UpgradeMinutes int `json:"upgrade_minutes"`
	// CallAlertMinutes: a called number that has not sat down after this
	// is marked red for staff to mark as missed.
	CallAlertMinutes int `json:"call_alert_minutes"`
	// RequeueAfter: a missed number that comes back goes in this many places
	// behind the head of its group.
	RequeueAfter int `json:"requeue_after"`
	// DineMinutes: how long a sitting takes when there is too little
	// history to tell.
	DineMinutes int `json:"dine_minutes"`
	// ReadyStayMinutes: how long a ready pickup number stays on the screen.
	ReadyStayMinutes int `json:"ready_stay_minutes"`
	// Which orders the tea board shows besides pickup orders.
	ShowDelivery bool `json:"show_delivery"`
	ShowDineIn   bool `json:"show_dine_in"`
	// SMS sends a text to a ticket with a phone number when it is called.
	SMS bool `json:"sms"`
	// Media shows the store's promo media at the bottom of the screens.
	Media bool `json:"media"`
}

func defaultQueueSettings() QueueSettings {
	return QueueSettings{Tea: false, Restaurant: false, UpgradeMinutes: 15, CallAlertMinutes: 5, RequeueAfter: 3,
		DineMinutes: 45, ReadyStayMinutes: 15, ShowDineIn: true, Media: true}
}

// parseQueueSettings reads the stored JSON over the defaults, so that a key
// added later has its default in stores that saved before.
func parseQueueSettings(raw []byte) QueueSettings {
	s := defaultQueueSettings()
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

func (s QueueSettings) validate() error {
	switch {
	case s.UpgradeMinutes < 0 || s.UpgradeMinutes > 240:
		return ErrValidation("upgrade_minutes must be 0 to 240")
	case s.CallAlertMinutes < 1 || s.CallAlertMinutes > 60:
		return ErrValidation("call_alert_minutes must be 1 to 60")
	case s.RequeueAfter < 0 || s.RequeueAfter > 20:
		return ErrValidation("requeue_after must be 0 to 20")
	case s.DineMinutes < 10 || s.DineMinutes > 300:
		return ErrValidation("dine_minutes must be 10 to 300")
	case s.ReadyStayMinutes < 1 || s.ReadyStayMinutes > 120:
		return ErrValidation("ready_stay_minutes must be 1 to 120")
	}
	return nil
}

func queueSettings(ctx context.Context, q Querier, branchID int64) (QueueSettings, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT queue_settings FROM branches WHERE id=$1`, branchID).Scan(&raw)
	if isNoRows(err) {
		return QueueSettings{}, ErrNotFound
	}
	return parseQueueSettings(raw), err
}

// QueueSettings returns the store's settings.
func (a *App) QueueSettings(ctx context.Context, branchID int64) (QueueSettings, error) {
	return queueSettings(ctx, a.DB, branchID)
}

// SaveQueueSettings replaces the store's settings.
func (a *App) SaveQueueSettings(ctx context.Context, branchID int64, in QueueSettings, actor Actor) (QueueSettings, error) {
	if err := in.validate(); err != nil {
		return in, err
	}
	b, _ := json.Marshal(in)
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE branches SET queue_settings=$2 WHERE id=$1`, branchID, b)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := audit(ctx, tx, branchID, 0, actor, "queue.settings", in); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID})
	})
	return in, err
}

// SetQueuePaused stops or resumes taking numbers (the "pause" button on the
// queue page, which needs only queue.use).
func (a *App) SetQueuePaused(ctx context.Context, branchID int64, paused bool, actor Actor) (QueueSettings, error) {
	var out QueueSettings
	err := a.inTx(ctx, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT queue_settings FROM branches WHERE id=$1 FOR UPDATE`, branchID).Scan(&raw); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		out = parseQueueSettings(raw)
		out.Paused = paused
		b, _ := json.Marshal(out)
		if _, err := tx.Exec(ctx, `UPDATE branches SET queue_settings=$2 WHERE id=$1`, branchID, b); err != nil {
			return err
		}
		if err := audit(ctx, tx, branchID, 0, actor, "queue.paused", map[string]any{"paused": paused}); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID})
	})
	return out, err
}

// ---- screens ----

// QueueScreen is a paired (or being paired) display.
type QueueScreen struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Mode        string     `json:"mode"`
	Orientation string     `json:"orientation"`
	Lang        string     `json:"lang"`
	Lang2       string     `json:"lang2"`
	Paired      bool       `json:"paired"`
	PairedAt    *time.Time `json:"paired_at"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
	// PairExpiresAt is set while the screen waits for its code.
	PairExpiresAt *time.Time `json:"pair_expires_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// QueueScreenInput is what the settings page sends for a screen.
type QueueScreenInput struct {
	Name        string `json:"name"`
	Mode        string `json:"mode"`
	Orientation string `json:"orientation"`
	Lang        string `json:"lang"`
	Lang2       string `json:"lang2"`
}

func (in *QueueScreenInput) clean() error {
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) > 40 {
		return ErrValidation("name too long")
	}
	if in.Mode != "tea" && in.Mode != "restaurant" {
		return ErrValidation("mode must be tea or restaurant")
	}
	if in.Orientation == "" {
		in.Orientation = "landscape"
	}
	if in.Orientation != "landscape" && in.Orientation != "portrait" {
		return ErrValidation("orientation must be landscape or portrait")
	}
	for _, l := range []string{in.Lang, in.Lang2} {
		if l != "" && !slices.Contains(Langs, l) {
			return ErrValidation("unknown language " + l)
		}
	}
	if in.Lang2 == in.Lang {
		in.Lang2 = ""
	}
	return nil
}

const screenCols = `id, name, mode, orientation, lang, lang2, token_hash IS NOT NULL, paired_at, last_seen_at,
	CASE WHEN token_hash IS NULL AND pair_expires_at > now() THEN pair_expires_at END, created_at`

func scanScreen(r pgx.Row) (QueueScreen, error) {
	var s QueueScreen
	err := r.Scan(&s.ID, &s.Name, &s.Mode, &s.Orientation, &s.Lang, &s.Lang2, &s.Paired, &s.PairedAt, &s.LastSeenAt,
		&s.PairExpiresAt, &s.CreatedAt)
	return s, err
}

// QueueScreens lists the store's screens: paired ones, and ones still
// waiting for their code (until the code expires).
func (a *App) QueueScreens(ctx context.Context, branchID int64) ([]QueueScreen, error) {
	rows, err := a.DB.Query(ctx, `SELECT `+screenCols+` FROM queue_screens
		WHERE branch_id=$1 AND revoked_at IS NULL AND (token_hash IS NOT NULL OR pair_expires_at > now())
		ORDER BY id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueScreen{}
	for rows.Next() {
		s, err := scanScreen(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// pairDigits: codes are digits only: a TV remote types digits most easily.
const pairDigits = "0123456789"

// CreateQueueScreen adds a screen and returns its one-time pairing code (6
// digits, 10 minutes).
func (a *App) CreateQueueScreen(ctx context.Context, branchID int64, in QueueScreenInput, actor Actor) (QueueScreen, string, error) {
	if err := in.clean(); err != nil {
		return QueueScreen{}, "", err
	}
	var creator *int64
	if actor.ID != 0 {
		creator = &actor.ID
	}
	for attempt := 0; ; attempt++ {
		raw := randomBytes(6)
		code := make([]byte, 6)
		for i, b := range raw {
			code[i] = pairDigits[int(b)%len(pairDigits)]
		}
		s, err := scanScreen(a.DB.QueryRow(ctx, `INSERT INTO queue_screens (branch_id, name, mode, orientation, lang, lang2,
			pair_code_hash, pair_expires_at, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7, now() + make_interval(secs => $8), $9)
			ON CONFLICT (pair_code_hash) WHERE pair_code_hash IS NOT NULL DO NOTHING
			RETURNING `+screenCols, branchID, in.Name, in.Mode, in.Orientation, in.Lang, in.Lang2,
			hashToken(string(code)), devicePairTTL.Seconds(), creator))
		if isNoRows(err) && attempt < 5 {
			continue // the same code is waiting elsewhere; draw another
		}
		if err != nil {
			return QueueScreen{}, "", err
		}
		return s, string(code), nil
	}
}

// UpdateQueueScreen changes what a screen shows; the screen follows at once.
func (a *App) UpdateQueueScreen(ctx context.Context, branchID, id int64, in QueueScreenInput) (QueueScreen, error) {
	if err := in.clean(); err != nil {
		return QueueScreen{}, err
	}
	s, err := scanScreen(a.DB.QueryRow(ctx, `UPDATE queue_screens SET name=$3, mode=$4, orientation=$5, lang=$6, lang2=$7
		WHERE id=$1 AND branch_id=$2 AND revoked_at IS NULL RETURNING `+screenCols,
		id, branchID, in.Name, in.Mode, in.Orientation, in.Lang, in.Lang2))
	if isNoRows(err) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	return s, notify(ctx, a.DB, Event{BranchID: branchID})
}

// RevokeQueueScreen unpairs a screen; it falls back to the pairing page.
func (a *App) RevokeQueueScreen(ctx context.Context, branchID, id int64) error {
	tag, err := a.DB.Exec(ctx, `UPDATE queue_screens SET revoked_at=now(), pair_code_hash=NULL
		WHERE id=$1 AND branch_id=$2 AND revoked_at IS NULL`, id, branchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return notify(ctx, a.DB, Event{BranchID: branchID})
}

// PairQueueScreen consumes a pairing code and returns the screen's token.
func (a *App) PairQueueScreen(ctx context.Context, code string) (string, error) {
	code = strings.Join(strings.Fields(code), "")
	token, hash := newToken()
	var branchID int64
	err := a.DB.QueryRow(ctx, `UPDATE queue_screens SET token_hash=$2, paired_at=now(), last_seen_at=now(), pair_code_hash=NULL
		WHERE pair_code_hash=$1 AND pair_expires_at > now() AND revoked_at IS NULL AND token_hash IS NULL
		RETURNING branch_id`, hashToken(code), hash).Scan(&branchID)
	if isNoRows(err) {
		return "", Err(http.StatusUnprocessableEntity, "PAIR_CODE_INVALID", "the pairing code is wrong or has expired")
	}
	if err != nil {
		return "", err
	}
	return token, notify(ctx, a.DB, Event{BranchID: branchID})
}

// ScreenAccess is what a screen's token points at.
type ScreenAccess struct {
	Screen   QueueScreen `json:"screen"`
	BranchID int64       `json:"-"`
	Slug     string      `json:"slug"`
	Store    string      `json:"store"`
	StoreEn  string      `json:"store_en"`
	Logo     string      `json:"logo"`
	Timezone string      `json:"timezone"`
}

// ScreenByToken resolves a screen and notes that it is on (at most once a
// minute, so the settings page can show it is alive).
func (a *App) ScreenByToken(ctx context.Context, token string) (*ScreenAccess, error) {
	if token == "" {
		return nil, errScreenNotPaired
	}
	var acc ScreenAccess
	var seen *time.Time
	err := a.DB.QueryRow(ctx, `SELECT s.id, s.name, s.mode, s.orientation, s.lang, s.lang2, s.paired_at, s.last_seen_at,
		s.created_at, s.branch_id, b.slug, b.name, b.name_en, b.timezone,
		COALESCE((SELECT t.published->>'logo_url' FROM channel_templates t WHERE t.branch_id = b.id AND t.channel = 'delivery'), '')
		FROM queue_screens s JOIN branches b ON b.id = s.branch_id
		WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND b.deleted_at IS NULL`, hashToken(token)).
		Scan(&acc.Screen.ID, &acc.Screen.Name, &acc.Screen.Mode, &acc.Screen.Orientation, &acc.Screen.Lang, &acc.Screen.Lang2,
			&acc.Screen.PairedAt, &seen, &acc.Screen.CreatedAt,
			&acc.BranchID, &acc.Slug, &acc.Store, &acc.StoreEn, &acc.Timezone, &acc.Logo)
	if isNoRows(err) {
		return nil, errScreenNotPaired
	}
	if err != nil {
		return nil, err
	}
	acc.Screen.LastSeenAt, acc.Screen.Paired = seen, true
	if seen == nil || time.Since(*seen) > time.Minute {
		_, _ = a.DB.Exec(ctx, `UPDATE queue_screens SET last_seen_at=now() WHERE id=$1`, acc.Screen.ID)
	}
	return &acc, nil
}

var errScreenNotPaired = Err(http.StatusUnauthorized, "SCREEN_NOT_PAIRED", "this screen is not paired with a store")

// ---- the tea board (§5) ----

// PickupNumber is one order on the tea board.
type PickupNumber struct {
	OrderID     int64  `json:"order_id"`
	Code        string `json:"code"`
	ServiceType string `json:"service_type"`
	// Stage is wait, cook or ready.
	Stage string `json:"stage"`
	// Since is when the order reached its stage: accepted (wait), ready
	// (ready). Stamp is what the screen calls out by: the later of ready and
	// the last call again; it changes on every call.
	Since time.Time  `json:"since"`
	Stamp *time.Time `json:"stamp,omitempty"`
	// Calls counts how often it was called: the first when it became ready.
	Calls int `json:"calls"`
	// Items is a short list of the dishes, for staff only.
	Items string `json:"items,omitempty"`
	// LeavesAt is when a ready number leaves the screen by itself.
	LeavesAt *time.Time `json:"leaves_at,omitempty"`
	// CanHandOver says staff can mark it as picked up here (pickup orders
	// packed or ready to pack, dine-in dishes ready).
	CanHandOver bool `json:"can_hand_over"`
}

// TeaBoard is what the tea screen and the "pickup numbers" tab show.
type TeaBoard struct {
	Wait  []PickupNumber `json:"wait"`
	Cook  []PickupNumber `json:"cook"`
	Ready []PickupNumber `json:"ready"`
	Now   time.Time      `json:"now"`
}

// TeaBoard lists today's pickup numbers by stage. withItems adds the dishes
// (the staff tab); the screen gets numbers only.
func (a *App) TeaBoard(ctx context.Context, branchID int64, withItems bool) (*TeaBoard, error) {
	set, err := queueSettings(ctx, a.DB, branchID)
	if err != nil {
		return nil, err
	}
	types := []string{"pickup"}
	if set.ShowDineIn {
		types = append(types, "dine_in")
	}
	if set.ShowDelivery {
		types = append(types, "delivery")
	}
	// Dishes the kitchen makes and that are still in the store: for dine-in
	// not served yet. An order without any (bottled drinks only) is ready.
	const k = `i.needs_prep AND i.prep_status <> 'cancelled' AND i.served_at IS NULL`
	rows, err := a.DB.Query(ctx, `SELECT o.id, o.pickup_code, o.service_type, o.packing_status, o.fulfillment_status,
		COALESCE(GREATEST(o.accepted_at, o.release_at), o.created_at),
		count(*) FILTER (WHERE `+k+`),
		count(*) FILTER (WHERE `+k+` AND i.prep_status = 'ready'),
		count(*) FILTER (WHERE `+k+` AND i.prep_status IN ('preparing', 'ready')),
		max(i.ready_at) FILTER (WHERE `+k+`),
		count(*) FILTER (WHERE i.prep_status <> 'cancelled' AND i.served_at IS NULL),
		pc.called_at, COALESCE(pc.calls, 0),
		COALESCE(string_agg(CASE WHEN i.prep_status <> 'cancelled' AND i.served_at IS NULL
			THEN i.name || ' ×' || i.qty END, '、' ORDER BY i.id), '')
		FROM orders o JOIN order_items i ON i.order_id = o.id
		LEFT JOIN pickup_calls pc ON pc.order_id = o.id
		WHERE o.branch_id=$1 AND o.acceptance_status='accepted'
			AND o.fulfillment_status IN ('none', 'awaiting_handoff', 'awaiting_pickup')
			AND o.accepted_at > now() - interval '18 hours'
			AND (o.release_at IS NULL OR o.release_at <= now())
			AND o.service_type = ANY($2)
		GROUP BY o.id, pc.called_at, pc.calls`, branchID, types)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	stay := time.Duration(set.ReadyStayMinutes) * time.Minute
	out := &TeaBoard{Wait: []PickupNumber{}, Cook: []PickupNumber{}, Ready: []PickupNumber{}, Now: now}
	for rows.Next() {
		var n PickupNumber
		var packing, fulfil, items string
		var lines, ready, started, open int
		var readyAt, calledAt *time.Time
		var accepted time.Time
		if err := rows.Scan(&n.OrderID, &n.Code, &n.ServiceType, &packing, &fulfil, &accepted, &lines, &ready, &started,
			&readyAt, &open, &calledAt, &n.Calls, &items); err != nil {
			return nil, err
		}
		if n.ServiceType == "dine_in" && open == 0 {
			continue // everything is at the table
		}
		switch {
		case lines == 0 || ready == lines:
			n.Stage = "ready"
			n.Since = accepted
			if lines > 0 && readyAt != nil {
				n.Since = *readyAt
			}
			stamp := n.Since
			if calledAt != nil && calledAt.After(stamp) {
				stamp = *calledAt
			}
			n.Stamp = &stamp
			leaves := stamp.Add(stay)
			if now.After(leaves) {
				continue // left the screen; staff still see it on the order page
			}
			n.LeavesAt = &leaves
			n.Calls++
			switch n.ServiceType {
			case "pickup":
				n.CanHandOver = packing == "ready_to_pack" || fulfil == "awaiting_pickup"
			case "dine_in":
				n.CanHandOver = ready > 0
			}
		case started > 0:
			n.Stage = "cook"
			n.Since = accepted
		default:
			n.Stage = "wait"
			n.Since = accepted
		}
		if withItems {
			n.Items = items
		}
		switch n.Stage {
		case "ready":
			out.Ready = append(out.Ready, n)
		case "cook":
			out.Cook = append(out.Cook, n)
		default:
			out.Wait = append(out.Wait, n)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byCode := func(l []PickupNumber) {
		sort.Slice(l, func(i, j int) bool {
			if !l[i].Since.Equal(l[j].Since) {
				return l[i].Since.Before(l[j].Since)
			}
			return l[i].OrderID < l[j].OrderID
		})
	}
	byCode(out.Wait)
	byCode(out.Cook)
	// The newest call first: the screen puts it in the big box.
	sort.Slice(out.Ready, func(i, j int) bool { return out.Ready[i].Stamp.After(*out.Ready[j].Stamp) })
	return out, nil
}

// RecallPickup calls a ready number again.
func (a *App) RecallPickup(ctx context.Context, branchID, orderID int64, actor Actor) error {
	return a.inTx(ctx, func(tx pgx.Tx) error {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orders WHERE id=$1 AND branch_id=$2
			AND acceptance_status='accepted')`, orderID, branchID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		var by *int64
		if actor.ID != 0 {
			by = &actor.ID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pickup_calls (order_id, branch_id, calls, called_at, called_by)
			VALUES ($1, $2, 1, now(), $3)
			ON CONFLICT (order_id) DO UPDATE SET calls = pickup_calls.calls + 1, called_at = now(), called_by = $3`,
			orderID, branchID, by); err != nil {
			return err
		}
		if err := audit(ctx, tx, branchID, orderID, actor, "queue.pickup_recalled", nil); err != nil {
			return err
		}
		return notify(ctx, tx, Event{BranchID: branchID, OrderID: orderID})
	})
}

// HandOverPickup is "picked up" on the pickup tab: a pickup order is packed
// (one bag) if the kitchen is done and handed to the customer, the same as
// on the order page; at a dine-in table the ready dishes count as served.
func (a *App) HandOverPickup(ctx context.Context, branchID, orderID int64, actor Actor) error {
	o, err := loadOrder(ctx, a.DB, orderID)
	if err != nil {
		return err
	}
	if o.BranchID != branchID {
		return ErrNotFound
	}
	switch o.ServiceType {
	case "pickup":
		if o.PackingStatus == "ready_to_pack" {
			if _, err := a.Pack(ctx, orderID, 1, o.ManifestVersion, actor); err != nil {
				return err
			}
		}
		_, err := a.StaffHandoff(ctx, orderID, actor)
		return err
	case "dine_in":
		return a.inTx(ctx, func(tx pgx.Tx) error {
			lo, err := lockOrder(ctx, tx, orderID)
			if err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `UPDATE order_items SET served_at = now()
				WHERE order_id=$1 AND prep_status='ready' AND served_at IS NULL`, lo.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return Err(http.StatusConflict, "ITEMS_NOT_READY", "nothing is ready yet")
			}
			if err := bump(ctx, tx, lo.ID); err != nil {
				return err
			}
			if err := audit(ctx, tx, lo.BranchID, lo.ID, actor, "queue.pickup_served", nil); err != nil {
				return err
			}
			return notify(ctx, tx, Event{BranchID: lo.BranchID, OrderID: lo.ID})
		})
	}
	return ErrInvalidState("a rider collects delivery orders")
}

// ScreenView is everything a paired screen shows.
type ScreenView struct {
	Access *ScreenAccess `json:"access"`
	// Paused: no new numbers (restaurant mode shows a notice instead of the QR code).
	Paused bool         `json:"paused"`
	Tea    *TeaBoard    `json:"tea,omitempty"`
	Queue  *QueuePublic `json:"queue,omitempty"`
	Media  []Media      `json:"media"`
	// ReadyStay: how long a ready number stays on a pickup screen (minutes).
	ReadyStay int `json:"ready_stay_minutes"`
}

// ScreenView loads what a screen shows in its mode.
func (a *App) ScreenView(ctx context.Context, acc *ScreenAccess) (*ScreenView, error) {
	set, err := queueSettings(ctx, a.DB, acc.BranchID)
	if err != nil {
		return nil, err
	}
	out := &ScreenView{Access: acc, Paused: set.Paused, Media: []Media{}, ReadyStay: set.ReadyStayMinutes}
	if acc.Screen.Mode == "tea" {
		if out.Tea, err = a.TeaBoard(ctx, acc.BranchID, false); err != nil {
			return nil, err
		}
	} else {
		if out.Queue, err = a.publicQueue(ctx, acc.BranchID, true); err != nil {
			return nil, err
		}
	}
	if set.Media {
		media, err := listMedia(ctx, a.DB, acc.BranchID, true)
		if err != nil {
			return nil, err
		}
		out.Media = media
	}
	return out, nil
}
