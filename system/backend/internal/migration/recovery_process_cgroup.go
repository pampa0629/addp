package migration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Only the exact pre-240 constraint can demonstrate the failed DDL rolled back.
const processMetricsConstraintBefore240 = `CHECK (((process_metrics IS NULL) OR (((jsonb_typeof(process_metrics) = 'object'::text) AND (process_metrics ?& ARRAY['schema_version'::text, 'endpoint'::text]) AND (((process_metrics - 'schema_version'::text) - 'endpoint'::text) = '{}'::jsonb) AND ((process_metrics ->> 'schema_version'::text) = 'addp.process-metrics/v1'::text) AND (jsonb_typeof((process_metrics -> 'endpoint'::text)) = 'string'::text) AND ((length((process_metrics ->> 'endpoint'::text)) >= 1) AND (length((process_metrics ->> 'endpoint'::text)) <= 512)) AND (process_started_at IS NOT NULL)) IS TRUE)))`

const processMetricsImmutableBody = `BEGIN
 IF NEW.process_metrics IS DISTINCT FROM OLD.process_metrics
 OR (OLD.process_metrics IS NOT NULL AND (
 NEW.role IS DISTINCT FROM OLD.role OR NEW.process_started_at IS DISTINCT FROM OLD.process_started_at
 )) THEN
 RAISE EXCEPTION 'process metrics declaration is immutable' USING ERRCODE = '23514';
 END IF;
 RETURN NEW;
END;`

// RepairDirtyProcessCgroupMigration240 restores only a fully rolled-back 240
// marker. It never applies DDL, changes declarations, or records a checksum.
func RepairDirtyProcessCgroupMigration240(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("migration repair database is required")
	}
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		return err
	}
	if len(catalog.Files) < 240 || catalog.Files[239].Name != "000240_process_cgroup_identity.up.sql" {
		return fmt.Errorf("migration 240 repair requires its registered source")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `LOCK TABLE system.schema_migrations IN ACCESS EXCLUSIVE MODE; LOCK TABLE system.schema_migration_checksums IN SHARE MODE; LOCK TABLE system.module_runtime_instances IN SHARE MODE`); err != nil {
		return fmt.Errorf("lock migration 240 facts: %w", err)
	}
	var count int
	var version uint
	var dirty bool
	if err = tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(min(version),0),COALESCE(bool_and(dirty),false) FROM system.schema_migrations`).Scan(&count, &version, &dirty); err != nil {
		return err
	}
	if count != 1 || version != 240 || !dirty {
		return fmt.Errorf("migration 240 repair requires exactly (240, dirty), got count=%d version=%d dirty=%t", count, version, dirty)
	}
	rows, err := tx.QueryContext(ctx, `SELECT version,filename,sha256 FROM system.schema_migration_checksums ORDER BY version`)
	if err != nil {
		return err
	}
	recorded, err := scanRecordedMigrationChecksums(rows)
	if err != nil {
		return err
	}
	if err = compareMigrationChecksums(catalog, 239, recorded, true); err != nil {
		return fmt.Errorf("migration 240 repair checksum precondition: %w", err)
	}
	var definition string
	var validated bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_get_constraintdef(oid),convalidated FROM pg_constraint WHERE conrelid=to_regclass('system.module_runtime_instances') AND conname='process_metrics_declaration_shape' AND contype='c'`).Scan(&definition, &validated); err != nil {
		return fmt.Errorf("read pre-240 declaration constraint: %w", err)
	}
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	if !validated || normalize(definition) != normalize(processMetricsConstraintBefore240) {
		return fmt.Errorf("migration 240 repair requires the exact validated pre-240 declaration constraint")
	}
	var body string
	if err = tx.QueryRowContext(ctx, `SELECT p.prosrc FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid WHERE t.tgrelid=to_regclass('system.module_runtime_instances') AND t.tgname='process_metrics_declaration_immutable' AND NOT t.tgisinternal AND t.tgenabled='O' AND NOT p.prosecdef AND p.prorettype='trigger'::regtype AND p.pronamespace='system'::regnamespace AND p.proname='guard_process_metrics_declaration'`).Scan(&body); err != nil {
		return fmt.Errorf("read declaration immutability guard: %w", err)
	}
	if normalize(body) != normalize(processMetricsImmutableBody) {
		return fmt.Errorf("migration 240 repair requires the unchanged declaration immutability guard")
	}
	result, err := tx.ExecContext(ctx, `UPDATE system.schema_migrations SET version=239,dirty=false WHERE version=240 AND dirty`)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return fmt.Errorf("migration state changed during migration 240 repair")
	}
	return tx.Commit()
}
