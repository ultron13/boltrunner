package postgres

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// schema_migrations is created outside the numbered migrations because it is
// what tracks them. Existing databases have 0001/0002 applied but unrecorded;
// both are idempotent (IF NOT EXISTS / ADD COLUMN IF NOT EXISTS), so the first
// run after this change re-applies them harmlessly and records them.
const createSchemaMigrations = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

type DB struct {
	Pool *pgxpool.Pool
}

func Connect(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	db.Pool.Close()
}

func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.Pool.Exec(ctx, createSchemaMigrations); err != nil {
		return err
	}
	names, err := migrationFilenames()
	if err != nil {
		return err
	}
	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		applied, err := db.migrationApplied(ctx, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err := db.applyMigration(ctx, version, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

// migrationFilenames returns every embedded .sql migration in ascending
// filename order, which is also version order given the NNNN_ prefix.
func migrationFilenames() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// migrationVersion parses the leading integer of a migration filename, so
// "0003_projects.sql" yields 3.
func migrationVersion(name string) (int, error) {
	i := strings.IndexByte(name, '_')
	if i <= 0 {
		return 0, fmt.Errorf("migration %q must be named <version>_<description>.sql", name)
	}
	v, err := strconv.Atoi(name[:i])
	if err != nil {
		return 0, fmt.Errorf("migration %q has a non-numeric version prefix: %w", name, err)
	}
	return v, nil
}

func (db *DB) migrationApplied(ctx context.Context, version int) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
	).Scan(&exists)
	return exists, err
}

// applyMigration runs one migration and records it in the same transaction, so
// a partially applied migration can never be marked as done.
func (db *DB) applyMigration(ctx context.Context, version int, body string) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, body); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (db *DB) CreateTest(ctx context.Context, t *model.Test) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO tests (name, target_url, virtual_users, duration_seconds)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		t.Name, t.TargetURL, t.VirtualUsers, t.DurationSeconds,
	).Scan(&t.ID, &t.CreatedAt)
}

func (db *DB) ListTests(ctx context.Context) ([]model.Test, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, target_url, virtual_users, duration_seconds, created_at FROM tests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Test{}
	for rows.Next() {
		var t model.Test
		if err := rows.Scan(&t.ID, &t.Name, &t.TargetURL, &t.VirtualUsers, &t.DurationSeconds, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) GetTest(ctx context.Context, id string) (*model.Test, error) {
	var t model.Test
	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, target_url, virtual_users, duration_seconds, created_at FROM tests WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.TargetURL, &t.VirtualUsers, &t.DurationSeconds, &t.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &t, err
}

func (db *DB) CreateRun(ctx context.Context, r *model.Run) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO runs (test_id, status) VALUES ($1, $2) RETURNING id, created_at`,
		r.TestID, r.Status,
	).Scan(&r.ID, &r.CreatedAt)
}

func (db *DB) GetRun(ctx context.Context, id string) (*model.Run, error) {
	var r model.Run
	err := db.Pool.QueryRow(ctx,
		`SELECT id, test_id, status, created_at, started_at, completed_at, error_message FROM runs WHERE id = $1`, id,
	).Scan(&r.ID, &r.TestID, &r.Status, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &r, err
}

func (db *DB) ListByTest(ctx context.Context, testID string) ([]model.Run, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, test_id, status, created_at, started_at, completed_at, error_message
		 FROM runs WHERE test_id = $1 ORDER BY created_at DESC`, testID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		var r model.Run
		if err := rows.Scan(&r.ID, &r.TestID, &r.Status, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) UpdateRunStatus(ctx context.Context, id string, status model.RunStatus, errMsg string) error {
	var startedAtExpr, completedAtExpr string
	switch status {
	case model.RunRunning:
		startedAtExpr = `, started_at = COALESCE(started_at, now())`
	case model.RunCompleted, model.RunFailed, model.RunStopped:
		completedAtExpr = `, completed_at = now()`
	}
	tag, err := db.Pool.Exec(ctx,
		`UPDATE runs SET status = $1, error_message = $2`+startedAtExpr+completedAtExpr+` WHERE id = $3`,
		status, errMsg, id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (db *DB) AppendMetricSnapshot(ctx context.Context, s *model.RunMetricSnapshot) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO run_metric_snapshots (run_id, ts, elapsed_seconds, throughput_rps, avg_response_time_ms, error_rate_pct, sample_count)
		 VALUES ($1, now(), $2, $3, $4, $5, $6) RETURNING id, ts`,
		s.RunID, s.ElapsedSeconds, s.ThroughputRPS, s.AvgResponseTimeMs, s.ErrorRatePct, s.SampleCount,
	).Scan(&s.ID, &s.Ts)
}

func (db *DB) LatestSnapshot(ctx context.Context, runID string) (*model.RunMetricSnapshot, error) {
	var s model.RunMetricSnapshot
	err := db.Pool.QueryRow(ctx,
		`SELECT id, run_id, ts, elapsed_seconds, throughput_rps, avg_response_time_ms, error_rate_pct, sample_count
		 FROM run_metric_snapshots WHERE run_id = $1 ORDER BY ts DESC LIMIT 1`, runID,
	).Scan(&s.ID, &s.RunID, &s.Ts, &s.ElapsedSeconds, &s.ThroughputRPS, &s.AvgResponseTimeMs, &s.ErrorRatePct, &s.SampleCount)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &s, err
}

func (db *DB) ListSnapshots(ctx context.Context, runID string) ([]model.RunMetricSnapshot, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, run_id, ts, elapsed_seconds, throughput_rps, avg_response_time_ms, error_rate_pct, sample_count
		 FROM run_metric_snapshots WHERE run_id = $1 ORDER BY ts ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.RunMetricSnapshot{}
	for rows.Next() {
		var s model.RunMetricSnapshot
		if err := rows.Scan(&s.ID, &s.RunID, &s.Ts, &s.ElapsedSeconds, &s.ThroughputRPS, &s.AvgResponseTimeMs, &s.ErrorRatePct, &s.SampleCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
