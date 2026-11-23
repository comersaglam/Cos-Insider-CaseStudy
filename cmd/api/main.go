// Command api serves the notification management HTTP API.
//
// Usage:
//
//	api            serve the API (the default)
//	api serve      the same
//	api migrate    apply the database migrations, then exit
//	api healthcheck  ask the API serving on HTTP_ADDR whether it is ready; exit 0 if it is, 1 otherwise. The
//	               container's health check runs it, the image having no shell or curl (ADR 0027).
//
// It is the composition root of the API process: it reads the configuration, builds the platform clients and the
// modules, mounts them through httpapi and serves them until SIGINT or SIGTERM, then shuts down gracefully.
//
// Configuration, from the environment:
//
//	DATABASE_URL       PostgreSQL URL; pool settings such as pool_max_conns may be added to it (required)
//	HTTP_ADDR          address to listen on (default :8080)
//	LOG_LEVEL          debug, info, warn or error (default info)
//	SHUTDOWN_TIMEOUT   how long requests in flight may take to finish when stopping (default 15s)
//	BATCH_MAX_BODY_BYTES  the largest batch request body, in bytes (default 10485760, 10 MiB)
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/health"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/httpapi"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/config"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/postgres"
	"github.com/comersaglam/Cos-Insider-CaseStudy/migrations"
)

// defaultAddr is where the API listens unless HTTP_ADDR says otherwise.
const defaultAddr = ":8080"

// defaultMaxBatchBody fits a full batch of the longest SMS or push messages (about 4 MB) or of e-mails averaging
// 10 KB, while bounding the memory one request can take; larger e-mails go in smaller batches (ADR 0017).
const defaultMaxBatchBody = 10 << 20

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.LookupEnv, os.Stdout)
	stop()
	if err != nil {
		// The logger may not exist yet (the configuration it depends on may be what failed), so plain stderr.
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

// run is the whole program, with its inputs passed in so that tests can drive it: it returns when ctx is
// cancelled and the server has shut down, or when anything fails.
func run(ctx context.Context, args []string, lookup func(string) (string, bool), stdout io.Writer) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	if command != "serve" && command != "migrate" && command != "healthcheck" {
		return fmt.Errorf("unknown command %q; use serve, migrate or healthcheck", command)
	}

	cfg := config.NewReader(lookup)
	if command == "healthcheck" {
		addr := cfg.String("HTTP_ADDR", defaultAddr)
		if err := cfg.Err(); err != nil {
			return fmt.Errorf("invalid configuration:\n%w", err)
		}
		return httpx.Probe(ctx, addr, "/readyz")
	}
	databaseURL := cfg.Secret("DATABASE_URL")
	addr := cfg.String("HTTP_ADDR", defaultAddr)
	level := cfg.LogLevel("LOG_LEVEL", slog.LevelInfo)
	shutdownTimeout := cfg.Duration("SHUTDOWN_TIMEOUT", 15*time.Second)
	maxBatchBody := cfg.Int("BATCH_MAX_BODY_BYTES", defaultMaxBatchBody)
	if err := cfg.Err(); err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
	log := observability.NewLogger(stdout, level)
	log.InfoContext(ctx, "configuration loaded", "command", command, "config", cfg)

	pool, err := postgres.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if command == "migrate" {
		if err := postgres.Migrate(pool, migrations.FS); err != nil {
			return err
		}
		log.InfoContext(ctx, "migrations applied")
		return nil
	}

	notifications := notification.NewHandler(notification.NewService(notification.NewStore(pool), log), log, int64(maxBatchBody))
	probes := health.NewHandler(map[string]health.Check{"postgres": pool.Ping}, log)
	srv := httpx.NewServer(httpapi.NewRouter(log, observability.NewRegistry(), probes, notifications), log)
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	log.InfoContext(ctx, "serving HTTP", "addr", ln.Addr().String())

	// Serve returns once the server has stopped; the deferred pool.Close then runs, after the last request.
	if err := httpx.Serve(ctx, srv, ln, shutdownTimeout); err != nil {
		return err
	}
	log.InfoContext(ctx, "stopped")
	return nil
}
