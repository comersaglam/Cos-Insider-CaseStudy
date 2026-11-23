package notification

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/coder/websocket"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
)

// updatesServer serves the handlers, with hub, on a server built as the API builds its own, so its timeouts apply.
func updatesServer(t *testing.T, hub *Hub) string {
	t.Helper()
	mux := http.NewServeMux()
	log := slog.New(slog.DiscardHandler)
	NewHandler(&fakeService{}, hub, log, testBatchLimit).RegisterRoutes(mux)
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httpx.NewServer(mux, log)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "ws://" + ln.Addr().String()
}

// dial opens a WebSocket to url; the handshake's response body is closed, as the linter asks of every response.
func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.Dial(t.Context(), url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// waitSubscribers waits until hub has n subscribers.
func waitSubscribers(t *testing.T, hub *Hub, n int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		hub.mu.Lock()
		got := len(hub.subs)
		hub.mu.Unlock()
		if got == n {
			return
		}
	}
	t.Fatalf("the hub did not reach %d subscribers", n)
}

func readEvent(t *testing.T, conn *websocket.Conn) StatusEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var e StatusEvent
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("not an event: %s", data)
	}
	return e
}

func TestStatusUpdatesStreamTheSubscribedEvents(t *testing.T) {
	t.Parallel()
	hub := NewHub(slog.New(slog.DiscardHandler))
	base := updatesServer(t, hub)
	id, batch := uuid.NewV7(), uuid.NewV7()
	conn := dial(t, base+"/v1/status-updates?notificationId="+id.String()+"&batchId="+batch.String())
	defer func() { _ = conn.CloseNow() }()
	waitSubscribers(t, hub, 1)

	hub.Publish(event(uuid.NewV7(), nil, StatusSent)) // neither its ID nor its batch
	hub.Publish(event(id, nil, StatusSending))
	hub.Publish(event(uuid.NewV7(), &batch, StatusSent))
	if e := readEvent(t, conn); e.NotificationID != id || e.Status != StatusSending {
		t.Errorf("first event = %+v", e)
	}
	if e := readEvent(t, conn); e.BatchID == nil || *e.BatchID != batch {
		t.Errorf("second event = %+v", e)
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
	waitSubscribers(t, hub, 0)
}

// TestStatusUpdatesOutliveTheServersTimeouts: a connection outlives the server's read and write timeouts (15 s),
// which net/http clears when the connection is taken over. Slow on purpose; it runs in parallel.
func TestStatusUpdatesOutliveTheServersTimeouts(t *testing.T) {
	t.Parallel()
	hub := NewHub(slog.New(slog.DiscardHandler))
	base := updatesServer(t, hub)
	id := uuid.NewV7()
	conn := dial(t, base+"/v1/status-updates?notificationId="+id.String())
	defer func() { _ = conn.CloseNow() }()
	waitSubscribers(t, hub, 1)

	time.Sleep(17 * time.Second)
	hub.Publish(event(id, nil, StatusSent))
	if e := readEvent(t, conn); e.Status != StatusSent {
		t.Errorf("event after 17 s = %+v", e)
	}
}

func TestStatusUpdatesCloseWhenTheServerStops(t *testing.T) {
	t.Parallel()
	hub := NewHub(slog.New(slog.DiscardHandler))
	base := updatesServer(t, hub)
	conn := dial(t, base+"/v1/status-updates?notificationId="+uuid.NewV7().String())
	defer func() { _ = conn.CloseNow() }()
	waitSubscribers(t, hub, 1)

	hub.stop()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, _, err := conn.Read(ctx); websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Errorf("read after the stop = %v, want close status going away", err)
	}
}

// TestStatusUpdatesRefuseBadRequests: the query, the upgrade and the origin are checked before the connection is
// taken over, and refused with the usual problems.
func TestStatusUpdatesRefuseBadRequests(t *testing.T) {
	upgrade := map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Version": "13", "Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ=="}
	tooMany := "/v1/status-updates?" + strings.Repeat("notificationId="+uuid.NewV7().String()+"&", maxSubscribedNotifications+1)
	tests := []struct {
		name, path string
		header     map[string]string
		status     int
		code       string
	}{
		{"nothing to follow", "/v1/status-updates", upgrade, 400, "invalid_query"},
		{"an ID that is not one", "/v1/status-updates?notificationId=42", upgrade, 400, "invalid_query"},
		{"two batches", "/v1/status-updates?batchId=" + uuid.NewV7().String() + "&batchId=" + uuid.NewV7().String(), upgrade, 400, "invalid_query"},
		{"too many notifications", tooMany, upgrade, 400, "invalid_query"},
		{"another parameter", "/v1/status-updates?batchId=" + uuid.NewV7().String() + "&status=sent", upgrade, 400, "invalid_query"},
		{"not a WebSocket request", "/v1/status-updates?batchId=" + uuid.NewV7().String(), nil, 426, "upgrade_required"},
		{"from another site", "/v1/status-updates?batchId=" + uuid.NewV7().String(),
			merge(upgrade, map[string]string{"Origin": "https://evil.example"}), 403, "origin_not_allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, &fakeService{}, http.MethodGet, tt.path, "", tt.header)
			if b := body(t, rec); rec.Code != tt.status || b["code"] != tt.code {
				t.Errorf("got %d %v, want %d %s", rec.Code, b["code"], tt.status, tt.code)
			}
		})
	}
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
