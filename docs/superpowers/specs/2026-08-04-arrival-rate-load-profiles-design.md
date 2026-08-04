# Arrival-Rate Load Profiles

Supersedes `2026-08-04-load-profile-ramp-up-design.md`, which was written and committed earlier
today and never implemented. That spec exposed the thread group's ramp as a single
`ramp_up_seconds` field. It is replaced rather than extended: this design changes the workload
model itself, which makes a concurrency ramp the wrong knob.

## Context

BoltRunner generates a JMeter plan from three values — target URL, virtual users, duration
(`backend/internal/jmx/template.go`). Two problems compound in that plan.

The thread group's ramp is hardcoded:

```xml
<stringProp name="ThreadGroup.ramp_time">1</stringProp>
```

so every run starts all its virtual users within a second. And the plan is a **closed** workload
model: N threads loop as fast as the system under test allows. Under a closed model, load is an
output, not an input — if the target slows down, offered load falls with it, and the system is
never pushed past the point where it started struggling. That is the opposite of what a load test
is usually asked to find out.

This replaces it with an **open** model: requests arrive at a scheduled rate regardless of
whether the system keeps up. Rate becomes an input the tester specifies, and falling behind
becomes an observable result rather than a silent feedback loop.

## Decisions made during brainstorming

1. **Arrival rate replaces the concurrency model rather than coexisting with it.** Every test
   becomes rate-based. `virtual_users` survives with a new meaning (decision 4).

   Rejected: *a `load_model` discriminator* with both models and conditional UI, and *an optional
   `target_rps` where zero means closed model*. Both preserve the 8 live tests' meaning exactly
   and cost a branch in every layer. Replacement was chosen deliberately, accepting that
   closed-model testing — "how many concurrent users can this hold" — is no longer expressible.

2. **The rate is scheduled, not constant, which requires a plugin.** Vanilla JMeter's
   `PreciseThroughputTimer` and `ConstantThroughputTimer` both hold a fixed rate; neither ramps.
   A constant arrival rate is the same flat-wall shape that motivated this work, only measured in
   requests instead of users. So the JMeter image gains jpgc's Throughput Shaping Timer.

   Rejected: *constant rate on vanilla now, scheduling later*. It avoids a third-party JAR, but
   the flat field it ships would be superseded by the stages model within one slice.

3. **A stage is `{start_rps, end_rps, duration_seconds}` — explicit, not k6-style.** A hold is
   `start == end`; a ramp is `start != end`.

   Rejected: *k6's `{target, duration}`*, where each stage ramps implicitly from the previous
   stage's target. Terser and more familiar, but it maps indirectly onto what the timer consumes —
   jpgc's rows are literally start/end/duration — so every stage needs derivation, the first
   stage needs a special rule about whether it ramps from zero, and anyone opening the generated
   JMX sees something that does not match what they typed. The explicit form also makes the
   backfill in decision 5 exact rather than approximate.

4. **`virtual_users` is redefined, not removed: it becomes the thread-pool ceiling.** Under an
   open model it no longer sets the load; it bounds the concurrency available to serve the rate.
   A ceiling too low for the target rate means actual throughput falls short — which the existing
   metrics pipeline already surfaces, since it reports observed throughput.

5. **Existing tests backfill to one flat stage at `rate = virtual_users`.** Defensible via
   Little's Law: N users with roughly one-second responses produce roughly N requests per second,
   so the number lands in the right order of magnitude and every test stays runnable.

   Rejected: *a fixed default rate for all*, which flattens 8 tests of differing intent into
   identical load so their names stop describing them; and *an empty schedule that makes them
   unrunnable until edited*, which is the most truthful option but breaks the walking-skeleton
   e2e and the Go integration test until a human opens each test.

   This does change what each existing test measures. That is inherent to replacing the model,
   not a flaw in the backfill.

6. **`duration_seconds` becomes derived from the stages.** It stops being an input and is
   computed server-side as the sum of stage durations, still stored so the thread group's
   scheduler and the UI have it.

   Rejected: *keeping it as an independent run window*, where a schedule shorter than the window
   means load silently drops to zero for the remainder and a longer one is truncated, with
   nothing in the UI explaining which happened. Also rejected: *requiring callers to send a value
   that must equal the sum*, which makes every caller compute a number the server already knows
   and forces a duration edit alongside every stage edit.

## Architecture

### The JMeter image — `deploy/Dockerfile.jmeter`

Two JARs into `/opt/apache-jmeter-${JMETER_VERSION}/lib/ext/`:

- **Throughput Shaping Timer** (`jpgc-tst`) — the timer itself
- **jpgc common** (`jmeter-plugins-cmn-jmeter`) — its runtime dependency

**Pin an exact version and verify a SHA-256 for each**, recorded in the Dockerfile next to the
download. Do not use the JMeter Plugins Manager: it resolves over the network at build time,
which makes the image non-reproducible and puts an unpinned third-party fetch in the build path.

This is the project's first third-party runtime dependency.

The exact versions and checksums are deliberately not stated here: they must match what is
actually published and compatible with JMeter 5.6.3, and a version number asserted from memory
would be worse than none — it would look authoritative and send the implementer chasing a
download that may not exist. Resolve them from Maven Central (`kg.apc:jmeter-plugins-tst` and
`kg.apc:jmeter-plugins-cmn-jmeter`), take the latest release compatible with 5.6.3, compute the
SHA-256 of each downloaded JAR, and record both the version and the checksum in the Dockerfile so
the next build verifies rather than trusts. The implementation report must state which versions
were pinned.

### Schema — `backend/internal/store/postgres/migrations/0006_load_stages.sql`

```sql
ALTER TABLE tests ADD COLUMN IF NOT EXISTS load_stages JSONB NOT NULL DEFAULT '[]'::jsonb;

-- One flat stage holding the old virtual-user count as a rate, for the old duration.
-- Guarded so a re-run cannot double-apply.
UPDATE tests SET load_stages = jsonb_build_array(jsonb_build_object(
    'start_rps', virtual_users,
    'end_rps', virtual_users,
    'duration_seconds', duration_seconds))
WHERE load_stages = '[]'::jsonb;
```

JSONB rather than a child table: `tests` rows are immutable per version
(`0004_test_versioning.sql`), so a stage list belongs to a version's configuration and is always
read and written as a unit. A child table would need per-version copies plus a join on every
read, buying nothing. Copy-on-write versioning then carries stages forward with no extra work.

### Model — `backend/internal/model/model.go`

```go
// LoadStage is one row of the arrival-rate schedule. A hold is start == end;
// a ramp is start != end.
type LoadStage struct {
	StartRPS        int `json:"start_rps"`
	EndRPS          int `json:"end_rps"`
	DurationSeconds int `json:"duration_seconds"`
}
```

`Test` gains one field:

```go
	LoadStages []LoadStage `json:"load_stages"`
```

`VirtualUsers` keeps its name and gains a comment recording its new meaning as the thread-pool
ceiling. `DurationSeconds` gains a comment recording that it is derived from the stages and not
accepted as input.

### Test plan — `backend/internal/jmx/template.go`

`jmx.Params` gains `LoadStages []LoadStage` and keeps `VirtualUsers` (thread count) and
`DurationSeconds` (scheduler window, now derived).

The template gains a Throughput Shaping Timer inside the thread group's `hashTree`, one row per
stage:

```xml
<kg.apc.jmeter.timers.VariableThroughputTimer
    guiclass="kg.apc.jmeter.timers.VariableThroughputTimerGui"
    testclass="kg.apc.jmeter.timers.VariableThroughputTimer"
    testname="BoltRunner Load Profile" enabled="true">
  <collectionProp name="load_profile">
    {{range .LoadStages}}<collectionProp name="stage">
      <stringProp name="start">{{.StartRPS}}</stringProp>
      <stringProp name="end">{{.EndRPS}}</stringProp>
      <stringProp name="dur">{{.DurationSeconds}}</stringProp>
    </collectionProp>
    {{end}}
  </collectionProp>
</kg.apc.jmeter.timers.VariableThroughputTimer>
<hashTree/>
```

The inner `stringProp` names are positional — JMeter reads a `collectionProp`'s children in
document order — so stable placeholders are used rather than the value hashes the JMeter GUI
writes. **This shape must be verified against the pinned plugin version rather than trusted from
this spec**; the walking-skeleton e2e is the check, because it runs a real JMeter pod and a plan
JMeter cannot parse fails there loudly.

`ThreadGroup.ramp_time` stays as it is. Under an open model it only staggers pool startup and no
longer shapes load, so it is not worth a field.

### Store — both backends

`load_stages` joins the shared `testColumns` projection and `scanTest`, marshalled as JSONB in
postgres and copied as a slice in memstore. **memstore must deep-copy the slice on write and on
read**, or two versions of a test would share one backing array and an edit would silently mutate
history — the exact property the versioning work exists to protect.

### API — `backend/internal/api/tests.go`

`testRequest` gains `load_stages` and changes `duration_seconds` to `*int`, so absent and zero
are distinguishable and a request that sends it can be rejected rather than having it silently
ignored. That silent-ignore is precisely the trap `testRequest.ProjectID` set on the update path
in the previous slice.

Validation, shared by create and update via the existing `valid()`:

- `load_stages` non-empty, at most 20 stages
- every stage: `duration_seconds > 0`, `start_rps >= 0`, `end_rps >= 0`, and at least one rate above 0
- `virtual_users > 0`
- `duration_seconds` absent

The derived total is computed in the **handler**, before the store call, so both backends agree
by construction rather than by two implementations happening to match.

## Error handling

| Case | Code | Body |
|---|---|---|
| `load_stages` missing or empty | 400 | `load_stages must contain at least one stage` |
| more than 20 stages | 400 | `load_stages must contain 20 stages or fewer` |
| a stage with `duration_seconds` ≤ 0 | 400 | `each stage needs a duration_seconds greater than 0` |
| a stage with a negative rate, or both rates 0 | 400 | `each stage needs a non-negative start_rps and end_rps, with at least one above 0` |
| `duration_seconds` present in the body | 400 | `duration_seconds is derived from load_stages; do not send it` |
| everything else | unchanged | existing create/update/versioning behaviour is untouched |

## Frontend

### `frontend/components/TestFields.tsx`

A repeatable stage editor: rows of *from N RPS → to M RPS over D seconds*, with add and remove.
At least one row is always present — Remove is disabled when only one row remains, rather than
allowing an empty list the API would reject. Beneath it, a live derived total — *"5 stages,
12m30s total"* — so the number the server will compute is visible before submitting.

The 20-stage cap is enforced server-side only. Duplicating it in the form would be a second place
to keep in sync for a limit no realistic profile approaches.

`virtual_users` is relabelled **Max threads**, with helper text stating it caps concurrency rather
than setting load. Without that, the field's redefinition is invisible and every existing user
reads it as the load level it used to be.

### `frontend/lib/api-client.ts`

`Test` gains `load_stages: LoadStage[]`. `CreateTestInput`/`UpdateTestInput` gain it and **drop
`duration_seconds`**, so a caller cannot send the field the API now rejects.

## Testing strategy

**The plugin actually loading is the load-bearing test.** The walking-skeleton e2e runs a real
JMeter pod against the built image; a missing JAR, a version incompatible with 5.6.3, or a
malformed timer element fails there. No unit test substitutes for it, and it is the reason the
e2e must run against a freshly built image rather than a cached one.

**Template.** Multi-row schedules render in stage order; a single stage renders one row; rates of
0 render as `0` rather than empty.

**Migration.** Against a real Postgres: existing rows backfill to exactly one flat stage whose
rates equal the old `virtual_users` and whose duration equals the old `duration_seconds`; a
re-run of the body is a no-op.

**Store.** JSONB round-trip in postgres; slice copy in memstore, including the aliasing case —
edit a test and assert the earlier version's stages are unchanged.

**API.** Every row of the error table, plus the derived total matching the sum of stage
durations, and an update carrying stages onto a new version.

**Frontend.** Adding and removing rows, the always-one-row floor, the derived total updating as
inputs change, and the relabelled thread field.

Both 88% gates hold: backend on the `go tool cover -func` total, frontend on lines, statements,
functions and branches.

## Out of scope

- **Closed-model / concurrency testing.** Removed by decision 1.
- **Per-stage thread ceilings.** One ceiling covers the whole run.
- **A visual profile preview chart.** The derived text total covers the comprehension need; a
  chart is a separate design question.
- **Poisson batching and distribution tuning** on the timer.
- **Ramping the thread pool.** `ThreadGroup.ramp_time` stays hardcoded; see the template section.
