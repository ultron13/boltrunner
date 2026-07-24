import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Card } from '@/components/ui/Card';
import { Tabs } from '@/components/ui/Tabs';

describe('Card', () => {
  it('renders its children', () => {
    render(<Card>hello</Card>);
    expect(screen.getByText('hello')).toBeInTheDocument();
  });
});

describe('Tabs', () => {
  const tabs = [
    { id: 'a', label: 'A' },
    { id: 'b', label: 'B' },
  ];

  it('marks the active tab as selected', () => {
    render(
      <Tabs tabs={tabs} activeId="a" onChange={() => {}}>
        content
      </Tabs>
    );
    expect(screen.getByRole('tab', { name: 'A' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: 'B' })).toHaveAttribute('aria-selected', 'false');
  });

  it('calls onChange with the clicked tab id', () => {
    const onChange = vi.fn();
    render(
      <Tabs tabs={tabs} activeId="a" onChange={onChange}>
        content
      </Tabs>
    );
    fireEvent.click(screen.getByRole('tab', { name: 'B' }));
    expect(onChange).toHaveBeenCalledWith('b');
  });

  it('renders the children content', () => {
    render(
      <Tabs tabs={tabs} activeId="a" onChange={() => {}}>
        panel content
      </Tabs>
    );
    expect(screen.getByText('panel content')).toBeInTheDocument();
  });
});
