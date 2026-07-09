package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"quickroom/internal/domain"
)

func TestAutoRating(t *testing.T) {
	cases := []struct{ good, bad, want int }{
		{0, 0, 100}, // no history: benefit of the doubt
		{5, 0, 100},
		{3, 1, 75},
		{1, 1, 50},
		{1, 3, 25},
		{0, 4, 0},
	}
	for _, c := range cases {
		if got := autoRating(c.good, c.bad); got != c.want {
			t.Errorf("autoRating(%d,%d) = %d, want %d", c.good, c.bad, got, c.want)
		}
	}
}

// seedHistory persists resolved app bookings: good shows, bad no-shows.
func seedHistory(t *testing.T, s *Server, userID string, good, bad int) {
	t.Helper()
	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < good; i++ {
		save := domain.Reservation{
			ReservationID: userID + "-good-" + string(rune('a'+i)), ZoomWorkspaceID: "ws-ubud",
			BookedByUserID: userID, StartTime: base, EndTime: base.Add(time.Hour),
			Status: domain.StatusBooked, CheckInStatus: domain.CheckedOut, Source: "app",
		}
		if err := s.db.SaveAppReservation(save); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < bad; i++ {
		save := domain.Reservation{
			ReservationID: userID + "-bad-" + string(rune('a'+i)), ZoomWorkspaceID: "ws-ubud",
			BookedByUserID: userID, StartTime: base, EndTime: base.Add(time.Hour),
			Status: domain.StatusReleased, CheckInStatus: domain.NotCheckedIn, Source: "app",
		}
		if err := s.db.SaveAppReservation(save); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUserRatingsComputedAndOverridden(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	_ = mintSession(t, s, "u-flaky", "flaky@x.y")
	seedHistory(t, s, "u-flaky", 1, 3) // 25

	ratings, err := s.userRatings()
	if err != nil {
		t.Fatal(err)
	}
	ri := ratings["u-flaky"]
	if ri.Auto != 25 || ri.Effective != 25 || ri.Good != 1 || ri.Bad != 3 || ri.Override != nil {
		t.Fatalf("computed rating = %+v", ri)
	}

	// Admin pins it back up: override wins over history.
	if err := s.db.SetUserRatingOverride("u-flaky", ptr(90)); err != nil {
		t.Fatal(err)
	}
	ratings, _ = s.userRatings()
	ri = ratings["u-flaky"]
	if ri.Auto != 25 || ri.Effective != 90 || ri.Override == nil || *ri.Override != 90 {
		t.Fatalf("overridden rating = %+v", ri)
	}

	// Override on a user with no history: auto stays the default 100.
	_ = mintSession(t, s, "u-new", "new@x.y")
	if err := s.db.SetUserRatingOverride("u-new", ptr(10)); err != nil {
		t.Fatal(err)
	}
	ratings, _ = s.userRatings()
	if ri := ratings["u-new"]; ri.Auto != 100 || ri.Effective != 10 {
		t.Fatalf("no-history override = %+v", ri)
	}
}

func ptr(v int) *int { return &v }

func TestEffectiveGraceHalvedForBadRating(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	ratings := map[string]ratingInfo{
		"u-bad":  {Effective: 25},
		"u-good": {Effective: 80},
	}
	booking := 60 * time.Minute // 10% = 6m
	if got := s.effectiveGrace(booking, "u-good", ratings); got != 6*time.Minute {
		t.Errorf("good grace = %v, want 6m", got)
	}
	if got := s.effectiveGrace(booking, "u-bad", ratings); got != 3*time.Minute {
		t.Errorf("bad grace = %v, want 3m", got)
	}
	if got := s.effectiveGrace(booking, "u-unknown", ratings); got != 6*time.Minute {
		t.Errorf("unknown booker grace = %v, want 6m", got)
	}
}

// A booking 4m into its window (60m long -> 6m default grace) survives the
// sweep for a good booker but is released for one rated below the threshold
// (grace halved to 3m).
func TestSweepNoShowsReleasesBadRatedBookerSooner(t *testing.T) {
	now := time.Now()
	s := newNoShowServer(t, now)
	_ = mintSession(t, s, "u-flaky", "flaky@x.y")
	seedHistory(t, s, "u-flaky", 0, 2) // rating 0

	res := domain.Reservation{
		ReservationID: "res-flaky", ZoomWorkspaceID: "ws-bedugul",
		BookedByUserID: "u-flaky", UserEmail: "flaky@x.y",
		StartTime: now.Add(-4 * time.Minute), EndTime: now.Add(56 * time.Minute),
		Status: domain.StatusBooked, CheckInStatus: domain.NotCheckedIn, Source: "app",
	}
	s.store.UpsertReservation(res)

	s.sweepNoShows(t.Context(), now)
	got, _ := s.store.Reservation("res-flaky")
	if got.Status != domain.StatusReleased {
		t.Fatalf("bad-rated booker status = %s, want released at 4m of a 3m grace", got.Status)
	}

	// Same booking shape, good booker: still inside the 6m grace.
	_ = mintSession(t, s, "u-solid", "solid@x.y")
	res2 := res
	res2.ReservationID, res2.BookedByUserID, res2.UserEmail = "res-solid", "u-solid", "solid@x.y"
	s.store.UpsertReservation(res2)
	s.sweepNoShows(t.Context(), now)
	if got, _ := s.store.Reservation("res-solid"); got.Status != domain.StatusBooked {
		t.Fatalf("good booker status = %s, want still booked at 4m of a 6m grace", got.Status)
	}
}

// A booking that already ended must never be released as a no-show — after a
// restart wiped in-memory presence, the sweep pushed "you didn't check in"
// for a booking that was over (and had been attended).
func TestSweepNoShowsSkipsEndedBookings(t *testing.T) {
	now := time.Now()
	s := newNoShowServer(t, now)
	res := domain.Reservation{
		ReservationID: "res-over-and-done", ZoomWorkspaceID: "ws-bedugul",
		BookedByUserID: "u-x", UserEmail: "x@x.y",
		StartTime: now.Add(-40 * time.Minute), EndTime: now.Add(-10 * time.Minute),
		Status: domain.StatusBooked, CheckInStatus: domain.NotCheckedIn, Source: "app",
	}
	s.store.UpsertReservation(res)

	s.sweepNoShows(t.Context(), now)
	if got, _ := s.store.Reservation("res-over-and-done"); got.Status != domain.StatusBooked {
		t.Fatalf("ended booking status = %s, want untouched booked", got.Status)
	}
}

// A checked-in booker stepping out must not be checked out immediately —
// only after checkoutLinger of absence. Returning within the linger clears
// the clock with no visible state change.
func TestDeferredCheckoutLinger(t *testing.T) {
	now := time.Now()
	s := newNoShowServer(t, now)
	res := domain.Reservation{
		ReservationID: "res-linger", ZoomWorkspaceID: "ws-bedugul",
		BookedByUserID: "u-lin", UserEmail: "lin@x.y",
		StartTime: now.Add(-30 * time.Minute), EndTime: now.Add(60 * time.Minute),
		Status: domain.StatusBooked, CheckInStatus: domain.CheckedIn, Source: "app",
	}
	s.store.UpsertReservation(res)

	// Absent booker: first sweep only starts the clock.
	s.sweepDeferredCheckouts(t.Context(), now)
	if got, _ := s.store.Reservation("res-linger"); got.CheckInStatus != domain.CheckedIn {
		t.Fatalf("immediately after exit: %s, want still checked_in", got.CheckInStatus)
	}

	// Comes back 2 minutes later — the clock resets, nothing happened.
	s.store.ApplyPresenceIfNewer("ws-bedugul", "u-lin", "Lin", now.UnixMilli(), true)
	s.sweepDeferredCheckouts(t.Context(), now.Add(2*time.Minute))
	s.store.ApplyPresenceIfNewer("ws-bedugul", "u-lin", "Lin", now.UnixMilli()+1, false)

	// Absent again: linger must elapse from the NEW absence, not the first.
	s.sweepDeferredCheckouts(t.Context(), now.Add(3*time.Minute))
	s.sweepDeferredCheckouts(t.Context(), now.Add(10*time.Minute))
	if got, _ := s.store.Reservation("res-linger"); got.CheckInStatus != domain.CheckedIn {
		t.Fatalf("7m into second absence: %s, want still checked_in", got.CheckInStatus)
	}
	s.sweepDeferredCheckouts(t.Context(), now.Add(19*time.Minute))
	if got, _ := s.store.Reservation("res-linger"); got.CheckInStatus != domain.CheckedOut {
		t.Fatalf("16m into second absence: %s, want checked_out", got.CheckInStatus)
	}
}

// A real no-show release keeps not_checked_in — stamping checked_out made
// every no-show count as "showed up" in the rating tally.
func TestNoShowReleaseKeepsNotCheckedIn(t *testing.T) {
	now := time.Now()
	s := newNoShowServer(t, now)
	// Default seed: res-agung is the live no-show at t=now.
	s.sweepNoShows(t.Context(), now)
	got, ok := s.store.Reservation("res-agung")
	if !ok || got.Status != domain.StatusReleased {
		t.Fatalf("res-agung = %+v ok=%v, want released", got, ok)
	}
	if got.CheckInStatus != domain.NotCheckedIn {
		t.Fatalf("check_in_status = %s, want not_checked_in", got.CheckInStatus)
	}
}

func TestPatchUserRatingOverride(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	_ = mintSession(t, s, "u-rt", "rt@x.y")

	if rec := adminDo(t, s, "PATCH", "/users/u-rt", `{"rating_override":30}`); rec.Code != http.StatusOK {
		t.Fatalf("set override status = %d body=%s", rec.Code, rec.Body)
	}
	ratings, _ := s.userRatings()
	if ri := ratings["u-rt"]; ri.Override == nil || *ri.Override != 30 || ri.Effective != 30 {
		t.Fatalf("after set = %+v", ri)
	}

	if rec := adminDo(t, s, "PATCH", "/users/u-rt", `{"rating_override":150}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("out-of-range status = %d", rec.Code)
	}
	if rec := adminDo(t, s, "PATCH", "/users/u-rt", `{}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty patch status = %d", rec.Code)
	}

	if rec := adminDo(t, s, "PATCH", "/users/u-rt", `{"clear_rating_override":true}`); rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d", rec.Code)
	}
	ratings, _ = s.userRatings()
	if ri, ok := ratings["u-rt"]; ok && ri.Override != nil {
		t.Fatalf("after clear = %+v", ri)
	}
}

func TestListUsersCarriesRating(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	_ = mintSession(t, s, "u-lst", "lst@x.y")
	seedHistory(t, s, "u-lst", 1, 1)

	rec := adminDo(t, s, "GET", "/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var body struct {
		Users []struct {
			UserID string     `json:"user_id"`
			Rating ratingInfo `json:"rating"`
		} `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, u := range body.Users {
		if u.UserID == "u-lst" {
			if u.Rating.Effective != 50 || u.Rating.Good != 1 || u.Rating.Bad != 1 {
				t.Fatalf("rating = %+v", u.Rating)
			}
			return
		}
	}
	t.Fatal("u-lst missing from /users")
}
