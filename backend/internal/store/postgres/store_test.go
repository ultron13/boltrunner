package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
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

func TestListTestsReturnsCreatedTest(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "list-tests-check", TargetURL: "http://example.com", VirtualUsers: 2, DurationSeconds: 5}
	if err := db.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	all, err := db.ListTests(ctx)
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	if all == nil {
		t.Fatal("expected a non-nil slice")
	}
	found := false
	for _, got := range all {
		if got.ID == tst.ID {
			found = true
			if got.Name != "list-tests-check" {
				t.Fatalf("unexpected name: %q", got.Name)
			}
		}
	}
	if !found {
		t.Fatalf("expected to find created test %s in ListTests result", tst.ID)
	}
}

func TestGetTestNotFound(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	_, err := db.GetTest(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetRunNotFound(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	_, err := db.GetRun(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateRunStatusNotFound(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	err := db.UpdateRunStatus(ctx, "00000000-0000-0000-0000-000000000000", model.RunRunning, "")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateRunStatusCompletedSetsCompletedAt(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = db.CreateTest(ctx, tst)
	run := &model.Run{TestID: tst.ID, Status: model.RunPending}
	if err := db.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	if err := db.UpdateRunStatus(ctx, run.ID, model.RunCompleted, ""); err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}

	got, err := db.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != model.RunCompleted {
		t.Fatalf("expected completed status, got %s", got.Status)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be set")
	}
}

func TestListSnapshotsReturnsAppendedSnapshotsInOrder(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = db.CreateTest(ctx, tst)
	run := &model.Run{TestID: tst.ID, Status: model.RunRunning}
	_ = db.CreateRun(ctx, run)

	first := &model.RunMetricSnapshot{RunID: run.ID, ElapsedSeconds: 1, ThroughputRPS: 1, SampleCount: 1}
	if err := db.AppendMetricSnapshot(ctx, first); err != nil {
		t.Fatalf("AppendMetricSnapshot: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	second := &model.RunMetricSnapshot{RunID: run.ID, ElapsedSeconds: 2, ThroughputRPS: 2, SampleCount: 2}
	if err := db.AppendMetricSnapshot(ctx, second); err != nil {
		t.Fatalf("AppendMetricSnapshot: %v", err)
	}

	snapshots, err := db.ListSnapshots(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}
	if snapshots[0].ID != first.ID || snapshots[1].ID != second.ID {
		t.Fatalf("expected ascending ts order, got %s then %s", snapshots[0].ID, snapshots[1].ID)
	}
}

// The following tests use an already-cancelled context to force the
// underlying pgx query/exec call to fail without needing to corrupt the
// schema or database connection: this exercises the "unexpected query
// error" (as opposed to not-found) branches that success-path tests can't
// reach.

func TestMigrateFailsWithCancelledContext(t *testing.T) {
	dsn := os.Getenv("BOLTRUNNER_TEST_DSN")
	if dsn == "" {
		t.Skip("BOLTRUNNER_TEST_DSN not set; skipping (requires a live Postgres)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	db, err := Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer db.Close()
	cancel()

	if err := db.Migrate(ctx); err == nil {
		t.Fatal("expected an error when migrating with a cancelled context")
	}
}

func TestListTestsFailsWithCancelledContext(t *testing.T) {
	db := setupDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := db.ListTests(ctx); err == nil {
		t.Fatal("expected an error when listing tests with a cancelled context")
	}
}

func TestListByTestFailsWithCancelledContext(t *testing.T) {
	db := setupDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := db.ListByTest(ctx, "any-test-id"); err == nil {
		t.Fatal("expected an error when listing runs with a cancelled context")
	}
}

func TestUpdateRunStatusFailsWithCancelledContext(t *testing.T) {
	db := setupDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := db.UpdateRunStatus(ctx, "any-run-id", model.RunRunning, ""); err == nil {
		t.Fatal("expected an error when updating run status with a cancelled context")
	}
}

func TestListSnapshotsFailsWithCancelledContext(t *testing.T) {
	db := setupDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := db.ListSnapshots(ctx, "any-run-id"); err == nil {
		t.Fatal("expected an error when listing snapshots with a cancelled context")
	}
}

func TestLatestSnapshotNotFound(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = db.CreateTest(ctx, tst)
	run := &model.Run{TestID: tst.ID, Status: model.RunPending}
	_ = db.CreateRun(ctx, run)

	_, err := db.LatestSnapshot(ctx, run.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMigrationVersionParsesLeadingInteger(t *testing.T) {
	got, err := migrationVersion("0003_projects.sql")
	if err != nil {
		t.Fatalf("migrationVersion: %v", err)
	}
	if got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
	if _, err := migrationVersion("nonsense.sql"); err == nil {
		t.Fatal("expected an error for a filename with no version prefix")
	}
	if _, err := migrationVersion("abcd_x.sql"); err == nil {
		t.Fatal("expected an error for a non-numeric version prefix")
	}
}

func TestMigrateRecordsVersionsAndIsIdempotent(t *testing.T) {
	db := setupDB(t) // setupDB already calls Migrate once
	ctx := context.Background()

	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count < 2 {
		t.Fatalf("expected the existing migrations to be recorded, got %d", count)
	}

	// A second Migrate must skip everything already applied rather than
	// re-running it.
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var after int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatalf("recount schema_migrations: %v", err)
	}
	if after != count {
		t.Fatalf("expected the count to stay %d, got %d", count, after)
	}
}
