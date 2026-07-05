package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"quickroom/internal/authtoken"
	"quickroom/internal/domain"
)

type ctxKey int

const userCtxKey ctxKey = iota

// userFromContext returns the authenticated user attached by requireUser.
func userFromContext(r *http.Request) (domain.User, bool) {
	u, ok := r.Context().Value(userCtxKey).(domain.User)
	return u, ok
}

// requireUser verifies a bearer JWT with role "user" and confirms the account
// still exists (so a deleted user loses access immediately), attaching it to
// the request context.
func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sub, role, ok := s.verifyBearer(w, r)
		if !ok {
			return
		}
		if role != authtoken.RoleUser {
			writeError(w, http.StatusForbidden, "user token required")
			return
		}
		user, found, err := s.db.UserByID(sub)
		if err != nil {
			s.log.Error("user lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !found {
			writeError(w, http.StatusUnauthorized, "account no longer exists")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, user)))
	}
}

// requireAdmin verifies a bearer JWT with role "admin" and confirms the admin
// account still exists.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sub, role, ok := s.verifyBearer(w, r)
		if !ok {
			return
		}
		if role != authtoken.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin token required")
			return
		}
		if _, found, err := s.db.AdminByID(sub); err != nil {
			s.log.Error("admin lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		} else if !found {
			writeError(w, http.StatusUnauthorized, "admin no longer exists")
			return
		}
		next.ServeHTTP(w, r)
	}
}

// verifyBearer extracts and verifies the JWT, writing the 401 itself on
// failure. Returns ok=false when a response was already written.
func (s *Server) verifyBearer(w http.ResponseWriter, r *http.Request) (sub, role string, ok bool) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return "", "", false
	}
	sub, role, err := s.signer.Verify(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return "", "", false
	}
	return sub, role, true
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimPrefix(h, prefix)
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
// record, and issues a user JWT. The response field keeps its historical
// "session_token" name so the mobile app needs no changes.
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

	token, err := s.signer.Mint(user.UserID, authtoken.RoleUser, s.userTokenTTL)
	if err != nil {
		s.log.Error("mint user token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"session_token": token, "user": user})
}

// postLogout is a client-side operation under JWTs (drop the token); the
// endpoint stays for mobile compatibility and answers ok.
func (s *Server) postLogout(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
