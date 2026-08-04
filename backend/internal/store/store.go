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
	// ErrProtected means the operation targets a row the system depends on --
	// today, only the default project, which is what an omitted project_id
	// falls back to.
	ErrProtected = errors.New("protected")
	// ErrNotEmpty means a project still has tests filed under it. Handlers check
	// this up front so they can report a count; postgres also surfaces it from
	// the tests.project_id foreign key if a delete races that check.
	ErrNotEmpty = errors.New("not empty")
)

type TestStore interface {
	CreateTest(ctx context.Context, t *model.Test) error
	ListTests(ctx context.Context) ([]model.Test, error)
	// ListTestsForProject is ListTests restricted to one project. An unknown or
	// malformed project id yields an empty slice, not an error -- it is
	// indistinguishable from a project that simply has no tests.
	ListTestsForProject(ctx context.Context, projectID string) ([]model.Test, error)
	GetTest(ctx context.Context, catalogID string) (*model.Test, error)
	// UpdateTest writes a new immutable version of t.ID's test rather than
	// mutating the current one.
	UpdateTest(ctx context.Context, t *model.Test) error
	// ListTestVersions returns every version of a test, newest first.
	ListTestVersions(ctx context.Context, catalogID string) ([]model.Test, error)
	// MoveTest refiles every version of catalogID under projectID. A project is
	// where a test is filed, not part of what a run executed, so moving does not
	// cut a new version. ErrNotFound if no such test; ErrInvalidReference if the
	// project does not exist.
	MoveTest(ctx context.Context, catalogID, projectID string) error
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
	// CreateProject assigns p.ID and p.CreatedAt. It returns ErrConflict if a
	// project with the same name already exists.
	CreateProject(ctx context.Context, p *model.Project) error
	// RenameProject returns the updated project. ErrNotFound if no project has
	// that id; ErrConflict if another project already holds the name.
	RenameProject(ctx context.Context, id, name string) (*model.Project, error)
	// DeleteProject removes a project. ErrNotFound if no project has that id;
	// ErrProtected if it is the default project. It does not count tests --
	// the handler does that so it can report how many. Postgres may still
	// return ErrNotEmpty from the foreign key if a delete races that count.
	DeleteProject(ctx context.Context, id string) error
}
