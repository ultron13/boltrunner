import { RunStatus } from '@/lib/api-client';

const VARIANT: Record<RunStatus, { label: string; bg: string; fg: string }> = {
  pending: { label: 'PENDING', bg: 'bg-status-info-bg', fg: 'text-status-info-fg' },
  running: { label: 'RUNNING', bg: 'bg-status-info-bg', fg: 'text-status-info-fg' },
  completed: { label: 'COMPLETED', bg: 'bg-status-pass-bg', fg: 'text-status-pass-fg' },
  failed: { label: 'FAILED', bg: 'bg-status-fail-bg', fg: 'text-status-fail-fg' },
  stopped: { label: 'STOPPED', bg: 'bg-status-warn-bg', fg: 'text-status-warn-fg' },
};

export function StatusBadge({ status }: { status: RunStatus }) {
  const v = VARIANT[status];
  return (
    <span className={`inline-block text-xs px-2 py-0.5 rounded border border-border ${v.bg} ${v.fg}`}>
      {v.label}
    </span>
  );
}
