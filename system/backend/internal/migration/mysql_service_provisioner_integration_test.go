package migration

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
	iamoauth "github.com/addp/system/internal/iam/oauth"
	"github.com/google/uuid"
	"github.com/ory/fosite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func testMySQLServiceProvisioner(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	repo := iam.NewRepository(db)
	exec := func(query string) {
		t.Helper()
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	const clientID = "addp-meta-publisher"
	const originalSecret = "fixture-only-original-publisher-secret"
	const rotatedSecret = "fixture-only-replacement-publisher-secret"
	var principalID int64
	if err := db.Raw("SELECT service_principal_id FROM oauth_clients WHERE client_id=?", clientID).Row().Scan(&principalID); err != nil {
		t.Fatal(err)
	}
	entityID := strconv.FormatInt(principalID, 10)

	provisioner, err := iam.NewServiceCredentialProvisioner(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := provisioner.ProvisionPublisher(ctx, clientID, originalSecret); err != nil {
			t.Fatal(err)
		}
	}
	assertServiceAuditCount(t, db, entityID, "iam.service_principal.credential.provisioned", 1)

	storage, err := iamoauth.NewStorage(db, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client, err := storage.GetClient(ctx, clientID)
	if err != nil || bcrypt.CompareHashAndPassword(client.GetHashedSecret(), []byte(originalSecret)) != nil {
		t.Fatalf("provisioned client did not authenticate: %v", err)
	}
	request := &fosite.AccessRequest{
		GrantTypes: fosite.Arguments{"client_credentials"},
		Request: fosite.Request{
			ID: uuid.NewString(), Client: client, Session: iamoauth.NewIAMSession(),
			RequestedScope: fosite.Arguments{"addp.api"}, RequestedAudience: fosite.Arguments{"addp.api"},
		},
	}
	if err := storage.PopulateServiceCredentialSession(ctx, request, "platform", ""); err != nil {
		t.Fatal(err)
	}
	audit := iam.AuditEvent{
		EventName: "oauth.token.issued", Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskLow,
		ModuleName: "system", EntityType: "oauth_security_event", EntityID: "oauth.token.issued",
		Details: map[string]any{"grant_type": "client_credentials"},
	}
	txContext, err := storage.BeginTX(iamoauth.WithTransactionAudit(ctx, audit))
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Rollback(txContext)
	signature := strings.Repeat("f", 64)
	if err := storage.CreateAccessTokenSession(txContext, signature, request); err != nil {
		t.Fatal(err)
	}
	if err := storage.Commit(txContext); err != nil {
		t.Fatal(err)
	}
	token, err := repo.GetAccessTokenByHash(ctx, signature)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := repo.GetPrincipal(ctx, principalID)
	if err != nil {
		t.Fatal(err)
	}

	exec(`CREATE TRIGGER fixture_fail_rotation_audit BEFORE INSERT ON audit_logs FOR EACH ROW
	      BEGIN IF NEW.event_name = 'iam.service_principal.credential.rotated' THEN
	        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fixture audit failure'; END IF; END`)
	if err := provisioner.ProvisionPublisher(ctx, clientID, rotatedSecret); err == nil {
		t.Fatal("credential rotation committed despite failed audit")
	}
	exec("DROP TRIGGER fixture_fail_rotation_audit")
	assertServiceAuditCount(t, db, entityID, "iam.service_principal.credential.rotated", 0)
	client, err = storage.GetClient(ctx, clientID)
	if err != nil || bcrypt.CompareHashAndPassword(client.GetHashedSecret(), []byte(originalSecret)) != nil {
		t.Fatalf("audit failure replaced the original credential: %v", err)
	}
	unchanged, err := repo.GetPrincipal(ctx, principalID)
	if err != nil || unchanged.AuthorizationVersion != principal.AuthorizationVersion {
		t.Fatalf("audit failure advanced the authorization version: %v", err)
	}
	if _, err := storage.GetAccessTokenSession(ctx, signature, nil); err != nil {
		t.Fatalf("audit failure revoked original token session: %v", err)
	}

	if err := provisioner.ProvisionPublisher(ctx, clientID, rotatedSecret); err != nil {
		t.Fatal(err)
	}
	assertServiceAuditCount(t, db, entityID, "iam.service_principal.credential.rotated", 1)
	client, err = storage.GetClient(ctx, clientID)
	if err != nil || bcrypt.CompareHashAndPassword(client.GetHashedSecret(), []byte(rotatedSecret)) != nil ||
		bcrypt.CompareHashAndPassword(client.GetHashedSecret(), []byte(originalSecret)) == nil {
		t.Fatalf("new credential did not replace the old hash: %v", err)
	}
	family, err := repo.GetRefreshTokenFamily(ctx, token.FamilyID)
	if err != nil || family.RevokedAt == nil {
		t.Fatalf("previous family remained active after credential rotation: %v", err)
	}
	if _, err := storage.GetAccessTokenSession(ctx, signature, nil); !errors.Is(err, fosite.ErrInactiveToken) {
		t.Fatalf("old token session returned %v", err)
	}

	auditWriter := iam.NewAuditWriter(repo)
	audit = iam.AuditEvent{
		EventName: "iam.test.audit", Result: iam.AuditResultDenied, RiskLevel: iam.AuditRiskLow,
		ModuleName: "system", EntityType: "service_principal", EntityID: entityID,
		Details: map[string]any{"nested": []any{map[string]string{"Client_Secret": "fixture-only"}}},
	}
	if err := auditWriter.Write(ctx, audit); err == nil {
		t.Fatal("audit stored a nested prohibited field")
	}
	for _, invalid := range []string{
		"UPDATE audit_logs SET event_name='iam.test.changed' WHERE event_name='iam.service_principal.credential.rotated'",
		"DELETE FROM audit_logs WHERE event_name='iam.service_principal.credential.rotated'",
	} {
		if err := db.Exec(invalid).Error; err == nil {
			t.Fatal("audit allowed mutation of recorded history")
		}
	}
}

func assertServiceAuditCount(t *testing.T, db *gorm.DB, entityID, event string, expected int) {
	t.Helper()
	var actual int
	if err := db.Raw("SELECT COUNT(*) FROM audit_logs WHERE event_name = ? AND entity_id = ?", event, entityID).Row().Scan(&actual); err != nil || actual != expected {
		t.Fatalf("audit event count for %q = %d, want %d: %v", event, actual, expected, err)
	}
}
