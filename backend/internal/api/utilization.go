package api

import (
	"net/http"
	"time"

	"roompulse/internal/domain"
)

// Utilization is an at-a-glance booking-health report — the "are rooms actually
// used?" question the mentors care about. Counts over the current reservation set
// plus live occupancy, so the admin panel can show the no-show / reclaim rate at
// the top of the dashboard.
type Utilization struct {
	Bookings       int       `json:"bookings"`         // reservations in the window
	CheckedIn      int       `json:"checked_in"`       // someone actually showed
	NoShowReleased int       `json:"no_show_released"` // auto-reclaimed no-shows
	Booked         int       `json:"booked"`           // still booked, not yet resolved
	NoShowRate     float64   `json:"no_show_rate"`     // released / bookings (0..1)
	RoomsTotal     int       `json:"rooms_total"`
	RoomsOccupied  int       `json:"rooms_occupied"`
	PeoplePresent  int       `json:"people_present"`
	GeneratedAt    time.Time `json:"generated_at"`
}

// utilization aggregates the current reservation set and live occupancy.
func (s *Server) utilization(now time.Time) Utilization {
	res := s.store.Reservations()
	u := Utilization{Bookings: len(res), RoomsTotal: len(s.store.Rooms()), GeneratedAt: now}
	for _, r := range res {
		switch r.Status {
		case domain.StatusReleased:
			u.NoShowReleased++
		case domain.StatusBooked:
			u.Booked++
		}
		if r.CheckInStatus == domain.CheckedIn {
			u.CheckedIn++
		}
	}
	if u.Bookings > 0 {
		u.NoShowRate = float64(u.NoShowReleased) / float64(u.Bookings)
	}
	for _, users := range s.store.AllOccupancy() {
		if len(users) > 0 {
			u.RoomsOccupied++
			u.PeoplePresent += len(users)
		}
	}
	return u
}

// getUtilization serves the booking-health report.
func (s *Server) getUtilization(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.utilization(time.Now()))
}
