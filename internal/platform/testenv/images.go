// Package testenv holds what integration tests need from their environment, starting with the container images
// they run against. The local stack in compose.yaml runs the same images, so a query that works in one works in
// the other.
package testenv

// Images that integration tests start. compose.yaml must use the same tags; TestComposeRunsTheTestImages fails
// when the two drift apart.
const (
	PostgresImage = "postgres:18.7-alpine"
	RabbitMQImage = "rabbitmq:4.4.1-management-alpine"
)
