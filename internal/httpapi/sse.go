package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/brianabc121/waimai/internal/app"
)

// subscription describes what one SSE connection is allowed to hear about.
// Exactly one of the fields is set.
type subscription struct {
	ch     chan struct{}
	branch int64
	order  int64
	rider  int64
}

func (s *subscription) matches(ev app.Event) bool {
	if s.branch != 0 {
		return s.branch == ev.BranchID
	}
	if s.order != 0 {
		return s.order == ev.OrderID
	}
	for _, r := range ev.RiderIDs {
		if r == s.rider {
			return true
		}
	}
	return false
}

// Hub fans refresh hints out to SSE connections. Hints carry no data: a hint
// can be lost, so screens always re-read the snapshot and also poll slowly.
type Hub struct {
	mu   sync.Mutex
	subs map[*subscription]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[*subscription]struct{}{}} }

func (h *Hub) add(s *subscription) {
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(s *subscription) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

func (h *Hub) publish(ev app.Event, all bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if all || s.matches(ev) {
			select {
			case s.ch <- struct{}{}:
			default: // a refresh is already pending for this connection
			}
		}
	}
}

// Listen holds a dedicated connection on LISTEN and reconnects forever. It
// must use a direct connection, not a transaction-mode pooler. After every
// reconnect all screens are told to refresh, since hints may have been missed.
func (h *Hub) Listen(ctx context.Context, url string) {
	for ctx.Err() == nil {
		err := h.listenOnce(ctx, url)
		if ctx.Err() != nil {
			return
		}
		log.Printf("sse: listener disconnected: %v; reconnecting", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (h *Hub) listenOnce(ctx context.Context, url string) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN "+app.NotifyChannel); err != nil {
		return err
	}
	h.publish(app.Event{}, true)
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var ev app.Event
		if json.Unmarshal([]byte(n.Payload), &ev) == nil {
			h.publish(ev, false)
		}
	}
}

const (
	sseHeartbeat   = 20 * time.Second
	sseMaxLifetime = 30 * time.Minute // forces periodic re-authorisation
)

func (h *Hub) serve(c *gin.Context, sub *subscription) {
	sub.ch = make(chan struct{}, 1)
	h.add(sub)
	defer h.remove(sub)

	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = w.WriteString("retry: 3000\nevent: ready\ndata: {}\n\n")
	w.Flush()

	beat := time.NewTicker(sseHeartbeat)
	defer beat.Stop()
	deadline := time.NewTimer(sseMaxLifetime)
	defer deadline.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-deadline.C:
			return
		case <-sub.ch:
			_, _ = w.WriteString("event: refresh\ndata: {}\n\n")
		case <-beat.C:
			_, _ = w.WriteString(": ping\n\n")
		}
		w.Flush()
	}
}
