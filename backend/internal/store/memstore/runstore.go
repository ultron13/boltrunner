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

type RunStore struct {
	mu        sync.RWMutex
	runs      map[string]model.Run
	snapshots map[string][]model.RunMetricSnapshot
}

func NewRunStore() *RunStore {
	return &RunStore{runs: make(map[string]model.Run), snapshots: make(map[string][]model.RunMetricSnapshot)}
}

func (s *RunStore) CreateRun(ctx context.Context, r *model.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = uuid.NewString()
	r.CreatedAt = time.Now().UTC()
	s.runs[r.ID] = *r
	return nil
}

func (s *RunStore) GetRun(ctx context.Context, id string) (*model.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &r, nil
}

func (s *RunStore) ListByTest(ctx context.Context, testID string) ([]model.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.Run{}
	for _, r := range s.runs {
		if r.TestID == testID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *RunStore) UpdateRunStatus(ctx context.Context, id string, status model.RunStatus, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return store.ErrNotFound
	}
	r.Status = status
	r.ErrorMessage = errMsg
	now := time.Now().UTC()
	switch status {
	case model.RunRunning:
		if r.StartedAt == nil {
			r.StartedAt = &now
		}
	case model.RunCompleted, model.RunFailed, model.RunStopped:
		r.CompletedAt = &now
	}
	s.runs[id] = r
	return nil
}

func (s *RunStore) AppendMetricSnapshot(ctx context.Context, snap *model.RunMetricSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[snap.RunID]; !ok {
		return store.ErrNotFound
	}
	snap.ID = uuid.NewString()
	snap.Ts = time.Now().UTC()
	s.snapshots[snap.RunID] = append(s.snapshots[snap.RunID], *snap)
	return nil
}

func (s *RunStore) LatestSnapshot(ctx context.Context, runID string) (*model.RunMetricSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := s.snapshots[runID]
	if len(list) == 0 {
		return nil, store.ErrNotFound
	}
	latest := list[len(list)-1]
	return &latest, nil
}

func (s *RunStore) ListSnapshots(ctx context.Context, runID string) ([]model.RunMetricSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.RunMetricSnapshot, len(s.snapshots[runID]))
	copy(out, s.snapshots[runID])
	return out, nil
}
