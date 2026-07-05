package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestAdminReservationCreateEditConflicts(t *testing.T) {
	s := newNoShowServer(t, time.Now())

	// Create for a real room + email recipient.
	rec := adminDo(t, s, "POST", "/admin/reservations",
		`{"workspace_id":"ws-agung","start_time":"2027-04-01T10:00:00Z","end_time":"2027-04-01T11:00:00Z","user_email":"guest@x.y"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body)
	}
	var created struct {
		ReservationID string `json:"reservation_id"`
		Source        string `json:"source"`
		UserEmail     string `json:"user_email"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Source != "app" || created.UserEmail != "guest@x.y" {
		t.Fatalf("created = %s", rec.Body)
	}

	// Overlapping admin create -> 409.
	rec = adminDo(t, s, "POST", "/admin/reservations",
		`{"workspace_id":"ws-agung","start_time":"2027-04-01T10:30:00Z","end_time":"2027-04-01T11:30:00Z"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d", rec.Code)
	}

	// PATCH moves the window (excluding itself from conflict checks).
	rec = adminDo(t, s, "PATCH", "/admin/reservations/"+created.ReservationID,
		`{"start_time":"2027-04-01T12:00:00Z","end_time":"2027-04-01T13:00:00Z"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body)
	}
	if res, _ := s.store.Reservation(created.ReservationID); res.StartTime.UTC().Hour() != 12 {
		t.Fatalf("window not moved: %+v", res)
	}

	// Zoom-sourced reservation edit -> 403 (res-agung comes from the mock seed).
	rec = adminDo(t, s, "PATCH", "/admin/reservations/res-agung", `{"end_time":"2027-04-01T13:00:00Z"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("zoom edit status = %d body=%s", rec.Code, rec.Body)
	}

	// Unknown id -> 404.
	rec = adminDo(t, s, "PATCH", "/admin/reservations/nope", `{"end_time":"2027-04-01T13:00:00Z"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d", rec.Code)
	}
}

func TestNotificationDeletes(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	s.notify.emit("k1", Notification{Type: "grace_reminder", Title: "a"})
	s.notify.emit("k2", Notification{Type: "room_freed", Title: "b"})
	all := s.notify.recent("", 10)
	if len(all) != 2 {
		t.Fatalf("seeded = %d", len(all))
	}

	rec := adminDo(t, s, "DELETE", "/notifications/"+strconv.FormatInt(all[0].ID, 10), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete one status = %d", rec.Code)
	}
	if rec := adminDo(t, s, "DELETE", "/notifications/99999", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d", rec.Code)
	}
	if rec := adminDo(t, s, "DELETE", "/notifications", ""); rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d", rec.Code)
	}
	if left := s.notify.recent("", 10); len(left) != 0 {
		t.Fatalf("after clear = %d", len(left))
	}
}

func TestPatchUserRename(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	_ = mintSession(t, s, "u-rn", "rn@x.y")

	rec := adminDo(t, s, "PATCH", "/users/u-rn", `{"name":"Renamed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body=%s", rec.Code, rec.Body)
	}
	u, _, _ := s.db.UserByID("u-rn")
	if u.Name != "Renamed" {
		t.Fatalf("name = %q", u.Name)
	}
	if rec := adminDo(t, s, "PATCH", "/users/ghost", `{"name":"X"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("ghost status = %d", rec.Code)
	}
	if rec := adminDo(t, s, "PATCH", "/users/u-rn", `{"name":""}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty name status = %d", rec.Code)
	}
}
