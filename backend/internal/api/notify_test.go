package api

import (
	"context"
	"testing"
	"time"
)

func TestNotifierDedup(t *testing.T) {
	n := newNotifier(10)
	if !n.emit("k", Notification{Type: "x"}) {
		t.Fatal("first emit should be new")
	}
	if n.emit("k", Notification{Type: "x"}) {
		t.Fatal("second emit with same key should be deduped")
	}
	if got := len(n.recent("", 10)); got != 1 {
		t.Fatalf("recent = %d, want 1", got)
	}
	// an empty key never dedupes
	n.emit("", Notification{Type: "y"})
	n.emit("", Notification{Type: "y"})
	if got := len(n.recent("", 10)); got != 3 {
		t.Fatalf("recent after 2 keyless = %d, want 3", got)
	}
}

func countByType(notes []Notification, typ string) int {
	c := 0
	for _, n := range notes {
		if n.Type == typ {
			c++
		}
	}
	return c
}

func reminderLevels(notes []Notification, resID string) []int {
	var out []int
	for _, n := range notes {
		if n.ReservationID == resID && n.Type == "grace_reminder" {
			out = append(out, n.Level)
		}
	}
	return out
}

// TestSweepGraceRemindersLadder: production ladder — first ping 2m into the
// booking, last call 2m before the 12m release. At t=now against the seed:
// res-petang (start -2m) is exactly at its first ping -> L1; res-ubud (start
// -5m) past the first, before its last call (+5m) -> L1; res-agung (start
// -10m) is past the first AND exactly at its last call (deadline +2m) -> L1+L2.
func TestSweepGraceRemindersLadder(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)
	srv.sweepGraceReminders(now)
	all := srv.notify.recent("", 100)

	if got := countByType(all, "grace_reminder"); got != 4 {
		t.Fatalf("grace_reminders = %d, want 4 (petang L1, ubud L1, agung L1+L2)", got)
	}
	if lv := reminderLevels(all, "res-petang"); len(lv) != 1 || lv[0] != 1 {
		t.Errorf("res-petang levels = %v, want [1]", lv)
	}
	if lv := reminderLevels(all, "res-ubud"); len(lv) != 1 || lv[0] != 1 {
		t.Errorf("res-ubud levels = %v, want [1]", lv)
	}
	if lv := reminderLevels(all, "res-agung"); len(lv) != 2 {
		t.Errorf("res-agung levels = %v, want both (1 and 2)", lv)
	}
	// idempotent: pings don't re-fire on a second sweep
	srv.sweepGraceReminders(now)
	if got := countByType(srv.notify.recent("", 100), "grace_reminder"); got != 4 {
		t.Errorf("grace_reminders after second sweep = %d, want 4", got)
	}
	// recipient filter narrows to one booker
	if got := len(srv.notify.recent("standup@adabali.dev", 100)); got != 1 {
		t.Errorf("petang booker notifications = %d, want 1", got)
	}
}

func TestSweepNoShowsEmitsNotifications(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)
	srv.sweepNoShows(context.Background(), now.Add(3*time.Minute))
	all := srv.notify.recent("", 100)

	if got := countByType(all, "no_show_released"); got != 1 {
		t.Fatalf("no_show_released = %d, want 1", got)
	}
	// No "room freed" broadcast: the booker's targeted note is the only emit.
	if got := countByType(all, "room_freed"); got != 0 {
		t.Fatalf("room_freed = %d, want 0", got)
	}
	found := false
	for _, n := range all {
		if n.Type == "no_show_released" && n.ReservationID == "res-agung" {
			found = true
			if n.Recipient != "demo.day@adabali.dev" {
				t.Errorf("recipient = %q, want demo.day@adabali.dev", n.Recipient)
			}
		}
	}
	if !found {
		t.Error("no no_show_released notification for res-agung")
	}
}

func TestAPNSFieldsPerType(t *testing.T) {
	cases := []struct {
		typ, resID, wsID                 string
		category, interruption, collapse string
	}{
		{"grace_reminder", "res-1", "ws-a", "GRACE_REMINDER", "time-sensitive", "grace-res-1"},
		{"no_show_released", "res-1", "ws-a", "NO_SHOW_RELEASED", "active", "res-res-1"},
		{"collision", "res-2", "ws-b", "COLLISION", "time-sensitive", "res-res-2"},
		{"overstay", "res-3", "ws-b", "OVERSTAY", "active", "res-res-3"},
		{"unknown_type", "res-4", "ws-c", "", "", ""},
	}
	for _, c := range cases {
		cat, level, collapse := apnsFields(Notification{Type: c.typ, ReservationID: c.resID, WorkspaceID: c.wsID})
		if cat != c.category || level != c.interruption || collapse != c.collapse {
			t.Fatalf("%s: got (%q,%q,%q), want (%q,%q,%q)", c.typ, cat, level, collapse, c.category, c.interruption, c.collapse)
		}
	}
}

func TestAPNSFieldsEmptyIDMeansNoCollapse(t *testing.T) {
	if _, _, collapse := apnsFields(Notification{Type: "grace_reminder"}); collapse != "" {
		t.Fatalf("collapse for empty reservation id = %q, want empty", collapse)
	}
}
