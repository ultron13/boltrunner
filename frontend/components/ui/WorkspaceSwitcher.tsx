'use client';

import { KeyboardEvent, useEffect, useRef, useState } from 'react';

export function WorkspaceSwitcher({ projectName = 'Default' }: { projectName?: string } = {}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

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

  function close() {
    setOpen(false);
    triggerRef.current?.focus();
  }

  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Escape') {
      close();
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
        {projectName} <span aria-hidden="true">▾</span>
      </button>
      {open && (
        <div
          role="menu"
          aria-label="Workspaces"
          className="absolute left-0 mt-1 w-40 rounded border border-border bg-surface text-text shadow-lg z-10"
        >
          <button
            type="button"
            role="menuitemradio"
            aria-checked="true"
            onClick={close}
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-surface-alt"
          >
            <span aria-hidden="true">✓</span> {projectName}
          </button>
          <button
            type="button"
            disabled
            aria-disabled="true"
            className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-text-muted cursor-not-allowed"
          >
            + New project
          </button>
        </div>
      )}
    </div>
  );
}
