// Command quickroom is the QuickRoom backend prototype: it syncs Zoom Workspace
// reservations into a local mirror and serves them over HTTP.
//
// Runs in "mock" mode by default (no Zoom credentials needed). Set ZOOM_MODE=live
// plus the ZOOM_* env vars to talk to the real Zoom Workspace Reservation API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"quickroom/internal/api"
	"quickroom/internal/apns"
	"quickroom/internal/appleauth"
	"quickroom/internal/authtoken"
	"quickroom/internal/config"
	syncsvc "quickroom/internal/sync"
	"quickroom/internal/store"
	"quickroom/internal/zoom"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	zc := buildZoomClient(cfg, log)
	st := store.NewMemory()
	db, err := store.OpenDB(cfg.DBPath)
	if err != nil {
		log.Error("open database", "path", cfg.DBPath, "err", err)
		os.Exit(1)
	}
	defer db.Close()
	log.Info("sqlite ready", "path", cfg.DBPath)

	// Seed the room<->iBeacon registry (from BeaconsFile if present, else the
	// built-in defaults) so GET /beacons can serve the app immediately.
	beacons, err := store.LoadBeacons(cfg.BeaconsFile)
	if err != nil {
		log.Warn("load beacons; using built-in defaults", "file", cfg.BeaconsFile, "err", err)
	}
	if len(beacons) == 0 {
		beacons = store.DefaultBeacons()
	}
	st.SeedBeacons(beacons)
	log.Info("beacon registry seeded", "count", len(beacons))

	// Reload persisted app-native bookings (Zoom sync below only covers
	// zoom-sourced reservations; app bookings have no external source to
	// recover from, so they must be reloaded from SQLite here).
	appRes, err := db.AppReservations()
	if err != nil {
		log.Warn("load app reservations", "err", err)
	}
	for _, r := range appRes {
		st.UpsertReservation(r)
	}
	log.Info("app reservations reloaded", "count", len(appRes))

	sync := syncsvc.New(zc, st, db, cfg.ZoomLocationID, log)

	// Initial sync so the API has data immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if _, err := sync.Run(ctx, time.Now()); err != nil {
		log.Warn("initial sync failed", "err", err)
	}
	cancel()

	// Background periodic sync.
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go runSyncLoop(rootCtx, sync, cfg.SyncInterval, log)

	// JWT signer: env secret wins, else a random one persisted next to the DB
	// so restarts don't invalidate every issued token.
	secret, err := authtoken.LoadOrCreateSecret(cfg.JWTSecret, filepath.Join(filepath.Dir(cfg.DBPath), "jwt_secret"))
	if err != nil {
		log.Error("jwt secret", "err", err)
		os.Exit(1)
	}
	signer := authtoken.NewSigner(secret)

	// Seed the first admin account (no-op when one exists already).
	if hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost); err != nil {
		log.Error("hash admin password", "err", err)
		os.Exit(1)
	} else if err := db.EnsureAdmin(cfg.AdminEmail, string(hash), time.Now()); err != nil {
		log.Error("seed admin", "err", err)
		os.Exit(1)
	}

	appleVerifier := appleauth.NewVerifier(cfg.AppleBundleID, nil)
	apiSrv := api.NewServer(st, db, sync, zc, cfg.ZoomMode, cfg.PresenceTTL, appleVerifier, cfg.SessionTTL, signer, log)
	apiSrv.ConfigureGrace(cfg.GraceWindow)
	apiSrv.ConfigureNotify(cfg.NotifyFirstAfter)
	apiSrv.ConfigureOverstay(cfg.OverstayGrace)
	apiSrv.ConfigureBeaconsFile(cfg.BeaconsFile)
	if cfg.APNSKeyFile != "" && cfg.APNSKeyID != "" && cfg.APNSTeamID != "" && cfg.APNSTopic != "" {
		keyPEM, err := os.ReadFile(cfg.APNSKeyFile)
		if err != nil {
			log.Error("apns disabled: read key", "err", err)
		} else if pushClient, err := apns.New(keyPEM, cfg.APNSKeyID, cfg.APNSTeamID, cfg.APNSTopic, apns.HostForEnv(cfg.APNSEnv)); err != nil {
			log.Error("apns disabled: bad key", "err", err)
		} else {
			apiSrv.ConfigureAPNS(pushClient)
			log.Info("apns push enabled", "env", cfg.APNSEnv)
		}
	} else {
		log.Info("apns push disabled (APNS_* not configured)")
	}
	go apiSrv.ReapLoop(rootCtx)  // expire stale presence (killed/offline phones)
	go apiSrv.GraceLoop(rootCtx) // grace reminders + no-show release

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr, "zoom_mode", cfg.ZoomMode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			stop()
		}
	}()

	<-rootCtx.Done()
	log.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

func buildZoomClient(cfg config.Config, log *slog.Logger) zoom.Client {
	switch cfg.ZoomMode {
	case "live":
		log.Info("using live Zoom client (S2S admin)")
		return zoom.NewHTTPClient(zoom.HTTPConfig{
			AccountID:    cfg.ZoomAccountID,
			ClientID:     cfg.ZoomClientID,
			ClientSecret: cfg.ZoomClientSecret,
		}, nil, log)
	case "user":
		log.Info("using user-OAuth Zoom client", "login_at", "/oauth/login")
		return zoom.NewUserClient(zoom.UserConfig{
			ClientID:     cfg.ZoomClientID,
			ClientSecret: cfg.ZoomClientSecret,
			RedirectURI:  cfg.ZoomRedirectURI,
			TokenFile:    cfg.ZoomTokenFile,
		}, nil, log)
	default:
		seed, err := zoom.LoadSeed(cfg.ZoomSeedFile)
		if err != nil {
			log.Warn("could not load seed file; using built-in default", "file", cfg.ZoomSeedFile, "err", err)
		} else if seed != nil {
			log.Info("loaded mock seed", "file", cfg.ZoomSeedFile, "rooms", len(seed.Rooms), "reservations", len(seed.Reservations))
		}
		log.Info("using mock Zoom client")
		return zoom.NewMockClient(time.Now(), seed, log)
	}
}

func runSyncLoop(ctx context.Context, sync *syncsvc.Service, interval time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c, cancel := context.WithTimeout(ctx, 10*time.Second)
			if _, err := sync.Run(c, time.Now()); err != nil {
				log.Warn("periodic sync failed", "err", err)
			}
			cancel()
		}
	}
}
