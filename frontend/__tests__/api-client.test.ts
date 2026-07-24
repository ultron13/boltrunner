import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  listRunsForTest,
  listTests,
  createTest,
  startRun,
  getRun,
  cancelRun,
} from '@/lib/api-client';

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

  it('throws with the response body when the request fails', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      text: async () => 'boom',
    }) as unknown as typeof fetch;

    await expect(listRunsForTest('t1')).rejects.toThrow('request failed (500): boom');
  });
});

describe('listTests', () => {
  afterEach(() => vi.restoreAllMocks());

  it('fetches all tests', async () => {
    const tests = [{ id: 't1', name: 'smoke', target_url: 'http://x', virtual_users: 1, duration_seconds: 1, created_at: '2026-07-24T00:00:00Z' }];
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => tests }) as unknown as typeof fetch;

    const result = await listTests();
    expect(result).toEqual(tests);
    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/tests'), expect.objectContaining({ cache: 'no-store' }));
  });

  it('defaults to an empty array if the API returns null', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => null }) as unknown as typeof fetch;
    const result = await listTests();
    expect(result).toEqual([]);
  });
});

describe('createTest', () => {
  afterEach(() => vi.restoreAllMocks());

  it('posts the input and returns the created test', async () => {
    const input = { name: 'smoke', target_url: 'http://x', virtual_users: 5, duration_seconds: 30 };
    const created = { id: 't1', ...input, created_at: '2026-07-24T00:00:00Z' };
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => created }) as unknown as typeof fetch;

    const result = await createTest(input);
    expect(result).toEqual(created);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/tests'),
      expect.objectContaining({ method: 'POST', body: JSON.stringify(input) })
    );
  });
});

describe('startRun', () => {
  afterEach(() => vi.restoreAllMocks());

  it('posts to the runs endpoint and returns the new run', async () => {
    const run = { id: 'r1', test_id: 't1', status: 'pending' };
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => run }) as unknown as typeof fetch;

    const result = await startRun('t1');
    expect(result).toEqual(run);
    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/tests/t1/runs'), expect.objectContaining({ method: 'POST' }));
  });
});

describe('getRun', () => {
  afterEach(() => vi.restoreAllMocks());

  it('fetches a single run with its history', async () => {
    const resp = { run: { id: 'r1', test_id: 't1', status: 'completed' }, history: [] };
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => resp }) as unknown as typeof fetch;

    const result = await getRun('r1');
    expect(result).toEqual(resp);
    expect(global.fetch).toHaveBeenCalledWith(expect.stringContaining('/api/runs/r1'), expect.objectContaining({ cache: 'no-store' }));
  });
});

describe('cancelRun', () => {
  afterEach(() => vi.restoreAllMocks());

  it('resolves when the API returns 204', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 204 }) as unknown as typeof fetch;
    await expect(cancelRun('r1')).resolves.toBeUndefined();
  });

  it('resolves when the API returns 200 ok', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200 }) as unknown as typeof fetch;
    await expect(cancelRun('r1')).resolves.toBeUndefined();
  });

  it('throws when the API returns an unexpected error status', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500 }) as unknown as typeof fetch;
    await expect(cancelRun('r1')).rejects.toThrow('cancel failed (500)');
  });
});
