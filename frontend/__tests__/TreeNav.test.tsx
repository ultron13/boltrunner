import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { TreeNav } from '@/components/ui/TreeNav';

const tests = [
  { id: '1', name: 'Checkout Load', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
  { id: '2', name: 'Search Spike', target_url: 'http://y', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
];

describe('TreeNav', () => {
  it('renders the Default workspace and every test as a link', () => {
    render(<TreeNav tests={tests} />);
    expect(screen.getByText(/Default/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Checkout Load/i })).toHaveAttribute('href', '/tests/1');
    expect(screen.getByRole('link', { name: /Search Spike/i })).toHaveAttribute('href', '/tests/2');
  });

  it('highlights the active test', () => {
    render(<TreeNav tests={tests} activeTestId="2" />);
    expect(screen.getByRole('link', { name: /Search Spike/i })).toHaveClass('text-accent');
    expect(screen.getByRole('link', { name: /Checkout Load/i })).not.toHaveClass('text-accent');
  });

  it('renders with no tests without crashing', () => {
    render(<TreeNav tests={[]} />);
    expect(screen.getByText(/Default/)).toBeInTheDocument();
  });
});
