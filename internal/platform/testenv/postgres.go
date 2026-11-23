package testenv

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/postgres"
	"github.com/comersaglam/Cos-Insider-CaseStudy/migrations"
)

// templateDatabase is migrated once per test process; every test gets a copy of it.
const templateDatabase = "migrated_template"

var pg struct {
	once  sync.Once
	err   error
	admin *pgxpool.Pool // connected to the "postgres" database, to create and drop the copies
	dsn   *url.URL      // the template's URL; a copy's URL differs only in its path
	// PostgreSQL refuses to copy a template while another copy is being made from it.
	copying sync.Mutex
}

// PostgresDSN returns the URL of a new database, migrated to the latest schema, that belongs to the calling test
// alone and is dropped when the test ends.
//
// The first call in a test process starts a PostgreSQL container from PostgresImage and migrates a template
// database in it; each call then copies the template, which takes milliseconds, so every test starts from a clean
// schema and tests can run in parallel. The container is removed when the process exits.
func PostgresDSN(t *testing.T) string {
	t.Helper()
	pg.once.Do(func() { pg.err = startPostgres(context.Background()) })
	if pg.err != nil {
		t.Fatalf("start PostgreSQL for tests: %v", pg.err)
	}

	name := "test_" + strings.ToLower(rand.Text())
	pg.copying.Lock()
	_, err := pg.admin.Exec(t.Context(), fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, templateDatabase))
	pg.copying.Unlock()
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		// t.Context() is already cancelled when cleanups run.
		if _, err := pg.admin.Exec(context.Background(), fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", name)); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})

	u := *pg.dsn
	u.Path = "/" + name
	return u.String()
}

func startPostgres(ctx context.Context) error {
	ctr, err := tcpostgres.Run(ctx, PostgresImage,
		tcpostgres.WithDatabase(templateDatabase),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return fmt.Errorf("run container: %w", err)
	}
	raw, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return fmt.Errorf("connection string: %w", err)
	}
	if pg.dsn, err = url.Parse(raw); err != nil {
		return fmt.Errorf("parse connection string: %w", err)
	}

	template, err := postgres.Connect(ctx, raw)
	if err != nil {
		return err
	}
	err = postgres.Migrate(template, migrations.FS)
	// No connection may stay open to a template while it is copied.
	template.Close()
	if err != nil {
		return err
	}

	admin := *pg.dsn
	admin.Path = "/postgres"
	pg.admin, err = postgres.Connect(ctx, admin.String())
	return err
}
