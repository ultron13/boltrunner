import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { CreateTestForm } from '@/components/CreateTestForm';
import * as api from '@/lib/api-client';

describe('CreateTestForm', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('submits the form and calls onCreated with the new test', async () => {
    const created = {
      id: 't1', name: 'Smoke', target_url: 'http://example.com',
      virtual_users: 10, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z',
    };
    vi.spyOn(api, 'createTest').mockResolvedValue(created);
    const onCreated = vi.fn();

    render(<CreateTestForm onCreated={onCreated} />);

    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'Smoke' } });
    fireEvent.change(screen.getByLabelText(/target url/i), { target: { value: 'http://example.com' } });
    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '10' } });
    fireEvent.change(screen.getByLabelText(/duration/i), { target: { value: '30' } });
    fireEvent.click(screen.getByRole('button', { name: /create test/i }));

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(created));
    expect(api.createTest).toHaveBeenCalledWith({
      name: 'Smoke', target_url: 'http://example.com', virtual_users: 10, duration_seconds: 30,
    });
  });
});
