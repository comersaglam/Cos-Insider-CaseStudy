// Package postgres opens the PostgreSQL connection pool and applies the versioned migrations. Transactions need no
// helper of ours: pgx.BeginFunc commits when its function returns nil and rolls back otherwise.
package postgres
