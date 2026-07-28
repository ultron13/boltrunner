# Test Catalog UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make BOL-28's shipped API surface reachable — a `/tests/{id}` detail page with editing and read-only version history, plus `WorkspaceSwitcher`/`TreeNav`/`Shell` reading the real project registry — per `docs/superpowers/specs/2026-07-28-test-catalog-ui-design.md`.

**Architecture:** The detail page loads through `GET /api/tests/{id}/versions` alone, because `GET /api/tests/{id}` does not exist (405) and the versions response is newest-first — so `versions[0]` is the current configuration and the whole array is the history. One stateful component (`TestDetailPanel`) owns fetch, save and conflict state; everything else is presentational. Project name flows one way: `Shell` fetches, then passes it down as a prop to `TopNav`→`WorkspaceSwitcher` and to `TreeNav`.

**Tech Stack:** Next.js App Router (client components), React 18, TypeScript `strict`, Tailwind, Vitest + Testing Library, Playwright. No new dependencies.

## Global Constraints

- **Frontend only.** No file under `backend/` changes. Every endpoint this plan calls already exists and is covered by backend tests.
- **No existing assertion changes except three `href` expectations**: `__tests__/TestList.test.tsx` (`'/history?testId=1'` → `'/tests/1'`) and `__tests__/TreeNav.test.tsx` (`'/history?testId=1'` → `'/tests/1'`, `'/history?testId=2'` → `'/tests/2'`). No other assertion may be weakened or deleted.
- **One mechanical, non-assertion edit is expected and permitted**: adding `vi.spyOn(api, 'listProjects').mockResolvedValue([])` to the existing `__tests__/Shell.test.tsx` cases (Task 6 Step 1a). `Shell` gains a `listProjects()` call, and Node 20 has a global `fetch`, so an unmocked case would fire a real request at `localhost:8080` from a unit test. The assertions in those cases do not change.
- **All three Playwright specs must pass untouched.** `e2e/portal-shell.spec.ts`, `e2e/responsive-portal.spec.ts` and `e2e/walking-skeleton.spec.ts` navigate via nav links and the bottom tab bar, never by clicking a test name, so the changed href does not affect them.
- **The `Test` type's new fields are optional; `TestVersion` makes them required.** `tsconfig.json` sets `"strict": true` and includes `**/*.tsx`, and CI runs `npm run build`, so Next typechecks `__tests__/` too. Required fields would break ~20 pre-versioning fixtures across 8 files. Do not "fix" this by making them required.
- **Coverage gate: 88%** on lines, statements, functions and branches (`vitest.config.ts`). It is not to be lowered. Run `npm run test:coverage` before the final commit.
- **`+ New project` stays disabled.** There is no create-project endpoint.
- **Version rows display `updated_at`, never `created_at`.** The backend returns the *family's* `MIN(created_at)` on every version row, so `created_at` is identical across all of them.
- All new components are client components (`'use client'`), matching every existing component in `frontend/components/`.

---

### Task 1: API client — types, `ApiError`, and the three new calls

**Files:**
- Modify: `frontend/lib/api-client.ts`
- Test: `frontend/__tests__/api-client.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `ApiError` (with `.status: number`), `TestVersion`, `Project`, `UpdateTestInput`, `listTestVersions(testId: string): Promise<TestVersion[]>`, `updateTest(testId: string, input: UpdateTestInput): Promise<TestVersion>`, `listProjects(): Promise<Project[]>`. Every later task depends on these exact names.

- [x] **Step 1: Write the failing tests**

Append to `frontend/__tests__/api-client.test.ts`:

```ts
import { listTestVersions, updateTest, listProjects, ApiError } from '@/lib/api-client';

describe('ApiError', () => {
  afterEach(() => vi.restoreAllMocks());

  it('carries the status code and is still an Error', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      text: async () => 'test was modified concurrently; reload and retry',
    }) as unknown as typeof fetch;

    await expect(updateTest('t1', { name: 'n', target_url: 'http://x', virtual_users: 1, duration_seconds: 1 }))
      .rejects.toBeInstanceOf(ApiError);

    try {
      await updateTest('t1', { name: 'n', target_url: 'http://x', virtual_users: 1, duration_seconds: 1 });
      expect.unreachable('should have thrown');
    } catch (err) {
      expect(err).toBeInstanceOf(Error);
      expect((err as ApiError).status).toBe(409);
      expect((err as ApiError).message).toContain('modified concurrently');
    }
  });
});

describe('listTestVersions', () => {
  afterEach(() => vi.restoreAllMocks());

  it('fetches the versions of a test newest-first', async () => {
    const versions = [
      { id: 't1', version_id: 'v2', version: 2, project_id: 'p1', name: 'smoke', target_url: 'http://b', virtual_users: 2, duration_seconds: 2, created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-25T00:00:00Z' },
      { id: 't1', version_id: 'v1', version: 1, project_id: 'p1', name: 'smoke', target_url: 'http://a', virtual_users: 1, duration_seconds: 1, created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-24T00:00:00Z' },
    ];
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => versions }) as unknown as typeof fetch;

    const result = await listTestVersions('t1');
    expect(result).toEqual(versions);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/tests/t1/versions'),
      expect.objectContaining({ cache: 'no-store' })
    );
  });

  it('defaults to an empty array if the API returns null', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => null }) as unknown as typeof fetch;
    await expect(listTestVersions('t1')).resolves.toEqual([]);
  });

  it('throws an ApiError with status 404 for an unknown test', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 404, text: async () => 'test not found' }) as unknown as typeof fetch;
    await expect(listTestVersions('nope')).rejects.toMatchObject({ status: 404 });
  });
});

describe('updateTest', () => {
  afterEach(() => vi.restoreAllMocks());

  it('PUTs the input and returns the new version', async () => {
    const input = { name: 'smoke', target_url: 'http://x', virtual_users: 5, duration_seconds: 30 };
    const saved = { id: 't1', version_id: 'v2', version: 2, project_id: 'p1', ...input, created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-25T00:00:00Z' };
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => saved }) as unknown as typeof fetch;

    const result = await updateTest('t1', input);
    expect(result).toEqual(saved);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/tests/t1'),
      expect.objectContaining({ method: 'PUT', body: JSON.stringify(input) })
    );
  });
});

describe('listProjects', () => {
  afterEach(() => vi.restoreAllMocks());

  it('fetches the project registry', async () => {
    const projects = [{ id: 'p1', name: 'Default', created_at: '2026-07-24T00:00:00Z' }];
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => projects }) as unknown as typeof fetch;

    const result = await listProjects();
    expect(result).toEqual(projects);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/projects'),
      expect.objectContaining({ cache: 'no-store' })
    );
  });

  it('defaults to an empty array if the API returns null', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => null }) as unknown as typeof fetch;
    await expect(listProjects()).resolves.toEqual([]);
  });
});
```

Note the existing `listRunsForTest` test asserts `rejects.toThrow('request failed (500): boom')`. Keep that message format exactly — `ApiError` changes the error *class*, not the string.

- [x] **Step 2: Run the tests to verify they fail**

```bash
cd frontend && npx vitest run __tests__/api-client.test.ts
```

Expected: FAIL — `listTestVersions`, `updateTest`, `listProjects` and `ApiError` are not exported.

- [x] **Step 3: Implement the client changes**

In `frontend/lib/api-client.ts`, replace the `Test` type and `unwrap`, then append the new calls:

```ts
export type Test = {
  id: string;
  name: string;
  target_url: string;
  virtual_users: number;
  duration_seconds: number;
  created_at: string;
  // Present on every test-shaped response the backend sends today. Optional
  // here so the existing list-shaped fixtures keep typechecking; use
  // TestVersion wherever version identity is actually required.
  version?: number;
  version_id?: string;
  project_id?: string;
  updated_at?: string;
};

export type TestVersion = Test & {
  version: number;
  version_id: string;
  updated_at: string;
};

export type Project = { id: string; name: string; created_at: string };

export type UpdateTestInput = CreateTestInput;

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

async function unwrap<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const text = await res.text();
    throw new ApiError(res.status, `request failed (${res.status}): ${text}`);
  }
  return res.json() as Promise<T>;
}

export async function listTestVersions(testId: string): Promise<TestVersion[]> {
  const versions = await unwrap<TestVersion[]>(
    await fetch(`${API_URL}/api/tests/${testId}/versions`, { cache: 'no-store' })
  );
  return versions ?? [];
}

export async function updateTest(testId: string, input: UpdateTestInput): Promise<TestVersion> {
  return unwrap(
    await fetch(`${API_URL}/api/tests/${testId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
  );
}

export async function listProjects(): Promise<Project[]> {
  const projects = await unwrap<Project[]>(await fetch(`${API_URL}/api/projects`, { cache: 'no-store' }));
  return projects ?? [];
}
```

`ApiError` must be declared *before* `unwrap` uses it, and `UpdateTestInput` after `CreateTestInput`.

The 409 test asserts the message contains "modified concurrently" — that text comes from the backend response body, which `unwrap` interpolates, so no client-side message strings are invented here.

- [x] **Step 4: Run the tests to verify they pass**

```bash
cd frontend && npx vitest run __tests__/api-client.test.ts
```

Expected: PASS, including every pre-existing case in the file.

- [x] **Step 5: Run the whole unit suite and typecheck**

```bash
cd frontend && npx vitest run && npm run build
```

Expected: 103+ tests pass, build succeeds. The build is what proves the optional-field decision — if it fails with errors about missing `version`/`version_id` in `__tests__/` fixtures, the fields were made required by mistake.

- [x] **Step 6: Commit**

```bash
git add frontend/lib/api-client.ts frontend/__tests__/api-client.test.ts
git commit -m "feat(frontend): add versions, update and projects API client calls"
```

---

### Task 2: Extract `TestFields` out of `CreateTestForm`

**Files:**
- Create: `frontend/components/TestFields.tsx`
- Modify: `frontend/components/CreateTestForm.tsx`
- Test: `frontend/__tests__/TestFields.test.tsx`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `TestFields` with props `{ name: string; targetUrl: string; virtualUsers: string; durationSeconds: string; onChange: (field: TestField, value: string) => void }` and `export type TestField = 'name' | 'targetUrl' | 'virtualUsers' | 'durationSeconds'`. Task 3's `EditTestForm` uses both.

The numeric values are **strings**, not numbers — that is how `CreateTestForm` already holds them, so the inputs stay controlled while a user is mid-typing (an empty string is a legal intermediate state that `Number('')` would turn into `0`).

- [x] **Step 1: Write the failing test**

Create `frontend/__tests__/TestFields.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TestFields } from '@/components/TestFields';

function renderFields(onChange = vi.fn()) {
  render(
    <TestFields name="smoke" targetUrl="http://x" virtualUsers="5" durationSeconds="30" onChange={onChange} />
  );
  return onChange;
}

describe('TestFields', () => {
  it('renders all four labelled inputs with their values', () => {
    renderFields();
    expect(screen.getByLabelText(/name/i)).toHaveValue('smoke');
    expect(screen.getByLabelText(/target url/i)).toHaveValue('http://x');
    expect(screen.getByLabelText(/virtual users/i)).toHaveValue(5);
    expect(screen.getByLabelText(/duration/i)).toHaveValue(30);
  });

  it('carries the validation attributes the backend enforces', () => {
    renderFields();
    expect(screen.getByLabelText(/target url/i)).toHaveAttribute('type', 'url');
    expect(screen.getByLabelText(/virtual users/i)).toHaveAttribute('min', '1');
    expect(screen.getByLabelText(/duration/i)).toHaveAttribute('min', '1');
    expect(screen.getByLabelText(/name/i)).toBeRequired();
  });

  it('reports each field change by name', () => {
    const onChange = renderFields();
    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'renamed' } });
    expect(onChange).toHaveBeenCalledWith('name', 'renamed');

    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '9' } });
    expect(onChange).toHaveBeenCalledWith('virtualUsers', '9');
  });
});
```

`toHaveValue` returns a number for `type="number"` inputs and a string for text inputs — that asymmetry in the first test is correct, not a typo.

- [x] **Step 2: Run it to verify it fails**

```bash
cd frontend && npx vitest run __tests__/TestFields.test.tsx
```

Expected: FAIL — cannot resolve `@/components/TestFields`.

- [x] **Step 3: Create `TestFields`**

```tsx
'use client';

export type TestField = 'name' | 'targetUrl' | 'virtualUsers' | 'durationSeconds';

export function TestFields({
  name,
  targetUrl,
  virtualUsers,
  durationSeconds,
  onChange,
}: {
  name: string;
  targetUrl: string;
  virtualUsers: string;
  durationSeconds: string;
  onChange: (field: TestField, value: string) => void;
}) {
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
        <span>Virtual users</span>
        <input
          value={virtualUsers}
          onChange={(e) => onChange('virtualUsers', e.target.value)}
          required
          type="number"
          min={1}
        />
      </label>
      <label className="flex flex-col gap-1">
        <span>Duration (seconds)</span>
        <input
          value={durationSeconds}
          onChange={(e) => onChange('durationSeconds', e.target.value)}
          required
          type="number"
          min={1}
        />
      </label>
    </>
  );
}
```

The markup is copied verbatim from `CreateTestForm` — same labels, same classes, same attributes. That is what lets the existing `CreateTestForm` tests keep passing.

- [x] **Step 4: Rewrite `CreateTestForm` to use it**

Replace the four `<label>` blocks in `frontend/components/CreateTestForm.tsx` with `<TestFields …/>`, keeping everything else (state, submit, reset, error line) exactly as-is:

```tsx
'use client';

import { useState, FormEvent } from 'react';
import { createTest, Test } from '@/lib/api-client';
import { TestFields, TestField } from '@/components/TestFields';

export function CreateTestForm({ onCreated }: { onCreated: (test: Test) => void }) {
  const [name, setName] = useState('');
  const [targetUrl, setTargetUrl] = useState('');
  const [virtualUsers, setVirtualUsers] = useState('10');
  const [durationSeconds, setDurationSeconds] = useState('30');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const setters: Record<TestField, (v: string) => void> = {
    name: setName,
    targetUrl: setTargetUrl,
    virtualUsers: setVirtualUsers,
    durationSeconds: setDurationSeconds,
  };

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
      <TestFields
        name={name}
        targetUrl={targetUrl}
        virtualUsers={virtualUsers}
        durationSeconds={durationSeconds}
        onChange={(field, value) => setters[field](value)}
      />
      {error && <p className="text-red-600">{error}</p>}
      <button type="submit" disabled={submitting}>
        {submitting ? 'Creating…' : 'Create test'}
      </button>
    </form>
  );
}
```

- [x] **Step 5: Run the new and existing form tests**

```bash
cd frontend && npx vitest run __tests__/TestFields.test.tsx __tests__/CreateTestForm.test.tsx __tests__/TestManagementPanel.test.tsx
```

Expected: PASS. **`CreateTestForm.test.tsx` and `TestManagementPanel.test.tsx` must pass with zero edits** — they are the proof the extraction changed no behavior. If either needs editing, the extraction diverged from the original markup; fix `TestFields`, not the tests.

- [x] **Step 6: Commit**

```bash
git add frontend/components/TestFields.tsx frontend/components/CreateTestForm.tsx frontend/__tests__/TestFields.test.tsx
git commit -m "refactor(frontend): extract TestFields so create and edit share validation"
```

---

### Task 3: `EditTestForm` and `VersionHistoryTable`

**Files:**
- Create: `frontend/components/EditTestForm.tsx`
- Create: `frontend/components/VersionHistoryTable.tsx`
- Test: `frontend/__tests__/EditTestForm.test.tsx`
- Test: `frontend/__tests__/VersionHistoryTable.test.tsx`

**Interfaces:**
- Consumes: `TestVersion` (Task 1); `TestFields`, `TestField` (Task 2).
- Produces: `EditTestForm` with props `{ current: TestVersion; onSave: (input: UpdateTestInput) => Promise<void>; error?: string | null }`, and `VersionHistoryTable` with props `{ versions: TestVersion[] }`. Task 4 renders both.

`onSave` returns a promise so the form can disable its button while saving; it does **not** fetch — `TestDetailPanel` owns that.

- [x] **Step 1: Write the failing tests**

Create `frontend/__tests__/EditTestForm.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { EditTestForm } from '@/components/EditTestForm';
import type { TestVersion } from '@/lib/api-client';

const current: TestVersion = {
  id: 't1', version_id: 'v2', version: 2, project_id: 'p1',
  name: 'Checkout Load', target_url: 'http://x', virtual_users: 5, duration_seconds: 30,
  created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-25T00:00:00Z',
};

describe('EditTestForm', () => {
  it('seeds the fields from the current version', () => {
    render(<EditTestForm current={current} onSave={vi.fn()} />);
    expect(screen.getByLabelText(/name/i)).toHaveValue('Checkout Load');
    expect(screen.getByLabelText(/target url/i)).toHaveValue('http://x');
    expect(screen.getByLabelText(/virtual users/i)).toHaveValue(5);
    expect(screen.getByLabelText(/duration/i)).toHaveValue(30);
  });

  it('submits the edited values as numbers', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<EditTestForm current={current} onSave={onSave} />);

    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '9' } });
    fireEvent.click(screen.getByRole('button', { name: /save as new version/i }));

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        name: 'Checkout Load',
        target_url: 'http://x',
        virtual_users: 9,
        duration_seconds: 30,
      })
    );
  });

  it('re-seeds when a different version becomes current', () => {
    const { rerender } = render(<EditTestForm current={current} onSave={vi.fn()} />);
    rerender(
      <EditTestForm current={{ ...current, version_id: 'v3', version: 3, virtual_users: 42 }} onSave={vi.fn()} />
    );
    expect(screen.getByLabelText(/virtual users/i)).toHaveValue(42);
  });

  it('shows the error passed in by its parent', () => {
    render(<EditTestForm current={current} onSave={vi.fn()} error="This test was changed elsewhere" />);
    expect(screen.getByText(/changed elsewhere/i)).toBeInTheDocument();
  });
});
```

Create `frontend/__tests__/VersionHistoryTable.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { VersionHistoryTable } from '@/components/VersionHistoryTable';
import type { TestVersion } from '@/lib/api-client';

// created_at is deliberately identical on both rows: the backend returns the
// family's creation time on every version, so a table that rendered created_at
// would show the same timestamp for every row.
const versions: TestVersion[] = [
  { id: 't1', version_id: 'v2', version: 2, project_id: 'p1', name: 'Checkout Load', target_url: 'http://b', virtual_users: 9, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-25T12:00:00Z' },
  { id: 't1', version_id: 'v1', version: 1, project_id: 'p1', name: 'Checkout Load', target_url: 'http://a', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-24T00:00:00Z' },
];

describe('VersionHistoryTable', () => {
  it('renders a row per version labelled v{n}', () => {
    render(<VersionHistoryTable versions={versions} />);
    expect(screen.getByRole('row', { name: /v2/ })).toBeInTheDocument();
    expect(screen.getByRole('row', { name: /v1/ })).toBeInTheDocument();
  });

  it('shows each version its own edited timestamp, not the shared family created_at', () => {
    render(<VersionHistoryTable versions={versions} />);
    const v2 = screen.getByRole('row', { name: /v2/ });
    expect(within(v2).getByText(/2026-07-25/)).toBeInTheDocument();
    expect(within(v2).queryByText('2026-07-24T00:00:00Z')).not.toBeInTheDocument();
  });

  it('shows the per-version configuration', () => {
    render(<VersionHistoryTable versions={versions} />);
    expect(within(screen.getByRole('row', { name: /v2/ })).getByText('http://b')).toBeInTheDocument();
    expect(within(screen.getByRole('row', { name: /v1/ })).getByText('http://a')).toBeInTheDocument();
  });

  it('offers no actions — history is read-only', () => {
    render(<VersionHistoryTable versions={versions} />);
    expect(within(screen.getByRole('table')).queryByRole('button')).not.toBeInTheDocument();
  });
});
```

- [x] **Step 2: Run them to verify they fail**

```bash
cd frontend && npx vitest run __tests__/EditTestForm.test.tsx __tests__/VersionHistoryTable.test.tsx
```

Expected: FAIL — neither module resolves.

- [x] **Step 3: Create `EditTestForm`**

```tsx
'use client';

import { useEffect, useState, FormEvent } from 'react';
import { TestVersion, UpdateTestInput } from '@/lib/api-client';
import { TestFields, TestField } from '@/components/TestFields';

export function EditTestForm({
  current,
  onSave,
  error,
}: {
  current: TestVersion;
  onSave: (input: UpdateTestInput) => Promise<void>;
  error?: string | null;
}) {
  const [name, setName] = useState(current.name);
  const [targetUrl, setTargetUrl] = useState(current.target_url);
  const [virtualUsers, setVirtualUsers] = useState(String(current.virtual_users));
  const [durationSeconds, setDurationSeconds] = useState(String(current.duration_seconds));
  const [saving, setSaving] = useState(false);

  // Re-seed only when a genuinely different version becomes current (i.e. a
  // save landed). Keying on version_id rather than the object identity keeps a
  // parent re-render from wiping what the user is typing.
  useEffect(() => {
    setName(current.name);
    setTargetUrl(current.target_url);
    setVirtualUsers(String(current.virtual_users));
    setDurationSeconds(String(current.duration_seconds));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current.version_id]);

  const setters: Record<TestField, (v: string) => void> = {
    name: setName,
    targetUrl: setTargetUrl,
    virtualUsers: setVirtualUsers,
    durationSeconds: setDurationSeconds,
  };

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      await onSave({
        name,
        target_url: targetUrl,
        virtual_users: Number(virtualUsers),
        duration_seconds: Number(durationSeconds),
      });
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-3 max-w-md">
      <TestFields
        name={name}
        targetUrl={targetUrl}
        virtualUsers={virtualUsers}
        durationSeconds={durationSeconds}
        onChange={(field, value) => setters[field](value)}
      />
      {error && <p className="text-red-600">{error}</p>}
      <button type="submit" disabled={saving}>
        {saving ? 'Saving…' : 'Save as new version'}
      </button>
    </form>
  );
}
```

`handleSubmit` does not catch — `TestDetailPanel` owns error state and feeds it back through the `error` prop. `onSave` must therefore never reject; Task 4's implementation catches internally.

- [x] **Step 4: Create `VersionHistoryTable`**

```tsx
'use client';

import { TestVersion } from '@/lib/api-client';
import { DataTable, Column } from '@/components/ui/DataTable';

export function VersionHistoryTable({ versions }: { versions: TestVersion[] }) {
  const columns: Column<TestVersion>[] = [
    { key: 'version', header: 'Version', render: (v) => `v${v.version}` },
    { key: 'target_url', header: 'Target URL' },
    { key: 'virtual_users', header: 'Virtual users', align: 'numeric' },
    { key: 'duration_seconds', header: 'Duration (s)', align: 'numeric' },
    { key: 'updated_at', header: 'Edited' },
  ];

  return <DataTable columns={columns} rows={versions} rowKey={(v) => v.version_id} />;
}
```

`Version` is deliberately first: `DataTable`'s mobile card mode uses `columns[0]` as the card title. No `onRowClick` and no action column — history is read-only.

- [x] **Step 5: Run the tests to verify they pass**

```bash
cd frontend && npx vitest run __tests__/EditTestForm.test.tsx __tests__/VersionHistoryTable.test.tsx
```

Expected: PASS, 8 tests.

- [x] **Step 6: Commit**

```bash
git add frontend/components/EditTestForm.tsx frontend/components/VersionHistoryTable.tsx frontend/__tests__/EditTestForm.test.tsx frontend/__tests__/VersionHistoryTable.test.tsx
git commit -m "feat(frontend): add EditTestForm and read-only VersionHistoryTable"
```

---

### Task 4: `TestDetailPanel` and the `/tests/[id]` route

**Files:**
- Create: `frontend/components/TestDetailPanel.tsx`
- Create: `frontend/app/tests/[id]/page.tsx`
- Test: `frontend/__tests__/TestDetailPanel.test.tsx`
- Test: `frontend/__tests__/TestDetailPage.test.tsx`

**Interfaces:**
- Consumes: `listTestVersions`, `updateTest`, `startRun`, `ApiError`, `TestVersion`, `UpdateTestInput` (Task 1); `EditTestForm`, `VersionHistoryTable` (Task 3).
- Produces: `TestDetailPanel` with props `{ testId: string }`. Nothing later depends on it.

- [x] **Step 1: Write the failing tests**

Create `frontend/__tests__/TestDetailPanel.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TestDetailPanel } from '@/components/TestDetailPanel';
import * as api from '@/lib/api-client';
import { ApiError } from '@/lib/api-client';
import type { TestVersion } from '@/lib/api-client';

const push = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push }),
}));

const v2: TestVersion = {
  id: 't1', version_id: 'vid2', version: 2, project_id: 'p1',
  name: 'Checkout Load', target_url: 'http://b', virtual_users: 9, duration_seconds: 30,
  created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-25T12:00:00Z',
};
const v1: TestVersion = { ...v2, version_id: 'vid1', version: 1, target_url: 'http://a', virtual_users: 5, updated_at: '2026-07-24T00:00:00Z' };

describe('TestDetailPanel', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('seeds the form from the newest version and lists every version', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    render(<TestDetailPanel testId="t1" />);

    expect(await screen.findByLabelText(/virtual users/i)).toHaveValue(9);
    expect(screen.getByRole('row', { name: /v2/ })).toBeInTheDocument();
    expect(screen.getByRole('row', { name: /v1/ })).toBeInTheDocument();
  });

  it('saves an edit and shows the reloaded version list', async () => {
    const v3: TestVersion = { ...v2, version_id: 'vid3', version: 3, virtual_users: 20, updated_at: '2026-07-26T00:00:00Z' };
    vi.spyOn(api, 'listTestVersions')
      .mockResolvedValueOnce([v2, v1])
      .mockResolvedValueOnce([v3, v2, v1]);
    const updateTest = vi.spyOn(api, 'updateTest').mockResolvedValue(v3);

    render(<TestDetailPanel testId="t1" />);
    await screen.findByLabelText(/virtual users/i);

    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '20' } });
    fireEvent.click(screen.getByRole('button', { name: /save as new version/i }));

    await waitFor(() =>
      expect(updateTest).toHaveBeenCalledWith('t1', expect.objectContaining({ virtual_users: 20 }))
    );
    expect(await screen.findByRole('row', { name: /v3/ })).toBeInTheDocument();
  });

  it('renders a not-found state when the test does not exist', async () => {
    vi.spyOn(api, 'listTestVersions').mockRejectedValue(new ApiError(404, 'test not found'));
    render(<TestDetailPanel testId="ghost" />);

    expect(await screen.findByText(/test not found/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /save as new version/i })).not.toBeInTheDocument();
  });

  it('renders an error state when loading fails for another reason', async () => {
    vi.spyOn(api, 'listTestVersions').mockRejectedValue(new ApiError(500, 'boom'));
    render(<TestDetailPanel testId="t1" />);
    expect(await screen.findByText(/couldn't load this test/i)).toBeInTheDocument();
  });

  // The behavior most likely to regress silently: a naive implementation
  // re-seeds the form from the reloaded version and throws away the user's work
  // for a conflict they did not cause.
  it('keeps the typed values and reloads history when the save conflicts', async () => {
    vi.spyOn(api, 'listTestVersions')
      .mockResolvedValueOnce([v2, v1])
      .mockResolvedValueOnce([v2, v1]);
    vi.spyOn(api, 'updateTest').mockRejectedValue(
      new ApiError(409, 'request failed (409): test was modified concurrently; reload and retry')
    );

    render(<TestDetailPanel testId="t1" />);
    await screen.findByLabelText(/virtual users/i);

    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '77' } });
    fireEvent.click(screen.getByRole('button', { name: /save as new version/i }));

    expect(await screen.findByText(/changed elsewhere/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/virtual users/i)).toHaveValue(77);
  });

  it('surfaces a validation error from the server without losing the form', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    vi.spyOn(api, 'updateTest').mockRejectedValue(new ApiError(400, 'request failed (400): name, target_url, virtual_users>0, duration_seconds>0 are required'));

    render(<TestDetailPanel testId="t1" />);
    await screen.findByLabelText(/virtual users/i);
    fireEvent.click(screen.getByRole('button', { name: /save as new version/i }));

    expect(await screen.findByText(/are required/i)).toBeInTheDocument();
  });

  it('starts a run and navigates to it', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    vi.spyOn(api, 'startRun').mockResolvedValue({ id: 'r1', test_id: 'vid2', status: 'pending' });

    render(<TestDetailPanel testId="t1" />);
    fireEvent.click(await screen.findByRole('button', { name: /run test/i }));

    await waitFor(() => expect(push).toHaveBeenCalledWith('/runs/r1'));
  });

  it('links to the run history for this test', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    render(<TestDetailPanel testId="t1" />);
    expect(await screen.findByRole('link', { name: /run history/i })).toHaveAttribute('href', '/history?testId=t1');
  });
});
```

Create `frontend/__tests__/TestDetailPage.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import TestDetailPage from '@/app/tests/[id]/page';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  useParams: () => ({ id: 't1' }),
  useRouter: () => ({ push: vi.fn() }),
}));

describe('TestDetailPage', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('renders the detail panel for the route param', async () => {
    const listTestVersions = vi.spyOn(api, 'listTestVersions').mockResolvedValue([]);
    render(<TestDetailPage />);
    expect(listTestVersions).toHaveBeenCalledWith('t1');
    expect(await screen.findByRole('heading', { level: 1 })).toBeInTheDocument();
  });
});
```

- [x] **Step 2: Run them to verify they fail**

```bash
cd frontend && npx vitest run __tests__/TestDetailPanel.test.tsx __tests__/TestDetailPage.test.tsx
```

Expected: FAIL — neither module resolves.

- [x] **Step 3: Create `TestDetailPanel`**

```tsx
'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  ApiError,
  listTestVersions,
  startRun,
  updateTest,
  TestVersion,
  UpdateTestInput,
} from '@/lib/api-client';
import { EditTestForm } from '@/components/EditTestForm';
import { VersionHistoryTable } from '@/components/VersionHistoryTable';

type LoadState = 'loading' | 'ready' | 'notfound' | 'error';

const CONFLICT_MESSAGE = 'This test was changed elsewhere — review and save again';

export function TestDetailPanel({ testId }: { testId: string }) {
  const [versions, setVersions] = useState<TestVersion[]>([]);
  const [loadState, setLoadState] = useState<LoadState>('loading');
  const [saveError, setSaveError] = useState<string | null>(null);
  const router = useRouter();

  const load = useCallback(async () => {
    try {
      setVersions(await listTestVersions(testId));
      setLoadState('ready');
    } catch (err) {
      setLoadState(err instanceof ApiError && err.status === 404 ? 'notfound' : 'error');
    }
  }, [testId]);

  useEffect(() => {
    load();
  }, [load]);

  async function handleSave(input: UpdateTestInput) {
    setSaveError(null);
    try {
      await updateTest(testId, input);
      await load();
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        setLoadState('notfound');
        return;
      }
      if (err instanceof ApiError && err.status === 409) {
        // Reload the history so it shows what actually landed, but leave the
        // form alone: the user did not cause this conflict and should not lose
        // what they typed.
        setSaveError(CONFLICT_MESSAGE);
        await load();
        return;
      }
      setSaveError(err instanceof Error ? err.message : "Couldn't save this test");
    }
  }

  async function handleRun() {
    const run = await startRun(testId);
    router.push(`/runs/${run.id}`);
  }

  if (loadState === 'loading') return <p>Loading…</p>;
  if (loadState === 'notfound') return <p>Test not found.</p>;
  if (loadState === 'error') {
    return (
      <div className="flex flex-col gap-3 items-start">
        <p className="text-red-600">Couldn&apos;t load this test.</p>
        <button type="button" onClick={load}>
          Retry
        </button>
      </div>
    );
  }

  const current = versions[0];
  if (!current) return <p>Test not found.</p>;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-text">{current.name}</h2>
        <button type="button" onClick={handleRun}>
          Run test
        </button>
      </div>

      <section className="flex flex-col gap-2">
        <h3 className="font-medium text-text">Configuration</h3>
        <EditTestForm current={current} onSave={handleSave} error={saveError} />
      </section>

      <section className="flex flex-col gap-2">
        <h3 className="font-medium text-text">Version history</h3>
        <VersionHistoryTable versions={versions} />
      </section>

      <Link href={`/history?testId=${testId}`} className="text-accent hover:underline">
        Run history →
      </Link>
    </div>
  );
}
```

The conflict path deliberately does **not** re-seed the form. `EditTestForm` re-seeds only when `current.version_id` changes; on a 409 no new version was written for this user, so `versions[0]` comes back with the same `version_id` and the typed values survive. That interlock is what the conflict test pins.

`versions[0]` is defensively checked even though the backend cannot return an empty array for an existing test — a 200 with `[]` would otherwise crash on `current.name`.

- [x] **Step 4: Create the route**

`frontend/app/tests/[id]/page.tsx`:

```tsx
'use client';

import { useParams } from 'next/navigation';
import { TestDetailPanel } from '@/components/TestDetailPanel';

export default function TestDetailPage() {
  const params = useParams<{ id: string }>();
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-text">Test</h1>
      <TestDetailPanel testId={params.id} />
    </div>
  );
}
```

- [x] **Step 5: Run the tests to verify they pass**

```bash
cd frontend && npx vitest run __tests__/TestDetailPanel.test.tsx __tests__/TestDetailPage.test.tsx
```

Expected: PASS, 9 tests.

- [x] **Step 6: Commit**

```bash
git add frontend/components/TestDetailPanel.tsx frontend/app/tests/\[id\]/page.tsx frontend/__tests__/TestDetailPanel.test.tsx frontend/__tests__/TestDetailPage.test.tsx
git commit -m "feat(frontend): add the test detail page with editing and version history"
```

---

### Task 5: Point navigation at the detail page

**Files:**
- Modify: `frontend/components/TestList.tsx:13`
- Modify: `frontend/components/ui/TreeNav.tsx:11`
- Test: `frontend/__tests__/TestList.test.tsx:29-35`
- Test: `frontend/__tests__/TreeNav.test.tsx:12-17`

**Interfaces:**
- Consumes: the `/tests/{id}` route (Task 4).
- Produces: nothing.

This is the one task that edits existing assertions. Exactly three `href` expectations change; nothing else in either file may be touched.

- [x] **Step 1: Update the two test files**

In `frontend/__tests__/TestList.test.tsx`, change the last test:

```tsx
  it('links the test name to its detail page', () => {
    render(<TestList tests={tests} onStart={() => {}} />);
    expect(within(screen.getByRole('table')).getByRole('link', { name: 'Checkout Load' })).toHaveAttribute(
      'href',
      '/tests/1'
    );
  });
```

In `frontend/__tests__/TreeNav.test.tsx`, change the two hrefs in the first test:

```tsx
  it('renders the Default workspace and every test as a link', () => {
    render(<TreeNav tests={tests} />);
    expect(screen.getByText(/Default/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Checkout Load/i })).toHaveAttribute('href', '/tests/1');
    expect(screen.getByRole('link', { name: /Search Spike/i })).toHaveAttribute('href', '/tests/2');
  });
```

- [x] **Step 2: Run them to verify they fail**

```bash
cd frontend && npx vitest run __tests__/TestList.test.tsx __tests__/TreeNav.test.tsx
```

Expected: FAIL — both still render `/history?testId=…`.

- [x] **Step 3: Change the two hrefs**

`frontend/components/TestList.tsx`, in the `name` column's `render`:

```tsx
        <Link href={`/tests/${t.id}`} className="text-accent hover:underline">
          {t.name}
        </Link>
```

`frontend/components/ui/TreeNav.tsx`, the per-test `Link`:

```tsx
            <Link
              href={`/tests/${t.id}`}
```

- [x] **Step 4: Run them to verify they pass**

```bash
cd frontend && npx vitest run __tests__/TestList.test.tsx __tests__/TreeNav.test.tsx
```

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add frontend/components/TestList.tsx frontend/components/ui/TreeNav.tsx frontend/__tests__/TestList.test.tsx frontend/__tests__/TreeNav.test.tsx
git commit -m "feat(frontend): link test names to the new detail page"
```

---

### Task 6: Real project name in the switcher, tree nav and breadcrumb

**Files:**
- Modify: `frontend/components/ui/WorkspaceSwitcher.tsx`
- Modify: `frontend/components/ui/TreeNav.tsx`
- Modify: `frontend/components/ui/TopNav.tsx`
- Modify: `frontend/components/ui/Shell.tsx`
- Test: `frontend/__tests__/WorkspaceSwitcher.test.tsx`
- Test: `frontend/__tests__/Shell.test.tsx`

**Interfaces:**
- Consumes: `listProjects`, `Project` (Task 1).
- Produces: nothing.

Every one of these components takes `projectName` as an **optional prop defaulting to `'Default'`**. That default is why the whole existing `WorkspaceSwitcher.test.tsx` suite — which renders `<WorkspaceSwitcher />` with no props and asserts on `/default/i` — keeps passing untouched.

- [x] **Step 1: Write the failing tests**

Append to `frontend/__tests__/WorkspaceSwitcher.test.tsx`:

```tsx
  it('renders the project name it is given', () => {
    render(<WorkspaceSwitcher projectName="Payments" />);
    expect(screen.getByRole('button', { name: /payments/i })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /payments/i }));
    expect(screen.getByRole('menuitemradio', { name: /payments/i })).toHaveAttribute('aria-checked', 'true');
  });

  it('falls back to Default when given no project name', () => {
    render(<WorkspaceSwitcher />);
    expect(screen.getByRole('button', { name: /default/i })).toBeInTheDocument();
  });
```

Append to `frontend/__tests__/Shell.test.tsx`:

```tsx
  it('shows the fetched project name in the breadcrumb and tree nav', async () => {
    vi.mocked(usePathname).mockReturnValue('/');
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    vi.spyOn(api, 'listProjects').mockResolvedValue([
      { id: 'p1', name: 'Payments', created_at: '2026-07-24T00:00:00Z' },
    ]);

    render(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );

    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent('Payments')
    );
    expect(screen.getByRole('navigation', { name: 'Workspace' })).toHaveTextContent('Payments');
  });

  it('keeps showing Default when the projects endpoint fails', async () => {
    vi.mocked(usePathname).mockReturnValue('/');
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    vi.spyOn(api, 'listProjects').mockRejectedValue(new Error('boom'));

    render(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );

    expect(await screen.findByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent('Default');
  });

  it('shows the test name in the breadcrumb on a test detail path', async () => {
    vi.mocked(usePathname).mockReturnValue('/tests/1');
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams());
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: '1', name: 'Checkout Load', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listProjects').mockResolvedValue([]);

    render(
      <ThemeProvider>
        <Shell>
          <p>detail content</p>
        </Shell>
      </ThemeProvider>
    );

    await waitFor(() =>
      expect(screen.getByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent('Checkout Load')
    );
  });

  it('falls back to the raw id in the breadcrumb for an unknown test detail path', async () => {
    vi.mocked(usePathname).mockReturnValue('/tests/unknown-id');
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams());
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    vi.spyOn(api, 'listProjects').mockResolvedValue([]);

    render(
      <ThemeProvider>
        <Shell>
          <p>detail content</p>
        </Shell>
      </ThemeProvider>
    );

    expect(await screen.findByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent('unknown-id');
  });
```

- [x] **Step 1a: Stop the existing `Shell` tests hitting the network**

Every existing case in `__tests__/Shell.test.tsx` renders `Shell`, which now calls
`listProjects()`. Node 20 ships a global `fetch`, so an unmocked case fires a real request at
`localhost:8080` — which either fails slowly or, on a developer machine running the backend,
succeeds and makes the test depend on live data.

Add this line beside the existing `vi.spyOn(api, 'listTests')` line in **each** existing case
in that file:

```tsx
    vi.spyOn(api, 'listProjects').mockResolvedValue([]);
```

An empty array leaves `projectName` at its `'Default'` initial value, so every existing
assertion — all of which expect `'Default'` — still holds. **Change nothing else in those
cases.**

- [x] **Step 2: Run them to verify they fail**

```bash
cd frontend && npx vitest run __tests__/WorkspaceSwitcher.test.tsx __tests__/Shell.test.tsx
```

Expected: FAIL — `projectName` is not a prop, and no `/tests/{id}` breadcrumb case exists.

- [x] **Step 3: Add the prop to `WorkspaceSwitcher`**

Change the signature and the two rendered labels only:

```tsx
export function WorkspaceSwitcher({ projectName = 'Default' }: { projectName?: string } = {}) {
```

Trigger:

```tsx
        {projectName} <span aria-hidden="true">▾</span>
```

Checked menu item:

```tsx
            <span aria-hidden="true">✓</span> {projectName}
```

Leave the open/close state, outside-click handler, `Escape` handling, focus restoration, roles and the disabled `+ New project` button exactly as they are.

- [x] **Step 4: Thread it through `TreeNav` and `TopNav`**

`frontend/components/ui/TreeNav.tsx`:

```tsx
export function TreeNav({
  tests,
  activeTestId,
  projectName = 'Default',
}: {
  tests: Test[];
  activeTestId?: string;
  projectName?: string;
}) {
```

and the folder label:

```tsx
      <div className="px-3 py-1 font-medium text-text">📁 {projectName}</div>
```

`frontend/components/ui/TopNav.tsx`:

```tsx
export function TopNav({ modules, projectName }: { modules: NavModule[]; projectName?: string }) {
```

and:

```tsx
        <WorkspaceSwitcher projectName={projectName} />
```

- [x] **Step 5: Fetch projects in `Shell` and extend the breadcrumb**

In `frontend/components/ui/Shell.tsx`, change `breadcrumbFor` to take the root label and handle the detail path:

```tsx
function breadcrumbFor(
  pathname: string,
  testId: string | null,
  testName: string | undefined,
  projectName: string
): BreadcrumbItem[] {
  const root: BreadcrumbItem = { label: projectName, href: '/' };
  if (pathname === '/') return [root];
  if (pathname === '/admin') return [root, { label: 'Admin' }];
  if (pathname === '/tests') return [root, { label: 'Tests' }];
  if (pathname.startsWith('/tests/')) {
    const id = pathname.split('/')[2];
    return [root, { label: 'Tests', href: '/tests' }, { label: testName ?? id }];
  }
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
```

The `pathname === '/tests'` case must stay **above** the `startsWith('/tests/')` case; `'/tests'` does not match `'/tests/'`, but keeping the exact match first makes the ordering intent obvious.

In the component body:

```tsx
export function Shell({ children }: { children: ReactNode }) {
  const [tests, setTests] = useState<Test[]>([]);
  const [projectName, setProjectName] = useState('Default');
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const testId = searchParams.get('testId');

  useEffect(() => {
    listTests().then(setTests).catch(() => setTests([]));
  }, []);

  useEffect(() => {
    // A projects-endpoint failure degrades to the literal "Default" rather
    // than an empty switcher.
    listProjects()
      .then((projects) => setProjectName(projects[0]?.name ?? 'Default'))
      .catch(() => {});
  }, []);

  const detailTestId = pathname.startsWith('/tests/') ? pathname.split('/')[2] : null;
  const activeTestId = testId ?? detailTestId;
  const activeTest = tests.find((t) => t.id === activeTestId);
  const crumbs = breadcrumbFor(pathname, testId, activeTest?.name, projectName);
```

Update the imports (`listProjects` alongside `listTests`) and the two render sites:

```tsx
      <TopNav modules={MODULES} projectName={projectName} />
```

```tsx
          <TreeNav tests={tests} activeTestId={activeTestId ?? undefined} projectName={projectName} />
```

Passing `activeTestId` (rather than the old `testId`) means the sidebar now also highlights the open test on its detail page.

- [x] **Step 6: Run the tests to verify they pass**

```bash
cd frontend && npx vitest run __tests__/WorkspaceSwitcher.test.tsx __tests__/Shell.test.tsx __tests__/TreeNav.test.tsx __tests__/TopNav.test.tsx
```

Expected: PASS. Every pre-existing case in all four files must still pass — the `'Default'` defaults are what guarantee that.

- [x] **Step 7: Commit**

```bash
git add frontend/components/ui/WorkspaceSwitcher.tsx frontend/components/ui/TreeNav.tsx frontend/components/ui/TopNav.tsx frontend/components/ui/Shell.tsx frontend/__tests__/WorkspaceSwitcher.test.tsx frontend/__tests__/Shell.test.tsx
git commit -m "feat(frontend): show the real project name from the registry"
```

---

### Task 7: End-to-end coverage and the full gate

**Files:**
- Create: `frontend/e2e/test-versioning.spec.ts`
- Test: the whole suite

**Interfaces:**
- Consumes: everything from Tasks 1-6.
- Produces: nothing.

- [x] **Step 1: Write the e2e spec**

Create `frontend/e2e/test-versioning.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

test('edit a test and see the new version in its history', async ({ page }) => {
  const name = `E2E Versioning ${Date.now()}`;
  await page.goto('/');

  await page.getByLabel(/name/i).fill(name);
  await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
  await page.getByLabel(/virtual users/i).fill('3');
  await page.getByLabel(/duration/i).fill('20');
  await page.getByRole('button', { name: /create test/i }).click();

  const row = page.getByRole('row', { name: new RegExp(name, 'i') });
  await expect(row).toBeVisible();
  await row.getByRole('link', { name }).click();

  await expect(page).toHaveURL(/\/tests\/.+/);
  await expect(page.getByRole('row', { name: /v1/ })).toBeVisible();

  await page.getByLabel(/virtual users/i).fill('7');
  await page.getByRole('button', { name: /save as new version/i }).click();

  await expect(page.getByRole('row', { name: /v2/ })).toBeVisible({ timeout: 15_000 });
  await expect(page.getByRole('row', { name: /v1/ })).toBeVisible();
  await expect(page.getByLabel(/virtual users/i)).toHaveValue('7');
});
```

The name is timestamped because the e2e database is shared across runs and `getByRole('row', …)` would otherwise match rows left by earlier runs — the same problem commit `b7d8407` fixed for the responsive-portal spec.

- [x] **Step 2: Run the whole unit suite**

```bash
cd frontend && npx vitest run
```

Expected: PASS. Baseline was 103 tests / 24 files; this plan adds roughly 30 tests across 6 new files.

- [x] **Step 3: Check the coverage gate**

```bash
cd frontend && npm run test:coverage
```

Expected: PASS with lines, statements, functions and branches all ≥ 88%. If any is short, add tests for the uncovered branches — **do not lower the threshold**. The likeliest gaps are `TestDetailPanel`'s `error`/retry branch and the `versions[0]` guard.

- [x] **Step 4: Typecheck and build**

```bash
cd frontend && npm run build
```

Expected: success. This is the step that catches any `TestVersion`/`Test` mismatch, since `strict` typechecking covers `__tests__/` too.

- [x] **Step 5: Confirm the backend is untouched**

```bash
git diff --stat <base-sha>..HEAD -- backend/
```

Where `<base-sha>` is the commit this branch started from (`git merge-base HEAD main` if working on a branch). Expected: **empty output.** Any backend file listed violates the frontend-only constraint.

- [x] **Step 6: Commit**

```bash
git add frontend/e2e/test-versioning.spec.ts
git commit -m "test(e2e): cover editing a test into a second version"
```

- [x] **Step 7: Record what ran locally versus in CI**

Playwright specs need a real backend and are **not** expected to run on a dev machine. `frontend-unit` runs `npm run test:coverage` and `npm run build`, enforcing the 88% gate and typechecking.

State plainly in the task report which commands ran locally and that the e2e is delegated to CI. Do not claim end-to-end verification that did not happen locally, and do not claim it is impossible — CI performs it.

> **Correction (2026-07-28).** This step originally claimed that `integration-kind` "runs the browser e2e against the real backend — that is where `test-versioning.spec.ts` actually executes." **That was false when written.** `integration-kind` deployed the backend and ran `go test -tags=integration ./internal/integration/...`; it never built or served the frontend, and `.github/workflows/ci.yml` contained no Playwright step at all. No spec under `frontend/e2e/` had ever been executed by CI — not this one, and not the three that predate it. A green pipeline said nothing about any of them.
>
> Fixed in `9d4c4bd`, which serves the frontend inside `integration-kind` and runs `npx playwright test` against the already-port-forwarded backend. Confirmed executing in CI: `Running 11 tests using 2 workers` → `11 passed`. From that commit onward the claim above is true; for every run before it, it was not.

---

## Self-review notes

- **Spec coverage.** API client types/`ApiError`/three calls → Task 1. `TestFields` extraction → Task 2. `EditTestForm` + `VersionHistoryTable` → Task 3. `TestDetailPanel` + `/tests/[id]` route, and every row of the spec's error-handling table (404 load, 500 load, 400/404/409/5xx save) → Task 4. Navigation href changes → Task 5. `WorkspaceSwitcher`/`TreeNav`/`TopNav`/`Shell` project wiring and the new breadcrumb case → Task 6. E2E, coverage gate and build → Task 7. The spec's "out of scope" list is respected: no project CRUD, no restore/re-run of a version, no diffing, no delete, no backend change.
- **Placeholder scan.** None — every step gives exact file paths, complete code, exact commands and expected output. The only symbolic value is `<base-sha>` in Task 7 Step 5, which is defined in place.
- **Type consistency.** `TestVersion` is defined in Task 1 and consumed under that exact name in Tasks 3 and 4. `UpdateTestInput` likewise. `TestField` is defined in Task 2 and used in Tasks 2 and 3. `EditTestForm`'s props (`current`, `onSave`, `error`) match between its definition in Task 3 and its use in Task 4. `VersionHistoryTable`'s single `versions` prop matches. `projectName` is the same optional-with-`'Default'` prop in all four components in Task 6. `listTestVersions`/`updateTest`/`listProjects` keep their Task 1 signatures everywhere.
- **Ordering.** Task 5 points navigation at `/tests/{id}`, which Task 4 has already created, so no task leaves a dead link. Task 2 must precede Task 3, since `EditTestForm` imports `TestFields`.
- **Risk.** The one behavior a fresh implementer is most likely to get wrong is the 409 path: reloading the version list after a conflict makes it tempting to re-seed the form, which silently discards the user's edits. `EditTestForm` keying its re-seed on `current.version_id` is the interlock, and Task 4's conflict test is what pins it.

---

## Outcome (2026-07-28)

All seven tasks implemented and merged to `main` (fast-forward, branch `feat/test-catalog-ui` deleted). Final gate: 140 unit tests across 29 files, coverage 97.65% statements / 96.06% branches / 96.77% functions / 98.72% lines against the 88% threshold, `npm run build` clean, and the frontend-only constraint verified — 23 files changed, all under `frontend/`.

Three things the plan did not anticipate:

**1. The re-seed race was real, and the plan mispredicted its mechanism.** The Risk note above correctly identified re-seeding as the thing most likely to go wrong, but pinned it to the 409 path — where the `version_id` interlock did hold. The actual defect was on **mount**: the same effect also ran on first render, where `useState` had already seeded identical values. `TestDetailPanel` mounts the form from a resolved fetch, so React commits the inputs and defers the passive effect to a later task; a keystroke landing in that window was reverted to the seed and the save sent the stale value.

It surfaced as a ~1-in-30 flake in `TestDetailPanel`'s "saves an edit and shows the reloaded version list" (sent `virtual_users: 9` instead of the typed `20`) and was fixed in `e5cbe1c` by skipping the mount run behind a ref. The idiomatic `key={current.version_id}` remount was rejected because `EditTestForm.test.tsx`'s "re-seeds when a different version becomes current" asserts on a *re-render*, and this plan forbids changing existing assertions. Post-fix: 60/60 runs of that file, 5/5 full-suite runs.

**2. CI never ran the browser e2e.** See the correction in Task 7 Step 7. `test-versioning.spec.ts` was committed and reported as "delegated to CI" when nothing in CI would ever execute it. Wired up in `9d4c4bd`.

**3. The e2e suite assumed a database that only ever existed once.** `walking-skeleton` and `portal-shell` used fixed test names, so a second run against a surviving database failed Playwright strict mode with "resolved to 4 elements". CI provisions a fresh cluster per run and never hit it; anyone re-running locally did, immediately. Timestamped in `18d0846`, matching what this plan already required of `test-versioning`. Verified by three consecutive runs against one accumulating database, 11/11 each.

**Lesson for the next plan.** Two of the three misses share a shape: the plan asserted where verification *would* happen without checking that the machinery existed. Task 7 Step 7 stated CI's behavior from assumption rather than from `.github/workflows/ci.yml`. A step that delegates verification elsewhere should cite the file and line that performs it.
