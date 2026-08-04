import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  listRunsForTest,
  listTests,
  createTest,
  startRun,
  getRun,
  cancelRun,
  listTestVersions,
  updateTest,
  listProjects,
  createProject,
  renameProject,
  deleteProject,
  moveTest,
  ApiError,
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

describe('ApiError', () => {
  afterEach(() => vi.restoreAllMocks());

  it('carries the status code and is still an Error', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      text: async () => 'test was modified concurrently; reload and retry',
    }) as unknown as typeof fetch;

    await expect(updateTest('t1', { name: 'n', target_url: 'http://x', virtual_users: 1, duration_seconds: 1 }))
      .rejects.toBeInstanceOf(ApiError);

    try {
      await updateTest('t1', { name: 'n', target_url: 'http://x', virtual_users: 1, duration_seconds: 1 });
      expect.unreachable('should have thrown');
    } catch (err) {
      expect(err).toBeInstanceOf(Error);
      expect((err as ApiError).status).toBe(409);
      expect((err as ApiError).message).toContain('modified concurrently');
    }
  });
});

describe('listTestVersions', () => {
  afterEach(() => vi.restoreAllMocks());

  it('fetches the versions of a test newest-first', async () => {
    const versions = [
      { id: 't1', version_id: 'v2', version: 2, project_id: 'p1', name: 'smoke', target_url: 'http://b', virtual_users: 2, duration_seconds: 2, created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-25T00:00:00Z' },
      { id: 't1', version_id: 'v1', version: 1, project_id: 'p1', name: 'smoke', target_url: 'http://a', virtual_users: 1, duration_seconds: 1, created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-24T00:00:00Z' },
    ];
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => versions }) as unknown as typeof fetch;

    const result = await listTestVersions('t1');
    expect(result).toEqual(versions);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/tests/t1/versions'),
      expect.objectContaining({ cache: 'no-store' })
    );
  });

  it('defaults to an empty array if the API returns null', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => null }) as unknown as typeof fetch;
    await expect(listTestVersions('t1')).resolves.toEqual([]);
  });

  it('throws an ApiError with status 404 for an unknown test', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 404, text: async () => 'test not found' }) as unknown as typeof fetch;
    await expect(listTestVersions('nope')).rejects.toMatchObject({ status: 404 });
  });
});

describe('updateTest', () => {
  afterEach(() => vi.restoreAllMocks());

  it('PUTs the input and returns the new version', async () => {
    const input = { name: 'smoke', target_url: 'http://x', virtual_users: 5, duration_seconds: 30 };
    const saved = { id: 't1', version_id: 'v2', version: 2, project_id: 'p1', ...input, created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-25T00:00:00Z' };
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => saved }) as unknown as typeof fetch;

    const result = await updateTest('t1', input);
    expect(result).toEqual(saved);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/tests/t1'),
      expect.objectContaining({ method: 'PUT', body: JSON.stringify(input) })
    );
  });
});

describe('listProjects', () => {
  afterEach(() => vi.restoreAllMocks());

  it('fetches the project registry', async () => {
    const projects = [{ id: 'p1', name: 'Default', created_at: '2026-07-24T00:00:00Z' }];
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => projects }) as unknown as typeof fetch;

    const result = await listProjects();
    expect(result).toEqual(projects);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/projects'),
      expect.objectContaining({ cache: 'no-store' })
    );
  });

  it('defaults to an empty array if the API returns null', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => null }) as unknown as typeof fetch;
    await expect(listProjects()).resolves.toEqual([]);
  });
});

describe('createProject', () => {
  afterEach(() => vi.restoreAllMocks());

  it('POSTs the name and returns the created project', async () => {
    const created = { id: 'p2', name: 'Payments', created_at: '2026-07-29T00:00:00Z' };
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => created }) as unknown as typeof fetch;

    await expect(createProject('Payments')).resolves.toEqual(created);
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/projects'),
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ name: 'Payments' }) })
    );
  });

  it('throws an ApiError with status 409 for a duplicate name', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      text: async () => 'a project with that name already exists',
    }) as unknown as typeof fetch;
    await expect(createProject('Payments')).rejects.toMatchObject({ status: 409 });
  });
});

describe('listTests project scoping', () => {
  afterEach(() => vi.restoreAllMocks());

  it('appends project_id when given one', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => [] }) as unknown as typeof fetch;
    await listTests('p2');
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/tests?project_id=p2'),
      expect.objectContaining({ cache: 'no-store' })
    );
  });

  it('omits the parameter entirely when given none', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => [] }) as unknown as typeof fetch;
    await listTests();
    const url = (global.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    expect(url).not.toContain('project_id');
  });
});

describe('renameProject', () => {
  // Earlier tests in this file stub `global.fetch` via plain assignment
  // (`global.fetch = vi.fn()...`) rather than `vi.spyOn`. restoreAllMocks
  // can't undo a plain reassignment, so that mock function -- and its call
  // history -- survives into these tests. vi.spyOn on an already-mocked
  // function reuses that same instance rather than wrapping it, so without
  // clearing here, fetchMock.mock.calls[0] would be a leftover call from
  // whichever test ran last, not this test's own call.
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.restoreAllMocks());

  it('renameProject PUTs the new name', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ id: 'p1', name: 'Billing', created_at: 'x', is_default: false }), { status: 200 })
    );

    const got = await renameProject('p1', 'Billing');

    expect(got.name).toBe('Billing');
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('/api/projects/p1');
    expect(init?.method).toBe('PUT');
    expect(init?.body).toBe(JSON.stringify({ name: 'Billing' }));
  });

  // Unlike the other write endpoints (which go through unwrap and get a
  // "request failed (409): " prefix), renameProject's message must be the
  // raw server text -- the admin table renders it verbatim next to the row.
  it('renameProject throws an ApiError carrying the status and the raw server text, with no unwrap prefix', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue(new Response('a project with that name already exists', { status: 409 }));
    await expect(renameProject('p1', 'Default')).rejects.toMatchObject({
      status: 409,
      message: 'a project with that name already exists',
    });
    try {
      await renameProject('p1', 'Default');
      expect.unreachable('should have thrown');
    } catch (err) {
      expect((err as ApiError).message).not.toContain('request failed');
    }
  });
});

describe('deleteProject', () => {
  // See the comment in the renameProject describe above: a leftover
  // global.fetch mock from an earlier plain-assignment test otherwise leaks
  // its call history into fetchMock.mock.calls here.
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.restoreAllMocks());

  it('deleteProject DELETEs and resolves on 204', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockResolvedValue(new Response(null, { status: 204 }));

    await expect(deleteProject('p1')).resolves.toBeUndefined();

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('/api/projects/p1');
    expect(init?.method).toBe('DELETE');
  });

  // The 409 body is the message the admin table shows, so it has to survive.
  it('deleteProject throws an ApiError whose message carries the server text', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response('Payments still has 3 tests; move or delete them first', { status: 409 })
    );
    await expect(deleteProject('p1')).rejects.toMatchObject({
      status: 409,
      message: expect.stringContaining('still has 3 tests'),
    });
  });
});

describe('moveTest', () => {
  // See the comment in the renameProject describe above.
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.restoreAllMocks());

  it('moveTest PUTs the destination project', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ id: 't1', name: 'x', target_url: 'http://x', virtual_users: 1, duration_seconds: 1, created_at: 'x', project_id: 'p2' }), { status: 200 })
    );

    const got = await moveTest('t1', 'p2');

    expect(got.project_id).toBe('p2');
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain('/api/tests/t1/project');
    expect(init?.method).toBe('PUT');
    expect(init?.body).toBe(JSON.stringify({ project_id: 'p2' }));
  });
});
