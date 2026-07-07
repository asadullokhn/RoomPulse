// Package domain holds QuickRoom's local model. Zoom stays the booking source
// of truth; these types are what the app reasons about (rooms + reservations
// joined to beacon identity).
package domain

import "time"

// Room is a local meeting room mapped to a Zoom workspace and an iBeacon.
type Room struct {
	RoomID          string `json:"room_id"`
	ZoomWorkspaceID string `json:"zoom_workspace_id"`
	Name            string `json:"name"`
	Floor           string `json:"floor"`
	Capacity        int    `json:"capacity"`
	HasTV           bool   `json:"has_tv"`
	IsZoomRoom      bool   `json:"is_zoom_room"`

	// Beacon identity (UUID=org/building, Major=floor/zone, Minor=room).
	// Set from local config, not from Zoom.
	BeaconUUID  string `json:"beacon_uuid,omitempty"`
	BeaconMajor int    `json:"beacon_major,omitempty"`
	BeaconMinor int    `json:"beacon_minor,omitempty"`
}

// Beacon is the iBeacon identity assigned to a room (local config, not Zoom).
// Per the concept: UUID = org/building, Major = floor/zone, Minor = room.
type Beacon struct {
	WorkspaceID string `json:"workspace_id"`
	UUID        string `json:"uuid"`
	Major       int    `json:"major"`
	Minor       int    `json:"minor"`
}

// CheckInStatus mirrors Zoom's reservation check-in state.
type CheckInStatus string

const (
	NotCheckedIn CheckInStatus = "not_checked_in"
	CheckedIn    CheckInStatus = "checked_in"
	CheckedOut   CheckInStatus = "checked_out"
)

// ReservationStatus is QuickRoom's view of a booking's lifecycle.
type ReservationStatus string

const (
	StatusBooked    ReservationStatus = "booked"
	StatusNoShow    ReservationStatus = "no_show"
	StatusReleased  ReservationStatus = "released"
	StatusCancelled ReservationStatus = "cancelled" // user-initiated, distinct from an auto-reclaimed no-show
)

// Reservation is a Zoom workspace reservation joined to a local room, OR a
// QuickRoom-native booking created from the mobile app (Source == "app") —
// Zoom has never heard of the latter, so callers must check Source before
// driving any Zoom API call for a reservation.
type Reservation struct {
	ReservationID   string            `json:"reservation_id"`
	RoomID          string            `json:"room_id"`
	ZoomWorkspaceID string            `json:"zoom_workspace_id"`
	UserID          string            `json:"user_id"`
	// No omitempty: the mobile app decodes user_email as non-optional, and
	// one missing key fails its whole reservations fetch (it goes empty
	// when the booking user's account is deleted).
	UserEmail       string            `json:"user_email"`
	StartTime       time.Time         `json:"start_time"`
	EndTime         time.Time         `json:"end_time"`
	Status          ReservationStatus `json:"status"`
	CheckInStatus   CheckInStatus     `json:"check_in_status"`

	// Source is "zoom" (mirrored from Zoom's reservation API) or "app"
	// (created via POST /reservations by a signed-in mobile user).
	Source         string `json:"source"`
	BookedByUserID string `json:"booked_by_user_id,omitempty"`
}

// User is an app account, established via Sign in with Apple.
type User struct {
	UserID    string    `json:"user_id"`
	AppleSub  string    `json:"-"` // Apple's stable per-app identifier; never serialized to clients
	// email and name must always be present in JSON (no omitempty): the
	// mobile app decodes them as non-optional and errors out if a key is
	// missing — which happens on re-sign-in after an account delete, when
	// Apple no longer sends the name.
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}
