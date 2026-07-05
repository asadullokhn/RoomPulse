package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"quickroom/internal/apns"
	"quickroom/internal/domain"
)

type fakePusher struct {
	mu    sync.Mutex
	calls []string // device tokens pushed to
	fail  map[string]error
}

func (f *fakePusher) Push(_ context.Context, tok string, _ apns.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tok)
	if f.fail != nil {
		return f.fail[tok]
	}
	return nil
}

func (f *fakePusher) tokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// waitFor polls briefly — pushes run on goroutines.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

func mustUser(t *testing.T, s *Server, userID, email string) {
	t.Helper()
	if err := s.db.UpsertUser(domain.User{UserID: userID, AppleSub: "sub-" + userID, Email: email, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestEmitPushesToRecipientTokens(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	mustUser(t, s, "u-1", "booker@x.y")
	_ = s.db.SaveAPNSToken("tokA", "u-1", time.Now())
	_ = s.db.SaveAPNSToken("tokB", "u-1", time.Now())

	fp := &fakePusher{}
	s.ConfigureAPNS(fp)

	// Recipient by email (bookerOf prefers email).
	s.notify.emit("k1", Notification{Type: "grace_reminder", Recipient: "booker@x.y", Title: "t", Body: "b"})
	waitFor(t, func() bool { return len(fp.tokens()) == 2 })

	// Dedup hit: same key emits nothing new, no extra pushes.
	s.notify.emit("k1", Notification{Type: "grace_reminder", Recipient: "booker@x.y", Title: "t", Body: "b"})
	time.Sleep(50 * time.Millisecond)
	if got := fp.tokens(); len(got) != 2 {
		t.Fatalf("dedup should not re-push, calls = %v", got)
	}
}

func TestEmitResolvesRecipientByUserID(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	mustUser(t, s, "u-9", "")
	_ = s.db.SaveAPNSToken("tokC", "u-9", time.Now())
	fp := &fakePusher{}
	s.ConfigureAPNS(fp)

	s.notify.emit("k", Notification{Type: "grace_reminder", Recipient: "u-9", Title: "t"})
	waitFor(t, func() bool { return len(fp.tokens()) == 1 })
}

func TestEmitBroadcastPushesToAllTokens(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	mustUser(t, s, "u-1", "a@x.y")
	mustUser(t, s, "u-2", "b@x.y")
	_ = s.db.SaveAPNSToken("tokA", "u-1", time.Now())
	_ = s.db.SaveAPNSToken("tokB", "u-2", time.Now())
	fp := &fakePusher{}
	s.ConfigureAPNS(fp)

	s.notify.emit("", Notification{Type: "room_freed", Recipient: "", Title: "t", Body: "b"})
	waitFor(t, func() bool { return len(fp.tokens()) == 2 })
}

func TestEmitUnknownRecipientPushesNothing(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	mustUser(t, s, "u-1", "a@x.y")
	_ = s.db.SaveAPNSToken("tokA", "u-1", time.Now())
	fp := &fakePusher{}
	s.ConfigureAPNS(fp)

	// Zoom-sourced booker with no app account: silently dropped.
	s.notify.emit("k", Notification{Type: "grace_reminder", Recipient: "stranger@zoom.co", Title: "t"})
	time.Sleep(100 * time.Millisecond)
	if got := fp.tokens(); len(got) != 0 {
		t.Fatalf("expected no pushes, got %v", got)
	}
}

func TestEmitPrunesUnregisteredTokens(t *testing.T) {
	s := newNoShowServer(t, time.Now())
	mustUser(t, s, "u-1", "a@x.y")
	_ = s.db.SaveAPNSToken("dead", "u-1", time.Now())
	fp := &fakePusher{fail: map[string]error{"dead": apns.ErrUnregistered}}
	s.ConfigureAPNS(fp)

	s.notify.emit("k", Notification{Recipient: "a@x.y", Title: "t"})
	waitFor(t, func() bool {
		left, _ := s.db.AllAPNSTokens()
		return len(left) == 0
	})
}
