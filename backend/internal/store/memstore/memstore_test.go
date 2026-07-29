package memstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

func TestTestStoreCreateListGet(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	in := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 10, DurationSeconds: 30}
	if err := ts.CreateTest(ctx, in); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if in.ID == "" {
		t.Fatal("expected CreateTest to assign an ID")
	}

	all, err := ts.ListTests(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListTests: %v, %d results", err, len(all))
	}

	got, err := ts.GetTest(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if got.Name != "smoke" {
		t.Fatalf("expected name 'smoke', got %q", got.Name)
	}

	if _, err := ts.GetTest(ctx, "missing"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateTestDefaultsToDefaultProject(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	in := &model.Test{Name: "no-project", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.CreateTest(ctx, in); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if in.ProjectID != DefaultProjectID {
		t.Fatalf("expected the Default project id, got %q", in.ProjectID)
	}

	got, err := ts.GetTest(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if got.ProjectID != DefaultProjectID {
		t.Fatalf("expected GetTest to report the Default project, got %q", got.ProjectID)
	}
}

func TestCreateTestHonoursExplicitProject(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	in := &model.Test{ProjectID: DefaultProjectID, Name: "explicit", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.CreateTest(ctx, in); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if in.ProjectID != DefaultProjectID {
		t.Fatalf("expected the explicit project id to be preserved, got %q", in.ProjectID)
	}
}

// An unknown project must be rejected rather than stored, so that ListTests can
// never report a project_id that ListProjects does not list. postgres enforces
// this with the tests.project_id foreign key; memstore knows only Default, so
// it rejects everything else. This test originally asserted the opposite --
// that any explicit id was preserved -- which was written before the invalid-
// reference contract existed.
func TestCreateTestRejectsUnknownProject(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	in := &model.Test{
		ProjectID: "11111111-1111-1111-1111-111111111111",
		Name:      "orphan", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1,
	}
	if err := ts.CreateTest(ctx, in); !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}

	all, err := ts.ListTests(ctx)
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected the rejected test not to be stored, got %d", len(all))
	}
}

func TestUpdateTestCreatesNewVersionAndKeepsTheOldOne(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	v1 := &model.Test{Name: "checkout", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 10}
	if err := ts.CreateTest(ctx, v1); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("expected version 1, got %d", v1.Version)
	}
	if v1.VersionID == "" {
		t.Fatal("expected a VersionID")
	}

	edit := &model.Test{ID: v1.ID, Name: "checkout", TargetURL: "http://b", VirtualUsers: 2, DurationSeconds: 20}
	if err := ts.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}
	if edit.Version != 2 {
		t.Fatalf("expected version 2, got %d", edit.Version)
	}
	if edit.ID != v1.ID {
		t.Fatalf("expected the catalog id to be stable, got %q want %q", edit.ID, v1.ID)
	}
	if edit.VersionID == v1.VersionID {
		t.Fatal("expected a new VersionID for the new version")
	}

	// GetTest resolves the latest version.
	latest, err := ts.GetTest(ctx, v1.ID)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if latest.Version != 2 || latest.TargetURL != "http://b" {
		t.Fatalf("expected v2/http://b, got v%d/%s", latest.Version, latest.TargetURL)
	}

	// ListTests collapses the family to one row.
	all, err := ts.ListTests(ctx)
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 row for 1 test family, got %d", len(all))
	}
	if all[0].Version != 2 {
		t.Fatalf("expected the latest version, got v%d", all[0].Version)
	}

	// The old version is still readable, unchanged.
	versions, err := ts.ListTestVersions(ctx, v1.ID)
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("expected newest-first, got v%d then v%d", versions[0].Version, versions[1].Version)
	}
	if versions[1].TargetURL != "http://a" {
		t.Fatalf("expected v1 to keep its original target, got %q", versions[1].TargetURL)
	}
}

func TestUpdateTestPreservesFamilyCreatedAt(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	v1 := &model.Test{Name: "x", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	_ = ts.CreateTest(ctx, v1)
	time.Sleep(5 * time.Millisecond)
	edit := &model.Test{ID: v1.ID, Name: "x", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	latest, _ := ts.GetTest(ctx, v1.ID)
	if !latest.CreatedAt.Equal(v1.CreatedAt) {
		t.Fatalf("expected CreatedAt to stay the family's first creation %v, got %v", v1.CreatedAt, latest.CreatedAt)
	}
	if !latest.UpdatedAt.After(latest.CreatedAt) {
		t.Fatalf("expected UpdatedAt (%v) to be after CreatedAt (%v)", latest.UpdatedAt, latest.CreatedAt)
	}
}

func TestUpdateTestDoesNotReorderListTests(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	older := &model.Test{Name: "older", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	_ = ts.CreateTest(ctx, older)
	time.Sleep(5 * time.Millisecond)
	newer := &model.Test{Name: "newer", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1}
	_ = ts.CreateTest(ctx, newer)

	// Editing the older test must not promote it: list order follows when each
	// test was first created, not when it was last touched.
	time.Sleep(5 * time.Millisecond)
	edit := &model.Test{ID: older.ID, Name: "older", TargetURL: "http://a2", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	all, err := ts.ListTests(ctx)
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 families, got %d", len(all))
	}
	if all[0].ID != newer.ID || all[1].ID != older.ID {
		t.Fatalf("expected [newer, older] after editing older, got [%s, %s]", all[0].Name, all[1].Name)
	}
}

func TestUpdateTestUnknownCatalogIDIsNotFound(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	err := ts.UpdateTest(ctx, &model.Test{ID: "missing", Name: "x", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListTestVersionsUnknownIDIsEmptyNotNil(t *testing.T) {
	ctx := context.Background()
	ts := NewTestStore()

	versions, err := ts.ListTestVersions(ctx, "missing")
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if versions == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(versions) != 0 {
		t.Fatalf("expected 0 versions, got %d", len(versions))
	}
}

// memstore's TestStore has no project registry: CreateTest rejects any id but
// the seeded Default (see its ErrInvalidReference branch). So the filter is
// exercised here as "the seeded project versus anything else"; postgres, which
// has a real registry, covers filtering between two populated projects.
func TestListTestsForProjectReturnsOnlyThatProjectsTests(t *testing.T) {
	s := NewTestStore()
	ctx := context.Background()
	mine := &model.Test{ProjectID: DefaultProjectID, Name: "mine", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	if err := s.CreateTest(ctx, mine); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	got, err := s.ListTestsForProject(ctx, DefaultProjectID)
	if err != nil {
		t.Fatalf("ListTestsForProject: %v", err)
	}
	if len(got) != 1 || got[0].Name != "mine" {
		t.Fatalf("expected the Default-project test, got %+v", got)
	}

	other, err := s.ListTestsForProject(ctx, "00000000-0000-0000-0000-0000000000ff")
	if err != nil {
		t.Fatalf("ListTestsForProject: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("expected no tests for another project, got %+v", other)
	}
}

func TestListTestsForProjectReturnsEmptyForAnUnknownProject(t *testing.T) {
	s := NewTestStore()
	got, err := s.ListTestsForProject(context.Background(), "p-nope")
	if err != nil {
		t.Fatalf("expected no error for an unknown project, got %v", err)
	}
	if got == nil {
		t.Fatal("expected an empty slice, not nil -- it is JSON-encoded directly")
	}
	if len(got) != 0 {
		t.Fatalf("expected an empty slice, got %+v", got)
	}
}

// The filter must collapse each family to its latest version, exactly as
// ListTests does -- a scoped list that showed every version would double-count.
func TestListTestsForProjectKeepsLatestVersionOnly(t *testing.T) {
	s := NewTestStore()
	ctx := context.Background()
	tst := &model.Test{ProjectID: DefaultProjectID, Name: "v1", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	if err := s.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	tst.Name = "v2"
	if err := s.UpdateTest(ctx, tst); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	got, err := s.ListTestsForProject(ctx, DefaultProjectID)
	if err != nil {
		t.Fatalf("ListTestsForProject: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one row for the family, got %d: %+v", len(got), got)
	}
	if got[0].Name != "v2" {
		t.Fatalf("expected the latest version, got %q", got[0].Name)
	}
}
