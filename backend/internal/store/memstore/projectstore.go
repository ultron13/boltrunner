package memstore

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

// DefaultProjectID identifies memstore's seeded "Default" project. memstore has
// no migrations, so it uses a fixed id while postgres generates one -- the two
// backends agree on the contract that an empty ProjectID resolves to the
// Default project, NOT on the literal id value, which is never portable
// between them.
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
		DefaultProjectID: {ID: DefaultProjectID, Name: DefaultProjectName, CreatedAt: time.Now().UTC(), IsDefault: true},
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

func (s *ProjectStore) CreateProject(ctx context.Context, p *model.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Postgres enforces this with a UNIQUE constraint. memstore has to check by
	// hand, and must agree: the API tests run against this implementation, so a
	// missing conflict here leaves the 409 path untested where it is exercised.
	for _, existing := range s.projects {
		if existing.Name == p.Name {
			return store.ErrConflict
		}
	}
	p.ID = uuid.NewString()
	p.CreatedAt = time.Now().UTC()
	s.projects[p.ID] = *p
	return nil
}
