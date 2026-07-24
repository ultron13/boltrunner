import { ReactNode } from 'react';

export type Column<T> = {
  key: string;
  header: string;
  align?: 'numeric';
  render?: (row: T) => ReactNode;
};

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  onRowClick,
  emptyMessage = 'No data.',
}: {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  onRowClick?: (row: T) => void;
  emptyMessage?: string;
}) {
  if (rows.length === 0) {
    return <p className="text-text-muted p-4">{emptyMessage}</p>;
  }
  return (
    <table className="w-full text-sm border-collapse">
      <thead className="sticky top-0 bg-surface-alt">
        <tr>
          {columns.map((col) => (
            <th
              key={col.key}
              className={`text-left px-3 py-2 border-b border-border ${
                col.align === 'numeric' ? 'text-right font-mono' : ''
              }`}
            >
              {col.header}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, i) => (
          <tr
            key={rowKey(row)}
            onClick={onRowClick ? () => onRowClick(row) : undefined}
            className={`${i % 2 === 1 ? 'bg-surface-alt' : 'bg-surface'} ${
              onRowClick ? 'cursor-pointer hover:bg-surface-alt' : ''
            }`}
          >
            {columns.map((col) => (
              <td
                key={col.key}
                className={`px-3 py-2 border-b border-border ${
                  col.align === 'numeric' ? 'text-right font-mono' : ''
                }`}
              >
                {col.render ? col.render(row) : String((row as Record<string, unknown>)[col.key] ?? '')}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
