package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Exercised by the existing no-skip OAuth PostgreSQL CI gate. This proves real
// credential issuance/revocation, not a fixture AuthContext response.
func exerciseSystemBasisCredential(t *testing.T, ctx context.Context, db *gorm.DB, runtime *IAMRuntime, router *gin.Engine, secrets map[string]string, tenantID int64) {
	t.Run("optional independent System basis identity", func(t *testing.T) {
		var id, roleID int64
		if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-system'").Scan(&id).Error; err != nil || id == 0 {
			t.Fatalf("system identity=%d %v", id, err)
		}
		if err := db.Raw("SELECT id FROM system.roles WHERE role_key='tenant.system_runtime'").Scan(&roleID).Error; err != nil || roleID == 0 {
			t.Fatalf("runtime role=%d %v", roleID, err)
		}
		if err := db.Exec(`INSERT INTO system.tenant_memberships (tenant_id,principal_id,status,source_type,joined_at,created_by_principal_id) VALUES (?,?,'active','bootstrap',clock_timestamp(),?)`, tenantID, id, id).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO system.role_assignments (principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type,grant_reason) VALUES (?,?,'tenant',?,'active',clock_timestamp(),'bootstrap','System basis fixture')`, id, roleID, tenantID).Error; err != nil {
			t.Fatal(err)
		}
		secret := "system-basis-fixture-0123456789abcdef0123456789abcdef"
		form := url.Values{"tenant_id": {strconv.FormatInt(tenantID, 10)}}
		if response := performIAMOAuthClientCredentialsFormRequest(t, router, "addp-system", secret, form); response.Code != http.StatusUnauthorized {
			t.Fatalf("disabled identity token status=%d", response.Code)
		}
		configured := make(map[string]string, len(secrets))
		for key, value := range secrets {
			configured[key] = value
		}
		configured["addp-system"] = secret
		provisioner, err := iam.NewServiceCredentialProvisioner(iam.NewRepository(db), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := provisioner.Apply(ctx, configured); err != nil {
			t.Fatal(err)
		}
		response := performIAMOAuthClientCredentialsFormRequest(t, router, "addp-system", secret, form)
		if response.Code != http.StatusOK {
			t.Fatalf("configured identity status=%d", response.Code)
		}
		var payload struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		ac, err := runtime.AuthContextService.ResolveAccessToken(ctx, payload.AccessToken)
		if err != nil || len(ac.Authorization.RoleAssignments) != 1 || len(ac.Authorization.RoleAssignments[0].Permissions) != 1 || ac.Authorization.RoleAssignments[0].Permissions[0] != "catalog.sharing_fulfillment.read" {
			t.Fatalf("unexpected basis authority: %v", err)
		}
		if response := performIAMOAuthPlatformClientCredentialsRequest(t, router, "addp-system", secret); response.Code != http.StatusBadRequest {
			t.Fatalf("System obtained platform authority: %d", response.Code)
		}
		if err := provisioner.Apply(ctx, secrets); err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.AuthContextService.ResolveAccessToken(ctx, payload.AccessToken); err == nil {
			t.Fatal("removed credential left old token valid")
		}
		if response := performIAMOAuthClientCredentialsFormRequest(t, router, "addp-system", secret, form); response.Code != http.StatusUnauthorized {
			t.Fatalf("removed credential status=%d", response.Code)
		}
	})
}
