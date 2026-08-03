# Project Registry CRUD

## Context

`docs/superpowers/specs/2026-07-29-multi-project-workspaces-design.md` shipped the switcher with
list and create, and deferred the rest:

> **Rename and delete.** Backlog §3.1 material; neither has an endpoint, and delete raises what
> happens-to-its-tests questions that deserve their own design.
>
> **Moving a test between projects.** Requires deciding whether the move applies to one version
> or the whole family.

This spec answers both and closes them. It covers the *project* registry only — the application
and environment registries that share BOL-49 stay out of scope, as does per-project RBAC, which
has no auth layer to hang on (BOL-46).

Two facts found while exploring shape the work more than the feature request does.

**Renaming a project is not safe today.** `backend/internal/store/postgres/postgres.go:199`
resolves the fallback project by name on every test creation:

```sql
COALESCE($6, (SELECT id FROM projects WHERE name = 'Default'))
```

Rename `Default` and the subquery returns NULL. `tests.project_id` is `NOT NULL`, so
`POST /api/tests` without an explicit `project_id` starts failing — a path exercised by
`backend/internal/integration/walking_skeleton_test.go` and `frontend/e2e/walking-skeleton.spec.ts`.
Nothing renames a project today, so nothing catches it. Adding rename without addressing this
ships the break.

**A move cannot currently be tested at the API layer.** `backend/internal/store/memstore/memstore.go:30-38`
rejects any `project_id` other than the seeded `DefaultProjectID`, because memstore's `TestStore`
holds no reference to its `ProjectStore`. The API tests run against memstore, so a move's
destination project is unresolvable there. This is a prerequisite, not a nice-to-have.

## Decisions made during brainstorming

1. **Delete refuses a non-empty project.** `DELETE` returns 409 naming the count — "Payments
   still has 3 tests" — and the user empties it deliberately. This composes with the move
   feature in the same slice: moving is how you empty a project.

   Rejected: *cascade delete*, which irreversibly destroys the run history that the versioning
   work exists to preserve, behind one click, with no auth, no audit log and no undo.
   *Reassign to Default*, which silently relocates work the user is not looking at and turns
   Default into a junk drawer. *Archive*, which is fully reversible but adds an `archived`
   state that every project and test query must then filter on, plus an unarchive UI to keep
   it discoverable.

2. **A move applies to the whole test family.** Every row sharing the `catalog_id` gets the new
   `project_id`. A project is where a test is filed, not part of what a run executed:
   `jmx.Generate` reads only `target_url`, `virtual_users` and `duration_seconds`
   (`backend/internal/api/runs.go:35`), so no historical run's meaning changes. It also matches
   what `UpdateTest` already does — it carries the family's `project_id` forward rather than
   letting an edit change it.

   Rejected: *cut a new version*, which is truer to copy-on-write but splits one test's history
   across two projects, so `/tests/{id}/versions` would list versions the current project does
   not own. *Latest version only*, which leaves older versions behind and makes
   `ListTestsForProject` and `ListTestVersions` disagree about where the test lives.

3. **Rename and delete live on `/admin`.** That page is currently a one-line stub, and it is
   already wired into `MODULES`, `TreeNav` and the breadcrumb cases, so it costs no new
   navigation. Destructive actions belong somewhere entered deliberately rather than in a
   popover opened dozens of times a day — a misclick beside "select project" must not be able
   to delete one.

   Rejected: *inline in `WorkspaceSwitcher`*, which loads an everyday menu with a destructive
   action and would need the fixed-width popover to grow an edit state and a confirm state.
   *A new `/projects` page*, the cleanest long-term home once applications and environments
   arrive, but a new route, nav entry, breadcrumb case and `TreeNav` decision for one table.

4. **Moving is per-test, from `/tests/{id}`.** One obvious home for a test's properties.
   Emptying a three-test project takes three visits, which is tedious but explicit.

   Rejected: *bulk move from the blocked-delete message*, which answers the question the 409
   raises most directly but adds a batch endpoint and a good deal of UI and test surface to one
   slice. *Bulk-only*, which leaves no way to move a single test.

5. **The fallback project is identified by a flag, not a name.** A new `is_default` column
   replaces the `WHERE name = 'Default'` lookup, so every project including the seeded one is
   renameable, and delete refuses the flagged row on the honest grounds that it is the fallback
   target rather than because of what it is called.

   Rejected: *refuse to touch anything named `Default`*, which needs no migration but hard-codes
   a magic name into business logic, permanently forbids renaming the seeded project, and leaves
   the by-name coupling for the next feature to trip over. *Require `project_id` on
   `POST /api/tests`*, the cleanest end state, but it breaks the integration test, the
   walking-skeleton e2e and the optional `CreateTestInput.project_id` the frontend relies on
   when nothing is selected — unrelated churn for a CRUD slice.

## Architecture

### Schema — `backend/internal/store/postgres/migrations/0005_project_default_flag.sql`

```sql
ALTER TABLE projects ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX IF NOT EXISTS projects_one_default ON projects (is_default) WHERE is_default;

-- 0003 seeds 'Default', so an empty projects table should be unreachable. Seeding
-- anyway costs one statement and removes the failure mode outright: with no flagged
-- row, CreateTest's COALESCE yields NULL against a NOT NULL column, and every
-- project-less test creation starts failing.
INSERT INTO projects (name, is_default)
SELECT 'Default', true WHERE NOT EXISTS (SELECT 1 FROM projects);

-- Flag the project named 'Default' when there is one, else the oldest, else any --
-- rather than matching on the name alone, which silently flags nothing if the row
-- was ever renamed by hand. The NOT EXISTS guard makes a re-run a no-op instead of
-- a unique violation against the index above.
UPDATE projects SET is_default = true
WHERE id = (
    SELECT id FROM projects
    ORDER BY (name = 'Default') DESC, created_at ASC, id ASC
    LIMIT 1
)
AND NOT EXISTS (SELECT 1 FROM projects WHERE is_default);
```

The partial index makes "exactly one default project" a database invariant rather than a
convention: the index covers only rows where `is_default` is true, and every such row indexes
the same value, so a second one collides.

The `ORDER BY ... LIMIT 1` form is deliberate over the obvious `WHERE name = 'Default'`. The
spec's first draft assumed exactly one row carries that name — true of any database this code
has produced, since `0003` seeds it and no delete endpoint has ever existed. But the assumption
is unverifiable at migration time and fails silently if it is ever wrong: nothing gets flagged,
the migration reports success, and test creation breaks later at a place that gives no hint why.
Falling back to the oldest project makes the migration total.

### Migrating the deployed database

A live cluster (`kubectl -n boltrunner`) is running a backend image built 2026-07-24, before
`0003` and `0004` existed. Its database is still on the `0001`/`0002` schema — no
`schema_migrations`, no `projects`, and `tests` in its flat six-column form — holding 8 tests,
8 runs and 112 metric snapshots of real data.

So `0005` will not arrive on a database that already has projects. The first startup of a
current image runs `0003`, `0004` and `0005` back to back against nine days of pre-migration
rows. CI only ever exercises the chain from empty, which is the easy case: `0004`'s backfill of
`catalog_id` and `0005`'s flagging both have real rows to act on here and none there.

This is a pre-existing gap that this slice inherits rather than creates, but `0005` is the
migration that makes it worth closing — see the testing strategy.

### Model — `backend/internal/model/model.go`

`Project` gains one field, so the frontend can disable the delete action on the right row rather
than inferring it from the name:

```go
	IsDefault bool `json:"is_default"`
```

### Resolving the fallback — `postgres.go:199`

```sql
COALESCE($6, (SELECT id FROM projects WHERE is_default))
```

This is the only line that has to change for rename to become safe.

### Avoiding a dependency cycle

memstore's `TestStore` needs the project registry to validate a move destination. Checking
whether a project is empty needs the test store. Wiring both directions is a cycle.

**The emptiness check lives in the handler, not the store.** `handleDeleteProject` calls the
existing `ListTestsForProject` first and returns 409 with the count when it is non-empty;
`ProjectStore.DeleteProject` enforces only existence and default-protection. In postgres the
existing `tests.project_id` foreign key is the authoritative backstop — a delete racing the
count surfaces as SQLSTATE 23503, which maps to the same 409. memstore has no foreign key and
so cannot backstop, which is acceptable: it is single-process and the handler's check runs
inside the same request.

The dependency stays one-way, `TestStore → ProjectStore`, which is what lets memstore's
`TestStore` drop the `t.ProjectID != DefaultProjectID` hard-code at `memstore.go:32` and accept
any registered project.

### Store — `backend/internal/store/store.go`

```go
type ProjectStore interface {
	ListProjects(ctx context.Context) ([]model.Project, error)
	CreateProject(ctx context.Context, p *model.Project) error
	// RenameProject returns the updated project. ErrNotFound if no project has
	// that id; ErrConflict if another project already holds the name.
	RenameProject(ctx context.Context, id, name string) (*model.Project, error)
	// DeleteProject removes a project. ErrNotFound if no project has that id;
	// ErrProtected if it is the default project. It does NOT check for tests --
	// the handler does that, to keep the store dependency one-way.
	DeleteProject(ctx context.Context, id string) error
}
```

`TestStore` gains:

```go
	// MoveTest refiles every version of catalogID under projectID.
	// ErrNotFound if no such test; ErrInvalidReference if the project does not exist.
	MoveTest(ctx context.Context, catalogID, projectID string) error
```

A single `UPDATE tests SET project_id = $2 WHERE catalog_id = $1`; zero rows affected is
`ErrNotFound`, SQLSTATE 23503 is `ErrInvalidReference`. A malformed destination UUID is rejected
up front with `uuid.Parse`, for the same reason `CreateTest` does it (`postgres.go:188-192`): a
pgx encode failure is indistinguishable by type from a connection failure, so inferring "bad
input" from the error type would report outages as client errors.

One new sentinel, `store.ErrProtected`, for the default project. `ErrConflict` already means
"the name is taken" on this store, and the two map to different messages.

### API — `backend/internal/api/`

| Route | Body | Success |
|---|---|---|
| `PUT /api/projects/{projectID}` | `{"name": "..."}` | 200, the project |
| `DELETE /api/projects/{projectID}` | — | 204 |
| `PUT /api/tests/{testID}/project` | `{"project_id": "..."}` | 200, the test |

The move gets its own route rather than a field on `PUT /api/tests/{testID}`: an edit cuts a new
version, a move rewrites the whole family. Sharing a submit would make one request mean two
different things, and is why `testRequest.ProjectID` stays ignored on the update path.

Create and rename share their name validation — trim, non-empty, `projectNameMaxLen` — extracted
from `handleCreateProject` into a helper both call, so the two routes cannot drift.

## Error handling

| Case | Code | Body |
|---|---|---|
| rename → name already taken | 409 | `a project with that name already exists` |
| rename → blank or >100 chars | 400 | the existing create-path messages |
| rename or delete → unknown id | 404 | `project not found` |
| delete → the default project | 409 | `the default project cannot be deleted` |
| delete → project still has tests | 409 | `<name> still has <n> tests; move or delete them first` |
| delete → FK violation racing the count | 409 | `<name> still has tests; move or delete them first` — no count, because re-reading it after losing the race would report a number that was already stale |
| move → unknown test | 404 | `test not found` |
| move → unknown or malformed project | 400 | `unknown project_id` |
| any store failure | 500 | generic, as elsewhere |

## Frontend

### `frontend/lib/api-client.ts`

`Project` gains `is_default: boolean`. Three calls added: `renameProject(id, name)`,
`deleteProject(id)`, `moveTest(testId, projectId)`. `deleteProject` follows `cancelRun`'s shape,
tolerating 204 with no body.

### `frontend/components/ui/ProjectProvider.tsx`

Gains `rename(id, name)` and `remove(id)`.

`remove` has one non-obvious job: when the deleted project is the selected one, it must fall back
to the default project and rewrite `localStorage`. Otherwise the switcher points at nothing —
the same failure the stale-id guard at `ProjectProvider.tsx:34` handles on load, arrived at by a
different route.

`rename` re-sorts the list by name, matching what `create` already does so the menu order stays
stable across reloads.

### `frontend/app/admin/page.tsx`

The stub gains a `DataTable` of projects — Name, Created, actions. The existing API-base-URL card
stays.

Both actions are two-step and inline, not `window.confirm`: a native dialog needs a stub in jsdom
and a `page.on('dialog')` handler in Playwright, and neither leaves the intermediate state
assertable.

- **Rename** swaps that row's name cell for a text input with Save and Cancel. Save calls
  `rename`; a 409 renders the error beside the input and leaves the row in edit state so the
  name can be corrected.
- **Delete** swaps the action cell for "Delete <name>? Confirm / Cancel". On the default row the
  button is disabled with a `title` of `the default project cannot be deleted`. A 409 from a
  non-empty project renders the server's message — which names the count — in the row.

### `frontend/app/tests/[id]/page.tsx`

A "Move to project" select and button, visually separate from the edit form so the two
operations cannot be confused. On success, redirect to `/tests`.

The redirect is deliberate. Staying put leaves a detail page open for a test the selected
workspace no longer contains, beside a `TreeNav` still listing it under the old project —
`Shell`'s fetch keys on `selectedId`, which has not changed, so nothing refetches. The redirect
makes the effect visible and remounts the scoped list.

## Testing strategy

**Backend.** Store-level tests for rename, delete and move against both memstore and postgres,
including: rename to a taken name, delete of the default project, move of a multi-version family
(assert every version moved), and move to a nonexistent project. API tests covering every row of
the error-handling table. The 88% backend coverage gate holds.

**Migrations.** Three cases against a real Postgres, in `postgres_test.go`'s existing
`BOLTRUNNER_TEST_DSN` style:

1. From empty — the chain `0001…0005` leaves exactly one project flagged.
2. Idempotence — running `0005` twice leaves exactly one flagged row rather than raising a
   unique violation against `projects_one_default`.
3. **From the `0002` schema with data** — build the pre-migration tables by hand, insert a test
   and a run, then migrate and assert the test survived with a `catalog_id`, `version = 1` and
   the flagged project. This is the case the deployed database will actually take, and the one
   CI has never run.

A fourth case, a projects table with no row named `Default`, asserts the oldest project is
flagged rather than none.

**Frontend.** `api-client` tests for the three new calls. `ProjectProvider` tests for `rename`
and `remove`, including removing the *currently selected* project and asserting the fallback and
the `localStorage` rewrite. `AdminPage` tests for the table, the rename edit, the confirm step,
and the disabled default row. `TestDetailPage` tests for the move control and the redirect. The
88% frontend coverage gate holds.

**E2E.** `frontend/e2e/project-workspaces.spec.ts` is extended rather than duplicated: create a
project → create a test in it → attempt delete and assert the blocked message → move the test to
Default → delete the now-empty project → assert it is gone from the switcher. That sequence pins
the delete rule and the move feature against each other, which is the pairing decision 1 rests
on.

## Out of scope

- **Application and environment registries.** The rest of BOL-49; each is its own table, UI and
  scoping question.
- **Bulk move.** See decision 4.
- **Per-project RBAC.** BOL-46; there is no auth layer yet.
- **Undo, archive, or a trash state.** See decision 1.
- **A project detail page.** Still nothing to show beyond a name and a test list the main view
  already provides.
- **Changing a test's project through `PUT /api/tests/{testID}`.** `testRequest.ProjectID` stays
  ignored there, now because moves have their own route rather than because the feature is
  missing.
