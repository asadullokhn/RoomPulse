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

// TestOpenSurfaceStaysOpen: the mobile app's pre-sign-in reads and device
// plumbing must never demand a token.
func TestOpenSurfaceStaysOpen(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	h := s.Handler()

	openRoutes := []struct {
		method, path, body string
	}{
		{"GET", "/rooms", ""},
		{"GET", "/reservations", ""},
		{"GET", "/beacons", ""},
		{"GET", "/occupancy", ""},
		{"GET", "/health/live", ""},
		{"GET", "/info", ""},
		{"GET", "/floor/rooms", ""},
		{"POST", "/presence", `{"workspace_id":"ws-agung","user_id":"dev-1","event_type":"entered"}`},
		{"POST", "/presence/heartbeat", `{"device_id":"dev-1"}`},
	}
	for _, route := range openRoutes {
		rec := httptest.NewRecorder()
		var req *http.Request
		if route.body != "" {
			req = httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		} else {
			req = httptest.NewRequest(route.method, route.path, nil)
		}
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Errorf("%s %s = %d — open route must not demand auth", route.method, route.path, rec.Code)
		}
	}
}
