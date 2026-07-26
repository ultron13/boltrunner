import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { BottomTabBar } from '@/components/ui/BottomTabBar';

vi.mock('next/navigation', () => ({
  usePathname: () => '/history',
}));

describe('BottomTabBar', () => {
  it('renders all four tabs with correct hrefs', () => {
    render(<BottomTabBar />);
    expect(screen.getByRole('link', { name: /dashboard/i })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: /tests/i })).toHaveAttribute('href', '/tests');
    expect(screen.getByRole('link', { name: /runs/i })).toHaveAttribute('href', '/history');
    expect(screen.getByRole('link', { name: /admin/i })).toHaveAttribute('href', '/admin');
  });

  it('marks the tab matching the current path as active', () => {
    render(<BottomTabBar />);
    expect(screen.getByRole('link', { name: /runs/i })).toHaveClass('text-accent');
    expect(screen.getByRole('link', { name: /dashboard/i })).not.toHaveClass('text-accent');
  });

  it('is hidden below md and shown at md and up', () => {
    render(<BottomTabBar />);
    expect(screen.getByRole('navigation', { name: 'Primary' })).toHaveClass('md:hidden');
  });
});
