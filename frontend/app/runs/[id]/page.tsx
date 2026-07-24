'use client';

import { useParams } from 'next/navigation';
import { useRunPolling } from '@/hooks/useRunPolling';
import { cancelRun } from '@/lib/api-client';
import { LiveMetrics } from '@/components/LiveMetrics';
import { MetricsChart } from '@/components/MetricsChart';

export default function RunPage() {
  const params = useParams<{ id: string }>();
  const { data, error } = useRunPolling(params.id);

  if (error) return <p className="text-red-600 p-8">{error}</p>;
  if (!data) return <p className="p-8">Loading…</p>;

  return (
    <main className="p-8 flex flex-col gap-8">
      <LiveMetrics run={data.run} latest={data.latest} onCancel={() => cancelRun(params.id)} />
      <MetricsChart history={data.history} />
    </main>
  );
}
