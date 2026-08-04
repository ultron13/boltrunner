'use client';

import { useState } from 'react';
import { Card } from '@/components/ui/Card';
import { DataTable, Column } from '@/components/ui/DataTable';
import { useProjects } from '@/components/ui/ProjectProvider';
import { Project } from '@/lib/api-client';

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080';

const DEFAULT_PROTECTED = 'the default project cannot be deleted';

export default function AdminPage() {
  const { projects, rename, remove } = useProjects();
  // Row-scoped UI state, keyed by project id so only one row is ever in an
  // edit or confirm state.
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftName, setDraftName] = useState('');
  const [confirmingId, setConfirmingId] = useState<string | null>(null);
  const [error, setError] = useState<{ id: string; message: string } | null>(null);

  function startRename(p: Project) {
    setConfirmingId(null);
    setError(null);
    setEditingId(p.id);
    setDraftName(p.name);
  }

  async function saveRename(p: Project) {
    setError(null);
    try {
      await rename(p.id, draftName.trim());
      setEditingId(null);
    } catch (err) {
      // The row stays in edit state with what was typed: a rejected name is
      // usually one character away from an accepted one.
      setError({ id: p.id, message: err instanceof Error ? err.message : "Couldn't rename this project" });
    }
  }

  async function confirmDelete(p: Project) {
    setError(null);
    try {
      await remove(p.id);
      setConfirmingId(null);
    } catch (err) {
      setError({ id: p.id, message: err instanceof Error ? err.message : "Couldn't delete this project" });
    }
  }

  const columns: Column<Project>[] = [
    {
      key: 'name',
      header: 'Project',
      render: (p) =>
        editingId === p.id ? (
          <span className="flex flex-col gap-1">
            <input
              aria-label="New name"
              value={draftName}
              onChange={(e) => setDraftName(e.target.value)}
              className="rounded border border-border bg-surface px-2 py-1 text-sm"
            />
            {error?.id === p.id && <span className="text-xs text-red-600">{error.message}</span>}
          </span>
        ) : (
          <span className="flex flex-col gap-1">
            <span>{p.name}</span>
            {error?.id === p.id && <span className="text-xs text-red-600">{error.message}</span>}
          </span>
        ),
    },
    { key: 'created_at', header: 'Created' },
    {
      key: 'actions',
      header: 'Actions',
      render: (p) => {
        if (editingId === p.id) {
          return (
            <span className="flex gap-2">
              <button type="button" onClick={() => saveRename(p)}>
                Save
              </button>
              <button type="button" onClick={() => setEditingId(null)}>
                Cancel
              </button>
            </span>
          );
        }
        if (confirmingId === p.id) {
          return (
            <span className="flex items-center gap-2">
              <span className="text-sm">Delete {p.name}?</span>
              <button type="button" onClick={() => confirmDelete(p)}>
                Confirm
              </button>
              <button type="button" onClick={() => setConfirmingId(null)}>
                Cancel
              </button>
            </span>
          );
        }
        return (
          <span className="flex gap-2">
            <button type="button" onClick={() => startRename(p)}>
              Rename {p.name}
            </button>
            <button
              type="button"
              disabled={p.is_default}
              title={p.is_default ? DEFAULT_PROTECTED : undefined}
              onClick={() => {
                setEditingId(null);
                setError(null);
                setConfirmingId(p.id);
              }}
            >
              Delete {p.name}
            </button>
          </span>
        );
      },
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold text-text">Admin</h1>

      <section className="flex flex-col gap-2">
        <h2 className="font-medium text-text">Projects</h2>
        <DataTable columns={columns} rows={projects} rowKey={(p) => p.id} emptyMessage="No projects yet." />
      </section>

      <Card>
        <div className="flex flex-col gap-1">
          <span className="text-xs uppercase text-text-muted">API base URL</span>
          <span className="font-mono text-text">{API_URL}</span>
        </div>
      </Card>
    </div>
  );
}
