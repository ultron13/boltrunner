package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/boltrunner/backend/internal/model"
)

func setupDB(t *testing.T) *DB {
	dsn := os.Getenv("BOLTRUNNER_TEST_DSN")
	if dsn == "" {
		t.Skip("BOLTRUNNER_TEST_DSN not set; skipping (requires a live Postgres)")
	}
	ctx := context.Background()
	db, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestTestStoreCRUD(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	if err := db.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if tst.ID == "" {
		t.Fatal("expected an ID")
	}

	got, err := db.GetTest(ctx, tst.ID)
	if err != nil || got.Name != "smoke" {
		t.Fatalf("GetTest: %+v, err=%v", got, err)
	}
}

func TestRunStoreLifecycle(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = db.CreateTest(ctx, tst)

	run := &model.Run{TestID: tst.ID, Status: model.RunPending}
	if err := db.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	if err := db.UpdateRunStatus(ctx, run.ID, model.RunRunning, ""); err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}

	snap := &model.RunMetricSnapshot{RunID: run.ID, ElapsedSeconds: 1, ThroughputRPS: 3, AvgResponseTimeMs: 90, ErrorRatePct: 0, SampleCount: 3}
	if err := db.AppendMetricSnapshot(ctx, snap); err != nil {
		t.Fatalf("AppendMetricSnapshot: %v", err)
	}

	latest, err := db.LatestSnapshot(ctx, run.ID)
	if err != nil || latest.ThroughputRPS != 3 {
		t.Fatalf("LatestSnapshot: %+v, err=%v", latest, err)
	}
}

// TestListXXXNeverReturnsNilSlice guards against a real bug found via e2e testing:
// a bare `var out []T` scanned zero times is nil, and encoding/json encodes a nil
// slice as `null`, not `[]` -- which crashes frontend code doing `list.length`.
func TestListTestsNeverReturnsNilSlice(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	// A run with no snapshots is the reliable way to force a zero-row result
	// without needing to truncate tables shared with other tests.
	tst := &model.Test{Name: "empty-list-check", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	_ = db.CreateTest(ctx, tst)
	run := &model.Run{TestID: tst.ID, Status: model.RunPending}
	_ = db.CreateRun(ctx, run)

	snapshots, err := db.ListSnapshots(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if snapshots == nil {
		t.Fatal("expected ListSnapshots to return a non-nil empty slice, got nil")
	}
	if len(snapshots) != 0 {
		t.Fatalf("expected 0 snapshots, got %d", len(snapshots))
	}
}

func TestCreateRunSetsCreatedAt(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = db.CreateTest(ctx, tst)

	run := &model.Run{TestID: tst.ID, Status: model.RunPending}
	if err := db.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}

	got, err := db.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("expected GetRun to populate CreatedAt")
	}
}

func TestListByTestNewestFirstAndNeverNil(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "list-by-test-check", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	_ = db.CreateTest(ctx, tst)

	// A brand-new test has no runs yet: must be an empty slice, not nil.
	none, err := db.ListByTest(ctx, tst.ID)
	if err != nil {
		t.Fatalf("ListByTest (empty): %v", err)
	}
	if none == nil {
		t.Fatal("expected an empty slice, got nil")
	}

	older := &model.Run{TestID: tst.ID, Status: model.RunCompleted}
	_ = db.CreateRun(ctx, older)
	time.Sleep(10 * time.Millisecond)
	newer := &model.Run{TestID: tst.ID, Status: model.RunRunning}
	_ = db.CreateRun(ctx, newer)

	runs, err := db.ListByTest(ctx, tst.ID)
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].ID != newer.ID || runs[1].ID != older.ID {
		t.Fatalf("expected newest-first order, got %s then %s", runs[0].ID, runs[1].ID)
	}
}
