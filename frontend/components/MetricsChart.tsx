'use client';

import { LineChart, Line, XAxis, YAxis, Tooltip, CartesianGrid, ResponsiveContainer } from 'recharts';
import { RunMetricSnapshot } from '@/lib/api-client';

export function MetricsChart({ history }: { history: RunMetricSnapshot[] }) {
  if (history.length === 0) {
    return <p>Waiting for the first metric snapshot…</p>;
  }
  return (
    <div style={{ width: '100%', height: 300 }}>
      <ResponsiveContainer>
        <LineChart data={history}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="elapsed_seconds" label={{ value: 'seconds', position: 'insideBottom', offset: -5 }} />
          <YAxis />
          <Tooltip />
          <Line type="monotone" dataKey="avg_response_time_ms" name="Avg response time (ms)" stroke="#209dd7" isAnimationActive={false} />
          <Line type="monotone" dataKey="throughput_rps" name="Throughput (req/s)" stroke="#753991" isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
