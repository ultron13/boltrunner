# Project Registry CRUD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename and delete projects, and move a test between projects — per `docs/superpowers/specs/2026-08-03-project-registry-crud-design.md`.

**Architecture:** A new `is_default` column replaces the by-name lookup that currently makes renaming unsafe. Rename and delete are new `ProjectStore` methods; moving is a new `TestStore.MoveTest` that refiles every version of a family. The "is this project empty" check lives in the *handler*, not the store, which is what keeps the memstore dependency one-way (`TestStore → ProjectStore`) and avoids a cycle. Rename/delete UI goes on `/admin`; the move control goes on the test detail page.

**Tech Stack:** Go 1.26, chi v5, pgx v5, PostgreSQL 16. Next.js App Router (client components), React 18, TypeScript `strict`, Vitest + Testing Library, Playwright. No new dependencies in either half.

## Global Constraints

- **Coverage gates: 88%** on both sides. Backend is enforced in `.github/workflows/ci.yml` against `go tool cover -func` total; frontend on lines, statements, functions and branches (`frontend/vitest.config.ts`). Neither is to be lowered. Frontend headroom before this plan: 97.5 / 93.8 / 97.94 / 98.99.
- **Run frontend commands from `frontend/`.** `npx vitest` from the repository root picks up a different vite and fails to parse JSX; the failure reads as a syntax error in the test file rather than a wrong-directory mistake.
- **Run backend commands from `backend/`.**
- **Postgres store tests need a live database.** They skip silently without `BOLTRUNNER_TEST_DSN`. A skipped test is not a passing test — every task that adds one states how to bring a database up, and you must see it run.
- **`GET /api/tests` without `project_id` stays unfiltered.** Three e2e specs and the Go integration test depend on it.
- **`POST /api/tests` without `project_id` must keep working.** It is how `walking-skeleton.spec.ts` and `internal/integration/walking_skeleton_test.go` create tests. Task 1 changes how the fallback project is *found*, never whether there is one.
- **Exactly one project carries `is_default`.** Enforced by a partial unique index, not by convention. Nothing may insert a second.
- **Names are trimmed before validation and before the uniqueness check.** Existing behavior on create (`projects.go:37-47`); rename must match it, because a project renamed to `" Payments "` would otherwise be a second name the user cannot tell apart from `Payments` in the switcher.

---

### Task 1: The `is_default` flag

Replaces the `WHERE name = 'Default'` lookup at `postgres.go:199` — the line that makes renaming a project break test creation.

**Files:**
- Create: `backend/internal/store/postgres/migrations/0005_project_default_flag.sql`
- Create: `backend/internal/store/postgres/migrate_test.go`
- Modify: `backend/internal/model/model.go:28-32`
- Modify: `backend/internal/store/postgres/postgres.go:199` (the COALESCE), `:333-359` (both project queries)
- Modify: `backend/internal/store/memstore/projectstore.go:30-34`

**Interfaces:**
- Produces: `model.Project.IsDefault bool` (`json:"is_default"`), read by Tasks 2, 4, 5 and 6. Postgres migration version `5`.
- Consumes: nothing.

- [ ] **Step 1: Write the migration**

Create `backend/internal/store/postgres/migrations/0005_project_default_flag.sql`:

```sql
ALTER TABLE projects ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX IF NOT EXISTS projects_one_default ON projects (is_default) WHERE is_default;

-- 0003 seeds 'Default', so an empty projects table should be unreachable. Seeding
-- anyway costs one statement and removes the failure mode outright: with no flagged
-- row, CreateTest's COALESCE yields NULL against a NOT NULL column, and every
-- project-less test creation starts failing.
INSERT INTO projects (name, is_default)
SELECT 'Default', true WHERE NOT EXISTS (SELECT 1 FROM projects);

-- Flag the project named 'Default' when there is one, else the oldest -- rather
-- than matching on the name alone, which silently flags nothing if the row was
-- ever renamed by hand. The NOT EXISTS guard makes a re-run a no-op instead of a
-- unique violation against the index above.
UPDATE projects SET is_default = true
WHERE id = (
    SELECT id FROM projects
    ORDER BY (name = 'Default') DESC, created_at ASC, id ASC
    LIMIT 1
)
AND NOT EXISTS (SELECT 1 FROM projects WHERE is_default);
```

No Go change is needed to *run* it: `Migrate` discovers `migrations/*.sql` by filename and parses the version prefix (`postgres.go:87-114`).

- [ ] **Step 2: Add the model field**

In `backend/internal/model/model.go`, extend `Project`:

```go
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	// IsDefault marks the project that an omitted project_id falls back to.
	// Exactly one project carries it, enforced by a partial unique index.
	IsDefault bool `json:"is_default"`
}
```

- [ ] **Step 3: Resolve the fallback by flag, and return the field**

In `backend/internal/store/postgres/postgres.go`, change the `CreateTest` COALESCE (currently line 199) from `WHERE name = 'Default'` to:

```go
		         COALESCE($6, (SELECT id FROM projects WHERE is_default)))
```

Change `ListProjects` to select and scan the column:

```go
func (db *DB) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, created_at, is_default FROM projects ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Project{}
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.IsDefault); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
```

And `CreateProject`, so a newly created project reports `is_default: false` rather than the zero value by luck:

```go
func (db *DB) CreateProject(ctx context.Context, p *model.Project) error {
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO projects (name) VALUES ($1) RETURNING id, created_at, is_default`,
		p.Name,
	).Scan(&p.ID, &p.CreatedAt, &p.IsDefault)
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	return err
}
```

- [ ] **Step 4: Flag memstore's seeded project**

In `backend/internal/store/memstore/projectstore.go`, `NewProjectStore`:

```go
func NewProjectStore() *ProjectStore {
	return &ProjectStore{projects: map[string]model.Project{
		DefaultProjectID: {ID: DefaultProjectID, Name: DefaultProjectName, CreatedAt: time.Now().UTC(), IsDefault: true},
	}}
}
```

- [ ] **Step 5: Write the migration tests**

Create `backend/internal/store/postgres/migrate_test.go`. These run against a real Postgres and each works in its own schema, so they cannot collide with each other or with `TestConnectAndMigrate`:

```go
package postgres

import (
	"context"
	"fmt"
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
	if fmt.Sprint(got.VersionID) != got.ID {
		t.Fatalf("expected the backfilled version row to keep its id, got %q", got.VersionID)
	}
}
```

- [ ] **Step 6: Bring up a database and run them**

```bash
docker run -d --rm --name br-pg-test -e POSTGRES_USER=boltrunner -e POSTGRES_PASSWORD=boltrunner -e POSTGRES_DB=boltrunner -p 5433:5432 postgres:16
sleep 8
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./internal/store/postgres/... -run 'TestMigrat' -v
```

Expected: four tests, all PASS, **none skipped**. Port 5433 is deliberate — it avoids colliding with a Postgres already on 5432. Leave the container up; Tasks 2 and 3 use it. Tear down with `docker rm -f br-pg-test` at the end of Task 3.

If you see `SKIP`, the DSN did not reach the test — fix that before continuing, because a skipped migration test proves nothing.

- [ ] **Step 7: Run the whole backend suite**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./...
```

Expected: PASS across all 11 packages. `memstore` and `api` do not touch Postgres and must be unaffected.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/store/postgres/migrations/0005_project_default_flag.sql \
        backend/internal/store/postgres/migrate_test.go \
        backend/internal/store/postgres/postgres.go \
        backend/internal/store/memstore/projectstore.go \
        backend/internal/model/model.go
git commit -m "feat(backend): identify the fallback project by flag, not by name"
```

---

### Task 2: Rename and delete in the project stores

**Files:**
- Modify: `backend/internal/store/store.go:10-17` (sentinels), `:46-51` (interface)
- Modify: `backend/internal/store/postgres/postgres.go` (append after `CreateProject`)
- Modify: `backend/internal/store/memstore/projectstore.go` (append)
- Test: `backend/internal/store/memstore/projectstore_test.go`, `backend/internal/store/postgres/store_test.go`

**Interfaces:**
- Consumes: `model.Project.IsDefault` from Task 1.
- Produces:
  - `store.ErrProtected`, `store.ErrNotEmpty`
  - `ProjectStore.RenameProject(ctx context.Context, id, name string) (*model.Project, error)`
  - `ProjectStore.DeleteProject(ctx context.Context, id string) error`

  Task 4 consumes both methods and both sentinels.

- [ ] **Step 1: Add the sentinels and extend the interface**

In `backend/internal/store/store.go`, add to the `var` block:

```go
	// ErrProtected means the operation targets a row the system depends on --
	// today, only the default project, which is what an omitted project_id
	// falls back to.
	ErrProtected = errors.New("protected")
	// ErrNotEmpty means a project still has tests filed under it. Handlers check
	// this up front so they can report a count; postgres also surfaces it from
	// the tests.project_id foreign key if a delete races that check.
	ErrNotEmpty = errors.New("not empty")
```

Extend `ProjectStore`:

```go
type ProjectStore interface {
	ListProjects(ctx context.Context) ([]model.Project, error)
	// CreateProject assigns p.ID and p.CreatedAt. It returns ErrConflict if a
	// project with the same name already exists.
	CreateProject(ctx context.Context, p *model.Project) error
	// RenameProject returns the updated project. ErrNotFound if no project has
	// that id; ErrConflict if another project already holds the name.
	RenameProject(ctx context.Context, id, name string) (*model.Project, error)
	// DeleteProject removes a project. ErrNotFound if no project has that id;
	// ErrProtected if it is the default project. It does not count tests --
	// the handler does that so it can report how many. Postgres may still
	// return ErrNotEmpty from the foreign key if a delete races that count.
	DeleteProject(ctx context.Context, id string) error
}
```

- [ ] **Step 2: Write the failing memstore tests**

Append to `backend/internal/store/memstore/projectstore_test.go`:

```go
func TestRenameProjectChangesTheName(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	if err := s.CreateProject(context.Background(), p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := s.RenameProject(context.Background(), p.ID, "Billing")
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if got.Name != "Billing" || got.ID != p.ID {
		t.Fatalf("unexpected project: %+v", got)
	}

	list, _ := s.ListProjects(context.Background())
	for _, l := range list {
		if l.ID == p.ID && l.Name != "Billing" {
			t.Fatalf("rename did not persist: %+v", l)
		}
	}
}

func TestRenameProjectReturnsConflictForATakenName(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	s.CreateProject(context.Background(), p)

	// "Default" is seeded, so renaming onto it must conflict.
	if _, err := s.RenameProject(context.Background(), p.ID, DefaultProjectName); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

// Renaming a project to the name it already has is a no-op, not a conflict
// with itself.
func TestRenameProjectToItsOwnNameSucceeds(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	s.CreateProject(context.Background(), p)

	if _, err := s.RenameProject(context.Background(), p.ID, "Payments"); err != nil {
		t.Fatalf("expected renaming to the same name to succeed, got %v", err)
	}
}

func TestRenameProjectReturnsNotFoundForAnUnknownID(t *testing.T) {
	s := NewProjectStore()
	if _, err := s.RenameProject(context.Background(), "no-such-id", "Billing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// Rename must preserve the flag: renaming the default project is the whole
// point of Task 1, and a rename that cleared it would break test creation.
func TestRenameProjectPreservesTheDefaultFlag(t *testing.T) {
	s := NewProjectStore()
	got, err := s.RenameProject(context.Background(), DefaultProjectID, "Shared")
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if !got.IsDefault {
		t.Fatal("renaming the default project must not clear is_default")
	}
}

func TestDeleteProjectRemovesIt(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	s.CreateProject(context.Background(), p)

	if err := s.DeleteProject(context.Background(), p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	list, _ := s.ListProjects(context.Background())
	for _, l := range list {
		if l.ID == p.ID {
			t.Fatal("expected the project to be gone")
		}
	}
}

func TestDeleteProjectRefusesTheDefault(t *testing.T) {
	s := NewProjectStore()
	if err := s.DeleteProject(context.Background(), DefaultProjectID); !errors.Is(err, store.ErrProtected) {
		t.Fatalf("expected ErrProtected, got %v", err)
	}
}

func TestDeleteProjectReturnsNotFoundForAnUnknownID(t *testing.T) {
	s := NewProjectStore()
	if err := s.DeleteProject(context.Background(), "no-such-id"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

Make sure the file imports `errors` and `github.com/boltrunner/backend/internal/store`; add them if they are not already there.

- [ ] **Step 3: Run to verify they fail**

```bash
cd backend && go test ./internal/store/memstore/... -run 'Rename|Delete'
```

Expected: compile failure — `s.RenameProject undefined`. That is the failure; a compile error here is the honest form of "not implemented yet".

- [ ] **Step 4: Implement in memstore**

Append to `backend/internal/store/memstore/projectstore.go`:

```go
func (s *ProjectStore) RenameProject(ctx context.Context, id, name string) (*model.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	for _, existing := range s.projects {
		// Excluding the project itself: renaming "Payments" to "Payments" is a
		// no-op, not a conflict with its own row.
		if existing.ID != id && existing.Name == name {
			return nil, store.ErrConflict
		}
	}
	p.Name = name
	s.projects[id] = p
	return &p, nil
}

func (s *ProjectStore) DeleteProject(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok {
		return store.ErrNotFound
	}
	if p.IsDefault {
		return store.ErrProtected
	}
	delete(s.projects, id)
	return nil
}

// exists reports whether id names a registered project. TestStore calls it to
// validate a project reference. The dependency is deliberately one-way --
// ProjectStore never calls into TestStore -- so holding TestStore.mu across
// this call cannot deadlock.
func (s *ProjectStore) exists(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.projects[id]
	return ok
}
```

`exists` is unused until Task 3. Go permits unused methods (unlike unused locals), so this compiles.

- [ ] **Step 5: Run the memstore tests**

```bash
cd backend && go test ./internal/store/memstore/... -run 'Rename|Delete' -v
```

Expected: PASS, 8 tests.

- [ ] **Step 6: Implement in postgres**

Append to `backend/internal/store/postgres/postgres.go`, after `CreateProject`:

```go
func (db *DB) RenameProject(ctx context.Context, id, name string) (*model.Project, error) {
	// Reject a malformed id before pgx tries to encode it, for the same reason
	// CreateTest does: an encode failure is indistinguishable by type from a
	// genuine connection failure, and would report bad input as an outage.
	if _, err := uuid.Parse(id); err != nil {
		return nil, store.ErrNotFound
	}
	var p model.Project
	err := db.Pool.QueryRow(ctx,
		`UPDATE projects SET name = $2 WHERE id = $1 RETURNING id, name, created_at, is_default`,
		id, name,
	).Scan(&p.ID, &p.Name, &p.CreatedAt, &p.IsDefault)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if isUniqueViolation(err) {
		return nil, store.ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (db *DB) DeleteProject(ctx context.Context, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return store.ErrNotFound
	}
	// Read the flag first so a protected project is reported as such rather
	// than as a delete that matched no rows -- DELETE ... WHERE NOT is_default
	// would collapse "not found" and "protected" into the same zero row count.
	var isDefault bool
	err := db.Pool.QueryRow(ctx, `SELECT is_default FROM projects WHERE id = $1`, id).Scan(&isDefault)
	if err == pgx.ErrNoRows {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	if isDefault {
		return store.ErrProtected
	}
	tag, err := db.Pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	// The tests.project_id foreign key is the authoritative backstop when a
	// delete races the handler's emptiness check.
	if isForeignKeyViolation(err) {
		return store.ErrNotEmpty
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}
```

- [ ] **Step 7: Write the postgres store tests**

Append to `backend/internal/store/postgres/store_test.go`. It already has `setupDB(t *testing.T) *DB` at line 16, which connects, migrates and skips when `BOLTRUNNER_TEST_DSN` is unset — use it, do not add a second helper.

```go
func TestPostgresRenameProject(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	p := &model.Project{Name: "Payments " + uuid.NewString()}
	if err := db.CreateProject(ctx, p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	renamed := "Billing " + uuid.NewString()
	got, err := db.RenameProject(ctx, p.ID, renamed)
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if got.Name != renamed {
		t.Fatalf("expected %q, got %q", renamed, got.Name)
	}
	if got.IsDefault {
		t.Fatal("a non-default project must not become default by being renamed")
	}
}

func TestPostgresRenameProjectConflictsOnATakenName(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	a := &model.Project{Name: "A " + uuid.NewString()}
	b := &model.Project{Name: "B " + uuid.NewString()}
	db.CreateProject(ctx, a)
	db.CreateProject(ctx, b)

	if _, err := db.RenameProject(ctx, b.ID, a.Name); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestPostgresRenameProjectNotFound(t *testing.T) {
	db := setupDB(t)
	if _, err := db.RenameProject(context.Background(), uuid.NewString(), "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// A malformed id must not surface as a driver encode error, which the caller
// could not distinguish from an outage.
func TestPostgresRenameProjectMalformedIDIsNotFound(t *testing.T) {
	db := setupDB(t)
	if _, err := db.RenameProject(context.Background(), "not-a-uuid", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPostgresDeleteProject(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	p := &model.Project{Name: "Doomed " + uuid.NewString()}
	db.CreateProject(ctx, p)

	if err := db.DeleteProject(ctx, p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	list, _ := db.ListProjects(ctx)
	for _, l := range list {
		if l.ID == p.ID {
			t.Fatal("expected the project to be gone")
		}
	}
}

func TestPostgresDeleteProjectRefusesTheDefault(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	var defaultID string
	if err := db.Pool.QueryRow(ctx, `SELECT id FROM projects WHERE is_default`).Scan(&defaultID); err != nil {
		t.Fatalf("find default: %v", err)
	}
	if err := db.DeleteProject(ctx, defaultID); !errors.Is(err, store.ErrProtected) {
		t.Fatalf("expected ErrProtected, got %v", err)
	}
}

// The foreign key backstop: this is the path a delete takes when it races the
// handler's emptiness check.
func TestPostgresDeleteProjectWithTestsIsNotEmpty(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	p := &model.Project{Name: "Occupied " + uuid.NewString()}
	db.CreateProject(ctx, p)
	tst := &model.Test{ProjectID: p.ID, Name: "t", TargetURL: "http://x", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	if err := db.DeleteProject(ctx, p.ID); !errors.Is(err, store.ErrNotEmpty) {
		t.Fatalf("expected ErrNotEmpty, got %v", err)
	}
}

func TestPostgresDeleteProjectNotFound(t *testing.T) {
	db := setupDB(t)
	if err := db.DeleteProject(context.Background(), uuid.NewString()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

Names are suffixed with a UUID because these tests share one database across runs and `projects.name` is `UNIQUE` — a fixed name passes once and 409s forever after.

- [ ] **Step 8: Run the postgres tests**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./internal/store/postgres/... -v
```

Expected: PASS, none skipped.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/store/store.go \
        backend/internal/store/postgres/postgres.go \
        backend/internal/store/postgres/store_test.go \
        backend/internal/store/memstore/projectstore.go \
        backend/internal/store/memstore/projectstore_test.go
git commit -m "feat(backend): rename and delete projects in both stores"
```

---

### Task 3: `MoveTest`, and memstore's registry coupling

The memstore change is a prerequisite, not a cleanup: `memstore.go:32` rejects every project id but the seeded one, so without it a move cannot be tested at the API layer at all.

**Files:**
- Modify: `backend/internal/store/store.go:19-32` (TestStore interface)
- Modify: `backend/internal/store/memstore/memstore.go:18-47`
- Modify: `backend/internal/store/postgres/postgres.go` (append after `updateTestAtVersion`)
- Modify: `backend/internal/api/runs_test.go:18-25` (`newTestServer`)
- Test: `backend/internal/store/memstore/memstore_test.go`, `backend/internal/store/postgres/store_test.go`

**Interfaces:**
- Consumes: `ProjectStore.exists(id string) bool` from Task 2 Step 4.
- Produces:
  - `TestStore.MoveTest(ctx context.Context, catalogID, projectID string) error`
  - `memstore.NewTestStore(projects *ProjectStore) *TestStore` — **signature change**, callers updated in this task.

- [ ] **Step 1: Extend the TestStore interface**

In `backend/internal/store/store.go`, add to `TestStore`:

```go
	// MoveTest refiles every version of catalogID under projectID. A project is
	// where a test is filed, not part of what a run executed, so moving does not
	// cut a new version. ErrNotFound if no such test; ErrInvalidReference if the
	// project does not exist.
	MoveTest(ctx context.Context, catalogID, projectID string) error
```

- [ ] **Step 2: Write the failing memstore tests**

Append to `backend/internal/store/memstore/memstore_test.go`:

```go
// A move applies to the whole family, so every version lands in the new
// project -- otherwise ListTestsForProject and ListTestVersions would disagree
// about where the test lives.
func TestMoveTestMovesEveryVersion(t *testing.T) {
	ctx := context.Background()
	ps := NewProjectStore()
	ts := NewTestStore(ps)
	dest := &model.Project{Name: "Billing"}
	if err := ps.CreateProject(ctx, dest); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	tst := &model.Test{Name: "smoke", TargetURL: "http://x", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	edit := &model.Test{ID: tst.ID, Name: "smoke v2", TargetURL: "http://x", VirtualUsers: 2, DurationSeconds: 1}
	if err := ts.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	if err := ts.MoveTest(ctx, tst.ID, dest.ID); err != nil {
		t.Fatalf("MoveTest: %v", err)
	}

	versions, err := ts.ListTestVersions(ctx, tst.ID)
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	for _, v := range versions {
		if v.ProjectID != dest.ID {
			t.Fatalf("version %d stayed in %q", v.Version, v.ProjectID)
		}
	}
}

func TestMoveTestReturnsNotFoundForAnUnknownTest(t *testing.T) {
	ctx := context.Background()
	ps := NewProjectStore()
	ts := NewTestStore(ps)
	if err := ts.MoveTest(ctx, "no-such-test", DefaultProjectID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMoveTestRejectsAnUnknownProject(t *testing.T) {
	ctx := context.Background()
	ps := NewProjectStore()
	ts := NewTestStore(ps)
	tst := &model.Test{Name: "smoke", TargetURL: "http://x", VirtualUsers: 1, DurationSeconds: 1}
	ts.CreateTest(ctx, tst)

	if err := ts.MoveTest(ctx, tst.ID, uuid.NewString()); !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}
}

// The coupling fix: before this, CreateTest rejected every project id but the
// seeded one, so a test could never be created in a project the user made.
func TestCreateTestAcceptsAnyRegisteredProject(t *testing.T) {
	ctx := context.Background()
	ps := NewProjectStore()
	ts := NewTestStore(ps)
	dest := &model.Project{Name: "Billing"}
	ps.CreateProject(ctx, dest)

	tst := &model.Test{ProjectID: dest.ID, Name: "smoke", TargetURL: "http://x", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.CreateTest(ctx, tst); err != nil {
		t.Fatalf("expected a registered project to be accepted, got %v", err)
	}
	if tst.ProjectID != dest.ID {
		t.Fatalf("expected the test to be filed under %q, got %q", dest.ID, tst.ProjectID)
	}
}
```

Ensure the file imports `errors`, `github.com/google/uuid` and `github.com/boltrunner/backend/internal/store`.

- [ ] **Step 3: Run to verify they fail**

```bash
cd backend && go test ./internal/store/memstore/...
```

Expected: compile failure — `NewTestStore` takes no arguments, `ts.MoveTest` undefined.

- [ ] **Step 4: Implement in memstore**

In `backend/internal/store/memstore/memstore.go`, change the struct and constructor:

```go
type TestStore struct {
	mu    sync.RWMutex
	tests map[string]model.Test
	// projects validates project references. The dependency runs one way only
	// -- ProjectStore never calls back into TestStore -- which is what lets the
	// emptiness check for a delete live in the handler instead of here, and
	// what makes holding s.mu across a projects call safe.
	projects *ProjectStore
}

func NewTestStore(projects *ProjectStore) *TestStore {
	return &TestStore{tests: make(map[string]model.Test), projects: projects}
}
```

Replace the project check in `CreateTest` (currently lines 30-38):

```go
	if t.ProjectID == "" {
		t.ProjectID = DefaultProjectID
	} else if !s.projects.exists(t.ProjectID) {
		// postgres enforces the same contract via the tests.project_id foreign
		// key, so both backends reject an unknown project rather than one
		// silently storing it.
		return store.ErrInvalidReference
	}
```

Append `MoveTest`:

```go
func (s *TestStore) MoveTest(ctx context.Context, catalogID, projectID string) error {
	if !s.projects.exists(projectID) {
		return store.ErrInvalidReference
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	moved := false
	for versionID, t := range s.tests {
		if t.ID != catalogID {
			continue
		}
		t.ProjectID = projectID
		s.tests[versionID] = t
		moved = true
	}
	if !moved {
		return store.ErrNotFound
	}
	return nil
}
```

- [ ] **Step 5: Update `newTestServer`**

In `backend/internal/api/runs_test.go`, lines 18-21:

```go
func newTestServer() *Server {
	ps := memstore.NewProjectStore()
	ts := memstore.NewTestStore(ps)
	rs := memstore.NewRunStore()
```

Leave the rest of the function as it is. Compile the API package to find any other `NewTestStore()` caller:

```bash
cd backend && go vet ./... 2>&1 | grep -i "newteststore" || echo "no remaining callers"
```

- [ ] **Step 6: Run the memstore and api suites**

```bash
cd backend && go test ./internal/store/memstore/... ./internal/api/... -v 2>&1 | tail -30
```

Expected: PASS. The four new memstore tests pass, and no existing API test regresses — `newTestServer` still seeds a store whose default project resolves exactly as before.

- [ ] **Step 7: Implement in postgres**

Append to `backend/internal/store/postgres/postgres.go`, after `updateTestAtVersion`:

```go
// MoveTest refiles every version of a family. project_id is not part of what a
// run executed -- jmx.Generate reads only target_url, virtual_users and
// duration_seconds -- so rewriting it across immutable version rows changes no
// historical run's meaning.
func (db *DB) MoveTest(ctx context.Context, catalogID, projectID string) error {
	// Both ids are checked before pgx encodes them, for the reason CreateTest
	// gives: an encode failure cannot be told apart from a connection failure.
	if _, err := uuid.Parse(projectID); err != nil {
		return store.ErrInvalidReference
	}
	if _, err := uuid.Parse(catalogID); err != nil {
		return store.ErrNotFound
	}
	tag, err := db.Pool.Exec(ctx, `UPDATE tests SET project_id = $2 WHERE catalog_id = $1`, catalogID, projectID)
	if isForeignKeyViolation(err) {
		return store.ErrInvalidReference
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}
```

- [ ] **Step 8: Write the postgres move tests**

Append to `backend/internal/store/postgres/store_test.go`:

```go
func TestPostgresMoveTestMovesEveryVersion(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	dest := &model.Project{Name: "Dest " + uuid.NewString()}
	if err := db.CreateProject(ctx, dest); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	tst := &model.Test{Name: "smoke", TargetURL: "http://x", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	edit := &model.Test{ID: tst.ID, Name: "smoke v2", TargetURL: "http://x", VirtualUsers: 2, DurationSeconds: 1}
	if err := db.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	if err := db.MoveTest(ctx, tst.ID, dest.ID); err != nil {
		t.Fatalf("MoveTest: %v", err)
	}

	versions, err := db.ListTestVersions(ctx, tst.ID)
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	for _, v := range versions {
		if v.ProjectID != dest.ID {
			t.Fatalf("version %d stayed in %q", v.Version, v.ProjectID)
		}
	}

	// And it is now listed under the destination rather than where it started.
	inDest, err := db.ListTestsForProject(ctx, dest.ID)
	if err != nil {
		t.Fatalf("ListTestsForProject: %v", err)
	}
	found := false
	for _, l := range inDest {
		if l.ID == tst.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the moved test to be listed in the destination project")
	}
}

func TestPostgresMoveTestUnknownTest(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	var defaultID string
	db.Pool.QueryRow(ctx, `SELECT id FROM projects WHERE is_default`).Scan(&defaultID)
	if err := db.MoveTest(ctx, uuid.NewString(), defaultID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPostgresMoveTestUnknownProject(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	tst := &model.Test{Name: "smoke", TargetURL: "http://x", VirtualUsers: 1, DurationSeconds: 1}
	db.CreateTest(ctx, tst)
	if err := db.MoveTest(ctx, tst.ID, uuid.NewString()); !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}
}

func TestPostgresMoveTestMalformedProjectID(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	tst := &model.Test{Name: "smoke", TargetURL: "http://x", VirtualUsers: 1, DurationSeconds: 1}
	db.CreateTest(ctx, tst)
	if err := db.MoveTest(ctx, tst.ID, "not-a-uuid"); !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}
}
```

- [ ] **Step 9: Run the full backend suite against Postgres**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./...
```

Expected: PASS across all 11 packages, nothing skipped in `internal/store/postgres`.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/store/store.go \
        backend/internal/store/memstore/memstore.go \
        backend/internal/store/memstore/memstore_test.go \
        backend/internal/store/postgres/postgres.go \
        backend/internal/store/postgres/store_test.go \
        backend/internal/api/runs_test.go
git commit -m "feat(backend): move a test between projects, and let memstore see the registry"
```

---

### Task 4: The three HTTP routes

**Files:**
- Modify: `backend/internal/api/server.go:33-43`
- Modify: `backend/internal/api/projects.go:31-61`
- Modify: `backend/internal/api/tests.go` (append)
- Test: `backend/internal/api/projects_test.go`, `backend/internal/api/tests_test.go`

**Interfaces:**
- Consumes: `RenameProject`, `DeleteProject`, `MoveTest`, `ErrProtected`, `ErrNotEmpty` from Tasks 2 and 3; `ListTestsForProject` (already exists).
- Produces: `PUT /api/projects/{projectID}`, `DELETE /api/projects/{projectID}`, `PUT /api/tests/{testID}/project`. Task 5 calls all three.

- [ ] **Step 1: Register the routes**

In `backend/internal/api/server.go`, after the existing project routes:

```go
	s.router.Get("/api/projects", s.handleListProjects)
	s.router.Post("/api/projects", s.handleCreateProject)
	s.router.Put("/api/projects/{projectID}", s.handleRenameProject)
	s.router.Delete("/api/projects/{projectID}", s.handleDeleteProject)
```

And after the existing `PUT /api/tests/{testID}`:

```go
	s.router.Put("/api/tests/{testID}/project", s.handleMoveTest)
```

- [ ] **Step 2: Extract the shared name validation**

In `backend/internal/api/projects.go`, replace the inline validation inside `handleCreateProject` (lines 39-47) with a helper both handlers use, so the two routes cannot drift:

```go
// validProjectName trims and checks a submitted name, writing the error
// response itself. It returns the trimmed name and whether to continue.
// Trimming happens before the uniqueness check so " Payments " cannot become a
// second project the user cannot tell apart from "Payments" in the menu.
func validProjectName(w http.ResponseWriter, raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return "", false
	}
	if len(name) > projectNameMaxLen {
		http.Error(w, "name must be 100 characters or fewer", http.StatusBadRequest)
		return "", false
	}
	return name, true
}
```

`handleCreateProject` becomes:

```go
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	name, ok := validProjectName(w, req.Name)
	if !ok {
		return
	}
	p := &model.Project{Name: name}
	err := s.projectStore.CreateProject(r.Context(), p)
	switch {
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "a project with that name already exists", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "failed to create project", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}
```

- [ ] **Step 3: Write the rename and delete handlers**

Append to `backend/internal/api/projects.go`:

```go
func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	name, ok := validProjectName(w, req.Name)
	if !ok {
		return
	}
	p, err := s.projectStore.RenameProject(r.Context(), chi.URLParam(r, "projectID"), name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "project not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "a project with that name already exists", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "failed to rename project", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// handleDeleteProject counts the project's tests before deleting, so a refusal
// can say how many are in the way. The store deliberately does not do this: it
// would need a reference to the test store, and memstore's test store already
// holds one to the project store, which would close the loop into a cycle.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "projectID")

	// ListProjects doubles as the existence and is_default lookup, and gives us
	// the name for the message -- no extra store method needed for either.
	projects, err := s.projectStore.ListProjects(r.Context())
	if err != nil {
		http.Error(w, "failed to load projects", http.StatusInternalServerError)
		return
	}
	var target *model.Project
	for i := range projects {
		if projects[i].ID == id {
			target = &projects[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if target.IsDefault {
		http.Error(w, "the default project cannot be deleted", http.StatusConflict)
		return
	}

	tests, err := s.testStore.ListTestsForProject(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to count tests", http.StatusInternalServerError)
		return
	}
	if len(tests) > 0 {
		http.Error(w, notEmptyMessage(target.Name, len(tests)), http.StatusConflict)
		return
	}

	err = s.projectStore.DeleteProject(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "project not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrProtected):
		http.Error(w, "the default project cannot be deleted", http.StatusConflict)
		return
	case errors.Is(err, store.ErrNotEmpty):
		// A test was filed here between the count and the delete. No count in
		// the message: re-reading it now would report a number already stale.
		http.Error(w, target.Name+" still has tests; move or delete them first", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "failed to delete project", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func notEmptyMessage(name string, n int) string {
	noun := "tests"
	if n == 1 {
		noun = "test"
	}
	return fmt.Sprintf("%s still has %d %s; move or delete them first", name, n, noun)
}
```

Add `fmt` and `github.com/go-chi/chi/v5` to the file's imports.

- [ ] **Step 4: Write the move handler**

Append to `backend/internal/api/tests.go`:

```go
type moveTestRequest struct {
	ProjectID string `json:"project_id"`
}

// handleMoveTest refiles a whole test family. It is a separate route from
// handleUpdateTest because an edit cuts a new version and a move does not --
// sharing one request would make it mean two different things.
func (s *Server) handleMoveTest(w http.ResponseWriter, r *http.Request) {
	var req moveTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.ProjectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	testID := chi.URLParam(r, "testID")
	err := s.testStore.MoveTest(r.Context(), testID, req.ProjectID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "test not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrInvalidReference):
		http.Error(w, "unknown project_id", http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, "failed to move test", http.StatusInternalServerError)
		return
	}
	moved, err := s.testStore.GetTest(r.Context(), testID)
	if err != nil {
		http.Error(w, "failed to load test", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(moved)
}
```

- [ ] **Step 5: Write the API tests**

Append to `backend/internal/api/projects_test.go`. `createProjectViaAPI` is a helper these share:

```go
func createProjectViaAPI(t *testing.T, srv *Server, name string) model.Project {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"`+name+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed project %q: expected 201, got %d (%s)", name, rec.Code, rec.Body.String())
	}
	var p model.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode seeded project: %v", err)
	}
	return p
}

func renameProjectViaAPI(srv *Server, id, name string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+id, strings.NewReader(`{"name":"`+name+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func TestRenameProjectReturns200AndTheNewName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")

	rec := renameProjectViaAPI(srv, p.ID, "Billing")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got model.Project
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "Billing" || got.ID != p.ID {
		t.Fatalf("unexpected project: %+v", got)
	}
}

func TestRenameProjectReturns409ForATakenName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	if rec := renameProjectViaAPI(srv, p.ID, "Default"); rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestRenameProjectReturns404ForAnUnknownID(t *testing.T) {
	srv := newTestServer()
	if rec := renameProjectViaAPI(srv, "no-such-id", "Billing"); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRenameProjectRejectsABlankName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	if rec := renameProjectViaAPI(srv, p.ID, "   "); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRenameProjectRejectsAnOverlongName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	if rec := renameProjectViaAPI(srv, p.ID, strings.Repeat("a", 101)); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRenameProjectRejectsAMalformedBody(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+p.ID, strings.NewReader(`{`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// The trimmed name is what gets stored, matching create.
func TestRenameProjectStoresTheTrimmedName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	rec := renameProjectViaAPI(srv, p.ID, "  Billing  ")
	var got model.Project
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "Billing" {
		t.Fatalf("expected a trimmed name, got %q", got.Name)
	}
}

func deleteProjectViaAPI(srv *Server, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+id, nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func TestDeleteProjectReturns204AndRemovesIt(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")

	if rec := deleteProjectViaAPI(srv, p.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	var list []model.Project
	json.Unmarshal(listRec.Body.Bytes(), &list)
	for _, l := range list {
		if l.ID == p.ID {
			t.Fatal("expected the project to be gone from the list")
		}
	}
}

func TestDeleteProjectReturns404ForAnUnknownID(t *testing.T) {
	srv := newTestServer()
	if rec := deleteProjectViaAPI(srv, "no-such-id"); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteProjectRefusesTheDefault(t *testing.T) {
	srv := newTestServer()
	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	var list []model.Project
	json.Unmarshal(listRec.Body.Bytes(), &list)
	var defaultID string
	for _, l := range list {
		if l.IsDefault {
			defaultID = l.ID
		}
	}
	if defaultID == "" {
		t.Fatal("expected a default project in the list")
	}

	rec := deleteProjectViaAPI(srv, defaultID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "default project cannot be deleted") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

// The message names the project and the count, because "it still has tests" on
// its own does not tell the user how much work emptying it is.
func TestDeleteProjectRefusesANonEmptyProjectAndNamesTheCount(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	for i := 0; i < 2; i++ {
		body := `{"name":"t","target_url":"http://x","virtual_users":1,"duration_seconds":1,"project_id":"` + p.ID + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/tests", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed test: expected 201, got %d (%s)", rec.Code, rec.Body.String())
		}
	}

	rec := deleteProjectViaAPI(srv, p.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Payments still has 2 tests") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

// One test, singular noun.
func TestDeleteProjectRefusalUsesTheSingularForOneTest(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	body := `{"name":"t","target_url":"http://x","virtual_users":1,"duration_seconds":1,"project_id":"` + p.ID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tests", strings.NewReader(body))
	srv.Router().ServeHTTP(httptest.NewRecorder(), req)

	rec := deleteProjectViaAPI(srv, p.ID)
	if !strings.Contains(rec.Body.String(), "still has 1 test;") {
		t.Fatalf("expected the singular noun, got: %s", rec.Body.String())
	}
}
```

Append to `backend/internal/api/tests_test.go`:

```go
func TestMoveTestReturns200AndTheMovedTest(t *testing.T) {
	srv := newTestServer()
	dest := createProjectViaAPI(t, srv, "Billing")

	createRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(createRec, httptest.NewRequest(http.MethodPost, "/api/tests",
		strings.NewReader(`{"name":"smoke","target_url":"http://x","virtual_users":1,"duration_seconds":1}`)))
	var created model.Test
	json.Unmarshal(createRec.Body.Bytes(), &created)

	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID+"/project",
		strings.NewReader(`{"project_id":"`+dest.ID+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var moved model.Test
	json.Unmarshal(rec.Body.Bytes(), &moved)
	if moved.ProjectID != dest.ID {
		t.Fatalf("expected project %q, got %q", dest.ID, moved.ProjectID)
	}

	// And the scoped list agrees.
	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/tests?project_id="+dest.ID, nil))
	var inDest []model.Test
	json.Unmarshal(listRec.Body.Bytes(), &inDest)
	if len(inDest) != 1 || inDest[0].ID != created.ID {
		t.Fatalf("expected the moved test in the destination list, got %+v", inDest)
	}
}

func TestMoveTestReturns404ForAnUnknownTest(t *testing.T) {
	srv := newTestServer()
	dest := createProjectViaAPI(t, srv, "Billing")
	req := httptest.NewRequest(http.MethodPut, "/api/tests/no-such-test/project",
		strings.NewReader(`{"project_id":"`+dest.ID+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestMoveTestReturns400ForAnUnknownProject(t *testing.T) {
	srv := newTestServer()
	createRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(createRec, httptest.NewRequest(http.MethodPost, "/api/tests",
		strings.NewReader(`{"name":"smoke","target_url":"http://x","virtual_users":1,"duration_seconds":1}`)))
	var created model.Test
	json.Unmarshal(createRec.Body.Bytes(), &created)

	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID+"/project",
		strings.NewReader(`{"project_id":"00000000-0000-0000-0000-0000000000ff"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestMoveTestRequiresAProjectID(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/tests/whatever/project", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestMoveTestRejectsAMalformedBody(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/tests/whatever/project", strings.NewReader(`{`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
```

- [ ] **Step 6: Run the API suite**

```bash
cd backend && go test ./internal/api/... -v 2>&1 | tail -40
```

Expected: PASS. 18 new tests.

- [ ] **Step 7: Check the backend coverage gate**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" \
  go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out | tail -1
```

Expected: total ≥ 88%. If it dipped, the uncovered lines are almost certainly the 500-mapping branches; `fault_injection_test.go` is the existing pattern for reaching those — follow it rather than deleting the branch.

`.gitignore` does not currently list `coverage.out` — add `backend/coverage.out` to it as part of this task, so the profile cannot be committed by a later `git add backend/`.

- [ ] **Step 8: Tear down the test database and commit**

```bash
docker rm -f br-pg-test
rm -f backend/coverage.out
git add backend/internal/api/ .gitignore
git commit -m "feat(backend): rename, delete and move over HTTP"
```

---

### Task 5: Frontend API client and `ProjectProvider`

**Files:**
- Modify: `frontend/lib/api-client.ts:25` (the `Project` type), append three calls
- Modify: `frontend/components/ui/ProjectProvider.tsx:8-14, 44-66`
- Test: `frontend/__tests__/api-client.test.ts`, `frontend/__tests__/ProjectProvider.test.tsx`

**Interfaces:**
- Consumes: the three routes from Task 4.
- Produces:
  - `renameProject(id: string, name: string): Promise<Project>`
  - `deleteProject(id: string): Promise<void>`
  - `moveTest(testId: string, projectId: string): Promise<Test>`
  - `useProjects()` gains `rename(id: string, name: string): Promise<Project>` and `remove(id: string): Promise<void>`.

  Tasks 6 and 7 consume these exact names.

- [ ] **Step 1: Write the failing api-client tests**

Append to `frontend/__tests__/api-client.test.ts`, following the `fetch`-stubbing pattern already in that file:

```ts
  it('renameProject PUTs the new name', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ id: 'p1', name: 'Billing', created_at: 'x', is_default: false }), { status: 200 })
    );

    const got = await renameProject('p1', 'Billing');

    expect(got.name).toBe('Billing');
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('/api/projects/p1');
    expect(init?.method).toBe('PUT');
    expect(init?.body).toBe(JSON.stringify({ name: 'Billing' }));
  });

  it('renameProject throws an ApiError carrying the status', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue(new Response('a project with that name already exists', { status: 409 }));
    await expect(renameProject('p1', 'Default')).rejects.toMatchObject({ status: 409 });
  });

  it('deleteProject DELETEs and resolves on 204', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockResolvedValue(new Response(null, { status: 204 }));

    await expect(deleteProject('p1')).resolves.toBeUndefined();

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('/api/projects/p1');
    expect(init?.method).toBe('DELETE');
  });

  // The 409 body is the message the admin table shows, so it has to survive.
  it('deleteProject throws an ApiError whose message carries the server text', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response('Payments still has 3 tests; move or delete them first', { status: 409 })
    );
    await expect(deleteProject('p1')).rejects.toMatchObject({
      status: 409,
      message: expect.stringContaining('still has 3 tests'),
    });
  });

  it('moveTest PUTs the destination project', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ id: 't1', name: 'x', target_url: 'http://x', virtual_users: 1, duration_seconds: 1, created_at: 'x', project_id: 'p2' }), { status: 200 })
    );

    const got = await moveTest('t1', 'p2');

    expect(got.project_id).toBe('p2');
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('/api/tests/t1/project');
    expect(init?.method).toBe('PUT');
    expect(init?.body).toBe(JSON.stringify({ project_id: 'p2' }));
  });
```

Add `renameProject`, `deleteProject` and `moveTest` to the file's import from `@/lib/api-client`.

- [ ] **Step 2: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/api-client.test.ts
```

Expected: FAIL — the three functions are not exported.

- [ ] **Step 3: Implement the api-client calls**

In `frontend/lib/api-client.ts`, extend the `Project` type:

```ts
export type Project = { id: string; name: string; created_at: string; is_default: boolean };
```

Append the three calls after `createProject`:

```ts
export async function renameProject(id: string, name: string): Promise<Project> {
  return unwrap(
    await fetch(`${API_URL}/api/projects/${id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    })
  );
}

export async function deleteProject(id: string): Promise<void> {
  const res = await fetch(`${API_URL}/api/projects/${id}`, { method: 'DELETE' });
  if (!res.ok) {
    // The 409 body names the project and its test count; it is the message the
    // admin table shows, so it has to reach the caller intact.
    throw new ApiError(res.status, await res.text());
  }
}

export async function moveTest(testId: string, projectId: string): Promise<Test> {
  return unwrap(
    await fetch(`${API_URL}/api/tests/${testId}/project`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ project_id: projectId }),
    })
  );
}
```

Note `deleteProject` throws the raw server text rather than `unwrap`'s `request failed (409): …` wrapper — the admin table renders the message verbatim.

- [ ] **Step 4: Fix the `Project` fixtures the new required field breaks**

`is_default` is required, so every `Project` literal in the test suite must gain it. Find them:

```bash
cd frontend && npx tsc --noEmit 2>&1 | head -30
```

Add `is_default: false` to each reported literal (and `true` where the fixture represents Default). Do not make the field optional to avoid this — the whole point is that consumers can rely on it.

- [ ] **Step 5: Run to verify they pass**

```bash
cd frontend && npx vitest run __tests__/api-client.test.ts
```

Expected: PASS.

- [ ] **Step 6: Write the failing ProjectProvider tests**

`frontend/__tests__/ProjectProvider.test.tsx` drives the context through a `Probe` component (line 10) whose buttons call the context methods and whose spans expose the results. Extend that Probe rather than adding a second one — the file's five existing cases assert against it:

```tsx
function Probe() {
  const { projects, selected, select, create, rename, remove } = useProjects();
  const [err, setErr] = useState('');
  return (
    <div>
      <span data-testid="selected">{selected?.name ?? 'none'}</span>
      <span data-testid="count">{projects.length}</span>
      <span data-testid="names">{projects.map((p) => p.name).join(',')}</span>
      <span data-testid="error">{err}</span>
      <button onClick={() => select('p2')}>pick payments</button>
      <button onClick={() => create('New One')}>create</button>
      <button onClick={() => rename('p2', 'Beta')}>rename payments</button>
      <button onClick={() => remove('p2')}>remove payments</button>
      <button onClick={() => remove('p1').catch((e) => setErr(e.message))}>remove default</button>
    </div>
  );
}
```

Add `import { useState } from 'react';` at the top. The two existing fixtures also need the new required field:

```tsx
const def: Project = { id: 'p1', name: 'Default', created_at: '2026-07-24T00:00:00Z', is_default: true };
const pay: Project = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z', is_default: false };
```

Then append inside the existing `describe`:

```tsx
  it('rename updates the list and keeps it sorted by name', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'renameProject').mockResolvedValue({
      id: 'p2', name: 'Beta', created_at: '2026-07-29T00:00:00Z', is_default: false,
    });

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('names')).toHaveTextContent('Default,Payments'));

    fireEvent.click(screen.getByText('rename payments'));

    // Re-sorted, so "Beta" lands before "Default" rather than staying put.
    await waitFor(() => expect(screen.getByTestId('names')).toHaveTextContent('Beta,Default'));
  });

  it('remove drops the project from the list', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'deleteProject').mockResolvedValue(undefined);

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('count')).toHaveTextContent('2'));

    fireEvent.click(screen.getByText('remove payments'));

    await waitFor(() => expect(screen.getByTestId('names')).toHaveTextContent('Default'));
    expect(screen.getByTestId('count')).toHaveTextContent('1');
  });

  // Deleting the selected project must not leave the switcher pointing at
  // nothing -- the same failure the stored-id guard handles on load, reached
  // by a different route.
  it('remove falls back to the default project when the selected one is deleted', async () => {
    localStorage.setItem('boltrunner-project', 'p2');
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'deleteProject').mockResolvedValue(undefined);

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Payments'));

    fireEvent.click(screen.getByText('remove payments'));

    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Default'));
    expect(localStorage.getItem('boltrunner-project')).toBe('p1');
  });

  // Deleting a project the user is not looking at must not move them.
  it('remove leaves the selection alone when another project is deleted', async () => {
    localStorage.setItem('boltrunner-project', 'p1');
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'deleteProject').mockResolvedValue(undefined);

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Default'));

    fireEvent.click(screen.getByText('remove payments'));

    await waitFor(() => expect(screen.getByTestId('count')).toHaveTextContent('1'));
    expect(screen.getByTestId('selected')).toHaveTextContent('Default');
  });

  // The 409 body is what the admin table renders, so remove must reject rather
  // than swallow it -- and must leave the project in the list, because the
  // delete did not happen.
  it('remove propagates a rejection', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'deleteProject').mockRejectedValue(
      new api.ApiError(409, 'Default still has 3 tests; move or delete them first')
    );

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('count')).toHaveTextContent('2'));

    fireEvent.click(screen.getByText('remove default'));

    await waitFor(() => expect(screen.getByTestId('error')).toHaveTextContent('still has 3 tests'));
    expect(screen.getByTestId('count')).toHaveTextContent('2');
  });
```

- [ ] **Step 7: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/ProjectProvider.test.tsx
```

Expected: FAIL — `rename` and `remove` are not on the context value.

- [ ] **Step 8: Implement rename and remove**

In `frontend/components/ui/ProjectProvider.tsx`, extend the imports and the context type:

```ts
import { listProjects, createProject, renameProject, deleteProject, Project } from '@/lib/api-client';

type ProjectContextValue = {
  projects: Project[];
  selectedId: string | null;
  selected: Project | null;
  select: (id: string) => void;
  create: (name: string) => Promise<Project>;
  rename: (id: string, name: string) => Promise<Project>;
  remove: (id: string) => Promise<void>;
};
```

Add the two callbacks after `create`:

```tsx
  const rename = useCallback(async (id: string, name: string) => {
    const updated = await renameProject(id, name);
    // Re-sorted for the same reason create sorts: both stores return
    // ListProjects ordered by name, so an unsorted local list would reshuffle
    // the menu on the next reload.
    setProjects((prev) =>
      prev.map((p) => (p.id === id ? updated : p)).sort((a, b) => a.name.localeCompare(b.name))
    );
    return updated;
  }, []);

  const remove = useCallback(async (id: string) => {
    await deleteProject(id);
    setProjects((prev) => {
      const next = prev.filter((p) => p.id !== id);
      setSelectedId((current) => {
        if (current !== id) return current;
        // The selected project just went away. Falling back to the default
        // keeps the switcher pointing at something real -- the same guard the
        // load path applies to a stored id that outlived its database.
        const fallback = next.find((p) => p.is_default)?.id ?? next[0]?.id ?? null;
        if (fallback) localStorage.setItem(STORAGE_KEY, fallback);
        else localStorage.removeItem(STORAGE_KEY);
        return fallback;
      });
      return next;
    });
  }, []);
```

Add both to the provider value:

```tsx
    <ProjectContext.Provider value={{ projects, selectedId, selected, select, create, rename, remove }}>
```

- [ ] **Step 9: Run to verify they pass**

```bash
cd frontend && npx vitest run __tests__/ProjectProvider.test.tsx
```

Expected: PASS.

- [ ] **Step 10: Run the whole unit suite**

```bash
cd frontend && npx vitest run
```

Expected: PASS. Baseline before this plan is 165 tests across 32 files. Any failure outside the two files you touched means a `Project` fixture is still missing `is_default` — fix the fixture, not the assertion.

- [ ] **Step 11: Commit**

```bash
git add frontend/lib/api-client.ts frontend/components/ui/ProjectProvider.tsx frontend/__tests__/
git commit -m "feat(frontend): rename, delete and move in the API client and ProjectProvider"
```

---

### Task 6: The project table on `/admin`

**Files:**
- Modify: `frontend/app/admin/page.tsx` (whole file)
- Test: `frontend/__tests__/AdminPage.test.tsx` (whole file)

**Interfaces:**
- Consumes: `useProjects()`'s `projects`, `rename`, `remove` from Task 5.
- Produces: nothing — no other module imports this page.

- [ ] **Step 1: Add the `useProjects` stub to the existing test**

`AdminPage` will call `useProjects()`, which throws outside a provider. `AdminPage.test.tsx` currently renders the page bare, so its one existing case breaks without a stub. This is the same mechanical edit six other test files already carry.

Replace `frontend/__tests__/AdminPage.test.tsx` entirely:

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import AdminPage from '@/app/admin/page';

const projectState = vi.hoisted(() => ({
  projects: [] as { id: string; name: string; created_at: string; is_default: boolean }[],
  rename: vi.fn(),
  remove: vi.fn(),
}));

vi.mock('@/components/ui/ProjectProvider', () => ({
  useProjects: () => ({
    projects: projectState.projects,
    selectedId: projectState.projects[0]?.id ?? null,
    selected: projectState.projects[0] ?? null,
    select: vi.fn(),
    create: vi.fn(),
    rename: projectState.rename,
    remove: projectState.remove,
  }),
}));

describe('AdminPage', () => {
  beforeEach(() => {
    projectState.projects = [
      { id: 'p1', name: 'Default', created_at: '2026-07-24T00:00:00Z', is_default: true },
      { id: 'p2', name: 'Payments', created_at: '2026-07-25T00:00:00Z', is_default: false },
    ];
    projectState.rename = vi.fn().mockResolvedValue({ id: 'p2', name: 'Billing', created_at: 'x', is_default: false });
    projectState.remove = vi.fn().mockResolvedValue(undefined);
  });

  it('renders the API base URL', () => {
    render(<AdminPage />);
    expect(screen.getByText(/API base URL/i)).toBeInTheDocument();
  });

  it('lists every project', () => {
    render(<AdminPage />);
    expect(screen.getByText('Default')).toBeInTheDocument();
    expect(screen.getByText('Payments')).toBeInTheDocument();
  });

  it('renames a project through the inline editor', async () => {
    render(<AdminPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Rename Payments' }));
    const input = screen.getByRole('textbox', { name: /new name/i });
    fireEvent.change(input, { target: { value: 'Billing' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(projectState.rename).toHaveBeenCalledWith('p2', 'Billing'));
  });

  it('cancelling a rename leaves the name alone', () => {
    render(<AdminPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Rename Payments' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(screen.queryByRole('textbox', { name: /new name/i })).not.toBeInTheDocument();
    expect(projectState.rename).not.toHaveBeenCalled();
  });

  // A rejected name is usually one character from an accepted one, so the row
  // stays in edit state with what was typed still there.
  it('keeps the editor open and shows the error when a rename is rejected', async () => {
    projectState.rename = vi.fn().mockRejectedValue(new Error('a project with that name already exists'));
    render(<AdminPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Rename Payments' }));
    fireEvent.change(screen.getByRole('textbox', { name: /new name/i }), { target: { value: 'Default' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByText(/already exists/i)).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: /new name/i })).toHaveValue('Default');
  });

  it('deletes a project after a confirmation step', async () => {
    render(<AdminPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Delete Payments' }));
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

    await waitFor(() => expect(projectState.remove).toHaveBeenCalledWith('p2'));
  });

  it('cancelling a delete does not remove anything', () => {
    render(<AdminPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Delete Payments' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(projectState.remove).not.toHaveBeenCalled();
  });

  // The server's 409 names the project and the count; showing it verbatim is
  // what tells the user how much work emptying the project is.
  it('shows the server message when a delete is refused', async () => {
    projectState.remove = vi
      .fn()
      .mockRejectedValue(new Error('Payments still has 3 tests; move or delete them first'));
    render(<AdminPage />);

    fireEvent.click(screen.getByRole('button', { name: 'Delete Payments' }));
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

    expect(await screen.findByText(/still has 3 tests/i)).toBeInTheDocument();
  });

  it('disables delete on the default project and says why', () => {
    render(<AdminPage />);
    const button = screen.getByRole('button', { name: 'Delete Default' });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('title', 'the default project cannot be deleted');
  });
});
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/AdminPage.test.tsx
```

Expected: the "renders the API base URL" case PASSES (the stub is inert against today's page); the eight new cases FAIL on missing buttons.

- [ ] **Step 3: Implement the page**

Replace `frontend/app/admin/page.tsx`:

```tsx
'use client';

import { useState } from 'react';
import { Card } from '@/components/ui/Card';
import { DataTable, Column } from '@/components/ui/DataTable';
import { useProjects } from '@/components/ui/ProjectProvider';
import { Project } from '@/lib/api-client';

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080';

const DEFAULT_PROTECTED = 'the default project cannot be deleted';

export default function AdminPage() {
  const { projects, rename, remove } = useProjects();
  // Row-scoped UI state, keyed by project id so only one row is ever in an
  // edit or confirm state.
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftName, setDraftName] = useState('');
  const [confirmingId, setConfirmingId] = useState<string | null>(null);
  const [error, setError] = useState<{ id: string; message: string } | null>(null);

  function startRename(p: Project) {
    setConfirmingId(null);
    setError(null);
    setEditingId(p.id);
    setDraftName(p.name);
  }

  async function saveRename(p: Project) {
    setError(null);
    try {
      await rename(p.id, draftName.trim());
      setEditingId(null);
    } catch (err) {
      // The row stays in edit state with what was typed: a rejected name is
      // usually one character away from an accepted one.
      setError({ id: p.id, message: err instanceof Error ? err.message : "Couldn't rename this project" });
    }
  }

  async function confirmDelete(p: Project) {
    setError(null);
    try {
      await remove(p.id);
      setConfirmingId(null);
    } catch (err) {
      setError({ id: p.id, message: err instanceof Error ? err.message : "Couldn't delete this project" });
    }
  }

  const columns: Column<Project>[] = [
    {
      key: 'name',
      header: 'Project',
      render: (p) =>
        editingId === p.id ? (
          <span className="flex flex-col gap-1">
            <input
              aria-label="New name"
              value={draftName}
              onChange={(e) => setDraftName(e.target.value)}
              className="rounded border border-border bg-surface px-2 py-1 text-sm"
            />
            {error?.id === p.id && <span className="text-xs text-red-600">{error.message}</span>}
          </span>
        ) : (
          <span className="flex flex-col gap-1">
            <span>{p.name}</span>
            {error?.id === p.id && <span className="text-xs text-red-600">{error.message}</span>}
          </span>
        ),
    },
    { key: 'created_at', header: 'Created' },
    {
      key: 'actions',
      header: 'Actions',
      render: (p) => {
        if (editingId === p.id) {
          return (
            <span className="flex gap-2">
              <button type="button" onClick={() => saveRename(p)}>
                Save
              </button>
              <button type="button" onClick={() => setEditingId(null)}>
                Cancel
              </button>
            </span>
          );
        }
        if (confirmingId === p.id) {
          return (
            <span className="flex items-center gap-2">
              <span className="text-sm">Delete {p.name}?</span>
              <button type="button" onClick={() => confirmDelete(p)}>
                Confirm
              </button>
              <button type="button" onClick={() => setConfirmingId(null)}>
                Cancel
              </button>
            </span>
          );
        }
        return (
          <span className="flex gap-2">
            <button type="button" onClick={() => startRename(p)}>
              Rename {p.name}
            </button>
            <button
              type="button"
              disabled={p.is_default}
              title={p.is_default ? DEFAULT_PROTECTED : undefined}
              onClick={() => {
                setEditingId(null);
                setError(null);
                setConfirmingId(p.id);
              }}
            >
              Delete {p.name}
            </button>
          </span>
        );
      },
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold text-text">Admin</h1>

      <section className="flex flex-col gap-2">
        <h2 className="font-medium text-text">Projects</h2>
        <DataTable columns={columns} rows={projects} rowKey={(p) => p.id} emptyMessage="No projects yet." />
      </section>

      <Card>
        <div className="flex flex-col gap-1">
          <span className="text-xs uppercase text-text-muted">API base URL</span>
          <span className="font-mono text-text">{API_URL}</span>
        </div>
      </Card>
    </div>
  );
}
```

The Rename and Delete buttons carry the project name in their label deliberately: `DataTable` renders both a desktop table and a mobile card list from the same rows, so a bare "Delete" would match two elements and `getByRole` would throw on ambiguity. Naming them keeps each query unique *and* gives screen readers a distinguishable label.

- [ ] **Step 4: Run to verify they pass**

```bash
cd frontend && npx vitest run __tests__/AdminPage.test.tsx
```

Expected: PASS, 9 cases.

If a query fails with "found multiple elements", it is the desktop/mobile double render described above — make the accessible name unique rather than switching to `getAllBy*`.

- [ ] **Step 5: Commit**

```bash
git add frontend/app/admin/page.tsx frontend/__tests__/AdminPage.test.tsx
git commit -m "feat(frontend): rename and delete projects from the admin page"
```

---

### Task 7: The move control on the test detail page

**Files:**
- Modify: `frontend/components/TestDetailPanel.tsx:1-20, 86-110`
- Test: `frontend/__tests__/TestDetailPanel.test.tsx`

**Interfaces:**
- Consumes: `moveTest` from Task 5, `useProjects()`'s `projects` and `selectedId`.
- Produces: nothing.

- [ ] **Step 1: Write the failing tests**

`frontend/__tests__/TestDetailPanel.test.tsx` already stubs `next/navigation` with a module-level `push` spy (lines 8-11) and defines `v2`/`v1` `TestVersion` fixtures (lines 13-33) whose `project_id` is `'p1'` — reuse all three rather than adding your own. Add the `useProjects` stub next to the existing `next/navigation` mock:

```tsx
const projectState = vi.hoisted(() => ({
  projects: [
    { id: 'p1', name: 'Default', created_at: 'x', is_default: true },
    { id: 'p2', name: 'Payments', created_at: 'x', is_default: false },
  ],
  selectedId: 'p1' as string | null,
}));

vi.mock('@/components/ui/ProjectProvider', () => ({
  useProjects: () => ({
    projects: projectState.projects,
    selectedId: projectState.selectedId,
    selected: projectState.projects.find((p) => p.id === projectState.selectedId) ?? null,
    select: vi.fn(),
    create: vi.fn(),
    rename: vi.fn(),
    remove: vi.fn(),
  }),
}));
```

And the cases:

```tsx
  it('moves the test to the chosen project', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    const moveTest = vi.spyOn(api, 'moveTest').mockResolvedValue({
      id: 't1', name: 'smoke', target_url: 'http://x', virtual_users: 1,
      duration_seconds: 1, created_at: 'x', project_id: 'p2',
    });

    render(<TestDetailPanel testId="t1" />);
    await screen.findByRole('heading', { name: /Checkout Load/i });

    fireEvent.change(screen.getByRole('combobox', { name: /move to project/i }), { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('button', { name: /^move$/i }));

    await waitFor(() => expect(moveTest).toHaveBeenCalledWith('t1', 'p2'));
  });

  // Staying put would leave a detail page open for a test the selected
  // workspace no longer contains, beside a TreeNav still listing it: Shell's
  // fetch keys on selectedId, which has not changed, so nothing refetches.
  it('redirects to the test list after a successful move', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    vi.spyOn(api, 'moveTest').mockResolvedValue({
      id: 't1', name: 'smoke', target_url: 'http://x', virtual_users: 1,
      duration_seconds: 1, created_at: 'x', project_id: 'p2',
    });

    render(<TestDetailPanel testId="t1" />);
    await screen.findByRole('heading', { name: /Checkout Load/i });

    fireEvent.change(screen.getByRole('combobox', { name: /move to project/i }), { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('button', { name: /^move$/i }));

    await waitFor(() => expect(push).toHaveBeenCalledWith('/tests'));
  });

  it('shows an error and stays put when the move fails', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    vi.spyOn(api, 'moveTest').mockRejectedValue(new api.ApiError(400, 'unknown project_id'));

    render(<TestDetailPanel testId="t1" />);
    await screen.findByRole('heading', { name: /Checkout Load/i });

    fireEvent.change(screen.getByRole('combobox', { name: /move to project/i }), { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('button', { name: /^move$/i }));

    expect(await screen.findByText(/unknown project_id/i)).toBeInTheDocument();
    expect(push).not.toHaveBeenCalledWith('/tests');
  });

  // Moving a test to the project it is already in is a no-op the user did not
  // mean, so the button is unavailable until the selection actually changes.
  it('disables the move button while the destination is the current project', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);

    render(<TestDetailPanel testId="t1" />);
    await screen.findByRole('heading', { name: /Checkout Load/i });

    expect(screen.getByRole('button', { name: /^move$/i })).toBeDisabled();
  });
```

The move fixtures resolve to `project_id: 'p2'`, while `v2` carries `'p1'` — that difference is what makes the "already in this project" case meaningful. `beforeEach` in this file calls `vi.restoreAllMocks()`, so each case stubs `listTestVersions` itself.

- [ ] **Step 2: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/TestDetailPanel.test.tsx
```

Expected: FAIL — no combobox named "Move to project".

- [ ] **Step 3: Implement the control**

In `frontend/components/TestDetailPanel.tsx`, extend the imports:

```ts
import {
  ApiError,
  listTestVersions,
  moveTest,
  startRun,
  updateTest,
  TestVersion,
  UpdateTestInput,
} from '@/lib/api-client';
import { useProjects } from '@/components/ui/ProjectProvider';
```

Add state and the handler alongside the existing ones:

```tsx
  const { projects } = useProjects();
  const [destination, setDestination] = useState('');
  const [moveError, setMoveError] = useState<string | null>(null);
```

After `handleRun`:

```tsx
  async function handleMove() {
    setMoveError(null);
    try {
      await moveTest(testId, destination);
      // Leaving the page is deliberate: the scoped test list remounts and
      // refetches, so the move is visible instead of silently stale.
      router.push('/tests');
    } catch (err) {
      setMoveError(err instanceof Error ? err.message : "Couldn't move this test");
    }
  }
```

Seed `destination` from the loaded test, so the button starts disabled on the current project. Add this effect after the existing `useEffect`:

```tsx
  const currentProjectId = versions[0]?.project_id ?? '';
  useEffect(() => {
    setDestination(currentProjectId);
  }, [currentProjectId]);
```

Add the section between "Configuration" and "Version history":

```tsx
      <section className="flex flex-col gap-2">
        <h3 className="font-medium text-text">Project</h3>
        <div className="flex items-center gap-2">
          <select
            aria-label="Move to project"
            value={destination}
            onChange={(e) => setDestination(e.target.value)}
            className="rounded border border-border bg-surface px-2 py-1 text-sm"
          >
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <button type="button" onClick={handleMove} disabled={!destination || destination === currentProjectId}>
            Move
          </button>
        </div>
        {moveError && <p className="text-sm text-red-600">{moveError}</p>}
      </section>
```

- [ ] **Step 4: Run to verify they pass**

```bash
cd frontend && npx vitest run __tests__/TestDetailPanel.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Run the whole unit suite**

```bash
cd frontend && npx vitest run
```

Expected: PASS. `TestDetailPage.test.tsx` renders the page that wraps this panel — if it now throws `useProjects must be used within a ProjectProvider`, it needs the same stub. Add it; do not wrap the component in a real provider, because that would make the test depend on `listProjects` too.

- [ ] **Step 6: Commit**

```bash
git add frontend/components/TestDetailPanel.tsx frontend/__tests__/
git commit -m "feat(frontend): move a test to another project from its detail page"
```

---

### Task 8: End-to-end coverage and the full gate

**Files:**
- Modify: `frontend/e2e/project-workspaces.spec.ts`
- Test: both suites, both coverage gates

**Interfaces:**
- Consumes: everything above.
- Produces: nothing.

- [ ] **Step 1: Add the e2e case**

Append to `frontend/e2e/project-workspaces.spec.ts` a third test. It exercises the delete rule and the move feature against each other, which is the pairing the whole design rests on:

```ts
test('a project must be emptied before it can be deleted', async ({ page }) => {
  // Timestamped for the same reason the first spec is: the database outlives a
  // run and project names are unique.
  const project = `E2E Deletable ${Date.now()}`;
  const testName = `E2E Movable ${Date.now()}`;
  await page.goto('/');

  // Create a project and put one test in it.
  await page.getByRole('button', { name: /default/i }).click();
  await page.getByRole('button', { name: /new project/i }).click();
  const input = page.getByRole('textbox', { name: /project name/i });
  await input.fill(project);
  await input.press('Enter');
  await expect(page.getByRole('button', { name: new RegExp(project, 'i') })).toBeVisible();

  await page.getByLabel(/name/i).fill(testName);
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('2');
  await page.getByLabel(/duration/i).fill('10');
  await page.getByRole('button', { name: /create test/i }).click();
  await expect(page.getByRole('row', { name: new RegExp(testName, 'i') })).toBeVisible();

  // Deleting it now is refused, and the message says how many tests are in the way.
  await page.getByRole('link', { name: 'Admin' }).click();
  await expect(page).toHaveURL(/\/admin/);
  await page.getByRole('button', { name: `Delete ${project}` }).first().click();
  await page.getByRole('button', { name: 'Confirm' }).first().click();
  await expect(page.getByText(/still has 1 test;/i).first()).toBeVisible();

  // Empty it by moving the test to Default.
  await page.getByRole('link', { name: 'Test Management' }).click();
  await page.getByRole('link', { name: testName }).click();
  await expect(page).toHaveURL(/\/tests\//);
  await page.getByRole('combobox', { name: /move to project/i }).selectOption({ label: 'Default' });
  await page.getByRole('button', { name: /^move$/i }).click();
  await expect(page).toHaveURL(/\/tests$/);

  // Now the delete succeeds and the project leaves the switcher.
  await page.getByRole('link', { name: 'Admin' }).click();
  await page.getByRole('button', { name: `Delete ${project}` }).first().click();
  await page.getByRole('button', { name: 'Confirm' }).first().click();
  await expect(page.getByRole('button', { name: `Delete ${project}` })).toHaveCount(0);

  // Deleting the selected project falls the switcher back to Default rather
  // than leaving it pointing at nothing.
  await expect(page.getByRole('button', { name: /default/i }).first()).toBeVisible();
});
```

`.first()` appears on the admin queries because `DataTable` renders the desktop table and the mobile card list simultaneously, so every action button exists twice in the DOM. The default Playwright viewport shows the table, but both are present to the selector.

- [ ] **Step 2: Check the frontend coverage gate**

```bash
cd frontend && npm run test:coverage
```

Expected: PASS with lines, statements, functions and branches all ≥ 88%. The previous run was 97.5 / 93.8 / 97.94 / 98.99. This plan adds a lot of branches on `/admin` — if branches dip below 88%, the uncovered ones will be the error paths in `saveRename`/`confirmDelete`; Task 6 Step 1 already covers both, so a dip means a case was dropped, not that the gate is wrong.

- [ ] **Step 3: Typecheck and build**

```bash
cd frontend && npm run build
```

Expected: success, 8 routes.

- [ ] **Step 4: Run the browser suite against a real backend**

```bash
docker run -d --rm --name br-pg -e POSTGRES_USER=boltrunner -e POSTGRES_PASSWORD=boltrunner -e POSTGRES_DB=boltrunner -p 5432:5432 postgres:16
sleep 8
docker build -f deploy/Dockerfile.server -t boltrunner/server:local .
docker run -d --rm --name br-api --network host \
  -e DATABASE_URL="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" \
  -e KUBECONFIG=/kube/config -v "$HOME/.kube/config:/kube/config:ro" boltrunner/server:local
sleep 8
curl -sf http://localhost:8080/healthz
cd frontend && npm run build && npm start &
sleep 5
npx playwright test
```

Expected: **14 passing across 5 files** — 13 before, plus the one this task adds. Tear down with `docker rm -f br-api br-pg`.

CI runs this suite in `integration-kind`. Verify the count in the job log rather than trusting the green check: `gh run view --job=<id> --log | grep -A5 "Run browser e2e"`.

- [ ] **Step 5: Run both suites one final time**

```bash
cd backend && go test ./... && cd ../frontend && npx vitest run
```

Expected: backend PASS across 11 packages; frontend PASS. Report the actual test counts, not "all passing".

- [ ] **Step 6: Commit**

```bash
git add frontend/e2e/project-workspaces.spec.ts
git commit -m "test(e2e): a project must be emptied before it can be deleted"
```

---

## Self-review notes

- **Spec coverage.** Schema and the partial index → Task 1 Steps 1, 5. `is_default` on the model → Task 1 Step 2. Resolving the fallback by flag → Task 1 Step 3. The migration-chain-over-real-data case → Task 1 Step 5, `TestMigrateFromThe0002SchemaPreservesData`. Decision 1, delete refuses non-empty → Task 2 (store), Task 4 Step 3 (the count and the message), Task 6 (the UI), Task 8 Step 1 (e2e). Decision 2, whole-family move → Task 3 Steps 4, 7, asserted per-version in Steps 2 and 8. Decision 3, `/admin` → Task 6. Decision 4, per-test move → Task 7. Decision 5, flag not name → Task 1. The dependency-cycle resolution → Task 3 Step 4 (one-way `TestStore → ProjectStore`) and Task 4 Step 3 (the handler-side count). Every row of the error table → Task 4 Step 5. Frontend sections → Tasks 5-7. Testing strategy → the test steps throughout, plus Task 8.
- **Placeholder scan.** None remain. Three steps originally named invented helpers — `newTestDB`, a `probeApi` handle, and `versionFixture()`. Checking the files showed the real names are `setupDB` (`store_test.go:16`), a `Probe` component driven by button clicks (`ProjectProvider.test.tsx:10`), and the `v2`/`v1` fixtures (`TestDetailPanel.test.tsx:13-33`). All three steps now name what is actually there, and the `ProjectProvider` step shows the exact Probe extension rather than telling the implementer to invent one. Everything else gives exact paths, complete code and exact commands.
- **Type consistency.** `RenameProject(ctx, id, name) (*model.Project, error)` and `DeleteProject(ctx, id) error` are named identically in the interface (Task 2 Step 1), both implementations (Steps 4, 6) and the handlers (Task 4 Step 3). `MoveTest(ctx, catalogID, projectID) error` likewise across Task 3 Steps 1, 4, 7 and Task 4 Step 4. `renameProject` / `deleteProject` / `moveTest` and the provider's `rename` / `remove` are named identically in Task 5 and consumed under those names in Tasks 6 and 7. `is_default` is snake_case in the JSON and TypeScript types, `IsDefault` in Go — matching the existing convention.
- **A correction made during review.** The delete handler first called `DeleteProject` and mapped `ErrNotEmpty` to a message with a count. But the store returns only a sentinel, so the count was unavailable at the point the message is written. Restructured to count first via the existing `ListTestsForProject`, keeping the store's error as the FK backstop with a count-free message — which is also why Task 4 Step 3 carries a comment explaining why the check is not in the store.
- **Risk.** The likeliest mistake is Task 6 and Task 8: `DataTable` renders desktop and mobile views simultaneously, so every action button exists twice. `getByRole` throws on that ambiguity in Vitest, and Playwright's strict mode throws too. Task 6 solves it by putting the project name in each button's accessible name; Task 8 additionally needs `.first()` because two rows' buttons still collide across the two renderings. Do not "fix" a resulting failure by switching to `getAllBy*` and indexing — that hides which view was matched.
- **Second risk.** Task 1 Step 6 and Task 2 Step 8 will silently *skip* without `BOLTRUNNER_TEST_DSN`. Both steps say to check for `SKIP` explicitly. A skipped migration test is the one that would let the deployed-database regression through.
