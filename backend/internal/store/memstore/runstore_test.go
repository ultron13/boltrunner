package memstore

import (
	"context"
	"testing"
	"time"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

func TestRunStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	rs := NewRunStore()

	r := &model.Run{TestID: "test-1", Status: model.RunPending}
	if err := rs.CreateRun(ctx, r); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if r.ID == "" {
		t.Fatal("expected an ID")
	}

	if err := rs.UpdateRunStatus(ctx, r.ID, model.RunRunning, ""); err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}
	got, err := rs.GetRun(ctx, r.ID)
	if err != nil || got.Status != model.RunRunning {
		t.Fatalf("expected running, got %+v, err=%v", got, err)
	}

	snap := &model.RunMetricSnapshot{RunID: r.ID, ElapsedSeconds: 1, ThroughputRPS: 10, AvgResponseTimeMs: 100, ErrorRatePct: 0, SampleCount: 10}
	if err := rs.AppendMetricSnapshot(ctx, snap); err != nil {
		t.Fatalf("AppendMetricSnapshot: %v", err)
	}
	latest, err := rs.LatestSnapshot(ctx, r.ID)
	if err != nil || latest.ThroughputRPS != 10 {
		t.Fatalf("LatestSnapshot: %+v, err=%v", latest, err)
	}
	all, err := rs.ListSnapshots(ctx, r.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListSnapshots: %d, err=%v", len(all), err)
	}
}

func TestCreateRunSetsCreatedAt(t *testing.T) {
	s := NewRunStore()
	before := time.Now().UTC()
	run := &model.Run{TestID: "t1", Status: model.RunPending}
	if err := s.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.CreatedAt.Before(before) || run.CreatedAt.After(time.Now().UTC()) {
		t.Fatalf("expected CreatedAt to be set to roughly now, got %v", run.CreatedAt)
	}
}

func TestListByTestReturnsOnlyMatchingRunsNewestFirst(t *testing.T) {
	s := NewRunStore()
	ctx := context.Background()

	older := &model.Run{TestID: "t1", Status: model.RunCompleted}
	_ = s.CreateRun(ctx, older)
	time.Sleep(2 * time.Millisecond)
	newer := &model.Run{TestID: "t1", Status: model.RunRunning}
	_ = s.CreateRun(ctx, newer)
	other := &model.Run{TestID: "t2", Status: model.RunPending}
	_ = s.CreateRun(ctx, other)

	runs, err := s.ListByTest(ctx, "t1")
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs for t1, got %d", len(runs))
	}
	if runs[0].ID != newer.ID || runs[1].ID != older.ID {
		t.Fatalf("expected newest-first order, got %s then %s", runs[0].ID, runs[1].ID)
	}
}

func TestListByTestReturnsEmptySliceNotNil(t *testing.T) {
	s := NewRunStore()
	runs, err := s.ListByTest(context.Background(), "no-such-test")
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	if runs == nil {
		t.Fatal("expected an empty slice, got nil")
	}
	if len(runs) != 0 {
		t.Fatalf("expected 0 runs, got %d", len(runs))
	}
}

func TestGetRunNotFound(t *testing.T) {
	s := NewRunStore()
	if _, err := s.GetRun(context.Background(), "missing"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateRunStatusNotFound(t *testing.T) {
	s := NewRunStore()
	if err := s.UpdateRunStatus(context.Background(), "missing", model.RunRunning, ""); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAppendMetricSnapshotNotFound(t *testing.T) {
	s := NewRunStore()
	snap := &model.RunMetricSnapshot{RunID: "missing", ElapsedSeconds: 1}
	if err := s.AppendMetricSnapshot(context.Background(), snap); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLatestSnapshotNotFound(t *testing.T) {
	s := NewRunStore()
	if _, err := s.LatestSnapshot(context.Background(), "missing"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateRunDerivesTestCatalogIDFromTestID(t *testing.T) {
	ctx := context.Background()
	rs := NewRunStore()

	run := &model.Run{TestID: "version-1-id", Status: model.RunPending}
	if err := rs.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.TestCatalogID != "version-1-id" {
		t.Fatalf("expected the catalog id to default to the test id, got %q", run.TestCatalogID)
	}
}

func TestListByTestGroupsRunsAcrossVersions(t *testing.T) {
	ctx := context.Background()
	rs := NewRunStore()

	// Two runs of the same test, executed against different versions.
	first := &model.Run{TestID: "v1", TestCatalogID: "catalog", Status: model.RunCompleted}
	_ = rs.CreateRun(ctx, first)
	second := &model.Run{TestID: "v2", TestCatalogID: "catalog", Status: model.RunCompleted}
	_ = rs.CreateRun(ctx, second)

	runs, err := rs.ListByTest(ctx, "catalog")
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected both versions' runs, got %d", len(runs))
	}
}
