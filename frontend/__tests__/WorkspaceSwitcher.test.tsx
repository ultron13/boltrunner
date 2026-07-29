import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { WorkspaceSwitcher } from '@/components/ui/WorkspaceSwitcher';
import { ProjectProvider } from '@/components/ui/ProjectProvider';
import * as api from '@/lib/api-client';
import { ApiError } from '@/lib/api-client';
import type { Project } from '@/lib/api-client';

const def: Project = { id: 'p1', name: 'Default', created_at: '2026-07-24T00:00:00Z' };
const pay: Project = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z' };

function renderSwitcher(projects: Project[] = [def, pay]) {
  vi.spyOn(api, 'listProjects').mockResolvedValue(projects);
  return render(
    <ProjectProvider>
      <WorkspaceSwitcher />
    </ProjectProvider>
  );
}

async function open() {
  const trigger = await screen.findByRole('button', { name: /default/i });
  fireEvent.click(trigger);
  return trigger;
}

describe('WorkspaceSwitcher', () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  it('renders closed by default', async () => {
    renderSwitcher();
    expect(await screen.findByRole('button', { name: /default/i })).toHaveAttribute(
      'aria-expanded',
      'false'
    );
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('lists every project with the selected one checked', async () => {
    renderSwitcher();
    await open();
    expect(screen.getByRole('menuitemradio', { name: /default/i })).toHaveAttribute(
      'aria-checked',
      'true'
    );
    expect(screen.getByRole('menuitemradio', { name: /payments/i })).toHaveAttribute(
      'aria-checked',
      'false'
    );
  });

  it('selects another project and closes', async () => {
    renderSwitcher();
    const trigger = await open();
    fireEvent.click(screen.getByRole('menuitemradio', { name: /payments/i }));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
    expect(await screen.findByRole('button', { name: /payments/i })).toBeInTheDocument();
  });

  it('closes on Escape and returns focus to the trigger', async () => {
    renderSwitcher();
    const trigger = await open();
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('closes when clicking outside', async () => {
    renderSwitcher();
    await open();
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('creates a project from the inline input and selects it', async () => {
    renderSwitcher([def]);
    vi.spyOn(api, 'createProject').mockResolvedValue({
      id: 'p9',
      name: 'Payments',
      created_at: '2026-07-29T00:00:00Z',
    });
    await open();

    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    const input = screen.getByRole('textbox', { name: /project name/i });
    fireEvent.change(input, { target: { value: 'Payments' } });
    fireEvent.submit(input);

    await waitFor(() => expect(api.createProject).toHaveBeenCalledWith('Payments'));
    expect(await screen.findByRole('button', { name: /payments/i })).toBeInTheDocument();
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('trims the typed name before creating', async () => {
    renderSwitcher([def]);
    const createSpy = vi.spyOn(api, 'createProject').mockResolvedValue({
      id: 'p9',
      name: 'Payments',
      created_at: '2026-07-29T00:00:00Z',
    });
    await open();

    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    const input = screen.getByRole('textbox', { name: /project name/i });
    fireEvent.change(input, { target: { value: '  Payments  ' } });
    fireEvent.submit(input);

    await waitFor(() => expect(createSpy).toHaveBeenCalledWith('Payments'));
  });

  // The user typed something worth keeping; a duplicate-name rejection must not
  // throw it away, or they retype the whole name to change one character.
  it('keeps the typed name and shows the message when the name is taken', async () => {
    renderSwitcher([def]);
    vi.spyOn(api, 'createProject').mockRejectedValue(
      new ApiError(409, 'request failed (409): a project with that name already exists')
    );
    await open();

    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    const input = screen.getByRole('textbox', { name: /project name/i });
    fireEvent.change(input, { target: { value: 'Default' } });
    fireEvent.submit(input);

    expect(await screen.findByText(/already exists/i)).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: /project name/i })).toHaveValue('Default');
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });

  it('abandons the inline input on Escape without closing the menu', async () => {
    renderSwitcher([def]);
    await open();
    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    const input = screen.getByRole('textbox', { name: /project name/i });
    fireEvent.keyDown(input, { key: 'Escape' });

    expect(screen.getByRole('menu')).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: /project name/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /new project/i })).toBeInTheDocument();
  });

  it('does not submit an empty or whitespace-only name', async () => {
    renderSwitcher([def]);
    const createSpy = vi.spyOn(api, 'createProject');
    await open();
    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    const input = screen.getByRole('textbox', { name: /project name/i });

    fireEvent.submit(input);
    fireEvent.change(input, { target: { value: '   ' } });
    fireEvent.submit(input);

    expect(createSpy).not.toHaveBeenCalled();
  });

  it('falls back to Default as the label when nothing is selected', async () => {
    vi.spyOn(api, 'listProjects').mockRejectedValue(new Error('boom'));
    render(
      <ProjectProvider>
        <WorkspaceSwitcher />
      </ProjectProvider>
    );
    expect(await screen.findByRole('button', { name: /default/i })).toBeInTheDocument();
  });
});
