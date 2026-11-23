// Package migrations holds the versioned database schema as golang-migrate files (NNNN_name.up.sql and
// NNNN_name.down.sql), embedded so that a binary carries the schema it expects.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
