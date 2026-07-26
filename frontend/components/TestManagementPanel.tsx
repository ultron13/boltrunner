'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { listTests, startRun, Test } from '@/lib/api-client';
import { CreateTestForm } from '@/components/CreateTestForm';
import { TestList } from '@/components/TestList';

export function TestManagementPanel() {
  const [tests, setTests] = useState<Test[]>([]);
  const router = useRouter();

  useEffect(() => {
    listTests()
      .then(setTests)
      .catch(() => setTests([]));
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
      <CreateTestForm onCreated={handleCreated} />
      <TestList tests={tests} onStart={handleStart} />
    </div>
  );
}
