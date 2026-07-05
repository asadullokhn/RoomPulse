package api

import (
	"net/http"
	"time"
)

// postRegisterAPNSToken stores the caller's APNs device token so outbox
// notifications can be pushed to their phone. Session-scoped: the token is
// attached to whoever is signed in on the device.
func (s *Server) postRegisterAPNSToken(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "no session")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Token == "" {
		writeError(w, http.StatusUnprocessableEntity, "token required")
		return
	}
	if err := s.db.SaveAPNSToken(body.Token, user.UserID, time.Now()); err != nil {
		s.log.Error("save apns token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
