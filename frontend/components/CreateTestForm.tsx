'use client';

import { useState, FormEvent } from 'react';
import { createTest, Test } from '@/lib/api-client';

export function CreateTestForm({ onCreated }: { onCreated: (test: Test) => void }) {
  const [name, setName] = useState('');
  const [targetUrl, setTargetUrl] = useState('');
  const [virtualUsers, setVirtualUsers] = useState('10');
  const [durationSeconds, setDurationSeconds] = useState('30');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const test = await createTest({
        name,
        target_url: targetUrl,
        virtual_users: Number(virtualUsers),
        duration_seconds: Number(durationSeconds),
      });
      onCreated(test);
      setName('');
      setTargetUrl('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'failed to create test');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-3 max-w-md">
      <label className="flex flex-col gap-1">
        <span>Name</span>
        <input value={name} onChange={(e) => setName(e.target.value)} required />
      </label>
      <label className="flex flex-col gap-1">
        <span>Target URL</span>
        <input value={targetUrl} onChange={(e) => setTargetUrl(e.target.value)} required type="url" />
      </label>
      <label className="flex flex-col gap-1">
        <span>Virtual users</span>
        <input value={virtualUsers} onChange={(e) => setVirtualUsers(e.target.value)} required type="number" min={1} />
      </label>
      <label className="flex flex-col gap-1">
        <span>Duration (seconds)</span>
        <input value={durationSeconds} onChange={(e) => setDurationSeconds(e.target.value)} required type="number" min={1} />
      </label>
      {error && <p className="text-red-600">{error}</p>}
      <button type="submit" disabled={submitting}>
        {submitting ? 'Creating…' : 'Create test'}
      </button>
    </form>
  );
}
