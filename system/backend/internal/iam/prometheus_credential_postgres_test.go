package iam

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/testsupport"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPrometheusCredentialAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System IAM PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	provisioner, err := NewServiceCredentialProvisioner(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	for i, id := range builtinServiceClientIDs {
		secrets[id] = fmt.Sprintf("%032d", i+1)
	}
	secrets["addp-prometheus"] = ""
	if err := provisioner.Apply(ctx, secrets); err != nil {
		t.Fatal(err)
	}
	var row serviceOAuthClientCredentialRow
	read := func() {
		t.Helper()
		if err := db.Where("client_id=?", "addp-prometheus").Take(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	read()
	if row.Status != "disabled" || row.ClientSecretHash != nil {
		t.Fatal("optional client enabled without credential")
	}
	var permissions []string
	if err := db.Raw(`SELECT p.permission_key FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id JOIN system.role_permissions rp ON rp.role_id=r.id JOIN system.permissions p ON p.id=rp.permission_id WHERE a.principal_id=? AND a.status='active' ORDER BY p.permission_key`, row.ServicePrincipalID).Scan(&permissions).Error; err != nil {
		t.Fatal(err)
	}
	if len(permissions) != 1 || permissions[0] != "monitor.metrics_discovery.read" {
		t.Fatalf("Prometheus permissions=%v", permissions)
	}

	var targetGrants, unexpectedGrants, invalidDefinitions int64
	if err := db.Raw(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key LIKE 'monitor.monitoring_target.%' AND r.role_key='platform.system_administrator' AND r.tenant_id IS NULL`).Scan(&targetGrants).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key LIKE 'monitor.monitoring_target.%' AND r.role_key<>'platform.system_administrator'`).Scan(&unexpectedGrants).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw(`SELECT count(*) FROM system.permissions WHERE (permission_key LIKE 'monitor.monitoring_target.%' OR permission_key='monitor.metrics_discovery.read') AND (owner_module<>'monitor' OR allowed_scope_types<>ARRAY['platform']::text[] OR delegable OR tenant_customizable OR status<>'active')`).Scan(&invalidDefinitions).Error; err != nil {
		t.Fatal(err)
	}
	if targetGrants != 4 || unexpectedGrants != 0 || invalidDefinitions != 0 {
		t.Fatalf("target grants=%d unexpected=%d invalid=%d", targetGrants, unexpectedGrants, invalidDefinitions)
	}
	var memberships int64
	if err := db.Table("system.tenant_memberships").Where("principal_id=?", row.ServicePrincipalID).Count(&memberships).Error; err != nil || memberships != 0 {
		t.Fatalf("tenant memberships=%d err=%v", memberships, err)
	}
	secrets["addp-prometheus"] = strings.Repeat("p", 32)
	if err := provisioner.Apply(ctx, secrets); err != nil {
		t.Fatal(err)
	}
	read()
	if row.Status != "active" || row.ClientSecretHash == nil || bcrypt.CompareHashAndPassword([]byte(*row.ClientSecretHash), []byte(secrets["addp-prometheus"])) != nil {
		t.Fatal("independent credential not provisioned")
	}
	var version int64
	readVersion := func() int64 {
		t.Helper()
		if err := db.Table("system.principals").Select("authorization_version").Where("id=?", row.ServicePrincipalID).Scan(&version).Error; err != nil {
			t.Fatal(err)
		}
		return version
	}
	before := readVersion()
	var familyID int64
	if err := db.Raw(`INSERT INTO system.refresh_token_families(protocol_request_id,principal_id,context_type,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at)
 VALUES (gen_random_uuid(),?,'platform',?,'addp-prometheus','oauth',ARRAY['addp.api'],ARRAY['addp.api'],ARRAY['service_secret'],'not_applicable',transaction_timestamp(),transaction_timestamp()+interval '5 minutes') RETURNING id`, row.ServicePrincipalID, before).Scan(&familyID).Error; err != nil {
		t.Fatal(err)
	}
	secrets["addp-prometheus"] = ""
	if err := provisioner.Apply(ctx, secrets); err != nil {
		t.Fatal(err)
	}
	read()
	if row.Status != "disabled" || readVersion() != before+1 {
		t.Fatal("client disabling did not revoke authorization version")
	}
	var reason string
	if err := db.Table("system.refresh_token_families").Select("revoked_reason").Where("id=? AND revoked_at IS NOT NULL", familyID).Scan(&reason).Error; err != nil || reason != "service_credential_disabled" {
		t.Fatalf("family revocation=%q err=%v", reason, err)
	}
	if err := provisioner.Apply(ctx, secrets); err != nil {
		t.Fatal(err)
	}
	if readVersion() != before+1 {
		t.Fatal("repeated disable changed version")
	}
	var monitorStatus string
	db.Table("system.oauth_clients").Select("status").Where("client_id='addp-monitor'").Scan(&monitorStatus)
	if monitorStatus != "active" {
		t.Fatal("optional disable affected Monitor client")
	}
}
