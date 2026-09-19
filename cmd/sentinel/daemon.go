package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/karamble/omarchy-site-sentinel/alerts"
	"github.com/karamble/omarchy-site-sentinel/api"
	"github.com/karamble/omarchy-site-sentinel/monitor"
	"github.com/karamble/omarchy-site-sentinel/sites"
	filestore "github.com/karamble/omarchy-site-sentinel/store"
)

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8098", "listen address; loopback by design")
	storePath := fs.String("store", "", "path to sites.json (default ~/.config/sitesentinel/sites.json)")
	debug := fs.Bool("debug", false, "log every request")
	if err := fs.Parse(args); err != nil {
		return err
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	path := *storePath
	if path == "" {
		path = sites.DefaultPath()
	}

	store, err := sites.Load(path)
	if errors.Is(err, sites.ErrNotConfigured) {
		// A first run has no store; write an empty one.
		store = &sites.Store{}
		store.SetPath(path)
		if err := store.Save(); err != nil {
			return fmt.Errorf("creating %s: %w", path, err)
		}
		logger.Info("created an empty site store", "path", path)
	} else if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dir := filepath.Dir(path)

	// Clear temporary files left by a write that was interrupted, including the
	// names the previous CreateTemp-based writers used. Without this they
	// accumulate: five were sitting here before the store package landed.
	if d, err := filestore.Shared(dir); err == nil {
		if n, err := d.Sweep(".state-", ".sites-", ".triggers-"); err == nil && n > 0 {
			logger.Info("swept interrupted writes", "files", n, "dir", dir)
		}
	}

	srv := api.NewServer(store, nil, logger, version)
	mon := monitor.New(srv.Store, filepath.Join(dir, "state.json"), logger)
	srv.SetMonitor(mon)

	// Both files have just been read, so this is the moment to reconcile them.
	// State for a site the store no longer lists is an orphan: it cannot be
	// probed, it cannot be removed, and until Snapshot started hiding it, it
	// was reported as a paused site stuck at whatever it last was. Quiet when
	// there is nothing to drop.
	keep := make(map[string]bool, len(store.Sites))
	for _, site := range store.Sites {
		keep[site.ID] = true
	}
	if dropped := mon.Prune(keep); dropped > 0 {
		logger.Info("dropped state for sites no longer watched", "sites", dropped)
	}

	// Triggers are stored separately from sites.
	triggers, err := alerts.Load(filepath.Join(dir, "triggers.json"))
	if err != nil {
		return fmt.Errorf("loading triggers: %w", err)
	}
	engine := alerts.NewEngine(
		triggers,
		srv.Snapshot,
		func() bool { return srv.Store().MonitoringOn() },
		alerts.NewDeliverer(alerts.Desktop),
		logger,
	)
	srv.SetEngine(engine)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", *addr, err)
	}
	logger.Info("sentinel daemon listening",
		"addr", ln.Addr().String(), "version", version, "sites", len(store.Sites), "config", dir)

	// Deadlines on every phase, not just the header: a local client holding a
	// connection open, or trickling a body, must not pin the daemon.
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go mon.Run(ctx)
	go engine.Run(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
	}()

	if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// Exit zero: the service entry point must not respawn a stop that was asked
	// for.
	logger.Info("sentinel daemon stopped cleanly")
	return nil
}
