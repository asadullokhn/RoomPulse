package store

import (
	"sort"

	"quickroom/internal/domain"
)

// SeedBeacons replaces the beacon registry (used at startup).
func (m *Memory) SeedBeacons(bs []domain.Beacon) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range bs {
		m.beacons[b.WorkspaceID] = b
	}
}

// SetBeacon assigns/updates the iBeacon identity for a room.
func (m *Memory) SetBeacon(b domain.Beacon) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beacons[b.WorkspaceID] = b
}

// RemoveBeacon deletes the iBeacon mapping for a workspace, if present.
func (m *Memory) RemoveBeacon(workspaceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.beacons, workspaceID)
}

// Beacon returns the beacon for a workspace.
func (m *Memory) Beacon(workspaceID string) (domain.Beacon, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.beacons[workspaceID]
	return b, ok
}

// Beacons returns all registered beacons, sorted by workspace id.
func (m *Memory) Beacons() []domain.Beacon {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]domain.Beacon, 0, len(m.beacons))
	for _, b := range m.beacons {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkspaceID < out[j].WorkspaceID })
	return out
}
