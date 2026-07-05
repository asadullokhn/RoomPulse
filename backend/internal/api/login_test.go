package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quickroom/internal/authtoken"
)

func postJSON(h http.Handler, path, body, bearer string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminLogin(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	_ = mintAdminToken(t, s) // seeds admin@test.local / "pw"
	h := s.Handler()

	// Good creds -> 200 with a verifiable admin JWT.
	rec := postJSON(h, "/auth/login", `{"email":"admin@test.local","password":"pw"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	start := strings.Index(body, `"token":"`) + len(`"token":"`)
	end := strings.Index(body[start:], `"`)
	token := body[start : start+end]
	sub, role, err := s.signer.Verify(token)
	if err != nil || role != authtoken.RoleAdmin || !strings.HasPrefix(sub, "adm_") {
		t.Fatalf("token verify = %q %q %v", sub, role, err)
	}

	// Wrong password / unknown email -> uniform 401.
	if rec := postJSON(h, "/auth/login", `{"email":"admin@test.local","password":"nope"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d", rec.Code)
	}
	if rec := postJSON(h, "/auth/login", `{"email":"ghost@test.local","password":"pw"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown email status = %d", rec.Code)
	}
	// Missing fields -> 422.
	if rec := postJSON(h, "/auth/login", `{"email":"admin@test.local"}`, ""); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing password status = %d", rec.Code)
	}
}

func TestRoleEnforcement(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	userToken := mintSession(t, s, "u-role", "role@x.y")
	adminToken := mintAdminToken(t, s)
	h := s.Handler()

	// User token on an admin route -> 403 (route wrapped in Task 4; /users
	// is wrapped there — here we test the middleware primitive directly).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/probe-admin", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	s.requireAdmin(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user-on-admin status = %d", rec.Code)
	}

	// Admin token on a user route -> 403.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/probe-user", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	s.requireUser(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin-on-user status = %d", rec.Code)
	}

	// Deleted user's still-valid JWT -> 401 on a user route.
	if err := s.db.DeleteUser("u-role"); err != nil {
		t.Fatal(err)
	}
	rec = postJSON(h, "/reservations", `{"workspace_id":"ws-agung","start_time":"2027-01-01T10:00:00Z","end_time":"2027-01-01T11:00:00Z"}`, userToken)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("deleted-user status = %d body=%s", rec.Code, rec.Body)
	}
}
