package api

import (
	"net/http"

	"quickroom/internal/domain"
)

// listUsers returns every app account.
func (s *Server) listUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := s.db.Users()
	if err != nil {
		s.log.Error("list users", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// userReservations returns one user's bookings (any status/source).
func (s *Server) userReservations(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, ok, err := s.db.UserByID(userID); err != nil {
		s.log.Error("lookup user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	out := []domain.Reservation{}
	for _, res := range s.store.Reservations() {
		if res.BookedByUserID == userID {
			out = append(out, res)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reservations": out})
}

// deleteUser removes an account: cancels their open app-sourced bookings
// (a removed account can't be left holding a room), then deletes the user
// row. JWTs need no revocation here — requireUser rejects tokens whose user
// no longer exists. Cancellation is best-effort; the row deletion must
// succeed for a 200.
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, ok, err := s.db.UserByID(userID); err != nil {
		s.log.Error("lookup user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	for _, res := range s.store.Reservations() {
		if res.Source != "app" || res.BookedByUserID != userID {
			continue
		}
		if res.Status != domain.StatusBooked {
			continue
		}
		res.Status = domain.StatusCancelled
		s.upsertReservation(res)
	}

	if err := s.db.DeleteUser(userID); err != nil {
		s.log.Error("delete user", "user", userID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
