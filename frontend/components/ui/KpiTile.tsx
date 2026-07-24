export function KpiTile({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="border border-border rounded bg-surface px-4 py-3 flex flex-col gap-1">
      <span className="text-xs uppercase text-text-muted">{label}</span>
      <span className="text-2xl font-semibold text-text">{value}</span>
    </div>
  );
}
