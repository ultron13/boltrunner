'use client';

import { createContext, useCallback, useContext, useEffect, useState, ReactNode } from 'react';
import { listProjects, createProject, Project } from '@/lib/api-client';

const STORAGE_KEY = 'boltrunner-project';

type ProjectContextValue = {
  projects: Project[];
  selectedId: string | null;
  selected: Project | null;
  select: (id: string) => void;
  create: (name: string) => Promise<Project>;
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

  const selected = projects.find((p) => p.id === selectedId) ?? null;

  return (
    <ProjectContext.Provider value={{ projects, selectedId, selected, select, create }}>
      {children}
    </ProjectContext.Provider>
  );
}

export function useProjects(): ProjectContextValue {
  const ctx = useContext(ProjectContext);
  if (!ctx) throw new Error('useProjects must be used within a ProjectProvider');
  return ctx;
}
