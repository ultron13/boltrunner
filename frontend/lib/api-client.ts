const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080';

export type Test = {
  id: string;
  name: string;
  target_url: string;
  virtual_users: number;
  duration_seconds: number;
  created_at: string;
  // Present on every test-shaped response the backend sends today. Optional
  // here so the existing list-shaped fixtures keep typechecking; use
  // TestVersion wherever version identity is actually required.
  version?: number;
  version_id?: string;
  project_id?: string;
  updated_at?: string;
};

export type TestVersion = Test & {
  version: number;
  version_id: string;
  updated_at: string;
};

export type Project = { id: string; name: string; created_at: string };

export type RunStatus = 'pending' | 'running' | 'completed' | 'failed' | 'stopped';

export type Run = {
  id: string;
  test_id: string;
  status: RunStatus;
  created_at?: string;
  started_at?: string;
  completed_at?: string;
  error_message?: string;
};

export type RunMetricSnapshot = {
  id: string;
  run_id: string;
  ts: string;
  elapsed_seconds: number;
  throughput_rps: number;
  avg_response_time_ms: number;
  error_rate_pct: number;
  sample_count: number;
};

export type GetRunResponse = {
  run: Run;
  latest?: RunMetricSnapshot;
  history: RunMetricSnapshot[];
};

export type CreateTestInput = {
  name: string;
  target_url: string;
  virtual_users: number;
  duration_seconds: number;
  // Optional: the backend COALESCEs a missing value to the Default project.
  project_id?: string;
};

export type UpdateTestInput = CreateTestInput;

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

async function unwrap<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const text = await res.text();
    throw new ApiError(res.status, `request failed (${res.status}): ${text}`);
  }
  return res.json() as Promise<T>;
}

export async function listTests(projectId?: string): Promise<Test[]> {
  const url = projectId
    ? `${API_URL}/api/tests?project_id=${encodeURIComponent(projectId)}`
    : `${API_URL}/api/tests`;
  const tests = await unwrap<Test[]>(await fetch(url, { cache: 'no-store' }));
  return tests ?? [];
}

export async function createTest(input: CreateTestInput): Promise<Test> {
  return unwrap(
    await fetch(`${API_URL}/api/tests`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
  );
}

export async function startRun(testId: string): Promise<Run> {
  return unwrap(await fetch(`${API_URL}/api/tests/${testId}/runs`, { method: 'POST' }));
}

export async function getRun(runId: string): Promise<GetRunResponse> {
  return unwrap(await fetch(`${API_URL}/api/runs/${runId}`, { cache: 'no-store' }));
}

export async function listRunsForTest(testId: string): Promise<Run[]> {
  const runs = await unwrap<Run[]>(await fetch(`${API_URL}/api/tests/${testId}/runs`, { cache: 'no-store' }));
  return runs ?? [];
}

export async function listTestVersions(testId: string): Promise<TestVersion[]> {
  const versions = await unwrap<TestVersion[]>(
    await fetch(`${API_URL}/api/tests/${testId}/versions`, { cache: 'no-store' })
  );
  return versions ?? [];
}

export async function updateTest(testId: string, input: UpdateTestInput): Promise<TestVersion> {
  return unwrap(
    await fetch(`${API_URL}/api/tests/${testId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
  );
}

export async function listProjects(): Promise<Project[]> {
  const projects = await unwrap<Project[]>(await fetch(`${API_URL}/api/projects`, { cache: 'no-store' }));
  return projects ?? [];
}

export async function createProject(name: string): Promise<Project> {
  return unwrap(
    await fetch(`${API_URL}/api/projects`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    })
  );
}

export async function cancelRun(runId: string): Promise<void> {
  const res = await fetch(`${API_URL}/api/runs/${runId}/cancel`, { method: 'POST' });
  if (!res.ok && res.status !== 204) {
    throw new Error(`cancel failed (${res.status})`);
  }
}
