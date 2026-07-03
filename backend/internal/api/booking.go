package api

import (
	"net/http"
	"time"

	"quickroom/internal/domain"
)

// conflictingReservation reports the first non-cancelled, non-released,
// non-no-show reservation for workspaceID that overlaps [start, end) —
// regardless of Source, per the product decision that app bookings must not
// collide with Zoom-synced ones either.
func (s *Server) conflictingReservation(workspaceID string, start, end time.Time) (domain.Reservation, bool) {
	for _, r := range s.store.Reservations() {
		if r.ZoomWorkspaceID != workspaceID {
			continue
		}
		switch r.Status {
		case domain.StatusReleased, domain.StatusCancelled, domain.StatusNoShow:
			continue
		}
		if start.Before(r.EndTime) && r.StartTime.Before(end) {
			return r, true
		}
	}
	return domain.Reservation{}, false
}

// createReservation books a room for the signed-in user.
func (s *Server) createReservation(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	var body struct {
		WorkspaceID string    `json:"workspace_id"`
		StartTime   time.Time `json:"start_time"`
		EndTime     time.Time `json:"end_time"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.WorkspaceID == "" || len(body.WorkspaceID) > maxIDLen {
		writeError(w, http.StatusUnprocessableEntity, "workspace_id required; 1..128 chars")
		return
	}
	if !body.EndTime.After(body.StartTime) {
		writeError(w, http.StatusUnprocessableEntity, "end_time must be after start_time")
		return
	}

	room, ok := s.store.RoomByWorkspace(body.WorkspaceID)
	if !ok {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	if conflict, has := s.conflictingReservation(body.WorkspaceID, body.StartTime, body.EndTime); has {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "room already booked in that window",
			"conflict": conflict,
		})
		return
	}

	res := domain.Reservation{
		ReservationID:   newReservationID(),
		RoomID:          room.RoomID,
		ZoomWorkspaceID: body.WorkspaceID,
		UserID:          user.UserID,
		UserEmail:       user.Email,
		StartTime:       body.StartTime,
		EndTime:         body.EndTime,
		Status:          domain.StatusBooked,
		CheckInStatus:   domain.NotCheckedIn,
		Source:          "app",
		BookedByUserID:  user.UserID,
	}
	s.upsertReservation(res)
	writeJSON(w, http.StatusOK, res)
}

// listMyReservations returns the signed-in user's own bookings.
func (s *Server) listMyReservations(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	mine := []domain.Reservation{}
	for _, res := range s.store.Reservations() {
		if res.BookedByUserID == user.UserID {
			mine = append(mine, res)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reservations": mine})
}

// cancelReservation cancels the caller's own app-sourced booking. 404 if it
// doesn't exist, 403 if it belongs to someone else or isn't app-sourced
// (Zoom-synced reservations aren't cancellable through this endpoint).
func (s *Server) cancelReservation(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	id := r.PathValue("id")
	res, ok := s.store.Reservation(id)
	if !ok {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if res.Source != "app" || res.BookedByUserID != user.UserID {
		writeError(w, http.StatusForbidden, "not your reservation")
		return
	}
	res.Status = domain.StatusCancelled
	s.upsertReservation(res)
	writeJSON(w, http.StatusOK, res)
}
