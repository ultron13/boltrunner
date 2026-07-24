import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import HistoryPage from '@/app/history/page';
import * as api from '@/lib/api-client';

const push = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push }),
  useSearchParams: () => new URLSearchParams(),
}));

describe('HistoryPage', () => {
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
});
