package api

import (
	"net/http"
	"strings"
	"time"

	"quickroom/internal/domain"
	"quickroom/internal/store"
)

const customRoomPrefix = "cr-"

// createRoom adds an admin-owned ("custom") room. Custom rooms live in SQLite
// and are re-applied after every Zoom sync, so they behave like real rooms
// everywhere (booking, beacons, occupancy) without a Zoom counterpart.
func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Capacity int    `json:"capacity"`
		HasTV    bool   `json:"has_tv"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == "" || len(body.Name) > maxNameLen {
		writeError(w, http.StatusUnprocessableEntity, "name required; 1..96 chars")
		return
	}
	if body.Capacity < 0 {
		writeError(w, http.StatusUnprocessableEntity, "capacity must be >= 0")
		return
	}

	ws := customRoomPrefix + randomPrefixedID("")[:8]
	room := domain.Room{
		RoomID:          "room-" + ws,
		ZoomWorkspaceID: ws,
		Name:            body.Name,
		Capacity:        body.Capacity,
		HasTV:           body.HasTV,
		IsZoomRoom:      false,
	}
	if err := s.db.SaveCustomRoom(room, time.Now()); err != nil {
		s.log.Error("save custom room", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.store.UpsertRoom(room)
	writeJSON(w, http.StatusOK, room)
}

// patchRoom edits a room. Custom rooms are edited directly; Zoom-synced rooms
// get a persistent override that is re-applied after every sync (the mirror
// would otherwise clobber the edit within a minute).
func (s *Server) patchRoom(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspace_id")
	room, ok := s.store.RoomByWorkspace(workspaceID)
	if !ok {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	var body struct {
		Name     *string `json:"name"`
		Capacity *int    `json:"capacity"`
		HasTV    *bool   `json:"has_tv"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == nil && body.Capacity == nil && body.HasTV == nil {
		writeError(w, http.StatusUnprocessableEntity, "nothing to change")
		return
	}
	if body.Name != nil && (*body.Name == "" || len(*body.Name) > maxNameLen) {
		writeError(w, http.StatusUnprocessableEntity, "name must be 1..96 chars")
		return
	}
	if body.Capacity != nil && *body.Capacity < 0 {
		writeError(w, http.StatusUnprocessableEntity, "capacity must be >= 0")
		return
	}

	// Apply to the live mirror immediately.
	if body.Name != nil {
		room.Name = *body.Name
	}
	if body.Capacity != nil {
		room.Capacity = *body.Capacity
	}
	if body.HasTV != nil {
		room.HasTV = *body.HasTV
	}

	if strings.HasPrefix(workspaceID, customRoomPrefix) {
		if err := s.db.SaveCustomRoom(room, time.Now()); err != nil {
			s.log.Error("update custom room", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	} else {
		override := store.RoomOverride{WorkspaceID: workspaceID, Name: "", Capacity: -1, HasTV: -1}
		if body.Name != nil {
			override.Name = *body.Name
		}
		if body.Capacity != nil {
			override.Capacity = *body.Capacity
		}
		if body.HasTV != nil {
			if *body.HasTV {
				override.HasTV = 1
			} else {
				override.HasTV = 0
			}
		}
		if err := s.db.SaveRoomOverride(override); err != nil {
			s.log.Error("save room override", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	s.store.UpsertRoom(room)
	writeJSON(w, http.StatusOK, room)
}

// deleteRoom removes a custom room entirely (cancelling its open app bookings
// and dropping its beacon mapping) or, for a Zoom-synced room, clears its
// override so the next sync restores Zoom truth.
func (s *Server) deleteRoom(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspace_id")
	if _, ok := s.store.RoomByWorkspace(workspaceID); !ok {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	if !strings.HasPrefix(workspaceID, customRoomPrefix) {
		if err := s.db.ClearRoomOverride(workspaceID); err != nil {
			s.log.Error("clear room override", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
		return
	}

	for _, res := range s.store.Reservations() {
		if res.ZoomWorkspaceID != workspaceID || res.Source != "app" || res.Status != domain.StatusBooked {
			continue
		}
		res.Status = domain.StatusCancelled
		s.upsertReservation(res)
	}
	s.store.RemoveBeacon(workspaceID)
	if err := s.db.DeleteCustomRoom(workspaceID); err != nil {
		s.log.Error("delete custom room", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.store.DeleteRoom(workspaceID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
