import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useState } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ProjectProvider, useProjects } from '@/components/ui/ProjectProvider';
import * as api from '@/lib/api-client';
import type { Project } from '@/lib/api-client';

const def: Project = { id: 'p1', name: 'Default', created_at: '2026-07-24T00:00:00Z', is_default: true };
const pay: Project = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z', is_default: false };

function Probe() {
  const { projects, selected, select, create, rename, remove } = useProjects();
  const [err, setErr] = useState('');
  return (
    <div>
      <span data-testid="selected">{selected?.name ?? 'none'}</span>
      <span data-testid="count">{projects.length}</span>
      <span data-testid="names">{projects.map((p) => p.name).join(',')}</span>
      <span data-testid="error">{err}</span>
      <button onClick={() => select('p2')}>pick payments</button>
      <button onClick={() => create('New One')}>create</button>
      <button onClick={() => rename('p2', 'Beta')}>rename payments</button>
      <button onClick={() => remove('p2')}>remove payments</button>
      <button onClick={() => remove('p1').catch((e) => setErr(e.message))}>remove default</button>
    </div>
  );
}

describe('ProjectProvider', () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it('selects the first project when nothing is stored', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Default'));
    expect(screen.getByTestId('count')).toHaveTextContent('2');
  });

  it('restores a stored selection', async () => {
    localStorage.setItem('boltrunner-project', 'p2');
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Payments'));
  });

  // localStorage outlives the database. A developer who drops and reseeds the
  // DB has a stored id that names nothing, and must not get an empty switcher.
  it('falls back to the first project when the stored id is unknown', async () => {
    localStorage.setItem('boltrunner-project', 'p-deleted');
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Default'));
    await waitFor(() => expect(localStorage.getItem('boltrunner-project')).toBe('p1'));
  });

  it('persists a selection', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Default'));

    fireEvent.click(screen.getByRole('button', { name: /pick payments/i }));
    expect(screen.getByTestId('selected')).toHaveTextContent('Payments');
    expect(localStorage.getItem('boltrunner-project')).toBe('p2');
  });

  it('selects a newly created project and adds it to the list', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def]);
    vi.spyOn(api, 'createProject').mockResolvedValue({
      id: 'p9',
      name: 'New One',
      created_at: '2026-07-29T00:00:00Z',
      is_default: false,
    });
    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Default'));

    fireEvent.click(screen.getByRole('button', { name: 'create' }));
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('New One'));
    expect(screen.getByTestId('count')).toHaveTextContent('2');
    expect(localStorage.getItem('boltrunner-project')).toBe('p9');
  });

  it('degrades to an empty list when the projects endpoint fails', async () => {
    vi.spyOn(api, 'listProjects').mockRejectedValue(new Error('boom'));
    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('none'));
    expect(screen.getByTestId('count')).toHaveTextContent('0');
  });

  it('leaves nothing selected when the registry is empty', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([]);
    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('none'));
  });

  it('throws when used outside a provider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<Probe />)).toThrow(/useProjects must be used within a ProjectProvider/);
    spy.mockRestore();
  });

  it('rename updates the list and keeps it sorted by name', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'renameProject').mockResolvedValue({
      id: 'p2', name: 'Beta', created_at: '2026-07-29T00:00:00Z', is_default: false,
    });

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('names')).toHaveTextContent('Default,Payments'));

    fireEvent.click(screen.getByText('rename payments'));

    // Re-sorted, so "Beta" lands before "Default" rather than staying put.
    await waitFor(() => expect(screen.getByTestId('names')).toHaveTextContent('Beta,Default'));
  });

  it('remove drops the project from the list', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'deleteProject').mockResolvedValue(undefined);

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('count')).toHaveTextContent('2'));

    fireEvent.click(screen.getByText('remove payments'));

    await waitFor(() => expect(screen.getByTestId('names')).toHaveTextContent('Default'));
    expect(screen.getByTestId('count')).toHaveTextContent('1');
  });

  // Deleting the selected project must not leave the switcher pointing at
  // nothing -- the same failure the stored-id guard handles on load, reached
  // by a different route.
  it('remove falls back to the default project when the selected one is deleted', async () => {
    localStorage.setItem('boltrunner-project', 'p2');
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'deleteProject').mockResolvedValue(undefined);

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Payments'));

    fireEvent.click(screen.getByText('remove payments'));

    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Default'));
    expect(localStorage.getItem('boltrunner-project')).toBe('p1');
  });

  // Deleting a project the user is not looking at must not move them.
  it('remove leaves the selection alone when another project is deleted', async () => {
    localStorage.setItem('boltrunner-project', 'p1');
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'deleteProject').mockResolvedValue(undefined);

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('selected')).toHaveTextContent('Default'));

    fireEvent.click(screen.getByText('remove payments'));

    await waitFor(() => expect(screen.getByTestId('count')).toHaveTextContent('1'));
    expect(screen.getByTestId('selected')).toHaveTextContent('Default');
  });

  // The 409 body is what the admin table renders, so remove must reject rather
  // than swallow it -- and must leave the project in the list, because the
  // delete did not happen.
  it('remove propagates a rejection', async () => {
    vi.spyOn(api, 'listProjects').mockResolvedValue([def, pay]);
    vi.spyOn(api, 'deleteProject').mockRejectedValue(
      new api.ApiError(409, 'Default still has 3 tests; move or delete them first')
    );

    render(
      <ProjectProvider>
        <Probe />
      </ProjectProvider>
    );
    await waitFor(() => expect(screen.getByTestId('count')).toHaveTextContent('2'));

    fireEvent.click(screen.getByText('remove default'));

    await waitFor(() => expect(screen.getByTestId('error')).toHaveTextContent('still has 3 tests'));
    expect(screen.getByTestId('count')).toHaveTextContent('2');
  });
});
