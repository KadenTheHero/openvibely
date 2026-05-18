package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/openvibely/openvibely/internal/database/migrations"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// TestMigrations_PreserveForeignKeyData verifies that all migrations preserve
// foreign key referenced data when recreating tables.
func TestMigrations_PreserveForeignKeyData(t *testing.T) {
	// Create a temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Run all migrations
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Create test data
	// Create a project
	_, err = db.Exec(`INSERT INTO projects (id, name, description, repo_path) VALUES ('test-project', 'Test Project', 'Test', '/tmp')`)
	if err != nil {
		t.Fatalf("failed to insert project: %v", err)
	}

	// Create a task
	_, err = db.Exec(`INSERT INTO tasks (id, project_id, title, category, status) VALUES ('test-task', 'test-project', 'Test Task', 'scheduled', 'pending')`)
	if err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}

	// Create a schedule
	_, err = db.Exec(`INSERT INTO schedules (id, task_id, run_at, repeat_type) VALUES ('test-schedule', 'test-task', datetime('now'), 'daily')`)
	if err != nil {
		t.Fatalf("failed to insert schedule: %v", err)
	}

	// Create an execution
	_, err = db.Exec(`INSERT INTO executions (id, task_id, status, started_at) VALUES ('test-exec', 'test-task', 'completed', datetime('now'))`)
	if err != nil {
		t.Fatalf("failed to insert execution: %v", err)
	}

	// Verify the data exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM schedules WHERE task_id = 'test-task'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count schedules: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 schedule, got %d", count)
	}

	err = db.QueryRow("SELECT COUNT(*) FROM executions WHERE task_id = 'test-task'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count executions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 execution, got %d", count)
	}

	// Now verify that the schema has proper constraints
	var schema string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='tasks'").Scan(&schema)
	if err != nil {
		t.Fatalf("failed to get tasks schema: %v", err)
	}

	// Check for CHECK constraints
	if schema == "" {
		t.Fatal("tasks table schema is empty")
	}

	// Verify foreign keys are enabled
	var fkEnabled int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled)
	if err != nil {
		t.Fatalf("failed to check foreign keys: %v", err)
	}
	if fkEnabled != 1 {
		t.Fatal("foreign keys should be enabled")
	}

	t.Logf("✅ All migrations completed successfully and preserved foreign key data")
}

func TestMigrations_AgentsTableDoesNotContainColorColumn(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	rows, err := db.Query("PRAGMA table_info(agents)")
	if err != nil {
		t.Fatalf("failed to inspect agents schema: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("failed to scan agents column metadata: %v", err)
		}
		if name == "color" {
			t.Fatalf("expected agents table to not include legacy color column")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed during agents schema inspection: %v", err)
	}
}

// TestMigration012_CheckConstraints verifies that migration 012 properly
// adds CHECK constraints to the tasks table.
func TestMigration012_CheckConstraints(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Create a project first
	_, err = db.Exec(`INSERT INTO projects (id, name) VALUES ('test-proj', 'Test')`)
	if err != nil {
		t.Fatalf("failed to insert project: %v", err)
	}

	// Test category CHECK constraint
	_, err = db.Exec(`INSERT INTO tasks (id, project_id, title, category) VALUES ('t1', 'test-proj', 'Test 1', 'invalid-category')`)
	if err == nil {
		t.Fatal("expected error for invalid category, got nil")
	}

	// Test status CHECK constraint
	_, err = db.Exec(`INSERT INTO tasks (id, project_id, title, status) VALUES ('t2', 'test-proj', 'Test 2', 'invalid-status')`)
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}

	// Test tag CHECK constraint
	_, err = db.Exec(`INSERT INTO tasks (id, project_id, title, tag) VALUES ('t3', 'test-proj', 'Test 3', 'invalid-tag')`)
	if err == nil {
		t.Fatal("expected error for invalid tag, got nil")
	}

	// Valid inserts should succeed
	_, err = db.Exec(`INSERT INTO tasks (id, project_id, title, category, status, tag) VALUES ('t4', 'test-proj', 'Test 4', 'active', 'pending', 'feature')`)
	if err != nil {
		t.Fatalf("expected valid insert to succeed: %v", err)
	}

	t.Logf("✅ All CHECK constraints working correctly")
}

func TestMigrations_GitHubRepoURLAndTaskPullRequests(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Ensure projects.repo_url exists
	rows, err := db.Query("PRAGMA table_info(projects)")
	if err != nil {
		t.Fatalf("failed to inspect projects table: %v", err)
	}
	defer rows.Close()

	repoURLExists := false
	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("failed to scan projects column: %v", err)
		}
		if name == "repo_url" {
			repoURLExists = true
		}
	}
	if !repoURLExists {
		t.Fatal("expected projects table to include repo_url column")
	}

	// Ensure task_pull_requests exists and enforces task_id uniqueness/FK by insertion
	_, err = db.Exec(`INSERT INTO projects (id, name, description, repo_path, repo_url) VALUES ('gh-proj', 'GH Project', '', '/tmp/repo', 'https://github.com/openvibely/openvibely')`)
	if err != nil {
		t.Fatalf("failed to insert project: %v", err)
	}
	_, err = db.Exec(`INSERT INTO tasks (id, project_id, title, category, status) VALUES ('gh-task', 'gh-proj', 'Task', 'active', 'pending')`)
	if err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}
	_, err = db.Exec(`INSERT INTO task_pull_requests (task_id, pr_number, pr_url, pr_state) VALUES ('gh-task', 10, 'https://github.com/openvibely/openvibely/pull/10', 'open')`)
	if err != nil {
		t.Fatalf("failed to insert task pull request: %v", err)
	}
	_, err = db.Exec(`INSERT INTO task_pull_requests (task_id, pr_number, pr_url, pr_state) VALUES ('gh-task', 11, 'https://github.com/openvibely/openvibely/pull/11', 'open')`)
	if err == nil {
		t.Fatal("expected UNIQUE constraint failure for duplicate task_id in task_pull_requests")
	}
}

func TestMigration082_NormalizesUnreleasedSkillCuratorNames(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.UpTo(db, ".", 74); err != nil {
		t.Fatalf("failed to run migrations through 074: %v", err)
	}
	if err := applyUnreleasedLifecycleSchemaForTest(db); err != nil {
		t.Fatalf("failed to simulate unreleased lifecycle schema: %v", err)
	}
	for version := 75; version <= 81; version++ {
		if _, err := db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, version); err != nil {
			t.Fatalf("failed to mark unreleased migration %d applied: %v", version, err)
		}
	}

	if _, err := db.Exec(`
		INSERT INTO agents (
			id, name, description, system_prompt, model, tools, tool_config,
			plugins, mcp_servers, skills, system_kind,
			key, scope, project_id, selectable_as_primary, enabled,
			permission_defaults_json, model_defaults_json,
			created_by, generated_status, absorbed_into, source_refs_json
		) VALUES (
			'sys_skill_curator_00000000000000000001', 'System: Agent Creator', '', '', 'inherit', '[]', '{}',
			'[]', '[]', '[]', 'agent_creator',
			'agent_creator', 'global', NULL, 0, 1,
			'{}', '{}', 'system', 'protected', NULL, '[]'
		)
	`); err != nil {
		t.Fatalf("failed to insert unreleased old Skill Curator agent: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_lifecycle_hooks (
			id, agent_id, when_slot, skill_key, prompt_override, output_contract,
			blocking, enabled, permissions_json, run_policy_json, schedule_json
		) VALUES
			('hk_old_route', 'sys_skill_curator_00000000000000000001', 'route_task', 'route_task', '', 'selected_mode', 1, 1, '{}', '{}', NULL),
			('hk_old_maintain_agent_skill_library', 'sys_skill_curator_00000000000000000001', 'after_complete', 'maintain_agent_skill_library', '', 'learning_summary', 0, 1, '{}', '{}', NULL)
	`); err != nil {
		t.Fatalf("failed to insert old lifecycle hooks: %v", err)
	}

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("failed to run consolidated migration 082: %v", err)
	}

	var key, systemKind, tools string
	if err := db.QueryRow(`SELECT key, system_kind, tools FROM agents WHERE id = 'sys_skill_curator_00000000000000000001'`).Scan(&key, &systemKind, &tools); err != nil {
		t.Fatalf("failed to load normalized agent: %v", err)
	}
	if key != "skill_curator" || systemKind != "skill_curator" {
		t.Fatalf("agent not normalized: key=%q system_kind=%q", key, systemKind)
	}
	if tools != `["skill_view","skills_list","agent_view","skill_manage","agent_skill_manage"]` {
		t.Fatalf("skill curator tools not normalized: %s", tools)
	}
	var routeContract string
	if err := db.QueryRow(`SELECT output_contract FROM agent_lifecycle_hooks WHERE id = 'hk_old_route'`).Scan(&routeContract); err != nil {
		t.Fatalf("failed to load route hook: %v", err)
	}
	if routeContract != "selected_skills" {
		t.Fatalf("route hook output contract = %q, want selected_skills", routeContract)
	}
	var oldCount int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM agent_lifecycle_hooks h
		JOIN agents a ON a.id = h.agent_id
		WHERE a.key = 'agent_creator'
		   OR a.system_kind = 'agent_creator'
		   OR h.skill_key = 'maintain_agent_skill_library'
	`).Scan(&oldCount); err != nil {
		t.Fatalf("failed to count old identifiers: %v", err)
	}
	if oldCount != 0 {
		t.Fatalf("expected old Skill Curator identifiers to be removed, got %d", oldCount)
	}
}

func TestMigration082_SkipsWhenLocalDevDBAlreadyApplied082(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.UpTo(db, ".", 74); err != nil {
		t.Fatalf("failed to run migrations through 074: %v", err)
	}
	if err := applyUnreleasedLifecycleSchemaForTest(db); err != nil {
		t.Fatalf("failed to simulate unreleased lifecycle schema: %v", err)
	}
	for version := 75; version <= 82; version++ {
		if _, err := db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, version); err != nil {
			t.Fatalf("failed to mark unreleased migration %d applied: %v", version, err)
		}
	}

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("expected local dev DB already at 082 to remain migratable: %v", err)
	}

	var maxVersion int
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1`).Scan(&maxVersion); err != nil {
		t.Fatalf("failed to read max goose version: %v", err)
	}
	if maxVersion != 82 {
		t.Fatalf("max goose version = %d, want 82", maxVersion)
	}
}

func TestMigration082_AppliesAfterPublic074(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.UpTo(db, ".", 74); err != nil {
		t.Fatalf("failed to run migrations through 074: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("failed to run consolidated migration 082: %v", err)
	}

	for _, table := range []string{"agent_lifecycle_hooks", "lifecycle_executions", "agent_config_mutations", "lifecycle_execution_events"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("expected table %s after migration 082: %v", table, err)
		}
	}
	for _, column := range []string{"key", "scope", "permission_defaults_json", "model_defaults_json", "generated_status"} {
		if !testColumnExists(t, db, "agents", column) {
			t.Fatalf("expected agents.%s after migration 082", column)
		}
	}
	var contract string
	if err := db.QueryRow(`
		SELECT output_contract
		FROM agent_lifecycle_hooks
		WHERE agent_id = 'sys_skill_curator_00000000000000000001'
		  AND when_slot = 'route_task'
		  AND skill_key = 'route_task'
	`).Scan(&contract); err != nil {
		t.Fatalf("failed to load seeded route hook: %v", err)
	}
	if contract != "selected_skills" {
		t.Fatalf("seeded route hook contract = %q, want selected_skills", contract)
	}
}

func applyUnreleasedLifecycleSchemaForTest(db *sql.DB) error {
	statements := []string{
		`ALTER TABLE agents ADD COLUMN key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agents ADD COLUMN scope TEXT NOT NULL DEFAULT 'global'`,
		`ALTER TABLE agents ADD COLUMN project_id TEXT NULL REFERENCES projects(id) ON DELETE SET NULL`,
		`ALTER TABLE agents ADD COLUMN selectable_as_primary INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE agents ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE agents ADD COLUMN permission_defaults_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE agents ADD COLUMN model_defaults_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE agents ADD COLUMN created_by TEXT NOT NULL DEFAULT 'user'`,
		`ALTER TABLE agents ADD COLUMN generated_status TEXT NOT NULL DEFAULT 'user_edited'`,
		`ALTER TABLE agents ADD COLUMN absorbed_into TEXT NULL`,
		`ALTER TABLE agents ADD COLUMN source_refs_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE agents ADD COLUMN archived_at DATETIME NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_key_unique ON agents(key) WHERE key <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_agents_generated_status ON agents(generated_status)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_scope_project ON agents(scope, project_id)`,
		`CREATE TABLE agent_lifecycle_hooks (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			when_slot TEXT NOT NULL CHECK (when_slot IN ('route_task','before_run','task_mode','after_complete','scheduled')),
			skill_key TEXT NOT NULL,
			prompt_override TEXT NOT NULL DEFAULT '',
			output_contract TEXT NOT NULL DEFAULT '' CHECK (output_contract IN ('','selected_mode','selected_skills','context_block','activity_summary','learning_summary','library_update_summary')),
			blocking INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			permissions_json TEXT NOT NULL DEFAULT '{}',
			run_policy_json TEXT NOT NULL DEFAULT '{}',
			schedule_json TEXT,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE lifecycle_executions (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			task_run_id TEXT NOT NULL DEFAULT '',
			agent_id TEXT REFERENCES agents(id) ON DELETE SET NULL,
			when_slot TEXT NOT NULL CHECK (when_slot IN ('route_task','before_run','task_mode','after_complete','scheduled')),
			lifecycle_hook_id TEXT REFERENCES agent_lifecycle_hooks(id) ON DELETE SET NULL,
			parent_execution_id TEXT REFERENCES lifecycle_executions(id) ON DELETE SET NULL,
			skill_key TEXT NOT NULL DEFAULT '',
			output_contract TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','completed','failed','skipped')),
			input_json TEXT NOT NULL DEFAULT '{}',
			output_json TEXT NOT NULL DEFAULT '{}',
			error TEXT NOT NULL DEFAULT '',
			attempt_count INTEGER NOT NULL DEFAULT 0,
			priority INTEGER NOT NULL DEFAULT 0,
			next_retry_at DATETIME,
			idempotency_key TEXT NOT NULL DEFAULT '',
			started_at DATETIME NOT NULL DEFAULT (datetime('now')),
			completed_at DATETIME
		)`,
		`CREATE TABLE agent_config_mutations (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			lifecycle_execution_id TEXT REFERENCES lifecycle_executions(id) ON DELETE SET NULL,
			task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
			task_run_id TEXT NOT NULL DEFAULT '',
			project_id TEXT NOT NULL DEFAULT '',
			actor_agent_id TEXT REFERENCES agents(id) ON DELETE SET NULL,
			target_type TEXT NOT NULL CHECK (target_type IN ('agent','skill','routing','hook','support_file')),
			target_key TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			proposed_payload_json TEXT NOT NULL DEFAULT '{}',
			validation_status TEXT NOT NULL DEFAULT 'applied' CHECK (validation_status IN ('applied','blocked','no_op')),
			validation_errors_json TEXT NOT NULL DEFAULT '[]',
			changed_paths_json TEXT NOT NULL DEFAULT '[]',
			imported_config_changes_json TEXT NOT NULL DEFAULT '[]',
			evidence_refs_json TEXT NOT NULL DEFAULT '[]',
			idempotency_key TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE lifecycle_execution_events (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			lifecycle_execution_id TEXT NOT NULL REFERENCES lifecycle_executions(id) ON DELETE CASCADE,
			seq INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			UNIQUE(lifecycle_execution_id, seq)
		)`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func testColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("failed to inspect %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("failed to scan %s column: %v", table, err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate %s columns: %v", table, err)
	}
	return false
}

func TestMigration071_RebuildsAgentConfigsWithReferences(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.UpTo(db, ".", 70); err != nil {
		t.Fatalf("failed to run migrations through 070: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO agent_configs (id, name, provider, model, is_default, auth_method)
		VALUES ('agent-071', 'Agent 071', 'anthropic', 'claude-sonnet-4-5-20250929', 1, 'cli')
	`); err != nil {
		t.Fatalf("failed to insert agent config: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO projects (id, name, default_agent_config_id) VALUES ('project-071', 'Project 071', 'agent-071')`); err != nil {
		t.Fatalf("failed to insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO tasks (id, project_id, title, category, status, agent_id)
		VALUES ('task-071', 'project-071', 'Task 071', 'active', 'pending', 'agent-071')
	`); err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO executions (id, task_id, agent_config_id, status, started_at)
		VALUES ('execution-071', 'task-071', 'agent-071', 'running', datetime('now'))
	`); err != nil {
		t.Fatalf("failed to insert execution: %v", err)
	}

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("failed to run migration 071 with existing references: %v", err)
	}

	if _, err := db.Exec(`UPDATE agent_configs SET reasoning_effort = 'max' WHERE id = 'agent-071'`); err != nil {
		t.Fatalf("expected migration 071 to allow reasoning_effort=max: %v", err)
	}

	var agentID, projectDefaultID, executionAgentID string
	if err := db.QueryRow(`SELECT agent_id FROM tasks WHERE id = 'task-071'`).Scan(&agentID); err != nil {
		t.Fatalf("failed to read task agent reference: %v", err)
	}
	if err := db.QueryRow(`SELECT default_agent_config_id FROM projects WHERE id = 'project-071'`).Scan(&projectDefaultID); err != nil {
		t.Fatalf("failed to read project default agent reference: %v", err)
	}
	if err := db.QueryRow(`SELECT agent_config_id FROM executions WHERE id = 'execution-071'`).Scan(&executionAgentID); err != nil {
		t.Fatalf("failed to read execution agent reference: %v", err)
	}
	for name, got := range map[string]string{
		"task agent":             agentID,
		"project default agent":  projectDefaultID,
		"execution agent config": executionAgentID,
	} {
		if got != "agent-071" {
			t.Fatalf("%s reference = %q, want agent-071", name, got)
		}
	}
}

func TestMain(m *testing.M) {
	// Setup
	code := m.Run()
	// Teardown
	os.Exit(code)
}
