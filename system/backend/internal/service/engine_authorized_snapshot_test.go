package service

import (
	"testing"

	"github.com/addp/common/secretcipher"
	"github.com/addp/system/internal/models"
)

func TestAuthorizedExecutionSnapshotDoesNotRefetchOrMutate(t *testing.T) {
	key := []byte("addp-dev-encryption-key-2025!!!!")
	secret, err := secretcipher.Encrypt("disposable-secret", key)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewEngineService(nil, key, nil) // no repository: a second lookup would fail.
	tenant := uint(41)
	engine := &models.Engine{ID: 9, TenantID: &tenant, Version: 4, EngineType: "ftp", LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"host": "ftp.invalid", "password": secret}}
	decoded, err := svc.DecryptAuthorizedExecutionSnapshot(engine)
	if err != nil || decoded == nil || decoded == engine || decoded.Version != 4 || decoded.ConnectionInfo["password"] != "disposable-secret" || engine.ConnectionInfo["password"] != secret {
		t.Fatalf("invalid snapshot decoding: %v", err)
	}
	decoded.ConnectionInfo["host"] = "changed.invalid"
	if engine.ConnectionInfo["host"] != "ftp.invalid" {
		t.Fatal("projection mutated stored snapshot")
	}
	nfs := *engine
	nfs.EngineType = "nfs"
	nfs.ConnectionInfo = models.ConnectionInfo{"server": "localhost", "export_path": "/exports/fixture"}
	if decoded, err := svc.DecryptAuthorizedExecutionSnapshot(&nfs); err != nil || decoded == nil || decoded.EngineType != "nfs" {
		t.Fatalf("NFS snapshot: %v", err)
	}
	for _, kind := range []string{"minio", "postgresql"} {
		copy := *engine
		copy.EngineType = kind
		if got, err := svc.DecryptAuthorizedExecutionSnapshot(&copy); err == nil || got != nil {
			t.Fatal("unadmitted snapshot decoded")
		}
	}
	copy := *engine
	copy.ConnectionInfo = models.ConnectionInfo{"password": "plaintext"}
	if got, err := svc.DecryptAuthorizedExecutionSnapshot(&copy); err == nil || got != nil {
		t.Fatal("invalid stored secret returned")
	}
}
