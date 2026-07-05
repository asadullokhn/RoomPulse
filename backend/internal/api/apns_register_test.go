package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quickroom/internal/domain"
)

// mintSession creates a user and a live session directly in the test DB,
// returning the raw bearer token.
func mintSession(t *testing.T, s *Server, userID, email string) string {
	t.Helper()
	now := time.Now()
	if err := s.db.UpsertUser(domain.User{UserID: userID, AppleSub: "sub-" + userID, Email: email, CreatedAt: now}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	raw, hash := newSessionToken()
	if err := s.db.CreateSession(hash, userID, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return raw
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
