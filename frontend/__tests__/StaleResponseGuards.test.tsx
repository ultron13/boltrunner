import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { TestManagementPanel } from '@/components/TestManagementPanel';
import { Shell } from '@/components/ui/Shell';
import DashboardPage from '@/app/page';
import { ThemeProvider } from '@/components/ui/theme';
import * as api from '@/lib/api-client';
import type { Test } from '@/lib/api-client';

// The provider resolves asynchronously in production, so selectedId goes
// null -> value. These tests drive that transition directly and control which
// response lands first, which is the only way to reach the stale-write path.
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

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
  usePathname: vi.fn(() => '/'),
  useSearchParams: vi.fn(() => new URLSearchParams()),
}));

function makeTest(id: string, name: string): Test {
  return {
    id,
    name,
    target_url: 'http://x',
    virtual_users: 1,
    duration_seconds: 1,
    created_at: '2026-07-24T00:00:00Z',
  };
}

const otherWorkspace = [makeTest('t-other', 'Other Workspace Test'), makeTest('t-other2', 'Second Other')];
const selectedWorkspace = [makeTest('t-mine', 'Selected Workspace Test')];

/**
 * Mocks listTests so the UNSCOPED call (the one made while selectedId is still
 * null) resolves only when we say so, while the scoped call resolves at once.
 * Returns a function that releases the stale unscoped response.
 */
function deferUnscopedResolution() {
  let releaseUnscoped: (tests: Test[]) => void = () => {};
  const unscoped = new Promise<Test[]>((resolve) => {
    releaseUnscoped = resolve;
  });

  vi.spyOn(api, 'listTests').mockImplementation((projectId?: string) =>
    projectId ? Promise.resolve(selectedWorkspace) : unscoped
  );

  return () => releaseUnscoped(otherWorkspace);
}

describe('stale-response guards', () => {
  beforeEach(() => {
    projectState.selectedId = null;
  });
  afterEach(() => vi.restoreAllMocks());

  it('TestManagementPanel keeps the scoped tests when the stale unscoped response lands late', async () => {
    const releaseStale = deferUnscopedResolution();

    const { rerender } = render(<TestManagementPanel />);

    projectState.selectedId = 'p2';
    rerender(<TestManagementPanel />);
    // findAllBy: DataTable renders each row twice -- a table row and a stacked
    // card for narrow viewports -- so the name legitimately matches more than once.
    expect(await screen.findAllByText('Selected Workspace Test')).not.toHaveLength(0);

    releaseStale();
    await waitFor(() => expect(api.listTests).toHaveBeenCalledTimes(2));

    // Without the guard the late unscoped response repaints the table here.
    expect(screen.queryAllByText('Other Workspace Test')).toHaveLength(0);
    expect(screen.queryAllByText('Selected Workspace Test')).not.toHaveLength(0);
  });

  it('Shell keeps the scoped tests in the tree nav when the stale response lands late', async () => {
    const releaseStale = deferUnscopedResolution();

    const { rerender } = render(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );

    projectState.selectedId = 'p2';
    rerender(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );
    expect(await screen.findByRole('link', { name: /Selected Workspace Test/i })).toBeInTheDocument();

    releaseStale();
    await waitFor(() => expect(api.listTests).toHaveBeenCalledTimes(2));

    expect(screen.queryByRole('link', { name: /Other Workspace Test/i })).not.toBeInTheDocument();
  });

  it('DashboardPage keeps the scoped KPI counts when the stale response lands late', async () => {
    const releaseStale = deferUnscopedResolution();
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([]);

    const { rerender } = render(<DashboardPage />);

    projectState.selectedId = 'p2';
    rerender(<DashboardPage />);

    const totalTile = await screen.findByText('Total Tests');
    await waitFor(() => expect(totalTile.closest('div')?.textContent).toContain('1'));

    releaseStale();
    // Four, not two: this page renders TestManagementPanel, which fetches its
    // own list, and both fire once unscoped and once scoped.
    await waitFor(() => expect(api.listTests).toHaveBeenCalledTimes(4));

    // The stale response carries two tests; without the guard the count flips to 2.
    expect(totalTile.closest('div')?.textContent).toContain('1');
  });
});
