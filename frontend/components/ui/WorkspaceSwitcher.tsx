'use client';

import { FormEvent, KeyboardEvent, useEffect, useRef, useState } from 'react';
import { useProjects } from '@/components/ui/ProjectProvider';

export function WorkspaceSwitcher() {
  const { projects, selectedId, selected, select, create } = useProjects();
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;

    function handleMouseDown(e: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleMouseDown);
    return () => document.removeEventListener('mousedown', handleMouseDown);
  }, [open]);

  useEffect(() => {
    if (creating) inputRef.current?.focus();
  }, [creating]);

  function resetCreate() {
    setCreating(false);
    setName('');
    setError(null);
  }

  function close() {
    setOpen(false);
    resetCreate();
    triggerRef.current?.focus();
  }

  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key !== 'Escape') return;
    // Escape backs out of the inline input first, so abandoning a create does
    // not also dismiss the menu the user is still working in.
    if (creating) {
      resetCreate();
      return;
    }
    close();
  }

  async function handleCreate(e: FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    setSaving(true);
    setError(null);
    try {
      await create(trimmed);
      close();
    } catch (err) {
      // The typed name stays in the input: a rejected name is usually one
      // character away from an accepted one.
      setError(err instanceof Error ? err.message : "Couldn't create project");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div ref={rootRef} className="relative" onKeyDown={handleKeyDown}>
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1 text-chrome-fg px-2 py-1 rounded hover:bg-white/10"
      >
        {selected?.name ?? 'Default'} <span aria-hidden="true">▾</span>
      </button>
      {open && (
        <div
          role="menu"
          aria-label="Workspaces"
          className="absolute left-0 mt-1 w-56 rounded border border-border bg-surface text-text shadow-lg z-10"
        >
          {projects.map((p) => (
            <button
              key={p.id}
              type="button"
              role="menuitemradio"
              aria-checked={p.id === selectedId}
              onClick={() => {
                select(p.id);
                close();
              }}
              className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-surface-alt"
            >
              <span aria-hidden="true">{p.id === selectedId ? '✓' : ' '}</span> {p.name}
            </button>
          ))}
          {creating ? (
            <form onSubmit={handleCreate} className="border-t border-border p-2">
              <input
                ref={inputRef}
                aria-label="Project name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={saving}
                placeholder="New project name"
                className="w-full rounded border border-border bg-surface px-2 py-1 text-sm"
              />
              {error && <p className="mt-1 text-xs text-red-600">{error}</p>}
            </form>
          ) : (
            <button
              type="button"
              onClick={() => setCreating(true)}
              className="flex w-full items-center gap-2 border-t border-border px-3 py-2 text-left text-sm hover:bg-surface-alt"
            >
              + New project
            </button>
          )}
        </div>
      )}
    </div>
  );
}
