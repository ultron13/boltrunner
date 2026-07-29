'use client';

import { ReactNode, useEffect, useState } from 'react';
import { usePathname, useSearchParams } from 'next/navigation';
import { listTests, Test } from '@/lib/api-client';
import { useProjects } from '@/components/ui/ProjectProvider';
import { TopNav } from '@/components/ui/TopNav';
import { TreeNav } from '@/components/ui/TreeNav';
import { BottomTabBar } from '@/components/ui/BottomTabBar';
import { Breadcrumb, BreadcrumbItem } from '@/components/ui/Breadcrumb';

const MODULES = [
  { label: 'Dashboard', href: '/' },
  { label: 'Test Management', href: '/' },
  { label: 'Test Runs', href: '/history' },
  { label: 'Admin', href: '/admin' },
];

function breadcrumbFor(
  pathname: string,
  testId: string | null,
  testName: string | undefined,
  projectName: string
): BreadcrumbItem[] {
  const root: BreadcrumbItem = { label: projectName, href: '/' };
  if (pathname === '/') return [root];
  if (pathname === '/admin') return [root, { label: 'Admin' }];
  // The exact '/tests' match must stay above the '/tests/' prefix match.
  if (pathname === '/tests') return [root, { label: 'Tests' }];
  if (pathname.startsWith('/tests/')) {
    const id = pathname.split('/')[2];
    return [root, { label: 'Tests', href: '/tests' }, { label: testName ?? id }];
  }
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
  const { selectedId, selected } = useProjects();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const testId = searchParams.get('testId');
  // A projects-endpoint failure leaves nothing selected, which degrades to the
  // literal "Default" rather than an empty label.
  const projectName = selected?.name ?? 'Default';

  useEffect(() => {
    listTests(selectedId ?? undefined)
      .then(setTests)
      .catch(() => setTests([]));
  }, [selectedId]);

  const detailTestId = pathname.startsWith('/tests/') ? pathname.split('/')[2] : null;
  const activeTestId = testId ?? detailTestId;
  const activeTest = tests.find((t) => t.id === activeTestId);
  const crumbs = breadcrumbFor(pathname, testId, activeTest?.name, projectName);

  return (
    <div className="min-h-screen flex flex-col bg-surface-alt text-text">
      <TopNav modules={MODULES} />
      <div className="flex flex-1">
        <div className="hidden md:block">
          <TreeNav tests={tests} activeTestId={activeTestId ?? undefined} projectName={projectName} />
        </div>
        <div className="flex-1 flex flex-col">
          <Breadcrumb items={crumbs} />
          <main className="flex-1 p-6 pb-20 md:pb-6">{children}</main>
        </div>
      </div>
      <BottomTabBar />
    </div>
  );
}
