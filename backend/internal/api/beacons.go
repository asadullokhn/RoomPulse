package api

import (
	"net/http"

	"quickroom/internal/domain"
	"quickroom/internal/store"
)

const (
	maxUUIDLen  = maxIDLen // reuse the existing 128-char id bound
	maxBeaconID = 65535    // iBeacon major/minor are 16-bit unsigned
)

// beaconView is a beacon entry joined to its room name — the shape returned
// by GET, PUT, and (implicitly, via the deleted resource) DELETE /beacons.
type beaconView struct {
	WorkspaceID string `json:"workspace_id"`
	UUID        string `json:"uuid"`
	Major       int    `json:"major"`
	Minor       int    `json:"minor"`
	Name        string `json:"name"`
}

func (s *Server) toBeaconView(b domain.Beacon) beaconView {
	name := ""
	if room, ok := s.store.RoomByWorkspace(b.WorkspaceID); ok {
		name = room.Name
	}
	return beaconView{WorkspaceID: b.WorkspaceID, UUID: b.UUID, Major: b.Major, Minor: b.Minor, Name: name}
}

// listBeacons returns the room↔iBeacon registry, each entry joined to its room
// name. The mobile app polls this to learn which beacons to range/monitor, so
// rooms can be added or re-mapped without shipping a new build.
func (s *Server) listBeacons(w http.ResponseWriter, _ *http.Request) {
	bs := s.store.Beacons()
	out := make([]beaconView, 0, len(bs))
	for _, b := range bs {
		out = append(out, s.toBeaconView(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"beacons": out})
}

// persistBeacons best-effort writes the full current beacon registry to
// BeaconsFile so admin edits survive a restart. Logged, not fatal, on failure
// — the in-memory state (already applied by the caller) is authoritative for
// the running process either way.
func (s *Server) persistBeacons() {
	if s.beaconsFile == "" {
		return
	}
	if err := store.SaveBeacons(s.beaconsFile, s.store.Beacons()); err != nil {
		s.log.Warn("persist beacons", "err", err)
	}
}

// putBeacon creates or replaces the beacon mapping for a room (idempotent
// upsert — the same operation whether or not one already existed, so there's
// no separate POST-for-create route).
func (s *Server) putBeacon(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspace_id")
	if _, ok := s.store.RoomByWorkspace(workspaceID); !ok {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	var body struct {
		UUID  string `json:"uuid"`
		Major int    `json:"major"`
		Minor int    `json:"minor"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.UUID == "" || len(body.UUID) > maxUUIDLen {
		writeError(w, http.StatusUnprocessableEntity, "uuid required; 1..128 chars")
		return
	}
	if body.Major < 0 || body.Major > maxBeaconID || body.Minor < 0 || body.Minor > maxBeaconID {
		writeError(w, http.StatusUnprocessableEntity, "major and minor must be 0..65535")
		return
	}

	b := domain.Beacon{WorkspaceID: workspaceID, UUID: body.UUID, Major: body.Major, Minor: body.Minor}
	s.store.SetBeacon(b)
	s.persistBeacons()
	writeJSON(w, http.StatusOK, s.toBeaconView(b))
}

// deleteBeacon removes a room's beacon mapping. 404 if none exists.
func (s *Server) deleteBeacon(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspace_id")
	if _, ok := s.store.Beacon(workspaceID); !ok {
		writeError(w, http.StatusNotFound, "beacon not found")
		return
	}
	s.store.RemoveBeacon(workspaceID)
	s.persistBeacons()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
