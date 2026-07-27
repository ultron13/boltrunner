package memstore

import (
	"context"
	"testing"

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

	const other = "11111111-1111-1111-1111-111111111111"
	in := &model.Test{ProjectID: other, Name: "explicit", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	if err := ts.CreateTest(ctx, in); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if in.ProjectID != other {
		t.Fatalf("expected the explicit project id to be preserved, got %q", in.ProjectID)
	}
}
