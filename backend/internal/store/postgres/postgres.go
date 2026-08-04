package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// nullableUUID converts an empty id to a SQL NULL so COALESCE can substitute a
// default; pgx would otherwise reject "" as a malformed UUID.
func nullableUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// testColumns is the projection every test read shares. catalog_id is exposed
// as the model's ID, and the family's earliest created_at is the test's
// CreatedAt, so editing a test never appears to change when it was created.
const testColumns = `catalog_id, id, version, project_id, name, target_url, virtual_users, duration_seconds,
       MIN(created_at) OVER (PARTITION BY catalog_id) AS catalog_created_at, created_at`

type testScanner interface {
	Scan(dest ...any) error
}

func scanTest(row testScanner, t *model.Test) error {
	return row.Scan(&t.ID, &t.VersionID, &t.Version, &t.ProjectID, &t.Name,
		&t.TargetURL, &t.VirtualUsers, &t.DurationSeconds, &t.CreatedAt, &t.UpdatedAt)
}

// isUniqueViolation reports whether err is SQLSTATE 23505 (unique_violation) --
// two concurrent edits racing for the same (catalog_id, version), or an
// attempt to create a project whose name is already taken.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isForeignKeyViolation reports whether err is SQLSTATE 23503
// (foreign_key_violation) -- here, a project_id that names no row in
// projects.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func (db *DB) CreateTest(ctx context.Context, t *model.Test) error {
	// Reject a malformed project_id up front. pgx would fail to encode it
	// client-side, producing an error indistinguishable by type from a genuine
	// connection or cancellation failure -- so inferring "bad input" from the
	// error type would report outages as client errors. Checking the format
	// explicitly keeps that distinction honest: everything that still fails
	// below is a real server-side problem.
	if t.ProjectID != "" {
		if _, perr := uuid.Parse(t.ProjectID); perr != nil {
			return store.ErrInvalidReference
		}
	}
	// The id is generated here so the row can reference it as its own
	// catalog_id in a single INSERT.
	id := uuid.NewString()
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO tests (id, catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id)
		 VALUES ($1, $1, 1, $2, $3, $4, $5,
		         COALESCE($6, (SELECT id FROM projects WHERE is_default)))
		 RETURNING catalog_id, id, version, project_id, created_at, created_at`,
		id, t.Name, t.TargetURL, t.VirtualUsers, t.DurationSeconds, nullableUUID(t.ProjectID),
	).Scan(&t.ID, &t.VersionID, &t.Version, &t.ProjectID, &t.CreatedAt, &t.UpdatedAt)
	if err == nil {
		return nil
	}
	// A well-formed id that names no project row: the foreign key catches it.
	if isForeignKeyViolation(err) {
		return store.ErrInvalidReference
	}
	return err
}

func (db *DB) ListTests(ctx context.Context) ([]model.Test, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT catalog_id, id, version, project_id, name, target_url, virtual_users,
		        duration_seconds, catalog_created_at, created_at
		 FROM (
		     SELECT DISTINCT ON (catalog_id) `+testColumns+`
		     FROM tests
		     ORDER BY catalog_id, version DESC
		 ) latest
		 ORDER BY catalog_created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Test{}
	for rows.Next() {
		var t model.Test
		if err := scanTest(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) ListTestsForProject(ctx context.Context, projectID string) ([]model.Test, error) {
	// Reject a malformed id before pgx tries to encode it, for the same reason
	// CreateTest does: an encode failure is indistinguishable by type from a
	// genuine connection failure, and would report bad input as an outage.
	if _, err := uuid.Parse(projectID); err != nil {
		return []model.Test{}, nil
	}
	rows, err := db.Pool.Query(ctx,
		`SELECT catalog_id, id, version, project_id, name, target_url, virtual_users,
		        duration_seconds, catalog_created_at, created_at
		 FROM (
		     SELECT DISTINCT ON (catalog_id) `+testColumns+`
		     FROM tests
		     WHERE project_id = $1
		     ORDER BY catalog_id, version DESC
		 ) latest
		 ORDER BY catalog_created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Test{}
	for rows.Next() {
		var t model.Test
		if err := scanTest(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) GetTest(ctx context.Context, catalogID string) (*model.Test, error) {
	var t model.Test
	err := scanTest(db.Pool.QueryRow(ctx,
		`SELECT `+testColumns+` FROM tests WHERE catalog_id = $1 ORDER BY version DESC LIMIT 1`,
		catalogID), &t)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &t, err
}

func (db *DB) ListTestVersions(ctx context.Context, catalogID string) ([]model.Test, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT `+testColumns+` FROM tests WHERE catalog_id = $1 ORDER BY version DESC`, catalogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Test{}
	for rows.Next() {
		var t model.Test
		if err := scanTest(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) UpdateTest(ctx context.Context, t *model.Test) error {
	latest, err := db.GetTest(ctx, t.ID)
	if err != nil {
		return err // ErrNotFound propagates
	}
	return db.updateTestAtVersion(ctx, t, latest.Version+1, latest.ProjectID, latest.CreatedAt)
}

// updateTestAtVersion inserts t as an explicit version number. The unique index
// on (catalog_id, version) is what turns a lost read-then-write race into
// ErrConflict instead of a silently forked version.
//
// familyCreatedAt is passed in rather than re-queried: the caller already read
// it, and re-reading would mean either a second round-trip or an error we
// cannot report without contradicting an INSERT that already succeeded.
func (db *DB) updateTestAtVersion(ctx context.Context, t *model.Test, version int, projectID string, familyCreatedAt time.Time) error {
	versionID := uuid.NewString()
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO tests (id, catalog_id, version, name, target_url, virtual_users, duration_seconds, project_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, version, created_at`,
		versionID, t.ID, version, t.Name, t.TargetURL, t.VirtualUsers, t.DurationSeconds, projectID,
	).Scan(&t.VersionID, &t.Version, &t.UpdatedAt)
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	if err != nil {
		return err
	}
	t.ProjectID = projectID
	t.CreatedAt = familyCreatedAt
	return nil
}

// MoveTest refiles every version of a family. project_id is not part of what a
// run executed -- jmx.Generate reads only target_url, virtual_users and
// duration_seconds -- so rewriting it across immutable version rows changes no
// historical run's meaning.
func (db *DB) MoveTest(ctx context.Context, catalogID, projectID string) error {
	// Both ids are checked before pgx encodes them, for the reason CreateTest
	// gives: an encode failure cannot be told apart from a connection failure.
	if _, err := uuid.Parse(projectID); err != nil {
		return store.ErrInvalidReference
	}
	if _, err := uuid.Parse(catalogID); err != nil {
		return store.ErrNotFound
	}
	tag, err := db.Pool.Exec(ctx, `UPDATE tests SET project_id = $2 WHERE catalog_id = $1`, catalogID, projectID)
	if isForeignKeyViolation(err) {
		return store.ErrInvalidReference
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (db *DB) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, created_at, is_default FROM projects ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Project{}
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.IsDefault); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) CreateProject(ctx context.Context, p *model.Project) error {
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO projects (name) VALUES ($1) RETURNING id, created_at, is_default`,
		p.Name,
	).Scan(&p.ID, &p.CreatedAt, &p.IsDefault)
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	return err
}

func (db *DB) RenameProject(ctx context.Context, id, name string) (*model.Project, error) {
	// Reject a malformed id before pgx tries to encode it, for the same reason
	// CreateTest does: an encode failure is indistinguishable by type from a
	// genuine connection failure, and would report bad input as an outage.
	if _, err := uuid.Parse(id); err != nil {
		return nil, store.ErrNotFound
	}
	var p model.Project
	err := db.Pool.QueryRow(ctx,
		`UPDATE projects SET name = $2 WHERE id = $1 RETURNING id, name, created_at, is_default`,
		id, name,
	).Scan(&p.ID, &p.Name, &p.CreatedAt, &p.IsDefault)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if isUniqueViolation(err) {
		return nil, store.ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (db *DB) DeleteProject(ctx context.Context, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return store.ErrNotFound
	}
	// Read the flag first so a protected project is reported as such rather
	// than as a delete that matched no rows -- DELETE ... WHERE NOT is_default
	// would collapse "not found" and "protected" into the same zero row count.
	var isDefault bool
	err := db.Pool.QueryRow(ctx, `SELECT is_default FROM projects WHERE id = $1`, id).Scan(&isDefault)
	if err == pgx.ErrNoRows {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	if isDefault {
		return store.ErrProtected
	}
	tag, err := db.Pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	// The tests.project_id foreign key is the authoritative backstop when a
	// delete races the handler's emptiness check.
	if isForeignKeyViolation(err) {
		return store.ErrNotEmpty
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (db *DB) CreateRun(ctx context.Context, r *model.Run) error {
	return db.Pool.QueryRow(ctx,
		`INSERT INTO runs (test_id, test_catalog_id, status)
		 VALUES ($1, COALESCE($2, (SELECT catalog_id FROM tests WHERE id = $1)), $3)
		 RETURNING id, test_catalog_id, created_at`,
		r.TestID, nullableUUID(r.TestCatalogID), r.Status,
	).Scan(&r.ID, &r.TestCatalogID, &r.CreatedAt)
}

func (db *DB) GetRun(ctx context.Context, id string) (*model.Run, error) {
	var r model.Run
	err := db.Pool.QueryRow(ctx,
		`SELECT id, test_id, test_catalog_id, status, created_at, started_at, completed_at, error_message
		 FROM runs WHERE id = $1`, id,
	).Scan(&r.ID, &r.TestID, &r.TestCatalogID, &r.Status, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage)
	if err == pgx.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return &r, err
}

func (db *DB) ListByTest(ctx context.Context, catalogID string) ([]model.Run, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, test_id, test_catalog_id, status, created_at, started_at, completed_at, error_message
		 FROM runs WHERE test_catalog_id = $1 ORDER BY created_at DESC`, catalogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		var r model.Run
		if err := rows.Scan(&r.ID, &r.TestID, &r.TestCatalogID, &r.Status, &r.CreatedAt, &r.StartedAt, &r.CompletedAt, &r.ErrorMessage); err != nil {
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
