# BOL-22: Portal Shell with LoadRunner Enterprise Look and Feel

## Context

The walking skeleton (BOL-1..19) shipped a single-page dashboard and a single-run detail
page — deliberately minimal, no nav shell, no history, no admin area. BOL-22 ("Portal UI:
extend beyond single-run dashboard") is the first Story in the full-platform backlog
(`Implementation/32-Full-Project-Implementation-Backlog.md`, epic BOL-20 Platform
Foundation: Core Services). Its stated goal: grow `frontend/app` into a real multi-page
portal — project/team switcher, test history list, admin/settings area — while existing
pages keep working.

Layered on top: the whole app should look and feel like OpenText/Micro Focus LoadRunner
Enterprise (LRE), the commercial product BoltRunner is an open-source alternative to. None
of the Implementation docs captured LRE's actual visual design (every doc just says "Portal
UI Service" as a one-line bullet), so this was grounded in fresh research plus a visual
brainstorming session with the user (mockups at
`.superpowers/brainstorm/668536-1784919457/content/`).

**Research findings on real LRE UI** (from OpenText/Micro Focus help docs and product
pages): a top navigation bar with module dropdowns (Test Management, Test Runs, Dashboard,
Admin); a left-side hierarchical tree for folders/projects; tabbed detail panels (Tests and
Scripts, Test Details, Runs, Relations, Attachments, Audit); icon-button toolbars; dense
data grids; the active/selected element denoted with a blue border/underline.

## Decisions made during brainstorming

1. **Layout**: top module bar + left tree nav + tabbed content panels (closest match to
   real LRE, confirmed via visual mockup — user selected this over a lighter icon-rail
   variant and a top-bar-only variant).
2. **Theme**: two palettes, user-toggleable, not a single fixed look:
   - **Light ("Classic Enterprise Blue")**: deep navy chrome, white/light-gray content,
     blue accent on active tab/tree-node/primary actions, status badges (green/amber/red),
     compact 13px zebra-striped data grids. Matches the real LRE "blue border on active
     element" signal found in research. **Default on first visit.**
   - **Dark ("Modern Dark Enterprise")**: same structure, dark slate chrome, cyan/teal
     accent instead of blue, light-on-dark text.
   - Persisted per-user after their first toggle (localStorage).
3. **Restyle scope**: applies to the *entire* app now, not just new pages — existing
   dashboard and run-detail pages get the new shell/theme too. BOL-22's "continue to work
   unchanged" acceptance criterion is read as *behavior* unchanged, not *pixels* unchanged.
4. **Project/team switcher**: there is no project/team concept in the data model yet (that
   lands with BOL-28, Test Catalog Service). Build the switcher UI chrome for real, but wire
   it to a single hardcoded "Default" workspace — visually real, ready to extend, no
   invented multi-tenant data.
5. **Test history data**: the backend has no list-runs endpoint today (only fetch-by-ID).
   Add one small, additive endpoint (`GET /api/tests/{id}/runs`) rather than either
   inventing fake history data or leaving the history page empty. This is a minimal
   extension, not the full Result Repository epic (BOL-29, later).
6. **Component approach (Approach 1 of 3 considered)**: extract only the concrete UI
   primitives BOL-22's own pages need, built directly for their actual usage — not a
   speculative general-purpose design-system layer with APIs (sorting, pagination,
   multi-level generality) that nothing yet uses. A fuller design system can follow once 2-3
   more UI-heavy tickets reveal what shape is actually needed.
7. **Test coverage**: 88% applies to backend + frontend code touched/added by this ticket
   (unit-level: `go test -cover` / Vitest coverage), not the whole pre-existing repo.
   Playwright e2e tests run as functional verification alongside, not counted toward the
   88% number.

## Architecture

### Component library — `frontend/components/ui/`

New directory, one file per component, each built directly for what BOL-22's pages need:

| Component | Purpose | Props (shape, not full TS) |
|---|---|---|
| `ThemeProvider` / `useTheme()` | light/dark state | context provider; reads/writes `localStorage["boltrunner-theme"]`; applies/removes `dark` class on `<html>` |
| `ThemeToggle` | switch theme | button using `useTheme()` |
| `Shell` | page frame | `{children}`; renders `TopNav` + `TreeNav` + `Breadcrumb` + content slot |
| `TopNav` | module bar | `{modules: {label, href}[]}`; active module derived from current route |
| `TreeNav` | left nav | `{workspace: "Default", tests: Test[]}`; renders one expandable workspace node → test leaf nodes; active node derived from current route |
| `Breadcrumb` | trail under top bar | `{items: {label, href?}[]}` |
| `KpiTile` | dashboard summary tile | `{label, value}` |
| `StatusBadge` | run/test status pill | `{status: RunStatus}` → maps to pass/warn/fail/running/pending/stopped colors |
| `DataTable` | data grid | `{columns: {key, header, align?: 'numeric'}[], rows, onRowClick?}`; sticky header; numeric columns render monospace |
| `Card` | bordered content panel | `{children}` |
| `Tabs` | tab strip + active panel | `{tabs: {id, label}[], activeId, onChange, children}` |

### Theming

CSS variables added to `frontend/app/globals.css`:

```
--chrome-bg, --chrome-fg, --accent,
--surface, --surface-alt, --border, --text, --text-muted,
--status-pass-bg, --status-pass-fg,
--status-warn-bg, --status-warn-fg,
--status-fail-bg, --status-fail-fg,
--status-info-bg, --status-info-fg
```

`:root` holds the light ("Classic Enterprise Blue") values; `.dark` overrides them with the
dark ("Modern Dark Enterprise") values. `tailwind.config.ts` changes `darkMode` from the
implicit `media` default to `'class'`, and maps each token into `theme.extend.colors` so
components use semantic Tailwind classes (`bg-chrome`, `text-accent`, etc.) rather than
hardcoded hex values.

### Navigation content

- **TopNav modules**: Dashboard | Test Management | Test Runs | Admin.
- **TreeNav**: single "Default" workspace node, expanded by default, listing existing tests
  (from `GET /api/tests`) as leaf nodes; clicking a test navigates to its detail view.
- **Breadcrumb**: `Default` at the root, `Default / <Test Name>` on a run/test detail page.

### Pages

- **`frontend/app/layout.tsx`**: wraps the app in `ThemeProvider` + `Shell`.
- **`frontend/app/page.tsx`** (Dashboard / Test Management default view): adds a KPI strip
  with two tiles — Total Tests (`tests.length` from `GET /api/tests`) and Active Runs (count
  of tests whose most recent run via `GET /api/tests/{id}/runs` has status `running`, the
  same per-test runs fetch the history page uses — computed once client-side, not a separate
  backend aggregate endpoint); rebuilds the existing `TestList` on top of `DataTable` +
  `StatusBadge`; keeps `CreateTestForm` as-is.
- **`frontend/app/runs/[id]/page.tsx`**: rendered inside `Shell`/`Tabs` (Details / Metrics);
  `useRunPolling` and `LiveMetrics`/`MetricsChart` logic unchanged, only re-skinned.
- **`frontend/app/history/page.tsx`** (new): fetches all tests via `GET /api/tests`, then
  each test's runs via `GET /api/tests/{id}/runs`, merges and sorts by `started_at` desc,
  renders as `DataTable` (Test Name, Run #, Status, VUs, Started At) with row-click →
  `/runs/{id}`.
- **`frontend/app/admin/page.tsx`** (new): theme toggle + read-only platform info (API base
  URL from `NEXT_PUBLIC_API_URL`). Intentionally minimal — no admin actions requiring auth,
  since RBAC doesn't exist yet (that's BOL-46, later).

### Backend addition

- `store.RunStore` interface (`backend/internal/store/store.go`): add
  `ListByTest(ctx context.Context, testID string) ([]model.Run, error)`.
- `memstore` and `postgres` implementations of `ListByTest`. Postgres impl explicitly
  initializes `out := []model.Run{}` (not a nil `var` declaration) — this codebase already
  hit the nil-slice-encodes-as-JSON-`null` bug once (`ListTests`/`ListSnapshots`, fixed in
  commit `dc25371`); the regression test added then (`TestListTestsNeverReturnsNilSlice`)
  gets a sibling for this new method.
- New route: `GET /api/tests/{testID}/runs` → `handleListRunsForTest` in
  `backend/internal/api/runs.go`, returning runs newest-first.

## Testing strategy

- **Backend**: unit tests for `ListByTest` in both `memstore` and `postgres` (including the
  nil-slice regression test), and a handler test for `GET /api/tests/{testID}/runs`
  (including the not-found and empty-list cases). `go test -cover ./...` on touched
  packages targets ≥88% line coverage.
- **Frontend**: Vitest unit tests for every new `components/ui/` primitive and the 4 pages
  (`page.tsx`, `runs/[id]/page.tsx`, `history/page.tsx`, `admin/page.tsx`).
  `vitest.config.ts` gets a `coverage.thresholds` block (lines/statements/functions/branches
  ≥88%) scoped to `components/`, `app/`, `hooks/`, `lib/`.
- **Playwright**: extends `frontend/e2e/`:
  - shell renders with top nav + tree nav on every page;
  - theme toggle switches palette and persists across reload;
  - history page shows real run data and each row links to the correct run detail page;
  - admin page renders with theme toggle and platform info;
  - existing `walking-skeleton.spec.ts` (create → run → live metrics → complete; cancel)
    keeps passing unchanged — `DataTable` renders a real `<table>`/`<tr>` so the existing
    row-scoped `getByRole('row', {name: ...})` locators stay valid.
- **CI** (`.github/workflows/ci.yml`): `frontend-unit` and `backend-unit` jobs gain a
  coverage-threshold check so 88% is enforced, not just measured.

## Out of scope (explicitly deferred)

- Real multi-tenancy / multiple projects or teams (BOL-28, Test Catalog Service).
- Real admin actions (user management, RBAC) — no auth exists yet (BOL-46).
- Cross-test result analytics beyond a flat, newest-first history list (BOL-29, Result
  Repository, and BOL-45, Trend Analysis, go further).
- A general-purpose/generic design-system API (sortable/paginated `DataTable`, multi-level
  `TreeNav`) — deferred until a second consumer's needs are known.
