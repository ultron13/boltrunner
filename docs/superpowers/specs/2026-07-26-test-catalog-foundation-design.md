# BOL-28: Test Catalog Foundation — Versioned Tests and a Minimal Project Registry

## Context

`store.TestStore` today has exactly three methods — `CreateTest`, `ListTests`, `GetTest` —
implemented twice (`memstore` in-memory map, `postgres` via pgx). `model.Test` is flat: name,
target URL, virtual users, duration. There is **no way to edit a test**: no `UpdateTest`, no
`PUT` route. `runs.test_id` has a foreign key to `tests(id)`.

BOL-28's summary reads "extend TestStore into project/scenario/versioning model", but its own
**Produces** is narrower — *"Schema migration adding versioning columns/table to `tests`;
`store.TestStore` gains version-aware methods"* — and it describes itself as "the foundation
the Parity-phase Centralized Project/Test Management epic builds on."

That epic (BOL-39) already owns the full features in dedicated stories: **BOL-49** Project /
Application / Environment registries, **BOL-50** Versioned test catalog, **BOL-51** Scenario
catalog, **BOL-52** Load profile catalog. Building all of projects + scenarios + versioning
here would preempt four downstream tickets, so this spec deliberately scopes BOL-28 to the
foundation layer.

## Decisions made during brainstorming

1. **Scope**: the versioning foundation, plus a *minimal* `projects` table (seeded "Default",
   `project_id` on tests) so a follow-up can wire `WorkspaceSwitcher` to real data. No
   scenarios, no load profiles, no project CRUD beyond listing.
2. **How versions are created**: copy-on-write on edit. A new `UpdateTest` writes an immutable
   new version row rather than mutating in place. Versioning is exercised by the natural user
   action, and prior versions stay intact for run history. (Rejected: a two-step
   draft/publish flow — more moving parts than this foundation needs; and schema-only with no
   way to create a second version — ships dead structure that can't be meaningfully tested.)
3. **Storage model**: one `tests` table of immutable rows keyed by `(catalog_id, version)`.
   `tests.id` stays the primary key and is unique *per version*; `catalog_id` is the stable
   identity across versions.

   Chosen over two alternatives:
   - *`tests` (identity) + `test_versions` (config)*: conceptually cleaner and closer to the
     ticket's "columns/**table**" wording, but `runs.test_id` would no longer identify a
     version, requiring a new `test_version_id` column plus backfill, and adding a join to
     every read path.
   - *Mutable `tests` + append-only history table*: smallest change, but runs would still pin
     nothing, leaving "what config did this run actually execute?" unanswerable — which
     defeats the point.

   Model A wins because `runs.test_id`'s existing foreign key **keeps working untouched and
   now pins the exact executed version as a side effect** — no migration on `runs`' FK, no
   change to run creation, and nothing in the JMX builder / `k8sjob` / watcher paths changes.
4. **API identity**: the API's `Test.id` is the **catalog** id (stable across versions); the
   version row's PK is exposed separately as `version_id`. Verified safe: the frontend never
   reads `run.test_id` — it is declared in the `Run` type in `frontend/lib/api-client.ts` but
   consumed nowhere, and every test id the client uses comes from `Test.id` via `ListTests`.
   Because the backfill sets `catalog_id = id`, existing API responses stay byte-identical and
   existing bookmarks, frontend code, and e2e specs keep working.
5. **Existing route semantics preserved**: `POST /api/tests/{id}/runs` treats `{id}` as a
   catalog id, resolves the latest version at start time, and pins it — giving both "run the
   current test" behavior and correct history. `GET /api/tests/{id}/runs` returns runs across
   *all* versions of that test, so the history page shows a whole test's history rather than
   fragmenting per version.
6. **Frontend**: out of scope. This is a backend ticket; the switcher keeps its hardcoded
   "Default" until a follow-up wires it to `GET /api/projects`.
7. **Migration versioning**: `10-Database.md` requires "Schema migrations shall be versioned",
   but `Migrate()` currently re-runs every embedded `.sql` on each boot relying on
   `IF NOT EXISTS`. Since this ticket adds migrations containing **data backfills that must run
   exactly once**, proper tracking is added here rather than deferred.

## Architecture

### Migration infrastructure — `backend/internal/store/postgres/postgres.go`

New table:

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

The two hand-written `//go:embed migrations/000N_*.sql` string vars are replaced by a single
`//go:embed migrations/*.sql` + `embed.FS`. `Migrate()` lists the FS, sorts by filename, parses
the leading integer as the version, and for each version not present in `schema_migrations`
applies the file and inserts the version row **in one transaction**. Adding a migration becomes
"add a file" — no Go change.

Existing deployments have 0001/0002 applied but unrecorded. Both are idempotent
(`CREATE TABLE IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`), so the first boot after this change
re-runs them as no-ops and records them. No bootstrap special case is needed.

### Schema — `0003_projects.sql`

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

### Schema — `0004_test_versioning.sql`

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

Two ids on `runs`, each with one job:
* `test_id` → the exact immutable version row that executed (pinning; FK unchanged).
* `test_catalog_id` → the test family (grouping). This denormalization is deliberate: it keeps
  run history a single-table filter instead of a join, and — decisively — lets `memstore`'s
  `RunStore` answer "runs for this test" without needing access to `TestStore`, keeping the two
  store implementations symmetric.

### Model — `backend/internal/model/model.go`

```go
type Test struct {
    ID              string    `json:"id"`         // catalog id — stable across versions
    VersionID       string    `json:"version_id"` // PK of this version row
    Version         int       `json:"version"`
    ProjectID       string    `json:"project_id"`
    Name            string    `json:"name"`
    TargetURL       string    `json:"target_url"`
    VirtualUsers    int       `json:"virtual_users"`
    DurationSeconds int       `json:"duration_seconds"`
    CreatedAt       time.Time `json:"created_at"` // when the test was FIRST created
    UpdatedAt       time.Time `json:"updated_at"` // when THIS version was cut
}

type Project struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
}
```

`Run` gains one field:

```go
    TestCatalogID string `json:"test_catalog_id"`
```

`CreatedAt` keeps its exact current meaning — the moment the test was first created — computed
as `MIN(created_at) OVER (PARTITION BY catalog_id)` rather than a denormalized column that
would have to lie about its own name. `UpdatedAt` is the version row's own `created_at`.

### Store — `backend/internal/store/store.go`

```go
type TestStore interface {
    // Signatures unchanged. CreateTest now assigns catalog_id and version=1;
    // ListTests returns the latest version per family; GetTest takes a catalog
    // id and returns that family's latest version.
    CreateTest(ctx context.Context, t *model.Test) error
    ListTests(ctx context.Context) ([]model.Test, error)
    GetTest(ctx context.Context, catalogID string) (*model.Test, error)

    // New.
    UpdateTest(ctx context.Context, t *model.Test) error                          // writes version N+1
    ListTestVersions(ctx context.Context, catalogID string) ([]model.Test, error) // newest first
}

type ProjectStore interface {
    ListProjects(ctx context.Context) ([]model.Project, error)
}
```

`RunStore.ListByTest(ctx, catalogID)` keeps its signature and switches to filtering
`runs.test_catalog_id`.

Postgres `ListTests` uses `DISTINCT ON (catalog_id) … ORDER BY catalog_id, version DESC` in a
subquery that also computes the family's `MIN(created_at)` as a window function; the outer
query then orders by **that family minimum**, descending. For today's data — where every test
has exactly one version, so the family minimum equals the row's own `created_at` — this
produces byte-identical ordering to the current `ORDER BY created_at DESC`. It also means
editing a test does not reshuffle the list, since a family's position is fixed by when it was
first created, not when it was last edited. `UpdateTest` inserts `version = (SELECT MAX(version) + 1 …)`
for the family; the unique index on `(catalog_id, version)` is what makes a concurrent
double-edit fail loudly instead of silently forking a version number.

That failure is part of the contract, not an incidental database error, so it gets a sentinel:
`store.ErrConflict` alongside the existing `store.ErrNotFound`. Postgres `UpdateTest` maps a
unique-violation (`SQLSTATE 23505`) on `idx_tests_catalog_version` to it; `memstore` returns it
when the version it computed is already occupied. Callers therefore distinguish "someone else
edited this test first" from a genuine failure without inspecting driver-specific errors.

`memstore` mirrors the same semantics over its map: group by `catalog_id`, take max version,
derive `CreatedAt` as the family minimum.

### API — `backend/internal/api/`

New routes:
* `PUT /api/tests/{testID}` → creates version N+1, responds `200` with the new latest.
  Returns `404` if the catalog id is unknown, `400` on the same validation rules
  `POST /api/tests` already enforces, and **`409 Conflict`** when `store.ErrConflict` comes
  back — i.e. a concurrent edit already claimed that version number. `409` is the honest code
  here: the request was valid and the client may retry against the new latest version. The
  eventual edit UI (a later ticket) must handle it rather than assuming success.
* `GET /api/tests/{testID}/versions` → all versions of a test, newest first.
* `GET /api/projects` → list projects.

Changed:
* `POST /api/tests` accepts an optional `project_id`, defaulting to the seeded Default project.
* `handleStartRun` sets `run.TestID = test.VersionID` and `run.TestCatalogID = test.ID`.

Unchanged: `GET /api/tests`, `GET /api/tests/{testID}/runs`, and every `/api/runs/*` route.

## Testing strategy

* **Store parity**: every new method tested against both `memstore` and `postgres`, asserting
  identical semantics — including that `UpdateTest` leaves the prior version readable and that
  `ListTests` returns exactly one row per family.
* **Nil-slice regression**: `ListTestVersions` and `ListProjects` each get a test asserting an
  empty (not nil) slice. This codebase already shipped that bug once — `ListTests`/
  `ListSnapshots`, fixed in `dc25371` — and every list method added since has carried a sibling
  test.
* **Migrations**: `schema_migrations` records each applied version; a second `Migrate()` call is
  a no-op; and running against a database already holding 0001/0002-era rows backfills
  `project_id`, `catalog_id`, and `test_catalog_id` correctly and exactly once.
* **Version pinning**: a run started before an edit still resolves to the config it actually
  executed after the edit — the correctness property the whole storage model exists to provide.
* **Conflict on concurrent edit**: both stores return `store.ErrConflict` when a version number
  is already claimed, and the `PUT` handler maps it to `409`. Postgres proves this against the
  real unique index rather than a simulated error; `memstore` proves the same contract holds for
  the in-memory implementation, so the two stay substitutable.
* **Handlers**: `PUT`, `/versions`, and `/projects` including 404 and validation paths.
* **Acceptance criterion**: every existing `memstore`, `postgres`, and handler test passes
  **unchanged**, and the frontend's existing unit and e2e suites pass untouched. The repo's 88%
  coverage gate continues to apply.

## Out of scope (explicitly deferred)

* Project/application/environment registry CRUD — BOL-49.
* Full versioned-catalog UX, import/export — BOL-50.
* Multi-step scenarios — BOL-51.
* Load profiles (ramp/step patterns) — BOL-52.
* Git-backed test definitions — BOL-150.
* Project-level RBAC — no auth exists yet (BOL-46).
* Any frontend change, including wiring `WorkspaceSwitcher`/`TreeNav` to real projects and a
  test-edit UI to exercise versioning.
* Deleting or archiving tests and versions.
