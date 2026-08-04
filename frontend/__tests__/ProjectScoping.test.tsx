import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ProjectProvider } from '@/components/ui/ProjectProvider';
import { CreateTestForm } from '@/components/CreateTestForm';
import { WorkspaceSwitcher } from '@/components/ui/WorkspaceSwitcher';
import { Shell } from '@/components/ui/Shell';
import { ThemeProvider } from '@/components/ui/theme';
import * as api from '@/lib/api-client';
import type { Project, Test } from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  usePathname: vi.fn(() => '/'),
  useSearchParams: vi.fn(() => new URLSearchParams()),
}));

const pay: Project = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z', is_default: false };

const created: Test = {
  id: 't1',
  name: 'x',
  target_url: 'http://x',
  virtual_users: 1,
  duration_seconds: 1,
  created_at: '2026-07-29T00:00:00Z',
};

// These exercise the real ProjectProvider against the real components, which is
// what the stubs in the older per-component test files deliberately skip.
describe('project scoping', () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it('sends the selected project id when creating a test', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([pay]);
    const createTest = vi.spyOn(api, 'createTest').mockResolvedValue(created);

    render(
      <ProjectProvider>
        <WorkspaceSwitcher />
        <CreateTestForm onCreated={vi.fn()} />
      </ProjectProvider>
    );

    // The selection arrives from a fetch, so wait for it to land before
    // submitting -- otherwise this asserts against the pre-selection render and
    // passes or fails on timing rather than on behavior.
    await screen.findByRole('button', { name: /payments/i });

    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'x' } });
    fireEvent.change(screen.getByLabelText(/target url/i), { target: { value: 'http://x' } });
    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '1' } });
    fireEvent.change(screen.getByLabelText(/duration/i), { target: { value: '1' } });
    fireEvent.click(screen.getByRole('button', { name: /create test/i }));

    await waitFor(() =>
      expect(createTest).toHaveBeenCalledWith(expect.objectContaining({ project_id: 'p2' }))
    );
  });

  // With no project selected the field is omitted entirely rather than sent as
  // an empty string, which the backend would reject as a malformed reference.
  it('omits project_id when no project is selected', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([]);
    const createTest = vi.spyOn(api, 'createTest').mockResolvedValue(created);

    render(
      <ProjectProvider>
        <WorkspaceSwitcher />
        <CreateTestForm onCreated={vi.fn()} />
      </ProjectProvider>
    );

    // An empty registry leaves the label at its Default fallback; waiting for
    // it keeps this deterministic rather than racing the fetch.
    await screen.findByRole('button', { name: /default/i });

    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'x' } });
    fireEvent.change(screen.getByLabelText(/target url/i), { target: { value: 'http://x' } });
    fireEvent.click(screen.getByRole('button', { name: /create test/i }));

    await waitFor(() => expect(createTest).toHaveBeenCalled());
    expect(createTest.mock.calls[0][0]).not.toHaveProperty('project_id');
  });

  it('scopes the shell test list to the selected project', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([pay]);
    const listTests = vi.spyOn(api, 'listTests').mockResolvedValue([]);

    render(
      <ThemeProvider>
        <ProjectProvider>
          <Shell>
            <p>page content</p>
          </Shell>
        </ProjectProvider>
      </ThemeProvider>
    );

    await waitFor(() => expect(listTests).toHaveBeenCalledWith('p2'));
  });
});
