// Package notification owns the notification lifecycle: its domain types and
// status transitions, content validation, the use cases (create, batch create, get,
// list, cancel, retry), their HTTP handlers and their PostgreSQL storage.
//
// It imports no other module; delivery and dispatch depend on it, never the reverse.
package notification
