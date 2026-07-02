// Package config loads runtime config from env vars. Prototype uses stdlib
// os.Getenv; production should switch to kelseyhightower/envconfig per Go rules.
package config

import (
	"fmt"
	"os"
	"strconv"
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
	// ever present is auto-released. Grace = GraceFraction of the booking length
	// (Reno's proportional model), clamped to [GraceMin, GraceMax]. So a 2h
	// booking at 10% = 12m; a 15m booking clamps up to GraceMin.
	GraceFraction float64
	GraceMin      time.Duration
	GraceMax      time.Duration

	// Grace-reminder ladder (Reno's model): "are you coming?" pings fire at these
	// fractions of the booking elapsed, before the no-show release at
	// GraceFraction. The second ping can be disabled to limit notification fatigue.
	NotifyFirstFraction  float64
	NotifySecondFraction float64
	NotifySecondEnabled  bool
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
		DBPath:           getenv("DB_PATH", "/data/roompulse.db"),
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

	frac, err := strconv.ParseFloat(getenv("GRACE_FRACTION", "0.10"), 64)
	if err != nil || frac <= 0 || frac >= 1 {
		return Config{}, fmt.Errorf("invalid GRACE_FRACTION (want 0<f<1): %q", getenv("GRACE_FRACTION", "0.10"))
	}
	c.GraceFraction = frac

	if c.GraceMin, err = time.ParseDuration(getenv("GRACE_MIN", "90s")); err != nil {
		return Config{}, fmt.Errorf("invalid GRACE_MIN: %w", err)
	}
	if c.GraceMax, err = time.ParseDuration(getenv("GRACE_MAX", "15m")); err != nil {
		return Config{}, fmt.Errorf("invalid GRACE_MAX: %w", err)
	}

	if c.NotifyFirstFraction, err = strconv.ParseFloat(getenv("NOTIFY_FIRST_FRACTION", "0.05"), 64); err != nil {
		return Config{}, fmt.Errorf("invalid NOTIFY_FIRST_FRACTION: %w", err)
	}
	if c.NotifySecondFraction, err = strconv.ParseFloat(getenv("NOTIFY_SECOND_FRACTION", "0.075"), 64); err != nil {
		return Config{}, fmt.Errorf("invalid NOTIFY_SECOND_FRACTION: %w", err)
	}
	c.NotifySecondEnabled = getenv("NOTIFY_SECOND_ENABLED", "true") != "false"

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
