# Test Catalog UI — Editing, Version History, and Real Projects

## Context

BOL-28 shipped a versioned test catalog and a minimal project registry, and deliberately
deferred every frontend change. The result is working, tested API surface that no user can
reach:

* `PUT /api/tests/{id}` — records an edit as a new immutable version. No caller.
* `GET /api/tests/{id}/versions` — every version of a test, newest first. No caller.
* `GET /api/projects` — the seeded project registry. No caller.

Meanwhile the word `Default` is hardcoded in **three** places: `WorkspaceSwitcher.tsx`
(trigger label and menu item), `TreeNav.tsx` (folder label), and `Shell.tsx`'s
`breadcrumbFor` (the breadcrumb root).

There is no way to edit a test in the product at all — copy-on-write versioning exists in the
database but no user action can create a second version.

This spec covers the frontend work that makes all three endpoints reachable. It is not a
single Jira story: test editing and version history are the frontend half of **BOL-50**
(Versioned test catalog), and the project wiring is a down payment on **BOL-49** (Project /
Application / Environment registries). Both remain open afterwards — see "Out of scope".

## Decisions made during brainstorming

1. **Scope**: all three endpoints, including the project switcher. Wiring the switcher yields
   a dropdown with exactly one entry until BOL-49 adds project CRUD, so its payoff is removing
   three hardcoded strings and proving the endpoint — not new user capability. That was
   weighed and accepted; the visible payoff of this work is editing and version history.
2. **Placement** (chosen over an inline expanding row and a modal dialog): a dedicated
   `/tests/{id}` detail page, mirroring the existing `/runs/{id}` route. Editing and version
   history both need real estate, and the detail page gives version history a natural home
   without crowding `TestList` — which just gained a mobile stacked-card mode that an inline
   editor would fight.
3. **Loading via the versions endpoint.** `GET /api/tests/{id}` does **not exist** — the
   router registers no such route, so it returns 405. `GET /api/tests/{id}/versions` returns
   every version newest-first and 404s on an unknown id, so `versions[0]` *is* the current
   configuration and the rest *is* the history. One request serves both panels and gives the
   not-found state for free. (Rejected: `listTests()` then filter client-side — an extra
   round trip that fetches every test to display one, and cannot distinguish "no such test"
   from "not loaded yet".)
4. **Version history is read-only.** No restore, no re-run-this-version, no expandable rows.
   There is no endpoint to run a specific version — `POST /api/tests/{id}/runs` always runs
   the latest — so anything beyond viewing would need new backend work or would be a partial
   illusion. A restore button was considered and deferred: it is implementable today with
   `PUT`, but it creates v4 rather than reverting to v2, and that distinction needs UX work
   this spec does not want to carry.
5. **Navigation**: the test name in `TestList` and `TreeNav` links to `/tests/{id}`, which
   links onward to `/history?testId={id}`. The detail page becomes the hub for a test.
6. **`+ New project` stays disabled.** No create-project endpoint exists. Enabling it would be
   an illusion.

## Architecture

### `frontend/lib/api-client.ts` (edit — purely additive)

Extend the existing `Test` type with the three fields the backend already returns:

```ts
export type Test = {
  id: string;              // catalog id — stable across versions
  version_id: string;      // this version's own id
  version: number;
  project_id: string;
  name: string;
  target_url: string;
  virtual_users: number;
  duration_seconds: number;
  created_at: string;      // the family's creation time, not this version's
  updated_at?: string;
};

export type Project = { id: string; name: string; created_at: string };

export type UpdateTestInput = CreateTestInput;
```

`created_at` is the **family's** creation time on every version row — the backend deliberately
returns the family's `MIN(created_at)` so that editing a test does not reshuffle the list.
Version rows carry their own timestamp in `updated_at`. The version history table must use
`updated_at`, not `created_at`, or every row will show the same value.

Three new functions, following the existing `unwrap` pattern:

* `listTestVersions(testId): Promise<Test[]>` — `GET /api/tests/{id}/versions`, `cache: 'no-store'`.
* `updateTest(testId, input): Promise<Test>` — `PUT /api/tests/{id}`, JSON body.
* `listProjects(): Promise<Project[]>` — `GET /api/projects`, `cache: 'no-store'`, `?? []` like `listTests`.

No existing signature changes.

`unwrap` currently throws `Error(\`request failed (${res.status}): ${text}\`)`, which loses the
status code as structured data. `updateTest` needs to distinguish 404 from 409 from everything
else, so `unwrap` gains a typed error carrying `status`:

```ts
export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message); }
}
```

`unwrap` throws `ApiError` instead of `Error`. `ApiError extends Error`, so every existing
`catch (err) { err instanceof Error ? err.message : … }` site keeps working unchanged — this is
the reason for subclassing rather than returning a result object.

### `frontend/app/tests/[id]/page.tsx` (new)

Reads the route param with `useParams<{ id: string }>()` — the same way `app/runs/[id]/page.tsx`
does — and renders `<TestDetailPanel testId={params.id} />` under an `<h1>`.

It delegates to a component rather than composing inline as the runs page does, because the
detail panel carries fetch, save and conflict state that is far easier to test directly than
through a route. This follows the precedent set when `TestManagementPanel` was extracted out
of the dashboard page for the same reason.

### `frontend/components/TestDetailPanel.tsx` (new)

The only stateful component. Owns:

* `versions: Test[]`, `loadState: 'loading' | 'ready' | 'notfound' | 'error'`, `notice: string | null`.
* On mount and after a successful save: `listTestVersions(testId)`. An `ApiError` with
  `status === 404` sets `notfound`; any other failure sets `error`.
* `current = versions[0]` — the configuration the form is seeded from.
* `handleSave(input)`: `updateTest(testId, input)`, then reload the version list.

Renders, top to bottom: a heading with the test name and a **Run test** button (reusing the
existing `startRun` + `router.push('/runs/{id}')` flow from `TestManagementPanel`), the
`EditTestForm`, the `VersionHistoryTable`, and a link to `/history?testId={id}`.

### `frontend/components/TestFields.tsx` (new, extracted from `CreateTestForm`)

Presentational only — no fetching, no submit handling. Renders the four labelled inputs
(`Name`, `Target URL`, `Virtual users`, `Duration (seconds)`) with the validation attributes
that exist today: `required` on all four, `type="url"` on target URL, `type="number" min={1}`
on the two numerics. Props: the four current values plus one `onChange` per field (or a single
`onChange(field, value)` — implementer's choice, as no consumer depends on the shape).

The extraction exists because the create and edit forms must agree on validation. The backend
applies one rule to both routes — `name != "" && target_url != "" && virtual_users > 0 &&
duration_seconds > 0`, returning 400 with the same message from both — so two independently
maintained field sets would drift away from a single server contract.

The extraction is internal: same labels, same DOM, same attributes. **The existing
`CreateTestForm.test.tsx` passing unchanged is the safety net that proves it.**

### `frontend/components/CreateTestForm.tsx` (edit)

Keeps its state, submit handler, error display and reset-on-success behavior. Its JSX inputs
are replaced by `<TestFields …/>`. No prop or behavior change.

### `frontend/components/EditTestForm.tsx` (new)

`TestFields` seeded from the current version, plus a **Save as new version** submit button and
an inline error line. Local state initialises from the `current` prop and re-syncs when
`current.version_id` changes, so a reload after saving shows the newly saved values rather
than stale ones. Submitting calls the `onSave` prop; it does not fetch.

The button reads "Save as new version" rather than "Save" because that is literally what the
backend does, and the version history sitting directly below it makes the consequence visible.

### `frontend/components/VersionHistoryTable.tsx` (new)

Read-only `DataTable` over `versions`, `rowKey={(v) => v.version_id}`. Columns in order:
`Version` (rendered `v{n}` — first column, so it becomes the card title in the mobile stacked
-card mode `DataTable` already supports), `Target URL`, `Virtual users` (numeric),
`Duration (s)` (numeric), `Edited` (`updated_at`). No `onRowClick`, no actions.

`emptyMessage` is left at its `'No data.'` default: the prop is optional, and a test that
loaded successfully always has at least one version, so the empty branch is unreachable here.

### `frontend/components/ui/WorkspaceSwitcher.tsx` (edit)

Gains an optional `projectName?: string` prop, defaulting to `'Default'`. The trigger label and
the checked menu item both render it. Everything else — the open/close state, outside-click
handling, `Escape`-to-close with focus restored to the trigger, `aria-haspopup`/`aria-expanded`
/`menuitemradio` roles — is untouched. `+ New project` stays disabled.

Taking a prop rather than fetching keeps this component presentational and leaves `Shell` as
the single place that talks to the projects endpoint.

### `frontend/components/ui/TreeNav.tsx` (edit)

Gains the same optional `projectName?: string` prop, defaulting to `'Default'`, replacing the
hardcoded `📁 Default`. Its per-test `Link` href changes from `/history?testId={id}` to
`/tests/{id}`.

### `frontend/components/ui/TopNav.tsx` (edit)

Threads `projectName` through to `WorkspaceSwitcher`.

### `frontend/components/ui/Shell.tsx` (edit)

* A second effect alongside the existing `listTests()` one:
  `listProjects().then(ps => setProjectName(ps[0]?.name ?? 'Default')).catch(() => {})`.
  On failure the state keeps its `'Default'` initial value, so a projects-endpoint outage
  degrades to today's behavior rather than an empty switcher.
* Passes `projectName` to `TopNav` and `TreeNav`.
* `breadcrumbFor` uses `projectName` for the root label instead of the literal `'Default'`,
  and gains a `/tests/{id}` case: `[root, { label: 'Tests', href: '/tests' }, { label: testName ?? id }]`.
  The test name comes from the already-fetched `tests` array by matching the path id; it falls
  back to the id when not yet loaded, mirroring how the existing `/history` case handles
  `activeTest?.name`.

### `frontend/components/TestList.tsx` (edit)

The `name` column's `Link` href changes from `/history?testId={t.id}` to `/tests/{t.id}`. One
line. No column added — **Run** stays, and the name itself is the route into editing.

## Error handling

| Condition | Behavior |
|---|---|
| `GET /versions` → 404 | "Test not found" message, no form, no history table |
| `GET /versions` → other failure | "Couldn't load this test" with a retry button |
| `PUT` → 400 | Inline message from the response body — the server's validation text is already human-readable |
| `PUT` → 404 | Same not-found state as above; the test was deleted elsewhere |
| `PUT` → 409 | Reload the version list, show "This test was changed elsewhere — review and save again", **keep the user's edits in the form** |
| `PUT` → 5xx | Inline "Couldn't save" message; form state preserved |

The 409 case is the one worth being deliberate about. The backend returns it when a concurrent
edit already claimed the next version number, with the message "test was modified
concurrently; reload and retry". Discarding the user's typing on that path would lose work for
a conflict they did not cause, so the form keeps its values while the history below it
refreshes to show what landed.

## Testing strategy

**Unit (Vitest + Testing Library).** New test files for `TestDetailPanel`, `EditTestForm`,
`VersionHistoryTable`, and `TestFields`; extensions to `api-client.test.ts`,
`WorkspaceSwitcher.test.tsx`, `TreeNav.test.tsx`, `TestList.test.tsx`, and `Shell.test.tsx`.
Cases that must exist:

* `TestDetailPanel` seeds the form from `versions[0]` and lists every version.
* Saving calls `updateTest` and then re-fetches, and the form shows the new values.
* 404 on load renders the not-found state and no form.
* **409 on save keeps the typed values in the form** and shows the conflict notice — the
  behavior most likely to regress silently, since a naive implementation resets the form.
* `VersionHistoryTable` renders `updated_at`, not `created_at` — the guard against every row
  showing an identical family timestamp.
* `listProjects` failure leaves the switcher reading `Default` rather than blank.
* `api-client` throws `ApiError` carrying the status for 404 and 409.

**Existing tests that must change** — exactly two assertions, both href expectations:
`TestList.test.tsx:32` (`'/history?testId=1'` → `'/tests/1'`) and `TreeNav.test.tsx:14-15`.
Every other existing unit test must pass untouched; `CreateTestForm.test.tsx` in particular is
the proof that the `TestFields` extraction changed nothing.

**Playwright.** One new spec: create a test, open it by name, change virtual users, save, and
assert v2 appears in the version history above v1. The existing `portal-shell.spec.ts`,
`responsive-portal.spec.ts` and `walking-skeleton.spec.ts` specs are expected to pass
**completely unchanged** — verified against their current source, they navigate via the nav
links and bottom tab bar rather than by clicking a test name, so the changed href does not
affect them.

**Coverage.** The repo's 88% Vitest gate on lines, statements, functions and branches
continues to apply unchanged.

## Out of scope (explicitly deferred)

* **Creating, renaming or deleting projects** — no endpoint exists; `+ New project` stays
  disabled. BOL-49.
* **Switching between projects** — the registry seeds exactly one project ("Default") and no
  endpoint can add another, so there is nothing to switch to. Any `project_id` that names no
  existing project is rejected with 400. BOL-49.
* **Assigning a test to a project** — `POST /api/tests` accepts an optional `project_id`, but
  with one project the control would be a dropdown of one. BOL-49.
* **Restoring or re-running a past version** — read-only history, per decision 4. Re-running a
  specific version needs a backend endpoint that does not exist.
* **Diffing two versions** — the whole configuration is four fields already visible in each row.
* **Deleting tests or versions** — no endpoint; versions are immutable by design.
* **Applications and environments** — BOL-49's other two registries.
* Any backend change. This spec is frontend-only; every endpoint it calls already exists and
  is covered by tests.
