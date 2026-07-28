'use client';

import { TestVersion } from '@/lib/api-client';
import { DataTable, Column } from '@/components/ui/DataTable';

export function VersionHistoryTable({ versions }: { versions: TestVersion[] }) {
  const columns: Column<TestVersion>[] = [
    // Version is deliberately first: DataTable's mobile card mode uses
    // columns[0] as the card title.
    { key: 'version', header: 'Version', render: (v) => `v${v.version}` },
    { key: 'target_url', header: 'Target URL' },
    { key: 'virtual_users', header: 'Virtual users', align: 'numeric' },
    { key: 'duration_seconds', header: 'Duration (s)', align: 'numeric' },
    // updated_at, not created_at: the backend returns the family's creation
    // time on every version row, so created_at is identical across all of them.
    { key: 'updated_at', header: 'Edited' },
  ];

  return <DataTable columns={columns} rows={versions} rowKey={(v) => v.version_id} />;
}
