package memstore

import (
	"context"
	"errors"
	"testing"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

func TestProjectStoreListsSeededDefault(t *testing.T) {
	ctx := context.Background()
	ps := NewProjectStore()

	projects, err := ps.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if projects == nil {
		t.Fatal("expected a non-nil slice")
	}
	if len(projects) != 1 {
		t.Fatalf("expected exactly the seeded Default project, got %d", len(projects))
	}
	if projects[0].Name != DefaultProjectName {
		t.Fatalf("expected name %q, got %q", DefaultProjectName, projects[0].Name)
	}
	if projects[0].ID != DefaultProjectID {
		t.Fatalf("expected id %q, got %q", DefaultProjectID, projects[0].ID)
	}
	if projects[0].CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
}

func TestCreateProjectPopulatesIDAndCreatedAt(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	if err := s.CreateProject(context.Background(), p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID == "" {
		t.Error("expected an id to be assigned")
	}
	if p.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestCreateProjectRejectsADuplicateName(t *testing.T) {
	s := NewProjectStore()
	first := &model.Project{Name: "Payments"}
	if err := s.CreateProject(context.Background(), first); err != nil {
		t.Fatalf("first CreateProject: %v", err)
	}
	err := s.CreateProject(context.Background(), &model.Project{Name: "Payments"})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for a duplicate name, got %v", err)
	}
}

// The seeded Default project is a row like any other: creating it again must
// conflict, not silently produce a second "Default".
func TestCreateProjectConflictsWithTheSeededDefault(t *testing.T) {
	s := NewProjectStore()
	err := s.CreateProject(context.Background(), &model.Project{Name: DefaultProjectName})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for the seeded name, got %v", err)
	}
}

func TestCreateProjectAppearsInListProjects(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	if err := s.CreateProject(context.Background(), p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	projects, err := s.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	var found bool
	for _, got := range projects {
		if got.ID == p.ID && got.Name == "Payments" {
			found = true
		}
	}
	if !found {
		t.Fatalf("created project missing from ListProjects: %+v", projects)
	}
}

func TestRenameProjectChangesTheName(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	if err := s.CreateProject(context.Background(), p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := s.RenameProject(context.Background(), p.ID, "Billing")
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if got.Name != "Billing" || got.ID != p.ID {
		t.Fatalf("unexpected project: %+v", got)
	}

	list, _ := s.ListProjects(context.Background())
	for _, l := range list {
		if l.ID == p.ID && l.Name != "Billing" {
			t.Fatalf("rename did not persist: %+v", l)
		}
	}
}

func TestRenameProjectReturnsConflictForATakenName(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	s.CreateProject(context.Background(), p)

	// "Default" is seeded, so renaming onto it must conflict.
	if _, err := s.RenameProject(context.Background(), p.ID, DefaultProjectName); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

// Renaming a project to the name it already has is a no-op, not a conflict
// with itself.
func TestRenameProjectToItsOwnNameSucceeds(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	s.CreateProject(context.Background(), p)

	if _, err := s.RenameProject(context.Background(), p.ID, "Payments"); err != nil {
		t.Fatalf("expected renaming to the same name to succeed, got %v", err)
	}
}

func TestRenameProjectReturnsNotFoundForAnUnknownID(t *testing.T) {
	s := NewProjectStore()
	if _, err := s.RenameProject(context.Background(), "no-such-id", "Billing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// Rename must preserve the flag: renaming the default project is the whole
// point of Task 1, and a rename that cleared it would break test creation.
func TestRenameProjectPreservesTheDefaultFlag(t *testing.T) {
	s := NewProjectStore()
	got, err := s.RenameProject(context.Background(), DefaultProjectID, "Shared")
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if !got.IsDefault {
		t.Fatal("renaming the default project must not clear is_default")
	}
}

func TestDeleteProjectRemovesIt(t *testing.T) {
	s := NewProjectStore()
	p := &model.Project{Name: "Payments"}
	s.CreateProject(context.Background(), p)

	if err := s.DeleteProject(context.Background(), p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	list, _ := s.ListProjects(context.Background())
	for _, l := range list {
		if l.ID == p.ID {
			t.Fatal("expected the project to be gone")
		}
	}
}

func TestDeleteProjectRefusesTheDefault(t *testing.T) {
	s := NewProjectStore()
	if err := s.DeleteProject(context.Background(), DefaultProjectID); !errors.Is(err, store.ErrProtected) {
		t.Fatalf("expected ErrProtected, got %v", err)
	}
}

func TestDeleteProjectReturnsNotFoundForAnUnknownID(t *testing.T) {
	s := NewProjectStore()
	if err := s.DeleteProject(context.Background(), "no-such-id"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
