package store

import (
	"testing"
	"time"
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
	if len(vacated) != 1 || vacated[0] != "ws-b" {
		t.Fatalf("vacated=%v want [ws-b]", vacated)
	}
}

func TestApplyPresenceIfNewer(t *testing.T) {
	m := NewMemory()
	if !m.ApplyPresenceIfNewer("ws-b", "u1", "U1", 100, true) {
		t.Fatal("first entered should apply")
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("after entered, ws-b=%d", occCount(m, "ws-b"))
	}
	if m.ApplyPresenceIfNewer("ws-b", "u1", "U1", 50, false) {
		t.Fatal("stale exit should be ignored")
	}
	if occCount(m, "ws-b") != 1 {
		t.Fatalf("after stale exit, ws-b=%d want 1", occCount(m, "ws-b"))
	}
	if !m.ApplyPresenceIfNewer("ws-b", "u1", "U1", 200, false) {
		t.Fatal("newer exit should apply")
	}
	if occCount(m, "ws-b") != 0 {
		t.Fatalf("after exit, ws-b=%d want 0", occCount(m, "ws-b"))
	}
}
