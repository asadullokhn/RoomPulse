package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAdminSurfaceRequiresAdminJWT sweeps every admin-protected route:
// no token -> 401, a user-role token -> 403.
func TestAdminSurfaceRequiresAdminJWT(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	userToken := mintSession(t, s, "u-matrix", "matrix@x.y")
	h := s.Handler()

	adminRoutes := []struct{ method, path string }{
		{"POST", "/decision"},
		{"GET", "/decision"},
		{"POST", "/scenario-answers"},
		{"GET", "/scenario-answers"},
		{"POST", "/sync"},
		{"PUT", "/beacons/ws-agung"},
		{"DELETE", "/beacons/ws-agung"},
		{"GET", "/devices"},
		{"GET", "/events"},
		{"POST", "/reservations/res-x/check-in"},
		{"POST", "/reservations/res-x/check-out"},
		{"POST", "/diag"},
		{"GET", "/diag"},
		{"POST", "/history"},
		{"GET", "/history"},
		{"GET", "/notifications"},
		{"GET", "/collisions"},
		{"GET", "/overstays"},
		{"GET", "/utilization"},
		{"GET", "/users"},
		{"GET", "/users/u-1/reservations"},
		{"DELETE", "/users/u-1"},
		{"POST", "/admin/reservations/res-x/cancel"},
	}
	for _, route := range adminRoutes {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, strings.NewReader("{}")))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token = %d, want 401", route.method, route.path, rec.Code)
		}

		rec = httptest.NewRecorder()
		req := httptest.NewRequest(route.method, route.path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+userToken)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with user token = %d, want 403", route.method, route.path, rec.Code)
		}
	}
}

// TestSharedSurfaceRequiresAnyJWT: reads shared by the mobile app and the
// admin panel demand a token of either role — 401 bare, 200-ish with both.
func TestSharedSurfaceRequiresAnyJWT(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	userToken := mintSession(t, s, "u-matrix", "matrix@x.y")
	adminToken := mintAdminToken(t, s)
	h := s.Handler()

	sharedRoutes := []struct{ method, path string }{
		{"GET", "/rooms"},
		{"GET", "/reservations"},
		{"GET", "/beacons"},
		{"GET", "/occupancy"},
		{"GET", "/floor/rooms"},
		{"GET", "/floor/image"},
	}
	for _, route := range sharedRoutes {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token = %d, want 401", route.method, route.path, rec.Code)
		}
		for name, tok := range map[string]string{"user": userToken, "admin": adminToken} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(route.method, route.path, nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Errorf("%s %s with %s token = %d — must be allowed", route.method, route.path, name, rec.Code)
			}
		}
	}
}

// The app's "I'm outside the whole region" foreground signal scrubs the
// user's presence everywhere — heals ghosts left by a lost exit POST.
func TestPresenceAbsentScrubsUserEverywhere(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	tok := mintSession(t, s, "u-abs", "abs@x.y")
	s.store.ApplyPresenceIfNewer("ws-agung", "u-abs", "Abs", 100, true)
	s.store.ApplyPresenceIfNewer("ws-ubud", "u-other", "Other", 100, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/presence/absent", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("absent status = %d body=%s", rec.Code, rec.Body)
	}
	occ := s.store.AllOccupancy()
	if len(occ["ws-agung"]) != 0 {
		t.Fatalf("ws-agung still occupied: %v", occ["ws-agung"])
	}
	if len(occ["ws-ubud"]) != 1 {
		t.Fatalf("other user must be untouched: %v", occ["ws-ubud"])
	}
}

// TestPresenceRequiresUserJWT: only phones (user tokens) report presence.
func TestPresenceRequiresUserJWT(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	userToken := mintSession(t, s, "u-matrix", "matrix@x.y")
	adminToken := mintAdminToken(t, s)
	h := s.Handler()

	routes := []struct{ path, body string }{
		{"/presence", `{"workspace_id":"ws-agung","user_id":"u-matrix","event_type":"entered"}`},
		{"/presence/absent", ``},
		{"/presence/heartbeat", `{"device_id":"dev-1"}`},
	}
	for _, route := range routes {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", route.path, strings.NewReader(route.body)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without token = %d, want 401", route.path, rec.Code)
		}

		rec = httptest.NewRecorder()
		req := httptest.NewRequest("POST", route.path, strings.NewReader(route.body))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s with admin token = %d, want 403", route.path, rec.Code)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest("POST", route.path, strings.NewReader(route.body))
		req.Header.Set("Authorization", "Bearer "+userToken)
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Errorf("POST %s with user token = %d — must be allowed", route.path, rec.Code)
		}
	}
}

// TestTrulyOpenSurface: only health, info, docs, and the sign-in entrances
// stay tokenless.
func TestTrulyOpenSurface(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	h := s.Handler()

	for _, path := range []string{"/health/live", "/health/ready", "/info", "/openapi.yaml"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Errorf("GET %s = %d — must stay open", path, rec.Code)
		}
	}
}
