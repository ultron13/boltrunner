'use client';

import { ReactNode } from 'react';

export type TabItem = { id: string; label: string };

export function Tabs({
  tabs,
  activeId,
  onChange,
  children,
}: {
  tabs: TabItem[];
  activeId: string;
  onChange: (id: string) => void;
  children: ReactNode;
}) {
  return (
    <div>
      <div className="flex gap-4 border-b border-border mb-3" role="tablist">
        {tabs.map((t) => (
          <button
            key={t.id}
            role="tab"
            aria-selected={t.id === activeId}
            onClick={() => onChange(t.id)}
            className={`pb-2 px-1 text-sm ${
              t.id === activeId ? 'border-b-2 border-accent text-accent' : 'text-text-muted'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div>{children}</div>
    </div>
  );
}
