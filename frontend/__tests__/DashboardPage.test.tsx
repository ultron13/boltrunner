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

  it('still renders the create form and test list', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    vi.spyOn(api, 'listRunsForTest').mockResolvedValue([]);
    render(<DashboardPage />);
    expect(screen.getByRole('button', { name: /create test/i })).toBeInTheDocument();
    expect(await screen.findByText('No tests yet — create one above.')).toBeInTheDocument();
  });
});
