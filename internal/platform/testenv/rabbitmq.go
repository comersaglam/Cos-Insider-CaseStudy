package testenv

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	tcrabbitmq "github.com/testcontainers/testcontainers-go/modules/rabbitmq"
)

var rmq struct {
	once  sync.Once
	ctr   *tcrabbitmq.RabbitMQContainer
	url   string // AMQP, default virtual host
	admin string // management API
	err   error
}

func startRabbitMQ() {
	ctx := context.Background()
	ctr, err := tcrabbitmq.Run(ctx, RabbitMQImage)
	if err != nil {
		rmq.err = fmt.Errorf("run container: %w", err)
		return
	}
	rmq.ctr = ctr
	if rmq.url, rmq.err = ctr.AmqpURL(ctx); rmq.err != nil {
		return
	}
	rmq.admin, rmq.err = ctr.HttpURL(ctx)
}

// RabbitMQURL returns the AMQP URL of a RabbitMQ container from RabbitMQImage, started on first use and shared by
// the tests of the process; it is removed when the process exits. Tests share its default virtual host, so each
// declares exchanges and queues under names of its own; RabbitMQVhostURL gives a test a virtual host of its own.
func RabbitMQURL(t *testing.T) string {
	t.Helper()
	rmq.once.Do(startRabbitMQ)
	if rmq.err != nil {
		t.Fatalf("start RabbitMQ for tests: %v", rmq.err)
	}
	return rmq.url
}

// RabbitMQVhostURL returns the AMQP URL of a new virtual host that belongs to the calling test alone and is deleted
// when the test ends: the test may use the service's real exchange and queue names, as PostgresDSN gives it a
// database of its own.
func RabbitMQVhostURL(t *testing.T) string {
	t.Helper()
	base := RabbitMQURL(t)
	vhost := "test-" + strings.ToLower(rand.Text())

	manage := func(method, path, body string) {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), method, rmq.admin+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth(rmq.ctr.AdminUsername, rmq.ctr.AdminPassword)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			t.Fatalf("%s %s: %s", method, path, resp.Status)
		}
	}
	manage(http.MethodPut, "/api/vhosts/"+vhost, "")
	manage(http.MethodPut, "/api/permissions/"+vhost+"/"+url.PathEscape(rmq.ctr.AdminUsername),
		`{"configure":".*","write":".*","read":".*"}`)
	t.Cleanup(func() { manage(http.MethodDelete, "/api/vhosts/"+vhost, "") })

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + vhost
	return u.String()
}
