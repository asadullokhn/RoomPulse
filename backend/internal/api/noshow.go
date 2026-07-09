package api

import (
	"context"
	"fmt"
	"time"

	"quickroom/internal/domain"
	"quickroom/internal/zoom"
)

// sweepNoShows releases bookings whose start passed by the grace window with
// nobody ever present: booked -> no_show -> released (best-effort Zoom
// check-out, the only "give the room back" event the client exposes). A booking
// is left alone if someone checked in at any point (CheckInStatus moved off
// not_checked_in) or the room is occupied right now — presence is the truth.
// Returns the reservations newly released.
func (s *Server) sweepNoShows(ctx context.Context, now time.Time) []domain.Reservation {
	occ := s.store.AllOccupancy()
	ratings := s.ratingsOrEmpty()
	var released []domain.Reservation
	for _, r := range s.store.Reservations() {
		if r.Status != domain.StatusBooked || r.CheckInStatus != domain.NotCheckedIn {
			continue // already resolved, or someone showed at some point
		}
		if !now.Before(r.EndTime) {
			continue // already over — releasing frees nothing (a restart mid-window
			// must not push "you didn't check in" for a booking that has ended)
		}
		if len(occ[r.ZoomWorkspaceID]) > 0 {
			continue // occupied right now — not a no-show
		}
		if now.Before(r.StartTime.Add(s.effectiveGrace(r.BookedByUserID, ratings))) {
			continue // still within the grace window
		}
		r.Status = domain.StatusNoShow
		if r.Source != "app" {
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := s.zoom.SendEvent(c, zoom.EventCheckOut, r.ReservationID)
			cancel()
			if err != nil {
				// Leave it flagged no_show; a later sweep retries the release.
				s.store.UpsertReservation(r)
				s.log.Warn("no-show release failed", "reservation", r.ReservationID, "err", err)
				continue
			}
		}
		// CheckInStatus stays not_checked_in — it's the truth (nobody came),
		// and the rating tally counts releases as no-shows only when the
		// booking was never checked in.
		r.Status = domain.StatusReleased
		s.upsertReservation(r)
		s.log.Info("released no-show booking", "reservation", r.ReservationID, "workspace", r.ZoomWorkspaceID, "user", r.UserID)

		room := s.roomName(r.ZoomWorkspaceID)
		s.notify.emit(r.ReservationID+"|released", Notification{
			Type: "no_show_released", WorkspaceID: r.ZoomWorkspaceID, ReservationID: r.ReservationID,
			Recipient: bookerOf(r), Title: "Booking released",
			Body:      fmt.Sprintf("You didn't check in, so %s was released back to the pool.", room),
			CreatedAt: now,
		})
		released = append(released, r)
	}
	return released
}

// academyTZ renders user-facing clock times — the backend runs in UTC, the
// Academy is in Bali (WITA, UTC+8, no DST).
var academyTZ = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Makassar"); err == nil {
		return loc
	}
	return time.FixedZone("WITA", 8*60*60)
}()

// sweepGraceReminders emits the single "are you coming?" ping for bookings
// that have started, are still unchecked-in and empty, and are inside the
// grace window. It fires notifyFirstAfter into the booking, names the exact
// release time, and fires once (deduped per reservation).
func (s *Server) sweepGraceReminders(now time.Time) {
	occ := s.store.AllOccupancy()
	ratings := s.ratingsOrEmpty()
	for _, r := range s.store.Reservations() {
		if r.Status != domain.StatusBooked || r.CheckInStatus != domain.NotCheckedIn {
			continue
		}
		if len(occ[r.ZoomWorkspaceID]) > 0 {
			continue
		}
		if r.EndTime.Sub(r.StartTime) <= 0 {
			continue
		}
		graceDeadline := r.StartTime.Add(s.effectiveGrace(r.BookedByUserID, ratings))
		if !now.Before(graceDeadline) {
			continue // past grace — the release path handles it
		}
		if now.Before(r.StartTime.Add(s.notifyFirstAfter)) {
			continue // too early to nag
		}
		s.notify.emit(r.ReservationID+"|1", Notification{
			Type: "grace_reminder", Level: 1, WorkspaceID: r.ZoomWorkspaceID, ReservationID: r.ReservationID,
			Recipient: bookerOf(r), Title: "Are you coming?",
			Body: fmt.Sprintf("%s hasn't checked in — it will be released at %s.",
				s.roomName(r.ZoomWorkspaceID), graceDeadline.In(academyTZ).Format("15.04")),
			CreatedAt: now,
		})
	}
}
