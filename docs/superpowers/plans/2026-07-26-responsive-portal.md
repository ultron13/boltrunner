# Responsive Portal (Phone + Tablet) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the BoltRunner portal usable on phone-width browsers, per
`docs/superpowers/specs/2026-07-26-responsive-portal-design.md`: a bottom tab bar replaces
the desktop top-nav module links and tree-nav sidebar below `md`, `DataTable` renders as
stacked cards instead of a table, and test management moves to a dedicated `/tests` route on
mobile.

**Architecture:** A single Tailwind `md` (768px) breakpoint. At/above it, today's desktop
layout is untouched. Below it: `TopNav` drops its module links, `TreeNav` disappears, a new
`BottomTabBar` takes over primary navigation, and `DataTable` swaps its `<table>` markup for
a `<ul>` of cards. The existing `Dashboard` page's create-form-and-list is extracted into a
shared `TestManagementPanel`, reused by both `Dashboard` (desktop only, KPI-only on mobile)
and a new `/tests` route (mobile's home for test management).

**Tech Stack:** Next.js 14 (App Router), React 18, TypeScript, Tailwind CSS (`hidden`,
`md:flex`, `md:table`, `md:hidden`, `md:block` responsive utilities — no new tokens), Vitest +
Testing Library (unit), Playwright (e2e, including a phone-viewport spec via `test.use({
viewport })`).

## Global Constraints

- Single breakpoint: Tailwind `md` (768px). No third, tablet-specific layout.
- Desktop/tablet (`md`+) behavior and markup must stay pixel-for-pixel what it is today —
  every new responsive class is additive (`hidden md:flex`, `md:table`, etc.), never a
  replacement of existing desktop classes.
- No new npm dependency for the bottom tab bar or card view — hand-rolled, matching every
  other `components/ui/` primitive.
- `BottomTabBar` takes no props (four tabs are hardcoded: Dashboard `/`, Tests `/tests`, Runs
  `/history`, Admin `/admin`) — same "hardcoded, single consumer" convention as `TreeNav`'s
  `📁 Default` label.
- `DataTable`'s `Column<T>` type gets no new fields. Card mode uses the first column in the
  `columns` array as the card title, the rest as `label: value` lines — convention, not
  configuration.
- **jsdom renders both the `<table>` and the `<ul>` card markup unconditionally** (no
  stylesheet is loaded in the Vitest environment, so Tailwind's `hidden`/`md:table`/`md:hidden`
  classes have no effect there — only Playwright's real browser actually hides/shows based on
  viewport). Any existing unit test that queries table content by plain text (not by
  `role="row"`/`role="columnheader"`, which only ever match `<tr>`/`<th>`, never the card
  `<li>`) will now match twice and must be scoped to `within(screen.getByRole('table'))` or
  rewritten as a `role="row"` query. This plan identifies every affected existing test by
  file:line — do not skip those fixes.
- Existing e2e specs (`portal-shell.spec.ts`, `walking-skeleton.spec.ts`) run at the default
  desktop viewport and must keep passing completely unchanged — do not edit them in this plan.

---

### Task 1: `DataTable` responsive card view

**Files:**
- Modify: `frontend/components/ui/DataTable.tsx`
- Modify: `frontend/__tests__/DataTable.test.tsx`
- Modify: `frontend/__tests__/TestList.test.tsx` (only consumer besides the history page whose
  tests are affected by the dual markup — see Global Constraints)

**Interfaces:**
- Consumes: nothing new.
- Produces: `DataTable`'s public props (`columns`, `rows`, `rowKey`, `onRowClick`,
  `emptyMessage`) are unchanged — later tasks import it exactly as today.

- [ ] **Step 1: Write the failing tests**

Replace the import line and append two tests to `frontend/__tests__/DataTable.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { DataTable, Column } from '@/components/ui/DataTable';
```

Append inside the existing `describe('DataTable', ...)` block (after the last `it(...)`):

```tsx
  it('renders the same rows as stacked cards for phone width, titled by the first column', () => {
    render(<DataTable columns={columns} rows={rows} rowKey={(r) => r.id} />);
    const list = screen.getByRole('list');
    const items = within(list).getAllByRole('listitem');
    expect(items).toHaveLength(1);
    expect(items[0]).toHaveTextContent('Alpha');
    expect(items[0]).toHaveTextContent('Count: 3');
  });

  it('calls onRowClick when a card is clicked', () => {
    const onRowClick = vi.fn();
    render(<DataTable columns={columns} rows={rows} rowKey={(r) => r.id} onRowClick={onRowClick} />);
    const card = within(screen.getByRole('list')).getByRole('listitem');
    fireEvent.click(card);
    expect(onRowClick).toHaveBeenCalledWith(rows[0]);
  });
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && npx vitest run __tests__/DataTable.test.tsx`
Expected: FAIL — `TestingLibraryElementError: Unable to find role="list"` (no card markup exists yet).

- [ ] **Step 3: Implement the card view in `DataTable.tsx`**

Replace the full contents of `frontend/components/ui/DataTable.tsx`:

```tsx
import { ReactNode } from 'react';

export type Column<T> = {
  key: string;
  header: string;
  align?: 'numeric';
  render?: (row: T) => ReactNode;
};

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  onRowClick,
  emptyMessage = 'No data.',
}: {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  onRowClick?: (row: T) => void;
  emptyMessage?: string;
}) {
  if (rows.length === 0) {
    return <p className="text-text-muted p-4">{emptyMessage}</p>;
  }

  function cellValue(col: Column<T>, row: T): ReactNode {
    return col.render ? col.render(row) : String((row as Record<string, unknown>)[col.key] ?? '');
  }

  const [titleCol, ...restCols] = columns;

  return (
    <>
      <table className="hidden md:table w-full text-sm border-collapse">
        <thead className="sticky top-0 bg-surface-alt">
          <tr>
            {columns.map((col) => (
              <th
                key={col.key}
                className={`text-left px-3 py-2 border-b border-border ${
                  col.align === 'numeric' ? 'text-right font-mono' : ''
                }`}
              >
                {col.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr
              key={rowKey(row)}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              className={`${i % 2 === 1 ? 'bg-surface-alt' : 'bg-surface'} ${
                onRowClick ? 'cursor-pointer hover:bg-surface-alt' : ''
              }`}
            >
              {columns.map((col) => (
                <td
                  key={col.key}
                  className={`px-3 py-2 border-b border-border ${
                    col.align === 'numeric' ? 'text-right font-mono' : ''
                  }`}
                >
                  {cellValue(col, row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <ul className="md:hidden flex flex-col gap-2 p-3">
        {rows.map((row) => (
          <li
            key={rowKey(row)}
            onClick={onRowClick ? () => onRowClick(row) : undefined}
            className={`border border-border rounded p-3 bg-surface ${
              onRowClick ? 'cursor-pointer hover:bg-surface-alt' : ''
            }`}
          >
            <div className="font-medium text-text">{cellValue(titleCol, row)}</div>
            {restCols.map((col) => (
              <div key={col.key} className="text-sm text-text-muted">
                {col.header && `${col.header}: `}
                {cellValue(col, row)}
              </div>
            ))}
          </li>
        ))}
      </ul>
    </>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd frontend && npx vitest run __tests__/DataTable.test.tsx`
Expected: PASS (6 tests: the original 4 plus the 2 new ones).

- [ ] **Step 5: Fix the now-ambiguous `TestList.test.tsx` queries**

`TestList` renders a "Run" button per row via a column `render` function
(`frontend/components/TestList.tsx:15`) — that button now appears in both the `<table>` row
and the new card `<li>`, so an unscoped `getByRole('button', { name: /run/i })` matches twice.
Replace the full contents of `frontend/__tests__/TestList.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { TestList } from '@/components/TestList';

const tests = [
  { id: '1', name: 'Checkout Load', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
];

describe('TestList', () => {
  it('shows the empty message when there are no tests', () => {
    render(<TestList tests={[]} onStart={() => {}} />);
    expect(screen.getByText('No tests yet — create one above.')).toBeInTheDocument();
  });

  it('renders a row per test with a Run button', () => {
    render(<TestList tests={tests} onStart={() => {}} />);
    expect(screen.getByRole('row', { name: /Checkout Load/i })).toBeInTheDocument();
    expect(within(screen.getByRole('table')).getByRole('button', { name: /run/i })).toBeInTheDocument();
  });

  it('calls onStart with the test id when Run is clicked', () => {
    const onStart = vi.fn();
    render(<TestList tests={tests} onStart={onStart} />);
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: /run/i }));
    expect(onStart).toHaveBeenCalledWith('1');
  });
});
```

- [ ] **Step 6: Run the full frontend unit suite to confirm no other regressions**

Run: `cd frontend && npx vitest run`
Expected: PASS. (The history page's tests are unaffected — every history-page assertion
already queries by `role="row"`, which only ever matches `<tr>` elements, never the new
card `<li>`s, so they stay unambiguous.)

- [ ] **Step 7: Commit**

```bash
git add frontend/components/ui/DataTable.tsx frontend/__tests__/DataTable.test.tsx frontend/__tests__/TestList.test.tsx
git commit -m "feat(frontend): render DataTable rows as stacked cards below md"
```

---

### Task 2: Extract `TestManagementPanel`, split `DashboardPage`'s tests

**Files:**
- Create: `frontend/components/TestManagementPanel.tsx`
- Create: `frontend/__tests__/TestManagementPanel.test.tsx`
- Modify: `frontend/app/page.tsx`
- Modify: `frontend/__tests__/DashboardPage.test.tsx`

**Interfaces:**
- Consumes: `TestList` (`frontend/components/TestList.tsx`, unchanged props), `CreateTestForm`
  (`frontend/components/CreateTestForm.tsx`, unchanged props), `listTests`/`startRun`/`Test`
  from `@/lib/api-client`.
- Produces: `export function TestManagementPanel(): JSX.Element` — no props, fetches its own
  tests, renders `CreateTestForm` + `TestList`. Task 3 imports it as
  `import { TestManagementPanel } from '@/components/TestManagementPanel';` with no props.

- [ ] **Step 1: Write the failing test for the new component**

Create `frontend/__tests__/TestManagementPanel.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { TestManagementPanel } from '@/components/TestManagementPanel';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe('TestManagementPanel', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('renders the create form and test list', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(<TestManagementPanel />);
    expect(screen.getByRole('button', { name: /create test/i })).toBeInTheDocument();
    expect(await screen.findByText('No tests yet — create one above.')).toBeInTheDocument();
  });

  it('shows tests once they load', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: '1', name: 'Critical Test', target_url: 'http://x', virtual_users: 10, duration_seconds: 60, created_at: '2026-07-24T00:00:00Z' },
    ]);

    render(<TestManagementPanel />);

    await expect(screen.findByRole('row', { name: /Critical Test/i })).resolves.toBeInTheDocument();
  });

  it('shows an empty list when listTests fails', async () => {
    vi.spyOn(api, 'listTests').mockRejectedValue(new Error('boom'));
    render(<TestManagementPanel />);
    expect(await screen.findByText('No tests yet — create one above.')).toBeInTheDocument();
  });
});
```

Note: the middle test asserts via `getByRole('row', ...)`, not `getByText(...)` — this stays
unambiguous even with Task 1's dual table/card markup in place (see Global Constraints).

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/TestManagementPanel.test.tsx`
Expected: FAIL — `Failed to resolve import "@/components/TestManagementPanel"` (module doesn't exist yet).

- [ ] **Step 3: Create `TestManagementPanel.tsx`**

Create `frontend/components/TestManagementPanel.tsx`:

```tsx
'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { listTests, startRun, Test } from '@/lib/api-client';
import { CreateTestForm } from '@/components/CreateTestForm';
import { TestList } from '@/components/TestList';

export function TestManagementPanel() {
  const [tests, setTests] = useState<Test[]>([]);
  const router = useRouter();

  useEffect(() => {
    listTests()
      .then(setTests)
      .catch(() => setTests([]));
  }, []);

  async function handleStart(testId: string) {
    const run = await startRun(testId);
    router.push(`/runs/${run.id}`);
  }

  function handleCreated(t: Test) {
    setTests((prev) => [t, ...prev]);
  }

  return (
    <div className="flex flex-col gap-6">
      <CreateTestForm onCreated={handleCreated} />
      <TestList tests={tests} onStart={handleStart} />
    </div>
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/TestManagementPanel.test.tsx`
Expected: PASS (3 tests).

- [ ] **Step 5: Update `DashboardPage` to use the panel, gated to `md`+**

Replace the full contents of `frontend/app/page.tsx`:

```tsx
'use client';

import { useEffect, useState } from 'react';
import { listTests, listRunsForTest, Test } from '@/lib/api-client';
import { TestManagementPanel } from '@/components/TestManagementPanel';
import { KpiTile } from '@/components/ui/KpiTile';

export default function DashboardPage() {
  const [tests, setTests] = useState<Test[]>([]);
  const [activeRuns, setActiveRuns] = useState(0);

  useEffect(() => {
    listTests()
      .then((fetched) => {
        setTests(fetched);
        Promise.all(fetched.map((t) => listRunsForTest(t.id)))
          .then((runLists) => {
            const running = runLists.flat().filter((r) => r.status === 'running').length;
            setActiveRuns(running);
          })
          .catch(() => {
            setActiveRuns(0);
          });
      })
      .catch(() => {
        setTests([]);
        setActiveRuns(0);
      });
  }, []);

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-text">Dashboard</h1>
      <div className="grid grid-cols-2 gap-4 max-w-md">
        <KpiTile label="Total Tests" value={tests.length} />
        <KpiTile label="Active Runs" value={activeRuns} />
      </div>
      <div className="hidden md:block">
        <TestManagementPanel />
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Trim `DashboardPage.test.tsx` to only its own (KPI) concerns**

The create-form/list assertions now belong to `TestManagementPanel.test.tsx` (Step 1 above).
Replace the full contents of `frontend/__tests__/DashboardPage.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import DashboardPage from '@/app/page';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe('DashboardPage', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('shows Total Tests and Active Runs KPI tiles computed from fetched data', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: '1', name: 'A', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
      { id: '2', name: 'B', target_url: 'http://y', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockImplementation(async (testId: string) =>
      testId === '1' ? [{ id: 'r1', test_id: '1', status: 'running', created_at: '2026-07-24T00:00:00Z' }] : []
    );

    render(<DashboardPage />);

    const totalTile = await screen.findByText('Total Tests');
    expect(totalTile.closest('div')?.textContent).toContain('2');

    const activeTile = await screen.findByText('Active Runs');
    expect(activeTile.closest('div')?.textContent).toContain('1');
  });

  it('resets Active Runs to 0 when the runs fetch fails', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: '1', name: 'Critical Test', target_url: 'http://x', virtual_users: 10, duration_seconds: 60, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockRejectedValue(new Error('Network error fetching runs'));

    render(<DashboardPage />);

    const activeTile = await screen.findByText('Active Runs');
    expect(activeTile.closest('div')?.textContent).toContain('0');
  });

  it('shows zeroed KPIs when listTests itself fails', async () => {
    vi.spyOn(api, 'listTests').mockRejectedValue(new Error('boom'));

    render(<DashboardPage />);

    const totalTile = await screen.findByText('Total Tests');
    expect(totalTile.closest('div')?.textContent).toContain('0');
    const activeTile = await screen.findByText('Active Runs');
    expect(activeTile.closest('div')?.textContent).toContain('0');
  });

  it('renders the test management panel', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(<DashboardPage />);
    expect(await screen.findByRole('button', { name: /create test/i })).toBeInTheDocument();
  });
});
```

- [ ] **Step 7: Run the full frontend unit suite to confirm no regressions**

Run: `cd frontend && npx vitest run`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add frontend/components/TestManagementPanel.tsx frontend/__tests__/TestManagementPanel.test.tsx frontend/app/page.tsx frontend/__tests__/DashboardPage.test.tsx
git commit -m "refactor(frontend): extract TestManagementPanel from DashboardPage"
```

---

### Task 3: New `/tests` route

**Files:**
- Create: `frontend/app/tests/page.tsx`
- Create: `frontend/__tests__/TestsPage.test.tsx`

**Interfaces:**
- Consumes: `TestManagementPanel` from Task 2 (`import { TestManagementPanel } from '@/components/TestManagementPanel';`, no props).
- Produces: nothing new consumed by later tasks — Task 6 (`Shell`) only needs to know the
  route `/tests` exists for its breadcrumb case, not any exported symbol from this page.

- [ ] **Step 1: Write the failing test**

Create `frontend/__tests__/TestsPage.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import TestsPage from '@/app/tests/page';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe('TestsPage', () => {
  it('renders the Tests heading and the test management panel', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(<TestsPage />);
    expect(screen.getByRole('heading', { name: 'Tests' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create test/i })).toBeInTheDocument();
    expect(await screen.findByText('No tests yet — create one above.')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/TestsPage.test.tsx`
Expected: FAIL — `Failed to resolve import "@/app/tests/page"` (route doesn't exist yet).

- [ ] **Step 3: Create the route**

Create `frontend/app/tests/page.tsx`:

```tsx
'use client';

import { TestManagementPanel } from '@/components/TestManagementPanel';

export default function TestsPage() {
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-text">Tests</h1>
      <TestManagementPanel />
    </div>
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd frontend && npx vitest run __tests__/TestsPage.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/app/tests/page.tsx frontend/__tests__/TestsPage.test.tsx
git commit -m "feat(frontend): add /tests route for mobile test management"
```

---

### Task 4: `BottomTabBar` component

**Files:**
- Create: `frontend/components/ui/BottomTabBar.tsx`
- Create: `frontend/__tests__/BottomTabBar.test.tsx`

**Interfaces:**
- Consumes: nothing (no props, hardcoded tabs — see Global Constraints).
- Produces: `export function BottomTabBar(): JSX.Element` — a `<nav aria-label="Primary">`
  containing four `Link`s (`Dashboard`/`Tests`/`Runs`/`Admin`, hrefs `/`, `/tests`, `/history`,
  `/admin`), root classes include `md:hidden`. Task 6 (`Shell`) imports it as
  `import { BottomTabBar } from '@/components/ui/BottomTabBar';` and renders `<BottomTabBar />`
  with no props.

- [ ] **Step 1: Write the failing tests**

Create `frontend/__tests__/BottomTabBar.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { BottomTabBar } from '@/components/ui/BottomTabBar';

vi.mock('next/navigation', () => ({
  usePathname: () => '/history',
}));

describe('BottomTabBar', () => {
  it('renders all four tabs with correct hrefs', () => {
    render(<BottomTabBar />);
    expect(screen.getByRole('link', { name: /dashboard/i })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: /tests/i })).toHaveAttribute('href', '/tests');
    expect(screen.getByRole('link', { name: /runs/i })).toHaveAttribute('href', '/history');
    expect(screen.getByRole('link', { name: /admin/i })).toHaveAttribute('href', '/admin');
  });

  it('marks the tab matching the current path as active', () => {
    render(<BottomTabBar />);
    expect(screen.getByRole('link', { name: /runs/i })).toHaveClass('text-accent');
    expect(screen.getByRole('link', { name: /dashboard/i })).not.toHaveClass('text-accent');
  });

  it('is hidden below md and shown at md and up', () => {
    render(<BottomTabBar />);
    expect(screen.getByRole('navigation', { name: 'Primary' })).toHaveClass('md:hidden');
  });
});
```

Note this file needs `import { vi } from 'vitest';` alongside `describe, it, expect` — write
the full import line as:

```tsx
import { describe, it, expect, vi } from 'vitest';
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && npx vitest run __tests__/BottomTabBar.test.tsx`
Expected: FAIL — `Failed to resolve import "@/components/ui/BottomTabBar"` (module doesn't exist yet).

- [ ] **Step 3: Implement the component**

Create `frontend/components/ui/BottomTabBar.tsx`:

```tsx
'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';

const TABS = [
  { label: 'Dashboard', href: '/', icon: '🏠' },
  { label: 'Tests', href: '/tests', icon: '📄' },
  { label: 'Runs', href: '/history', icon: '⏱' },
  { label: 'Admin', href: '/admin', icon: '⚙' },
];

export function BottomTabBar() {
  const pathname = usePathname();
  return (
    <nav
      aria-label="Primary"
      className="md:hidden fixed inset-x-0 bottom-0 bg-chrome text-chrome-fg flex text-xs border-t border-border"
    >
      {TABS.map((tab) => {
        const active = pathname === tab.href;
        return (
          <Link
            key={tab.href}
            href={tab.href}
            className={`flex-1 flex flex-col items-center gap-0.5 py-2 ${active ? 'text-accent' : 'text-chrome-fg'}`}
          >
            <span aria-hidden="true">{tab.icon}</span>
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd frontend && npx vitest run __tests__/BottomTabBar.test.tsx`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/BottomTabBar.tsx frontend/__tests__/BottomTabBar.test.tsx
git commit -m "feat(frontend): add BottomTabBar component"
```

---

### Task 5: `TopNav` hides module links below `md`

**Files:**
- Modify: `frontend/components/ui/TopNav.tsx`
- Modify: `frontend/__tests__/TopNav.test.tsx`

**Interfaces:**
- Consumes: nothing new.
- Produces: `TopNav`'s props (`{ modules: NavModule[] }`) are unchanged.

- [ ] **Step 1: Write the failing test**

Append inside the existing `describe('TopNav', ...)` block in `frontend/__tests__/TopNav.test.tsx`
(after the `'includes a theme toggle'` test, before the closing `});`):

```tsx

  it('wraps the module links so they are hidden below md and shown at md and up', () => {
    render(
      <ThemeProvider>
        <TopNav modules={modules} />
      </ThemeProvider>
    );
    const dashboardLink = screen.getByRole('link', { name: 'Dashboard' });
    expect(dashboardLink.closest('nav')).toHaveClass('hidden', 'md:flex');
  });
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run __tests__/TopNav.test.tsx`
Expected: FAIL — `expect(received).toHaveClass()` fails because `closest('nav')` is `null` (no `<nav>` wraps the links yet).

- [ ] **Step 3: Wrap the module links in `TopNav.tsx`**

Replace the full contents of `frontend/components/ui/TopNav.tsx`:

```tsx
'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { ThemeToggle } from '@/components/ui/ThemeToggle';
import { WorkspaceSwitcher } from '@/components/ui/WorkspaceSwitcher';

export type NavModule = { label: string; href: string };

export function TopNav({ modules }: { modules: NavModule[] }) {
  const pathname = usePathname();
  return (
    <header className="bg-chrome text-chrome-fg flex items-center justify-between px-4 py-2 text-sm">
      <div className="flex items-center gap-4">
        <span className="font-semibold">BoltRunner</span>
        <WorkspaceSwitcher />
        <nav className="hidden md:flex items-center gap-4">
          {modules.map((m) => {
            const active = pathname === m.href;
            return (
              <Link key={m.label} href={m.href} className={`pb-1 ${active ? 'border-b-2 border-accent' : ''}`}>
                {m.label}
              </Link>
            );
          })}
        </nav>
      </div>
      <ThemeToggle />
    </header>
  );
}
```

- [ ] **Step 4: Run the full `TopNav` test file to verify all cases pass**

Run: `cd frontend && npx vitest run __tests__/TopNav.test.tsx`
Expected: PASS (5 tests — the 4 original plus the new one).

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/TopNav.tsx frontend/__tests__/TopNav.test.tsx
git commit -m "feat(frontend): hide TopNav module links below md"
```

---

### Task 6: `Shell` wires in `BottomTabBar`, hides `TreeNav`, adds the `/tests` breadcrumb

**Files:**
- Modify: `frontend/components/ui/Shell.tsx`
- Modify: `frontend/__tests__/Shell.test.tsx`

**Interfaces:**
- Consumes: `BottomTabBar` from Task 4 (`import { BottomTabBar } from '@/components/ui/BottomTabBar';`, no props).
- Produces: `Shell`'s props (`{ children: ReactNode }`) are unchanged.

- [ ] **Step 1: Write the failing tests**

Append inside the existing `describe('Shell', ...)` block in `frontend/__tests__/Shell.test.tsx`
(after the last `it(...)`, before the closing `});`):

```tsx

  it('renders the bottom tab bar', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );
    expect(await screen.findByRole('navigation', { name: 'Primary' })).toBeInTheDocument();
  });

  it('wraps the tree nav so it is hidden below md and shown at md and up', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );
    const treeNav = await screen.findByRole('navigation', { name: 'Workspace' });
    expect(treeNav.parentElement).toHaveClass('hidden', 'md:block');
  });

  it('shows a Tests breadcrumb on the tests path', async () => {
    vi.mocked(usePathname).mockReturnValue('/tests');
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(
      <ThemeProvider>
        <Shell>
          <p>tests content</p>
        </Shell>
      </ThemeProvider>
    );
    expect(await screen.findByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent('Tests');
  });
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && npx vitest run __tests__/Shell.test.tsx`
Expected: FAIL — the "bottom tab bar" test fails with `Unable to find role="navigation" and name "Primary"`;
the "tree nav" test fails because `treeNav.parentElement` doesn't carry `hidden`/`md:block` yet;
the "Tests breadcrumb" test fails because `breadcrumbFor` has no `/tests` case (falls through
to the default `[root]`, so the text `'Tests'` isn't present).

- [ ] **Step 3: Update `Shell.tsx`**

Replace the full contents of `frontend/components/ui/Shell.tsx`:

```tsx
'use client';

import { ReactNode, useEffect, useState } from 'react';
import { usePathname, useSearchParams } from 'next/navigation';
import { listTests, Test } from '@/lib/api-client';
import { TopNav } from '@/components/ui/TopNav';
import { TreeNav } from '@/components/ui/TreeNav';
import { BottomTabBar } from '@/components/ui/BottomTabBar';
import { Breadcrumb, BreadcrumbItem } from '@/components/ui/Breadcrumb';

const MODULES = [
  { label: 'Dashboard', href: '/' },
  { label: 'Test Management', href: '/' },
  { label: 'Test Runs', href: '/history' },
  { label: 'Admin', href: '/admin' },
];

function breadcrumbFor(pathname: string, testId: string | null, testName?: string): BreadcrumbItem[] {
  const root: BreadcrumbItem = { label: 'Default', href: '/' };
  if (pathname === '/') return [root];
  if (pathname === '/admin') return [root, { label: 'Admin' }];
  if (pathname === '/tests') return [root, { label: 'Tests' }];
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

export function Shell({ children }: { children: ReactNode }) {
  const [tests, setTests] = useState<Test[]>([]);
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const testId = searchParams.get('testId');

  useEffect(() => {
    listTests().then(setTests).catch(() => setTests([]));
  }, []);

  const activeTest = tests.find((t) => t.id === testId);
  const crumbs = breadcrumbFor(pathname, testId, activeTest?.name);

  return (
    <div className="min-h-screen flex flex-col bg-surface-alt text-text">
      <TopNav modules={MODULES} />
      <div className="flex flex-1">
        <div className="hidden md:block">
          <TreeNav tests={tests} activeTestId={testId ?? undefined} />
        </div>
        <div className="flex-1 flex flex-col">
          <Breadcrumb items={crumbs} />
          <main className="flex-1 p-6 pb-20 md:pb-6">{children}</main>
        </div>
      </div>
      <BottomTabBar />
    </div>
  );
}
```

- [ ] **Step 4: Run the full `Shell` test file to verify all cases pass**

Run: `cd frontend && npx vitest run __tests__/Shell.test.tsx`
Expected: PASS (11 tests — the 8 original plus the 3 new ones).

- [ ] **Step 5: Run the full frontend unit suite to confirm no other regressions**

Run: `cd frontend && npx vitest run`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/components/ui/Shell.tsx frontend/__tests__/Shell.test.tsx
git commit -m "feat(frontend): wire BottomTabBar into Shell, hide TreeNav below md"
```

---

### Task 7: Phone-viewport e2e coverage

**Files:**
- Create: `frontend/e2e/responsive-portal.spec.ts`

**Interfaces:**
- Consumes: the running app from Tasks 1-6 (no code interfaces — this is a Playwright test
  driving the browser at a phone-sized viewport via `test.use({ viewport })`).
- Produces: nothing consumed elsewhere — final task in this plan.

- [ ] **Step 1: Write the e2e spec**

Create `frontend/e2e/responsive-portal.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

test.describe('responsive portal (phone viewport)', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('shows the bottom tab bar instead of the desktop nav and tree', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('navigation', { name: 'Primary' })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Workspace' })).toBeHidden();
    await expect(page.getByRole('link', { name: 'Test Runs' })).toBeHidden();
  });

  test('navigates between all four tabs', async ({ page }) => {
    await page.goto('/');
    const tabBar = page.getByRole('navigation', { name: 'Primary' });

    await tabBar.getByRole('link', { name: /tests/i }).click();
    await expect(page).toHaveURL(/\/tests$/);
    await expect(page.getByRole('heading', { name: 'Tests' })).toBeVisible();

    await tabBar.getByRole('link', { name: /runs/i }).click();
    await expect(page).toHaveURL(/\/history$/);

    await tabBar.getByRole('link', { name: /admin/i }).click();
    await expect(page).toHaveURL(/\/admin$/);

    await tabBar.getByRole('link', { name: /dashboard/i }).click();
    await expect(page).toHaveURL(/\/$/);
  });

  test('history table renders as stacked cards instead of a table', async ({ page }) => {
    await page.goto('/tests');
    await page.getByLabel(/name/i).fill('Mobile E2E Test');
    await page.getByLabel(/target url/i).fill('http://boltrunner-backend.boltrunner.svc:8080/healthz');
    await page.getByLabel(/virtual users/i).fill('2');
    await page.getByLabel(/duration/i).fill('10');
    await page.getByRole('button', { name: /create test/i }).click();
    await page.getByRole('button', { name: /run/i }).click();
    await expect(page).toHaveURL(/\/runs\/.+/);

    await page.getByRole('navigation', { name: 'Primary' }).getByRole('link', { name: /runs/i }).click();
    await expect(page).toHaveURL(/\/history$/);

    await expect(page.getByRole('table')).toBeHidden();
    await expect(page.getByText('Mobile E2E Test')).toBeVisible();
  });
});
```

- [ ] **Step 2: Run the new spec**

Run: `cd frontend && npx playwright test e2e/responsive-portal.spec.ts`
Expected: PASS (3 tests). Requires the dev stack (`deploy/dev-up.sh` + `npm run dev`) already
running, same as every other spec in `frontend/e2e/`.

- [ ] **Step 3: Run the existing desktop-viewport specs to confirm they still pass unchanged**

Run: `cd frontend && npx playwright test e2e/portal-shell.spec.ts e2e/walking-skeleton.spec.ts`
Expected: PASS, identical to their behavior before this plan (they run at the default desktop
viewport, entirely above the `md` breakpoint).

- [ ] **Step 4: Commit**

```bash
git add frontend/e2e/responsive-portal.spec.ts
git commit -m "test(e2e): cover the phone-viewport responsive layout"
```

---

## Self-review notes

- **Spec coverage:** all 6 spec decisions map to a task — breakpoint (Global Constraints,
  applied throughout), nav pattern/bottom tab bar (Tasks 4-6), tab set (Task 4), Tests-tab
  content/panel extraction (Tasks 2-3), DataTable card view (Task 1), "everything else
  unchanged" (no task touches `CreateTestForm`, `MetricsChart`, `Tabs`, `Breadcrumb`, or
  `AdminPage`).
- **Placeholder scan:** none — every step has real code/commands.
- **Type consistency:** `TestManagementPanel` exported with no props in Task 2, imported with
  no props in Task 3; `BottomTabBar` exported with no props in Task 4, imported with no props
  in Task 6; `DataTable`'s `Column<T>` and its consumer props are untouched throughout.
- **jsdom/dual-markup risk:** traced every existing consumer of `DataTable` (`TestList.tsx`,
  `history/page.tsx` — confirmed by `grep -rl DataTable frontend/app frontend/components`) and
  every existing test file touching either. `HistoryPage.test.tsx` needed no changes (already
  `role="row"`-scoped throughout); `TestList.test.tsx` needed scoping fixes (Task 1, Step 5);
  the one plain-text assertion at risk (`DashboardPage.test.tsx`'s old "Critical Test" check)
  is rewritten as a `role="row"` query when it moves to `TestManagementPanel.test.tsx` (Task 2).
