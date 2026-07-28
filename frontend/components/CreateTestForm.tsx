'use client';

import { useState, FormEvent } from 'react';
import { createTest, Test } from '@/lib/api-client';
import { TestFields, TestField } from '@/components/TestFields';

export function CreateTestForm({ onCreated }: { onCreated: (test: Test) => void }) {
  const [name, setName] = useState('');
  const [targetUrl, setTargetUrl] = useState('');
  const [virtualUsers, setVirtualUsers] = useState('10');
  const [durationSeconds, setDurationSeconds] = useState('30');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const setters: Record<TestField, (v: string) => void> = {
    name: setName,
    targetUrl: setTargetUrl,
    virtualUsers: setVirtualUsers,
    durationSeconds: setDurationSeconds,
  };

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
      <TestFields
        name={name}
        targetUrl={targetUrl}
        virtualUsers={virtualUsers}
        durationSeconds={durationSeconds}
        onChange={(field, value) => setters[field](value)}
      />
      {error && <p className="text-red-600">{error}</p>}
      <button type="submit" disabled={submitting}>
        {submitting ? 'Creating…' : 'Create test'}
      </button>
    </form>
  );
}
