import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StatusBadge } from '@/components/ui/StatusBadge';
import type { RunStatus } from '@/lib/api-client';

describe('StatusBadge', () => {
  const cases: [RunStatus, string][] = [
    ['pending', 'PENDING'],
    ['running', 'RUNNING'],
    ['completed', 'COMPLETED'],
    ['failed', 'FAILED'],
    ['stopped', 'STOPPED'],
  ];

  it.each(cases)('renders %s as %s', (status, label) => {
    render(<StatusBadge status={status} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });
});
