package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"quickroom/internal/domain"
	"quickroom/internal/store"
)

// Collision is a live booker-vs-occupant mismatch: a room booked by one person
// but physically occupied by someone else (the booker isn't among the present
// users). Presence is the truth for "occupied"; the booking says who *should* be
// there. When they disagree we flag rather than silently check in whoever walked
// in — so the booker and an admin can resolve it instead of the conflict hiding
// behind a green "checked in".
type Collision struct {
	WorkspaceID   string    `json:"workspace_id"`
	RoomName      string    `json:"room_name"`
	ReservationID string    `json:"reservation_id"`
	Booker        string    `json:"booker"`
	Occupants     []string  `json:"occupants"`
	Since         time.Time `json:"since"`
}

// normIdent normalises an identity for matching: lower-cased, reduced to an
// email's local-part ("Standup@adabali.dev" -> "standup"), then stripped to
// alphanumerics so separators don't matter ("demo.day" and "Demo Day" both ->
// "demoday").
func normIdent(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexByte(s, '@'); i > 0 {
		s = s[:i]
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// identityMatch reports whether an occupant identity refers to the same person
// as the booker. Bookers are known by email or user id; occupants by display
// name or user id, so a display name may carry extra words ("Standup Team" vs
// "standup@..."). We match on normalised equality or a containment either way,
// guarded by a min length so short tokens don't match spuriously. Heuristic by
// design — enough to tell "the booker is here" from "a stranger is".
func identityMatch(booker, occupant string) bool {
	b, o := normIdent(booker), normIdent(occupant)
	if len(b) < 2 || len(o) < 2 {
		return false
	}
	if b == o {
		return true
	}
	if len(b) >= 3 && strings.Contains(o, b) {
		return true
	}
	if len(o) >= 3 && strings.Contains(b, o) {
		return true
	}
	return false
}

// bookerPresent reports whether the booker is among the present occupants.
// Exact id equality wins first: app presence carries the account user id and
// the reservation records the same id — this is what keeps a Sign in with
// Apple private-relay booker (whose email tells us nothing) from colliding
// with their own presence. The name heuristic remains as the fallback for
// Zoom-side bookings that only know an email.
func bookerPresent(r domain.Reservation, occupants []store.Ident) bool {
	booker := bookerOf(r)
	for _, o := range occupants {
		if o.ID != "" && (o.ID == r.BookedByUserID || o.ID == r.UserID) {
			return true
		}
		if identityMatch(booker, o.Name) || identityMatch(booker, o.ID) {
			return true
		}
	}
	return false
}

// currentCollisions computes live booker-vs-occupant mismatches: for each booked
// reservation inside its window whose room is occupied but by nobody matching the
// booker. Pure (no side effects) so the /collisions endpoint and the sweep share
// one definition of "in conflict right now".
func (s *Server) currentCollisions(now time.Time) []Collision {
	occ := s.store.AllOccupancyIdents()
	out := []Collision{}
	for _, r := range s.store.Reservations() {
		if r.Status != domain.StatusBooked {
			continue // released / no-show bookings can't collide
		}
		if now.Before(r.StartTime) || !now.Before(r.EndTime) {
			continue // only during the booked window
		}
		idents := occ[r.ZoomWorkspaceID]
		if len(idents) == 0 {
			continue // empty — that's the no-show path, not a collision
		}
		if bookerPresent(r, idents) {
			continue // the booker is here — legitimate use
		}
		users := make([]string, len(idents))
		for i, o := range idents {
			users[i] = o.Name
		}
		out = append(out, Collision{
			WorkspaceID:   r.ZoomWorkspaceID,
			RoomName:      s.roomName(r.ZoomWorkspaceID),
			ReservationID: r.ReservationID,
			Booker:        bookerOf(r),
			Occupants:     users,
			Since:         now,
		})
	}
	return out
}

// sweepCollisions detects live conflicts and emits one notification per
// reservation (deduped) — a heads-up to the booker and a broadcast for the admin
// panel. Non-destructive: presence stays the truth and the room isn't released;
// a human resolves it. Returns the live collisions.
func (s *Server) sweepCollisions(now time.Time) []Collision {
	cs := s.currentCollisions(now)
	for _, c := range cs {
		s.notify.emit(c.ReservationID+"|collision", Notification{
			Type: "collision", WorkspaceID: c.WorkspaceID, ReservationID: c.ReservationID,
			Recipient: c.Booker, Title: "Someone's in your room",
			Body:      fmt.Sprintf("%s is booked to you but someone else is using it right now.", c.RoomName),
			CreatedAt: now,
		})
		s.notify.emit(c.ReservationID+"|collision-admin", Notification{
			Type: "collision", WorkspaceID: c.WorkspaceID, ReservationID: c.ReservationID,
			AdminOnly: true, Title: "Booking conflict",
			Body:      fmt.Sprintf("%s is occupied by someone other than the booker (%s).", c.RoomName, c.Booker),
			CreatedAt: now,
		})
	}
	return cs
}

// getCollisions serves the live booker-vs-occupant conflicts for the admin panel.
func (s *Server) getCollisions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"collisions": s.currentCollisions(time.Now())})
}
