// Package store is the prototype's in-memory persistence. Production target is
// PostgreSQL (pgx + sqlc per Go rules); this keeps the sync prototype runnable
// with no DB.
package store

import (
	"sort"
	"sync"
	"time"

	"quickroom/internal/domain"
)

// Memory holds rooms, reservations, and live presence keyed by ID.
type Memory struct {
	mu             sync.RWMutex
	rooms          map[string]domain.Room
	reservations   map[string]domain.Reservation
	presence       map[string]map[string]struct{} // workspaceID -> set of present userIDs
	lastPresenceTS map[string]int64               // "workspaceID|userID" -> last applied event_ts (ms)
	presenceSeenAt map[string]time.Time           // "workspaceID|userID" -> server receipt time of the enter (for TTL)
	displayNames   map[string]string              // userID -> display label
	deviceRoom     map[string]string              // deviceID -> current workspaceID ("" = none)
	deviceTS       map[string]int64               // deviceID -> last heartbeat ts (ms)
	deviceSeenAt   map[string]time.Time           // deviceID -> server receipt time (for TTL)
	beacons        map[string]domain.Beacon       // workspaceID -> iBeacon identity
}

func NewMemory() *Memory {
	return &Memory{
		rooms:          make(map[string]domain.Room),
		reservations:   make(map[string]domain.Reservation),
		presence:       make(map[string]map[string]struct{}),
		lastPresenceTS: make(map[string]int64),
		presenceSeenAt: make(map[string]time.Time),
		displayNames:   make(map[string]string),
		deviceRoom:     make(map[string]string),
		deviceTS:       make(map[string]int64),
		deviceSeenAt:   make(map[string]time.Time),
		beacons:        make(map[string]domain.Beacon),
	}
}

// ReapedDevice is a device expired by the TTL backstop, with the room it left.
type ReapedDevice struct {
	DeviceID    string
	DisplayName string
	WorkspaceID string
}

// ReapStale removes devices not seen within maxAge (by SERVER receipt time, so
// client clock skew can't matter). Returns the reaped devices that were in a
// room, so the caller can log "left" events and reflect check-out. This is the
// backstop for a killed/offline phone that never sent a leave.
func (m *Memory) ReapStale(maxAge time.Duration) []ReapedDevice {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	out := []ReapedDevice{}
	for dev, seen := range m.deviceSeenAt {
		if seen.After(cutoff) {
			continue
		}
		if room := m.deviceRoom[dev]; room != "" {
			if set := m.presence[room]; set != nil {
				delete(set, dev)
			}
			out = append(out, ReapedDevice{DeviceID: dev, DisplayName: m.displayNames[dev], WorkspaceID: room})
		}
		delete(m.deviceRoom, dev)
		delete(m.deviceSeenAt, dev)
		delete(m.deviceTS, dev)
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
// last applied event for this (workspace, user). Returns applied=false for
// stale events — this makes out-of-order phone POSTs non-corrupting
// (last-event-wins). A person is in one room at a time, but the app's exit
// only fires when the phone leaves the whole beacon region — walking from
// room A to room B just produces a fresh "entered". So an enter MOVES the
// user: they're removed from every other workspace, returned in movedFrom so
// the caller can log the implicit leaves.
func (m *Memory) ApplyPresenceIfNewer(workspaceID, userID, displayName string, ts int64, entered bool) (applied bool, movedFrom []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := workspaceID + "|" + userID
	if ts > 0 && ts < m.lastPresenceTS[key] {
		return false, nil // stale: a newer event already applied
	}
	if entered {
		// Cross-room staleness: presence in another room applied from a newer
		// event means this enter is a late arrival — moving would corrupt.
		for ws, set := range m.presence {
			if ws == workspaceID {
				continue
			}
			if _, in := set[userID]; in && ts > 0 && ts < m.lastPresenceTS[ws+"|"+userID] {
				return false, nil
			}
		}
	}
	m.lastPresenceTS[key] = ts
	if m.presence[workspaceID] == nil {
		m.presence[workspaceID] = make(map[string]struct{})
	}
	if entered {
		for ws, set := range m.presence {
			if ws == workspaceID {
				continue
			}
			if _, in := set[userID]; in {
				delete(set, userID)
				delete(m.presenceSeenAt, ws+"|"+userID)
				movedFrom = append(movedFrom, ws)
			}
		}
		sort.Strings(movedFrom)
		m.presence[workspaceID][userID] = struct{}{}
		m.presenceSeenAt[key] = time.Now() // server-side TTL clock
		if displayName == "" {
			displayName = userID
		}
		m.displayNames[userID] = displayName
	} else {
		delete(m.presence[workspaceID], userID)
		delete(m.presenceSeenAt, key)
	}
	return true, movedFrom
}

// ReapedUser is a user-presence entry expired by the TTL backstop.
type ReapedUser struct {
	UserID      string
	DisplayName string
	WorkspaceID string
}

// ReapStaleUserPresence removes user presence (phone enter/exit events) not
// re-confirmed within maxAge — the backstop for an exit event that never
// arrived (locked phone, dead battery, killed app). The app re-reports its
// enter on every foreground, which refreshes the clock for legit occupants.
func (m *Memory) ReapStaleUserPresence(maxAge time.Duration) []ReapedUser {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	out := []ReapedUser{}
	for ws, set := range m.presence {
		for id := range set {
			key := ws + "|" + id
			seen, tracked := m.presenceSeenAt[key]
			if tracked && seen.After(cutoff) {
				continue
			}
			if !tracked && m.deviceRoom[id] != "" {
				continue // device-heartbeat entry: ReapStale owns it
			}
			delete(set, id)
			delete(m.presenceSeenAt, key)
			out = append(out, ReapedUser{UserID: id, DisplayName: m.displayNames[id], WorkspaceID: ws})
		}
	}
	return out
}

// ClearUserPresence removes a user from every room, returning the rooms they
// were in. The app calls this when it foregrounds outside the beacon region —
// the definitive "I'm nowhere" — which scrubs any ghost left by a lost exit.
func (m *Memory) ClearUserPresence(userID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cleared := []string{}
	for ws, set := range m.presence {
		if _, in := set[userID]; in {
			delete(set, userID)
			delete(m.presenceSeenAt, ws+"|"+userID)
			cleared = append(cleared, ws)
		}
	}
	sort.Strings(cleared)
	return cleared
}

// Ident is one present identity: the raw presence key (a user id or device id)
// plus its display name.
type Ident struct {
	ID   string
	Name string
}

// AllOccupancyIdents returns the present identities per workspace, ids
// included — collision matching needs the id, not just the display name.
func (m *Memory) AllOccupancyIdents() map[string][]Ident {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]Ident, len(m.presence))
	for ws, set := range m.presence {
		idents := make([]Ident, 0, len(set))
		for id := range set {
			name := m.displayNames[id]
			if name == "" {
				name = id
			}
			idents = append(idents, Ident{ID: id, Name: name})
		}
		sort.Slice(idents, func(i, j int) bool { return idents[i].Name < idents[j].Name })
		out[ws] = idents
	}
	return out
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

// DeleteRoom removes a room from the live mirror by workspace id (admin
// deletion of a custom room). Zoom-synced rooms reappear on the next sync,
// so callers only use this for admin-owned rooms.
func (m *Memory) DeleteRoom(workspaceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.rooms {
		if r.ZoomWorkspaceID == workspaceID {
			delete(m.rooms, id)
			return
		}
	}
}

// UpsertReservation inserts or updates a reservation.
func (m *Memory) UpsertReservation(r domain.Reservation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reservations[r.ReservationID] = r
}

// Rooms returns all rooms: Zoom rooms first, then the rest, each sorted by
// name — preserves the established ordering now that names lost their shared
// "BINB" prefix (a plain name sort would interleave the non-Zoom rooms).
func (m *Memory) Rooms() []domain.Room {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsZoomRoom != out[j].IsZoomRoom {
			return out[i].IsZoomRoom
		}
		return out[i].Name < out[j].Name
	})
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

// ReservationOwningWorkspace returns the reservation a presence event should
// drive right now. Presence events carry a workspace, not a reservation id —
// and a workspace accumulates history, so "earliest reservation" once
// checked a phone into a cancelled booking from two days prior. Priority:
// a booked reservation whose window covers now; else a checked-in one that
// ended within the past hour (late leavers checking out); else a booked one
// starting within 15 minutes (early arrivals).
func (m *Memory) ReservationOwningWorkspace(workspaceID string, now time.Time) (domain.Reservation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var current, recent, upcoming domain.Reservation
	var haveCurrent, haveRecent, haveUpcoming bool
	for _, r := range m.reservations {
		if r.ZoomWorkspaceID != workspaceID {
			continue
		}
		switch {
		case r.Status == domain.StatusBooked && !now.Before(r.StartTime) && now.Before(r.EndTime):
			if !haveCurrent || r.StartTime.Before(current.StartTime) {
				current, haveCurrent = r, true
			}
		case r.CheckInStatus == domain.CheckedIn && !now.Before(r.EndTime) && now.Sub(r.EndTime) <= time.Hour:
			if !haveRecent || r.EndTime.After(recent.EndTime) {
				recent, haveRecent = r, true
			}
		case r.Status == domain.StatusBooked && now.Before(r.StartTime) && r.StartTime.Sub(now) <= 15*time.Minute:
			if !haveUpcoming || r.StartTime.Before(upcoming.StartTime) {
				upcoming, haveUpcoming = r, true
			}
		}
	}
	if haveCurrent {
		return current, true
	}
	if haveRecent {
		return recent, true
	}
	if haveUpcoming {
		return upcoming, true
	}
	return domain.Reservation{}, false
}
