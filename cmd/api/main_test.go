package main

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/postgres"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/testenv"
)

// env returns a lookup function over a fixed set of variables.
func env(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestRunRefusesAnUnknownCommand(t *testing.T) {
	err := run(t.Context(), []string{"serv"}, env(nil), io.Discard)
	if err == nil || !strings.Contains(err.Error(), `"serv"`) {
		t.Errorf("run = %v, want an unknown command error", err)
	}
}

func TestRunReportsMissingConfiguration(t *testing.T) {
	err := run(t.Context(), nil, env(map[string]string{"SHUTDOWN_TIMEOUT": "soon"}), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "SHUTDOWN_TIMEOUT") {
		t.Errorf("run = %v, want both problems reported", err)
	}
}

func TestRunMigrate(t *testing.T) {
	dsn := testenv.PostgresDSN(t)
	pool, err := postgres.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// The helper's database is migrated already; empty it, whatever tables later migrations add, so that migrate
	// has work to do.
	if _, err := pool.Exec(t.Context(), "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}

	if err := run(t.Context(), []string{"migrate"}, env(map[string]string{"DATABASE_URL": dsn}), io.Discard); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass('notifications') IS NOT NULL").Scan(&exists); err != nil || !exists {
		t.Errorf("notifications table after migrate: exists %v, err %v", exists, err)
	}
}

// startAPI runs the API on a free port against a database of the test's own and returns its base URL and the
// database's URL. The API stops when the test ends, and must stop cleanly.
func startAPI(t *testing.T) (base, dsn string) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	logs, logsW := io.Pipe()
	done := make(chan error, 1)
	dsn = testenv.PostgresDSN(t)
	go func() {
		done <- run(ctx, nil, env(map[string]string{
			"DATABASE_URL": dsn,
			"HTTP_ADDR":    "127.0.0.1:0",
		}), logsW)
		_ = logsW.Close()
	}()
	t.Cleanup(func() {
		stop()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("run after stop = %v, want nil", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("the API did not stop within 10 seconds")
		}
	})

	// The log line "serving HTTP" carries the address the listener got.
	addr := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(logs)
		for scanner.Scan() {
			var line map[string]any
			if json.Unmarshal(scanner.Bytes(), &line) == nil && line["msg"] == "serving HTTP" {
				addr <- line["addr"].(string)
			}
		} // keeps reading until the API stops, so logging never blocks
	}()
	select {
	case a := <-addr:
		return "http://" + a, dsn
	case err := <-done:
		t.Fatalf("the API stopped before serving: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the API did not start serving within 30 seconds")
	}
	return "", ""
}

// call sends a request and returns the status, the headers and the decoded JSON body.
func call(t *testing.T, method, url, body string, header map[string]string) (int, http.Header, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var decoded map[string]any
	if err := json.UnmarshalRead(resp.Body, &decoded); err != nil {
		t.Fatalf("%s %s: response is not JSON: %v", method, url, err)
	}
	return resp.StatusCode, resp.Header, decoded
}

// TestAPIEndToEnd drives the running API against a real database: create, read back, repeat with the same
// idempotency key, reuse the key for another request, send an invalid notification, ask for an unknown path.
func TestAPIEndToEnd(t *testing.T) {
	base, _ := startAPI(t)
	const sms = `{"to":"+905551234567","channel":"sms","content":"Your order has shipped."}`
	key := map[string]string{"Idempotency-Key": "order-42-sms", "X-Correlation-ID": "e2e-1"}

	status, header, created := call(t, http.MethodPost, base+"/v1/notifications", sms, key)
	if status != http.StatusAccepted || created["status"] != "queued" || created["priority"] != "normal" {
		t.Fatalf("create = %d %v", status, created)
	}
	if created["correlationId"] != "e2e-1" || header.Get("X-Correlation-ID") != "e2e-1" {
		t.Errorf("correlation ID: body %v, header %q", created["correlationId"], header.Get("X-Correlation-ID"))
	}
	location := header.Get("Location")

	status, _, read := call(t, http.MethodGet, base+location, "", nil)
	if status != http.StatusOK || read["id"] != created["id"] || read["to"] != "+905551234567" {
		t.Errorf("get %s = %d %v", location, status, read)
	}

	status, header, again := call(t, http.MethodPost, base+"/v1/notifications", sms, key)
	if status != http.StatusAccepted || again["id"] != created["id"] || header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("repeat = %d %v, replayed %q; want the first notification", status, again["id"], header.Get("Idempotent-Replayed"))
	}

	other := strings.Replace(sms, "shipped", "delayed", 1)
	if status, _, p := call(t, http.MethodPost, base+"/v1/notifications", other, key); status != 422 || p["code"] != "idempotency_key_reused" {
		t.Errorf("reused key = %d %v", status, p["code"])
	}

	invalid := `{"to":"05551234567","channel":"sms","content":""}`
	status, _, p := call(t, http.MethodPost, base+"/v1/notifications", invalid, nil)
	if status != 422 || p["code"] != "validation_failed" {
		t.Errorf("invalid = %d %v", status, p)
	}
	if errs, _ := p["errors"].([]any); len(errs) != 2 {
		t.Errorf("invalid: errors = %v, want two (to and content)", p["errors"])
	}

	if status, _, p := call(t, http.MethodGet, base+"/v1/nothing", "", nil); status != 404 || p["code"] != "not_found" {
		t.Errorf("unknown path = %d %v", status, p)
	}
}

// TestAPIBatchEndToEnd sends a batch with one invalid item among valid ones: the valid ones are accepted and can
// be read, the invalid one is reported, and the batch counts its notifications by status.
func TestAPIBatchEndToEnd(t *testing.T) {
	base, _ := startAPI(t)
	const batch = `{"notifications":[
		{"to":"+905551234567","channel":"sms","content":"one"},
		{"to":"not-a-number","channel":"sms","content":"two"},
		{"to":"ayse@example.com","channel":"email","content":"three"}]}`

	status, header, created := call(t, http.MethodPost, base+"/v1/batches", batch, map[string]string{"Idempotency-Key": "campaign-1"})
	if status != http.StatusAccepted || created["accepted"] != float64(2) || created["rejected"] != float64(1) {
		t.Fatalf("create batch = %d %v", status, created)
	}
	items, _ := created["notifications"].([]any)
	if len(items) != 2 {
		t.Fatalf("notifications = %v, want two", created["notifications"])
	}
	second, _ := items[1].(map[string]any)
	if second["index"] != float64(2) {
		t.Errorf("second accepted item has index %v, want 2", second["index"])
	}
	if status, _, n := call(t, http.MethodGet, base+"/v1/notifications/"+second["id"].(string), "", nil); status != 200 || n["to"] != "ayse@example.com" {
		t.Errorf("get the second item = %d %v", status, n)
	}

	status, _, read := call(t, http.MethodGet, base+header.Get("Location"), "", nil)
	if status != http.StatusOK {
		t.Fatalf("get batch = %d %v", status, read)
	}
	if statuses, _ := read["statuses"].(map[string]any); statuses["queued"] != float64(2) {
		t.Errorf("statuses = %v, want 2 queued", read["statuses"])
	}
	rejections, _ := read["rejections"].([]any)
	if len(rejections) != 1 || !strings.Contains(fmt.Sprint(rejections[0]), "/notifications/1/to") {
		t.Errorf("rejections = %v, want item 1's recipient", read["rejections"])
	}

	status, header, again := call(t, http.MethodPost, base+"/v1/batches", batch, map[string]string{"Idempotency-Key": "campaign-1"})
	if status != http.StatusAccepted || again["id"] != created["id"] || header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("repeat = %d %v; want the first batch, replayed", status, again["id"])
	}
}

func TestRunReportsAnAddressItCannotListenOn(t *testing.T) {
	err := run(t.Context(), nil, env(map[string]string{
		"DATABASE_URL": testenv.PostgresDSN(t),
		"HTTP_ADDR":    "127.0.0.1:99999",
	}), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Errorf("run = %v, want a listen error", err)
	}
}

// TestAPIListCancelRetryEndToEnd lists, pages, cancels and retries through the running API. Failures are made
// in the database, as the worker will make them.
func TestAPIListCancelRetryEndToEnd(t *testing.T) {
	base, dsn := startAPI(t)
	pool, err := postgres.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	create := func(body string) string {
		t.Helper()
		status, _, n := call(t, http.MethodPost, base+"/v1/notifications", body, nil)
		if status != http.StatusAccepted {
			t.Fatalf("create = %d %v", status, n)
		}
		return n["id"].(string)
	}
	first := create(`{"to":"+905551234567","channel":"sms","content":"one"}`)
	create(`{"to":"+905551234568","channel":"sms","content":"two"}`)
	email := create(`{"to":"ayse@example.com","channel":"email","content":"three"}`)

	_, _, page := call(t, http.MethodGet, base+"/v1/notifications?channel=sms&limit=1", "", nil)
	items, _ := page["items"].([]any)
	cursor, _ := page["nextCursor"].(string)
	if len(items) != 1 || cursor == "" {
		t.Fatalf("first page = %v", page)
	}
	_, _, page = call(t, http.MethodGet, base+"/v1/notifications?channel=sms&limit=1&cursor="+cursor, "", nil)
	if items, _ := page["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != first {
		t.Errorf("second page = %v, want the first SMS", page)
	}
	if _, ok := page["nextCursor"]; ok {
		t.Error("the last page has a next cursor")
	}

	for range 2 { // cancelling twice is safe
		if status, _, n := call(t, http.MethodPost, base+"/v1/notifications/"+first+"/cancel", "", nil); status != 200 || n["status"] != "cancelled" {
			t.Errorf("cancel = %d %v", status, n)
		}
	}

	if _, err := pool.Exec(t.Context(), "UPDATE notifications SET status = 'failed', failed_at = now(), failure_reason = 'attempts_exhausted' WHERE id = $1", email); err != nil {
		t.Fatal(err)
	}
	if status, _, n := call(t, http.MethodPost, base+"/v1/notifications/"+email+"/retry", "", nil); status != 202 || n["status"] != "queued" {
		t.Errorf("retry = %d %v", status, n)
	}
	if status, _, p := call(t, http.MethodPost, base+"/v1/notifications/"+email+"/retry", "", nil); status != 409 || p["code"] != "not_retryable" {
		t.Errorf("retrying a queued notification = %d %v", status, p)
	}

	if _, err := pool.Exec(t.Context(), "UPDATE notifications SET status = 'failed', failed_at = '2026-11-23T10:30:00Z', failure_reason = 'attempts_exhausted' WHERE id = $1", email); err != nil {
		t.Fatal(err)
	}
	window := `{"failedFrom":"2026-11-23T10:00:00Z","failedTo":"2026-11-23T11:00:00Z"`
	if status, _, r := call(t, http.MethodPost, base+"/v1/notifications/retry", window+`,"dryRun":true}`, nil); status != 200 || r["matched"] != float64(1) || r["requeued"] != float64(0) {
		t.Errorf("dry run = %d %v", status, r)
	}
	if status, _, r := call(t, http.MethodPost, base+"/v1/notifications/retry", window+`}`, nil); status != 202 || r["requeued"] != float64(1) {
		t.Errorf("bulk retry = %d %v", status, r)
	}

	if status, _, p := call(t, http.MethodGet, base+"/v1/notifications?status=pending", "", nil); status != 400 || p["code"] != "invalid_query" {
		t.Errorf("bad query = %d %v", status, p)
	}
}
