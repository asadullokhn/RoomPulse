package store

import (
	"testing"

	"quickroom/internal/domain"
)

func TestSetAndRemoveBeacon(t *testing.T) {
	m := NewMemory()
	m.SetBeacon(domain.Beacon{WorkspaceID: "ws-1", UUID: "11111111-2222-3333-4444-555555555555", Major: 1, Minor: 101})

	if _, ok := m.Beacon("ws-1"); !ok {
		t.Fatal("beacon not found after SetBeacon")
	}

	m.RemoveBeacon("ws-1")

	if _, ok := m.Beacon("ws-1"); ok {
		t.Fatal("beacon still present after RemoveBeacon")
	}

	// Removing a non-existent beacon is a no-op, not an error.
	m.RemoveBeacon("ws-does-not-exist")
}
