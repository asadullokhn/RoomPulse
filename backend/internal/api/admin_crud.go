package api

import (
	"net/http"
	"strconv"
	"time"

	"quickroom/internal/domain"
)

// adminCreateReservation books a room on someone's behalf. The booking is
// app-sourced (this service owns its lifecycle) with the admin as booker;
// user_email routes notifications to the intended person.
func (s *Server) adminCreateReservation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceID string    `json:"workspace_id"`
		StartTime   time.Time `json:"start_time"`
		EndTime     time.Time `json:"end_time"`
		UserEmail   string    `json:"user_email"`
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
		UserID:          "admin",
		UserEmail:       clamp(body.UserEmail, maxNameLen),
		StartTime:       body.StartTime,
		EndTime:         body.EndTime,
		Status:          domain.StatusBooked,
		CheckInStatus:   domain.NotCheckedIn,
		Source:          "app",
		BookedByUserID:  "admin",
	}
	s.upsertReservation(res)
	writeJSON(w, http.StatusOK, res)
}

// adminPatchReservation moves an app-sourced booking's window. Zoom-sourced
// reservations are a synced mirror — edits there would be overwritten within
// a minute, so they are rejected instead of silently lied about.
func (s *Server) adminPatchReservation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, ok := s.store.Reservation(id)
	if !ok {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}
	if res.Source != "app" {
		writeError(w, http.StatusForbidden, "only app-sourced bookings can be edited")
		return
	}

	var body struct {
		StartTime *time.Time `json:"start_time"`
		EndTime   *time.Time `json:"end_time"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.StartTime == nil && body.EndTime == nil {
		writeError(w, http.StatusUnprocessableEntity, "nothing to change")
		return
	}
	start, end := res.StartTime, res.EndTime
	if body.StartTime != nil {
		start = *body.StartTime
	}
	if body.EndTime != nil {
		end = *body.EndTime
	}
	if !end.After(start) {
		writeError(w, http.StatusUnprocessableEntity, "end_time must be after start_time")
		return
	}
	if conflict, has := s.conflictingReservation(res.ZoomWorkspaceID, start, end); has && conflict.ReservationID != id {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "room already booked in that window",
			"conflict": conflict,
		})
		return
	}

	res.StartTime, res.EndTime = start, end
	s.upsertReservation(res)
	writeJSON(w, http.StatusOK, res)
}

// deleteNotification removes one outbox entry; clearNotifications empties it.
func (s *Server) deleteNotification(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.notify.remove(id) {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) clearNotifications(w http.ResponseWriter, _ *http.Request) {
	s.notify.clear()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// patchUser renames an account (identities come from Sign in with Apple, so
// name is the only editable field).
func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, ok, err := s.db.UserByID(userID); err != nil {
		s.log.Error("lookup user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == "" || len(body.Name) > maxNameLen {
		writeError(w, http.StatusUnprocessableEntity, "name required; 1..96 chars")
		return
	}
	if err := s.db.UpdateUserName(userID, body.Name); err != nil {
		s.log.Error("rename user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
