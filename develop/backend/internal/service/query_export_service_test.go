package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/exportartifact"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type queryExportTransferClientStub struct {
	request *commonClient.CreateTransferExecutionRequest
}

func (s *queryExportTransferClientStub) CreateExecution(_ context.Context, request *commonClient.CreateTransferExecutionRequest) (*commonClient.CreateTransferExecutionResponse, error) {
	s.request = request
	return &commonClient.CreateTransferExecutionResponse{ExecutionID: "transfer-export-1", Status: "pending"}, nil
}

func (s *queryExportTransferClientStub) GetExecution(_ string, _ uint) (*commonClient.TransferExecutionResponse, error) {
	return &commonClient.TransferExecutionResponse{Status: "pending"}, nil
}

func newQueryExportServiceForTest(t *testing.T, db *gorm.DB, repository *commonExecution.TaskExecutionRepository, transfer *queryExportTransferClientStub) *QueryExportService {
	t.Helper()
	if err := exportartifact.EnsureStore(db, "export_sessions"); err != nil {
		t.Fatalf("ensure export session store: %v", err)
	}
	minioClient, err := minio.New("127.0.0.1:1", &minio.Options{
		Creds: credentials.NewStaticV4("test", "test-secret", ""),
	})
	if err != nil {
		t.Fatalf("create minio client: %v", err)
	}
	artifacts := exportartifact.NewService(
		transfer,
		exportartifact.NewGormStore(db, "export_sessions"),
		minioClient,
		"manager",
		"develop",
		"/api/v1/develop/exports",
	)
	return NewQueryExportService(NewDevExecutor(nil, repository, nil, nil, nil, nil, nil, nil, 500), artifacts)
}

func TestQueryExportUsesFrozenSuccessfulExecutionSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	repository := commonExecution.NewTaskExecutionRepository(db)
	sourceTaskID := "52"
	execution := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "develop-query-1", Module: commonExecution.ModuleDevelop,
		TaskType: commonExecution.TaskTypeQuery, Source: commonExecution.ModuleDevelop,
		SourceTaskID: &sourceTaskID, Status: commonExecution.ExecutionStatusSuccess,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggerType:       commonExecution.TriggerTypeManual,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id": float64(11),
			"content": map[string]interface{}{
				"query":          "SELECT id, total FROM public.orders WHERE status = :status",
				"query_type":     "sql",
				"target_locator": "addp://engine/11/path/public/orders?type=table",
			},
			"inputs": map[string]interface{}{
				"effective_parameters": map[string]interface{}{"status": "paid"},
				"effective_inputs": map[string]interface{}{
					"persons": map[string]interface{}{
						"locator": "addp://engine/11/path/public/persons?type=table&item_id=23",
					},
					"activities": map[string]interface{}{
						"locator": "addp://engine/11/path/public/activities?type=table&item_id=22",
					},
				},
			},
		},
		Metadata: commonModels.JSONMap{
			"result": map[string]interface{}{
				"result_kind": "table", "columns": []string{"id", "total"},
			},
		},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := repository.Create(context.Background(), execution); err != nil {
		t.Fatalf("create query execution: %v", err)
	}
	transfer := &queryExportTransferClientStub{}
	exporter := newQueryExportServiceForTest(t, db, repository, transfer)

	result, err := exporter.Create(context.Background(), execution.ExecutionID, models.CreateQueryExportRequest{
		Format: "csv", FileName: "orders",
	}, 7, 9)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.TransferExecutionID != "transfer-export-1" || result.Status != "pending" {
		t.Fatalf("result = %#v", result)
	}
	request := transfer.request
	if request == nil || request.TenantID != 7 {
		t.Fatalf("transfer request = %#v", request)
	}
	if request.Config.Source.Query == nil || request.Config.Source.Query.Statement != "SELECT id, total FROM public.orders WHERE status = :status" {
		t.Fatalf("query source = %#v", request.Config.Source)
	}
	if request.Config.Source.Query.Parameters["status"] != "paid" {
		t.Fatalf("query parameters = %#v", request.Config.Source.Query.Parameters)
	}
	queryInputs := request.Config.Source.Query.Inputs
	if len(queryInputs) != 2 || queryInputs[0].Name != "activities" || queryInputs[0].Locator != "addp://engine/11/path/public/activities?type=table&item_id=22" ||
		queryInputs[1].Name != "persons" || queryInputs[1].Locator != "addp://engine/11/path/public/persons?type=table&item_id=23" {
		t.Fatalf("query inputs = %#v", queryInputs)
	}
	if !strings.HasPrefix(request.Config.Target.ParentLocator, "addp-infra://minio/manager/tenant_7/export/develop/") || request.Config.Target.Name != "orders.csv" {
		t.Fatalf("target = %#v", request.Config.Target)
	}
	if request.AutoScanMetadata {
		t.Fatal("one-off export must not scan metadata")
	}
	if len(request.Config.Transforms) != 1 || request.Config.Transforms[0].Type != "field_mapping" || request.Config.Transforms[0].Version != "v1" || request.Config.Transforms[0].Mode != "project" {
		t.Fatalf("query export field projection = %#v", request.Config.Transforms)
	}
	fields := request.Config.Transforms[0].Fields
	if len(fields) != 2 || fields[0].Source != "id" || fields[0].Target != "id" || fields[0].TargetType != "unknown" || !fields[0].Nullable ||
		fields[1].Source != "total" || fields[1].Target != "total" || fields[1].TargetType != "unknown" || !fields[1].Nullable {
		t.Fatalf("query export identity fields = %#v", fields)
	}
}

func TestQueryExportSourceLocatorUsesFirstSortedRelationInput(t *testing.T) {
	inputs := queryExportInputs(map[string]interface{}{
		"persons":    map[string]interface{}{"locator": "addp://engine/11/path/public/persons?type=table"},
		"activities": map[string]interface{}{"locator": "addp://engine/11/path/public/activities?type=table"},
	})

	if got := queryExportSourceLocator(map[string]interface{}{}, inputs); got != "addp://engine/11/path/public/activities?type=table" {
		t.Fatalf("queryExportSourceLocator() = %q", got)
	}
}

func TestQueryExportRejectsUnsuccessfulExecution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	repository := commonExecution.NewTaskExecutionRepository(db)
	execution := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "develop-query-failed", Module: commonExecution.ModuleDevelop,
		TaskType: commonExecution.TaskTypeQuery, Source: commonExecution.ModuleDevelop,
		Status: commonExecution.ExecutionStatusFailed, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggerType: commonExecution.TriggerTypeManual, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := repository.Create(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	exporter := newQueryExportServiceForTest(t, db, repository, &queryExportTransferClientStub{})
	if _, err := exporter.Create(context.Background(), execution.ExecutionID, models.CreateQueryExportRequest{
		Format: "csv", FileName: "orders.csv",
	}, 7, 9); err == nil || !errors.Is(err, ErrQueryExportInvalid) {
		t.Fatalf("Create() error = %v", err)
	}
}
