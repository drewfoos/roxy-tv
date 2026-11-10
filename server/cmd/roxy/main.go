// Command roxy is the Roxy Server binary.
//
//	roxy serve            run the HTTP server (default)
//	roxy scan [--force]   scan all configured libraries once and exit (--force: re-read
//	                      every file, not only new and changed ones)
//	roxy probe <file>     print the normalized probe result for one file
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/roxy-media/roxy/server/internal/safefs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/api"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/beacon"
	"github.com/roxy-media/roxy/server/internal/config"
	"github.com/roxy-media/roxy/server/internal/ffmpeg"
	"github.com/roxy-media/roxy/server/internal/library"
	"github.com/roxy-media/roxy/server/internal/metadata"
	"github.com/roxy-media/roxy/server/internal/model"
	"github.com/roxy-media/roxy/server/internal/probe"
	"github.com/roxy-media/roxy/server/internal/segments"
	"github.com/roxy-media/roxy/server/internal/store"
	"github.com/roxy-media/roxy/server/internal/trickplay"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}

	ff, err := ffmpeg.New(cfg.FFmpegPath, cfg.FFprobePath)
	if err != nil {
		return err
	}
	slog.Info("ffmpeg", "ffmpeg", ff.FFmpeg, "ffprobe", ff.FFprobe, "version", ff.Version)

	if cmd == "probe" {
		if len(args) < 2 {
			return fmt.Errorf("usage: roxy probe <file>")
		}
		info, err := probe.File(context.Background(), ff, args[1])
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer db.Close()

	lib := library.New(db, ff, cfg)
	if err := lib.EnsureLibraries(context.Background(), cfg.MediaRoots); err != nil {
		return err
	}
	// Media is read-only to Roxy: writes and deletes are only allowed in the data folder.
	safefs.Init(cfg.DataDir)
	library.GuardMediaRoots(context.Background(), db, cfg.MediaRoots)
	authSvc := auth.New(db)
	if err := bootstrapAdmin(context.Background(), db, cfg); err != nil {
		return err
	}

	switch cmd {
	case "scan":
		return lib.ScanAll(context.Background(), len(args) > 1 && args[1] == "--force")
	case "serve":
		return serve(cfg, db, lib, ff, authSvc)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// bootstrapAdmin creates the first admin user when the users table is empty.
func bootstrapAdmin(ctx context.Context, db *store.Store, cfg *config.Config) error {
	n, err := db.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	// No ROXY_ADMIN_PASSWORD: the first person to open the app on the home network creates
	// the admin in the setup wizard (Jellyfin's way). Never a known default.
	pw := cfg.AdminPassword
	if os.Getenv("ROXY_ADMIN_PASSWORD") == "" {
		slog.Warn("no accounts yet: open the Roxy app on a phone on the same network to set up this server")
		return nil
	}
	if err := auth.ValidateNewPassword(pw); err != nil {
		slog.Warn("ROXY_ADMIN_PASSWORD is weak; change it after signing in", "why", err)
	}
	generated := false
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	u := &model.User{ID: uuid.NewString(), Name: cfg.AdminUser, PasswordHash: hash, IsAdmin: true}
	if err := db.CreateUser(ctx, u); err != nil {
		return err
	}
	if generated {
		slog.Warn("created initial admin user with a random password; sign in and keep it safe (shown only now)", "name", u.Name, "password", pw)
	} else {
		slog.Warn("created initial admin user", "name", u.Name, "password_from_env", true)
	}
	return nil
}

func serve(cfg *config.Config, db *store.Store, lib *library.Library, ff *ffmpeg.FFmpeg, authSvc *auth.Service) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Metadata lookups run off a queue fed by ingest (library must not import metadata).
	meta := metadata.New(db, cfg)
	meta.UseFFmpeg(ff)
	lib.OnItemIngested = meta.Enqueue

	// Seek-preview thumbnails, generated after ingest (and swept for anything missing).
	trick := trickplay.New(db, ff, cfg)
	trick.Start(ctx)
	// Intro/outro detection by audio fingerprint for files without chapter markers; runs
	// per season once its episodes have settled.
	segs := segments.New(db, ff, cfg)
	segs.Start(ctx)
	lib.OnSourceIngested = func(id string) { trick.Enqueue(id); segs.EnqueueSource(id) }
	// A season that just got its MAL id can ask AniSkip. Set before the metadata worker starts.
	meta.OnSeasonMal = segs.EnqueueSeason
	meta.Start(ctx)

	if cfg.ScanOnStart {
		go func() {
			if err := lib.ScanAll(ctx, false); err != nil {
				slog.Error("initial scan failed", "err", err)
			}
			recheckChapterSegments(ctx, lib)
			trick.SweepMissing(ctx)
			segs.SweepAll(ctx)
			sweepCaches(ctx, lib)
		}()
	} else {
		// No scan: still sweep for missing/stale metadata and thumbnails.
		go func() {
			if ids, err := db.TopLevelItemIDs(ctx); err == nil {
				for _, id := range ids {
					meta.Enqueue(id)
				}
			}
			recheckChapterSegments(ctx, lib)
			trick.SweepMissing(ctx)
			segs.SweepAll(ctx)
			sweepCaches(ctx, lib)
		}()
	}

	// Rescan when files change in a library folder (copied in by hand, renamed, removed).
	if cfg.Watch {
		go lib.Watch(ctx)
	}

	// LAN discovery: answer the clients' "who is roxy?" broadcast with our name and port.
	if cfg.DiscoveryPort > 0 {
		httpPort := beacon.HTTPPort(cfg.Listen)
		go beacon.Run(ctx, cfg.DiscoveryPort, func(ctx context.Context) beacon.Info {
			id, _ := db.ServerID(ctx)
			return beacon.Info{ID: id, Name: db.ServerName(ctx), Version: config.Version, Port: httpPort, Address: cfg.AdvertiseURL, Advertised: cfg.AdvertiseURL != ""}
		})
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	slog.Info("roxy server listening", "addr", cfg.Listen, "data", cfg.DataDir)
	return serveHTTP(ctx, ln, api.NewRouter(cfg, db, lib, ff, authSvc, meta, trick, segs))
}

// recheckChapterSegments corrects skip markers an older build took from chapters, once
// (see library.RecheckChapterSegments). It runs after the scan, which has by then dropped
// the files that are gone, and before the detector looks for what the chapters left open.
func recheckChapterSegments(ctx context.Context, lib *library.Library) {
	if _, err := lib.RecheckChapterSegments(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("chapter segment recheck failed", "err", err)
	}
}

// sweepCaches clears the caches of what removed titles and files left there: once now
// (after the first scan, when the library is known) and then daily.
func sweepCaches(ctx context.Context, lib *library.Library) {
	for {
		if _, err := lib.SweepCaches(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("cache sweep failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

// serveHTTP answers HTTP on ln until ctx ends. Requests end with the server: their contexts
// are cancelled with ctx (on SIGTERM), which stops the ffmpeg each stream runs; Shutdown
// alone waits for them (a stream is never idle) and gave up after 5 s, leaving them
// running as the process exited.
func serveHTTP(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		// A slow client cannot hold a request open forever (bodies are capped at 32 MB). No
		// WriteTimeout: streams legitimately run for hours.
		ReadTimeout: 2 * time.Minute,
		IdleTimeout: 2 * time.Minute,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	// Serve returns the moment Shutdown is called, not when it is done: without waiting
	// here the caller closed the database under requests that were still finishing.
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	<-stopped
	return nil
}

// shutdownGrace is how long requests in flight get to finish once the server is told to stop.
var shutdownGrace = 5 * time.Second
