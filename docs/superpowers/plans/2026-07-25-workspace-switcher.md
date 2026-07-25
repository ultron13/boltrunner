# Workspace Switcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the project/team workspace switcher to `TopNav`, the last unbuilt piece of BOL-22, per `docs/superpowers/specs/2026-07-25-workspace-switcher-design.md`.

**Architecture:** A new self-contained client component, `WorkspaceSwitcher`, hand-rolled (no dropdown/menu dependency) to match every other `components/ui/` primitive. It renders a trigger button plus a conditionally-rendered `role="menu"` with one checked "Default" item and one disabled "New project" item. It is rendered in `TopNav` right after the brand text. No backend calls, no new props — same hardcoded-"Default" pattern already used by `TreeNav`.

**Tech Stack:** Next.js 14 (App Router), React 18, TypeScript, Tailwind CSS (semantic tokens: `bg-chrome`, `text-chrome-fg`, `bg-surface`, `border-border`, `text-text`, `text-text-muted`, `text-accent`), Vitest + Testing Library (unit), Playwright (e2e).

## Global Constraints

- No new npm dependency — hand-roll the dropdown (spec decision 5).
- Single hardcoded "Default" workspace, no backend call (spec decision 2).
- Switcher goes in `TopNav`, immediately after the `BoltRunner` brand span, before the module links (spec decision 3).
- Menu contains exactly two items: "Default" (checked, closes menu on select) and "New project" (disabled, inert) (spec decision 4).
- `frontend/__tests__/TopNav.test.tsx` needs no changes — the brand text stays its own element (spec, Architecture section).
- 88% coverage threshold already applies repo-wide (from the parent BOL-22 spec) — this component's own unit tests must fully exercise it.

---

### Task 1: `WorkspaceSwitcher` component

**Files:**
- Create: `frontend/components/ui/WorkspaceSwitcher.tsx`
- Test: `frontend/__tests__/WorkspaceSwitcher.test.tsx`

**Interfaces:**
- Consumes: nothing from other components (self-contained, no props).
- Produces: `export function WorkspaceSwitcher(): JSX.Element` — a `<div>` root containing a trigger `<button>` with accessible name matching `/default/i` and `aria-expanded`/`aria-haspopup="menu"`, and (while open) a `role="menu"` containing a `role="menuitemradio"` item named `/default/i` with `aria-checked="true"`, and a disabled `<button>` named `/new project/i`. Task 2 imports this as `import { WorkspaceSwitcher } from '@/components/ui/WorkspaceSwitcher';`.

- [ ] **Step 1: Write the failing tests**

Create `frontend/__tests__/WorkspaceSwitcher.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { WorkspaceSwitcher } from '@/components/ui/WorkspaceSwitcher';

describe('WorkspaceSwitcher', () => {
  it('renders closed by default', () => {
    render(<WorkspaceSwitcher />);
    expect(screen.getByRole('button', { name: /default/i })).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('opens the menu on click, showing Default checked and New project disabled', () => {
    render(<WorkspaceSwitcher />);
    fireEvent.click(screen.getByRole('button', { name: /default/i }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
    expect(screen.getByRole('menuitemradio', { name: /default/i })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('button', { name: /new project/i })).toBeDisabled();
  });

  it('closes the menu when Default is selected', () => {
    render(<WorkspaceSwitcher />);
    fireEvent.click(screen.getByRole('button', { name: /default/i }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: /default/i }));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('closes on Escape and returns focus to the trigger', () => {
    render(<WorkspaceSwitcher />);
    const trigger = screen.getByRole('button', { name: /default/i });
    fireEvent.click(trigger);
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('closes when clicking outside', () => {
    render(
      <div>
        <WorkspaceSwitcher />
        <button>outside</button>
      </div>
    );
    fireEvent.click(screen.getByRole('button', { name: /default/i }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByRole('button', { name: 'outside' }));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('does nothing when the disabled New project item is clicked', () => {
    render(<WorkspaceSwitcher />);
    fireEvent.click(screen.getByRole('button', { name: /default/i }));
    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && npx vitest run __tests__/WorkspaceSwitcher.test.tsx`
Expected: FAIL — `Failed to resolve import "@/components/ui/WorkspaceSwitcher"` (module doesn't exist yet).

- [ ] **Step 3: Implement the component**

Create `frontend/components/ui/WorkspaceSwitcher.tsx`:

```tsx
'use client';

import { KeyboardEvent, useEffect, useRef, useState } from 'react';

export function WorkspaceSwitcher() {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;

    function handleMouseDown(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleMouseDown);
    return () => document.removeEventListener('mousedown', handleMouseDown);
  }, [open]);

  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Escape') {
      setOpen(false);
      triggerRef.current?.focus();
    }
  }

  return (
    <div ref={rootRef} className="relative" onKeyDown={handleKeyDown}>
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1 text-chrome-fg px-2 py-1 rounded hover:bg-white/10"
      >
        Default <span aria-hidden="true">▾</span>
      </button>
      {open && (
        <div
          role="menu"
          aria-label="Workspaces"
          className="absolute left-0 mt-1 w-40 rounded border border-border bg-surface text-text shadow-lg z-10"
        >
          <button
            type="button"
            role="menuitemradio"
            aria-checked="true"
            onClick={() => setOpen(false)}
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-surface-alt"
          >
            <span aria-hidden="true">✓</span> Default
          </button>
          <button
            type="button"
            disabled
            aria-disabled="true"
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-text-muted cursor-not-allowed"
          >
            + New project
          </button>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd frontend && npx vitest run __tests__/WorkspaceSwitcher.test.tsx`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/components/ui/WorkspaceSwitcher.tsx frontend/__tests__/WorkspaceSwitcher.test.tsx
git commit -m "feat(frontend): add WorkspaceSwitcher component"
```

---

### Task 2: Integrate `WorkspaceSwitcher` into `TopNav`

**Files:**
- Modify: `frontend/components/ui/TopNav.tsx`

**Interfaces:**
- Consumes: `WorkspaceSwitcher` from Task 1 (`import { WorkspaceSwitcher } from '@/components/ui/WorkspaceSwitcher';`), rendered with no props.
- Produces: nothing new consumed by later tasks — Task 3's e2e test just drives the already-rendered page.

- [ ] **Step 1: Edit `TopNav.tsx`**

In `frontend/components/ui/TopNav.tsx`, add the import and render `<WorkspaceSwitcher />` right after the brand span:

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
        {modules.map((m) => {
          const active = pathname === m.href;
          return (
            <Link key={m.label} href={m.href} className={`pb-1 ${active ? 'border-b-2 border-accent' : ''}`}>
              {m.label}
            </Link>
          );
        })}
      </div>
      <ThemeToggle />
    </header>
  );
}
```

- [ ] **Step 2: Run the existing `TopNav` and `Shell` unit tests to confirm no regressions**

Run: `cd frontend && npx vitest run __tests__/TopNav.test.tsx __tests__/Shell.test.tsx`
Expected: PASS, unchanged — `getByText('BoltRunner')` still matches the brand span exactly since `WorkspaceSwitcher` is a sibling element, not a wrapper.

- [ ] **Step 3: Run the full frontend unit suite to confirm no other regressions**

Run: `cd frontend && npx vitest run`
Expected: PASS, all suites green (including `WorkspaceSwitcher.test.tsx` from Task 1).

- [ ] **Step 4: Commit**

```bash
git add frontend/components/ui/TopNav.tsx
git commit -m "feat(frontend): render WorkspaceSwitcher in TopNav"
```

---

### Task 3: e2e coverage

**Files:**
- Modify: `frontend/e2e/portal-shell.spec.ts`

**Interfaces:**
- Consumes: the running app shell from Task 2 (no code interfaces — this is a Playwright test driving the browser).
- Produces: nothing consumed elsewhere — final task in this plan.

- [ ] **Step 1: Add the e2e test**

Append to `frontend/e2e/portal-shell.spec.ts` (after the existing `admin page renders...` test):

```ts
test('workspace switcher shows Default checked and a disabled New project action', async ({ page }) => {
  await page.goto('/');
  const trigger = page.getByRole('button', { name: /default/i });
  await trigger.click();
  await expect(page.getByRole('menuitemradio', { name: /default/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /new project/i })).toBeDisabled();

  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).toBeHidden();
});
```

- [ ] **Step 2: Run the e2e suite**

Run: `cd frontend && npx playwright test e2e/portal-shell.spec.ts`
Expected: PASS, including the new test. (Requires the dev stack from `docker-compose.yml` / whatever the existing `portal-shell.spec.ts` tests already assume — same setup already used by the other tests in this file, no new setup needed.)

- [ ] **Step 3: Commit**

```bash
git add frontend/e2e/portal-shell.spec.ts
git commit -m "test(e2e): cover the workspace switcher"
```

---

## Self-review notes

- **Spec coverage:** all 5 spec decisions map to a task — data/scope (Task 1 props-less component), placement (Task 2), menu content (Task 1 test/impl), implementation style (Task 1, no new dependency), testing strategy (Tasks 1 and 3; no `TopNav.test.tsx` changes as the spec specifies).
- **Placeholder scan:** none — every step has real code/commands.
- **Type consistency:** `WorkspaceSwitcher` exported with no props in Task 1, imported with no props in Task 2 — consistent.
