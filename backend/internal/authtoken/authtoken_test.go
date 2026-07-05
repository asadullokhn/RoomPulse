package authtoken

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMintVerifyRoundtrip(t *testing.T) {
	s := NewSigner([]byte("test-secret"))
	tok, err := s.Mint("usr_1", RoleUser, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sub, role, err := s.Verify(tok)
	if err != nil || sub != "usr_1" || role != RoleUser {
		t.Fatalf("verify = %q %q %v", sub, role, err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	s := NewSigner([]byte("k"))
	tok, _ := s.Mint("usr_1", RoleUser, -time.Minute)
	if _, _, err := s.Verify(tok); err == nil {
		t.Fatal("expected expiry error")
	}
}

func TestVerifyRejectsWrongKeyAndGarbage(t *testing.T) {
	tok, _ := NewSigner([]byte("k1")).Mint("usr_1", RoleAdmin, time.Hour)
	if _, _, err := NewSigner([]byte("k2")).Verify(tok); err == nil {
		t.Fatal("expected signature error")
	}
	if _, _, err := NewSigner([]byte("k1")).Verify("garbage"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadOrCreateSecret(t *testing.T) {
	if got, _ := LoadOrCreateSecret("env-secret", ""); string(got) != "env-secret" {
		t.Fatalf("env should win, got %q", got)
	}
	path := filepath.Join(t.TempDir(), "jwt_secret")
	first, err := LoadOrCreateSecret("", path)
	if err != nil || len(first) != 32 {
		t.Fatalf("create = %v len %d", err, len(first))
	}
	second, _ := LoadOrCreateSecret("", path)
	if string(first) != string(second) {
		t.Fatal("secret must be stable across loads")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", fi.Mode().Perm())
	}
}
