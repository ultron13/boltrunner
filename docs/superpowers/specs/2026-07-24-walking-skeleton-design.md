# BoltRunner Walking Skeleton — Design

**Status:** Approved
**Date:** 2026-07-24
**Increment:** First agile increment — thinnest possible end-to-end slice

## Background

BoltRunner is an independent, from-scratch open-source alternative to OpenText/Micro Focus
LoadRunner Enterprise. The full long-range vision (Kubernetes-native execution, multi-engine
support, AI-assisted analysis, multi-region, enterprise governance) is captured in
`Implementation/` (33 docs covering architecture, functional/non-functional requirements,
microservices, RBAC, observability, CI/CD, and a full implementation backlog).

Research inputs:
- OpenText's CE 25.3 LoadRunner Enterprise release video: highlights Splunk APM integration, a
  refreshed UI, AI-assisted script generation ("Performance Engineering Aviator"), expanded cloud
  compatibility, accessibility, and security improvements.
- A 2020 "Micro Focus SaaS LoadRunner Enterprise — Tenant Administration" video, confirming the
  tenant/project administration model that `Implementation/09-RBAC.md` and
  `02-Functional-Requirements.md` already describe.

A separate, independently-developed project called **speedRacer** exists on this machine
(`~/DevOps_Projects/speedRacer`) implementing a large surface of this same vision (Go backend,
Next.js frontend, Postgres/Redis, K8s operator, CI). BoltRunner is a **deliberate, separate
rewrite** — no code or infrastructure is shared or reused from speedRacer.

The full `Implementation/` backlog (`32-Full-Project-Implementation-Backlog.md`) describes a
multi-phase enterprise platform. Building any single phase from that backlog directly would
still be too large for a first increment. This spec instead defines a **walking skeleton**: the
smallest possible slice that exercises every architectural layer end-to-end, so later increments
extend proven seams instead of integrating them for the first time under pressure.

## Goal

A user can:
1. Create a test (name, target URL, virtual users, duration).
2. Start a run.
3. Watch live metrics (throughput, avg response time, error rate) update in a browser while the
   test executes as a real JMeter pod on a local Kubernetes cluster.
4. See a final summary once the run completes.

## Decisions

| Decision | Choice | Why |
|---|---|---|
| Execution runtime | Local Kubernetes (`kind`/`minikube`) | Proves the real K8s-native architecture from day one instead of a throwaway shortcut. |
| Backend language | Go | Matches the K8s ecosystem (`client-go`), strong fit for controller-style code. |
| Frontend | Next.js + React | Mainstream dashboard ecosystem; independent codebase from speedRacer's frontend. |
| Live updates | Polling (`GET /api/runs/{id}` every 1-2s) | Simplest to build/debug; can move to WebSocket/SSE later without changing the data model. |
| Durable storage | PostgreSQL from day one | Matches target architecture; avoids a storage-layer rewrite once auth/RBAC/audit land. |
| Auth | None for this increment | Keeps scope on proving the execution path; auth is a separate, well-scoped later increment. |
| Metrics extraction | Sidecar reporter container | Tails the JTL file, computes rolling aggregates, POSTs snapshots to the backend. No special K8s exec/RBAC permissions needed, and the "parse engine output → post a standard snapshot" contract generalizes cleanly to future engines (k6, Gatling, Locust). |

## Architecture

A monorepo: `backend/` (Go), `frontend/` (Next.js), `deploy/` (kind config + K8s manifests).

The backend is **one deployable service** for this increment — not yet split into the docs'
separate Portal/Gateway/Scheduler/Controller microservices. That split is premature until there's
more than one real workflow to justify the boundaries.

Each run's pod has two containers:
- **JMeter**: runs a generated test plan (`jmeter -n -t plan.jmx -l results.jtl`) against the
  target URL.
- **Sidecar reporter**: a small Go binary that tails `results.jtl`, computes rolling
  throughput/avg-response-time/error-rate every ~1s, and POSTs snapshots to the backend.

## Data model

```
Test
  id, name, target_url, virtual_users, duration_seconds, created_at

Run
  id, test_id, status (pending|running|completed|failed|stopped),
  started_at, completed_at, error_message

RunMetricSnapshot
  id, run_id, ts, elapsed_seconds,
  throughput_rps, avg_response_time_ms, error_rate_pct, sample_count
```

## API

- `POST /api/tests` — create a test.
- `GET /api/tests` — list tests.
- `POST /api/tests/{id}/runs` — start a run (generates the `.jmx`, builds and submits the K8s
  Job, creates a `Run` row in `pending`).
- `GET /api/runs/{id}` — current status + latest metrics + history (used for polling).
- `POST /api/runs/{id}/metrics` — internal ingest endpoint the sidecar posts snapshots to.
- `POST /api/runs/{id}/cancel` — deletes the K8s Job, marks the run `stopped`.

## Data flow

1. User submits the "New Test" form → `POST /api/tests` → row in Postgres.
2. User clicks Run → `POST /api/tests/{id}/runs` → backend generates a parameterized `.jmx` from
   a fixed template, builds a K8s Job manifest, submits it via `client-go`, creates a `Run` row
   (`pending`).
3. Backend flips the run to `running` once the pod is observed.
4. The sidecar posts a metrics snapshot every ~1s; the backend stores the latest snapshot and
   appends it to `RunMetricSnapshot` history.
5. The frontend polls `GET /api/runs/{id}` every 1-2s and renders live summary cards + a chart.
6. When JMeter exits, the sidecar posts a final snapshot. A lightweight backend watcher
   (polling K8s Job status) independently flips the run to `completed`/`failed` based on the
   Job's actual exit state — never solely on the sidecar's last message, so a dropped POST can't
   strand a run as "running" forever.
7. The UI shows the final run summary pulled from `RunMetricSnapshot` history.

## Error handling

- **Unschedulable pod** (no cluster capacity): run stays `pending`; after a 30s timeout the
  backend marks it `failed` with reason `"unschedulable"` instead of hanging indefinitely.
- **JMeter exits non-zero**: the sidecar's final snapshot carries `status=failed` plus the last
  lines of JMeter's stderr; the UI shows a failed state with that error text.
- **Sidecar can't reach the backend** (transient network issue): it retries with backoff. Run
  status never depends solely on the sidecar succeeding — the Job-status watcher independently
  determines `completed`/`failed` from the real K8s Job state. Worst case is a completed run with
  incomplete metric history, never a stuck run.
- **User cancels a running test**: `POST /api/runs/{id}/cancel` deletes the K8s Job (killing the
  pod); status set to `stopped`.

## Testing

- **Backend unit tests**: run state-machine transitions; JTL parsing/aggregation as a pure
  function against fixture files; Job-manifest generation (assert on the built object, no live
  cluster required).
- **Integration test (CI)**: spin up `kind`, target a trivial echo-server, run one full
  create → start → poll → complete cycle against the real cluster, assert final metrics are
  non-zero and status is `completed`.
- **Frontend**: component tests for the create-test form and live-metrics chart against mocked
  API responses.
- **E2E**: one Playwright test driving the full flow (create → start → watch live metrics →
  completion) against a real backend + `kind` cluster.

## Explicitly out of scope for this increment

Deferred to later, well-scoped increments — not because they're unimportant, but because
including them now would defeat the purpose of a *walking* skeleton:

- Authentication, RBAC, audit logging.
- Multiple projects/teams/environments (only a flat list of tests for now).
- Multiple engines (k6, Gatling, Locust, Playwright) — JMeter only.
- Arbitrary test-script upload — the `.jmx` is generated from one fixed template.
- Load generator pools, capacity reservation, scheduling/reservation windows.
- SLA thresholds, baseline/trend comparison, enterprise reporting.
- WebSocket/SSE live updates (polling only).
- CI/CD integration, GitOps configuration, multi-region execution.
- AI-assisted features of any kind.

## Next step

Convert this spec into an implementation plan via the `writing-plans` skill, then execute it.
Once the walking skeleton is working, the resulting backlog of next increments (drawn from
`Implementation/32-Full-Project-Implementation-Backlog.md`, re-scoped realistically) will be
turned into a Jira backlog via the Atlassian MCP/`spec-to-backlog` skill for agile tracking.
