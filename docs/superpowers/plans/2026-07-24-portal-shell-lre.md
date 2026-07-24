# BOL-22: Portal Shell with LoadRunner Enterprise Look and Feel — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Grow BoltRunner's frontend from a single-page dashboard into a real multi-page portal — a persistent LoadRunner-Enterprise-style shell (top module bar + left tree nav + breadcrumb), a user-toggleable light/dark theme, a test history page, and an admin page — while every existing page keeps working.

**Architecture:** A new `frontend/components/ui/` library of small, directly-built (not speculative) UI primitives composes into a `Shell` layout that wraps the whole app via `app/layout.tsx`. Theming is CSS-variable-driven (`:root` / `.dark`) mapped into Tailwind's color palette, toggled via React context and persisted to `localStorage`. One small backend addition (`GET /api/tests/{testID}/runs`) supplies the real data the new history page and dashboard KPI strip need.

**Tech Stack:** Go 1.26, `github.com/go-chi/chi/v5`, PostgreSQL 16 (`pgx/v5`), Next.js 14.2.35 (App Router) + TypeScript, Tailwind CSS 3.4, Vitest 4 + `@vitest/coverage-v8`, `@testing-library/react`, Playwright.

## Global Constraints

- Restyle applies to the entire app now (existing dashboard + run-detail pages included), not just new pages — per the approved spec, "continue to work unchanged" means behavior, not pixels.
- No project/team data model exists yet — the workspace switcher is UI chrome wired to one hardcoded "Default" workspace, not invented multi-tenant data.
- No auth/RBAC exists yet — the admin page shows read-only platform info only, no admin actions.
- Component primitives are built directly for what this ticket's pages need (Approach 1) — no speculative generic APIs (no sorting/pagination on `DataTable`, no multi-level generality on `TreeNav`) beyond current usage.
- Test coverage target: ≥88% (lines/statements/functions/branches) on backend and frontend code touched/added by this ticket, enforced via `go tool cover` in CI and Vitest `coverage.thresholds`. Playwright e2e is functional verification, not counted toward the 88%.
- Existing Playwright spec `frontend/e2e/walking-skeleton.spec.ts` must keep passing unmodified — its locators (`getByRole('row', {name: ...})`, `getByRole('button', {name: /run/i})`, `getByText(/status: completed/i)`, etc.) must keep resolving to the same semantics after the restyle.
- Module path: `github.com/boltrunner/backend` (unchanged).

---

## File Structure

```
backend/
  internal/model/model.go                          — MODIFY: add Run.CreatedAt
  internal/store/store.go                           — MODIFY: add RunStore.ListByTest
  internal/store/memstore/runstore.go                — MODIFY: CreateRun sets CreatedAt; add ListByTest
  internal/store/postgres/migrations/0002_add_run_created_at.sql — CREATE
  internal/store/postgres/postgres.go                — MODIFY: embed migration 0002, run it in Migrate(); CreateRun/GetRun select created_at; add ListByTest
  internal/api/runs.go                               — MODIFY: add handleListRunsForTest
  internal/api/server.go                             — MODIFY: add GET /api/tests/{testID}/runs route

frontend/
  package.json                                       — MODIFY: add @vitest/coverage-v8, test:coverage script
  vitest.config.ts                                    — MODIFY: add coverage config + 88% thresholds
  tailwind.config.ts                                  — MODIFY: darkMode: 'class', semantic color tokens
  app/globals.css                                     — MODIFY: light/dark CSS variable tokens
  app/layout.tsx                                       — MODIFY: wrap in ThemeProvider + Shell, anti-flash script, title fix
  app/page.tsx                                         — MODIFY: KPI strip, use TestList as rebuilt
  app/runs/[id]/page.tsx                               — MODIFY: Tabs (Details/Metrics) + Card
  app/history/page.tsx                                 — CREATE
  app/admin/page.tsx                                   — CREATE
  components/TestList.tsx                              — MODIFY: rebuilt on DataTable + StatusBadge
  components/LiveMetrics.tsx                            — MODIFY: theme-aware classes only (no text/role changes)
  components/ui/theme.tsx                               — CREATE: ThemeProvider, useTheme
  components/ui/ThemeToggle.tsx                          — CREATE
  components/ui/StatusBadge.tsx                          — CREATE
  components/ui/KpiTile.tsx                              — CREATE
  components/ui/Breadcrumb.tsx                           — CREATE
  components/ui/Card.tsx                                 — CREATE
  components/ui/Tabs.tsx                                 — CREATE
  components/ui/DataTable.tsx                            — CREATE
  components/ui/TopNav.tsx                               — CREATE
  components/ui/TreeNav.tsx                              — CREATE
  components/ui/Shell.tsx                                — CREATE
  lib/api-client.ts                                      — MODIFY: Run.created_at (optional), listRunsForTest()
  __tests__/ThemeProvider.test.tsx                       — CREATE
  __tests__/ThemeToggle.test.tsx                         — CREATE
  __tests__/StatusBadge.test.tsx                          — CREATE
  __tests__/KpiTile.test.tsx                              — CREATE
  __tests__/Breadcrumb.test.tsx                           — CREATE
  __tests__/CardAndTabs.test.tsx                          — CREATE
  __tests__/DataTable.test.tsx                             — CREATE
  __tests__/TopNav.test.tsx                                — CREATE
  __tests__/TreeNav.test.tsx                                — CREATE
  __tests__/Shell.test.tsx                                  — CREATE
  __tests__/TestList.test.tsx                               — CREATE
  __tests__/DashboardPage.test.tsx                          — CREATE
  __tests__/RunPage.test.tsx                                 — CREATE
  __tests__/HistoryPage.test.tsx                             — CREATE
  __tests__/AdminPage.test.tsx                               — CREATE
  e2e/portal-shell.spec.ts                                   — CREATE

.github/workflows/ci.yml                                    — MODIFY: coverage-threshold steps
```

---

### Task 1: `Run.CreatedAt` field + migration

**Files:**
- Modify: `backend/internal/model/model.go`
- Create: `backend/internal/store/postgres/migrations/0002_add_run_created_at.sql`
- Modify: `backend/internal/store/postgres/postgres.go`
- Modify: `backend/internal/store/memstore/runstore.go`
- Test: `backend/internal/store/memstore/runstore_test.go` (extend existing)
- Test: `backend/internal/store/postgres/store_test.go` (extend existing)

**Interfaces:**
- Produces: `model.Run.CreatedAt time.Time` (json: `created_at`), always set on creation. Later tasks (`ListByTest`) sort by this field.

- [ ] **Step 1: Write the failing test** — append to `backend/internal/store/memstore/runstore_test.go`

```go
func TestCreateRunSetsCreatedAt(t *testing.T) {
	s := NewRunStore()
	before := time.Now().UTC()
	run := &model.Run{TestID: "t1", Status: model.RunPending}
	if err := s.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.CreatedAt.Before(before) || run.CreatedAt.After(time.Now().UTC()) {
		t.Fatalf("expected CreatedAt to be set to roughly now, got %v", run.CreatedAt)
	}
}
```

Add `"time"` to the imports of `runstore_test.go` if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/memstore/... -run TestCreateRunSetsCreatedAt -v`
Expected: FAIL — `run.CreatedAt` is the zero value (`0001-01-01 00:00:00 +0000 UTC`), which is before `time.Now()`, or a compile error if `model.Run` has no `CreatedAt` field yet (it doesn't — this is expected to fail to compile first).

- [ ] **Step 3: Add the field to the model** — `backend/internal/model/model.go`

Modify the `Run` struct:

```go
type Run struct {
	ID           string     `json:"id"`
	TestID       string     `json:"test_id"`
	Status       RunStatus  `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
}
```

- [ ] **Step 4: Set it in the memstore implementation** — `backend/internal/store/memstore/runstore.go`

Modify `CreateRun`:

```go
func (s *RunStore) CreateRun(ctx context.Context, r *model.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = uuid.NewString()
	r.CreatedAt = time.Now().UTC()
	s.runs[r.ID] = *r
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/memstore/... -v`
Expected: PASS (all memstore tests, including the new one and the pre-existing `TestRunStoreLifecycle`).

- [ ] **Step 6: Write the Postgres migration** — `backend/internal/store/postgres/migrations/0002_add_run_created_at.sql`

```sql
ALTER TABLE runs ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
```

- [ ] **Step 7: Embed and run the new migration, update CreateRun/GetRun** — `backend/internal/store/postgres/postgres.go`

Add the embed directive next to the existing one:

```go
//go:embed migrations/0001_init.sql
var migration0001 string

//go:embed migrations/0002_add_run_created_at.sql
var migration0002 string
```

Change `Migrate`:

```go
func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.Pool.Exec(ctx, migration0001); err != nil {
		return err
	}
	_, err := db.Pool.Exec(ctx, migration0002)
	return err
}
```

Change `CreateRun`:

```go
func (db *DB) CreateRun(ctx context.Context, r *model.Run) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO runs (test_id, status) VALUES ($1, $2) RETURNING id, created_at`,
		r.TestID, r.Status,
	).Scan(&r.ID, &r.CreatedAt)
}
```

Change `GetRun`:

```go
func (db *DB) GetRun(ctx context.Context, id string) (*model.Run, error) {
	var r model.Run
	err := db.Pool.QueryRow(ctx,
		`SELECT id, test_id, status, created_at, started_at, completed_at, error_message FROM runs WHERE id = $1`, id,
	).Scan(&r.ID, &r.TestID, &r.Status, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &r, err
}
```

- [ ] **Step 8: Write the failing Postgres test** — append to `backend/internal/store/postgres/store_test.go`

```go
func TestCreateRunSetsCreatedAt(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = db.CreateTest(ctx, tst)

	run := &model.Run{TestID: tst.ID, Status: model.RunPending}
	if err := db.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}

	got, err := db.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("expected GetRun to populate CreatedAt")
	}
}
```

- [ ] **Step 9: Run test to verify it fails, then passes**

Run: `cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" go test ./internal/store/postgres/... -run TestCreateRunSetsCreatedAt -v`

(If no local Postgres is running, start one: `docker run -d --rm -e POSTGRES_USER=boltrunner -e POSTGRES_PASSWORD=boltrunner -e POSTGRES_DB=boltrunner -p 5432:5432 postgres:16`.)

Expected before Step 6/7: FAIL (column doesn't exist / field zero). After: PASS.

- [ ] **Step 10: Run the full backend suite to confirm no regressions**

Run: `cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" go test ./...`
Expected: PASS — all existing tests (including `TestPostMetricsAndGetRun`, `TestRunStoreLifecycle`) still pass unmodified, since `CreatedAt` is additive.

- [ ] **Step 11: Commit**

```bash
git add backend/internal/model/model.go backend/internal/store/postgres/migrations/0002_add_run_created_at.sql backend/internal/store/postgres/postgres.go backend/internal/store/memstore/runstore.go backend/internal/store/memstore/runstore_test.go backend/internal/store/postgres/store_test.go
git commit -m "feat(backend): add Run.CreatedAt so run history can be ordered newest-first"
```

---

### Task 2: `RunStore.ListByTest` — interface + memstore

**Files:**
- Modify: `backend/internal/store/store.go`
- Modify: `backend/internal/store/memstore/runstore.go`
- Test: `backend/internal/store/memstore/runstore_test.go`

**Interfaces:**
- Consumes: `model.Run.CreatedAt` (Task 1).
- Produces: `store.RunStore.ListByTest(ctx context.Context, testID string) ([]model.Run, error)`; `memstore.RunStore.ListByTest` implementation. Later tasks (Task 3's Postgres impl, Task 4's handler) implement/consume this same signature.

- [ ] **Step 1: Write the failing test** — append to `backend/internal/store/memstore/runstore_test.go`

```go
func TestListByTestReturnsOnlyMatchingRunsNewestFirst(t *testing.T) {
	s := NewRunStore()
	ctx := context.Background()

	older := &model.Run{TestID: "t1", Status: model.RunCompleted}
	_ = s.CreateRun(ctx, older)
	time.Sleep(2 * time.Millisecond)
	newer := &model.Run{TestID: "t1", Status: model.RunRunning}
	_ = s.CreateRun(ctx, newer)
	other := &model.Run{TestID: "t2", Status: model.RunPending}
	_ = s.CreateRun(ctx, other)

	runs, err := s.ListByTest(ctx, "t1")
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs for t1, got %d", len(runs))
	}
	if runs[0].ID != newer.ID || runs[1].ID != older.ID {
		t.Fatalf("expected newest-first order, got %s then %s", runs[0].ID, runs[1].ID)
	}
}

func TestListByTestReturnsEmptySliceNotNil(t *testing.T) {
	s := NewRunStore()
	runs, err := s.ListByTest(context.Background(), "no-such-test")
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	if runs == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(runs) != 0 {
		t.Fatalf("expected 0 runs, got %d", len(runs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/memstore/... -run TestListByTest -v`
Expected: FAIL — `s.ListByTest` undefined.

- [ ] **Step 3: Add to the interface** — `backend/internal/store/store.go`

```go
type RunStore interface {
	CreateRun(ctx context.Context, r *model.Run) error
	GetRun(ctx context.Context, id string) (*model.Run, error)
	ListByTest(ctx context.Context, testID string) ([]model.Run, error)
	UpdateRunStatus(ctx context.Context, id string, status model.RunStatus, errMsg string) error
	AppendMetricSnapshot(ctx context.Context, s *model.RunMetricSnapshot) error
	LatestSnapshot(ctx context.Context, runID string) (*model.RunMetricSnapshot, error)
	ListSnapshots(ctx context.Context, runID string) ([]model.RunMetricSnapshot, error)
}
```

- [ ] **Step 4: Implement in memstore** — `backend/internal/store/memstore/runstore.go`

Add `"sort"` to imports, then add:

```go
func (s *RunStore) ListByTest(ctx context.Context, testID string) ([]model.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.Run{}
	for _, r := range s.runs {
		if r.TestID == testID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/memstore/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/store/store.go backend/internal/store/memstore/runstore.go backend/internal/store/memstore/runstore_test.go
git commit -m "feat(backend): add RunStore.ListByTest (interface + memstore)"
```

---

### Task 3: `RunStore.ListByTest` — Postgres implementation

**Files:**
- Modify: `backend/internal/store/postgres/postgres.go`
- Test: `backend/internal/store/postgres/store_test.go`

**Interfaces:**
- Consumes: `store.RunStore.ListByTest` signature (Task 2).
- Produces: `postgres.DB` satisfies `store.RunStore` fully (compile-time check already implicit via usage in `cmd/server/main.go`).

- [ ] **Step 1: Write the failing test** — append to `backend/internal/store/postgres/store_test.go`

```go
func TestListByTestNewestFirstAndNeverNil(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "list-by-test-check", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	_ = db.CreateTest(ctx, tst)

	// A brand-new test has no runs yet: must be an empty slice, not nil.
	none, err := db.ListByTest(ctx, tst.ID)
	if err != nil {
		t.Fatalf("ListByTest (empty): %v", err)
	}
	if none == nil {
		t.Fatal("expected an empty slice, got nil")
	}

	older := &model.Run{TestID: tst.ID, Status: model.RunCompleted}
	_ = db.CreateRun(ctx, older)
	time.Sleep(10 * time.Millisecond)
	newer := &model.Run{TestID: tst.ID, Status: model.RunRunning}
	_ = db.CreateRun(ctx, newer)

	runs, err := db.ListByTest(ctx, tst.ID)
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].ID != newer.ID || runs[1].ID != older.ID {
		t.Fatalf("expected newest-first order, got %s then %s", runs[0].ID, runs[1].ID)
	}
}
```

Add `"time"` to imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" go test ./internal/store/postgres/... -run TestListByTest -v`
Expected: FAIL — `db.ListByTest` undefined.

- [ ] **Step 3: Implement** — `backend/internal/store/postgres/postgres.go`

```go
func (db *DB) ListByTest(ctx context.Context, testID string) ([]model.Run, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, test_id, status, created_at, started_at, completed_at, error_message
		 FROM runs WHERE test_id = $1 ORDER BY created_at DESC`, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		var r model.Run
		if err := rows.Scan(&r.ID, &r.TestID, &r.Status, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" go test ./internal/store/postgres/... -v`
Expected: PASS — all Postgres tests, including the pre-existing `TestListTestsNeverReturnsNilSlice`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/postgres/postgres.go backend/internal/store/postgres/store_test.go
git commit -m "feat(backend): Postgres implementation of RunStore.ListByTest"
```

---

### Task 4: `GET /api/tests/{testID}/runs` handler

**Files:**
- Modify: `backend/internal/api/runs.go`
- Modify: `backend/internal/api/server.go`
- Test: `backend/internal/api/runs_test.go`

**Interfaces:**
- Consumes: `store.RunStore.ListByTest` (Task 2/3), `store.TestStore.GetTest` (existing).
- Produces: route `GET /api/tests/{testID}/runs` → `200 [model.Run...]` (or `[]` if none) / `404` if the test doesn't exist. This is what the frontend's `listRunsForTest` (Task 13) calls.

- [ ] **Step 1: Write the failing tests** — append to `backend/internal/api/runs_test.go`

```go
func TestListRunsForTest(t *testing.T) {
	s := newTestServer()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = s.testStore.CreateTest(nil, test)
	run1 := &model.Run{TestID: test.ID, Status: model.RunCompleted}
	_ = s.runStore.CreateRun(nil, run1)
	run2 := &model.Run{TestID: test.ID, Status: model.RunRunning}
	_ = s.runStore.CreateRun(nil, run2)

	req := httptest.NewRequest(http.MethodGet, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var runs []model.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
}

func TestListRunsForTestEmptyIsNotNull(t *testing.T) {
	s := newTestServer()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = s.testStore.CreateTest(nil, test)

	req := httptest.NewRequest(http.MethodGet, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "[]\n" {
		t.Fatalf("expected an empty JSON array, got %q", rec.Body.String())
	}
}

func TestListRunsForUnknownTest(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/tests/missing/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -run TestListRunsForTest -v`
Expected: FAIL — 404/no route matched (chi returns 404 for unmatched routes, so `TestListRunsForTest` and `TestListRunsForTestEmptyIsNotNull` fail on the 200 assertion; add a temporary print or just trust chi's default to confirm; simplest confirmation is that `TestListRunsForTest` fails because the body can't unmarshal into 2 runs from a 404 page body).

- [ ] **Step 3: Implement the handler** — append to `backend/internal/api/runs.go`

```go
func (s *Server) handleListRunsForTest(w http.ResponseWriter, r *http.Request) {
	testID := chi.URLParam(r, "testID")
	if _, err := s.testStore.GetTest(r.Context(), testID); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "test not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to load test", http.StatusInternalServerError)
		return
	}
	runs, err := s.runStore.ListByTest(r.Context(), testID)
	if err != nil {
		http.Error(w, "failed to load runs", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}
```

- [ ] **Step 4: Register the route** — `backend/internal/api/server.go`

```go
	s.router.Get("/api/tests/{testID}/runs", s.handleListRunsForTest)
```

(Add this line next to the existing `s.router.Post("/api/tests/{testID}/runs", s.handleStartRun)`.)

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/... -v`
Expected: PASS — all handler tests, including the new three and every pre-existing one (`TestStartRunCreatesJob`, `TestCancelRunDeletesJob`, etc.).

- [ ] **Step 6: Run the full backend suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: PASS, no vet warnings.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/runs.go backend/internal/api/server.go backend/internal/api/runs_test.go
git commit -m "feat(backend): GET /api/tests/{testID}/runs endpoint"
```

---

### Task 5: Vitest coverage tooling

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/vitest.config.ts`

**Interfaces:**
- Produces: `npm run test:coverage` script; `vitest.config.ts` coverage thresholds (88% lines/statements/functions/branches, scoped to `components/`, `app/`, `hooks/`, `lib/`). Task 24 (final verification) is the first task where this threshold is expected to actually pass — until all components/pages exist, running it will legitimately report under-target coverage or fail; that is expected at this point in the plan, not a bug.

- [ ] **Step 1: Install the coverage provider**

Run: `cd frontend && npm install -D @vitest/coverage-v8`
Expected: adds `@vitest/coverage-v8` to `devDependencies` in `package.json` and `package-lock.json`.

- [ ] **Step 2: Add the coverage config** — `frontend/vitest.config.ts`

```ts
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
    exclude: ['node_modules/**', 'e2e/**'],
    coverage: {
      provider: 'v8',
      include: ['components/**', 'app/**', 'hooks/**', 'lib/**'],
      exclude: ['**/*.d.ts', 'app/**/layout.tsx', 'app/fonts/**'],
      thresholds: {
        lines: 88,
        statements: 88,
        functions: 88,
        branches: 88,
      },
    },
  },
  resolve: {
    alias: { '@': path.resolve(__dirname, '.') },
  },
});
```

`app/layout.tsx` is excluded from coverage: it is a Next.js root layout that renders `<html>`/`<body>`, which Testing Library advises against mounting directly (see Task 16's note); its logic is covered indirectly through the `ThemeProvider`/`Shell` unit tests plus the Playwright e2e suite and `npm run build`.

- [ ] **Step 3: Add the npm script** — `frontend/package.json`

Add to `"scripts"`:

```json
    "test:coverage": "vitest run --coverage",
```

- [ ] **Step 4: Run it to confirm the tool works end-to-end**

Run: `cd frontend && npm run test:coverage`
Expected: the existing test suite runs and a coverage table prints per-file percentages. At this point in the plan the run may legitimately **fail the threshold** (not enough of the codebase is covered yet, since none of this ticket's new components exist) — that failure is expected here and is not a bug to fix in this task; Task 24 is where the threshold must actually be met.

- [ ] **Step 5: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/vitest.config.ts
git commit -m "test(frontend): add coverage tooling with an 88% threshold"
```

---

### Task 6: Theme tokens + `ThemeProvider`/`useTheme`

**Files:**
- Modify: `frontend/app/globals.css`
- Modify: `frontend/tailwind.config.ts`
- Create: `frontend/components/ui/theme.tsx`
- Test: `frontend/__tests__/ThemeProvider.test.tsx`

**Interfaces:**
- Produces: `ThemeProvider({children}): JSX.Element`, `useTheme(): {theme: 'light'|'dark', toggleTheme: () => void}`, Tailwind semantic color classes (`bg-chrome`, `text-chrome-fg`, `bg-accent`, `text-accent`, `border-accent`, `bg-surface`, `bg-surface-alt`, `border-border`, `text-text`, `text-text-muted`, `bg-status-pass-bg`, `text-status-pass-fg`, `bg-status-warn-bg`, `text-status-warn-fg`, `bg-status-fail-bg`, `text-status-fail-fg`, `bg-status-info-bg`, `text-status-info-fg`). Every later component/page task consumes these.

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/ThemeProvider.test.tsx`

```tsx
import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ThemeProvider, useTheme } from '@/components/ui/theme';

function Probe() {
  const { theme, toggleTheme } = useTheme();
  return (
    <div>
      <span data-testid="theme">{theme}</span>
      <button onClick={toggleTheme}>toggle</button>
    </div>
  );
}

describe('ThemeProvider', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.classList.remove('dark');
  });

  it('defaults to light theme with no dark class', () => {
    render(<ThemeProvider><Probe /></ThemeProvider>);
    expect(screen.getByTestId('theme').textContent).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('toggles to dark and applies the dark class', () => {
    render(<ThemeProvider><Probe /></ThemeProvider>);
    fireEvent.click(screen.getByText('toggle'));
    expect(screen.getByTestId('theme').textContent).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('persists the chosen theme to localStorage', () => {
    render(<ThemeProvider><Probe /></ThemeProvider>);
    fireEvent.click(screen.getByText('toggle'));
    expect(localStorage.getItem('boltrunner-theme')).toBe('dark');
  });

  it('reads a persisted theme on mount', () => {
    localStorage.setItem('boltrunner-theme', 'dark');
    render(<ThemeProvider><Probe /></ThemeProvider>);
    expect(screen.getByTestId('theme').textContent).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('throws when useTheme is used outside a ThemeProvider', () => {
    function Bare() {
      useTheme();
      return null;
    }
    expect(() => render(<Bare />)).toThrow('useTheme must be used within a ThemeProvider');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/ThemeProvider.test.tsx`
Expected: FAIL — `@/components/ui/theme` doesn't exist.

- [ ] **Step 3: Add the theme tokens** — `frontend/app/globals.css`

Replace the existing `:root` / dark-media-query block with:

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

:root {
  --chrome-bg: #0f2440;
  --chrome-fg: #e8edf5;
  --accent: #3b82f6;
  --surface: #ffffff;
  --surface-alt: #f4f6f9;
  --border: #d3d9e2;
  --text: #1f2937;
  --text-muted: #6b7280;
  --status-pass-bg: #e7f6ea;
  --status-pass-fg: #1e7d32;
  --status-warn-bg: #fdf3d8;
  --status-warn-fg: #946200;
  --status-fail-bg: #fdecea;
  --status-fail-fg: #c0392b;
  --status-info-bg: #e0ecfb;
  --status-info-fg: #1d4ed8;
}

.dark {
  --chrome-bg: #1a1f2b;
  --chrome-fg: #cbd5e1;
  --accent: #2dd4bf;
  --surface: #12161f;
  --surface-alt: #161b25;
  --border: #262d3a;
  --text: #e2e8f0;
  --text-muted: #94a3b8;
  --status-pass-bg: #17331f;
  --status-pass-fg: #4ade80;
  --status-warn-bg: #3a2f12;
  --status-warn-fg: #fbbf24;
  --status-fail-bg: #3b1f1f;
  --status-fail-fg: #f87171;
  --status-info-bg: #1e293b;
  --status-info-fg: #38bdf8;
}

body {
  color: var(--text);
  background: var(--surface-alt);
  font-family: Arial, Helvetica, sans-serif;
}

@layer utilities {
  .text-balance {
    text-wrap: balance;
  }
}
```

- [ ] **Step 4: Map tokens into Tailwind** — `frontend/tailwind.config.ts`

```ts
import type { Config } from "tailwindcss";

const config: Config = {
  darkMode: 'class',
  content: [
    "./pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        chrome: { DEFAULT: "var(--chrome-bg)", fg: "var(--chrome-fg)" },
        accent: "var(--accent)",
        surface: { DEFAULT: "var(--surface)", alt: "var(--surface-alt)" },
        border: "var(--border)",
        text: { DEFAULT: "var(--text)", muted: "var(--text-muted)" },
        status: {
          "pass-bg": "var(--status-pass-bg)",
          "pass-fg": "var(--status-pass-fg)",
          "warn-bg": "var(--status-warn-bg)",
          "warn-fg": "var(--status-warn-fg)",
          "fail-bg": "var(--status-fail-bg)",
          "fail-fg": "var(--status-fail-fg)",
          "info-bg": "var(--status-info-bg)",
          "info-fg": "var(--status-info-fg)",
        },
      },
    },
  },
  plugins: [],
};
export default config;
```

This makes classes like `bg-status-pass-bg` and `text-status-pass-fg` available (Tailwind flattens the nested `status` object with `-` joins).

- [ ] **Step 5: Implement `ThemeProvider`/`useTheme`** — `frontend/components/ui/theme.tsx`

```tsx
'use client';

import { createContext, useContext, useEffect, useState, ReactNode } from 'react';

export type Theme = 'light' | 'dark';
const STORAGE_KEY = 'boltrunner-theme';

type ThemeContextValue = { theme: Theme; toggleTheme: () => void };
const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>('light');

  useEffect(() => {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === 'dark' || stored === 'light') {
      setTheme(stored);
    }
  }, []);

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark');
  }, [theme]);

  function toggleTheme() {
    setTheme((prev) => {
      const next: Theme = prev === 'light' ? 'dark' : 'light';
      localStorage.setItem(STORAGE_KEY, next);
      return next;
    });
  }

  return <ThemeContext.Provider value={{ theme, toggleTheme }}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme must be used within a ThemeProvider');
  return ctx;
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/ThemeProvider.test.tsx`
Expected: PASS (all 5 cases).

- [ ] **Step 7: Commit**

```bash
git add frontend/app/globals.css frontend/tailwind.config.ts frontend/components/ui/theme.tsx frontend/__tests__/ThemeProvider.test.tsx
git commit -m "feat(frontend): light/dark theme tokens and ThemeProvider"
```

---

### Task 7: `ThemeToggle`

**Files:**
- Create: `frontend/components/ui/ThemeToggle.tsx`
- Test: `frontend/__tests__/ThemeToggle.test.tsx`

**Interfaces:**
- Consumes: `useTheme()` (Task 6).
- Produces: `ThemeToggle(): JSX.Element`, an accessible button (`aria-label="Toggle theme"`). Consumed by `TopNav` (Task 12) and the Playwright e2e suite (Task 23).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/ThemeToggle.test.tsx`

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ThemeProvider } from '@/components/ui/theme';
import { ThemeToggle } from '@/components/ui/ThemeToggle';

describe('ThemeToggle', () => {
  it('toggles the theme when clicked', () => {
    render(
      <ThemeProvider>
        <ThemeToggle />
      </ThemeProvider>
    );
    const button = screen.getByRole('button', { name: /toggle theme/i });
    expect(document.documentElement.classList.contains('dark')).toBe(false);
    fireEvent.click(button);
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    fireEvent.click(button);
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/ThemeToggle.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** — `frontend/components/ui/ThemeToggle.tsx`

```tsx
'use client';

import { useTheme } from '@/components/ui/theme';

export function ThemeToggle() {
  const { theme, toggleTheme } = useTheme();
  return (
    <button
      onClick={toggleTheme}
      aria-label="Toggle theme"
      className="text-chrome-fg px-2 py-1 rounded hover:bg-white/10"
    >
      {theme === 'light' ? '🌙' : '☀️'}
    </button>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/ThemeToggle.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/ThemeToggle.tsx frontend/__tests__/ThemeToggle.test.tsx
git commit -m "feat(frontend): ThemeToggle button"
```

---

### Task 8: `Run.created_at` (frontend type) + `listRunsForTest`

**Files:**
- Modify: `frontend/lib/api-client.ts`
- Test: `frontend/__tests__/api-client.test.ts`

**Interfaces:**
- Produces: `Run.created_at?: string` (optional — existing test literals across the codebase construct `Run` without it, e.g. `frontend/__tests__/LiveMetrics.test.tsx:10`, and must keep compiling); `listRunsForTest(testId: string): Promise<Run[]>`. Consumed by the Dashboard KPI strip (Task 18) and the History page (Task 20).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/api-client.test.ts`

```ts
import { describe, it, expect, vi, afterEach } from 'vitest';
import { listRunsForTest } from '@/lib/api-client';

describe('listRunsForTest', () => {
  afterEach(() => vi.restoreAllMocks());

  it('fetches runs for a test and returns them', async () => {
    const runs = [{ id: 'r1', test_id: 't1', status: 'completed', created_at: '2026-07-24T00:00:00Z' }];
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => runs,
    }) as unknown as typeof fetch;

    const result = await listRunsForTest('t1');
    expect(result).toEqual(runs);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/tests/t1/runs'),
      expect.objectContaining({ cache: 'no-store' })
    );
  });

  it('defaults to an empty array if the API returns null', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => null }) as unknown as typeof fetch;
    const result = await listRunsForTest('t1');
    expect(result).toEqual([]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/api-client.test.ts`
Expected: FAIL — `listRunsForTest` is not exported.

- [ ] **Step 3: Implement** — `frontend/lib/api-client.ts`

Modify the `Run` type (add `created_at`, matching the pattern already used elsewhere — optional because `model.Run.CreatedAt` was added on the backend after several existing frontend test fixtures were written without it):

```ts
export type Run = {
  id: string;
  test_id: string;
  status: RunStatus;
  created_at?: string;
  started_at?: string;
  completed_at?: string;
  error_message?: string;
};
```

Add the new function next to `getRun`:

```ts
export async function listRunsForTest(testId: string): Promise<Run[]> {
  const runs = await unwrap<Run[]>(await fetch(`${API_URL}/api/tests/${testId}/runs`, { cache: 'no-store' }));
  return runs ?? [];
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/api-client.test.ts`
Expected: PASS.

- [ ] **Step 5: Run the full frontend suite to confirm no regressions from the `Run` type change**

Run: `cd frontend && npm test`
Expected: PASS — in particular `__tests__/LiveMetrics.test.tsx` (which builds `Run` objects without `created_at`) still type-checks and passes, since the field is optional.

- [ ] **Step 6: Commit**

```bash
git add frontend/lib/api-client.ts frontend/__tests__/api-client.test.ts
git commit -m "feat(frontend): add Run.created_at and listRunsForTest to the API client"
```

---

### Task 9: `StatusBadge`

**Files:**
- Create: `frontend/components/ui/StatusBadge.tsx`
- Test: `frontend/__tests__/StatusBadge.test.tsx`

**Interfaces:**
- Consumes: `RunStatus` (`@/lib/api-client`), theme tokens (Task 6).
- Produces: `StatusBadge({status: RunStatus}): JSX.Element`. Consumed by `TestList` (Task 17) and the History page (Task 20).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/StatusBadge.test.tsx`

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StatusBadge } from '@/components/ui/StatusBadge';
import type { RunStatus } from '@/lib/api-client';

describe('StatusBadge', () => {
  const cases: [RunStatus, string][] = [
    ['pending', 'PENDING'],
    ['running', 'RUNNING'],
    ['completed', 'COMPLETED'],
    ['failed', 'FAILED'],
    ['stopped', 'STOPPED'],
  ];

  it.each(cases)('renders %s as %s', (status, label) => {
    render(<StatusBadge status={status} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/StatusBadge.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** — `frontend/components/ui/StatusBadge.tsx`

```tsx
import { RunStatus } from '@/lib/api-client';

const VARIANT: Record<RunStatus, { label: string; bg: string; fg: string }> = {
  pending: { label: 'PENDING', bg: 'bg-status-info-bg', fg: 'text-status-info-fg' },
  running: { label: 'RUNNING', bg: 'bg-status-info-bg', fg: 'text-status-info-fg' },
  completed: { label: 'COMPLETED', bg: 'bg-status-pass-bg', fg: 'text-status-pass-fg' },
  failed: { label: 'FAILED', bg: 'bg-status-fail-bg', fg: 'text-status-fail-fg' },
  stopped: { label: 'STOPPED', bg: 'bg-status-warn-bg', fg: 'text-status-warn-fg' },
};

export function StatusBadge({ status }: { status: RunStatus }) {
  const v = VARIANT[status];
  return (
    <span className={`inline-block text-xs px-2 py-0.5 rounded border border-border ${v.bg} ${v.fg}`}>
      {v.label}
    </span>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/StatusBadge.test.tsx`
Expected: PASS (5 cases).

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/StatusBadge.tsx frontend/__tests__/StatusBadge.test.tsx
git commit -m "feat(frontend): StatusBadge component"
```

---

### Task 10: `KpiTile`

**Files:**
- Create: `frontend/components/ui/KpiTile.tsx`
- Test: `frontend/__tests__/KpiTile.test.tsx`

**Interfaces:**
- Produces: `KpiTile({label: string, value: string | number}): JSX.Element`. Consumed by the Dashboard KPI strip (Task 18).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/KpiTile.test.tsx`

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { KpiTile } from '@/components/ui/KpiTile';

describe('KpiTile', () => {
  it('renders the label and value', () => {
    render(<KpiTile label="Total Tests" value={7} />);
    expect(screen.getByText('Total Tests')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/KpiTile.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** — `frontend/components/ui/KpiTile.tsx`

```tsx
export function KpiTile({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="border border-border rounded bg-surface px-4 py-3 flex flex-col gap-1">
      <span className="text-xs uppercase text-text-muted">{label}</span>
      <span className="text-2xl font-semibold text-text">{value}</span>
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/KpiTile.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/KpiTile.tsx frontend/__tests__/KpiTile.test.tsx
git commit -m "feat(frontend): KpiTile component"
```

---

### Task 11: `Breadcrumb`

**Files:**
- Create: `frontend/components/ui/Breadcrumb.tsx`
- Test: `frontend/__tests__/Breadcrumb.test.tsx`

**Interfaces:**
- Produces: `type BreadcrumbItem = {label: string, href?: string}`; `Breadcrumb({items: BreadcrumbItem[]}): JSX.Element`. Consumed by `Shell` (Task 15).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/Breadcrumb.test.tsx`

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Breadcrumb } from '@/components/ui/Breadcrumb';

describe('Breadcrumb', () => {
  it('renders each item as text', () => {
    render(<Breadcrumb items={[{ label: 'Default', href: '/' }, { label: 'Checkout Load' }]} />);
    expect(screen.getByText('Default')).toBeInTheDocument();
    expect(screen.getByText('Checkout Load')).toBeInTheDocument();
  });

  it('renders items with an href as links', () => {
    render(<Breadcrumb items={[{ label: 'Default', href: '/' }, { label: 'Checkout Load' }]} />);
    expect(screen.getByRole('link', { name: 'Default' })).toHaveAttribute('href', '/');
    expect(screen.queryByRole('link', { name: 'Checkout Load' })).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/Breadcrumb.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** — `frontend/components/ui/Breadcrumb.tsx`

```tsx
import Link from 'next/link';

export type BreadcrumbItem = { label: string; href?: string };

export function Breadcrumb({ items }: { items: BreadcrumbItem[] }) {
  return (
    <nav aria-label="Breadcrumb" className="text-sm text-text-muted px-4 py-2">
      {items.map((item, i) => (
        <span key={i}>
          {item.href ? (
            <Link href={item.href} className="hover:text-accent">
              {item.label}
            </Link>
          ) : (
            <span>{item.label}</span>
          )}
          {i < items.length - 1 && <span className="mx-1">/</span>}
        </span>
      ))}
    </nav>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/Breadcrumb.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/Breadcrumb.tsx frontend/__tests__/Breadcrumb.test.tsx
git commit -m "feat(frontend): Breadcrumb component"
```

---

### Task 12: `Card` + `Tabs`

**Files:**
- Create: `frontend/components/ui/Card.tsx`
- Create: `frontend/components/ui/Tabs.tsx`
- Test: `frontend/__tests__/CardAndTabs.test.tsx`

**Interfaces:**
- Produces: `Card({children}): JSX.Element`; `type TabItem = {id: string, label: string}`; `Tabs({tabs: TabItem[], activeId: string, onChange: (id: string) => void, children}): JSX.Element`. Consumed by the run-detail page (Task 19), history/admin pages (Tasks 20–21).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/CardAndTabs.test.tsx`

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Card } from '@/components/ui/Card';
import { Tabs } from '@/components/ui/Tabs';

describe('Card', () => {
  it('renders its children', () => {
    render(<Card>hello</Card>);
    expect(screen.getByText('hello')).toBeInTheDocument();
  });
});

describe('Tabs', () => {
  const tabs = [
    { id: 'a', label: 'A' },
    { id: 'b', label: 'B' },
  ];

  it('marks the active tab as selected', () => {
    render(
      <Tabs tabs={tabs} activeId="a" onChange={() => {}}>
        content
      </Tabs>
    );
    expect(screen.getByRole('tab', { name: 'A' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: 'B' })).toHaveAttribute('aria-selected', 'false');
  });

  it('calls onChange with the clicked tab id', () => {
    const onChange = vi.fn();
    render(
      <Tabs tabs={tabs} activeId="a" onChange={onChange}>
        content
      </Tabs>
    );
    fireEvent.click(screen.getByRole('tab', { name: 'B' }));
    expect(onChange).toHaveBeenCalledWith('b');
  });

  it('renders the children content', () => {
    render(
      <Tabs tabs={tabs} activeId="a" onChange={() => {}}>
        panel content
      </Tabs>
    );
    expect(screen.getByText('panel content')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/CardAndTabs.test.tsx`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement `Card`** — `frontend/components/ui/Card.tsx`

```tsx
import { ReactNode } from 'react';

export function Card({ children }: { children: ReactNode }) {
  return <div className="border border-border rounded bg-surface p-4">{children}</div>;
}
```

- [ ] **Step 4: Implement `Tabs`** — `frontend/components/ui/Tabs.tsx`

```tsx
'use client';

import { ReactNode } from 'react';

export type TabItem = { id: string; label: string };

export function Tabs({
  tabs,
  activeId,
  onChange,
  children,
}: {
  tabs: TabItem[];
  activeId: string;
  onChange: (id: string) => void;
  children: ReactNode;
}) {
  return (
    <div>
      <div className="flex gap-4 border-b border-border mb-3" role="tablist">
        {tabs.map((t) => (
          <button
            key={t.id}
            role="tab"
            aria-selected={t.id === activeId}
            onClick={() => onChange(t.id)}
            className={`pb-2 px-1 text-sm ${
              t.id === activeId ? 'border-b-2 border-accent text-accent' : 'text-text-muted'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div>{children}</div>
    </div>
  );
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/CardAndTabs.test.tsx`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/components/ui/Card.tsx frontend/components/ui/Tabs.tsx frontend/__tests__/CardAndTabs.test.tsx
git commit -m "feat(frontend): Card and Tabs components"
```

---

### Task 13: `DataTable`

**Files:**
- Create: `frontend/components/ui/DataTable.tsx`
- Test: `frontend/__tests__/DataTable.test.tsx`

**Interfaces:**
- Produces: `type Column<T> = {key: string, header: string, align?: 'numeric', render?: (row: T) => ReactNode}`; `DataTable<T>({columns: Column<T>[], rows: T[], rowKey: (row: T) => string, onRowClick?: (row: T) => void, emptyMessage?: string}): JSX.Element`. Renders a real `<table>`/`<tr>`/`<td>` so `getByRole('row', ...)` locators (used by the existing e2e spec and this ticket's new tests) keep working. Consumed by `TestList` (Task 17) and the History page (Task 20).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/DataTable.test.tsx`

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { DataTable, Column } from '@/components/ui/DataTable';

type Row = { id: string; name: string; count: number };

describe('DataTable', () => {
  const columns: Column<Row>[] = [
    { key: 'name', header: 'Name' },
    { key: 'count', header: 'Count', align: 'numeric' },
  ];
  const rows: Row[] = [{ id: '1', name: 'Alpha', count: 3 }];

  it('renders column headers and row cells as a real table', () => {
    render(<DataTable columns={columns} rows={rows} rowKey={(r) => r.id} />);
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeInTheDocument();
    expect(screen.getByRole('row', { name: /Alpha/i })).toBeInTheDocument();
  });

  it('shows the empty message when there are no rows', () => {
    render(<DataTable columns={columns} rows={[]} rowKey={(r) => r.id} emptyMessage="Nothing here" />);
    expect(screen.getByText('Nothing here')).toBeInTheDocument();
  });

  it('calls onRowClick with the row when a row is clicked', () => {
    const onRowClick = vi.fn();
    render(<DataTable columns={columns} rows={rows} rowKey={(r) => r.id} onRowClick={onRowClick} />);
    fireEvent.click(screen.getByRole('row', { name: /Alpha/i }));
    expect(onRowClick).toHaveBeenCalledWith(rows[0]);
  });

  it('uses a custom render function when provided', () => {
    const withRender: Column<Row>[] = [{ key: 'name', header: 'Name', render: (r) => <span>Custom {r.name}</span> }];
    render(<DataTable columns={withRender} rows={rows} rowKey={(r) => r.id} />);
    expect(screen.getByText('Custom Alpha')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/DataTable.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** — `frontend/components/ui/DataTable.tsx`

```tsx
import { ReactNode } from 'react';

export type Column<T> = {
  key: string;
  header: string;
  align?: 'numeric';
  render?: (row: T) => ReactNode;
};

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  onRowClick,
  emptyMessage = 'No data.',
}: {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  onRowClick?: (row: T) => void;
  emptyMessage?: string;
}) {
  if (rows.length === 0) {
    return <p className="text-text-muted p-4">{emptyMessage}</p>;
  }
  return (
    <table className="w-full text-sm border-collapse">
      <thead className="sticky top-0 bg-surface-alt">
        <tr>
          {columns.map((col) => (
            <th
              key={col.key}
              className={`text-left px-3 py-2 border-b border-border ${
                col.align === 'numeric' ? 'text-right font-mono' : ''
              }`}
            >
              {col.header}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, i) => (
          <tr
            key={rowKey(row)}
            onClick={onRowClick ? () => onRowClick(row) : undefined}
            className={`${i % 2 === 1 ? 'bg-surface-alt' : 'bg-surface'} ${
              onRowClick ? 'cursor-pointer hover:bg-surface-alt' : ''
            }`}
          >
            {columns.map((col) => (
              <td
                key={col.key}
                className={`px-3 py-2 border-b border-border ${
                  col.align === 'numeric' ? 'text-right font-mono' : ''
                }`}
              >
                {col.render ? col.render(row) : String((row as Record<string, unknown>)[col.key] ?? '')}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/DataTable.test.tsx`
Expected: PASS (4 cases).

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/DataTable.tsx frontend/__tests__/DataTable.test.tsx
git commit -m "feat(frontend): DataTable component"
```

---

### Task 14: `TopNav`

**Files:**
- Create: `frontend/components/ui/TopNav.tsx`
- Test: `frontend/__tests__/TopNav.test.tsx`

**Interfaces:**
- Consumes: `ThemeToggle` (Task 7).
- Produces: `type NavModule = {label: string, href: string}`; `TopNav({modules: NavModule[]}): JSX.Element`. Consumed by `Shell` (Task 16).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/TopNav.test.tsx`

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ThemeProvider } from '@/components/ui/theme';
import { TopNav } from '@/components/ui/TopNav';

vi.mock('next/navigation', () => ({
  usePathname: () => '/history',
}));

describe('TopNav', () => {
  const modules = [
    { label: 'Dashboard', href: '/' },
    { label: 'Test Runs', href: '/history' },
  ];

  it('renders every module label and the BoltRunner brand', () => {
    render(
      <ThemeProvider>
        <TopNav modules={modules} />
      </ThemeProvider>
    );
    expect(screen.getByText('BoltRunner')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Dashboard' })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: 'Test Runs' })).toHaveAttribute('href', '/history');
  });

  it('marks the module matching the current path as active', () => {
    render(
      <ThemeProvider>
        <TopNav modules={modules} />
      </ThemeProvider>
    );
    expect(screen.getByRole('link', { name: 'Test Runs' })).toHaveClass('border-accent');
    expect(screen.getByRole('link', { name: 'Dashboard' })).not.toHaveClass('border-accent');
  });

  it('includes a theme toggle', () => {
    render(
      <ThemeProvider>
        <TopNav modules={modules} />
      </ThemeProvider>
    );
    expect(screen.getByRole('button', { name: /toggle theme/i })).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/TopNav.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** — `frontend/components/ui/TopNav.tsx`

```tsx
'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { ThemeToggle } from '@/components/ui/ThemeToggle';

export type NavModule = { label: string; href: string };

export function TopNav({ modules }: { modules: NavModule[] }) {
  const pathname = usePathname();
  return (
    <header className="bg-chrome text-chrome-fg flex items-center justify-between px-4 py-2 text-sm">
      <div className="flex items-center gap-4">
        <span className="font-semibold">BoltRunner</span>
        {modules.map((m) => {
          const active = pathname === m.href;
          return (
            <Link key={m.label} href={m.href} className={`pb-1 ${active ? 'border-b-2 border-accent' : ''}`}>
              {m.label}
            </Link>
          );
        })}
      </div>
      <ThemeToggle />
    </header>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/TopNav.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/TopNav.tsx frontend/__tests__/TopNav.test.tsx
git commit -m "feat(frontend): TopNav component"
```

---

### Task 15: `TreeNav`

**Files:**
- Create: `frontend/components/ui/TreeNav.tsx`
- Test: `frontend/__tests__/TreeNav.test.tsx`

**Interfaces:**
- Consumes: `Test` type (`@/lib/api-client`).
- Produces: `TreeNav({tests: Test[], activeTestId?: string}): JSX.Element`. Renders one "Default" workspace node with each test as a leaf link to `/history?testId={id}` — this is how a test's history page (Task 20) is reached from navigation. Consumed by `Shell` (Task 16).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/TreeNav.test.tsx`

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { TreeNav } from '@/components/ui/TreeNav';

const tests = [
  { id: '1', name: 'Checkout Load', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
  { id: '2', name: 'Search Spike', target_url: 'http://y', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
];

describe('TreeNav', () => {
  it('renders the Default workspace and every test as a link', () => {
    render(<TreeNav tests={tests} />);
    expect(screen.getByText(/Default/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Checkout Load/i })).toHaveAttribute('href', '/history?testId=1');
    expect(screen.getByRole('link', { name: /Search Spike/i })).toHaveAttribute('href', '/history?testId=2');
  });

  it('highlights the active test', () => {
    render(<TreeNav tests={tests} activeTestId="2" />);
    expect(screen.getByRole('link', { name: /Search Spike/i })).toHaveClass('text-accent');
    expect(screen.getByRole('link', { name: /Checkout Load/i })).not.toHaveClass('text-accent');
  });

  it('renders with no tests without crashing', () => {
    render(<TreeNav tests={[]} />);
    expect(screen.getByText(/Default/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/TreeNav.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** — `frontend/components/ui/TreeNav.tsx`

```tsx
import Link from 'next/link';
import { Test } from '@/lib/api-client';

export function TreeNav({ tests, activeTestId }: { tests: Test[]; activeTestId?: string }) {
  return (
    <nav aria-label="Workspace" className="bg-surface-alt border-r border-border w-56 shrink-0 text-sm py-2">
      <div className="px-3 py-1 font-medium text-text">📁 Default</div>
      <ul>
        {tests.map((t) => (
          <li key={t.id}>
            <Link
              href={`/history?testId=${t.id}`}
              className={`block px-6 py-1 truncate ${
                t.id === activeTestId ? 'bg-accent/10 border-l-2 border-accent text-accent' : 'text-text'
              }`}
            >
              📄 {t.name}
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/TreeNav.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/TreeNav.tsx frontend/__tests__/TreeNav.test.tsx
git commit -m "feat(frontend): TreeNav component"
```

---

### Task 16: `Shell`

**Files:**
- Create: `frontend/components/ui/Shell.tsx`
- Test: `frontend/__tests__/Shell.test.tsx`

**Interfaces:**
- Consumes: `TopNav` (Task 14), `TreeNav` (Task 15), `Breadcrumb`/`BreadcrumbItem` (Task 11), `listTests` (`@/lib/api-client`, existing).
- Produces: `Shell({children}): JSX.Element` — the full page frame. Consumed by `app/layout.tsx` (Task 17).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/Shell.test.tsx`

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@/components/ui/theme';
import { Shell } from '@/components/ui/Shell';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  usePathname: () => '/',
  useSearchParams: () => new URLSearchParams(),
}));

describe('Shell', () => {
  it('renders the top nav, tree nav, breadcrumb and children', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: '1', name: 'Checkout Load', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);

    render(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );

    expect(screen.getByText('BoltRunner')).toBeInTheDocument();
    expect(screen.getByText('page content')).toBeInTheDocument();
    expect(screen.getByRole('navigation', { name: 'Workspace' })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('link', { name: /Checkout Load/i })).toBeInTheDocument());
  });

  it('shows a Default-only breadcrumb on the root path', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );
    expect(await screen.findByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent('Default');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/Shell.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** — `frontend/components/ui/Shell.tsx`

```tsx
'use client';

import { ReactNode, useEffect, useState } from 'react';
import { usePathname, useSearchParams } from 'next/navigation';
import { listTests, Test } from '@/lib/api-client';
import { TopNav } from '@/components/ui/TopNav';
import { TreeNav } from '@/components/ui/TreeNav';
import { Breadcrumb, BreadcrumbItem } from '@/components/ui/Breadcrumb';

const MODULES = [
  { label: 'Dashboard', href: '/' },
  { label: 'Test Management', href: '/' },
  { label: 'Test Runs', href: '/history' },
  { label: 'Admin', href: '/admin' },
];

function breadcrumbFor(pathname: string, testId: string | null, testName?: string): BreadcrumbItem[] {
  const root: BreadcrumbItem = { label: 'Default', href: '/' };
  if (pathname === '/') return [root];
  if (pathname === '/admin') return [root, { label: 'Admin' }];
  if (pathname === '/history') {
    return testId
      ? [root, { label: 'Test Runs', href: '/history' }, { label: testName ?? testId }]
      : [root, { label: 'Test Runs' }];
  }
  if (pathname.startsWith('/runs/')) {
    const runId = pathname.split('/')[2];
    return [root, { label: `Run ${runId}` }];
  }
  return [root];
}

export function Shell({ children }: { children: ReactNode }) {
  const [tests, setTests] = useState<Test[]>([]);
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const testId = searchParams.get('testId');

  useEffect(() => {
    listTests().then(setTests).catch(() => setTests([]));
  }, []);

  const activeTest = tests.find((t) => t.id === testId);
  const crumbs = breadcrumbFor(pathname, testId, activeTest?.name);

  return (
    <div className="min-h-screen flex flex-col bg-surface-alt text-text">
      <TopNav modules={MODULES} />
      <div className="flex flex-1">
        <TreeNav tests={tests} activeTestId={testId ?? undefined} />
        <div className="flex-1 flex flex-col">
          <Breadcrumb items={crumbs} />
          <main className="flex-1 p-6">{children}</main>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/Shell.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/Shell.tsx frontend/__tests__/Shell.test.tsx
git commit -m "feat(frontend): Shell layout composing TopNav, TreeNav, and Breadcrumb"
```

---

### Task 17: Wire `app/layout.tsx`

**Files:**
- Modify: `frontend/app/layout.tsx`

**Interfaces:**
- Consumes: `ThemeProvider` (Task 6), `Shell` (Task 16).
- Produces: every page in the app now renders inside the portal shell.

No dedicated unit test for this file: it is a Next.js Server Component root layout that renders `<html>`/`<body>`, which Testing Library explicitly advises against mounting directly. Its logic (theme application, shell composition) is already covered by the `ThemeProvider` (Task 6) and `Shell` (Task 16) unit tests; the integrated behavior is verified by the Playwright e2e suite (Task 23) and `npm run build` (Task 24).

- [ ] **Step 1: Rewrite the layout** — `frontend/app/layout.tsx`

```tsx
import type { Metadata } from "next";
import localFont from "next/font/local";
import { Suspense } from "react";
import "./globals.css";
import { ThemeProvider } from "@/components/ui/theme";
import { Shell } from "@/components/ui/Shell";

const geistSans = localFont({
  src: "./fonts/GeistVF.woff",
  variable: "--font-geist-sans",
  weight: "100 900",
});
const geistMono = localFont({
  src: "./fonts/GeistMonoVF.woff",
  variable: "--font-geist-mono",
  weight: "100 900",
});

export const metadata: Metadata = {
  title: "BoltRunner",
  description: "Open-source, Kubernetes-native load testing.",
};

// Applies a persisted dark theme before hydration so there's no flash of the
// light theme on load for users who chose dark last time.
const THEME_INIT_SCRIPT = `
(function() {
  try {
    var stored = localStorage.getItem('boltrunner-theme');
    if (stored === 'dark') document.documentElement.classList.add('dark');
  } catch (e) {}
})();
`;

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
      </head>
      <body className={`${geistSans.variable} ${geistMono.variable} antialiased`}>
        <ThemeProvider>
          <Suspense fallback={null}>
            <Shell>{children}</Shell>
          </Suspense>
        </ThemeProvider>
      </body>
    </html>
  );
}
```

`Shell` is wrapped in `<Suspense>` because it uses `useSearchParams()`, which Next.js App Router requires to be inside a Suspense boundary to avoid de-opting the whole app to client-side rendering.

- [ ] **Step 2: Verify the dev server boots and the shell renders**

Run: `cd frontend && npm run build`
Expected: build succeeds with no TypeScript or React errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/app/layout.tsx
git commit -m "feat(frontend): wrap the app in ThemeProvider and Shell"
```

---

### Task 18: Rebuild `TestList` on `DataTable` + `StatusBadge`

**Files:**
- Modify: `frontend/components/TestList.tsx`
- Test: `frontend/__tests__/TestList.test.tsx`

**Interfaces:**
- Consumes: `DataTable`/`Column` (Task 13).
- Produces: same public props as before — `TestList({tests: Test[], onStart: (testId: string) => void}): JSX.Element` — so `app/page.tsx` doesn't need its call site changed. Preserves the exact empty-state text ("No tests yet — create one above.") and exact "Run" button text so the existing Playwright spec's locators keep matching.

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/TestList.test.tsx`

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TestList } from '@/components/TestList';

const tests = [
  { id: '1', name: 'Checkout Load', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
];

describe('TestList', () => {
  it('shows the empty message when there are no tests', () => {
    render(<TestList tests={[]} onStart={() => {}} />);
    expect(screen.getByText('No tests yet — create one above.')).toBeInTheDocument();
  });

  it('renders a row per test with a Run button', () => {
    render(<TestList tests={tests} onStart={() => {}} />);
    expect(screen.getByRole('row', { name: /Checkout Load/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /run/i })).toBeInTheDocument();
  });

  it('calls onStart with the test id when Run is clicked', () => {
    const onStart = vi.fn();
    render(<TestList tests={tests} onStart={onStart} />);
    fireEvent.click(screen.getByRole('button', { name: /run/i }));
    expect(onStart).toHaveBeenCalledWith('1');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/TestList.test.tsx`
Expected: FAIL — the current `TestList` renders a plain `<table>` without a Run button whose accessible name matches after this test's expectations are laid over it (specifically, this is a fresh test file, so run it now to confirm it passes against the *old* implementation first is unnecessary; instead confirm the file compiles and the *new* assertions using `getByRole('row', ...)` still trivially pass against the old plain-table impl, then move to Step 3 to layer in `DataTable`). To keep this a true red step, temporarily rename the import in the test to a not-yet-existing named export is unnecessary here since `TestList` already exists — instead, run the suite now and confirm all three cases already pass against the pre-existing implementation (they do, since the old markup already satisfies them), then proceed to Step 3's refactor and re-run in Step 4 to confirm behavior is preserved. This task is a refactor-with-safety-net: the test is the safety net, not a red/green gate on new behavior.

- [ ] **Step 3: Rebuild the component** — `frontend/components/TestList.tsx`

```tsx
'use client';

import { Test } from '@/lib/api-client';
import { DataTable, Column } from '@/components/ui/DataTable';

export function TestList({ tests, onStart }: { tests: Test[]; onStart: (testId: string) => void }) {
  const columns: Column<Test>[] = [
    { key: 'name', header: 'Name' },
    { key: 'target_url', header: 'Target URL' },
    { key: 'virtual_users', header: 'Virtual users', align: 'numeric' },
    { key: 'duration_seconds', header: 'Duration (s)', align: 'numeric' },
    {
      key: 'actions',
      header: '',
      render: (t) => <button onClick={() => onStart(t.id)}>Run</button>,
    },
  ];

  return (
    <DataTable columns={columns} rows={tests} rowKey={(t) => t.id} emptyMessage="No tests yet — create one above." />
  );
}
```

- [ ] **Step 4: Run test to verify it still passes**

Run: `cd frontend && npx vitest run __tests__/TestList.test.tsx`
Expected: PASS (3 cases) — confirms the `DataTable`-based rebuild preserves the exact text/roles the old hand-rolled table had.

- [ ] **Step 5: Commit**

```bash
git add frontend/components/TestList.tsx frontend/__tests__/TestList.test.tsx
git commit -m "refactor(frontend): rebuild TestList on DataTable"
```

---

### Task 19: Restyle `app/page.tsx` — KPI strip

**Files:**
- Modify: `frontend/app/page.tsx`
- Test: `frontend/__tests__/DashboardPage.test.tsx`

**Interfaces:**
- Consumes: `KpiTile` (Task 10), `listRunsForTest` (Task 8), `TestList` (Task 18, unchanged props).
- Produces: Dashboard now shows Total Tests and Active Runs KPI tiles above the existing create-form/test-list.

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/DashboardPage.test.tsx`

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import DashboardPage from '@/app/page';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe('DashboardPage', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('shows Total Tests and Active Runs KPI tiles computed from fetched data', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: '1', name: 'A', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
      { id: '2', name: 'B', target_url: 'http://y', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockImplementation(async (testId: string) =>
      testId === '1' ? [{ id: 'r1', test_id: '1', status: 'running', created_at: '2026-07-24T00:00:00Z' }] : []
    );

    render(<DashboardPage />);

    const totalTile = await screen.findByText('Total Tests');
    expect(totalTile.closest('div')?.textContent).toContain('2');

    const activeTile = await screen.findByText('Active Runs');
    expect(activeTile.closest('div')?.textContent).toContain('1');
  });

  it('still renders the create form and test list', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([]);
    render(<DashboardPage />);
    expect(screen.getByRole('button', { name: /create test/i })).toBeInTheDocument();
    expect(await screen.findByText('No tests yet — create one above.')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/DashboardPage.test.tsx`
Expected: FAIL — no "Total Tests"/"Active Runs" text exists yet.

- [ ] **Step 3: Implement** — `frontend/app/page.tsx`

```tsx
'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { listTests, listRunsForTest, startRun, Test } from '@/lib/api-client';
import { CreateTestForm } from '@/components/CreateTestForm';
import { TestList } from '@/components/TestList';
import { KpiTile } from '@/components/ui/KpiTile';

export default function DashboardPage() {
  const [tests, setTests] = useState<Test[]>([]);
  const [activeRuns, setActiveRuns] = useState(0);
  const router = useRouter();

  useEffect(() => {
    listTests()
      .then((fetched) => {
        setTests(fetched);
        return Promise.all(fetched.map((t) => listRunsForTest(t.id)));
      })
      .then((runLists) => {
        const running = runLists.flat().filter((r) => r.status === 'running').length;
        setActiveRuns(running);
      })
      .catch(() => {
        setTests([]);
        setActiveRuns(0);
      });
  }, []);

  async function handleStart(testId: string) {
    const run = await startRun(testId);
    router.push(`/runs/${run.id}`);
  }

  function handleCreated(t: Test) {
    setTests((prev) => [t, ...prev]);
  }

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-text">BoltRunner</h1>
      <div className="grid grid-cols-2 gap-4 max-w-md">
        <KpiTile label="Total Tests" value={tests.length} />
        <KpiTile label="Active Runs" value={activeRuns} />
      </div>
      <CreateTestForm onCreated={handleCreated} />
      <TestList tests={tests} onStart={handleStart} />
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/DashboardPage.test.tsx`
Expected: PASS.

- [ ] **Step 5: Run the existing `CreateTestForm` test to confirm no regression**

Run: `cd frontend && npx vitest run __tests__/CreateTestForm.test.tsx`
Expected: PASS unmodified — `CreateTestForm` itself wasn't touched.

- [ ] **Step 6: Commit**

```bash
git add frontend/app/page.tsx frontend/__tests__/DashboardPage.test.tsx
git commit -m "feat(frontend): dashboard KPI strip (Total Tests, Active Runs)"
```

---

### Task 20: Restyle `app/runs/[id]/page.tsx` — Tabs

**Files:**
- Modify: `frontend/app/runs/[id]/page.tsx`
- Modify: `frontend/components/LiveMetrics.tsx` (theme-aware classes only)
- Test: `frontend/__tests__/RunPage.test.tsx`

**Interfaces:**
- Consumes: `Tabs`/`TabItem` (Task 12), `Card` (Task 12).
- Produces: run-detail page now shows Details/Metrics tabs. `LiveMetrics`'s rendered text and roles (`Status: {status}`, the `Cancel` button) are unchanged — only Tailwind classes are updated to use theme tokens — so the pre-existing `frontend/__tests__/LiveMetrics.test.tsx` and the Playwright walking-skeleton spec keep passing unmodified. (A `StatusBadge` is deliberately *not* added inside `LiveMetrics`: its label text — e.g. "RUNNING" — would also match the existing test's `screen.getByText(/running/i)` query alongside the `Status: running` heading, turning a single-match query into a multi-match "Found multiple elements" failure. `StatusBadge` is used instead in `TestList` and the History page, which have no such pre-existing brittle text query.)

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/RunPage.test.tsx`

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import RunPage from '@/app/runs/[id]/page';
import { useRunPolling } from '@/hooks/useRunPolling';

vi.mock('next/navigation', () => ({
  useParams: () => ({ id: 'r1' }),
}));
vi.mock('@/hooks/useRunPolling');
vi.mock('@/lib/api-client', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api-client')>('@/lib/api-client');
  return { ...actual, cancelRun: vi.fn() };
});

describe('RunPage', () => {
  it('shows a loading state while data is null', () => {
    vi.mocked(useRunPolling).mockReturnValue({ data: null, error: null });
    render(<RunPage />);
    expect(screen.getByText(/loading/i)).toBeInTheDocument();
  });

  it('shows an error message when polling fails', () => {
    vi.mocked(useRunPolling).mockReturnValue({ data: null, error: 'boom' });
    render(<RunPage />);
    expect(screen.getByText('boom')).toBeInTheDocument();
  });

  it('renders the Details tab by default with live metrics', () => {
    vi.mocked(useRunPolling).mockReturnValue({
      data: { run: { id: 'r1', test_id: 't1', status: 'running' }, history: [] },
      error: null,
    });
    render(<RunPage />);
    expect(screen.getByText(/status: running/i)).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Details' })).toHaveAttribute('aria-selected', 'true');
  });

  it('switches to the Metrics tab on click', () => {
    vi.mocked(useRunPolling).mockReturnValue({
      data: { run: { id: 'r1', test_id: 't1', status: 'running' }, history: [] },
      error: null,
    });
    render(<RunPage />);
    fireEvent.click(screen.getByRole('tab', { name: 'Metrics' }));
    expect(screen.getByText(/waiting for the first metric snapshot/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/RunPage.test.tsx`
Expected: FAIL — no `role="tab"` elements exist yet in the current page.

- [ ] **Step 3: Implement the page** — `frontend/app/runs/[id]/page.tsx`

```tsx
'use client';

import { useState } from 'react';
import { useParams } from 'next/navigation';
import { useRunPolling } from '@/hooks/useRunPolling';
import { cancelRun } from '@/lib/api-client';
import { LiveMetrics } from '@/components/LiveMetrics';
import { MetricsChart } from '@/components/MetricsChart';
import { Tabs } from '@/components/ui/Tabs';
import { Card } from '@/components/ui/Card';

export default function RunPage() {
  const params = useParams<{ id: string }>();
  const { data, error } = useRunPolling(params.id);
  const [activeTab, setActiveTab] = useState('details');

  if (error) return <p className="text-red-600">{error}</p>;
  if (!data) return <p>Loading…</p>;

  return (
    <div className="flex flex-col gap-4">
      <Tabs
        tabs={[
          { id: 'details', label: 'Details' },
          { id: 'metrics', label: 'Metrics' },
        ]}
        activeId={activeTab}
        onChange={setActiveTab}
      >
        {activeTab === 'details' && (
          <Card>
            <LiveMetrics run={data.run} latest={data.latest} onCancel={() => cancelRun(params.id)} />
          </Card>
        )}
        {activeTab === 'metrics' && (
          <Card>
            <MetricsChart history={data.history} />
          </Card>
        )}
      </Tabs>
    </div>
  );
}
```

- [ ] **Step 4: Make `LiveMetrics` theme-aware without changing its text/roles** — `frontend/components/LiveMetrics.tsx`

```tsx
'use client';

import { Run, RunMetricSnapshot } from '@/lib/api-client';

const ACTIVE_STATUSES = new Set(['pending', 'running']);

export function LiveMetrics({
  run,
  latest,
  onCancel,
}: {
  run: Run;
  latest?: RunMetricSnapshot;
  onCancel: () => void;
}) {
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center gap-4">
        <h2 className="text-xl text-text">Status: {run.status}</h2>
        {ACTIVE_STATUSES.has(run.status) && <button onClick={onCancel}>Cancel</button>}
      </div>
      {run.error_message && <p className="text-red-600">{run.error_message}</p>}
      <div className="grid grid-cols-4 gap-4">
        <div>
          <div className="text-sm text-text-muted">Throughput (req/s)</div>
          <div className="text-2xl text-text">{latest ? latest.throughput_rps.toFixed(1) : '—'}</div>
        </div>
        <div>
          <div className="text-sm text-text-muted">Avg response time (ms)</div>
          <div className="text-2xl text-text">{latest ? latest.avg_response_time_ms.toFixed(0) : '—'}</div>
        </div>
        <div>
          <div className="text-sm text-text-muted">Error rate (%)</div>
          <div className="text-2xl text-text">{latest ? latest.error_rate_pct.toFixed(1) : '—'}</div>
        </div>
        <div>
          <div className="text-sm text-text-muted">Elapsed (s)</div>
          <div className="text-2xl text-text">{latest ? latest.elapsed_seconds : '—'}</div>
        </div>
      </div>
    </section>
  );
}
```

Only class names changed (`text-gray-500` → `text-text-muted`, added `text-text` to value/heading spans) — no text content, elements, or roles were added, removed, or reordered.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/RunPage.test.tsx`
Expected: PASS.

- [ ] **Step 6: Run the pre-existing `LiveMetrics` test to confirm zero regression**

Run: `cd frontend && npx vitest run __tests__/LiveMetrics.test.tsx`
Expected: PASS unmodified — confirms the theme-class-only change didn't touch anything the test depends on.

- [ ] **Step 7: Commit**

```bash
git add frontend/app/runs/[id]/page.tsx frontend/components/LiveMetrics.tsx frontend/__tests__/RunPage.test.tsx
git commit -m "feat(frontend): restyle run-detail page with Details/Metrics tabs"
```

---

### Task 21: `app/history/page.tsx`

**Files:**
- Create: `frontend/app/history/page.tsx`
- Test: `frontend/__tests__/HistoryPage.test.tsx`

**Interfaces:**
- Consumes: `listTests`, `listRunsForTest` (Task 8), `DataTable`/`Column` (Task 13), `StatusBadge` (Task 9).
- Produces: `/history` route, optionally filtered by `?testId=` (the link `TreeNav`, Task 15, points to).

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/HistoryPage.test.tsx`

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import HistoryPage from '@/app/history/page';
import * as api from '@/lib/api-client';

const push = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push }),
  useSearchParams: () => new URLSearchParams(),
}));

describe('HistoryPage', () => {
  it('merges runs across all tests and sorts newest first', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: 't1', name: 'Checkout', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([
      { id: 'r1', test_id: 't1', status: 'completed', created_at: '2026-07-24T00:00:01Z' },
      { id: 'r2', test_id: 't1', status: 'failed', created_at: '2026-07-24T00:00:02Z' },
    ]);

    render(<HistoryPage />);

    const rows = await screen.findAllByRole('row');
    expect(rows[1]).toHaveTextContent('r2');
    expect(rows[2]).toHaveTextContent('r1');
  });

  it('navigates to the run detail page when a row is clicked', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: 't1', name: 'Checkout', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([
      { id: 'r1', test_id: 't1', status: 'completed', created_at: '2026-07-24T00:00:01Z' },
    ]);

    render(<HistoryPage />);
    const row = await screen.findByRole('row', { name: /r1/i });
    fireEvent.click(row);
    expect(push).toHaveBeenCalledWith('/runs/r1');
  });

  it('shows the empty message when there are no runs', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(<HistoryPage />);
    expect(await screen.findByText('No runs yet.')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/HistoryPage.test.tsx`
Expected: FAIL — `@/app/history/page` doesn't exist.

- [ ] **Step 3: Implement** — `frontend/app/history/page.tsx`

```tsx
'use client';

import { useEffect, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { listTests, listRunsForTest, Run, Test } from '@/lib/api-client';
import { DataTable, Column } from '@/components/ui/DataTable';
import { StatusBadge } from '@/components/ui/StatusBadge';

type HistoryRow = Run & { testName: string };

export default function HistoryPage() {
  const [rows, setRows] = useState<HistoryRow[]>([]);
  const [loaded, setLoaded] = useState(false);
  const router = useRouter();
  const searchParams = useSearchParams();
  const testId = searchParams.get('testId');

  useEffect(() => {
    async function load() {
      const tests: Test[] = await listTests();
      const filtered = testId ? tests.filter((t) => t.id === testId) : tests;
      const perTest = await Promise.all(
        filtered.map(async (t) => {
          const runs = await listRunsForTest(t.id);
          return runs.map((r) => ({ ...r, testName: t.name }));
        })
      );
      const merged = perTest
        .flat()
        .sort((a, b) => (a.created_at && b.created_at ? b.created_at.localeCompare(a.created_at) : 0));
      setRows(merged);
      setLoaded(true);
    }
    load().catch(() => setLoaded(true));
  }, [testId]);

  const columns: Column<HistoryRow>[] = [
    { key: 'testName', header: 'Test' },
    { key: 'id', header: 'Run' },
    { key: 'status', header: 'Status', render: (r) => <StatusBadge status={r.status} /> },
    { key: 'started_at', header: 'Started At' },
  ];

  if (!loaded) return <p>Loading…</p>;

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold text-text">Test Runs</h1>
      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(r) => r.id}
        onRowClick={(r) => router.push(`/runs/${r.id}`)}
        emptyMessage="No runs yet."
      />
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/HistoryPage.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/app/history/page.tsx frontend/__tests__/HistoryPage.test.tsx
git commit -m "feat(frontend): test history page"
```

---

### Task 22: `app/admin/page.tsx`

**Files:**
- Create: `frontend/app/admin/page.tsx`
- Test: `frontend/__tests__/AdminPage.test.tsx`

**Interfaces:**
- Consumes: `Card` (Task 12).
- Produces: `/admin` route with read-only platform info. (Deliberately does *not* render its own `ThemeToggle` — the global one lives in `TopNav` via `Shell`; a second toggle on this page would give `getByRole('button', {name: /toggle theme/i})` two matches and break Playwright's strict-mode single-element requirement.)

- [ ] **Step 1: Write the failing test** — `frontend/__tests__/AdminPage.test.tsx`

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import AdminPage from '@/app/admin/page';

describe('AdminPage', () => {
  it('renders the API base URL', () => {
    render(<AdminPage />);
    expect(screen.getByText(/API base URL/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/AdminPage.test.tsx`
Expected: FAIL — `@/app/admin/page` doesn't exist.

- [ ] **Step 3: Implement** — `frontend/app/admin/page.tsx`

```tsx
'use client';

import { Card } from '@/components/ui/Card';

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080';

export default function AdminPage() {
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold text-text">Admin</h1>
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

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/AdminPage.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/app/admin/page.tsx frontend/__tests__/AdminPage.test.tsx
git commit -m "feat(frontend): admin page with platform info"
```

---

### Task 23: CI coverage-threshold steps

**Files:**
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `go test -coverprofile` (Go stdlib), `npm run test:coverage` (Task 5).
- Produces: `backend-unit` and `frontend-unit` CI jobs fail if coverage drops below 88%.

- [ ] **Step 1: Add a Go coverage-threshold step to `backend-unit`**

In `.github/workflows/ci.yml`, replace the existing `- name: Test` step under `backend-unit` with:

```yaml
      - name: Test with coverage
        working-directory: backend
        env:
          BOLTRUNNER_TEST_DSN: "postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable"
        run: |
          go test ./... -coverprofile=coverage.out
          go tool cover -func=coverage.out
          PCT=$(go tool cover -func=coverage.out | grep total: | awk '{print substr($3, 1, length($3)-1)}')
          echo "Total coverage: ${PCT}%"
          awk -v pct="$PCT" 'BEGIN { if (pct + 0 < 88) { print "Coverage " pct "% is below the 88% threshold"; exit 1 } }'
```

- [ ] **Step 2: Point `frontend-unit` at the coverage script**

Replace the existing `run: npm test` step under `frontend-unit` with:

```yaml
      - working-directory: frontend
        run: npm run test:coverage
```

(`vitest.config.ts`'s `coverage.thresholds`, added in Task 5, already fails the process on its own if any metric drops below 88% — no extra shell logic is needed here.)

- [ ] **Step 3: Verify the workflow YAML is well-formed**

Run: `cd /home/belo/Documents/AI_Projects/BoltRunner && python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo OK`
Expected: `OK` (no YAML syntax errors). If `python3`/`pyyaml` isn't available, visually re-check indentation against the surrounding steps instead.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: enforce an 88% coverage threshold on backend and frontend unit tests"
```

---

### Task 24: Playwright e2e additions

**Files:**
- Create: `frontend/e2e/portal-shell.spec.ts`

**Interfaces:**
- Consumes: the full running stack (backend + frontend + kind cluster), same as the existing `frontend/e2e/walking-skeleton.spec.ts`.

- [ ] **Step 1: Write the new spec** — `frontend/e2e/portal-shell.spec.ts`

```ts
import { test, expect } from '@playwright/test';

test('portal shell renders top nav, tree nav, and breadcrumb on the dashboard', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText('BoltRunner')).toBeVisible();
  await expect(page.getByRole('link', { name: 'Dashboard' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Test Runs' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Admin' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Workspace' })).toBeVisible();
});

test('theme toggle switches to dark mode and persists across reload', async ({ page }) => {
  await page.goto('/');
  const toggle = page.getByRole('button', { name: /toggle theme/i });
  await toggle.click();
  await expect(page.locator('html')).toHaveClass(/dark/);

  await page.reload();
  await expect(page.locator('html')).toHaveClass(/dark/);
});

test('history page lists a real run and links to its detail page', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel(/name/i).fill('History E2E Test');
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('2');
  await page.getByLabel(/duration/i).fill('10');
  await page.getByRole('button', { name: /create test/i }).click();

  const row = page.getByRole('row', { name: /History E2E Test/i });
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: /run/i }).click();
  await expect(page).toHaveURL(/\/runs\/.+/);
  const runId = page.url().split('/runs/')[1];

  await page.getByRole('link', { name: 'Test Runs' }).click();
  await expect(page).toHaveURL(/\/history/);
  const historyRow = page.getByRole('row', { name: new RegExp(runId, 'i') });
  await expect(historyRow).toBeVisible({ timeout: 15_000 });
  await historyRow.click();
  await expect(page).toHaveURL(new RegExp(`/runs/${runId}`));
});

test('admin page renders with the API base URL', async ({ page }) => {
  await page.goto('/admin');
  await expect(page.getByText(/API base URL/i)).toBeVisible();
  await expect(page.getByRole('button', { name: /toggle theme/i })).toBeVisible();
});
```

The last assertion in the admin test relies on Task 22's decision not to render a second `ThemeToggle` on the admin page itself — only the one global toggle in `TopNav` (rendered by `Shell`, which wraps every page) exists, so this locator resolves to exactly one element.

- [ ] **Step 2: Bring up the stack and run the new spec**

Run (requires a local `kind` cluster; see `deploy/dev-up.sh` and the project README):

```bash
deploy/dev-up.sh
cd frontend && NEXT_PUBLIC_API_URL=http://localhost:8080 npm run dev &
npx playwright test e2e/portal-shell.spec.ts
```

Expected: all 4 tests PASS against the live stack.

- [ ] **Step 3: Confirm the existing walking-skeleton spec still passes unmodified**

Run: `cd frontend && npx playwright test e2e/walking-skeleton.spec.ts`
Expected: both existing tests PASS — confirms the repo-wide restyle didn't break the original create→run→watch→complete and cancel flows.

- [ ] **Step 4: Commit**

```bash
git add frontend/e2e/portal-shell.spec.ts
git commit -m "test(e2e): portal shell navigation, theme persistence, and history flow"
```

---

### Task 25: Final verification — coverage, build, full regression

**Files:** none (verification only; may touch test files if coverage gaps are found).

- [ ] **Step 1: Backend coverage**

Run:

```bash
cd backend
BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

Read the per-function table. If the `total:` line is below 88%, identify which function(s) have the lowest coverage (typically error-handling branches in `handleListRunsForTest` or `ListByTest`), write an additional test case exercising that branch (e.g. a Postgres connection error path, if not already covered — following the exact pattern of the existing `TestGetRunUnknown`/`TestListRunsForUnknownTest`-style tests), and re-run until `total:` ≥ 88%.

- [ ] **Step 2: Frontend coverage**

Run: `cd frontend && npm run test:coverage`

`vitest.config.ts`'s thresholds (Task 5) will fail the command if any metric is under 88%. If it fails, the printed per-file table shows exactly which file/lines are uncovered — typically an error-handling `catch` branch (e.g. the `.catch()` in `Shell`, `DashboardPage`, or `HistoryPage`). Add a test that forces that branch (e.g. `vi.spyOn(api, 'listTests').mockRejectedValue(new Error('boom'))` and assert the page still renders without crashing), following the existing pattern in Task 6's `ThemeProvider` tests. Re-run until it passes.

- [ ] **Step 3: Full backend regression**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: PASS, no vet warnings.

- [ ] **Step 4: Full frontend regression + build**

Run:

```bash
cd frontend
npm test
npm run build
```

Expected: all unit tests PASS, `next build` succeeds with no TypeScript errors.

- [ ] **Step 5: Full e2e regression against a live stack**

Run:

```bash
deploy/dev-up.sh
cd frontend && NEXT_PUBLIC_API_URL=http://localhost:8080 npm run dev &
npm run test:e2e
```

Expected: all tests in both `e2e/walking-skeleton.spec.ts` and `e2e/portal-shell.spec.ts` PASS.

- [ ] **Step 6: Tear down and final commit**

```bash
deploy/dev-down.sh
git status
```

If Steps 1–2 required additional test cases to clear the 88% threshold, commit them now:

```bash
git add -A
git commit -m "test: close coverage gaps to meet the 88% threshold"
```

If nothing changed in this task, there is nothing to commit — the plan is complete.

---

## Self-Review Notes

- **Spec coverage:** every section of `docs/superpowers/specs/2026-07-24-portal-shell-lre-design.md` maps to a task — component library → Tasks 6–16; theming → Task 6; navigation/pages → Tasks 17–22; backend addition → Tasks 1–4; testing/coverage → Tasks 5, 23–25.
- **Resolved ambiguities not spelled out in the spec** (locked down here so no task is a placeholder): `model.Run` had no `created_at` column, so "newest first" ordering had nothing to sort by — Task 1 adds it. `StatusBadge`'s status values were aligned to the real `RunStatus` enum (`pending|running|completed|failed|stopped`) rather than the spec prose's informal "passed/warning" wording, since those aren't real statuses the backend produces. `TreeNav`'s "clicking a test navigates to its detail view" was resolved to `/history?testId={id}`, since no standalone test-detail page exists — this reuses the history page rather than inventing a new one. `LiveMetrics` does **not** get a `StatusBadge` (only `TestList` and the history page do), because it would collide with the existing `LiveMetrics.test.tsx`'s `getByText(/running/i)` query. The admin page does **not** render its own `ThemeToggle` (only the global one in `TopNav`), for the same single-match-locator reason, applied to the new Playwright spec.
- **Type consistency check:** `Column<T>`, `DataTable` props, `TabItem`, `BreadcrumbItem`, `NavModule`, and `Run`/`Test` fields are used identically across every task that consumes them (Tasks 13/18/21 all import the same `Column` from `@/components/ui/DataTable`; Tasks 12/20/21/22 all import the same `Card`/`Tabs`).
