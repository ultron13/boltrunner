import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TestDetailPanel } from '@/components/TestDetailPanel';
import * as api from '@/lib/api-client';
import { ApiError } from '@/lib/api-client';
import type { TestVersion } from '@/lib/api-client';

const push = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push }),
}));

const projectState = vi.hoisted(() => ({
  projects: [
    { id: 'p1', name: 'Default', created_at: 'x', is_default: true },
    { id: 'p2', name: 'Payments', created_at: 'x', is_default: false },
  ],
  selectedId: 'p1' as string | null,
}));

vi.mock('@/components/ui/ProjectProvider', () => ({
  useProjects: () => ({
    projects: projectState.projects,
    selectedId: projectState.selectedId,
    selected: projectState.projects.find((p) => p.id === projectState.selectedId) ?? null,
    select: vi.fn(),
    create: vi.fn(),
    rename: vi.fn(),
    remove: vi.fn(),
  }),
}));

const v2: TestVersion = {
  id: 't1',
  version_id: 'vid2',
  version: 2,
  project_id: 'p1',
  name: 'Checkout Load',
  target_url: 'http://b',
  virtual_users: 9,
  duration_seconds: 30,
  created_at: '2026-07-24T00:00:00Z',
  updated_at: '2026-07-25T12:00:00Z',
};
const v1: TestVersion = {
  ...v2,
  version_id: 'vid1',
  version: 1,
  target_url: 'http://a',
  virtual_users: 5,
  updated_at: '2026-07-24T00:00:00Z',
};

describe('TestDetailPanel', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    // `push` is a plain vi.fn(), not a vi.spyOn() spy, so restoreAllMocks()
    // (which only restores spies) leaves its call history intact across
    // tests. Later cases assert push was NOT called with a given path, which
    // needs a clean slate rather than history accumulated from earlier tests.
    push.mockClear();
  });

  it('seeds the form from the newest version and lists every version', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    render(<TestDetailPanel testId="t1" />);

    expect(await screen.findByLabelText(/virtual users/i)).toHaveValue(9);
    expect(screen.getByRole('row', { name: /v2/ })).toBeInTheDocument();
    expect(screen.getByRole('row', { name: /v1/ })).toBeInTheDocument();
  });

  it('saves an edit and shows the reloaded version list', async () => {
    const v3: TestVersion = {
      ...v2,
      version_id: 'vid3',
      version: 3,
      virtual_users: 20,
      updated_at: '2026-07-26T00:00:00Z',
    };
    vi.spyOn(api, 'listTestVersions')
      .mockResolvedValueOnce([v2, v1])
      .mockResolvedValueOnce([v3, v2, v1]);
    const updateTest = vi.spyOn(api, 'updateTest').mockResolvedValue(v3);

    render(<TestDetailPanel testId="t1" />);
    await screen.findByLabelText(/virtual users/i);

    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '20' } });
    fireEvent.click(screen.getByRole('button', { name: /save as new version/i }));

    await waitFor(() =>
      expect(updateTest).toHaveBeenCalledWith('t1', expect.objectContaining({ virtual_users: 20 }))
    );
    expect(await screen.findByRole('row', { name: /v3/ })).toBeInTheDocument();
  });

  it('renders a not-found state when the test does not exist', async () => {
    vi.spyOn(api, 'listTestVersions').mockRejectedValue(new ApiError(404, 'test not found'));
    render(<TestDetailPanel testId="ghost" />);

    expect(await screen.findByText(/test not found/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /save as new version/i })).not.toBeInTheDocument();
  });

  it('renders an error state when loading fails for another reason', async () => {
    vi.spyOn(api, 'listTestVersions').mockRejectedValue(new ApiError(500, 'boom'));
    render(<TestDetailPanel testId="t1" />);
    expect(await screen.findByText(/couldn't load this test/i)).toBeInTheDocument();
  });

  it('reloads when Retry is clicked after a load failure', async () => {
    const listTestVersions = vi
      .spyOn(api, 'listTestVersions')
      .mockRejectedValueOnce(new ApiError(500, 'boom'))
      .mockResolvedValueOnce([v2, v1]);

    render(<TestDetailPanel testId="t1" />);
    fireEvent.click(await screen.findByRole('button', { name: /retry/i }));

    expect(await screen.findByLabelText(/virtual users/i)).toHaveValue(9);
    expect(listTestVersions).toHaveBeenCalledTimes(2);
  });

  // The behavior most likely to regress silently: a naive implementation
  // re-seeds the form from the reloaded version and throws away the user's work
  // for a conflict they did not cause.
  it('keeps the typed values and reloads history when the save conflicts', async () => {
    vi.spyOn(api, 'listTestVersions')
      .mockResolvedValueOnce([{ ...v2 }, { ...v1 }])
      // Fresh objects, as a real fetch returns: reusing the same references
      // here would let a re-seed keyed on object identity pass this test while
      // still wiping the user's edits in production.
      .mockResolvedValueOnce([{ ...v2 }, { ...v1 }]);
    vi.spyOn(api, 'updateTest').mockRejectedValue(
      new ApiError(409, 'request failed (409): test was modified concurrently; reload and retry')
    );

    render(<TestDetailPanel testId="t1" />);
    await screen.findByLabelText(/virtual users/i);

    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '77' } });
    fireEvent.click(screen.getByRole('button', { name: /save as new version/i }));

    expect(await screen.findByText(/changed elsewhere/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/virtual users/i)).toHaveValue(77);
  });

  it('surfaces a validation error from the server without losing the form', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    vi.spyOn(api, 'updateTest').mockRejectedValue(
      new ApiError(400, 'request failed (400): name, target_url, virtual_users>0, duration_seconds>0 are required')
    );

    render(<TestDetailPanel testId="t1" />);
    await screen.findByLabelText(/virtual users/i);
    fireEvent.click(screen.getByRole('button', { name: /save as new version/i }));

    expect(await screen.findByText(/are required/i)).toBeInTheDocument();
  });

  it('shows the not-found state when the test is deleted between load and save', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    vi.spyOn(api, 'updateTest').mockRejectedValue(new ApiError(404, 'test not found'));

    render(<TestDetailPanel testId="t1" />);
    await screen.findByLabelText(/virtual users/i);
    fireEvent.click(screen.getByRole('button', { name: /save as new version/i }));

    expect(await screen.findByText(/test not found/i)).toBeInTheDocument();
  });

  it('starts a run and navigates to it', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    vi.spyOn(api, 'startRun').mockResolvedValue({ id: 'r1', test_id: 'vid2', status: 'pending' });

    render(<TestDetailPanel testId="t1" />);
    fireEvent.click(await screen.findByRole('button', { name: /run test/i }));

    await waitFor(() => expect(push).toHaveBeenCalledWith('/runs/r1'));
  });

  it('links to the run history for this test', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    render(<TestDetailPanel testId="t1" />);
    expect(await screen.findByRole('link', { name: /run history/i })).toHaveAttribute(
      'href',
      '/history?testId=t1'
    );
  });

  it('treats an empty version list as not found rather than crashing', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([]);
    render(<TestDetailPanel testId="t1" />);
    expect(await screen.findByText(/test not found/i)).toBeInTheDocument();
  });

  it('moves the test to the chosen project', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    const moveTest = vi.spyOn(api, 'moveTest').mockResolvedValue({
      id: 't1', name: 'smoke', target_url: 'http://x', virtual_users: 1,
      duration_seconds: 1, created_at: 'x', project_id: 'p2',
    });

    render(<TestDetailPanel testId="t1" />);
    await screen.findByRole('heading', { name: /Checkout Load/i });

    fireEvent.change(screen.getByRole('combobox', { name: /move to project/i }), { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('button', { name: /^move$/i }));

    await waitFor(() => expect(moveTest).toHaveBeenCalledWith('t1', 'p2'));
  });

  // Staying put would leave a detail page open for a test the selected
  // workspace no longer contains, beside a TreeNav still listing it: Shell's
  // fetch keys on selectedId, which has not changed, so nothing refetches.
  it('redirects to the test list after a successful move', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    vi.spyOn(api, 'moveTest').mockResolvedValue({
      id: 't1', name: 'smoke', target_url: 'http://x', virtual_users: 1,
      duration_seconds: 1, created_at: 'x', project_id: 'p2',
    });

    render(<TestDetailPanel testId="t1" />);
    await screen.findByRole('heading', { name: /Checkout Load/i });

    fireEvent.change(screen.getByRole('combobox', { name: /move to project/i }), { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('button', { name: /^move$/i }));

    await waitFor(() => expect(push).toHaveBeenCalledWith('/tests'));
  });

  it('shows an error and stays put when the move fails', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);
    vi.spyOn(api, 'moveTest').mockRejectedValue(new api.ApiError(400, 'unknown project_id'));

    render(<TestDetailPanel testId="t1" />);
    await screen.findByRole('heading', { name: /Checkout Load/i });

    fireEvent.change(screen.getByRole('combobox', { name: /move to project/i }), { target: { value: 'p2' } });
    fireEvent.click(screen.getByRole('button', { name: /^move$/i }));

    expect(await screen.findByText(/unknown project_id/i)).toBeInTheDocument();
    expect(push).not.toHaveBeenCalledWith('/tests');
  });

  // Moving a test to the project it is already in is a no-op the user did not
  // mean, so the button is unavailable until the selection actually changes.
  it('disables the move button while the destination is the current project', async () => {
    vi.spyOn(api, 'listTestVersions').mockResolvedValue([v2, v1]);

    // Reordered so the test's current project ('p1') is NOT the first
    // <option> in the DOM. A controlled <select> whose `value` matches no
    // <option> (e.g. destination stuck at '' because the seeding effect was
    // deleted) falls back to the browser's native "select the first option"
    // default -- which would silently read as 'p1' below and defeat this
    // assertion if 'p1' stayed first in the list. Restored after the test so
    // later additions to this file are not surprised by fixture order.
    const original = projectState.projects;
    projectState.projects = [
      { id: 'p2', name: 'Payments', created_at: 'x', is_default: false },
      { id: 'p1', name: 'Default', created_at: 'x', is_default: true },
    ];

    try {
      render(<TestDetailPanel testId="t1" />);
      await screen.findByRole('heading', { name: /Checkout Load/i });

      // Pins the reason the button is disabled: the destination was actually
      // seeded to the test's current project (v2/v1's project_id is 'p1'),
      // not merely left empty. Without this, deleting the seeding effect
      // entirely leaves destination === '' forever, which also disables the
      // button via the `!destination` clause and would pass unnoticed.
      expect(await screen.findByRole('combobox', { name: /move to project/i })).toHaveValue('p1');
      expect(screen.getByRole('button', { name: /^move$/i })).toBeDisabled();
    } finally {
      projectState.projects = original;
    }
  });
});
