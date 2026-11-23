package testenv

import (
	"context"
	"fmt"
	"sync"
	"testing"

	tcrabbitmq "github.com/testcontainers/testcontainers-go/modules/rabbitmq"
)

var rmq struct {
	once sync.Once
	url  string
	err  error
}

// RabbitMQURL returns the AMQP URL of a RabbitMQ container from RabbitMQImage, started on first use and shared by
// the tests of the process; it is removed when the process exits. Tests share its virtual host, so each declares
// exchanges and queues under names of its own.
func RabbitMQURL(t *testing.T) string {
	t.Helper()
	rmq.once.Do(func() {
		ctx := context.Background()
		ctr, err := tcrabbitmq.Run(ctx, RabbitMQImage)
		if err != nil {
			rmq.err = fmt.Errorf("run container: %w", err)
			return
		}
		rmq.url, rmq.err = ctr.AmqpURL(ctx)
	})
	if rmq.err != nil {
		t.Fatalf("start RabbitMQ for tests: %v", rmq.err)
	}
	return rmq.url
}
