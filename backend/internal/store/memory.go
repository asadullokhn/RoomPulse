// Package store is the prototype's in-memory persistence. Production target is
// PostgreSQL (pgx + sqlc per Go rules); this keeps the sync prototype runnable
// with no DB.
package store

import (
	"sort"
	"sync"
	"time"

	"roompulse/internal/domain"
)

// Memory holds rooms, reservations, and live presence keyed by ID.
type Memory struct {
	mu             sync.RWMutex
	rooms          map[string]domain.Room
	reservations   map[string]domain.Reservation
	presence       map[string]map[string]struct{} // workspaceID -> set of present userIDs
	lastPresenceTS map[string]int64               // "workspaceID|userID" -> last applied event_ts (ms)
	displayNames   map[string]string              // userID -> display label
	deviceRoom     map[string]string              // deviceID -> current workspaceID ("" = none)
	deviceTS       map[string]int64               // deviceID -> last heartbeat ts (ms)
	deviceSeenAt   map[string]time.Time           // deviceID -> server receipt time (for TTL)
}

func NewMemory() *Memory {
	return &Memory{
		rooms:          make(map[string]domain.Room),
		reservations:   make(map[string]domain.Reservation),
		presence:       make(map[string]map[string]struct{}),
		lastPresenceTS: make(map[string]int64),
		displayNames:   make(map[string]string),
		deviceRoom:     make(map[string]string),
		deviceTS:       make(map[string]int64),
		deviceSeenAt:   make(map[string]time.Time),
	}
}

// ReapStale removes devices not seen within maxAge (by SERVER receipt time, so
// client clock skew can't matter). Returns the workspace ids that lost an
// occupant, so the caller can reflect check-out on those reservations. This is
// the backstop for a killed/offline phone that never sent a leave.
func (m *Memory) ReapStale(maxAge time.Duration) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	vacated := map[string]struct{}{}
	for dev, seen := range m.deviceSeenAt {
		if seen.After(cutoff) {
			continue
		}
		if room := m.deviceRoom[dev]; room != "" {
			if set := m.presence[room]; set != nil {
				delete(set, dev)
			}
			vacated[room] = struct{}{}
		}
		delete(m.deviceRoom, dev)
		delete(m.deviceSeenAt, dev)
		delete(m.deviceTS, dev)
	}
	out := make([]string, 0, len(vacated))
	for ws := range vacated {
		out = append(out, ws)
	}
	return out
}

// SetDeviceRoom reconciles a device's current room from a heartbeat (idempotent
// full state, not a delta). workspaceID "" means the device is in no room.
// Returns whether the room changed and the previous workspaceID. Stale
// heartbeats (older ts) are ignored.
func (m *Memory) SetDeviceRoom(deviceID, workspaceID, displayName string, ts int64) (changed bool, prevRoom string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ts > 0 && ts < m.deviceTS[deviceID] {
		return false, m.deviceRoom[deviceID] // stale
	}
	m.deviceTS[deviceID] = ts
	m.deviceSeenAt[deviceID] = time.Now() // server-side TTL clock
	if displayName != "" {
		m.displayNames[deviceID] = displayName
	}
	prev := m.deviceRoom[deviceID]
	if prev == workspaceID {
		return false, prev // no change
	}
	if prev != "" {
		if set := m.presence[prev]; set != nil {
			delete(set, deviceID)
		}
	}
	m.deviceRoom[deviceID] = workspaceID
	if workspaceID != "" {
		if m.presence[workspaceID] == nil {
			m.presence[workspaceID] = make(map[string]struct{})
		}
		m.presence[workspaceID][deviceID] = struct{}{}
	}
	return true, prev
}

// ApplyPresenceIfNewer updates presence only if ts is at least as new as the
// last applied event for this (workspace, user). Returns false for stale events
// — this makes out-of-order phone POSTs non-corrupting (last-event-wins).
func (m *Memory) ApplyPresenceIfNewer(workspaceID, userID, displayName string, ts int64, entered bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := workspaceID + "|" + userID
	if ts > 0 && ts < m.lastPresenceTS[key] {
		return false // stale: a newer event already applied
	}
	m.lastPresenceTS[key] = ts
	if m.presence[workspaceID] == nil {
		m.presence[workspaceID] = make(map[string]struct{})
	}
	if entered {
		m.presence[workspaceID][userID] = struct{}{}
		if displayName == "" {
			displayName = userID
		}
		m.displayNames[userID] = displayName
	} else {
		delete(m.presence[workspaceID], userID)
	}
	return true
}

// AllOccupancy returns the present users per workspace.
func (m *Memory) AllOccupancy() map[string][]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]string, len(m.presence))
	for ws, set := range m.presence {
		users := make([]string, 0, len(set))
		for id := range set {
			name := m.displayNames[id]
			if name == "" {
				name = id
			}
			users = append(users, name)
		}
		sort.Strings(users)
		out[ws] = users
	}
	return out
}

// UpsertRoom inserts or updates a room, preserving any existing beacon mapping
// (Zoom never supplies beacon fields, so a sync must not wipe them).
func (m *Memory) UpsertRoom(r domain.Room) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.rooms[r.RoomID]; ok {
		if r.BeaconUUID == "" {
			r.BeaconUUID = existing.BeaconUUID
			r.BeaconMajor = existing.BeaconMajor
			r.BeaconMinor = existing.BeaconMinor
		}
	}
	m.rooms[r.RoomID] = r
}

// UpsertReservation inserts or updates a reservation.
func (m *Memory) UpsertReservation(r domain.Reservation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reservations[r.ReservationID] = r
}

// Rooms returns all rooms sorted by name.
func (m *Memory) Rooms() []domain.Room {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RoomByWorkspace finds the local room mapped to a Zoom workspace id.
func (m *Memory) RoomByWorkspace(workspaceID string) (domain.Room, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.rooms {
		if r.ZoomWorkspaceID == workspaceID {
			return r, true
		}
	}
	return domain.Room{}, false
}

// Reservations returns all reservations sorted by start time.
func (m *Memory) Reservations() []domain.Reservation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Reservation, 0, len(m.reservations))
	for _, r := range m.reservations {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartTime.Before(out[j].StartTime) })
	return out
}

// Reservation returns one reservation by id.
func (m *Memory) Reservation(id string) (domain.Reservation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.reservations[id]
	return r, ok
}

// ReservationByWorkspace returns the earliest-starting reservation for a
// workspace. Presence events carry a workspace, not a reservation id.
func (m *Memory) ReservationByWorkspace(workspaceID string) (domain.Reservation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best domain.Reservation
	found := false
	for _, r := range m.reservations {
		if r.ZoomWorkspaceID != workspaceID {
			continue
		}
		if !found || r.StartTime.Before(best.StartTime) {
			best = r
			found = true
		}
	}
	return best, found
}
