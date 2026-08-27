package migration

import (
	"context"
	"database/sql"
	"fmt"
)

const executionAudienceRepairMigrationVersion = 75

// RepairDirtyExecutionAudienceMigration75 repairs the one published migration
// failure that attempted to normalize an immutable execution authorization
// audience. It is deliberately not a generic migration force mechanism.
func RepairDirtyExecutionAudienceMigration75(ctx context.Context, db *sql.DB) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("migration repair database is required")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, fmt.Errorf("begin migration 75 repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `LOCK TABLE system.schema_migrations IN ACCESS EXCLUSIVE MODE`); err != nil {
		return 0, fmt.Errorf("lock migration state: %w", err)
	}
	var version uint
	var dirty bool
	if err := tx.QueryRowContext(ctx, `SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		return 0, fmt.Errorf("read migration state: %w", err)
	}
	if version != executionAudienceRepairMigrationVersion || !dirty {
		return 0, fmt.Errorf("migration 75 repair requires state (75, dirty), got (%d, dirty=%t)", version, dirty)
	}
	var maxChecksum uint
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM system.schema_migration_checksums`).Scan(&maxChecksum); err != nil {
		return 0, fmt.Errorf("read migration checksums: %w", err)
	}
	if maxChecksum != executionAudienceRepairMigrationVersion-1 {
		return 0, fmt.Errorf("migration 75 repair requires checksums through 74, got %d", maxChecksum)
	}

	var legacyCount, constraintCount, triggerCount int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM system.execution_authorizations WHERE audience = 'addp-quality'`).Scan(&legacyCount); err != nil {
		return 0, fmt.Errorf("count legacy execution audiences: %w", err)
	}
	if legacyCount == 0 {
		return 0, fmt.Errorf("migration 75 repair found no legacy addp-quality authorization")
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM pg_constraint
		WHERE conrelid = 'system.execution_authorizations'::regclass
		  AND conname = 'execution_authorizations_audience_check'
		  AND pg_get_constraintdef(oid) LIKE '%addp-quality%'
	`).Scan(&constraintCount); err != nil {
		return 0, fmt.Errorf("inspect execution audience constraint: %w", err)
	}
	if constraintCount != 1 {
		return 0, fmt.Errorf("migration 75 repair requires the legacy audience constraint")
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM pg_trigger
		WHERE tgrelid = 'system.execution_authorizations'::regclass
		  AND tgname = 'trg_execution_authorizations_validate_update'
		  AND NOT tgisinternal
	`).Scan(&triggerCount); err != nil {
		return 0, fmt.Errorf("inspect execution authorization update trigger: %w", err)
	}
	if triggerCount != 1 {
		return 0, fmt.Errorf("migration 75 repair requires the immutable update trigger")
	}

	if _, err := tx.ExecContext(ctx, `LOCK TABLE system.execution_authorizations IN ACCESS EXCLUSIVE MODE`); err != nil {
		return 0, fmt.Errorf("lock execution authorizations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE system.execution_authorizations DROP CONSTRAINT execution_authorizations_audience_check`); err != nil {
		return 0, fmt.Errorf("drop legacy execution audience constraint: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER trg_execution_authorizations_validate_update ON system.execution_authorizations`); err != nil {
		return 0, fmt.Errorf("suspend immutable execution authorization trigger: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE system.execution_authorizations SET audience = 'quality' WHERE audience = 'addp-quality'`)
	if err != nil {
		return 0, fmt.Errorf("normalize legacy execution audience: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count normalized execution audiences: %w", err)
	}
	if updated != legacyCount {
		return 0, fmt.Errorf("normalized execution audience count %d does not match expected %d", updated, legacyCount)
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TRIGGER trg_execution_authorizations_validate_update
		BEFORE UPDATE ON system.execution_authorizations
		FOR EACH ROW EXECUTE FUNCTION system.validate_execution_authorization_update()
	`); err != nil {
		return 0, fmt.Errorf("restore immutable execution authorization trigger: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		ALTER TABLE system.execution_authorizations
		ADD CONSTRAINT execution_authorizations_audience_check
		CHECK (audience IN ('addp-quality', 'develop', 'duckdb', 'quality', 'service'))
	`); err != nil {
		return 0, fmt.Errorf("restore transitional execution audience constraint: %w", err)
	}
	stateResult, err := tx.ExecContext(ctx, `
		UPDATE system.schema_migrations
		SET version = 74, dirty = false
		WHERE version = 75 AND dirty = true
	`)
	if err != nil {
		return 0, fmt.Errorf("restore migration state to clean 74: %w", err)
	}
	stateRows, err := stateResult.RowsAffected()
	if err != nil || stateRows != 1 {
		return 0, fmt.Errorf("migration state changed during repair")
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit migration 75 repair: %w", err)
	}
	return updated, nil
}
