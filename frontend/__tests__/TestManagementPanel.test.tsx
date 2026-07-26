import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
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
});
