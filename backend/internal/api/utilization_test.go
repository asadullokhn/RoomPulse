package api

import (
	"context"
	"testing"
	"time"

	"quickroom/internal/domain"
)

func TestUtilizationReport(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)

	// Baseline: 4 seed bookings, all booked, none checked in, none occupied.
	u := srv.utilization(now)
	if u.Bookings != 4 {
		t.Fatalf("bookings = %d, want 4", u.Bookings)
	}
	if u.Booked != 4 || u.CheckedIn != 0 || u.NoShowReleased != 0 {
		t.Errorf("baseline counts = booked %d / checked_in %d / released %d, want 4/0/0",
			u.Booked, u.CheckedIn, u.NoShowReleased)
	}
	if u.NoShowRate != 0 {
		t.Errorf("baseline no_show_rate = %v, want 0", u.NoShowRate)
	}
	if u.RoomsTotal != 10 {
		t.Errorf("rooms_total = %d, want 10 (seed rooms)", u.RoomsTotal)
	}

	// Release the one true no-show (res-agung) and put someone in a room.
	srv.sweepNoShows(context.Background(), now)
	srv.store.ApplyPresenceIfNewer("ws-petang", "u1", "One", now.UnixMilli(), true)
	srv.store.ApplyPresenceIfNewer("ws-petang", "u2", "Two", now.UnixMilli(), true)

	u = srv.utilization(now)
	if u.NoShowReleased != 1 {
		t.Errorf("no_show_released = %d, want 1", u.NoShowReleased)
	}
	if u.Booked != 3 {
		t.Errorf("booked = %d, want 3 (one released)", u.Booked)
	}
	if want := 1.0 / 4.0; u.NoShowRate != want {
		t.Errorf("no_show_rate = %v, want %v", u.NoShowRate, want)
	}
	if u.RoomsOccupied != 1 || u.PeoplePresent != 2 {
		t.Errorf("occupancy = %d rooms / %d people, want 1/2", u.RoomsOccupied, u.PeoplePresent)
	}
}

func TestUtilizationCountsCheckedIn(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)
	r, _ := srv.store.Reservation("res-ubud")
	r.CheckInStatus = domain.CheckedIn
	srv.store.UpsertReservation(r)

	if u := srv.utilization(now); u.CheckedIn != 1 {
		t.Errorf("checked_in = %d, want 1", u.CheckedIn)
	}
}
