const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080';

export type Test = {
  id: string;
  name: string;
  target_url: string;
  virtual_users: number;
  duration_seconds: number;
  created_at: string;
};

export type RunStatus = 'pending' | 'running' | 'completed' | 'failed' | 'stopped';

export type Run = {
  id: string;
  test_id: string;
  status: RunStatus;
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
};

async function unwrap<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`request failed (${res.status}): ${text}`);
  }
  return res.json() as Promise<T>;
}

export async function listTests(): Promise<Test[]> {
  const tests = await unwrap<Test[]>(await fetch(`${API_URL}/api/tests`, { cache: 'no-store' }));
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

export async function cancelRun(runId: string): Promise<void> {
  const res = await fetch(`${API_URL}/api/runs/${runId}/cancel`, { method: 'POST' });
  if (!res.ok && res.status !== 204) {
    throw new Error(`cancel failed (${res.status})`);
  }
}
