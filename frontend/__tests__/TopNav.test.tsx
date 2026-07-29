import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ThemeProvider } from '@/components/ui/theme';
import { ProjectProvider } from '@/components/ui/ProjectProvider';
import { TopNav } from '@/components/ui/TopNav';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  usePathname: () => '/history',
}));

describe('TopNav', () => {
  // TopNav renders a WorkspaceSwitcher, which reads project context and would
  // otherwise fire a real request at localhost:8080 from a unit test.
  beforeEach(() => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([]);
  });
  afterEach(() => vi.restoreAllMocks());

  const modules = [
    { label: 'Dashboard', href: '/' },
    { label: 'Test Runs', href: '/history' },
  ];

  it('renders every module label and the BoltRunner brand', () => {
    render(
      <ThemeProvider>
        <ProjectProvider>
          <TopNav modules={modules} />
        </ProjectProvider>
      </ThemeProvider>
    );
    expect(screen.getByText('BoltRunner')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Dashboard' })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: 'Test Runs' })).toHaveAttribute('href', '/history');
  });

  it('marks the module matching the current path as active', () => {
    render(
      <ThemeProvider>
        <ProjectProvider>
          <TopNav modules={modules} />
        </ProjectProvider>
      </ThemeProvider>
    );
    expect(screen.getByRole('link', { name: 'Test Runs' })).toHaveClass('border-accent');
    expect(screen.getByRole('link', { name: 'Dashboard' })).not.toHaveClass('border-accent');
  });

  it('includes a theme toggle', () => {
    render(
      <ThemeProvider>
        <ProjectProvider>
          <TopNav modules={modules} />
        </ProjectProvider>
      </ThemeProvider>
    );
    expect(screen.getByRole('button', { name: /toggle theme/i })).toBeInTheDocument();
  });

  it('renders the workspace switcher', () => {
    render(
      <ThemeProvider>
        <ProjectProvider>
          <TopNav modules={modules} />
        </ProjectProvider>
      </ThemeProvider>
    );
    expect(screen.getByRole('button', { name: /default/i })).toHaveAttribute('aria-haspopup', 'menu');
  });

  it('wraps the module links so they are hidden below md and shown at md and up', () => {
    render(
      <ThemeProvider>
        <ProjectProvider>
          <TopNav modules={modules} />
        </ProjectProvider>
      </ThemeProvider>
    );
    const dashboardLink = screen.getByRole('link', { name: 'Dashboard' });
    expect(dashboardLink.closest('nav')).toHaveClass('hidden', 'md:flex');
  });
});
