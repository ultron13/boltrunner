ALTER TABLE projects ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX IF NOT EXISTS projects_one_default ON projects (is_default) WHERE is_default;

-- 0003 seeds 'Default', so an empty projects table should be unreachable. Seeding
-- anyway costs one statement and removes the failure mode outright: with no flagged
-- row, CreateTest's COALESCE yields NULL against a NOT NULL column, and every
-- project-less test creation starts failing.
INSERT INTO projects (name, is_default)
SELECT 'Default', true WHERE NOT EXISTS (SELECT 1 FROM projects);

-- Flag the project named 'Default' when there is one, else the oldest -- rather
-- than matching on the name alone, which silently flags nothing if the row was
-- ever renamed by hand. The NOT EXISTS guard makes a re-run a no-op instead of a
-- unique violation against the index above.
UPDATE projects SET is_default = true
WHERE id = (
    SELECT id FROM projects
    ORDER BY (name = 'Default') DESC, created_at ASC, id ASC
    LIMIT 1
)
AND NOT EXISTS (SELECT 1 FROM projects WHERE is_default);
