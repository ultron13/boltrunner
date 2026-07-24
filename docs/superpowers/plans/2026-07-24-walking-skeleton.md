# BoltRunner Walking Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the thinnest end-to-end slice of BoltRunner: create a test, run it as a real JMeter pod on a local Kubernetes cluster, watch live metrics in a Next.js UI, see a final summary.

**Architecture:** Go backend (single deployable service) backed by Postgres, submitting Kubernetes Jobs via client-go. Each Job pod runs a JMeter container plus a Go sidecar container that tails JMeter's `.jtl` output and POSTs metric snapshots back to the backend. The Next.js frontend polls the backend for status/metrics.

**Tech Stack:** Go 1.22+, `k8s.io/client-go`, `github.com/jackc/pgx/v5`, `github.com/go-chi/chi/v5`, PostgreSQL 16, Next.js 14 (App Router) + TypeScript, `kind` for local Kubernetes, Playwright for e2e.

## Global Constraints

- No authentication in this increment (per spec — deferred).
- Live updates via polling only, no WebSocket/SSE (per spec).
- Only JMeter as an engine; test plans are generated from one fixed template, no arbitrary script upload (per spec).
- Run status must always be derived from the real Kubernetes Job state via the watcher, never solely from the sidecar's last message (per spec's error-handling section).
- Every Kubernetes resource created for a run must be labeled `boltrunner.dev/run-id=<runID>` for correlation and later cleanup.
- Module path: `github.com/boltrunner/backend` for the Go module.

---

## File Structure

```
backend/
  go.mod, go.sum
  cmd/server/main.go            — wires everything, starts HTTP server + watcher goroutine
  cmd/sidecar/main.go           — sidecar reporter binary (runs inside the Job pod)
  internal/model/model.go       — Test, Run, RunMetricSnapshot, RunStatus
  internal/store/store.go       — TestStore, RunStore interfaces
  internal/store/memstore/memstore.go — in-memory impl, used in handler unit tests
  internal/store/postgres/postgres.go — pgx impl
  internal/store/postgres/migrations/0001_init.sql
  internal/jmx/template.go      — generates a .jmx string from parameters
  internal/k8sjob/builder.go    — builds ConfigMap + Job objects for a run
  internal/watcher/watcher.go   — polls Job status, flips run status
  internal/jtl/parse.go         — parses .jtl lines, computes rolling aggregates
  internal/api/server.go        — Server struct (deps) + router
  internal/api/tests.go         — POST/GET /api/tests
  internal/api/runs.go          — POST .../runs, GET /api/runs/{id}, POST .../metrics, POST .../cancel
deploy/
  kind-config.yaml
  Dockerfile.server
  Dockerfile.sidecar
  Dockerfile.jmeter
docker-compose.yml               — Postgres only, for local dev
frontend/
  package.json, tsconfig.json, next.config.ts, tailwind.config.ts
  app/layout.tsx
  app/page.tsx                   — test list + create form
  app/runs/[id]/page.tsx         — live run view
  components/CreateTestForm.tsx
  components/TestList.tsx
  components/LiveMetrics.tsx
  components/MetricsChart.tsx
  lib/api-client.ts
  hooks/useRunPolling.ts
  __tests__/CreateTestForm.test.tsx
  __tests__/LiveMetrics.test.tsx
  e2e/walking-skeleton.spec.ts
.github/workflows/ci.yml
README.md
```

---

### Task 1: Backend scaffold with health endpoint

**Files:**
- Create: `backend/go.mod`
- Create: `backend/cmd/server/main.go`
- Create: `backend/internal/api/server.go`
- Test: `backend/internal/api/server_test.go`

**Interfaces:**
- Produces: `api.NewServer() *api.Server` (extended in later tasks), `api.Server.Router() http.Handler`, `GET /healthz` returning `200 {"status":"ok"}`.

- [ ] **Step 1: Initialize the Go module**

```bash
cd backend
go mod init github.com/boltrunner/backend
go get github.com/go-chi/chi/v5@v5.0.12
```

- [ ] **Step 2: Write the failing test** — `backend/internal/api/server_test.go`

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	s := NewServer()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != `{"status":"ok"}` {
		t.Fatalf("unexpected body: %s", body)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/...`
Expected: FAIL — `NewServer` undefined.

- [ ] **Step 4: Write minimal implementation** — `backend/internal/api/server.go`

```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	router chi.Router
}

func NewServer() *Server {
	s := &Server{router: chi.NewRouter()}
	s.router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/...`
Expected: PASS

- [ ] **Step 6: Write `cmd/server/main.go`**

```go
package main

import (
	"log"
	"net/http"

	"github.com/boltrunner/backend/internal/api"
)

func main() {
	s := api.NewServer()
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", s.Router()); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 7: Verify it builds and runs**

Run: `cd backend && go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add backend/go.mod backend/go.sum backend/cmd backend/internal/api
git commit -m "feat(backend): scaffold Go module with health endpoint"
```

---

### Task 2: Data model + Postgres migration

**Files:**
- Create: `backend/internal/model/model.go`
- Create: `backend/internal/store/postgres/migrations/0001_init.sql`
- Create: `backend/internal/store/postgres/postgres.go`
- Test: `backend/internal/store/postgres/postgres_test.go`

**Interfaces:**
- Produces: `model.Test{ID, Name, TargetURL, VirtualUsers, DurationSeconds, CreatedAt}`, `model.Run{ID, TestID, Status, StartedAt, CompletedAt, ErrorMessage}`, `model.RunStatus` (`pending|running|completed|failed|stopped`), `model.RunMetricSnapshot{ID, RunID, Ts, ElapsedSeconds, ThroughputRPS, AvgResponseTimeMs, ErrorRatePct, SampleCount}`.
- Produces: `postgres.Connect(ctx context.Context, dsn string) (*postgres.DB, error)`, `postgres.DB.Migrate(ctx context.Context) error`.

- [ ] **Step 1: Write the model types** — `backend/internal/model/model.go`

```go
package model

import "time"

type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunStopped   RunStatus = "stopped"
)

type Test struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	TargetURL       string    `json:"target_url"`
	VirtualUsers    int       `json:"virtual_users"`
	DurationSeconds int       `json:"duration_seconds"`
	CreatedAt       time.Time `json:"created_at"`
}

type Run struct {
	ID           string     `json:"id"`
	TestID       string     `json:"test_id"`
	Status       RunStatus  `json:"status"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
}

type RunMetricSnapshot struct {
	ID                string    `json:"id"`
	RunID             string    `json:"run_id"`
	Ts                time.Time `json:"ts"`
	ElapsedSeconds    int       `json:"elapsed_seconds"`
	ThroughputRPS     float64   `json:"throughput_rps"`
	AvgResponseTimeMs float64   `json:"avg_response_time_ms"`
	ErrorRatePct      float64   `json:"error_rate_pct"`
	SampleCount       int       `json:"sample_count"`
}
```

- [ ] **Step 2: Write the migration** — `backend/internal/store/postgres/migrations/0001_init.sql`

```sql
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
    started_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    error_message TEXT NOT NULL DEFAULT ''
);

CREATE TABLE run_metric_snapshots (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id                UUID NOT NULL REFERENCES runs(id),
    ts                    TIMESTAMPTZ NOT NULL,
    elapsed_seconds       INTEGER NOT NULL,
    throughput_rps        DOUBLE PRECISION NOT NULL,
    avg_response_time_ms  DOUBLE PRECISION NOT NULL,
    error_rate_pct        DOUBLE PRECISION NOT NULL,
    sample_count          INTEGER NOT NULL
);

CREATE INDEX idx_run_metric_snapshots_run_id ON run_metric_snapshots(run_id, ts);
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

- [ ] **Step 3: Write the failing test** — `backend/internal/store/postgres/postgres_test.go`

```go
package postgres

import (
	"context"
	"os"
	"testing"
)

func TestConnectAndMigrate(t *testing.T) {
	dsn := os.Getenv("BOLTRUNNER_TEST_DSN")
	if dsn == "" {
		t.Skip("BOLTRUNNER_TEST_DSN not set; skipping (requires a live Postgres)")
	}
	ctx := context.Background()
	db, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/postgres/...`
Expected: FAIL — `Connect` undefined (compile error).

- [ ] **Step 5: Get the pgx dependency and embed migrations**

```bash
cd backend
go get github.com/jackc/pgx/v5@v5.6.0
```

- [ ] **Step 6: Write minimal implementation** — `backend/internal/store/postgres/postgres.go`

```go
package postgres

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/0001_init.sql
var migration0001 string

type DB struct {
	Pool *pgxpool.Pool
}

func Connect(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	db.Pool.Close()
}

func (db *DB) Migrate(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, migration0001)
	return err
}
```

- [ ] **Step 7: Run test to verify it passes (or skips cleanly without DSN)**

Run: `cd backend && go test ./internal/store/postgres/...`
Expected: `ok` (test skipped if `BOLTRUNNER_TEST_DSN` unset — a live-Postgres run happens in Task 11's docker-compose setup).

- [ ] **Step 8: Commit**

```bash
git add backend/internal/model backend/internal/store/postgres backend/go.mod backend/go.sum
git commit -m "feat(backend): add data model and Postgres migration"
```

---

### Task 3: Test store (interface + in-memory + Postgres) and Tests API

**Files:**
- Create: `backend/internal/store/store.go`
- Create: `backend/internal/store/memstore/memstore.go`
- Modify: `backend/internal/store/postgres/postgres.go` — add `TestStore` methods
- Test: `backend/internal/store/memstore/memstore_test.go`
- Create: `backend/internal/api/tests.go`
- Modify: `backend/internal/api/server.go` — accept a `store.TestStore` and register routes
- Test: `backend/internal/api/tests_test.go`

**Interfaces:**
- Consumes: `model.Test` (Task 2).
- Produces: `store.TestStore` interface: `CreateTest(ctx, *model.Test) error`, `ListTests(ctx) ([]model.Test, error)`, `GetTest(ctx, id string) (*model.Test, error)`, `store.ErrNotFound`.
- Produces: `memstore.NewTestStore() *memstore.TestStore` (implements `store.TestStore`).
- Produces: `api.NewServer(ts store.TestStore) *api.Server` (signature changes — later tasks add more params).
- Produces routes: `POST /api/tests`, `GET /api/tests`.

- [ ] **Step 1: Write the store interface** — `backend/internal/store/store.go`

```go
package store

import (
	"context"
	"errors"

	"github.com/boltrunner/backend/internal/model"
)

var ErrNotFound = errors.New("not found")

type TestStore interface {
	CreateTest(ctx context.Context, t *model.Test) error
	ListTests(ctx context.Context) ([]model.Test, error)
	GetTest(ctx context.Context, id string) (*model.Test, error)
}
```

- [ ] **Step 2: Write the failing memstore test** — `backend/internal/store/memstore/memstore_test.go`

```go
package memstore

import (
	"context"
	"testing"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

func TestTestStoreCreateListGet(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	in := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 10, DurationSeconds: 30}
	if err := ts.CreateTest(ctx, in); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if in.ID == "" {
		t.Fatal("expected CreateTest to assign an ID")
	}

	all, err := ts.ListTests(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListTests: %v, %d results", err, len(all))
	}

	got, err := ts.GetTest(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if got.Name != "smoke" {
		t.Fatalf("expected name 'smoke', got %q", got.Name)
	}

	if _, err := ts.GetTest(ctx, "missing"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/memstore/...`
Expected: FAIL — package `memstore` has no `NewTestStore`.

- [ ] **Step 4: Write minimal implementation** — `backend/internal/store/memstore/memstore.go`

```go
package memstore

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

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
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now().UTC()
	s.tests[t.ID] = *t
	return nil
}

func (s *TestStore) ListTests(ctx context.Context) ([]model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Test, 0, len(s.tests))
	for _, t := range s.tests {
		out = append(out, t)
	}
	return out, nil
}

func (s *TestStore) GetTest(ctx context.Context, id string) (*model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tests[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &t, nil
}
```

- [ ] **Step 5: Get the uuid dependency and run test to verify it passes**

```bash
cd backend && go get github.com/google/uuid@v1.6.0
go test ./internal/store/memstore/...
```

Expected: PASS

- [ ] **Step 6: Write the failing API test** — `backend/internal/api/tests_test.go`

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func TestCreateAndListTests(t *testing.T) {
	s := NewServer(memstore.NewTestStore())

	body, _ := json.Marshal(map[string]any{
		"name": "smoke", "target_url": "http://example.com",
		"virtual_users": 10, "duration_seconds": 30,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tests", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected an ID")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/tests", nil)
	rec2 := httptest.NewRecorder()
	s.Router().ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	var list []model.Test
	if err := json.Unmarshal(rec2.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 test, got %d", len(list))
	}
}
```

- [ ] **Step 7: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/...`
Expected: FAIL — `NewServer` signature mismatch (still takes zero args from Task 1).

- [ ] **Step 8: Update `server.go` and add `tests.go`** — `backend/internal/api/server.go`

```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/boltrunner/backend/internal/store"
)

type Server struct {
	router    chi.Router
	testStore store.TestStore
}

func NewServer(testStore store.TestStore) *Server {
	s := &Server{router: chi.NewRouter(), testStore: testStore}
	s.router.Get("/healthz", s.handleHealthz)
	s.router.Post("/api/tests", s.handleCreateTest)
	s.router.Get("/api/tests", s.handleListTests)
	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
```

`backend/internal/api/tests.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/boltrunner/backend/internal/model"
)

type createTestRequest struct {
	Name            string `json:"name"`
	TargetURL       string `json:"target_url"`
	VirtualUsers    int    `json:"virtual_users"`
	DurationSeconds int    `json:"duration_seconds"`
}

func (s *Server) handleCreateTest(w http.ResponseWriter, r *http.Request) {
	var req createTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.TargetURL == "" || req.VirtualUsers <= 0 || req.DurationSeconds <= 0 {
		http.Error(w, "name, target_url, virtual_users>0, duration_seconds>0 are required", http.StatusBadRequest)
		return
	}
	t := &model.Test{
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
```

- [ ] **Step 9: Update `cmd/server/main.go` to match the new `NewServer` signature**

```go
package main

import (
	"log"
	"net/http"

	"github.com/boltrunner/backend/internal/api"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func main() {
	s := api.NewServer(memstore.NewTestStore())
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", s.Router()); err != nil {
		log.Fatal(err)
	}
}
```

(This is replaced with the real Postgres store in Task 11.)

- [ ] **Step 10: Run tests and build to verify everything passes**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add backend/internal/store backend/internal/api backend/cmd/server/main.go
git commit -m "feat(backend): Test store and Tests API (create/list)"
```

---

### Task 4: JMX template generator

**Files:**
- Create: `backend/internal/jmx/template.go`
- Test: `backend/internal/jmx/template_test.go`

**Interfaces:**
- Produces: `jmx.Params{TargetURL string, VirtualUsers int, DurationSeconds int}`, `jmx.Generate(p Params) (string, error)`.

- [ ] **Step 1: Write the failing test** — `backend/internal/jmx/template_test.go`

```go
package jmx

import "testing"

func TestGenerateContainsParams(t *testing.T) {
	out, err := Generate(Params{TargetURL: "http://example.com/path", VirtualUsers: 25, DurationSeconds: 60})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	mustContain := []string{
		`<stringProp name="ThreadGroup.num_threads">25</stringProp>`,
		`<stringProp name="HTTPSampler.domain">example.com</stringProp>`,
		`<stringProp name="HTTPSampler.path">/path</stringProp>`,
		`<stringProp name="ThreadGroup.duration">60</stringProp>`,
	}
	for _, want := range mustContain {
		if !contains(out, want) {
			t.Fatalf("expected generated jmx to contain %q\n---\n%s", want, out)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestGenerateRejectsInvalidURL(t *testing.T) {
	_, err := Generate(Params{TargetURL: "not-a-url", VirtualUsers: 1, DurationSeconds: 1})
	if err == nil {
		t.Fatal("expected an error for an invalid target URL")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/jmx/...`
Expected: FAIL — `Generate` undefined.

- [ ] **Step 3: Write minimal implementation** — `backend/internal/jmx/template.go`

```go
package jmx

import (
	"bytes"
	"fmt"
	"net/url"
	"text/template"
)

type Params struct {
	TargetURL       string
	VirtualUsers    int
	DurationSeconds int
}

const jmxTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<jmeterTestPlan version="1.2" properties="5.0">
  <hashTree>
    <TestPlan testname="BoltRunner Generated Plan" enabled="true"/>
    <hashTree>
      <ThreadGroup testname="BoltRunner Threads" enabled="true">
        <stringProp name="ThreadGroup.num_threads">{{.VirtualUsers}}</stringProp>
        <stringProp name="ThreadGroup.ramp_time">1</stringProp>
        <stringProp name="ThreadGroup.duration">{{.DurationSeconds}}</stringProp>
        <boolProp name="ThreadGroup.scheduler">true</boolProp>
        <elementProp name="ThreadGroup.main_controller" elementType="LoopController">
          <boolProp name="LoopController.continue_forever">true</boolProp>
          <stringProp name="LoopController.loops">-1</stringProp>
        </elementProp>
      </ThreadGroup>
      <hashTree>
        <HTTPSamplerProxy testname="BoltRunner Request" enabled="true">
          <stringProp name="HTTPSampler.domain">{{.Host}}</stringProp>
          <stringProp name="HTTPSampler.port">{{.Port}}</stringProp>
          <stringProp name="HTTPSampler.protocol">{{.Scheme}}</stringProp>
          <stringProp name="HTTPSampler.path">{{.Path}}</stringProp>
          <stringProp name="HTTPSampler.method">GET</stringProp>
        </HTTPSamplerProxy>
        <hashTree/>
        <ResultCollector testname="BoltRunner Results" enabled="true">
          <stringProp name="filename">results.jtl</stringProp>
        </ResultCollector>
        <hashTree/>
      </hashTree>
    </hashTree>
  </hashTree>
</jmeterTestPlan>
`

type templateData struct {
	Params
	Scheme string
	Host   string
	Port   string
	Path   string
}

func Generate(p Params) (string, error) {
	u, err := url.Parse(p.TargetURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid target URL %q", p.TargetURL)
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	path := u.Path
	if path == "" {
		path = "/"
	}

	tmpl, err := template.New("jmx").Parse(jmxTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	data := templateData{Params: p, Scheme: u.Scheme, Host: u.Hostname(), Port: port, Path: path}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/jmx/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/jmx
git commit -m "feat(backend): generate JMeter test plans from run parameters"
```

---

### Task 5: Kubernetes Job/ConfigMap builder

**Files:**
- Create: `backend/internal/k8sjob/builder.go`
- Test: `backend/internal/k8sjob/builder_test.go`

**Interfaces:**
- Consumes: `jmx.Generate` output (a string), run ID.
- Produces: `k8sjob.Config{Namespace, JMeterImage, SidecarImage, BackendURL string}`, `k8sjob.Build(cfg Config, runID, jmxContent string) (*corev1.ConfigMap, *batchv1.Job)`.
- Label convention: every built object carries `boltrunner.dev/run-id=<runID>` (per Global Constraints).

- [ ] **Step 1: Add client-go dependencies**

```bash
cd backend
go get k8s.io/client-go@v0.30.2 k8s.io/api@v0.30.2 k8s.io/apimachinery@v0.30.2
```

- [ ] **Step 2: Write the failing test** — `backend/internal/k8sjob/builder_test.go`

```go
package k8sjob

import "testing"

func TestBuildLabelsAndContainers(t *testing.T) {
	cfg := Config{
		Namespace:    "boltrunner",
		JMeterImage:  "boltrunner/jmeter:local",
		SidecarImage: "boltrunner/sidecar:local",
		BackendURL:   "http://backend.boltrunner.svc:8080",
	}
	cm, job := Build(cfg, "run-123", "<jmeterTestPlan/>")

	if cm.Namespace != "boltrunner" || cm.Data["plan.jmx"] != "<jmeterTestPlan/>" {
		t.Fatalf("unexpected configmap: %+v", cm)
	}
	if job.Labels["boltrunner.dev/run-id"] != "run-123" {
		t.Fatalf("expected run-id label, got %v", job.Labels)
	}
	if len(job.Spec.Template.Spec.Containers) != 2 {
		t.Fatalf("expected 2 containers (jmeter+sidecar), got %d", len(job.Spec.Template.Spec.Containers))
	}
	names := map[string]bool{}
	for _, c := range job.Spec.Template.Spec.Containers {
		names[c.Name] = true
	}
	if !names["jmeter"] || !names["sidecar"] {
		t.Fatalf("expected containers named jmeter and sidecar, got %v", names)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/k8sjob/...`
Expected: FAIL — `Build` undefined.

- [ ] **Step 4: Write minimal implementation** — `backend/internal/k8sjob/builder.go`

```go
package k8sjob

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Config struct {
	Namespace    string
	JMeterImage  string
	SidecarImage string
	BackendURL   string
}

const runIDLabel = "boltrunner.dev/run-id"

func Build(cfg Config, runID, jmxContent string) (*corev1.ConfigMap, *batchv1.Job) {
	name := "run-" + runID
	labels := map[string]string{runIDLabel: runID, "app": "boltrunner-run"}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-plan", Namespace: cfg.Namespace, Labels: labels},
		Data:       map[string]string{"plan.jmx": jmxContent},
	}

	backoffLimit := int32(0)
	resultsVolume := corev1.Volume{Name: "results", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}
	planVolume := corev1.Volume{
		Name: "plan",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: cm.Name}},
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cfg.Namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes:       []corev1.Volume{resultsVolume, planVolume},
					Containers: []corev1.Container{
						{
							Name:  "jmeter",
							Image: cfg.JMeterImage,
							Command: []string{
								"sh", "-c",
								"jmeter -n -t /plan/plan.jmx -l /results/results.jtl; touch /results/done",
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "plan", MountPath: "/plan"},
								{Name: "results", MountPath: "/results"},
							},
						},
						{
							Name:  "sidecar",
							Image: cfg.SidecarImage,
							Env: []corev1.EnvVar{
								{Name: "RUN_ID", Value: runID},
								{Name: "BACKEND_URL", Value: cfg.BackendURL},
								{Name: "JTL_PATH", Value: "/results/results.jtl"},
								{Name: "DONE_PATH", Value: "/results/done"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "results", MountPath: "/results"},
							},
						},
					},
				},
			},
		},
	}

	return cm, job
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/k8sjob/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/k8sjob backend/go.mod backend/go.sum
git commit -m "feat(backend): build K8s ConfigMap+Job for a run (jmeter + sidecar)"
```

---

### Task 6: Run store + POST /api/tests/{id}/runs

**Files:**
- Modify: `backend/internal/store/store.go` — add `RunStore`
- Create: `backend/internal/store/memstore/runstore.go`
- Test: `backend/internal/store/memstore/runstore_test.go`
- Create: `backend/internal/api/runs.go`
- Modify: `backend/internal/api/server.go` — accept `RunStore`, a `kubernetes.Interface`, and `k8sjob.Config`; register the run route
- Test: `backend/internal/api/runs_test.go`

**Interfaces:**
- Consumes: `jmx.Generate` (Task 4), `k8sjob.Build` (Task 5), `store.ErrNotFound` (Task 3).
- Produces: `store.RunStore` interface: `CreateRun(ctx, *model.Run) error`, `GetRun(ctx, id string) (*model.Run, error)`, `UpdateRunStatus(ctx, id string, status model.RunStatus, errMsg string) error`, `AppendMetricSnapshot(ctx, *model.RunMetricSnapshot) error`, `LatestSnapshot(ctx, runID string) (*model.RunMetricSnapshot, error)`, `ListSnapshots(ctx, runID string) ([]model.RunMetricSnapshot, error)`.
- Produces: `memstore.NewRunStore() *memstore.RunStore` (implements `store.RunStore`).
- Produces: `api.NewServer(testStore store.TestStore, runStore store.RunStore, k8sClient kubernetes.Interface, jobCfg k8sjob.Config) *api.Server` (signature changes again).
- Produces route: `POST /api/tests/{id}/runs` → `201 model.Run` with `status=pending`.

- [ ] **Step 1: Add `RunStore` to the store interface** — append to `backend/internal/store/store.go`

```go
type RunStore interface {
	CreateRun(ctx context.Context, r *model.Run) error
	GetRun(ctx context.Context, id string) (*model.Run, error)
	UpdateRunStatus(ctx context.Context, id string, status model.RunStatus, errMsg string) error
	AppendMetricSnapshot(ctx context.Context, s *model.RunMetricSnapshot) error
	LatestSnapshot(ctx context.Context, runID string) (*model.RunMetricSnapshot, error)
	ListSnapshots(ctx context.Context, runID string) ([]model.RunMetricSnapshot, error)
}
```

- [ ] **Step 2: Write the failing memstore run test** — `backend/internal/store/memstore/runstore_test.go`

```go
package memstore

import (
	"context"
	"testing"

	"github.com/boltrunner/backend/internal/model"
)

func TestRunStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	rs := NewRunStore()

	r := &model.Run{TestID: "test-1", Status: model.RunPending}
	if err := rs.CreateRun(ctx, r); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if r.ID == "" {
		t.Fatal("expected an ID")
	}

	if err := rs.UpdateRunStatus(ctx, r.ID, model.RunRunning, ""); err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}
	got, err := rs.GetRun(ctx, r.ID)
	if err != nil || got.Status != model.RunRunning {
		t.Fatalf("expected running, got %+v, err=%v", got, err)
	}

	snap := &model.RunMetricSnapshot{RunID: r.ID, ElapsedSeconds: 1, ThroughputRPS: 10, AvgResponseTimeMs: 100, ErrorRatePct: 0, SampleCount: 10}
	if err := rs.AppendMetricSnapshot(ctx, snap); err != nil {
		t.Fatalf("AppendMetricSnapshot: %v", err)
	}
	latest, err := rs.LatestSnapshot(ctx, r.ID)
	if err != nil || latest.ThroughputRPS != 10 {
		t.Fatalf("LatestSnapshot: %+v, err=%v", latest, err)
	}
	all, err := rs.ListSnapshots(ctx, r.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListSnapshots: %d, err=%v", len(all), err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/memstore/...`
Expected: FAIL — `NewRunStore` undefined.

- [ ] **Step 4: Write minimal implementation** — `backend/internal/store/memstore/runstore.go`

```go
package memstore

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

type RunStore struct {
	mu        sync.RWMutex
	runs      map[string]model.Run
	snapshots map[string][]model.RunMetricSnapshot
}

func NewRunStore() *RunStore {
	return &RunStore{runs: make(map[string]model.Run), snapshots: make(map[string][]model.RunMetricSnapshot)}
}

func (s *RunStore) CreateRun(ctx context.Context, r *model.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = uuid.NewString()
	s.runs[r.ID] = *r
	return nil
}

func (s *RunStore) GetRun(ctx context.Context, id string) (*model.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &r, nil
}

func (s *RunStore) UpdateRunStatus(ctx context.Context, id string, status model.RunStatus, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return store.ErrNotFound
	}
	r.Status = status
	r.ErrorMessage = errMsg
	now := time.Now().UTC()
	switch status {
	case model.RunRunning:
		if r.StartedAt == nil {
			r.StartedAt = &now
		}
	case model.RunCompleted, model.RunFailed, model.RunStopped:
		r.CompletedAt = &now
	}
	s.runs[id] = r
	return nil
}

func (s *RunStore) AppendMetricSnapshot(ctx context.Context, snap *model.RunMetricSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[snap.RunID]; !ok {
		return store.ErrNotFound
	}
	snap.ID = uuid.NewString()
	snap.Ts = time.Now().UTC()
	s.snapshots[snap.RunID] = append(s.snapshots[snap.RunID], *snap)
	return nil
}

func (s *RunStore) LatestSnapshot(ctx context.Context, runID string) (*model.RunMetricSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := s.snapshots[runID]
	if len(list) == 0 {
		return nil, store.ErrNotFound
	}
	latest := list[len(list)-1]
	return &latest, nil
}

func (s *RunStore) ListSnapshots(ctx context.Context, runID string) ([]model.RunMetricSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.RunMetricSnapshot, len(s.snapshots[runID]))
	copy(out, s.snapshots[runID])
	return out, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/memstore/...`
Expected: PASS

- [ ] **Step 6: Write the failing API test** — `backend/internal/api/runs_test.go`

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func newTestServer() *Server {
	ts := memstore.NewTestStore()
	rs := memstore.NewRunStore()
	fakeClient := k8sfake.NewSimpleClientset()
	cfg := k8sjob.Config{Namespace: "boltrunner", JMeterImage: "jmeter:local", SidecarImage: "sidecar:local", BackendURL: "http://backend:8080"}
	return NewServer(ts, rs, fakeClient, cfg)
}

func TestStartRunCreatesJob(t *testing.T) {
	s := newTestServer()

	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	if err := s.testStore.CreateTest(nil, test); err != nil {
		t.Fatalf("seed test: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var run model.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if run.Status != model.RunPending {
		t.Fatalf("expected pending, got %s", run.Status)
	}

	jobs, err := s.k8sClient.BatchV1().Jobs("boltrunner").List(req.Context(), metaListOpts())
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("expected 1 job created, got %d", len(jobs.Items))
	}
}

func TestStartRunUnknownTest(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/tests/missing/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
```

- [ ] **Step 7: Add a tiny helper used by the test** — append to `backend/internal/api/runs_test.go`

```go
func metaListOpts() (opts metav1.ListOptions) { return }
```

Add the import `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` to the test file's import block.

- [ ] **Step 8: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/...`
Expected: FAIL — `NewServer` signature mismatch, `s.k8sClient`/`s.testStore` not accessible with new shape.

- [ ] **Step 9: Update `server.go`** — replace its body with:

```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/kubernetes"

	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/store"
)

type Server struct {
	router    chi.Router
	testStore store.TestStore
	runStore  store.RunStore
	k8sClient kubernetes.Interface
	jobCfg    k8sjob.Config
}

func NewServer(testStore store.TestStore, runStore store.RunStore, k8sClient kubernetes.Interface, jobCfg k8sjob.Config) *Server {
	s := &Server{
		router:    chi.NewRouter(),
		testStore: testStore,
		runStore:  runStore,
		k8sClient: k8sClient,
		jobCfg:    jobCfg,
	}
	s.router.Get("/healthz", s.handleHealthz)
	s.router.Post("/api/tests", s.handleCreateTest)
	s.router.Get("/api/tests", s.handleListTests)
	s.router.Post("/api/tests/{testID}/runs", s.handleStartRun)
	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
```

- [ ] **Step 10: Write `handleStartRun`** — `backend/internal/api/runs.go`

```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/boltrunner/backend/internal/jmx"
	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	testID := chi.URLParam(r, "testID")
	test, err := s.testStore.GetTest(r.Context(), testID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "test not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to load test", http.StatusInternalServerError)
		return
	}

	run := &model.Run{TestID: test.ID, Status: model.RunPending}
	if err := s.runStore.CreateRun(r.Context(), run); err != nil {
		http.Error(w, "failed to create run", http.StatusInternalServerError)
		return
	}

	plan, err := jmx.Generate(jmx.Params{TargetURL: test.TargetURL, VirtualUsers: test.VirtualUsers, DurationSeconds: test.DurationSeconds})
	if err != nil {
		s.runStore.UpdateRunStatus(r.Context(), run.ID, model.RunFailed, "failed to generate test plan: "+err.Error())
		http.Error(w, "failed to generate test plan", http.StatusInternalServerError)
		return
	}

	cm, job := k8sjob.Build(s.jobCfg, run.ID, plan)
	if _, err := s.k8sClient.CoreV1().ConfigMaps(s.jobCfg.Namespace).Create(r.Context(), cm, metav1.CreateOptions{}); err != nil {
		s.runStore.UpdateRunStatus(r.Context(), run.ID, model.RunFailed, "failed to create configmap: "+err.Error())
		http.Error(w, "failed to submit run", http.StatusInternalServerError)
		return
	}
	if _, err := s.k8sClient.BatchV1().Jobs(s.jobCfg.Namespace).Create(r.Context(), job, metav1.CreateOptions{}); err != nil {
		s.runStore.UpdateRunStatus(r.Context(), run.ID, model.RunFailed, "failed to create job: "+err.Error())
		http.Error(w, "failed to submit run", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}
```

- [ ] **Step 11: Update `cmd/server/main.go` for the new constructor**

```go
package main

import (
	"log"
	"net/http"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/boltrunner/backend/internal/api"
	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func main() {
	cfg := k8sjob.Config{Namespace: "boltrunner", JMeterImage: "boltrunner/jmeter:local", SidecarImage: "boltrunner/sidecar:local", BackendURL: "http://localhost:8080"}
	s := api.NewServer(memstore.NewTestStore(), memstore.NewRunStore(), fake.NewSimpleClientset(), cfg)
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", s.Router()); err != nil {
		log.Fatal(err)
	}
}
```

(The fake clientset here is temporary — Task 11 wires a real one from kubeconfig/in-cluster config.)

- [ ] **Step 12: Run tests and build to verify everything passes**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 13: Commit**

```bash
git add backend/internal/store backend/internal/api backend/cmd/server/main.go
git commit -m "feat(backend): Run store and POST /api/tests/:id/runs (submits K8s Job)"
```

---

### Task 7: Job-status watcher

**Files:**
- Create: `backend/internal/watcher/watcher.go`
- Test: `backend/internal/watcher/watcher_test.go`

**Interfaces:**
- Consumes: `store.RunStore` (Task 6), `kubernetes.Interface` (client-go), the `boltrunner.dev/run-id` label (Task 5).
- Produces: `watcher.New(k8sClient kubernetes.Interface, runStore store.RunStore, namespace string, unschedulableTimeout time.Duration) *watcher.Watcher`, `(*Watcher).PollOnce(ctx context.Context) error`, `(*Watcher).Run(ctx context.Context, interval time.Duration)`.

- [ ] **Step 1: Write the failing test** — `backend/internal/watcher/watcher_test.go`

```go
package watcher

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func TestPollOnceMarksCompleted(t *testing.T) {
	ctx := context.Background()
	rs := memstore.NewRunStore()
	run := &model.Run{Status: model.RunRunning}
	_ = rs.CreateRun(ctx, run)

	client := k8sfake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "run-" + run.ID, Namespace: "boltrunner",
			Labels: map[string]string{"boltrunner.dev/run-id": run.ID},
		},
		Status: batchv1.JobStatus{Succeeded: 1},
	})

	w := New(client, rs, "boltrunner", 30*time.Second)
	if err := w.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := rs.GetRun(ctx, run.ID)
	if got.Status != model.RunCompleted {
		t.Fatalf("expected completed, got %s", got.Status)
	}
}

func TestPollOnceMarksFailed(t *testing.T) {
	ctx := context.Background()
	rs := memstore.NewRunStore()
	run := &model.Run{Status: model.RunRunning}
	_ = rs.CreateRun(ctx, run)

	client := k8sfake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "run-" + run.ID, Namespace: "boltrunner",
			Labels: map[string]string{"boltrunner.dev/run-id": run.ID},
		},
		Status: batchv1.JobStatus{Failed: 1},
	})

	w := New(client, rs, "boltrunner", 30*time.Second)
	if err := w.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := rs.GetRun(ctx, run.ID)
	if got.Status != model.RunFailed {
		t.Fatalf("expected failed, got %s", got.Status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/watcher/...`
Expected: FAIL — `New`/`PollOnce` undefined.

- [ ] **Step 3: Write minimal implementation** — `backend/internal/watcher/watcher.go`

```go
package watcher

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

type Watcher struct {
	k8sClient            kubernetes.Interface
	runStore             store.RunStore
	namespace            string
	unschedulableTimeout time.Duration
}

func New(k8sClient kubernetes.Interface, runStore store.RunStore, namespace string, unschedulableTimeout time.Duration) *Watcher {
	return &Watcher{k8sClient: k8sClient, runStore: runStore, namespace: namespace, unschedulableTimeout: unschedulableTimeout}
}

// PollOnce reconciles every Job labeled with a run-id against the run's stored status.
// Completion/failure is always derived from the Job's real status, never from the
// sidecar's last metrics POST alone (see spec's error-handling section).
func (w *Watcher) PollOnce(ctx context.Context) error {
	jobs, err := w.k8sClient.BatchV1().Jobs(w.namespace).List(ctx, metav1.ListOptions{LabelSelector: "boltrunner.dev/run-id"})
	if err != nil {
		return err
	}
	for _, job := range jobs.Items {
		runID := job.Labels["boltrunner.dev/run-id"]
		run, err := w.runStore.GetRun(ctx, runID)
		if err != nil {
			continue
		}
		switch {
		case job.Status.Succeeded > 0 && run.Status != model.RunCompleted:
			w.runStore.UpdateRunStatus(ctx, runID, model.RunCompleted, "")
		case job.Status.Failed > 0 && run.Status != model.RunFailed:
			w.runStore.UpdateRunStatus(ctx, runID, model.RunFailed, "job failed")
		case job.Status.Active > 0 && run.Status == model.RunPending:
			w.runStore.UpdateRunStatus(ctx, runID, model.RunRunning, "")
		case run.Status == model.RunPending && job.Status.Active == 0 && job.Status.Succeeded == 0 && job.Status.Failed == 0:
			if run.StartedAt == nil && time.Since(job.CreationTimestamp.Time) > w.unschedulableTimeout {
				w.runStore.UpdateRunStatus(ctx, runID, model.RunFailed, "unschedulable")
			}
		}
	}
	return nil
}

// Run polls on the given interval until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = w.PollOnce(ctx)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/watcher/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/watcher
git commit -m "feat(backend): Job-status watcher reconciles run status from K8s Job state"
```

---

### Task 8: Metrics ingest + GET /api/runs/{id}

**Files:**
- Modify: `backend/internal/api/runs.go` — add `handlePostMetrics`, `handleGetRun`
- Modify: `backend/internal/api/server.go` — register the two routes
- Modify: `backend/internal/api/runs_test.go` — add tests

**Interfaces:**
- Consumes: `store.RunStore.AppendMetricSnapshot`, `LatestSnapshot`, `ListSnapshots`, `GetRun` (Task 6).
- Produces routes: `POST /api/runs/{runID}/metrics` (body: `elapsed_seconds, throughput_rps, avg_response_time_ms, error_rate_pct, sample_count`) → `202`. `GET /api/runs/{runID}` → `200 {run, latest, history}`.

- [ ] **Step 1: Write the failing tests** — append to `backend/internal/api/runs_test.go`

```go
func TestPostMetricsAndGetRun(t *testing.T) {
	s := newTestServer()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = s.testStore.CreateTest(nil, test)
	run := &model.Run{TestID: test.ID, Status: model.RunRunning}
	_ = s.runStore.CreateRun(nil, run)

	body, _ := json.Marshal(map[string]any{
		"elapsed_seconds": 1, "throughput_rps": 12.5, "avg_response_time_ms": 210.0,
		"error_rate_pct": 0.0, "sample_count": 12,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/metrics", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID, nil)
	rec2 := httptest.NewRecorder()
	s.Router().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	var resp struct {
		Run     model.Run                 `json:"run"`
		Latest  *model.RunMetricSnapshot  `json:"latest"`
		History []model.RunMetricSnapshot `json:"history"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Latest == nil || resp.Latest.ThroughputRPS != 12.5 {
		t.Fatalf("unexpected latest: %+v", resp.Latest)
	}
	if len(resp.History) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(resp.History))
	}
}
```

Add `"bytes"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/...`
Expected: FAIL — 404 (routes not registered).

- [ ] **Step 3: Register the routes** — in `backend/internal/api/server.go`, add inside `NewServer` after the existing routes:

```go
	s.router.Post("/api/runs/{runID}/metrics", s.handlePostMetrics)
	s.router.Get("/api/runs/{runID}", s.handleGetRun)
```

- [ ] **Step 4: Implement the handlers** — append to `backend/internal/api/runs.go`

```go
type postMetricsRequest struct {
	ElapsedSeconds    int     `json:"elapsed_seconds"`
	ThroughputRPS     float64 `json:"throughput_rps"`
	AvgResponseTimeMs float64 `json:"avg_response_time_ms"`
	ErrorRatePct      float64 `json:"error_rate_pct"`
	SampleCount       int     `json:"sample_count"`
}

func (s *Server) handlePostMetrics(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	var req postMetricsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	snap := &model.RunMetricSnapshot{
		RunID: runID, ElapsedSeconds: req.ElapsedSeconds, ThroughputRPS: req.ThroughputRPS,
		AvgResponseTimeMs: req.AvgResponseTimeMs, ErrorRatePct: req.ErrorRatePct, SampleCount: req.SampleCount,
	}
	if err := s.runStore.AppendMetricSnapshot(r.Context(), snap); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to store snapshot", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type getRunResponse struct {
	Run     model.Run                 `json:"run"`
	Latest  *model.RunMetricSnapshot  `json:"latest,omitempty"`
	History []model.RunMetricSnapshot `json:"history"`
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	run, err := s.runStore.GetRun(r.Context(), runID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to load run", http.StatusInternalServerError)
		return
	}
	history, err := s.runStore.ListSnapshots(r.Context(), runID)
	if err != nil {
		http.Error(w, "failed to load history", http.StatusInternalServerError)
		return
	}
	resp := getRunResponse{Run: *run, History: history}
	if latest, err := s.runStore.LatestSnapshot(r.Context(), runID); err == nil {
		resp.Latest = latest
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api
git commit -m "feat(backend): metrics ingest endpoint and GET /api/runs/:id"
```

---

### Task 9: Cancel endpoint

**Files:**
- Modify: `backend/internal/api/runs.go` — add `handleCancelRun`
- Modify: `backend/internal/api/server.go` — register route
- Modify: `backend/internal/api/runs_test.go` — add test

**Interfaces:**
- Produces route: `POST /api/runs/{runID}/cancel` → deletes the Job (name `run-<runID>`), sets status `stopped`, `204`.

- [ ] **Step 1: Write the failing test** — append to `backend/internal/api/runs_test.go`

```go
func TestCancelRunDeletesJob(t *testing.T) {
	s := newTestServer()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = s.testStore.CreateTest(nil, test)

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	var run model.Run
	json.Unmarshal(rec.Body.Bytes(), &run)

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/cancel", nil)
	cancelRec := httptest.NewRecorder()
	s.Router().ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", cancelRec.Code, cancelRec.Body.String())
	}

	got, _ := s.runStore.GetRun(cancelReq.Context(), run.ID)
	if got.Status != model.RunStopped {
		t.Fatalf("expected stopped, got %s", got.Status)
	}

	jobs, _ := s.k8sClient.BatchV1().Jobs("boltrunner").List(cancelReq.Context(), metaListOpts())
	if len(jobs.Items) != 0 {
		t.Fatalf("expected job to be deleted, still have %d", len(jobs.Items))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/...`
Expected: FAIL — 404 (route not registered).

- [ ] **Step 3: Register the route** — in `backend/internal/api/server.go`, add:

```go
	s.router.Post("/api/runs/{runID}/cancel", s.handleCancelRun)
```

- [ ] **Step 4: Implement the handler** — append to `backend/internal/api/runs.go`

```go
func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	if _, err := s.runStore.GetRun(r.Context(), runID); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}

	background := metav1.DeletePropagationBackground
	err := s.k8sClient.BatchV1().Jobs(s.jobCfg.Namespace).Delete(r.Context(), "run-"+runID, metav1.DeleteOptions{PropagationPolicy: &background})
	if err != nil && !k8serrors.IsNotFound(err) {
		http.Error(w, "failed to delete job", http.StatusInternalServerError)
		return
	}

	if err := s.runStore.UpdateRunStatus(r.Context(), runID, model.RunStopped, ""); err != nil {
		http.Error(w, "failed to update run", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Add the import `k8serrors "k8s.io/apimachinery/pkg/api/errors"` to `runs.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api
git commit -m "feat(backend): cancel endpoint deletes the K8s Job and marks run stopped"
```

---

### Task 10: JTL parser (pure function)

**Files:**
- Create: `backend/internal/jtl/parse.go`
- Test: `backend/internal/jtl/parse_test.go`

**Interfaces:**
- Produces: `jtl.Sample{TimestampMs int64, ElapsedMs int64, Success bool}`, `jtl.ParseLine(line string) (jtl.Sample, bool, error)` (second return is `false` for the CSV header line), `jtl.Aggregate{ThroughputRPS, AvgResponseTimeMs, ErrorRatePct float64; SampleCount int}`, `jtl.Aggregate(samples []Sample, windowSeconds float64) Aggregate`.

- [ ] **Step 1: Write the failing test** — `backend/internal/jtl/parse_test.go`

```go
package jtl

import "testing"

const header = "timeStamp,elapsed,label,responseCode,responseMessage,threadName,dataType,success,failureMessage,bytes,sentBytes,grpThreads,allThreads,URL,Latency,IdleTime,Connect"

func TestParseLineHeaderIsSkipped(t *testing.T) {
	_, ok, err := ParseLine(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected header line to be skipped (ok=false)")
	}
}

func TestParseLineSample(t *testing.T) {
	line := "1690000000000,214,BoltRunner Request,200,OK,Thread Group 1-1,text,true,,1024,128,5,5,http://example.com/,210,0,3"
	s, ok, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a data line")
	}
	if s.TimestampMs != 1690000000000 || s.ElapsedMs != 214 || !s.Success {
		t.Fatalf("unexpected sample: %+v", s)
	}
}

func TestParseLineFailedSample(t *testing.T) {
	line := "1690000000000,214,BoltRunner Request,500,Error,Thread Group 1-1,text,false,,0,0,5,5,http://example.com/,210,0,3"
	s, ok, err := ParseLine(line)
	if err != nil || !ok {
		t.Fatalf("unexpected: ok=%v err=%v", ok, err)
	}
	if s.Success {
		t.Fatal("expected Success=false")
	}
}

func TestAggregate(t *testing.T) {
	samples := []Sample{
		{ElapsedMs: 100, Success: true},
		{ElapsedMs: 200, Success: true},
		{ElapsedMs: 300, Success: false},
		{ElapsedMs: 400, Success: true},
	}
	agg := Aggregate(samples, 2.0)

	if agg.SampleCount != 4 {
		t.Fatalf("expected 4 samples, got %d", agg.SampleCount)
	}
	if agg.ThroughputRPS != 2.0 {
		t.Fatalf("expected throughput 2.0 (4 samples / 2s), got %f", agg.ThroughputRPS)
	}
	if agg.AvgResponseTimeMs != 250.0 {
		t.Fatalf("expected avg response time 250.0, got %f", agg.AvgResponseTimeMs)
	}
	if agg.ErrorRatePct != 25.0 {
		t.Fatalf("expected error rate 25.0, got %f", agg.ErrorRatePct)
	}
}

func TestAggregateEmpty(t *testing.T) {
	agg := Aggregate(nil, 1.0)
	if agg.SampleCount != 0 || agg.ThroughputRPS != 0 || agg.AvgResponseTimeMs != 0 || agg.ErrorRatePct != 0 {
		t.Fatalf("expected all-zero aggregate for no samples, got %+v", agg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/jtl/...`
Expected: FAIL — `ParseLine`/`Aggregate` undefined.

- [ ] **Step 3: Write minimal implementation** — `backend/internal/jtl/parse.go`

```go
package jtl

import (
	"strconv"
	"strings"
)

type Sample struct {
	TimestampMs int64
	ElapsedMs   int64
	Success     bool
}

// ParseLine parses one line of a JMeter CSV .jtl file. The second return value
// is false for the header line (or any unparseable line), never an error, so
// callers tailing a live file can simply skip non-sample lines.
func ParseLine(line string) (Sample, bool, error) {
	fields := strings.Split(strings.TrimRight(line, "\r\n"), ",")
	if len(fields) < 8 {
		return Sample{}, false, nil
	}
	ts, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return Sample{}, false, nil // header or malformed line
	}
	elapsed, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return Sample{}, false, nil
	}
	success := fields[7] == "true"
	return Sample{TimestampMs: ts, ElapsedMs: elapsed, Success: success}, true, nil
}

type AggregateResult struct {
	ThroughputRPS     float64
	AvgResponseTimeMs float64
	ErrorRatePct      float64
	SampleCount       int
}

// Aggregate computes rolling metrics for a batch of samples collected over windowSeconds.
func Aggregate(samples []Sample, windowSeconds float64) AggregateResult {
	if len(samples) == 0 {
		return AggregateResult{}
	}
	var totalElapsed int64
	var failed int
	for _, s := range samples {
		totalElapsed += s.ElapsedMs
		if !s.Success {
			failed++
		}
	}
	n := len(samples)
	result := AggregateResult{
		SampleCount:       n,
		AvgResponseTimeMs: float64(totalElapsed) / float64(n),
		ErrorRatePct:      float64(failed) / float64(n) * 100,
	}
	if windowSeconds > 0 {
		result.ThroughputRPS = float64(n) / windowSeconds
	}
	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/jtl/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/jtl
git commit -m "feat(backend): JTL line parser and rolling aggregate (pure functions)"
```

---

### Task 11: Sidecar reporter binary

**Files:**
- Create: `backend/cmd/sidecar/main.go`
- Test: `backend/cmd/sidecar/main_test.go`

**Interfaces:**
- Consumes: `jtl.ParseLine`, `jtl.Aggregate` (Task 10).
- Produces: `readNewLines(f *os.File, offset int64) (lines []string, newOffset int64, err error)`, `postSnapshot(backendURL, runID string, agg jtl.AggregateResult, elapsedSeconds int) error` — both unit-testable in isolation from `main()`.

- [ ] **Step 1: Write the failing test** — `backend/cmd/sidecar/main_test.go`

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/boltrunner/backend/internal/jtl"
)

func TestReadNewLinesIncremental(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "results-*.jtl")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()

	f.WriteString("header\nline1\n")
	lines, offset, err := readNewLines(f, 0)
	if err != nil {
		t.Fatalf("readNewLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}

	f.WriteString("line2\n")
	more, offset2, err := readNewLines(f, offset)
	if err != nil {
		t.Fatalf("readNewLines: %v", err)
	}
	if len(more) != 1 || more[0] != "line2" {
		t.Fatalf("expected [line2], got %v", more)
	}
	if offset2 <= offset {
		t.Fatalf("expected offset to advance, got %d -> %d", offset, offset2)
	}
}

func TestPostSnapshotSendsExpectedJSON(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	agg := jtl.AggregateResult{ThroughputRPS: 5, AvgResponseTimeMs: 120, ErrorRatePct: 1.5, SampleCount: 5}
	if err := postSnapshot(srv.URL, "run-1", agg, 3); err != nil {
		t.Fatalf("postSnapshot: %v", err)
	}
	if received["elapsed_seconds"].(float64) != 3 {
		t.Fatalf("unexpected payload: %+v", received)
	}
	if received["throughput_rps"].(float64) != 5 {
		t.Fatalf("unexpected payload: %+v", received)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./cmd/sidecar/...`
Expected: FAIL — `readNewLines`/`postSnapshot` undefined.

- [ ] **Step 3: Write minimal implementation** — `backend/cmd/sidecar/main.go`

```go
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/boltrunner/backend/internal/jtl"
)

func readNewLines(f *os.File, offset int64) ([]string, int64, error) {
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, offset, err
	}
	var lines []string
	scanner := bufio.NewScanner(f)
	newOffset := offset
	for scanner.Scan() {
		line := scanner.Text()
		newOffset += int64(len(line)) + 1 // +1 for the newline
		lines = append(lines, line)
	}
	return lines, newOffset, scanner.Err()
}

func postSnapshot(backendURL, runID string, agg jtl.AggregateResult, elapsedSeconds int) error {
	payload := map[string]any{
		"elapsed_seconds":      elapsedSeconds,
		"throughput_rps":       agg.ThroughputRPS,
		"avg_response_time_ms": agg.AvgResponseTimeMs,
		"error_rate_pct":       agg.ErrorRatePct,
		"sample_count":         agg.SampleCount,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := strings.TrimRight(backendURL, "/") + "/api/runs/" + runID + "/metrics"
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func main() {
	runID := os.Getenv("RUN_ID")
	backendURL := os.Getenv("BACKEND_URL")
	jtlPath := os.Getenv("JTL_PATH")
	donePath := os.Getenv("DONE_PATH")

	var f *os.File
	var err error
	for {
		f, err = os.Open(jtlPath)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	defer f.Close()

	var offset int64
	elapsed := 0
	for {
		lines, newOffset, err := readNewLines(f, offset)
		offset = newOffset
		if err != nil {
			log.Printf("read error: %v", err)
		}

		var samples []jtl.Sample
		for _, line := range lines {
			s, ok, perr := jtl.ParseLine(line)
			if perr == nil && ok {
				samples = append(samples, s)
			}
		}
		elapsed++
		agg := jtl.Aggregate(samples, 1.0)
		if err := postSnapshot(backendURL, runID, agg, elapsed); err != nil {
			log.Printf("post snapshot failed: %v", err)
		}

		if _, err := os.Stat(donePath); err == nil {
			log.Println("done marker found, exiting")
			return
		}
		time.Sleep(1 * time.Second)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./cmd/sidecar/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/sidecar
git commit -m "feat(backend): sidecar reporter tails JTL output and posts metric snapshots"
```

---

### Task 12: Postgres implementations of TestStore and RunStore

**Files:**
- Modify: `backend/internal/store/postgres/postgres.go` — add `TestStore`/`RunStore` methods on `*DB`
- Test: `backend/internal/store/postgres/store_test.go`

**Interfaces:**
- Consumes: `store.TestStore`, `store.RunStore` (Task 3, Task 6), `postgres.DB` (Task 2).
- Produces: `*postgres.DB` satisfies both `store.TestStore` and `store.RunStore`.

- [ ] **Step 1: Write the failing test** — `backend/internal/store/postgres/store_test.go`

```go
package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/boltrunner/backend/internal/model"
)

func setupDB(t *testing.T) *DB {
	dsn := os.Getenv("BOLTRUNNER_TEST_DSN")
	if dsn == "" {
		t.Skip("BOLTRUNNER_TEST_DSN not set; skipping (requires a live Postgres)")
	}
	ctx := context.Background()
	db, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestTestStoreCRUD(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	if err := db.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if tst.ID == "" {
		t.Fatal("expected an ID")
	}

	got, err := db.GetTest(ctx, tst.ID)
	if err != nil || got.Name != "smoke" {
		t.Fatalf("GetTest: %+v, err=%v", got, err)
	}
}

func TestRunStoreLifecycle(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = db.CreateTest(ctx, tst)

	run := &model.Run{TestID: tst.ID, Status: model.RunPending}
	if err := db.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	if err := db.UpdateRunStatus(ctx, run.ID, model.RunRunning, ""); err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}

	snap := &model.RunMetricSnapshot{RunID: run.ID, ElapsedSeconds: 1, ThroughputRPS: 3, AvgResponseTimeMs: 90, ErrorRatePct: 0, SampleCount: 3}
	if err := db.AppendMetricSnapshot(ctx, snap); err != nil {
		t.Fatalf("AppendMetricSnapshot: %v", err)
	}

	latest, err := db.LatestSnapshot(ctx, run.ID)
	if err != nil || latest.ThroughputRPS != 3 {
		t.Fatalf("LatestSnapshot: %+v, err=%v", latest, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails or skips**

Run: `cd backend && go test ./internal/store/postgres/...`
Expected: FAIL if `BOLTRUNNER_TEST_DSN` is set (compile error, methods undefined) — otherwise SKIP.

- [ ] **Step 3: Write minimal implementation** — append to `backend/internal/store/postgres/postgres.go`

```go
func (db *DB) CreateTest(ctx context.Context, t *model.Test) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO tests (name, target_url, virtual_users, duration_seconds)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		t.Name, t.TargetURL, t.VirtualUsers, t.DurationSeconds,
	).Scan(&t.ID, &t.CreatedAt)
}

func (db *DB) ListTests(ctx context.Context) ([]model.Test, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, target_url, virtual_users, duration_seconds, created_at FROM tests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Test
	for rows.Next() {
		var t model.Test
		if err := rows.Scan(&t.ID, &t.Name, &t.TargetURL, &t.VirtualUsers, &t.DurationSeconds, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) GetTest(ctx context.Context, id string) (*model.Test, error) {
	var t model.Test
	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, target_url, virtual_users, duration_seconds, created_at FROM tests WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.TargetURL, &t.VirtualUsers, &t.DurationSeconds, &t.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &t, err
}

func (db *DB) CreateRun(ctx context.Context, r *model.Run) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO runs (test_id, status) VALUES ($1, $2) RETURNING id`,
		r.TestID, r.Status,
	).Scan(&r.ID)
}

func (db *DB) GetRun(ctx context.Context, id string) (*model.Run, error) {
	var r model.Run
	err := db.Pool.QueryRow(ctx,
		`SELECT id, test_id, status, started_at, completed_at, error_message FROM runs WHERE id = $1`, id,
	).Scan(&r.ID, &r.TestID, &r.Status, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &r, err
}

func (db *DB) UpdateRunStatus(ctx context.Context, id string, status model.RunStatus, errMsg string) error {
	var startedAtExpr, completedAtExpr string
	switch status {
	case model.RunRunning:
		startedAtExpr = `, started_at = COALESCE(started_at, now())`
	case model.RunCompleted, model.RunFailed, model.RunStopped:
		completedAtExpr = `, completed_at = now()`
	}
	tag, err := db.Pool.Exec(ctx,
		`UPDATE runs SET status = $1, error_message = $2`+startedAtExpr+completedAtExpr+` WHERE id = $3`,
		status, errMsg, id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (db *DB) AppendMetricSnapshot(ctx context.Context, s *model.RunMetricSnapshot) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO run_metric_snapshots (run_id, ts, elapsed_seconds, throughput_rps, avg_response_time_ms, error_rate_pct, sample_count)
		 VALUES ($1, now(), $2, $3, $4, $5, $6) RETURNING id, ts`,
		s.RunID, s.ElapsedSeconds, s.ThroughputRPS, s.AvgResponseTimeMs, s.ErrorRatePct, s.SampleCount,
	).Scan(&s.ID, &s.Ts)
}

func (db *DB) LatestSnapshot(ctx context.Context, runID string) (*model.RunMetricSnapshot, error) {
	var s model.RunMetricSnapshot
	err := db.Pool.QueryRow(ctx,
		`SELECT id, run_id, ts, elapsed_seconds, throughput_rps, avg_response_time_ms, error_rate_pct, sample_count
		 FROM run_metric_snapshots WHERE run_id = $1 ORDER BY ts DESC LIMIT 1`, runID,
	).Scan(&s.ID, &s.RunID, &s.Ts, &s.ElapsedSeconds, &s.ThroughputRPS, &s.AvgResponseTimeMs, &s.ErrorRatePct, &s.SampleCount)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &s, err
}

func (db *DB) ListSnapshots(ctx context.Context, runID string) ([]model.RunMetricSnapshot, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, run_id, ts, elapsed_seconds, throughput_rps, avg_response_time_ms, error_rate_pct, sample_count
		 FROM run_metric_snapshots WHERE run_id = $1 ORDER BY ts ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RunMetricSnapshot
	for rows.Next() {
		var s model.RunMetricSnapshot
		if err := rows.Scan(&s.ID, &s.RunID, &s.Ts, &s.ElapsedSeconds, &s.ThroughputRPS, &s.AvgResponseTimeMs, &s.ErrorRatePct, &s.SampleCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
```

Add imports `"github.com/jackc/pgx/v5"`, `"github.com/boltrunner/backend/internal/model"`, and `"github.com/boltrunner/backend/internal/store"` to `postgres.go`.

- [ ] **Step 4: Run test to verify it passes (or skips cleanly)**

Run: `cd backend && go test ./internal/store/postgres/...`
Expected: PASS or SKIP.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/postgres
git commit -m "feat(backend): Postgres implementations of TestStore and RunStore"
```

---

### Task 13: Wire main.go to Postgres + real Kubernetes client + watcher; deploy infra

**Files:**
- Modify: `backend/cmd/server/main.go` — real Postgres connection, real client-go config, start watcher
- Create: `deploy/Dockerfile.server`
- Create: `deploy/Dockerfile.sidecar`
- Create: `deploy/Dockerfile.jmeter`
- Create: `deploy/kind-config.yaml`
- Create: `docker-compose.yml`
- Create: `.env.example`

**Interfaces:**
- Consumes: `postgres.Connect`/`Migrate` (Task 2), `postgres.DB` as `store.TestStore`+`store.RunStore` (Task 12), `watcher.New`/`Run` (Task 7), `api.NewServer` (Task 6/8/9).

- [ ] **Step 1: Rewrite `cmd/server/main.go`**

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/boltrunner/backend/internal/api"
	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/store/postgres"
	"github.com/boltrunner/backend/internal/watcher"
)

func buildK8sClient() (kubernetes.Interface, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return kubernetes.NewForConfig(cfg)
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

func main() {
	ctx := context.Background()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	db, err := postgres.Connect(ctx, dsn)
	if err != nil {
		log.Fatalf("connect to postgres: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	k8sClient, err := buildK8sClient()
	if err != nil {
		log.Fatalf("build k8s client: %v", err)
	}

	jobCfg := k8sjob.Config{
		Namespace:    envOr("BOLTRUNNER_NAMESPACE", "boltrunner"),
		JMeterImage:  envOr("JMETER_IMAGE", "boltrunner/jmeter:local"),
		SidecarImage: envOr("SIDECAR_IMAGE", "boltrunner/sidecar:local"),
		BackendURL:   envOr("BACKEND_URL", "http://boltrunner-backend.boltrunner.svc:8080"),
	}

	w := watcher.New(k8sClient, db, jobCfg.Namespace, 30*time.Second)
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go w.Run(watchCtx, 2*time.Second)

	s := api.NewServer(db, db, k8sClient, jobCfg)
	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", s.Router()); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 2: Verify it builds**

Run: `cd backend && go build ./...`
Expected: no errors (note: `db` implements both `store.TestStore` and `store.RunStore` from Task 12, satisfying `api.NewServer`'s first two parameters).

- [ ] **Step 3: Write `deploy/Dockerfile.server`**

```dockerfile
FROM golang:1.22 AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
ENTRYPOINT ["/server"]
```

- [ ] **Step 4: Write `deploy/Dockerfile.sidecar`**

```dockerfile
FROM golang:1.22 AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /out/sidecar ./cmd/sidecar

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/sidecar /sidecar
ENTRYPOINT ["/sidecar"]
```

- [ ] **Step 5: Write `deploy/Dockerfile.jmeter`**

```dockerfile
FROM eclipse-temurin:17-jre-jammy
ARG JMETER_VERSION=5.6.3
RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && curl -fsSL https://dlcdn.apache.org//jmeter/binaries/apache-jmeter-${JMETER_VERSION}.tgz -o /tmp/jmeter.tgz \
    && tar -xzf /tmp/jmeter.tgz -C /opt \
    && rm /tmp/jmeter.tgz \
    && apt-get purge -y curl && rm -rf /var/lib/apt/lists/*
ENV PATH="/opt/apache-jmeter-${JMETER_VERSION}/bin:${PATH}"
ENTRYPOINT ["jmeter"]
```

- [ ] **Step 6: Write `deploy/kind-config.yaml`**

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
```

- [ ] **Step 7: Write `docker-compose.yml`** (Postgres for local dev)

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: boltrunner
      POSTGRES_PASSWORD: boltrunner
      POSTGRES_DB: boltrunner
    ports:
      - "5432:5432"
    volumes:
      - boltrunner-postgres-data:/var/lib/postgresql/data

volumes:
  boltrunner-postgres-data:
```

- [ ] **Step 8: Write `.env.example`**

```
DATABASE_URL=postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable
BOLTRUNNER_NAMESPACE=boltrunner
JMETER_IMAGE=boltrunner/jmeter:local
SIDECAR_IMAGE=boltrunner/sidecar:local
BACKEND_URL=http://boltrunner-backend.boltrunner.svc:8080
```

- [ ] **Step 9: Commit**

```bash
git add backend/cmd/server/main.go deploy docker-compose.yml .env.example
git commit -m "feat(backend): wire real Postgres + K8s client + watcher; add deploy infra"
```

---

### Task 14: Frontend scaffold + API client + Create Test form + Test list

**Files:**
- Create: `frontend/package.json`, `frontend/tsconfig.json`, `frontend/next.config.ts`, `frontend/tailwind.config.ts`, `frontend/vitest.config.ts`
- Create: `frontend/lib/api-client.ts`
- Create: `frontend/app/layout.tsx`, `frontend/app/page.tsx`
- Create: `frontend/components/CreateTestForm.tsx`, `frontend/components/TestList.tsx`
- Test: `frontend/__tests__/CreateTestForm.test.tsx`

**Interfaces:**
- Consumes: backend JSON shapes from Tasks 3, 6, 8 (`Test`, `Run`, `GetRunResponse`).
- Produces: `lib/api-client.ts` exports `listTests()`, `createTest(input: CreateTestInput)`, `startRun(testId: string)`, `getRun(runId: string)`, `cancelRun(runId: string)` — all consumed by Task 15's live run view.
- Produces: `<CreateTestForm onCreated={(test: Test) => void} />`, `<TestList tests={Test[]} onStart={(testId: string) => void} />`.

- [ ] **Step 1: Scaffold the Next.js app**

```bash
cd frontend
npx create-next-app@14 . --typescript --tailwind --app --no-eslint --no-src-dir --import-alias "@/*" --use-npm
npm install --save-dev vitest @vitejs/plugin-react jsdom @testing-library/react @testing-library/jest-dom @testing-library/user-event
npm install --save-dev @playwright/test
```

- [ ] **Step 2: Configure Vitest** — `frontend/vitest.config.ts`

```typescript
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
  },
  resolve: {
    alias: { '@': path.resolve(__dirname, '.') },
  },
});
```

`frontend/vitest.setup.ts`:

```typescript
import '@testing-library/jest-dom/vitest';
```

Add to `frontend/package.json` scripts: `"test": "vitest run"`, `"test:watch": "vitest"`.

- [ ] **Step 3: Write the API client types and functions** — `frontend/lib/api-client.ts`

```typescript
const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080';

export type Test = {
  id: string;
  name: string;
  target_url: string;
  virtual_users: number;
  duration_seconds: number;
  created_at: string;
};

export type RunStatus = 'pending' | 'running' | 'completed' | 'failed' | 'stopped';

export type Run = {
  id: string;
  test_id: string;
  status: RunStatus;
  started_at?: string;
  completed_at?: string;
  error_message?: string;
};

export type RunMetricSnapshot = {
  id: string;
  run_id: string;
  ts: string;
  elapsed_seconds: number;
  throughput_rps: number;
  avg_response_time_ms: number;
  error_rate_pct: number;
  sample_count: number;
};

export type GetRunResponse = {
  run: Run;
  latest?: RunMetricSnapshot;
  history: RunMetricSnapshot[];
};

export type CreateTestInput = {
  name: string;
  target_url: string;
  virtual_users: number;
  duration_seconds: number;
};

async function unwrap<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`request failed (${res.status}): ${text}`);
  }
  return res.json() as Promise<T>;
}

export async function listTests(): Promise<Test[]> {
  return unwrap(await fetch(`${API_URL}/api/tests`, { cache: 'no-store' }));
}

export async function createTest(input: CreateTestInput): Promise<Test> {
  return unwrap(
    await fetch(`${API_URL}/api/tests`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
  );
}

export async function startRun(testId: string): Promise<Run> {
  return unwrap(await fetch(`${API_URL}/api/tests/${testId}/runs`, { method: 'POST' }));
}

export async function getRun(runId: string): Promise<GetRunResponse> {
  return unwrap(await fetch(`${API_URL}/api/runs/${runId}`, { cache: 'no-store' }));
}

export async function cancelRun(runId: string): Promise<void> {
  const res = await fetch(`${API_URL}/api/runs/${runId}/cancel`, { method: 'POST' });
  if (!res.ok && res.status !== 204) {
    throw new Error(`cancel failed (${res.status})`);
  }
}
```

- [ ] **Step 4: Write the failing component test** — `frontend/__tests__/CreateTestForm.test.tsx`

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { CreateTestForm } from '@/components/CreateTestForm';
import * as api from '@/lib/api-client';

describe('CreateTestForm', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('submits the form and calls onCreated with the new test', async () => {
    const created = {
      id: 't1', name: 'Smoke', target_url: 'http://example.com',
      virtual_users: 10, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z',
    };
    vi.spyOn(api, 'createTest').mockResolvedValue(created);
    const onCreated = vi.fn();

    render(<CreateTestForm onCreated={onCreated} />);

    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Smoke' } });
    fireEvent.change(screen.getByLabelText(/target url/i), { target: { value: 'http://example.com' } });
    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '10' } });
    fireEvent.change(screen.getByLabelText(/duration/i), { target: { value: '30' } });
    fireEvent.click(screen.getByRole('button', { name: /create test/i }));

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(created));
    expect(api.createTest).toHaveBeenCalledWith({
      name: 'Smoke', target_url: 'http://example.com', virtual_users: 10, duration_seconds: 30,
    });
  });
});
```

- [ ] **Step 5: Run test to verify it fails**

Run: `cd frontend && npm test`
Expected: FAIL — `components/CreateTestForm` module not found.

- [ ] **Step 6: Write minimal implementation** — `frontend/components/CreateTestForm.tsx`

```tsx
'use client';

import { useState, FormEvent } from 'react';
import { createTest, Test } from '@/lib/api-client';

export function CreateTestForm({ onCreated }: { onCreated: (test: Test) => void }) {
  const [name, setName] = useState('');
  const [targetUrl, setTargetUrl] = useState('');
  const [virtualUsers, setVirtualUsers] = useState('10');
  const [durationSeconds, setDurationSeconds] = useState('30');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const test = await createTest({
        name,
        target_url: targetUrl,
        virtual_users: Number(virtualUsers),
        duration_seconds: Number(durationSeconds),
      });
      onCreated(test);
      setName('');
      setTargetUrl('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to create test');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-3 max-w-md">
      <label className="flex flex-col gap-1">
        <span>Name</span>
        <input value={name} onChange={(e) => setName(e.target.value)} required />
      </label>
      <label className="flex flex-col gap-1">
        <span>Target URL</span>
        <input value={targetUrl} onChange={(e) => setTargetUrl(e.target.value)} required type="url" />
      </label>
      <label className="flex flex-col gap-1">
        <span>Virtual users</span>
        <input value={virtualUsers} onChange={(e) => setVirtualUsers(e.target.value)} required type="number" min={1} />
      </label>
      <label className="flex flex-col gap-1">
        <span>Duration (seconds)</span>
        <input value={durationSeconds} onChange={(e) => setDurationSeconds(e.target.value)} required type="number" min={1} />
      </label>
      {error && <p className="text-red-600">{error}</p>}
      <button type="submit" disabled={submitting}>
        {submitting ? 'Creating…' : 'Create test'}
      </button>
    </form>
  );
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `cd frontend && npm test`
Expected: PASS

- [ ] **Step 8: Write `TestList` (no test — trivial presentational component wired up in Task 15's integration)** — `frontend/components/TestList.tsx`

```tsx
'use client';

import { Test } from '@/lib/api-client';

export function TestList({ tests, onStart }: { tests: Test[]; onStart: (testId: string) => void }) {
  if (tests.length === 0) {
    return <p>No tests yet — create one above.</p>;
  }
  return (
    <table>
      <thead>
        <tr>
          <th>Name</th>
          <th>Target URL</th>
          <th>Virtual users</th>
          <th>Duration (s)</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {tests.map((t) => (
          <tr key={t.id}>
            <td>{t.name}</td>
            <td>{t.target_url}</td>
            <td>{t.virtual_users}</td>
            <td>{t.duration_seconds}</td>
            <td>
              <button onClick={() => onStart(t.id)}>Run</button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
```

- [ ] **Step 9: Write the dashboard page** — `frontend/app/page.tsx`

```tsx
'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { listTests, startRun, Test } from '@/lib/api-client';
import { CreateTestForm } from '@/components/CreateTestForm';
import { TestList } from '@/components/TestList';

export default function DashboardPage() {
  const [tests, setTests] = useState<Test[]>([]);
  const router = useRouter();

  useEffect(() => {
    listTests().then(setTests).catch(() => setTests([]));
  }, []);

  async function handleStart(testId: string) {
    const run = await startRun(testId);
    router.push(`/runs/${run.id}`);
  }

  return (
    <main className="p-8 flex flex-col gap-8">
      <h1 className="text-2xl font-semibold">BoltRunner</h1>
      <CreateTestForm onCreated={(t) => setTests((prev) => [t, ...prev])} />
      <TestList tests={tests} onStart={handleStart} />
    </main>
  );
}
```

- [ ] **Step 10: Run the full frontend test suite and the build**

Run: `cd frontend && npm test && npm run build`
Expected: PASS / build succeeds.

- [ ] **Step 11: Commit**

```bash
git add frontend
git commit -m "feat(frontend): scaffold Next.js app, API client, create-test form, test list"
```

---

### Task 15: Live run view — polling hook, metrics cards, chart

**Files:**
- Create: `frontend/hooks/useRunPolling.ts`
- Create: `frontend/components/LiveMetrics.tsx`
- Create: `frontend/components/MetricsChart.tsx`
- Create: `frontend/app/runs/[id]/page.tsx`
- Test: `frontend/__tests__/useRunPolling.test.tsx`
- Test: `frontend/__tests__/LiveMetrics.test.tsx`

**Interfaces:**
- Consumes: `getRun`, `cancelRun`, `GetRunResponse`, `Run`, `RunMetricSnapshot` (Task 14).
- Produces: `useRunPolling(runId: string, intervalMs?: number): { data: GetRunResponse | null; error: string | null }` — polls while `data.run.status` is `pending`/`running`, stops otherwise.
- Produces: `<LiveMetrics run={Run} latest={RunMetricSnapshot | undefined} onCancel={() => void} />`, `<MetricsChart history={RunMetricSnapshot[]} />`.

- [ ] **Step 1: Install the charting dependency**

```bash
cd frontend && npm install recharts
```

- [ ] **Step 2: Write the failing hook test** — `frontend/__tests__/useRunPolling.test.tsx`

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useRunPolling } from '@/hooks/useRunPolling';
import * as api from '@/lib/api-client';

describe('useRunPolling', () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(() => vi.useRealTimers());

  it('polls while running and stops once completed', async () => {
    const running = { run: { id: 'r1', test_id: 't1', status: 'running' as const }, history: [] };
    const completed = { run: { id: 'r1', test_id: 't1', status: 'completed' as const }, history: [] };

    const getRun = vi.spyOn(api, 'getRun')
      .mockResolvedValueOnce(running)
      .mockResolvedValueOnce(completed);

    const { result } = renderHook(() => useRunPolling('r1', 5));

    await waitFor(() => expect(result.current.data?.run.status).toBe('running'));
    await waitFor(() => expect(result.current.data?.run.status).toBe('completed'));

    const callsAtCompletion = getRun.mock.calls.length;
    await new Promise((r) => setTimeout(r, 30));
    expect(getRun.mock.calls.length).toBe(callsAtCompletion);
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd frontend && npm test`
Expected: FAIL — `hooks/useRunPolling` module not found.

- [ ] **Step 4: Write minimal implementation** — `frontend/hooks/useRunPolling.ts`

```typescript
'use client';

import { useEffect, useRef, useState } from 'react';
import { getRun, GetRunResponse } from '@/lib/api-client';

const TERMINAL_STATUSES = new Set(['completed', 'failed', 'stopped']);

export function useRunPolling(runId: string, intervalMs = 1500) {
  const [data, setData] = useState<GetRunResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const stopped = useRef(false);

  useEffect(() => {
    stopped.current = false;

    async function poll() {
      if (stopped.current) return;
      try {
        const resp = await getRun(runId);
        if (stopped.current) return;
        setData(resp);
        if (!TERMINAL_STATUSES.has(resp.run.status)) {
          setTimeout(poll, intervalMs);
        }
      } catch (err) {
        if (!stopped.current) {
          setError(err instanceof Error ? err.message : 'failed to fetch run');
        }
      }
    }

    poll();
    return () => {
      stopped.current = true;
    };
  }, [runId, intervalMs]);

  return { data, error };
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && npm test`
Expected: PASS

- [ ] **Step 6: Write the failing `LiveMetrics` test** — `frontend/__tests__/LiveMetrics.test.tsx`

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { LiveMetrics } from '@/components/LiveMetrics';

describe('LiveMetrics', () => {
  it('renders status and latest metrics, and calls onCancel', () => {
    const onCancel = vi.fn();
    render(
      <LiveMetrics
        run={{ id: 'r1', test_id: 't1', status: 'running' }}
        latest={{
          id: 's1', run_id: 'r1', ts: '2026-07-24T00:00:01Z', elapsed_seconds: 1,
          throughput_rps: 12.5, avg_response_time_ms: 210, error_rate_pct: 0, sample_count: 12,
        }}
        onCancel={onCancel}
      />
    );

    expect(screen.getByText(/running/i)).toBeInTheDocument();
    expect(screen.getByText(/12.5/)).toBeInTheDocument();
    expect(screen.getByText(/210/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(onCancel).toHaveBeenCalled();
  });

  it('hides the cancel button once the run is terminal', () => {
    render(<LiveMetrics run={{ id: 'r1', test_id: 't1', status: 'completed' }} onCancel={vi.fn()} />);
    expect(screen.queryByRole('button', { name: /cancel/i })).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 7: Run test to verify it fails**

Run: `cd frontend && npm test`
Expected: FAIL — `components/LiveMetrics` module not found.

- [ ] **Step 8: Write minimal implementation** — `frontend/components/LiveMetrics.tsx`

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
        <h2 className="text-xl">Status: {run.status}</h2>
        {ACTIVE_STATUSES.has(run.status) && <button onClick={onCancel}>Cancel</button>}
      </div>
      {run.error_message && <p className="text-red-600">{run.error_message}</p>}
      <div className="grid grid-cols-4 gap-4">
        <div>
          <div className="text-sm text-gray-500">Throughput (req/s)</div>
          <div className="text-2xl">{latest ? latest.throughput_rps.toFixed(1) : '—'}</div>
        </div>
        <div>
          <div className="text-sm text-gray-500">Avg response time (ms)</div>
          <div className="text-2xl">{latest ? latest.avg_response_time_ms.toFixed(0) : '—'}</div>
        </div>
        <div>
          <div className="text-sm text-gray-500">Error rate (%)</div>
          <div className="text-2xl">{latest ? latest.error_rate_pct.toFixed(1) : '—'}</div>
        </div>
        <div>
          <div className="text-sm text-gray-500">Elapsed (s)</div>
          <div className="text-2xl">{latest ? latest.elapsed_seconds : '—'}</div>
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 9: Run test to verify it passes**

Run: `cd frontend && npm test`
Expected: PASS

- [ ] **Step 10: Write `MetricsChart` (no dedicated test — thin wrapper over `recharts`, exercised by the e2e test in Task 16)** — `frontend/components/MetricsChart.tsx`

```tsx
'use client';

import { LineChart, Line, XAxis, YAxis, Tooltip, CartesianGrid, ResponsiveContainer } from 'recharts';
import { RunMetricSnapshot } from '@/lib/api-client';

export function MetricsChart({ history }: { history: RunMetricSnapshot[] }) {
  if (history.length === 0) {
    return <p>Waiting for the first metric snapshot…</p>;
  }
  return (
    <div style={{ width: '100%', height: 300 }}>
      <ResponsiveContainer>
        <LineChart data={history}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="elapsed_seconds" label={{ value: 'seconds', position: 'insideBottom', offset: -5 }} />
          <YAxis />
          <Tooltip />
          <Line type="monotone" dataKey="avg_response_time_ms" name="Avg response time (ms)" stroke="#209dd7" isAnimationActive={false} />
          <Line type="monotone" dataKey="throughput_rps" name="Throughput (req/s)" stroke="#753991" isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
```

- [ ] **Step 11: Write the run page** — `frontend/app/runs/[id]/page.tsx`

```tsx
'use client';

import { useParams } from 'next/navigation';
import { useRunPolling } from '@/hooks/useRunPolling';
import { cancelRun } from '@/lib/api-client';
import { LiveMetrics } from '@/components/LiveMetrics';
import { MetricsChart } from '@/components/MetricsChart';

export default function RunPage() {
  const params = useParams<{ id: string }>();
  const { data, error } = useRunPolling(params.id);

  if (error) return <p className="text-red-600 p-8">{error}</p>;
  if (!data) return <p className="p-8">Loading…</p>;

  return (
    <main className="p-8 flex flex-col gap-8">
      <LiveMetrics run={data.run} latest={data.latest} onCancel={() => cancelRun(params.id)} />
      <MetricsChart history={data.history} />
    </main>
  );
}
```

- [ ] **Step 12: Run the full frontend test suite and the build**

Run: `cd frontend && npm test && npm run build`
Expected: PASS / build succeeds.

- [ ] **Step 13: Commit**

```bash
git add frontend
git commit -m "feat(frontend): live run view with polling, metrics cards, and chart"
```

---

### Task 16: In-cluster deploy manifests + RBAC + dev-up script

For the sidecar's metrics POSTs to reach the backend, and for the backend to reach Postgres, the simplest portable local setup runs Postgres and the backend **inside** the `kind` cluster (not on the host) — this avoids Linux-vs-Docker-Desktop `host.docker.internal` differences entirely and matches the target production shape (`BACKEND_URL` already defaults to the in-cluster DNS name `http://boltrunner-backend.boltrunner.svc:8080`, set in Task 13). The frontend still runs on the host, reaching the backend through `kubectl port-forward`.

**Files:**
- Create: `deploy/k8s/namespace.yaml`
- Create: `deploy/k8s/rbac.yaml`
- Create: `deploy/k8s/postgres.yaml`
- Create: `deploy/k8s/backend.yaml`
- Create: `deploy/dev-up.sh`
- Create: `deploy/dev-down.sh`

**Interfaces:**
- Consumes: `deploy/Dockerfile.server`, `deploy/Dockerfile.sidecar`, `deploy/Dockerfile.jmeter`, `deploy/kind-config.yaml` (Task 13).
- Produces: a running `boltrunner` namespace in `kind` with Postgres + backend reachable at `localhost:8080` via port-forward — the target Task 17's e2e test runs against.

- [ ] **Step 1: Write `deploy/k8s/namespace.yaml`**

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: boltrunner
```

- [ ] **Step 2: Write `deploy/k8s/rbac.yaml`** — grants the backend permission to manage Jobs/ConfigMaps in its own namespace only

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: boltrunner-backend
  namespace: boltrunner
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: boltrunner-backend
  namespace: boltrunner
rules:
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["create", "get", "list", "watch", "delete"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["create", "get", "list", "watch", "delete"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: boltrunner-backend
  namespace: boltrunner
subjects:
  - kind: ServiceAccount
    name: boltrunner-backend
    namespace: boltrunner
roleRef:
  kind: Role
  name: boltrunner-backend
  apiGroup: rbac.authorization.k8s.io
```

- [ ] **Step 3: Write `deploy/k8s/postgres.yaml`** (single pod, `emptyDir` — disposable dev data, not for production)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: boltrunner-postgres
  namespace: boltrunner
spec:
  replicas: 1
  selector:
    matchLabels: { app: boltrunner-postgres }
  template:
    metadata:
      labels: { app: boltrunner-postgres }
    spec:
      containers:
        - name: postgres
          image: postgres:16
          env:
            - { name: POSTGRES_USER, value: boltrunner }
            - { name: POSTGRES_PASSWORD, value: boltrunner }
            - { name: POSTGRES_DB, value: boltrunner }
          ports:
            - containerPort: 5432
          volumeMounts:
            - { name: data, mountPath: /var/lib/postgresql/data }
      volumes:
        - name: data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: boltrunner-postgres
  namespace: boltrunner
spec:
  selector: { app: boltrunner-postgres }
  ports:
    - { port: 5432, targetPort: 5432 }
```

- [ ] **Step 4: Write `deploy/k8s/backend.yaml`**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: boltrunner-backend
  namespace: boltrunner
spec:
  replicas: 1
  selector:
    matchLabels: { app: boltrunner-backend }
  template:
    metadata:
      labels: { app: boltrunner-backend }
    spec:
      serviceAccountName: boltrunner-backend
      containers:
        - name: backend
          image: boltrunner/server:local
          imagePullPolicy: IfNotPresent
          env:
            - { name: DATABASE_URL, value: "postgres://boltrunner:boltrunner@boltrunner-postgres:5432/boltrunner?sslmode=disable" }
            - { name: BOLTRUNNER_NAMESPACE, value: boltrunner }
            - { name: JMETER_IMAGE, value: "boltrunner/jmeter:local" }
            - { name: SIDECAR_IMAGE, value: "boltrunner/sidecar:local" }
            - { name: BACKEND_URL, value: "http://boltrunner-backend.boltrunner.svc:8080" }
          ports:
            - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: boltrunner-backend
  namespace: boltrunner
spec:
  selector: { app: boltrunner-backend }
  ports:
    - { port: 8080, targetPort: 8080 }
```

- [ ] **Step 5: Write `deploy/dev-up.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER_NAME="boltrunner"

if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
  kind create cluster --name "${CLUSTER_NAME}" --config deploy/kind-config.yaml
fi

docker build -f deploy/Dockerfile.server -t boltrunner/server:local .
docker build -f deploy/Dockerfile.sidecar -t boltrunner/sidecar:local .
docker build -f deploy/Dockerfile.jmeter -t boltrunner/jmeter:local .

kind load docker-image boltrunner/server:local --name "${CLUSTER_NAME}"
kind load docker-image boltrunner/sidecar:local --name "${CLUSTER_NAME}"
kind load docker-image boltrunner/jmeter:local --name "${CLUSTER_NAME}"

kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/rbac.yaml
kubectl apply -f deploy/k8s/postgres.yaml
kubectl apply -f deploy/k8s/backend.yaml

kubectl -n boltrunner rollout status deployment/boltrunner-postgres --timeout=120s
kubectl -n boltrunner rollout status deployment/boltrunner-backend --timeout=120s

echo "Port-forwarding boltrunner-backend to localhost:8080 (Ctrl+C to stop, or run deploy/dev-down.sh)"
kubectl -n boltrunner port-forward svc/boltrunner-backend 8080:8080
```

- [ ] **Step 6: Write `deploy/dev-down.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
kind delete cluster --name boltrunner
```

- [ ] **Step 7: Make both scripts executable**

```bash
chmod +x deploy/dev-up.sh deploy/dev-down.sh
```

- [ ] **Step 8: Commit**

```bash
git add deploy
git commit -m "feat(deploy): in-cluster K8s manifests, RBAC, and dev-up/down scripts"
```

---

### Task 17: Playwright e2e test (full flow against `kind`)

**Prerequisites (run once, in a separate terminal, before this task's test runs):**

```bash
deploy/dev-up.sh   # leaves a port-forward running in that terminal
cd frontend && NEXT_PUBLIC_API_URL=http://localhost:8080 npm run dev
```

**Files:**
- Create: `frontend/playwright.config.ts`
- Create: `frontend/e2e/walking-skeleton.spec.ts`

**Interfaces:**
- Consumes: the running dashboard (Task 14's `app/page.tsx`) and run page (Task 15's `app/runs/[id]/page.tsx`) via a real browser, against the real backend + `kind` stack from Task 16.

- [ ] **Step 1: Write `frontend/playwright.config.ts`**

```typescript
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  timeout: 120_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:3000',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
```

Add to `frontend/package.json` scripts: `"test:e2e": "playwright test"`.

- [ ] **Step 2: Write the e2e test** — `frontend/e2e/walking-skeleton.spec.ts`

```typescript
import { test, expect } from '@playwright/test';

test('create a test, run it, watch live metrics, see completion', async ({ page }) => {
  await page.goto('/');

  await page.getByLabel(/name/i).fill('E2E Smoke Test');
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('3');
  await page.getByLabel(/duration/i).fill('20');
  await page.getByRole('button', { name: /create test/i }).click();

  await expect(page.getByText('E2E Smoke Test')).toBeVisible();

  await page.getByRole('button', { name: /run/i }).click();

  await expect(page).toHaveURL(/\/runs\/.+/);
  await expect(page.getByText(/status: (pending|running)/i)).toBeVisible({ timeout: 15_000 });

  // Live metrics should start populating within a few seconds of the run starting.
  await expect(page.getByText(/throughput/i)).toBeVisible();

  // Run duration is 20s; allow generous headroom for pod scheduling + JMeter startup.
  await expect(page.getByText(/status: completed/i)).toBeVisible({ timeout: 90_000 });
});

test('cancel a running test stops it', async ({ page }) => {
  await page.goto('/');

  await page.getByLabel(/name/i).fill('E2E Cancel Test');
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('3');
  await page.getByLabel(/duration/i).fill('60');
  await page.getByRole('button', { name: /create test/i }).click();
  await page.getByRole('button', { name: /run/i }).click();

  await expect(page).toHaveURL(/\/runs\/.+/);
  await expect(page.getByText(/status: (pending|running)/i)).toBeVisible({ timeout: 15_000 });

  await page.getByRole('button', { name: /cancel/i }).click();

  await expect(page.getByText(/status: stopped/i)).toBeVisible({ timeout: 15_000 });
});
```

- [ ] **Step 3: Run it against the real stack**

Run (with the prerequisites above already running in other terminals):
```bash
cd frontend && npx playwright install --with-deps chromium
npm run test:e2e
```
Expected: both tests PASS. (This is the first point in the plan where the Playwright plugin, once `/reload-plugins` has run, can drive this interactively instead of via the CLI — useful for debugging a failure by watching the browser.)

- [ ] **Step 4: Commit**

```bash
git add frontend/playwright.config.ts frontend/e2e frontend/package.json
git commit -m "test(e2e): full create-run-watch-complete flow against kind"
```

---

### Task 18: Go integration test (build-tagged) + CI workflow + README

**Files:**
- Create: `backend/internal/integration/walking_skeleton_test.go`
- Create: `.github/workflows/ci.yml`
- Create: `README.md`

**Interfaces:**
- Consumes: the live backend's public API (Tasks 6, 8) against a real `kind` cluster (Task 16) — this is the spec's required "Integration test (CI)" (spin up `kind`, run one full create → start → poll → complete cycle, assert non-zero metrics and `status=completed`).

- [ ] **Step 1: Write the integration test** — `backend/internal/integration/walking_skeleton_test.go`

```go
//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func baseURL() string {
	if v := os.Getenv("BOLTRUNNER_API_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func TestWalkingSkeletonEndToEnd(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}

	createBody, _ := json.Marshal(map[string]any{
		"name": "ci-integration-smoke", "target_url": baseURL() + "/healthz",
		"virtual_users": 3, "duration_seconds": 15,
	})
	resp, err := client.Post(baseURL()+"/api/tests", "application/json", bytes.NewReader(createBody))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create test failed: err=%v status=%v", err, resp)
	}
	var test struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&test)
	resp.Body.Close()

	runResp, err := client.Post(baseURL()+"/api/tests/"+test.ID+"/runs", "application/json", nil)
	if err != nil || runResp.StatusCode != http.StatusCreated {
		t.Fatalf("start run failed: err=%v status=%v", err, runResp)
	}
	var run struct {
		ID string `json:"id"`
	}
	json.NewDecoder(runResp.Body).Decode(&run)
	runResp.Body.Close()

	deadline := time.Now().Add(90 * time.Second)
	var finalStatus string
	var sawMetrics bool
	for time.Now().Before(deadline) {
		getResp, err := client.Get(baseURL() + "/api/runs/" + run.ID)
		if err != nil {
			t.Fatalf("get run failed: %v", err)
		}
		var body struct {
			Run struct {
				Status string `json:"status"`
			} `json:"run"`
			Latest *struct {
				SampleCount int `json:"sample_count"`
			} `json:"latest"`
		}
		json.NewDecoder(getResp.Body).Decode(&body)
		getResp.Body.Close()

		if body.Latest != nil && body.Latest.SampleCount > 0 {
			sawMetrics = true
		}
		if body.Run.Status == "completed" || body.Run.Status == "failed" {
			finalStatus = body.Run.Status
			break
		}
		time.Sleep(2 * time.Second)
	}

	if finalStatus != "completed" {
		t.Fatalf("expected run to complete, got status=%q", finalStatus)
	}
	if !sawMetrics {
		t.Fatal("expected at least one non-zero metric snapshot during the run")
	}
}
```

- [ ] **Step 2: Write the CI workflow** — `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  backend-unit:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_USER: boltrunner
          POSTGRES_PASSWORD: boltrunner
          POSTGRES_DB: boltrunner
        ports: ["5432:5432"]
        options: >-
          --health-cmd pg_isready --health-interval 5s --health-timeout 5s --health-retries 10
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - name: Test
        working-directory: backend
        env:
          BOLTRUNNER_TEST_DSN: "postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable"
        run: go test ./...
      - name: Build
        working-directory: backend
        run: go build ./...

  frontend-unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: "20" }
      - working-directory: frontend
        run: npm ci
      - working-directory: frontend
        run: npm test
      - working-directory: frontend
        run: npm run build

  integration-kind:
    runs-on: ubuntu-latest
    needs: [backend-unit, frontend-unit]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - uses: helm/kind-action@v1
        with: { cluster_name: boltrunner }
      - name: Build and load images
        run: |
          docker build -f deploy/Dockerfile.server -t boltrunner/server:local .
          docker build -f deploy/Dockerfile.sidecar -t boltrunner/sidecar:local .
          docker build -f deploy/Dockerfile.jmeter -t boltrunner/jmeter:local .
          kind load docker-image boltrunner/server:local --name boltrunner
          kind load docker-image boltrunner/sidecar:local --name boltrunner
          kind load docker-image boltrunner/jmeter:local --name boltrunner
      - name: Deploy
        run: |
          kubectl apply -f deploy/k8s/namespace.yaml
          kubectl apply -f deploy/k8s/rbac.yaml
          kubectl apply -f deploy/k8s/postgres.yaml
          kubectl apply -f deploy/k8s/backend.yaml
          kubectl -n boltrunner rollout status deployment/boltrunner-postgres --timeout=120s
          kubectl -n boltrunner rollout status deployment/boltrunner-backend --timeout=120s
      - name: Port-forward backend
        run: kubectl -n boltrunner port-forward svc/boltrunner-backend 8080:8080 &
      - name: Wait for backend
        run: |
          for i in $(seq 1 30); do curl -sf http://localhost:8080/healthz && break; sleep 2; done
      - name: Run integration test
        working-directory: backend
        run: go test -tags=integration ./internal/integration/...
```

- [ ] **Step 3: Write `README.md`**

```markdown
# BoltRunner

An open-source, Kubernetes-native alternative to LoadRunner Enterprise. See `Implementation/`
for the full long-range platform vision, and `docs/superpowers/specs/` for the design of the
current increment.

## Current increment: walking skeleton

Create a test, run it as a real JMeter pod on a local Kubernetes cluster, watch live metrics,
see a final summary. No auth, one engine (JMeter), one fixed test-plan template — see
`docs/superpowers/specs/2026-07-24-walking-skeleton-design.md` for the full design and
explicitly out-of-scope list.

## Local development

Prerequisites: Go 1.22+, Node 20+, Docker, `kind`, `kubectl`.

```bash
# 1. Bring up Postgres + backend inside a local kind cluster (leaves a port-forward running):
deploy/dev-up.sh

# 2. In another terminal, run the frontend against it:
cd frontend
NEXT_PUBLIC_API_URL=http://localhost:8080 npm run dev
# open http://localhost:3000
```

Tear down: `deploy/dev-down.sh`.

## Tests

```bash
cd backend && go test ./...                          # unit tests
cd frontend && npm test                               # component tests
cd frontend && npm run test:e2e                        # e2e (needs the stack from dev-up.sh running)
go test -tags=integration ./backend/internal/integration/...   # needs a live backend (see CI workflow)
```
```

- [ ] **Step 4: Commit**

```bash
git add backend/internal/integration .github/workflows/ci.yml README.md
git commit -m "test(ci): kind-based integration test and CI workflow; add README"
```

---

## Plan Self-Review

**Spec coverage:** Every section of `docs/superpowers/specs/2026-07-24-walking-skeleton-design.md`
maps to a task — Architecture/Components → Tasks 1, 5, 11, 13; Data model → Task 2; API → Tasks
3, 6, 8, 9; Data flow steps 1-7 → Tasks 3, 6, 7, 8; Error handling (unschedulable timeout, failed
exit, sidecar retry/watcher independence, cancel) → Tasks 7, 9, 11 (retry is in the sidecar's
`postSnapshot` — noted as a future hardening step below); Testing section → Tasks 1-15 (TDD unit
tests throughout), Task 18 (kind integration test), Task 17 (Playwright e2e); Explicitly-out-of-scope
list is respected — no task adds auth, multi-engine, scheduling, SLA, WebSockets, or AI features.

**Known gap carried forward (not a placeholder — a scoping call):** the spec says the sidecar
"retries with backoff" on a failed POST; Task 11's `postSnapshot` currently logs and drops on
failure rather than retrying, relying on the watcher (Task 7) for correctness as designed. Add a
follow-up task before considering this increment fully done: wrap `postSnapshot` in a small retry
loop (2-3 attempts, short backoff) in `cmd/sidecar/main.go`. This doesn't block the walking
skeleton from working end-to-end — the watcher already guarantees runs never get stuck — but it
should be closed out as a fast-follow rather than forgotten.

**Type consistency:** `model.RunStatus` values (`pending|running|completed|failed|stopped`) are
used identically in Go (Tasks 2, 6, 7, 9) and TypeScript (Task 14's `RunStatus` union, Task 15's
`ACTIVE_STATUSES`/`TERMINAL_STATUSES`). `api.NewServer`'s signature is introduced in Task 1 and
updated consistently in Tasks 3, 6 (final shape: `testStore, runStore, k8sClient, jobCfg`) — Task
13's `main.go` matches that final shape. Field names in JSON payloads (`throughput_rps`,
`avg_response_time_ms`, `error_rate_pct`, `sample_count`, `elapsed_seconds`) are consistent across
`model.RunMetricSnapshot` (Task 2), the metrics-ingest handler (Task 8), the sidecar's
`postSnapshot` (Task 11), and the frontend's `RunMetricSnapshot` type (Task 14).

**No placeholders found** — every step has complete, runnable code or an exact command.

