package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	commonclient "github.com/addp/common/client"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

func TestStandardPermissionFailureMapsToUnavailableAcrossModelServices(t *testing.T) {
	standardServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"denied","error_code":"permission_denied"}`))
	}))
	defer standardServer.Close()

	standardClient := commonclient.NewStandardClient(
		standardServer.URL,
		commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "token", nil }),
		nil,
	)
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	tableRepo := repository.NewLogicalTableRepository(db)
	layerRepo := repository.NewDWLayerRepository(db)
	metricRepo := repository.NewFactMetricRepository(db)

	entityService := NewEntityService(entityRepo, relationRepo)
	entityService.SetStandardClient(standardClient)
	_, err := entityService.CreateEntity(&models.CreateEntityRequest{
		DomainID: int64Pointer(9), Name: "Customer", Code: "customer",
	}, 1, 1)
	requireUnavailableDomainError(t, err)

	layer := models.DWLayer{TenantID: 1, LayerCode: "dwd", LayerName: "DWD"}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatalf("create layer: %v", err)
	}
	table := models.LogicalTable{
		TenantID: 1, Name: "Order", Code: "order_fact", TableType: "fact", Layer: "dwd",
		Status: "draft", GrainDescription: "one order", CreatedBy: 1,
	}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	logicalTableService := NewLogicalTableService(tableRepo, entityRepo, layerRepo)
	logicalTableService.SetStandardClient(standardClient)
	_, err = logicalTableService.CreateField(table.ID, 1, &models.CreateLogicalFieldRequest{
		ElementID: int64Pointer(9), Name: "Amount", ColumnName: "amount", DataType: "decimal",
	})
	requireUnavailableDomainError(t, err)

	factMetricService := NewFactMetricService(metricRepo, tableRepo)
	factMetricService.SetStandardClient(standardClient)
	_, err = factMetricService.AddMetric(table.ID, 1, 1, &models.CreateFactMetricMappingRequest{MetricID: 9})
	requireUnavailableDomainError(t, err)
}

func int64Pointer(value int64) *int64 { return &value }

func requireUnavailableDomainError(t *testing.T, err error) {
	t.Helper()
	domainErr, ok := apperrors.As(err)
	if !ok || domainErr.Kind != apperrors.KindUnavailable || domainErr.Code != "standard_service_unavailable" {
		t.Fatalf("error = %#v, want standard_service_unavailable", err)
	}
}
