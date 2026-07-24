# BoltRunner

An open-source, Kubernetes-native alternative to LoadRunner Enterprise. See `Implementation/`
for the full long-range platform vision, and `docs/superpowers/specs/` for the design of the
current increment.

## Current increment: walking skeleton

Create a test, run it as a real JMeter pod on a local Kubernetes cluster, watch live metrics,
see a final summary. No auth, one engine (JMeter), one fixed test-plan template — see
`docs/superpowers/specs/2026-07-24-walking-skeleton-design.md` for the full design and
explicitly out-of-scope list, and `docs/superpowers/plans/2026-07-24-walking-skeleton.md`
for the implementation plan this was built from.

## Local development

Prerequisites: Go 1.26+, Node 20+, Docker, `kind`, `kubectl`.

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
cd backend && go test ./...                                   # unit tests
cd frontend && npm test                                        # component tests
cd frontend && npm run test:e2e                                 # e2e (needs the stack from dev-up.sh + npm run dev running)
cd backend && go test -tags=integration ./internal/integration/...   # needs a live backend (see CI workflow)
```

## Architecture

Go backend (single deployable service) backed by PostgreSQL, submitting Kubernetes Jobs via
client-go. Each Job pod runs a JMeter container plus a Go sidecar container that tails JMeter's
`.jtl` output and POSTs metric snapshots back to the backend. A Job-status watcher independently
reconciles run status from the real Kubernetes Job state, so a run can never get stuck if the
sidecar's metrics POST fails. The Next.js frontend polls the backend for status/metrics.

See `docs/superpowers/specs/2026-07-24-walking-skeleton-design.md` for the full design.
