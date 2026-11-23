// Package health serves liveness and readiness: liveness says the process runs,
// readiness says its dependencies (PostgreSQL, RabbitMQ) answer.
package health
