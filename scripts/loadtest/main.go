// Command loadtest sends a seeded burst of notifications to a running service and measures how it drains: the
// service accepts the burst at once, and the workers send it at the rate limit, by priority (docs/adr/0035).
//
// Usage:
//
//	go run ./scripts/loadtest -n 6000 -seed 1
//	go run ./scripts/loadtest -n 3000 -seed 2 -unavailable 0.15 -rejected 0.05
//
// The workload is half SMS, 30% e-mail and 20% push; 20% high, 60% normal and 20% low priority. -unavailable and
// -rejected send that share of the SMS to the stand-in provider's demonstration numbers, which answer 503 and 422.
// One seed always gives one workload.
package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"time"
)

// The stand-in provider's demonstration numbers (deploy/wiremock).
const (
	unavailableRecipient = "+905550000503"
	rejectedRecipient    = "+905550000422"
)

// batchSize is the largest batch the API takes.
const batchSize = 1000

// item is one notification of the workload, as a batch item.
type item struct {
	To       string `json:"to"`
	Channel  string `json:"channel"`
	Content  string `json:"content"`
	Priority string `json:"priority"`
}

func main() {
	base := flag.String("url", "http://localhost:8080", "the API's base URL")
	n := flag.Int("n", 3000, "how many notifications to send")
	seed := flag.Uint64("seed", 1, "the workload's seed")
	unavailable := flag.Float64("unavailable", 0, "the share of SMS sent to the number the stand-in answers 503")
	rejected := flag.Float64("rejected", 0, "the share of SMS sent to the number the stand-in answers 422")
	timeout := flag.Duration("timeout", 10*time.Minute, "how long to wait for the burst to drain")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := run(ctx, *base, workload(*seed, *n, *unavailable, *rejected)); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

// workload returns n notifications drawn from seed: the same seed, the same notifications.
func workload(seed uint64, n int, unavailable, rejected float64) []item {
	r := rand.New(rand.NewPCG(seed, seed)) //nolint:gosec // a seeded generator is the point: one seed, one workload

	items := make([]item, n)
	for i := range items {
		it := item{Content: fmt.Sprintf("Load test %d, notification %d.", seed, i)}
		switch c := r.Float64(); {
		case c < 0.5:
			it.Channel, it.To = "sms", fmt.Sprintf("+9055%08d", r.IntN(100_000_000))
			switch f := r.Float64(); {
			case f < unavailable:
				it.To = unavailableRecipient
			case f < unavailable+rejected:
				it.To = rejectedRecipient
			}
		case c < 0.8:
			it.Channel, it.To = "email", fmt.Sprintf("user%d@example.com", r.IntN(1_000_000))
		default:
			it.Channel, it.To = "push", fmt.Sprintf("device-token-%016x", r.Uint64())
		}
		switch p := r.Float64(); {
		case p < 0.2:
			it.Priority = "high"
		case p < 0.8:
			it.Priority = "normal"
		default:
			it.Priority = "low"
		}
		items[i] = it
	}
	return items
}

// run sends the workload in batches, then follows GET /v1/metrics until nothing waits, and reports the times and
// the rate measured per channel.
func run(ctx context.Context, base string, items []item) error {
	perChannel := map[string]int{}
	for _, it := range items {
		perChannel[it.Channel]++
	}
	start := time.Now()
	for from := 0; from < len(items); from += batchSize {
		to := min(from+batchSize, len(items))
		if err := post(ctx, base+"/v1/batches", map[string]any{"notifications": items[from:to]}); err != nil {
			return err
		}
	}
	accepted := time.Since(start)
	fmt.Printf("accepted %d notifications (sms %d, email %d, push %d) in %v\n",
		len(items), perChannel["sms"], perChannel["email"], perChannel["push"], accepted.Round(time.Millisecond))

	drained := map[string]time.Duration{}
	for len(drained) < len(perChannel) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("the burst did not drain: %w", ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
		waiting, err := waitingNow(ctx, base)
		if err != nil {
			return err
		}
		for channel := range perChannel {
			if _, done := drained[channel]; !done && waiting[channel] == 0 {
				drained[channel] = time.Since(start)
			}
		}
	}
	for _, channel := range []string{"sms", "email", "push"} {
		if d, ok := drained[channel]; ok {
			fmt.Printf("%-5s %5d sent or settled in %6.1f s: %5.1f a second\n",
				channel, perChannel[channel], d.Seconds(), float64(perChannel[channel])/d.Seconds())
		}
	}
	return nil
}

// waitingNow returns how many notifications wait to be sent now, by channel; those waiting to be retried later do
// not count, since the burst has been through the queue.
func waitingNow(ctx context.Context, base string) (map[string]int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/metrics", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var summary struct {
		Channels map[string]struct {
			Waiting struct {
				Queued  int `json:"queued"`
				Sending int `json:"sending"`
			} `json:"waiting"`
		} `json:"channels"`
	}
	if err := json.UnmarshalRead(resp.Body, &summary); err != nil {
		return nil, fmt.Errorf("read the summary: %w", err)
	}
	out := map[string]int{}
	for channel, s := range summary.Channels {
		out[channel] = s.Waiting.Queued + s.Waiting.Sending
	}
	return out, nil
}

func post(ctx context.Context, url string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		answer, _ := io.ReadAll(resp.Body)
		return errors.New("POST " + url + ": " + resp.Status + ": " + string(answer))
	}
	return nil
}
