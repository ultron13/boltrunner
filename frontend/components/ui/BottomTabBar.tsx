'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';

const TABS = [
  { label: 'Dashboard', href: '/', icon: '🏠' },
  { label: 'Tests', href: '/tests', icon: '📄' },
  { label: 'Runs', href: '/history', icon: '⏱' },
  { label: 'Admin', href: '/admin', icon: '⚙' },
];

export function BottomTabBar() {
  const pathname = usePathname();
  return (
    <nav
      aria-label="Primary"
      className="md:hidden fixed inset-x-0 bottom-0 bg-chrome text-chrome-fg flex text-xs border-t border-border"
    >
      {TABS.map((tab) => {
        const active = pathname === tab.href;
        return (
          <Link
            key={tab.href}
            href={tab.href}
            className={`flex-1 flex flex-col items-center gap-0.5 py-2 ${active ? 'text-accent' : 'text-chrome-fg'}`}
          >
            <span aria-hidden="true">{tab.icon}</span>
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}
