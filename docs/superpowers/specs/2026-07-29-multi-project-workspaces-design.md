# Multi-Project Workspaces — Create, Select, and Scope Tests

## Context

`WorkspaceSwitcher` ships a visibly disabled `+ New project` button. The obvious reading is that only a `POST /api/projects` endpoint is missing, but the gap is wider on the frontend than on the backend.

Today:

- `Shell` calls `listProjects()` and displays `projects[0]?.name ?? 'Default'` — literally the first project, with no notion of a *selected* one.
- `WorkspaceSwitcher` renders exactly one `menuitemradio`, hardcoded `aria-checked="true"`, from the `projectName` prop.
- `GET /api/tests` returns every test regardless of `project_id`.

So adding only the endpoint would produce a feature with no observable effect: a created project would not appear in the menu, could not be selected, and would scope nothing. The single visible consequence would be a confusing one — if the new name sorted first, the workspace label would silently change.

The backend is further along than the frontend. `POST /api/tests` already accepts and validates `project_id`, returning `400 unknown project_id` via `store.ErrInvalidReference`. The schema already has what we need:

```sql
-- migrations/0003_projects.sql
CREATE TABLE IF NOT EXISTS projects (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`tests.project_id` is `NOT NULL` with a FK to `projects(id)`, and `CreateTest` COALESCEs a missing value to the seeded `Default` project. `ProjectStore` currently exposes only `ListProjects`.

This is the first change under `backend/` since BOL-28, so the work is no longer a frontend-only slice — it must clear the backend 88% coverage gate as well as the frontend one.

## Decisions made during brainstorming

1. **Full multi-project workspaces**, not a bare endpoint. Create, list, select, and scope the test list. This is the smallest slice where creating a project does something observable.
2. **Selection lives in `localStorage`**, under `boltrunner-project`, following the pattern `ThemeProvider` already uses. Nav links stay untouched. Accepted cost: the selection is not shareable via URL. The alternative — threading `?project=` through every link in `Shell`, `TreeNav`, `Breadcrumb` and every `router.push` — is invasive, and silently resets the selection anywhere the param is dropped.
3. **Inline create in the dropdown.** `+ New project` turns into a text input in place; Enter creates, selects, and closes. Escape returns to the menu. Reuses the menu's existing Escape and outside-click handling rather than introducing a dialog and focus trap.
4. **Duplicate names are rejected with 409**, reusing the `ErrConflict` → 409 mapping `handleUpdateTest` already established. The DB unique constraint makes this the natural contract.
5. **Creating a project selects it.** A create that left you in the previous workspace would read as a no-op.
6. **`GET /api/tests` without `project_id` stays unfiltered**, so existing callers and specs keep working.

## Architecture

### `backend/internal/store/store.go` (edit)

```go
type ProjectStore interface {
	ListProjects(ctx context.Context) ([]model.Project, error)
	CreateProject(ctx context.Context, p *model.Project) error
}
```

`CreateProject` populates `p.ID` and `p.CreatedAt` on success. A duplicate name returns `store.ErrConflict`; the caller does not inspect driver errors.

### `backend/internal/store/postgres/postgres.go` (edit)

`CreateProject` inserts and returns `id, created_at` via `RETURNING`. A `23505` unique violation maps to `store.ErrConflict`. Detection is on the SQLSTATE code, not on message text.

`ListTests` gains an optional project filter. Rather than change its signature for every caller, add:

```go
ListTestsForProject(ctx context.Context, projectID string) ([]model.Test, error)
```

`ListTests` keeps its current unfiltered behavior. This keeps the existing version-family ordering logic in one place, with the filter applied as an extra predicate.

### `backend/internal/store/memstore/projectstore.go` (edit)

Mirror the postgres semantics: `CreateProject` scans existing projects for a name match and returns `store.ErrConflict` before inserting. The memstore is what the API tests run against, so its contract must match postgres exactly, including the conflict.

### `backend/internal/api/projects.go` (edit)

```go
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request)
```

- Decode `{"name": string}`; a malformed body is `400 invalid body`.
- Trim the name. Empty after trimming is `400 name is required`.
- Names longer than 100 characters are `400`. `TEXT` has no limit, and the switcher's fixed-width menu degrades badly past that.
- `store.ErrConflict` → `409 a project with that name already exists`.
- Any other error → `500 failed to create project`.
- Success → `201` with the created project as JSON.

Route registration in `server.go`:

```go
s.router.Post("/api/projects", s.handleCreateProject)
```

### `backend/internal/api/tests.go` (edit)

`handleListTests` reads `r.URL.Query().Get("project_id")`. When empty, it calls `ListTests` as it does today. When set, it calls `ListTestsForProject`. An unknown id is not an error — it yields an empty list, matching how a project with no tests behaves.

### `frontend/lib/api-client.ts` (edit — purely additive)

```ts
export async function createProject(name: string): Promise<Project>;
export async function listTests(projectId?: string): Promise<Test[]>;
```

`listTests` appends `?project_id=` only when the argument is present, so existing call sites are unaffected. `createProject` throws `ApiError` with `.status`, as every other call already does.

### `frontend/components/ui/ProjectProvider.tsx` (new)

The one stateful unit. Client component holding:

```ts
{
  projects: Project[];
  selectedId: string | null;
  selected: Project | null;
  select(id: string): void;
  create(name: string): Promise<Project>;
  loading: boolean;
}
```

- On mount, fetches `listProjects()`.
- Reads `localStorage['boltrunner-project']` after mount, never during render, to avoid a hydration mismatch — the same guard `ThemeProvider` uses for `boltrunner-theme`.
- **If the stored id is not in the fetched list, falls back to the first project and rewrites storage.** `localStorage` outlives the database; every developer running against a fresh DB hits a stale id on their first load.
- `create(name)` posts, prepends to `projects`, selects the new project, and returns it. Errors propagate to the caller so the switcher can render them inline.
- A failed `listProjects()` degrades to an empty list and a `null` selection, matching the existing "don't break the shell over a projects failure" behavior in `Shell`.

### `frontend/components/ui/WorkspaceSwitcher.tsx` (edit)

Consumes the context instead of taking a `projectName` prop.

- Trigger label: the selected project's name, or `Default` when nothing is selected.
- Menu: one `menuitemradio` per project, `aria-checked` on the selected one. Clicking selects and closes.
- `+ New project` swaps in an inline text input, focused on open. Enter submits; Escape reverts to the menu without closing it.
- While the create is in flight the input is disabled.
- On error, the message renders under the input, **the input keeps focus and the typed value**, and the menu stays open.

### `frontend/components/ui/Shell.tsx` (edit)

Wraps its subtree in `ProjectProvider` and consumes the selection instead of calling `listProjects()` itself. This removes the `projects[0]?.name ?? 'Default'` hack. The test list it already fetches becomes `listTests(selectedId)`, refetched when the selection changes.

### `frontend/components/TestList.tsx` and `TreeNav.tsx` (edit)

No new fetching. Both already receive tests from above; they inherit scoping for free. `TreeNav`'s project label comes from the context rather than a prop.

### `frontend/components/CreateTestForm.tsx` (edit)

Sends `project_id: selectedId` in the create payload. When nothing is selected, the field is omitted and the backend COALESCEs to `Default`, exactly as today.

## Error handling

| Case | Status | UI |
|---|---|---|
| Duplicate name | 409 | Inline under the input; input keeps focus and typed value |
| Empty/whitespace name | 400 | Same; submit also disabled while empty |
| Name over 100 chars | 400 | Same |
| Malformed body | 400 | Not reachable from the UI; covered by API tests |
| Network/5xx | — | "Couldn't create project"; menu stays open |
| Stored id not in project list | — | Silent fallback to first project; storage rewritten |
| `listProjects()` fails | — | Empty switcher, shell still renders |

## Testing strategy

**Backend, postgres store:** create returns id and created_at; duplicate name returns `ErrConflict`; created project appears in `ListProjects`; `ListTestsForProject` returns only that project's tests and preserves the existing version-family ordering; an unknown project id returns an empty slice, not an error.

**Backend, memstore:** the same conflict contract, so API tests exercise identical semantics.

**Backend, API:** `201` shape; `400` for empty, whitespace-only, and over-length names; `400` for a malformed body; `409` for a duplicate; `GET /api/tests?project_id=` filters, and omitting the param does not.

**Frontend:** `ProjectProvider` — persists a selection, restores it on remount, falls back when the stored id is stale, and survives a `listProjects()` rejection. `WorkspaceSwitcher` — lists N projects with correct `aria-checked`, selects on click, creates inline, and on 409 keeps the typed value while showing the message. `CreateTestForm` — includes `project_id` when a project is selected and omits it when none is. `Shell` — refetches tests when the selection changes.

**E2E** (`frontend/e2e/project-workspaces.spec.ts`): create a timestamped project → switch to it → test list is empty → create a test in it → switch back to Default → the original list is unchanged and does not contain the new test. Timestamped names, per the convention now in all four existing specs.

Both coverage gates apply: backend 88% (`go test ./... -coverprofile`) and frontend 88% (`vitest --coverage`).

## Out of scope (explicitly deferred)

- **Rename and delete.** Backlog §3.1 material; neither has an endpoint, and delete raises what-happens-to-its-tests questions that deserve their own design.
- **Per-project RBAC.** §3.1 lists it; there is no auth layer to hang it on yet.
- **A project detail page.** Nothing to show beyond a name and a test list that the main view already provides.
- **Scoping run history.** Runs are not project-scoped in the model — `Run` references a test, and filtering `/history` means joining through to `project_id`. That is its own slice. `/history` stays honestly global rather than half-filtered.
- **Moving a test between projects.** Requires deciding whether the move applies to one version or the whole family.
