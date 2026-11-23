package repository

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/schema"
	"github.com/addp/inference/internal/config"
	"github.com/addp/inference/internal/migration"
	"github.com/addp/inference/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestInferencePlatformStore(t *testing.T) {
	if os.Getenv("ADDP_TEST_INFERENCE_PLATFORM") != "1" {
		t.Skip("requires gate-owned platform database")
	}
	db, err := bootstrap.OpenPlatformRuntime(config.PlatformDatabase(), "inference", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := schema.Require(db, "inference", migration.Version); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store := NewStore(db)
	p := models.ProviderConnection{ID: uuid.NewString(), Name: "provider", ScopeType: "platform", AdapterType: "test", Endpoint: "https://example.invalid"}
	if err := store.CreateProvider(ctx, &p, []models.ProviderTenantGrant{{ProviderConnectionID: p.ID, TenantID: 1}}); err != nil {
		t.Fatal(err)
	}
	d := models.ModelDeployment{ID: uuid.NewString(), ProviderConnectionID: p.ID, Name: "model", UpstreamModel: "test", Operations: []byte(`["chat"]`), Modalities: []byte(`["text"]`)}
	if err := store.CreateDeployment(ctx, &d); err != nil {
		t.Fatal(err)
	}
	read, err := store.GetDeployment(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	if json.Unmarshal(read.Operations, &ops) != nil || len(ops) != 1 || ops[0] != "chat" {
		t.Fatal("lost operations")
	}
	a, b := uint(1), uint(2)
	for _, sc := range []struct {
		owner     string
		tenant    *uint
		duplicate bool
	}{{"platform", nil, false}, {"tenant", &a, false}, {"tenant", &b, false}, {"platform", nil, true}, {"tenant", &a, true}} {
		profile := models.ModelProfile{ID: uuid.NewString(), Name: "profile", Code: "same-code", ScopeType: sc.owner, TenantID: sc.tenant, ModelDeploymentID: d.ID}
		err := store.CreateProfile(ctx, &profile)
		if (err != nil) != sc.duplicate {
			t.Fatalf("scope %s duplicate %t: %v", sc.owner, sc.duplicate, err)
		}
	}
	if _, err := store.RotateCredential(ctx, p.ID, "new-ciphertext", "set", 1); err != nil {
		t.Fatal(err)
	}
	var audit models.CredentialAudit
	if err := db.Where("provider_connection_id=?", p.ID).First(&audit).Error; err != nil || audit.OldVersion != 0 || audit.NewVersion != 1 {
		t.Fatalf("credential audit: %v", err)
	}
	// A too-long audit action must undo the provider write in the same transaction.
	if _, err := store.RotateCredential(ctx, p.ID, "uncommitted", "too-long-for-action-column", 1); err == nil {
		t.Fatal("expected audit write rejection")
	}
	readProvider, err := store.GetProvider(ctx, p.ID, false)
	if err != nil || readProvider.CredentialVersion != 1 || readProvider.CredentialCiphertext != "new-ciphertext" {
		t.Fatalf("credential rollback: %v", err)
	}
}
