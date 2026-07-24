import { describe, it, expect, vi, afterEach } from 'vitest';
import { listRunsForTest } from '@/lib/api-client';

describe('listRunsForTest', () => {
  afterEach(() => vi.restoreAllMocks());

  it('fetches runs for a test and returns them', async () => {
    const runs = [{ id: 'r1', test_id: 't1', status: 'completed', created_at: '2026-07-24T00:00:00Z' }];
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => runs,
    }) as unknown as typeof fetch;

    const result = await listRunsForTest('t1');
    expect(result).toEqual(runs);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/tests/t1/runs'),
      expect.objectContaining({ cache: 'no-store' })
    );
  });

  it('defaults to an empty array if the API returns null', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => null }) as unknown as typeof fetch;
    const result = await listRunsForTest('t1');
    expect(result).toEqual([]);
  });
});
