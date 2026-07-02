package api

import (
	"fmt"
	"net/http"
	"time"

	"quickroom/internal/domain"
)

// Overstay is a booking that has ended while the room is still occupied — the
// inverse of a no-show. The slot is over but people haven't left, so the next
// booker is squeezed out. We flag it once the end passes by overstayGrace so the
// current occupants get a "time's up" nudge and the admin panel sees the squeeze.
type Overstay struct {
	WorkspaceID   string    `json:"workspace_id"`
	RoomName      string    `json:"room_name"`
	ReservationID string    `json:"reservation_id"`
	Booker        string    `json:"booker"`
	Occupants     []string  `json:"occupants"`
	EndedAt       time.Time `json:"ended_at"`
	OverBySec     int64     `json:"over_by_sec"`
}

// hasActiveBooking reports whether some OTHER reservation for the workspace owns
// the room right now (its window covers now, and it isn't released/no-show). Used
// to avoid mis-flagging back-to-back bookings: if a following booking has begun,
// the occupancy is theirs, not an overstay of the one that ended.
func (s *Server) hasActiveBooking(workspaceID, excludeID string, now time.Time) bool {
	for _, r := range s.store.Reservations() {
		if r.ReservationID == excludeID || r.ZoomWorkspaceID != workspaceID {
			continue
		}
		if r.Status == domain.StatusReleased || r.Status == domain.StatusNoShow {
			continue
		}
		if !now.Before(r.StartTime) && now.Before(r.EndTime) {
			return true
		}
	}
	return false
}

// currentOverstays computes rooms occupied past their booking's end by more than
// overstayGrace, unless a following booking now owns the room. Pure; shared by
// the /overstays endpoint and the sweep.
func (s *Server) currentOverstays(now time.Time) []Overstay {
	occ := s.store.AllOccupancy()
	out := []Overstay{}
	for _, r := range s.store.Reservations() {
		if now.Before(r.EndTime.Add(s.overstayGrace)) {
			continue // not ended yet, or still inside the wrap-up grace
		}
		users := occ[r.ZoomWorkspaceID]
		if len(users) == 0 {
			continue // room emptied out — no overstay
		}
		if s.hasActiveBooking(r.ZoomWorkspaceID, r.ReservationID, now) {
			continue // a following booking owns the room now
		}
		out = append(out, Overstay{
			WorkspaceID:   r.ZoomWorkspaceID,
			RoomName:      s.roomName(r.ZoomWorkspaceID),
			ReservationID: r.ReservationID,
			Booker:        bookerOf(r),
			Occupants:     users,
			EndedAt:       r.EndTime,
			OverBySec:     int64(now.Sub(r.EndTime) / time.Second),
		})
	}
	return out
}

// sweepOverstays flags live overstays and emits one notification per reservation
// (deduped): a "time's up" nudge to the booker and an admin broadcast. Returns
// the live overstays.
func (s *Server) sweepOverstays(now time.Time) []Overstay {
	os := s.currentOverstays(now)
	for _, o := range os {
		over := (time.Duration(o.OverBySec) * time.Second).Round(time.Minute)
		s.notify.emit(o.ReservationID+"|overstay", Notification{
			Type: "overstay", WorkspaceID: o.WorkspaceID, ReservationID: o.ReservationID,
			Recipient: o.Booker, Title: "Time's up",
			Body:      fmt.Sprintf("Your booking for %s ended %s ago but the room is still in use — please wrap up.", o.RoomName, over),
			CreatedAt: now,
		})
		s.notify.emit(o.ReservationID+"|overstay-admin", Notification{
			Type: "overstay", WorkspaceID: o.WorkspaceID, ReservationID: o.ReservationID,
			Title: "Room overstay",
			Body:  fmt.Sprintf("%s is occupied past its booked end.", o.RoomName),
			CreatedAt: now,
		})
	}
	return os
}

// getOverstays serves the live overstays for the admin panel.
func (s *Server) getOverstays(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"overstays": s.currentOverstays(time.Now())})
}
