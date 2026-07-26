'use client';

import { TestManagementPanel } from '@/components/TestManagementPanel';

export default function TestsPage() {
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-text">Tests</h1>
      <TestManagementPanel />
    </div>
  );
}
