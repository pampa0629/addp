package service

import (
	"os"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
)

func verifyRustFSRegistration(t *testing.T, connection models.ConnectionInfo) (*EngineService, uint) {
	t.Helper()
	repo := newEngineServiceTestRepository(t)
	service := NewEngineService(repo, []byte("addp-dev-encryption-key-2025!!!!"), nil)
	descriptors, err := engineplugin.ListEngineTypeDescriptors("general")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, descriptor := range descriptors {
		if descriptor.Type != "rustfs" {
			continue
		}
		found = true
		if descriptor.DisplayName != "RustFS" || descriptor.Capabilities.EngineType != "rustfs" || descriptor.CatalogModel == nil || descriptor.CatalogModel.RootTerm != "service" {
			t.Fatalf("wrong RustFS descriptor: %#v", descriptor)
		}
	}
	if !found {
		t.Fatal("RustFS missing from general registration catalog")
	}
	request := &models.EngineCreateRequest{Name: "RustFS fixture", EngineType: "rustfs", EngineOrigin: "general", ConnectionInfo: connection}
	created, isNew, err := service.Create(request, 42, 7)
	if err != nil || !isNew {
		t.Fatal("RustFS registration failed", err)
	}
	stored, err := repo.GetByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"access_key", "secret_key"} {
		if created.ConnectionInfo[key] != "******" {
			t.Fatalf("public %s is not masked", key)
		}
		if stored.ConnectionInfo[key] == connection[key] {
			t.Fatalf("stored %s is plaintext", key)
		}
	}
	plain, err := service.decryptStoredConnectionInfo("rustfs", stored.ConnectionInfo)
	if err != nil || plain["access_key"] != connection["access_key"] || plain["secret_key"] != connection["secret_key"] {
		t.Fatal("RustFS encrypted credentials cannot be recovered")
	}
	again, isNew, err := service.Create(request, 42, 7)
	if err != nil || isNew || again.ID != created.ID {
		t.Fatal("RustFS registration is not idempotent", err)
	}
	return service, created.ID
}

func TestRustFSRegistrationUsesIndependentTypeAndSensitiveFields(t *testing.T) {
	verifyRustFSRegistration(t, models.ConnectionInfo{"endpoint": "rustfs.invalid:9000", "access_key": "test-access-key", "secret_key": "test-secret-key", "use_ssl": false})
}

func TestIntegrationRustFSSystemConnection(t *testing.T) {
	if os.Getenv("ADDP_RUSTFS_INTEGRATION") != "1" {
		t.Skip("owned RustFS T2 required")
	}
	service, id := verifyRustFSRegistration(t, models.ConnectionInfo{
		"endpoint": os.Getenv("ADDP_RUSTFS_TEST_ENDPOINT"), "access_key": os.Getenv("ADDP_RUSTFS_TEST_ACCESS_KEY"), "secret_key": os.Getenv("ADDP_RUSTFS_TEST_SECRET_KEY"), "use_ssl": false,
	})
	if !service.CheckAndUpdateConnectionStatus(id) {
		t.Fatal("System RustFS connection probe failed")
	}
	if _, err := service.GetForExecution(id, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetForExecution(id, 8); err == nil {
		t.Fatal("cross-tenant RustFS access accepted")
	}
	verifier := NewIndependentGrantTargetVerifier(service)
	target := engineplugin.ObjectItemPath(id, os.Getenv("ADDP_RUSTFS_TEST_BUCKET"), "样例/订单.csv")
	version, err := verifier.VerifyIndependentReadTarget(t.Context(), 7, target)
	if err != nil || version <= 0 {
		t.Fatalf("real RustFS independent Grant target rejected: version=%d error=%v", version, err)
	}
	if _, err := verifier.VerifyIndependentReadTarget(t.Context(), 7, engineplugin.FileItemPath(id, target.StringPath())); err == nil {
		t.Fatal("RustFS admitted the file path model")
	}
	missing := engineplugin.ObjectItemPath(id, os.Getenv("ADDP_RUSTFS_TEST_BUCKET"), "样例/not-present.csv")
	if _, err := verifier.VerifyIndependentReadTarget(t.Context(), 7, missing); err == nil {
		t.Fatal("RustFS authorized a missing physical object")
	}
	t.Log("real IndependentGrantTargetVerifier resolved the exact RustFS object; wrong path model and missing object were rejected")
}
