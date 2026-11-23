package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// Connect opens a pool of connections to the database at dsn and checks that it answers. Pool settings travel in
// the URL, as pgxpool reads them (for example pool_max_conns=20).
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("reach database: %w", err)
	}
	return pool, nil
}

// Migrate applies the migrations in src that the database has not applied yet. golang-migrate holds an advisory
// lock while it works, so processes that start together apply each migration once.
func Migrate(pool *pgxpool.Pool, src fs.FS) (err error) {
	m, err := newMigrator(pool, src)
	if err != nil {
		return err
	}
	defer func() {
		srcErr, dbErr := m.Close()
		err = errors.Join(err, srcErr, dbErr)
	}()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// newMigrator returns a golang-migrate instance over pool. It goes through database/sql because the migration
// driver needs it; closing the instance closes that adapter, not the pool.
func newMigrator(pool *pgxpool.Pool, src fs.FS) (*migrate.Migrate, error) {
	driver, err := migratepgx.WithInstance(stdlib.OpenDBFromPool(pool), &migratepgx.Config{})
	if err != nil {
		return nil, fmt.Errorf("open migration driver: %w", err)
	}
	source, err := iofs.New(src, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		return nil, fmt.Errorf("prepare migrations: %w", err)
	}
	return m, nil
}
