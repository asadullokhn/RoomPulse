package api

import (
	"context"
	"time"

	"roompulse/internal/domain"
	"roompulse/internal/zoom"
)

// graceDuration returns the no-show grace window for a booking of length d:
// a fraction of the booking (Reno's proportional model), clamped to [min,max].
// A 2h booking at 10% = 12m; a 15m booking clamps up to min.
func graceDuration(bookingLen time.Duration, fraction float64, min, max time.Duration) time.Duration {
	g := time.Duration(float64(bookingLen) * fraction)
	if g < min {
		g = min
	}
	if g > max {
		g = max
	}
	return g
}

// sweepNoShows releases bookings whose start passed by the grace window with
// nobody ever present: booked -> no_show -> released (best-effort Zoom
// check-out, the only "give the room back" event the client exposes). A booking
// is left alone if someone checked in at any point (CheckInStatus moved off
// not_checked_in) or the room is occupied right now — presence is the truth.
// Returns the reservations newly released.
func (s *Server) sweepNoShows(ctx context.Context, now time.Time) []domain.Reservation {
	occ := s.store.AllOccupancy()
	var released []domain.Reservation
	for _, r := range s.store.Reservations() {
		if r.Status != domain.StatusBooked || r.CheckInStatus != domain.NotCheckedIn {
			continue // already resolved, or someone showed at some point
		}
		if len(occ[r.ZoomWorkspaceID]) > 0 {
			continue // occupied right now — not a no-show
		}
		if now.Before(r.StartTime.Add(graceDuration(r.EndTime.Sub(r.StartTime), s.graceFraction, s.graceMin, s.graceMax))) {
			continue // still within the grace window
		}
		r.Status = domain.StatusNoShow
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.zoom.SendEvent(c, zoom.EventCheckOut, r.ReservationID)
		cancel()
		if err != nil {
			// Leave it flagged no_show; a later sweep retries the release.
			s.store.UpsertReservation(r)
			s.log.Warn("no-show release failed", "reservation", r.ReservationID, "err", err)
			continue
		}
		r.Status = domain.StatusReleased
		r.CheckInStatus = domain.CheckedOut
		s.store.UpsertReservation(r)
		s.log.Info("released no-show booking", "reservation", r.ReservationID, "workspace", r.ZoomWorkspaceID, "user", r.UserID)
		released = append(released, r)
	}
	return released
}
