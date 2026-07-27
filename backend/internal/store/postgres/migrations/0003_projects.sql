CREATE TABLE IF NOT EXISTS projects (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO projects (name) VALUES ('Default') ON CONFLICT (name) DO NOTHING;

ALTER TABLE tests ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id);
UPDATE tests SET project_id = (SELECT id FROM projects WHERE name = 'Default')
  WHERE project_id IS NULL;
ALTER TABLE tests ALTER COLUMN project_id SET NOT NULL;
