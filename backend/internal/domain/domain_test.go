package domain

import (
	"encoding/json"
	"testing"
)

// The mobile app decodes these fields as non-optional; a single omitted key
// fails its entire response decode (one email-less reservation from a
// deleted account blanked the whole app). Zero-value structs must still
// serialize every key the app requires.
func TestJSONContractKeysAlwaysPresent(t *testing.T) {
	cases := []struct {
		name string
		v    any
		keys []string
	}{
		{"reservation", Reservation{}, []string{
			"reservation_id", "room_id", "zoom_workspace_id", "user_id",
			"user_email", "start_time", "end_time", "status", "check_in_status", "source",
		}},
		{"room", Room{}, []string{
			"room_id", "zoom_workspace_id", "name", "capacity", "has_tv", "is_zoom_room",
		}},
		{"user", User{}, []string{"user_id", "email", "name"}},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(tc.v)
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.name, err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: unmarshal: %v", tc.name, err)
		}
		for _, k := range tc.keys {
			if _, ok := m[k]; !ok {
				t.Errorf("%s: zero value omits %q — the app decodes it as non-optional", tc.name, k)
			}
		}
	}
}
