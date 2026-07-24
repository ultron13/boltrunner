'use client';

import { useEffect, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import Link from 'next/link';
import { listTests, listRunsForTest, Run, Test } from '@/lib/api-client';
import { DataTable, Column } from '@/components/ui/DataTable';
import { StatusBadge } from '@/components/ui/StatusBadge';

type HistoryRow = Run & { testName: string };

export default function HistoryPage() {
  const [rows, setRows] = useState<HistoryRow[]>([]);
  const [loaded, setLoaded] = useState(false);
  const router = useRouter();
  const searchParams = useSearchParams();
  const testId = searchParams.get('testId');

  useEffect(() => {
    async function load() {
      const tests: Test[] = await listTests();
      const filtered = testId ? tests.filter((t) => t.id === testId) : tests;
      const settled = await Promise.allSettled(
        filtered.map(async (t) => {
          const runs = await listRunsForTest(t.id);
          return runs.map((r) => ({ ...r, testName: t.name }));
        })
      );
      const perTest = settled
        .filter((s): s is PromiseFulfilledResult<HistoryRow[]> => s.status === 'fulfilled')
        .map((s) => s.value);
      const merged = perTest
        .flat()
        .sort((a, b) => (a.created_at && b.created_at ? b.created_at.localeCompare(a.created_at) : 0));
      setRows(merged);
      setLoaded(true);
    }
    load().catch(() => setLoaded(true));
  }, [testId]);

  const columns: Column<HistoryRow>[] = [
    { key: 'testName', header: 'Test' },
    { key: 'id', header: 'Run', render: (r) => (
        <Link href={`/runs/${r.id}`} className="text-accent hover:underline" onClick={(e) => e.stopPropagation()}>
          {r.id}
        </Link>
      ) },
    { key: 'status', header: 'Status', render: (r) => <StatusBadge status={r.status} /> },
    { key: 'started_at', header: 'Started At' },
  ];

  if (!loaded) return <p>Loading…</p>;

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold text-text">Test Runs</h1>
      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(r) => r.id}
        onRowClick={(r) => router.push(`/runs/${r.id}`)}
        emptyMessage="No runs yet."
      />
    </div>
  );
}
