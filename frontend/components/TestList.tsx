'use client';

import { Test } from '@/lib/api-client';
import { DataTable, Column } from '@/components/ui/DataTable';

export function TestList({ tests, onStart }: { tests: Test[]; onStart: (testId: string) => void }) {
  const columns: Column<Test>[] = [
    { key: 'name', header: 'Name' },
    { key: 'target_url', header: 'Target URL' },
    { key: 'virtual_users', header: 'Virtual users', align: 'numeric' },
    { key: 'duration_seconds', header: 'Duration (s)', align: 'numeric' },
    {
      key: 'actions',
      header: '',
      render: (t) => <button onClick={() => onStart(t.id)}>Run</button>,
    },
  ];

  return (
    <DataTable columns={columns} rows={tests} rowKey={(t) => t.id} emptyMessage="No tests yet — create one above." />
  );
}
