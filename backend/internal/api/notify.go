package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"quickroom/internal/apns"
	"quickroom/internal/domain"
)

// Notification is one outbox message for the app/admin to surface: grace-window
// "are you coming?" pings, no-show releases, and room-freed events. Kept in an
// in-memory ring with per-key dedup so a reminder fires once, not every sweep.
type Notification struct {
	ID            int64     `json:"id"`
	Type          string    `json:"type"` // grace_reminder | no_show_released | room_freed | collision | overstay
	Level         int       `json:"level,omitempty"`
	WorkspaceID   string    `json:"workspace_id,omitempty"`
	ReservationID string    `json:"reservation_id,omitempty"`
	Recipient     string    `json:"recipient,omitempty"` // booker; "" = broadcast
	Title         string    `json:"title"`
	Body          string    `json:"body"`
	CreatedAt     time.Time `json:"created_at"`
}

type notifier struct {
	mu     sync.Mutex
	max    int
	seq    int64
	list   []Notification
	sent   map[string]bool    // dedup key -> already emitted
	onEmit func(Notification) // set when APNs is configured; called on fresh emits only
}

func newNotifier(max int) *notifier { return &notifier{max: max, sent: map[string]bool{}} }

// emit appends a notification unless its dedup key was already emitted. An empty
// key disables dedup. Returns whether it was newly emitted.
func (n *notifier) emit(key string, note Notification) bool {
	n.mu.Lock()
	if key != "" {
		if n.sent[key] {
			n.mu.Unlock()
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
	cb := n.onEmit
	n.mu.Unlock()
	if cb != nil {
		cb(note)
	}
	return true
}

// remove deletes one notification by id, reporting whether it existed.
func (n *notifier) remove(id int64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, note := range n.list {
		if note.ID == id {
			n.list = append(n.list[:i], n.list[i+1:]...)
			return true
		}
	}
	return false
}

// clear empties the outbox (dedup keys stay so cleared reminders don't refire).
func (n *notifier) clear() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.list = nil
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

// apnsFields maps an outbox notification type to the APNs presentation fields
// of the notification contract (QuickRoom #18). Unknown types get no extras;
// a missing id suppresses the collapse key rather than emitting "grace-".
func apnsFields(note Notification) (category, interruption, collapseID string) {
	collapse := func(prefix, id string) string {
		if id == "" {
			return ""
		}
		return prefix + id
	}
	switch note.Type {
	case "grace_reminder":
		return "GRACE_REMINDER", "time-sensitive", collapse("grace-", note.ReservationID)
	case "no_show_released":
		return "NO_SHOW_RELEASED", "active", collapse("res-", note.ReservationID)
	case "room_freed":
		return "ROOM_FREED", "passive", collapse("freed-", note.WorkspaceID)
	case "collision":
		return "COLLISION", "time-sensitive", collapse("res-", note.ReservationID)
	case "overstay":
		return "OVERSTAY", "active", collapse("res-", note.ReservationID)
	}
	return "", "", ""
}

// notificationPusher is what the fan-out needs from the APNs client;
// interface so tests can fake it.
type notificationPusher interface {
	Push(ctx context.Context, deviceToken string, n apns.Notification) error
}

// pushNotification delivers one freshly emitted outbox notification to the
// relevant device tokens. Recipient "" = broadcast (room_freed); otherwise
// the recipient is bookerOf() output — email when known, else a user id.
// Fire-and-forget: failures are logged, the outbox stays the source of truth.
func (s *Server) pushNotification(p notificationPusher, note Notification) {
	var tokens []string
	var err error
	if note.Recipient == "" {
		tokens, err = s.db.AllAPNSTokens()
	} else {
		user, ok, lookupErr := s.db.UserByID(note.Recipient)
		if lookupErr == nil && !ok {
			user, ok, lookupErr = s.db.UserByEmail(note.Recipient)
		}
		if lookupErr != nil {
			s.log.Error("apns recipient lookup", "recipient", note.Recipient, "err", lookupErr)
			return
		}
		if !ok {
			return // Zoom-sourced booker without an app account: normal, drop
		}
		tokens, err = s.db.APNSTokensForUser(user.UserID)
	}
	if err != nil {
		s.log.Error("apns token lookup", "err", err)
		return
	}

	category, interruption, collapseID := apnsFields(note)
	payload := apns.Notification{
		Title: note.Title, Body: note.Body, Type: note.Type,
		WorkspaceID: note.WorkspaceID, ReservationID: note.ReservationID,
		Category: category, ThreadID: note.WorkspaceID,
		CollapseID: collapseID, InterruptionLevel: interruption,
	}
	for _, tok := range tokens {
		go func(tok string) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			err := p.Push(ctx, tok, payload)
			switch {
			case errors.Is(err, apns.ErrUnregistered):
				if delErr := s.db.DeleteAPNSToken(tok); delErr != nil {
					s.log.Error("prune apns token", "err", delErr)
				}
			case err != nil:
				s.log.Error("apns push", "type", note.Type, "err", err)
			}
		}(tok)
	}
}
