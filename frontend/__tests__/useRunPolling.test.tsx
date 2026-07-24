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
});
