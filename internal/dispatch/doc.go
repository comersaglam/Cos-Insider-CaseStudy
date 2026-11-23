// Package dispatch moves due work to RabbitMQ: the outbox relay publishes committed
// outbox rows with publisher confirms, and the scheduler turns notifications whose
// next attempt is due into outbox rows.
package dispatch
