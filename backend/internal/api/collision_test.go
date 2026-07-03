package api

import (
	"testing"
	"time"

	"quickroom/internal/domain"
)

func TestIdentityMatch(t *testing.T) {
	cases := []struct {
		booker, occupant string
		want             bool
	}{
		{"standup@adabali.dev", "standup@adabali.dev", true}, // same email
		{"standup@adabali.dev", "Standup", true},             // local-part vs name
		{"standup@adabali.dev", "Standup Team", true},        // extra words
		{"mentors@adabali.dev", "Mentors Panel", true},
		{"demo.day@adabali.dev", "Random Guy", false}, // a stranger
		{"demo.day@adabali.dev", "", false},           // no identity
		{"", "someone", false},
		{"standup@adabali.dev", "interviews@adabali.dev", false}, // different bookers
	}
	for _, c := range cases {
		if got := identityMatch(c.booker, c.occupant); got != c.want {
			t.Errorf("identityMatch(%q, %q) = %v, want %v", c.booker, c.occupant, got, c.want)
		}
	}
}

// TestCurrentCollisionsFlagsStranger: res-agung (booked to demo.day@, active
// window) with a non-booker physically present is a collision; the booker being
// present instead is not.
func TestCurrentCollisionsFlagsStranger(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)

	// A stranger walks into ws-agung (booked by demo.day@adabali.dev).
	srv.store.ApplyPresenceIfNewer("ws-agung", "u-random", "Random Guy", now.UnixMilli(), true)

	cs := srv.currentCollisions(now)
	if len(cs) != 1 || cs[0].ReservationID != "res-agung" {
		var got []string
		for _, c := range cs {
			got = append(got, c.ReservationID)
		}
		t.Fatalf("collisions = %v, want [res-agung]", got)
	}
	if cs[0].Booker != "demo.day@adabali.dev" {
		t.Errorf("booker = %q, want demo.day@adabali.dev", cs[0].Booker)
	}

	// Now the booker also arrives -> legitimate, no collision.
	srv.store.ApplyPresenceIfNewer("ws-agung", "demo.day@adabali.dev", "Demo Day", now.UnixMilli()+1, true)
	if cs := srv.currentCollisions(now); len(cs) != 0 {
		t.Errorf("collisions with booker present = %d, want 0", len(cs))
	}
}

// TestCurrentCollisionsIgnoresEmptyAndFuture: an empty room is a no-show not a
// collision, and a not-yet-started booking is neither.
func TestCurrentCollisionsIgnoresEmptyAndFuture(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)

	// res-sanur starts +15m; even if occupied it's outside its window.
	srv.store.ApplyPresenceIfNewer("ws-sanur", "u-early", "Early Bird", now.UnixMilli(), true)
	for _, c := range srv.currentCollisions(now) {
		if c.ReservationID == "res-sanur" {
			t.Errorf("res-sanur flagged before its window starts")
		}
	}
	// Default seed rooms are otherwise empty -> no collisions.
	base := newNoShowServer(t, now)
	if cs := base.currentCollisions(now); len(cs) != 0 {
		t.Errorf("baseline collisions = %d, want 0 (all rooms empty)", len(cs))
	}
}

// TestCurrentCollisionsSkipsReleased: once a booking is released it can't collide.
func TestCurrentCollisionsSkipsReleased(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)
	srv.store.ApplyPresenceIfNewer("ws-agung", "u-random", "Random Guy", now.UnixMilli(), true)

	r, _ := srv.store.Reservation("res-agung")
	r.Status = domain.StatusReleased
	srv.store.UpsertReservation(r)

	if cs := srv.currentCollisions(now); len(cs) != 0 {
		t.Errorf("released booking flagged as collision: %d", len(cs))
	}
}

// TestSweepCollisionsEmitsNotifications: the sweep emits a booker heads-up and an
// admin broadcast, once (deduped) across repeated sweeps.
func TestSweepCollisionsEmitsNotifications(t *testing.T) {
	now := time.Now()
	srv := newNoShowServer(t, now)
	srv.store.ApplyPresenceIfNewer("ws-agung", "u-random", "Random Guy", now.UnixMilli(), true)

	srv.sweepCollisions(now)
	all := srv.notify.recent("", 100)
	if got := countByType(all, "collision"); got != 2 {
		t.Fatalf("collision notifications = %d, want 2 (booker + admin)", got)
	}
	// booker gets exactly one heads-up
	if got := len(srv.notify.recent("demo.day@adabali.dev", 100)); got != 1 {
		t.Errorf("booker collision notifications = %d, want 1", got)
	}
	// idempotent
	srv.sweepCollisions(now)
	if got := countByType(srv.notify.recent("", 100), "collision"); got != 2 {
		t.Errorf("collision notifications after second sweep = %d, want 2", got)
	}
}
