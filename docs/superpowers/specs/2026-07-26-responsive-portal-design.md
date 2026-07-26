# Responsive Portal (Phone + Tablet)

## Context

The portal shell (`Shell`/`TopNav`/`TreeNav`, built for BOL-22) assumes desktop width: a
horizontal top nav bar and a persistent 224px `TreeNav` sidebar. On a phone-width viewport
the sidebar can't stay pinned and the top nav's module links don't fit. This wasn't part of
any existing Jira ticket — a JQL search across the BOL project found no story mentioning
mobile/responsive — so this is new, standalone scope, brainstormed and decided visually with
the user (mockups at `.superpowers/brainstorm/211307-1785047111/content/`).

## Decisions made during brainstorming

1. **Breakpoint**: a single Tailwind `md` (768px) breakpoint. Below it, the new mobile
   layout applies. At or above it, today's desktop layout applies completely unchanged —
   tablet rides along with desktop rather than getting its own intermediate layout.
2. **Navigation pattern** (chosen over a slide-in drawer and a top-accordion alternative,
   compared as mockups): a **bottom tab bar**, the standard mobile-app convention. `TopNav`
   shrinks to brand + `WorkspaceSwitcher` + `ThemeToggle` on mobile; the existing module
   links move into a new fixed bottom tab bar; `TreeNav`'s sidebar disappears below `md`
   entirely (no drawer replacement — its content is redistributed, see decision 4).
3. **Tab set**: four tabs — **Dashboard** (`/`), **Tests** (`/tests`, new), **Runs**
   (`/history`), **Admin** (`/admin`). This collapses the desktop nav's existing duplicate
   "Dashboard" and "Test Management" labels (both already point to `/`) into one tab, rather
   than preserving both as separate mobile tabs.
4. **Tests tab content**: the desktop `Dashboard` page currently bundles a KPI strip,
   `CreateTestForm`, and the full `TestList` table together. Rather than duplicating that
   list under a mobile-only "Tests" tab that mirrors `TreeNav`'s old simple link list, the
   form+list (and their fetch/create/run logic) get **extracted into a shared panel**. Mobile
   `Dashboard` becomes KPI-only; the new `/tests` route renders the extracted panel
   unconditionally; desktop `Dashboard` renders the same panel, unchanged in effect, just
   sourced from the shared component instead of inline JSX.
5. **DataTable on phone width** (chosen over horizontal scroll and a priority-columns
   variant, compared as mockups): rows render as **stacked cards** below `md` instead of
   `<tr>`s — no data dropped, no sideways scrolling. Convention over configuration: the
   **first column in the `columns` array is the card title**, the rest render as
   `label: value` lines. No new prop on `Column<T>` — every existing table already puts its
   identifying field first (`TestList`'s `name`, the history page's `testName`), so the
   existing shape is sufficient.
6. **Everything else is unchanged**: `CreateTestForm` (already `flex flex-col`, `max-w-md`),
   `MetricsChart` (already wrapped in recharts' `ResponsiveContainer`), `Tabs`, `Breadcrumb`,
   and `AdminPage` all already work at phone width with no code changes.

## Architecture

### `frontend/components/ui/TopNav.tsx` (edit)

The `modules.map(...)` block moves into its own element with `hidden md:flex` — gone below
`md`, unchanged at `md`+. Brand span + `WorkspaceSwitcher` + `ThemeToggle` stay visible at
every width (already the case for the first two; `ThemeToggle` already renders unconditionally
today).

### `frontend/components/ui/BottomTabBar.tsx` (new)

Self-contained client component, no props — same "hardcoded, single consumer" convention as
`TreeNav`'s `📁 Default` label, rather than a generic tab-bar API nothing else needs yet.
Fixed to the viewport bottom (`fixed inset-x-0 bottom-0`), `md:hidden`. Four tabs, each an
active-aware `Link` (active state derived from `usePathname()`, same pattern `TopNav` already
uses): Dashboard (`/`), Tests (`/tests`), Runs (`/history`), Admin (`/admin`), each with a
small icon glyph + label, active tab highlighted with the existing `accent` token.

### `frontend/components/ui/Shell.tsx` (edit)

- `TreeNav` wrapped in `hidden md:block`.
- `<BottomTabBar />` rendered after the content area (fixed positioning takes it out of flow).
- `<main>` gains `pb-16 md:pb-0` so the fixed bar never overlaps page content.
- `breadcrumbFor` gains a `/tests` case: `[root, { label: 'Tests' }]` (mirrors the existing
  `/admin` case).
- `MODULES` constant (Dashboard/Test Management/Test Runs/Admin) is unchanged — still what
  `TopNav` renders at `md`+.

### `frontend/components/TestManagementPanel.tsx` (new, extracted)

Everything `frontend/app/page.tsx` currently does *except* the KPI strip: `useState<Test[]>`,
the `listTests`/`listRunsForTest` fetch effect insofar as it feeds the list (the KPI-driving
"active runs" count stays in `page.tsx`, since KPIs stay on `Dashboard` only), `handleStart`,
`handleCreated`, and the `<CreateTestForm />` + `<TestList />` JSX. Exports one component with
no props (same fetch-it-yourself pattern every existing page component already uses).

### `frontend/app/page.tsx` (edit)

Keeps the KPI-strip fetch logic (tests count, active runs count) and the `<h1>`. Replaces the
inline `<CreateTestForm />` + `<TestList />` block with `<div className="hidden md:block"><TestManagementPanel /></div>`.

### `frontend/app/tests/page.tsx` (new)

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

### `frontend/components/ui/DataTable.tsx` (edit)

Below `md`, replace `<tr>` rendering with a stacked-card list; both markups read the same
`columns`/`rows`/`rowKey`/`onRowClick`/per-column `render` props, so any embedded interactive
content (e.g. the history page's `<Link>` in its `id` column, added in commit `a14fe2d` for
keyboard accessibility) appears in card mode too, with no extra work.

- Table markup: existing `<table>`, gains `hidden md:table` (currently unconditional).
- New card markup: a `<div className="md:hidden">` containing one card per row. Card title =
  `columns[0]`'s rendered value (or raw field, same fallback logic as today); remaining
  columns render as `<div>{col.header}: {value}</div>`. Clicking a card calls `onRowClick`
  the same way a row click does today.
- `Column<T>` type is unchanged — no new fields.

## Testing strategy

- **Unit** (Vitest + Testing Library): `TopNav.test.tsx` gets a case asserting module links
  are present but the container carries `hidden md:flex` (class-based, since jsdom doesn't
  evaluate media queries) — actually verifying *content* stays the same is enough; the
  Playwright viewport tests below are what actually prove the responsive behavior. New tests
  for `BottomTabBar.tsx` (renders 4 links with correct hrefs, active-link highlighting mirrors
  `TopNav.test.tsx`'s existing pattern), `TestManagementPanel.tsx` (moved verbatim from
  today's `DashboardPage.test.tsx` assertions covering create/list/run), `DashboardPage.test.tsx`
  (updated: KPI assertions stay, create/list/run assertions move to
  `TestManagementPanel.test.tsx`), a new `TestsPage.test.tsx` (renders the panel), and
  `DataTable.test.tsx` gains cases for the card markup's presence and content.
- **Playwright** (extend `frontend/e2e/`, using `test.use({ viewport: ... })` per describe
  block or per-test): a new spec exercising the phone-width layout (e.g. iPhone-width
  viewport) — bottom tab bar visible and navigates correctly between all 4 routes, `TreeNav`
  and the desktop module links are not visible, `/tests` renders the create form and list, a
  `DataTable`-backed page (history) shows cards instead of a table. The *existing*
  `portal-shell.spec.ts` and `walking-skeleton.spec.ts` specs keep running at the default
  desktop viewport and are expected to pass completely unchanged — proving the "existing
  pages continue to work unchanged" constraint holds.
- Coverage: this repo's 88% Vitest coverage gate (from the BOL-22 work) continues to apply;
  no gate changes needed, new components get their own tests same as everything else.

## Out of scope (explicitly deferred)

- A third, tablet-specific layout (e.g. collapsible/icon-only sidebar) — tablet uses the
  desktop layout unchanged.
- Any change to `CreateTestForm`, `MetricsChart`, `Tabs`, `Breadcrumb`, or `AdminPage` — all
  already work at phone width.
- A generic/reusable tab-bar or priority-column API — `BottomTabBar` is hardcoded like
  `TreeNav`; `DataTable`'s card mode uses the first-column convention instead of a new prop.
- Touch gestures (swipe between tabs, pull-to-refresh) — tap-only navigation, matching what
  the rest of the portal already does.
