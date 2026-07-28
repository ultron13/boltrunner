import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { VersionHistoryTable } from '@/components/VersionHistoryTable';
import type { TestVersion } from '@/lib/api-client';

// created_at is deliberately identical on both rows: the backend returns the
// family's creation time on every version, so a table that rendered created_at
// would show the same timestamp for every row.
const versions: TestVersion[] = [
  {
    id: 't1', version_id: 'v2', version: 2, project_id: 'p1', name: 'Checkout Load',
    target_url: 'http://b', virtual_users: 9, duration_seconds: 30,
    created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-25T12:00:00Z',
  },
  {
    id: 't1', version_id: 'v1', version: 1, project_id: 'p1', name: 'Checkout Load',
    target_url: 'http://a', virtual_users: 5, duration_seconds: 30,
    created_at: '2026-07-24T00:00:00Z', updated_at: '2026-07-24T00:00:00Z',
  },
];

describe('VersionHistoryTable', () => {
  it('renders a row per version labelled v{n}', () => {
    render(<VersionHistoryTable versions={versions} />);
    expect(screen.getByRole('row', { name: /v2/ })).toBeInTheDocument();
    expect(screen.getByRole('row', { name: /v1/ })).toBeInTheDocument();
  });

  it('shows each version its own edited timestamp, not the shared family created_at', () => {
    render(<VersionHistoryTable versions={versions} />);
    const v2 = screen.getByRole('row', { name: /v2/ });
    expect(within(v2).getByText(/2026-07-25/)).toBeInTheDocument();
    expect(within(v2).queryByText('2026-07-24T00:00:00Z')).not.toBeInTheDocument();
  });

  it('shows the per-version configuration', () => {
    render(<VersionHistoryTable versions={versions} />);
    expect(within(screen.getByRole('row', { name: /v2/ })).getByText('http://b')).toBeInTheDocument();
    expect(within(screen.getByRole('row', { name: /v1/ })).getByText('http://a')).toBeInTheDocument();
  });

  it('offers no actions — history is read-only', () => {
    render(<VersionHistoryTable versions={versions} />);
    expect(within(screen.getByRole('table')).queryByRole('button')).not.toBeInTheDocument();
  });
});
