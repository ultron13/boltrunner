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
