import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MetricsChart } from '@/components/MetricsChart';

describe('MetricsChart', () => {
  it('shows a waiting message when there is no history yet', () => {
    render(<MetricsChart history={[]} />);
    expect(screen.getByText(/waiting for the first metric snapshot/i)).toBeInTheDocument();
  });

  it('renders a chart once metric snapshots are available', () => {
    const history = [
      {
        id: 's1', run_id: 'r1', ts: '2026-07-24T00:00:01Z', elapsed_seconds: 1,
        throughput_rps: 5, avg_response_time_ms: 100, error_rate_pct: 0, sample_count: 5,
      },
      {
        id: 's2', run_id: 'r1', ts: '2026-07-24T00:00:02Z', elapsed_seconds: 2,
        throughput_rps: 6, avg_response_time_ms: 110, error_rate_pct: 0, sample_count: 6,
      },
    ];
    const { container } = render(<MetricsChart history={history} />);
    expect(screen.queryByText(/waiting for the first metric snapshot/i)).not.toBeInTheDocument();
    expect(container.querySelector('.recharts-responsive-container')).toBeInTheDocument();
  });
});
