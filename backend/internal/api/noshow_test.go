package api

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"roompulse/internal/domain"
	"roompulse/internal/store"
	syncsvc "roompulse/internal/sync"
	"roompulse/internal/zoom"
)

func TestGraceDuration(t *testing.T) {
	min, max := 90*time.Second, 15*time.Minute
	cases := []struct {
		booking, want time.Duration
	}{
		{120 * time.Minute, 12 * time.Minute}, // 10%
		{60 * time.Minute, 6 * time.Minute},
		{30 * time.Minute, 3 * time.Minute},
		{15 * time.Minute, 90 * time.Second},  // 10% == min
		{5 * time.Minute, 90 * time.Second},   // below min -> clamp up
		{300 * time.Minute, 15 * time.Minute}, // 10%=30m -> clamp down to max
	}
	for _, c := range cases {
		if got := graceDuration(c.booking, 0.10, min, max); got != c.want {
			t.Errorf("graceDuration(%v) = %v, want %v", c.booking, got, c.want)
		}
	}
}

// newNoShowServer builds a server over the seeded mock and runs one sync. The
// default seed has res-agung (start -10m, 90m booking -> 9m grace, deadline
// now-1m) sitting empty and unchecked — the one true no-show at t=now.
func newNoShowServer(t *testing.T, now time.Time) *Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.NewMemory()
	db, err := store.OpenDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	zc := zoom.NewMockClient(now, nil, log)
	sy := syncsvc.New(zc, st, "", log)
	if _, err := sy.Run(context.Background(), now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	return NewServer(st, db, sy, zc, "mock", 30*time.Minute, log)
}

func TestSweepNoShowsReleasesExpiredEmptyBooking(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)

	released := srv.sweepNoShows(context.Background(), now)
	if len(released) != 1 || released[0].ReservationID != "res-agung" {
		var got []string
		for _, r := range released {
			got = append(got, r.ReservationID)
		}
		t.Fatalf("released = %v, want [res-agung]", got)
	}
	if r, _ := srv.store.Reservation("res-agung"); r.Status != domain.StatusReleased {
		t.Errorf("res-agung status = %q, want %q", r.Status, domain.StatusReleased)
	}
	// A booking still inside its grace window must be untouched.
	if r, _ := srv.store.Reservation("res-petang"); r.Status != domain.StatusBooked {
		t.Errorf("res-petang status = %q, want booked (still in grace)", r.Status)
	}
	// Idempotent: a second sweep releases nothing more.
	if again := srv.sweepNoShows(context.Background(), now); len(again) != 0 {
		t.Errorf("second sweep released %d, want 0", len(again))
	}
}

func TestSweepNoShowsSkipsOccupiedRoom(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)
	// A phone is physically in ws-agung -> presence truth beats the clock.
	srv.store.SetDeviceRoom("dev-x", "ws-agung", "Someone", now.UnixMilli())

	for _, r := range srv.sweepNoShows(context.Background(), now) {
		if r.ReservationID == "res-agung" {
			t.Fatalf("res-agung released despite live presence")
		}
	}
	if r, _ := srv.store.Reservation("res-agung"); r.Status != domain.StatusBooked {
		t.Errorf("res-agung status = %q, want booked (occupied)", r.Status)
	}
}
