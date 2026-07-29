# Project-Scoped Run History

## Context

`docs/superpowers/specs/2026-07-29-multi-project-workspaces-design.md` deferred this, and gave a reason that turned out to be wrong:

> **Scoping run history.** Runs are not project-scoped in the model — `Run` references a test, and filtering `/history` means joining through to `project_id`. That is its own slice.

The premise is false. `HistoryPage` never queries runs directly. It fetches the test list and then fans out:

```ts
const tests: Test[] = await listTests();
const filtered = testId ? tests.filter((t) => t.id === testId) : tests;
const settled = await Promise.allSettled(
  filtered.map(async (t) => {
    const runs = await listRunsForTest(t.id);
    return runs.map((r) => ({ ...r, testName: t.name }));
  })
);
```

The join through `project_id` already happens, in the frontend, via the test list. `listTests` gained an optional project filter in the multi-project work, so scoping the history is a matter of passing the selection to a call that already accepts it. No backend change, no new endpoint, no `Run` model change.

This leaves `/history` as the last view that ignores the selected workspace. The dashboard table, the KPIs and the tree nav all follow it.

## Decisions made during brainstorming

1. **`testId` wins over project scoping.** `/history?testId=X` fetches the unscoped test list, so the link resolves whichever workspace is selected. `TestList` and `TreeNav` produce these links and are themselves scoped, so in normal navigation the linked test is always in the current project; the cross-project case only arises from a bookmarked or shared URL. Making that render blank would be a dead end with no explanation. The accepted cost is that such a link briefly shows a test from another workspace without saying so.
2. **No project switch on cross-project deep links.** Considered and rejected: a URL silently rewriting the persisted selection would affect every later page, and `Run` does not carry a project id to switch to.
3. **No empty-state copy in this slice.** An empty history in a fresh project looks the same as a failed fetch. Real, but a separate concern from scoping, and it applies equally to the dashboard table that shipped without it.

## Architecture

### `frontend/app/history/page.tsx` (edit — the only source file)

Consume the selection and branch the fetch:

```ts
const { selectedId } = useProjects();

useEffect(() => {
  async function load() {
    // An explicit ?testId= is a request for one test's history and must resolve
    // whichever workspace is selected, so it deliberately skips the filter.
    const tests: Test[] = testId ? await listTests() : await listTests(selectedId ?? undefined);
    const filtered = testId ? tests.filter((t) => t.id === testId) : tests;
    // ... unchanged from here
  }
  load().catch(() => setLoaded(true));
}, [testId, selectedId]);
```

`selectedId` joins the dependency array so switching workspaces refetches. Everything downstream — the `Promise.allSettled` fan-out, the merge, the sort, the columns, the row click handler — is untouched.

## Error handling

Unchanged, and deliberately so:

| Case | Behavior |
|---|---|
| One test's runs fail to load | `Promise.allSettled` drops that test; the rest of the table still renders |
| The test list fails | outer `catch` sets `loaded`, producing the existing empty state |
| No project selected (`selectedId` null) | `listTests(undefined)` — the unscoped list, matching how the rest of the app degrades |

## Testing strategy

**Unit** (`frontend/__tests__/HistoryPage.test.tsx`): the file's existing cases gain the `useProjects` stub already used by the other pre-existing test files; their assertions do not change. Two new cases:

- with a selection and no `testId`, `listTests` is called with the project id;
- with a `testId`, `listTests` is called with no argument, and the linked test's runs render even though it is not in the selected project.

**E2E** (`frontend/e2e/project-workspaces.spec.ts`, extended rather than a new file): after creating the test in the new project, its Test Runs page shows no rows. That fails loudly if scoping regresses, because the runs the other specs create in Default would leak in.

Frontend coverage gate 88% applies. No backend change, so `backend/` must show no diff.

## Out of scope

- **Empty-state copy naming the workspace.** See decision 3.
- **A project filter on the runs API.** The frontend join is sufficient at this size. A `GET /api/runs?project_id=` endpoint would remove the N+1 fan-out, but that fan-out predates this work and its cost is unchanged by it — optimising it is a separate decision on its own evidence.
- **Scoping `/runs/{id}`.** A run detail page reached by id needs no workspace filter, for the same reason `?testId=` bypasses one.
