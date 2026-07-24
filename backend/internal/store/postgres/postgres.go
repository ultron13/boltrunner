package postgres

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

//go:embed migrations/0001_init.sql
var migration0001 string

//go:embed migrations/0002_add_run_created_at.sql
var migration0002 string

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
	if _, err := db.Pool.Exec(ctx, migration0001); err != nil {
		return err
	}
	_, err := db.Pool.Exec(ctx, migration0002)
	return err
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
	// TODO: Implement in Task 3
	return []model.Run{}, nil
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
