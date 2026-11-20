package migration

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func testMySQLTenantAdministrator(t *testing.T, db *gorm.DB) {
	t.Helper()
	exec := func(query string) {
		t.Helper()
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO tenants (id, code, name) VALUES (90, 'admin-check', 'Admin check')")
	const initialize = "UPDATE tenants SET initialized_at = CURRENT_TIMESTAMP(6), initialized_by_principal_id = 100 WHERE id = 90"
	assertLastAdminViolation(t, db.Exec(initialize).Error)
	exec("INSERT INTO tenant_memberships (tenant_id, principal_id, source_type, joined_at) VALUES (90, 100, 'manual', NOW()), (90, 103, 'manual', NOW())")
	exec(`INSERT INTO roles
		(id, role_key, name_i18n_key, description_i18n_key, role_type, allowed_scope_types, allowed_principal_types, immutable)
		VALUES (90, 'tenant.administrator', 'admin.name', 'admin.description', 'tenant_builtin',
		        JSON_ARRAY('tenant'), JSON_ARRAY('user'), true)`)
	exec(`INSERT INTO role_assignments (id, principal_id, role_id, scope_type, tenant_id, valid_from, source_type)
		VALUES (91, 100, 90, 'tenant', 90, CURRENT_TIMESTAMP(6), 'bootstrap'),
		       (92, 103, 90, 'tenant', 90, CURRENT_TIMESTAMP(6), 'bootstrap')`)
	exec(initialize)

	const revoke = `UPDATE role_assignments
		SET status = 'revoked', revoked_by_principal_id = 100, revoked_at = CURRENT_TIMESTAMP(6), revoked_reason = 'gate'
		WHERE id = ?`
	assertConcurrentAdminRevokes(t, db, revoke)

	for _, invalid := range []string{
		"UPDATE tenant_memberships SET status = 'suspended' WHERE tenant_id = 90 AND principal_id = 103",
		"UPDATE tenant_memberships SET expires_at = DATE_ADD(NOW(), INTERVAL 1 DAY) WHERE tenant_id = 90 AND principal_id = 103",
		"DELETE FROM tenant_memberships WHERE tenant_id = 90 AND principal_id = 103",
		"UPDATE principals SET status = 'suspended' WHERE id = 103",
		"UPDATE roles SET status = 'disabled' WHERE id = 90",
	} {
		assertLastAdminViolation(t, db.Exec(invalid).Error)
	}
	ctx := context.Background()
	repo := iam.NewRepository(db)
	err := repo.Transaction(ctx, func(tx *iam.Repository) error {
		return tx.RevokeTenantRoleAssignment(ctx, 92, 100, "gate", time.Now())
	})
	if !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("repository returned unexpected last-administrator error: %v", err)
	}

	// The original administrator may be replaced atomically once the new
	// assignment is eligible.
	var version int64
	if err := db.Raw("SELECT authorization_version FROM principals WHERE id = 103").Row().Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO role_assignments (principal_id, role_id, scope_type, tenant_id, source_type)
			VALUES (100, 90, 'tenant', 90, 'bootstrap')`).Error; err != nil {
			return err
		}
		return tx.Exec(revoke, 92).Error
	}); err != nil {
		t.Fatal(err)
	}
	var updatedVersion int64
	if err := db.Raw("SELECT authorization_version FROM principals WHERE id = 103").Row().Scan(&updatedVersion); err != nil {
		t.Fatal(err)
	}
	if updatedVersion != version+1 {
		t.Fatalf("expected exactly one version bump for committed revocation, got %d -> %d", version, updatedVersion)
	}
}

func assertConcurrentAdminRevokes(t *testing.T, db *gorm.DB, revoke string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	first, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	var id1, id2 uint64
	if err := first.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id1); err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id2); err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Fatal("expected two distinct physical MySQL sessions")
	}

	tx1, err := first.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback()
	if _, err := tx1.ExecContext(ctx, revoke, 91); err != nil {
		t.Fatal(err)
	}
	tx2, err := second.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()

	secondResult := make(chan error, 1)
	go func() {
		_, err := tx2.ExecContext(ctx, revoke, 92)
		secondResult <- err
	}()

	// Observe the actual wait, without relying on a scheduling delay as
	// evidence of the lock. The third connection is a read-only observer.
	for {
		var waiting bool
		err := db.WithContext(ctx).Raw(`SELECT EXISTS (
			SELECT 1 FROM performance_schema.data_lock_waits AS waits
			JOIN performance_schema.threads AS requester ON requester.THREAD_ID = waits.REQUESTING_THREAD_ID
			JOIN performance_schema.threads AS blocker ON blocker.THREAD_ID = waits.BLOCKING_THREAD_ID
			WHERE requester.PROCESSLIST_ID = ? AND blocker.PROCESSLIST_ID = ?
		)`, id2, id1).Row().Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-secondResult:
			t.Fatalf("second revoke completed before the first transaction committed: %v", err)
		case <-ctx.Done():
			t.Fatal("did not observe the second connection wait on the first")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := tx1.Commit(); err != nil {
		t.Fatal(err)
	}
	assertLastAdminViolation(t, <-secondResult)
	if err := tx2.Rollback(); err != nil {
		t.Fatal(err)
	}
	t.Logf("MySQL sessions %d and %d: first revoke committed, second waited and was rejected", id1, id2)

	var revoked, survivor string
	if err := db.Raw("SELECT status FROM role_assignments WHERE id = 91").Row().Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT status FROM role_assignments WHERE id = 92").Row().Scan(&survivor); err != nil {
		t.Fatal(err)
	}
	if revoked != "revoked" || survivor != "active" {
		t.Fatalf("assignment status after concurrent writes: %s, %s", revoked, survivor)
	}
}

func assertLastAdminViolation(t *testing.T, err error) {
	t.Helper()
	var mysqlError *mysqldriver.MySQLError
	if !errors.As(err, &mysqlError) || mysqlError.Number != 30001 {
		t.Fatalf("expected database last-administrator violation, got %v", err)
	}
}
