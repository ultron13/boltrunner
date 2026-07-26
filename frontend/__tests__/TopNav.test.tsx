import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ThemeProvider } from '@/components/ui/theme';
import { TopNav } from '@/components/ui/TopNav';

vi.mock('next/navigation', () => ({
  usePathname: () => '/history',
}));

describe('TopNav', () => {
  const modules = [
    { label: 'Dashboard', href: '/' },
    { label: 'Test Runs', href: '/history' },
  ];

  it('renders every module label and the BoltRunner brand', () => {
    render(
      <ThemeProvider>
        <TopNav modules={modules} />
      </ThemeProvider>
    );
    expect(screen.getByText('BoltRunner')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Dashboard' })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: 'Test Runs' })).toHaveAttribute('href', '/history');
  });

  it('marks the module matching the current path as active', () => {
    render(
      <ThemeProvider>
        <TopNav modules={modules} />
      </ThemeProvider>
    );
    expect(screen.getByRole('link', { name: 'Test Runs' })).toHaveClass('border-accent');
    expect(screen.getByRole('link', { name: 'Dashboard' })).not.toHaveClass('border-accent');
  });

  it('includes a theme toggle', () => {
    render(
      <ThemeProvider>
        <TopNav modules={modules} />
      </ThemeProvider>
    );
    expect(screen.getByRole('button', { name: /toggle theme/i })).toBeInTheDocument();
  });

  it('renders the workspace switcher', () => {
    render(
      <ThemeProvider>
        <TopNav modules={modules} />
      </ThemeProvider>
    );
    expect(screen.getByRole('button', { name: /default/i })).toHaveAttribute('aria-haspopup', 'menu');
  });

  it('wraps the module links so they are hidden below md and shown at md and up', () => {
    render(
      <ThemeProvider>
        <TopNav modules={modules} />
      </ThemeProvider>
    );
    const dashboardLink = screen.getByRole('link', { name: 'Dashboard' });
    expect(dashboardLink.closest('nav')).toHaveClass('hidden', 'md:flex');
  });
});
