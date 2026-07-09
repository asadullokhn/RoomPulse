// Package config loads runtime config from env vars. Prototype uses stdlib
// os.Getenv; production should switch to kelseyhightower/envconfig per Go rules.
package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	HTTPAddr     string
	ZoomMode     string // "mock" (default), "live" (S2S admin), or "user" (user OAuth)
	SyncInterval time.Duration

	// Zoom OAuth credentials. "live" uses Account ID (S2S); "user" uses
	// ClientID/Secret + redirect URI (authorization code flow).
	ZoomAccountID    string
	ZoomClientID     string
	ZoomClientSecret string
	ZoomLocationID   string

	// User-OAuth ("user" mode) settings.
	ZoomRedirectURI string
	ZoomTokenFile   string

	// Mock-mode seed file (mirror your real rooms). Optional.
	ZoomSeedFile string

	// PresenceTTL: a device not seen within this window is reaped (checked out).
	// Backstop for a killed/offline phone that never sent a leave.
	PresenceTTL time.Duration

	// BeaconsFile persists the room↔iBeacon registry (admin edits survive restart).
	BeaconsFile string

	// DBPath is the SQLite file backing the durable device registry. Defaults
	// under /data so it persists in the container volume; override for local runs.
	DBPath string

	// No-show grace: a booking whose start passes by this window with nobody
	// ever present is auto-released. Production model: one fixed window for
	// every booking length; bookers rated below 50 get half of it.
	GraceWindow time.Duration

	// Grace-reminder ladder: "are you coming?" fires NotifyFirstAfter into
	// the booking; the optional last call fires NotifyLastCallBefore ahead of
	// the release. The second ping can be disabled to limit notification fatigue.
	NotifyFirstAfter     time.Duration
	NotifyLastCallBefore time.Duration
	NotifySecondEnabled  bool

	// OverstayGrace: a room still occupied this long past its booking's end is
	// flagged as an overstay (the inverse of a no-show).
	OverstayGrace time.Duration

	// Sign in with Apple: AppleBundleID is checked against the identity
	// token's aud claim. SessionTTL controls how long an issued session
	// (opaque bearer token) stays valid.
	AppleBundleID string
	SessionTTL    time.Duration

	// APNs push delivery (all of KeyFile/KeyID/TeamID/Topic required to
	// enable; see internal/apns). Env selects the sandbox vs production host.
	APNSKeyFile string
	APNSKeyID   string
	APNSTeamID  string
	APNSTopic   string
	APNSEnv     string

	// JWT auth. JWTSecret empty = generate and persist next to the DB.
	// Admin creds seed the admins table only when it's empty; defaults are
	// dev placeholders — set real values in the deployment .env.
	JWTSecret     string
	AdminEmail    string
	AdminPassword string
}

func Load() (Config, error) {
	c := Config{
		HTTPAddr:         getenv("HTTP_ADDR", ":8080"),
		ZoomMode:         getenv("ZOOM_MODE", "mock"),
		ZoomAccountID:    os.Getenv("ZOOM_ACCOUNT_ID"),
		ZoomClientID:     os.Getenv("ZOOM_CLIENT_ID"),
		ZoomClientSecret: os.Getenv("ZOOM_CLIENT_SECRET"),
		ZoomLocationID:   os.Getenv("ZOOM_LOCATION_ID"),
		ZoomRedirectURI:  getenv("ZOOM_REDIRECT_URI", "http://localhost:8080/oauth/callback"),
		ZoomTokenFile:    getenv("ZOOM_TOKEN_FILE", "zoom_token.json"),
		ZoomSeedFile:     getenv("ZOOM_SEED_FILE", "seed.json"),
		BeaconsFile:      getenv("BEACONS_FILE", "/data/beacons.json"),
		DBPath:           getenv("DB_PATH", "/data/quickroom.db"),
	}

	interval, err := time.ParseDuration(getenv("SYNC_INTERVAL", "60s"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid SYNC_INTERVAL: %w", err)
	}
	c.SyncInterval = interval

	ttl, err := time.ParseDuration(getenv("PRESENCE_TTL", "90s"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid PRESENCE_TTL: %w", err)
	}
	c.PresenceTTL = ttl

	if c.GraceWindow, err = time.ParseDuration(getenv("GRACE_WINDOW", "12m")); err != nil {
		return Config{}, fmt.Errorf("invalid GRACE_WINDOW: %w", err)
	}

	if c.NotifyFirstAfter, err = time.ParseDuration(getenv("NOTIFY_FIRST_AFTER", "2m")); err != nil {
		return Config{}, fmt.Errorf("invalid NOTIFY_FIRST_AFTER: %w", err)
	}
	if c.NotifyLastCallBefore, err = time.ParseDuration(getenv("NOTIFY_LAST_CALL_BEFORE", "2m")); err != nil {
		return Config{}, fmt.Errorf("invalid NOTIFY_LAST_CALL_BEFORE: %w", err)
	}
	c.NotifySecondEnabled = getenv("NOTIFY_SECOND_ENABLED", "true") != "false"

	if c.OverstayGrace, err = time.ParseDuration(getenv("OVERSTAY_GRACE", "5m")); err != nil {
		return Config{}, fmt.Errorf("invalid OVERSTAY_GRACE: %w", err)
	}

	c.AppleBundleID = os.Getenv("APPLE_BUNDLE_ID")

	c.APNSKeyFile = os.Getenv("APNS_KEY_FILE")
	c.APNSKeyID = os.Getenv("APNS_KEY_ID")
	c.APNSTeamID = os.Getenv("APNS_TEAM_ID")
	c.APNSTopic = os.Getenv("APNS_TOPIC")
	c.APNSEnv = getenv("APNS_ENV", "sandbox")

	c.JWTSecret = os.Getenv("JWT_SECRET")
	c.AdminEmail = getenv("ADMIN_EMAIL", "admin@example.com")
	c.AdminPassword = getenv("ADMIN_PASSWORD", "SuperAdmin123!")

	if c.SessionTTL, err = time.ParseDuration(getenv("SESSION_TTL", "720h")); err != nil { // 30 days
		return Config{}, fmt.Errorf("invalid SESSION_TTL: %w", err)
	}

	switch c.ZoomMode {
	case "live":
		if c.ZoomAccountID == "" || c.ZoomClientID == "" || c.ZoomClientSecret == "" {
			return Config{}, fmt.Errorf("live mode requires ZOOM_ACCOUNT_ID, ZOOM_CLIENT_ID, ZOOM_CLIENT_SECRET")
		}
	case "user":
		if c.ZoomClientID == "" || c.ZoomClientSecret == "" {
			return Config{}, fmt.Errorf("user mode requires ZOOM_CLIENT_ID, ZOOM_CLIENT_SECRET")
		}
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
