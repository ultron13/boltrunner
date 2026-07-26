import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TestManagementPanel } from '@/components/TestManagementPanel';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe('TestManagementPanel', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('renders the create form and test list', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(<TestManagementPanel />);
    expect(screen.getByRole('button', { name: /create test/i })).toBeInTheDocument();
    expect(await screen.findByText('No tests yet — create one above.')).toBeInTheDocument();
  });

  it('shows tests once they load', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: '1', name: 'Critical Test', target_url: 'http://x', virtual_users: 10, duration_seconds: 60, created_at: '2026-07-24T00:00:00Z' },
    ]);

    render(<TestManagementPanel />);

    await expect(screen.findByRole('row', { name: /Critical Test/i })).resolves.toBeInTheDocument();
  });

  it('shows an empty list when listTests fails', async () => {
    vi.spyOn(api, 'listTests').mockRejectedValue(new Error('boom'));
    render(<TestManagementPanel />);
    expect(await screen.findByText('No tests yet — create one above.')).toBeInTheDocument();
  });

  it('calls onTestCreated when a test is created', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    vi.spyOn(api, 'createTest').mockResolvedValue({
      id: '1',
      name: 'New Test',
      target_url: 'http://x',
      virtual_users: 5,
      duration_seconds: 30,
      created_at: '2026-07-26T00:00:00Z',
    });
    const onTestCreated = vi.fn();
    render(<TestManagementPanel onTestCreated={onTestCreated} />);

    await screen.findByText('No tests yet — create one above.');
    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'New Test' } });
    fireEvent.change(screen.getByLabelText(/target url/i), { target: { value: 'http://x' } });
    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '5' } });
    fireEvent.change(screen.getByLabelText(/duration/i), { target: { value: '30' } });
    fireEvent.click(screen.getByRole('button', { name: /create test/i }));

    await waitFor(() =>
      expect(onTestCreated).toHaveBeenCalledWith(expect.objectContaining({ id: '1', name: 'New Test' }))
    );
  });
});
