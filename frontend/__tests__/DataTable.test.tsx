import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { DataTable, Column } from '@/components/ui/DataTable';

type Row = { id: string; name: string; count: number };

describe('DataTable', () => {
  const columns: Column<Row>[] = [
    { key: 'name', header: 'Name' },
    { key: 'count', header: 'Count', align: 'numeric' },
  ];
  const rows: Row[] = [{ id: '1', name: 'Alpha', count: 3 }];

  it('renders column headers and row cells as a real table', () => {
    render(<DataTable columns={columns} rows={rows} rowKey={(r) => r.id} />);
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeInTheDocument();
    expect(screen.getByRole('row', { name: /Alpha/i })).toBeInTheDocument();
  });

  it('shows the empty message when there are no rows', () => {
    render(<DataTable columns={columns} rows={[]} rowKey={(r) => r.id} emptyMessage="Nothing here" />);
    expect(screen.getByText('Nothing here')).toBeInTheDocument();
  });

  it('calls onRowClick with the row when a row is clicked', () => {
    const onRowClick = vi.fn();
    render(<DataTable columns={columns} rows={rows} rowKey={(r) => r.id} onRowClick={onRowClick} />);
    fireEvent.click(screen.getByRole('row', { name: /Alpha/i }));
    expect(onRowClick).toHaveBeenCalledWith(rows[0]);
  });

  it('uses a custom render function when provided', () => {
    const withRender: Column<Row>[] = [{ key: 'name', header: 'Name', render: (r) => <span>Custom {r.name}</span> }];
    render(<DataTable columns={withRender} rows={rows} rowKey={(r) => r.id} />);
    expect(screen.getByText('Custom Alpha')).toBeInTheDocument();
  });
});
