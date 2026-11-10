// Command umago runs the Umago analytics server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/philippdormann/umago/server/internal/api"
	"github.com/philippdormann/umago/server/internal/auth"
	"github.com/philippdormann/umago/server/internal/config"
	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/events"
	"github.com/philippdormann/umago/server/internal/favicons"
	"github.com/philippdormann/umago/server/internal/funnels"
	"github.com/philippdormann/umago/server/internal/geoip"
	"github.com/philippdormann/umago/server/internal/ids"
	"github.com/philippdormann/umago/server/internal/journeys"
	"github.com/philippdormann/umago/server/internal/links"
	"github.com/philippdormann/umago/server/internal/polar"
	"github.com/philippdormann/umago/server/internal/rollup"
	"github.com/philippdormann/umago/server/internal/searchconsole"
	"github.com/philippdormann/umago/server/internal/settings"
	"github.com/philippdormann/umago/server/internal/sites"
	"github.com/philippdormann/umago/server/internal/stats"
	"github.com/philippdormann/umago/server/internal/tokens"
	"github.com/philippdormann/umago/server/internal/users"
	"github.com/philippdormann/umago/server/internal/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "umago:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)

	dbName := cfg.DatabasePath
	var db *database.DB
	if cfg.DatabaseURL != "" {
		dbName = "postgres"
		db, err = database.OpenPostgres(cfg.DatabaseURL)
		if err != nil {
			return err
		}
	} else {
		db, err = database.OpenSQLite(cfg.DatabasePath)
		if err != nil {
			return fmt.Errorf("open database %s: %w", cfg.DatabasePath, err)
		}
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	admin := auth.NewAdmin(cfg.AdminUser, cfg.AdminPassword, auth.NewSessionStore(db))
	accounts := users.New(db)
	admin.SetUsers(accounts)
	if err := accounts.Bootstrap(ctx, cfg.AdminUser, cfg.AdminPassword); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if cfg.MCPToken != "" {
		log.Info("mcp.enabled", "endpoint", "/mcp")
	}
	if admin.Enabled() {
		log.Info("auth.enabled", "user", cfg.AdminUser)
	} else {
		log.Warn("auth.disabled", "hint", "set UMAGO_ADMIN_USER and UMAGO_ADMIN_PASSWORD to protect the dashboard")
	}

	writer := events.NewWriter(db, log)
	writer.Start(ctx)
	siteStore := sites.New(db)
	fetcher := favicons.New(db)

	st := settings.New(db)
	google := searchconsole.NewService(searchconsole.NewStore(db), searchconsole.NewClient(cfg.GoogleClientID, cfg.GoogleClientSecret), log)
	if google.Configured() {
		log.Info("google.enabled", "callback", "/api/v1/google/callback")
	}
	polarSvc := polar.NewService(polar.NewStore(db), polar.NewClient(), log)
	polarSvc.Domain = func(ctx context.Context, id string) string {
		st, err := siteStore.Get(ctx, id)
		if err != nil {
			return ""
		}
		return st.Domain
	}
	srv := &api.Server{
		DB: db, Log: log, Sites: siteStore, Settings: st, Writer: writer, Stats: stats.New(db),
		Favicons: fetcher, Admin: admin, Web: web.Handler(), TrustProxy: true, MCPToken: cfg.MCPToken,
		Tokens: tokens.New(db), Links: links.New(db), Funnels: funnels.New(db), Journeys: journeys.New(db),
		GeoIP: geoip.New(db, log),
		Users: accounts, Keys: users.NewKeyStore(db), RetentionDays: cfg.RetentionDays, RetentionFromEnv: cfg.RetentionDaysSet, StartedAt: time.Now(), DatabasePath: cfg.DatabasePath, Google: google, Polar: polarSvc,
		ScriptNames: cfg.ScriptNames,
	}
	srv.GeoIP.FilePath = cfg.GeoIPPath
	srv.GeoIP.Mode, srv.GeoIP.ModeSet = cfg.GeoIPMode, cfg.GeoIPModeSet
	srv.GeoIP.Source, srv.GeoIP.SourceSet = cfg.GeoIPSource, cfg.GeoIPSourceSet
	srv.GeoIP.EnvKey = cfg.MaxMindKey
	srv.GeoIP.EnsureLoaded(context.Background())

	go maintenance(ctx, log, db, siteStore, st, fetcher, cfg)
	go searchConsoleSync(ctx, google)
	go polarSync(ctx, polarSvc)

	httpServer := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	log.Info("server.started", "port", cfg.Port, "database", dbName, "version", api.Version)

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	log.Info("server.stopping")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	stop()
	writer.Wait()
	// Final rollup so nothing written in the last minutes is lost to the UI.
	_ = rollup.Run(context.Background(), db, log, time.Now())
	return nil
}

// maintenance rolls up every minute, prunes raw events hourly and refreshes
// site favicons weekly.
func maintenance(ctx context.Context, log *slog.Logger, db *sqlDB, siteStore *sites.Store, st *settings.Store, fetcher *favicons.Fetcher, cfg config.Config) {
	roll := func() {
		if err := rollup.Run(ctx, db, log, time.Now()); err != nil && ctx.Err() == nil {
			log.Error("rollup.failed", "error", err.Error())
		}
	}
	prune := func() {
		days := cfg.RetentionDays
		if g, err := st.General(ctx, cfg.RetentionDays, cfg.RetentionDaysSet); err == nil {
			days = g.RetentionDays
		}
		n, err := events.Prune(ctx, db, days, time.Now())
		if err != nil && ctx.Err() == nil {
			log.Error("prune.failed", "error", err.Error())
		} else if n > 0 {
			log.Info("prune.completed", "deleted", n)
		}
		_ = auth.NewSessionStore(db).Prune(ctx, time.Now())
		stale, err := siteStore.StaleFavicons(ctx, ids.Format(time.Now().AddDate(0, 0, -7)))
		if err != nil {
			return
		}
		for _, st := range stale {
			fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			data, ctype, err := fetcher.ForDomain(fctx, st.Domain)
			cancel()
			if err != nil {
				_ = siteStore.SetFavicon(ctx, st.ID, nil, "")
				continue
			}
			_ = siteStore.SetFavicon(ctx, st.ID, data, ctype)
		}
	}
	roll()
	prune()
	rt := time.NewTicker(time.Minute)
	pt := time.NewTicker(time.Hour)
	defer rt.Stop()
	defer pt.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-rt.C:
			roll()
		case <-pt.C:
			prune()
		}
	}
}

type sqlDB = database.DB

// polarSync reconciles orders for every connected site once a day, so
// refunds and missed webhooks are picked up.
func polarSync(ctx context.Context, svc *polar.Service) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	svc.SyncStale(ctx, 20*time.Hour)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			svc.SyncStale(ctx, 20*time.Hour)
		}
	}
}

// searchConsoleSync pulls search terms for every connected site once a day.
// Google's data trails by a few days, so the cadence is checked hourly but
// each site is refreshed only when its last pull is over 20 hours old.
func searchConsoleSync(ctx context.Context, google *searchconsole.Service) {
	if !google.Configured() {
		return
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	google.SyncStale(ctx, 20*time.Hour)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			google.SyncStale(ctx, 20*time.Hour)
		}
	}
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
