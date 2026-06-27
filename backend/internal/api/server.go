// Package api exposes the prototype's HTTP surface using the stdlib mux
// (Go 1.22+ method+path patterns). Production should adopt chi per Go rules.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"roompulse/internal/domain"
	syncsvc "roompulse/internal/sync"
	"roompulse/internal/store"
	"roompulse/internal/zoom"
)

//go:embed dashboard.html
var dashboardHTML []byte

//go:embed floor.html
var floorHTML []byte

//go:embed floor.png
var floorImage []byte

//go:embed floor.json
var floorData []byte // raw Zoom workspace export (rooms + polygons)

const (
	maxBody    = 1 << 20 // 1 MiB request body cap
	maxIDLen   = 128
	maxNameLen = 96
)

// OAuthFlow is implemented by the user-OAuth Zoom client. When the active
// client supports it, the server exposes /oauth/login and /oauth/callback.
type OAuthFlow interface {
	AuthCodeURL() string
	Exchange(ctx context.Context, code, state string) error
	Authorized() bool
}

// Server wires handlers over the store, sync service and Zoom client.
type Server struct {
	store *store.Memory
	db    *store.DB // durable device registry (SQLite)
	sync  *syncsvc.Service
	zoom  zoom.Client
	oauth OAuthFlow // non-nil only in user mode
	mode  string
	ttl   time.Duration // presence stale-after window
	log   *slog.Logger
}

func NewServer(st *store.Memory, db *store.DB, sync *syncsvc.Service, zc zoom.Client, mode string, ttl time.Duration, log *slog.Logger) *Server {
	s := &Server{store: st, db: db, sync: sync, zoom: zc, mode: mode, ttl: ttl, log: log}
	if of, ok := zc.(OAuthFlow); ok {
		s.oauth = of
	}
	return s
}

// ReapLoop periodically expires devices not seen within the TTL and reflects
// check-out on the rooms they vacated — the backstop for a killed/offline phone
// that never sent a leave. Bind ctx to the app's root context.
func (s *Server) ReapLoop(ctx context.Context) {
	ticker := time.NewTicker(s.ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			vacated := s.store.ReapStale(s.ttl)
			for _, ws := range vacated {
				c, cancel := context.WithTimeout(ctx, 5*time.Second)
				s.driveReservation(c, ws, zoom.EventCheckOut, domain.CheckedOut)
				cancel()
			}
			if len(vacated) > 0 {
				s.log.Info("reaped stale presence", "rooms", vacated, "ttl", s.ttl)
			}
		}
	}
}

// Handler builds the routed mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.dashboard)
	mux.HandleFunc("GET /floor", s.floor)
	mux.HandleFunc("GET /floor/image", s.floorImageHandler)
	mux.HandleFunc("GET /floor/rooms", s.floorRooms)
	mux.HandleFunc("GET /info", s.info)
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.live)
	mux.HandleFunc("POST /sync", s.runSync)
	mux.HandleFunc("GET /rooms", s.listRooms)
	mux.HandleFunc("GET /beacons", s.listBeacons)
	mux.HandleFunc("GET /devices", s.listDevices)
	mux.HandleFunc("GET /reservations", s.listReservations)
	mux.HandleFunc("POST /reservations/{id}/check-in", s.checkIn)
	mux.HandleFunc("POST /reservations/{id}/check-out", s.checkOut)
	mux.HandleFunc("POST /presence", s.presence)
	mux.HandleFunc("POST /presence/heartbeat", s.heartbeat)
	mux.HandleFunc("GET /occupancy", s.occupancy)
	if s.oauth != nil {
		mux.HandleFunc("GET /oauth/login", s.oauthLogin)
		mux.HandleFunc("GET /oauth/callback", s.oauthCallback)
	}
	return recovery(s.log, logging(s.log, mux))
}

func (s *Server) oauthLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, s.oauth.AuthCodeURL(), http.StatusFound)
}

func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		writeError(w, http.StatusBadRequest, "zoom denied authorization: "+errMsg)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing authorization code")
		return
	}
	if err := s.oauth.Exchange(r.Context(), code, state); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Pull data immediately so the user sees results right after login.
	if _, err := s.sync.Run(r.Context(), time.Now()); err != nil {
		s.log.Warn("post-auth sync failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "authorized",
		"next":   "open /reservations and /rooms to see your data",
	})
}

func (s *Server) dashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}

func (s *Server) floor(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(floorHTML)
}

func (s *Server) floorImageHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(floorImage)
}

// floorRooms projects the raw Zoom export into just what the floor view needs:
// each room's label and polygon, plus the transform that maps the polygon's
// coordinate space onto the floor image (calibrated against floor.png).
func (s *Server) floorRooms(w http.ResponseWriter, _ *http.Request) {
	type entry struct {
		Name     string      `json:"name"`
		Points   [][]float64 `json:"points"`
		Kind     string      `json:"kind"` // "room" or "workspace"
		Capacity int         `json:"capacity"`
		Screens  int         `json:"screens"`
		Busy     bool        `json:"busy"`
	}
	var src struct {
		Data []struct {
			Name     string `json:"locationName"`
			Points   string `json:"points"`
			DeskType string `json:"deskType"`
			Capacity int    `json:"roomCapacity"`
			Screens  int    `json:"roomScreenCount"`
			Busy     bool   `json:"roomBusy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(floorData, &src); err != nil {
		writeError(w, http.StatusInternalServerError, "floor data unreadable")
		return
	}
	rooms := make([]entry, 0, len(src.Data))
	for _, d := range src.Data {
		var pts [][]float64
		if json.Unmarshal([]byte(d.Points), &pts); len(pts) < 3 {
			continue
		}
		kind := "workspace"
		if d.DeskType == "room" {
			kind = "room"
		}
		rooms = append(rooms, entry{d.Name, pts, kind, d.Capacity, d.Screens, d.Busy})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rooms": rooms,
		// Polygon coordinate window that exactly covers the image, calibrated
		// against the rendered floor plan (see floor.html).
		"view_box": map[string]float64{"x": 1.9, "y": 153.0, "w": 1209.3, "h": 682.0},
		"image":    map[string]int{"w": 2489, "h": 1380},
	})
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"zoom_mode":  s.mode,
		"authorized": s.oauth == nil || s.oauth.Authorized(),
	})
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) runSync(w http.ResponseWriter, r *http.Request) {
	res, err := s.sync.Run(r.Context(), time.Now())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listRooms(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"rooms": s.store.Rooms()})
}

func (s *Server) listReservations(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"reservations": s.store.Reservations()})
}

// listBeacons returns the room↔iBeacon registry, each entry joined to its room
// name. The mobile app polls this to learn which beacons to range/monitor, so
// rooms can be added or re-mapped without shipping a new build.
func (s *Server) listBeacons(w http.ResponseWriter, _ *http.Request) {
	type entry struct {
		WorkspaceID string `json:"workspace_id"`
		UUID        string `json:"uuid"`
		Major       int    `json:"major"`
		Minor       int    `json:"minor"`
		Name        string `json:"name"`
	}
	bs := s.store.Beacons()
	out := make([]entry, 0, len(bs))
	for _, b := range bs {
		name := ""
		if room, ok := s.store.RoomByWorkspace(b.WorkspaceID); ok {
			name = room.Name
		}
		out = append(out, entry{WorkspaceID: b.WorkspaceID, UUID: b.UUID, Major: b.Major, Minor: b.Minor, Name: name})
	}
	writeJSON(w, http.StatusOK, map[string]any{"beacons": out})
}

// listDevices returns the durable device registry (from SQLite). Each row
// carries the device's last known room as a workspace id; the dashboard joins
// it to the room name it already has from /rooms.
func (s *Server) listDevices(w http.ResponseWriter, _ *http.Request) {
	devices, err := s.db.Devices(time.Now())
	if err != nil {
		s.log.Error("list devices", "err", err)
		writeError(w, http.StatusInternalServerError, "could not read devices")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

// occupancy reports how many people (phones) are currently in each room.
func (s *Server) occupancy(w http.ResponseWriter, _ *http.Request) {
	type entry struct {
		WorkspaceID string   `json:"workspace_id"`
		Count       int      `json:"count"`
		Users       []string `json:"users"`
	}
	all := s.store.AllOccupancy()
	out := make([]entry, 0, len(all))
	for ws, users := range all {
		out = append(out, entry{WorkspaceID: ws, Count: len(users), Users: users})
	}
	writeJSON(w, http.StatusOK, map[string]any{"occupancy": out})
}

func (s *Server) checkIn(w http.ResponseWriter, r *http.Request) {
	s.checkEvent(w, r, zoom.EventCheckIn, domain.CheckedIn)
}

func (s *Server) checkOut(w http.ResponseWriter, r *http.Request) {
	s.checkEvent(w, r, zoom.EventCheckOut, domain.CheckedOut)
}

// heartbeat reconciles a device's CURRENT room (idempotent full state, not a
// delta), so a lost message can't strand a user "in" a room — the next
// heartbeat corrects it. The iOS app calls this every few seconds.
func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceID    string `json:"device_id"`
		DisplayName string `json:"display_name"`
		WorkspaceID string `json:"workspace_id"` // "" = not in any room
		TS          int64  `json:"ts"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.DeviceID) == 0 || len(body.DeviceID) > maxIDLen || len(body.WorkspaceID) > maxIDLen {
		writeError(w, http.StatusUnprocessableEntity, "device_id required; ids must be 1..128 chars")
		return
	}
	body.DisplayName = clamp(body.DisplayName, maxNameLen)

	// Durable registry: every heartbeat refreshes the device's last-seen and room.
	if err := s.db.UpsertDevice(body.DeviceID, body.DisplayName, body.WorkspaceID, time.Now()); err != nil {
		s.log.Warn("persist device", "device", body.DeviceID, "err", err)
	}

	changed, prev := s.store.SetDeviceRoom(body.DeviceID, body.WorkspaceID, body.DisplayName, body.TS)
	if changed {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if body.WorkspaceID != "" {
			s.driveReservation(ctx, body.WorkspaceID, zoom.EventCheckIn, domain.CheckedIn)
		}
		if prev != "" {
			s.driveReservation(ctx, prev, zoom.EventCheckOut, domain.CheckedOut)
		}
		s.log.Info("presence state change", "device", body.DeviceID, "room", body.WorkspaceID, "prev", prev)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "room": body.WorkspaceID})
}

// driveReservation reflects a room's occupancy onto its booking's check-in
// state (best-effort; Zoom stays the source of truth).
func (s *Server) driveReservation(ctx context.Context, workspaceID string, event zoom.EventType, newStatus domain.CheckInStatus) {
	res, ok := s.store.ReservationByWorkspace(workspaceID)
	if !ok {
		return
	}
	if err := s.zoom.SendEvent(ctx, event, res.ReservationID); err != nil {
		s.log.Warn("driveReservation", "err", err)
		return
	}
	res.CheckInStatus = newStatus
	s.store.UpsertReservation(res)
}

// presence ingests a phone's arrive/leave event for a room and drives check-in
// (entered) or check-out (exited) on the matching reservation. Used by the
// dashboard sim buttons.
func (s *Server) presence(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkspaceID string  `json:"workspace_id"`
		UserID      string  `json:"user_id"`
		DisplayName string  `json:"display_name"`
		EventType   string  `json:"event_type"`
		Confidence  float64 `json:"confidence"`
		EventTS     int64   `json:"event_ts"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.UserID == "" || len(body.UserID) > maxIDLen || body.WorkspaceID == "" || len(body.WorkspaceID) > maxIDLen {
		writeError(w, http.StatusUnprocessableEntity, "user_id and workspace_id required; 1..128 chars")
		return
	}
	body.DisplayName = clamp(body.DisplayName, maxNameLen)

	var event zoom.EventType
	var newStatus domain.CheckInStatus
	switch body.EventType {
	case "entered":
		event, newStatus = zoom.EventCheckIn, domain.CheckedIn
	case "exited":
		event, newStatus = zoom.EventCheckOut, domain.CheckedOut
	default:
		writeError(w, http.StatusUnprocessableEntity, "event_type must be 'entered' or 'exited'")
		return
	}

	// Headcount: apply only if this event is newer than the last for this
	// (workspace, user). Drops out-of-order/flap events so state can't corrupt.
	if !s.store.ApplyPresenceIfNewer(body.WorkspaceID, body.UserID, body.DisplayName, body.EventTS, body.EventType == "entered") {
		writeJSON(w, http.StatusOK, map[string]any{"status": "stale_ignored", "workspace_id": body.WorkspaceID})
		return
	}

	// Presence (headcount) is tracked above regardless of bookings. Below we
	// best-effort drive the booker's reservation check-in/out.
	res, ok := s.store.ReservationByWorkspace(body.WorkspaceID)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "recorded",
			"workspace_id": body.WorkspaceID,
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.zoom.SendEvent(ctx, event, res.ReservationID); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	res.CheckInStatus = newStatus
	s.store.UpsertReservation(res)
	s.log.Info("presence applied", "event", body.EventType, "workspace", body.WorkspaceID, "user", body.UserID)
	writeJSON(w, http.StatusOK, res)
}

// checkEvent sends the event to Zoom, then reflects it locally. Zoom stays the
// source of truth, so we only update the local mirror after Zoom accepts.
func (s *Server) checkEvent(w http.ResponseWriter, r *http.Request, event zoom.EventType, newStatus domain.CheckInStatus) {
	id := r.PathValue("id")
	res, ok := s.store.Reservation(id)
	if !ok {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := s.zoom.SendEvent(ctx, event, id); err != nil {
		if errors.Is(err, zoom.ErrReservationNotFound) {
			writeError(w, http.StatusNotFound, "reservation not found in zoom")
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	res.CheckInStatus = newStatus
	s.store.UpsertReservation(res)
	writeJSON(w, http.StatusOK, res)
}

// --- helpers ---------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeBody caps the request body and decodes JSON into v.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	return json.NewDecoder(r.Body).Decode(v)
}

// clamp trims a string to at most n runes (defends the dashboard + store).
func clamp(s string, n int) string {
	rs := []rune(s)
	if len(rs) > n {
		return string(rs[:n])
	}
	return s
}

// recovery turns a panic in any handler into a 500 instead of a dropped
// connection, and must wrap (be outside) the logging middleware.
func recovery(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic recovered", "err", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Info("request", "method", r.Method, "path", r.URL.Path, "dur_ms", time.Since(start).Milliseconds())
	})
}
