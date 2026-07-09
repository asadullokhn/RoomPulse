package api

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"quickroom/internal/appleauth"
	"quickroom/internal/authtoken"
	"quickroom/internal/domain"
	"quickroom/internal/store"
	syncsvc "quickroom/internal/sync"
	"quickroom/internal/zoom"
)

// newNoShowServer builds a server over the seeded mock and runs one sync.
// Production grace: a fixed 12m window. The seed's res-agung (start -10m) is
// the ripest empty unchecked booking — its deadline is now+2m, so sweeps at
// now+3m release it while res-ubud (-5m) and res-petang (-2m) stay in grace.
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
	sy := syncsvc.New(zc, st, db, "", log)
	if _, err := sy.Run(context.Background(), now); err != nil {
		t.Fatalf("sync: %v", err)
	}
	return NewServer(st, db, sy, zc, "mock", 30*time.Minute, appleauth.NewVerifier("test.bundle.id", nil), time.Hour, authtoken.NewSigner([]byte("test-jwt-secret")), log)
}

func TestSweepNoShowsReleasesExpiredEmptyBooking(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)

	released := srv.sweepNoShows(context.Background(), now.Add(3*time.Minute))
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
	if again := srv.sweepNoShows(context.Background(), now.Add(3*time.Minute)); len(again) != 0 {
		t.Errorf("second sweep released %d, want 0", len(again))
	}
}

func TestSweepNoShowsSkipsOccupiedRoom(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)
	// A phone is physically in ws-agung -> presence truth beats the clock.
	srv.store.SetDeviceRoom("dev-x", "ws-agung", "Someone", now.UnixMilli())

	for _, r := range srv.sweepNoShows(context.Background(), now.Add(3*time.Minute)) {
		if r.ReservationID == "res-agung" {
			t.Fatalf("res-agung released despite live presence")
		}
	}
	if r, _ := srv.store.Reservation("res-agung"); r.Status != domain.StatusBooked {
		t.Errorf("res-agung status = %q, want booked (occupied)", r.Status)
	}
}
