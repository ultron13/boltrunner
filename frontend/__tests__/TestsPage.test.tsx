import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import TestsPage from '@/app/tests/page';
import * as api from '@/lib/api-client';

// These cases predate project scoping and do not exercise it; stubbing the hook
// keeps them focused on their own component. ProjectProvider's own behavior is
// covered by ProjectProvider.test.tsx, and the wiring by ProjectScoping.test.tsx.
vi.mock('@/components/ui/ProjectProvider', () => ({
  useProjects: () => ({
    projects: [],
    selectedId: null,
    selected: null,
    select: vi.fn(),
    create: vi.fn(),
  }),
}));


vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe('TestsPage', () => {
  it('renders the Tests heading and the test management panel', async () => {
    vi.spyOn(api, 'listTests').mockResolvedValue([]);
    render(<TestsPage />);
    expect(screen.getByRole('heading', { name: 'Tests' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create test/i })).toBeInTheDocument();
    expect(await screen.findByText('No tests yet — create one above.')).toBeInTheDocument();
  });
});
