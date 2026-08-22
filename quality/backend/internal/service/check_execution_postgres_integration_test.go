package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataquality"
	"github.com/addp/common/dbbridge"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	qualityMigration "github.com/addp/quality/internal/migration"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresQualityCheckPersistsRuleIdentity(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}

	db, err := gorm.Open(postgres.Open(qualityServiceIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatalf("run quality migrations: %v", err)
	}

	tenantID := time.Now().UnixNano()%100000000 + 940000000
	userID := int64(42)
	engineID := tenantID
	elementID := tenantID
	schemaName := fmt.Sprintf("quality_e2e_%d", tenantID)
	tableName := "quality_values"
	columnName := "value"
	ruleKey := "00000000-0000-4000-8000-000000000123"
	quotedSchema := `"` + strings.ReplaceAll(schemaName, `"`, `""`) + `"`
	var engineAccessDelayMilliseconds atomic.Int64

	if err := db.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatalf("create target schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE").Error })
	if err := db.Exec("CREATE TABLE " + quotedSchema + `."quality_values" ("value" TEXT)`).Error; err != nil {
		t.Fatalf("create target table: %v", err)
	}
	if err := db.Exec("INSERT INTO " + quotedSchema + `."quality_values" ("value") VALUES (NULL), ('ok')`).Error; err != nil {
		t.Fatalf("insert target rows: %v", err)
	}
	t.Cleanup(func() { _ = dbbridge.ClosePool(uint(engineID)) })
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Issue{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.CheckTask{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.RuleApplication{}).Error
	})

	standardServer := newQualityExecutionContractServer(t, engineID, elementID, tenantID, schemaName, tableName, columnName, ruleKey, &engineAccessDelayMilliseconds)
	defer standardServer.Close()
	tokenSource := qualityCatalogTokenSource("quality-e2e-service-token")
	standardClient := commonClient.NewStandardClient(standardServer.URL, tokenSource, standardServer.Client())
	systemClient := commonClient.NewSystemServiceClient(standardServer.URL, tokenSource, standardServer.Client())

	ruleEngine := NewRuleEngineService(standardClient, systemClient, repository.NewRuleApplicationRepository(db))
	ruleApplication, err := ruleEngine.CreateRuleApplication(context.Background(), tenantID, userID, &CreateRuleApplicationRequest{
		ElementID: elementID, EngineID: engineID, SchemaName: schemaName, TableName: tableName, ColumnName: columnName,
	})
	if err != nil {
		t.Fatalf("create rule application: %v", err)
	}
	if ruleApplication.ID == 0 {
		t.Fatal("created rule application has no ID")
	}

	checkTaskService := NewCheckTaskService(repository.NewCheckTaskRepository(db), systemClient)
	task, err := checkTaskService.Create(context.Background(), tenantID, userID, &CreateCheckTaskRequest{
		Name: "quality identity e2e", EngineID: engineID, SchemaName: schemaName, TableName: tableName,
	})
	if err != nil {
		t.Fatalf("create check task: %v", err)
	}

	executor := NewCheckExecutor(
		systemClient,
		commonClient.NewSystemExecutionAuthorizationClient(standardServer.URL, standardServer.Client()),
		repository.NewCheckTaskRepository(db),
		repository.NewIssueRepository(db),
		30*time.Second,
		1,
	)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	executor.StartWorker(workerCtx)

	executionID, err := executor.RunCheck(context.Background(), task.ID, tenantID, userID, "addp_at_quality_user")
	if err != nil {
		t.Fatalf("run quality check: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	var execution *commonExecution.TaskExecution
	for time.Now().Before(deadline) {
		execution, err = commonExecution.NewTaskExecutionRepository(db).GetByExecutionID(context.Background(), executionID, int(tenantID))
		if err != nil {
			t.Fatalf("load execution: %v", err)
		}
		if execution.IsCompleted() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if execution == nil || execution.Status != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("execution = %#v, want success", execution)
	}

	var details []RuleResult
	rawDetails, err := json.Marshal(execution.Metadata["rule_details"])
	if err != nil {
		t.Fatalf("marshal execution rule details: %v", err)
	}
	if err := json.Unmarshal(rawDetails, &details); err != nil {
		t.Fatalf("decode execution rule details: %v", err)
	}
	if len(details) != 1 || details[0].RuleKey != ruleKey || details[0].FailedCount != 1 || details[0].TotalCount != 2 || details[0].Passed {
		t.Fatalf("execution rule details = %#v, want key=%s and 1/2 failed", details, ruleKey)
	}

	var issue models.Issue
	if err := db.Where("tenant_id = ? AND rule_application_id = ? AND rule_key = ?", tenantID, ruleApplication.ID, ruleKey).First(&issue).Error; err != nil {
		t.Fatalf("load reconciled issue: %v", err)
	}
	if issue.ExecutionID != executionID || issue.LastExecutionID != executionID || issue.Status != "open" || issue.FailedCount != 1 || issue.TotalCount != 2 {
		t.Fatalf("issue = %#v, want open issue linked to execution", issue)
	}

	executor.StopWorker()
	engineAccessDelayMilliseconds.Store(100)
	timeoutExecutor := NewCheckExecutor(
		systemClient,
		commonClient.NewSystemExecutionAuthorizationClient(standardServer.URL, standardServer.Client()),
		repository.NewCheckTaskRepository(db),
		repository.NewIssueRepository(db),
		20*time.Millisecond,
		1,
	)
	timeoutWorkerCtx, cancelTimeoutWorker := context.WithCancel(context.Background())
	timeoutExecutor.StartWorker(timeoutWorkerCtx)
	defer func() {
		cancelTimeoutWorker()
		timeoutExecutor.StopWorker()
	}()
	timeoutExecutionID, err := timeoutExecutor.RunCheck(context.Background(), task.ID, tenantID, userID, "addp_at_quality_user")
	if err != nil {
		t.Fatalf("run timeout quality check: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	var timeoutExecution *commonExecution.TaskExecution
	for time.Now().Before(deadline) {
		timeoutExecution, err = commonExecution.NewTaskExecutionRepository(db).GetByExecutionID(context.Background(), timeoutExecutionID, int(tenantID))
		if err != nil {
			t.Fatalf("load timeout execution: %v", err)
		}
		if timeoutExecution.IsCompleted() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if timeoutExecution == nil || timeoutExecution.Status != commonExecution.ExecutionStatusTimeout {
		t.Fatalf("timeout execution = %#v, want timeout", timeoutExecution)
	}
	if timeoutExecution.ErrorDetails["code"] != qualityExecutionTimeout {
		t.Fatalf("timeout error details = %#v", timeoutExecution.ErrorDetails)
	}
	var unchangedIssue models.Issue
	if err := db.First(&unchangedIssue, issue.ID).Error; err != nil {
		t.Fatalf("reload issue after timeout: %v", err)
	}
	if unchangedIssue.LastExecutionID != executionID || unchangedIssue.Status != "open" {
		t.Fatalf("issue changed after timeout = %#v", unchangedIssue)
	}
}

func TestIntegrationPostgresQualityWorkerHonorsConcurrencyLimit(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}

	db, err := gorm.Open(postgres.Open(qualityServiceIntegrationDSN()), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure common execution store: %v", err)
	}
	if err := qualityMigration.NewRunner(db).Run(context.Background()); err != nil {
		t.Fatalf("run quality migrations: %v", err)
	}

	tenantID := time.Now().UnixNano()%100000000 + 950000000
	engineID := tenantID
	userID := int64(42)
	schemaName := fmt.Sprintf("quality_concurrency_%d", tenantID)
	quotedSchema := `"` + strings.ReplaceAll(schemaName, `"`, `""`) + `"`
	if err := db.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatalf("create target schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE").Error })
	t.Cleanup(func() { _ = dbbridge.ClosePool(uint(engineID)) })
	t.Cleanup(func() {
		_ = db.Where("tenant_id = ?", tenantID).Delete(&commonExecution.TaskExecution{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.Issue{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.CheckTask{}).Error
		_ = db.Where("tenant_id = ?", tenantID).Delete(&models.RuleApplication{}).Error
	})

	ruleDocument := dataquality.Document{
		SchemaVersion: dataquality.RulesSchemaVersion,
		Rules: []dataquality.Rule{{
			RuleKey: "00000000-0000-4000-8000-000000000321",
			Type:    dataquality.RuleTypeNotNull, Enabled: true, Severity: dataquality.SeverityError, Params: dataquality.Parameters{},
		}},
	}
	ruleConfig, err := json.Marshal(ruleDocument)
	if err != nil {
		t.Fatalf("marshal rule config: %v", err)
	}

	now := time.Now().UTC()
	tasks := make([]models.CheckTask, 0, 3)
	for index := 1; index <= 3; index++ {
		tableName := fmt.Sprintf("quality_values_%d", index)
		quotedTable := `"` + tableName + `"`
		if err := db.Exec("CREATE TABLE " + quotedSchema + "." + quotedTable + ` ("value" TEXT NOT NULL)`).Error; err != nil {
			t.Fatalf("create target table %s: %v", tableName, err)
		}
		if err := db.Exec("INSERT INTO " + quotedSchema + "." + quotedTable + ` ("value") VALUES ('ok')`).Error; err != nil {
			t.Fatalf("insert target row %s: %v", tableName, err)
		}
		application := models.RuleApplication{
			TenantID: tenantID, ElementID: tenantID + int64(index), EngineID: engineID,
			SchemaName: schemaName, Table: tableName, ColumnName: "value", RuleConfig: ruleConfig,
			Enabled: true, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&application).Error; err != nil {
			t.Fatalf("create rule application %s: %v", tableName, err)
		}
		task := models.CheckTask{
			TenantID: tenantID, Name: fmt.Sprintf("quality concurrency %d", index), EngineID: engineID,
			SchemaName: schemaName, Table: tableName, CreatedBy: userID, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("create check task %s: %v", tableName, err)
		}
		tasks = append(tasks, task)
	}

	tracker := &qualityExecutionConcurrencyTracker{}
	systemServer := newQualityConcurrencyServer(t, engineID, tenantID, 300*time.Millisecond, tracker)
	defer systemServer.Close()
	tokenSource := qualityCatalogTokenSource("quality-concurrency-service-token")
	executor := NewCheckExecutor(
		commonClient.NewSystemServiceClient(systemServer.URL, tokenSource, systemServer.Client()),
		commonClient.NewSystemExecutionAuthorizationClient(systemServer.URL, systemServer.Client()),
		repository.NewCheckTaskRepository(db),
		repository.NewIssueRepository(db),
		5*time.Second,
		2,
	)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	executor.StartWorker(workerCtx)
	defer func() {
		cancelWorker()
		executor.StopWorker()
	}()

	executionIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		executionID, runErr := executor.RunCheck(context.Background(), task.ID, tenantID, userID, "addp_at_quality_user")
		if runErr != nil {
			t.Fatalf("run quality check %d: %v", task.ID, runErr)
		}
		executionIDs = append(executionIDs, executionID)
	}
	for _, executionID := range executionIDs {
		execution := waitForQualityExecution(t, db, tenantID, executionID, 10*time.Second)
		if execution.Status != commonExecution.ExecutionStatusSuccess {
			t.Fatalf("execution %s status = %s, want success", executionID, execution.Status)
		}
	}
	if tracker.maximum.Load() != 2 {
		t.Fatalf("maximum concurrent engine accesses = %d, want 2", tracker.maximum.Load())
	}
}

type qualityExecutionConcurrencyTracker struct {
	current atomic.Int64
	maximum atomic.Int64
}

func (t *qualityExecutionConcurrencyTracker) enter() func() {
	current := t.current.Add(1)
	for {
		maximum := t.maximum.Load()
		if current <= maximum || t.maximum.CompareAndSwap(maximum, current) {
			break
		}
	}
	return func() { t.current.Add(-1) }
}

func newQualityConcurrencyServer(t *testing.T, engineID, tenantID int64, delay time.Duration, tracker *qualityExecutionConcurrencyTracker) *httptest.Server {
	t.Helper()
	var authorizationSequence atomic.Int64
	engine := commonModels.Engine{
		ID: uint(engineID), TenantID: uintPtr(uint(tenantID)), Name: "quality concurrency PostgreSQL", EngineType: "postgresql", LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOnline,
		ConnectionInfo: commonModels.ConnectionInfo{
			"host":     qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
			"port":     qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
			"user":     qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
			"password": qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
			"database": qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
			"sslmode":  qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/auth/execution-authorizations":
			var request commonClient.IssueExecutionAuthorizationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			authorizationID := strconv.FormatInt(800000+authorizationSequence.Add(1), 10)
			_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
				ID: authorizationID, ExecutionID: request.ExecutionID, Audience: request.Audience,
				EngineIDs: request.EngineIDs, Effects: request.Effects, ExpiresAt: time.Now().UTC().Add(time.Hour),
				ActorPrincipalID: "42", TenantID: strconv.FormatInt(tenantID, 10), TenantMembershipID: "800002",
				IssuedAuthorizationVersion: "1", SourceType: "user",
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/system/execution-authorizations/") && strings.HasSuffix(r.URL.Path, "/engine-accesses"):
			leave := tracker.enter()
			defer leave()
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
			var request commonClient.ExecutionEngineAccessRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			authorizationID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/system/execution-authorizations/"), "/engine-accesses")
			_ = json.NewEncoder(w).Encode(commonClient.ExecutionEngineAccess{
				AuthorizationID: authorizationID, ExecutionID: request.ExecutionID, Audience: "addp-quality",
				EngineID: request.EngineID, Effects: request.RequiredEffects, ExpiresAt: time.Now().UTC().Add(time.Hour), Engine: &engine,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func waitForQualityExecution(t *testing.T, db *gorm.DB, tenantID int64, executionID string, timeout time.Duration) *commonExecution.TaskExecution {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		execution, err := commonExecution.NewTaskExecutionRepository(db).GetByExecutionID(context.Background(), executionID, int(tenantID))
		if err != nil {
			t.Fatalf("load execution %s: %v", executionID, err)
		}
		if execution.IsCompleted() {
			return execution
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("execution %s did not complete within %s", executionID, timeout)
	return nil
}

func newQualityExecutionContractServer(t *testing.T, engineID, elementID, tenantID int64, schemaName, tableName, columnName, ruleKey string, engineAccessDelayMilliseconds *atomic.Int64) *httptest.Server {
	t.Helper()
	var authorizationSequence atomic.Int64
	rule := dataquality.Rule{RuleKey: ruleKey, Type: dataquality.RuleTypeNotNull, Enabled: true, Severity: dataquality.SeverityError, Params: dataquality.Parameters{}}
	document := dataquality.Document{SchemaVersion: dataquality.RulesSchemaVersion, Rules: []dataquality.Rule{rule}}
	engine := commonModels.Engine{
		ID: uint(engineID), TenantID: uintPtr(uint(tenantID)), Name: "quality e2e PostgreSQL", EngineType: "postgresql", LifecycleState: commonModels.EngineLifecycleActive, ConnectionStatus: commonModels.EngineConnectionOnline,
		ConnectionInfo: commonModels.ConnectionInfo{
			"host":     qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
			"port":     qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
			"user":     qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
			"password": qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
			"database": qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
			"sslmode":  qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			t.Fatalf("missing authorization for %s %s", r.Method, r.URL.Path)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/api/v1/system/engines/%d", engineID):
			_ = json.NewEncoder(w).Encode(engine)
		case r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/api/v1/system/engines/%d/catalog/children", engineID):
			var request commonClient.EngineCatalogListChildrenRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode catalog children request: %v", err)
			}
			switch len(request.Path.Segments) {
			case 0:
				_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{{Name: "PostgreSQL", Role: "branch", Path: commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: uint(engineID), Segments: []commonClient.EngineCatalogSegment{{Term: "server", Kind: "server"}}}}}})
			case 1:
				_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{{Name: schemaName, Role: "branch", Path: commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: uint(engineID), Segments: []commonClient.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "schema", Kind: "namespace", Name: schemaName}}}}}})
			case 2:
				_ = json.NewEncoder(w).Encode(commonClient.EngineCatalogListChildrenResponse{Nodes: []commonClient.EngineCatalogEntry{{Name: tableName, Role: "leaf", Path: commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: uint(engineID), Segments: []commonClient.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "schema", Kind: "namespace", Name: schemaName}, {Term: "table", Kind: "table", Name: tableName}}}}}})
			default:
				t.Fatalf("unexpected catalog path: %#v", request.Path)
			}
		case r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/api/v1/system/engines/%d/catalog/facts", engineID):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"table": map[string]interface{}{"fields": []map[string]interface{}{{"name": columnName, "nullable": true}}}})
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/api/v1/standard/elements/%d/quality-rules", elementID):
			_ = json.NewEncoder(w).Encode(document)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/system/auth/execution-authorizations":
			var request commonClient.IssueExecutionAuthorizationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode authorization request: %v", err)
			}
			authorizationID := strconv.FormatInt(700000+authorizationSequence.Add(1), 10)
			_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{ID: authorizationID, ExecutionID: request.ExecutionID, Audience: request.Audience, EngineIDs: request.EngineIDs, Effects: request.Effects, ExpiresAt: time.Now().UTC().Add(time.Hour), ActorPrincipalID: "42", TenantID: strconv.FormatInt(tenantID, 10), TenantMembershipID: "700002", IssuedAuthorizationVersion: "1", SourceType: "user"})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/system/execution-authorizations/") && strings.HasSuffix(r.URL.Path, "/engine-accesses"):
			if engineAccessDelayMilliseconds != nil {
				delay := time.Duration(engineAccessDelayMilliseconds.Load()) * time.Millisecond
				if delay > 0 {
					select {
					case <-time.After(delay):
					case <-r.Context().Done():
						return
					}
				}
			}
			var request commonClient.ExecutionEngineAccessRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode engine access request: %v", err)
			}
			authorizationID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/system/execution-authorizations/"), "/engine-accesses")
			_ = json.NewEncoder(w).Encode(commonClient.ExecutionEngineAccess{AuthorizationID: authorizationID, ExecutionID: request.ExecutionID, Audience: "addp-quality", EngineID: request.EngineID, Effects: request.RequiredEffects, ExpiresAt: time.Now().UTC().Add(time.Hour), Engine: &engine})
		default:
			http.NotFound(w, r)
		}
	}))
}

func uintPtr(value uint) *uint { return &value }
