package api

import (
	"time"
)

// User rating (admin-only, invisible to the user): how reliably someone uses
// the rooms they book. Checking in on time keeps the score up; no-show
// releases pull it down. Below the threshold the no-show grace window is
// halved — a booking a good user could reclaim for 10 minutes is released
// after 5 for a repeat no-shower.
const (
	badRatingThreshold   = 50
	badRatingGraceFactor = 0.5
)

// ratingInfo is the admin-facing rating breakdown for one user.
type ratingInfo struct {
	Auto      int  `json:"auto"`               // computed from booking history
	Override  *int `json:"override,omitempty"` // admin-pinned value, wins when set
	Effective int  `json:"effective"`
	Good      int  `json:"good"` // bookings they showed up for
	Bad       int  `json:"bad"`  // no-show releases
}

// autoRating scores booking behaviour 0..100: the share of resolved bookings
// the user actually showed up for. No history = benefit of the doubt (100).
func autoRating(good, bad int) int {
	if good+bad == 0 {
		return 100
	}
	return (good * 100) / (good + bad)
}

// userRatings assembles the rating for every user with history or an
// override. Users absent from the map are implicitly 100 (no history).
func (s *Server) userRatings() (map[string]ratingInfo, error) {
	counts, err := s.db.UserRatingCounts()
	if err != nil {
		return nil, err
	}
	overrides, err := s.db.UserRatingOverrides()
	if err != nil {
		return nil, err
	}
	out := make(map[string]ratingInfo, len(counts)+len(overrides))
	for id, c := range counts {
		out[id] = ratingInfo{Auto: autoRating(c.Good, c.Bad), Good: c.Good, Bad: c.Bad}
	}
	for id, v := range overrides {
		ri := out[id]
		if _, hadHistory := counts[id]; !hadHistory {
			ri.Auto = autoRating(0, 0)
		}
		o := v
		ri.Override = &o
		out[id] = ri
	}
	for id, ri := range out {
		ri.Effective = ri.Auto
		if ri.Override != nil {
			ri.Effective = *ri.Override
		}
		out[id] = ri
	}
	return out, nil
}

// ratingsOrEmpty is userRatings for the sweeps: a DB hiccup must not stall
// no-show handling, so it degrades to default ratings (full grace).
func (s *Server) ratingsOrEmpty() map[string]ratingInfo {
	ratings, err := s.userRatings()
	if err != nil {
		s.log.Warn("user ratings", "err", err)
		return map[string]ratingInfo{}
	}
	return ratings
}

// effectiveGrace is the no-show grace window for a booking, shortened for
// bookers with a bad rating. Bookers without an app account (Zoom-sourced)
// get the default window.
func (s *Server) effectiveGrace(bookingLen time.Duration, bookerID string, ratings map[string]ratingInfo) time.Duration {
	g := graceDuration(bookingLen, s.graceFraction, s.graceMin, s.graceMax)
	if ri, ok := ratings[bookerID]; ok && ri.Effective < badRatingThreshold {
		g = time.Duration(float64(g) * badRatingGraceFactor)
	}
	return g
}
