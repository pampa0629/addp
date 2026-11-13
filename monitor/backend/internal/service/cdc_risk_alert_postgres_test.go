package service

import (
	"context"
	"fmt"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

type cdcRiskReadResolver struct{ owners []string }

func (r *cdcRiskReadResolver) Resolve(_ context.Context, owners []string, tenant int, principal int64, _ string) ([]commonExecution.ReadScope, error) {
	r.owners = owners
	if len(owners) != 1 || owners[0] != "transfer" {
		return nil, fmt.Errorf("unexpected risk Owner modules: %v", owners)
	}
	return []commonExecution.ReadScope{{TenantID: tenant, PrincipalID: principal, Module: "transfer", Grants: []commonExecution.ReadGrant{{TaskType: "sync", TaskHistory: true}}}}, nil
}

func TestIntegrationPostgresCDCRiskUnknownClearAndGeneration(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-monitor-postgres")
	}
	db, err := gorm.Open(postgres.Open(webhookIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = EnsureMonitorStore(db); err != nil {
		t.Fatal(err)
	}
	tenant := int(time.Now().UnixNano()%100000000) + 600000000
	t.Cleanup(func() {
		for _, model := range []interface{}{&monitorModels.WebhookDelivery{}, &monitorModels.EmailDelivery{}, &monitorModels.AlertEvent{}, &monitorModels.AlertIncident{}, &monitorModels.WebhookDestination{}} {
			if err := db.Where("tenant_id = ?", tenant).Delete(model).Error; err != nil {
				t.Error("CDC alert cleanup failed", err)
			}
		}
	})
	now := time.Now().UTC()
	at := now.Add(-time.Second)
	expiry := now.Add(time.Minute)
	risk := commonClient.CDCRiskObservation{State: "active", Reason: "wal_lost", SampledAt: &at, ExpiresAt: &expiry}
	row := commonClient.TransferCDCRiskItem{TenantID: uint(tenant), TaskID: 7, Generation: 1, ExecutionID: "bb706a62-4256-453e-b471-0dbdbb305c53", State: "observing", Source: risk, Buffer: commonClient.CDCRiskObservation{State: "unknown"}}
	webhooks := NewWebhookService(db, []byte("addp-dev-encryption-key-2025!!!!"), true, "http://localhost:5170", nil)
	_, err = webhooks.CreateDestination(context.Background(), CreateWebhookDestinationInput{TenantID: tenant, Name: "cdc-risk-test", URL: "http://127.0.0.1:18080/hook", Secret: "0123456789abcdef", Enabled: true, EventTypes: []string{monitorModels.AlertEventOpened, monitorModels.AlertEventResolved}})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAlertService(db, NewNotificationService(webhooks, nil))
	reader := &riskPageReader{}
	svc.SetCDCRiskReader(reader)
	apply := func() {
		t.Helper()
		reader.pages = []*commonClient.TransferCDCRisks{{Items: []commonClient.TransferCDCRiskItem{row}}}
		signals, preserve, err := svc.evaluateCDCRisks(context.Background(), now)
		if err != nil {
			t.Fatal(err)
		}
		if err = svc.reconcileSignals(context.Background(), signals, preserve, now); err != nil {
			t.Fatal(err)
		}
	}
	apply()
	apply()
	var incident monitorModels.AlertIncident
	if err = db.Where("tenant_id = ?", tenant).First(&incident).Error; err != nil {
		t.Fatal(err)
	}
	if incident.Status != "open" {
		t.Fatal("confirmed loss did not open")
	}
	assertRuleOutboxCounts(t, db, incident.ID, 1, 1, 0)
	query := NewExecutionQueryService(commonExecution.NewTaskExecutionRepository(db))
	resolver := &cdcRiskReadResolver{}
	query.SetReadResolver(resolver)
	authorized, err := svc.AuthorizeReadRequest(context.Background(), query, tenant, 9, "Bearer safe-fixture", "")
	if err != nil || len(resolver.owners) != 1 {
		t.Fatalf("risk Owner was not consulted independently: %v", err)
	}
	allowed, err := svc.List(authorized, ListAlertsRequest{TenantID: tenant})
	if err != nil || allowed.Total != 1 {
		t.Fatalf("risk not visible under current Owner grant: %#v %v", allowed, err)
	}
	// There is deliberately no corresponding common execution row.
	ctx := commonExecution.WithReadScopes(context.Background(), []commonExecution.ReadScope{{TenantID: tenant, PrincipalID: 9, Module: "transfer", Grants: []commonExecution.ReadGrant{{TaskType: "sync", TaskHistory: true, OwnAdHoc: true}}}})
	visible, err := svc.List(ctx, ListAlertsRequest{TenantID: tenant})
	if err != nil || visible.Total != 1 || visible.Data[0].Details["reason"] != "wal_lost" {
		t.Fatalf("task risk invisible without execution: %#v %v", visible, err)
	}
	for _, scope := range []commonExecution.ReadScope{
		{TenantID: tenant + 1, PrincipalID: 9, Module: "transfer", Grants: []commonExecution.ReadGrant{{TaskType: "sync", TaskHistory: true}}},
		{TenantID: tenant, PrincipalID: 9, Module: "transfer", Grants: []commonExecution.ReadGrant{{TaskType: "sync", OwnAdHoc: true}}},
		{TenantID: tenant, PrincipalID: 9, Module: "develop", Grants: []commonExecution.ReadGrant{{TaskType: "sync", TaskHistory: true}}},
	} {
		denied := commonExecution.WithReadScopes(context.Background(), []commonExecution.ReadScope{scope})
		page, err := svc.List(denied, ListAlertsRequest{TenantID: tenant})
		if err != nil || page.Total != 0 {
			t.Fatalf("risk read escaped Owner grant: %#v %v", page, err)
		}
	}
	if err = db.Model(&incident).Update("status", "acknowledged").Error; err != nil {
		t.Fatal(err)
	}
	observed := incident.LastObservedAt
	row.Source.State = "unknown"
	now = now.Add(time.Second)
	apply()
	if err = db.First(&incident, incident.ID).Error; err != nil {
		t.Fatal(err)
	}
	if incident.Status != "acknowledged" || !incident.LastObservedAt.Equal(observed) {
		t.Fatal("unknown changed incident or observation clock")
	}
	assertRuleOutboxCounts(t, db, incident.ID, 1, 1, 0)
	row.Source = risk
	row.Source.State = "clear"
	apply()
	if err = db.First(&incident, incident.ID).Error; err != nil || incident.Status != "resolved" {
		t.Fatal("fresh clear did not resolve", err)
	}
	assertRuleOutboxCounts(t, db, incident.ID, 2, 2, 0)
	row.Source = risk
	row.Generation = 2
	apply()
	var count int64
	if err = db.Model(&monitorModels.AlertIncident{}).Where("tenant_id = ?", tenant).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("new generation reused an incident", err)
	}
	row.State = "closed"
	apply()
	if err = db.Model(&monitorModels.AlertIncident{}).Where("tenant_id = ? AND status IN ?", tenant, []string{"open", "acknowledged"}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("Owner close left active risk", err)
	}
	if err = db.Model(&monitorModels.AlertEvent{}).Where("tenant_id = ?", tenant).Count(&count).Error; err != nil || count != 4 {
		t.Fatalf("expected one open and one resolved event per generation, got %d: %v", count, err)
	}
	// The current-generation capture warning shares lifecycle, acknowledgement and outbox.
	row.State = "observing"
	row.Generation = 3
	row.Source.State = "clear"
	since := now.Add(-60 * time.Second)
	sample := now
	row.Capture = commonClient.CDCCaptureObservation{State: "failed", Reason: "task_not_running", FailedSince: &since, SampledAt: &sample, ExpiresAt: &expiry}
	apply()
	apply()
	var warning monitorModels.AlertIncident
	if err = db.Where("tenant_id = ? AND signal_code = ?", tenant, "cdc_capture_failed").First(&warning).Error; err != nil || warning.Status != "open" || warning.Severity != "warning" {
		t.Fatal("capture warning not opened", err)
	}
	assertRuleOutboxCounts(t, db, warning.ID, 1, 1, 0)
	visible, e := svc.List(authorized, ListAlertsRequest{TenantID: tenant, Severity: "warning"})
	if e != nil || visible.Total != 1 || visible.Data[0].Details["reason"] != "task_not_running" || visible.Data[0].Details["trigger_seconds"] != 60 {
		t.Fatal("capture warning lost safe public evidence", e)
	}

	if err = db.Model(&warning).Update("status", "acknowledged").Error; err != nil {
		t.Fatal(err)
	}
	last := warning.LastObservedAt
	row.Capture = commonClient.CDCCaptureObservation{State: "unknown"}
	now = now.Add(time.Second)
	apply()
	if err = db.First(&warning, warning.ID).Error; err != nil || warning.Status != "acknowledged" || !warning.LastObservedAt.Equal(last) {
		t.Fatal("capture unknown changed acknowledged warning", err)
	}
	assertRuleOutboxCounts(t, db, warning.ID, 1, 1, 0)
	sample = now
	row.Capture = commonClient.CDCCaptureObservation{State: "healthy", SampledAt: &sample, ExpiresAt: &expiry}
	apply()
	if err = db.First(&warning, warning.ID).Error; err != nil || warning.Status != "resolved" {
		t.Fatal("capture healthy did not resolve warning", err)
	}
	assertRuleOutboxCounts(t, db, warning.ID, 2, 2, 0)

}
