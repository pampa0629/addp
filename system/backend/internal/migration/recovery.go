package migration

import (
	"context"
	"database/sql"
	"fmt"
)

const executionAudienceRepairMigrationVersion = 75

const securityModuleRepairMigrationVersion = 113

const securityProtectionAccessRequestRepairMigrationVersion = 130

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

// RepairDirtySecurityModuleMigration113 repairs only the fully rolled-back
// development failure of migration 113. It does not mark migration 113 as
// applied; it restores the runner to 112/clean so the corrected migration can
// execute normally.
func RepairDirtySecurityModuleMigration113(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("migration repair database is required")
	}
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		return fmt.Errorf("read migration catalog for migration 113 repair: %w", err)
	}
	if len(catalog.Files) < securityModuleRepairMigrationVersion ||
		catalog.Files[securityModuleRepairMigrationVersion-1].Name != "000113_iam_security_module.up.sql" {
		return fmt.Errorf("migration 113 repair requires the registered Security module migration")
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin migration 113 repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `LOCK TABLE system.schema_migrations IN ACCESS EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("lock migration state: %w", err)
	}
	var version uint
	var dirty bool
	if err := tx.QueryRowContext(ctx, `SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	if version != securityModuleRepairMigrationVersion || !dirty {
		return fmt.Errorf("migration 113 repair requires state (113, dirty), got (%d, dirty=%t)", version, dirty)
	}

	var maxChecksum uint
	var migration113ChecksumCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0), count(*) FILTER (WHERE version = 113)
		FROM system.schema_migration_checksums
	`).Scan(&maxChecksum, &migration113ChecksumCount); err != nil {
		return fmt.Errorf("read migration checksums: %w", err)
	}
	if maxChecksum != securityModuleRepairMigrationVersion-1 || migration113ChecksumCount != 0 {
		return fmt.Errorf("migration 113 repair requires checksums through 112 with no 113 record, got max=%d migration_113=%d", maxChecksum, migration113ChecksumCount)
	}

	if _, err := tx.ExecContext(ctx, `
		LOCK TABLE system.permissions, system.roles, system.service_principals,
		           system.oauth_clients, system.role_assignments IN SHARE MODE
	`); err != nil {
		return fmt.Errorf("lock migration 113 facts: %w", err)
	}

	var securityPermissionCount, runtimeRoleCount, servicePrincipalCount, oauthClientCount, runtimeAssignmentCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT
		    (SELECT count(*) FROM system.permissions WHERE owner_module = 'security'),
		    (SELECT count(*) FROM system.roles WHERE role_key = 'platform.security_runtime'),
		    (SELECT count(*) FROM system.service_principals WHERE name = 'addp-security'),
		    (SELECT count(*) FROM system.oauth_clients WHERE client_id = 'addp-security'),
		    (SELECT count(*)
		     FROM system.role_assignments assignment
		     LEFT JOIN system.roles role ON role.id = assignment.role_id
		     LEFT JOIN system.service_principals principal ON principal.id = assignment.principal_id
		     WHERE role.role_key = 'platform.security_runtime' OR principal.name = 'addp-security')
	`).Scan(&securityPermissionCount, &runtimeRoleCount, &servicePrincipalCount, &oauthClientCount, &runtimeAssignmentCount); err != nil {
		return fmt.Errorf("inspect migration 113 target facts: %w", err)
	}
	if securityPermissionCount != 0 || runtimeRoleCount != 0 || servicePrincipalCount != 0 || oauthClientCount != 0 || runtimeAssignmentCount != 0 {
		return fmt.Errorf(
			"migration 113 repair requires zero target facts, got permissions=%d roles=%d principals=%d clients=%d assignments=%d",
			securityPermissionCount, runtimeRoleCount, servicePrincipalCount, oauthClientCount, runtimeAssignmentCount,
		)
	}

	var standardPermissionCount, activeStandardPermissionCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*), count(*) FILTER (WHERE status = 'active')
		FROM system.permissions
		WHERE permission_key LIKE 'standard.classification.%'
	`).Scan(&standardPermissionCount, &activeStandardPermissionCount); err != nil {
		return fmt.Errorf("inspect pre-migration Standard classification permissions: %w", err)
	}
	if standardPermissionCount != 4 || activeStandardPermissionCount != 4 {
		return fmt.Errorf(
			"migration 113 repair requires four active Standard classification permissions, got total=%d active=%d",
			standardPermissionCount, activeStandardPermissionCount,
		)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE system.schema_migrations
		SET version = 112, dirty = false
		WHERE version = 113 AND dirty = true
	`)
	if err != nil {
		return fmt.Errorf("restore migration state to clean 112: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return fmt.Errorf("migration state changed during migration 113 repair")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration 113 repair: %w", err)
	}
	return nil
}

// RepairDirtySecurityProtectionAccessRequestMigration130 repairs only the
// fully rolled-back development failure of migration 130. It does not mark
// migration 130 as applied; it restores the runner to 129/clean so the
// corrected migration can execute normally.
func RepairDirtySecurityProtectionAccessRequestMigration130(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("migration repair database is required")
	}
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		return fmt.Errorf("read migration catalog for migration 130 repair: %w", err)
	}
	if len(catalog.Files) < securityProtectionAccessRequestRepairMigrationVersion ||
		catalog.Files[securityProtectionAccessRequestRepairMigrationVersion-1].Name != "000130_iam_security_protection_access_request.up.sql" {
		return fmt.Errorf("migration 130 repair requires the registered Security protection access request migration")
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin migration 130 repair: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `LOCK TABLE system.schema_migrations IN ACCESS EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("lock migration state: %w", err)
	}
	var version uint
	var dirty bool
	if err := tx.QueryRowContext(ctx, `SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		return fmt.Errorf("read migration state: %w", err)
	}
	if version != securityProtectionAccessRequestRepairMigrationVersion || !dirty {
		return fmt.Errorf("migration 130 repair requires state (130, dirty), got (%d, dirty=%t)", version, dirty)
	}

	var maxChecksum uint
	var migration130ChecksumCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0), count(*) FILTER (WHERE version = 130)
		FROM system.schema_migration_checksums
	`).Scan(&maxChecksum, &migration130ChecksumCount); err != nil {
		return fmt.Errorf("read migration checksums: %w", err)
	}
	if maxChecksum != securityProtectionAccessRequestRepairMigrationVersion-1 || migration130ChecksumCount != 0 {
		return fmt.Errorf("migration 130 repair requires checksums through 129 with no 130 record, got max=%d migration_130=%d", maxChecksum, migration130ChecksumCount)
	}

	if _, err := tx.ExecContext(ctx, `
		LOCK TABLE system.permissions, system.roles, system.role_permissions IN SHARE MODE
	`); err != nil {
		return fmt.Errorf("lock migration 130 facts: %w", err)
	}
	var legacyPermissionCount, activeLegacyPermissionCount, accessRequestPermissionCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT
		    count(*) FILTER (WHERE permission_key IN ('security.protection_exemption.create', 'security.protection_exemption.update')),
		    count(*) FILTER (WHERE permission_key IN ('security.protection_exemption.create', 'security.protection_exemption.update') AND status = 'active'),
		    count(*) FILTER (WHERE permission_key IN ('security.protection_access_request.create', 'security.protection_access_request.read', 'security.protection_access_request.update'))
		FROM system.permissions
	`).Scan(&legacyPermissionCount, &activeLegacyPermissionCount, &accessRequestPermissionCount); err != nil {
		return fmt.Errorf("inspect migration 130 permission facts: %w", err)
	}
	if accessRequestPermissionCount != 0 {
		return fmt.Errorf("migration 130 repair requires zero access request permissions, got %d", accessRequestPermissionCount)
	}
	if legacyPermissionCount != 2 || activeLegacyPermissionCount != 2 {
		return fmt.Errorf("migration 130 repair requires two active legacy exemption mutation permissions, got total=%d active=%d", legacyPermissionCount, activeLegacyPermissionCount)
	}

	var builtinLegacyRolePermissionCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		JOIN system.roles role ON role.id = role_permission.role_id
		WHERE role.tenant_id IS NULL
		  AND role.role_key IN ('tenant.administrator', 'tenant.governance_manager')
		  AND permission.permission_key IN ('security.protection_exemption.create', 'security.protection_exemption.update')
	`).Scan(&builtinLegacyRolePermissionCount); err != nil {
		return fmt.Errorf("inspect migration 130 legacy role permission facts: %w", err)
	}
	if builtinLegacyRolePermissionCount != 4 {
		return fmt.Errorf("migration 130 repair requires four built-in legacy exemption mutation role permissions, got %d", builtinLegacyRolePermissionCount)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE system.schema_migrations
		SET version = 129, dirty = false
		WHERE version = 130 AND dirty = true
	`)
	if err != nil {
		return fmt.Errorf("restore migration state to clean 129: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return fmt.Errorf("migration state changed during migration 130 repair")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration 130 repair: %w", err)
	}
	return nil
}
