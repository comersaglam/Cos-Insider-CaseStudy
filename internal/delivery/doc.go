// Package delivery sends one notification to the external provider: it consumes a
// channel queue as its single active consumer, enforces the per-channel rate limit,
// classifies provider outcomes, schedules retries and guards the provider with a
// circuit breaker.
package delivery
