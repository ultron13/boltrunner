import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
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

  it('hides the test management panel below md', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(<DashboardPage />);
    const createButton = await screen.findByRole('button', { name: /create test/i });
    expect(createButton.closest('div.hidden')).toHaveClass('hidden', 'md:block');
  });

  it('increments the Total Tests KPI when a test is created through the panel', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    vi.spyOn(api, 'createTest').mockResolvedValue({
      id: '1',
      name: 'New Test',
      target_url: 'http://x',
      virtual_users: 5,
      duration_seconds: 30,
      created_at: '2026-07-26T00:00:00Z',
    });

    render(<DashboardPage />);

    const totalTileBefore = await screen.findByText('Total Tests');
    expect(totalTileBefore.closest('div')?.textContent).toContain('0');

    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'New Test' } });
    fireEvent.change(screen.getByLabelText(/target url/i), { target: { value: 'http://x' } });
    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '5' } });
    fireEvent.change(screen.getByLabelText(/duration/i), { target: { value: '30' } });
    fireEvent.click(screen.getByRole('button', { name: /create test/i }));

    await waitFor(() => {
      const totalTile = screen.getByText('Total Tests');
      expect(totalTile.closest('div')?.textContent).toContain('1');
    });
  });
});
