# Project-Scoped Run History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/history` follow the selected workspace, leaving `?testId=` deep links unscoped — per `docs/superpowers/specs/2026-07-29-project-scoped-run-history-design.md`.

**Architecture:** `HistoryPage` already derives runs from the test list rather than querying runs directly, and `listTests` already accepts an optional project filter. So the whole change is choosing which test list to fetch: scoped when browsing, unscoped when a `?testId=` names one test. One source file, no backend work.

**Tech Stack:** Next.js App Router (client components), React 18, TypeScript `strict`, Vitest + Testing Library, Playwright. No new dependencies.

## Global Constraints

- **Frontend only.** No file under `backend/` changes. Verify with `git diff --stat <base>..HEAD -- backend/` **run from the repository root** — a stale shell cwd makes that pathspec resolve to `frontend/backend/` and report a false clean.
- **No existing assertion changes.** `HistoryPage.test.tsx`'s four existing cases keep their assertions exactly. They gain only the `useProjects` stub described in Task 1 Step 1, which is the mechanical edit the multi-project work already applied to five other files.
- **`?testId=` must stay unscoped.** Spec decision 1. `TestList` and `TreeNav` link to `/history?testId=`, and a bookmarked link must resolve whichever workspace is selected. A regression here renders a blank page with no explanation.
- **`GET /api/tests` without `project_id` stays unfiltered**, which is what the unscoped branch relies on.
- **Coverage gate: 88%** on lines, statements, functions and branches (`vitest.config.ts`). Not to be lowered.
- **Run the frontend commands from `frontend/`.** `npx vitest` invoked from the repository root picks up a different vite and fails to parse JSX; the failure looks like a syntax error in the test file rather than a wrong-directory mistake.

---

### Task 1: Scope the history fetch to the selected project

**Files:**
- Modify: `frontend/app/history/page.tsx:12-39`
- Test: `frontend/__tests__/HistoryPage.test.tsx`

**Interfaces:**
- Consumes: `useProjects()` from `@/components/ui/ProjectProvider`, returning `{ projects, selectedId, selected, select, create }`; `listTests(projectId?: string)` from `@/lib/api-client`.
- Produces: nothing. No other module imports from this page.

- [ ] **Step 1: Add the hook stub to the existing cases**

`HistoryPage` will call `useProjects()`, which throws outside a provider. The two new cases need different selections, so the stub reads a mutable object rather than a fixed value.

Declare that object with `vi.hoisted`, so it exists before the hoisted `vi.mock` factory runs — a plain `let` is in its temporal dead zone at that point and throws `Cannot access before initialization`. It must be an **object** whose property the factory reads at call time; destructuring to a bare binding would give the factory a copy that later reassignment never updates.

Add this immediately after the `next/navigation` mock in `frontend/__tests__/HistoryPage.test.tsx`:

```ts
const projectState = vi.hoisted(() => ({ selectedId: null as string | null }));

vi.mock('@/components/ui/ProjectProvider', () => ({
  useProjects: () => ({
    projects: [],
    selectedId: projectState.selectedId,
    selected: null,
    select: vi.fn(),
    create: vi.fn(),
  }),
}));
```

Reset it in the existing `afterEach`, alongside the calls already there:

```ts
  afterEach(() => {
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams());
    projectState.selectedId = null;
    vi.restoreAllMocks();
  });
```

- [ ] **Step 2: Run the existing cases to confirm the stub is inert**

```bash
cd frontend && npx vitest run __tests__/HistoryPage.test.tsx
```

Expected: PASS, all existing cases, unchanged. The page does not call `useProjects()` yet, so the stub does nothing — this run proves the stub itself broke nothing before any behavior changes.

- [ ] **Step 3: Write the failing tests**

Append inside the `describe('HistoryPage', ...)` block:

```tsx
  it('scopes the fetch to the selected project when browsing', async () => {
    projectState.selectedId = 'p2';
    const listTests = vi.spyOn(api, 'listTests').mockResolvedValue([]);

    render(<HistoryPage />);

    await waitFor(() => expect(listTests).toHaveBeenCalledWith('p2'));
  });

  // A ?testId= link is an explicit request for one test's history. It must
  // resolve whichever workspace is selected, or a bookmarked link renders blank
  // with no explanation.
  it('ignores the project filter when a testId is present', async () => {
    projectState.selectedId = 'p2';
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams('testId=t9'));
    const listTests = vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: 't9', name: 'Elsewhere', target_url: 'http://x', virtual_users: 1, duration_seconds: 1, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([
      { id: 'r9', test_id: 't9', status: 'completed', created_at: '2026-07-24T00:00:01Z' },
    ]);

    render(<HistoryPage />);

    // The run renders even though t9 is not in the selected project.
    expect(await screen.findByRole('row', { name: /r9/i })).toBeInTheDocument();
    expect(listTests).toHaveBeenCalledWith();
  });
```

Add `waitFor` to the existing `@testing-library/react` import:

```ts
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
```

`expect(listTests).toHaveBeenCalledWith()` asserts the call had **no arguments**, which is what distinguishes the unscoped branch from `listTests(undefined)`. Both would reach the same endpoint, but the distinction keeps the two branches legible in the test.

- [ ] **Step 4: Run to verify they fail**

```bash
cd frontend && npx vitest run __tests__/HistoryPage.test.tsx
```

Expected: the scoped case FAILS with `listTests` called with no arguments instead of `'p2'`. The `testId` case PASSES already — the page is unscoped today, so its behavior is coincidentally correct. Keep it: it is what pins the branch once the other case forces the change.

- [ ] **Step 5: Implement**

In `frontend/app/history/page.tsx`, add the import:

```ts
import { useProjects } from '@/components/ui/ProjectProvider';
```

Read the selection alongside the existing hooks:

```ts
  const searchParams = useSearchParams();
  const testId = searchParams.get('testId');
  const { selectedId } = useProjects();
```

Replace the first line of `load()` and the dependency array:

```ts
  useEffect(() => {
    async function load() {
      // An explicit ?testId= is a request for one test's history and must
      // resolve whichever workspace is selected, so it skips the filter.
      const tests: Test[] = testId ? await listTests() : await listTests(selectedId ?? undefined);
      const filtered = testId ? tests.filter((t) => t.id === testId) : tests;
      // ... everything below is unchanged
    }
    load().catch(() => setLoaded(true));
  }, [testId, selectedId]);
```

Change nothing else. The `Promise.allSettled` fan-out, the merge, the sort, the columns and the row click handler all stay exactly as they are.

- [ ] **Step 6: Run the tests**

```bash
cd frontend && npx vitest run __tests__/HistoryPage.test.tsx
```

Expected: PASS, all six cases.

- [ ] **Step 7: Run the whole unit suite**

```bash
cd frontend && npx vitest run
```

Expected: PASS. Baseline before this plan is 157 tests across 31 files; this task adds 2. Any failure outside `HistoryPage.test.tsx` means something other than this page consumed the changed behavior — investigate rather than adjusting the failing assertion.

- [ ] **Step 8: Commit**

```bash
git add frontend/app/history/page.tsx frontend/__tests__/HistoryPage.test.tsx
git commit -m "feat(frontend): scope the run history to the selected project"
```

---

### Task 2: End-to-end coverage and the full gate

**Files:**
- Modify: `frontend/e2e/project-workspaces.spec.ts`
- Test: the whole suite

**Interfaces:**
- Consumes: Task 1's behavior.
- Produces: nothing.

- [ ] **Step 1: Extend the existing e2e spec**

In `frontend/e2e/project-workspaces.spec.ts`, inside the first test (`create a project, switch to it, and scope tests to it`), immediately after the assertion that the created test row is visible and **before** switching back to Default, add:

```ts
  // Run history follows the selection too: this fresh project has a test but no
  // runs, so its history is empty. If scoping regressed, the runs the other
  // specs create in Default would appear here.
  await page.getByRole('link', { name: 'Test Runs' }).click();
  await expect(page).toHaveURL(/\/history/);
  await expect(page.getByText('No runs yet.')).toBeVisible();

  await page.getByRole('link', { name: 'Dashboard' }).click();
  await expect(page).toHaveURL(/\/$|\/\?/);
```

The navigation back to Dashboard matters: the rest of that test asserts on the test table, which only exists on the dashboard.

- [ ] **Step 2: Check the coverage gate**

```bash
cd frontend && npm run test:coverage
```

Expected: PASS with lines, statements, functions and branches all ≥ 88%. The previous run was 98.04 / 96.19 / 97.85 / 99.19, and this task adds a branch plus two tests covering it, so there is ample headroom.

- [ ] **Step 3: Typecheck and build**

```bash
cd frontend && npm run build
```

Expected: success.

- [ ] **Step 4: Confirm the backend is untouched**

```bash
git -C "$(git rev-parse --show-toplevel)" diff --stat "$(git merge-base HEAD main)"..HEAD -- backend/
```

Expected: **empty output.** The `git -C` prefix is deliberate — running this from `frontend/` resolves the pathspec to `frontend/backend/` and prints nothing regardless of what changed, which reads as a pass and is not one.

- [ ] **Step 5: Run the browser suite against a real backend**

```bash
docker run -d --rm --name br-pg -e POSTGRES_USER=boltrunner -e POSTGRES_PASSWORD=boltrunner -e POSTGRES_DB=boltrunner -p 5432:5432 postgres:16
sleep 8
docker build -f deploy/Dockerfile.server -t boltrunner/server:local .
docker run -d --rm --name br-api --network host \
  -e DATABASE_URL="postgres://boltrunner:boltrunner@localhost:5432/boltrunner?sslmode=disable" \
  -e KUBECONFIG=/kube/config -v "$HOME/.kube/config:/kube/config:ro" boltrunner/server:local
sleep 8
curl -sf http://localhost:8080/healthz
cd frontend && npm run build && npm start &
npx playwright test
```

Expected: 13 passing across 5 files — the count is unchanged, because this task extends an existing test rather than adding one. Tear down with `docker rm -f br-api br-pg` afterwards.

CI runs this suite too, in `integration-kind` (wired in `9d4c4bd`). Verify the count in the job log rather than trusting the green check: `gh run view --job=<id> --log | grep "Run browser e2e"`.

- [ ] **Step 6: Commit**

```bash
git add frontend/e2e/project-workspaces.spec.ts
git commit -m "test(e2e): assert run history follows the selected project"
```

---

## Self-review notes

- **Spec coverage.** The single architecture change (`page.tsx` branch plus `selectedId` dependency) → Task 1 Step 5. Decision 1, `testId` wins → Task 1 Steps 3-5, pinned by the second new case. Decision 2, no project switch → nothing to implement; the plan adds no such behavior. Decision 3, no empty-state copy → nothing to implement; Task 2 Step 1 asserts the *existing* `No runs yet.` message rather than introducing new copy. The error-handling table describes behavior that is explicitly unchanged, so it has no task; Task 1 Step 5 says so in as many words. Testing strategy → Task 1 Steps 3-7 and Task 2.
- **Placeholder scan.** None. Every step gives exact paths, complete code and exact commands.
- **Type consistency.** `useProjects()` is destructured as `{ selectedId }`, matching the shape `ProjectProvider.tsx` exports and the stub in Task 1 Step 1. `listTests(projectId?: string)` matches its signature in `lib/api-client.ts`. `projectState` is named identically in the stub, the `afterEach` reset and both new cases.
- **A correction made during review.** Step 1 first showed `vi.hoisted` destructured to a bare `mockSelectedId`, which cannot work: reassigning that binding in a test is invisible through the copy the factory closed over. Corrected to a `projectState` object whose property the factory reads at call time, with the reason stated in the step so the next person does not "simplify" it back.
- **Risk.** The likeliest mistake is scoping the `testId` branch too — passing `selectedId` to both calls. It looks tidier and every unit test but one still passes, because the fixtures put the linked test in the selected project. The second new case exists solely to catch that, and Task 2's e2e does not cover it, so do not weaken it.
