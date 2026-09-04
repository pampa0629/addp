package exportartifact

import (
	"context"
	"errors"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type transferStub struct {
	request *commonClient.CreateTransferExecutionRequest
	err     error
}

func (s *transferStub) CreateExecution(_ context.Context, request *commonClient.CreateTransferExecutionRequest) (*commonClient.CreateTransferExecutionResponse, error) {
	s.request = request
	if s.err != nil {
		return nil, s.err
	}
	return &commonClient.CreateTransferExecutionResponse{ExecutionID: "transfer-1", Status: StatusPending}, nil
}

func (s *transferStub) GetExecution(_ string, _ uint) (*commonClient.TransferExecutionResponse, error) {
	return &commonClient.TransferExecutionResponse{Status: StatusPending}, nil
}

func testMinIOClient(t *testing.T) *minio.Client {
	t.Helper()
	client, err := minio.New("127.0.0.1:1", &minio.Options{
		Creds: credentials.NewStaticV4("test", "test-secret", ""),
	})
	if err != nil {
		t.Fatalf("create minio client: %v", err)
	}
	return client
}

func TestCreateOwnsInfraTargetAndDisablesMetadataScan(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(db, "export_sessions"); err != nil {
		t.Fatal(err)
	}
	transfer := &transferStub{}
	service := NewService(transfer, NewGormStore(db, "export_sessions"), testMinIOClient(t), "manager", "develop", "/api/v1/develop/exports")

	created, err := service.Create(context.Background(), CreateRequest{
		TenantID: 7, UserID: 9, SourceRef: "query-execution-1", Format: format.FormatCSV, FileName: "orders.csv",
		ExecutionConfig: commonClient.TransferExecutionConfig{
			Target: commonClient.TransferExecutionEndpoint{ParentLocator: "addp://engine/99/path/ignored", Locator: "addp://engine/99/path/ignored.csv"},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.TransferExecutionID != "transfer-1" || created.Status != StatusPending || created.FileName != "orders.csv" {
		t.Fatalf("created = %#v", created)
	}
	request := transfer.request
	if request == nil || request.TenantID != 7 || request.AutoScanMetadata {
		t.Fatalf("transfer request = %#v", request)
	}
	if !strings.HasPrefix(request.Config.Target.ParentLocator, "addp-infra://minio/manager/tenant_7/export/develop/") {
		t.Fatalf("target parent locator = %q", request.Config.Target.ParentLocator)
	}
	if request.Config.Target.Locator != "" || request.Config.Target.Name != "orders.csv" {
		t.Fatalf("target = %#v", request.Config.Target)
	}
}

func TestGormStoreScopesSessionByTenantAndUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(db, "export_sessions"); err != nil {
		t.Fatal(err)
	}
	store := NewGormStore(db, "export_sessions")
	session := &Session{TenantID: 7, UserID: 9, SourceRef: "source", Format: "csv", FileName: "data.csv", TargetParentLocator: "addp-infra://minio/manager/tenant_7/export/develop/session?type=prefix", TargetLocator: "addp-infra://minio/manager/tenant_7/export/develop/session/data.csv?type=object", TransferExecutionID: "transfer-1", Status: StatusPending}
	if err := store.Create(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(context.Background(), session.ID, 7, 9); err != nil || got == nil {
		t.Fatalf("owner Get() = %#v, %v", got, err)
	}
	for _, scope := range [][2]uint{{8, 9}, {7, 10}} {
		if got, err := store.Get(context.Background(), session.ID, scope[0], scope[1]); err != nil || got != nil {
			t.Fatalf("foreign scope %v Get() = %#v, %v", scope, got, err)
		}
	}
}

func TestCreateKeepsFailedSessionForCleanupWhenTransferCreationFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(db, "export_sessions"); err != nil {
		t.Fatal(err)
	}
	transfer := &transferStub{err: errors.New("transfer unavailable")}
	service := NewService(transfer, NewGormStore(db, "export_sessions"), testMinIOClient(t), "manager", "develop", "/api/v1/develop/exports")

	_, err = service.Create(context.Background(), CreateRequest{
		TenantID: 7, UserID: 9, SourceRef: "query-execution-1", Format: format.FormatCSV, FileName: "orders.csv",
	})
	if err == nil || !strings.Contains(err.Error(), "transfer unavailable") {
		t.Fatalf("Create() error = %v", err)
	}
	var session Session
	if err := db.Table("export_sessions").First(&session).Error; err != nil {
		t.Fatal(err)
	}
	if session.Status != StatusFailed || session.TransferExecutionID != "" || session.TargetParentLocator == "" {
		t.Fatalf("failed session = %#v", session)
	}
}

func TestManifestKeepsMultiFileExportAsZip(t *testing.T) {
	session := &Session{Format: string(format.FormatShapefile), FileName: "roads.zip", TargetLocator: "addp-infra://minio/manager/tenant_7/export/manager/session/roads.shp?type=object"}
	manifestJSON := buildManifest(session, map[string]interface{}{
		"target_refs": []interface{}{
			map[string]interface{}{"path": "tenant_7/export/manager/session/roads.shp", "role": "main", "required": true, "primary": true, "extension": ".shp"},
			map[string]interface{}{"path": "tenant_7/export/manager/session/roads.shx", "role": "shx", "required": true, "extension": ".shx"},
		},
	})
	manifest := manifestFromJSON(commonModels.JSONMap(manifestJSON))
	if manifest.SchemaVersion != manifestVersion || manifest.Layout != format.LayoutMulti || manifest.Download.Kind != "zip" || manifest.Download.FileName != "roads.zip" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Refs) != 2 || manifest.Refs[0].Entry != "roads.shp" || manifest.Refs[1].Entry != "roads.shx" {
		t.Fatalf("refs = %#v", manifest.Refs)
	}
}

func TestInfraObjectPathValidatesBucket(t *testing.T) {
	got, err := InfraObjectPath("addp-infra://minio/manager/tenant_7/export/develop/session/data.csv?type=object", "manager")
	if err != nil || got != "tenant_7/export/develop/session/data.csv" {
		t.Fatalf("InfraObjectPath() = %q, %v", got, err)
	}
	if _, err := InfraObjectPath("addp-infra://minio/other/tenant_7/export/data.csv?type=object", "manager"); err == nil {
		t.Fatal("InfraObjectPath() accepted a different bucket")
	}
}
