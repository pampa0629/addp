package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
)

// Called with the runtime identity by the gate that owns the installation.
func TestManagerPreparedRuntime(t *testing.T) {
	mode := os.Getenv("ADDP_TEST_MANAGER_PREPARED_RUNTIME")
	if mode == "" {
		t.Skip("requires an isolated platform installation")
	}
	cfg := config.Load()
	db, err := InitDatabase(cfg)
	if mode == "missing" {
		if err == nil || !(strings.Contains(err.Error(), "manager.startup_schema_revision") || strings.Contains(err.Error(), "schema manager ")) {
			t.Fatalf("expected Manager version rejection: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := projectionstore.Open(db, "manager", "manager", nil); err != nil {
		t.Fatal(err)
	}
	enabled, err := HasEmbeddingStorage(db)
	if err != nil || enabled != (mode == "vector") {
		t.Fatalf("embedding storage: enabled=%v err=%v", enabled, err)
	}
	if _, err := NewEmbeddingConfigurationRepository(db).Get(t.Context()); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	repo := NewInferenceScenarioBindingRepository(tx)
	const id = "f8d30286-3a1f-45a2-ba30-235695100001"
	const overrideID = "f8d30286-3a1f-45a2-ba30-235695100002"
	base := models.InferenceScenarioBinding{ScenarioCode: "image_segmentation", ScopeType: "platform", ModelProfileID: id, UpdatedBy: 1}
	if err := repo.Save(t.Context(), &base, 0); err != nil {
		t.Fatal(err)
	}
	tenant := uint(801)
	override := models.InferenceScenarioBinding{ScenarioCode: base.ScenarioCode, ScopeType: "tenant", TenantID: &tenant, ModelProfileID: overrideID, UpdatedBy: 1}
	if err := repo.Save(t.Context(), &override, 0); err != nil {
		t.Fatal(err)
	}
	for tenantID, want := range map[uint]string{tenant: overrideID, tenant + 1: id} {
		got, err := repo.Resolve(t.Context(), tenantID, base.ScenarioCode)
		if err != nil || got == nil || got.ModelProfileID != want {
			t.Fatalf("resolve tenant %d: %+v, %v", tenantID, got, err)
		}
	}
}
