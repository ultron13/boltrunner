'use client';

import { ReactNode, useEffect, useState } from 'react';
import { usePathname, useSearchParams } from 'next/navigation';
import { listTests, Test } from '@/lib/api-client';
import { TopNav } from '@/components/ui/TopNav';
import { TreeNav } from '@/components/ui/TreeNav';
import { Breadcrumb, BreadcrumbItem } from '@/components/ui/Breadcrumb';

const MODULES = [
  { label: 'Dashboard', href: '/' },
  { label: 'Test Management', href: '/' },
  { label: 'Test Runs', href: '/history' },
  { label: 'Admin', href: '/admin' },
];

function breadcrumbFor(pathname: string, testId: string | null, testName?: string): BreadcrumbItem[] {
  const root: BreadcrumbItem = { label: 'Default', href: '/' };
  if (pathname === '/') return [root];
  if (pathname === '/admin') return [root, { label: 'Admin' }];
  if (pathname === '/history') {
    return testId
      ? [root, { label: 'Test Runs', href: '/history' }, { label: testName ?? testId }]
      : [root, { label: 'Test Runs' }];
  }
  if (pathname.startsWith('/runs/')) {
    const runId = pathname.split('/')[2];
    return [root, { label: `Run ${runId}` }];
  }
  return [root];
}

export function Shell({ children }: { children: ReactNode }) {
  const [tests, setTests] = useState<Test[]>([]);
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const testId = searchParams.get('testId');

  useEffect(() => {
    listTests().then(setTests).catch(() => setTests([]));
  }, []);

  const activeTest = tests.find((t) => t.id === testId);
  const crumbs = breadcrumbFor(pathname, testId, activeTest?.name);

  return (
    <div className="min-h-screen flex flex-col bg-surface-alt text-text">
      <TopNav modules={MODULES} />
      <div className="flex flex-1">
        <TreeNav tests={tests} activeTestId={testId ?? undefined} />
        <div className="flex-1 flex flex-col">
          <Breadcrumb items={crumbs} />
          <main className="flex-1 p-6">{children}</main>
        </div>
      </div>
    </div>
  );
}
