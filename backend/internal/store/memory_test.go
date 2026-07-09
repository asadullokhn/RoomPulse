package store

import (
	"testing"
	"time"

	"quickroom/internal/domain"
)

func occCount(m *Memory, ws string) int {
	return len(m.AllOccupancy()[ws])
}

func TestSetDeviceRoom_Reconciliation(t *testing.T) {
	m := NewMemory()

	if changed, prev := m.SetDeviceRoom("dev1", "ws-b", "Ali", 100); !changed || prev != "" {
		t.Fatalf("enter: changed=%v prev=%q", changed, prev)
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("after enter, ws-b=%d want 1", occCount(m, "ws-b"))
	}

	// idempotent re-send (same room, newer ts) -> no change, still present
	if changed, _ := m.SetDeviceRoom("dev1", "ws-b", "Ali", 101); changed {
		t.Fatal("idempotent re-send reported a change")
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("after idempotent, ws-b=%d want 1", occCount(m, "ws-b"))
	}

	// stale ts ignored
	if changed, _ := m.SetDeviceRoom("dev1", "", "Ali", 50); changed {
		t.Fatal("stale ts caused a change")
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("after stale, ws-b=%d want 1", occCount(m, "ws-b"))
	}

	// move rooms
	if changed, prev := m.SetDeviceRoom("dev1", "ws-c", "Ali", 102); !changed || prev != "ws-b" {
		t.Fatalf("move: changed=%v prev=%q", changed, prev)
	}
	if occCount(m, "ws-b") != 0 || occCount(m, "ws-c") != 1 {
		t.Fatalf("after move: b=%d c=%d", occCount(m, "ws-b"), occCount(m, "ws-c"))
	}

	// leave
	if changed, prev := m.SetDeviceRoom("dev1", "", "Ali", 103); !changed || prev != "ws-c" {
		t.Fatalf("leave: changed=%v prev=%q", changed, prev)
	}
	if occCount(m, "ws-c") != 0 {
		t.Fatalf("after leave, ws-c=%d want 0", occCount(m, "ws-c"))
	}
}

func TestReapStale(t *testing.T) {
	m := NewMemory()
	m.SetDeviceRoom("fresh", "ws-a", "Fresh", 1)
	m.SetDeviceRoom("stale", "ws-b", "Stale", 1)

	time.Sleep(20 * time.Millisecond)
	m.SetDeviceRoom("fresh", "ws-a", "Fresh", 2) // refresh fresh's seen time

	vacated := m.ReapStale(10 * time.Millisecond)

	if occCount(m, "ws-a") != 1 {
		t.Fatalf("fresh wrongly reaped: ws-a=%d", occCount(m, "ws-a"))
	}
	if occCount(m, "ws-b") != 0 {
		t.Fatalf("stale not reaped: ws-b=%d", occCount(m, "ws-b"))
	}
	if len(vacated) != 1 || vacated[0].WorkspaceID != "ws-b" {
		t.Fatalf("vacated=%v want one device in ws-b", vacated)
	}
}

func TestReapStaleUserPresence(t *testing.T) {
	m := NewMemory()
	m.ApplyPresenceIfNewer("ws-a", "u-fresh", "Fresh", 1, true)
	m.ApplyPresenceIfNewer("ws-b", "u-stale", "Stale", 1, true)
	m.SetDeviceRoom("dev-1", "ws-b", "Device", 1) // device entries belong to ReapStale

	time.Sleep(20 * time.Millisecond)
	m.ApplyPresenceIfNewer("ws-a", "u-fresh", "Fresh", 2, true) // re-report refreshes the clock

	reaped := m.ReapStaleUserPresence(10 * time.Millisecond)

	if occCount(m, "ws-a") != 1 {
		t.Fatalf("fresh user wrongly reaped: ws-a=%d", occCount(m, "ws-a"))
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("ws-b=%d want 1 (stale user gone, device kept)", occCount(m, "ws-b"))
	}
	if len(reaped) != 1 || reaped[0].UserID != "u-stale" || reaped[0].WorkspaceID != "ws-b" {
		t.Fatalf("reaped=%v want u-stale in ws-b", reaped)
	}
}

// The selector must ignore workspace history (a cancelled booking from two
// days ago once swallowed a live check-in) and pick what owns the room now.
func TestReservationOwningWorkspace(t *testing.T) {
	m := NewMemory()
	now := time.Now()
	mk := func(id string, start, end time.Duration, status domain.ReservationStatus, ci domain.CheckInStatus) {
		m.UpsertReservation(domain.Reservation{
			ReservationID: id, ZoomWorkspaceID: "ws-a",
			StartTime: now.Add(start), EndTime: now.Add(end),
			Status: status, CheckInStatus: ci,
		})
	}
	mk("ancient-cancelled", -48*time.Hour, -47*time.Hour, domain.StatusCancelled, domain.NotCheckedIn)
	mk("yesterday", -24*time.Hour, -23*time.Hour, domain.StatusReleased, domain.NotCheckedIn)

	if _, ok := m.ReservationOwningWorkspace("ws-a", now); ok {
		t.Fatal("history alone must not own the workspace")
	}

	mk("upcoming", 10*time.Minute, 40*time.Minute, domain.StatusBooked, domain.NotCheckedIn)
	if r, ok := m.ReservationOwningWorkspace("ws-a", now); !ok || r.ReservationID != "upcoming" {
		t.Fatalf("early arrival should pick upcoming, got %+v ok=%v", r, ok)
	}

	mk("recent-end", -90*time.Minute, -5*time.Minute, domain.StatusBooked, domain.CheckedIn)
	if r, _ := m.ReservationOwningWorkspace("ws-a", now); r.ReservationID != "recent-end" {
		t.Fatalf("late leaver should beat upcoming, got %s", r.ReservationID)
	}

	mk("current", -10*time.Minute, 20*time.Minute, domain.StatusBooked, domain.NotCheckedIn)
	if r, _ := m.ReservationOwningWorkspace("ws-a", now); r.ReservationID != "current" {
		t.Fatalf("current window must win, got %s", r.ReservationID)
	}
}

func TestApplyPresenceIfNewer(t *testing.T) {
	m := NewMemory()
	if ok, _ := m.ApplyPresenceIfNewer("ws-b", "u1", "U1", 100, true); !ok {
		t.Fatal("first entered should apply")
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("after entered, ws-b=%d", occCount(m, "ws-b"))
	}
	if ok, _ := m.ApplyPresenceIfNewer("ws-b", "u1", "U1", 50, false); ok {
		t.Fatal("stale exit should be ignored")
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("after stale exit, ws-b=%d want 1", occCount(m, "ws-b"))
	}
	if ok, _ := m.ApplyPresenceIfNewer("ws-b", "u1", "U1", 200, false); !ok {
		t.Fatal("newer exit should apply")
	}
	if occCount(m, "ws-b") != 0 {
		t.Fatalf("after exit, ws-b=%d want 0", occCount(m, "ws-b"))
	}
}

// A person is in one room at a time: an enter in a new room moves the user
// out of the old one (the phone never sends that exit — the beacon region
// covers the whole building).
func TestApplyPresenceMovesUserBetweenRooms(t *testing.T) {
	m := NewMemory()
	m.ApplyPresenceIfNewer("ws-a", "u1", "U1", 100, true)
	m.SetDeviceRoom("dev-1", "ws-a", "Device", 100) // device entry must not be swept up

	ok, moved := m.ApplyPresenceIfNewer("ws-b", "u1", "U1", 200, true)
	if !ok || len(moved) != 1 || moved[0] != "ws-a" {
		t.Fatalf("move: ok=%v moved=%v, want ws-a", ok, moved)
	}
	if occCount(m, "ws-a") != 1 { // the device stays
		t.Fatalf("ws-a=%d want 1 (device only)", occCount(m, "ws-a"))
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("ws-b=%d want 1", occCount(m, "ws-b"))
	}

	// A late out-of-order enter for the old room must not move the user back.
	if ok, _ := m.ApplyPresenceIfNewer("ws-a", "u1", "U1", 150, true); ok {
		t.Fatal("stale cross-room enter should be ignored")
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("after stale enter, ws-b=%d want 1", occCount(m, "ws-b"))
	}
}
