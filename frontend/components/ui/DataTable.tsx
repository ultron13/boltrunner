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

  function cellValue(col: Column<T>, row: T): ReactNode {
    return col.render ? col.render(row) : String((row as Record<string, unknown>)[col.key] ?? '');
  }

  const [titleCol, ...restCols] = columns;

  return (
    <>
      <table className="hidden md:table w-full text-sm border-collapse">
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
                  {cellValue(col, row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <ul className="md:hidden flex flex-col gap-2 p-3">
        {rows.map((row) => (
          <li
            key={rowKey(row)}
            onClick={onRowClick ? () => onRowClick(row) : undefined}
            className={`border border-border rounded p-3 bg-surface ${
              onRowClick ? 'cursor-pointer hover:bg-surface-alt' : ''
            }`}
          >
            <div className="font-medium text-text">{cellValue(titleCol, row)}</div>
            {restCols.map((col) => (
              <div key={col.key} className="text-sm text-text-muted">
                {col.header && `${col.header}: `}
                {cellValue(col, row)}
              </div>
            ))}
          </li>
        ))}
      </ul>
    </>
  );
}
