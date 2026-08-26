package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestManagerTransferRuntimeForwardMigrationsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Manager Transfer runtime migration schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	through48, _ := migrationFilesBeforeAndThrough(t, "000049_query_parameter_capabilities.up.sql")
	through65, through66 := migrationFilesBeforeAndThrough(t, "000066_iam_manager_transfer_runtime.up.sql")
	_, through67 := migrationFilesBeforeAndThrough(t, "000067_iam_manager_transfer_read.up.sql")
	if err := (&Runner{DSN: dsn, FS: through48, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 48: %v", err)
	}
	if err := (&Runner{DSN: dsn, FS: through65, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 65: %v", err)
	}

	countManagerTransferPermissions := func() int {
		t.Helper()
		var count int
		if err := db.QueryRow(`
			SELECT count(*)
			FROM system.role_permissions AS role_permission
			JOIN system.roles AS role ON role.id = role_permission.role_id
			JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
			WHERE role.tenant_id IS NULL
			  AND role.role_key = 'tenant.manager_runtime'
			  AND permission.permission_key IN ('transfer.task.create', 'transfer.task.execute', 'transfer.task.read')
		`).Scan(&count); err != nil {
			t.Fatalf("count Manager Transfer runtime permissions: %v", err)
		}
		return count
	}
	if count := countManagerTransferPermissions(); count != 0 {
		t.Fatalf("Manager Transfer runtime permissions before migration 66 = %d, want 0", count)
	}

	if err := (&Runner{DSN: dsn, FS: through66, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply Manager Transfer runtime migration 66: %v", err)
	}
	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration 66 version: %v", err)
	}
	if count := countManagerTransferPermissions(); version != 66 || dirty || count != 2 {
		t.Fatalf("migration 66 state=(%d,%t) Manager Transfer runtime permissions=%d", version, dirty, count)
	}

	if err := (&Runner{DSN: dsn, FS: through67, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply Manager Transfer read migration 67: %v", err)
	}
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration 67 version: %v", err)
	}
	if count := countManagerTransferPermissions(); version != 67 || dirty || count != 3 {
		t.Fatalf("migration 67 state=(%d,%t) Manager Transfer runtime permissions=%d", version, dirty, count)
	}
}

func TestStandardReferenceRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Standard reference runtime migration schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	through48, _ := migrationFilesBeforeAndThrough(t, "000049_query_parameter_capabilities.up.sql")
	through64, through65 := migrationFilesBeforeAndThrough(t, "000065_iam_standard_reference_runtime.up.sql")
	if err := (&Runner{DSN: dsn, FS: through48, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 48: %v", err)
	}
	if err := (&Runner{DSN: dsn, FS: through64, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 64: %v", err)
	}
	_, tenantID := seedInitializedMigrationTenant(t, db, "standard-runtime-migration", "Standard Runtime Migration")

	if err := (&Runner{DSN: dsn, FS: through65, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply Standard reference runtime migration 65: %v", err)
	}

	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration 65 version: %v", err)
	}
	var permissionCount, rolePermissionCount, membershipCount, assignmentCount int
	if err := db.QueryRow(`
		SELECT count(*) FROM system.permissions
		WHERE permission_key = 'model.standard_reference.update'
		  AND owner_module = 'model' AND status = 'active'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("count Standard reference update permission: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.tenant_id IS NULL
		  AND role.role_key = 'tenant.standard_runtime'
		  AND permission.permission_key = 'model.standard_reference.update'
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("count Standard runtime role permission: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.tenant_memberships membership
		JOIN system.service_principals service_principal ON service_principal.id = membership.principal_id
		WHERE membership.tenant_id = $1
		  AND membership.status = 'active'
		  AND service_principal.name = 'addp-standard'
	`, tenantID).Scan(&membershipCount); err != nil {
		t.Fatalf("count Standard runtime tenant membership: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_assignments assignment
		JOIN system.service_principals service_principal ON service_principal.id = assignment.principal_id
		JOIN system.roles role ON role.id = assignment.role_id
		WHERE assignment.tenant_id = $1
		  AND assignment.scope_type = 'tenant'
		  AND assignment.status = 'active'
		  AND service_principal.name = 'addp-standard'
		  AND role.tenant_id IS NULL
		  AND role.role_key = 'tenant.standard_runtime'
	`, tenantID).Scan(&assignmentCount); err != nil {
		t.Fatalf("count Standard runtime tenant assignment: %v", err)
	}
	if version != 65 || dirty || permissionCount != 1 || rolePermissionCount != 1 ||
		membershipCount != 1 || assignmentCount != 1 {
		t.Fatalf(
			"migration 65 state=(%d,%t) permission=%d role_permission=%d membership=%d assignment=%d",
			version, dirty, permissionCount, rolePermissionCount, membershipCount, assignmentCount,
		)
	}
}

func TestMetaServicePrincipalForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Meta forward migration schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	through24 := fstest.MapFS{}
	through25 := fstest.MapFS{}
	entries, err := fs.ReadDir(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() >= "000026_" {
			continue
		}
		migrationPath := path.Join(DefaultMigrationsRoot, entry.Name())
		data, err := fs.ReadFile(EmbeddedSQL, migrationPath)
		if err != nil {
			t.Fatalf("read migration %s: %v", migrationPath, err)
		}
		through25[migrationPath] = &fstest.MapFile{Data: data}
		if entry.Name() < "000025_" {
			through24[migrationPath] = &fstest.MapFile{Data: data}
		}
	}
	if err := (&Runner{DSN: dsn, FS: through24, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 24: %v", err)
	}

	var administratorID, tenantID int64
	if err := db.QueryRow(`INSERT INTO system.principals (principal_type) VALUES ('user') RETURNING id`).Scan(&administratorID); err != nil {
		t.Fatalf("create migration administrator principal: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES ($1, 'Meta Migration Administrator')`, administratorID); err != nil {
		t.Fatalf("create migration administrator user: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO system.tenants (code, name)
		VALUES ('meta-forward-migration', 'Meta Forward Migration')
		RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create migration tenant: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
		VALUES ($1, $2, 'active', 'bootstrap', now(), $2)
	`, tenantID, administratorID); err != nil {
		t.Fatalf("create migration tenant administrator membership: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type)
		SELECT $2, role.id, 'tenant', $1, 'active', now(), 'bootstrap'
		FROM system.roles role
		WHERE role.tenant_id IS NULL AND role.role_key = 'tenant.administrator'
	`, tenantID, administratorID); err != nil {
		t.Fatalf("create migration tenant administrator assignment: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.tenants
		SET initialized_at = now(), initialized_by_principal_id = $2
		WHERE id = $1
	`, tenantID, administratorID); err != nil {
		t.Fatalf("initialize migration tenant: %v", err)
	}
	if _, err := db.Exec(`
		CREATE SCHEMA common;
		CREATE TABLE common.task_executions (
		    id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
		    module text NOT NULL,
		    execution_config jsonb NOT NULL,
		    updated_at timestamptz NOT NULL
		);
		INSERT INTO common.task_executions (module, execution_config, updated_at)
		VALUES ('meta', '{"engine_id":2,"token":"legacy-user-token"}'::jsonb, now() - interval '1 hour');
	`); err != nil {
		t.Fatalf("create historical Meta execution: %v", err)
	}

	if err := (&Runner{DSN: dsn, FS: through25, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply Meta service principal migration 25: %v", err)
	}
	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read forward migration version: %v", err)
	}
	if version != 25 || dirty {
		t.Fatalf("forward migration state = (%d, %t), want (25, false)", version, dirty)
	}

	var membershipCount, assignmentCount, executionTokenCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.tenant_memberships membership
		JOIN system.service_principals service_principal ON service_principal.id = membership.principal_id
		WHERE membership.tenant_id = $1
		  AND membership.status = 'active'
		  AND service_principal.name = 'addp-meta'
	`, tenantID).Scan(&membershipCount); err != nil {
		t.Fatalf("count migrated Meta membership: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_assignments assignment
		JOIN system.service_principals service_principal ON service_principal.id = assignment.principal_id
		JOIN system.roles role ON role.id = assignment.role_id
		WHERE service_principal.name = 'addp-meta'
		  AND assignment.status = 'active'
		  AND (
		      (role.role_key = 'tenant.meta_runtime' AND assignment.scope_type = 'tenant' AND assignment.tenant_id = $1)
		      OR (role.role_key = 'platform.meta_runtime' AND assignment.scope_type = 'platform' AND assignment.tenant_id IS NULL)
		  )
	`, tenantID).Scan(&assignmentCount); err != nil {
		t.Fatalf("count migrated Meta role assignments: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM common.task_executions
		WHERE module = 'meta' AND execution_config ? 'token'
	`).Scan(&executionTokenCount); err != nil {
		t.Fatalf("count historical Meta execution tokens: %v", err)
	}
	if membershipCount != 1 || assignmentCount != 2 || executionTokenCount != 0 {
		t.Fatalf("Meta forward migration membership=%d assignments=%d execution_tokens=%d", membershipCount, assignmentCount, executionTokenCount)
	}

	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, status, valid_from,
		     source_type, created_by_principal_id)
		SELECT $2, role.id, 'tenant', $1, 'active', now(), 'manual', $2
		FROM system.roles role
		WHERE role.tenant_id IS NULL AND role.role_key = 'tenant.data_engineer'
	`, tenantID, administratorID); err != nil {
		t.Fatalf("create Data Engineer assignment before Manager catalog migration: %v", err)
	}
	var authorizationVersionBefore int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, administratorID).Scan(&authorizationVersionBefore); err != nil {
		t.Fatalf("read authorization version before Manager catalog migration: %v", err)
	}

	if err := NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply Manager data profile authorization migration 26: %v", err)
	}
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read Manager catalog migration version: %v", err)
	}
	latestVersion := latestMigrationVersion(t)
	if version != latestVersion || dirty {
		t.Fatalf("latest migration state = (%d, %t), want (%d, false)", version, dirty, latestVersion)
	}
	var authorizationVersionAfter int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, administratorID).Scan(&authorizationVersionAfter); err != nil {
		t.Fatalf("read authorization version after Manager catalog migration: %v", err)
	}
	if authorizationVersionAfter <= authorizationVersionBefore {
		t.Fatalf("authorization version after catalog migrations = %d, want greater than %d", authorizationVersionAfter, authorizationVersionBefore)
	}
	assertManagerDataProfileAuthorizationCatalog(t, db)
}

func TestAssetPortalBoundaryForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Asset/Portal forward migration schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	through31, through32 := migrationFilesBeforeAndThrough(t, "000032_iam_asset_portal_boundary.up.sql")
	if err := (&Runner{DSN: dsn, FS: through31, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 31: %v", err)
	}
	_, tenantID := seedInitializedMigrationTenant(t, db, "portal-forward-migration", "Portal Forward Migration")

	if err := (&Runner{DSN: dsn, FS: through32, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply Asset/Portal boundary migration 32: %v", err)
	}
	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read Asset/Portal forward migration version: %v", err)
	}
	if version != 32 || dirty {
		t.Fatalf("Asset/Portal forward migration state = (%d, %t), want (32, false)", version, dirty)
	}

	var membershipCount, assignmentCount, endpointPermissionCount, portalAssetPermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.tenant_memberships membership
		JOIN system.service_principals principal ON principal.id = membership.principal_id
		WHERE membership.tenant_id = $1 AND membership.status = 'active'
		  AND principal.name = 'addp-portal'
	`, tenantID).Scan(&membershipCount); err != nil {
		t.Fatalf("count migrated Portal membership: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_assignments assignment
		JOIN system.service_principals principal ON principal.id = assignment.principal_id
		JOIN system.roles role ON role.id = assignment.role_id
		WHERE principal.name = 'addp-portal'
		  AND role.role_key = 'tenant.portal_runtime'
		  AND assignment.scope_type = 'tenant' AND assignment.tenant_id = $1
		  AND assignment.status = 'active'
	`, tenantID).Scan(&assignmentCount); err != nil {
		t.Fatalf("count migrated Portal role assignment: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.portal_runtime'
		  AND permission.permission_key = 'service.endpoint.read'
		  AND permission.status = 'active'
	`).Scan(&endpointPermissionCount); err != nil {
		t.Fatalf("count Portal endpoint permission: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.portal_runtime' AND permission.permission_key LIKE 'asset.%'
	`).Scan(&portalAssetPermissionCount); err != nil {
		t.Fatalf("count forbidden Portal Asset permissions: %v", err)
	}
	if membershipCount != 1 || assignmentCount != 1 || endpointPermissionCount != 1 || portalAssetPermissionCount != 0 {
		t.Fatalf(
			"Portal forward migration memberships=%d assignments=%d endpoint_permissions=%d asset_permissions=%d",
			membershipCount, assignmentCount, endpointPermissionCount, portalAssetPermissionCount,
		)
	}
}

func TestAssetPortalBoundaryMigrationRollsBackBusinessFactsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Asset/Portal rollback schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	through31, through32 := migrationFilesBeforeAndThrough(t, "000032_iam_asset_portal_boundary.up.sql")
	if err := (&Runner{DSN: dsn, FS: through31, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 31: %v", err)
	}
	_, tenantID := seedInitializedMigrationTenant(t, db, "portal-rollback-migration", "Portal Rollback Migration")
	if _, err := db.Exec(`
		WITH principal AS (
			INSERT INTO system.principals (principal_type, status)
			VALUES ('service_principal', 'active')
			RETURNING id
		)
		INSERT INTO system.service_principals (
			id, name, description, owner_scope, created_by_principal_id
		)
		SELECT id, 'addp-portal', 'conflicting Portal principal', 'platform', id
		FROM principal
	`); err != nil {
		t.Fatalf("create conflicting Portal principal: %v", err)
	}

	if err := (&Runner{DSN: dsn, FS: through32, Root: DefaultMigrationsRoot}).Run(ctx); err == nil {
		t.Fatal("migration 32 succeeded despite conflicting Portal principal")
	}
	var managementPermissionCount, portalRoleCount, endpointActiveCount, membershipCount, assignmentCount int
	if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key = 'asset.management.read'`).Scan(&managementPermissionCount); err != nil {
		t.Fatalf("count rolled-back management permission: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.roles WHERE role_key = 'tenant.portal_runtime'`).Scan(&portalRoleCount); err != nil {
		t.Fatalf("count rolled-back Portal role: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key = 'service.endpoint.read' AND status = 'active'`).Scan(&endpointActiveCount); err != nil {
		t.Fatalf("count rolled-back endpoint activation: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM system.tenant_memberships membership
		JOIN system.service_principals principal ON principal.id = membership.principal_id
		WHERE membership.tenant_id = $1 AND principal.name = 'addp-portal'
	`, tenantID).Scan(&membershipCount); err != nil {
		t.Fatalf("count rolled-back Portal membership: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM system.role_assignments assignment
		JOIN system.service_principals principal ON principal.id = assignment.principal_id
		WHERE assignment.tenant_id = $1 AND principal.name = 'addp-portal'
	`, tenantID).Scan(&assignmentCount); err != nil {
		t.Fatalf("count rolled-back Portal assignment: %v", err)
	}
	if managementPermissionCount != 0 || portalRoleCount != 0 || endpointActiveCount != 0 || membershipCount != 0 || assignmentCount != 0 {
		t.Fatalf(
			"migration rollback left management_permissions=%d portal_roles=%d endpoint_active=%d memberships=%d assignments=%d",
			managementPermissionCount, portalRoleCount, endpointActiveCount, membershipCount, assignmentCount,
		)
	}
}

func TestPortalPlatformRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Portal platform runtime schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	through32, through33 := migrationFilesBeforeAndThrough(t, "000033_iam_portal_platform_runtime.up.sql")
	if err := (&Runner{DSN: dsn, FS: through32, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 32: %v", err)
	}
	if err := (&Runner{DSN: dsn, FS: through33, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply Portal platform runtime migration 33: %v", err)
	}

	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read Portal platform runtime migration version: %v", err)
	}
	var roleCount, permissionCount, assignmentCount int
	if err := db.QueryRow(`
		SELECT count(*) FROM system.roles
		WHERE role_key = 'platform.portal_runtime'
		  AND role_type = 'platform_builtin'
		  AND allowed_scope_types = ARRAY['platform']::text[]
		  AND allowed_principal_types = ARRAY['service_principal']::text[]
		  AND immutable AND status = 'active'
	`).Scan(&roleCount); err != nil {
		t.Fatalf("count Portal platform runtime role: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'platform.portal_runtime'
		  AND permission.permission_key = 'system.runtime_registry.update'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("count Portal platform runtime permission: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_assignments assignment
		JOIN system.service_principals principal ON principal.id = assignment.principal_id
		JOIN system.roles role ON role.id = assignment.role_id
		WHERE principal.name = 'addp-portal'
		  AND role.role_key = 'platform.portal_runtime'
		  AND assignment.scope_type = 'platform'
		  AND assignment.status = 'active'
	`).Scan(&assignmentCount); err != nil {
		t.Fatalf("count Portal platform runtime assignment: %v", err)
	}
	if version != 33 || dirty || roleCount != 1 || permissionCount != 1 || assignmentCount != 1 {
		t.Fatalf(
			"Portal platform runtime migration state=(%d,%t) roles=%d permissions=%d assignments=%d",
			version, dirty, roleCount, permissionCount, assignmentCount,
		)
	}
}

func TestPortalPlatformRuntimeMigrationRollsBackAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Portal platform runtime rollback schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	through32, through33 := migrationFilesBeforeAndThrough(t, "000033_iam_portal_platform_runtime.up.sql")
	if err := (&Runner{DSN: dsn, FS: through32, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 32: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.roles (
			role_key, name_i18n_key, description_i18n_key, role_type,
			allowed_scope_types, allowed_principal_types, immutable, status
		) VALUES (
			'platform.portal_runtime', 'conflict.name', 'conflict.description', 'platform_builtin',
			ARRAY['platform']::text[], ARRAY['service_principal']::text[], true, 'active'
		)
	`); err != nil {
		t.Fatalf("create conflicting Portal platform role: %v", err)
	}

	if err := (&Runner{DSN: dsn, FS: through33, Root: DefaultMigrationsRoot}).Run(ctx); err == nil {
		t.Fatal("migration 33 succeeded despite conflicting Portal platform role")
	}
	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read rolled-back Portal migration version: %v", err)
	}
	var permissionCount, assignmentCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'platform.portal_runtime'
		  AND permission.permission_key = 'system.runtime_registry.update'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("count rolled-back Portal permission: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_assignments assignment
		JOIN system.service_principals principal ON principal.id = assignment.principal_id
		JOIN system.roles role ON role.id = assignment.role_id
		WHERE principal.name = 'addp-portal'
		  AND role.role_key = 'platform.portal_runtime'
		  AND assignment.scope_type = 'platform'
	`).Scan(&assignmentCount); err != nil {
		t.Fatalf("count rolled-back Portal assignment: %v", err)
	}
	if version != 33 || !dirty || permissionCount != 0 || assignmentCount != 0 {
		t.Fatalf(
			"rolled-back Portal migration state=(%d,%t) permissions=%d assignments=%d",
			version, dirty, permissionCount, assignmentCount,
		)
	}
}

func TestDevelopNotebookUpdateForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Develop Notebook update schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	through33, through34 := migrationFilesBeforeAndThrough(t, "000034_iam_develop_notebook_update.up.sql")
	if err := (&Runner{DSN: dsn, FS: through33, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 33: %v", err)
	}

	var status string
	var rolePermissionCount int
	if err := db.QueryRow(`
		SELECT status FROM system.permissions
		WHERE permission_key = 'develop.notebook.update'
	`).Scan(&status); err != nil {
		t.Fatalf("read Notebook update permission before migration 34: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.data_engineer'
		  AND permission.permission_key = 'develop.notebook.update'
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("count Notebook update role binding before migration 34: %v", err)
	}
	if status != "disabled" || rolePermissionCount != 0 {
		t.Fatalf("before migration 34 status=%q role_permissions=%d", status, rolePermissionCount)
	}

	if err := (&Runner{DSN: dsn, FS: through34, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply Develop Notebook update migration 34: %v", err)
	}
	if err := db.QueryRow(`
		SELECT permission.status, count(*)
		FROM system.permissions permission
		JOIN system.role_permissions role_permission ON role_permission.permission_id = permission.id
		JOIN system.roles role ON role.id = role_permission.role_id
		WHERE permission.permission_key = 'develop.notebook.update'
		  AND role.role_key = 'tenant.data_engineer'
		GROUP BY permission.status
	`).Scan(&status, &rolePermissionCount); err != nil {
		t.Fatalf("read Notebook update catalog after migration 34: %v", err)
	}
	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration 34 version: %v", err)
	}
	if version != 34 || dirty || status != "active" || rolePermissionCount != 1 {
		t.Fatalf("migration 34 state=(%d,%t) status=%q role_permissions=%d", version, dirty, status, rolePermissionCount)
	}
}

func migrationFilesBeforeAndThrough(t *testing.T, boundary string) (fstest.MapFS, fstest.MapFS) {
	t.Helper()
	before := fstest.MapFS{}
	through := fstest.MapFS{}
	entries, err := fs.ReadDir(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() > boundary {
			continue
		}
		migrationPath := path.Join(DefaultMigrationsRoot, entry.Name())
		data, err := fs.ReadFile(EmbeddedSQL, migrationPath)
		if err != nil {
			t.Fatalf("read migration %s: %v", migrationPath, err)
		}
		through[migrationPath] = &fstest.MapFile{Data: data}
		if entry.Name() < boundary {
			before[migrationPath] = &fstest.MapFile{Data: data}
		}
	}
	return before, through
}

func TestTaskProviderModuleDeclarationForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset TaskProvider migration schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	through71, through72 := migrationFilesBeforeAndThrough(t, "000072_task_provider_module_declaration.up.sql")
	if err := (&Runner{DSN: dsn, FS: through71, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 71: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE system.task_providers (
			module_name varchar(50) PRIMARY KEY,
			display_name varchar(100) NOT NULL,
			description text NOT NULL,
			task_list_endpoint varchar(255) NOT NULL,
			task_detail_endpoint varchar(255) NOT NULL,
			task_execute_endpoint varchar(255) NOT NULL,
			task_status_endpoint varchar(255) NOT NULL,
			task_cancel_endpoint varchar(255) NOT NULL DEFAULT '',
			capabilities jsonb NOT NULL
		);
		INSERT INTO system.module_definitions (module_name, route_prefix)
		VALUES ('meta', '/meta');
		INSERT INTO system.task_providers (
			module_name, display_name, description,
			task_list_endpoint, task_detail_endpoint, task_execute_endpoint, task_status_endpoint,
			capabilities
		) VALUES (
			'meta', 'Meta', 'Metadata tasks',
			'/api/v1/meta/tasks', '/api/v1/meta/tasks/{task_type}/{id}',
			'/api/v1/meta/tasks/{task_type}/{id}/execute', '/api/v1/meta/executions/{execution_id}',
			'{"schema_version":"task.capabilities/v2","task_capabilities":[]}'::jsonb
		)
	`); err != nil {
		t.Fatalf("seed legacy TaskProvider: %v", err)
	}

	var moduleID int64
	if err := db.QueryRow(`SELECT id FROM system.module_definitions WHERE module_name = 'meta'`).Scan(&moduleID); err != nil {
		t.Fatalf("read module ID before migration: %v", err)
	}
	if err := (&Runner{DSN: dsn, FS: through72, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migration 72: %v", err)
	}

	var migratedID, version int64
	var displayName, taskListEndpoint string
	if err := db.QueryRow(`
		SELECT id, version, task_provider->>'display_name', task_provider->>'task_list_endpoint'
		FROM system.module_definitions WHERE module_name = 'meta'
	`).Scan(&migratedID, &version, &displayName, &taskListEndpoint); err != nil {
		t.Fatalf("read migrated TaskProvider declaration: %v", err)
	}
	var legacyTableExists bool
	if err := db.QueryRow(`SELECT to_regclass('system.task_providers') IS NOT NULL`).Scan(&legacyTableExists); err != nil {
		t.Fatalf("inspect retired TaskProvider table: %v", err)
	}
	if migratedID != moduleID || version != 2 || displayName != "Meta" ||
		taskListEndpoint != "/api/v1/meta/tasks" || legacyTableExists {
		t.Fatalf(
			"migrated TaskProvider id=%d version=%d display=%q list=%q legacy_table=%t",
			migratedID, version, displayName, taskListEndpoint, legacyTableExists,
		)
	}
}

func seedInitializedMigrationTenant(t *testing.T, db *sql.DB, code, name string) (int64, int64) {
	t.Helper()
	var administratorID, tenantID int64
	if err := db.QueryRow(`INSERT INTO system.principals (principal_type) VALUES ('user') RETURNING id`).Scan(&administratorID); err != nil {
		t.Fatalf("create migration administrator principal: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES ($1, 'Migration Administrator')`, administratorID); err != nil {
		t.Fatalf("create migration administrator user: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO system.tenants (code, name) VALUES ($1, $2) RETURNING id`, code, name).Scan(&tenantID); err != nil {
		t.Fatalf("create migration tenant: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
		VALUES ($1, $2, 'active', 'bootstrap', now(), $2)
	`, tenantID, administratorID); err != nil {
		t.Fatalf("create migration tenant administrator membership: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type)
		SELECT $2, role.id, 'tenant', $1, 'active', now(), 'bootstrap'
		FROM system.roles role
		WHERE role.tenant_id IS NULL AND role.role_key = 'tenant.administrator'
	`, tenantID, administratorID); err != nil {
		t.Fatalf("create migration tenant administrator assignment: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.tenants
		SET initialized_at = now(), initialized_by_principal_id = $2
		WHERE id = $1
	`, tenantID, administratorID); err != nil {
		t.Fatalf("initialize migration tenant: %v", err)
	}
	return administratorID, tenantID
}

func TestNotebookSessionAuthorizationRepairAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Notebook authorization repair schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, through36 := migrationFilesBeforeAndThrough(t, "000036_iam_service_query_sample.up.sql")
	if err := (&Runner{DSN: dsn, FS: through36, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 36: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE system.notebook_catalog_authorizations (
		    id uuid PRIMARY KEY,
		    session_id uuid NOT NULL UNIQUE,
		    task_id bigint NOT NULL CHECK (task_id > 0),
		    actor_principal_id bigint NOT NULL REFERENCES system.principals(id),
		    tenant_id bigint NOT NULL REFERENCES system.tenants(id),
		    tenant_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
		    token_family_id bigint NOT NULL REFERENCES system.refresh_token_families(id),
		    issued_authorization_version bigint NOT NULL CHECK (issued_authorization_version > 0),
		    audience text NOT NULL CHECK (audience = 'develop'),
		    operation text NOT NULL CHECK (operation = 'catalog.list_children'),
		    expires_at timestamptz NOT NULL,
		    revoked_at timestamptz,
		    revoked_reason text,
		    created_at timestamptz NOT NULL DEFAULT now(),
		    CHECK (expires_at > created_at),
		    CHECK (expires_at <= created_at + interval '1 hour'),
		    CHECK (
		        (revoked_at IS NULL AND revoked_reason IS NULL)
		        OR (revoked_at IS NOT NULL AND revoked_reason IS NOT NULL AND btrim(revoked_reason) <> '')
		    ),
		    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
		);
		CREATE INDEX idx_notebook_catalog_authorizations_actor_active
		    ON system.notebook_catalog_authorizations (actor_principal_id, tenant_id, expires_at)
		    WHERE revoked_at IS NULL;
		CREATE INDEX idx_notebook_catalog_authorizations_membership_active
		    ON system.notebook_catalog_authorizations (tenant_membership_id, expires_at)
		    WHERE revoked_at IS NULL;
		CREATE INDEX idx_notebook_catalog_authorizations_family_active
		    ON system.notebook_catalog_authorizations (token_family_id, expires_at)
		    WHERE revoked_at IS NULL;
		CREATE INDEX idx_notebook_catalog_authorizations_expiry_active
		    ON system.notebook_catalog_authorizations (expires_at, id)
		    WHERE revoked_at IS NULL;
		INSERT INTO system.permissions (
		    permission_key, owner_module, action, risk_level, delegable,
		    allowed_scope_types, tenant_customizable, name_i18n_key,
		    description_i18n_key, status
		) VALUES (
		    'system.notebook_catalog_authorization.execute', 'system', 'execute', 'low', false,
		    ARRAY['tenant']::text[], false,
		    'permissions.system.notebook_catalog_authorization.execute.name',
		    'permissions.system.notebook_catalog_authorization.execute.description', 'active'
		);
		INSERT INTO system.role_permissions (
		    role_id, permission_id, source_type, created_by_principal_id
		)
		SELECT role.id, permission.id, 'product', NULL
		FROM system.roles role
		JOIN system.permissions permission
		  ON permission.permission_key = 'system.notebook_catalog_authorization.execute'
		WHERE role.tenant_id IS NULL
		  AND role.role_key = 'tenant.develop_runtime'
		  AND role.role_type = 'tenant_builtin'
		  AND role.status = 'active';
		UPDATE system.schema_migrations SET version = 37, dirty = false;
	`); err != nil {
		t.Fatalf("create historical migration 37 schema: %v", err)
	}

	if err := NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply Notebook Session authorization repair migration 38: %v", err)
	}

	var version, checksumCount, canonicalPermissionCount, legacyActivePermissionCount, legacyDisabledPermissionCount int
	var canonicalRoleBindingCount, legacyRoleBindingCount, canonicalTriggerCount int
	var dirty, canonicalTable, legacyTable, sourceColumn, sourceConstraint bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read repaired migration version: %v", err)
	}
	if err := db.QueryRow(`
		SELECT to_regclass('system.notebook_session_authorizations') IS NOT NULL,
		       to_regclass('system.notebook_catalog_authorizations') IS NOT NULL,
		       EXISTS (
		           SELECT 1 FROM information_schema.columns
		           WHERE table_schema = 'system'
		             AND table_name = 'execution_authorizations'
		             AND column_name = 'source_notebook_session_authorization_id'
		       ),
		       EXISTS (
		           SELECT 1 FROM pg_constraint
		           WHERE conrelid = 'system.execution_authorizations'::regclass
		             AND conname = 'execution_authorizations_notebook_session_source_check'
		       )
	`).Scan(&canonicalTable, &legacyTable, &sourceColumn, &sourceConstraint); err != nil {
		t.Fatalf("inspect repaired Notebook Session authorization schema: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM pg_trigger
		WHERE tgrelid = 'system.notebook_session_authorizations'::regclass
		  AND NOT tgisinternal
	`).Scan(&canonicalTriggerCount); err != nil {
		t.Fatalf("count repaired Notebook Session authorization triggers: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.schema_migration_checksums`).Scan(&checksumCount); err != nil {
		t.Fatalf("count migration checksums: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FILTER (WHERE permission_key = 'system.notebook_session_authorization.execute'),
		       count(*) FILTER (
		           WHERE permission_key = 'system.notebook_catalog_authorization.execute' AND status = 'active'
		       ),
		       count(*) FILTER (
		           WHERE permission_key = 'system.notebook_catalog_authorization.execute' AND status = 'disabled'
		       )
		FROM system.permissions
	`).Scan(&canonicalPermissionCount, &legacyActivePermissionCount, &legacyDisabledPermissionCount); err != nil {
		t.Fatalf("inspect repaired Notebook authorization permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FILTER (
		           WHERE role.role_key = 'tenant.develop_runtime'
		             AND permission.permission_key = 'system.notebook_session_authorization.execute'
		       ),
		       count(*) FILTER (
		           WHERE permission.permission_key = 'system.notebook_catalog_authorization.execute'
		       )
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
	`).Scan(&canonicalRoleBindingCount, &legacyRoleBindingCount); err != nil {
		t.Fatalf("inspect repaired Notebook authorization role bindings: %v", err)
	}
	latestVersion := latestMigrationVersion(t)
	if version != latestVersion || dirty || !canonicalTable || legacyTable || !sourceColumn ||
		!sourceConstraint || canonicalTriggerCount != 4 || checksumCount != latestVersion ||
		canonicalPermissionCount != 1 || legacyActivePermissionCount != 0 || legacyDisabledPermissionCount != 1 ||
		canonicalRoleBindingCount != 1 || legacyRoleBindingCount != 0 {
		t.Fatalf(
			"repair state version=(%d,%t) tables=(%t,%t) source=(%t,%t) triggers=%d checksums=%d permissions=(%d,%d,%d) role_bindings=(%d,%d)",
			version, dirty, canonicalTable, legacyTable, sourceColumn, sourceConstraint,
			canonicalTriggerCount, checksumCount, canonicalPermissionCount, legacyActivePermissionCount, legacyDisabledPermissionCount,
			canonicalRoleBindingCount, legacyRoleBindingCount,
		)
	}
}

func TestNotebookSessionAuthorizationRepairCreatesMissingSchemaAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset missing Notebook authorization schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, through36 := migrationFilesBeforeAndThrough(t, "000036_iam_service_query_sample.up.sql")
	if err := (&Runner{DSN: dsn, FS: through36, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatalf("apply migrations through 36: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.schema_migrations SET version = 37, dirty = false`); err != nil {
		t.Fatalf("record migration 37 without its schema: %v", err)
	}

	if err := NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("repair missing Notebook Session authorization schema: %v", err)
	}

	var version, triggerCount, checksumCount, roleBindingCount int
	var dirty, canonicalTable, sourceColumn, sourceConstraint bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read repaired migration version: %v", err)
	}
	if err := db.QueryRow(`
		SELECT to_regclass('system.notebook_session_authorizations') IS NOT NULL,
		       EXISTS (
		           SELECT 1 FROM information_schema.columns
		           WHERE table_schema = 'system'
		             AND table_name = 'execution_authorizations'
		             AND column_name = 'source_notebook_session_authorization_id'
		       ),
		       EXISTS (
		           SELECT 1 FROM pg_constraint
		           WHERE conrelid = 'system.execution_authorizations'::regclass
		             AND conname = 'execution_authorizations_notebook_session_source_check'
		       )
	`).Scan(&canonicalTable, &sourceColumn, &sourceConstraint); err != nil {
		t.Fatalf("inspect created Notebook Session authorization schema: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM pg_trigger
		WHERE tgrelid = 'system.notebook_session_authorizations'::regclass
		  AND NOT tgisinternal
	`).Scan(&triggerCount); err != nil {
		t.Fatalf("count created Notebook Session authorization triggers: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.schema_migration_checksums`).Scan(&checksumCount); err != nil {
		t.Fatalf("count migration checksums: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.develop_runtime'
		  AND permission.permission_key = 'system.notebook_session_authorization.execute'
		  AND permission.status = 'active'
	`).Scan(&roleBindingCount); err != nil {
		t.Fatalf("count created Notebook authorization role binding: %v", err)
	}
	latestVersion := latestMigrationVersion(t)
	if version != latestVersion || dirty || !canonicalTable || !sourceColumn || !sourceConstraint ||
		triggerCount != 4 || checksumCount != latestVersion || roleBindingCount != 1 {
		t.Fatalf(
			"missing-schema repair state version=(%d,%t) table=%t source=(%t,%t) triggers=%d checksums=%d role_binding=%d",
			version, dirty, canonicalTable, sourceColumn, sourceConstraint,
			triggerCount, checksumCount, roleBindingCount,
		)
	}
}

func TestRunnerAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`); err != nil {
		t.Fatalf("reset test schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runner := NewRunner(dsn)
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	latestVersion := latestMigrationVersion(t)
	if version != latestVersion || dirty {
		t.Fatalf("migration state = (%d, %t), want (%d, false)", version, dirty, latestVersion)
	}

	assertIAMCatalogSeed(t, db)
	assertStandardDocumentCatalog(t, db)
	assertMonitorAuthorizationCatalog(t, db)
	assertModelAuthorizationCatalog(t, db)
	assertStandardAuthorizationCatalog(t, db)
	assertManagerAuthorizationCatalog(t, db)
	assertManagerDataProfileAuthorizationCatalog(t, db)
	assertAuthorizationCatalogRetirement(t, db)
	assertIdentityTenantConstraints(t, db)
	assertFederationOrganizationConstraints(t, db)
	assertAuthorizationGovernanceConstraints(t, db)
	assertSessionTokenFamilyConstraints(t, db)
	assertAuthorizationVersionRefreshConstraints(t, db)
	assertOAuthFositeStorageConstraints(t, db)
	assertAuditContextConstraints(t, db)
	assertInvitationEnrollmentConstraints(t, db)
	assertMFABootstrapConstraints(t, db)
	assertIAMRecoveryConstraints(t, db)
	assertTenantAdministrationClosure(t, db)
	assertServicePrincipalRuntimeConstraints(t, db)
	assertExecutionAuthorizationConstraints(t, db)
	assertEngineRuntimeDescriptorConstraints(t, db)
	assertTaskExecutionRuntimeConstraints(t, db)
	assertModuleManagementControlPlane(t, db)
	assertDuckDBRuntimeConstraints(t, db)
	assertAssetServiceRuntimeConstraints(t, db)
	assertAssetPortalBoundaryConstraints(t, db)
	assertRoleKeyNamespace(t, db)
	assertForeignKeyColumnsIndexed(t, db)

	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatalf("read embedded migration catalog for checksum test: %v", err)
	}
	_, tampered := migrationFilesBeforeAndThrough(t, catalog.Names[len(catalog.Names)-1])
	tamperedMigration := tampered["sql/000037_iam_notebook_session_authorization.up.sql"]
	tamperedMigration.Data = append(append([]byte(nil), tamperedMigration.Data...), []byte("\n-- rewritten\n")...)
	if err := (&Runner{DSN: dsn, FS: tampered, Root: DefaultMigrationsRoot}).Run(ctx); err == nil || !strings.Contains(err.Error(), "does not match embedded file") {
		t.Fatalf("Run() checksum error = %v, want immutable migration rejection", err)
	}

	if _, err := db.Exec(`UPDATE system.schema_migrations SET dirty = true`); err != nil {
		t.Fatalf("mark migration dirty: %v", err)
	}
	if err := runner.Run(ctx); err == nil || !strings.Contains(err.Error(), "is dirty") {
		t.Fatalf("Run() error = %v, want dirty-state rejection", err)
	}
	if _, err := db.Exec(`UPDATE system.schema_migrations SET version = $1, dirty = false`, latestMigrationVersion(t)+1); err != nil {
		t.Fatalf("set newer migration version: %v", err)
	}
	if err := runner.Run(ctx); err == nil || !strings.Contains(err.Error(), "newer than embedded") {
		t.Fatalf("Run() error = %v, want newer-version rejection", err)
	}

	if _, err := db.Exec(`DROP SCHEMA system CASCADE; CREATE SCHEMA system; CREATE TABLE system.users (id bigint PRIMARY KEY)`); err != nil {
		t.Fatalf("prepare legacy schema: %v", err)
	}
	if err := runner.Run(ctx); err == nil || !strings.Contains(err.Error(), "legacy system IAM schema") {
		t.Fatalf("Run() error = %v, want legacy-schema rejection", err)
	}
}

func assertModuleManagementControlPlane(t *testing.T, db *sql.DB) {
	t.Helper()
	var versionColumnCount, versionConstraintCount, permissionCount, rolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'system' AND table_name = 'module_definitions'
		  AND column_name = 'version' AND data_type = 'bigint' AND is_nullable = 'NO'
	`).Scan(&versionColumnCount); err != nil {
		t.Fatalf("count module definition version column: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM pg_constraint
		WHERE conrelid = 'system.module_definitions'::regclass
		  AND conname = 'module_definitions_version_positive'
	`).Scan(&versionConstraintCount); err != nil {
		t.Fatalf("count module definition version constraint: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM system.permissions
		WHERE permission_key IN ('platform.module.read', 'platform.module.update')
		  AND status = 'active'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("count module management permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions AS role_permission
		JOIN system.roles AS role ON role.id = role_permission.role_id
		JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
		WHERE role.tenant_id IS NULL
		  AND role.role_key = 'platform.system_administrator'
		  AND permission.permission_key IN ('platform.module.read', 'platform.module.update')
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("count platform system administrator module management permissions: %v", err)
	}
	if versionColumnCount != 1 || versionConstraintCount != 1 || permissionCount != 2 || rolePermissionCount != 2 {
		t.Fatalf(
			"module management version_column=%d version_constraint=%d permissions=%d role_permissions=%d",
			versionColumnCount, versionConstraintCount, permissionCount, rolePermissionCount,
		)
	}
}

func assertDuckDBRuntimeConstraints(t *testing.T, db *sql.DB) {
	t.Helper()
	var roleCount, rolePermissionCount, principalCount, clientCount, sourceConstraintCount int
	if err := db.QueryRow(`
		SELECT count(*) FROM system.roles
		WHERE tenant_id IS NULL AND role_key = 'tenant.duckdb_runtime'
		  AND role_type = 'tenant_builtin' AND status = 'active'
	`).Scan(&roleCount); err != nil {
		t.Fatalf("count DuckDB runtime role: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.tenant_id IS NULL AND role.role_key = 'tenant.duckdb_runtime'
		  AND permission.permission_key IN ('system.execution_authorization.execute', 'meta.catalog.read')
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("count DuckDB runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM system.service_principals
		WHERE name = 'addp-duckdb' AND owner_scope = 'platform'
	`).Scan(&principalCount); err != nil {
		t.Fatalf("count DuckDB service principal: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM system.oauth_clients
		WHERE client_id = 'addp-duckdb' AND client_type = 'confidential'
		  AND token_endpoint_auth_method = 'client_secret_basic' AND status = 'disabled'
	`).Scan(&clientCount); err != nil {
		t.Fatalf("count DuckDB OAuth client: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM pg_constraint
		WHERE conrelid = 'system.execution_authorizations'::regclass
		  AND conname = 'execution_authorizations_source_check'
	`).Scan(&sourceConstraintCount); err != nil {
		t.Fatalf("count execution authorization source constraint: %v", err)
	}
	if roleCount != 1 || rolePermissionCount != 2 || principalCount != 1 || clientCount != 1 || sourceConstraintCount != 1 {
		t.Fatalf(
			"DuckDB runtime catalog role=%d permissions=%d principal=%d client=%d source_constraint=%d",
			roleCount, rolePermissionCount, principalCount, clientCount, sourceConstraintCount,
		)
	}
}

func assertAssetPortalBoundaryConstraints(t *testing.T, db *sql.DB) {
	t.Helper()
	var managementPermissionCount, endpointPermissionCount, rolePermissionCount, principalCount int
	if err := db.QueryRow(`
		SELECT count(*) FROM system.permissions
		WHERE permission_key = 'asset.management.read'
		  AND owner_module = 'asset' AND action = 'read' AND status = 'active'
	`).Scan(&managementPermissionCount); err != nil {
		t.Fatalf("count Asset management permission: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM system.permissions
		WHERE permission_key = 'service.endpoint.read' AND status = 'active'
	`).Scan(&endpointPermissionCount); err != nil {
		t.Fatalf("count Service endpoint permission: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE (role.role_key, permission.permission_key) IN (
		    ('tenant.asset_manager', 'asset.management.read'),
		    ('tenant.portal_runtime', 'service.endpoint.read')
		)
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("count Asset/Portal role permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.service_principals service_principal
		JOIN system.oauth_clients oauth_client
		  ON oauth_client.service_principal_id = service_principal.id
		WHERE service_principal.name = 'addp-portal'
		  AND oauth_client.client_id = 'addp-portal'
		  AND oauth_client.grant_types = ARRAY['client_credentials']::text[]
		  AND oauth_client.status = 'disabled'
	`).Scan(&principalCount); err != nil {
		t.Fatalf("count Portal service principal: %v", err)
	}
	if managementPermissionCount != 1 || endpointPermissionCount != 1 || rolePermissionCount != 2 || principalCount != 1 {
		t.Fatalf(
			"Asset/Portal boundary management_permission=%d endpoint_permission=%d role_permissions=%d principals=%d",
			managementPermissionCount, endpointPermissionCount, rolePermissionCount, principalCount,
		)
	}
}

func assertEngineRuntimeDescriptorConstraints(t *testing.T, db *sql.DB) {
	t.Helper()
	var permissionCount, rolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*) FROM system.permissions
		WHERE permission_key = 'system.engine_descriptor.read'
		  AND owner_module = 'system'
		  AND action = 'read'
		  AND allowed_scope_types = ARRAY['tenant']::text[]
		  AND NOT delegable
		  AND status = 'active'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("count engine descriptor permission: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.develop_runtime'
		  AND permission.permission_key = 'system.engine_descriptor.read'
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("count develop runtime engine descriptor permission: %v", err)
	}
	if permissionCount != 1 || rolePermissionCount != 1 {
		t.Fatalf("engine descriptor permission=%d role_permission=%d", permissionCount, rolePermissionCount)
	}
}

func assertTaskExecutionRuntimeConstraints(t *testing.T, db *sql.DB) {
	t.Helper()
	var permissionCount, roleCount, rolePermissionCount, principalCount int
	if err := db.QueryRow(`
		SELECT count(*) FROM system.permissions
		WHERE permission_key IN (
			'develop.task_provider.execute', 'develop.task_provider.read',
			'system.runtime_registry.read', 'system.task_authorization.execute'
		) AND status = 'active'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("count task execution runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM system.roles
		WHERE tenant_id IS NULL
		  AND role_key IN ('platform.orchestrator_runtime', 'tenant.orchestrator_runtime')
		  AND status = 'active'
	`).Scan(&roleCount); err != nil {
		t.Fatalf("count Orchestrator runtime roles: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE (role.role_key, permission.permission_key) IN (
			('platform.orchestrator_runtime', 'system.runtime_registry.read'),
			('platform.orchestrator_runtime', 'system.runtime_registry.update'),
			('tenant.orchestrator_runtime', 'develop.task_provider.execute'),
			('tenant.orchestrator_runtime', 'develop.task_provider.read'),
			('tenant.orchestrator_runtime', 'system.task_authorization.execute')
		  )
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("count Orchestrator runtime role permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.service_principals service_principal
		JOIN system.oauth_clients oauth_client
		  ON oauth_client.service_principal_id = service_principal.id
		WHERE service_principal.name = 'addp-orchestrator'
		  AND oauth_client.client_id = 'addp-orchestrator'
		  AND oauth_client.grant_types = ARRAY['client_credentials']::text[]
	`).Scan(&principalCount); err != nil {
		t.Fatalf("count Orchestrator service principal: %v", err)
	}
	var sourceTokenColumnCount, actorColumnCount, subjectTableCount int
	if err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'system' AND table_name = 'execution_authorizations'
		  AND column_name = 'source_access_token_id'
	`).Scan(&sourceTokenColumnCount); err != nil {
		t.Fatalf("inspect retired execution authorization token column: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'system' AND table_name = 'execution_authorizations'
		  AND column_name IN (
			'actor_principal_id', 'tenant_id', 'tenant_membership_id',
			'issued_authorization_version'
		  ) AND is_nullable = 'NO'
	`).Scan(&actorColumnCount); err != nil {
		t.Fatalf("inspect execution authorization actor columns: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'system' AND table_name = 'task_authorization_subjects'
	`).Scan(&subjectTableCount); err != nil {
		t.Fatalf("inspect task authorization subject table: %v", err)
	}
	if permissionCount != 4 || roleCount != 2 || rolePermissionCount != 5 || principalCount != 1 ||
		sourceTokenColumnCount != 0 || actorColumnCount != 4 || subjectTableCount != 1 {
		t.Fatalf(
			"task execution runtime catalog permissions=%d roles=%d role_permissions=%d principals=%d source_token_columns=%d actor_columns=%d subject_tables=%d",
			permissionCount, roleCount, rolePermissionCount, principalCount,
			sourceTokenColumnCount, actorColumnCount, subjectTableCount,
		)
	}
}

func assertExecutionAuthorizationConstraints(t *testing.T, db *sql.DB) {
	t.Helper()
	var tableName string
	if err := db.QueryRow(`SELECT to_regclass('system.execution_authorizations')::text`).Scan(&tableName); err != nil {
		t.Fatalf("resolve execution_authorizations table: %v", err)
	}
	if tableName != "system.execution_authorizations" {
		t.Fatalf("execution_authorizations table = %q", tableName)
	}
	var permissionCount, rolePermissionCount, triggerCount, audienceConstraintCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.permissions
		WHERE permission_key IN (
			'develop.data_read.execute', 'develop.data_write.execute',
			'develop.data_ddl.execute', 'develop.data_external_effect.execute',
			'model.materialization.execute',
			'model.task_provider.execute', 'model.task_provider.read',
			'system.execution_authorization.execute'
		)
		  AND status = 'active'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("count execution authorization permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE (role.role_key, permission.permission_key) IN (
			('tenant.data_architect', 'develop.data_ddl.execute'),
			('tenant.data_architect', 'develop.data_read.execute'),
			('tenant.data_architect', 'develop.data_write.execute'),
			('tenant.data_architect', 'develop.task.execute'),
			('tenant.data_architect', 'model.materialization.execute'),
			('tenant.data_architect', 'system.execution_authorization.create'),
			('tenant.data_engineer', 'develop.data_read.execute'),
			('tenant.data_engineer', 'develop.data_write.execute'),
			('tenant.data_steward', 'develop.data_read.execute'),
			('tenant.data_steward', 'develop.data_write.execute'),
			('tenant.data_viewer', 'develop.data_read.execute'),
			('tenant.develop_runtime', 'system.execution_authorization.execute'),
			('tenant.governance_manager', 'develop.data_read.execute'),
			('tenant.governance_manager', 'system.execution_authorization.create'),
			('tenant.model_runtime', 'system.engine_descriptor.read'),
			('tenant.model_runtime', 'system.execution_authorization.execute'),
			('tenant.orchestrator_runtime', 'model.task_provider.execute'),
			('tenant.orchestrator_runtime', 'model.task_provider.read'),
			('tenant.quality_runtime', 'system.execution_authorization.execute')
		)
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("count execution authorization role permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM pg_constraint
		WHERE conrelid = 'system.execution_authorizations'::regclass
		  AND conname = 'execution_authorizations_audience_check'
		  AND pg_get_constraintdef(oid) LIKE '%model%'
		  AND pg_get_constraintdef(oid) LIKE '%quality%'
		  AND pg_get_constraintdef(oid) NOT LIKE '%addp-quality%'
	`).Scan(&audienceConstraintCount); err != nil {
		t.Fatalf("inspect execution authorization audience constraint: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM pg_trigger trigger
		JOIN pg_class relation ON relation.oid = trigger.tgrelid
		JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'system'
		  AND relation.relname = 'execution_authorizations'
		  AND NOT trigger.tgisinternal
	`).Scan(&triggerCount); err != nil {
		t.Fatalf("count execution authorization triggers: %v", err)
	}
	if permissionCount != 8 || rolePermissionCount != 19 || triggerCount != 3 || audienceConstraintCount != 1 {
		t.Fatalf("execution authorization catalog permissions=%d role_permissions=%d triggers=%d audience_constraints=%d", permissionCount, rolePermissionCount, triggerCount, audienceConstraintCount)
	}
}

func assertServicePrincipalRuntimeConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var principalCount, clientCount, roleCount, permissionCount int
	var platformRoleCount, platformRoleAssignmentCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.service_principals
		WHERE owner_scope = 'platform'
		  AND name IN ('addp-agent', 'addp-asset', 'addp-copilot', 'addp-develop', 'addp-duckdb', 'addp-gateway', 'addp-graph', 'addp-inference', 'addp-manager', 'addp-meta', 'addp-model', 'addp-monitor', 'addp-orchestrator', 'addp-portal', 'addp-quality', 'addp-service', 'addp-standard', 'addp-transfer')
	`).Scan(&principalCount); err != nil {
		t.Fatalf("count built-in service principals: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.oauth_clients
		WHERE service_principal_id IS NOT NULL
		  AND client_type = 'confidential'
		  AND token_endpoint_auth_method = 'client_secret_basic'
		  AND grant_types = ARRAY['client_credentials']::text[]
		  AND allowed_scopes = ARRAY['addp.api']::text[]
		  AND allowed_audiences = ARRAY['addp.api']::text[]
		  AND client_secret_hash IS NULL
		  AND status = 'disabled'
	`).Scan(&clientCount); err != nil {
		t.Fatalf("count built-in OAuth service clients: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.roles
		WHERE role_key LIKE 'tenant.%_runtime'
		  AND role_type = 'tenant_builtin'
		  AND allowed_scope_types = ARRAY['tenant']::text[]
		  AND allowed_principal_types = ARRAY['service_principal']::text[]
		  AND immutable
	`).Scan(&roleCount); err != nil {
		t.Fatalf("count built-in service runtime roles: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		WHERE role.role_key LIKE 'tenant.%_runtime'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("count built-in service runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.roles
		WHERE role_key IN ('platform.gateway_runtime', 'platform.model_runtime', 'platform.monitor_runtime', 'platform.quality_runtime', 'platform.service_runtime', 'platform.standard_runtime', 'platform.transfer_runtime')
		  AND role_type = 'platform_builtin'
		  AND allowed_scope_types = ARRAY['platform']::text[]
		  AND allowed_principal_types = ARRAY['service_principal']::text[]
		  AND immutable
	`).Scan(&platformRoleCount); err != nil {
		t.Fatalf("count platform service runtime roles: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_assignments assignment
		JOIN system.service_principals service_principal ON service_principal.id = assignment.principal_id
		JOIN system.roles role ON role.id = assignment.role_id
		WHERE service_principal.name IN ('addp-gateway', 'addp-model', 'addp-monitor', 'addp-quality', 'addp-service', 'addp-standard', 'addp-transfer')
		  AND role.role_key IN ('platform.gateway_runtime', 'platform.model_runtime', 'platform.monitor_runtime', 'platform.quality_runtime', 'platform.service_runtime', 'platform.standard_runtime', 'platform.transfer_runtime')
		  AND assignment.scope_type = 'platform'
		  AND assignment.tenant_id IS NULL
		  AND assignment.status = 'active'
	`).Scan(&platformRoleAssignmentCount); err != nil {
		t.Fatalf("count platform service runtime assignments: %v", err)
	}
	if principalCount != 18 || clientCount < 18 || roleCount < 15 || permissionCount < 46 ||
		platformRoleCount != 7 || platformRoleAssignmentCount != 7 {
		t.Fatalf("service runtime catalog principals=%d clients=%d roles=%d permissions=%d platform_roles=%d platform_assignments=%d", principalCount, clientCount, roleCount, permissionCount, platformRoleCount, platformRoleAssignmentCount)
	}
	var managerTenantPermissions, metaTenantPermissions, transferTenantPermissions string
	var developTenantPermissions, copilotTenantPermissions, qualityTenantPermissions string
	var managerPlatformPermissions, metaPlatformPermissions, developPlatformPermissions string
	if err := db.QueryRow(`
		SELECT string_agg(permission.permission_key, ',' ORDER BY permission.permission_key)
		FROM system.roles role
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.manager_runtime'
	`).Scan(&managerTenantPermissions); err != nil {
		t.Fatalf("read tenant.manager_runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT string_agg(permission.permission_key, ',' ORDER BY permission.permission_key)
		FROM system.roles role
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.meta_runtime'
	`).Scan(&metaTenantPermissions); err != nil {
		t.Fatalf("read tenant.meta_runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT string_agg(permission.permission_key, ',' ORDER BY permission.permission_key)
		FROM system.roles role
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.transfer_runtime'
	`).Scan(&transferTenantPermissions); err != nil {
		t.Fatalf("read tenant.transfer_runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT string_agg(permission.permission_key, ',' ORDER BY permission.permission_key)
		FROM system.roles role
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.develop_runtime'
	`).Scan(&developTenantPermissions); err != nil {
		t.Fatalf("read tenant.develop_runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT string_agg(permission.permission_key, ',' ORDER BY permission.permission_key)
		FROM system.roles role
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.copilot_runtime'
	`).Scan(&copilotTenantPermissions); err != nil {
		t.Fatalf("read tenant.copilot_runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT string_agg(permission.permission_key, ',' ORDER BY permission.permission_key)
		FROM system.roles role
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'tenant.quality_runtime'
	`).Scan(&qualityTenantPermissions); err != nil {
		t.Fatalf("read tenant.quality_runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT string_agg(permission.permission_key, ',' ORDER BY permission.permission_key)
		FROM system.roles role
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'platform.meta_runtime'
	`).Scan(&metaPlatformPermissions); err != nil {
		t.Fatalf("read platform.meta_runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT string_agg(permission.permission_key, ',' ORDER BY permission.permission_key)
		FROM system.roles role
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'platform.develop_runtime'
	`).Scan(&developPlatformPermissions); err != nil {
		t.Fatalf("read platform.develop_runtime permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT string_agg(permission.permission_key, ',' ORDER BY permission.permission_key)
		FROM system.roles role
		JOIN system.role_permissions role_permission ON role_permission.role_id = role.id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.role_key = 'platform.manager_runtime'
	`).Scan(&managerPlatformPermissions); err != nil {
		t.Fatalf("read platform.manager_runtime permissions: %v", err)
	}
	if managerTenantPermissions != "inference.runtime.execute,meta.catalog.read,meta.scan_task.execute,system.engine_descriptor.read,system.engine.read,transfer.task.create,transfer.task.execute,transfer.task.read" ||
		metaTenantPermissions != "audit.tenant_event.create,system.engine_descriptor.read,system.engine.read" ||
		transferTenantPermissions != "meta.catalog.read,meta.inspect.execute,meta.scan_task.execute,system.engine_descriptor.read,system.engine.read" ||
		developTenantPermissions != "meta.catalog.read,meta.scan_task.execute,model.materialization_context.read,system.engine_descriptor.read,system.execution_authorization.execute,system.notebook_session_authorization.execute" ||
		copilotTenantPermissions != "develop.task.read,inference.runtime.execute,system.engine_descriptor.read" ||
		qualityTenantPermissions != "meta.catalog.read,standard.element.read,system.engine.read,system.execution_authorization.execute" ||
		metaPlatformPermissions != "system.runtime_registry.update" ||
		developPlatformPermissions != "system.runtime_registry.update" ||
		managerPlatformPermissions != "system.runtime_registry.update" {
		t.Fatalf("runtime permissions manager_tenant=%q meta_tenant=%q transfer_tenant=%q develop_tenant=%q copilot_tenant=%q quality_tenant=%q meta_platform=%q develop_platform=%q manager_platform=%q",
			managerTenantPermissions, metaTenantPermissions, transferTenantPermissions, developTenantPermissions, copilotTenantPermissions, qualityTenantPermissions, metaPlatformPermissions, developPlatformPermissions, managerPlatformPermissions)
	}

	var developPlatformAssignmentCount, managerPlatformAssignmentCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_assignments assignment
		JOIN system.service_principals service_principal ON service_principal.id = assignment.principal_id
		JOIN system.roles role ON role.id = assignment.role_id
		WHERE service_principal.name = 'addp-develop'
		  AND role.role_key = 'platform.develop_runtime'
		  AND assignment.scope_type = 'platform'
		  AND assignment.tenant_id IS NULL
		  AND assignment.status = 'active'
	`).Scan(&developPlatformAssignmentCount); err != nil {
		t.Fatalf("count Develop platform runtime assignment: %v", err)
	}
	if developPlatformAssignmentCount != 1 {
		t.Fatalf("Develop platform runtime assignment count = %d, want 1", developPlatformAssignmentCount)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_assignments assignment
		JOIN system.service_principals service_principal ON service_principal.id = assignment.principal_id
		JOIN system.roles role ON role.id = assignment.role_id
		WHERE service_principal.name = 'addp-manager'
		  AND role.role_key = 'platform.manager_runtime'
		  AND assignment.scope_type = 'platform'
		  AND assignment.tenant_id IS NULL
		  AND assignment.status = 'active'
	`).Scan(&managerPlatformAssignmentCount); err != nil {
		t.Fatalf("count Manager platform runtime assignment: %v", err)
	}
	if managerPlatformAssignmentCount != 1 {
		t.Fatalf("Manager platform runtime assignment count = %d, want 1", managerPlatformAssignmentCount)
	}

	if _, err := db.Exec(`
		INSERT INTO system.oauth_clients (
			client_id, display_name, client_type, redirect_uris, grant_types, response_types,
			allowed_scopes, allowed_audiences, token_endpoint_auth_method, status
		) VALUES (
			'invalid-unbound-service', 'Invalid', 'confidential', ARRAY[]::text[],
			ARRAY['client_credentials']::text[], ARRAY[]::text[], ARRAY['addp.api']::text[],
			ARRAY['addp.api']::text[], 'client_secret_basic', 'disabled'
		)
	`); err == nil {
		t.Fatal("unbound client_credentials OAuth client was accepted")
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_clients
		SET service_principal_id = (
			SELECT id FROM system.service_principals WHERE name = 'addp-monitor'
		)
		WHERE client_id = 'addp-manager'
	`); err == nil {
		t.Fatal("OAuth service principal binding was mutable")
	}
}

func assertAssetServiceRuntimeConstraints(t *testing.T, db *sql.DB) {
	t.Helper()
	var principalCount, roleCount, rolePermissionCount, platformAssignmentCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.service_principals service_principal
		JOIN system.oauth_clients oauth_client
		  ON oauth_client.service_principal_id = service_principal.id
		WHERE service_principal.name = 'addp-asset'
		  AND service_principal.owner_scope = 'platform'
		  AND oauth_client.client_id = 'addp-asset'
		  AND oauth_client.client_type = 'confidential'
		  AND oauth_client.grant_types = ARRAY['client_credentials']::text[]
	`).Scan(&principalCount); err != nil {
		t.Fatalf("count Asset service principal: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.roles
		WHERE tenant_id IS NULL
		  AND role_key IN ('platform.asset_runtime', 'tenant.asset_runtime')
		  AND status = 'active'
	`).Scan(&roleCount); err != nil {
		t.Fatalf("count Asset runtime roles: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE (role.role_key, permission.permission_key) IN (
			('platform.asset_runtime', 'system.runtime_registry.update'),
			('tenant.asset_runtime', 'develop.task.read'),
			('tenant.asset_runtime', 'meta.catalog.read'),
			('tenant.asset_runtime', 'service.definition.read'),
			('tenant.asset_runtime', 'standard.metric.read')
		)
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("count Asset runtime role permissions: %v", err)
	}
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_assignments assignment
		JOIN system.service_principals service_principal ON service_principal.id = assignment.principal_id
		JOIN system.roles role ON role.id = assignment.role_id
		WHERE service_principal.name = 'addp-asset'
		  AND role.role_key = 'platform.asset_runtime'
		  AND assignment.scope_type = 'platform'
		  AND assignment.tenant_id IS NULL
		  AND assignment.status = 'active'
	`).Scan(&platformAssignmentCount); err != nil {
		t.Fatalf("count Asset platform runtime assignment: %v", err)
	}
	if principalCount != 1 || roleCount != 2 || rolePermissionCount != 5 || platformAssignmentCount != 1 {
		t.Fatalf(
			"Asset runtime catalog principals=%d roles=%d role_permissions=%d platform_assignments=%d",
			principalCount, roleCount, rolePermissionCount, platformAssignmentCount,
		)
	}
}

func assertTenantAdministrationClosure(t *testing.T, db *sql.DB) {
	t.Helper()

	var permissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.permissions
		WHERE status = 'active'
		  AND permission_key IN (
		      'platform.tenant.initialize',
		      'iam.tenant_role.create',
		      'iam.tenant_role.delete',
		      'iam.tenant_role.read',
		      'iam.tenant_role.update',
		      'iam.tenant_role_assignment.create',
		      'iam.tenant_role_assignment.read',
		      'iam.tenant_role_assignment.revoke'
		  )
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("read tenant administration permissions: %v", err)
	}
	if permissionCount != 8 {
		t.Fatalf("tenant administration permission count = %d, want 8", permissionCount)
	}

	var columnCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'system'
		  AND table_name = 'tenants'
		  AND column_name IN ('initialized_at', 'initialized_by_principal_id')
	`).Scan(&columnCount); err != nil {
		t.Fatalf("read tenant initialization columns: %v", err)
	}
	if columnCount != 2 {
		t.Fatalf("tenant initialization column count = %d, want 2", columnCount)
	}
}

func assertRoleKeyNamespace(t *testing.T, db *sql.DB) {
	t.Helper()

	var principalID int64
	if err := db.QueryRow(`INSERT INTO system.principals (principal_type) VALUES ('user') RETURNING id`).Scan(&principalID); err != nil {
		t.Fatalf("create role namespace principal: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES ($1, 'Role Namespace User')`, principalID); err != nil {
		t.Fatalf("create role namespace user: %v", err)
	}

	createTenant := func(code string) int64 {
		t.Helper()
		var tenantID int64
		if err := db.QueryRow(`INSERT INTO system.tenants (code, name) VALUES ($1, $1) RETURNING id`, code).Scan(&tenantID); err != nil {
			t.Fatalf("create role namespace tenant %s: %v", code, err)
		}
		if _, err := db.Exec(`
			INSERT INTO system.tenant_memberships
			    (tenant_id, principal_id, source_type, joined_at, created_by_principal_id)
			VALUES ($1, $2, 'manual', now(), $2)
		`, tenantID, principalID); err != nil {
			t.Fatalf("create role namespace membership %s: %v", code, err)
		}
		return tenantID
	}
	firstTenantID := createTenant("role-namespace-first")
	secondTenantID := createTenant("role-namespace-second")

	insertCustomRole := func(tenantID int64, roleKey string) error {
		_, err := db.Exec(`
			INSERT INTO system.roles (
			    tenant_id, role_key, name, role_type, allowed_scope_types,
			    allowed_principal_types, immutable, created_by_principal_id
			) VALUES ($1, $2, 'Namespace Role', 'tenant_custom', ARRAY['tenant'], ARRAY['user'], false, $3)
		`, tenantID, roleKey, principalID)
		return err
	}
	if err := insertCustomRole(firstTenantID, "custom.namespace_operator"); err != nil {
		t.Fatalf("create first tenant custom role: %v", err)
	}
	if err := insertCustomRole(secondTenantID, "custom.namespace_operator"); err != nil {
		t.Fatalf("reuse custom role key in another tenant: %v", err)
	}
	if err := insertCustomRole(firstTenantID, "tenant.administrator"); err == nil {
		t.Fatal("tenant custom role reused a built-in role key")
	}
	if _, err := db.Exec(`
		INSERT INTO system.roles (
		    role_key, name_i18n_key, description_i18n_key, role_type,
		    allowed_scope_types, allowed_principal_types, immutable
		) VALUES (
		    'custom.namespace_operator', 'roles.custom.namespace_operator.name',
		    'roles.custom.namespace_operator.description', 'tenant_builtin',
		    ARRAY['tenant'], ARRAY['user'], true
		)
	`); err == nil {
		t.Fatal("built-in role reused an existing tenant custom role key")
	}
}

func assertIAMRecoveryConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var firstAttemptID int64
	if err := db.QueryRow(`
		INSERT INTO system.iam_recovery_attempts
		    (secret_hash, status, prepared_at, expires_at)
		VALUES ($1, 'prepared', now(), now() + interval '1 hour')
		RETURNING id
	`, tokenHash('a')).Scan(&firstAttemptID); err != nil {
		t.Fatalf("prepare IAM recovery: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.iam_recovery_attempts
		    (secret_hash, status, prepared_at, expires_at)
		VALUES ($1, 'prepared', now(), now() + interval '1 hour')
	`, tokenHash('b')); err == nil {
		t.Fatal("second prepared IAM recovery attempt succeeded")
	}
	if _, err := db.Exec(`
		UPDATE system.iam_recovery_attempts
		SET status = 'completed', secret_hash = NULL, completed_at = now()
		WHERE id = $1
	`, firstAttemptID); err != nil {
		t.Fatalf("complete IAM recovery: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.iam_recovery_attempts
		SET status = 'prepared', secret_hash = $1, completed_at = NULL
		WHERE id = $2
	`, tokenHash('c'), firstAttemptID); err == nil {
		t.Fatal("completed IAM recovery attempt reopened")
	}
	if _, err := db.Exec(`DELETE FROM system.iam_recovery_attempts WHERE id = $1`, firstAttemptID); err == nil {
		t.Fatal("IAM recovery attempt physical delete succeeded")
	}
	if _, err := db.Exec(`TRUNCATE system.iam_recovery_attempts`); err == nil {
		t.Fatal("IAM recovery attempt truncate succeeded")
	}

	var principalID, activeCredentialID int64
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'MFA Bootstrap User'`).Scan(&principalID); err != nil {
		t.Fatalf("find MFA recovery test user: %v", err)
	}
	if err := db.QueryRow(`
		SELECT id FROM system.mfa_credentials
		WHERE user_id = $1 AND method = 'totp' AND status = 'active'
	`, principalID).Scan(&activeCredentialID); err != nil {
		t.Fatalf("find active MFA credential: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.mfa_credentials SET status = 'disabled' WHERE id = $1
	`, activeCredentialID); err != nil {
		t.Fatalf("disable old MFA credential: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.mfa_credentials
		    (user_id, method, secret_ciphertext, secret_nonce, key_version)
		VALUES ($1, 'totp', decode(repeat('12', 32), 'hex'), decode(repeat('34', 12), 'hex'), 1)
	`, principalID); err != nil {
		t.Fatalf("create replacement MFA credential: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.mfa_credentials
		    (user_id, method, secret_ciphertext, secret_nonce, key_version)
		VALUES ($1, 'totp', decode(repeat('56', 32), 'hex'), decode(repeat('78', 12), 'hex'), 1)
	`, principalID); err == nil {
		t.Fatal("second active replacement MFA credential succeeded")
	}
}

func assertManagerAuthorizationCatalog(t *testing.T, db *sql.DB) {
	t.Helper()
	var rolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.tenant_id IS NULL
		  AND role.role_key = 'tenant.data_steward'
		  AND permission.permission_key = 'manager.derived_artifact.update'
		  AND permission.owner_module = 'manager'
		  AND permission.status = 'active'
		  AND role_permission.source_type = 'product'
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("read Data Steward Manager update permission: %v", err)
	}
	if rolePermissionCount != 1 {
		t.Fatalf("Data Steward Manager update permission count = %d, want 1", rolePermissionCount)
	}

	var configurationPermissionCount int
	if err := db.QueryRow(`
			SELECT count(*)
			FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
			JOIN system.permissions permission ON permission.id = role_permission.permission_id
			WHERE role.tenant_id IS NULL
			  AND role.role_key IN ('platform.system_administrator', 'tenant.administrator')
			  AND role.role_type IN ('platform_builtin', 'tenant_builtin')
			  AND permission.permission_key IN (
			      'manager.configuration.read',
			      'manager.configuration.update'
			  )
			  AND permission.owner_module = 'manager'
			  AND permission.allowed_scope_types = ARRAY['platform', 'tenant']::text[]
			  AND permission.status = 'active'
			  AND role_permission.source_type = 'product'
		`).Scan(&configurationPermissionCount); err != nil {
		t.Fatalf("read Manager configuration permissions: %v", err)
	}
	if configurationPermissionCount != 4 {
		t.Fatalf("Manager configuration permission count = %d, want 4", configurationPermissionCount)
	}
}

func assertManagerDataProfileAuthorizationCatalog(t *testing.T, db *sql.DB) {
	t.Helper()
	var rolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.tenant_id IS NULL
		  AND role.role_key = 'tenant.data_engineer'
		  AND permission.permission_key = 'manager.data_profile.execute'
		  AND permission.owner_module = 'manager'
		  AND permission.action = 'execute'
		  AND permission.risk_level = 'medium'
		  AND permission.delegable = false
		  AND permission.allowed_scope_types = ARRAY['tenant', 'department', 'project_group']::text[]
		  AND permission.tenant_customizable = true
		  AND permission.status = 'active'
		  AND role_permission.source_type = 'product'
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("read Data Engineer Manager data profile permission: %v", err)
	}
	if rolePermissionCount != 1 {
		t.Fatalf("Data Engineer Manager data profile permission count = %d, want 1", rolePermissionCount)
	}
}

func assertAuthorizationCatalogRetirement(t *testing.T, db *sql.DB) {
	t.Helper()
	var activePermissionCount, disabledPermissionCount int
	if err := db.QueryRow(`
		SELECT
		    count(*) FILTER (WHERE status = 'active'),
		    count(*) FILTER (WHERE status = 'disabled')
		FROM system.permissions
	`).Scan(&activePermissionCount, &disabledPermissionCount); err != nil {
		t.Fatalf("read retired Permission counts: %v", err)
	}
	if activePermissionCount < 271 || disabledPermissionCount != 65 {
		t.Fatalf("Permission status counts = active:%d disabled:%d, want at least 271 and exactly 65", activePermissionCount, disabledPermissionCount)
	}

	var disabledRoles string
	if err := db.QueryRow(`
		SELECT string_agg(role_key, ',' ORDER BY role_key)
		FROM system.roles
		WHERE tenant_id IS NULL AND status = 'disabled'
	`).Scan(&disabledRoles); err != nil {
		t.Fatalf("read disabled builtin Roles: %v", err)
	}
	wantDisabledRoles := "platform.statistics_viewer,tenant.department_coordinator,tenant.project_group_coordinator"
	if disabledRoles != wantDisabledRoles {
		t.Fatalf("disabled builtin Roles = %q, want %q", disabledRoles, wantDisabledRoles)
	}

	var invalidRolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions AS role_permission
		JOIN system.permissions AS permission ON permission.id = role_permission.permission_id
		JOIN system.roles AS role ON role.id = role_permission.role_id
		WHERE permission.status <> 'active' OR role.status <> 'active'
	`).Scan(&invalidRolePermissionCount); err != nil {
		t.Fatalf("validate retired Role Permissions: %v", err)
	}
	if invalidRolePermissionCount != 0 {
		t.Fatalf("Role Permissions referencing disabled catalog entries = %d, want 0", invalidRolePermissionCount)
	}
}

func assertStandardAuthorizationCatalog(t *testing.T, db *sql.DB) {
	t.Helper()
	var permissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.permissions
		WHERE owner_module = 'standard'
		  AND status = 'active'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("read Standard authorization permissions: %v", err)
	}
	if permissionCount != 41 {
		t.Fatalf("Standard authorization permission count = %d, want 41", permissionCount)
	}

	var rolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.tenant_id IS NULL
		  AND role.role_key = 'tenant.governance_manager'
		  AND permission.owner_module = 'standard'
		  AND role_permission.source_type = 'product'
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("read Governance Manager Standard permissions: %v", err)
	}
	if rolePermissionCount != 41 {
		t.Fatalf("Governance Manager Standard permission count = %d, want 41", rolePermissionCount)
	}
}

func assertModelAuthorizationCatalog(t *testing.T, db *sql.DB) {
	t.Helper()
	var permissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.permissions
		WHERE owner_module = 'model'
		  AND status = 'active'
		  AND (
		      permission_key LIKE 'model.entity.%'
		      OR permission_key LIKE 'model.entity_relation.%'
		      OR permission_key LIKE 'model.dw_layer.%'
		  )
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("read Model authorization permissions: %v", err)
	}
	if permissionCount != 13 {
		t.Fatalf("Model authorization permission count = %d, want 13", permissionCount)
	}

	var rolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.tenant_id IS NULL
		  AND role.role_key = 'tenant.data_architect'
		  AND permission.permission_key IN (
		      'model.entity.approve',
		      'model.entity.create',
		      'model.entity.delete',
		      'model.entity.read',
		      'model.entity.update',
		      'model.entity_relation.create',
		      'model.entity_relation.delete',
		      'model.entity_relation.read',
		      'model.entity_relation.update'
		  )
		  AND role_permission.source_type = 'product'
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("read Data Architect Model permissions: %v", err)
	}
	if rolePermissionCount != 9 {
		t.Fatalf("Data Architect Model permission count = %d, want 9", rolePermissionCount)
	}
}

func assertMonitorAuthorizationCatalog(t *testing.T, db *sql.DB) {
	t.Helper()
	var permissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.permissions
		WHERE owner_module = 'monitor'
		  AND status = 'active'
		  AND (
		      permission_key LIKE 'monitor.alert_incident.%'
		      OR permission_key LIKE 'monitor.alert_rule.%'
		      OR permission_key LIKE 'monitor.notification_delivery.%'
		      OR permission_key LIKE 'monitor.notification_destination.%'
		  )
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("read Monitor authorization permissions: %v", err)
	}
	if permissionCount != 13 {
		t.Fatalf("Monitor authorization permission count = %d, want 13", permissionCount)
	}

	var rolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.tenant_id IS NULL
		  AND role.role_key = 'tenant.monitoring_operator'
		  AND role.role_type = 'tenant_builtin'
		  AND role.allowed_scope_types = ARRAY['tenant']::text[]
		  AND role_permission.source_type = 'product'
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("read Tenant Monitoring Operator permissions: %v", err)
	}
	if rolePermissionCount != 16 {
		t.Fatalf("Tenant Monitoring Operator permission count = %d, want 16", rolePermissionCount)
	}
}

func assertStandardDocumentCatalog(t *testing.T, db *sql.DB) {
	t.Helper()
	var permissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.permissions
		WHERE permission_key IN (
			'standard.document.create',
			'standard.document.delete',
			'standard.document.read',
			'standard.document.update'
		)
		  AND owner_module = 'standard'
		  AND status = 'active'
	`).Scan(&permissionCount); err != nil {
		t.Fatalf("read Standard document permissions: %v", err)
	}
	if permissionCount != 4 {
		t.Fatalf("Standard document permission count = %d, want 4", permissionCount)
	}

	var rolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.roles role ON role.id = role_permission.role_id
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role.tenant_id IS NULL
		  AND role.role_key = 'tenant.governance_manager'
		  AND permission.permission_key LIKE 'standard.document.%'
		  AND role_permission.source_type = 'product'
	`).Scan(&rolePermissionCount); err != nil {
		t.Fatalf("read Governance Manager document permissions: %v", err)
	}
	if rolePermissionCount != 4 {
		t.Fatalf("Governance Manager document permission count = %d, want 4", rolePermissionCount)
	}
}

func assertMFABootstrapConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	principalID := createMigrationUser(t, db, "MFA Bootstrap User")
	var credentialID int64
	if err := db.QueryRow(`
		INSERT INTO system.mfa_credentials
		    (user_id, method, secret_ciphertext, secret_nonce, key_version)
		VALUES ($1, 'totp', decode(repeat('ab', 32), 'hex'), decode(repeat('cd', 12), 'hex'), 1)
		RETURNING id
	`, principalID).Scan(&credentialID); err != nil {
		t.Fatalf("create MFA credential: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.mfa_credentials
		    (user_id, method, secret_ciphertext, secret_nonce, key_version)
		VALUES ($1, 'totp', decode(repeat('ef', 32), 'hex'), decode(repeat('01', 12), 'hex'), 1)
	`, principalID); err == nil {
		t.Fatal("duplicate TOTP credential succeeded")
	}
	if _, err := db.Exec(`UPDATE system.mfa_credentials SET last_accepted_counter = 100 WHERE id = $1`, credentialID); err != nil {
		t.Fatalf("record MFA counter: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.mfa_credentials SET last_accepted_counter = 100 WHERE id = $1`, credentialID); err == nil {
		t.Fatal("replayed MFA counter succeeded")
	}

	var challengeID int64
	if err := db.QueryRow(`
		INSERT INTO system.mfa_challenges
		    (token_hash, principal_id, issued_authorization_version,
		     authentication_methods, authenticated_at, expires_at, purpose)
		VALUES ($1, $2, 1, ARRAY['password'], now(), now() + interval '5 minutes', 'login')
		RETURNING id
	`, tokenHash('d'), principalID).Scan(&challengeID); err != nil {
		t.Fatalf("create MFA challenge: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.mfa_challenges SET consumed_at = now() WHERE id = $1`, challengeID); err != nil {
		t.Fatalf("consume MFA challenge: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.mfa_challenges SET consumed_at = now() WHERE id = $1`, challengeID); err == nil {
		t.Fatal("MFA challenge was consumed twice")
	}
	if _, err := db.Exec(`
		INSERT INTO system.mfa_challenges
		    (token_hash, principal_id, issued_authorization_version,
		     authentication_methods, authenticated_at, expires_at, purpose)
		VALUES ($1, $2, 1, ARRAY['password'], now(), now() + interval '5 minutes', 'step_up')
	`, tokenHash('9'), principalID); err == nil {
		t.Fatal("step-up MFA challenge without source family succeeded")
	}

	if _, err := db.Exec(`
		INSERT INTO system.iam_bootstrap_state
		    (status, secret_hash, prepared_at, expires_at)
		VALUES ('prepared', $1, now(), now() + interval '1 hour')
	`, tokenHash('e')); err != nil {
		t.Fatalf("prepare IAM bootstrap: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.iam_bootstrap_state
		    (status, secret_hash, prepared_at, expires_at)
		VALUES ('prepared', $1, now(), now() + interval '1 hour')
	`, tokenHash('f')); err == nil {
		t.Fatal("second IAM bootstrap state succeeded")
	}
	if _, err := db.Exec(`
		UPDATE system.iam_bootstrap_state
		SET status = 'completed', secret_hash = NULL, completed_at = now()
	`); err != nil {
		t.Fatalf("complete IAM bootstrap: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.iam_bootstrap_state SET status = 'prepared', secret_hash = $1, completed_at = NULL`, tokenHash('a')); err == nil {
		t.Fatal("completed IAM bootstrap reopened")
	}
	if _, err := db.Exec(`DELETE FROM system.mfa_credentials WHERE id = $1`, credentialID); err == nil {
		t.Fatal("MFA credential physical delete succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.mfa_challenges WHERE id = $1`, challengeID); err == nil {
		t.Fatal("MFA challenge physical delete succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.iam_bootstrap_state`); err == nil {
		t.Fatal("IAM bootstrap state physical delete succeeded")
	}
}

func assertInvitationEnrollmentConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var tenantID, creatorPrincipalID int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find invitation test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&creatorPrincipalID); err != nil {
		t.Fatalf("find invitation creator: %v", err)
	}
	invitedPrincipalID := createMigrationUser(t, db, "Invitation User")

	var invitationID int64
	if err := db.QueryRow(`
		INSERT INTO system.tenant_invitations
		    (tenant_id, email, normalized_email, secret_hash, expires_at, created_by_principal_id)
		VALUES ($1, 'Invitee@Example.Test', 'invitee@example.test', $2, now() + interval '7 days', $3)
		RETURNING id
	`, tenantID, tokenHash('a'), creatorPrincipalID).Scan(&invitationID); err != nil {
		t.Fatalf("create tenant invitation: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_invitations
		    (tenant_id, email, normalized_email, secret_hash, expires_at, created_by_principal_id)
		VALUES ($1, 'INVITEE@example.test', 'invitee@example.test', $2, now() + interval '7 days', $3)
	`, tenantID, tokenHash('b'), creatorPrincipalID); err == nil {
		t.Fatal("duplicate pending invitation for tenant and normalized email succeeded")
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_invitations
		    (tenant_id, email, normalized_email, secret_hash, expires_at, created_by_principal_id)
		VALUES ($1, 'bad@example.test', 'bad@example.test', 'not-a-hash', now() + interval '7 days', $2)
	`, tenantID, creatorPrincipalID); err == nil {
		t.Fatal("tenant invitation accepted a non-SHA256 secret hash")
	}

	var authorizationVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, invitedPrincipalID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("read invited principal authorization version: %v", err)
	}
	var ticketID int64
	if err := db.QueryRow(`
		INSERT INTO system.enrollment_tickets
		    (token_hash, principal_id, invitation_id, issued_authorization_version, authenticated_at, expires_at)
		VALUES ($1, $2, $3, $4, now(), now() + interval '5 minutes')
		RETURNING id
	`, tokenHash('c'), invitedPrincipalID, invitationID, authorizationVersion).Scan(&ticketID); err != nil {
		t.Fatalf("create enrollment ticket: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.enrollment_tickets SET principal_id = $1, consumed_at = now() WHERE id = $2`, creatorPrincipalID, ticketID); err == nil {
		t.Fatal("enrollment ticket principal binding update succeeded")
	}
	if _, err := db.Exec(`UPDATE system.enrollment_tickets SET consumed_at = now() WHERE id = $1`, ticketID); err != nil {
		t.Fatalf("consume enrollment ticket: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.enrollment_tickets SET consumed_at = now() WHERE id = $1`, ticketID); err == nil {
		t.Fatal("enrollment ticket was consumed twice")
	}

	if _, err := db.Exec(`
		UPDATE system.tenant_invitations
		SET status = 'accepted', accepted_at = now(), accepted_by_principal_id = $1
		WHERE id = $2
	`, invitedPrincipalID, invitationID); err != nil {
		t.Fatalf("accept tenant invitation: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.tenant_invitations SET status = 'pending', accepted_at = NULL, accepted_by_principal_id = NULL WHERE id = $1`, invitationID); err == nil {
		t.Fatal("accepted tenant invitation returned to pending")
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, source_type, source_ref, joined_at, created_by_principal_id)
		VALUES ($1, $2, 'invitation', $3, now(), $4)
	`, tenantID, invitedPrincipalID, fmt.Sprint(invitationID), creatorPrincipalID); err != nil {
		t.Fatalf("create invitation-sourced tenant membership: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM system.tenant_invitations WHERE id = $1`, invitationID); err == nil {
		t.Fatal("tenant invitation physical delete succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.enrollment_tickets WHERE id = $1`, ticketID); err == nil {
		t.Fatal("enrollment ticket physical delete succeeded")
	}
	if _, err := db.Exec(`TRUNCATE system.enrollment_tickets`); err == nil {
		t.Fatal("enrollment ticket truncate succeeded")
	}
}

func assertAuditContextConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var tenantID, userPrincipalID int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find audit test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&userPrincipalID); err != nil {
		t.Fatalf("find audit test user: %v", err)
	}

	var auditLogID int64
	if err := db.QueryRow(`
		INSERT INTO system.audit_logs
		    (principal_id, principal_type, context_type, tenant_id,
		     event_name, result, risk_level, module_name,
		     http_method, resource_path, http_status, request_id, ip_address,
		     entity_type, entity_id, details)
		VALUES
		    ($1, 'user', 'tenant', $2,
		     'iam.tenant_membership.suspended', 'succeeded', 'high', 'system',
		     'POST', '/api/v1/system/tenant-memberships/1/suspend', 200,
		     'audit-request-1', '127.0.0.1', 'tenant_membership', '1',
		     '{"reason":"security review","authorization_version":3}'::jsonb)
		RETURNING id
	`, userPrincipalID, tenantID).Scan(&auditLogID); err != nil {
		t.Fatalf("create tenant audit event: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, entity_type, entity_id, details)
		VALUES
		    ('oauth.token.failed', 'failed', 'medium', 'system',
		     'oauth_security_event', 'oauth.token.failed',
		     '{"client_id":"addp-cli","grant_type":"refresh_token","error_code":"invalid_grant"}'::jsonb)
	`); err != nil {
		t.Fatalf("create no-context OAuth audit event: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (principal_id, principal_type, event_name, result, risk_level, module_name)
		VALUES ($1, 'service_principal', 'iam.identity.updated', 'succeeded', 'low', 'system')
	`, userPrincipalID); err == nil {
		t.Fatal("audit event accepted a mismatched principal type")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (context_type, event_name, result, risk_level, module_name)
		VALUES ('tenant', 'iam.identity.updated', 'succeeded', 'low', 'system')
	`); err == nil {
		t.Fatal("tenant audit context succeeded without tenant")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (context_type, tenant_id, event_name, result, risk_level, module_name)
		VALUES ('platform', $1, 'iam.identity.updated', 'succeeded', 'low', 'system')
	`, tenantID); err == nil {
		t.Fatal("platform audit context accepted a tenant")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (tenant_id, event_name, result, risk_level, module_name)
		VALUES ($1, 'iam.identity.updated', 'succeeded', 'low', 'system')
	`, tenantID); err == nil {
		t.Fatal("no-context audit event accepted a tenant")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, details)
		VALUES
		    ('oauth.token.failed', 'failed', 'high', 'system',
		     '{"oauth":{"refresh_token":"secret"}}'::jsonb)
	`); err == nil {
		t.Fatal("audit details accepted a nested refresh token")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, details)
		VALUES ('iam.identity.updated', 'succeeded', 'low', 'system', '[]'::jsonb)
	`); err == nil {
		t.Fatal("audit details accepted a non-object JSON value")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name,
		     entity_type, entity_id, details)
		VALUES
		    ('oauth.token.failed', 'failed', 'medium', 'system',
		     'oauth_security_event', 'oauth.token.failed',
		     '{"client_id":"addp-cli","request_secret":"secret"}'::jsonb)
	`); err == nil {
		t.Fatal("OAuth audit details accepted a field outside the whitelist")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, entity_type, entity_id)
		VALUES
		    ('oauth.token.failed', 'failed', 'medium', 'system',
		     'oauth_security_event', 'oauth.token.issued')
	`); err == nil {
		t.Fatal("OAuth audit event accepted mismatched event and entity identifiers")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, entity_type, entity_id)
		VALUES
		    ('oauth.token.refresh_reuse_detected', 'failed', 'medium', 'system',
		     'oauth_security_event', 'oauth.token.refresh_reuse_detected')
	`); err == nil {
		t.Fatal("refresh token reuse audit event accepted a non-high risk level")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, http_method)
		VALUES ('iam.identity.updated', 'succeeded', 'low', 'system', 'POST')
	`); err == nil {
		t.Fatal("audit event accepted a partial HTTP context")
	}

	if _, err := db.Exec(`UPDATE system.audit_logs SET result = 'failed' WHERE id = $1`, auditLogID); err == nil {
		t.Fatal("audit log update succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.audit_logs WHERE id = $1`, auditLogID); err == nil {
		t.Fatal("audit log delete succeeded")
	}
	if _, err := db.Exec(`TRUNCATE system.audit_logs`); err == nil {
		t.Fatal("audit log truncate succeeded")
	}

	var columns string
	if err := db.QueryRow(`
		SELECT string_agg(column_name, ',' ORDER BY ordinal_position)
		FROM information_schema.columns
		WHERE table_schema = 'system' AND table_name = 'audit_logs'
	`).Scan(&columns); err != nil {
		t.Fatalf("read audit log columns: %v", err)
	}
	wantColumns := "id,principal_id,principal_type,context_type,tenant_id,event_name,result,risk_level,module_name,http_method,resource_path,http_status,request_id,ip_address,user_agent,entity_type,entity_id,details,created_at"
	if columns != wantColumns {
		t.Fatalf("audit log columns = %q, want %q", columns, wantColumns)
	}

	var auditTableCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'system'
		  AND table_type = 'BASE TABLE'
		  AND table_name LIKE '%audit%'
	`).Scan(&auditTableCount); err != nil {
		t.Fatalf("count audit tables: %v", err)
	}
	if auditTableCount != 1 {
		t.Fatalf("system audit table count = %d, want 1", auditTableCount)
	}

	var indexes string
	if err := db.QueryRow(`
		SELECT string_agg(indexname, ',' ORDER BY indexname)
		FROM pg_indexes
		WHERE schemaname = 'system'
		  AND tablename = 'audit_logs'
		  AND indexname <> 'audit_logs_pkey'
	`).Scan(&indexes); err != nil {
		t.Fatalf("read audit log indexes: %v", err)
	}
	wantIndexes := "idx_audit_logs_created_at,idx_audit_logs_entity,idx_audit_logs_event_created_at,idx_audit_logs_high_risk_created_at,idx_audit_logs_principal_created_at,idx_audit_logs_request_id,idx_audit_logs_tenant_created_at"
	if indexes != wantIndexes {
		t.Fatalf("audit log indexes = %q, want %q", indexes, wantIndexes)
	}

	var publicMutationPrivilegeCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM information_schema.role_table_grants
		WHERE table_schema = 'system'
		  AND table_name = 'audit_logs'
		  AND grantee = 'PUBLIC'
		  AND privilege_type IN ('UPDATE', 'DELETE', 'TRUNCATE')
	`).Scan(&publicMutationPrivilegeCount); err != nil {
		t.Fatalf("read public audit privileges: %v", err)
	}
	if publicMutationPrivilegeCount != 0 {
		t.Fatalf("public audit mutation privilege count = %d, want 0", publicMutationPrivilegeCount)
	}
}

func assertIAMCatalogSeed(t *testing.T, db *sql.DB) {
	t.Helper()

	assertTableCountAtLeast(t, db, "system.permissions", 336)
	assertTableCountAtLeast(t, db, "system.roles", 43)
	assertTableCountAtLeast(t, db, "system.role_permissions", 343)
	assertTableCount(t, db, "system.role_conflicts", 3)
	assertTableCountAtLeast(t, db, "system.oauth_clients", 16)
	assertTableCountAtLeast(t, db, "system.principals", 15)
	assertTableCountAtLeast(t, db, "system.service_principals", 15)
	assertTableCount(t, db, "system.tenants", 0)
	assertTableCountAtLeast(t, db, "system.role_assignments", 10)

	var ownerCount, systemPermissionCount int
	if err := db.QueryRow(`SELECT count(DISTINCT owner_module), count(*) FILTER (WHERE owner_module = 'system') FROM system.permissions`).Scan(&ownerCount, &systemPermissionCount); err != nil {
		t.Fatalf("read seeded Permission owners: %v", err)
	}
	if ownerCount != 16 || systemPermissionCount != 116 {
		t.Fatalf("seeded Permission owners = %d and System Permissions = %d, want 16 and 116", ownerCount, systemPermissionCount)
	}

	var invalidRoleCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.roles
		WHERE tenant_id IS NOT NULL
		   OR role_type NOT IN ('platform_builtin', 'tenant_builtin')
		   OR NOT immutable
		   OR created_by_principal_id IS NOT NULL
	`).Scan(&invalidRoleCount); err != nil {
		t.Fatalf("validate seeded builtin Roles: %v", err)
	}
	if invalidRoleCount != 0 {
		t.Fatalf("invalid seeded builtin Role count = %d", invalidRoleCount)
	}

	var invalidRolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		JOIN system.roles role ON role.id = role_permission.role_id
		WHERE role_permission.source_type <> 'product'
		   OR role_permission.created_by_principal_id IS NOT NULL
		   OR permission.status <> 'active'
		   OR role.status <> 'active'
	`).Scan(&invalidRolePermissionCount); err != nil {
		t.Fatalf("validate seeded Role Permissions: %v", err)
	}
	if invalidRolePermissionCount != 0 {
		t.Fatalf("invalid seeded Role Permission count = %d", invalidRolePermissionCount)
	}

	var securityPolicyBindingCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		JOIN system.roles role ON role.id = role_permission.role_id
		WHERE role.role_key = 'platform.security_administrator'
		  AND permission.permission_key IN ('iam.security_policy.read', 'iam.security_policy.update')
	`).Scan(&securityPolicyBindingCount); err != nil {
		t.Fatalf("read IAM security policy role bindings: %v", err)
	}
	if securityPolicyBindingCount != 2 {
		t.Fatalf("IAM security policy role binding count = %d, want 2", securityPolicyBindingCount)
	}

	var conflicts string
	if err := db.QueryRow(`
		SELECT string_agg(low_role.role_key || ':' || high_role.role_key, ',' ORDER BY low_role.role_key, high_role.role_key)
		FROM system.role_conflicts conflict
		JOIN system.roles low_role ON low_role.id = conflict.role_id_low
		JOIN system.roles high_role ON high_role.id = conflict.role_id_high
		WHERE conflict.reason = 'platform_three_administrators_separation_of_duties'
	`).Scan(&conflicts); err != nil {
		t.Fatalf("read platform administrator conflicts: %v", err)
	}
	wantConflicts := "platform.audit_administrator:platform.security_administrator," +
		"platform.audit_administrator:platform.system_administrator," +
		"platform.security_administrator:platform.system_administrator"
	if conflicts != wantConflicts {
		t.Fatalf("platform administrator conflicts = %q, want %q", conflicts, wantConflicts)
	}

	var validClientCount, firstPartyClientCount int
	if err := db.QueryRow(`
		SELECT
		    count(*) FILTER (
		        WHERE client_id = 'addp-cli'
		          AND display_name = 'ADDP CLI'
		          AND client_type = 'public'
		          AND client_secret_hash IS NULL
		          AND redirect_uris = ARRAY['http://127.0.0.1/callback']::text[]
		          AND grant_types = ARRAY['authorization_code', 'refresh_token', 'urn:ietf:params:oauth:grant-type:device_code']::text[]
		          AND response_types = ARRAY['code']::text[]
		          AND allowed_scopes = ARRAY['addp.api']::text[]
		          AND allowed_audiences = ARRAY['addp.api']::text[]
		          AND token_endpoint_auth_method = 'none'
		          AND status = 'active'
		    ),
		    count(*) FILTER (WHERE client_id = 'addp-web')
		FROM system.oauth_clients
	`).Scan(&validClientCount, &firstPartyClientCount); err != nil {
		t.Fatalf("validate seeded OAuth Client: %v", err)
	}
	if validClientCount != 1 || firstPartyClientCount != 0 {
		t.Fatalf("seeded OAuth Client counts = valid:%d addp-web:%d, want 1 and 0", validClientCount, firstPartyClientCount)
	}
}

func assertTableCount(t *testing.T, db *sql.DB, tableName string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT count(*) FROM ` + tableName).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", tableName, got, want)
	}
}

func assertTableCountAtLeast(t *testing.T, db *sql.DB, tableName string, minimum int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT count(*) FROM ` + tableName).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	if got < minimum {
		t.Fatalf("%s count = %d, want at least %d", tableName, got, minimum)
	}
}

func assertSessionTokenFamilyConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var tenantID, tenantUserID, tenantMembershipID, otherMembershipID, authorizationVersion int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find session test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&tenantUserID); err != nil {
		t.Fatalf("find session test user: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id = $2`, tenantID, tenantUserID).Scan(&tenantMembershipID); err != nil {
		t.Fatalf("find session test membership: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id <> $2 LIMIT 1`, tenantID, tenantUserID).Scan(&otherMembershipID); err != nil {
		t.Fatalf("find other tenant membership: %v", err)
	}
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, tenantUserID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("read session test authorization version: %v", err)
	}

	var selectionTicketID int64
	if err := db.QueryRow(`
		INSERT INTO system.context_selection_tickets
		    (token_hash, principal_id, issued_authorization_version, authentication_methods, assurance_level, authenticated_at, expires_at)
		VALUES ($1, $2, $3, ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '5 minutes')
		RETURNING id
	`, tokenHash('a'), tenantUserID, authorizationVersion).Scan(&selectionTicketID); err != nil {
		t.Fatalf("create context selection ticket: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.context_selection_ticket_options (ticket_id, context_type, tenant_membership_id)
		VALUES ($1, 'tenant', $2)
	`, selectionTicketID, tenantMembershipID); err != nil {
		t.Fatalf("create tenant context option: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.context_selection_ticket_options (ticket_id, context_type, tenant_membership_id)
		VALUES ($1, 'tenant', $2)
	`, selectionTicketID, otherMembershipID); err == nil {
		t.Fatal("context option accepted another principal's tenant membership")
	}
	if _, err := db.Exec(`UPDATE system.context_selection_tickets SET consumed_at = now() WHERE id = $1`, selectionTicketID); err != nil {
		t.Fatalf("consume context selection ticket: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.context_selection_tickets SET consumed_at = now() WHERE id = $1`, selectionTicketID); err == nil {
		t.Fatal("context selection ticket was consumed twice")
	}

	platformUserID := createMigrationUser(t, db, "Session Platform User")
	var platformRoleID int64
	if err := db.QueryRow(`SELECT id FROM system.roles WHERE role_key = 'platform.test_administrator'`).Scan(&platformRoleID); err != nil {
		t.Fatalf("find platform role for session test: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments (principal_id, role_id, scope_type, source_type)
		VALUES ($1, $2, 'platform', 'bootstrap')
	`, platformUserID, platformRoleID); err != nil {
		t.Fatalf("bootstrap platform role for session test: %v", err)
	}
	var platformVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, platformUserID).Scan(&platformVersion); err != nil {
		t.Fatalf("read platform session authorization version: %v", err)
	}
	var lowAALTicketID int64
	if err := db.QueryRow(`
		INSERT INTO system.context_selection_tickets
		    (token_hash, principal_id, issued_authorization_version, authentication_methods, assurance_level, authenticated_at, expires_at)
		VALUES ($1, $2, $3, ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '5 minutes')
		RETURNING id
	`, tokenHash('b'), platformUserID, platformVersion).Scan(&lowAALTicketID); err != nil {
		t.Fatalf("create low-AAL platform ticket: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.context_selection_ticket_options (ticket_id, context_type) VALUES ($1, 'platform')`, lowAALTicketID); err == nil {
		t.Fatal("platform context option accepted aal1")
	}
	var platformTicketID int64
	if err := db.QueryRow(`
		INSERT INTO system.context_selection_tickets
		    (token_hash, principal_id, issued_authorization_version, authentication_methods, assurance_level, authenticated_at, expires_at)
		VALUES ($1, $2, $3, ARRAY['password', 'totp'], 'aal2', now() - interval '1 minute', now() + interval '5 minutes')
		RETURNING id
	`, tokenHash('c'), platformUserID, platformVersion).Scan(&platformTicketID); err != nil {
		t.Fatalf("create platform context ticket: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.context_selection_ticket_options (ticket_id, context_type) VALUES ($1, 'platform')`, platformTicketID); err != nil {
		t.Fatalf("create platform context option: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.context_selection_tickets SET step_up_expires_at = now() WHERE id = $1`, platformTicketID); err == nil {
		t.Fatal("context selection ticket allowed step-up facts to change")
	}
	if _, err := db.Exec(`
		INSERT INTO system.context_selection_tickets
		    (token_hash, principal_id, issued_authorization_version, authentication_methods,
		     assurance_level, authenticated_at, step_up_expires_at, expires_at)
		VALUES
		    ($1, $2, $3, ARRAY['password', 'totp'], 'aal2', now() - interval '1 minute',
		     now() - interval '2 minutes', now() + interval '5 minutes')
	`, tokenHash('d'), platformUserID, platformVersion); err == nil {
		t.Fatal("context selection ticket accepted step-up expiry before authentication")
	}

	var familyID int64
	if err := db.QueryRow(`
		INSERT INTO system.refresh_token_families
		    (principal_id, context_type, tenant_membership_id, issued_authorization_version,
		     client_id, auth_type, audiences, scopes, authentication_methods, assurance_level,
		     authenticated_at, expires_at)
		VALUES
		    ($1, 'tenant', $2, $3, 'addp-web', 'first_party', ARRAY['addp.api'], ARRAY[]::text[],
		     ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '1 hour')
		RETURNING id
	`, tenantUserID, tenantMembershipID, authorizationVersion).Scan(&familyID); err != nil {
		t.Fatalf("create first-party tenant family: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.refresh_token_families
		    (principal_id, context_type, tenant_membership_id, issued_authorization_version,
		     client_id, auth_type, audiences, scopes, authentication_methods, assurance_level,
		     authenticated_at, expires_at)
		VALUES
		    ($1, 'tenant', $2, $3, 'addp-web', 'first_party', ARRAY['addp.api'], ARRAY[]::text[],
		     ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '1 hour')
	`, tenantUserID, tenantMembershipID, authorizationVersion-1); err == nil {
		t.Fatal("token family accepted a stale authorization version")
	}
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET expires_at = expires_at + interval '1 hour' WHERE id = $1`, familyID); err == nil {
		t.Fatal("token family final expiry update succeeded")
	}

	var accessTokenID int64
	if err := db.QueryRow(`
		INSERT INTO system.access_tokens (token_hash, family_id, expires_at)
		VALUES ($1, $2, now() + interval '15 minutes')
		RETURNING id
	`, tokenHash('d'), familyID).Scan(&accessTokenID); err != nil {
		t.Fatalf("create access token: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.access_tokens (token_hash, family_id, expires_at) VALUES ($1, $2, now() + interval '15 minutes')`, tokenHash('e'), familyID); err == nil {
		t.Fatal("family accepted two active access tokens")
	}

	var refreshTokenID int64
	if err := db.QueryRow(`
		INSERT INTO system.refresh_tokens (token_hash, family_id, issued_access_token_id, expires_at)
		VALUES ($1, $2, $3, now() + interval '59 minutes')
		RETURNING id
	`, tokenHash('f'), familyID, accessTokenID).Scan(&refreshTokenID); err != nil {
		t.Fatalf("create refresh token: %v", err)
	}

	var delegatedTokenID int64
	if err := db.QueryRow(`
		INSERT INTO system.delegated_access_tokens
		    (token_hash, source_access_token_id, audience, scopes, agent_run_id, tool_call_id, expires_at)
		VALUES ($1, $2, 'develop', ARRAY['workflow.run'], 'run-session-test', 'call-session-test', now() + interval '2 minutes')
		RETURNING id
	`, tokenHash('1'), accessTokenID).Scan(&delegatedTokenID); err != nil {
		t.Fatalf("create delegated access token: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.delegated_access_tokens
		    (token_hash, source_access_token_id, audience, scopes, agent_run_id, tool_call_id, expires_at)
		VALUES ($1, $2, 'develop', ARRAY['workflow.run'], 'run-too-long', 'call-too-long', now() + interval '20 minutes')
	`, tokenHash('2'), accessTokenID); err == nil {
		t.Fatal("delegated token expiry exceeded its source access token")
	}

	var resourceTicketID int64
	if err := db.QueryRow(`
		INSERT INTO system.resource_access_tickets (token_hash, family_id, owner, expires_at)
		VALUES ($1, $2, 'manager', now() + interval '15 minutes')
		RETURNING id
	`, tokenHash('3'), familyID).Scan(&resourceTicketID); err != nil {
		t.Fatalf("create resource access ticket: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.resource_access_tickets (token_hash, family_id, owner, expires_at) VALUES ($1, $2, 'manager', now() + interval '10 minutes')`, tokenHash('4'), familyID); err == nil {
		t.Fatal("family accepted two active resource tickets for one owner")
	}

	if _, err := db.Exec(`UPDATE system.access_tokens SET revoked_at = now() WHERE id = $1`, accessTokenID); err != nil {
		t.Fatalf("revoke source access token: %v", err)
	}
	assertTimestampSet(t, db, `SELECT revoked_at FROM system.delegated_access_tokens WHERE id = $1`, delegatedTokenID, "delegated token source revocation")
	if _, err := db.Exec(`UPDATE system.refresh_tokens SET used_at = now() WHERE id = $1`, refreshTokenID); err != nil {
		t.Fatalf("mark refresh token used: %v", err)
	}

	var replacementAccessTokenID, replacementRefreshTokenID int64
	if err := db.QueryRow(`
		INSERT INTO system.access_tokens (token_hash, family_id, expires_at)
		VALUES ($1, $2, now() + interval '15 minutes')
		RETURNING id
	`, tokenHash('5'), familyID).Scan(&replacementAccessTokenID); err != nil {
		t.Fatalf("create replacement access token: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO system.refresh_tokens
		    (token_hash, family_id, issued_access_token_id, parent_token_id, expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '59 minutes')
		RETURNING id
	`, tokenHash('6'), familyID, replacementAccessTokenID, refreshTokenID).Scan(&replacementRefreshTokenID); err != nil {
		t.Fatalf("create replacement refresh token: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.refresh_tokens SET replaced_by_token_id = $1 WHERE id = $2`, replacementRefreshTokenID, refreshTokenID); err != nil {
		t.Fatalf("link refresh token replacement: %v", err)
	}

	protocolRequestID := "11111111-1111-4111-8111-111111111111"
	var oauthFamilyID int64
	if err := db.QueryRow(`
		INSERT INTO system.refresh_token_families
		    (protocol_request_id, principal_id, context_type, tenant_membership_id, issued_authorization_version,
		     client_id, auth_type, audiences, scopes, authentication_methods, assurance_level,
		     authenticated_at, expires_at)
		VALUES
		    ($1, $2, 'tenant', $3, $4, 'addp-cli', 'oauth', ARRAY['addp.api'], ARRAY['addp.api'],
		     ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '1 hour')
		RETURNING id
	`, protocolRequestID, tenantUserID, tenantMembershipID, authorizationVersion).Scan(&oauthFamilyID); err != nil {
		t.Fatalf("create OAuth family: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.resource_access_tickets (token_hash, family_id, owner, expires_at) VALUES ($1, $2, 'manager', now() + interval '10 minutes')`, tokenHash('7'), oauthFamilyID); err == nil {
		t.Fatal("OAuth family issued a browser resource ticket")
	}

	if _, err := db.Exec(`UPDATE system.refresh_token_families SET revoked_at = now(), revoked_reason = 'session_test' WHERE id = $1`, familyID); err != nil {
		t.Fatalf("revoke token family: %v", err)
	}
	assertTimestampSet(t, db, `SELECT revoked_at FROM system.access_tokens WHERE id = $1`, replacementAccessTokenID, "family access-token revocation")
	assertTimestampSet(t, db, `SELECT revoked_at FROM system.refresh_tokens WHERE id = $1`, replacementRefreshTokenID, "family refresh-token revocation")
	assertTimestampSet(t, db, `SELECT revoked_at FROM system.resource_access_tickets WHERE id = $1`, resourceTicketID, "family resource-ticket revocation")
}

func assertAuthorizationVersionRefreshConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	principalID := createMigrationUser(t, db, "Authorization Refresh User")
	var tenantID int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find authorization refresh tenant: %v", err)
	}
	var membershipID int64
	if err := db.QueryRow(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
		VALUES ($1, $2, 'active', 'manual', now() - interval '1 minute', $2)
		RETURNING id
	`, tenantID, principalID).Scan(&membershipID); err != nil {
		t.Fatalf("create authorization refresh membership: %v", err)
	}
	var authorizationVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, principalID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("read authorization refresh version: %v", err)
	}
	var familyID int64
	if err := db.QueryRow(`
		INSERT INTO system.refresh_token_families
		    (principal_id, context_type, tenant_membership_id, issued_authorization_version,
		     client_id, auth_type, audiences, scopes, authentication_methods, assurance_level,
		     authenticated_at, expires_at)
		VALUES
		    ($1, 'tenant', $2, $3, 'addp-web', 'first_party', ARRAY['addp.api'], ARRAY[]::text[],
		     ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '1 hour')
		RETURNING id
	`, principalID, membershipID, authorizationVersion).Scan(&familyID); err != nil {
		t.Fatalf("create authorization refresh family: %v", err)
	}

	authorizationVersion = incrementMigrationPrincipalAuthorizationVersion(t, db, principalID)
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET issued_authorization_version = $1 WHERE id = $2`, authorizationVersion, familyID); err != nil {
		t.Fatalf("advance active family to current authorization version: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET issued_authorization_version = $1 WHERE id = $2`, authorizationVersion, familyID); err == nil {
		t.Fatal("active family accepted a non-increasing authorization version")
	}
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET issued_authorization_version = $1 WHERE id = $2`, authorizationVersion+1, familyID); err == nil {
		t.Fatal("active family advanced beyond the current principal authorization version")
	}

	authorizationVersion = incrementMigrationPrincipalAuthorizationVersion(t, db, principalID)
	if _, err := db.Exec(`
		UPDATE system.refresh_token_families
		SET issued_authorization_version = $1, client_id = 'changed-client'
		WHERE id = $2
	`, authorizationVersion, familyID); err == nil {
		t.Fatal("authorization version advance accepted a simultaneous family identity change")
	}
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET issued_authorization_version = $1 WHERE id = $2`, authorizationVersion, familyID); err != nil {
		t.Fatalf("advance family after rejected identity change: %v", err)
	}

	authorizationVersion = incrementMigrationPrincipalAuthorizationVersion(t, db, principalID)
	if _, err := db.Exec(`
		UPDATE system.refresh_token_families
		SET issued_authorization_version = $1, revoked_at = now(), revoked_reason = 'combined_change'
		WHERE id = $2
	`, authorizationVersion, familyID); err == nil {
		t.Fatal("family revocation accepted an authorization version change")
	}
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET issued_authorization_version = $1 WHERE id = $2`, authorizationVersion, familyID); err != nil {
		t.Fatalf("advance family before revocation: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET revoked_at = now(), revoked_reason = 'test_complete' WHERE id = $1`, familyID); err != nil {
		t.Fatalf("revoke authorization refresh family: %v", err)
	}
	authorizationVersion = incrementMigrationPrincipalAuthorizationVersion(t, db, principalID)
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET issued_authorization_version = $1 WHERE id = $2`, authorizationVersion, familyID); err == nil {
		t.Fatal("revoked family accepted an authorization version advance")
	}
}

func incrementMigrationPrincipalAuthorizationVersion(t *testing.T, db *sql.DB, principalID int64) int64 {
	t.Helper()
	var authorizationVersion int64
	if err := db.QueryRow(`
		UPDATE system.principals
		SET authorization_version = authorization_version + 1
		WHERE id = $1
		RETURNING authorization_version
	`, principalID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("increment migration principal authorization version: %v", err)
	}
	return authorizationVersion
}

func assertOAuthFositeStorageConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`
		INSERT INTO system.oauth_clients
		    (client_id, display_name, client_type, client_secret_hash, redirect_uris, grant_types,
		     response_types, allowed_scopes, allowed_audiences, token_endpoint_auth_method)
		VALUES
		    ('bad-public', 'Bad Public Client', 'public', 'secret-hash', ARRAY['http://127.0.0.1/callback'],
		     ARRAY['authorization_code'], ARRAY['code'], ARRAY['addp.api'], ARRAY['addp.api'], 'none')
	`); err == nil {
		t.Fatal("public OAuth client accepted a client secret")
	}

	var tenantID, tenantUserID, tenantMembershipID, authorizationVersion int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find OAuth test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&tenantUserID); err != nil {
		t.Fatalf("find OAuth test user: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id = $2`, tenantID, tenantUserID).Scan(&tenantMembershipID); err != nil {
		t.Fatalf("find OAuth test membership: %v", err)
	}
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, tenantUserID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("read OAuth test authorization version: %v", err)
	}

	authorizationRequestID := "22222222-2222-4222-8222-222222222222"
	if _, err := db.Exec(`
		INSERT INTO system.oauth_authorization_requests
		    (id, request_secret_hash, client_id, redirect_uri, response_types, response_mode,
		     requested_scopes, requested_audiences, requested_at, expires_at)
		VALUES
		    ($1, $2, 'addp-cli', 'http://127.0.0.1:49152/callback', ARRAY['code'], 'query',
		     ARRAY['unregistered.scope'], ARRAY['addp.api'], now() - interval '1 second', now() + interval '5 minutes')
	`, "21111111-1111-4111-8111-111111111111", tokenHash('8')); err == nil {
		t.Fatal("authorization request accepted an unregistered scope")
	}
	if _, err := db.Exec(`
		INSERT INTO system.oauth_authorization_requests
		    (id, request_secret_hash, client_id, redirect_uri, response_types, response_mode,
		     requested_scopes, requested_audiences, requested_at, expires_at)
		VALUES
		    ($1, $2, 'addp-cli', 'http://127.0.0.1:49152/callback', ARRAY['code'], 'query',
		     ARRAY['addp.api'], ARRAY['addp.api'], now() - interval '1 second', now() + interval '5 minutes')
	`, authorizationRequestID, tokenHash('8')); err != nil {
		t.Fatalf("create authorization request: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.oauth_pkce_sessions
		    (authorization_request_id, code_challenge, code_challenge_method, expires_at)
		VALUES ($1, 'pkce-s256-challenge', 'S256', now() + interval '4 minutes')
	`, authorizationRequestID); err != nil {
		t.Fatalf("create PKCE session: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_authorization_requests
		SET status = 'approved',
		    principal_id = $2,
		    context_type = 'tenant',
		    tenant_membership_id = $3,
		    issued_authorization_version = $4,
		    granted_scopes = ARRAY['addp.api'],
		    granted_audiences = ARRAY['addp.api'],
		    authentication_methods = ARRAY['password'],
		    assurance_level = 'aal1',
		    authenticated_at = now() - interval '1 minute',
		    completed_at = now()
		WHERE id = $1
	`, authorizationRequestID, tenantUserID, tenantMembershipID, authorizationVersion-1); err == nil {
		t.Fatal("authorization request accepted a stale authorization version")
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_authorization_requests
		SET status = 'approved',
		    principal_id = $2,
		    context_type = 'tenant',
		    tenant_membership_id = $3,
		    issued_authorization_version = $4,
		    granted_scopes = ARRAY['addp.api'],
		    granted_audiences = ARRAY['addp.api'],
		    authentication_methods = ARRAY['password'],
		    assurance_level = 'aal1',
		    authenticated_at = now() - interval '1 minute',
		    completed_at = now()
		WHERE id = $1
	`, authorizationRequestID, tenantUserID, tenantMembershipID, authorizationVersion); err != nil {
		t.Fatalf("approve authorization request: %v", err)
	}

	authorizationCodeHash := tokenHash('9')
	var authorizationCodeID int64
	if err := db.QueryRow(`
		INSERT INTO system.oauth_authorization_codes (code_hash, authorization_request_id, expires_at)
		VALUES ($1, $2, now() + interval '4 minutes')
		RETURNING id
	`, authorizationCodeHash, authorizationRequestID).Scan(&authorizationCodeID); err != nil {
		t.Fatalf("create authorization code: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_pkce_sessions
		SET authorization_code_hash = $2, verified_at = now(), consumed_at = now()
		WHERE authorization_request_id = $1
	`, authorizationRequestID, authorizationCodeHash); err != nil {
		t.Fatalf("consume PKCE session: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.oauth_authorization_codes SET invalidated_at = now() WHERE id = $1`, authorizationCodeID); err != nil {
		t.Fatalf("invalidate authorization code: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.oauth_authorization_codes SET invalidated_at = now() WHERE id = $1`, authorizationCodeID); err == nil {
		t.Fatal("authorization code was invalidated twice")
	}
	assertTimestampSet(t, db, `SELECT invalidated_at FROM system.oauth_authorization_codes WHERE id = $1`, authorizationCodeID, "authorization code invalidation")
	if _, err := db.Exec(`UPDATE system.oauth_authorization_requests SET status = 'cancelled' WHERE id = $1`, authorizationRequestID); err == nil {
		t.Fatal("terminal authorization request changed state")
	}

	if _, err := db.Exec(`
		INSERT INTO system.oauth_clients
		    (client_id, display_name, client_type, redirect_uris, grant_types, response_types,
		     allowed_scopes, allowed_audiences, token_endpoint_auth_method)
		VALUES
		    ('oidc-test', 'OIDC Storage Test', 'public', ARRAY['http://127.0.0.1/callback'],
		     ARRAY['authorization_code'], ARRAY['code'], ARRAY['openid'], ARRAY['addp.api'], 'none')
	`); err != nil {
		t.Fatalf("create OIDC storage test client: %v", err)
	}
	oidcRequestID := "33333333-3333-4333-8333-333333333333"
	if _, err := db.Exec(`
		INSERT INTO system.oauth_authorization_requests
		    (id, request_secret_hash, client_id, redirect_uri, response_types, response_mode,
		     requested_scopes, requested_audiences, requested_at, expires_at)
		VALUES
		    ($1, $2, 'oidc-test', 'http://127.0.0.1:49153/callback', ARRAY['code'], 'query',
		     ARRAY['openid'], ARRAY['addp.api'], now() - interval '1 second', now() + interval '5 minutes')
	`, oidcRequestID, tokenHash('a')); err != nil {
		t.Fatalf("create OIDC authorization request: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.oauth_oidc_sessions
		    (authorization_request_id, nonce, requested_at, expires_at)
		SELECT id, 'oidc-nonce', requested_at, now() + interval '4 minutes'
		FROM system.oauth_authorization_requests
		WHERE id = $1
	`, oidcRequestID); err != nil {
		t.Fatalf("create OIDC session: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_authorization_requests
		SET status = 'approved',
		    principal_id = $2,
		    context_type = 'tenant',
		    tenant_membership_id = $3,
		    issued_authorization_version = $4,
		    granted_scopes = ARRAY['openid'],
		    granted_audiences = ARRAY['addp.api'],
		    authentication_methods = ARRAY['password'],
		    assurance_level = 'aal1',
		    authenticated_at = now() - interval '1 minute',
		    completed_at = now()
		WHERE id = $1
	`, oidcRequestID, tenantUserID, tenantMembershipID, authorizationVersion); err != nil {
		t.Fatalf("approve OIDC authorization request: %v", err)
	}
	oidcAuthorizationCodeHash := tokenHash('d')
	if _, err := db.Exec(`
		INSERT INTO system.oauth_authorization_codes (code_hash, authorization_request_id, expires_at)
		VALUES ($1, $2, now() + interval '4 minutes')
	`, oidcAuthorizationCodeHash, oidcRequestID); err != nil {
		t.Fatalf("create OIDC authorization code: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_oidc_sessions
		SET authorization_code_hash = $2, subject = 'subject-1', authenticated_at = now() - interval '1 minute', acr = 'aal1',
		    amr = ARRAY['password'], extra_claims_schema_version = 1, extra_claims = '[]'::jsonb
		WHERE authorization_request_id = $1
	`, oidcRequestID, oidcAuthorizationCodeHash); err == nil {
		t.Fatal("OIDC session accepted non-object extra claims")
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_oidc_sessions
		SET authorization_code_hash = $2, subject = 'subject-1', authenticated_at = now() - interval '1 minute', acr = 'aal1',
		    amr = ARRAY['password'], extra_claims_schema_version = 1, extra_claims = '{}'::jsonb
		WHERE authorization_request_id = $1
	`, oidcRequestID, oidcAuthorizationCodeHash); err != nil {
		t.Fatalf("complete OIDC session: %v", err)
	}

	deviceRequestID := "44444444-4444-4444-8444-444444444444"
	if _, err := db.Exec(`
		INSERT INTO system.oauth_device_authorizations
		    (id, device_code_hash, user_code_hash, client_id, requested_scopes, requested_audiences,
		     next_poll_at, requested_at, expires_at)
		VALUES
		    ($1, $2, $3, 'addp-cli', ARRAY['addp.api'], ARRAY['addp.api'],
		     now() + interval '5 seconds', now() - interval '1 second', now() + interval '10 minutes')
	`, deviceRequestID, tokenHash('b'), tokenHash('c')); err != nil {
		t.Fatalf("create device authorization: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_device_authorizations
		SET last_polled_at = now(), next_poll_at = now() + interval '5 seconds'
		WHERE id = $1
	`, deviceRequestID); err != nil {
		t.Fatalf("record allowed device poll: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_device_authorizations
		SET poll_interval_seconds = poll_interval_seconds + 5,
		    last_polled_at = now(),
		    next_poll_at = now() + interval '10 seconds'
		WHERE id = $1
	`, deviceRequestID); err != nil {
		t.Fatalf("record device slow_down: %v", err)
	}
	var pollInterval int
	if err := db.QueryRow(`SELECT poll_interval_seconds FROM system.oauth_device_authorizations WHERE id = $1`, deviceRequestID).Scan(&pollInterval); err != nil {
		t.Fatalf("read device poll interval: %v", err)
	}
	if pollInterval != 10 {
		t.Fatalf("device poll interval = %d, want 10", pollInterval)
	}
	if _, err := db.Exec(`UPDATE system.oauth_device_authorizations SET poll_interval_seconds = 5 WHERE id = $1`, deviceRequestID); err == nil {
		t.Fatal("device authorization poll interval decreased")
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_device_authorizations
		SET status = 'approved',
		    granted_scopes = ARRAY['addp.api'],
		    granted_audiences = ARRAY['addp.api'],
		    principal_id = $2,
		    context_type = 'tenant',
		    tenant_membership_id = $3,
		    issued_authorization_version = $4,
		    authentication_methods = ARRAY['password'],
		    assurance_level = 'aal1',
		    authenticated_at = now() - interval '1 minute',
		    decided_at = now()
		WHERE id = $1
	`, deviceRequestID, tenantUserID, tenantMembershipID, authorizationVersion); err != nil {
		t.Fatalf("approve device authorization: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.oauth_clients SET status = 'disabled' WHERE client_id = 'addp-cli'`); err != nil {
		t.Fatalf("disable OAuth client: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_device_authorizations
		SET status = 'invalidated', invalidated_at = now()
		WHERE id = $1
	`, deviceRequestID); err != nil {
		t.Fatalf("invalidate device authorization: %v", err)
	}
	assertTimestampSet(t, db, `SELECT invalidated_at FROM system.oauth_device_authorizations WHERE id = $1`, deviceRequestID, "device authorization invalidation")
}

func tokenHash(character byte) string {
	return strings.Repeat(string(character), 64)
}

func assertTimestampSet(t *testing.T, db *sql.DB, query string, id any, operation string) {
	t.Helper()
	var timestamp sql.NullTime
	if err := db.QueryRow(query, id).Scan(&timestamp); err != nil {
		t.Fatalf("read timestamp after %s: %v", operation, err)
	}
	if !timestamp.Valid {
		t.Fatalf("timestamp after %s is null", operation)
	}
}

func assertForeignKeyColumnsIndexed(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`
		SELECT c.conrelid::regclass::text, attribute.attname
		FROM pg_constraint c
		JOIN pg_namespace namespace ON namespace.oid = c.connamespace
		JOIN unnest(c.conkey) AS key(attnum) ON true
		JOIN pg_attribute attribute
		  ON attribute.attrelid = c.conrelid AND attribute.attnum = key.attnum
		WHERE c.contype = 'f'
		  AND namespace.nspname = 'system'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM pg_index index_definition
		      WHERE index_definition.indrelid = c.conrelid
		        AND key.attnum = ANY(index_definition.indkey)
		  )
		ORDER BY c.conrelid::regclass::text, attribute.attname
	`)
	if err != nil {
		t.Fatalf("inspect foreign-key indexes: %v", err)
	}
	defer rows.Close()

	var missing []string
	for rows.Next() {
		var tableName, columnName string
		if err := rows.Scan(&tableName, &columnName); err != nil {
			t.Fatalf("scan missing foreign-key index: %v", err)
		}
		missing = append(missing, tableName+"."+columnName)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate missing foreign-key indexes: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("foreign-key columns without indexes: %s", strings.Join(missing, ", "))
	}
}

func assertAuthorizationGovernanceConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var tenantID, tenantUserID, tenantMembershipID int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find authorization test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&tenantUserID); err != nil {
		t.Fatalf("find authorization test user: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id = $2`, tenantID, tenantUserID).Scan(&tenantMembershipID); err != nil {
		t.Fatalf("find authorization test membership: %v", err)
	}

	var tenantPermissionID, platformPermissionID, departmentPermissionID int64
	if err := db.QueryRow(`
		INSERT INTO system.permissions
		    (permission_key, owner_module, action, risk_level, allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key)
		VALUES
		    ('test.resource.read', 'test', 'read', 'low', ARRAY['tenant'], true, 'permissions.test.resource.read.name', 'permissions.test.resource.read.description')
		RETURNING id
	`).Scan(&tenantPermissionID); err != nil {
		t.Fatalf("create tenant permission: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO system.permissions
		    (permission_key, owner_module, action, risk_level, allowed_scope_types, name_i18n_key, description_i18n_key)
		VALUES
		    ('test.platform.read', 'test', 'read', 'low', ARRAY['platform'], 'permissions.test.platform.read.name', 'permissions.test.platform.read.description')
		RETURNING id
	`).Scan(&platformPermissionID); err != nil {
		t.Fatalf("create platform permission: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO system.permissions
		    (permission_key, owner_module, action, risk_level, allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key)
		VALUES
		    ('test.department.read', 'test', 'read', 'low', ARRAY['department'], true, 'permissions.test.department.read.name', 'permissions.test.department.read.description')
		RETURNING id
	`).Scan(&departmentPermissionID); err != nil {
		t.Fatalf("create department permission: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.permissions SET permission_key = 'test.resource.changed' WHERE id = $1`, tenantPermissionID); err == nil {
		t.Fatal("published permission key update succeeded")
	}
	if _, err := db.Exec(`
		INSERT INTO system.permissions
		    (permission_key, owner_module, action, risk_level, allowed_scope_types, name_i18n_key, description_i18n_key)
		VALUES
		    ('test.duplicate.read', 'test', 'read', 'low', ARRAY['tenant', 'tenant'], 'permissions.test.duplicate.read.name', 'permissions.test.duplicate.read.description')
	`); err == nil {
		t.Fatal("permission accepted duplicate scope types")
	}

	var tenantRoleID, departmentRoleID int64
	if err := db.QueryRow(`
		INSERT INTO system.roles
		    (tenant_id, role_key, name, description, role_type, allowed_scope_types, allowed_principal_types, immutable, created_by_principal_id)
		VALUES
		    ($1, 'tenant.custom_reader', 'Custom Reader', '', 'tenant_custom', ARRAY['tenant'], ARRAY['user'], false, $2)
		RETURNING id
	`, tenantID, tenantUserID).Scan(&tenantRoleID); err != nil {
		t.Fatalf("create tenant custom role: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'tenant', $3)
	`, tenantRoleID, tenantPermissionID, tenantUserID); err != nil {
		t.Fatalf("attach tenant-customizable permission: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'tenant', $3)
	`, tenantRoleID, platformPermissionID, tenantUserID); err == nil {
		t.Fatal("tenant custom role accepted non-customizable platform permission")
	}
	if _, err := db.Exec(`
		INSERT INTO system.roles
		    (role_key, role_type, allowed_scope_types, allowed_principal_types, immutable)
		VALUES ('tenant.missing_i18n', 'tenant_builtin', ARRAY['tenant'], ARRAY['user'], true)
	`); err == nil {
		t.Fatal("built-in role without i18n keys succeeded")
	}

	if err := db.QueryRow(`
		INSERT INTO system.roles
		    (role_key, name_i18n_key, description_i18n_key, role_type, allowed_scope_types, allowed_principal_types, immutable)
		VALUES
		    ('tenant.department_reader', 'roles.tenant.department_reader.name', 'roles.tenant.department_reader.description', 'tenant_builtin', ARRAY['department'], ARRAY['user'], true)
		RETURNING id
	`).Scan(&departmentRoleID); err != nil {
		t.Fatalf("create department built-in role: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_permissions (role_id, permission_id, source_type) VALUES ($1, $2, 'product')`, departmentRoleID, departmentPermissionID); err != nil {
		t.Fatalf("attach department permission: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_permissions (role_id, permission_id, source_type) VALUES ($1, $2, 'product')`, departmentRoleID, platformPermissionID); err == nil {
		t.Fatal("role permission accepted a scope narrower than the role")
	}

	var tenantAssignmentID int64
	if err := db.QueryRow(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'tenant', $3, 'manual', $1)
		RETURNING id
	`, tenantUserID, tenantRoleID, tenantID).Scan(&tenantAssignmentID); err != nil {
		t.Fatalf("create tenant role assignment: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'tenant', $3, 'manual', $1)
	`, tenantUserID, tenantRoleID, tenantID); err == nil {
		t.Fatal("duplicate active role assignment succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.role_assignments WHERE id = $1`, tenantAssignmentID); err == nil {
		t.Fatal("physical role assignment deletion succeeded")
	}

	var departmentID int64
	if err := db.QueryRow(`INSERT INTO system.departments (tenant_id, code, name) VALUES ($1, 'authorization', 'Authorization') RETURNING id`, tenantID).Scan(&departmentID); err != nil {
		t.Fatalf("create authorization department: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.department_memberships
		    (tenant_id, department_id, tenant_membership_id, membership_type)
		VALUES ($1, $2, $3, 'additional')
	`, tenantID, departmentID, tenantMembershipID); err != nil {
		t.Fatalf("create authorization department membership: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, department_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'department', $3, $4, 'manual', $1)
	`, tenantUserID, departmentRoleID, tenantID, departmentID); err != nil {
		t.Fatalf("create department role assignment: %v", err)
	}

	targetUserID := createMigrationUser(t, db, "Governed Target")
	requesterUserID := createMigrationUser(t, db, "Governed Requester")
	reviewerUserID := createMigrationUser(t, db, "Governed Reviewer")

	var platformRoleID, conflictingPlatformRoleID int64
	if err := db.QueryRow(`
		INSERT INTO system.roles
		    (role_key, name_i18n_key, description_i18n_key, role_type, allowed_scope_types, allowed_principal_types, immutable)
		VALUES
		    ('platform.test_administrator', 'roles.platform.test_administrator.name', 'roles.platform.test_administrator.description', 'platform_builtin', ARRAY['platform'], ARRAY['user'], true)
		RETURNING id
	`).Scan(&platformRoleID); err != nil {
		t.Fatalf("create platform test role: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO system.roles
		    (role_key, name_i18n_key, description_i18n_key, role_type, allowed_scope_types, allowed_principal_types, immutable)
		VALUES
		    ('platform.conflicting_administrator', 'roles.platform.conflicting_administrator.name', 'roles.platform.conflicting_administrator.description', 'platform_builtin', ARRAY['platform'], ARRAY['user'], true)
		RETURNING id
	`).Scan(&conflictingPlatformRoleID); err != nil {
		t.Fatalf("create conflicting platform role: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_conflicts (role_id_low, role_id_high, reason) VALUES (LEAST($1::bigint, $2::bigint), GREATEST($1::bigint, $2::bigint), 'separation of duties')`, platformRoleID, conflictingPlatformRoleID); err != nil {
		t.Fatalf("create platform role conflict: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, source_type, created_by_principal_id)
		VALUES ($1, $2, 'platform', 'manual', $3)
	`, targetUserID, platformRoleID, requesterUserID); err == nil {
		t.Fatal("platform role assignment succeeded without an approved grant request")
	}
	if _, err := db.Exec(`
		INSERT INTO system.privileged_change_requests
		    (change_type, target_principal_id, target_role_id, reason, requested_by_principal_id, status, decided_at)
		VALUES ('platform_role_grant', $1, $2, 'forged approval', $3, 'approved', now())
	`, targetUserID, platformRoleID, requesterUserID); err == nil {
		t.Fatal("privileged request insert accepted a forged approved status")
	}

	grantRequestID := createPrivilegedChangeRequest(t, db, "platform_role_grant", targetUserID, platformRoleID, requesterUserID)
	if _, err := db.Exec(`UPDATE system.privileged_change_requests SET status = 'approved', decided_at = now() WHERE id = $1`, grantRequestID); err == nil {
		t.Fatal("privileged request direct approval succeeded without an approval row")
	}
	if _, err := db.Exec(`
		INSERT INTO system.privileged_change_approvals (request_id, reviewer_principal_id, decision)
		VALUES ($1, $2, 'approved')
	`, grantRequestID, requesterUserID); err == nil {
		t.Fatal("privileged request requester approved their own request")
	}
	approvePrivilegedChangeRequest(t, db, grantRequestID, reviewerUserID)

	var platformAssignmentID int64
	if err := db.QueryRow(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, source_type, created_by_principal_id, grant_change_request_id)
		VALUES ($1, $2, 'platform', 'manual', $3, $4)
		RETURNING id
	`, targetUserID, platformRoleID, requesterUserID, grantRequestID).Scan(&platformAssignmentID); err != nil {
		t.Fatalf("apply approved platform role grant: %v", err)
	}
	assertPrivilegedChangeStatus(t, db, grantRequestID, "applied")
	if _, err := db.Exec(`UPDATE system.privileged_change_approvals SET reason = 'changed' WHERE request_id = $1`, grantRequestID); err == nil {
		t.Fatal("privileged approval update succeeded")
	}

	conflictRequestID := createPrivilegedChangeRequest(t, db, "platform_role_grant", targetUserID, conflictingPlatformRoleID, requesterUserID)
	approvePrivilegedChangeRequest(t, db, conflictRequestID, reviewerUserID)
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, source_type, created_by_principal_id, grant_change_request_id)
		VALUES ($1, $2, 'platform', 'manual', $3, $4)
	`, targetUserID, conflictingPlatformRoleID, requesterUserID, conflictRequestID); err == nil {
		t.Fatal("conflicting platform role assignment succeeded")
	}
	assertPrivilegedChangeStatus(t, db, conflictRequestID, "approved")

	if _, err := db.Exec(`UPDATE system.principals SET status = 'suspended' WHERE id = $1`, targetUserID); err == nil {
		t.Fatal("governed platform principal suspension succeeded without approval")
	}
	suspendRequestID := createPrivilegedChangeRequest(t, db, "platform_identity_suspend", targetUserID, 0, requesterUserID)
	approvePrivilegedChangeRequest(t, db, suspendRequestID, reviewerUserID)
	if _, err := db.Exec(`UPDATE system.principals SET status = 'suspended', status_change_request_id = $1 WHERE id = $2`, suspendRequestID, targetUserID); err != nil {
		t.Fatalf("apply approved principal suspension: %v", err)
	}
	assertPrivilegedChangeStatus(t, db, suspendRequestID, "applied")

	revokeRequestID := createPrivilegedChangeRequest(t, db, "platform_role_revoke", targetUserID, platformRoleID, requesterUserID)
	approvePrivilegedChangeRequest(t, db, revokeRequestID, reviewerUserID)
	if _, err := db.Exec(`
		UPDATE system.role_assignments
		SET status = 'revoked',
		    revoked_by_principal_id = $1,
		    revoked_at = now(),
		    revoke_change_request_id = $2
		WHERE id = $3
	`, requesterUserID, revokeRequestID, platformAssignmentID); err != nil {
		t.Fatalf("apply approved platform role revocation: %v", err)
	}
	assertPrivilegedChangeStatus(t, db, revokeRequestID, "applied")
}

func createMigrationUser(t *testing.T, db *sql.DB, displayName string) int64 {
	t.Helper()
	var principalID int64
	if err := db.QueryRow(`INSERT INTO system.principals (principal_type) VALUES ('user') RETURNING id`).Scan(&principalID); err != nil {
		t.Fatalf("create %s principal: %v", displayName, err)
	}
	if _, err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES ($1, $2)`, principalID, displayName); err != nil {
		t.Fatalf("create %s user: %v", displayName, err)
	}
	return principalID
}

func createPrivilegedChangeRequest(t *testing.T, db *sql.DB, changeType string, targetPrincipalID, targetRoleID, requesterPrincipalID int64) int64 {
	t.Helper()
	var requestID int64
	var err error
	if targetRoleID == 0 {
		err = db.QueryRow(`
			INSERT INTO system.privileged_change_requests
			    (change_type, target_principal_id, reason, requested_by_principal_id)
			VALUES ($1, $2, 'migration constraint test', $3)
			RETURNING id
		`, changeType, targetPrincipalID, requesterPrincipalID).Scan(&requestID)
	} else {
		err = db.QueryRow(`
			INSERT INTO system.privileged_change_requests
			    (change_type, target_principal_id, target_role_id, reason, requested_by_principal_id)
			VALUES ($1, $2, $3, 'migration constraint test', $4)
			RETURNING id
		`, changeType, targetPrincipalID, targetRoleID, requesterPrincipalID).Scan(&requestID)
	}
	if err != nil {
		t.Fatalf("create %s request: %v", changeType, err)
	}
	return requestID
}

func approvePrivilegedChangeRequest(t *testing.T, db *sql.DB, requestID, reviewerPrincipalID int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO system.privileged_change_approvals (request_id, reviewer_principal_id, decision)
		VALUES ($1, $2, 'approved')
	`, requestID, reviewerPrincipalID); err != nil {
		t.Fatalf("approve privileged request %d: %v", requestID, err)
	}
	assertPrivilegedChangeStatus(t, db, requestID, "approved")
}

func assertPrivilegedChangeStatus(t *testing.T, db *sql.DB, requestID int64, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT status FROM system.privileged_change_requests WHERE id = $1`, requestID).Scan(&got); err != nil {
		t.Fatalf("read privileged request %d status: %v", requestID, err)
	}
	if got != want {
		t.Fatalf("privileged request %d status = %q, want %q", requestID, got, want)
	}
}

func assertFederationOrganizationConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var userPrincipalID, servicePrincipalID, tenantID, serviceTenantMembershipID int64
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&userPrincipalID); err != nil {
		t.Fatalf("find migration user: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.service_principals WHERE name = 'test-service'`).Scan(&servicePrincipalID); err != nil {
		t.Fatalf("find migration service principal: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find migration tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id = $2`, tenantID, servicePrincipalID).Scan(&serviceTenantMembershipID); err != nil {
		t.Fatalf("find service tenant membership: %v", err)
	}

	var userTenantMembershipID int64
	if err := db.QueryRow(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, source_type, joined_at, created_by_principal_id)
		VALUES ($1, $2, 'manual', now(), $2)
		RETURNING id
	`, tenantID, userPrincipalID).Scan(&userTenantMembershipID); err != nil {
		t.Fatalf("create user tenant membership: %v", err)
	}

	var identityProviderID, connectionID int64
	if err := db.QueryRow(`
		INSERT INTO system.identity_providers (issuer, protocol, display_name)
		VALUES ('https://idp.example.test', 'oidc', 'Migration IdP')
		RETURNING id
	`).Scan(&identityProviderID); err != nil {
		t.Fatalf("create identity provider: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.identity_providers (issuer, protocol, display_name) VALUES ('https://idp.example.test', 'oidc', 'Duplicate')`); err == nil {
		t.Fatal("duplicate identity provider issuer succeeded")
	}
	if err := db.QueryRow(`
		INSERT INTO system.tenant_idp_connections (tenant_id, identity_provider_id, provisioning_mode)
		VALUES ($1, $2, 'jit')
		RETURNING id
	`, tenantID, identityProviderID).Scan(&connectionID); err != nil {
		t.Fatalf("create tenant IdP connection: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.external_identities (identity_provider_id, subject, user_id)
		VALUES ($1, 'subject-1', $2)
	`, identityProviderID, userPrincipalID); err != nil {
		t.Fatalf("create external identity: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.user_attribute_authorities
		    (user_id, attribute_name, authority_type, tenant_idp_connection_id)
		VALUES ($1, 'display_name', 'identity_provider', $2)
	`, userPrincipalID, connectionID); err != nil {
		t.Fatalf("create external user attribute authority: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.user_attribute_authorities
		    (user_id, attribute_name, authority_type, tenant_idp_connection_id)
		VALUES ($1, 'primary_email', 'local', $2)
	`, userPrincipalID, connectionID); err == nil {
		t.Fatal("local user attribute authority accepted an IdP connection")
	}

	var otherTenantID int64
	if err := db.QueryRow(`INSERT INTO system.tenants (code, name) VALUES ('other-test', 'Other Test') RETURNING id`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create second tenant: %v", err)
	}
	var rootDepartmentID, childDepartmentID int64
	if err := db.QueryRow(`INSERT INTO system.departments (tenant_id, code, name) VALUES ($1, 'root', 'Root') RETURNING id`, tenantID).Scan(&rootDepartmentID); err != nil {
		t.Fatalf("create root department: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO system.departments (tenant_id, parent_id, code, name) VALUES ($1, $2, 'child', 'Child') RETURNING id`, tenantID, rootDepartmentID).Scan(&childDepartmentID); err != nil {
		t.Fatalf("create child department: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.departments SET parent_id = $1 WHERE id = $2`, childDepartmentID, rootDepartmentID); err == nil {
		t.Fatal("department cycle update succeeded")
	}
	if _, err := db.Exec(`INSERT INTO system.departments (tenant_id, parent_id, code, name) VALUES ($1, $2, 'cross-tenant', 'Cross Tenant')`, otherTenantID, rootDepartmentID); err == nil {
		t.Fatal("cross-tenant department parent succeeded")
	}

	if _, err := db.Exec(`
		INSERT INTO system.department_memberships
		    (tenant_id, department_id, tenant_membership_id, membership_type)
		VALUES ($1, $2, $3, 'primary')
	`, tenantID, rootDepartmentID, userTenantMembershipID); err != nil {
		t.Fatalf("create user department membership: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.department_memberships
		    (tenant_id, department_id, tenant_membership_id, membership_type)
		VALUES ($1, $2, $3, 'additional')
	`, tenantID, rootDepartmentID, serviceTenantMembershipID); err == nil {
		t.Fatal("department membership accepted a service principal")
	}
	if _, err := db.Exec(`
		INSERT INTO system.department_memberships
		    (tenant_id, department_id, tenant_membership_id, membership_type)
		VALUES ($1, $2, $3, 'primary')
	`, tenantID, childDepartmentID, userTenantMembershipID); err == nil {
		t.Fatal("second active primary department membership succeeded")
	}
	assertAuthorizationVersion(t, db, userPrincipalID, 3, "department membership creation")
	if _, err := db.Exec(`UPDATE system.departments SET status = 'disabled' WHERE id = $1`, rootDepartmentID); err != nil {
		t.Fatalf("disable department: %v", err)
	}
	assertAuthorizationVersion(t, db, userPrincipalID, 4, "department disable")

	var projectGroupID int64
	if err := db.QueryRow(`
		INSERT INTO system.project_groups (tenant_id, code, name, status)
		VALUES ($1, 'migration-project', 'Migration Project', 'active')
		RETURNING id
	`, tenantID).Scan(&projectGroupID); err != nil {
		t.Fatalf("create project group: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.project_group_memberships
		    (tenant_id, project_group_id, tenant_membership_id, relation_role)
		VALUES ($1, $2, $3, 'coordinator')
	`, tenantID, projectGroupID, serviceTenantMembershipID); err != nil {
		t.Fatalf("create service principal project group membership: %v", err)
	}
	assertAuthorizationVersion(t, db, servicePrincipalID, 3, "project group membership creation")
	if _, err := db.Exec(`UPDATE system.project_groups SET status = 'closed' WHERE id = $1`, projectGroupID); err != nil {
		t.Fatalf("close project group: %v", err)
	}
	assertAuthorizationVersion(t, db, servicePrincipalID, 4, "project group close")
	if _, err := db.Exec(`
		INSERT INTO system.project_group_memberships
		    (tenant_id, project_group_id, tenant_membership_id)
		VALUES ($1, $2, $3)
	`, tenantID, projectGroupID, userTenantMembershipID); err == nil {
		t.Fatal("active membership in a closed project group succeeded")
	}
}

func assertAuthorizationVersion(t *testing.T, db *sql.DB, principalID, want int64, operation string) {
	t.Helper()
	var got int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, principalID).Scan(&got); err != nil {
		t.Fatalf("read authorization version after %s: %v", operation, err)
	}
	if got != want {
		t.Fatalf("authorization_version after %s = %d, want %d", operation, got, want)
	}
}

func assertIdentityTenantConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var userPrincipalID int64
	if err := db.QueryRow(`INSERT INTO system.principals (principal_type) VALUES ('user') RETURNING id`).Scan(&userPrincipalID); err != nil {
		t.Fatalf("create user principal: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES ($1, 'Migration User')`, userPrincipalID); err != nil {
		t.Fatalf("create user subtype: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.principals SET principal_type = 'service_principal' WHERE id = $1`, userPrincipalID); err == nil {
		t.Fatal("principal type update succeeded after subtype creation")
	}

	var servicePrincipalID int64
	if err := db.QueryRow(`INSERT INTO system.principals (principal_type) VALUES ('service_principal') RETURNING id`).Scan(&servicePrincipalID); err != nil {
		t.Fatalf("create service principal: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES ($1, 'Wrong Subtype')`, servicePrincipalID); err == nil {
		t.Fatal("user subtype accepted a service principal")
	}

	var tenantID int64
	if err := db.QueryRow(`INSERT INTO system.tenants (code, name) VALUES ('migration-test', 'Migration Test') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.service_principals (id, name, owner_scope, owner_tenant_id, created_by_principal_id)
		VALUES ($1, 'test-service', 'tenant', $2, $3)
	`, servicePrincipalID, tenantID, userPrincipalID); err == nil {
		t.Fatal("tenant-owned service principal succeeded without tenant membership")
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, source_type, joined_at, created_by_principal_id)
		VALUES ($1, $2, 'manual', now(), $3)
	`, tenantID, servicePrincipalID, userPrincipalID); err != nil {
		t.Fatalf("create service principal membership: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.service_principals (id, name, owner_scope, owner_tenant_id, created_by_principal_id)
		VALUES ($1, 'test-service', 'tenant', $2, $3)
	`, servicePrincipalID, tenantID, userPrincipalID); err != nil {
		t.Fatalf("create tenant-owned service principal: %v", err)
	}

	var authorizationVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, servicePrincipalID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("read authorization version: %v", err)
	}
	if authorizationVersion != 2 {
		t.Fatalf("authorization_version = %d, want 2 after membership creation", authorizationVersion)
	}
	if _, err := db.Exec(`UPDATE system.tenant_memberships SET principal_id = $1 WHERE principal_id = $2`, userPrincipalID, servicePrincipalID); err == nil {
		t.Fatal("tenant membership principal identity update succeeded")
	}
}

func latestMigrationVersion(t *testing.T) int {
	t.Helper()
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatalf("read embedded migration catalog: %v", err)
	}
	return int(catalog.LatestVersion)
}
