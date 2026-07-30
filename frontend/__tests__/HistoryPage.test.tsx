import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import HistoryPage from '@/app/history/page';
import * as api from '@/lib/api-client';
import type { Test } from '@/lib/api-client';
import { useSearchParams } from 'next/navigation';

const push = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push }),
  useSearchParams: vi.fn(() => new URLSearchParams()),
}));

const projectState = vi.hoisted(() => ({
  selectedId: null as string | null,
  selected: null as { id: string; name: string; created_at: string } | null,
}));

vi.mock('@/components/ui/ProjectProvider', () => ({
  useProjects: () => ({
    projects: [],
    selectedId: projectState.selectedId,
    selected: projectState.selected,
    select: vi.fn(),
    create: vi.fn(),
  }),
}));

describe('HistoryPage', () => {
  afterEach(() => {
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams());
    projectState.selectedId = null;
    projectState.selected = null;
    vi.restoreAllMocks();
  });

  it('merges runs across all tests and sorts newest first', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: 't1', name: 'Checkout', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([
      { id: 'r1', test_id: 't1', status: 'completed', created_at: '2026-07-24T00:00:01Z' },
      { id: 'r2', test_id: 't1', status: 'failed', created_at: '2026-07-24T00:00:02Z' },
    ]);

    render(<HistoryPage />);

    const rows = await screen.findAllByRole('row');
    expect(rows[1]).toHaveTextContent('r2');
    expect(rows[2]).toHaveTextContent('r1');
  });

  it('navigates to the run detail page when a row is clicked', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: 't1', name: 'Checkout', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([
      { id: 'r1', test_id: 't1', status: 'completed', created_at: '2026-07-24T00:00:01Z' },
    ]);

    render(<HistoryPage />);
    const row = await screen.findByRole('row', { name: /r1/i });
    fireEvent.click(row);
    expect(push).toHaveBeenCalledWith('/runs/r1');
  });

  it('shows the empty message when there are no runs', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(<HistoryPage />);
    expect(await screen.findByText('No runs yet.')).toBeInTheDocument();
  });

  // An empty list and a broken fetch look identical without this, and the user
  // cannot tell which workspace they are looking at from the table alone.
  it('names the selected workspace in the empty message', async () => {
    projectState.selectedId = 'p2';
    projectState.selected = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z' };
    vi.spyOn(api, 'listTests').mockResolvedValue([]);

    render(<HistoryPage />);

    expect(await screen.findByText('No runs in Payments yet.')).toBeInTheDocument();
  });

  // A ?testId= list is deliberately unscoped, so the linked test may belong to
  // another workspace. Naming the selected one there would be false.
  it('keeps the generic empty message on a testId deep link', async () => {
    projectState.selectedId = 'p2';
    projectState.selected = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z' };
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams('testId=t-elsewhere'));
    vi.spyOn(api, 'listTests').mockResolvedValue([]);

    render(<HistoryPage />);

    expect(await screen.findByText('No runs yet.')).toBeInTheDocument();
    expect(screen.queryByText(/No runs in Payments yet\./)).not.toBeInTheDocument();
  });

  it('filters to a single test when a testId query param is present', async () => {
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams('testId=t1'));
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: 't1', name: 'Checkout', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
      { id: 't2', name: 'Login', target_url: 'http://y', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockImplementation(async (testId: string) =>
      testId === 't1' ? [{ id: 'r1', test_id: 't1', status: 'completed', created_at: '2026-07-24T00:00:01Z' }] : []
    );

    render(<HistoryPage />);

    const rows = await screen.findAllByRole('row');
    expect(rows).toHaveLength(2); // header + r1 only
    expect(api.listRunsForTest).toHaveBeenCalledTimes(1);
    expect(api.listRunsForTest).toHaveBeenCalledWith('t1');
  });

  it('still renders the successful test runs when another test fetch fails', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: 't1', name: 'Checkout', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
      { id: 't2', name: 'Login', target_url: 'http://y', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockImplementation(async (testId: string) => {
      if (testId === 't1') {
        return [{ id: 'r1', test_id: 't1', status: 'completed', created_at: '2026-07-24T00:00:01Z' }];
      }
      throw new Error('boom');
    });

    render(<HistoryPage />);

    const row = await screen.findByRole('row', { name: /r1/i });
    expect(row).toBeInTheDocument();
  });

  it('sorts runs without a created_at to a stable relative order', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: 't1', name: 'Checkout', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([
      { id: 'r1', test_id: 't1', status: 'pending' },
      { id: 'r2', test_id: 't1', status: 'pending' },
    ]);

    render(<HistoryPage />);

    const rows = await screen.findAllByRole('row');
    expect(rows).toHaveLength(3); // header + r1 + r2, no crash from the missing created_at
  });

  it('scopes the fetch to the selected project when browsing', async () => {
    projectState.selectedId = 'p2';
    const listTests = vi.spyOn(api, 'listTests').mockResolvedValue([]);

    render(<HistoryPage />);

    await waitFor(() => expect(listTests).toHaveBeenCalledWith('p2'));
  });

  it('refetches when the selected project changes after mount, and a slower unscoped response does not clobber the scoped rows', async () => {
    projectState.selectedId = null;

    // The initial (null-selection) listTests call is left pending so it can
    // be resolved later, after the scoped call has already painted the
    // table -- reproducing the cold-load race where the wider, unscoped
    // fan-out finishes after the narrower, scoped one.
    let resolveUnscoped!: (tests: Test[]) => void;
    const unscopedTests = new Promise<Test[]>((resolve) => {
      resolveUnscoped = resolve;
    });

    const listTests = vi.spyOn(api, 'listTests').mockImplementation(async (projectId?: string) =>
      projectId === 'p3'
        ? [{ id: 't3', name: 'Scoped', target_url: 'http://x', virtual_users: 1, duration_seconds: 1, created_at: '2026-07-24T00:00:00Z' }]
        : unscopedTests
    );
    vi.spyOn(api, 'listRunsForTest').mockImplementation(async (testId: string) =>
      testId === 't3'
        ? [{ id: 'r3', test_id: 't3', status: 'completed', created_at: '2026-07-24T00:00:01Z' }]
        : [{ id: 'r-unscoped', test_id: testId, status: 'completed', created_at: '2026-07-24T00:00:01Z' }]
    );

    const { rerender } = render(<HistoryPage />);
    await waitFor(() => expect(listTests).toHaveBeenCalledWith(undefined));

    // The workspace resolves after first paint: selectedId flips from null
    // to a real id, the same transition useProjects() produces on a cold load.
    projectState.selectedId = 'p3';
    rerender(<HistoryPage />);
    await waitFor(() => expect(listTests).toHaveBeenCalledWith('p3'));
    await screen.findByRole('row', { name: /r3/i });

    // The stale null-selection call (still pending from the first render)
    // now resolves. It must not repaint the table with its (unscoped) results.
    await act(async () => {
      resolveUnscoped([
        { id: 't1', name: 'Other', target_url: 'http://y', virtual_users: 1, duration_seconds: 1, created_at: '2026-07-24T00:00:00Z' },
      ]);
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(screen.queryByRole('row', { name: /r-unscoped/i })).not.toBeInTheDocument();
    expect(screen.getByRole('row', { name: /r3/i })).toBeInTheDocument();
  });

  // A ?testId= link is an explicit request for one test's history. It must
  // resolve whichever workspace is selected, or a bookmarked link renders blank
  // with no explanation.
  it('ignores the project filter when a testId is present', async () => {
    projectState.selectedId = 'p2';
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams('testId=t9'));
    // Argument-sensitive: t9 only comes back on the unscoped call. If the
    // component regressed to scoping the deep link, this mock would return
    // [] for the 'p2' call and the row assertion below would actually fail
    // instead of passing regardless of which call was made.
    const listTests = vi.spyOn(api, 'listTests').mockImplementation(async (projectId?: string) =>
      projectId
        ? []
        : [{ id: 't9', name: 'Elsewhere', target_url: 'http://x', virtual_users: 1, duration_seconds: 1, created_at: '2026-07-24T00:00:00Z' }]
    );
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([
      { id: 'r9', test_id: 't9', status: 'completed', created_at: '2026-07-24T00:00:01Z' },
    ]);

    render(<HistoryPage />);

    // The row only renders if listTests was called unscoped (see mock above),
    // so this assertion now proves the deep-link path skips the project filter.
    expect(await screen.findByRole('row', { name: /r9/i })).toBeInTheDocument();
    expect(listTests).toHaveBeenCalledWith();
  });
});
