package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	client "github.com/addp/common/client"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	execution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type materializationTestTokens struct{}

func (materializationTestTokens) Token(context.Context, uint) (string, error) {
	return "test-service", nil
}
func (materializationTestTokens) PlatformToken(context.Context) (string, error) {
	return "test-platform", nil
}

func TestPostgresMaterializationExecutionLifecycle(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	if err := execution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	schema := "model_task_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	// Physical DDL uses a separate connection and therefore a dedicated, cleaned schema.
	pool, _ := tx.DB()
	if _, err := pool.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec("DROP SCHEMA " + schema + " CASCADE") })
	if err := tx.Create(&models.DWLayer{TenantID: tenantID, LayerCode: "task_layer", LayerName: "Task layer", Version: 1}).Error; err != nil {
		t.Fatal(err)
	}
	table := models.LogicalTable{Layer: "task_layer", TenantID: tenantID, Name: "Task target", Code: "task_target", TableType: "entity", Status: "approved", Version: 1, CreatedBy: 1, Materialization: models.JSONB{"target_parent_locator": "addp://engine/719/path/" + schema + "?type=schema", "target_name": "task_target"}}
	if err := tx.Create(&table).Error; err != nil {
		t.Fatal(err)
	}
	field := models.LogicalField{TableID: table.ID, Name: "value", ColumnName: "value", DataType: "int", FieldRole: "regular", Nullable: true}
	if err := tx.Create(&field).Error; err != nil {
		t.Fatal(err)
	}
	dsn, _ := url.Parse(os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN"))
	port, _ := strconv.Atoi(dsn.Port())
	password, _ := dsn.User.Password()
	engine := &commonModels.Engine{ID: 719, EngineType: "postgresql", ConnectionInfo: commonModels.ConnectionInfo{"host": dsn.Hostname(), "port": port, "user": dsn.User.Username(), "password": password, "database": strings.TrimPrefix(dsn.Path, "/"), "sslmode": "disable"}}
	var childIssues, authorizationSequence int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/engine-accesses") {
			var req client.ExecutionEngineAccessRequest
			json.NewDecoder(r.Body).Decode(&req)
			json.NewEncoder(w).Encode(client.ExecutionEngineAccess{AuthorizationID: strings.Split(r.URL.Path, "/")[5], ExecutionID: req.ExecutionID, Audience: execution.AudienceModel, EngineID: req.EngineID, Effects: req.RequiredEffects, ExpiresAt: time.Now().Add(time.Hour), Engine: engine})
			return
		}
		var req struct {
			Audience          string                              `json:"audience"`
			ExecutionID       string                              `json:"execution_id"`
			ParentExecutionID string                              `json:"parent_execution_id"`
			Accesses          []client.ExecutionEngineAccessScope `json:"accesses"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.ParentExecutionID != "" {
			childIssues++
		}
		if len(req.Accesses) != 1 || req.Accesses[0].EngineID != "719" {
			t.Errorf("unexpected accesses: %+v", req.Accesses)
		}
		authorizationSequence++
		json.NewEncoder(w).Encode(client.IssuedExecutionAuthorization{ID: strconv.Itoa(authorizationSequence), ExecutionID: req.ExecutionID, Audience: req.Audience, Accesses: req.Accesses, ExpiresAt: time.Now().Add(time.Hour), ActorPrincipalID: "11", TenantMembershipID: "12", IssuedAuthorizationVersion: "1", TenantID: strconv.FormatInt(tenantID, 10)})
	}))
	defer server.Close()
	repo := repository.NewLogicalTableRepository(tx)
	svc := NewMaterializationService(client.NewSystemServiceClient(server.URL, materializationTestTokens{}, nil), repo, &LogicalTableService{})
	svc.SetExecutionAuthorizationIssuer(client.NewSystemExecutionAuthorizationClient(server.URL, nil))
	run := func(item *execution.TaskExecution) {
		t.Helper()
		var claimed *execution.TaskExecution
		var lease *execution.Lease
		err := tx.Transaction(func(db *gorm.DB) error {
			var e error
			claimed, lease, e = execution.ClaimNext(ctx, db, execution.ClaimOptions{Module: execution.ModuleModel, TaskType: MaterializationTaskType, WorkerID: "model-test", LeaseDuration: time.Minute, RequireAuthorization: true})
			return e
		})
		if err != nil || claimed == nil || claimed.ExecutionID != item.ExecutionID {
			t.Fatalf("claim: %+v %v", claimed, err)
		}
		svc.runMaterialization(ctx, claimed, *lease)
	}
	first, err := svc.EnqueueMaterialization(ctx, table.ID, tenantID, 1, "addp_at_test_user", "", "manual")
	if err != nil {
		t.Fatal(err)
	}
	if first.StartedAt != nil || first.Status != "pending" {
		t.Fatal("queued execution already started")
	}
	_, err = svc.EnqueueMaterialization(ctx, table.ID, tenantID, 1, "addp_at_test_user", "", "manual")
	requireDomainErrorCode(t, err, "materialization_execution_active")
	run(first)
	check := func(id, status string) *execution.TaskExecution {
		t.Helper()
		item, e := execution.NewTaskExecutionRepository(tx).GetByExecutionID(ctx, id, int(tenantID))
		if e != nil || item.Status != status || item.CompletedAt == nil || item.ExecutionTimeMs == nil {
			t.Fatalf("execution: %+v %v", item, e)
		}
		return item
	}
	completed := check(first.ExecutionID, "success")
	outputs := completed.Metadata["outputs"].(map[string]interface{})
	if outputs["target_locator"] != "addp://engine/719/path/"+schema+"/task_target?type=table" {
		t.Fatal(outputs)
	}
	if _, err = pool.Exec("INSERT INTO " + schema + ".task_target VALUES (42)"); err != nil {
		t.Fatal(err)
	}
	second, err := svc.EnqueueMaterialization(ctx, table.ID, tenantID, 1, "addp_at_test_user", "", "manual")
	if err != nil {
		t.Fatal(err)
	}
	run(second)
	check(second.ExecutionID, "success")
	var count int
	if err = pool.QueryRow("SELECT count(*) FROM " + schema + ".task_target").Scan(&count); err != nil || count != 1 {
		t.Fatalf("preserved count %d: %v", count, err)
	}
	stale, err := svc.EnqueueMaterialization(ctx, table.ID, tenantID, 1, "addp_at_test_user", "", "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Model(&table).Update("version", 2).Error; err != nil {
		t.Fatal(err)
	}
	run(stale)
	failed := check(stale.ExecutionID, "failed")
	if failed.ErrorDetails["code"] != "resource_version_conflict" {
		t.Fatal(failed.ErrorDetails)
	}
	_, err = svc.EnqueueMaterialization(ctx, table.ID, tenantID, 2, "", uuid.NewString(), "manual")
	requireDomainErrorCode(t, err, "materialization_parent_invalid")
	principal, membership, version := int64(11), int64(12), int64(1)
	started := time.Now()
	parent := execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenantID), Module: "orchestrator", TaskType: "orchestration", Source: "orchestrator", Status: "running", TriggerType: "manual", ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, StartedAt: &started}
	if err = tx.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	child, err := svc.EnqueueMaterialization(ctx, table.ID, tenantID, 2, "", parent.ExecutionID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentExecutionID == nil || *child.ParentExecutionID != parent.ExecutionID || child.ActorPrincipalID == nil || childIssues != 1 {
		t.Fatalf("child lineage: %+v issues:%d", child, childIssues)
	}
	// An expired lease is terminal; it is never silently returned to the queue.
	expired := time.Now().Add(-time.Minute)
	token := uuid.NewString()
	owner := "expired-test"
	if err = tx.Model(child).Updates(map[string]interface{}{"status": "running", "started_at": expired, "lease_owner": owner, "lease_token": token, "lease_expires_at": expired, "attempt": 1}).Error; err != nil {
		t.Fatal(err)
	}
	svc.recoverMaterializationExecutions(ctx)
	var status string
	tx.Model(child).Select("status").Scan(&status)
	if status != "failed" {
		t.Fatal(fmt.Sprintf("expired status %s", status))
	}
}
