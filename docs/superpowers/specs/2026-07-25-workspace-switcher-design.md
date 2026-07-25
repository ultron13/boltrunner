# BOL-22: Workspace (Project/Team) Switcher

## Context

`docs/superpowers/specs/2026-07-24-portal-shell-lre-design.md` (decision 4) already called
for a project/team switcher: "there is no project/team concept in the data model yet... Build
the switcher UI chrome for real, but wire it to a single hardcoded 'Default' workspace —
visually real, ready to extend, no invented multi-tenant data." That decision was never
implemented — `TreeNav` only renders a static, non-interactive `📁 Default` label, and
`TopNav` has no switcher at all. This is the last unbuilt piece of BOL-22's stated goal (the
history page, admin page, and nav shell all shipped already). This spec covers building it.

## Decisions

1. **Scope**: only the switcher. History page, admin page, and shell already satisfy the rest
   of BOL-22.
2. **Data**: single hardcoded "Default" workspace, no backend call — matches the existing
   `TreeNav` stub and decision 4 above. Real multi-project data lands with BOL-28 (Test
   Catalog Service).
3. **Placement**: `TopNav`, immediately after the `BoltRunner` brand text, before the module
   links.
4. **Menu content**: "Default" (checked) plus a disabled "New project" action — visible
   affordance for what's coming, without implying it works yet.
5. **Implementation**: a new hand-rolled component, consistent with every other
   `components/ui/` primitive (`Tabs`, `ThemeToggle`, `DataTable`) — no dropdown/menu
   dependency added to `package.json`.

## Architecture

### `frontend/components/ui/WorkspaceSwitcher.tsx` (new)

Self-contained client component, no props (nothing to parameterize yet — same reasoning as
`TreeNav`'s hardcoded label).

- Trigger: `<button aria-haspopup="menu" aria-expanded={open}>Default ▾</button>`.
- Menu (`role="menu"`, only in the DOM while `open`):
  - `Default` — `role="menuitemradio" aria-checked="true"`, checkmark shown; clicking it just
    closes the menu (no-op, already selected).
  - `New project` — `<button disabled aria-disabled="true">`, visually greyed out, inert.
- Close triggers: selecting "Default", `Escape` (closes and returns focus to the trigger),
  and a `mousedown` outside the component's root (listener attached to `document` while open,
  removed on close/unmount).
- Open triggers: click, or `Enter`/`Space` on the trigger while focused.

### `frontend/components/ui/TopNav.tsx` (edit)

Render `<WorkspaceSwitcher />` directly after the brand `<span>BoltRunner</span>`, before the
`modules.map(...)` links. The brand text stays its own element, so existing
`getByText('BoltRunner')` assertions (`TopNav.test.tsx`, `portal-shell.spec.ts`) are
unaffected.

## Testing strategy

- **Unit** (`frontend/__tests__/WorkspaceSwitcher.test.tsx`, Vitest + Testing Library):
  - trigger renders with closed menu by default.
  - click opens the menu; menu shows "Default" checked and "New project" disabled.
  - clicking "Default" closes the menu.
  - `Escape` while open closes the menu and returns focus to the trigger.
  - a `mousedown` outside the component closes the menu.
  - clicking the disabled "New project" item does nothing (no navigation, no state change,
    menu stays open).
- **e2e** (extend `frontend/e2e/portal-shell.spec.ts`): from the dashboard, open the
  switcher, assert "Default" and "New project" are visible, assert `Escape` closes it.
- No changes needed to `TopNav.test.tsx`, `Shell.test.tsx`, or any other existing test/page.
- Coverage: new component is small and fully exercised by the unit tests above; no separate
  coverage-threshold changes needed (same 88% gate from the parent spec already applies).

## Out of scope (unchanged from the parent spec)

- Real multi-tenancy / multiple projects or teams (BOL-28).
- Any backend endpoint for projects/teams.
- Making "New project" functional.
