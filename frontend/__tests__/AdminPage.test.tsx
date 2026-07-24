import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import AdminPage from '@/app/admin/page';

describe('AdminPage', () => {
  it('renders the API base URL', () => {
    render(<AdminPage />);
    expect(screen.getByText(/API base URL/i)).toBeInTheDocument();
  });
});
