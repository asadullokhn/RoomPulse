package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quickroom/internal/authtoken"
)

func adminDo(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	tok := mintAdminToken(t, s)
	rec := httptest.NewRecorder()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestCustomRoomLifecycle(t *testing.T) {
	s := newNoShowServer(t, time.Now())

	rec := adminDo(t, s, "POST", "/rooms", `{"name":"Focus Pod","capacity":2,"has_tv":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body)
	}
	var created struct {
		ZoomWorkspaceID string `json:"zoom_workspace_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || !strings.HasPrefix(created.ZoomWorkspaceID, "cr-") {
		t.Fatalf("created = %s", rec.Body)
	}

	// Appears in the open rooms list.
	if room, ok := s.store.RoomByWorkspace(created.ZoomWorkspaceID); !ok || room.Name != "Focus Pod" {
		t.Fatalf("custom room not in mirror: %+v ok=%v", room, ok)
	}

	// Survives a sync run.
	if _, err := s.sync.Run(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.store.RoomByWorkspace(created.ZoomWorkspaceID); !ok {
		t.Fatal("custom room vanished after sync")
	}

	// PATCH renames it.
	rec = adminDo(t, s, "PATCH", "/rooms/"+created.ZoomWorkspaceID, `{"name":"Quiet Pod"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body)
	}
	if room, _ := s.store.RoomByWorkspace(created.ZoomWorkspaceID); room.Name != "Quiet Pod" {
		t.Fatalf("rename not applied: %+v", room)
	}

	// Book it, then DELETE: room gone, booking cancelled.
	userTok := mintSession(t, s, "u-croom", "croom@x.y")
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/reservations", strings.NewReader(
		`{"workspace_id":"`+created.ZoomWorkspaceID+`","start_time":"2027-03-01T10:00:00Z","end_time":"2027-03-01T11:00:00Z"}`))
	req.Header.Set("Authorization", "Bearer "+userTok)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("book custom room status = %d body=%s", rec.Code, rec.Body)
	}
	var booked struct {
		ReservationID string `json:"reservation_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &booked)

	rec = adminDo(t, s, "DELETE", "/rooms/"+created.ZoomWorkspaceID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body)
	}
	if _, ok := s.store.RoomByWorkspace(created.ZoomWorkspaceID); ok {
		t.Fatal("custom room still in mirror after delete")
	}
	if res, ok := s.store.Reservation(booked.ReservationID); !ok || string(res.Status) != "cancelled" {
		t.Fatalf("booking after room delete = %+v ok=%v, want cancelled", res, ok)
	}
}

func TestZoomRoomOverrideSurvivesSync(t *testing.T) {
	s := newNoShowServer(t, time.Now())

	rec := adminDo(t, s, "PATCH", "/rooms/ws-agung", `{"name":"Agung (Renamed)","capacity":99}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch zoom room status = %d body=%s", rec.Code, rec.Body)
	}
	if room, _ := s.store.RoomByWorkspace("ws-agung"); room.Name != "Agung (Renamed)" || room.Capacity != 99 {
		t.Fatalf("override not applied live: %+v", room)
	}

	// The Zoom sync would normally restore the seed name — the override must win.
	if _, err := s.sync.Run(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if room, _ := s.store.RoomByWorkspace("ws-agung"); room.Name != "Agung (Renamed)" || room.Capacity != 99 {
		t.Fatalf("override lost after sync: %+v", room)
	}

	// DELETE on a zoom room clears the override; the next sync restores Zoom truth.
	rec = adminDo(t, s, "DELETE", "/rooms/ws-agung", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "reset") {
		t.Fatalf("reset status = %d body=%s", rec.Code, rec.Body)
	}
	if _, err := s.sync.Run(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if room, _ := s.store.RoomByWorkspace("ws-agung"); room.Name != "BINB Agung Zoom" {
		t.Fatalf("zoom truth not restored: %+v", room)
	}
}

func TestRoomsCRUDRequiresAdmin(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	userTok, _ := s.signer.Mint("u-nobody", authtoken.RoleUser, time.Hour)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rooms", strings.NewReader(`{"name":"X"}`))
	req.Header.Set("Authorization", "Bearer "+userTok)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user create room status = %d, want 403", rec.Code)
	}
}
