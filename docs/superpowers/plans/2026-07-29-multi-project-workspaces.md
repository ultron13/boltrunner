# Multi-Project Workspaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `+ New project` work — create projects, list and select them in `WorkspaceSwitcher`, and scope the test list to the selection — per `docs/superpowers/specs/2026-07-29-multi-project-workspaces-design.md`.

**Architecture:** The backend gains `CreateProject` on `ProjectStore` (both implementations) and `ListTestsForProject` on `TestStore`, exposed as `POST /api/projects` and `GET /api/tests?project_id=`. The frontend gains one stateful unit, `ProjectProvider`, mounted in `layout.tsx` beside `ThemeProvider`; it owns the project list, the selection, and persistence to `localStorage`. Everything else consumes that context.

**Tech Stack:** Go 1.26, chi router, pgx, Postgres. Next.js App Router (client components), React 18, TypeScript `strict`, Tailwind, Vitest + Testing Library, Playwright. No new dependencies.

## Global Constraints

- **This touches `backend/`.** Unlike the previous plan, this is not a frontend-only slice. **Both** coverage gates apply: backend 88% (`go test ./... -coverprofile=coverage.out`, enforced by the `awk` check in `.github/workflows/ci.yml`) and frontend 88% (`vitest.config.ts`). Neither is to be lowered.
- **Some existing assertions MUST change, and only these.** Enumerated here so any other change is a red flag:
  - `frontend/__tests__/WorkspaceSwitcher.test.tsx` — the file is rewritten in Task 6. `toBeDisabled()` on `+ New project` (line 17) and "does nothing when the disabled New project item is clicked" (lines 57-62) assert the absence of the feature being built. The `projectName` prop cases (lines 64-74) move to context.
  - `frontend/e2e/portal-shell.spec.ts` — the `workspace switcher shows Default checked and a disabled New project action` case asserts `toBeDisabled()` on the same button. Updated in Task 8.
  - `frontend/__tests__/Shell.test.tsx` — cases gain a `ProjectProvider` wrapper and a `listProjects` mock. Existing assertions in those cases do not change.
  - No other assertion in any file may be weakened or deleted.
- **`GET /api/tests` without `project_id` stays unfiltered.** Existing callers, the Go integration test, and three e2e specs depend on it.
- **`TreeNav` keeps its `projectName` prop.** `Shell` passes the selected name down. Neither `TopNav.test.tsx` nor `TreeNav.test.tsx` references `projectName`, so this stays quiet.
- **`TopNav` drops its `projectName` prop**, since it only forwarded it to `WorkspaceSwitcher`, which now reads context.
- **Storage key is `boltrunner-project`**, hyphenated to match the existing `boltrunner-theme`.
- **memstore and postgres must agree on the conflict contract.** API tests run against memstore; if only postgres returns `ErrConflict` for a duplicate name, the 409 path is untested where it is actually exercised.
- All new frontend components are client components (`'use client'`).

---

### Task 1: `CreateProject` on both stores

**Files:**
- Modify: `backend/internal/store/store.go` (the `ProjectStore` interface)
- Modify: `backend/internal/store/memstore/projectstore.go`
- Modify: `backend/internal/store/postgres/postgres.go`
- Test: `backend/internal/store/memstore/projectstore_test.go`, `backend/internal/store/postgres/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `ProjectStore.CreateProject(ctx context.Context, p *model.Project) error`. Populates `p.ID` and `p.CreatedAt`. Returns `store.ErrConflict` when a project with that name exists. Tasks 2 and 3 depend on this exact signature.

- [ ] **Step 1: Write the failing memstore tests**

Append to `backend/internal/store/memstore/projectstore_test.go`:

```go
func TestCreateProjectPopulatesIDAndCreatedAt(t *testing.T) {
	s := memstore.NewProjectStore()
	p := &model.Project{Name: "Payments"}
	if err := s.CreateProject(context.Background(), p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID == "" {
		t.Error("expected an id to be assigned")
	}
	if p.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreateProjectRejectsADuplicateName(t *testing.T) {
	s := memstore.NewProjectStore()
	first := &model.Project{Name: "Payments"}
	if err := s.CreateProject(context.Background(), first); err != nil {
		t.Fatalf("first CreateProject: %v", err)
	}
	err := s.CreateProject(context.Background(), &model.Project{Name: "Payments"})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for a duplicate name, got %v", err)
	}
}

// The seeded Default project is a row like any other: creating it again must
// conflict, not silently produce a second "Default".
func TestCreateProjectConflictsWithTheSeededDefault(t *testing.T) {
	s := memstore.NewProjectStore()
	err := s.CreateProject(context.Background(), &model.Project{Name: memstore.DefaultProjectName})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for the seeded name, got %v", err)
	}
}

func TestCreateProjectAppearsInListProjects(t *testing.T) {
	s := memstore.NewProjectStore()
	p := &model.Project{Name: "Payments"}
	if err := s.CreateProject(context.Background(), p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	projects, err := s.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	var found bool
	for _, got := range projects {
		if got.ID == p.ID && got.Name == "Payments" {
			found = true
		}
	}
	if !found {
		t.Fatalf("created project missing from ListProjects: %+v", projects)
	}
}
```

Ensure the file's imports include `errors`, `context`, `testing`, `github.com/boltrunner/backend/internal/model`, `github.com/boltrunner/backend/internal/store`, and `github.com/boltrunner/backend/internal/store/memstore`.

- [ ] **Step 2: Run them to verify they fail**

```bash
cd backend && go test ./internal/store/memstore/ -run TestCreateProject -v
```

Expected: compile failure — `s.CreateProject undefined`. That is the correct failure at this point.

- [ ] **Step 3: Add the interface method**

In `backend/internal/store/store.go`, replace the `ProjectStore` interface:

```go
type ProjectStore interface {
	ListProjects(ctx context.Context) ([]model.Project, error)
	// CreateProject assigns p.ID and p.CreatedAt. It returns ErrConflict if a
	// project with the same name already exists.
	CreateProject(ctx context.Context, p *model.Project) error
}
```

- [ ] **Step 4: Implement it in memstore**

In `backend/internal/store/memstore/projectstore.go`, add to the imports `"github.com/boltrunner/backend/internal/store"` and `"github.com/google/uuid"`, then append:

```go
func (s *ProjectStore) CreateProject(ctx context.Context, p *model.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Postgres enforces this with a UNIQUE constraint. memstore has to check by
	// hand, and must agree: the API tests run against this implementation, so a
	// missing conflict here leaves the 409 path untested where it is exercised.
	for _, existing := range s.projects {
		if existing.Name == p.Name {
			return store.ErrConflict
		}
	}
	p.ID = uuid.NewString()
	p.CreatedAt = time.Now().UTC()
	s.projects[p.ID] = *p
	return nil
}
```

- [ ] **Step 5: Run the memstore tests**

```bash
cd backend && go test ./internal/store/memstore/ -run TestCreateProject -v
```

Expected: PASS, all four.

- [ ] **Step 6: Write the failing postgres tests**

Append to `backend/internal/store/postgres/store_test.go`, following the existing scratch-DB helper convention in that file (copy the setup line from a neighbouring test such as `TestListProjectsIncludesSeededDefaultAndIsNeverNil`):

```go
func TestCreateProjectPersistsAndConflictsOnName(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	p := &model.Project{Name: "Payments"}
	if err := db.CreateProject(ctx, p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID == "" || p.CreatedAt.IsZero() {
		t.Fatalf("expected id and created_at to be populated, got %+v", p)
	}

	err := db.CreateProject(ctx, &model.Project{Name: "Payments"})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for a duplicate name, got %v", err)
	}
}

func TestCreateProjectConflictsWithTheSeededDefault(t *testing.T) {
	db := setupDB(t)
	err := db.CreateProject(context.Background(), &model.Project{Name: "Default"})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for the seeded Default name, got %v", err)
	}
}
```

`setupDB(t *testing.T) *DB` is the existing helper at `store_test.go:16`; it skips when no `BOLTRUNNER_TEST_DSN` is reachable. Do not introduce a second one. (`newScratchDB` at line 428 is for version-race fixtures and is not what these tests want.)

- [ ] **Step 7: Implement it in postgres**

In `backend/internal/store/postgres/postgres.go`, add after `ListProjects`:

```go
func (db *DB) CreateProject(ctx context.Context, p *model.Project) error {
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO projects (name) VALUES ($1) RETURNING id, created_at`,
		p.Name,
	).Scan(&p.ID, &p.CreatedAt)
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	return err
}
```

`isUniqueViolation` already exists. Widen its doc comment, which currently names only the version race:

```go
// isUniqueViolation reports whether err is SQLSTATE 23505 (unique_violation) --
// two concurrent edits racing for the same (catalog_id, version), or an
// attempt to create a project whose name is already taken.
```

- [ ] **Step 8: Run the postgres tests**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" go test ./internal/store/postgres/ -run TestCreateProject -v
```

Expected: PASS. These tests skip without a reachable database; if they skip, start one with `docker run -d --rm --name br-pg -e POSTGRES_USER=boltrunner -e POSTGRES_PASSWORD=boltrunner -e POSTGRES_DB=boltrunner -p 5432:5432 postgres:16` and re-run. A skipped test is not a passing test — do not report it as one.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/store/
git commit -m "feat(backend): add CreateProject to the project store"
```

---

### Task 2: `POST /api/projects`

**Files:**
- Modify: `backend/internal/api/projects.go`
- Modify: `backend/internal/api/server.go:33` (route table)
- Test: `backend/internal/api/projects_test.go` (create if absent)

**Interfaces:**
- Consumes: `ProjectStore.CreateProject` from Task 1.
- Produces: `POST /api/projects` accepting `{"name": string}`, returning `201` + `model.Project`. Task 4's `createProject` client call targets it.

- [ ] **Step 1: Write the failing handler tests**

Create `backend/internal/api/projects_test.go`. Use the existing `newTestServer()` helper at `runs_test.go:18` — it takes no arguments and already wires a `memstore.NewProjectStore()`, so these tests exercise the real conflict path from Task 1. Do not build a `Server` by hand.

```go
func TestCreateProjectReturns201AndTheProject(t *testing.T) {
	srv := newTestServer()
	body := strings.NewReader(`{"name":"Payments"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", body)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got model.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Payments" || got.ID == "" {
		t.Fatalf("unexpected project: %+v", got)
	}
}

func TestCreateProjectRejectsAnEmptyName(t *testing.T) {
	for _, name := range []string{`""`, `"   "`} {
		srv := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":`+name+`}`))
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("name=%s: expected 400, got %d", name, rec.Code)
		}
	}
}

func TestCreateProjectRejectsAnOverlongName(t *testing.T) {
	srv := newTestServer()
	long := strings.Repeat("a", 101)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"`+long+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateProjectRejectsAMalformedBody(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateProjectReturns409ForADuplicateName(t *testing.T) {
	srv := newTestServer()
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"Payments"}`))
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusCreated {
			t.Fatalf("first create: expected 201, got %d", rec.Code)
		}
		if i == 1 && rec.Code != http.StatusConflict {
			t.Fatalf("second create: expected 409, got %d (%s)", rec.Code, rec.Body.String())
		}
	}
}

// A name that differs only by surrounding whitespace is the same name: it is
// trimmed before the uniqueness check, so it must conflict rather than create
// a near-duplicate the user cannot tell apart in the switcher.
func TestCreateProjectTrimsBeforeCheckingUniqueness(t *testing.T) {
	srv := newTestServer()
	first := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"Payments"}`))
	srv.Router().ServeHTTP(httptest.NewRecorder(), first)

	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"  Payments  "}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a whitespace-padded duplicate, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd backend && go test ./internal/api/ -run TestCreateProject -v
```

Expected: FAIL — the route does not exist, so the router returns 405 or 404 rather than 201.

- [ ] **Step 3: Implement the handler**

In `backend/internal/api/projects.go`, add the imports `errors`, `strings`, `github.com/boltrunner/backend/internal/model`, `github.com/boltrunner/backend/internal/store`, then append:

```go
type createProjectRequest struct {
	Name string `json:"name"`
}

// projectNameMaxLen bounds a column that is plain TEXT. The switcher menu is a
// fixed-width popover, so an unbounded name is a layout bug waiting to happen.
const projectNameMaxLen = 100

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if len(name) > projectNameMaxLen {
		http.Error(w, "name must be 100 characters or fewer", http.StatusBadRequest)
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

- [ ] **Step 4: Register the route**

In `backend/internal/api/server.go`, directly below the existing `GET /api/projects` line:

```go
	s.router.Get("/api/projects", s.handleListProjects)
	s.router.Post("/api/projects", s.handleCreateProject)
```

- [ ] **Step 5: Run the tests**

```bash
cd backend && go test ./internal/api/ -run TestCreateProject -v
```

Expected: PASS, all six.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api/
git commit -m "feat(backend): create projects over HTTP"
```

---

### Task 3: Scope the test list by project

**Files:**
- Modify: `backend/internal/store/store.go` (the `TestStore` interface)
- Modify: `backend/internal/store/memstore/memstore.go`
- Modify: `backend/internal/store/postgres/postgres.go`
- Modify: `backend/internal/api/tests.go:62-70` (`handleListTests`)
- Test: `backend/internal/store/memstore/memstore_test.go`, `backend/internal/store/postgres/store_test.go`, `backend/internal/api/tests_test.go`

**Interfaces:**
- Consumes: nothing from Tasks 1-2.
- Produces: `TestStore.ListTestsForProject(ctx context.Context, projectID string) ([]model.Test, error)` and the `?project_id=` query parameter on `GET /api/tests`. Task 4's `listTests(projectId?)` targets the latter.

- [ ] **Step 1: Write the failing store tests**

Append to `backend/internal/store/memstore/memstore_test.go`:

```go
func TestListTestsForProjectReturnsOnlyThatProjectsTests(t *testing.T) {
	s := memstore.NewTestStore()
	ctx := context.Background()
	mine := &model.Test{ProjectID: "p-mine", Name: "mine", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	theirs := &model.Test{ProjectID: "p-theirs", Name: "theirs", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1}
	if err := s.CreateTest(ctx, mine); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if err := s.CreateTest(ctx, theirs); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	got, err := s.ListTestsForProject(ctx, "p-mine")
	if err != nil {
		t.Fatalf("ListTestsForProject: %v", err)
	}
	if len(got) != 1 || got[0].Name != "mine" {
		t.Fatalf("expected only the p-mine test, got %+v", got)
	}
}

func TestListTestsForProjectReturnsEmptyForAnUnknownProject(t *testing.T) {
	s := memstore.NewTestStore()
	got, err := s.ListTestsForProject(context.Background(), "p-nope")
	if err != nil {
		t.Fatalf("expected no error for an unknown project, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected an empty slice, got %+v", got)
	}
	if got == nil {
		t.Fatal("expected an empty slice, not nil -- it is JSON-encoded directly")
	}
}
```

Append to `backend/internal/store/postgres/store_test.go`, using the same scratch-DB helper as Task 1:

```go
func TestListTestsForProjectFiltersAndKeepsLatestVersionOnly(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	projects, err := db.ListProjects(ctx)
	if err != nil || len(projects) == 0 {
		t.Fatalf("need the seeded Default project: %v", err)
	}
	defaultID := projects[0].ID

	other := &model.Project{Name: "Payments"}
	if err := db.CreateProject(ctx, other); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	inDefault := &model.Test{ProjectID: defaultID, Name: "in-default", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	inOther := &model.Test{ProjectID: other.ID, Name: "in-other", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, inDefault); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if err := db.CreateTest(ctx, inOther); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	// A second version of the other-project test: the filter must still collapse
	// the family to its latest version, exactly as ListTests does.
	inOther.Name = "in-other-v2"
	if err := db.UpdateTest(ctx, inOther); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	got, err := db.ListTestsForProject(ctx, other.ID)
	if err != nil {
		t.Fatalf("ListTestsForProject: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one row for the other project, got %d: %+v", len(got), got)
	}
	if got[0].Name != "in-other-v2" {
		t.Fatalf("expected the latest version, got %q", got[0].Name)
	}
}

// A malformed id must not surface as a 500. pgx fails to encode a non-UUID
// client-side, which is indistinguishable by type from a connection failure.
func TestListTestsForProjectReturnsEmptyForAMalformedID(t *testing.T) {
	db := setupDB(t)
	got, err := db.ListTestsForProject(context.Background(), "not-a-uuid")
	if err != nil {
		t.Fatalf("expected no error for a malformed id, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected an empty slice, got %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd backend && go test ./internal/store/... -run ListTestsForProject -v
```

Expected: compile failure — `ListTestsForProject undefined`.

- [ ] **Step 3: Add the interface method**

In `backend/internal/store/store.go`, inside `TestStore`, below `ListTests`:

```go
	// ListTestsForProject is ListTests restricted to one project. An unknown or
	// malformed project id yields an empty slice, not an error -- it is
	// indistinguishable from a project with no tests.
	ListTestsForProject(ctx context.Context, projectID string) ([]model.Test, error)
```

- [ ] **Step 4: Implement in memstore**

In `backend/internal/store/memstore/memstore.go`, below `ListTests`:

```go
func (s *TestStore) ListTestsForProject(ctx context.Context, projectID string) ([]model.Test, error) {
	all, err := s.ListTests(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Test, 0, len(all))
	for _, t := range all {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}
```

`ListTests` already collapses each family to its latest version and sorts, so filtering its output inherits both.

- [ ] **Step 5: Implement in postgres**

In `backend/internal/store/postgres/postgres.go`, below `ListTests`:

```go
func (db *DB) ListTestsForProject(ctx context.Context, projectID string) ([]model.Test, error) {
	// Reject a malformed id before pgx tries to encode it, for the same reason
	// CreateTest does: an encode failure is indistinguishable by type from a
	// genuine connection failure, and would report bad input as an outage.
	if _, err := uuid.Parse(projectID); err != nil {
		return []model.Test{}, nil
	}
	rows, err := db.Pool.Query(ctx,
		`SELECT catalog_id, id, version, project_id, name, target_url, virtual_users,
		        duration_seconds, catalog_created_at, created_at
		 FROM (
		     SELECT DISTINCT ON (catalog_id) `+testColumns+`
		     FROM tests
		     WHERE project_id = $1
		     ORDER BY catalog_id, version DESC
		 ) latest
		 ORDER BY catalog_created_at DESC`, projectID)
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
```

- [ ] **Step 6: Run the store tests**

```bash
cd backend && go test ./internal/store/... -run ListTestsForProject -v
```

Expected: PASS.

- [ ] **Step 7: Write the failing API test**

Append to `backend/internal/api/tests_test.go`:

```go
func TestListTestsFiltersByProjectIDWhenGiven(t *testing.T) {
	srv := newTestServer()

	create := func(name, projectID string) {
		body := `{"name":"` + name + `","target_url":"http://a","virtual_users":1,"duration_seconds":1,"project_id":"` + projectID + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/tests", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: expected 201, got %d (%s)", name, rec.Code, rec.Body.String())
		}
	}
	create("in-default", memstore.DefaultProjectID)

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tests?project_id="+memstore.DefaultProjectID, nil))
	var scoped []model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &scoped); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(scoped) != 1 {
		t.Fatalf("expected 1 test in Default, got %d", len(scoped))
	}

	rec = httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tests?project_id=00000000-0000-0000-0000-0000000000ff", nil))
	var empty []model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no tests for an unknown project, got %d", len(empty))
	}
}

// The three e2e specs and the Go integration test all call GET /api/tests with
// no parameter and expect everything back.
func TestListTestsWithoutProjectIDStaysUnfiltered(t *testing.T) {
	srv := newTestServer()
	body := `{"name":"anything","target_url":"http://a","virtual_users":1,"duration_seconds":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/tests", strings.NewReader(body))
	srv.Router().ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tests", nil))
	var all []model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected the unfiltered list to contain the test, got %d", len(all))
	}
}
```

- [ ] **Step 8: Wire the query parameter**

Replace `handleListTests` in `backend/internal/api/tests.go`:

```go
func (s *Server) handleListTests(w http.ResponseWriter, r *http.Request) {
	var (
		tests []model.Test
		err   error
	)
	// An absent project_id means "every project": existing callers, the Go
	// integration test and three e2e specs all depend on that.
	if projectID := r.URL.Query().Get("project_id"); projectID != "" {
		tests, err = s.testStore.ListTestsForProject(r.Context(), projectID)
	} else {
		tests, err = s.testStore.ListTests(r.Context())
	}
	if err != nil {
		http.Error(w, "failed to list tests", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tests)
}
```

- [ ] **Step 9: Run the full backend suite and the coverage gate**

```bash
cd backend && go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out | tail -1
```

Expected: PASS, total coverage ≥ 88%. If short, add store-level tests for the uncovered branches — do not lower the threshold.

- [ ] **Step 10: Commit**

```bash
git add backend/
git commit -m "feat(backend): scope the test list to a project"
```

---

### Task 4: API client — `createProject` and a scoped `listTests`

**Files:**
- Modify: `frontend/lib/api-client.ts`
- Test: `frontend/__tests__/api-client.test.ts`

**Interfaces:**
- Consumes: the endpoints from Tasks 2 and 3.
- Produces: `createProject(name: string): Promise<Project>`, `listTests(projectId?: string): Promise<Test[]>`, and `CreateTestInput.project_id?: string`. Tasks 5-7 use these exact signatures.

- [ ] **Step 1: Write the failing tests**

Append to `frontend/__tests__/api-client.test.ts`:

```ts
describe('createProject', () => {
  afterEach(() => vi.restoreAllMocks());

  it('POSTs the name and returns the created project', async () => {
    const created = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z' };
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => created }) as unknown as typeof fetch;

    await expect(createProject('Payments')).resolves.toEqual(created);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/projects'),
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ name: 'Payments' }) })
    );
  });

  it('throws an ApiError with status 409 for a duplicate name', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      text: async () => 'a project with that name already exists',
    }) as unknown as typeof fetch;
    await expect(createProject('Payments')).rejects.toMatchObject({ status: 409 });
  });
});

describe('listTests project scoping', () => {
  afterEach(() => vi.restoreAllMocks());

  it('appends project_id when given one', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => [] }) as unknown as typeof fetch;
    await listTests('p2');
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/tests?project_id=p2'),
      expect.objectContaining({ cache: 'no-store' })
    );
  });

  it('omits the parameter entirely when given none', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => [] }) as unknown as typeof fetch;
    await listTests();
    const url = (global.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    expect(url).not.toContain('project_id');
  });
});
```

Add `createProject` to the file's existing import from `@/lib/api-client`.

- [ ] **Step 2: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/api-client.test.ts
```

Expected: FAIL — `createProject is not a function`.

- [ ] **Step 3: Implement**

In `frontend/lib/api-client.ts`, add `project_id` to `CreateTestInput`:

```ts
export type CreateTestInput = {
  name: string;
  target_url: string;
  virtual_users: number;
  duration_seconds: number;
  // Optional: the backend COALESCEs a missing value to the Default project.
  project_id?: string;
};
```

Replace `listTests` and add `createProject`:

```ts
export async function listTests(projectId?: string): Promise<Test[]> {
  const url = projectId
    ? `${API_URL}/api/tests?project_id=${encodeURIComponent(projectId)}`
    : `${API_URL}/api/tests`;
  const tests = await unwrap<Test[]>(await fetch(url, { cache: 'no-store' }));
  return tests ?? [];
}

export async function createProject(name: string): Promise<Project> {
  return unwrap(
    await fetch(`${API_URL}/api/projects`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    })
  );
}
```

`UpdateTestInput = CreateTestInput` already, so it inherits the optional field. That is harmless: `handleUpdateTest` ignores `project_id`.

- [ ] **Step 4: Run the tests**

```bash
cd frontend && npx vitest run __tests__/api-client.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/lib/api-client.ts frontend/__tests__/api-client.test.ts
git commit -m "feat(frontend): add createProject and project-scoped listTests"
```

---

### Task 5: `ProjectProvider`

**Files:**
- Create: `frontend/components/ui/ProjectProvider.tsx`
- Modify: `frontend/app/layout.tsx`
- Test: `frontend/__tests__/ProjectProvider.test.tsx`

**Interfaces:**
- Consumes: `listProjects`, `createProject`, `Project` from Task 4.
- Produces: `ProjectProvider` and `useProjects(): { projects, selectedId, selected, select, create }`. Tasks 6 and 7 consume this exact shape.

- [ ] **Step 1: Write the failing tests**

Create `frontend/__tests__/ProjectProvider.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ProjectProvider, useProjects } from '@/components/ui/ProjectProvider';
import * as api from '@/lib/api-client';
import type { Project } from '@/lib/api-client';

const def: Project = { id: 'p1', name: 'Default', created_at: '2026-07-24T00:00:00Z' };
const pay: Project = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z' };

function Probe() {
  const { projects, selected, select, create } = useProjects();
  return (
    <div>
      <span data-testid="selected">{selected?.name ?? 'none'}</span>
      <span data-testid="count">{projects.length}</span>
      <button onClick={() => select('p2')}>pick payments</button>
      <button onClick={() => create('New One')}>create</button>
    </div>
  );
}

describe('ProjectProvider', () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it('selects the first project when nothing is stored', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    render(<ProjectProvider><Probe /></ProjectProvider>);
    expect(await screen.findByText('Default')).toBeInTheDocument();
    expect(screen.getByTestId('count')).toHaveTextContent('2');
  });

  it('restores a stored selection', async () => {
    localStorage.setItem('boltrunner-project', 'p2');
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    render(<ProjectProvider><Probe /></ProjectProvider>);
    expect(await screen.findByText('Payments')).toBeInTheDocument();
  });

  // localStorage outlives the database. A developer who drops and reseeds the
  // DB has a stored id that names nothing, and must not get an empty switcher.
  it('falls back to the first project when the stored id is unknown', async () => {
    localStorage.setItem('boltrunner-project', 'p-deleted');
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    render(<ProjectProvider><Probe /></ProjectProvider>);
    expect(await screen.findByText('Default')).toBeInTheDocument();
    await waitFor(() => expect(localStorage.getItem('boltrunner-project')).toBe('p1'));
  });

  it('persists a selection', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    render(<ProjectProvider><Probe /></ProjectProvider>);
    await screen.findByText('Default');
    fireEvent.click(screen.getByRole('button', { name: /pick payments/i }));
    expect(screen.getByTestId('selected')).toHaveTextContent('Payments');
    expect(localStorage.getItem('boltrunner-project')).toBe('p2');
  });

  it('selects a newly created project and adds it to the list', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def]);
    vi.spyOn(api, 'createProject').mockResolvedValue({ id: 'p9', name: 'New One', created_at: '2026-07-29T00:00:00Z' });
    render(<ProjectProvider><Probe /></ProjectProvider>);
    await screen.findByText('Default');

    fireEvent.click(screen.getByRole('button', { name: /create/i }));
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('New One'));
    expect(screen.getByTestId('count')).toHaveTextContent('2');
    expect(localStorage.getItem('boltrunner-project')).toBe('p9');
  });

  it('degrades to an empty list when the projects endpoint fails', async () => {
    vi.spyOn(api, 'listProjects').mockRejectedValue(new Error('boom'));
    render(<ProjectProvider><Probe /></ProjectProvider>);
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('none'));
    expect(screen.getByTestId('count')).toHaveTextContent('0');
  });

  it('throws when used outside a provider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<Probe />)).toThrow(/useProjects must be used within a ProjectProvider/);
    spy.mockRestore();
  });
});
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/ProjectProvider.test.tsx
```

Expected: FAIL — the module does not exist.

- [ ] **Step 3: Implement**

Create `frontend/components/ui/ProjectProvider.tsx`:

```tsx
'use client';

import { createContext, useCallback, useContext, useEffect, useState, ReactNode } from 'react';
import { listProjects, createProject, Project } from '@/lib/api-client';

const STORAGE_KEY = 'boltrunner-project';

type ProjectContextValue = {
  projects: Project[];
  selectedId: string | null;
  selected: Project | null;
  select: (id: string) => void;
  create: (name: string) => Promise<Project>;
};

const ProjectContext = createContext<ProjectContextValue | null>(null);

export function ProjectProvider({ children }: { children: ReactNode }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    // localStorage is read here rather than in a useState initialiser: the
    // server render has no localStorage, and reading it during render would
    // produce markup that does not match the client's. ThemeProvider does the
    // same for boltrunner-theme.
    listProjects()
      .then((list) => {
        setProjects(list);
        const stored = localStorage.getItem(STORAGE_KEY);
        // A stored id outlives the database that issued it. After a drop or a
        // reseed it names no project, and keeping it would leave the switcher
        // pointing at nothing.
        const resolved = stored && list.some((p) => p.id === stored) ? stored : list[0]?.id ?? null;
        setSelectedId(resolved);
        if (resolved) localStorage.setItem(STORAGE_KEY, resolved);
      })
      .catch(() => {
        setProjects([]);
        setSelectedId(null);
      });
  }, []);

  const select = useCallback((id: string) => {
    setSelectedId(id);
    localStorage.setItem(STORAGE_KEY, id);
  }, []);

  const create = useCallback(async (name: string) => {
    const project = await createProject(name);
    // Sorted by name to match what both stores return from ListProjects, so a
    // reload does not reshuffle the menu.
    setProjects((prev) => [...prev, project].sort((a, b) => a.name.localeCompare(b.name)));
    setSelectedId(project.id);
    localStorage.setItem(STORAGE_KEY, project.id);
    return project;
  }, []);

  const selected = projects.find((p) => p.id === selectedId) ?? null;

  return (
    <ProjectContext.Provider value={{ projects, selectedId, selected, select, create }}>
      {children}
    </ProjectContext.Provider>
  );
}

export function useProjects(): ProjectContextValue {
  const ctx = useContext(ProjectContext);
  if (!ctx) throw new Error('useProjects must be used within a ProjectProvider');
  return ctx;
}
```

- [ ] **Step 4: Run the tests**

```bash
cd frontend && npx vitest run __tests__/ProjectProvider.test.tsx
```

Expected: PASS, all seven.

- [ ] **Step 5: Mount it in the layout**

In `frontend/app/layout.tsx`, import `ProjectProvider` and wrap `Shell` — inside `ThemeProvider`, outside `Suspense`:

```tsx
        <ThemeProvider>
          <ProjectProvider>
            <Suspense fallback={null}>
              <Shell>{children}</Shell>
            </Suspense>
          </ProjectProvider>
        </ThemeProvider>
```

`Shell` consumes the selection, so the provider has to sit above it.

- [ ] **Step 6: Commit**

```bash
git add frontend/components/ui/ProjectProvider.tsx frontend/__tests__/ProjectProvider.test.tsx frontend/app/layout.tsx
git commit -m "feat(frontend): add ProjectProvider with a persisted selection"
```

---

### Task 6: `WorkspaceSwitcher` — list, select, create inline

**Files:**
- Modify: `frontend/components/ui/WorkspaceSwitcher.tsx`
- Modify: `frontend/components/ui/TopNav.tsx:10-16`
- Test: `frontend/__tests__/WorkspaceSwitcher.test.tsx` (rewritten)

**Interfaces:**
- Consumes: `useProjects()` from Task 5.
- Produces: a `WorkspaceSwitcher` taking no props.

- [ ] **Step 1: Rewrite the test file**

Replace `frontend/__tests__/WorkspaceSwitcher.test.tsx` entirely. The existing `toBeDisabled()` and "does nothing when clicked" cases assert the absence of this feature; the `projectName` prop cases move to context. Every other behavior the old file pinned — closed by default, closes on select, Escape returns focus, outside-click closes — is preserved below.

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { WorkspaceSwitcher } from '@/components/ui/WorkspaceSwitcher';
import { ProjectProvider } from '@/components/ui/ProjectProvider';
import * as api from '@/lib/api-client';
import { ApiError } from '@/lib/api-client';
import type { Project } from '@/lib/api-client';

const def: Project = { id: 'p1', name: 'Default', created_at: '2026-07-24T00:00:00Z' };
const pay: Project = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z' };

function renderSwitcher(projects: Project[] = [def, pay]) {
  vi.spyOn(api, 'listProjects').mockResolvedValue(projects);
  return render(
    <ProjectProvider>
      <WorkspaceSwitcher />
    </ProjectProvider>
  );
}

async function open() {
  const trigger = await screen.findByRole('button', { name: /default/i });
  fireEvent.click(trigger);
  return trigger;
}

describe('WorkspaceSwitcher', () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it('renders closed by default', async () => {
    renderSwitcher();
    expect(await screen.findByRole('button', { name: /default/i })).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('lists every project with the selected one checked', async () => {
    renderSwitcher();
    await open();
    expect(screen.getByRole('menuitemradio', { name: /default/i })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('menuitemradio', { name: /payments/i })).toHaveAttribute('aria-checked', 'false');
  });

  it('selects another project and closes', async () => {
    renderSwitcher();
    const trigger = await open();
    fireEvent.click(screen.getByRole('menuitemradio', { name: /payments/i }));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
    expect(await screen.findByRole('button', { name: /payments/i })).toBeInTheDocument();
  });

  it('closes on Escape and returns focus to the trigger', async () => {
    renderSwitcher();
    const trigger = await open();
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('closes when clicking outside', async () => {
    renderSwitcher();
    await open();
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('creates a project from the inline input and selects it', async () => {
    renderSwitcher([def]);
    vi.spyOn(api, 'createProject').mockResolvedValue({ id: 'p9', name: 'Payments', created_at: '2026-07-29T00:00:00Z' });
    await open();

    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    const input = screen.getByRole('textbox', { name: /project name/i });
    fireEvent.change(input, { target: { value: 'Payments' } });
    fireEvent.submit(input);

    await waitFor(() => expect(api.createProject).toHaveBeenCalledWith('Payments'));
    expect(await screen.findByRole('button', { name: /payments/i })).toBeInTheDocument();
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  // The user typed something worth keeping; a duplicate-name rejection must not
  // throw it away, or they retype the whole name to change one character.
  it('keeps the typed name and shows the message when the name is taken', async () => {
    renderSwitcher([def]);
    vi.spyOn(api, 'createProject').mockRejectedValue(
      new ApiError(409, 'request failed (409): a project with that name already exists')
    );
    await open();

    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    const input = screen.getByRole('textbox', { name: /project name/i });
    fireEvent.change(input, { target: { value: 'Default' } });
    fireEvent.submit(input);

    expect(await screen.findByText(/already exists/i)).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: /project name/i })).toHaveValue('Default');
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });

  it('abandons the inline input on Escape without closing the menu', async () => {
    renderSwitcher([def]);
    await open();
    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    const input = screen.getByRole('textbox', { name: /project name/i });
    fireEvent.keyDown(input, { key: 'Escape' });

    expect(screen.getByRole('menu')).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: /project name/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /new project/i })).toBeInTheDocument();
  });

  it('does not submit an empty name', async () => {
    renderSwitcher([def]);
    const createSpy = vi.spyOn(api, 'createProject');
    await open();
    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    fireEvent.submit(screen.getByRole('textbox', { name: /project name/i }));
    expect(createSpy).not.toHaveBeenCalled();
  });

  it('falls back to Default as the label when nothing is selected', async () => {
    vi.spyOn(api, 'listProjects').mockRejectedValue(new Error('boom'));
    render(
      <ProjectProvider>
        <WorkspaceSwitcher />
      </ProjectProvider>
    );
    expect(await screen.findByRole('button', { name: /default/i })).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/WorkspaceSwitcher.test.tsx
```

Expected: FAIL — the component still takes a `projectName` prop and renders a disabled button.

- [ ] **Step 3: Implement**

Replace `frontend/components/ui/WorkspaceSwitcher.tsx`:

```tsx
'use client';

import { FormEvent, KeyboardEvent, useEffect, useRef, useState } from 'react';
import { useProjects } from '@/components/ui/ProjectProvider';

export function WorkspaceSwitcher() {
  const { projects, selectedId, selected, select, create } = useProjects();
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;

    function handleMouseDown(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleMouseDown);
    return () => document.removeEventListener('mousedown', handleMouseDown);
  }, [open]);

  useEffect(() => {
    if (creating) inputRef.current?.focus();
  }, [creating]);

  function close() {
    setOpen(false);
    setCreating(false);
    setName('');
    setError(null);
    triggerRef.current?.focus();
  }

  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key !== 'Escape') return;
    // Escape backs out of the inline input first, so an abandoned create does
    // not also dismiss the menu the user is still working in.
    if (creating) {
      setCreating(false);
      setName('');
      setError(null);
      return;
    }
    close();
  }

  async function handleCreate(e: FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    setSaving(true);
    setError(null);
    try {
      await create(trimmed);
      close();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Couldn't create project");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div ref={rootRef} className="relative" onKeyDown={handleKeyDown}>
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1 text-chrome-fg px-2 py-1 rounded hover:bg-white/10"
      >
        {selected?.name ?? 'Default'} <span aria-hidden="true">▾</span>
      </button>
      {open && (
        <div
          role="menu"
          aria-label="Workspaces"
          className="absolute left-0 mt-1 w-56 rounded border border-border bg-surface text-text shadow-lg z-10"
        >
          {projects.map((p) => (
            <button
              key={p.id}
              type="button"
              role="menuitemradio"
              aria-checked={p.id === selectedId}
              onClick={() => {
                select(p.id);
                close();
              }}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-surface-alt"
            >
              <span aria-hidden="true">{p.id === selectedId ? '✓' : ' '}</span> {p.name}
            </button>
          ))}
          {creating ? (
            <form onSubmit={handleCreate} className="border-t border-border p-2">
              <input
                ref={inputRef}
                aria-label="Project name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={saving}
                placeholder="New project name"
                className="w-full rounded border border-border bg-surface px-2 py-1 text-sm"
              />
              {error && <p className="mt-1 text-xs text-red-600">{error}</p>}
            </form>
          ) : (
            <button
              type="button"
              onClick={() => setCreating(true)}
              className="flex w-full items-center gap-2 border-t border-border px-3 py-2 text-left text-sm hover:bg-surface-alt"
            >
              + New project
            </button>
          )}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Drop the forwarded prop from `TopNav`**

In `frontend/components/ui/TopNav.tsx`:

```tsx
export function TopNav({ modules }: { modules: NavModule[] }) {
```

and render `<WorkspaceSwitcher />` with no props. `TopNav.test.tsx` never references `projectName`, so it needs no change.

- [ ] **Step 5: Run the tests**

```bash
cd frontend && npx vitest run __tests__/WorkspaceSwitcher.test.tsx __tests__/TopNav.test.tsx
```

Expected: PASS. If `TopNav.test.tsx` renders `TopNav` without a `ProjectProvider`, wrap it — `WorkspaceSwitcher` now throws outside one. This is a wrapper addition, not an assertion change.

- [ ] **Step 6: Commit**

```bash
git add frontend/components/ui/WorkspaceSwitcher.tsx frontend/components/ui/TopNav.tsx frontend/__tests__/WorkspaceSwitcher.test.tsx
git commit -m "feat(frontend): list, select and create projects in the switcher"
```

---

### Task 7: Scope the shell and the create form

**Files:**
- Modify: `frontend/components/ui/Shell.tsx`
- Modify: `frontend/components/CreateTestForm.tsx:27-32`
- Test: `frontend/__tests__/Shell.test.tsx`, `frontend/__tests__/CreateTestForm.test.tsx`

**Interfaces:**
- Consumes: `useProjects()` from Task 5, `listTests(projectId?)` from Task 4.
- Produces: nothing.

- [ ] **Step 1: Write the failing tests**

Append to `frontend/__tests__/CreateTestForm.test.tsx`:

```tsx
it('sends the selected project id', async () => {
  vi.spyOn(api, 'listProjects').mockResolvedValue([
    { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z' },
  ]);
  const createTest = vi.spyOn(api, 'createTest').mockResolvedValue({
    id: 't1', name: 'x', target_url: 'http://x', virtual_users: 1, duration_seconds: 1,
  } as never);

  render(
    <ProjectProvider>
      <CreateTestForm onCreated={vi.fn()} />
    </ProjectProvider>
  );

  fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'x' } });
  fireEvent.change(screen.getByLabelText(/target url/i), { target: { value: 'http://x' } });
  fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '1' } });
  fireEvent.change(screen.getByLabelText(/duration/i), { target: { value: '1' } });
  fireEvent.click(screen.getByRole('button', { name: /create test/i }));

  await waitFor(() =>
    expect(createTest).toHaveBeenCalledWith(expect.objectContaining({ project_id: 'p2' }))
  );
});
```

Append to `frontend/__tests__/Shell.test.tsx`:

```tsx
it('scopes the test list to the selected project', async () => {
  vi.spyOn(api, 'listProjects').mockResolvedValue([
    { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z' },
  ]);
  const listTests = vi.spyOn(api, 'listTests').mockResolvedValue([]);

  render(
    <ProjectProvider>
      <Shell>content</Shell>
    </ProjectProvider>
  );

  await waitFor(() => expect(listTests).toHaveBeenCalledWith('p2'));
});
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/Shell.test.tsx __tests__/CreateTestForm.test.tsx
```

Expected: FAIL — `listTests` is called with no argument, and `createTest` is called without `project_id`.

- [ ] **Step 3: Wire `Shell`**

In `frontend/components/ui/Shell.tsx`: import `useProjects`, delete the local `projectName` state and the `listProjects()` effect entirely, and replace them with the context. Refetch tests when the selection changes:

```tsx
  const { selectedId, selected } = useProjects();
  const projectName = selected?.name ?? 'Default';

  useEffect(() => {
    listTests(selectedId ?? undefined).then(setTests).catch(() => setTests([]));
  }, [selectedId]);
```

Remove `listProjects` from the import from `@/lib/api-client`. `TopNav` now takes only `modules`; `TreeNav` keeps receiving `projectName`.

- [ ] **Step 4: Wire `CreateTestForm`**

Import `useProjects`, read `selectedId`, and include it in the payload:

```tsx
  const { selectedId } = useProjects();
```

```tsx
      const test = await createTest({
        name,
        target_url: targetUrl,
        virtual_users: Number(virtualUsers),
        duration_seconds: Number(durationSeconds),
        // Omitted when nothing is selected: the backend then COALESCEs to Default.
        ...(selectedId ? { project_id: selectedId } : {}),
      });
```

- [ ] **Step 5: Add provider wrappers to existing cases**

Existing `Shell.test.tsx` and `CreateTestForm.test.tsx` cases now render components that call `useProjects()`, which throws outside a provider. Wrap each render in `<ProjectProvider>` and add `vi.spyOn(api, 'listProjects').mockResolvedValue([])` where absent. **Assertions in those cases do not change** — this is the mechanical wrapper edit the Global Constraints permit.

- [ ] **Step 6: Run the full frontend suite**

```bash
cd frontend && npx vitest run
```

Expected: PASS. Any failure here is a missing provider wrapper, not a behavior change.

- [ ] **Step 7: Commit**

```bash
git add frontend/components/ui/Shell.tsx frontend/components/CreateTestForm.tsx frontend/__tests__/
git commit -m "feat(frontend): scope the test list and new tests to the selected project"
```

---

### Task 8: End-to-end coverage and both gates

**Files:**
- Create: `frontend/e2e/project-workspaces.spec.ts`
- Modify: `frontend/e2e/portal-shell.spec.ts` (the disabled-button assertion)
- Test: the whole suite, both languages

**Interfaces:**
- Consumes: everything from Tasks 1-7.
- Produces: nothing.

- [ ] **Step 1: Update the stale e2e assertion**

In `frontend/e2e/portal-shell.spec.ts`, the case named `workspace switcher shows Default checked and a disabled New project action` asserts `toBeDisabled()` on `+ New project`. Replace that case:

```ts
test('workspace switcher shows Default checked and an enabled New project action', async ({ page }) => {
  await page.goto('/');
  const trigger = page.getByRole('button', { name: /default/i });
  await trigger.click();
  await expect(page.getByRole('menuitemradio', { name: /default/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /new project/i })).toBeEnabled();

  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).toBeHidden();
});
```

- [ ] **Step 2: Write the e2e spec**

Create `frontend/e2e/project-workspaces.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

test('create a project, switch to it, and scope tests to it', async ({ page }) => {
  // Timestamped because the database outlives a run, and project names are
  // unique -- a fixed name would 409 on the second run.
  const project = `E2E Project ${Date.now()}`;
  const testName = `E2E Scoped Test ${Date.now()}`;
  await page.goto('/');

  await page.getByRole('button', { name: /default/i }).click();
  await page.getByRole('button', { name: /new project/i }).click();
  await page.getByRole('textbox', { name: /project name/i }).fill(project);
  await page.getByRole('textbox', { name: /project name/i }).press('Enter');

  // The switcher now reads the new project, and it starts empty.
  await expect(page.getByRole('button', { name: new RegExp(project, 'i') })).toBeVisible();
  await expect(page.getByRole('row', { name: new RegExp(testName, 'i') })).toHaveCount(0);

  await page.getByLabel(/name/i).fill(testName);
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('2');
  await page.getByLabel(/duration/i).fill('10');
  await page.getByRole('button', { name: /create test/i }).click();
  await expect(page.getByRole('row', { name: new RegExp(testName, 'i') })).toBeVisible();

  // Switching back to Default must not show the other project's test.
  await page.getByRole('button', { name: new RegExp(project, 'i') }).click();
  await page.getByRole('menuitemradio', { name: /default/i }).click();
  await expect(page.getByRole('button', { name: /default/i })).toBeVisible();
  await expect(page.getByRole('row', { name: new RegExp(testName, 'i') })).toHaveCount(0);
});

test('rejects a duplicate project name without losing what was typed', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: /default/i }).click();
  await page.getByRole('button', { name: /new project/i }).click();
  const input = page.getByRole('textbox', { name: /project name/i });
  await input.fill('Default');
  await input.press('Enter');

  await expect(page.getByText(/already exists/i)).toBeVisible();
  await expect(page.getByRole('textbox', { name: /project name/i })).toHaveValue('Default');
});
```

- [ ] **Step 3: Run both unit suites**

```bash
cd backend && go test ./...
cd ../frontend && npx vitest run
```

Expected: PASS both.

- [ ] **Step 4: Check both coverage gates**

```bash
cd backend && go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out | tail -1
cd ../frontend && npm run test:coverage
```

Expected: both ≥ 88% on every metric. Neither threshold is to be lowered.

- [ ] **Step 5: Typecheck and build**

```bash
cd frontend && npm run build
```

Expected: success.

- [ ] **Step 6: Run the browser suite against a real backend**

CI runs this in `integration-kind` (added in `9d4c4bd`), but it is runnable locally and worth running before pushing:

```bash
docker run -d --rm --name br-pg -e POSTGRES_USER=boltrunner -e POSTGRES_PASSWORD=boltrunner -e POSTGRES_DB=boltrunner -p 5432:5432 postgres:16
docker build -f deploy/Dockerfile.server -t boltrunner/server:local .
docker run -d --rm --name br-api --network host \
  -e DATABASE_URL="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" \
  -e KUBECONFIG=/kube/config -v "$HOME/.kube/config:/kube/config:ro" boltrunner/server:local
cd frontend && npm run build && npm start &
npx playwright test
```

Expected: all specs pass, now 13 tests across 5 files. Tear down with `docker rm -f br-api br-pg` afterwards.

- [ ] **Step 7: Commit**

```bash
git add frontend/e2e/
git commit -m "test(e2e): cover creating a project and scoping tests to it"
```

- [ ] **Step 8: Report what ran where**

State plainly which commands ran locally and which are delegated to CI. The Playwright suite genuinely does run in CI now — `integration-kind` serves the frontend and runs `npx playwright test` — so unlike the previous plan, "delegated to CI" is a checkable claim. Verify it in the job log rather than assuming it: `gh run view --job=<id> --log | grep "Run browser e2e"`.

---

## Self-review notes

- **Spec coverage.** `CreateProject` on both stores → Task 1. `POST /api/projects` with 201/400/409 → Task 2. `ListTestsForProject` and `?project_id=` → Task 3. `createProject` / `listTests(projectId?)` / `CreateTestInput.project_id` → Task 4. `ProjectProvider` including the stale-id fallback and the `listProjects` failure path → Task 5. Switcher list/select/inline-create and the 409-keeps-input behavior → Task 6. `Shell` and `CreateTestForm` scoping → Task 7. E2E and both gates → Task 8. Every row of the spec's error-handling table has a test: 409 (Tasks 2, 4, 6, 8), 400 empty/over-length/malformed (Task 2), network failure (Task 5), stale stored id (Task 5), `listProjects` failure (Tasks 5, 6).
- **Placeholder scan.** None. The one deliberately unresolved name is the postgres scratch-DB helper in Tasks 1 and 3, which the step tells the implementer to read from neighbouring tests rather than invent — the file already has one and a second would fragment setup.
- **Type consistency.** `CreateProject(ctx, *model.Project) error` is defined in Task 1 and consumed under that name in Tasks 2 and 3. `ListTestsForProject(ctx, string) ([]model.Test, error)` likewise between Tasks 3 and its two implementations. `useProjects()` returns `{ projects, selectedId, selected, select, create }` in Task 5 and is destructured with exactly those names in Tasks 6 and 7. `createProject(name)` and `listTests(projectId?)` keep their Task 4 signatures throughout. The storage key is `boltrunner-project` in Task 5's implementation and in Task 5's tests.
- **Ordering.** Task 5 must precede 6 and 7, which consume its context. Task 4 must precede 5. Task 1 must precede 2. Task 3 is independent of 1-2 and could run in parallel, but is sequenced after them so the backend commits land together.
- **Risk.** Two things a fresh implementer is most likely to get wrong. First, the stale-id fallback in Task 5: it is easy to read the stored id and trust it, which produces an empty switcher for anyone whose database was reseeded — Task 5's third test is what pins it. Second, the provider wrappers in Task 7 Step 5: `useProjects()` throws outside a provider, so every existing case rendering `Shell`, `TopNav` or `CreateTestForm` needs a wrapper. That failure is loud and mechanical, but it will look alarming mid-task; it is not a behavior regression.
