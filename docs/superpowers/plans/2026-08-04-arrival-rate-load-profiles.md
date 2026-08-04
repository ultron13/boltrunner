# Arrival-Rate Load Profiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace BoltRunner's closed (concurrency) workload model with an open (arrival-rate) one, where a test declares a schedule of request rates — per `docs/superpowers/specs/2026-08-04-arrival-rate-load-profiles-design.md`.

**Architecture:** A test gains `load_stages`, a JSONB list of `{start_rps, end_rps, duration_seconds}`. The generated JMeter plan gains a jpgc Throughput Shaping Timer with one row per stage. `virtual_users` survives as the thread-pool ceiling; `duration_seconds` stops being an input and is computed server-side as the sum of stage durations.

**Tech Stack:** Go 1.26, chi v5, pgx v5, PostgreSQL 16, JMeter 5.6.3 + jpgc Throughput Shaping Timer. Next.js App Router, React 18, TypeScript `strict`, Vitest + Testing Library, Playwright.

## Global Constraints

- **Coverage gates: 88%.** Backend on the `go tool cover -func` total (currently 90.2%); frontend on lines, statements, functions AND branches (currently 97.7 / 92.52 / 98.27 / 98.94). Neither is to be lowered.
- **Run frontend commands from `frontend/`.** From the repository root `npx vitest` picks up a different vite and fails to parse JSX — the error reads as a syntax error in the test file rather than a wrong-directory mistake.
- **Run backend commands from `backend/`.**
- **Postgres store tests skip silently without `BOLTRUNNER_TEST_DSN`.** A skipped test is not a passing test. Task 2 Step 1 brings a database up; every later backend task reuses it.
- **`sleep` in the foreground is blocked** by the agent bash tool. Use `until ... do :; done` readiness polling.
- **`duration_seconds` is never accepted as input.** It is derived. A request that sends it gets a 400, not a silent ignore — that silent-ignore is the exact trap `testRequest.ProjectID` set on the update path in the previous slice.
- **A stage is `{start_rps, end_rps, duration_seconds}`.** A hold is `start == end`; a ramp is `start != end`. Do not "simplify" to a single target with implicit ramping — the spec rejects that explicitly, and it would stop matching what the timer consumes.
- **Maximum 20 stages**, enforced server-side only.
- **Existing tests backfill to one flat stage at `rate = virtual_users`.** There are 8 real tests on the deployed database.

---

### Task 1: The JMeter image, and verifying the timer's XML

This task exists to kill the plan's biggest unknown first. Nobody here has verified jpgc's XML element shape against a real JMeter, and every later task builds on it. You will pin the plugin **and** prove a plan containing the timer actually parses.

**Files:**
- Modify: `deploy/Dockerfile.jmeter`
- Create: `backend/internal/jmx/testdata/timer-probe.jmx` (a hand-written probe plan, committed as the record of the verified shape)

**Interfaces:**
- Consumes: nothing.
- Produces: a verified XML fragment for the Throughput Shaping Timer, which Task 3 templatises. Record the exact fragment in your report.

- [ ] **Step 1: Resolve the plugin versions**

The spec deliberately does not state version numbers — they must match what is published and compatible with JMeter 5.6.3.

```bash
curl -s https://repo1.maven.org/maven2/kg/apc/jmeter-plugins-tst/maven-metadata.xml | grep -E "<latest>|<release>|<version>" | tail -5
curl -s https://repo1.maven.org/maven2/kg/apc/jmeter-plugins-cmn-jmeter/maven-metadata.xml | grep -E "<latest>|<release>|<version>" | tail -5
```

Take the latest release of each. Download both JARs and record their checksums:

```bash
TST=<version>; CMN=<version>
curl -fsSLO https://repo1.maven.org/maven2/kg/apc/jmeter-plugins-tst/$TST/jmeter-plugins-tst-$TST.jar
curl -fsSLO https://repo1.maven.org/maven2/kg/apc/jmeter-plugins-cmn-jmeter/$CMN/jmeter-plugins-cmn-jmeter-$CMN.jar
sha256sum jmeter-plugins-tst-$TST.jar jmeter-plugins-cmn-jmeter-$CMN.jar
```

Write the two versions and two checksums into your report. If either artifact 404s, STOP and report BLOCKED with the URL — do not substitute a different artifact or fall back to the Plugins Manager.

- [ ] **Step 2: Add the JARs to the image**

In `deploy/Dockerfile.jmeter`, after the JMeter extraction and before the `apt-get purge`, add a layer that downloads both JARs into `lib/ext/` and verifies each checksum. Substitute the real versions and checksums from Step 1:

```dockerfile
ARG TST_VERSION=<from step 1>
ARG TST_SHA256=<from step 1>
ARG CMN_VERSION=<from step 1>
ARG CMN_SHA256=<from step 1>
RUN set -eux; \
    EXT=/opt/apache-jmeter-${JMETER_VERSION}/lib/ext; \
    curl -fsSL "https://repo1.maven.org/maven2/kg/apc/jmeter-plugins-tst/${TST_VERSION}/jmeter-plugins-tst-${TST_VERSION}.jar" -o "$EXT/jmeter-plugins-tst.jar"; \
    echo "${TST_SHA256}  $EXT/jmeter-plugins-tst.jar" | sha256sum -c -; \
    curl -fsSL "https://repo1.maven.org/maven2/kg/apc/jmeter-plugins-cmn-jmeter/${CMN_VERSION}/jmeter-plugins-cmn-jmeter-${CMN_VERSION}.jar" -o "$EXT/jmeter-plugins-cmn-jmeter.jar"; \
    echo "${CMN_SHA256}  $EXT/jmeter-plugins-cmn-jmeter.jar" | sha256sum -c -
```

`curl` is already installed at that point and purged afterwards, so no new package is needed. The checksum lines are the whole point: a silent CDN swap becomes a failed build rather than a mystery at runtime.

- [ ] **Step 3: Write a probe plan containing the timer**

Create `backend/internal/jmx/testdata/timer-probe.jmx`. This is a minimal but complete plan — one thread group, one HTTP sampler, and the timer with two rows (one ramp, one hold):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<jmeterTestPlan version="1.2" properties="5.0" jmeter="5.6.3">
  <hashTree>
    <TestPlan guiclass="TestPlanGui" testclass="TestPlan" testname="Timer Probe" enabled="true">
      <boolProp name="TestPlan.functional_mode">false</boolProp>
      <boolProp name="TestPlan.serialize_threadgroups">false</boolProp>
      <elementProp name="TestPlan.user_defined_variables" elementType="Arguments" guiclass="ArgumentsPanel" testclass="Arguments" testname="User Defined Variables" enabled="true">
        <collectionProp name="Arguments.arguments"/>
      </elementProp>
    </TestPlan>
    <hashTree>
      <ThreadGroup guiclass="ThreadGroupGui" testclass="ThreadGroup" testname="Probe Threads" enabled="true">
        <stringProp name="ThreadGroup.on_sample_error">continue</stringProp>
        <elementProp name="ThreadGroup.main_controller" elementType="LoopController" guiclass="LoopControlPanel" testclass="LoopController" testname="Loop Controller" enabled="true">
          <boolProp name="LoopController.continue_forever">false</boolProp>
          <stringProp name="LoopController.loops">-1</stringProp>
        </elementProp>
        <stringProp name="ThreadGroup.num_threads">2</stringProp>
        <stringProp name="ThreadGroup.ramp_time">1</stringProp>
        <boolProp name="ThreadGroup.scheduler">true</boolProp>
        <stringProp name="ThreadGroup.duration">3</stringProp>
      </ThreadGroup>
      <hashTree>
        <kg.apc.jmeter.timers.VariableThroughputTimer guiclass="kg.apc.jmeter.timers.VariableThroughputTimerGui" testclass="kg.apc.jmeter.timers.VariableThroughputTimer" testname="Probe Load Profile" enabled="true">
          <collectionProp name="load_profile">
            <collectionProp name="stage">
              <stringProp name="start">1</stringProp>
              <stringProp name="end">5</stringProp>
              <stringProp name="dur">2</stringProp>
            </collectionProp>
            <collectionProp name="stage">
              <stringProp name="start">5</stringProp>
              <stringProp name="end">5</stringProp>
              <stringProp name="dur">1</stringProp>
            </collectionProp>
          </collectionProp>
        </kg.apc.jmeter.timers.VariableThroughputTimer>
        <hashTree/>
        <HTTPSamplerProxy guiclass="HttpTestSampleGui" testclass="HTTPSamplerProxy" testname="Probe Request" enabled="true">
          <elementProp name="HTTPsampler.Arguments" elementType="Arguments" guiclass="HTTPArgumentsPanel" testclass="Arguments" testname="User Defined Variables" enabled="true">
            <collectionProp name="Arguments.arguments"/>
          </elementProp>
          <stringProp name="HTTPSampler.domain">localhost</stringProp>
          <stringProp name="HTTPSampler.port">1</stringProp>
          <stringProp name="HTTPSampler.protocol">http</stringProp>
          <stringProp name="HTTPSampler.path">/</stringProp>
          <stringProp name="HTTPSampler.method">GET</stringProp>
        </HTTPSamplerProxy>
        <hashTree/>
      </hashTree>
    </hashTree>
  </hashTree>
</jmeterTestPlan>
```

The sampler points at a closed port on purpose — the requests fail fast, which is fine. You are testing that JMeter **parses and loads the timer**, not that it can reach anything.

- [ ] **Step 4: Build the image and run the probe**

```bash
docker build -f deploy/Dockerfile.jmeter -t boltrunner/jmeter:local .
docker run --rm -v "$PWD/backend/internal/jmx/testdata:/probe:ro" boltrunner/jmeter:local \
  -n -t /probe/timer-probe.jmx -l /tmp/out.jtl 2>&1 | tail -30
```

Expected: JMeter starts, runs for ~3 seconds and exits. **Success is the absence of a parse error.** Look specifically for these failure signatures, any of which means the shape or the plugin is wrong:

- `ClassNotFoundException: kg.apc.jmeter.timers.VariableThroughputTimer` — the JAR is missing or in the wrong directory
- `NoClassDefFoundError` mentioning `kg.apc` — the `cmn` dependency is missing
- `Error in NonGUIDriver` / `The test plan is empty` — JMeter failed to deserialise the plan
- A stack trace mentioning `CollectionProperty` — the inner property structure is wrong

If the run reports errors about connection refused on port 1, that is expected and is not a failure.

- [ ] **Step 5: If the shape is wrong, discover the right one**

Only if Step 4 failed on the timer element. Do not guess repeatedly. Ask JMeter itself what it expects:

```bash
docker run --rm boltrunner/jmeter:local -n --version 2>&1 | head -5
docker run --rm --entrypoint sh boltrunner/jmeter:local -c \
  'cd /opt/apache-jmeter-*/lib/ext && unzip -l jmeter-plugins-tst.jar | grep -i timer'
```

The class name in the JAR is authoritative for `guiclass`/`testclass`. For the inner property layout, the reliable move is to let JMeter write it: JMeter's XML for a `CollectionProperty` reads children positionally, so if the class loads but the rows do not, the likely cause is nesting depth rather than the `name` attributes.

Record in your report what you changed and why. Update `timer-probe.jmx` to the verified shape.

- [ ] **Step 6: Commit**

```bash
git add deploy/Dockerfile.jmeter backend/internal/jmx/testdata/timer-probe.jmx
git commit -m "build(jmeter): add the jpgc Throughput Shaping Timer, pinned and checksummed"
```

---

### Task 2: The stage model, the migration, and both stores

**Files:**
- Create: `backend/internal/store/postgres/migrations/0006_load_stages.sql`
- Modify: `backend/internal/model/model.go:15-26`
- Modify: `backend/internal/store/postgres/postgres.go` (the `testColumns` const, `scanTest`, `CreateTest`, `updateTestAtVersion`)
- Modify: `backend/internal/store/memstore/memstore.go`
- Test: `backend/internal/store/postgres/store_test.go`, `backend/internal/store/postgres/migrate_test.go`, `backend/internal/store/memstore/memstore_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces:
  - `model.LoadStage{StartRPS, EndRPS, DurationSeconds int}` with JSON tags `start_rps`, `end_rps`, `duration_seconds`
  - `model.Test.LoadStages []LoadStage` (`json:"load_stages"`)

  Tasks 3, 4 and 5 all depend on these exact names.

- [ ] **Step 1: Bring up a test database**

```bash
docker run -d --rm --name br-pg-test -e POSTGRES_USER=boltrunner -e POSTGRES_PASSWORD=boltrunner -e POSTGRES_DB=boltrunner -p 5433:5432 postgres:16
until docker exec br-pg-test pg_isready -U boltrunner -q; do :; done
```

Leave it running — Tasks 3 and 4 reuse it. The DSN for every backend command below is:
`postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable`

- [ ] **Step 2: Add the model type**

In `backend/internal/model/model.go`, above `Test`:

```go
// LoadStage is one row of a test's arrival-rate schedule. A hold is
// StartRPS == EndRPS; a ramp is StartRPS != EndRPS. The shape mirrors what the
// JMeter Throughput Shaping Timer consumes, so nothing has to be derived
// between what a user enters and what the generated plan contains.
type LoadStage struct {
	StartRPS        int `json:"start_rps"`
	EndRPS          int `json:"end_rps"`
	DurationSeconds int `json:"duration_seconds"`
}
```

In `Test`, add the field and re-comment the two whose meaning changed:

```go
	// VirtualUsers is the thread-pool ceiling, NOT the load level. Under an
	// arrival-rate model the rate sets the load; this bounds the concurrency
	// available to serve it. Too low, and observed throughput falls short of
	// the target.
	VirtualUsers int `json:"virtual_users"`
	// DurationSeconds is derived: the sum of LoadStages' durations. It is
	// stored so the thread group's scheduler and the UI have it, but it is
	// never accepted as API input.
	DurationSeconds int         `json:"duration_seconds"`
	LoadStages      []LoadStage `json:"load_stages"`
```

- [ ] **Step 3: Write the migration**

Create `backend/internal/store/postgres/migrations/0006_load_stages.sql`:

```sql
ALTER TABLE tests ADD COLUMN IF NOT EXISTS load_stages JSONB NOT NULL DEFAULT '[]'::jsonb;

-- Every pre-existing test becomes one flat stage holding its old virtual-user
-- count as a request rate, for its old duration. Little's Law makes that the
-- right order of magnitude: N users with ~1s responses produce ~N req/s.
-- The WHERE guard makes a re-run a no-op rather than re-flattening a test
-- whose schedule someone has since edited.
UPDATE tests SET load_stages = jsonb_build_array(jsonb_build_object(
    'start_rps', virtual_users,
    'end_rps', virtual_users,
    'duration_seconds', duration_seconds))
WHERE load_stages = '[]'::jsonb;
```

- [ ] **Step 4: Write the failing migration test**

Append to `backend/internal/store/postgres/migrate_test.go`:

```go
func TestMigration0006BackfillsOneFlatStage(t *testing.T) {
	ctx := context.Background()
	db := newSchemaDB(t, "br_mig_stages")

	// Build the pre-0006 world: migrate, then insert a test the old way.
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.Pool.Exec(ctx,
		`INSERT INTO tests (id, catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id, load_stages)
		 VALUES (gen_random_uuid(), gen_random_uuid(), 1, 'legacy', 'http://x', 7, 90,
		         (SELECT id FROM projects WHERE is_default), '[]'::jsonb)`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	body, err := migrationsFS.ReadFile("migrations/0006_load_stages.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, string(body)); err != nil {
		t.Fatalf("re-apply 0006: %v", err)
	}

	var stages []model.LoadStage
	if err := db.Pool.QueryRow(ctx,
		`SELECT load_stages FROM tests WHERE name = 'legacy'`).Scan(&stages); err != nil {
		t.Fatalf("read back stages: %v", err)
	}
	if len(stages) != 1 {
		t.Fatalf("expected exactly one backfilled stage, got %d (%+v)", len(stages), stages)
	}
	if stages[0].StartRPS != 7 || stages[0].EndRPS != 7 || stages[0].DurationSeconds != 90 {
		t.Fatalf("expected a flat 7 rps stage for 90s, got %+v", stages[0])
	}
}

// The guard must make a re-run inert: a test whose schedule was edited after
// the migration ran must not be flattened back.
func TestMigration0006DoesNotReflattenAnEditedSchedule(t *testing.T) {
	ctx := context.Background()
	db := newSchemaDB(t, "br_mig_stages_rerun")
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.Pool.Exec(ctx,
		`INSERT INTO tests (id, catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id, load_stages)
		 VALUES (gen_random_uuid(), gen_random_uuid(), 1, 'edited', 'http://x', 7, 90,
		         (SELECT id FROM projects WHERE is_default),
		         '[{"start_rps":1,"end_rps":50,"duration_seconds":30}]'::jsonb)`); err != nil {
		t.Fatalf("seed edited row: %v", err)
	}

	body, _ := migrationsFS.ReadFile("migrations/0006_load_stages.sql")
	if _, err := db.Pool.Exec(ctx, string(body)); err != nil {
		t.Fatalf("re-apply 0006: %v", err)
	}

	var stages []model.LoadStage
	db.Pool.QueryRow(ctx, `SELECT load_stages FROM tests WHERE name = 'edited'`).Scan(&stages)
	if len(stages) != 1 || stages[0].EndRPS != 50 {
		t.Fatalf("expected the edited schedule to survive, got %+v", stages)
	}
}
```

Add `"github.com/boltrunner/backend/internal/model"` to that file's imports if it is not already there.

- [ ] **Step 5: Run to verify they fail**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./internal/store/postgres/... -run 'TestMigration0006' -v
```

Expected: FAIL — `column "load_stages" does not exist`, because `Migrate` does not yet know about `0006`. It will pick the file up automatically once it exists (`postgres.go` discovers `migrations/*.sql` by filename), so if the file from Step 3 is saved this may instead fail on the assertion. Either is a genuine red.

- [ ] **Step 6: Wire the column through the postgres store**

In `backend/internal/store/postgres/postgres.go`:

Add `load_stages` to the shared projection:

```go
const testColumns = `catalog_id, id, version, project_id, name, target_url, virtual_users, duration_seconds, load_stages,
       MIN(created_at) OVER (PARTITION BY catalog_id) AS catalog_created_at, created_at`
```

Add it to `scanTest`, in the same position:

```go
func scanTest(row testScanner, t *model.Test) error {
	return row.Scan(&t.ID, &t.VersionID, &t.Version, &t.ProjectID, &t.Name,
		&t.TargetURL, &t.VirtualUsers, &t.DurationSeconds, &t.LoadStages, &t.CreatedAt, &t.UpdatedAt)
}
```

pgx v5 maps `jsonb` through `encoding/json`, so `&t.LoadStages` scans and `t.LoadStages` passes as a parameter directly — no manual marshalling.

Both explicit `SELECT` lists that name columns (in `ListTests` and `ListTestsForProject`) must gain `load_stages` in the same position as the projection, or the scan will mis-align. Search for `duration_seconds,` in that file and add `load_stages,` after each occurrence inside a `SELECT`.

`CreateTest` gains the column:

```go
		`INSERT INTO tests (id, catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id, load_stages)
		 VALUES ($1, $1, 1, $2, $3, $4, $5,
		         COALESCE($6, (SELECT id FROM projects WHERE is_default)), $7)
		 RETURNING catalog_id, id, version, project_id, created_at, created_at`,
		id, t.Name, t.TargetURL, t.VirtualUsers, t.DurationSeconds, nullableUUID(t.ProjectID), t.LoadStages,
```

`updateTestAtVersion` likewise:

```go
		`INSERT INTO tests (id, catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id, load_stages)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, version, created_at`,
		versionID, t.ID, version, t.Name, t.TargetURL, t.VirtualUsers, t.DurationSeconds, projectID, t.LoadStages,
```

- [ ] **Step 7: Run the migration tests**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./internal/store/postgres/... -run 'TestMigration0006' -v
```

Expected: PASS, both tests, neither SKIPped.

- [ ] **Step 8: Write the failing memstore aliasing test**

This is the test that matters most in this task. `LoadStage` has no pointer fields, so copying elements is a deep copy — but sharing the *slice* between two versions is not, and that would let an edit silently rewrite a completed run's configuration.

Append to `backend/internal/store/memstore/memstore_test.go`:

```go
// Two versions must not share one backing array. If they do, editing a test
// rewrites the schedule of every earlier version -- destroying the record of
// what a finished run actually executed, which is the whole point of
// copy-on-write versioning.
func TestUpdateTestDoesNotAliasLoadStages(t *testing.T) {
	ctx := context.Background()
	ps := NewProjectStore()
	ts := NewTestStore(ps)

	original := []model.LoadStage{{StartRPS: 1, EndRPS: 10, DurationSeconds: 30}}
	tst := &model.Test{Name: "smoke", TargetURL: "http://x", VirtualUsers: 5, DurationSeconds: 30, LoadStages: original}
	if err := ts.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	// Mutating the caller's slice after the write must not reach the store.
	original[0].EndRPS = 999

	edit := &model.Test{
		ID: tst.ID, Name: "smoke v2", TargetURL: "http://x", VirtualUsers: 5, DurationSeconds: 60,
		LoadStages: []model.LoadStage{{StartRPS: 10, EndRPS: 20, DurationSeconds: 60}},
	}
	if err := ts.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	versions, err := ts.ListTestVersions(ctx, tst.ID)
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	var v1 model.LoadStage
	for _, v := range versions {
		if v.Version == 1 {
			v1 = v.LoadStages[0]
		}
	}
	if v1.StartRPS != 1 || v1.EndRPS != 10 || v1.DurationSeconds != 30 {
		t.Fatalf("version 1's schedule was mutated: %+v", v1)
	}

	// And a read must not hand out the store's own array either.
	versions[0].LoadStages[0].EndRPS = -1
	again, _ := ts.ListTestVersions(ctx, tst.ID)
	for _, v := range again {
		if v.LoadStages[0].EndRPS == -1 {
			t.Fatal("ListTestVersions returned the store's own slice; a caller can corrupt it")
		}
	}
}
```

- [ ] **Step 9: Run to verify it fails**

```bash
cd backend && go test ./internal/store/memstore/... -run TestUpdateTestDoesNotAliasLoadStages -v
```

Expected: FAIL on one of the two aliasing assertions.

- [ ] **Step 10: Implement the copy in memstore**

In `backend/internal/store/memstore/memstore.go`, add the helper:

```go
// cloneStages defends the store's copy from its callers and vice versa.
// LoadStage has no pointer fields, so copying elements is a full deep copy --
// the slice header is the only sharing hazard.
func cloneStages(in []model.LoadStage) []model.LoadStage {
	if in == nil {
		return nil
	}
	out := make([]model.LoadStage, len(in))
	copy(out, in)
	return out
}
```

Call it on every write into `s.tests` and on every value handed back out. In `CreateTest`, before `s.tests[t.VersionID] = *t`:

```go
	t.LoadStages = cloneStages(t.LoadStages)
```

Then find every place the store copies a `model.Test` out of the map — `ListTests`, `ListTestsForProject`, `GetTest`, `ListTestVersions` — and clone the field on the outgoing copy. Read the file and cover all of them; a missed one is exactly the bug the test hunts.

`UpdateTest` writes through the same path as `CreateTest`; make sure its stored copy is cloned too.

- [ ] **Step 11: Run the memstore tests**

```bash
cd backend && go test ./internal/store/memstore/... -v 2>&1 | tail -20
```

Expected: PASS, including the new aliasing test.

- [ ] **Step 12: Write and run the postgres round-trip test**

Append to `backend/internal/store/postgres/store_test.go`:

```go
func TestPostgresLoadStagesRoundTripAndSurviveAnEdit(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	stages := []model.LoadStage{
		{StartRPS: 0, EndRPS: 50, DurationSeconds: 60},
		{StartRPS: 50, EndRPS: 50, DurationSeconds: 300},
	}
	tst := &model.Test{Name: "ramped", TargetURL: "http://x", VirtualUsers: 100, DurationSeconds: 360, LoadStages: stages}
	if err := db.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	got, err := db.GetTest(ctx, tst.ID)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if len(got.LoadStages) != 2 || got.LoadStages[1].StartRPS != 50 || got.LoadStages[0].EndRPS != 50 {
		t.Fatalf("stages did not round-trip: %+v", got.LoadStages)
	}

	edit := &model.Test{
		ID: tst.ID, Name: "ramped v2", TargetURL: "http://x", VirtualUsers: 100, DurationSeconds: 60,
		LoadStages: []model.LoadStage{{StartRPS: 200, EndRPS: 200, DurationSeconds: 60}},
	}
	if err := db.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	versions, err := db.ListTestVersions(ctx, tst.ID)
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	for _, v := range versions {
		if v.Version == 1 && (len(v.LoadStages) != 2 || v.LoadStages[1].StartRPS != 50) {
			t.Fatalf("version 1's schedule changed under an edit: %+v", v.LoadStages)
		}
		if v.Version == 2 && (len(v.LoadStages) != 1 || v.LoadStages[0].StartRPS != 200) {
			t.Fatalf("version 2 did not store its own schedule: %+v", v.LoadStages)
		}
	}
}
```

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./internal/store/postgres/... -v 2>&1 | tail -25
```

Expected: PASS, nothing skipped.

- [ ] **Step 13: Run the whole backend suite**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./...
```

Expected: `internal/api` FAILS — its fixtures create tests with no stages and the handlers do not know about them yet. That is Task 4's job. Every other package passes. Note which api tests fail so Task 4 can confirm it fixed exactly those.

- [ ] **Step 14: Commit**

```bash
git add backend/internal/model/model.go \
        backend/internal/store/postgres/migrations/0006_load_stages.sql \
        backend/internal/store/postgres/postgres.go \
        backend/internal/store/postgres/store_test.go \
        backend/internal/store/postgres/migrate_test.go \
        backend/internal/store/memstore/memstore.go \
        backend/internal/store/memstore/memstore_test.go
git commit -m "feat(backend): store an arrival-rate schedule on each test version"
```

---

### Task 3: Rendering the timer into the generated plan

**Files:**
- Modify: `backend/internal/jmx/template.go:10-14` (Params), the template const, and `Generate`
- Modify: `backend/internal/api/runs.go:35`
- Test: `backend/internal/jmx/template_test.go`

**Interfaces:**
- Consumes: `model.LoadStage` from Task 2; the verified XML fragment from Task 1.
- Produces: `jmx.Params.LoadStages []model.LoadStage`, consumed by `handleStartRun`.

- [ ] **Step 1: Write the failing template tests**

Append to `backend/internal/jmx/template_test.go`:

```go
func TestGenerateRendersEveryStageInOrder(t *testing.T) {
	out, err := Generate(Params{
		TargetURL: "http://example.com", VirtualUsers: 100, DurationSeconds: 360,
		LoadStages: []model.LoadStage{
			{StartRPS: 0, EndRPS: 50, DurationSeconds: 60},
			{StartRPS: 50, EndRPS: 50, DurationSeconds: 300},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	mustContain := []string{
		`<stringProp name="start">0</stringProp>`,
		`<stringProp name="end">50</stringProp>`,
		`<stringProp name="dur">60</stringProp>`,
		`<stringProp name="start">50</stringProp>`,
		`<stringProp name="dur">300</stringProp>`,
		`kg.apc.jmeter.timers.VariableThroughputTimer`,
	}
	for _, want := range mustContain {
		if !contains(out, want) {
			t.Fatalf("expected generated jmx to contain %q\n---\n%s", want, out)
		}
	}
	// Order matters: the 60s ramp must precede the 300s hold.
	if idx(out, `<stringProp name="dur">60</stringProp>`) > idx(out, `<stringProp name="dur">300</stringProp>`) {
		t.Fatal("stages rendered out of order")
	}
}

// A zero rate is meaningful -- it is how a schedule starts from idle. It must
// render as the literal 0, never as an empty string: JMeter fails to parse an
// empty numeric property, and that failure happens inside the pod where it is
// expensive to diagnose.
func TestGenerateRendersAZeroRateAsZero(t *testing.T) {
	out, err := Generate(Params{
		TargetURL: "http://example.com", VirtualUsers: 10, DurationSeconds: 30,
		LoadStages: []model.LoadStage{{StartRPS: 0, EndRPS: 10, DurationSeconds: 30}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !contains(out, `<stringProp name="start">0</stringProp>`) {
		t.Fatalf("expected a literal zero start rate\n---\n%s", out)
	}
}

func idx(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
```

Add `"github.com/boltrunner/backend/internal/model"` to the file's imports.

- [ ] **Step 2: Run to verify they fail**

```bash
cd backend && go test ./internal/jmx/... -run TestGenerateRenders -v
```

Expected: compile failure — `Params` has no field `LoadStages`.

- [ ] **Step 3: Implement**

In `backend/internal/jmx/template.go`, extend `Params`:

```go
type Params struct {
	TargetURL       string
	VirtualUsers    int
	DurationSeconds int
	LoadStages      []model.LoadStage
}
```

Add the import for `model`.

In the template const, insert the timer immediately after the `<hashTree>` that opens the thread group's children — that is, directly before `<HTTPSamplerProxy`. Use the fragment Task 1 verified:

```xml
        <kg.apc.jmeter.timers.VariableThroughputTimer guiclass="kg.apc.jmeter.timers.VariableThroughputTimerGui" testclass="kg.apc.jmeter.timers.VariableThroughputTimer" testname="BoltRunner Load Profile" enabled="true">
          <collectionProp name="load_profile">
{{range .LoadStages}}            <collectionProp name="stage">
              <stringProp name="start">{{.StartRPS}}</stringProp>
              <stringProp name="end">{{.EndRPS}}</stringProp>
              <stringProp name="dur">{{.DurationSeconds}}</stringProp>
            </collectionProp>
{{end}}          </collectionProp>
        </kg.apc.jmeter.timers.VariableThroughputTimer>
        <hashTree/>
```

**If Task 1's verified fragment differs from this, use Task 1's.** It was checked against a real JMeter; this was not.

The `{{range}}` and `{{end}}` sit at column 0 deliberately so the emitted XML keeps one row per line. Whitespace is insignificant here, but readable output makes a failed plan far cheaper to debug from a pod log.

- [ ] **Step 4: Run to verify they pass**

```bash
cd backend && go test ./internal/jmx/... -v
```

Expected: PASS, all five tests (three existing, two new).

- [ ] **Step 5: Pass the stages through at run time**

In `backend/internal/api/runs.go`, line 35:

```go
	plan, err := jmx.Generate(jmx.Params{
		TargetURL:       test.TargetURL,
		VirtualUsers:    test.VirtualUsers,
		DurationSeconds: test.DurationSeconds,
		LoadStages:      test.LoadStages,
	})
```

`test` here is the resolved version from `GetTest`, so a run gets the schedule of the exact version it executes — which is what keeps a finished run's record truthful.

- [ ] **Step 6: Verify the generated plan actually parses**

This is the step that connects the template to Task 1's evidence. Write a throwaway program that prints a generated plan, then feed it to the real JMeter image:

```bash
cd backend && cat > /tmp/genplan.go <<'EOF'
package main

import (
	"fmt"
	"os"

	"github.com/boltrunner/backend/internal/jmx"
	"github.com/boltrunner/backend/internal/model"
)

func main() {
	out, err := jmx.Generate(jmx.Params{
		TargetURL: "http://localhost:1/", VirtualUsers: 2, DurationSeconds: 3,
		LoadStages: []model.LoadStage{
			{StartRPS: 1, EndRPS: 5, DurationSeconds: 2},
			{StartRPS: 5, EndRPS: 5, DurationSeconds: 1},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(out)
}
EOF
go run /tmp/genplan.go > /tmp/generated.jmx && head -5 /tmp/generated.jmx
docker run --rm -v /tmp:/probe:ro boltrunner/jmeter:local -n -t /probe/generated.jmx -l /tmp/out2.jtl 2>&1 | tail -20
rm -f /tmp/genplan.go /tmp/generated.jmx
```

Expected: same as Task 1 Step 4 — JMeter runs for ~3 seconds and exits, with no `ClassNotFoundException`, no `CollectionProperty` stack trace, and no "test plan is empty". Connection-refused errors against port 1 are expected.

If this fails while Task 1's probe passed, the difference is in the template, not the plugin — diff the generated file against `backend/internal/jmx/testdata/timer-probe.jmx`.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/jmx/template.go backend/internal/jmx/template_test.go backend/internal/api/runs.go
git commit -m "feat(backend): render the arrival-rate schedule into the generated plan"
```

---

### Task 4: The API — stages in, derived duration out

**Files:**
- Modify: `backend/internal/api/tests.go:19-29` (request and validation), `handleCreateTest`, `handleUpdateTest`
- Test: `backend/internal/api/tests_test.go`, and every existing api test fixture that creates a test

**Interfaces:**
- Consumes: `model.LoadStage`, `model.Test.LoadStages` from Task 2.
- Produces: the request contract Task 5's api-client mirrors — `load_stages` required, `duration_seconds` rejected.

- [ ] **Step 1: Replace the request shape and validation**

In `backend/internal/api/tests.go`, replace `testRequest`, `valid()` and `testValidationMessage`:

```go
// testRequest is shared by create and update. ProjectID is only honoured on
// create -- moving a test between projects has its own route
// (PUT /api/tests/{testID}/project, see handleMoveTest below).
//
// DurationSeconds is a *int rather than an int so that "absent" and "zero" are
// distinguishable: it is derived from LoadStages, and a request that sends it
// is rejected rather than silently ignored.
type testRequest struct {
	Name            string            `json:"name"`
	TargetURL       string            `json:"target_url"`
	VirtualUsers    int               `json:"virtual_users"`
	DurationSeconds *int              `json:"duration_seconds"`
	ProjectID       string            `json:"project_id"`
	LoadStages      []model.LoadStage `json:"load_stages"`
}

// maxLoadStages bounds a JSONB column that is otherwise unbounded. No realistic
// profile approaches it; the cap exists so a malicious or buggy client cannot
// store an arbitrarily large document.
const maxLoadStages = 20

// validate returns the message to send and whether the request is acceptable.
// It returns distinct messages per failure because the frontend renders them
// verbatim -- "invalid request" tells a user nothing about which stage is wrong.
func (req testRequest) validate() (string, bool) {
	if req.Name == "" || req.TargetURL == "" || req.VirtualUsers <= 0 {
		return "name, target_url and virtual_users>0 are required", false
	}
	if req.DurationSeconds != nil {
		return "duration_seconds is derived from load_stages; do not send it", false
	}
	if len(req.LoadStages) == 0 {
		return "load_stages must contain at least one stage", false
	}
	if len(req.LoadStages) > maxLoadStages {
		return "load_stages must contain 20 stages or fewer", false
	}
	for _, s := range req.LoadStages {
		if s.DurationSeconds <= 0 {
			return "each stage needs a duration_seconds greater than 0", false
		}
		if s.StartRPS < 0 || s.EndRPS < 0 || (s.StartRPS == 0 && s.EndRPS == 0) {
			return "each stage needs a non-negative start_rps and end_rps, with at least one above 0", false
		}
	}
	return "", true
}

// totalDuration is the derived run window. Computing it here rather than in
// each store is what keeps the two backends agreeing by construction.
func totalDuration(stages []model.LoadStage) int {
	total := 0
	for _, s := range stages {
		total += s.DurationSeconds
	}
	return total
}
```

- [ ] **Step 2: Use it in both handlers**

In `handleCreateTest`, replace the validation block and the struct literal:

```go
	if msg, ok := req.validate(); !ok {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	t := &model.Test{
		ProjectID:       req.ProjectID,
		Name:            req.Name,
		TargetURL:       req.TargetURL,
		VirtualUsers:    req.VirtualUsers,
		DurationSeconds: totalDuration(req.LoadStages),
		LoadStages:      req.LoadStages,
	}
```

In `handleUpdateTest`, the same two changes:

```go
	if msg, ok := req.validate(); !ok {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	t := &model.Test{
		ID:              chi.URLParam(r, "testID"),
		Name:            req.Name,
		TargetURL:       req.TargetURL,
		VirtualUsers:    req.VirtualUsers,
		DurationSeconds: totalDuration(req.LoadStages),
		LoadStages:      req.LoadStages,
	}
```

- [ ] **Step 3: Fix the existing fixtures**

Task 2 Step 13 listed the api tests that now fail. Every JSON body in `backend/internal/api/` that creates or updates a test needs `duration_seconds` removed and `load_stages` added. Find them:

```bash
cd backend && grep -rn "duration_seconds" internal/api/ | grep -v "^internal/api/tests.go"
```

The mechanical replacement is `"duration_seconds":N` → `"load_stages":[{"start_rps":1,"end_rps":1,"duration_seconds":N}]`, which preserves each fixture's total duration. Apply it to every hit, including `fault_injection_test.go` and `runs_test.go`.

Also fix Go struct literals that build a `model.Test` directly (they bypass the handler, so they need `LoadStages` set only where a test then asserts on it — but set `DurationSeconds` consistently with any stages you add).

- [ ] **Step 4: Write the failing validation tests**

Append to `backend/internal/api/tests_test.go`:

```go
func createTestBody(stages string) string {
	return `{"name":"t","target_url":"http://x","virtual_users":10,"load_stages":` + stages + `}`
}

func postTest(srv *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/tests", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func TestCreateTestDerivesDurationFromStages(t *testing.T) {
	srv := newTestServer()
	rec := postTest(srv, createTestBody(`[{"start_rps":0,"end_rps":50,"duration_seconds":60},{"start_rps":50,"end_rps":50,"duration_seconds":300}]`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got model.Test
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.DurationSeconds != 360 {
		t.Fatalf("expected a derived duration of 360, got %d", got.DurationSeconds)
	}
	if len(got.LoadStages) != 2 {
		t.Fatalf("expected 2 stages back, got %+v", got.LoadStages)
	}
}

func TestCreateTestRejectsASentDuration(t *testing.T) {
	srv := newTestServer()
	body := `{"name":"t","target_url":"http://x","virtual_users":10,"duration_seconds":60,` +
		`"load_stages":[{"start_rps":1,"end_rps":1,"duration_seconds":60}]}`
	rec := postTest(srv, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "derived from load_stages") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

// Zero must be distinguishable from absent, which is why the field is a *int.
func TestCreateTestRejectsAnExplicitZeroDuration(t *testing.T) {
	srv := newTestServer()
	body := `{"name":"t","target_url":"http://x","virtual_users":10,"duration_seconds":0,` +
		`"load_stages":[{"start_rps":1,"end_rps":1,"duration_seconds":60}]}`
	if rec := postTest(srv, body); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an explicit zero, got %d", rec.Code)
	}
}

func TestCreateTestRejectsAnEmptySchedule(t *testing.T) {
	srv := newTestServer()
	rec := postTest(srv, createTestBody(`[]`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "at least one stage") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestCreateTestRejectsTooManyStages(t *testing.T) {
	srv := newTestServer()
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 21; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"start_rps":1,"end_rps":1,"duration_seconds":1}`)
	}
	sb.WriteString("]")
	rec := postTest(srv, createTestBody(sb.String()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "20 stages or fewer") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestCreateTestAcceptsExactlyTwentyStages(t *testing.T) {
	srv := newTestServer()
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 20; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"start_rps":1,"end_rps":1,"duration_seconds":1}`)
	}
	sb.WriteString("]")
	if rec := postTest(srv, createTestBody(sb.String())); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 at exactly the limit, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateTestRejectsAZeroDurationStage(t *testing.T) {
	srv := newTestServer()
	rec := postTest(srv, createTestBody(`[{"start_rps":1,"end_rps":1,"duration_seconds":0}]`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "duration_seconds greater than 0") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestCreateTestRejectsAStageWithNoLoad(t *testing.T) {
	srv := newTestServer()
	rec := postTest(srv, createTestBody(`[{"start_rps":0,"end_rps":0,"duration_seconds":30}]`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "at least one above 0") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestCreateTestRejectsANegativeRate(t *testing.T) {
	srv := newTestServer()
	if rec := postTest(srv, createTestBody(`[{"start_rps":-1,"end_rps":10,"duration_seconds":30}]`)); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// A zero start rate is legitimate -- it is how a schedule ramps from idle.
func TestCreateTestAcceptsAZeroStartRate(t *testing.T) {
	srv := newTestServer()
	if rec := postTest(srv, createTestBody(`[{"start_rps":0,"end_rps":10,"duration_seconds":30}]`)); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for a ramp from zero, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateTestCarriesStagesOntoTheNewVersion(t *testing.T) {
	srv := newTestServer()
	createRec := postTest(srv, createTestBody(`[{"start_rps":1,"end_rps":1,"duration_seconds":30}]`))
	var created model.Test
	json.Unmarshal(createRec.Body.Bytes(), &created)

	body := `{"name":"t2","target_url":"http://x","virtual_users":10,` +
		`"load_stages":[{"start_rps":10,"end_rps":100,"duration_seconds":120}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var updated model.Test
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.DurationSeconds != 120 || len(updated.LoadStages) != 1 || updated.LoadStages[0].EndRPS != 100 {
		t.Fatalf("unexpected updated test: %+v", updated)
	}
}
```

- [ ] **Step 5: Run the api suite**

```bash
cd backend && go test ./internal/api/... -v 2>&1 | tail -30
```

Expected: PASS. The 11 new tests pass and every fixture you fixed in Step 3 passes again.

- [ ] **Step 6: Run the whole backend suite and check the gate**

```bash
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out | tail -1
rm -f coverage.out
```

Expected: all 11 packages pass; total ≥ 88%. Report the number.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api/
git commit -m "feat(backend): accept a load schedule and derive the run duration from it"
```

---

### Task 5: The API client and types

**Files:**
- Modify: `frontend/lib/api-client.ts:3-23` (`Test`), `:56-65` (`CreateTestInput`)
- Test: `frontend/__tests__/api-client.test.ts`

**Interfaces:**
- Consumes: Task 4's request contract.
- Produces:
  - `export type LoadStage = { start_rps: number; end_rps: number; duration_seconds: number }`
  - `Test.load_stages: LoadStage[]`
  - `CreateTestInput` / `UpdateTestInput` carrying `load_stages` and **no** `duration_seconds`

  Tasks 6 and 7 consume these exact names.

- [ ] **Step 1: Update the types**

In `frontend/lib/api-client.ts`, add above `Test`:

```ts
export type LoadStage = { start_rps: number; end_rps: number; duration_seconds: number };
```

`Test` keeps `duration_seconds` — the server still returns it, derived — and gains `load_stages`:

```ts
export type Test = {
  id: string;
  name: string;
  target_url: string;
  virtual_users: number;
  // Derived server-side from load_stages. Present on every response; never sent.
  duration_seconds: number;
  load_stages: LoadStage[];
  created_at: string;
  version?: number;
  version_id?: string;
  project_id?: string;
  updated_at?: string;
};
```

`CreateTestInput` **drops** `duration_seconds`, so a caller cannot send the field the API now rejects:

```ts
export type CreateTestInput = {
  name: string;
  target_url: string;
  virtual_users: number;
  load_stages: LoadStage[];
  // Optional: the backend COALESCEs a missing value to the default project.
  project_id?: string;
};
```

- [ ] **Step 2: Fix the fixtures the new required fields break**

```bash
cd frontend && npx tsc --noEmit 2>&1 | grep -v "URLSearchParams" | head -30
```

Ignore any `URLSearchParams` vs `ReadonlyURLSearchParams` errors — those are pre-existing in the navigation mocks and unrelated.

Every `Test`-typed literal now needs `load_stages`. Give each one a single stage whose `duration_seconds` matches that fixture's existing `duration_seconds`, so no fixture changes meaning:

```ts
load_stages: [{ start_rps: 1, end_rps: 1, duration_seconds: 30 }],
```

- [ ] **Step 3: Write the failing api-client test**

Append to `frontend/__tests__/api-client.test.ts`:

```ts
  it('createTest sends load_stages and no duration_seconds', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          id: 't1', name: 'x', target_url: 'http://x', virtual_users: 10,
          duration_seconds: 60, load_stages: [{ start_rps: 1, end_rps: 10, duration_seconds: 60 }],
          created_at: 'x',
        }),
        { status: 201 }
      )
    );

    const got = await createTest({
      name: 'x',
      target_url: 'http://x',
      virtual_users: 10,
      load_stages: [{ start_rps: 1, end_rps: 10, duration_seconds: 60 }],
    });

    expect(got.duration_seconds).toBe(60);
    expect(got.load_stages).toHaveLength(1);
    const body = JSON.parse(String(fetchMock.mock.calls[0][1]?.body));
    expect(body.load_stages).toEqual([{ start_rps: 1, end_rps: 10, duration_seconds: 60 }]);
    // The API rejects a sent duration; the input type must make that unsendable.
    expect(body).not.toHaveProperty('duration_seconds');
  });
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd frontend && npx vitest run __tests__/api-client.test.ts
```

Expected: PASS. `createTest` needs no code change — it forwards whatever the typed input carries — so this test pins the *type* boundary rather than driving new logic. That is the point: it fails at compile time if `duration_seconds` is ever added back to the input.

- [ ] **Step 5: Commit**

```bash
git add frontend/lib/api-client.ts frontend/__tests__/
git commit -m "feat(frontend): carry a load schedule through the API client"
```

---

### Task 6: The stage editor

**Files:**
- Modify: `frontend/components/TestFields.tsx` (whole file)
- Test: `frontend/__tests__/TestFields.test.tsx`

**Interfaces:**
- Consumes: `LoadStage` from Task 5.
- Produces:
  - `export type StageDraft = { start_rps: string; end_rps: string; duration_seconds: string }`
  - `export type TestField = 'name' | 'targetUrl' | 'virtualUsers'` — **`durationSeconds` is removed from this union**
  - `TestFields` props gain `stages: StageDraft[]` and `onStagesChange: (stages: StageDraft[]) => void`

  Task 7 consumes all three.

- [ ] **Step 1: Write the failing tests**

`frontend/__tests__/TestFields.test.tsx` currently has this helper at line 5:

```tsx
function renderFields(onChange = vi.fn()) {
  render(
    <TestFields name="smoke" targetUrl="http://x" virtualUsers="5" durationSeconds="30" onChange={onChange} />
  );
  return onChange;
}
```

It takes a bare `onChange`, and its three existing cases call `renderFields()` and `renderFields(onChange)`. Widen it to an options object, keeping a default for every field so the existing calls still work with no arguments — but the one case that does `const onChange = renderFields()` must change, because the helper can no longer return a single spy:

```tsx
function renderFields(
  opts: {
    stages?: StageDraft[];
    onChange?: ReturnType<typeof vi.fn>;
    onStagesChange?: ReturnType<typeof vi.fn>;
  } = {}
) {
  const onChange = opts.onChange ?? vi.fn();
  const onStagesChange = opts.onStagesChange ?? vi.fn();
  render(
    <TestFields
      name="smoke"
      targetUrl="http://x"
      virtualUsers="5"
      stages={opts.stages ?? [{ start_rps: '1', end_rps: '1', duration_seconds: '30' }]}
      onChange={onChange}
      onStagesChange={onStagesChange}
    />
  );
  return { onChange, onStagesChange };
}
```

Update the three existing cases: `renderFields()` still works, `const onChange = renderFields()` becomes `const { onChange } = renderFields()`, and the two assertions on `/duration/i` and `/virtual users/i` must be retargeted — `/virtual users/i` becomes `/max threads/i`, and the duration assertions move to the stage inputs. Do not delete those cases.

Then append:

```tsx
  it('renders one row per stage', () => {
    renderFields({
      stages: [
        { start_rps: '0', end_rps: '50', duration_seconds: '60' },
        { start_rps: '50', end_rps: '50', duration_seconds: '300' },
      ],
    });
    expect(screen.getAllByRole('spinbutton', { name: /from rps/i })).toHaveLength(2);
  });

  it('adds a stage', () => {
    const onStagesChange = vi.fn();
    renderFields({ stages: [{ start_rps: '1', end_rps: '1', duration_seconds: '30' }], onStagesChange });

    fireEvent.click(screen.getByRole('button', { name: /add stage/i }));

    expect(onStagesChange).toHaveBeenCalledWith([
      { start_rps: '1', end_rps: '1', duration_seconds: '30' },
      { start_rps: '1', end_rps: '1', duration_seconds: '30' },
    ]);
  });

  it('removes a stage', () => {
    const onStagesChange = vi.fn();
    renderFields({
      stages: [
        { start_rps: '1', end_rps: '1', duration_seconds: '30' },
        { start_rps: '2', end_rps: '2', duration_seconds: '60' },
      ],
      onStagesChange,
    });

    fireEvent.click(screen.getAllByRole('button', { name: /remove stage/i })[0]);

    expect(onStagesChange).toHaveBeenCalledWith([{ start_rps: '2', end_rps: '2', duration_seconds: '60' }]);
  });

  // The API rejects an empty schedule, so the form must not be able to produce
  // one. Disabling is better than rejecting on submit: the user never reaches
  // an invalid state to be told about.
  it('disables remove when only one stage remains', () => {
    renderFields({ stages: [{ start_rps: '1', end_rps: '1', duration_seconds: '30' }] });
    expect(screen.getByRole('button', { name: /remove stage/i })).toBeDisabled();
  });

  it('edits a stage field in place', () => {
    const onStagesChange = vi.fn();
    renderFields({ stages: [{ start_rps: '1', end_rps: '1', duration_seconds: '30' }], onStagesChange });

    fireEvent.change(screen.getByRole('spinbutton', { name: /to rps/i }), { target: { value: '75' } });

    expect(onStagesChange).toHaveBeenCalledWith([{ start_rps: '1', end_rps: '75', duration_seconds: '30' }]);
  });

  it('shows the derived total duration', () => {
    renderFields({
      stages: [
        { start_rps: '0', end_rps: '50', duration_seconds: '60' },
        { start_rps: '50', end_rps: '50', duration_seconds: '690' },
      ],
    });
    expect(screen.getByText(/2 stages, 12m30s total/i)).toBeInTheDocument();
  });

  it('shows a singular stage count for one stage', () => {
    renderFields({ stages: [{ start_rps: '1', end_rps: '1', duration_seconds: '45' }] });
    expect(screen.getByText(/1 stage, 45s total/i)).toBeInTheDocument();
  });

  // Mid-typing an empty box is a legal intermediate state, not a zero.
  it('treats an empty duration as zero in the total without crashing', () => {
    renderFields({ stages: [{ start_rps: '1', end_rps: '1', duration_seconds: '' }] });
    expect(screen.getByText(/1 stage, 0s total/i)).toBeInTheDocument();
  });

  it('labels the thread field as a ceiling, not a load level', () => {
    renderFields();
    expect(screen.getByRole('spinbutton', { name: /max threads/i })).toBeInTheDocument();
  });
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/TestFields.test.tsx
```

Expected: FAIL — no stage inputs, no add/remove buttons, and the existing `Virtual users` label does not match `/max threads/i`.

- [ ] **Step 3: Implement**

Replace `frontend/components/TestFields.tsx`:

```tsx
'use client';

export type TestField = 'name' | 'targetUrl' | 'virtualUsers';

// Draft values are strings, not numbers: that keeps the inputs controlled while
// a user is mid-typing, where an empty string is a legal intermediate state
// that Number('') would silently turn into 0.
export type StageDraft = { start_rps: string; end_rps: string; duration_seconds: string };

export function emptyStage(): StageDraft {
  return { start_rps: '0', end_rps: '10', duration_seconds: '60' };
}

function formatTotal(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return s === 0 ? `${m}m` : `${m}m${s}s`;
}

export function TestFields({
  name,
  targetUrl,
  virtualUsers,
  stages,
  onChange,
  onStagesChange,
}: {
  name: string;
  targetUrl: string;
  virtualUsers: string;
  stages: StageDraft[];
  onChange: (field: TestField, value: string) => void;
  onStagesChange: (stages: StageDraft[]) => void;
}) {
  // Number('') is 0, which is the right reading here: a half-typed box
  // contributes nothing to the total rather than making it NaN.
  const totalSeconds = stages.reduce((sum, s) => sum + (Number(s.duration_seconds) || 0), 0);

  function updateStage(index: number, field: keyof StageDraft, value: string) {
    onStagesChange(stages.map((s, i) => (i === index ? { ...s, [field]: value } : s)));
  }

  return (
    <>
      <label className="flex flex-col gap-1">
        <span>Name</span>
        <input value={name} onChange={(e) => onChange('name', e.target.value)} required />
      </label>
      <label className="flex flex-col gap-1">
        <span>Target URL</span>
        <input value={targetUrl} onChange={(e) => onChange('targetUrl', e.target.value)} required type="url" />
      </label>
      <label className="flex flex-col gap-1">
        <span>Max threads</span>
        <input
          value={virtualUsers}
          onChange={(e) => onChange('virtualUsers', e.target.value)}
          required
          type="number"
          min={1}
        />
        <span className="text-xs text-text-muted">
          Caps how much concurrency is available to serve the rate. It does not set the load.
        </span>
      </label>

      <fieldset className="flex flex-col gap-2 border border-border rounded p-3">
        <legend className="text-sm px-1">Load profile</legend>
        {stages.map((stage, i) => (
          <div key={i} className="flex flex-wrap items-end gap-2">
            <label className="flex flex-col gap-1">
              <span className="text-xs">From RPS</span>
              <input
                aria-label={`Stage ${i + 1} from RPS`}
                value={stage.start_rps}
                onChange={(e) => updateStage(i, 'start_rps', e.target.value)}
                required
                type="number"
                min={0}
                className="w-24"
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-xs">To RPS</span>
              <input
                aria-label={`Stage ${i + 1} to RPS`}
                value={stage.end_rps}
                onChange={(e) => updateStage(i, 'end_rps', e.target.value)}
                required
                type="number"
                min={0}
                className="w-24"
              />
            </label>
            <label className="flex flex-col gap-1">
              <span className="text-xs">Over seconds</span>
              <input
                aria-label={`Stage ${i + 1} duration seconds`}
                value={stage.duration_seconds}
                onChange={(e) => updateStage(i, 'duration_seconds', e.target.value)}
                required
                type="number"
                min={1}
                className="w-24"
              />
            </label>
            <button
              type="button"
              aria-label={`Remove stage ${i + 1}`}
              disabled={stages.length === 1}
              onClick={() => onStagesChange(stages.filter((_, j) => j !== i))}
            >
              Remove
            </button>
          </div>
        ))}
        <div className="flex items-center gap-3">
          <button type="button" onClick={() => onStagesChange([...stages, { ...stages[stages.length - 1] }])}>
            Add stage
          </button>
          <span className="text-xs text-text-muted">
            {stages.length} {stages.length === 1 ? 'stage' : 'stages'}, {formatTotal(totalSeconds)} total
          </span>
        </div>
      </fieldset>
    </>
  );
}
```

Two details worth not "simplifying":

- **Add copies the last stage** rather than inserting a fresh default. A schedule is usually built by repeating and tweaking, and a copied row means the common case (hold at the rate you just ramped to) needs one edit rather than three.
- **The `aria-label`s are numbered.** Without the index, every row's "From RPS" would share one accessible name, and `getByRole` would throw on ambiguity the moment a second stage exists.

- [ ] **Step 4: Run to verify they pass**

```bash
cd frontend && npx vitest run __tests__/TestFields.test.tsx
```

Expected: PASS.

If a query fails with "found multiple elements", the numbered `aria-label` is missing from that input — fix the label rather than switching the query to `getAllBy*`.

- [ ] **Step 5: Commit**

```bash
git add frontend/components/TestFields.tsx frontend/__tests__/TestFields.test.tsx
git commit -m "feat(frontend): edit a load schedule as repeatable stages"
```

---

### Task 7: Wiring both forms

**Files:**
- Modify: `frontend/components/CreateTestForm.tsx`
- Modify: `frontend/components/EditTestForm.tsx`
- Test: `frontend/__tests__/CreateTestForm.test.tsx`, `frontend/__tests__/EditTestForm.test.tsx`

**Interfaces:**
- Consumes: `StageDraft`, `emptyStage`, `TestField` from Task 6; `LoadStage`, `CreateTestInput` from Task 5.
- Produces: nothing.

- [ ] **Step 1: Write the failing CreateTestForm test**

Append to `frontend/__tests__/CreateTestForm.test.tsx`, following the file's existing setup:

```tsx
  it('submits the stage schedule and no duration_seconds', async () => {
    const createTest = vi.spyOn(api, 'createTest').mockResolvedValue({
      id: 't1', name: 'x', target_url: 'http://x', virtual_users: 10,
      duration_seconds: 60, load_stages: [{ start_rps: 0, end_rps: 10, duration_seconds: 60 }],
      created_at: 'x',
    });

    render(<CreateTestForm onCreated={vi.fn()} />);
    fireEvent.change(screen.getByLabelText(/^name$/i), { target: { value: 'x' } });
    fireEvent.change(screen.getByLabelText(/target url/i), { target: { value: 'http://x' } });
    fireEvent.change(screen.getByRole('spinbutton', { name: /stage 1 to rps/i }), { target: { value: '10' } });
    fireEvent.click(screen.getByRole('button', { name: /create test/i }));

    await waitFor(() => expect(createTest).toHaveBeenCalled());
    const sent = createTest.mock.calls[0][0];
    expect(sent.load_stages).toEqual([{ start_rps: 0, end_rps: 10, duration_seconds: 60 }]);
    expect(sent).not.toHaveProperty('duration_seconds');
  });
```

- [ ] **Step 2: Implement CreateTestForm**

In `frontend/components/CreateTestForm.tsx`, replace the `durationSeconds` state with stage state and convert on submit:

```tsx
import { TestFields, TestField, StageDraft, emptyStage } from '@/components/TestFields';
```

```tsx
  const [virtualUsers, setVirtualUsers] = useState('10');
  const [stages, setStages] = useState<StageDraft[]>([emptyStage()]);
```

Remove `durationSeconds` from `setters` (`TestField` no longer includes it):

```tsx
  const setters: Record<TestField, (v: string) => void> = {
    name: setName,
    targetUrl: setTargetUrl,
    virtualUsers: setVirtualUsers,
  };
```

In `handleSubmit`:

```tsx
      const test = await createTest({
        name,
        target_url: targetUrl,
        virtual_users: Number(virtualUsers),
        load_stages: stages.map((s) => ({
          start_rps: Number(s.start_rps),
          end_rps: Number(s.end_rps),
          duration_seconds: Number(s.duration_seconds),
        })),
        ...(selectedId ? { project_id: selectedId } : {}),
      });
```

And in the JSX, pass the new props:

```tsx
      <TestFields
        name={name}
        targetUrl={targetUrl}
        virtualUsers={virtualUsers}
        stages={stages}
        onChange={(field, value) => setters[field](value)}
        onStagesChange={setStages}
      />
```

Leave the post-create reset as it is (`setName('')`, `setTargetUrl('')`) — the schedule is worth keeping between creates, since consecutive tests usually share a profile.

- [ ] **Step 3: Run the CreateTestForm tests**

```bash
cd frontend && npx vitest run __tests__/CreateTestForm.test.tsx
```

Expected: PASS. Existing cases in that file that referenced the duration input need their queries updated to the stage inputs — do that rather than deleting the cases.

- [ ] **Step 4: Write the failing EditTestForm test**

Append to `frontend/__tests__/EditTestForm.test.tsx`:

```tsx
  it('seeds the stage rows from the current version', () => {
    render(
      <EditTestForm
        current={{
          ...current,
          load_stages: [
            { start_rps: 0, end_rps: 50, duration_seconds: 60 },
            { start_rps: 50, end_rps: 50, duration_seconds: 300 },
          ],
        }}
        onSave={vi.fn()}
      />
    );

    expect(screen.getByRole('spinbutton', { name: /stage 1 to rps/i })).toHaveValue(50);
    expect(screen.getByRole('spinbutton', { name: /stage 2 duration seconds/i })).toHaveValue(300);
  });

  it('submits the edited schedule', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(
      <EditTestForm
        current={{ ...current, load_stages: [{ start_rps: 1, end_rps: 1, duration_seconds: 30 }] }}
        onSave={onSave}
      />
    );

    fireEvent.change(screen.getByRole('spinbutton', { name: /stage 1 to rps/i }), { target: { value: '99' } });
    fireEvent.click(screen.getByRole('button', { name: /save/i }));

    await waitFor(() => expect(onSave).toHaveBeenCalled());
    expect(onSave.mock.calls[0][0].load_stages).toEqual([{ start_rps: 1, end_rps: 99, duration_seconds: 30 }]);
  });
```

That file already declares a module-level `TestVersion` fixture named **`current`** at line 6 — reuse it, and add `load_stages` to its declaration so every existing case in the file keeps typechecking. Note the fixture and the prop share the name; the spreads above are `{...current, load_stages: [...]}`, which is intentional and not a typo.

Its existing cases also assert on `/virtual users/i` and `/duration/i`. Retarget those the same way as in Task 6: `/max threads/i`, and the stage inputs.

- [ ] **Step 5: Implement EditTestForm**

Mirror the CreateTestForm changes, plus the re-seed. Replace the `durationSeconds` state:

```tsx
  const [stages, setStages] = useState<StageDraft[]>(
    current.load_stages.map((s) => ({
      start_rps: String(s.start_rps),
      end_rps: String(s.end_rps),
      duration_seconds: String(s.duration_seconds),
    }))
  );
```

In the `useEffect` keyed on `current.version_id`, replace the `setDurationSeconds` line with the same mapping into `setStages`. That effect's existing mount-skip guard stays exactly as it is — it is what stops a keystroke landing in the commit window from being reverted.

In the submit handler, send `load_stages` built the same way as CreateTestForm, and stop sending `duration_seconds`.

Update the `setters` record and the `TestFields` props as in Step 2.

- [ ] **Step 6: Run the EditTestForm tests, then the whole suite**

```bash
cd frontend && npx vitest run __tests__/EditTestForm.test.tsx
cd frontend && npx vitest run
```

Expected: both PASS. Any failure elsewhere is a fixture missing `load_stages` — fix the fixture, not the assertion.

- [ ] **Step 7: Commit**

```bash
git add frontend/components/CreateTestForm.tsx frontend/components/EditTestForm.tsx frontend/__tests__/
git commit -m "feat(frontend): create and edit tests with a load schedule"
```

---

### Task 8: End-to-end, the integration test, and the full gate

**Files:**
- Modify: `backend/internal/integration/walking_skeleton_test.go`
- Modify: `frontend/e2e/walking-skeleton.spec.ts`, `frontend/e2e/project-workspaces.spec.ts`, `frontend/e2e/test-versioning.spec.ts`
- Test: everything

**Interfaces:**
- Consumes: all of the above.
- Produces: nothing.

- [ ] **Step 1: Fix the Go integration test**

```bash
cd backend && grep -n "duration_seconds\|virtual_users" internal/integration/walking_skeleton_test.go
```

It posts a test-creation body. Replace `"duration_seconds":N` with `"load_stages":[{"start_rps":1,"end_rps":1,"duration_seconds":N}]`. Keep the rest of the body as it is.

- [ ] **Step 2: Fix the e2e specs**

Every spec that fills the create-test form uses `page.getByLabel(/duration/i)`, which no longer exists. Find them:

```bash
cd frontend && grep -rn "duration\|virtual users" e2e/
```

Replace the duration fill with the stage inputs, and the virtual-users label with the new one:

```ts
  await page.getByLabel(/max threads/i).fill('2');
  await page.getByRole('spinbutton', { name: /stage 1 from rps/i }).fill('1');
  await page.getByRole('spinbutton', { name: /stage 1 to rps/i }).fill('1');
  await page.getByRole('spinbutton', { name: /stage 1 duration seconds/i }).fill('10');
```

Keep every spec's effective run duration the same as it is today, so no suite gets slower.

- [ ] **Step 3: Add the e2e that proves a multi-stage profile survives**

Append to `frontend/e2e/test-versioning.spec.ts` a test that creates a two-stage profile and confirms both rows come back on the detail page:

```ts
test('a multi-stage load profile survives a round trip', async ({ page }) => {
  const testName = `E2E Profile ${Date.now()}`;
  await page.goto('/');

  await page.getByLabel(/^name$/i).fill(testName);
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/max threads/i).fill('2');
  await page.getByRole('spinbutton', { name: /stage 1 from rps/i }).fill('1');
  await page.getByRole('spinbutton', { name: /stage 1 to rps/i }).fill('5');
  await page.getByRole('spinbutton', { name: /stage 1 duration seconds/i }).fill('5');

  await page.getByRole('button', { name: /add stage/i }).click();
  await page.getByRole('spinbutton', { name: /stage 2 from rps/i }).fill('5');
  await page.getByRole('spinbutton', { name: /stage 2 to rps/i }).fill('5');
  await page.getByRole('spinbutton', { name: /stage 2 duration seconds/i }).fill('5');

  // The derived total is what the server will compute; if these disagree the
  // form and the API have drifted.
  await expect(page.getByText(/2 stages, 10s total/i)).toBeVisible();

  await page.getByRole('button', { name: /create test/i }).click();
  await page.getByRole('link', { name: testName }).click();
  await expect(page).toHaveURL(/\/tests\//);

  await expect(page.getByRole('spinbutton', { name: /stage 2 to rps/i })).toHaveValue('5');
  await expect(page.getByText(/2 stages, 10s total/i)).toBeVisible();
});
```

- [ ] **Step 4: Both coverage gates and the build**

```bash
cd frontend && npm run test:coverage
cd frontend && npm run build
cd backend && BOLTRUNNER_TEST_DSN="postgres://boltrunner:boltrunner@localhost:5433/boltrunner?sslmode=disable" go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out | tail -1
rm -f backend/coverage.out
```

Expected: frontend ≥ 88% on all four axes; backend ≥ 88% total; build succeeds. Report all five numbers.

- [ ] **Step 5: Run the browser suite against a real backend**

This is the step that proves the plugin works. **Build the JMeter image fresh** — a cached one from before Task 1 would pass while shipping a broken image.

```bash
docker rm -f br-api br-pg 2>/dev/null || true
docker run -d --rm --name br-pg -e POSTGRES_USER=boltrunner -e POSTGRES_PASSWORD=boltrunner -e POSTGRES_DB=boltrunner -p 5432:5432 postgres:16
until docker exec br-pg pg_isready -U boltrunner -q; do :; done
docker build -f deploy/Dockerfile.server -t boltrunner/server:local .
docker build -f deploy/Dockerfile.jmeter -t boltrunner/jmeter:local .
docker build -f deploy/Dockerfile.sidecar -t boltrunner/sidecar:local .
docker run -d --rm --name br-api --network host \
  -e DATABASE_URL="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" \
  -e KUBECONFIG=/kube/config -v "$HOME/.kube/config:/kube/config:ro" boltrunner/server:local
until curl -sf http://localhost:8080/healthz >/dev/null; do :; done
```

Then serve the frontend — **check nothing else is on :3000 first**, because a stale server there will silently serve a different build and every spec will fail for reasons that have nothing to do with your change:

```bash
(ss -ltn | grep -q ':3000' && echo "PORT 3000 IS TAKEN - find and stop that process first") || echo "3000 free"
cd frontend && NEXT_PUBLIC_API_URL=http://localhost:8080 npm run build && npm start &
until curl -sf http://localhost:3000/admin 2>/dev/null | grep -q "API base URL"; do :; done
npx playwright test
```

Polling for actual page content rather than a 200 is deliberate: a 200 only proves *something* is listening on that port.

Expected: **15 passed across 5 files** — 14 before, plus the profile round-trip added in Step 3.

Tear down with `docker rm -f br-api br-pg` and stop the Next server. Leave `br-pg-test` on 5433 alone.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/integration/ frontend/e2e/
git commit -m "test: cover multi-stage load profiles end to end"
```

- [ ] **Step 7: Clean up**

```bash
docker rm -f br-pg-test
```

---

## Self-review notes

- **Spec coverage.** Plugin pinned with checksums → Task 1 Steps 1-2. Verifying the timer's XML shape → Task 1 Steps 3-5, and again against the real template in Task 3 Step 6. Decision 3 (explicit start/end/duration stage) → Task 2 Step 2. Decision 5 (backfill to one flat stage) → Task 2 Steps 3-4, including the re-run guard. Decision 4 (`virtual_users` as ceiling) → Task 2 Step 2's comment and Task 6's relabel plus helper text. Decision 6 (derived duration) → Task 4 Steps 1-2, rejected as input by the `*int`. JSONB round-trip and version isolation → Task 2 Steps 8-12. Every row of the error table → Task 4 Step 4. Frontend sections → Tasks 5-7. Testing strategy → the test steps throughout, plus Task 8.
- **Placeholder scan.** One deliberate deferral, with a resolution procedure rather than hand-waving: the plugin versions, where Task 1 Step 1 gives the exact `curl` commands and requires the values in the report. Task 3 Step 3 tells the implementer to prefer Task 1's verified XML fragment over the one written here, since only one of them was checked against a real JMeter. Everything else gives exact paths, complete code and exact commands.
- **Two invented names caught on review.** The first draft told the implementer to adapt a `renderFields` helper and reuse a `versionFixture`. Checking the files showed `renderFields` takes a bare `onChange` and returns that one spy — an options object would break its three existing callers — and `EditTestForm.test.tsx`'s fixture is named `current`, not `versionFixture`. Both steps now show the exact widened helper and name the real fixture, including the fact that the fixture and the prop share the name so the spread reads oddly on purpose. This is the same class of error the previous plan shipped three of; verifying helper names against the files is worth the two minutes.
- **Type consistency.** `model.LoadStage{StartRPS, EndRPS, DurationSeconds}` is named identically in Tasks 2, 3 and 4. Its JSON tags `start_rps`/`end_rps`/`duration_seconds` match the TypeScript `LoadStage` in Task 5 and the raw JSON bodies in Task 4's tests. `StageDraft` (strings) and `LoadStage` (numbers) are deliberately distinct types, converted only at the submit boundary in Task 7. `TestField` loses `durationSeconds` in Task 6 and every consumer is updated in Task 7. `emptyStage()` is defined in Task 6 and consumed in Task 7.
- **A correction made during review.** Task 4's first draft kept `valid() bool` and a single `testValidationMessage` const. That cannot express eight distinct failures, and the spec's error table requires distinct messages because the frontend renders them verbatim. Changed to `validate() (string, bool)`, and the const is deleted rather than left orphaned.
- **Risk.** The riskiest unknown is jpgc's XML shape, which is why Task 1 verifies it against a real JMeter before any Go code depends on it, and Task 3 Step 6 re-verifies the templated output. If Task 1 Step 4 fails, do not proceed to Task 2 — the whole plan's XML fragment is wrong and every later task would encode the error.
- **Second risk.** Task 2 Step 6 changes a column projection that three `SELECT` statements share. A `load_stages` added to `testColumns` but missed in `ListTests`'s or `ListTestsForProject`'s explicit column list produces a scan misalignment, which surfaces as a confusing type error on an unrelated field rather than as a missing column. The step says to search for every occurrence.
