import { ReactNode } from 'react';

export function Card({ children }: { children: ReactNode }) {
  return <div className="border border-border rounded bg-surface p-4">{children}</div>;
}
