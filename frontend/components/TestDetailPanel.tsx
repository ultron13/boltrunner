'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import {
  ApiError,
  listTestVersions,
  moveTest,
  startRun,
  updateTest,
  TestVersion,
  UpdateTestInput,
} from '@/lib/api-client';
import { useProjects } from '@/components/ui/ProjectProvider';
import { EditTestForm } from '@/components/EditTestForm';
import { VersionHistoryTable } from '@/components/VersionHistoryTable';

type LoadState = 'loading' | 'ready' | 'notfound' | 'error';

const CONFLICT_MESSAGE = 'This test was changed elsewhere — review and save again';

export function TestDetailPanel({ testId }: { testId: string }) {
  const [versions, setVersions] = useState<TestVersion[]>([]);
  const [loadState, setLoadState] = useState<LoadState>('loading');
  const [saveError, setSaveError] = useState<string | null>(null);
  const router = useRouter();
  const { projects } = useProjects();
  const [destination, setDestination] = useState('');
  const [moveError, setMoveError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setVersions(await listTestVersions(testId));
      setLoadState('ready');
    } catch (err) {
      setLoadState(err instanceof ApiError && err.status === 404 ? 'notfound' : 'error');
    }
  }, [testId]);

  useEffect(() => {
    load();
  }, [load]);

  const currentProjectId = versions[0]?.project_id ?? '';
  useEffect(() => {
    setDestination(currentProjectId);
  }, [currentProjectId]);

  async function handleSave(input: UpdateTestInput) {
    setSaveError(null);
    try {
      await updateTest(testId, input);
      await load();
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        setLoadState('notfound');
        return;
      }
      if (err instanceof ApiError && err.status === 409) {
        // Reload the history so it shows what actually landed, but leave the
        // form alone: the user did not cause this conflict and should not lose
        // what they typed. EditTestForm only re-seeds when version_id changes,
        // and no new version was written for this user, so the values survive.
        setSaveError(CONFLICT_MESSAGE);
        await load();
        return;
      }
      setSaveError(err instanceof Error ? err.message : "Couldn't save this test");
    }
  }

  async function handleRun() {
    const run = await startRun(testId);
    router.push(`/runs/${run.id}`);
  }

  async function handleMove() {
    setMoveError(null);
    try {
      await moveTest(testId, destination);
      // Leaving the page is deliberate: the scoped test list remounts and
      // refetches, so the move is visible instead of silently stale.
      router.push('/tests');
    } catch (err) {
      setMoveError(err instanceof Error ? err.message : "Couldn't move this test");
      // The backend commits the move and only then re-reads to build the
      // response; if that re-read fails, this catch runs even though the
      // move already happened. Reload so the panel reflects reality (the new
      // project) instead of the stale pre-move state the form still shows.
      await load();
    }
  }

  if (loadState === 'loading') return <p>Loading…</p>;
  if (loadState === 'notfound') return <p>Test not found.</p>;
  if (loadState === 'error') {
    return (
      <div className="flex flex-col gap-3 items-start">
        <p className="text-red-600">Couldn&apos;t load this test.</p>
        <button type="button" onClick={load}>
          Retry
        </button>
      </div>
    );
  }

  // The backend cannot return an empty list for a test that exists, but a 200
  // with [] would otherwise crash on current.name.
  const current = versions[0];
  if (!current) return <p>Test not found.</p>;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-text">{current.name}</h2>
        <button type="button" onClick={handleRun}>
          Run test
        </button>
      </div>

      <section className="flex flex-col gap-2">
        <h3 className="font-medium text-text">Configuration</h3>
        <EditTestForm current={current} onSave={handleSave} error={saveError} />
      </section>

      <section className="flex flex-col gap-2">
        <h3 className="font-medium text-text">Project</h3>
        <div className="flex items-center gap-2">
          <select
            aria-label="Move to project"
            value={destination}
            onChange={(e) => setDestination(e.target.value)}
            className="rounded border border-border bg-surface px-2 py-1 text-sm"
          >
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <button type="button" onClick={handleMove} disabled={!destination || destination === currentProjectId}>
            Move
          </button>
        </div>
        {moveError && (
          <p role="alert" className="text-sm text-red-600">
            {moveError}
          </p>
        )}
      </section>

      <section className="flex flex-col gap-2">
        <h3 className="font-medium text-text">Version history</h3>
        <VersionHistoryTable versions={versions} />
      </section>

      <Link href={`/history?testId=${testId}`} className="text-accent hover:underline">
        Run history →
      </Link>
    </div>
  );
}
