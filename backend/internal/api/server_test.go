package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"quickroom/internal/api"
	"quickroom/internal/store"
	syncsvc "quickroom/internal/sync"
	"quickroom/internal/zoom"
)

// newTestHandler wires a full server over the in-memory store, a temp SQLite DB,
// and the seeded mock Zoom client, then runs one sync so rooms/reservations exist.
func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()
	st := store.NewMemory()
	db, err := store.OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	zc := zoom.NewMockClient(now, nil, log) // default seed: ws-petang has an active reservation
	sy := syncsvc.New(zc, st, "", log)
	if _, err := sy.Run(context.Background(), now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	return api.NewServer(st, db, sy, zc, "mock", 30*time.Minute, log).Handler()
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func occupancyCount(t *testing.T, h http.Handler, ws string) int {
	t.Helper()
	rec := do(t, h, "GET", "/occupancy", nil)
	var out struct {
		Occupancy []struct {
			WorkspaceID string `json:"workspace_id"`
			Count       int    `json:"count"`
		} `json:"occupancy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode occupancy: %v", err)
	}
	for _, e := range out.Occupancy {
		if e.WorkspaceID == ws {
			return e.Count
		}
	}
	return 0
}

func checkInStatus(t *testing.T, h http.Handler, resID string) string {
	t.Helper()
	rec := do(t, h, "GET", "/reservations", nil)
	var out struct {
		Reservations []struct {
			ReservationID string `json:"reservation_id"`
			CheckInStatus string `json:"check_in_status"`
		} `json:"reservations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode reservations: %v", err)
	}
	for _, r := range out.Reservations {
		if r.ReservationID == resID {
			return r.CheckInStatus
		}
	}
	return ""
}

// TestHeartbeatDrivesOccupancyAndCheckIn covers the core loop: a phone entering a
// booked room bumps occupancy AND drives the reservation's Zoom check-in; leaving
// clears occupancy AND drives check-out.
func TestHeartbeatDrivesOccupancyAndCheckIn(t *testing.T) {
	h := newTestHandler(t)

	if rec := do(t, h, "POST", "/presence/heartbeat", map[string]any{
		"device_id": "dev-1", "display_name": "Ali", "workspace_id": "ws-petang", "ts": 1000,
	}); rec.Code != http.StatusOK {
		t.Fatalf("enter heartbeat: %d %s", rec.Code, rec.Body.String())
	}
	if got := occupancyCount(t, h, "ws-petang"); got != 1 {
		t.Fatalf("occupancy after enter = %d, want 1", got)
	}
	if s := checkInStatus(t, h, "res-petang"); s != "checked_in" {
		t.Fatalf("check_in_status after enter = %q, want checked_in", s)
	}

	if rec := do(t, h, "POST", "/presence/heartbeat", map[string]any{
		"device_id": "dev-1", "workspace_id": "", "ts": 2000,
	}); rec.Code != http.StatusOK {
		t.Fatalf("leave heartbeat: %d", rec.Code)
	}
	if got := occupancyCount(t, h, "ws-petang"); got != 0 {
		t.Fatalf("occupancy after leave = %d, want 0", got)
	}
	if s := checkInStatus(t, h, "res-petang"); s != "checked_out" {
		t.Fatalf("check_in_status after leave = %q, want checked_out", s)
	}
}

// TestStaleHeartbeatIgnored confirms an out-of-order (older ts) heartbeat can't
// resurrect a left room.
func TestStaleHeartbeatIgnored(t *testing.T) {
	h := newTestHandler(t)
	do(t, h, "POST", "/presence/heartbeat", map[string]any{"device_id": "dev-1", "workspace_id": "ws-petang", "ts": 5000})
	do(t, h, "POST", "/presence/heartbeat", map[string]any{"device_id": "dev-1", "workspace_id": "", "ts": 6000})
	// a delayed old "enter" arrives out of order — must be ignored
	do(t, h, "POST", "/presence/heartbeat", map[string]any{"device_id": "dev-1", "workspace_id": "ws-petang", "ts": 4000})
	if got := occupancyCount(t, h, "ws-petang"); got != 0 {
		t.Fatalf("occupancy after stale enter = %d, want 0", got)
	}
}

// TestBoundaryValidation locks the request-validation contract at the edges.
func TestBoundaryValidation(t *testing.T) {
	h := newTestHandler(t)
	cases := []struct {
		name, method, path string
		body               any
		want               int
	}{
		{"heartbeat missing device_id", "POST", "/presence/heartbeat", map[string]any{"workspace_id": "ws-petang"}, http.StatusUnprocessableEntity},
		{"presence bad event_type", "POST", "/presence", map[string]any{"user_id": "u1", "workspace_id": "ws-petang", "event_type": "nope"}, http.StatusUnprocessableEntity},
		{"presence missing ids", "POST", "/presence", map[string]any{"event_type": "entered"}, http.StatusUnprocessableEntity},
		{"events missing workspace_id", "GET", "/events", nil, http.StatusBadRequest},
		{"check-in unknown reservation", "POST", "/reservations/does-not-exist/check-in", nil, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := do(t, h, c.method, c.path, c.body); rec.Code != c.want {
				t.Fatalf("%s = %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// TestDocsServed confirms the Swagger spec + UI are reachable.
func TestDocsServed(t *testing.T) {
	h := newTestHandler(t)
	if rec := do(t, h, "GET", "/openapi.yaml", nil); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("openapi: 3.1")) {
		t.Fatalf("/openapi.yaml = %d, body starts %q", rec.Code, rec.Body.String()[:min(40, rec.Body.Len())])
	}
	if rec := do(t, h, "GET", "/docs", nil); rec.Code != http.StatusOK {
		t.Fatalf("/docs = %d", rec.Code)
	}
}
