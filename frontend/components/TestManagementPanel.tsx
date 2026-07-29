'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { listTests, startRun, Test } from '@/lib/api-client';
import { CreateTestForm } from '@/components/CreateTestForm';
import { TestList } from '@/components/TestList';
import { useProjects } from '@/components/ui/ProjectProvider';

export function TestManagementPanel({ onTestCreated }: { onTestCreated?: (test: Test) => void } = {}) {
  const [tests, setTests] = useState<Test[]>([]);
  const { selectedId } = useProjects();
  const router = useRouter();

  // This panel owns the visible test table, so it has to follow the selection
  // itself -- Shell's list feeds the tree nav, not this one.
  //
  // selectedId starts null and resolves shortly after, so this fires twice on a
  // cold load: once unscoped over every test in the database, once scoped. The
  // unscoped response can land second and repaint the table with another
  // workspace's tests, and nothing refetches to correct it.
  useEffect(() => {
    let current = true;
    listTests(selectedId ?? undefined)
      .then((fetched) => {
        if (current) setTests(fetched);
      })
      .catch(() => {
        if (current) setTests([]);
      });
    return () => {
      current = false;
    };
  }, [selectedId]);

  async function handleStart(testId: string) {
    const run = await startRun(testId);
    router.push(`/runs/${run.id}`);
  }

  function handleCreated(t: Test) {
    setTests((prev) => [t, ...prev]);
    onTestCreated?.(t);
  }

  return (
    <div className="flex flex-col gap-6">
      <CreateTestForm onCreated={handleCreated} />
      <TestList tests={tests} onStart={handleStart} />
    </div>
  );
}
