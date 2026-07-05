// Package apns pushes alert notifications to Apple Push Notification service
// using token-based (p8 / ES256) provider authentication. Kept intentionally
// small: one POST per device token, provider JWT cached ~50 minutes (Apple
// asks for a refresh every 20-60 min).
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrUnregistered means APNs reports the device token as dead (uninstalled
// app or invalidated token); the caller should delete it.
var ErrUnregistered = errors.New("apns: device token unregistered")

// Notification is one alert push. Type/WorkspaceID/ReservationID ride along
// as custom payload keys so the app can deep-link later.
type Notification struct {
	Title         string
	Body          string
	Type          string
	WorkspaceID   string
	ReservationID string
}

// HostForEnv maps the APNS_ENV config to Apple's host. Development-signed
// builds produce sandbox tokens, so sandbox is the default.
func HostForEnv(env string) string {
	if env == "production" {
		return "api.push.apple.com"
	}
	return "api.sandbox.push.apple.com"
}

type Client struct {
	// BaseURL and HTTPClient are exported for tests (httptest injection).
	BaseURL    string
	HTTPClient *http.Client

	topic  string
	keyID  string
	teamID string
	key    *ecdsa.PrivateKey

	mu       sync.Mutex
	jwt      string
	jwtUntil time.Time
}

// New parses the .p8 provider key and returns a ready client.
func New(keyPEM []byte, keyID, teamID, topic, host string) (*Client, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("apns: key file is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: parse key: %w", err)
	}
	ecKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns: key is not an EC key")
	}
	return &Client{
		BaseURL:    "https://" + host,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		topic:      topic,
		keyID:      keyID,
		teamID:     teamID,
		key:        ecKey,
	}, nil
}

// providerToken returns the cached ES256 provider JWT, minting a fresh one
// when it is older than ~50 minutes.
func (c *Client) providerToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.jwt != "" && now.Before(c.jwtUntil) {
		return c.jwt, nil
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": c.teamID,
		"iat": now.Unix(),
	})
	tok.Header["kid"] = c.keyID
	signed, err := tok.SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("apns: sign provider token: %w", err)
	}
	c.jwt = signed
	c.jwtUntil = now.Add(50 * time.Minute)
	return signed, nil
}

// Push sends one alert to one device token.
func (c *Client) Push(ctx context.Context, deviceToken string, n Notification) error {
	provider, err := c.providerToken()
	if err != nil {
		return err
	}

	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]any{"title": n.Title, "body": n.Body},
			"sound": "default",
		},
	}
	if n.Type != "" {
		payload["type"] = n.Type
	}
	if n.WorkspaceID != "" {
		payload["workspace_id"] = n.WorkspaceID
	}
	if n.ReservationID != "" {
		payload["reservation_id"] = n.ReservationID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/3/device/"+deviceToken, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "bearer "+provider)
	req.Header.Set("apns-topic", c.topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("apns: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	var apnsErr struct {
		Reason string `json:"reason"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = json.Unmarshal(raw, &apnsErr)
	if resp.StatusCode == http.StatusGone || apnsErr.Reason == "BadDeviceToken" {
		return fmt.Errorf("%w (reason %s)", ErrUnregistered, apnsErr.Reason)
	}
	return fmt.Errorf("apns: status %d reason %s", resp.StatusCode, apnsErr.Reason)
}
