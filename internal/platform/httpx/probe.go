package httpx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// probeTimeout bounds a probe; a container's health check gives up on it.
const probeTimeout = 3 * time.Second

// Probe asks the server listening on addr (a listen address such as ":8080") for path on the local machine, and
// returns nil when it answers 200. The binaries' healthcheck command uses it: the distroless image has no curl or
// shell for a Compose health check to run.
func Probe(ctx context.Context, addr, path string) error {
	url, err := localURL(addr, path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build probe: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe %s: %s", url, resp.Status)
	}
	return nil
}

// localURL turns a listen address into a URL on the loopback interface, where the server can always be reached
// from its own container.
func localURL(addr, path string) (string, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("read the listen address %q: %w", addr, err)
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port) + path, nil
}
