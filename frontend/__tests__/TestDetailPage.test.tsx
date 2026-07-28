import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import TestDetailPage from '@/app/tests/[id]/page';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  useParams: () => ({ id: 't1' }),
  useRouter: () => ({ push: vi.fn() }),
}));

describe('TestDetailPage', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('renders the detail panel for the route param', async () => {
    const listTestVersions = vi.spyOn(api, 'listTestVersions').mockResolvedValue([]);
    render(<TestDetailPage />);
    expect(listTestVersions).toHaveBeenCalledWith('t1');
    expect(await screen.findByRole('heading', { level: 1 })).toBeInTheDocument();
  });
});
