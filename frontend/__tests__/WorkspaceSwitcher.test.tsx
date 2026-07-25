import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { WorkspaceSwitcher } from '@/components/ui/WorkspaceSwitcher';

describe('WorkspaceSwitcher', () => {
  it('renders closed by default', () => {
    render(<WorkspaceSwitcher />);
    expect(screen.getByRole('button', { name: /default/i })).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('opens the menu on click, showing Default checked and New project disabled', () => {
    render(<WorkspaceSwitcher />);
    fireEvent.click(screen.getByRole('button', { name: /default/i }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
    expect(screen.getByRole('menuitemradio', { name: /default/i })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('button', { name: /new project/i })).toBeDisabled();
  });

  it('closes the menu when Default is selected', () => {
    render(<WorkspaceSwitcher />);
    fireEvent.click(screen.getByRole('button', { name: /default/i }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: /default/i }));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('closes on Escape and returns focus to the trigger', () => {
    render(<WorkspaceSwitcher />);
    const trigger = screen.getByRole('button', { name: /default/i });
    fireEvent.click(trigger);
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('returns focus to the trigger when Default is selected', () => {
    render(<WorkspaceSwitcher />);
    const trigger = screen.getByRole('button', { name: /default/i });
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole('menuitemradio', { name: /default/i }));
    expect(trigger).toHaveFocus();
  });

  it('closes when clicking outside', () => {
    render(
      <div>
        <WorkspaceSwitcher />
        <button>outside</button>
      </div>
    );
    fireEvent.click(screen.getByRole('button', { name: /default/i }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByRole('button', { name: 'outside' }));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('does nothing when the disabled New project item is clicked', () => {
    render(<WorkspaceSwitcher />);
    fireEvent.click(screen.getByRole('button', { name: /default/i }));
    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });
});
