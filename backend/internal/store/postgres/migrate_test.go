package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newSchemaDB connects and switches the pool's search_path to a private schema,
// so each migration test starts from genuinely nothing. Migrate() writes
// unqualified names, which resolve to the first entry in search_path.
func newSchemaDB(t *testing.T, schema string) *DB {
	t.Helper()
	dsn := os.Getenv("BOLTRUNNER_TEST_DSN")
	if dsn == "" {
		t.Skip("BOLTRUNNER_TEST_DSN not set; skipping (requires a live Postgres)")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db := &DB{Pool: pool}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
		db.Close()
	})
	return db
}

func countDefaults(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM projects WHERE is_default`).Scan(&n); err != nil {
		t.Fatalf("count defaults: %v", err)
	}
	return n
}

func TestMigrateFromEmptyFlagsExactlyOneDefault(t *testing.T) {
	db := newSchemaDB(t, "br_mig_empty")
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if n := countDefaults(t, db); n != 1 {
		t.Fatalf("expected exactly 1 default project, got %d", n)
	}
}

// Migrate() is guarded by schema_migrations, so re-running it is a no-op. This
// asserts the migration body itself is idempotent, which is what protects a
// database whose 0003/0004 predate migration tracking (see postgres.go:25-28).
func TestMigration0005IsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := newSchemaDB(t, "br_mig_twice")
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	body, err := migrationsFS.ReadFile("migrations/0005_project_default_flag.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, string(body)); err != nil {
		t.Fatalf("re-applying 0005 must be a no-op, got: %v", err)
	}
	if n := countDefaults(t, db); n != 1 {
		t.Fatalf("expected exactly 1 default project after a re-run, got %d", n)
	}
}

// The deployed database has no project named 'Default' yet -- it predates 0003
// entirely -- but a hand-renamed row is the case that would silently flag
// nothing under a WHERE name = 'Default' backfill.
func TestMigration0005FlagsTheOldestWhenNoneIsNamedDefault(t *testing.T) {
	ctx := context.Background()
	db := newSchemaDB(t, "br_mig_renamed")
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Undo 0005 and rename, reproducing a database where the seeded project was
	// renamed by hand before this migration ever ran.
	if _, err := db.Pool.Exec(ctx, `UPDATE projects SET is_default = false, name = 'Shared'`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	body, _ := migrationsFS.ReadFile("migrations/0005_project_default_flag.sql")
	if _, err := db.Pool.Exec(ctx, string(body)); err != nil {
		t.Fatalf("re-apply 0005: %v", err)
	}
	var name string
	if err := db.Pool.QueryRow(ctx, `SELECT name FROM projects WHERE is_default`).Scan(&name); err != nil {
		t.Fatalf("expected a flagged project, got: %v", err)
	}
	if name != "Shared" {
		t.Fatalf("expected the renamed project to be flagged, got %q", name)
	}
}

// The case the deployed database will actually take: 0001/0002 tables holding
// real rows, then 0003/0004/0005 back to back. CI only ever migrates from
// empty, where 0004's catalog_id backfill has nothing to act on.
func TestMigrateFromThe0002SchemaPreservesData(t *testing.T) {
	ctx := context.Background()
	db := newSchemaDB(t, "br_mig_legacy")

	// Build the pre-0003 schema by hand, matching 0001 + 0002.
	legacy := `
CREATE TABLE tests (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    target_url       TEXT NOT NULL,
    virtual_users    INTEGER NOT NULL,
    duration_seconds INTEGER NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    test_id       UUID NOT NULL REFERENCES tests(id),
    status        TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    error_message TEXT NOT NULL DEFAULT ''
);
INSERT INTO tests (id, name, target_url, virtual_users, duration_seconds)
VALUES ('11111111-1111-1111-1111-111111111111', 'legacy', 'http://example.com', 3, 30);
INSERT INTO runs (test_id, status)
VALUES ('11111111-1111-1111-1111-111111111111', 'completed');`
	if _, err := db.Pool.Exec(ctx, legacy); err != nil {
		t.Fatalf("build legacy schema: %v", err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate over legacy data: %v", err)
	}

	tests, err := db.ListTests(ctx)
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("expected the legacy test to survive, got %d", len(tests))
	}
	got := tests[0]
	if got.Name != "legacy" || got.Version != 1 || got.ID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected migrated test: %+v", got)
	}
	if got.ProjectID == "" {
		t.Fatal("expected the legacy test to be filed under a project")
	}
	if n := countDefaults(t, db); n != 1 {
		t.Fatalf("expected exactly 1 default project, got %d", n)
	}
	// 0004 backfills catalog_id = id, so version 1's row keeps its original id.
	if got.VersionID != got.ID {
		t.Fatalf("expected the backfilled version row to keep its id, got %q", got.VersionID)
	}
}
