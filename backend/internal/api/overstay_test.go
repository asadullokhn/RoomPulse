package api

import (
	"testing"
	"time"

	"quickroom/internal/domain"
)

// seedEndedBooking upserts a booking that ended `endedAgo` ago in `ws`, occupied
// by `occupant`, and returns the server. overstayGrace is set to 0 so "ended"
// means "flag now".
func overstayServer(t *testing.T, now time.Time) *Server {
	t.Helper()
	srv := newNoShowServer(t, now)
	srv.overstayGrace = 0
	return srv
}

func TestCurrentOverstaysFlagsOccupiedPastEnd(t *testing.T) {
	now := time.Now()
	srv := overstayServer(t, now)

	// A booking that ended 3m ago, room still occupied.
	srv.store.UpsertReservation(domain.Reservation{
		ReservationID: "res-over", ZoomWorkspaceID: "ws-bedugul", UserEmail: "team@adabali.dev",
		StartTime: now.Add(-63 * time.Minute), EndTime: now.Add(-3 * time.Minute),
		Status: domain.StatusBooked, CheckInStatus: domain.CheckedIn,
	})
	srv.store.ApplyPresenceIfNewer("ws-bedugul", "u-stay", "Late Team", now.UnixMilli(), true)

	os := srv.currentOverstays(now)
	if len(os) != 1 || os[0].ReservationID != "res-over" {
		t.Fatalf("overstays = %+v, want [res-over]", os)
	}
	if os[0].OverBySec < 170 || os[0].OverBySec > 190 {
		t.Errorf("over_by_sec = %d, want ~180", os[0].OverBySec)
	}

	// Room empties out -> no overstay.
	srv.store.ApplyPresenceIfNewer("ws-bedugul", "u-stay", "Late Team", now.UnixMilli()+1, false)
	if os := srv.currentOverstays(now); len(os) != 0 {
		t.Errorf("overstays after room emptied = %d, want 0", len(os))
	}
}

// Stale bookings whose checkout was lost must not resurface as "wrap up"
// nudges when the room is occupied again long after — that burst hit every
// booker of the previous two days at once.
func TestOverstaySkipsLongOverAndResolvedBookings(t *testing.T) {
	now := time.Now()
	srv := overstayServer(t, now)
	base := domain.Reservation{
		ZoomWorkspaceID: "ws-bedugul", UserEmail: "team@adabali.dev",
		Status: domain.StatusBooked, CheckInStatus: domain.CheckedIn,
	}

	ancient := base
	ancient.ReservationID = "res-ancient"
	ancient.StartTime, ancient.EndTime = now.Add(-37*time.Hour), now.Add(-36*time.Hour)
	srv.store.UpsertReservation(ancient)

	left := base
	left.ReservationID = "res-left"
	left.CheckInStatus = domain.CheckedOut // properly checked out
	left.StartTime, left.EndTime = now.Add(-63*time.Minute), now.Add(-3*time.Minute)
	srv.store.UpsertReservation(left)

	ghosted := base
	ghosted.ReservationID = "res-ghosted"
	ghosted.CheckInStatus = domain.NotCheckedIn // never came
	ghosted.StartTime, ghosted.EndTime = now.Add(-63*time.Minute), now.Add(-3*time.Minute)
	srv.store.UpsertReservation(ghosted)

	srv.store.ApplyPresenceIfNewer("ws-bedugul", "u-x", "Someone", now.UnixMilli(), true)

	for _, o := range srv.currentOverstays(now) {
		switch o.ReservationID {
		case "res-ancient", "res-left", "res-ghosted":
			t.Errorf("%s flagged as overstay", o.ReservationID)
		}
	}
}

func TestOverstaySkipsBackToBackBooking(t *testing.T) {
	now := time.Now()
	srv := overstayServer(t, now)
	// Ended booking...
	srv.store.UpsertReservation(domain.Reservation{
		ReservationID: "res-prev", ZoomWorkspaceID: "ws-bedugul", UserEmail: "prev@adabali.dev",
		StartTime: now.Add(-60 * time.Minute), EndTime: now.Add(-5 * time.Minute),
		Status: domain.StatusBooked, CheckInStatus: domain.CheckedIn,
	})
	// ...immediately followed by a booking that now owns the room.
	srv.store.UpsertReservation(domain.Reservation{
		ReservationID: "res-next", ZoomWorkspaceID: "ws-bedugul", UserEmail: "next@adabali.dev",
		StartTime: now.Add(-5 * time.Minute), EndTime: now.Add(55 * time.Minute),
		Status: domain.StatusBooked, CheckInStatus: domain.CheckedIn,
	})
	srv.store.ApplyPresenceIfNewer("ws-bedugul", "u-x", "Someone", now.UnixMilli(), true)

	for _, o := range srv.currentOverstays(now) {
		if o.ReservationID == "res-prev" {
			t.Errorf("res-prev flagged as overstay despite an active following booking")
		}
	}
}

func TestSweepOverstaysEmitsNotifications(t *testing.T) {
	now := time.Now()
	srv := overstayServer(t, now)
	srv.store.UpsertReservation(domain.Reservation{
		ReservationID: "res-over", ZoomWorkspaceID: "ws-bedugul", UserEmail: "team@adabali.dev",
		StartTime: now.Add(-63 * time.Minute), EndTime: now.Add(-3 * time.Minute),
		Status: domain.StatusBooked, CheckInStatus: domain.CheckedIn,
	})
	srv.store.ApplyPresenceIfNewer("ws-bedugul", "u-stay", "Late Team", now.UnixMilli(), true)

	srv.sweepOverstays(now)
	all := srv.notify.recent("", 100)
	if got := countByType(all, "overstay"); got != 1 {
		t.Fatalf("overstay notifications = %d, want 1 (booker only)", got)
	}
	if got := len(srv.notify.recent("team@adabali.dev", 100)); got != 1 {
		t.Errorf("booker overstay notifications = %d, want 1", got)
	}
	// idempotent
	srv.sweepOverstays(now)
	if got := countByType(srv.notify.recent("", 100), "overstay"); got != 1 {
		t.Errorf("overstay notifications after second sweep = %d, want 1", got)
	}
}

// Default seed: nothing has ended at t=now, so no overstays even with the grace
// at 0.
func TestCurrentOverstaysBaselineEmpty(t *testing.T) {
	now := time.Now()
	srv := overstayServer(t, now)
	if os := srv.currentOverstays(now); len(os) != 0 {
		t.Errorf("baseline overstays = %d, want 0", len(os))
	}
}
