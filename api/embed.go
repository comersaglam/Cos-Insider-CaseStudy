// Package api holds the service's HTTP contract, openapi.yaml, embedded so that the API serves it and tests check
// the server against it (ADR 0016, 0029).
package api

import _ "embed"

// Spec is openapi.yaml, the contract of the notification API.
//
//go:embed openapi.yaml
var Spec []byte
