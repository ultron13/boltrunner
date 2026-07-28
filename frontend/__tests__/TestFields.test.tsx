import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TestFields } from '@/components/TestFields';

function renderFields(onChange = vi.fn()) {
  render(
    <TestFields name="smoke" targetUrl="http://x" virtualUsers="5" durationSeconds="30" onChange={onChange} />
  );
  return onChange;
}

describe('TestFields', () => {
  it('renders all four labelled inputs with their values', () => {
    renderFields();
    expect(screen.getByLabelText(/name/i)).toHaveValue('smoke');
    expect(screen.getByLabelText(/target url/i)).toHaveValue('http://x');
    expect(screen.getByLabelText(/virtual users/i)).toHaveValue(5);
    expect(screen.getByLabelText(/duration/i)).toHaveValue(30);
  });

  it('carries the validation attributes the backend enforces', () => {
    renderFields();
    expect(screen.getByLabelText(/target url/i)).toHaveAttribute('type', 'url');
    expect(screen.getByLabelText(/virtual users/i)).toHaveAttribute('min', '1');
    expect(screen.getByLabelText(/duration/i)).toHaveAttribute('min', '1');
    expect(screen.getByLabelText(/name/i)).toBeRequired();
  });

  it('reports each field change by name', () => {
    const onChange = renderFields();
    fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'renamed' } });
    expect(onChange).toHaveBeenCalledWith('name', 'renamed');

    fireEvent.change(screen.getByLabelText(/virtual users/i), { target: { value: '9' } });
    expect(onChange).toHaveBeenCalledWith('virtualUsers', '9');
  });
});
