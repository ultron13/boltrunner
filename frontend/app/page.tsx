'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { listTests, startRun, Test } from '@/lib/api-client';
import { CreateTestForm } from '@/components/CreateTestForm';
import { TestList } from '@/components/TestList';

export default function DashboardPage() {
  const [tests, setTests] = useState<Test[]>([]);
  const router = useRouter();

  useEffect(() => {
    listTests().then(setTests).catch(() => setTests([]));
  }, []);

  async function handleStart(testId: string) {
    const run = await startRun(testId);
    router.push(`/runs/${run.id}`);
  }

  return (
    <main className="p-8 flex flex-col gap-8">
      <h1 className="text-2xl font-semibold">BoltRunner</h1>
      <CreateTestForm onCreated={(t) => setTests((prev) => [t, ...prev])} />
      <TestList tests={tests} onStart={handleStart} />
    </main>
  );
}
