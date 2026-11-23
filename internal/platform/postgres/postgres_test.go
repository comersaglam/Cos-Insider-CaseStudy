package postgres_test

import (
	"testing"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/postgres"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/testenv"
	"github.com/comersaglam/Cos-Insider-CaseStudy/migrations"
)

func TestConnectReachesTheDatabase(t *testing.T) {
	pool, err := postgres.Connect(t.Context(), testenv.PostgresDSN(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)
}

func TestConnectFailsWhenNothingAnswers(t *testing.T) {
	// Port 1 is reserved and never served, so the connection is refused at once.
	if _, err := postgres.Connect(t.Context(), "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"); err == nil {
		t.Fatal("Connect succeeded against a closed port")
	}
}

// TestMigrationsRollBackAndReapply runs every down migration and then every up migration again, on a database
// the helper has already migrated: a down migration that leaves anything behind makes the second up fail.
func TestMigrationsRollBackAndReapply(t *testing.T) {
	pool, err := postgres.Connect(t.Context(), testenv.PostgresDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	// Cleanups run last-registered first, so the migrator gives its connection back before the pool closes;
	// pool.Close waits for every connection, and a deferred Close here would wait forever.
	t.Cleanup(pool.Close)

	m, err := postgres.NewMigrator(pool, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			t.Errorf("close migrator: %v, %v", srcErr, dbErr)
		}
	})
	if err := m.Down(); err != nil {
		t.Fatalf("down: %v", err)
	}
	var tables int
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'schema_migrations'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Errorf("%d tables left after every down migration, want 0", tables)
	}
	if err := postgres.Migrate(pool, migrations.FS); err != nil {
		t.Fatalf("up again: %v", err)
	}
}
