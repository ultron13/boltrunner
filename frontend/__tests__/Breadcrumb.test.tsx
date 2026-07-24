import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Breadcrumb } from '@/components/ui/Breadcrumb';

describe('Breadcrumb', () => {
  it('renders each item as text', () => {
    render(<Breadcrumb items={[{ label: 'Default', href: '/' }, { label: 'Checkout Load' }]} />);
    expect(screen.getByText('Default')).toBeInTheDocument();
    expect(screen.getByText('Checkout Load')).toBeInTheDocument();
  });

  it('renders items with an href as links', () => {
    render(<Breadcrumb items={[{ label: 'Default', href: '/' }, { label: 'Checkout Load' }]} />);
    expect(screen.getByRole('link', { name: 'Default' })).toHaveAttribute('href', '/');
    expect(screen.queryByRole('link', { name: 'Checkout Load' })).not.toBeInTheDocument();
  });
});
