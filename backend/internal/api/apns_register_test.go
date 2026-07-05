package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"quickroom/internal/authtoken"
	"quickroom/internal/domain"
)

// mintSession creates a user and a live session directly in the test DB,
// returning a bearer JWT for them.
func mintSession(t *testing.T, s *Server, userID, email string) string {
	t.Helper()
	now := time.Now()
	if err := s.db.UpsertUser(domain.User{UserID: userID, AppleSub: "sub-" + userID, Email: email, CreatedAt: now}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	token, err := s.signer.Mint(userID, authtoken.RoleUser, time.Hour)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return token
}

// mintAdminToken seeds an admin (idempotent) and returns an admin JWT.
func mintAdminToken(t *testing.T, s *Server) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.EnsureAdmin("admin@test.local", string(hash), time.Now()); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	admin, ok, err := s.db.AdminByEmail("admin@test.local")
	if err != nil || !ok {
		// EnsureAdmin no-ops when another admin exists; find any admin id via login path instead.
		t.Fatalf("admin lookup: ok=%v err=%v", ok, err)
	}
	token, err := s.signer.Mint(admin.AdminID, authtoken.RoleAdmin, time.Hour)
	if err != nil {
		t.Fatalf("mint admin token: %v", err)
	}
	return token
}

func TestRegisterAPNSToken(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	token := mintSession(t, s, "u-apns", "apns@example.com")
	h := s.Handler()

	// No session -> 401
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/devices/apns", strings.NewReader(`{"token":"abc"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-auth status = %d", rec.Code)
	}

	// Empty token -> 422
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/devices/apns", strings.NewReader(`{"token":""}`))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty-token status = %d body=%s", rec.Code, rec.Body)
	}

	// Happy path -> 200, token persisted for the session user
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/devices/apns", strings.NewReader(`{"token":"devtok1"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	got, err := s.db.APNSTokensForUser("u-apns")
	if err != nil || len(got) != 1 || got[0] != "devtok1" {
		t.Fatalf("persisted tokens = %v err=%v", got, err)
	}
}
