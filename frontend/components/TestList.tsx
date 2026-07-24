'use client';

import { Test } from '@/lib/api-client';

export function TestList({ tests, onStart }: { tests: Test[]; onStart: (testId: string) => void }) {
  if (tests.length === 0) {
    return <p>No tests yet — create one above.</p>;
  }
  return (
    <table>
      <thead>
        <tr>
          <th>Name</th>
          <th>Target URL</th>
          <th>Virtual users</th>
          <th>Duration (s)</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {tests.map((t) => (
          <tr key={t.id}>
            <td>{t.name}</td>
            <td>{t.target_url}</td>
            <td>{t.virtual_users}</td>
            <td>{t.duration_seconds}</td>
            <td>
              <button onClick={() => onStart(t.id)}>Run</button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
