import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { TestList } from '@/components/TestList';

const tests = [
  { id: '1', name: 'Checkout Load', target_url: 'http://x', virtual_users: 5, duration_seconds: 30, created_at: '2026-07-24T00:00:00Z' },
];

describe('TestList', () => {
  it('shows the empty message when there are no tests', () => {
    render(<TestList tests={[]} onStart={() => {}} />);
    expect(screen.getByText('No tests yet — create one above.')).toBeInTheDocument();
  });

  it('renders a row per test with a Run button', () => {
    render(<TestList tests={tests} onStart={() => {}} />);
    expect(screen.getByRole('row', { name: /Checkout Load/i })).toBeInTheDocument();
    expect(within(screen.getByRole('table')).getByRole('button', { name: /run/i })).toBeInTheDocument();
  });

  it('calls onStart with the test id when Run is clicked', () => {
    const onStart = vi.fn();
    render(<TestList tests={tests} onStart={onStart} />);
    fireEvent.click(within(screen.getByRole('table')).getByRole('button', { name: /run/i }));
    expect(onStart).toHaveBeenCalledWith('1');
  });
});
