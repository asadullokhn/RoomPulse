package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPutBeaconCreatesAndUpdates(t *testing.T) {
	h := newTestHandler(t)
	tok := adminToken(t, h)

	// ws-agung is one of the mock seed's built-in rooms.
	body, _ := json.Marshal(map[string]any{"uuid": "11111111-2222-3333-4444-555555555555", "major": 1, "minor": 999})
	req := httptest.NewRequest(http.MethodPut, "/beacons/ws-agung", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	var created struct {
		WorkspaceID string `json:"workspace_id"`
		Minor       int    `json:"minor"`
		Name        string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Minor != 999 || created.Name == "" {
		t.Fatalf("PUT response = %s", rec.Body.String())
	}

	// Update: same workspace, different minor.
	body2, _ := json.Marshal(map[string]any{"uuid": "11111111-2222-3333-4444-555555555555", "major": 1, "minor": 888})
	req2 := httptest.NewRequest(http.MethodPut, "/beacons/ws-agung", bytes.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer "+tok)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("PUT (update) status = %d, want 200", rec2.Code)
	}
	var updated struct {
		Minor int `json:"minor"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &updated)
	if updated.Minor != 888 {
		t.Fatalf("PUT (update) minor = %d, want 888", updated.Minor)
	}
}

func TestPutBeaconUnknownWorkspace404s(t *testing.T) {
	h := newTestHandler(t)
	tok := adminToken(t, h)
	body, _ := json.Marshal(map[string]any{"uuid": "11111111-2222-3333-4444-555555555555", "major": 1, "minor": 1})
	req := httptest.NewRequest(http.MethodPut, "/beacons/ws-does-not-exist", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPutBeaconValidation(t *testing.T) {
	h := newTestHandler(t)
	tok := adminToken(t, h)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"empty uuid", map[string]any{"uuid": "", "major": 1, "minor": 1}},
		{"major too large", map[string]any{"uuid": "x", "major": 70000, "minor": 1}},
		{"minor negative", map[string]any{"uuid": "x", "major": 1, "minor": -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, _ := json.Marshal(c.body)
			req := httptest.NewRequest(http.MethodPut, "/beacons/ws-agung", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+tok)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s: status = %d, want 422", c.name, rec.Code)
			}
		})
	}
}

func TestDeleteBeacon(t *testing.T) {
	h := newTestHandler(t)
	tok := adminToken(t, h)

	putBody, _ := json.Marshal(map[string]any{"uuid": "11111111-2222-3333-4444-555555555555", "major": 1, "minor": 1})
	seedReq := httptest.NewRequest(http.MethodPut, "/beacons/ws-agung", bytes.NewReader(putBody))
	seedReq.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(httptest.NewRecorder(), seedReq)

	delReq := httptest.NewRequest(http.MethodDelete, "/beacons/ws-agung", nil)
	delReq.Header.Set("Authorization", "Bearer "+tok)
	delRec := httptest.NewRecorder()
	h.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", delRec.Code)
	}

	// Deleting again (already gone) 404s.
	delReq2 := httptest.NewRequest(http.MethodDelete, "/beacons/ws-agung", nil)
	delReq2.Header.Set("Authorization", "Bearer "+tok)
	delRec2 := httptest.NewRecorder()
	h.ServeHTTP(delRec2, delReq2)
	if delRec2.Code != http.StatusNotFound {
		t.Fatalf("second DELETE status = %d, want 404", delRec2.Code)
	}
}
