'use client';

import { useState } from 'react';
import { useParams } from 'next/navigation';
import { useRunPolling } from '@/hooks/useRunPolling';
import { cancelRun } from '@/lib/api-client';
import { LiveMetrics } from '@/components/LiveMetrics';
import { MetricsChart } from '@/components/MetricsChart';
import { Tabs } from '@/components/ui/Tabs';
import { Card } from '@/components/ui/Card';

export default function RunPage() {
  const params = useParams<{ id: string }>();
  const { data, error } = useRunPolling(params.id);
  const [activeTab, setActiveTab] = useState('details');

  if (error) return <p className="text-red-600">{error}</p>;
  if (!data) return <p>Loading…</p>;

  return (
    <div className="flex flex-col gap-4">
      <Tabs
        tabs={[
          { id: 'details', label: 'Details' },
          { id: 'metrics', label: 'Metrics' },
        ]}
        activeId={activeTab}
        onChange={setActiveTab}
      >
        {activeTab === 'details' && (
          <Card>
            <LiveMetrics run={data.run} latest={data.latest} onCancel={() => cancelRun(params.id)} />
          </Card>
        )}
        {activeTab === 'metrics' && (
          <Card>
            <MetricsChart history={data.history} />
          </Card>
        )}
      </Tabs>
    </div>
  );
}
