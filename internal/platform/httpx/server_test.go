package httpx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

var discard = slog.New(slog.DiscardHandler)

// listen opens a listener on a free local port.
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

// TestServeFinishesARequestInFlightWhenStopped cancels the context while a request is being handled: the server
// stops accepting connections at once, but the request still gets its answer, and Serve returns nil.
func TestServeFinishesARequestInFlightWhenStopped(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	srv := NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "done")
	}), discard)
	ln := listen(t)
	addr := ln.Addr().String()
	ctx, stop := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, srv, ln, 5*time.Second) }()

	answer := make(chan string, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			answer <- "error: " + err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }() // a read-only body: closing it cannot lose data
		body, _ := io.ReadAll(resp.Body)
		answer <- string(body)
	}()
	<-entered
	stop()

	// Shutdown closes the listener first; wait until new connections are refused, then let the request finish.
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(context.Background(), "tcp", addr)
		if err != nil {
			break
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("the server still accepts connections after its context was cancelled")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)

	if got := <-answer; got != "done" {
		t.Errorf("request in flight got %q, want done", got)
	}
	if err := <-served; err != nil {
		t.Errorf("Serve = %v, want nil after a clean shutdown", err)
	}
}

func TestServeGivesUpOnARequestThatOutlastsTheShutdownTimeout(t *testing.T) {
	entered := make(chan struct{})
	srv := NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done() // never finishes on its own
	}), discard)
	ln := listen(t)
	ctx, stop := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, srv, ln, 50*time.Millisecond) }()

	go func() {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String(), nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered
	stop()

	if err := <-served; !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Serve = %v, want the shutdown deadline exceeded", err)
	}
}

func TestServeReportsAListenerThatFails(t *testing.T) {
	ln := listen(t)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Serve(t.Context(), NewServer(http.NotFoundHandler(), discard), ln, time.Second); err == nil {
		t.Error("Serve = nil on a closed listener, want an error")
	}
}

func TestNewServerSetsTimeouts(t *testing.T) {
	srv := NewServer(http.NotFoundHandler(), discard)
	if srv.ReadHeaderTimeout == 0 || srv.ReadTimeout == 0 || srv.WriteTimeout == 0 || srv.IdleTimeout == 0 {
		t.Errorf("a timeout is unset: %+v", srv)
	}
}
