package memstore

import (
	"context"
	"testing"
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
