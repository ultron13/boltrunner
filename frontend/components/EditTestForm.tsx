'use client';

import { useEffect, useRef, useState, FormEvent } from 'react';
import { TestVersion, UpdateTestInput } from '@/lib/api-client';
import { TestFields, TestField } from '@/components/TestFields';

export function EditTestForm({
  current,
  onSave,
  error,
}: {
  current: TestVersion;
  onSave: (input: UpdateTestInput) => Promise<void>;
  error?: string | null;
}) {
  const [name, setName] = useState(current.name);
  const [targetUrl, setTargetUrl] = useState(current.target_url);
  const [virtualUsers, setVirtualUsers] = useState(String(current.virtual_users));
  const [durationSeconds, setDurationSeconds] = useState(String(current.duration_seconds));
  const [saving, setSaving] = useState(false);

  // Re-seed only when a genuinely different version becomes current (i.e. a
  // save landed). Keying on version_id rather than the object identity is what
  // lets the parent reload the version list after a 409 without wiping what the
  // user typed.
  //
  // The mount run is skipped deliberately. useState above has already seeded
  // these exact values, so seeding again on mount writes nothing new — but it
  // is not harmless: the parent mounts this form from a resolved fetch, so
  // React commits the inputs and defers this passive effect to a later task.
  // A keystroke landing in that window would be reverted to the seed.
  const seeded = useRef(false);
  useEffect(() => {
    if (!seeded.current) {
      seeded.current = true;
      return;
    }
    setName(current.name);
    setTargetUrl(current.target_url);
    setVirtualUsers(String(current.virtual_users));
    setDurationSeconds(String(current.duration_seconds));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current.version_id]);

  const setters: Record<TestField, (v: string) => void> = {
    name: setName,
    targetUrl: setTargetUrl,
    virtualUsers: setVirtualUsers,
    durationSeconds: setDurationSeconds,
  };

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      await onSave({
        name,
        target_url: targetUrl,
        virtual_users: Number(virtualUsers),
        duration_seconds: Number(durationSeconds),
      });
    } finally {
      setSaving(false);
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
      <button type="submit" disabled={saving}>
        {saving ? 'Saving…' : 'Save as new version'}
      </button>
    </form>
  );
}
