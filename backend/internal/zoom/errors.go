package zoom

import "errors"

var (
	// ErrReservationNotFound is returned when a check-in/out targets an unknown reservation.
	ErrReservationNotFound = errors.New("zoom: reservation not found")
	// ErrUnauthorized indicates the OAuth token was rejected (re-auth needed).
	ErrUnauthorized = errors.New("zoom: unauthorized")
)
