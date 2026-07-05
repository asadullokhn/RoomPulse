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

func TestUsersListAndDelete(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().Truncate(time.Second)
	db.UpsertUser(domain.User{UserID: "usr_a", AppleSub: "sub-a", Email: "a@example.com", Name: "A", CreatedAt: now})
	db.UpsertUser(domain.User{UserID: "usr_b", AppleSub: "sub-b", Email: "b@example.com", Name: "B", CreatedAt: now})

	users, err := db.Users()
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("Users() returned %d, want 2", len(users))
	}

	if err := db.CreateSession("hash-a", "usr_a", now, now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.DeleteSessionsForUser("usr_a"); err != nil {
		t.Fatalf("DeleteSessionsForUser: %v", err)
	}
	if _, ok, err := db.SessionUserID("hash-a", now); err != nil || ok {
		t.Fatalf("session should be gone after DeleteSessionsForUser: ok=%v err=%v", ok, err)
	}

	if err := db.DeleteUser("usr_a"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, ok, err := db.UserByID("usr_a"); err != nil || ok {
		t.Fatalf("user should be gone after DeleteUser: ok=%v err=%v", ok, err)
	}
	users, _ = db.Users()
	if len(users) != 1 {
		t.Fatalf("Users() after delete returned %d, want 1", len(users))
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

func TestAPNSTokens(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	if err := db.UpsertUser(domain.User{UserID: "u-1", AppleSub: "sub-1", Email: "a@b.c", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertUser(domain.User{UserID: "u-2", AppleSub: "sub-2", Email: "x@y.z", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	if err := db.SaveAPNSToken("tok1", "u-1", now); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAPNSToken("tok2", "u-1", now); err != nil {
		t.Fatal(err)
	}

	got, err := db.APNSTokensForUser("u-1")
	if err != nil || len(got) != 2 {
		t.Fatalf("tokens for u-1 = %v err=%v", got, err)
	}

	// Same device signs into another account: token re-homes.
	if err := db.SaveAPNSToken("tok1", "u-2", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, _ = db.APNSTokensForUser("u-1")
	if len(got) != 1 || got[0] != "tok2" {
		t.Fatalf("after re-home, u-1 tokens = %v", got)
	}

	all, err := db.AllAPNSTokens()
	if err != nil || len(all) != 2 {
		t.Fatalf("all tokens = %v err=%v", all, err)
	}

	if err := db.DeleteAPNSToken("tok1"); err != nil {
		t.Fatal(err)
	}
	all, _ = db.AllAPNSTokens()
	if len(all) != 1 {
		t.Fatalf("after delete, all = %v", all)
	}
}

func TestUserByEmail(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(domain.User{UserID: "u-1", AppleSub: "sub-1", Email: "a@b.c", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	u, ok, err := db.UserByEmail("a@b.c")
	if err != nil || !ok || u.UserID != "u-1" {
		t.Fatalf("UserByEmail = %+v ok=%v err=%v", u, ok, err)
	}
	if _, ok, _ = db.UserByEmail("missing@x.y"); ok {
		t.Fatal("expected miss for unknown email")
	}
}
