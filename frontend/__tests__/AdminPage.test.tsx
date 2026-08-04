import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import AdminPage from '@/app/admin/page';
import { ApiError } from '@/lib/api-client';

const projectState = vi.hoisted(() => ({
  projects: [] as { id: string; name: string; created_at: string; is_default: boolean }[],
  rename: vi.fn(),
  remove: vi.fn(),
}));

vi.mock('@/components/ui/ProjectProvider', () => ({
  useProjects: () => ({
    projects: projectState.projects,
    selectedId: projectState.projects[0]?.id ?? null,
    selected: projectState.projects[0] ?? null,
    select: vi.fn(),
    create: vi.fn(),
    rename: projectState.rename,
    remove: projectState.remove,
  }),
}));

describe('AdminPage', () => {
  beforeEach(() => {
    projectState.projects = [
      { id: 'p1', name: 'Default', created_at: '2026-07-24T00:00:00Z', is_default: true },
      { id: 'p2', name: 'Payments', created_at: '2026-07-25T00:00:00Z', is_default: false },
    ];
    projectState.rename = vi.fn().mockResolvedValue({ id: 'p2', name: 'Billing', created_at: 'x', is_default: false });
    projectState.remove = vi.fn().mockResolvedValue(undefined);
  });

  it('renders the API base URL', () => {
    render(<AdminPage />);
    expect(screen.getByText(/API base URL/i)).toBeInTheDocument();
  });

  it('lists every project', () => {
    render(<AdminPage />);
    expect(within(screen.getByRole('table')).getByText('Default')).toBeInTheDocument();
    expect(within(screen.getByRole('table')).getByText('Payments')).toBeInTheDocument();
  });

  it('renames a project through the inline editor', async () => {
    render(<AdminPage />);

    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Rename Payments' }));
    const input = within(screen.getByRole('table')).getByRole('textbox', { name: /new name/i });
    fireEvent.change(input, { target: { value: 'Billing' } });
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(projectState.rename).toHaveBeenCalledWith('p2', 'Billing'));
    // A successful rename must close the inline editor -- otherwise the row
    // stays stuck showing the input and Save/Cancel buttons.
    await waitFor(() =>
      expect(within(screen.getByRole('table')).queryByRole('textbox', { name: /new name/i })).not.toBeInTheDocument()
    );
  });

  it('cancelling a rename leaves the name alone', () => {
    render(<AdminPage />);

    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Rename Payments' }));
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Cancel' }));

    expect(screen.queryByRole('textbox', { name: /new name/i })).not.toBeInTheDocument();
    expect(projectState.rename).not.toHaveBeenCalled();
  });

  // A rejected name is usually one character from an accepted one, so the row
  // stays in edit state with what was typed still there. The mock here
  // rejects with the same ApiError shape renameProject throws, but that shape
  // is asserted by mocking useProjects().rename directly -- it says nothing
  // about renameProject's own implementation, so it would not catch a
  // regression back to unwrap()'s "request failed (…): " prefix. That
  // discrimination happens at the api-client boundary, in
  // __tests__/api-client.test.ts's "renameProject throws an ApiError
  // carrying the status and the raw server text, with no unwrap prefix".
  it('keeps the editor open and shows the error when a rename is rejected', async () => {
    projectState.rename = vi.fn().mockRejectedValue(new ApiError(409, 'a project with that name already exists'));
    render(<AdminPage />);

    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Rename Payments' }));
    fireEvent.change(within(screen.getByRole('table')).getByRole('textbox', { name: /new name/i }), {
      target: { value: 'Default' },
    });
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Save' }));

    const message = await within(screen.getByRole('table')).findByText(/already exists/i);
    expect(message).toBeInTheDocument();
    expect(message.textContent).not.toContain('request failed');
    expect(within(screen.getByRole('table')).getByRole('textbox', { name: /new name/i })).toHaveValue('Default');
  });

  // Cancel used to close the editor without clearing `error`, so a rejected
  // rename's message stayed pinned under the project name indefinitely.
  it('cancelling a rejected rename clears the error message', async () => {
    projectState.rename = vi.fn().mockRejectedValue(new ApiError(409, 'a project with that name already exists'));
    render(<AdminPage />);

    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Rename Payments' }));
    fireEvent.change(within(screen.getByRole('table')).getByRole('textbox', { name: /new name/i }), {
      target: { value: 'Default' },
    });
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Save' }));
    await within(screen.getByRole('table')).findByText(/already exists/i);

    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Cancel' }));

    expect(within(screen.getByRole('table')).queryByText(/already exists/i)).not.toBeInTheDocument();
  });

  it('deletes a project after a confirmation step', async () => {
    render(<AdminPage />);

    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Delete Payments' }));
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Confirm' }));

    await waitFor(() => expect(projectState.remove).toHaveBeenCalledWith('p2'));
    // A successful delete must close the confirm strip -- otherwise the row
    // stays stuck showing "Delete Payments?" / Confirm / Cancel.
    await waitFor(() =>
      expect(within(screen.getByRole('table')).queryByRole('button', { name: 'Confirm' })).not.toBeInTheDocument()
    );
  });

  it('cancelling a delete does not remove anything', () => {
    render(<AdminPage />);

    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Delete Payments' }));
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Cancel' }));

    expect(projectState.remove).not.toHaveBeenCalled();
  });

  // The server's 409 names the project and the count; showing it verbatim is
  // what tells the user how much work emptying the project is.
  it('shows the server message when a delete is refused', async () => {
    projectState.remove = vi
      .fn()
      .mockRejectedValue(new Error('Payments still has 3 tests; move or delete them first'));
    render(<AdminPage />);

    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Delete Payments' }));
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Confirm' }));

    expect(await within(screen.getByRole('table')).findByText(/still has 3 tests/i)).toBeInTheDocument();
  });

  // Same bug as the rename Cancel button: closing the confirm step without
  // clearing `error` left a refused delete's message pinned under the name.
  it('cancelling after a refused delete clears the error message', async () => {
    projectState.remove = vi
      .fn()
      .mockRejectedValue(new Error('Payments still has 3 tests; move or delete them first'));
    render(<AdminPage />);

    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Delete Payments' }));
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Confirm' }));
    await within(screen.getByRole('table')).findByText(/still has 3 tests/i);

    // confirmDelete only sets `error` on failure; confirmingId is untouched,
    // so the row is still in the confirm step and Cancel is already visible.
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: 'Cancel' }));

    expect(within(screen.getByRole('table')).queryByText(/still has 3 tests/i)).not.toBeInTheDocument();
  });

  it('disables delete on the default project and says why', () => {
    render(<AdminPage />);
    const button = within(screen.getByRole('table')).getByRole('button', { name: 'Delete Default' });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('title', 'the default project cannot be deleted');
  });
});
