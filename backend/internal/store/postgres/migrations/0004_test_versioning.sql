ALTER TABLE tests ADD COLUMN IF NOT EXISTS catalog_id UUID;
ALTER TABLE tests ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
UPDATE tests SET catalog_id = id WHERE catalog_id IS NULL;
ALTER TABLE tests ALTER COLUMN catalog_id SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_tests_catalog_version ON tests (catalog_id, version);
CREATE INDEX IF NOT EXISTS idx_tests_catalog ON tests (catalog_id);

ALTER TABLE runs ADD COLUMN IF NOT EXISTS test_catalog_id UUID;
UPDATE runs SET test_catalog_id = t.catalog_id FROM tests t
  WHERE runs.test_id = t.id AND runs.test_catalog_id IS NULL;
ALTER TABLE runs ALTER COLUMN test_catalog_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_runs_test_catalog_id ON runs (test_catalog_id, created_at DESC);
