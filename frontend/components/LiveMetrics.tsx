'use client';

import { Run, RunMetricSnapshot } from '@/lib/api-client';

const ACTIVE_STATUSES = new Set(['pending', 'running']);

export function LiveMetrics({
  run,
  latest,
  onCancel,
}: {
  run: Run;
  latest?: RunMetricSnapshot;
  onCancel: () => void;
}) {
  return (
    <section className="flex flex-col gap-4">
      <div className="flex items-center gap-4">
        <h2 className="text-xl">Status: {run.status}</h2>
        {ACTIVE_STATUSES.has(run.status) && <button onClick={onCancel}>Cancel</button>}
      </div>
      {run.error_message && <p className="text-red-600">{run.error_message}</p>}
      <div className="grid grid-cols-4 gap-4">
        <div>
          <div className="text-sm text-gray-500">Throughput (req/s)</div>
          <div className="text-2xl">{latest ? latest.throughput_rps.toFixed(1) : '—'}</div>
        </div>
        <div>
          <div className="text-sm text-gray-500">Avg response time (ms)</div>
          <div className="text-2xl">{latest ? latest.avg_response_time_ms.toFixed(0) : '—'}</div>
        </div>
        <div>
          <div className="text-sm text-gray-500">Error rate (%)</div>
          <div className="text-2xl">{latest ? latest.error_rate_pct.toFixed(1) : '—'}</div>
        </div>
        <div>
          <div className="text-sm text-gray-500">Elapsed (s)</div>
          <div className="text-2xl">{latest ? latest.elapsed_seconds : '—'}</div>
        </div>
      </div>
    </section>
  );
}
