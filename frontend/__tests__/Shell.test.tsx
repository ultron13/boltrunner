import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { ThemeProvider } from '@/components/ui/theme';
import { Shell } from '@/components/ui/Shell';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  usePathname: () => '/',
  useSearchParams: () => new URLSearchParams(),
}));

describe('Shell', () => {
  it('renders the top nav, tree nav, breadcrumb and children', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([
      { id: '1', name: 'Checkout Load', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
    ]);

    render(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );

    expect(screen.getByText('BoltRunner')).toBeInTheDocument();
    expect(screen.getByText('page content')).toBeInTheDocument();
    expect(screen.getByRole('navigation', { name: 'Workspace' })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('link', { name: /Checkout Load/i })).toBeInTheDocument());
  });

  it('shows a Default-only breadcrumb on the root path', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(
      <ThemeProvider>
        <Shell>
          <p>page content</p>
        </Shell>
      </ThemeProvider>
    );
    expect(await screen.findByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent('Default');
  });
});
