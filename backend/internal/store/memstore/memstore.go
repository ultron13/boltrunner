package memstore

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

type TestStore struct {
	mu    sync.RWMutex
	tests map[string]model.Test
}

func NewTestStore() *TestStore {
	return &TestStore{tests: make(map[string]model.Test)}
}

func (s *TestStore) CreateTest(ctx context.Context, t *model.Test) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now().UTC()
	s.tests[t.ID] = *t
	return nil
}

func (s *TestStore) ListTests(ctx context.Context) ([]model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Test, 0, len(s.tests))
	for _, t := range s.tests {
		out = append(out, t)
	}
	return out, nil
}

func (s *TestStore) GetTest(ctx context.Context, id string) (*model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tests[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &t, nil
}
