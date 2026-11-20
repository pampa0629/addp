package migration

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/schema"
	"github.com/addp/system/internal/iam"
	passwordutils "github.com/addp/system/pkg/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMySQLIAMFoundation(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_SYSTEM_IAM_FOUNDATION") != "1" {
		t.Skip("run the disposable platform-system-iam-foundation MySQL gate")
	}
	dialector, err := platformmysql.Dialector(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
		DBName: "system", ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })

	// Build revision 4 with its original DDL before testing an upgrade.
	applyDDL := func(conn *gorm.DB, files ...string) error {
		for _, file := range files {
			ddl, err := mysqlIAMFoundationSQL.ReadFile("mysql_iam/" + file)
			if err != nil {
				return err
			}
			if err := conn.Exec(string(ddl)).Error; err != nil {
				return err
			}
		}
		return nil
	}
	err = schema.MigrateMySQL(db, mysqlIAMFoundationOwner, 4, func(conn *gorm.DB) error {
		files, err := fs.ReadDir(mysqlIAMFoundationSQL, "mysql_iam")
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.Name() > "031_role_assignments_before_delete.sql" {
				break
			}
			if err := applyDDL(conn, file.Name()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	mustExec := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	mustExec("INSERT INTO principals (id, principal_type) VALUES (100, 'user'), (101, 'service_principal'), (103, 'user')")
	mustExec("INSERT INTO users (id, display_name) VALUES (100, ?), (103, 'Another user')", "验证用户 🧪")

	mustExec("INSERT INTO tenants (id, code, name) VALUES (1, 'before-upgrade', 'Before upgrade')")
	interrupted := errors.New("fixture interrupt after service principal DDL")
	err = schema.MigrateMySQL(db, mysqlIAMFoundationOwner, mysqlIAMFoundationVersion, func(conn *gorm.DB) error {
		if err := applyDDL(conn, "032_service_principals.sql"); err != nil {
			return err
		}
		return interrupted
	})
	if !errors.Is(err, interrupted) {
		t.Fatalf("upgrade did not reach the interruption: %v", err)
	}
	if err := schema.RequireMySQL(db, mysqlIAMFoundationOwner, 4); err == nil {
		t.Fatal("pending upgrade passed the previous revision check")
	}
	if err := schema.RequireMySQL(db, mysqlIAMFoundationOwner, mysqlIAMFoundationVersion); err == nil {
		t.Fatal("partial upgrade passed the new revision check")
	}
	if err := MigrateMySQLIAMFoundation(db); err != nil {
		t.Fatal(err)
	}
	if err := schema.RequireMySQL(db, mysqlIAMFoundationOwner, mysqlIAMFoundationVersion); err != nil {
		t.Fatal(err)
	}
	var existingCode string
	if err := db.Raw("SELECT code FROM tenants WHERE id=1").Row().Scan(&existingCode); err != nil || existingCode != "before-upgrade" {
		t.Fatalf("upgrade did not retain the existing tenant: %q, %v", existingCode, err)
	}
	for _, invalid := range []struct{ name, query string }{
		{"missing principal", "INSERT INTO users (id, display_name) VALUES (999, 'User')"},
		{"different principal subtype", "INSERT INTO users (id, display_name) VALUES (101, 'User')"},
		{"existing subtype changes type", "UPDATE principals SET principal_type='service_principal' WHERE id=100"},
		{"inactive principal without time", "UPDATE principals SET status='deactivated' WHERE id=100"},
		{"nonpositive version", "UPDATE principals SET authorization_version=0 WHERE id=100"},
		{"empty display name", "UPDATE users SET display_name=' ' WHERE id=100"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			if err := db.Exec(invalid.query).Error; err == nil {
				t.Fatal("schema accepted the invalid IAM fact")
			}
		})
	}

	mustExec("UPDATE principals SET authorization_version=authorization_version+1, updated_at='2000-01-01' WHERE id=100")
	mustExec("UPDATE users SET updated_at='2000-01-01' WHERE id=100")
	if err := MigrateMySQLIAMFoundation(db); err != nil {
		t.Fatal(err)
	}
	var display string
	var version int64
	var principalUpdated, userUpdated time.Time
	err = db.Raw(`SELECT u.display_name, p.authorization_version, p.updated_at, u.updated_at
		FROM principals p JOIN users u ON u.id=p.id WHERE p.id=100`).Row().Scan(&display, &version, &principalUpdated, &userUpdated)
	if err != nil || display != "验证用户 🧪" || version != 2 || principalUpdated.Year() <= 2000 || userUpdated.Year() <= 2000 {
		t.Fatalf("restarted schema returned wrong persisted data or timestamps: %q %d %v %v %v", display, version, principalUpdated, userUpdated, err)
	}

	rollback := errors.New("fixture rollback after identity writes")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO principals (id, principal_type) VALUES (102, 'user')").Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO users (id, display_name) VALUES (102, 'Rollback')").Error; err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("identity transaction did not reach the rollback: %v", err)
	}
	var leftovers int
	if err := db.Raw("SELECT COUNT(*) FROM principals WHERE id=102").Scan(&leftovers).Error; err != nil || leftovers != 0 {
		t.Fatalf("identity rollback left a principal: %d, %v", leftovers, err)
	}

	t.Run("tenant membership and authorization version", func(t *testing.T) {
		testMySQLIAMMembership(t, db)
	})
	t.Run("local password credential", func(t *testing.T) {
		testMySQLIAMLocalAccount(t, db)
	})
	t.Run("TOTP credential", func(t *testing.T) {
		testMySQLIAMMFACredential(t, db)
	})
	t.Run("concurrent tenant administrator revocations", func(t *testing.T) {
		testMySQLTenantAdministrator(t, db)
	})
	t.Run("oauth service credential", func(t *testing.T) {
		testMySQLOAuthServiceCredential(t, db)
	})
}

func testMySQLIAMMembership(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repo := iam.NewRepository(db)
	longCode := strings.Repeat("租", 1100)
	lab := iam.Tenant{ID: 10, Code: " " + longCode + " ", Name: "Lab"}
	if err := repo.CreateTenant(ctx, &lab); err != nil || lab.Code != longCode {
		t.Fatalf("create normalized tenant: %v", err)
	}
	other := iam.Tenant{ID: 11, Code: longCode + "x", Name: "Other"}
	if err := repo.CreateTenant(ctx, &other); err != nil {
		t.Fatal(err)
	}
	duplicateTenant := iam.Tenant{Code: longCode, Name: "Duplicate"}
	if err := repo.CreateTenant(ctx, &duplicateTenant); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate tenant code returned %v", err)
	}
	if err := db.Exec("UPDATE tenants SET initialized_at = NOW() WHERE id = 10").Error; err == nil {
		t.Fatal("schema accepted an incomplete tenant initialization fact")
	}
	if err := db.Exec("INSERT INTO tenants (code, name) VALUES ('UPPER', 'Invalid')").Error; err == nil {
		t.Fatal("schema accepted an unnormalized tenant code")
	}

	version := func(expected int64) {
		t.Helper()
		var actual int64
		if err := db.Raw("SELECT authorization_version FROM principals WHERE id = 100").Row().Scan(&actual); err != nil || actual != expected {
			t.Fatalf("authorization version = %d, want %d: %v", actual, expected, err)
		}
	}
	initial := int64(2)
	version(initial)
	membership := iam.TenantMembership{
		TenantID: 10, PrincipalID: 100, SourceType: "invitation", JoinedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repo.CreateTenantMembership(ctx, &membership); err != nil {
		t.Fatal(err)
	}
	version(initial + 1)
	duplicate := membership
	duplicate.ID = 0
	if err := repo.CreateTenantMembership(ctx, &duplicate); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate membership returned %v", err)
	}
	for _, invalid := range []string{
		"UPDATE tenant_memberships SET tenant_id = 11 WHERE tenant_id = 10",
		"UPDATE tenant_memberships SET principal_id = 101 WHERE tenant_id = 10",
		"UPDATE tenant_memberships SET status = 'ended' WHERE tenant_id = 10",
		"UPDATE tenant_memberships SET expires_at = joined_at WHERE tenant_id = 10",
		"UPDATE tenant_memberships SET source_type = 'unknown' WHERE tenant_id = 10",
		"INSERT INTO tenant_memberships (tenant_id, principal_id, source_type, joined_at) VALUES (99, 100, 'manual', NOW())",
	} {
		if err := db.Exec(invalid).Error; err == nil {
			t.Fatalf("accepted invalid member change: %s", invalid)
		}
	}
	version(initial + 1)

	rollback := errors.New("rollback membership")
	err := repo.Transaction(ctx, func(tx *iam.Repository) error {
		locked, err := tx.LockTenantMembership(ctx, 10, 100)
		if err != nil {
			return err
		}
		if err := tx.UpdateTenantMembershipLifecycle(ctx, locked.ID, "suspended", nil, nil); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("membership transaction did not reach rollback: %v", err)
	}
	version(initial + 1)
	restored, err := repo.LockTenantMembership(ctx, 10, 100)
	if err != nil || restored.Status != "active" {
		t.Fatalf("rollback did not restore active membership: %+v, %v", restored, err)
	}
	ended := time.Now().UTC()
	if err := repo.UpdateTenantMembershipLifecycle(ctx, membership.ID, "ended", &ended, nil); err != nil {
		t.Fatal(err)
	}
	version(initial + 2)
	if err := db.Exec("DELETE FROM tenant_memberships WHERE id = ?", membership.ID).Error; err != nil {
		t.Fatal(err)
	}
	version(initial + 3)
}

func testMySQLIAMLocalAccount(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repo := iam.NewRepository(db)
	const password = "fixture-only-pw-2026"
	hash, err := passwordutils.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	username := "ＡÉ" + strings.Repeat("用户名", 400)
	normalized := "aé" + strings.Repeat("用户名", 400)
	now := time.Now().UTC().Truncate(time.Microsecond)
	account := iam.LocalAccount{UserID: 100, Username: username, PasswordHash: hash, PasswordChangedAt: now}
	if err := repo.CreateLocalAccount(ctx, &account); err != nil {
		t.Fatal(err)
	}
	identity, err := repo.GetLocalUserIdentityByUsername(ctx, " "+normalized+" ")
	if err != nil || identity.AccountID != account.ID || identity.NormalizedUsername != normalized ||
		!passwordutils.CheckPassword(password, identity.PasswordHash) ||
		passwordutils.CheckPassword("wrong-password", identity.PasswordHash) {
		t.Fatalf("username or password lookup failed: %v", err)
	}
	lookup, err := repo.GetLocalAccountByNormalizedUsername(ctx, normalized)
	if err != nil || lookup.ID != account.ID {
		t.Fatalf("normalized username lookup failed: %v", err)
	}
	if _, err := repo.GetLocalUserIdentityByUsername(ctx, strings.Replace(normalized, "é", "e", 1)); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("distinct normalized username returned %v", err)
	}
	duplicate := iam.LocalAccount{UserID: 103, Username: normalized, PasswordHash: hash, PasswordChangedAt: now}
	if err := repo.CreateLocalAccount(ctx, &duplicate); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("equivalent normalized username returned %v", err)
	}
	for _, change := range []string{"password_hash = ''", "locked_until = NOW()", "status = 'other'"} {
		if err := db.Exec("UPDATE local_accounts SET "+change+" WHERE id = ?", account.ID).Error; err == nil {
			t.Fatalf("invalid account change succeeded: %s", change)
		}
	}

	replacement, err := passwordutils.HashPassword("new-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback credential")
	err = repo.Transaction(ctx, func(tx *iam.Repository) error {
		if err := tx.UpdateLocalAccountPassword(ctx, account.ID, replacement, now.Add(time.Minute)); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("password change did not reach rollback: %v", err)
	}
	lookup, err = repo.GetLocalAccountByUserID(ctx, account.UserID)
	if err != nil || lookup.PasswordHash != hash || !lookup.PasswordChangedAt.Equal(now) {
		t.Fatalf("rollback changed the original password credential: %v", err)
	}
	if err := repo.UpdateLocalAccountPassword(ctx, account.ID, replacement, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	lookup, err = repo.GetLocalAccountByUserID(ctx, account.UserID)
	if err != nil || !passwordutils.CheckPassword("new-fixture-password", lookup.PasswordHash) {
		t.Fatalf("committed password change could not be verified: %v", err)
	}
}

func testMySQLIAMMFACredential(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repo := iam.NewRepository(db)
	cipher, err := iam.NewMFACredentialCipher(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "JBSWY3DPEHPK3PXP"
	ciphertext, nonce, version, err := cipher.EncryptTOTPSecret(100, secret)
	if err != nil {
		t.Fatal(err)
	}
	credential := iam.MFACredential{
		UserID: 100, Method: "totp", Status: iam.MFACredentialStatusActive,
		SecretCiphertext: ciphertext, SecretNonce: nonce, KeyVersion: version,
	}
	if err := repo.CreateMFACredential(ctx, &credential); err != nil {
		t.Fatal(err)
	}
	replacement := credential
	replacement.ID = 0
	if err := repo.CreateMFACredential(ctx, &replacement); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate active TOTP credential returned %v", err)
	}
	if err := repo.Transaction(ctx, func(tx *iam.Repository) error {
		stored, err := tx.LockActiveMFACredential(ctx, 100)
		if err != nil {
			return err
		}
		decrypted, err := cipher.DecryptTOTPSecret(stored)
		if err != nil || decrypted != secret {
			return errors.New("stored TOTP ciphertext failed round-trip")
		}
		return tx.UpdateMFALastAcceptedCounter(ctx, stored.ID, 10)
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateMFALastAcceptedCounter(ctx, credential.ID, 10); !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("replayed TOTP counter returned %v", err)
	}
	if err := repo.DisableMFACredential(ctx, credential.ID); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"UPDATE mfa_credentials SET status = 'active' WHERE id = ?",
		"UPDATE mfa_credentials SET last_accepted_counter = NULL WHERE id = ?",
		"UPDATE mfa_credentials SET user_id = 103 WHERE id = ?",
		"DELETE FROM mfa_credentials WHERE id = ?",
	} {
		if err := db.Exec(invalid, credential.ID).Error; err == nil {
			t.Fatalf("invalid MFA credential change succeeded: %s", invalid)
		}
	}
	if err := repo.CreateMFACredential(ctx, &replacement); err != nil {
		t.Fatal(err)
	}
	active, err := repo.LockActiveMFACredential(ctx, 100)
	if err != nil || active.ID != replacement.ID || active.LastAcceptedCounter != nil {
		t.Fatalf("replacement MFA credential could not be read: %v", err)
	}
}
