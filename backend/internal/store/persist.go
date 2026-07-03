package store

import (
	"encoding/json"
	"os"

	"quickroom/internal/domain"
)

// orgUUID is the shared building/organization iBeacon UUID (Major=floor, Minor=room).
const orgUUID = "11111111-2222-3333-4444-555555555555"

// DefaultBeacons is the built-in registry, one beacon per floor-plan room
// (Major = floor/zone, Minor = room).
func DefaultBeacons() []domain.Beacon {
	return []domain.Beacon{
		{WorkspaceID: "ws-nusadua", UUID: orgUUID, Major: 1, Minor: 101},
		{WorkspaceID: "ws-petang", UUID: orgUUID, Major: 1, Minor: 102},
		{WorkspaceID: "ws-bedugul", UUID: orgUUID, Major: 1, Minor: 103},
		{WorkspaceID: "ws-mengwi", UUID: orgUUID, Major: 1, Minor: 104},
		{WorkspaceID: "ws-sanur", UUID: orgUUID, Major: 1, Minor: 105},
		{WorkspaceID: "ws-agung", UUID: orgUUID, Major: 1, Minor: 106},
		{WorkspaceID: "ws-ubud", UUID: orgUUID, Major: 1, Minor: 107},
		{WorkspaceID: "ws-lembongan", UUID: orgUUID, Major: 1, Minor: 108},
		{WorkspaceID: "ws-ceningan", UUID: orgUUID, Major: 1, Minor: 109},
		{WorkspaceID: "ws-penida", UUID: orgUUID, Major: 1, Minor: 110},
	}
}

// LoadBeacons reads the beacon registry from a JSON file. Returns nil (no error)
// if the file is absent, so the caller falls back to DefaultBeacons.
func LoadBeacons(path string) ([]domain.Beacon, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var bs []domain.Beacon
	if err := json.Unmarshal(b, &bs); err != nil {
		return nil, err
	}
	return bs, nil
}

// SaveBeacons persists the beacon registry to a JSON file (admin edits survive restart).
func SaveBeacons(path string, bs []domain.Beacon) error {
	out, err := json.MarshalIndent(bs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}
