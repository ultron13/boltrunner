package memstore

import (
	"context"
	"testing"
	"time"

	"github.com/boltrunner/backend/internal/model"
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
