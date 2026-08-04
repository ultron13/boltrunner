# Load Profiles — Ramp-Up

## Context

BoltRunner generates a JMeter plan from three values: target URL, virtual users, duration
(`backend/internal/jmx/template.go`). The thread group's ramp time is not one of them — it is
hardcoded:

```xml
<stringProp name="ThreadGroup.ramp_time">1</stringProp>
```

So every test starts all its virtual users within one second and holds them flat until the
scheduler stops the run. That is a thundering herd, and it distorts results before any other
variable is considered: connection pools, JIT warm-up and autoscalers all behave differently
under an instantaneous step than under a ramp, and the first seconds of every run are measuring
the step rather than the system.

This is BOL-52's first phase. It closes the gap between "runs load" and "runs a load profile"
with the smallest change that is genuinely useful.

## Decisions made during brainstorming

1. **Ramp-up and hold only, on vanilla JMeter.** The standard `ThreadGroup` already supports
   ramp via `ramp_time`, so exposing it costs one template variable and no new dependencies.

   Rejected: *stepped multi-stage profiles* — ramp to 50, hold, step to 200, hold, ramp down.
   That is what LoadRunner Enterprise does and where this project is headed, but it needs the
   jpgc Custom Thread Groups plugin (`ConcurrencyThreadGroup`) baked into the JMeter image, a
   stages collection on the model rather than two integers, and a repeatable-row editor in the
   UI — roughly double this slice, plus a third-party JAR in the runtime image. It deserves its
   own decision on its own evidence.

   Also rejected: *ramp-down by stacking a second standard thread group with a delay*. No plugin
   needed, but stacked standard groups are additive, so "how many users are running right now"
   stops being obvious to us and to the user. Muddy semantics are worse than a missing feature.

2. **`duration_seconds` keeps JMeter's meaning: the total window, ramp included.** Ramp 60 with
   duration 300 is 60 seconds ramping and 240 at full load. It maps 1:1 onto
   `ThreadGroup.duration`, so nothing is computed behind the user's back and the number in the UI
   is the number in the generated plan.

   Rejected: *redefining it as hold time* and computing `ramp + hold` for the JMX. Arguably
   closer to what someone means by "run for 5 minutes at 200 users", but it silently changes the
   meaning of a field on every existing test — each would start running longer than it does
   today — and it breaks the correspondence between the UI value and the generated plan.

   Also rejected: *renaming the field* to `hold_seconds` or `total_seconds`. Unambiguous, but a
   breaking API change plus a migration rename touching the walking-skeleton e2e, the integration
   test and every fixture — a lot of churn for a wording improvement.

3. **Existing rows backfill to `0`, not `1`.** `0` is the value that means "no ramp". Preserving
   the hardcoded `1` would carry a magic number forward into a column whose whole purpose is to
   make the ramp explicit. The behavioural difference is one second in how threads start, which
   at any realistic virtual-user count is the same instantaneous step.

## Architecture

### Schema — `backend/internal/store/postgres/migrations/0006_test_ramp_up.sql`

```sql
ALTER TABLE tests ADD COLUMN IF NOT EXISTS ramp_up_seconds INTEGER NOT NULL DEFAULT 0;
```

`tests` rows are immutable per version (`0004_test_versioning.sql`), so ramp is per-version
configuration like every other field: an edit cuts a new version carrying its own ramp, and a run
stays pinned to the exact ramp it executed. No extra work is needed for that — it follows from
the column living on `tests`.

### Model — `backend/internal/model/model.go`

`Test` gains one field:

```go
	RampUpSeconds int `json:"ramp_up_seconds"`
```

### Test plan — `backend/internal/jmx/template.go`

The change the slice exists for:

```diff
- <stringProp name="ThreadGroup.ramp_time">1</stringProp>
+ <stringProp name="ThreadGroup.ramp_time">{{.RampUpSeconds}}</stringProp>
```

`jmx.Params` gains `RampUpSeconds int`. `ThreadGroup.duration` stays bound to `DurationSeconds`
unchanged — that binding is what keeps duration total, per decision 2.

`handleStartRun` (`backend/internal/api/runs.go:35`) passes the field through when it builds
`jmx.Params` from the resolved test version.

### Store — both backends

`CreateTest`, `updateTestAtVersion`, the shared `testColumns` projection and `scanTest` all carry
the new column. memstore mirrors it. Nothing about the version-pinning or project-scoping paths
changes.

### API — `backend/internal/api/tests.go`

`testRequest` gains `ramp_up_seconds`. Validation extends the existing `valid()`, which is shared
by create and update — so both routes enforce it and cannot drift:

- `ramp_up_seconds >= 0`
- `ramp_up_seconds < duration_seconds`

Rejecting `ramp == duration` is deliberate. It is the guard against the footgun decision 2
accepts: a plan whose entire window is spent ramping never reaches target load, so the run
produces a graph that looks like a mistake and is one.

## Error handling

| Case | Code | Body |
|---|---|---|
| `ramp_up_seconds` negative, or ≥ `duration_seconds` | 400 | `ramp_up_seconds must be 0 or more and less than duration_seconds` |
| Everything else | unchanged | the existing create/update/validation behaviour is untouched |

`POST /api/tests` and `PUT /api/tests/{id}` that omit `ramp_up_seconds` decode it as `0`, which
is valid — so existing clients keep working without change.

## Frontend

### `frontend/components/TestFields.tsx`

A **Ramp-up (seconds)** number input, so create and edit both inherit it from the shared
component and cannot drift.

### The derived summary

Beneath the fields, one line computed from the three values:

> Ramps to 200 users over 60s, then holds for 240s.

This is not decoration. It is what makes "duration is total" visible *before* submitting, and it
is the cheapest defence against someone entering ramp 300 / duration 300 and wondering why the
run did nothing. It recomputes as the user types, and it reads from the same values the form will
send, so it cannot describe a plan different from the one submitted.

When ramp is `0` the line reads:

> Starts all 200 users at once, then holds for 300s.

— so it never claims a ramp that isn't there. Below `0`, or at a ramp the API would reject, the
line is suppressed rather than describing a plan that cannot be submitted.

## Testing strategy

**Template.** `jmx/template_test.go` asserts the ramp value reaches the generated XML, and pins
`0` rendering as the literal `0`. An `int` always renders, so that second case is not guarding
today's code — it guards the field's *type*. If `RampUpSeconds` ever becomes a `*int` or a
`string` to model "unset", the zero value would render as empty, and an empty `ramp_time` is a
JMeter parse failure rather than a default. The test turns that into a red build instead of a
run that dies in the pod.

**Store.** Round-trip in both backends, plus the version case: editing a test to a new ramp
leaves the earlier version's ramp intact, which is what keeps a completed run's configuration
truthful.

**API.** The validation boundaries — `0` accepted, `duration - 1` accepted, `duration` rejected,
negative rejected — and that an omitted field decodes to `0` and is accepted, since that is what
existing clients send.

**Migration.** Existing rows backfill to `0`, exercised against a real Postgres in the pattern
`migrate_test.go` already uses.

**Frontend.** The field renders, submits its value, and the derived summary updates as the inputs
change, including the ramp-`0` wording.

**E2E.** Create a test with a ramp and confirm the value survives to the detail page. This is the
only assurance the field is wired end to end through a real backend.

Both 88% coverage gates hold: backend on the `go tool cover -func` total, frontend on lines,
statements, functions and branches.

## Out of scope

- **Stepped multi-stage profiles and the jpgc plugin.** See decision 1. This is the natural next
  slice.
- **Ramp-down.** Same reason; the standard thread group cannot express it cleanly.
- **Arrival-rate (throughput) profiles.** A different model of load entirely — target requests
  per second rather than target concurrency.
- **Per-stage virtual-user targets.** Requires the stages collection decision 1 defers.
- **Renaming `duration_seconds`.** See decision 2.
