package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server timeouts. ReadHeaderTimeout bounds a client that sends its headers slowly to hold connections open
// (Slowloris); the others bound reading a request, writing its answer, and an idle keep-alive connection.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
)

// NewServer returns an HTTP server for h with production timeouts. The server's own errors (a failed TLS
// handshake, a broken connection) go to log at warning level instead of the standard library's plain-text logger.
func NewServer(h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// Serve serves srv on ln until ctx is cancelled, then shuts down gracefully: it stops accepting connections and
// waits up to shutdownTimeout for the requests in flight to finish. It returns nil after a clean shutdown, and an
// error when serving fails or the requests outlast the timeout.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, shutdownTimeout time.Duration) error {
	// Buffered, so the serving goroutine can always deliver its result and end, even after Serve has returned.
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	select {
	case err := <-served:
		// Serve returns only on failure while nobody has asked it to stop.
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	// ctx is already cancelled; the shutdown gets a context of its own that keeps ctx's values.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	return nil
}
