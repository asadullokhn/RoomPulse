package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func testKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestHostForEnv(t *testing.T) {
	if got := HostForEnv("production"); got != "api.push.apple.com" {
		t.Fatalf("production host = %q", got)
	}
	if got := HostForEnv("sandbox"); got != "api.sandbox.push.apple.com" {
		t.Fatalf("sandbox host = %q", got)
	}
	if got := HostForEnv(""); got != "api.sandbox.push.apple.com" {
		t.Fatalf("default host = %q", got)
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New([]byte("not a key"), "KEY1", "TEAM1", "com.example.app", "h"); err == nil {
		t.Fatal("expected error for garbage key")
	}
}

func TestPushSendsWellFormedRequest(t *testing.T) {
	var gotPath, gotAuth, gotTopic, gotPushType string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotTopic = r.Header.Get("apns-topic")
		gotPushType = r.Header.Get("apns-push-type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c, err := New(testKeyPEM(t), "KEY1", "TEAM1", "com.example.app", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()

	err = c.Push(context.Background(), "devtok123", Notification{
		Title: "Are you coming?", Body: "Room frees in 3 min",
		Type: "grace_reminder", WorkspaceID: "ws-x", ReservationID: "res-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != "/3/device/devtok123" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotTopic != "com.example.app" || gotPushType != "alert" {
		t.Fatalf("headers topic=%q pushType=%q", gotTopic, gotPushType)
	}

	raw := strings.TrimPrefix(gotAuth, "bearer ")
	parsed, _, err := jwt.NewParser().ParseUnverified(raw, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("auth header not a JWT: %v (auth=%q)", err, gotAuth)
	}
	if parsed.Header["kid"] != "KEY1" || parsed.Header["alg"] != "ES256" {
		t.Fatalf("jwt header = %v", parsed.Header)
	}
	if iss, _ := parsed.Claims.(jwt.MapClaims)["iss"].(string); iss != "TEAM1" {
		t.Fatalf("iss = %q", iss)
	}

	aps := gotBody["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	if alert["title"] != "Are you coming?" || aps["sound"] != "default" {
		t.Fatalf("payload aps = %v", aps)
	}
	if gotBody["type"] != "grace_reminder" || gotBody["workspace_id"] != "ws-x" {
		t.Fatalf("custom keys = %v", gotBody)
	}
}

func TestPushReusesProviderToken(t *testing.T) {
	var tokens []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	c, _ := New(testKeyPEM(t), "K", "T", "topic", "h")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()
	_ = c.Push(context.Background(), "a", Notification{Title: "x"})
	_ = c.Push(context.Background(), "b", Notification{Title: "y"})
	if len(tokens) != 2 || tokens[0] != tokens[1] {
		t.Fatalf("expected cached provider token reuse, got %v", tokens)
	}
}

func TestPushUnregistered(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"reason":"Unregistered"}`))
	}))
	defer ts.Close()
	c, _ := New(testKeyPEM(t), "K", "T", "topic", "h")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()
	err := c.Push(context.Background(), "dead", Notification{Title: "x"})
	if !errors.Is(err, ErrUnregistered) {
		t.Fatalf("expected ErrUnregistered, got %v", err)
	}
}

func TestPushOtherErrorCarriesReason(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"reason":"InvalidProviderToken"}`))
	}))
	defer ts.Close()
	c, _ := New(testKeyPEM(t), "K", "T", "topic", "h")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()
	err := c.Push(context.Background(), "tok", Notification{Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "InvalidProviderToken") {
		t.Fatalf("expected reason in error, got %v", err)
	}
}
