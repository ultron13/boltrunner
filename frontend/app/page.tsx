'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { listTests, listRunsForTest, startRun, Test } from '@/lib/api-client';
import { CreateTestForm } from '@/components/CreateTestForm';
import { TestList } from '@/components/TestList';
import { KpiTile } from '@/components/ui/KpiTile';

export default function DashboardPage() {
  const [tests, setTests] = useState<Test[]>([]);
  const [activeRuns, setActiveRuns] = useState(0);
  const router = useRouter();

  useEffect(() => {
    listTests()
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
  }, []);

  async function handleStart(testId: string) {
    const run = await startRun(testId);
    router.push(`/runs/${run.id}`);
  }

  function handleCreated(t: Test) {
    setTests((prev) => [t, ...prev]);
  }

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-text">Dashboard</h1>
      <div className="grid grid-cols-2 gap-4 max-w-md">
        <KpiTile label="Total Tests" value={tests.length} />
        <KpiTile label="Active Runs" value={activeRuns} />
      </div>
      <CreateTestForm onCreated={handleCreated} />
      <TestList tests={tests} onStart={handleStart} />
    </div>
  );
}
