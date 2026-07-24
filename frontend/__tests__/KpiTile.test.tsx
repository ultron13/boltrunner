import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { KpiTile } from '@/components/ui/KpiTile';

describe('KpiTile', () => {
  it('renders the label and value', () => {
    render(<KpiTile label="Total Tests" value={7} />);
    expect(screen.getByText('Total Tests')).toBeInTheDocument();
    expect(screen.getByText('7')).toBeInTheDocument();
  });
});
