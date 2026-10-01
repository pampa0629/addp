package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresAlertExecutionVisibility(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMonitorStore(db); err != nil {
		t.Fatal(err)
	}
	tenant := int(time.Now().UnixNano()%100000000) + 500000000
	t.Cleanup(func() {
		for _, table := range []interface{}{&monitorModels.AlertIncident{}, &execution.TaskExecution{}} {
			if err := db.Where("tenant_id = ?", tenant).Delete(table).Error; err != nil {
				t.Errorf("cleanup tenant fixture: %v", err)
				continue
			}
			var remaining int64
			if err := db.Model(table).Where("tenant_id = ?", tenant).Count(&remaining).Error; err != nil || remaining != 0 {
				t.Errorf("fixture remains count=%d error=%v", remaining, err)
			}
		}
	})
	task, actor, other := "deleted-task", 9, 10
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for i, id := range ids {
		row := execution.TaskExecution{TenantID: tenant, ExecutionID: id, Module: "transfer", TaskType: "sync", Status: "failed", TriggerType: "manual"}
		if i == 0 {
			row.SourceTaskID = &task
		} else if i == 1 {
			row.TriggeredBy = &actor
		} else {
			row.TriggeredBy = &other
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		incident := monitorModels.AlertIncident{TenantID: tenant, ExecutionID: id, Module: "transfer", TaskType: "sync", SourceTaskID: task, SignalCode: "diagnostics_error", Fingerprint: uuid.NewString(), Status: "open", Severity: "warning", Details: models.JSONMap{"error": "password=private-sentinel", "body": "private-sentinel"}, OpenedAt: time.Now(), LastObservedAt: time.Now()}
		if err := db.Create(&incident).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := execution.WithReadScopes(context.Background(), []execution.ReadScope{{TenantID: tenant, PrincipalID: 9, Module: "transfer", Grants: []execution.ReadGrant{{TaskType: "sync", TaskHistory: true, OwnAdHoc: true}}}})
	alerts := NewAlertService(db, nil)
	page, err := alerts.List(ctx, ListAlertsRequest{TenantID: tenant})
	if err != nil || page.Total != 2 || len(page.Data) != 2 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	for _, alert := range page.Data {
		if alert.Details["error"] != "execution_failed" || alert.Details["body"] != nil {
			t.Fatalf("unsafe details=%#v", alert.Details)
		}
	}
	var hidden monitorModels.AlertIncident
	if err := db.Where("execution_id = ?", ids[2]).First(&hidden).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := alerts.Acknowledge(ctx, hidden.ID, tenant, "9", time.Now()); !errors.Is(err, ErrAlertNotActive) {
		t.Fatalf("hidden acknowledge err=%v", err)
	}
	if _, err := alerts.Suppress(ctx, hidden.ID, tenant, time.Now().Add(time.Hour)); !errors.Is(err, ErrAlertNotActive) {
		t.Fatalf("hidden suppress err=%v", err)
	}
}
