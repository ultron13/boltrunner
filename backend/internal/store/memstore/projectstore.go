package memstore

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/boltrunner/backend/internal/model"
)

// DefaultProjectID is the id of the seeded "Default" project. memstore has no
// migrations to seed it with a generated UUID, so both stores agree on a fixed
// well-known value instead -- that keeps NewTestStore() argument-free while
// still letting CreateTest fill in a project.
const (
	DefaultProjectID   = "00000000-0000-0000-0000-000000000001"
	DefaultProjectName = "Default"
)

type ProjectStore struct {
	mu       sync.RWMutex
	projects map[string]model.Project
}

func NewProjectStore() *ProjectStore {
	return &ProjectStore{projects: map[string]model.Project{
		DefaultProjectID: {ID: DefaultProjectID, Name: DefaultProjectName, CreatedAt: time.Now().UTC()},
	}}
}

func (s *ProjectStore) ListProjects(ctx context.Context) ([]model.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
