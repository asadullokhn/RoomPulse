package zoom

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

// MockClient is an in-memory Zoom stand-in so the service runs with no
// credentials. It seeds rooms and reservations anchored to "now" so check-in
// windows stay realistic during a demo. Seed data can be supplied from a JSON
// file (mirror your real rooms) or falls back to a built-in default.
type MockClient struct {
	mu           sync.Mutex
	workspaces   []Workspace
	reservations map[string]*Reservation // by ReservationID
	log          *slog.Logger
}

// Seed is the editable shape for mock data. Reservation times are expressed as
// minute offsets from "now" so a demo is always live.
type Seed struct {
	Rooms        []SeedRoom        `json:"rooms"`
	Reservations []SeedReservation `json:"reservations"`
}

type SeedRoom struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Capacity    int    `json:"capacity"`
	HasTV       bool   `json:"has_tv"`
	Location    string `json:"location"`
}

type SeedReservation struct {
	ReservationID  string `json:"reservation_id"`
	WorkspaceID    string `json:"workspace_id"`
	UserEmail      string `json:"user_email"`
	StartOffsetMin int    `json:"start_offset_min"`
	EndOffsetMin   int    `json:"end_offset_min"`
	CheckInStatus  string `json:"check_in_status"`
}

// LoadSeed reads seed data from a JSON file. Returns nil if the file is absent
// (caller then uses the built-in default).
func LoadSeed(path string) (*Seed, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s Seed
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// NewMockClient builds the seeded mock. now is injected so callers control the
// clock. If seed is nil, a built-in default (Room A/B/C) is used.
func NewMockClient(now time.Time, seed *Seed, log *slog.Logger) *MockClient {
	if seed == nil {
		seed = defaultSeed()
	}

	ws := make([]Workspace, 0, len(seed.Rooms))
	for _, r := range seed.Rooms {
		typ := "reservation_only"
		if r.HasTV {
			typ = "ZoomRoom"
		}
		ws = append(ws, Workspace{
			ID: r.WorkspaceID, Name: r.Name, Type: typ, LocationID: r.Location,
			Capacity: r.Capacity, Status: "available", HasTV: r.HasTV,
		})
	}

	res := make(map[string]*Reservation, len(seed.Reservations))
	for _, r := range seed.Reservations {
		status := r.CheckInStatus
		if status == "" {
			status = "not_checked_in"
		}
		res[r.ReservationID] = &Reservation{
			ReservationID: r.ReservationID, WorkspaceID: r.WorkspaceID,
			UserEmail:     r.UserEmail,
			StartTime:     now.Add(time.Duration(r.StartOffsetMin) * time.Minute),
			EndTime:       now.Add(time.Duration(r.EndOffsetMin) * time.Minute),
			CheckInStatus: status,
		}
	}

	return &MockClient{workspaces: ws, reservations: res, log: log}
}

// defaultSeed mirrors the Apple Developer Academy Bali floor: the same 10 rooms
// drawn on the floor plan, so occupancy, beacons and the map all line up.
func defaultSeed() *Seed {
	const loc = "Bali"
	return &Seed{
		Rooms: []SeedRoom{
			{WorkspaceID: "ws-nusadua", Name: "BINB Nusa Dua Zoom", Capacity: 6, HasTV: true, Location: loc},
			{WorkspaceID: "ws-petang", Name: "BINB Petang Zoom", Capacity: 6, HasTV: true, Location: loc},
			{WorkspaceID: "ws-bedugul", Name: "BINB Bedugul Zoom", Capacity: 8, HasTV: true, Location: loc},
			{WorkspaceID: "ws-mengwi", Name: "BINB Mengwi Zoom", Capacity: 6, HasTV: true, Location: loc},
			{WorkspaceID: "ws-sanur", Name: "BINB Sanur Zoom", Capacity: 8, HasTV: true, Location: loc},
			{WorkspaceID: "ws-agung", Name: "BINB Agung Zoom", Capacity: 80, HasTV: true, Location: loc},
			{WorkspaceID: "ws-ubud", Name: "BINB Ubud Zoom", Capacity: 12, HasTV: true, Location: loc},
			{WorkspaceID: "ws-lembongan", Name: "Lembongan", Capacity: 4, HasTV: false, Location: loc},
			{WorkspaceID: "ws-ceningan", Name: "Ceningan", Capacity: 4, HasTV: false, Location: loc},
			{WorkspaceID: "ws-penida", Name: "Penida", Capacity: 4, HasTV: false, Location: loc},
		},
		Reservations: []SeedReservation{
			{ReservationID: "res-agung", WorkspaceID: "ws-agung", UserEmail: "demo.day@adabali.dev", StartOffsetMin: -10, EndOffsetMin: 80, CheckInStatus: "not_checked_in"},
			{ReservationID: "res-ubud", WorkspaceID: "ws-ubud", UserEmail: "mentors@adabali.dev", StartOffsetMin: -5, EndOffsetMin: 55, CheckInStatus: "not_checked_in"},
			{ReservationID: "res-petang", WorkspaceID: "ws-petang", UserEmail: "standup@adabali.dev", StartOffsetMin: -2, EndOffsetMin: 28, CheckInStatus: "not_checked_in"},
			{ReservationID: "res-sanur", WorkspaceID: "ws-sanur", UserEmail: "interviews@adabali.dev", StartOffsetMin: 15, EndOffsetMin: 75, CheckInStatus: "not_checked_in"},
		},
	}
}

func (m *MockClient) ListWorkspaces(_ context.Context, locationID string) ([]Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Workspace, 0, len(m.workspaces))
	for _, w := range m.workspaces {
		if locationID == "" || w.LocationID == locationID {
			out = append(out, w)
		}
	}
	return out, nil
}

func (m *MockClient) ListReservations(_ context.Context, _ string, from, to time.Time) ([]Reservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Reservation, 0, len(m.reservations))
	for _, r := range m.reservations {
		// overlap with [from, to]
		if r.StartTime.Before(to) && r.EndTime.After(from) {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *MockClient) SendEvent(_ context.Context, event EventType, reservationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.reservations[reservationID]
	if !ok {
		return ErrReservationNotFound
	}
	switch event {
	case EventCheckIn:
		r.CheckInStatus = "checked_in"
	case EventCheckOut:
		r.CheckInStatus = "checked_out"
	}
	m.log.Info("mock zoom event applied", "event", event, "reservation_id", reservationID)
	return nil
}
