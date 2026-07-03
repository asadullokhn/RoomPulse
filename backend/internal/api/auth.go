package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"quickroom/internal/domain"
)

type ctxKey int

const userCtxKey ctxKey = iota

// userFromContext returns the authenticated user attached by authMiddleware.
func userFromContext(r *http.Request) (domain.User, bool) {
	u, ok := r.Context().Value(userCtxKey).(domain.User)
	return u, ok
}

// authMiddleware resolves a bearer session token to its user and attaches it
// to the request context. 401s on a missing, invalid, or expired session.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		user, ok, err := s.sessionUser(token)
		if err != nil {
			s.log.Error("session lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, user)))
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimPrefix(h, prefix)
}

func (s *Server) sessionUser(token string) (domain.User, bool, error) {
	userID, ok, err := s.db.SessionUserID(hashToken(token), time.Now())
	if err != nil || !ok {
		return domain.User{}, false, err
	}
	return s.db.UserByID(userID)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newSessionToken returns a fresh opaque token (returned to the caller once)
// and its SHA-256 hash (what's actually persisted).
func newSessionToken() (raw, hash string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is effectively unrecoverable in practice;
		// falling back to a fixed value here would be a real security bug
		// (predictable session tokens), so surface it as an empty token —
		// callers must treat an empty raw token as a hard failure.
		return "", ""
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw)
}

// newUserID generates a local user identifier, distinct from Apple's sub.
func newUserID() string { return randomPrefixedID("usr_") }

// newReservationID generates an id for an app-sourced booking, distinct from
// Zoom's own reservation id scheme.
func newReservationID() string { return randomPrefixedID("app-") }

func randomPrefixedID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return prefix + hex.EncodeToString([]byte("quickroom-fallback"))
	}
	return prefix + hex.EncodeToString(b)
}

// postAppleAuth verifies an Apple identity token, upserts the local user
// record, and issues a new session.
func (s *Server) postAppleAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IdentityToken string `json:"identity_token"`
		Name          string `json:"name"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.IdentityToken == "" {
		writeError(w, http.StatusUnprocessableEntity, "identity_token required")
		return
	}

	claims, err := s.appleVerifier.VerifyIdentityToken(r.Context(), body.IdentityToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid apple identity token")
		return
	}

	user, existed, err := s.db.UserByAppleSub(claims.Sub)
	if err != nil {
		s.log.Error("lookup user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !existed {
		user = domain.User{
			UserID:    newUserID(),
			AppleSub:  claims.Sub,
			Email:     claims.Email,
			Name:      clamp(body.Name, maxNameLen),
			CreatedAt: time.Now(),
		}
	} else {
		user.Email = claims.Email
	}
	if err := s.db.UpsertUser(user); err != nil {
		s.log.Error("upsert user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	raw, hash := newSessionToken()
	if raw == "" {
		s.log.Error("generate session token: crypto/rand failed")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := time.Now()
	if err := s.db.CreateSession(hash, user.UserID, now, now.Add(s.sessionTTL)); err != nil {
		s.log.Error("create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"session_token": raw, "user": user})
}

// postLogout deletes the caller's session. Idempotent: no-op if already gone.
func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token != "" {
		if err := s.db.DeleteSession(hashToken(token)); err != nil {
			s.log.Warn("delete session", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
