package api

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"roompulse/internal/domain"
)

// Notification is one outbox message for the app/admin to surface: grace-window
// "are you coming?" pings, no-show releases, and room-freed events. Kept in an
// in-memory ring with per-key dedup so a reminder fires once, not every sweep.
type Notification struct {
	ID            int64     `json:"id"`
	Type          string    `json:"type"` // grace_reminder | no_show_released | room_freed
	Level         int       `json:"level,omitempty"`
	WorkspaceID   string    `json:"workspace_id,omitempty"`
	ReservationID string    `json:"reservation_id,omitempty"`
	Recipient     string    `json:"recipient,omitempty"` // booker; "" = broadcast
	Title         string    `json:"title"`
	Body          string    `json:"body"`
	CreatedAt     time.Time `json:"created_at"`
}

type notifier struct {
	mu   sync.Mutex
	max  int
	seq  int64
	list []Notification
	sent map[string]bool // dedup key -> already emitted
}

func newNotifier(max int) *notifier { return &notifier{max: max, sent: map[string]bool{}} }

// emit appends a notification unless its dedup key was already emitted. An empty
// key disables dedup. Returns whether it was newly emitted.
func (n *notifier) emit(key string, note Notification) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if key != "" {
		if n.sent[key] {
			return false
		}
		n.sent[key] = true
	}
	n.seq++
	note.ID = n.seq
	n.list = append(n.list, note)
	if len(n.list) > n.max {
		n.list = n.list[len(n.list)-n.max:]
	}
	return true
}

// recent returns newest-first notifications, optionally filtered by recipient.
func (n *notifier) recent(recipient string, limit int) []Notification {
	n.mu.Lock()
	defer n.mu.Unlock()
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	out := []Notification{}
	for i := len(n.list) - 1; i >= 0 && len(out) < limit; i-- {
		if recipient != "" && n.list[i].Recipient != recipient {
			continue
		}
		out = append(out, n.list[i])
	}
	return out
}

// getNotifications serves the outbox. ?recipient= filters to one booker; ?limit=.
func (s *Server) getNotifications(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": s.notify.recent(r.URL.Query().Get("recipient"), limit),
	})
}

// bookerOf is the notification recipient for a reservation: email if known,
// else the user id.
func bookerOf(r domain.Reservation) string {
	if r.UserEmail != "" {
		return r.UserEmail
	}
	return r.UserID
}

// roomName resolves a workspace id to its room name, falling back to the id.
func (s *Server) roomName(workspaceID string) string {
	if r, ok := s.store.RoomByWorkspace(workspaceID); ok && r.Name != "" {
		return r.Name
	}
	return workspaceID
}
