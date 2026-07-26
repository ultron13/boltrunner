import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import TestsPage from '@/app/tests/page';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe('TestsPage', () => {
  it('renders the Tests heading and the test management panel', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(<TestsPage />);
    expect(screen.getByRole('heading', { name: 'Tests' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create test/i })).toBeInTheDocument();
    expect(await screen.findByText('No tests yet — create one above.')).toBeInTheDocument();
  });
});
