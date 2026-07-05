package api

import (
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"quickroom/internal/authtoken"
)

// adminTokenTTL bounds an admin login; short because the panel holds broad
// mutation rights and JWTs are not revocable.
const adminTokenTTL = 12 * time.Hour

// postLogin authenticates an admin by email+password and issues an admin JWT.
// The failure message is uniform so it never confirms which part was wrong.
func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Email == "" || body.Password == "" {
		writeError(w, http.StatusUnprocessableEntity, "email and password required")
		return
	}

	admin, ok, err := s.db.AdminByEmail(body.Email)
	if err != nil {
		s.log.Error("admin lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok || bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(body.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := s.signer.Mint(admin.AdminID, authtoken.RoleAdmin, adminTokenTTL)
	if err != nil {
		s.log.Error("mint admin token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "email": admin.Email})
}
