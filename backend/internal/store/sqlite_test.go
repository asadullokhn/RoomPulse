package store

import (
	"path/filepath"
	"testing"
	"time"

	"quickroom/internal/domain"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestUserRoundTrip(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().Truncate(time.Second)
	u := domain.User{UserID: "usr_1", AppleSub: "apple.sub.1", Email: "a@example.com", Name: "Ava", CreatedAt: now}

	if err := db.UpsertUser(u); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	got, ok, err := db.UserByAppleSub("apple.sub.1")
	if err != nil || !ok {
		t.Fatalf("UserByAppleSub: got=%v ok=%v err=%v", got, ok, err)
	}
	if got.UserID != u.UserID || got.Email != u.Email || got.Name != u.Name {
		t.Fatalf("UserByAppleSub = %+v, want %+v", got, u)
	}

	byID, ok, err := db.UserByID("usr_1")
	if err != nil || !ok || byID.AppleSub != "apple.sub.1" {
		t.Fatalf("UserByID: got=%+v ok=%v err=%v", byID, ok, err)
	}

	_, ok, err = db.UserByAppleSub("nonexistent")
	if err != nil || ok {
		t.Fatalf("UserByAppleSub(nonexistent): ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	if err := db.CreateSession("hash-1", "usr_1", now, now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	userID, ok, err := db.SessionUserID("hash-1", now.Add(time.Minute))
	if err != nil || !ok || userID != "usr_1" {
		t.Fatalf("SessionUserID (valid) = %q, %v, %v", userID, ok, err)
	}

	_, ok, err = db.SessionUserID("hash-1", now.Add(2*time.Hour))
	if err != nil || ok {
		t.Fatalf("SessionUserID (expired): ok=%v err=%v, want ok=false", ok, err)
	}

	if err := db.DeleteSession("hash-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	_, ok, err = db.SessionUserID("hash-1", now.Add(time.Minute))
	if err != nil || ok {
		t.Fatalf("SessionUserID (after delete): ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestAppReservationRoundTrip(t *testing.T) {
	db := newTestDB(t)
	r := domain.Reservation{
		ReservationID: "app-1", RoomID: "room-1", ZoomWorkspaceID: "ws-1",
		UserID: "usr_1", UserEmail: "a@example.com",
		StartTime: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		Status:    domain.StatusBooked, CheckInStatus: domain.NotCheckedIn,
		Source: "app", BookedByUserID: "usr_1",
	}
	if err := db.SaveAppReservation(r); err != nil {
		t.Fatalf("SaveAppReservation (create): %v", err)
	}

	r.Status = domain.StatusCancelled
	if err := db.SaveAppReservation(r); err != nil {
		t.Fatalf("SaveAppReservation (update): %v", err)
	}

	all, err := db.AppReservations()
	if err != nil {
		t.Fatalf("AppReservations: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("AppReservations returned %d rows, want 1", len(all))
	}
	got := all[0]
	if got.ReservationID != "app-1" || got.Status != domain.StatusCancelled || got.Source != "app" {
		t.Fatalf("AppReservations[0] = %+v, want status=cancelled source=app", got)
	}
	if !got.StartTime.Equal(r.StartTime) || !got.EndTime.Equal(r.EndTime) {
		t.Fatalf("AppReservations[0] times = %v..%v, want %v..%v", got.StartTime, got.EndTime, r.StartTime, r.EndTime)
	}
}
