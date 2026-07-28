'use client';

export type TestField = 'name' | 'targetUrl' | 'virtualUsers' | 'durationSeconds';

// The numeric values are strings, not numbers: that keeps the inputs controlled
// while a user is mid-typing, where an empty string is a legal intermediate
// state that Number('') would silently turn into 0.
export function TestFields({
  name,
  targetUrl,
  virtualUsers,
  durationSeconds,
  onChange,
}: {
  name: string;
  targetUrl: string;
  virtualUsers: string;
  durationSeconds: string;
  onChange: (field: TestField, value: string) => void;
}) {
  return (
    <>
      <label className="flex flex-col gap-1">
        <span>Name</span>
        <input value={name} onChange={(e) => onChange('name', e.target.value)} required />
      </label>
      <label className="flex flex-col gap-1">
        <span>Target URL</span>
        <input value={targetUrl} onChange={(e) => onChange('targetUrl', e.target.value)} required type="url" />
      </label>
      <label className="flex flex-col gap-1">
        <span>Virtual users</span>
        <input
          value={virtualUsers}
          onChange={(e) => onChange('virtualUsers', e.target.value)}
          required
          type="number"
          min={1}
        />
      </label>
      <label className="flex flex-col gap-1">
        <span>Duration (seconds)</span>
        <input
          value={durationSeconds}
          onChange={(e) => onChange('durationSeconds', e.target.value)}
          required
          type="number"
          min={1}
        />
      </label>
    </>
  );
}
