// Command roxy is the Roxy Server binary.
//
//	roxy serve            run the HTTP server (default)
//	roxy scan             scan all configured libraries once and exit
//	roxy probe <file>     print the normalized probe result for one file
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	authSvc := auth.New(db)
	if err := bootstrapAdmin(context.Background(), db, cfg); err != nil {
		return err
	}

	switch cmd {
	case "scan":
		return lib.ScanAll(context.Background())
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
	// No ROXY_ADMIN_PASSWORD: never fall back to a known default on a real server; make a
	// random one and show it once in the log.
	pw := cfg.AdminPassword
	generated := os.Getenv("ROXY_ADMIN_PASSWORD") == ""
	if generated {
		pw = auth.NewToken()[:20]
	}
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
	meta.Start(ctx)
	lib.OnItemIngested = meta.Enqueue

	// Seek-preview thumbnails, generated after ingest (and swept for anything missing).
	trick := trickplay.New(db, ff, cfg)
	trick.Start(ctx)
	// Intro/outro detection by audio fingerprint for files without chapter markers; runs
	// per season once its episodes have settled.
	segs := segments.New(db, ff, cfg)
	segs.Start(ctx)
	lib.OnSourceIngested = func(id string) { trick.Enqueue(id); segs.EnqueueSource(id) }

	if cfg.ScanOnStart {
		go func() {
			if err := lib.ScanAll(ctx); err != nil {
				slog.Error("initial scan failed", "err", err)
			}
			trick.SweepMissing(ctx)
			segs.SweepAll(ctx)
		}()
	} else {
		// No scan: still sweep for missing/stale metadata and thumbnails.
		go func() {
			if ids, err := db.TopLevelItemIDs(ctx); err == nil {
				for _, id := range ids {
					meta.Enqueue(id)
				}
			}
			trick.SweepMissing(ctx)
			segs.SweepAll(ctx)
		}()
	}

	// LAN discovery: answer the clients' "who is roxy?" broadcast with our name and port.
	if cfg.DiscoveryPort > 0 {
		httpPort := beacon.HTTPPort(cfg.Listen)
		go beacon.Run(ctx, cfg.DiscoveryPort, func(ctx context.Context) beacon.Info {
			id, _ := db.ServerID(ctx)
			return beacon.Info{ID: id, Name: db.ServerName(ctx), Version: config.Version, Port: httpPort, Address: cfg.AdvertiseURL, Advertised: cfg.AdvertiseURL != ""}
		})
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.NewRouter(cfg, db, lib, ff, authSvc, meta, trick, segs),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	slog.Info("roxy server listening", "addr", cfg.Listen, "data", cfg.DataDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
