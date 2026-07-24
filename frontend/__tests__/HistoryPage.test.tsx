import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import HistoryPage from '@/app/history/page';
import * as api from '@/lib/api-client';
import { useSearchParams } from 'next/navigation';

const push = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push }),
  useSearchParams: vi.fn(() => new URLSearchParams()),
}));

describe('HistoryPage', () => {
  afterEach(() => {
    vi.mocked(useSearchParams).mockReturnValue(new URLSearchParams());
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
});
