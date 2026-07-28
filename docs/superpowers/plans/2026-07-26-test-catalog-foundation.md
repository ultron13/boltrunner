# Test Catalog Foundation Implementation Plan (BOL-28)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the flat single-version `tests` table into a versioned catalog with a minimal project registry, per `docs/superpowers/specs/2026-07-26-test-catalog-foundation-design.md`.

**Architecture:** One immutable `tests` table keyed by `(catalog_id, version)`; `tests.id` remains the primary key and is unique *per version*, while `catalog_id` is the stable identity the API exposes as `Test.id`. Editing a test writes a new version row (copy-on-write). `runs.test_id`'s existing foreign key therefore pins the exact executed version with no FK migration, and a new denormalized `runs.test_catalog_id` groups runs by test family. Migrations become properly versioned via a `schema_migrations` table.

**Tech Stack:** Go 1.26, chi v5 router, pgx v5 (`pgxpool`, `pgconn` for SQLSTATE inspection), `embed.FS` for migrations, standard `testing` package. No new module dependencies — everything needed is already in `backend/go.mod`.

## Global Constraints

- **Existing behavior unchanged.** Every existing test in `backend/internal/store/memstore`, `backend/internal/store/postgres`, and `backend/internal/api` must pass **without editing its assertions**. Two mechanical exceptions are expected and permitted: `newTestServer()` in `backend/internal/api/runs_test.go` and the `api.NewServer(...)` call in `backend/cmd/server/main.go` both gain a `ProjectStore` argument (Task 4). No assertion anywhere may be weakened to accommodate new behavior.
- **The frontend is not touched.** No file under `frontend/` changes. Its unit and e2e suites must keep passing untouched.
- `store.TestStore`'s three existing method signatures — `CreateTest`, `ListTests`, `GetTest` — stay byte-identical. Only their internals change.
- The API's `Test.id` is the **catalog id**. The version row's PK is exposed separately as `version_id`. Because the backfill sets `catalog_id = id`, responses for existing data stay byte-identical.
- `ListTests` orders by the **family's** `MIN(created_at)` descending — so editing a test does not reshuffle the list.
- A `(catalog_id, version)` collision from concurrent edits returns `store.ErrConflict`, surfaced by the API as **HTTP 409**. It is never silently retried and never forks a version number.
- Postgres store tests are skipped unless `BOLTRUNNER_TEST_DSN` is set (existing `setupDB` behavior). CI's `backend-unit` job sets it against a `postgres:16` service, so these tests **do** run there and **do** count toward the 88% coverage gate — never treat them as optional. Locally, never point that DSN at the shared dev database `boltrunner`; use a dedicated database. Tests must tolerate pre-existing rows, as the current ones do.
- The test DSN user must be able to `CREATE DATABASE` (the `newScratchDB` helper needs it). This holds for both CI's `postgres:16` service and the dev cluster, where `POSTGRES_USER=boltrunner` is created as a superuser. `replaceDBName` must preserve the DSN's query string, because CI's DSN carries `?sslmode=disable`.
- **Known limitation, deliberately accepted:** migrations 0003/0004 add `NOT NULL` columns without defaults, so an *old* server binary still running against the *new* schema will fail inserts. This project migrates on boot inside the same binary and has no zero-downtime requirement yet, so no rolling-upgrade compatibility shim is built. Do not migrate the shared dev database while the old image is deployed.

---

### Task 1: Versioned migration infrastructure

**Files:**
- Modify: `backend/internal/store/postgres/postgres.go`
- Test: `backend/internal/store/postgres/store_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `Migrate(ctx)` applies any `migrations/*.sql` file not yet recorded in `schema_migrations`, in filename order, each in its own transaction. Later tasks add migrations by **adding a file only** — no Go change. Also produces the package-level helper `migrationVersion(name string) (int, error)`.

- [x] **Step 1: Write the failing tests**

Append to `backend/internal/store/postgres/store_test.go`:

```go
func TestMigrationVersionParsesLeadingInteger(t *testing.T) {
	got, err := migrationVersion("0003_projects.sql")
	if err != nil {
		t.Fatalf("migrationVersion: %v", err)
	}
	if got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
	if _, err := migrationVersion("nonsense.sql"); err == nil {
		t.Fatal("expected an error for a filename with no version prefix")
	}
	if _, err := migrationVersion("abcd_x.sql"); err == nil {
		t.Fatal("expected an error for a non-numeric version prefix")
	}
}

func TestMigrateRecordsVersionsAndIsIdempotent(t *testing.T) {
	db := setupDB(t) // setupDB already calls Migrate once
	ctx := context.Background()

	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count < 2 {
		t.Fatalf("expected the existing migrations to be recorded, got %d", count)
	}

	// A second Migrate must skip everything already applied rather than
	// re-running it.
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var after int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatalf("recount schema_migrations: %v", err)
	}
	if after != count {
		t.Fatalf("expected the count to stay %d, got %d", count, after)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/store/postgres/ -run 'TestMigrationVersion|TestMigrateRecords' -v`
Expected: FAIL to **compile** — `undefined: migrationVersion`. (The compile failure is the red state; `migrationVersion` doesn't exist yet.)

- [x] **Step 3: Rewrite `Migrate` and its helpers**

In `backend/internal/store/postgres/postgres.go`, replace the import block and the two embed vars plus `Migrate`. The final top-of-file and migration section must read exactly:

```go
package postgres

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// schema_migrations is created outside the numbered migrations because it is
// what tracks them. Existing databases have 0001/0002 applied but unrecorded;
// both are idempotent (IF NOT EXISTS / ADD COLUMN IF NOT EXISTS), so the first
// run after this change re-applies them harmlessly and records them.
const createSchemaMigrations = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

type DB struct {
	Pool *pgxpool.Pool
}
```

(Keep `Connect` and `Close` exactly as they are.) Then replace `Migrate` with:

```go
func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.Pool.Exec(ctx, createSchemaMigrations); err != nil {
		return err
	}
	names, err := migrationFilenames()
	if err != nil {
		return err
	}
	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		applied, err := db.migrationApplied(ctx, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err := db.applyMigration(ctx, version, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

// migrationFilenames returns every embedded .sql migration in ascending
// filename order, which is also version order given the NNNN_ prefix.
func migrationFilenames() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// migrationVersion parses the leading integer of a migration filename, so
// "0003_projects.sql" yields 3.
func migrationVersion(name string) (int, error) {
	i := strings.IndexByte(name, '_')
	if i <= 0 {
		return 0, fmt.Errorf("migration %q must be named <version>_<description>.sql", name)
	}
	v, err := strconv.Atoi(name[:i])
	if err != nil {
		return 0, fmt.Errorf("migration %q has a non-numeric version prefix: %w", name, err)
	}
	return v, nil
}

func (db *DB) migrationApplied(ctx context.Context, version int) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
	).Scan(&exists)
	return exists, err
}

// applyMigration runs one migration and records it in the same transaction, so
// a partially applied migration can never be marked as done.
func (db *DB) applyMigration(ctx context.Context, version int, body string) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, body); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
```

Delete the now-unused `migration0001` and `migration0002` vars.

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/store/postgres/ -run 'TestMigrationVersion|TestMigrateRecords' -v`
Expected: PASS. `TestMigrationVersionParsesLeadingInteger` passes with or without a DSN; `TestMigrateRecordsVersionsAndIsIdempotent` passes with a DSN and skips without one.

- [x] **Step 5: Run the whole backend suite**

Run: `cd backend && go test ./...`
Expected: PASS. In particular `TestConnectAndMigrate` and `TestMigrateFailsWithCancelledContext` (which relies on `Migrate` erroring on a cancelled context — the first `Exec` still does) must pass unchanged.

- [x] **Step 6: Commit**

```bash
git add backend/internal/store/postgres/postgres.go backend/internal/store/postgres/store_test.go
git commit -m "feat(backend): track applied migrations in schema_migrations"
```

---

### Task 2: Projects table and ProjectStore

**Files:**
- Create: `backend/internal/store/postgres/migrations/0003_projects.sql`
- Create: `backend/internal/store/memstore/projectstore.go`
- Create: `backend/internal/store/memstore/projectstore_test.go`
- Modify: `backend/internal/model/model.go`
- Modify: `backend/internal/store/store.go`
- Modify: `backend/internal/store/postgres/postgres.go`
- Modify: `backend/internal/store/memstore/memstore.go`
- Test: `backend/internal/store/postgres/store_test.go`, `backend/internal/store/memstore/memstore_test.go`

**Interfaces:**
- Consumes: Task 1's file-driven `Migrate` — this task adds `0003_projects.sql` and writes **no** Go migration code.
- Produces:
  - `model.Project{ID, Name string; CreatedAt time.Time}`.
  - `model.Test.ProjectID string` with JSON tag `project_id`.
  - `store.ProjectStore` interface with `ListProjects(ctx) ([]model.Project, error)`; implemented by `*postgres.DB` and `*memstore.ProjectStore`.
  - `memstore.NewProjectStore() *memstore.ProjectStore`, plus exported constants `memstore.DefaultProjectID` and `memstore.DefaultProjectName`.
  - Contract both stores honor: `CreateTest` with an empty `ProjectID` assigns the Default project.
  - `postgres.newScratchDB(t)` test helper (see Step 7) reused and extended by Task 3.

- [x] **Step 1: Write the migration**

Create `backend/internal/store/postgres/migrations/0003_projects.sql`:

```sql
CREATE TABLE IF NOT EXISTS projects (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO projects (name) VALUES ('Default') ON CONFLICT (name) DO NOTHING;

ALTER TABLE tests ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id);
UPDATE tests SET project_id = (SELECT id FROM projects WHERE name = 'Default')
  WHERE project_id IS NULL;
ALTER TABLE tests ALTER COLUMN project_id SET NOT NULL;
```

- [x] **Step 2: Add the model and interface**

In `backend/internal/model/model.go`, add `ProjectID` to `Test` (place it directly after `ID`) and append the `Project` type:

```go
type Test struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	Name            string    `json:"name"`
	TargetURL       string    `json:"target_url"`
	VirtualUsers    int       `json:"virtual_users"`
	DurationSeconds int       `json:"duration_seconds"`
	CreatedAt       time.Time `json:"created_at"`
}

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}
```

In `backend/internal/store/store.go`, append:

```go
type ProjectStore interface {
	ListProjects(ctx context.Context) ([]model.Project, error)
}
```

- [x] **Step 3: Write the failing memstore tests**

Create `backend/internal/store/memstore/projectstore_test.go`:

```go
package memstore

import (
	"context"
	"testing"
)

func TestProjectStoreListsSeededDefault(t *testing.T) {
	ctx := context.Background()
	ps := NewProjectStore()

	projects, err := ps.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if projects == nil {
		t.Fatal("expected a non-nil slice")
	}
	if len(projects) != 1 {
		t.Fatalf("expected exactly the seeded Default project, got %d", len(projects))
	}
	if projects[0].Name != DefaultProjectName {
		t.Fatalf("expected name %q, got %q", DefaultProjectName, projects[0].Name)
	}
	if projects[0].ID != DefaultProjectID {
		t.Fatalf("expected id %q, got %q", DefaultProjectID, projects[0].ID)
	}
	if projects[0].CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
}
```

Append to `backend/internal/store/memstore/memstore_test.go`:

```go
func TestCreateTestDefaultsToDefaultProject(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	in := &model.Test{Name: "no-project", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.CreateTest(ctx, in); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if in.ProjectID != DefaultProjectID {
		t.Fatalf("expected the Default project id, got %q", in.ProjectID)
	}

	got, err := ts.GetTest(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if got.ProjectID != DefaultProjectID {
		t.Fatalf("expected GetTest to report the Default project, got %q", got.ProjectID)
	}
}

func TestCreateTestHonoursExplicitProject(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	const other = "11111111-1111-1111-1111-111111111111"
	in := &model.Test{ProjectID: other, Name: "explicit", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.CreateTest(ctx, in); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if in.ProjectID != other {
		t.Fatalf("expected the explicit project id to be preserved, got %q", in.ProjectID)
	}
}
```

- [x] **Step 4: Run them to verify they fail**

Run: `cd backend && go test ./internal/store/memstore/ -run 'TestProjectStore|TestCreateTestDefaults|TestCreateTestHonours' -v`
Expected: FAIL to compile — `undefined: NewProjectStore`, `undefined: DefaultProjectID`.

- [x] **Step 5: Implement the memstore side**

Create `backend/internal/store/memstore/projectstore.go`:

```go
package memstore

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/boltrunner/backend/internal/model"
)

// DefaultProjectID is the id of the seeded "Default" project. memstore has no
// migrations to seed it with a generated UUID, so both stores agree on a fixed
// well-known value instead -- that keeps NewTestStore() argument-free while
// still letting CreateTest fill in a project.
const (
	DefaultProjectID   = "00000000-0000-0000-0000-000000000001"
	DefaultProjectName = "Default"
)

type ProjectStore struct {
	mu       sync.RWMutex
	projects map[string]model.Project
}

func NewProjectStore() *ProjectStore {
	return &ProjectStore{projects: map[string]model.Project{
		DefaultProjectID: {ID: DefaultProjectID, Name: DefaultProjectName, CreatedAt: time.Now().UTC()},
	}}
}

func (s *ProjectStore) ListProjects(ctx context.Context) ([]model.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
```

In `backend/internal/store/memstore/memstore.go`, add the project default to `CreateTest` — insert these two lines immediately after `defer s.mu.Unlock()`:

```go
	if t.ProjectID == "" {
		t.ProjectID = DefaultProjectID
	}
```

- [x] **Step 6: Run the memstore tests to verify they pass**

Run: `cd backend && go test ./internal/store/memstore/ -v`
Expected: PASS, including the pre-existing `TestTestStoreCreateListGet` unchanged.

- [x] **Step 7: Write the failing postgres tests, including the legacy-upgrade helper**

Append to `backend/internal/store/postgres/store_test.go`. `newScratchDB` is the helper Task 3 extends — it proves migrations work on a database that already holds pre-0003 rows, which is exactly the shared dev database's situation:

```go
// newScratchDB creates a throwaway database, applies only the migrations up to
// and including maxVersion, and returns a connection to it. It exists to test
// the upgrade path of an *existing* deployment: seed legacy rows, then run the
// full Migrate and assert the backfills.
func newScratchDB(t *testing.T, maxVersion int) *DB {
	t.Helper()
	dsn := os.Getenv("BOLTRUNNER_TEST_DSN")
	if dsn == "" {
		t.Skip("BOLTRUNNER_TEST_DSN not set; skipping (requires a live Postgres)")
	}
	ctx := context.Background()

	admin, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect (admin): %v", err)
	}
	defer admin.Close()

	name := fmt.Sprintf("boltrunner_scratch_%d", time.Now().UnixNano())
	if _, err := admin.Pool.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("CREATE DATABASE: %v", err)
	}

	scratchDSN, err := replaceDBName(dsn, name)
	if err != nil {
		t.Fatalf("replaceDBName: %v", err)
	}
	db, err := Connect(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("Connect (scratch): %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		cleanup, err := Connect(ctx, dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		cleanup.Pool.Exec(ctx, `DROP DATABASE IF EXISTS `+name)
	})

	// Apply only the migrations at or below maxVersion, simulating a
	// deployment that predates the newer ones.
	if _, err := db.Pool.Exec(ctx, createSchemaMigrations); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	names, err := migrationFilenames()
	if err != nil {
		t.Fatalf("migrationFilenames: %v", err)
	}
	for _, n := range names {
		v, err := migrationVersion(n)
		if err != nil {
			t.Fatalf("migrationVersion(%s): %v", n, err)
		}
		if v > maxVersion {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + n)
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		if err := db.applyMigration(ctx, v, string(body)); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
	}
	return db
}

func replaceDBName(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

func TestListProjectsIncludesSeededDefaultAndIsNeverNil(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	projects, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if projects == nil {
		t.Fatal("expected a non-nil slice")
	}
	found := false
	for _, p := range projects {
		if p.Name == "Default" {
			found = true
			if p.ID == "" || p.CreatedAt.IsZero() {
				t.Fatalf("expected a fully populated project, got %+v", p)
			}
		}
	}
	if !found {
		t.Fatal("expected the seeded Default project")
	}
}

func TestCreateTestDefaultsToDefaultProject(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "pg-default-project", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if tst.ProjectID == "" {
		t.Fatal("expected CreateTest to populate ProjectID")
	}

	got, err := db.GetTest(ctx, tst.ID)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if got.ProjectID != tst.ProjectID {
		t.Fatalf("expected GetTest to report project %q, got %q", tst.ProjectID, got.ProjectID)
	}
}

func TestMigrateBackfillsProjectIDForLegacyRows(t *testing.T) {
	ctx := context.Background()
	db := newScratchDB(t, 2) // a database as it looked before 0003

	// A legacy row, inserted with no project_id because the column did not
	// exist yet.
	var legacyID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO tests (name, target_url, virtual_users, duration_seconds)
		 VALUES ('legacy', 'http://example.com', 1, 1) RETURNING id`,
	).Scan(&legacyID); err != nil {
		t.Fatalf("insert legacy test: %v", err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var projectName string
	if err := db.Pool.QueryRow(ctx,
		`SELECT p.name FROM tests t JOIN projects p ON p.id = t.project_id WHERE t.id = $1`, legacyID,
	).Scan(&projectName); err != nil {
		t.Fatalf("read backfilled project: %v", err)
	}
	if projectName != "Default" {
		t.Fatalf("expected the legacy row to land in Default, got %q", projectName)
	}
}
```

Add `"fmt"`, `"net/url"`, and `"time"` to that file's imports if not already present (`time` and `os` already are).

- [x] **Step 8: Run them to verify they fail**

Run: `cd backend && go test ./internal/store/postgres/ -run 'TestListProjects|TestCreateTestDefaults|TestMigrateBackfillsProjectID' -v`
Expected: FAIL to compile — `db.ListProjects` undefined. (With no DSN set they skip instead; if so, set one per Task 5 Step 1 before continuing, because this task's postgres behavior cannot otherwise be verified.)

- [x] **Step 9: Implement the postgres side**

In `backend/internal/store/postgres/postgres.go`, replace `CreateTest`, `ListTests`, and `GetTest`, and append `ListProjects` plus the `nullableUUID` helper:

```go
// nullableUUID converts an empty id to a SQL NULL so COALESCE can substitute a
// default; pgx would otherwise reject "" as a malformed UUID.
func nullableUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func (db *DB) CreateTest(ctx context.Context, t *model.Test) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO tests (name, target_url, virtual_users, duration_seconds, project_id)
		 VALUES ($1, $2, $3, $4,
		         COALESCE($5, (SELECT id FROM projects WHERE name = 'Default')))
		 RETURNING id, project_id, created_at`,
		t.Name, t.TargetURL, t.VirtualUsers, t.DurationSeconds, nullableUUID(t.ProjectID),
	).Scan(&t.ID, &t.ProjectID, &t.CreatedAt)
}

func (db *DB) ListTests(ctx context.Context) ([]model.Test, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, project_id, name, target_url, virtual_users, duration_seconds, created_at
		 FROM tests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Test{}
	for rows.Next() {
		var t model.Test
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Name, &t.TargetURL, &t.VirtualUsers, &t.DurationSeconds, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) GetTest(ctx context.Context, id string) (*model.Test, error) {
	var t model.Test
	err := db.Pool.QueryRow(ctx,
		`SELECT id, project_id, name, target_url, virtual_users, duration_seconds, created_at
		 FROM tests WHERE id = $1`, id,
	).Scan(&t.ID, &t.ProjectID, &t.Name, &t.TargetURL, &t.VirtualUsers, &t.DurationSeconds, &t.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &t, err
}

func (db *DB) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, created_at FROM projects ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Project{}
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
```

- [x] **Step 10: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/store/postgres/ -v`
Expected: PASS, with every pre-existing test in the file unchanged.

- [x] **Step 11: Run the whole backend suite**

Run: `cd backend && go test ./...`
Expected: PASS.

- [x] **Step 12: Commit**

```bash
git add backend/internal/model/model.go backend/internal/store/store.go \
        backend/internal/store/postgres/migrations/0003_projects.sql \
        backend/internal/store/postgres/postgres.go backend/internal/store/postgres/store_test.go \
        backend/internal/store/memstore/projectstore.go backend/internal/store/memstore/projectstore_test.go \
        backend/internal/store/memstore/memstore.go backend/internal/store/memstore/memstore_test.go
git commit -m "feat(backend): add a projects registry and default tests to it"
```

---

### Task 3: Versioned tests

**Files:**
- Create: `backend/internal/store/postgres/migrations/0004_test_versioning.sql`
- Modify: `backend/internal/model/model.go`
- Modify: `backend/internal/store/store.go`
- Modify: `backend/internal/store/postgres/postgres.go`
- Modify: `backend/internal/store/memstore/memstore.go`, `backend/internal/store/memstore/runstore.go`
- Modify: `backend/internal/api/runs.go`
- Test: `backend/internal/store/postgres/store_test.go`, `backend/internal/store/memstore/memstore_test.go`

**Interfaces:**
- Consumes: Task 1's file-driven `Migrate`; Task 2's `model.Test.ProjectID` and the `newScratchDB(t, maxVersion)` helper.
- Produces:
  - `model.Test` gains `VersionID string` (`version_id`), `Version int` (`version`), `UpdatedAt time.Time` (`updated_at`). `ID` now carries the **catalog** id; `CreatedAt` is the family's first-creation time; `UpdatedAt` is this version's creation time.
  - `model.Run` gains `TestCatalogID string` (`test_catalog_id`).
  - `store.ErrConflict`.
  - `store.TestStore` gains `UpdateTest(ctx, *model.Test) error` and `ListTestVersions(ctx, catalogID string) ([]model.Test, error)` (newest version first).
  - Contract both stores honor: `CreateRun` with an empty `TestCatalogID` derives it from `TestID`; `ListByTest` takes a **catalog** id and returns runs across all versions.

- [x] **Step 1: Write the migration**

Create `backend/internal/store/postgres/migrations/0004_test_versioning.sql`:

```sql
ALTER TABLE tests ADD COLUMN IF NOT EXISTS catalog_id UUID;
ALTER TABLE tests ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
UPDATE tests SET catalog_id = id WHERE catalog_id IS NULL;
ALTER TABLE tests ALTER COLUMN catalog_id SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tests_catalog_version ON tests (catalog_id, version);
CREATE INDEX IF NOT EXISTS idx_tests_catalog ON tests (catalog_id);

ALTER TABLE runs ADD COLUMN IF NOT EXISTS test_catalog_id UUID;
UPDATE runs SET test_catalog_id = t.catalog_id FROM tests t
  WHERE runs.test_id = t.id AND runs.test_catalog_id IS NULL;
ALTER TABLE runs ALTER COLUMN test_catalog_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_runs_test_catalog_id ON runs (test_catalog_id, created_at DESC);
```

- [x] **Step 2: Extend the model and the store interface**

In `backend/internal/model/model.go`, `Test` becomes:

```go
type Test struct {
	ID              string    `json:"id"`         // catalog id: stable across versions
	VersionID       string    `json:"version_id"` // primary key of this version row
	Version         int       `json:"version"`
	ProjectID       string    `json:"project_id"`
	Name            string    `json:"name"`
	TargetURL       string    `json:"target_url"`
	VirtualUsers    int       `json:"virtual_users"`
	DurationSeconds int       `json:"duration_seconds"`
	CreatedAt       time.Time `json:"created_at"` // when the test was first created
	UpdatedAt       time.Time `json:"updated_at"` // when this version was cut
}
```

and add one field to `Run`, after `TestID`:

```go
	TestCatalogID string `json:"test_catalog_id"`
```

In `backend/internal/store/store.go`:

```go
var (
	ErrNotFound = errors.New("not found")
	// ErrConflict means a concurrent edit already claimed the version number
	// this update tried to write.
	ErrConflict = errors.New("conflict")
)

type TestStore interface {
	CreateTest(ctx context.Context, t *model.Test) error
	ListTests(ctx context.Context) ([]model.Test, error)
	GetTest(ctx context.Context, catalogID string) (*model.Test, error)
	// UpdateTest writes a new immutable version of t.ID's test rather than
	// mutating the current one.
	UpdateTest(ctx context.Context, t *model.Test) error
	// ListTestVersions returns every version of a test, newest first.
	ListTestVersions(ctx context.Context, catalogID string) ([]model.Test, error)
}
```

(Delete the old standalone `var ErrNotFound = ...` line.)

- [x] **Step 3: Write the failing memstore tests**

Append to `backend/internal/store/memstore/memstore_test.go`:

```go
func TestUpdateTestCreatesNewVersionAndKeepsTheOldOne(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	v1 := &model.Test{Name: "checkout", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 10}
	if err := ts.CreateTest(ctx, v1); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("expected version 1, got %d", v1.Version)
	}
	if v1.VersionID == "" {
		t.Fatal("expected a VersionID")
	}

	edit := &model.Test{ID: v1.ID, Name: "checkout", TargetURL: "http://b", VirtualUsers: 2, DurationSeconds: 20}
	if err := ts.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}
	if edit.Version != 2 {
		t.Fatalf("expected version 2, got %d", edit.Version)
	}
	if edit.ID != v1.ID {
		t.Fatalf("expected the catalog id to be stable, got %q want %q", edit.ID, v1.ID)
	}
	if edit.VersionID == v1.VersionID {
		t.Fatal("expected a new VersionID for the new version")
	}

	// GetTest resolves the latest version.
	latest, err := ts.GetTest(ctx, v1.ID)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if latest.Version != 2 || latest.TargetURL != "http://b" {
		t.Fatalf("expected v2/http://b, got v%d/%s", latest.Version, latest.TargetURL)
	}

	// ListTests collapses the family to one row.
	all, err := ts.ListTests(ctx)
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 row for 1 test family, got %d", len(all))
	}
	if all[0].Version != 2 {
		t.Fatalf("expected the latest version, got v%d", all[0].Version)
	}

	// The old version is still readable, unchanged.
	versions, err := ts.ListTestVersions(ctx, v1.ID)
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("expected newest-first, got v%d then v%d", versions[0].Version, versions[1].Version)
	}
	if versions[1].TargetURL != "http://a" {
		t.Fatalf("expected v1 to keep its original target, got %q", versions[1].TargetURL)
	}
}

func TestUpdateTestPreservesFamilyCreatedAt(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	v1 := &model.Test{Name: "x", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	_ = ts.CreateTest(ctx, v1)
	time.Sleep(5 * time.Millisecond)
	edit := &model.Test{ID: v1.ID, Name: "x", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	latest, _ := ts.GetTest(ctx, v1.ID)
	if !latest.CreatedAt.Equal(v1.CreatedAt) {
		t.Fatalf("expected CreatedAt to stay the family's first creation %v, got %v", v1.CreatedAt, latest.CreatedAt)
	}
	if !latest.UpdatedAt.After(latest.CreatedAt) {
		t.Fatalf("expected UpdatedAt (%v) to be after CreatedAt (%v)", latest.UpdatedAt, latest.CreatedAt)
	}
}

func TestUpdateTestUnknownCatalogIDIsNotFound(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	err := ts.UpdateTest(ctx, &model.Test{ID: "missing", Name: "x", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListTestVersionsUnknownIDIsEmptyNotNil(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	versions, err := ts.ListTestVersions(ctx, "missing")
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if versions == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(versions) != 0 {
		t.Fatalf("expected 0 versions, got %d", len(versions))
	}
}
```

Add `"errors"`, `"time"`, and the `store` import to that file if missing (`store` is already imported).

Append to `backend/internal/store/memstore/runstore_test.go`:

```go
func TestCreateRunDerivesTestCatalogIDFromTestID(t *testing.T) {
	ctx := context.Background()
	rs := NewRunStore()

	run := &model.Run{TestID: "version-1-id", Status: model.RunPending}
	if err := rs.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.TestCatalogID != "version-1-id" {
		t.Fatalf("expected the catalog id to default to the test id, got %q", run.TestCatalogID)
	}
}

func TestListByTestGroupsRunsAcrossVersions(t *testing.T) {
	ctx := context.Background()
	rs := NewRunStore()

	// Two runs of the same test, executed against different versions.
	first := &model.Run{TestID: "v1", TestCatalogID: "catalog", Status: model.RunCompleted}
	_ = rs.CreateRun(ctx, first)
	second := &model.Run{TestID: "v2", TestCatalogID: "catalog", Status: model.RunCompleted}
	_ = rs.CreateRun(ctx, second)

	runs, err := rs.ListByTest(ctx, "catalog")
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected both versions' runs, got %d", len(runs))
	}
}
```

- [x] **Step 4: Run them to verify they fail**

Run: `cd backend && go test ./internal/store/memstore/ -v`
Expected: FAIL to compile — `ts.UpdateTest` and `ts.ListTestVersions` undefined, `model.Test` has no field `VersionID`. (If Step 2 is already applied, the model fields exist and the failure is only the two undefined methods.)

- [x] **Step 5: Implement the memstore side**

Replace `backend/internal/store/memstore/memstore.go` entirely:

```go
package memstore

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

// TestStore keys every version row by its own VersionID, mirroring the
// postgres table where the primary key is per-version and catalog_id is the
// stable identity.
type TestStore struct {
	mu    sync.RWMutex
	tests map[string]model.Test
}

func NewTestStore() *TestStore {
	return &TestStore{tests: make(map[string]model.Test)}
}

func (s *TestStore) CreateTest(ctx context.Context, t *model.Test) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.ProjectID == "" {
		t.ProjectID = DefaultProjectID
	}
	now := time.Now().UTC()
	t.ID = uuid.NewString()
	t.VersionID = t.ID
	t.Version = 1
	t.CreatedAt = now
	t.UpdatedAt = now
	s.tests[t.VersionID] = *t
	return nil
}

func (s *TestStore) ListTests(ctx context.Context) ([]model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	latest := map[string]model.Test{}
	for _, t := range s.tests {
		if cur, ok := latest[t.ID]; !ok || t.Version > cur.Version {
			latest[t.ID] = t
		}
	}
	out := make([]model.Test, 0, len(latest))
	for _, t := range latest {
		t.CreatedAt = s.familyCreatedAt(t.ID)
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *TestStore) GetTest(ctx context.Context, catalogID string) (*model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.latestLocked(catalogID)
	if !ok {
		return nil, store.ErrNotFound
	}
	t.CreatedAt = s.familyCreatedAt(catalogID)
	return &t, nil
}

func (s *TestStore) UpdateTest(ctx context.Context, t *model.Test) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest, ok := s.latestLocked(t.ID)
	if !ok {
		return store.ErrNotFound
	}
	next := latest.Version + 1
	for _, existing := range s.tests {
		if existing.ID == t.ID && existing.Version == next {
			return store.ErrConflict
		}
	}
	t.ProjectID = latest.ProjectID
	t.VersionID = uuid.NewString()
	t.Version = next
	t.CreatedAt = s.familyCreatedAt(t.ID)
	t.UpdatedAt = time.Now().UTC()
	s.tests[t.VersionID] = *t
	return nil
}

func (s *TestStore) ListTestVersions(ctx context.Context, catalogID string) ([]model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	created := s.familyCreatedAt(catalogID)
	out := []model.Test{}
	for _, t := range s.tests {
		if t.ID == catalogID {
			t.CreatedAt = created
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

// latestLocked returns the highest-numbered version of a family. Callers must
// hold the lock.
func (s *TestStore) latestLocked(catalogID string) (model.Test, bool) {
	var found model.Test
	ok := false
	for _, t := range s.tests {
		if t.ID == catalogID && (!ok || t.Version > found.Version) {
			found = t
			ok = true
		}
	}
	return found, ok
}

// familyCreatedAt is when the test was first created, i.e. the earliest
// version's timestamp -- the postgres equivalent of
// MIN(created_at) OVER (PARTITION BY catalog_id). Callers must hold the lock.
func (s *TestStore) familyCreatedAt(catalogID string) time.Time {
	var earliest time.Time
	for _, t := range s.tests {
		if t.ID != catalogID {
			continue
		}
		if earliest.IsZero() || t.UpdatedAt.Before(earliest) {
			earliest = t.UpdatedAt
		}
	}
	return earliest
}
```

In `backend/internal/store/memstore/runstore.go`, add the catalog default to `CreateRun` (immediately after `defer s.mu.Unlock()`) and switch `ListByTest` to filter on it:

```go
	if r.TestCatalogID == "" {
		// For an unversioned caller the test id *is* the catalog id, matching
		// the postgres COALESCE fallback.
		r.TestCatalogID = r.TestID
	}
```

```go
func (s *RunStore) ListByTest(ctx context.Context, catalogID string) ([]model.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.Run{}
	for _, r := range s.runs {
		if r.TestCatalogID == catalogID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
```

- [x] **Step 6: Run the memstore tests to verify they pass**

Run: `cd backend && go test ./internal/store/memstore/ -v`
Expected: PASS, including every pre-existing test unchanged.

- [x] **Step 7: Write the failing postgres tests**

Append to `backend/internal/store/postgres/store_test.go`:

```go
func TestUpdateTestCreatesNewVersionAndPinsOldRuns(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	v1 := &model.Test{Name: "pg-versioning", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 10}
	if err := db.CreateTest(ctx, v1); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if v1.Version != 1 || v1.VersionID == "" {
		t.Fatalf("expected v1 with a VersionID, got v%d/%q", v1.Version, v1.VersionID)
	}

	// A run of v1, pinned to the exact version that executed.
	run := &model.Run{TestID: v1.VersionID, TestCatalogID: v1.ID, Status: model.RunCompleted}
	if err := db.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	edit := &model.Test{ID: v1.ID, Name: "pg-versioning", TargetURL: "http://b", VirtualUsers: 2, DurationSeconds: 20}
	if err := db.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}
	if edit.Version != 2 || edit.ID != v1.ID {
		t.Fatalf("expected v2 under the same catalog id, got v%d/%q", edit.Version, edit.ID)
	}

	latest, err := db.GetTest(ctx, v1.ID)
	if err != nil || latest.Version != 2 || latest.TargetURL != "http://b" {
		t.Fatalf("GetTest: %+v, err=%v", latest, err)
	}

	// The pinned run still points at v1, whose config is untouched.
	got, err := db.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.TestID != v1.VersionID {
		t.Fatalf("expected the run to stay pinned to %q, got %q", v1.VersionID, got.TestID)
	}
	if got.TestCatalogID != v1.ID {
		t.Fatalf("expected the run's catalog id to be %q, got %q", v1.ID, got.TestCatalogID)
	}

	versions, err := db.ListTestVersions(ctx, v1.ID)
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("expected newest-first [v2 v1], got %d versions", len(versions))
	}
	if versions[1].TargetURL != "http://a" {
		t.Fatalf("expected v1 to keep http://a, got %q", versions[1].TargetURL)
	}

	// Run history is family-scoped: still found via the catalog id.
	runs, err := db.ListByTest(ctx, v1.ID)
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	found := false
	for _, r := range runs {
		if r.ID == run.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the v1 run to still appear in the test's history after the edit")
	}
}

func TestListTestsReturnsOneRowPerFamily(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "pg-one-row", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	_ = db.CreateTest(ctx, tst)
	edit := &model.Test{ID: tst.ID, Name: "pg-one-row", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	all, err := db.ListTests(ctx)
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	seen := 0
	for _, got := range all {
		if got.ID == tst.ID {
			seen++
			if got.Version != 2 {
				t.Fatalf("expected the latest version, got v%d", got.Version)
			}
			if !got.CreatedAt.Equal(tst.CreatedAt) {
				t.Fatalf("expected the family's original CreatedAt %v, got %v", tst.CreatedAt, got.CreatedAt)
			}
			if !got.UpdatedAt.After(got.CreatedAt) {
				t.Fatalf("expected UpdatedAt after CreatedAt, got %v vs %v", got.UpdatedAt, got.CreatedAt)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("expected exactly 1 row for the family, got %d", seen)
	}
}

func TestUpdateTestConflictsOnDuplicateVersion(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "pg-conflict", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	_ = db.CreateTest(ctx, tst)

	// Claim version 2 directly, simulating the winner of a race.
	if _, err := db.Pool.Exec(ctx,
		`INSERT INTO tests (catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id)
		 VALUES ($1, 2, 'pg-conflict', 'http://race', 1, 1, $2)`,
		tst.ID, tst.ProjectID,
	); err != nil {
		t.Fatalf("seed racing version: %v", err)
	}

	// UpdateTest read version 1 as latest before the race landed, so it also
	// tries to write version 2 and must lose against the unique index.
	err := db.updateTestAtVersion(ctx,
		&model.Test{ID: tst.ID, Name: "pg-conflict", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1},
		2, tst.ProjectID)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestUpdateTestUnknownCatalogIDIsNotFound(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	err := db.UpdateTest(ctx, &model.Test{
		ID: "00000000-0000-0000-0000-000000000000", Name: "x",
		TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1,
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListTestVersionsUnknownIDIsEmptyNotNil(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	versions, err := db.ListTestVersions(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if versions == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(versions) != 0 {
		t.Fatalf("expected 0 versions, got %d", len(versions))
	}
}

func TestMigrateBackfillsVersioningForLegacyRows(t *testing.T) {
	ctx := context.Background()
	db := newScratchDB(t, 2) // pre-0003, pre-0004

	var legacyTestID, legacyRunID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO tests (name, target_url, virtual_users, duration_seconds)
		 VALUES ('legacy', 'http://example.com', 1, 1) RETURNING id`,
	).Scan(&legacyTestID); err != nil {
		t.Fatalf("insert legacy test: %v", err)
	}
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO runs (test_id, status) VALUES ($1, 'completed') RETURNING id`, legacyTestID,
	).Scan(&legacyRunID); err != nil {
		t.Fatalf("insert legacy run: %v", err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var catalogID string
	var version int
	if err := db.Pool.QueryRow(ctx,
		`SELECT catalog_id, version FROM tests WHERE id = $1`, legacyTestID,
	).Scan(&catalogID, &version); err != nil {
		t.Fatalf("read backfilled test: %v", err)
	}
	if catalogID != legacyTestID {
		t.Fatalf("expected catalog_id to be backfilled to the row's own id %q, got %q", legacyTestID, catalogID)
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}

	var runCatalogID string
	if err := db.Pool.QueryRow(ctx,
		`SELECT test_catalog_id FROM runs WHERE id = $1`, legacyRunID,
	).Scan(&runCatalogID); err != nil {
		t.Fatalf("read backfilled run: %v", err)
	}
	if runCatalogID != legacyTestID {
		t.Fatalf("expected the run's test_catalog_id to be %q, got %q", legacyTestID, runCatalogID)
	}

	// The upgraded database must be usable by the new code path too.
	got, err := db.GetTest(ctx, legacyTestID)
	if err != nil {
		t.Fatalf("GetTest after upgrade: %v", err)
	}
	if got.Version != 1 || got.VersionID != legacyTestID {
		t.Fatalf("expected v1 with VersionID %q, got v%d/%q", legacyTestID, got.Version, got.VersionID)
	}
}
```

- [x] **Step 8: Run them to verify they fail**

Run: `cd backend && go test ./internal/store/postgres/ -v`
Expected: FAIL to compile — `db.UpdateTest`, `db.ListTestVersions`, `db.updateTestAtVersion` undefined.

- [x] **Step 9: Implement the postgres side**

In `backend/internal/store/postgres/postgres.go`, add `"errors"` and `"github.com/jackc/pgx/v5/pgconn"` to the imports, then replace `CreateTest`, `ListTests`, `GetTest`, `CreateRun`, `GetRun`, and `ListByTest`, and add the new methods. Note `testColumns`/`scanTest` keep the three version-aware read queries from drifting apart:

```go
// testColumns is the projection every test read shares. catalog_id is exposed
// as the model's ID, and the family's earliest created_at is the test's
// CreatedAt, so editing a test never appears to change when it was created.
const testColumns = `catalog_id, id, version, project_id, name, target_url, virtual_users, duration_seconds,
       MIN(created_at) OVER (PARTITION BY catalog_id) AS catalog_created_at, created_at`

type testScanner interface {
	Scan(dest ...any) error
}

func scanTest(row testScanner, t *model.Test) error {
	return row.Scan(&t.ID, &t.VersionID, &t.Version, &t.ProjectID, &t.Name,
		&t.TargetURL, &t.VirtualUsers, &t.DurationSeconds, &t.CreatedAt, &t.UpdatedAt)
}

// isUniqueViolation reports whether err is SQLSTATE 23505 (unique_violation) --
// here, two concurrent edits racing for the same (catalog_id, version).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (db *DB) CreateTest(ctx context.Context, t *model.Test) error {
	// The id is generated here so the row can reference it as its own
	// catalog_id in a single INSERT.
	id := uuid.NewString()
	return db.Pool.QueryRow(ctx,
		`INSERT INTO tests (id, catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id)
		 VALUES ($1, $1, 1, $2, $3, $4, $5,
		         COALESCE($6, (SELECT id FROM projects WHERE name = 'Default')))
		 RETURNING catalog_id, id, version, project_id, created_at, created_at`,
		id, t.Name, t.TargetURL, t.VirtualUsers, t.DurationSeconds, nullableUUID(t.ProjectID),
	).Scan(&t.ID, &t.VersionID, &t.Version, &t.ProjectID, &t.CreatedAt, &t.UpdatedAt)
}

func (db *DB) ListTests(ctx context.Context) ([]model.Test, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT catalog_id, id, version, project_id, name, target_url, virtual_users,
		        duration_seconds, catalog_created_at, created_at
		 FROM (
		     SELECT DISTINCT ON (catalog_id) `+testColumns+`
		     FROM tests
		     ORDER BY catalog_id, version DESC
		 ) latest
		 ORDER BY catalog_created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Test{}
	for rows.Next() {
		var t model.Test
		if err := scanTest(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) GetTest(ctx context.Context, catalogID string) (*model.Test, error) {
	var t model.Test
	err := scanTest(db.Pool.QueryRow(ctx,
		`SELECT `+testColumns+` FROM tests WHERE catalog_id = $1 ORDER BY version DESC LIMIT 1`,
		catalogID), &t)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &t, err
}

func (db *DB) ListTestVersions(ctx context.Context, catalogID string) ([]model.Test, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT `+testColumns+` FROM tests WHERE catalog_id = $1 ORDER BY version DESC`, catalogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Test{}
	for rows.Next() {
		var t model.Test
		if err := scanTest(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) UpdateTest(ctx context.Context, t *model.Test) error {
	latest, err := db.GetTest(ctx, t.ID)
	if err != nil {
		return err // ErrNotFound propagates
	}
	return db.updateTestAtVersion(ctx, t, latest.Version+1, latest.ProjectID)
}

// updateTestAtVersion inserts t as an explicit version number. The unique index
// on (catalog_id, version) is what turns a lost read-then-write race into
// ErrConflict instead of a silently forked version.
func (db *DB) updateTestAtVersion(ctx context.Context, t *model.Test, version int, projectID string) error {
	versionID := uuid.NewString()
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO tests (id, catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, version, created_at`,
		versionID, t.ID, version, t.Name, t.TargetURL, t.VirtualUsers, t.DurationSeconds, projectID,
	).Scan(&t.VersionID, &t.Version, &t.UpdatedAt)
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	if err != nil {
		return err
	}
	t.ProjectID = projectID
	// Refresh CreatedAt so it reports the family's first creation, not this
	// version's.
	if refreshed, err := db.GetTest(ctx, t.ID); err == nil {
		t.CreatedAt = refreshed.CreatedAt
	}
	return nil
}

func (db *DB) CreateRun(ctx context.Context, r *model.Run) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO runs (test_id, test_catalog_id, status)
		 VALUES ($1, COALESCE($2, (SELECT catalog_id FROM tests WHERE id = $1)), $3)
		 RETURNING id, test_catalog_id, created_at`,
		r.TestID, nullableUUID(r.TestCatalogID), r.Status,
	).Scan(&r.ID, &r.TestCatalogID, &r.CreatedAt)
}

func (db *DB) GetRun(ctx context.Context, id string) (*model.Run, error) {
	var r model.Run
	err := db.Pool.QueryRow(ctx,
		`SELECT id, test_id, test_catalog_id, status, created_at, started_at, completed_at, error_message
		 FROM runs WHERE id = $1`, id,
	).Scan(&r.ID, &r.TestID, &r.TestCatalogID, &r.Status, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &r, err
}

func (db *DB) ListByTest(ctx context.Context, catalogID string) ([]model.Run, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, test_id, test_catalog_id, status, created_at, started_at, completed_at, error_message
		 FROM runs WHERE test_catalog_id = $1 ORDER BY created_at DESC`, catalogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		var r model.Run
		if err := rows.Scan(&r.ID, &r.TestID, &r.TestCatalogID, &r.Status, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

Add `"github.com/google/uuid"` to the imports.

- [x] **Step 10: Pin the executed version when starting a run**

In `backend/internal/api/runs.go`, change the one line in `handleStartRun` that builds the run:

```go
	run := &model.Run{TestID: test.VersionID, TestCatalogID: test.ID, Status: model.RunPending}
```

- [x] **Step 11: Run the whole backend suite**

Run: `cd backend && go test ./...`
Expected: PASS — every new test plus every pre-existing memstore, postgres, and api test unchanged.

- [x] **Step 12: Commit**

```bash
git add backend/internal/model/model.go backend/internal/store/store.go \
        backend/internal/store/postgres/migrations/0004_test_versioning.sql \
        backend/internal/store/postgres/postgres.go backend/internal/store/postgres/store_test.go \
        backend/internal/store/memstore/memstore.go backend/internal/store/memstore/memstore_test.go \
        backend/internal/store/memstore/runstore.go backend/internal/store/memstore/runstore_test.go \
        backend/internal/api/runs.go
git commit -m "feat(backend): version tests copy-on-write and pin runs to the executed version"
```

---

### Task 4: API endpoints

**Files:**
- Modify: `backend/internal/api/server.go`
- Modify: `backend/internal/api/tests.go`
- Create: `backend/internal/api/projects.go`
- Modify: `backend/cmd/server/main.go`
- Test: `backend/internal/api/tests_test.go`, `backend/internal/api/runs_test.go` (helper only)

**Interfaces:**
- Consumes: `store.ProjectStore` (Task 2); `store.ErrConflict`, `TestStore.UpdateTest`, `TestStore.ListTestVersions`, `model.Test.VersionID`/`Version` (Task 3).
- Produces: `api.NewServer(testStore store.TestStore, runStore store.RunStore, projectStore store.ProjectStore, k8sClient kubernetes.Interface, jobCfg k8sjob.Config) *Server` — note the **third** parameter is new. Routes `PUT /api/tests/{testID}`, `GET /api/tests/{testID}/versions`, `GET /api/projects`.

- [x] **Step 1: Write the failing handler tests**

Append to `backend/internal/api/tests_test.go`:

```go
func createTestViaAPI(t *testing.T, s *Server, name string) model.Test {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name": name, "target_url": "http://example.com",
		"virtual_users": 1, "duration_seconds": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tests", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed test: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode seeded test: %v", err)
	}
	return created
}

func TestUpdateTestCreatesANewVersion(t *testing.T) {
	s := newTestServer()
	created := createTestViaAPI(t, s, "editable")

	body, _ := json.Marshal(map[string]any{
		"name": "editable", "target_url": "http://changed",
		"virtual_users": 7, "duration_seconds": 70,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
	if updated.ID != created.ID {
		t.Fatalf("expected a stable catalog id %q, got %q", created.ID, updated.ID)
	}
	if updated.TargetURL != "http://changed" {
		t.Fatalf("expected the edited target url, got %q", updated.TargetURL)
	}
}

func TestUpdateTestValidatesBodyAndID(t *testing.T) {
	s := newTestServer()
	created := createTestViaAPI(t, s, "validated")

	// Malformed JSON.
	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, bytes.NewReader([]byte("{")))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed json, got %d", rec.Code)
	}

	// Valid JSON, invalid values.
	bad, _ := json.Marshal(map[string]any{
		"name": "", "target_url": "", "virtual_users": 0, "duration_seconds": 0,
	})
	req2 := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, bytes.NewReader(bad))
	rec2 := httptest.NewRecorder()
	s.Router().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid values, got %d", rec2.Code)
	}

	// Unknown test.
	good, _ := json.Marshal(map[string]any{
		"name": "x", "target_url": "http://example.com",
		"virtual_users": 1, "duration_seconds": 1,
	})
	req3 := httptest.NewRequest(http.MethodPut, "/api/tests/missing", bytes.NewReader(good))
	rec3 := httptest.NewRecorder()
	s.Router().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown test, got %d", rec3.Code)
	}
}

func TestListTestVersionsReturnsNewestFirst(t *testing.T) {
	s := newTestServer()
	created := createTestViaAPI(t, s, "history")

	body, _ := json.Marshal(map[string]any{
		"name": "history", "target_url": "http://v2",
		"virtual_users": 1, "duration_seconds": 1,
	})
	putReq := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, bytes.NewReader(body))
	s.Router().ServeHTTP(httptest.NewRecorder(), putReq)

	req := httptest.NewRequest(http.MethodGet, "/api/tests/"+created.ID+"/versions", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var versions []model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("expected newest-first, got v%d then v%d", versions[0].Version, versions[1].Version)
	}
}

func TestListTestVersionsUnknownTestIs404(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/tests/missing/versions", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListProjectsReturnsTheDefaultProject(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var projects []model.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &projects); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "Default" {
		t.Fatalf("expected exactly the Default project, got %+v", projects)
	}
}

func TestCreatedTestBelongsToTheDefaultProject(t *testing.T) {
	s := newTestServer()
	created := createTestViaAPI(t, s, "projected")

	if created.ProjectID == "" {
		t.Fatal("expected the created test to carry a project id")
	}
	if created.Version != 1 {
		t.Fatalf("expected version 1, got %d", created.Version)
	}
	if created.VersionID == "" {
		t.Fatal("expected a version_id")
	}
}
```

- [x] **Step 2: Run them to verify they fail**

Run: `cd backend && go test ./internal/api/ -v`
Expected: FAIL — the `PUT`, `/versions`, and `/projects` requests return 404/405 because no route exists yet.

- [x] **Step 3: Add the project handler**

Create `backend/internal/api/projects.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.projectStore.ListProjects(r.Context())
	if err != nil {
		http.Error(w, "failed to list projects", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}
```

- [x] **Step 4: Add the test update and versions handlers**

Replace `backend/internal/api/tests.go` entirely:

```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

// testRequest is shared by create and update. ProjectID is only honoured on
// create -- moving a test between projects belongs to the project registry
// work (BOL-49), so an update inherits the family's existing project.
type testRequest struct {
	Name            string `json:"name"`
	TargetURL       string `json:"target_url"`
	VirtualUsers    int    `json:"virtual_users"`
	DurationSeconds int    `json:"duration_seconds"`
	ProjectID       string `json:"project_id"`
}

func (req testRequest) valid() bool {
	return req.Name != "" && req.TargetURL != "" && req.VirtualUsers > 0 && req.DurationSeconds > 0
}

const testValidationMessage = "name, target_url, virtual_users>0, duration_seconds>0 are required"

func (s *Server) handleCreateTest(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !req.valid() {
		http.Error(w, testValidationMessage, http.StatusBadRequest)
		return
	}
	t := &model.Test{
		ProjectID:       req.ProjectID,
		Name:            req.Name,
		TargetURL:       req.TargetURL,
		VirtualUsers:    req.VirtualUsers,
		DurationSeconds: req.DurationSeconds,
	}
	if err := s.testStore.CreateTest(r.Context(), t); err != nil {
		http.Error(w, "failed to create test", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

func (s *Server) handleListTests(w http.ResponseWriter, r *http.Request) {
	tests, err := s.testStore.ListTests(r.Context())
	if err != nil {
		http.Error(w, "failed to list tests", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tests)
}

// handleUpdateTest records an edit as a new immutable version rather than
// mutating the current one, so runs of earlier versions keep their exact
// configuration.
func (s *Server) handleUpdateTest(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !req.valid() {
		http.Error(w, testValidationMessage, http.StatusBadRequest)
		return
	}
	t := &model.Test{
		ID:              chi.URLParam(r, "testID"),
		Name:            req.Name,
		TargetURL:       req.TargetURL,
		VirtualUsers:    req.VirtualUsers,
		DurationSeconds: req.DurationSeconds,
	}
	err := s.testStore.UpdateTest(r.Context(), t)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "test not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrConflict):
		// A concurrent edit already claimed the next version number. The
		// request was valid; the client may retry against the new latest.
		http.Error(w, "test was modified concurrently; reload and retry", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "failed to update test", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func (s *Server) handleListTestVersions(w http.ResponseWriter, r *http.Request) {
	testID := chi.URLParam(r, "testID")
	if _, err := s.testStore.GetTest(r.Context(), testID); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "test not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to load test", http.StatusInternalServerError)
		return
	}
	versions, err := s.testStore.ListTestVersions(r.Context(), testID)
	if err != nil {
		http.Error(w, "failed to list versions", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions)
}
```

- [x] **Step 5: Wire the routes and the new store**

In `backend/internal/api/server.go`, add the field, the parameter, and the routes:

```go
type Server struct {
	router       chi.Router
	testStore    store.TestStore
	runStore     store.RunStore
	projectStore store.ProjectStore
	k8sClient    kubernetes.Interface
	jobCfg       k8sjob.Config
}

func NewServer(testStore store.TestStore, runStore store.RunStore, projectStore store.ProjectStore, k8sClient kubernetes.Interface, jobCfg k8sjob.Config) *Server {
	s := &Server{
		router:       chi.NewRouter(),
		testStore:    testStore,
		runStore:     runStore,
		projectStore: projectStore,
		k8sClient:    k8sClient,
		jobCfg:       jobCfg,
	}
	s.router.Use(corsMiddleware)
	s.router.Get("/healthz", s.handleHealthz)
	s.router.Get("/api/projects", s.handleListProjects)
	s.router.Post("/api/tests", s.handleCreateTest)
	s.router.Get("/api/tests", s.handleListTests)
	s.router.Put("/api/tests/{testID}", s.handleUpdateTest)
	s.router.Get("/api/tests/{testID}/versions", s.handleListTestVersions)
	s.router.Post("/api/tests/{testID}/runs", s.handleStartRun)
	s.router.Get("/api/tests/{testID}/runs", s.handleListRunsForTest)
	s.router.Post("/api/runs/{runID}/metrics", s.handlePostMetrics)
	s.router.Get("/api/runs/{runID}", s.handleGetRun)
	s.router.Post("/api/runs/{runID}/cancel", s.handleCancelRun)
	return s
}
```

Add `PUT` to the CORS `Access-Control-Allow-Methods` header so a browser can call the new route:

```go
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
```

In `backend/internal/api/runs_test.go`, update the helper (its only change):

```go
func newTestServer() *Server {
	ts := memstore.NewTestStore()
	rs := memstore.NewRunStore()
	ps := memstore.NewProjectStore()
	fakeClient := k8sfake.NewSimpleClientset()
	cfg := k8sjob.Config{Namespace: "boltrunner", JMeterImage: "jmeter:local", SidecarImage: "sidecar:local", BackendURL: "http://backend:8080"}
	return NewServer(ts, rs, ps, fakeClient, cfg)
}
```

In `backend/cmd/server/main.go`, pass the DB as the project store too — it satisfies all three interfaces:

```go
	s := api.NewServer(db, db, db, k8sClient, jobCfg)
```

- [x] **Step 6: Run the API tests to verify they pass**

Run: `cd backend && go test ./internal/api/ -v`
Expected: PASS, including every pre-existing api test unchanged.

- [x] **Step 7: Run the whole backend suite and build**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS.

- [x] **Step 8: Commit**

```bash
git add backend/internal/api/server.go backend/internal/api/tests.go backend/internal/api/projects.go \
        backend/internal/api/tests_test.go backend/internal/api/runs_test.go backend/cmd/server/main.go
git commit -m "feat(backend): expose test versions and the project registry over HTTP"
```

---

### Task 5: Verification against a real Postgres

**Files:** none — this task changes no code. It exists because the migrations contain data backfills whose only honest test is a real database, and because the frontend must be proven untouched.

**Interfaces:**
- Consumes: everything from Tasks 1-4.
- Produces: no code. A verification record for the final review.

- [x] **Step 1: Port-forward the dev Postgres and create a dedicated test database**

The dev cluster already runs Postgres. Forward it and make a database **separate from the shared `boltrunner` database** — never point tests at that one (see Global Constraints).

```bash
kubectl -n boltrunner port-forward svc/boltrunner-postgres 5432:5432 &
sleep 3
kubectl -n boltrunner exec deploy/boltrunner-postgres -- \
  psql -U boltrunner -d boltrunner -c "CREATE DATABASE boltrunner_bol28_test"
```

Expected: `CREATE DATABASE`. If it already exists, `psql` reports an error that is safe to ignore.

- [x] **Step 2: Run the full backend suite against it**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5432/boltrunner_bol28_test" go test ./... -count=1
```

Expected: PASS with no skips in `internal/store/postgres`. This is the run that actually exercises the migrations, the `DISTINCT ON` + window-function queries, the `23505` conflict path, and the legacy-upgrade backfills in `newScratchDB`.

- [x] **Step 3: Confirm coverage still clears the gate**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5432/boltrunner_bol28_test" \
  go test ./... -count=1 -coverprofile=/tmp/bol28.out && go tool cover -func=/tmp/bol28.out | tail -1
```

Expected: total coverage ≥ 88%. If short, add tests for the uncovered branches — do not lower the threshold.

- [x] **Step 4: Prove the frontend is untouched**

```bash
git diff --stat 576b364..HEAD -- frontend/
```

Expected: **empty output.** Any file listed means the "frontend is not touched" constraint was violated and must be reverted.

```bash
cd frontend && npx vitest run
```

Expected: PASS, the same count as before this plan began (103 tests / 24 files).

- [x] **Step 5: Record what is verified locally vs. in CI**

Do **not** attempt to migrate the shared dev database or redeploy the in-cluster backend image. `kind` is not installed on this machine (`kind: command not found`), so the image cannot be rebuilt or loaded locally, and migrating the shared database while the old image is deployed would break it — the new `NOT NULL` columns have no defaults, so the old binary's inserts would fail (see Global Constraints).

This is a gap in *local* verification only. `.github/workflows/ci.yml` covers the rest:
* `backend-unit` runs a `postgres:16` service and sets `BOLTRUNNER_TEST_DSN`, so the migrations, the window-function queries, the conflict path, and the `newScratchDB` upgrade tests all execute in CI — and count toward the 88% gate there.
* `integration-kind` builds all three images, loads them with `kind`, deploys, and runs the browser e2e against the **new** backend. That is where end-to-end verification happens.

Write a short note in the task report stating plainly which checks ran locally, that browser e2e against the new backend is delegated to CI's `integration-kind` job rather than run here, and that the frontend is provably unchanged. Do not claim end-to-end verification that did not happen locally — and equally, do not claim it is impossible, because CI performs it.

- [x] **Step 6: Clean up**

```bash
kubectl -n boltrunner exec deploy/boltrunner-postgres -- \
  psql -U boltrunner -d boltrunner -c "DROP DATABASE IF EXISTS boltrunner_bol28_test"
pkill -f "port-forward svc/boltrunner-postgres"
```

Expected: `DROP DATABASE`. Scratch databases created by `newScratchDB` drop themselves via `t.Cleanup`; if a crashed run left any behind, list them with `SELECT datname FROM pg_database WHERE datname LIKE 'boltrunner_scratch_%'` and drop them.

---

## Self-review notes

- **Spec coverage.** Migration tracking → Task 1. Projects table, `ProjectStore`, `tests.project_id` → Task 2. Versioning schema, model, `ErrConflict`, `UpdateTest`/`ListTestVersions`, family-scoped run history, run pinning → Task 3. `PUT`/`versions`/`projects` routes, optional `project_id` on create, `NewServer` wiring → Task 4. Real-database verification of the backfills and the coverage gate → Task 5. The spec's "out of scope" list is respected: no scenarios, no load profiles, no project CRUD beyond list, no frontend change, no RBAC, no delete/archive.
- **Placeholder scan.** None — every step names exact files, exact code, exact commands, and expected output.
- **Type consistency.** `model.Test.ID` is the catalog id everywhere; `VersionID` is the version PK everywhere. `GetTest`/`ListTestVersions`/`ListByTest` all take a catalog id. `NewServer`'s new `projectStore` is the third parameter in the definition (Task 4 Step 5), in `newTestServer` (Step 5), and in `main.go` (Step 5) — all consistent. `store.ErrConflict` is defined in Task 3 and consumed in Tasks 3 and 4. `newScratchDB(t, maxVersion)` is defined in Task 2 Step 7 and reused in Task 3 Step 7 with the same signature. `nullableUUID` is introduced in Task 2 Step 9 and reused by `CreateRun` in Task 3 Step 9.
- **Ordering.** Task 3 makes `runs.test_catalog_id` `NOT NULL` and in the same task updates `handleStartRun` plus both `CreateRun` implementations to populate it, so no task leaves the system unable to start a run.

---

## Verification record (2026-07-28)

**Verified locally**, against the dev-cluster Postgres via port-forward, on a
dedicated `boltrunner_bol28_test` database (never the shared `boltrunner` one):

* `go test ./... -count=1` — **PASS, zero skips** across every package. 36 tests
  ran in `internal/store/postgres`, so the migrations, the backfills, the
  `DISTINCT ON` + window-function queries and the `23505` conflict path all
  actually executed rather than being skipped.
* Coverage **89.0%**, clearing the 88% gate. Computed exactly as
  `.github/workflows/ci.yml` does it (`go test ./... -coverprofile` →
  `go tool cover -func | grep total:`).
* Re-run against a **freshly created empty** database with a
  `?sslmode=disable` query string, mirroring CI's service container and DSN:
  identical PASS and identical 89.0%. This is what exercises `replaceDBName`'s
  query-string preservation, which the local DSN alone would not.
* `gofmt -l` clean, `go vet ./...` clean.
* Frontend untouched: `git diff 5708923..HEAD -- frontend/` is **empty** and
  no `frontend/` file is modified in the working tree. `npx vitest run` →
  **103 passed / 24 files**, unchanged.
* Shared dev database confirmed **not migrated** — `public` still holds only
  `tests, runs, run_metric_snapshots`, and `tests` still has the pre-0003
  column set. The deployed old backend image is therefore unaffected.
* No leaked scratch databases (`boltrunner_scratch_%` empty after the run), and
  both `boltrunner_bol28_*` databases were dropped afterwards.

**Manual smoke test of handlers against real Postgres.** The API tests use
memstore, so "real handlers + real Postgres" is otherwise untested. A server
was run against the test database and exercised by hand: `GET /api/projects`,
`POST /api/tests`, `PUT /api/tests/{id}` (v1 → v2 with a new `version_id` and a
preserved family `created_at`), `GET /api/tests/{id}/versions` (newest first),
unknown and malformed `project_id` → **400**, unknown catalog id on `PUT` →
**404**. `GET /api/tests` confirmed that a family edited later still does not
overtake a family created after it.

**Not verified locally, delegated to CI.** Browser e2e against the new backend
was **not** run here: `kind` is not installed on this machine, so the images
cannot be rebuilt or loaded, and migrating the shared dev database while the
old image is deployed would break it. CI's `integration-kind` job builds all
three images, deploys them and runs the browser e2e against the new backend —
that is where end-to-end verification happens. CI's `backend-unit` job
independently re-runs everything above against a clean `postgres:16`.

**Plan correction.** Task 5 Step 4 named `576b364` as the frontend baseline,
but that commit sits mid-way through the *previous* (responsive-portal) plan,
so its diff shows that plan's frontend work and can never be empty. The true
BOL-28 baseline is `5708923` (parent of the first BOL-28 commit `024d61d`),
against which the frontend diff is empty as the constraint requires.
