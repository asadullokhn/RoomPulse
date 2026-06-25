// Package zoom is the boundary to the Zoom Workspace Reservation API.
//
// Endpoints modelled (Zoom API v2, base https://api.zoom.us/v2):
//   GET  /workspaces?location_id=...                     list workspaces
//   GET  /workspaces/reservations?location_id=...        list reservations by location
//   POST /workspaces/events                              check in / check out
//
// Auth is Server-to-Server OAuth (POST https://zoom.us/oauth/token,
// grant_type=account_credentials, Basic client_id:client_secret).
//
// NOTE: exact response field names must be verified against the org's Zoom
// developer environment before production — the doc itself flags this. The
// DTOs below are a plausible shape; the mock client returns the same shape so
// the rest of the system is exercised regardless.
package zoom

import (
	"context"
	"time"
)

// Workspace is a reservable Zoom space (room, desk, etc.).
type Workspace struct {
	ID         string
	Name       string
	Type       string // e.g. "ZoomRoom", "reservation_only"
	LocationID string
	Capacity   int
	Status     string
	HasTV      bool
}

// Reservation is a Zoom workspace reservation. WorkspaceName/LocationName are
// best-effort (present on the user-reservations endpoint) so rooms can be
// derived when the workspace-list endpoint is admin-gated.
type Reservation struct {
	ReservationID string
	WorkspaceID   string
	WorkspaceName string
	LocationName  string
	UserID        string
	UserEmail     string
	StartTime     time.Time
	EndTime       time.Time
	CheckInStatus string // "not_checked_in" | "checked_in" | "checked_out"
}

// EventType is a check-in/out action sent to POST /workspaces/events.
type EventType string

const (
	EventCheckIn  EventType = "check_in"
	EventCheckOut EventType = "check_out"
)

// Client is the consumer-defined port to Zoom. Implemented by the live HTTP
// client and by the mock. Keeping it small keeps the mock trivial.
type Client interface {
	// ListWorkspaces returns reservable spaces for a location.
	ListWorkspaces(ctx context.Context, locationID string) ([]Workspace, error)
	// ListReservations returns reservations for a location in [from, to].
	ListReservations(ctx context.Context, locationID string, from, to time.Time) ([]Reservation, error)
	// SendEvent checks a reservation in or out.
	SendEvent(ctx context.Context, event EventType, reservationID string) error
}
