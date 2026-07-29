'use client';

import { useEffect, useState } from 'react';
import { listTests, listRunsForTest, Test } from '@/lib/api-client';
import { TestManagementPanel } from '@/components/TestManagementPanel';
import { KpiTile } from '@/components/ui/KpiTile';
import { useProjects } from '@/components/ui/ProjectProvider';

export default function DashboardPage() {
  const [tests, setTests] = useState<Test[]>([]);
  const [activeRuns, setActiveRuns] = useState(0);
  const { selectedId } = useProjects();

  // The KPIs describe the selected workspace, so they follow the selection too.
  useEffect(() => {
    listTests(selectedId ?? undefined)
      .then((fetched) => {
        setTests(fetched);
        Promise.all(fetched.map((t) => listRunsForTest(t.id)))
          .then((runLists) => {
            const running = runLists.flat().filter((r) => r.status === 'running').length;
            setActiveRuns(running);
          })
          .catch(() => {
            setActiveRuns(0);
          });
      })
      .catch(() => {
        setTests([]);
        setActiveRuns(0);
      });
  }, [selectedId]);

  function handleTestCreated(t: Test) {
    setTests((prev) => [t, ...prev]);
  }

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-text">Dashboard</h1>
      <div className="grid grid-cols-2 gap-4 max-w-md">
        <KpiTile label="Total Tests" value={tests.length} />
        <KpiTile label="Active Runs" value={activeRuns} />
      </div>
      <div className="hidden md:block">
        <TestManagementPanel onTestCreated={handleTestCreated} />
      </div>
    </div>
  );
}
