package exportartifact

import (
	"context"
	"errors"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/common/infra/objectstore"
	objectminio "github.com/addp/common/infra/objectstore/minio"
	commonModels "github.com/addp/common/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type transferStub struct {
	result  *commonClient.TransferExecutionResponse
	reads   int
	request *commonClient.CreateTransferExecutionRequest
	err     error
}

func (s *transferStub) CreateExecution(_ context.Context, request *commonClient.CreateTransferExecutionRequest) (*commonClient.CreateTransferExecutionResponse, error) {
	s.request = request
	if s.err != nil {
		return nil, s.err
	}
	return &commonClient.CreateTransferExecutionResponse{ExecutionID: request.ExportSession.ExecutionID, Status: StatusPending}, nil
}

func (s *transferStub) GetExecution(_ context.Context, id string, _ uint) (*commonClient.TransferExecutionResponse, error) {
	s.reads++
	if s.result != nil {
		result := *s.result
		if result.ExecutionID == "" {
			result.ExecutionID = id
		}
		return &result, nil
	}
	return &commonClient.TransferExecutionResponse{Status: StatusPending}, nil
}

func testMinIOClient(t *testing.T) objectstore.ContentArea {
	t.Helper()
	client, err := minio.New("127.0.0.1:1", &minio.Options{
		Creds: credentials.NewStaticV4("test", "test-secret", ""),
	})
	if err != nil {
		t.Fatalf("create minio client: %v", err)
	}
	return objectminio.New(client, "manager")
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
	if created.TransferExecutionID != transfer.request.ExportSession.ExecutionID || created.Status != StatusPending || created.FileName != "orders.csv" {
		t.Fatalf("created = %#v", created)
	}
	request := transfer.request
	if request == nil || request.TenantID != 7 || request.AutoScanMetadata {
		t.Fatalf("transfer request = %#v", request)
	}
	if !strings.HasPrefix(request.Config.Target.ParentLocator, "addp-infra://object/manager/tenant_7/export/develop/") {
		t.Fatalf("target parent locator = %q", request.Config.Target.ParentLocator)
	}
	if request.Config.Target.Locator != "" || request.Config.Target.Name != "orders.csv" {
		t.Fatalf("target = %#v", request.Config.Target)
	}
}

func TestFileGDBCreateAdmitsOnlyAfterFreezingSessionAndPendingExecution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(db, "export_sessions"); err != nil {
		t.Fatal(err)
	}
	transfer := &transferStub{}
	store := NewGormStore(db, "export_sessions")
	service := NewService(transfer, store, testMinIOClient(t), "manager", "manager", "/api/v1/manager/exports")
	binding, err := authorizationfacts.NativeCandidateBinding(plugin.TabularItemPath(5, "schema", "business", "roads"))
	if err != nil {
		t.Fatal(err)
	}
	const locator = "addp://engine/5/path/business/roads?type=table"
	denied := errors.New("source authorization denied")
	_, err = service.Create(t.Context(), CreateRequest{
		TenantID: 7, UserID: 9, SourceRef: locator, Format: format.FormatFileGDB, FileName: "roads",
		FileGDBActor: &commonClient.FileGDBExportActor{PrincipalID: 9, TenantMembershipID: 10, AuthorizationVersion: 3},
		ExecutionConfig: commonClient.TransferExecutionConfig{
			FileGDBSource: &execution.FileGDBExportSource{EngineVersion: "1", Binding: binding},
			Source:        commonClient.TransferExecutionEndpoint{Locator: locator, DataType: "table", Representation: "native"},
			Target:        commonClient.TransferExecutionEndpoint{DataType: "table", Representation: "encoded", Format: "filegdb"},
		},
		AdmitFileGDB: func(ctx context.Context, id string, cfg commonClient.TransferExecutionConfig) error {
			request := transfer.request
			if request == nil || request.ExportSession.ExecutionID != id || cfg.Target.Name != "roads.gdb.zip" {
				t.Fatal("admission did not receive the frozen pending export")
			}
			digest, err := commonClient.ExportRequestDigest(request)
			if err != nil {
				t.Fatal(err)
			}
			provenance := commonClient.ExportExecutionSourceRequest{ExecutionID: id, RequestDigest: digest}
			session, err := store.GetExecutionSource(ctx, request.ExportSession.SessionID, 7, provenance)
			if err != nil || session == nil || session.UserID != 9 {
				t.Fatalf("pending owner session is not observable: %v", err)
			}
			return denied
		},
	})
	if !errors.Is(err, denied) {
		t.Fatalf("source denial was lost: %v", err)
	}
	session, err := store.Get(t.Context(), transfer.request.ExportSession.SessionID, 7, 9)
	if err != nil || session == nil || session.Status != StatusFailed {
		t.Fatalf("denied export session did not close: %v", err)
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
	session := &Session{TenantID: 7, UserID: 9, SourceRef: "source", Format: "csv", FileName: "data.csv", TargetParentLocator: "addp-infra://object/manager/tenant_7/export/develop/session?type=prefix", TargetLocator: "addp-infra://object/manager/tenant_7/export/develop/session/data.csv?type=object", TransferExecutionID: "transfer-1", Status: StatusPending}
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
	if session.Status != StatusFailed || session.TransferExecutionID == "" || session.TargetParentLocator == "" {
		t.Fatalf("failed session = %#v", session)
	}
}

func TestManifestKeepsMultiFileExportAsZip(t *testing.T) {
	session := &Session{Format: string(format.FormatShapefile), FileName: "roads.zip", TargetLocator: "addp-infra://object/manager/tenant_7/export/manager/session/roads.shp?type=object"}
	manifestJSON := buildManifest(session, map[string]interface{}{
		"target_refs": []interface{}{
			map[string]interface{}{"path": "manager/tenant_7/export/manager/session/roads.shp", "role": "main", "required": true, "primary": true, "extension": ".shp"},
			map[string]interface{}{"path": "manager/tenant_7/export/manager/session/roads.shx", "role": "shx", "required": true, "extension": ".shx"},
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
	got, err := InfraObjectPath("addp-infra://object/manager/tenant_7/export/develop/session/data.csv?type=object", "manager")
	if err != nil || got != "tenant_7/export/develop/session/data.csv" {
		t.Fatalf("InfraObjectPath() = %q, %v", got, err)
	}
	if _, err := InfraObjectPath("addp-infra://object/other/tenant_7/export/data.csv?type=object", "manager"); err == nil {
		t.Fatal("InfraObjectPath() accepted a different bucket")
	}
}

func TestFileGDBManifestRequiresExactPublishedArchive(t *testing.T) {
	const archive = "manager/tenant_7/export/manager/session/roads.gdb.zip"
	session := &Session{Format: string(format.FormatFileGDB), FileName: "roads.gdb.zip", TargetLocator: "addp-infra://object/" + archive + "?type=object"}
	ref := func(name string, primary bool) map[string]interface{} {
		return map[string]interface{}{"path": name, "role": "main", "required": true, "primary": primary}
	}
	for name, refs := range map[string][]map[string]interface{}{
		"missing":      nil,
		"wrong bucket": {ref("other/tenant_7/export/manager/session/roads.gdb.zip", true)},
		"directory":    {ref(strings.TrimSuffix(archive, ".zip"), true)},
		"nonprimary":   {ref(archive, false)},
		"extra":        {ref(archive, true), ref(archive+".tmp", false)},
	} {
		t.Run(name, func(t *testing.T) {
			if manifest := buildManifest(session, map[string]interface{}{"target_refs": refs}); len(manifest) != 0 {
				t.Fatalf("accepted invalid FileGDB receipt: %v", manifest)
			}
		})
	}
	manifest := manifestFromJSON(buildManifest(session, map[string]interface{}{"target_refs": []interface{}{ref(archive, true)}}))
	if manifest.Layout != format.LayoutSingle || manifest.Download.Kind != "stream" || manifest.Download.FileName != session.FileName ||
		manifest.PrimaryRef != strings.TrimPrefix(archive, "manager/") {
		t.Fatalf("FileGDB export must stream the published archive: %#v", manifest)
	}
}

func TestFileGDBManifestKeepsDecodedObjectKey(t *testing.T) {
	key := "tenant_7/export/manager/session/汉字 +%.gdb.zip"
	session := &Session{Format: string(format.FormatFileGDB), FileName: "汉字 +%.gdb.zip", TargetLocator: objectstore.ObjectURI(objectstore.Manager, key)}
	manifest := manifestFromJSON(buildManifest(session, map[string]interface{}{"target_refs": []map[string]interface{}{{"path": "manager/" + key, "required": true, "primary": true, "role": "main"}}}))
	if manifest.PrimaryRef != key {
		t.Fatalf("lost decoded key %+v", manifest)
	}
}
