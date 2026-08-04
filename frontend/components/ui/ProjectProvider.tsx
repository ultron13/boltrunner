'use client';

import { createContext, useCallback, useContext, useEffect, useState, ReactNode } from 'react';
import { listProjects, createProject, renameProject, deleteProject, Project } from '@/lib/api-client';

const STORAGE_KEY = 'boltrunner-project';

type ProjectContextValue = {
  projects: Project[];
  selectedId: string | null;
  selected: Project | null;
  select: (id: string) => void;
  create: (name: string) => Promise<Project>;
  rename: (id: string, name: string) => Promise<Project>;
  remove: (id: string) => Promise<void>;
};

const ProjectContext = createContext<ProjectContextValue | null>(null);

export function ProjectProvider({ children }: { children: ReactNode }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    // localStorage is read here rather than in a useState initialiser: the
    // server render has no localStorage, and reading it during render would
    // produce markup that does not match the client's. ThemeProvider does the
    // same for boltrunner-theme.
    listProjects()
      .then((list) => {
        setProjects(list);
        const stored = localStorage.getItem(STORAGE_KEY);
        // A stored id outlives the database that issued it. After a drop or a
        // reseed it names no project, and keeping it would leave the switcher
        // pointing at nothing.
        const resolved = stored && list.some((p) => p.id === stored) ? stored : list[0]?.id ?? null;
        setSelectedId(resolved);
        if (resolved) localStorage.setItem(STORAGE_KEY, resolved);
      })
      .catch(() => {
        setProjects([]);
        setSelectedId(null);
      });
  }, []);

  const select = useCallback((id: string) => {
    setSelectedId(id);
    localStorage.setItem(STORAGE_KEY, id);
  }, []);

  const create = useCallback(async (name: string) => {
    const project = await createProject(name);
    // Sorted by name to match what both stores return from ListProjects, so a
    // reload does not reshuffle the menu.
    setProjects((prev) => [...prev, project].sort((a, b) => a.name.localeCompare(b.name)));
    setSelectedId(project.id);
    localStorage.setItem(STORAGE_KEY, project.id);
    return project;
  }, []);

  const rename = useCallback(async (id: string, name: string) => {
    const updated = await renameProject(id, name);
    // Re-sorted for the same reason create sorts: both stores return
    // ListProjects ordered by name, so an unsorted local list would reshuffle
    // the menu on the next reload.
    setProjects((prev) =>
      prev.map((p) => (p.id === id ? updated : p)).sort((a, b) => a.name.localeCompare(b.name))
    );
    return updated;
  }, []);

  const remove = useCallback(async (id: string) => {
    await deleteProject(id);
    setProjects((prev) => {
      const next = prev.filter((p) => p.id !== id);
      setSelectedId((current) => {
        if (current !== id) return current;
        // The selected project just went away. Falling back to the default
        // keeps the switcher pointing at something real -- the same guard the
        // load path applies to a stored id that outlived its database.
        const fallback = next.find((p) => p.is_default)?.id ?? next[0]?.id ?? null;
        if (fallback) localStorage.setItem(STORAGE_KEY, fallback);
        else localStorage.removeItem(STORAGE_KEY);
        return fallback;
      });
      return next;
    });
  }, []);

  const selected = projects.find((p) => p.id === selectedId) ?? null;

  return (
    <ProjectContext.Provider value={{ projects, selectedId, selected, select, create, rename, remove }}>
      {children}
    </ProjectContext.Provider>
  );
}

export function useProjects(): ProjectContextValue {
  const ctx = useContext(ProjectContext);
  if (!ctx) throw new Error('useProjects must be used within a ProjectProvider');
  return ctx;
}
