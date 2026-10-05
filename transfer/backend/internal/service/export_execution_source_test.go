package service

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/format"
	"github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

type exportOwnerRegistry struct{ url string }

func (r exportOwnerRegistry) ListActiveModules(context.Context) ([]*commonClient.ModuleInfo, error) {
	return []*commonClient.ModuleInfo{{ModuleName: "manager", Enabled: true, Instances: []commonClient.ModuleRuntimeInstanceInfo{{Role: commonClient.ModuleRuntimeRoleBackend, Status: "up", ModuleURL: r.url, LeaseExpiresAt: time.Now().Add(time.Minute)}}}}, nil
}

type exportDispatch struct {
	service *TaskService
	request *commonClient.CreateTransferExecutionRequest
	tamper  bool
}

func (d *exportDispatch) CreateExecution(ctx context.Context, request *commonClient.CreateTransferExecutionRequest) (*commonClient.CreateTransferExecutionResponse, error) {
	d.request = request
	if d.tamper {
		request.Config.Target.Name = "tampered.csv"
	}
	result, err := d.service.CreateAdHocExecution(ctx, request, "manager", request.TenantID)
	if err != nil {
		return nil, err
	}
	return &commonClient.CreateTransferExecutionResponse{ExecutionID: result.ExecutionID, Status: result.Status}, nil
}
func (*exportDispatch) GetExecution(string, uint) (*commonClient.TransferExecutionResponse, error) {
	return &commonClient.TransferExecutionResponse{Status: "pending"}, nil
}

func TestExportExecutionUsesOwnerVerifiedInitiatorAndOwnAdHocIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTransferTaskServiceTestDB(t)
	repo := commonExecution.NewTaskExecutionRepository(db)
	taskService := NewTaskService(db, nil, nil)
	taskService.SetExecutionService(NewExecutionService(db, repo))
	if err := exportartifact.EnsureStore(db, "manager_export_sessions"); err != nil {
		t.Fatal(err)
	}
	store := exportartifact.NewGormStore(db, "manager_export_sessions")
	dispatch := &exportDispatch{service: taskService}
	minioClient, err := minio.New("127.0.0.1:1", &minio.Options{})
	if err != nil {
		t.Fatal(err)
	}
	owner := exportartifact.NewService(dispatch, store, minioClient, "manager", "manager", "/exports")
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{"Bearer transfer-service": {ClientID: "addp-transfer", Permissions: []string{"manager.export_provenance.read"}}})
	defer authServer.Close()
	router := gin.New()
	router.Use(auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: authServer.URL}))
	group := router.Group("/api/v1/manager/runtime/export-sessions", exportartifact.ExecutionSourceGuards("manager.export_provenance.read")...)
	group.POST("/:id/execution-source", func(c *gin.Context) { exportartifact.ServeExecutionSource(c, owner) })
	server := httptest.NewServer(router)
	defer server.Close()
	tokens := commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "transfer-service", nil })
	taskService.SetExportExecutionSourceResolver(commonClient.NewExportExecutionSourceClient(exportOwnerRegistry{server.URL}, tokens))
	result, err := owner.Create(t.Context(), exportartifact.CreateRequest{TenantID: 7, UserID: 9, SourceRef: "orders", Format: format.FormatCSV, FileName: "orders", ExecutionConfig: commonClient.TransferExecutionConfig{
		Runtime: commonClient.TransferExecutionRuntime{Boundary: "bounded"}, Load: commonClient.TransferExecutionLoad{Mode: "snapshot"},
		Source: commonClient.TransferExecutionEndpoint{Locator: "addp://engine/1/path/public/orders?type=table", DataType: "table", Representation: "native"},
		Target: commonClient.TransferExecutionEndpoint{DataType: "table", Representation: "encoded", Format: "csv", Policy: map[string]interface{}{"apply_mode": "replace"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	row, err := repo.GetByExecutionID(t.Context(), result.TransferExecutionID, 7)
	if err != nil || row.TriggeredBy == nil || *row.TriggeredBy != 9 || row.Source != "manager" || row.SourceTaskID != nil {
		t.Fatalf("lost verified provenance: row=%#v err=%v", row, err)
	}
	if row.ActorPrincipalID != nil || row.ActorTenantMembershipID != nil || row.IssuedAuthorizationVersion != nil {
		t.Fatal("observation provenance became execution authority")
	}
	runtimeTask, err := (&ExecutionEngineService{}).runtimeTaskForExecution(row)
	if err != nil || runtimeTask.CreatedBy != nil {
		t.Fatal("worker promoted observation provenance into execution authority")
	}
	for _, scope := range []struct {
		tenant, user int
		visible      bool
	}{{7, 9, true}, {7, 10, false}, {8, 9, false}} {
		ctx := commonExecution.WithReadScopes(t.Context(), []commonExecution.ReadScope{{Module: "transfer", TenantID: scope.tenant, PrincipalID: int64(scope.user), Grants: []commonExecution.ReadGrant{{TaskType: "sync", OwnAdHoc: true}}}})
		_, err := repo.GetByExecutionID(ctx, row.ExecutionID, scope.tenant)
		if (err == nil) != scope.visible {
			t.Fatalf("wrong visibility tenant=%d user=%d", scope.tenant, scope.user)
		}
		_, total, err := repo.List(ctx, commonExecution.TaskExecutionFilter{TenantID: scope.tenant, Page: 1, PageSize: 20})
		want := int64(0)
		if scope.visible {
			want = 1
		}
		if err != nil || total != want {
			t.Fatalf("wrong scoped count=%d err=%v", total, err)
		}
	}
	if _, err := taskService.CreateAdHocExecution(t.Context(), dispatch.request, "manager", 7); err == nil {
		t.Fatal("created duplicate execution UUID")
	}
	var count int64
	if err := db.Model(&commonExecution.TaskExecution{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("duplicate execution persisted")
	}
	dispatch.request.Config.Target.Name = "tampered.csv"
	if _, err := taskService.CreateAdHocExecution(t.Context(), dispatch.request, "manager", 7); err != commonAPI.ErrForbidden {
		t.Fatalf("tampered request err=%v", err)
	}
}
