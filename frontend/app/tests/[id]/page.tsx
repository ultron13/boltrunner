'use client';

import { useParams } from 'next/navigation';
import { TestDetailPanel } from '@/components/TestDetailPanel';

export default function TestDetailPage() {
  const params = useParams<{ id: string }>();
  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-text">Test</h1>
      <TestDetailPanel testId={params.id} />
    </div>
  );
}
