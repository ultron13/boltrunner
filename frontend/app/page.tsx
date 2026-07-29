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
  //
  // selectedId starts null and resolves shortly after, so this fires twice on a
  // cold load: once unscoped over every test in the database, once scoped. The
  // unscoped pass is the slower of the two -- it fans out over every test in
  // every project -- so its response can land second and report another
  // workspace's counts. The guard covers the inner run fan-out as well, which
  // resolves later still and would otherwise overwrite Active Runs on its own.
  useEffect(() => {
    let current = true;
    listTests(selectedId ?? undefined)
      .then((fetched) => {
        if (!current) return;
        setTests(fetched);
        Promise.all(fetched.map((t) => listRunsForTest(t.id)))
          .then((runLists) => {
            if (!current) return;
            const running = runLists.flat().filter((r) => r.status === 'running').length;
            setActiveRuns(running);
          })
          .catch(() => {
            if (current) setActiveRuns(0);
          });
      })
      .catch(() => {
        if (!current) return;
        setTests([]);
        setActiveRuns(0);
      });
    return () => {
      current = false;
    };
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
