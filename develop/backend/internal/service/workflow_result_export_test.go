package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/exportartifact"
	objectminio "github.com/addp/common/infra/objectstore/minio"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWorkflowExportUsesCurrentOwnerCompleteSourcesAndOriginalDeadline(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	if err := exportartifact.EnsureStore(db, "export_sessions"); err != nil {
		t.Fatal(err)
	}
	repo := execution.NewTaskExecutionRepository(db)
	actor, member, version, by := int64(9), int64(1), int64(1), 9
	item := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: "develop", TaskType: "workflow", Source: "orchestrator", Status: "success", Attempt: 1, TriggerType: "manual", TriggeredBy: &by, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, Metadata: map[string]interface{}{"result": map[string]interface{}{"final_result": map[string]interface{}{"runtime_type": "dataframe", "row_count": 2}}}}
	if err := repo.Create(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	paths := []plugin.EngineCatalogPath{plugin.FileItemPath(12, "one.gpkg"), plugin.FileItemPath(12, "two.gpkg")}
	checks := 0
	denied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			w.Header().Set("ETag", `"x"`)
			io.Copy(io.Discard, r.Body)
			return
		}
		checks++
		var req struct {
			Targets []plugin.EngineCatalogPath `json:"targets"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Targets) != 2 || r.Header.Get("Authorization") != "Bearer addp_at_current" {
			t.Error("wrong current source check")
		}
		if denied {
			w.WriteHeader(403)
			return
		}
		io.WriteString(w, `{"observed_at":"2026-11-14T01:00:00Z"}`)
	}))
	defer server.Close()
	m, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("test", "test-secret", ""), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	transfer := &queryExportTransferClientStub{}
	e := &DevExecutor{taskExecutionRepo: repo, workflowResults: newWorkflowResultCache(), workflowEngine: &WorkflowEngineService{systemService: client.NewSystemServiceClient(server.URL, nil, server.Client()), protectionGate: &workflowResultGate{}}}
	lease := execution.Lease{ExecutionID: item.ExecutionID, Attempt: 1, Token: "generation"}
	provider := fileInputsProvider(fileInputsData(t))
	table := &plugin.WorkflowTableResult{SchemaVersion: "addp.workflow.table-result/v1", Columns: []string{"surface"}, Rows: []map[string]interface{}{{"surface": 42.375}, {"surface": 9.125}}, RowCount: 2}
	if err := e.workflowResults.put(item, lease, time.Now().Add(time.Minute), []workflowResultSource{{Provider: provider, Target: paths[0]}, {Provider: provider, Target: paths[1]}}, table); err != nil {
		t.Fatal(err)
	}
	defer e.workflowResults.remove(item.ExecutionID, lease.Token)
	service := NewQueryExportService(e, exportartifact.NewService(transfer, exportartifact.NewGormStore(db, "export_sessions"), objectminio.New(m, "manager"), "manager", "develop", "/exports"))
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read", "develop.data_read.execute", "develop.task.execute"})
	ctx := WithExportUserToken(WithExecutionAuthContext(t.Context(), facts), "addp_at_current")
	created, err := service.Create(ctx, item.ExecutionID, models.CreateQueryExportRequest{FileName: "areas"}, 7, 9)
	if err != nil {
		t.Fatal(err)
	}
	binding := transfer.request.Config.WorkflowResult
	if binding == nil || binding.ExecutionID != item.ExecutionID || binding.RowCount != 2 || len(binding.Targets) != 2 || binding.ExpiresAt != e.workflowResults.get(item.ExecutionID, 1, 7, 9).expires || checks != 1 {
		t.Fatal("lost full result or deadline")
	}
	raw, _ := json.Marshal(transfer.request)
	if strings.Contains(string(raw), "42.375") {
		t.Fatal("row values leaked into durable Transfer config")
	}
	other := authtest.NewTenantUserAuthContext("7", "10", []string{"develop.task.read", "develop.data_read.execute", "develop.task.execute"})
	if _, err := service.Create(WithExecutionAuthContext(t.Context(), other), item.ExecutionID, models.CreateQueryExportRequest{}, 7, 10); err == nil {
		t.Fatal("accepted another user")
	}
	denied = true
	if _, err := service.Get(ctx, created.ID, 7, 9); err == nil {
		t.Fatal("delivered after source grant refusal")
	}
	denied = false
	e.workflowResults.remove(item.ExecutionID, lease.Token)
	if _, err := service.Get(ctx, created.ID, 7, 9); err == nil {
		t.Fatal("delivered after original result expired")
	}
	reader := workflowExportDeadlineReader{ReadCloser: io.NopCloser(strings.NewReader("private")), expires: time.Now().Add(-time.Second)}
	if _, err := reader.Read(make([]byte, 8)); err == nil {
		t.Fatal("stream crossed original deadline")
	}
}
