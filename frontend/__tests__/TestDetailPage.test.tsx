import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import TestDetailPage from '@/app/tests/[id]/page';
import * as api from '@/lib/api-client';

vi.mock('next/navigation', () => ({
  useParams: () => ({ id: 't1' }),
  useRouter: () => ({ push: vi.fn() }),
}));

// TestDetailPanel now reads useProjects() for the "move to project" control.
// A real ProjectProvider would pull this test's render in on listProjects()
// too, which is not what this test is about; stub it the same way
// TestDetailPanel.test.tsx does.
vi.mock('@/components/ui/ProjectProvider', () => ({
  useProjects: () => ({
    projects: [{ id: 'p1', name: 'Default', created_at: 'x', is_default: true }],
    selectedId: 'p1',
    selected: { id: 'p1', name: 'Default', created_at: 'x', is_default: true },
    select: vi.fn(),
    create: vi.fn(),
    rename: vi.fn(),
    remove: vi.fn(),
  }),
}));

describe('TestDetailPage', () => {
  beforeEach(() => vi.restoreAllMocks());

  it('renders the detail panel for the route param', async () => {
    const listTestVersions = vi.spyOn(api, 'listTestVersions').mockResolvedValue([]);
    render(<TestDetailPage />);
    expect(listTestVersions).toHaveBeenCalledWith('t1');
    expect(await screen.findByRole('heading', { level: 1 })).toBeInTheDocument();
  });
});
