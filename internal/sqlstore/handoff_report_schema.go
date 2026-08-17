package sqlstore

import (
	"context"
	"fmt"
	"strings"
)

// handoffReportSchema is kept separate from builtinSchema so a disabled
// Handoff Report deployment creates no pc_handoff_report_* object at all.
var handoffReportSchema = []string{
	`CREATE TABLE IF NOT EXISTS pc_handoff_report_projects (
        project_id VARCHAR(256) NOT NULL PRIMARY KEY,
        project_key VARCHAR(64) NOT NULL UNIQUE,
        version INTEGER NOT NULL,
        catalog_state VARCHAR(16) NOT NULL,
        payload TEXT NOT NULL,
        CONSTRAINT ck_pc_handoff_report_projects_version_positive CHECK (version > 0)
    )`,
	`CREATE TABLE IF NOT EXISTS pc_handoff_report_project_revisions (
        project_id VARCHAR(256) NOT NULL,
        version INTEGER NOT NULL,
        effective_at VARCHAR(32) NOT NULL,
        payload TEXT NOT NULL,
        PRIMARY KEY (project_id, version),
        CONSTRAINT ck_pc_handoff_report_project_revisions_version_positive CHECK (version > 0)
    )`,
	`CREATE INDEX IF NOT EXISTS ix_pc_handoff_report_project_revisions_effective_at
        ON pc_handoff_report_project_revisions (project_id, effective_at, version)`,
	`CREATE TABLE IF NOT EXISTS pc_handoff_report_workstreams (
        scope_id VARCHAR(256) NOT NULL PRIMARY KEY,
        project_id VARCHAR(256) NOT NULL,
        workstream_key VARCHAR(64),
        version INTEGER NOT NULL,
        catalog_state VARCHAR(16) NOT NULL,
        payload TEXT NOT NULL,
        CONSTRAINT uq_pc_handoff_report_workstreams_project_key UNIQUE (project_id, workstream_key),
        CONSTRAINT ck_pc_handoff_report_workstreams_version_positive CHECK (version > 0)
    )`,
	`CREATE INDEX IF NOT EXISTS ix_pc_handoff_report_workstreams_project_scope
        ON pc_handoff_report_workstreams (project_id, scope_id)`,
	`CREATE TABLE IF NOT EXISTS pc_handoff_report_workstream_revisions (
        scope_id VARCHAR(256) NOT NULL,
        version INTEGER NOT NULL,
        project_id VARCHAR(256) NOT NULL,
        effective_at VARCHAR(32) NOT NULL,
        payload TEXT NOT NULL,
        PRIMARY KEY (scope_id, version),
        CONSTRAINT ck_pc_handoff_report_workstream_revisions_version_positive CHECK (version > 0)
    )`,
	`CREATE INDEX IF NOT EXISTS ix_pc_handoff_report_workstream_revisions_effective_at
        ON pc_handoff_report_workstream_revisions (scope_id, effective_at, version)`,
	`CREATE TABLE IF NOT EXISTS pc_handoff_report_workspace_bindings (
        workspace_instance_id VARCHAR(256) NOT NULL PRIMARY KEY,
        project_id VARCHAR(256) NOT NULL,
        provider VARCHAR(32) NOT NULL,
        repository_id VARCHAR(256),
        normalized_remote VARCHAR(2048),
        subpath VARCHAR(1024),
        state VARCHAR(16) NOT NULL,
        confirmed_at VARCHAR(32) NOT NULL,
        version INTEGER NOT NULL,
        payload TEXT NOT NULL,
        CONSTRAINT ck_pc_handoff_report_workspace_bindings_version_positive CHECK (version > 0)
    )`,
	`CREATE TABLE IF NOT EXISTS pc_handoff_report_activity_heads (
        project_id VARCHAR(256) NOT NULL PRIMARY KEY,
        cursor BIGINT NOT NULL
    )`,
	`CREATE TABLE IF NOT EXISTS pc_handoff_report_activities (
        project_id VARCHAR(256) NOT NULL,
        cursor BIGINT NOT NULL,
        event_id VARCHAR(256) NOT NULL UNIQUE,
        scope_id VARCHAR(256),
        source VARCHAR(64) NOT NULL,
        source_event_id VARCHAR(256) NOT NULL,
        occurred_at VARCHAR(32),
        observed_at VARCHAR(32) NOT NULL,
        period_at VARCHAR(32),
        time_basis VARCHAR(32) NOT NULL,
        payload TEXT NOT NULL,
        PRIMARY KEY (project_id, cursor),
        CONSTRAINT uq_pc_handoff_report_activities_source_event UNIQUE (source, source_event_id)
    )`,
	`CREATE INDEX IF NOT EXISTS ix_pc_handoff_report_activities_project_period
        ON pc_handoff_report_activities (project_id, period_at, cursor)`,
	`CREATE INDEX IF NOT EXISTS ix_pc_handoff_report_activities_project_source_cursor
        ON pc_handoff_report_activities (project_id, source, cursor)`,
}

// EnsureHandoffReportSchema creates the frozen optional schema. Callers must
// invoke it only after the feature has been enabled.
func EnsureHandoffReportSchema(ctx context.Context, db DBTX) error {
	return EnsureHandoffReportSchemaForDialect(ctx, db, SQLiteDialect)
}

func EnsureHandoffReportSchemaForDialect(ctx context.Context, db DBTX, dialect Dialect) error {
	if dialect != SQLiteDialect && dialect != MySQLDialect {
		return fmt.Errorf("sqlstore: unsupported Handoff Report schema dialect %q", dialect)
	}
	for _, statement := range handoffReportSchema {
		if dialect == MySQLDialect && strings.HasPrefix(strings.TrimSpace(statement), "CREATE INDEX IF NOT EXISTS ") {
			fields := strings.Fields(statement)
			if len(fields) < 8 {
				return fmt.Errorf("sqlstore: invalid Handoff Report index statement")
			}
			indexName, tableName := fields[5], fields[7]
			var count int64
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.statistics
                WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
				tableName, indexName).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				continue
			}
			statement = strings.Replace(statement, "INDEX IF NOT EXISTS", "INDEX", 1)
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
