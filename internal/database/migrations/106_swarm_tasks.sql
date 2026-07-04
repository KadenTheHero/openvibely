-- +goose Up
ALTER TABLE tasks ADD COLUMN swarm_role TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN swarm_status TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN swarm_config TEXT NOT NULL DEFAULT '{}';
ALTER TABLE tasks ADD COLUMN swarm_sequence INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_tasks_swarm_parent
  ON tasks(parent_task_id, swarm_role, swarm_sequence);

-- +goose Down
DROP INDEX IF EXISTS idx_tasks_swarm_parent;
ALTER TABLE tasks DROP COLUMN swarm_sequence;
ALTER TABLE tasks DROP COLUMN swarm_config;
ALTER TABLE tasks DROP COLUMN swarm_status;
ALTER TABLE tasks DROP COLUMN swarm_role;
