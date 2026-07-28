package store

import (
	"context"
	"errors"

	"github.com/boltrunner/backend/internal/model"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrConflict means a concurrent edit already claimed the version number
	// this update tried to write.
	ErrConflict = errors.New("conflict")
	// ErrInvalidReference means a referenced entity does not exist.
	ErrInvalidReference = errors.New("invalid reference")
)

type TestStore interface {
	CreateTest(ctx context.Context, t *model.Test) error
	ListTests(ctx context.Context) ([]model.Test, error)
	GetTest(ctx context.Context, catalogID string) (*model.Test, error)
	// UpdateTest writes a new immutable version of t.ID's test rather than
	// mutating the current one.
	UpdateTest(ctx context.Context, t *model.Test) error
	// ListTestVersions returns every version of a test, newest first.
	ListTestVersions(ctx context.Context, catalogID string) ([]model.Test, error)
}

type RunStore interface {
	CreateRun(ctx context.Context, r *model.Run) error
	GetRun(ctx context.Context, id string) (*model.Run, error)
	// ListByTest takes a catalog id and returns runs across all versions of
	// that test family.
	ListByTest(ctx context.Context, catalogID string) ([]model.Run, error)
	UpdateRunStatus(ctx context.Context, id string, status model.RunStatus, errMsg string) error
	AppendMetricSnapshot(ctx context.Context, s *model.RunMetricSnapshot) error
	LatestSnapshot(ctx context.Context, runID string) (*model.RunMetricSnapshot, error)
	ListSnapshots(ctx context.Context, runID string) ([]model.RunMetricSnapshot, error)
}

type ProjectStore interface {
	ListProjects(ctx context.Context) ([]model.Project, error)
}
