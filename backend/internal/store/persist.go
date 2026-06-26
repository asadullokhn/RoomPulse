package store

import (
	"encoding/json"
	"os"

	"roompulse/internal/domain"
)

// orgUUID is the shared building/organization iBeacon UUID (Major=floor, Minor=room).
const orgUUID = "11111111-2222-3333-4444-555555555555"

// DefaultBeacons is the built-in registry, matching the app's room presets.
func DefaultBeacons() []domain.Beacon {
	return []domain.Beacon{
		{WorkspaceID: "ws-a", UUID: orgUUID, Major: 1, Minor: 101},
		{WorkspaceID: "ws-b", UUID: orgUUID, Major: 1, Minor: 102},
		{WorkspaceID: "ws-c", UUID: orgUUID, Major: 2, Minor: 201},
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
