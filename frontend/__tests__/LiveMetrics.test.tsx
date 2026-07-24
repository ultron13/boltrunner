import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { LiveMetrics } from '@/components/LiveMetrics';

describe('LiveMetrics', () => {
  it('renders status and latest metrics, and calls onCancel', () => {
    const onCancel = vi.fn();
    render(
      <LiveMetrics
        run={{ id: 'r1', test_id: 't1', status: 'running' }}
        latest={{
          id: 's1', run_id: 'r1', ts: '2026-07-24T00:00:01Z', elapsed_seconds: 1,
          throughput_rps: 12.5, avg_response_time_ms: 210, error_rate_pct: 0, sample_count: 12,
        }}
        onCancel={onCancel}
      />
    );

    expect(screen.getByText(/running/i)).toBeInTheDocument();
    expect(screen.getByText(/12.5/)).toBeInTheDocument();
    expect(screen.getByText(/210/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(onCancel).toHaveBeenCalled();
  });

  it('hides the cancel button once the run is terminal', () => {
    render(<LiveMetrics run={{ id: 'r1', test_id: 't1', status: 'completed' }} onCancel={vi.fn()} />);
    expect(screen.queryByRole('button', { name: /cancel/i })).not.toBeInTheDocument();
  });
});
