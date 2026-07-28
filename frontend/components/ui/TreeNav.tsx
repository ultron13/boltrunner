import Link from 'next/link';
import { Test } from '@/lib/api-client';

export function TreeNav({
  tests,
  activeTestId,
  projectName = 'Default',
}: {
  tests: Test[];
  activeTestId?: string;
  projectName?: string;
}) {
  return (
    <nav aria-label="Workspace" className="bg-surface-alt border-r border-border w-56 shrink-0 text-sm py-2">
      <div className="px-3 py-1 font-medium text-text">📁 {projectName}</div>
      <ul>
        {tests.map((t) => (
          <li key={t.id}>
            <Link
              href={`/tests/${t.id}`}
              className={`block px-6 py-1 truncate ${
                t.id === activeTestId ? 'bg-accent/10 border-l-2 border-accent text-accent' : 'text-text'
              }`}
            >
              📄 {t.name}
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}
