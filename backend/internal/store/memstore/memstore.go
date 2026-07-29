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

// TestStore keys every version row by its own VersionID, mirroring the
// postgres table where the primary key is per-version and catalog_id is the
// stable identity.
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
	if t.ProjectID == "" {
		t.ProjectID = DefaultProjectID
	} else if t.ProjectID != DefaultProjectID {
		// memstore has no project registry beyond the seeded Default, so no
		// other id is resolvable here. postgres enforces the same contract via
		// the tests.project_id foreign key, so both backends reject an unknown
		// project rather than one silently storing it.
		return store.ErrInvalidReference
	}
	now := time.Now().UTC()
	t.ID = uuid.NewString()
	t.VersionID = t.ID
	t.Version = 1
	t.CreatedAt = now
	t.UpdatedAt = now
	s.tests[t.VersionID] = *t
	return nil
}

func (s *TestStore) ListTests(ctx context.Context) ([]model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	latest := map[string]model.Test{}
	for _, t := range s.tests {
		if cur, ok := latest[t.ID]; !ok || t.Version > cur.Version {
			latest[t.ID] = t
		}
	}
	out := make([]model.Test, 0, len(latest))
	for _, t := range latest {
		t.CreatedAt = s.familyCreatedAt(t.ID)
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// ListTestsForProject filters ListTests rather than re-deriving the latest
// version per family, so it inherits that collapsing and the sort order.
func (s *TestStore) ListTestsForProject(ctx context.Context, projectID string) ([]model.Test, error) {
	all, err := s.ListTests(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Test, 0, len(all))
	for _, t := range all {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *TestStore) GetTest(ctx context.Context, catalogID string) (*model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.latestLocked(catalogID)
	if !ok {
		return nil, store.ErrNotFound
	}
	t.CreatedAt = s.familyCreatedAt(catalogID)
	return &t, nil
}

func (s *TestStore) UpdateTest(ctx context.Context, t *model.Test) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest, ok := s.latestLocked(t.ID)
	if !ok {
		return store.ErrNotFound
	}
	next := latest.Version + 1
	// No conflict check is needed here: UpdateTest holds s.mu for its entire
	// body and latestLocked always returns the current max version, so no
	// other goroutine can ever have claimed `next` by the time we write it.
	// Postgres has no equivalent in-process lock across connections, so it
	// relies instead on the (catalog_id, version) unique index and reports
	// store.ErrConflict when that index rejects a racing insert.
	t.ProjectID = latest.ProjectID
	t.VersionID = uuid.NewString()
	t.Version = next
	t.CreatedAt = s.familyCreatedAt(t.ID)
	t.UpdatedAt = time.Now().UTC()
	s.tests[t.VersionID] = *t
	return nil
}

func (s *TestStore) ListTestVersions(ctx context.Context, catalogID string) ([]model.Test, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	created := s.familyCreatedAt(catalogID)
	out := []model.Test{}
	for _, t := range s.tests {
		if t.ID == catalogID {
			t.CreatedAt = created
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

// latestLocked returns the highest-numbered version of a family. Callers must
// hold the lock.
func (s *TestStore) latestLocked(catalogID string) (model.Test, bool) {
	var found model.Test
	ok := false
	for _, t := range s.tests {
		if t.ID == catalogID && (!ok || t.Version > found.Version) {
			found = t
			ok = true
		}
	}
	return found, ok
}

// familyCreatedAt is when the test was first created, i.e. the earliest
// version's timestamp -- the postgres equivalent of
// MIN(created_at) OVER (PARTITION BY catalog_id). Callers must hold the lock.
func (s *TestStore) familyCreatedAt(catalogID string) time.Time {
	var earliest time.Time
	for _, t := range s.tests {
		if t.ID != catalogID {
			continue
		}
		if earliest.IsZero() || t.UpdatedAt.Before(earliest) {
			earliest = t.UpdatedAt
		}
	}
	return earliest
}
