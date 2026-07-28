import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { EditTestForm } from '@/components/EditTestForm';
import type { TestVersion } from '@/lib/api-client';

const current: TestVersion = {
  id: 't1',
  version_id: 'v2',
  version: 2,
  project_id: 'p1',
  name: 'Checkout Load',
  target_url: 'http://x',
  virtual_users: 5,
  duration_seconds: 30,
  created_at: '2026-07-24T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
};

describe('EditTestForm', () => {
  it('seeds the fields from the current version', () => {
    render(<EditTestForm current={current} onSave={vi.fn()} />);
    expect(screen.getByLabelText(/name/i)).toHaveValue('Checkout Load');
    expect(screen.getByLabelText(/target url/i)).toHaveValue('http://x');
    expect(screen.getByLabelText(/virtual users/i)).toHaveValue(5);
    expect(screen.getByLabelText(/duration/i)).toHaveValue(30);
  });

  it('submits the edited values as numbers', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(<EditTestForm current={current} onSave={onSave} />);

    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '9' } });
    fireEvent.click(screen.getByRole('button', { name: /save as new version/i }));

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        name: 'Checkout Load',
        target_url: 'http://x',
        virtual_users: 9,
        duration_seconds: 30,
      })
    );
  });

  it('re-seeds when a different version becomes current', () => {
    const { rerender } = render(<EditTestForm current={current} onSave={vi.fn()} />);
    rerender(
      <EditTestForm current={{ ...current, version_id: 'v3', version: 3, virtual_users: 42 }} onSave={vi.fn()} />
    );
    expect(screen.getByLabelText(/virtual users/i)).toHaveValue(42);
  });

  // The interlock that makes the parent's 409 handling work: a re-render that
  // carries the same version_id must not overwrite what the user is typing.
  it('keeps edits when re-rendered with the same version', () => {
    const { rerender } = render(<EditTestForm current={current} onSave={vi.fn()} />);
    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '77' } });
    rerender(<EditTestForm current={{ ...current }} onSave={vi.fn()} />);
    expect(screen.getByLabelText(/virtual users/i)).toHaveValue(77);
  });

  it('shows the error passed in by its parent', () => {
    render(<EditTestForm current={current} onSave={vi.fn()} error="This test was changed elsewhere" />);
    expect(screen.getByText(/changed elsewhere/i)).toBeInTheDocument();
  });
});
