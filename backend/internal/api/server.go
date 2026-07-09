// Package api exposes the prototype's HTTP surface using the stdlib mux
// (Go 1.22+ method+path patterns). Production should adopt chi per Go rules.
package api

import (
	"compress/gzip"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"quickroom/internal/appleauth"
	"quickroom/internal/authtoken"
	"quickroom/internal/domain"
	"quickroom/internal/store"
	syncsvc "quickroom/internal/sync"
	"quickroom/internal/zoom"
)

//go:embed floor.png
var floorImage []byte

//go:embed floor.json
var floorData []byte // raw Zoom workspace export (rooms + polygons)

//go:embed favicon.svg
var faviconSVG []byte

//go:embed scenarios/*.jpg
var scenarioImages embed.FS // one illustration per scenario, served at /scenarios/img/{id}

const (
	maxBody        = 1 << 20 // 1 MiB request body cap
	maxIDLen       = 128
	maxNameLen     = 96
	eventRetention = 14 * 24 * time.Hour // activity history kept this long

	// graceSweepInterval drives grace reminders + no-show release. Finer than the
	// presence-reap cadence because grace windows on short bookings are minutes.
	graceSweepInterval = 30 * time.Second

	// userPresenceTTL expires phone-reported presence whose exit never arrived
	// (locked phone, dead battery, killed app). Generous — one max-length
	// booking — because a backgrounded phone in the room sends nothing between
	// its enter and exit; the app refreshes the clock on every foreground.
	userPresenceTTL = 2 * time.Hour

	// checkoutLinger is how long a checked-in booking's booker must be absent
	// before the booking flips to checked-out. Stepping out for a coffee must
	// not check you out; coming back within the linger means nothing happened.
	checkoutLinger = 15 * time.Minute
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
	store     *store.Memory
	db        *store.DB // durable device registry (SQLite)
	sync      *syncsvc.Service
	zoom      zoom.Client
	oauth     OAuthFlow // non-nil only in user mode
	mode      string
	ttl       time.Duration // presence stale-after window
	log       *slog.Logger
	diags     *diagBuffer    // recent device diagnostics (in-memory, for GET /diag)
	decisions *decisionStore // next-step choices from /decide (in-memory, for GET /decision)

	scenarioAnswers *scenarioAnswerStore // chosen answer per scenario (in-memory, for GET /scenario-answers)
	history         *historyBuffer       // full device event-log dumps (in-memory, for GET /history)

	// No-show grace model (Reno's proportional window). Set from config via
	// ConfigureGrace; defaults applied in NewServer.
	graceFraction float64
	graceMin      time.Duration
	graceMax      time.Duration

	// Notification outbox + grace-reminder ladder (Reno's model).
	notify              *notifier
	notifyFirstFrac     float64
	notifySecondFrac    float64
	notifySecondEnabled bool

	// Overstay: a room still occupied this long past its booking's end is
	// flagged (the inverse of a no-show). Set via ConfigureOverstay.
	overstayGrace time.Duration

	// absentSince tracks when a checked-in booking's booker was first seen
	// absent, keyed by reservation id — the checkout-linger clock. Touched
	// only from the GraceLoop goroutine.
	absentSince map[string]time.Time

	// Sign in with Apple + JWT issuance.
	appleVerifier *appleauth.Verifier
	userTokenTTL  time.Duration
	signer        *authtoken.Signer

	// BeaconsFile path for persisting admin beacon-mapping edits. Empty
	// disables persistence (in-memory only) — set via ConfigureBeaconsFile.
	beaconsFile string
}

func NewServer(st *store.Memory, db *store.DB, sync *syncsvc.Service, zc zoom.Client, mode string, ttl time.Duration, appleVerifier *appleauth.Verifier, userTokenTTL time.Duration, signer *authtoken.Signer, log *slog.Logger) *Server {
	s := &Server{store: st, db: db, sync: sync, zoom: zc, mode: mode, ttl: ttl, log: log, diags: newDiagBuffer(50), decisions: newDecisionStore(), scenarioAnswers: newScenarioAnswerStore(), history: newHistoryBuffer(20),
		graceFraction: 0.10, graceMin: 90 * time.Second, graceMax: 15 * time.Minute,
		notify: newNotifier(200), notifyFirstFrac: 0.05, notifySecondFrac: 0.075, notifySecondEnabled: true,
		overstayGrace: 5 * time.Minute, absentSince: map[string]time.Time{},
		appleVerifier: appleVerifier, userTokenTTL: userTokenTTL, signer: signer}
	if of, ok := zc.(OAuthFlow); ok {
		s.oauth = of
	}
	return s
}

// ConfigureGrace overrides the no-show grace model (proportional fraction of the
// booking length, clamped to [min,max]). Non-positive values are ignored so
// callers can override selectively.
func (s *Server) ConfigureGrace(fraction float64, min, max time.Duration) {
	if fraction > 0 {
		s.graceFraction = fraction
	}
	if min > 0 {
		s.graceMin = min
	}
	if max > 0 {
		s.graceMax = max
	}
}

// ConfigureNotify sets the grace-reminder ladder: first/second ping fractions of
// the booking elapsed, and whether the second ping fires (Reno flagged
// notification fatigue, so it can be turned off).
func (s *Server) ConfigureNotify(first, second float64, secondEnabled bool) {
	if first > 0 {
		s.notifyFirstFrac = first
	}
	if second > 0 {
		s.notifySecondFrac = second
	}
	s.notifySecondEnabled = secondEnabled
}

// ConfigureBeaconsFile sets the path admin beacon-mapping edits persist to.
// Empty disables persistence (edits apply in-memory only for the process's
// lifetime).
func (s *Server) ConfigureBeaconsFile(path string) {
	s.beaconsFile = path
}

// ConfigureAPNS turns on push delivery for freshly emitted outbox
// notifications. Not calling it leaves the outbox poll-only.
func (s *Server) ConfigureAPNS(p notificationPusher) {
	s.notify.onEmit = func(n Notification) { go s.pushNotification(p, n) }
}

// ConfigureOverstay sets how long a room may stay occupied past its booking's end
// before it's flagged as an overstay. Non-positive values are ignored.
func (s *Server) ConfigureOverstay(grace time.Duration) {
	if grace > 0 {
		s.overstayGrace = grace
	}
}

// GraceLoop drives booking-side maintenance on a short cadence: grace-window
// reminders then no-show release. Separate from ReapLoop (presence TTL) because
// grace windows are measured in minutes. Bind ctx to the app's root context.
func (s *Server) GraceLoop(ctx context.Context) {
	ticker := time.NewTicker(graceSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			s.sweepGraceReminders(now)
			if flagged := s.sweepNoShows(ctx, now); len(flagged) > 0 {
				s.log.Info("released no-show bookings", "count", len(flagged))
			}
			s.sweepDeferredCheckouts(ctx, now)
			if conflicts := s.sweepCollisions(now); len(conflicts) > 0 {
				s.log.Info("booking conflicts flagged", "count", len(conflicts))
			}
			if over := s.sweepOverstays(now); len(over) > 0 {
				s.log.Info("overstays flagged", "count", len(over))
			}
		}
	}
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
			reaped := s.store.ReapStale(s.ttl)
			now := time.Now()
			rooms := map[string]struct{}{}
			for _, rd := range reaped {
				s.logEvent(rd.WorkspaceID, rd.DeviceID, rd.DisplayName, "leave", now)
				rooms[rd.WorkspaceID] = struct{}{}
			}
			reapedUsers := s.store.ReapStaleUserPresence(userPresenceTTL)
			for _, ru := range reapedUsers {
				s.logEvent(ru.WorkspaceID, ru.UserID, ru.DisplayName, "leave", now)
				rooms[ru.WorkspaceID] = struct{}{}
			}
			occ := s.store.AllOccupancy()
			for ws := range rooms {
				if len(occ[ws]) > 0 {
					continue // someone's still inside — don't check the booking out
				}
				c, cancel := context.WithTimeout(ctx, 5*time.Second)
				s.driveReservation(c, ws, zoom.EventCheckOut, domain.CheckedOut)
				cancel()
			}
			if len(reaped) > 0 {
				s.log.Info("reaped stale presence", "devices", len(reaped), "ttl", s.ttl)
			}
			if len(reapedUsers) > 0 {
				s.log.Info("reaped stale user presence", "users", len(reapedUsers), "ttl", userPresenceTTL)
			}
			if err := s.db.PruneEvents(now.Add(-eventRetention)); err != nil {
				s.log.Warn("prune events", "err", err)
			}
		}
	}
}

// Handler builds the routed mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.spaIndex)
	mux.Handle("GET /assets/", http.FileServerFS(webDist))
	mux.HandleFunc("GET /favicon.svg", s.favicon)
	mux.HandleFunc("GET /scenarios/img/{name}", s.scenarioImage)
	mux.HandleFunc("POST /decision", s.requireAdmin(s.postDecision))
	mux.HandleFunc("GET /decision", s.requireAdmin(s.getDecision))
	mux.HandleFunc("POST /scenario-answers", s.requireAdmin(s.postScenarioAnswers))
	mux.HandleFunc("GET /scenario-answers", s.requireAdmin(s.getScenarioAnswers))
	mux.HandleFunc("GET /floor/image", s.requireAuth(s.floorImageHandler))
	mux.HandleFunc("GET /floor/rooms", s.requireAuth(s.floorRooms))
	mux.HandleFunc("GET /info", s.info)
	mux.HandleFunc("GET /docs", s.docs)
	mux.HandleFunc("GET /docs/swagger-ui.css", s.docsAsset(swaggerCSS, "text/css; charset=utf-8"))
	mux.HandleFunc("GET /docs/swagger-ui-bundle.js", s.docsAsset(swaggerBundleJS, "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /docs/swagger-ui-standalone-preset.js", s.docsAsset(swaggerPresetJS, "application/javascript; charset=utf-8"))
	mux.HandleFunc("GET /openapi.yaml", s.openapiYAML)
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.live)
	mux.HandleFunc("POST /sync", s.requireAdmin(s.runSync))
	mux.HandleFunc("GET /rooms", s.requireAuth(s.listRooms))
	mux.HandleFunc("POST /rooms", s.requireAdmin(s.createRoom))
	mux.HandleFunc("PATCH /rooms/{workspace_id}", s.requireAdmin(s.patchRoom))
	mux.HandleFunc("DELETE /rooms/{workspace_id}", s.requireAdmin(s.deleteRoom))
	mux.HandleFunc("GET /beacons", s.requireAuth(s.listBeacons))
	mux.HandleFunc("PUT /beacons/{workspace_id}", s.requireAdmin(s.putBeacon))
	mux.HandleFunc("DELETE /beacons/{workspace_id}", s.requireAdmin(s.deleteBeacon))
	mux.HandleFunc("GET /devices", s.requireAdmin(s.listDevices))
	mux.HandleFunc("GET /events", s.requireAdmin(s.listEvents))
	mux.HandleFunc("GET /reservations", s.requireAuth(s.listReservations))
	mux.HandleFunc("POST /reservations/{id}/check-in", s.requireAdmin(s.checkIn))
	mux.HandleFunc("POST /reservations/{id}/check-out", s.requireAdmin(s.checkOut))
	mux.HandleFunc("POST /presence", s.requireUser(s.presence))
	mux.HandleFunc("POST /presence/absent", s.requireUser(s.presenceAbsent))
	mux.HandleFunc("POST /presence/heartbeat", s.requireUser(s.heartbeat))
	mux.HandleFunc("POST /diag", s.requireAdmin(s.postDiag))
	mux.HandleFunc("GET /diag", s.requireAdmin(s.getDiag))
	mux.HandleFunc("POST /history", s.requireAdmin(s.postHistory))
	mux.HandleFunc("GET /history", s.requireAdmin(s.getHistory))
	mux.HandleFunc("GET /occupancy", s.requireAuth(s.occupancy))
	mux.HandleFunc("GET /notifications", s.requireAdmin(s.getNotifications))
	mux.HandleFunc("GET /collisions", s.requireAdmin(s.getCollisions))
	mux.HandleFunc("GET /overstays", s.requireAdmin(s.getOverstays))
	mux.HandleFunc("GET /utilization", s.requireAdmin(s.getUtilization))
	mux.HandleFunc("POST /auth/apple", s.postAppleAuth)
	mux.HandleFunc("POST /auth/login", s.postLogin)
	mux.HandleFunc("POST /auth/logout", s.requireUser(s.postLogout))
	mux.HandleFunc("GET /reservations/mine", s.requireUser(s.listMyReservations))
	mux.HandleFunc("POST /reservations", s.requireUser(s.createReservation))
	mux.HandleFunc("PATCH /reservations/{id}", s.requireUser(s.patchReservation))
	mux.HandleFunc("POST /reservations/{id}/cancel", s.requireUser(s.cancelReservation))
	mux.HandleFunc("POST /devices/apns", s.requireUser(s.postRegisterAPNSToken))
	mux.HandleFunc("GET /users", s.requireAdmin(s.listUsers))
	mux.HandleFunc("GET /users/{id}/reservations", s.requireAdmin(s.userReservations))
	mux.HandleFunc("DELETE /users/{id}", s.requireAdmin(s.deleteUser))
	mux.HandleFunc("POST /admin/reservations/{id}/cancel", s.requireAdmin(s.adminCancelReservation))
	mux.HandleFunc("POST /admin/reservations", s.requireAdmin(s.adminCreateReservation))
	mux.HandleFunc("PATCH /admin/reservations/{id}", s.requireAdmin(s.adminPatchReservation))
	mux.HandleFunc("DELETE /notifications/{id}", s.requireAdmin(s.deleteNotification))
	mux.HandleFunc("DELETE /notifications", s.requireAdmin(s.clearNotifications))
	mux.HandleFunc("PATCH /users/{id}", s.requireAdmin(s.patchUser))
	if s.oauth != nil {
		mux.HandleFunc("GET /oauth/login", s.oauthLogin)
		mux.HandleFunc("GET /oauth/callback", s.oauthCallback)
	}
	return recovery(s.log, logging(s.log, cors(gzipped(mux))))
}

// cors allows the API to be called cross-origin — notably Swagger UI's "Try it
// out" when the docs are opened from a different host than the one picked in the
// server dropdown. Safe here: the API is public and carries no cookies/credentials,
// so "*" exposes nothing a direct request wouldn't. Preflights are answered here
// since the method+path mux wouldn't match a bare OPTIONS.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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

func (s *Server) favicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(faviconSVG)
}

// scenarioImage serves a scenario's illustration (embedded JPEG). The {name} is a
// scenario id; restricting it to lowercase letters keeps it inside the embedded
// scenarios/ directory (no path traversal) and matches the ids used in scenarios.html.
func (s *Server) scenarioImage(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(r.PathValue("name"), ".jpg")
	if !isScenarioName(name) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	data, err := scenarioImages.ReadFile("scenarios/" + name + ".jpg")
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

func isScenarioName(name string) bool {
	if name == "" || len(name) > 32 {
		return false
	}
	for _, c := range name {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
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
// listEvents returns a room's recent presence activity (the floor modal's
// history). Requires a workspace_id query param.
func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	ws := r.URL.Query().Get("workspace_id")
	if ws == "" || len(ws) > maxIDLen {
		writeError(w, http.StatusBadRequest, "workspace_id required")
		return
	}
	limit := 30
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	events, err := s.db.Events(ws, limit, time.Now())
	if err != nil {
		s.log.Error("list events", "err", err)
		writeError(w, http.StatusInternalServerError, "could not read events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
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
		now := time.Now()
		if prev != "" {
			s.logEvent(prev, body.DeviceID, body.DisplayName, "leave", now)
		}
		if body.WorkspaceID != "" {
			s.logEvent(body.WorkspaceID, body.DeviceID, body.DisplayName, "enter", now)
		}
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

// logEvent records a presence event, logging (never failing) on a write error —
// history is best-effort and must not break a heartbeat.
func (s *Server) logEvent(workspaceID, actor, name, kind string, at time.Time) {
	if err := s.db.LogEvent(workspaceID, actor, name, kind, at); err != nil {
		s.log.Warn("log event", "err", err)
	}
}

// upsertReservation applies a reservation change to the in-memory store and,
// for app-sourced reservations only, also persists it to SQLite — app
// bookings are the only record of themselves (no Zoom to re-sync from), so
// losing them on restart would lose a real user's booking. Best-effort on the
// SQLite write: the in-memory state is applied regardless, matching how
// logEvent treats history as best-effort.
func (s *Server) upsertReservation(res domain.Reservation) {
	s.store.UpsertReservation(res)
	if res.Source != "app" {
		return
	}
	if err := s.db.SaveAppReservation(res); err != nil {
		s.log.Warn("persist app reservation", "reservation", res.ReservationID, "err", err)
	}
}

// sweepDeferredCheckouts flips checked-in bookings to checked-out once their
// booker has been absent for checkoutLinger. The exit event itself never
// checks out (a 2-minute step-out isn't leaving); returning within the
// linger resets the clock without a visible state change.
func (s *Server) sweepDeferredCheckouts(ctx context.Context, now time.Time) {
	occ := s.store.AllOccupancyIdents()
	live := map[string]bool{}
	for _, r := range s.store.Reservations() {
		if r.Status != domain.StatusBooked || r.CheckInStatus != domain.CheckedIn {
			continue
		}
		live[r.ReservationID] = true
		if bookerPresent(r, occ[r.ZoomWorkspaceID]) {
			delete(s.absentSince, r.ReservationID)
			continue
		}
		since, tracked := s.absentSince[r.ReservationID]
		if !tracked {
			s.absentSince[r.ReservationID] = now
			continue
		}
		if now.Sub(since) < checkoutLinger {
			continue
		}
		if r.Source != "app" {
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := s.zoom.SendEvent(c, zoom.EventCheckOut, r.ReservationID)
			cancel()
			if err != nil {
				s.log.Warn("deferred checkout failed", "reservation", r.ReservationID, "err", err)
				continue // retried next sweep
			}
		}
		r.CheckInStatus = domain.CheckedOut
		s.upsertReservation(r)
		delete(s.absentSince, r.ReservationID)
		s.log.Info("checked out after linger", "reservation", r.ReservationID, "workspace", r.ZoomWorkspaceID)
	}
	for id := range s.absentSince {
		if !live[id] {
			delete(s.absentSince, id) // booking resolved some other way
		}
	}
}

// driveReservation reflects a room's occupancy onto its booking's check-in
// state (best-effort; Zoom stays the source of truth for zoom-sourced
// bookings). App-sourced bookings (Source == "app") have no Zoom
// counterpart, so the Zoom call is skipped for them.
func (s *Server) driveReservation(ctx context.Context, workspaceID string, event zoom.EventType, newStatus domain.CheckInStatus) {
	res, ok := s.store.ReservationOwningWorkspace(workspaceID, time.Now())
	if !ok {
		return
	}
	if res.Source != "app" {
		if err := s.zoom.SendEvent(ctx, event, res.ReservationID); err != nil {
			s.log.Warn("driveReservation", "err", err)
			return
		}
	}
	res.CheckInStatus = newStatus
	s.upsertReservation(res)
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

	if body.EventType != "entered" && body.EventType != "exited" {
		writeError(w, http.StatusUnprocessableEntity, "event_type must be 'entered' or 'exited'")
		return
	}
	entered := body.EventType == "entered"

	// Headcount: apply only if this event is newer than the last for this
	// (workspace, user). Drops out-of-order/flap events so state can't corrupt.
	applied, movedFrom := s.store.ApplyPresenceIfNewer(body.WorkspaceID, body.UserID, body.DisplayName, body.EventTS, entered)
	if !applied {
		writeJSON(w, http.StatusOK, map[string]any{"status": "stale_ignored", "workspace_id": body.WorkspaceID})
		return
	}

	now := time.Now()
	kind := "leave"
	if entered {
		kind = "enter"
	}
	s.logEvent(body.WorkspaceID, body.UserID, body.DisplayName, kind, now)
	// An enter in a new room is an implicit leave of the old one — the phone
	// never sends that exit (the beacon region covers all rooms).
	for _, prev := range movedFrom {
		s.logEvent(prev, body.UserID, body.DisplayName, "leave", now)
	}

	// Exits (explicit or via a room move) never drive check-out here: a short
	// step-out must not flip the booking. sweepDeferredCheckouts checks out a
	// booking only after its booker has been absent for checkoutLinger.
	if !entered {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "recorded",
			"workspace_id": body.WorkspaceID,
		})
		return
	}

	// Presence (headcount) is tracked above regardless of bookings. Below we
	// best-effort drive the booker's reservation check-in.
	res, ok := s.store.ReservationOwningWorkspace(body.WorkspaceID, now)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "recorded",
			"workspace_id": body.WorkspaceID,
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if res.Source != "app" {
		if err := s.zoom.SendEvent(ctx, zoom.EventCheckIn, res.ReservationID); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	res.CheckInStatus = domain.CheckedIn
	s.upsertReservation(res)
	s.log.Info("presence applied", "event", body.EventType, "workspace", body.WorkspaceID, "user", body.UserID)
	writeJSON(w, http.StatusOK, res)
}

// presenceAbsent scrubs the calling user from every room — the app sends it
// when it foregrounds outside the beacon region, the definitive "I'm
// nowhere". Heals ghosts left by a lost exit event (network blip, backend
// restart mid-POST) without waiting for the presence TTL.
func (s *Server) presenceAbsent(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "user token required")
		return
	}
	cleared := s.store.ClearUserPresence(user.UserID)
	now := time.Now()
	for _, ws := range cleared {
		s.logEvent(ws, user.UserID, user.Name, "leave", now)
	}
	if len(cleared) > 0 {
		s.log.Info("presence scrubbed", "user", user.UserID, "rooms", cleared)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "cleared": cleared})
}

// checkEvent sends the event to Zoom, then reflects it locally (zoom-sourced
// reservations only — Zoom stays the source of truth for those). App-sourced
// reservations have no Zoom counterpart, so the Zoom call is skipped and the
// local state change applies directly.
func (s *Server) checkEvent(w http.ResponseWriter, r *http.Request, event zoom.EventType, newStatus domain.CheckInStatus) {
	id := r.PathValue("id")
	res, ok := s.store.Reservation(id)
	if !ok {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}

	if res.Source != "app" {
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
	}

	res.CheckInStatus = newStatus
	s.upsertReservation(res)
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

// statusWriter records the response status so the request log can carry it —
// without it, auth failures (401s signing users out of the app) are invisible.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "dur_ms", time.Since(start).Milliseconds())
	})
}

// gzipped compresses responses for clients that accept it. The pages are served
// over a public tunnel where the ~35 KB dashboard HTML is the slowest payload;
// gzip shrinks HTML/CSS/JS/JSON ~5-8x, so the page loads far faster on a slow link.
type gzipWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (w *gzipWriter) Write(b []byte) (int, error) { return w.gz.Write(b) }

func gzipped(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(&gzipWriter{ResponseWriter: w, gz: gz}, r)
	})
}
