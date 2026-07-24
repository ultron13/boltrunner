import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useRunPolling } from '@/hooks/useRunPolling';
import * as api from '@/lib/api-client';

describe('useRunPolling', () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(() => vi.useRealTimers());

  it('polls repeatedly while running, then stops once a terminal status is reached', async () => {
    const running = { run: { id: 'r1', test_id: 't1', status: 'running' as const }, history: [] };
    const completed = { run: { id: 'r1', test_id: 't1', status: 'completed' as const }, history: [] };

    const getRun = vi.spyOn(api, 'getRun')
      .mockResolvedValueOnce(running)
      .mockResolvedValueOnce(running)
      .mockResolvedValue(completed);

    const { result } = renderHook(() => useRunPolling('r1', 5));

    await waitFor(() => expect(result.current.data?.run.status).toBe('completed'));
    expect(getRun.mock.calls.length).toBeGreaterThanOrEqual(3);

    const callsAtCompletion = getRun.mock.calls.length;
    await new Promise((r) => setTimeout(r, 30));
    expect(getRun.mock.calls.length).toBe(callsAtCompletion);
  });

  it('surfaces an error message when getRun fails', async () => {
    vi.spyOn(api, 'getRun').mockRejectedValue(new Error('run not found'));

    const { result } = renderHook(() => useRunPolling('missing', 5));

    await waitFor(() => expect(result.current.error).toBe('run not found'));
    expect(result.current.data).toBeNull();
  });

  it('shows a generic error message when getRun throws a non-Error value', async () => {
    vi.spyOn(api, 'getRun').mockRejectedValue('some string rejection');

    const { result } = renderHook(() => useRunPolling('missing', 5));

    await waitFor(() => expect(result.current.error).toBe('failed to fetch run'));
  });
});
