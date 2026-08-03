package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

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

// TestApplyMigrationDoesNotRecordAFailedMigration guards the transactional
// wrapping in applyMigration: the migration body and the schema_migrations
// insert run in the same transaction, so a body that fails must not leave
// behind a record claiming it succeeded -- that would make Migrate skip it
// forever on every future run.
func TestApplyMigrationDoesNotRecordAFailedMigration(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	const version = 9999 // far outside the real migration range; cannot collide
	if err := db.applyMigration(ctx, version, "SELECT 1/0"); err == nil {
		t.Fatal("expected an error from a division-by-zero migration body")
	}

	applied, err := db.migrationApplied(ctx, version)
	if err != nil {
		t.Fatalf("migrationApplied: %v", err)
	}
	if applied {
		t.Fatal("expected the failed migration to not be recorded as applied")
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

// newScratchDB creates a throwaway database, applies only the migrations up to
// and including maxVersion, and returns a connection to it. It exists to test
// the upgrade path of an *existing* deployment: seed legacy rows, then run the
// full Migrate and assert the backfills.
func newScratchDB(t *testing.T, maxVersion int) *DB {
	t.Helper()
	dsn := os.Getenv("BOLTRUNNER_TEST_DSN")
	if dsn == "" {
		t.Skip("BOLTRUNNER_TEST_DSN not set; skipping (requires a live Postgres)")
	}
	ctx := context.Background()

	admin, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("Connect (admin): %v", err)
	}
	defer admin.Close()

	name := fmt.Sprintf("boltrunner_scratch_%d", time.Now().UnixNano())
	if _, err := admin.Pool.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatalf("CREATE DATABASE: %v", err)
	}

	// Registered before anything else can fail, so a later t.Fatalf cannot
	// strand the database we just created on a shared Postgres instance.
	var db *DB
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		cleanup, err := Connect(context.Background(), dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		cleanup.Pool.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name)
	})

	scratchDSN, err := replaceDBName(dsn, name)
	if err != nil {
		t.Fatalf("replaceDBName: %v", err)
	}
	db, err = Connect(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("Connect (scratch): %v", err)
	}

	// Apply only the migrations at or below maxVersion, simulating a
	// deployment that predates the newer ones.
	if _, err := db.Pool.Exec(ctx, createSchemaMigrations); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	names, err := migrationFilenames()
	if err != nil {
		t.Fatalf("migrationFilenames: %v", err)
	}
	for _, n := range names {
		v, err := migrationVersion(n)
		if err != nil {
			t.Fatalf("migrationVersion(%s): %v", n, err)
		}
		if v > maxVersion {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + n)
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		if err := db.applyMigration(ctx, v, string(body)); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
	}
	return db
}

func replaceDBName(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

func TestListProjectsIncludesSeededDefaultAndIsNeverNil(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	projects, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if projects == nil {
		t.Fatal("expected a non-nil slice")
	}
	found := false
	for _, p := range projects {
		if p.Name == "Default" {
			found = true
			if p.ID == "" || p.CreatedAt.IsZero() {
				t.Fatalf("expected a fully populated project, got %+v", p)
			}
		}
	}
	if !found {
		t.Fatal("expected the seeded Default project")
	}
}

func TestUpdateTestCreatesNewVersionAndPinsOldRuns(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	v1 := &model.Test{Name: "pg-versioning", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 10}
	if err := db.CreateTest(ctx, v1); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if v1.Version != 1 || v1.VersionID == "" {
		t.Fatalf("expected v1 with a VersionID, got v%d/%q", v1.Version, v1.VersionID)
	}

	// A run of v1, pinned to the exact version that executed.
	run := &model.Run{TestID: v1.VersionID, TestCatalogID: v1.ID, Status: model.RunCompleted}
	if err := db.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	edit := &model.Test{ID: v1.ID, Name: "pg-versioning", TargetURL: "http://b", VirtualUsers: 2, DurationSeconds: 20}
	if err := db.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}
	if edit.Version != 2 || edit.ID != v1.ID {
		t.Fatalf("expected v2 under the same catalog id, got v%d/%q", edit.Version, edit.ID)
	}
	// UpdateTest must populate the struct it was handed, not just the row: this
	// is the value a handler serializes straight into its response, so a zeroed
	// or version-local CreatedAt would ship as the test's creation date.
	if !edit.CreatedAt.Equal(v1.CreatedAt) {
		t.Fatalf("expected UpdateTest to report the family's CreatedAt %v, got %v", v1.CreatedAt, edit.CreatedAt)
	}
	if !edit.UpdatedAt.After(edit.CreatedAt) {
		t.Fatalf("expected UpdatedAt (%v) to be after CreatedAt (%v)", edit.UpdatedAt, edit.CreatedAt)
	}

	latest, err := db.GetTest(ctx, v1.ID)
	if err != nil || latest.Version != 2 || latest.TargetURL != "http://b" {
		t.Fatalf("GetTest: %+v, err=%v", latest, err)
	}

	// The pinned run still points at v1, whose config is untouched.
	got, err := db.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.TestID != v1.VersionID {
		t.Fatalf("expected the run to stay pinned to %q, got %q", v1.VersionID, got.TestID)
	}
	if got.TestCatalogID != v1.ID {
		t.Fatalf("expected the run's catalog id to be %q, got %q", v1.ID, got.TestCatalogID)
	}

	versions, err := db.ListTestVersions(ctx, v1.ID)
	if err != nil {
		t.Fatalf("ListTestVersions: %v", err)
	}
	if len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("expected newest-first [v2 v1], got %d versions", len(versions))
	}
	if versions[1].TargetURL != "http://a" {
		t.Fatalf("expected v1 to keep http://a, got %q", versions[1].TargetURL)
	}

	// Run history is family-scoped: still found via the catalog id.
	runs, err := db.ListByTest(ctx, v1.ID)
	if err != nil {
		t.Fatalf("ListByTest: %v", err)
	}
	found := false
	for _, r := range runs {
		if r.ID == run.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the v1 run to still appear in the test's history after the edit")
	}
}

// TestUpdateTestDoesNotReorderListTests is the postgres-backed counterpart to
// memstore's test of the same name. It is the only reason ListTests uses a
// DISTINCT ON subquery ordered by MIN(created_at) OVER (PARTITION BY
// catalog_id) instead of a plain ORDER BY created_at DESC -- the latter would
// let editing a test promote it to the top of the list. Because this
// database is shared and pre-populated across tests, absolute positions
// can't be asserted; instead this checks the *relative* order of two fresh
// families survives an edit to the older one.
func TestUpdateTestDoesNotReorderListTests(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	older := &model.Test{Name: "pg-order-older", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, older); err != nil {
		t.Fatalf("CreateTest (older): %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	newer := &model.Test{Name: "pg-order-newer", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, newer); err != nil {
		t.Fatalf("CreateTest (newer): %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	edit := &model.Test{ID: older.ID, Name: "pg-order-older", TargetURL: "http://a2", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	all, err := db.ListTests(ctx)
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	olderIdx, newerIdx := -1, -1
	for i, got := range all {
		if got.ID == older.ID {
			olderIdx = i
		}
		if got.ID == newer.ID {
			newerIdx = i
		}
	}
	if olderIdx == -1 || newerIdx == -1 {
		t.Fatalf("expected to find both families in ListTests, olderIdx=%d newerIdx=%d", olderIdx, newerIdx)
	}
	// ListTests orders newest-family-first, so newer must come first
	// (a lower index) even though older was the one just edited.
	if olderIdx <= newerIdx {
		t.Fatalf("expected editing the older family not to promote it: older index %d, newer index %d", olderIdx, newerIdx)
	}
}

func TestListTestsReturnsOneRowPerFamily(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "pg-one-row", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	_ = db.CreateTest(ctx, tst)
	edit := &model.Test{ID: tst.ID, Name: "pg-one-row", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.UpdateTest(ctx, edit); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	all, err := db.ListTests(ctx)
	if err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	seen := 0
	for _, got := range all {
		if got.ID == tst.ID {
			seen++
			if got.Version != 2 {
				t.Fatalf("expected the latest version, got v%d", got.Version)
			}
			if !got.CreatedAt.Equal(tst.CreatedAt) {
				t.Fatalf("expected the family's original CreatedAt %v, got %v", tst.CreatedAt, got.CreatedAt)
			}
			if !got.UpdatedAt.After(got.CreatedAt) {
				t.Fatalf("expected UpdatedAt after CreatedAt, got %v vs %v", got.UpdatedAt, got.CreatedAt)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("expected exactly 1 row for the family, got %d", seen)
	}
}

func TestUpdateTestConflictsOnDuplicateVersion(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "pg-conflict", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	_ = db.CreateTest(ctx, tst)

	// Claim version 2 directly, simulating the winner of a race.
	if _, err := db.Pool.Exec(ctx,
		`INSERT INTO tests (catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id)
		 VALUES ($1, 2, 'pg-conflict', 'http://race', 1, 1, $2)`,
		tst.ID, tst.ProjectID,
	); err != nil {
		t.Fatalf("seed racing version: %v", err)
	}

	// UpdateTest read version 1 as latest before the race landed, so it also
	// tries to write version 2 and must lose against the unique index.
	err := db.updateTestAtVersion(ctx,
		&model.Test{ID: tst.ID, Name: "pg-conflict", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1},
		2, tst.ProjectID, tst.CreatedAt)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestUpdateTestUnknownCatalogIDIsNotFound(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	err := db.UpdateTest(ctx, &model.Test{
		ID: "00000000-0000-0000-0000-000000000000", Name: "x",
		TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1,
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListTestVersionsUnknownIDIsEmptyNotNil(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	versions, err := db.ListTestVersions(ctx, "00000000-0000-0000-0000-000000000000")
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

func TestMigrateBackfillsVersioningForLegacyRows(t *testing.T) {
	ctx := context.Background()
	db := newScratchDB(t, 2) // pre-0003, pre-0004

	var legacyTestID, legacyRunID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO tests (name, target_url, virtual_users, duration_seconds)
		 VALUES ('legacy', 'http://example.com', 1, 1) RETURNING id`,
	).Scan(&legacyTestID); err != nil {
		t.Fatalf("insert legacy test: %v", err)
	}
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO runs (test_id, status) VALUES ($1, 'completed') RETURNING id`, legacyTestID,
	).Scan(&legacyRunID); err != nil {
		t.Fatalf("insert legacy run: %v", err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var catalogID string
	var version int
	if err := db.Pool.QueryRow(ctx,
		`SELECT catalog_id, version FROM tests WHERE id = $1`, legacyTestID,
	).Scan(&catalogID, &version); err != nil {
		t.Fatalf("read backfilled test: %v", err)
	}
	if catalogID != legacyTestID {
		t.Fatalf("expected catalog_id to be backfilled to the row's own id %q, got %q", legacyTestID, catalogID)
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}

	var runCatalogID string
	if err := db.Pool.QueryRow(ctx,
		`SELECT test_catalog_id FROM runs WHERE id = $1`, legacyRunID,
	).Scan(&runCatalogID); err != nil {
		t.Fatalf("read backfilled run: %v", err)
	}
	if runCatalogID != legacyTestID {
		t.Fatalf("expected the run's test_catalog_id to be %q, got %q", legacyTestID, runCatalogID)
	}

	// The upgraded database must be usable by the new code path too.
	got, err := db.GetTest(ctx, legacyTestID)
	if err != nil {
		t.Fatalf("GetTest after upgrade: %v", err)
	}
	if got.Version != 1 || got.VersionID != legacyTestID {
		t.Fatalf("expected v1 with VersionID %q, got v%d/%q", legacyTestID, got.Version, got.VersionID)
	}
}

func TestCreateTestDefaultsToDefaultProject(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{Name: "pg-default-project", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	if tst.ProjectID == "" {
		t.Fatal("expected CreateTest to populate ProjectID")
	}

	got, err := db.GetTest(ctx, tst.ID)
	if err != nil {
		t.Fatalf("GetTest: %v", err)
	}
	if got.ProjectID != tst.ProjectID {
		t.Fatalf("expected GetTest to report project %q, got %q", tst.ProjectID, got.ProjectID)
	}
}

func TestCreateTestUnknownProjectIDIsInvalidReference(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{
		Name: "pg-unknown-project", TargetURL: "http://example.com",
		VirtualUsers: 1, DurationSeconds: 1,
		ProjectID: "00000000-0000-0000-0000-000000000099", // well-formed UUID, no such project
	}
	err := db.CreateTest(ctx, tst)
	if !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}
}

func TestCreateTestMalformedProjectIDIsInvalidReference(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	tst := &model.Test{
		Name: "pg-malformed-project", TargetURL: "http://example.com",
		VirtualUsers: 1, DurationSeconds: 1,
		ProjectID: "not-a-uuid",
	}
	if err := db.CreateTest(ctx, tst); !errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("expected ErrInvalidReference, got %v", err)
	}
}

// An infrastructure failure must not be reported as a client error just because
// the caller happened to supply a project_id. This pins the distinction that
// makes handleCreateTest's 400-vs-500 split trustworthy: a cancelled context is
// the server's problem, not the request's.
func TestCreateTestInfrastructureFailureIsNotInvalidReference(t *testing.T) {
	db := setupDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	projects, err := db.ListProjects(context.Background())
	if err != nil || len(projects) == 0 {
		t.Fatalf("need at least the Default project to build a valid request: %v", err)
	}

	tst := &model.Test{
		Name: "pg-cancelled", TargetURL: "http://example.com",
		VirtualUsers: 1, DurationSeconds: 1,
		ProjectID: projects[0].ID, // a real, well-formed project
	}
	err = db.CreateTest(ctx, tst)
	if err == nil {
		t.Fatal("expected an error from a cancelled context")
	}
	if errors.Is(err, store.ErrInvalidReference) {
		t.Fatalf("a cancelled context must not be reported as an invalid reference, got %v", err)
	}
}

func TestMigrateBackfillsProjectIDForLegacyRows(t *testing.T) {
	ctx := context.Background()
	db := newScratchDB(t, 2) // a database as it looked before 0003

	// A legacy row, inserted with no project_id because the column did not
	// exist yet.
	var legacyID string
	if err := db.Pool.QueryRow(ctx,
		`INSERT INTO tests (name, target_url, virtual_users, duration_seconds)
		 VALUES ('legacy', 'http://example.com', 1, 1) RETURNING id`,
	).Scan(&legacyID); err != nil {
		t.Fatalf("insert legacy test: %v", err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var projectName string
	if err := db.Pool.QueryRow(ctx,
		`SELECT p.name FROM tests t JOIN projects p ON p.id = t.project_id WHERE t.id = $1`, legacyID,
	).Scan(&projectName); err != nil {
		t.Fatalf("read backfilled project: %v", err)
	}
	if projectName != "Default" {
		t.Fatalf("expected the legacy row to land in Default, got %q", projectName)
	}
}

// uniqueProjectName keeps these tests repeatable: setupDB reuses one database,
// so a fixed name would conflict with the row left by the previous run.
func uniqueProjectName(prefix string) string {
	return fmt.Sprintf("%s %d", prefix, time.Now().UnixNano())
}

func TestCreateProjectPersistsAndConflictsOnName(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	name := uniqueProjectName("Payments")
	p := &model.Project{Name: name}
	if err := db.CreateProject(ctx, p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID == "" || p.CreatedAt.IsZero() {
		t.Fatalf("expected id and created_at to be populated, got %+v", p)
	}

	err := db.CreateProject(ctx, &model.Project{Name: name})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for a duplicate name, got %v", err)
	}
}

func TestCreateProjectConflictsWithTheSeededDefault(t *testing.T) {
	db := setupDB(t)
	err := db.CreateProject(context.Background(), &model.Project{Name: "Default"})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict for the seeded Default name, got %v", err)
	}
}

func TestListTestsForProjectFiltersAndKeepsLatestVersionOnly(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	other := &model.Project{Name: uniqueProjectName("Scoped")}
	if err := db.CreateProject(ctx, other); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	inOther := &model.Test{ProjectID: other.ID, Name: "in-other", TargetURL: "http://b", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, inOther); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}
	// A test in the Default project, which must not appear in the scoped list.
	inDefault := &model.Test{Name: "in-default", TargetURL: "http://a", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, inDefault); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	// A second version: the filter must still collapse the family to its
	// latest version, exactly as ListTests does.
	inOther.Name = "in-other-v2"
	if err := db.UpdateTest(ctx, inOther); err != nil {
		t.Fatalf("UpdateTest: %v", err)
	}

	got, err := db.ListTestsForProject(ctx, other.ID)
	if err != nil {
		t.Fatalf("ListTestsForProject: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one row for the other project, got %d: %+v", len(got), got)
	}
	if got[0].Name != "in-other-v2" {
		t.Fatalf("expected the latest version, got %q", got[0].Name)
	}
}

// A malformed id must not surface as a 500. pgx fails to encode a non-UUID
// client-side, which is indistinguishable by type from a connection failure.
func TestListTestsForProjectReturnsEmptyForAMalformedID(t *testing.T) {
	db := setupDB(t)
	got, err := db.ListTestsForProject(context.Background(), "not-a-uuid")
	if err != nil {
		t.Fatalf("expected no error for a malformed id, got %v", err)
	}
	if got == nil {
		t.Fatal("expected an empty slice, not nil -- it is JSON-encoded directly")
	}
	if len(got) != 0 {
		t.Fatalf("expected an empty slice, got %+v", got)
	}
}

func TestListTestsForProjectReturnsEmptyForAnUnknownProject(t *testing.T) {
	db := setupDB(t)
	got, err := db.ListTestsForProject(context.Background(), "00000000-0000-0000-0000-0000000000ff")
	if err != nil {
		t.Fatalf("expected no error for an unknown project, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected an empty slice, got %+v", got)
	}
}

func TestPostgresRenameProject(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	p := &model.Project{Name: "Payments " + uuid.NewString()}
	if err := db.CreateProject(ctx, p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	renamed := "Billing " + uuid.NewString()
	got, err := db.RenameProject(ctx, p.ID, renamed)
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if got.Name != renamed {
		t.Fatalf("expected %q, got %q", renamed, got.Name)
	}
	if got.IsDefault {
		t.Fatal("a non-default project must not become default by being renamed")
	}
}

func TestPostgresRenameProjectConflictsOnATakenName(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	a := &model.Project{Name: "A " + uuid.NewString()}
	b := &model.Project{Name: "B " + uuid.NewString()}
	db.CreateProject(ctx, a)
	db.CreateProject(ctx, b)

	if _, err := db.RenameProject(ctx, b.ID, a.Name); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestPostgresRenameProjectNotFound(t *testing.T) {
	db := setupDB(t)
	if _, err := db.RenameProject(context.Background(), uuid.NewString(), "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// A malformed id must not surface as a driver encode error, which the caller
// could not distinguish from an outage.
func TestPostgresRenameProjectMalformedIDIsNotFound(t *testing.T) {
	db := setupDB(t)
	if _, err := db.RenameProject(context.Background(), "not-a-uuid", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPostgresDeleteProject(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	p := &model.Project{Name: "Doomed " + uuid.NewString()}
	db.CreateProject(ctx, p)

	if err := db.DeleteProject(ctx, p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	list, _ := db.ListProjects(ctx)
	for _, l := range list {
		if l.ID == p.ID {
			t.Fatal("expected the project to be gone")
		}
	}
}

func TestPostgresDeleteProjectRefusesTheDefault(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	var defaultID string
	if err := db.Pool.QueryRow(ctx, `SELECT id FROM projects WHERE is_default`).Scan(&defaultID); err != nil {
		t.Fatalf("find default: %v", err)
	}
	if err := db.DeleteProject(ctx, defaultID); !errors.Is(err, store.ErrProtected) {
		t.Fatalf("expected ErrProtected, got %v", err)
	}
}

// The foreign key backstop: this is the path a delete takes when it races the
// handler's emptiness check.
func TestPostgresDeleteProjectWithTestsIsNotEmpty(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	p := &model.Project{Name: "Occupied " + uuid.NewString()}
	db.CreateProject(ctx, p)
	tst := &model.Test{ProjectID: p.ID, Name: "t", TargetURL: "http://x", VirtualUsers: 1, DurationSeconds: 1}
	if err := db.CreateTest(ctx, tst); err != nil {
		t.Fatalf("CreateTest: %v", err)
	}

	if err := db.DeleteProject(ctx, p.ID); !errors.Is(err, store.ErrNotEmpty) {
		t.Fatalf("expected ErrNotEmpty, got %v", err)
	}
}

func TestPostgresDeleteProjectNotFound(t *testing.T) {
	db := setupDB(t)
	if err := db.DeleteProject(context.Background(), uuid.NewString()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
