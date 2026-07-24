import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import RunPage from '@/app/runs/[id]/page';
import { useRunPolling } from '@/hooks/useRunPolling';

vi.mock('next/navigation', () => ({
  useParams: () => ({ id: 'r1' }),
}));
vi.mock('@/hooks/useRunPolling');
vi.mock('@/lib/api-client', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api-client')>('@/lib/api-client');
  return { ...actual, cancelRun: vi.fn() };
});

describe('RunPage', () => {
  it('shows a loading state while data is null', () => {
    vi.mocked(useRunPolling).mockReturnValue({ data: null, error: null });
    render(<RunPage />);
    expect(screen.getByText(/loading/i)).toBeInTheDocument();
  });

  it('shows an error message when polling fails', () => {
    vi.mocked(useRunPolling).mockReturnValue({ data: null, error: 'boom' });
    render(<RunPage />);
    expect(screen.getByText('boom')).toBeInTheDocument();
  });

  it('renders the Details tab by default with live metrics', () => {
    vi.mocked(useRunPolling).mockReturnValue({
      data: { run: { id: 'r1', test_id: 't1', status: 'running' }, history: [] },
      error: null,
    });
    render(<RunPage />);
    expect(screen.getByText(/status: running/i)).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Details' })).toHaveAttribute('aria-selected', 'true');
  });

  it('switches to the Metrics tab on click', () => {
    vi.mocked(useRunPolling).mockReturnValue({
      data: { run: { id: 'r1', test_id: 't1', status: 'running' }, history: [] },
      error: null,
    });
    render(<RunPage />);
    fireEvent.click(screen.getByRole('tab', { name: 'Metrics' }));
    expect(screen.getByText(/waiting for the first metric snapshot/i)).toBeInTheDocument();
  });
});
