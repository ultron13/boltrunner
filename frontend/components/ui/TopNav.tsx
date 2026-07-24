'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { ThemeToggle } from '@/components/ui/ThemeToggle';

export type NavModule = { label: string; href: string };

export function TopNav({ modules }: { modules: NavModule[] }) {
  const pathname = usePathname();
  return (
    <header className="bg-chrome text-chrome-fg flex items-center justify-between px-4 py-2 text-sm">
      <div className="flex items-center gap-4">
        <span className="font-semibold">BoltRunner</span>
        {modules.map((m) => {
          const active = pathname === m.href;
          return (
            <Link key={m.label} href={m.href} className={`pb-1 ${active ? 'border-b-2 border-accent' : ''}`}>
              {m.label}
            </Link>
          );
        })}
      </div>
      <ThemeToggle />
    </header>
  );
}
