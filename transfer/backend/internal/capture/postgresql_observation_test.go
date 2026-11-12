package capture

import (
	"context"
	"database/sql"
	"github.com/addp/transfer/internal/models"
	"testing"
	"time"
)

func TestPostgresSlotObservationIdentityRiskAndExactBytes(t *testing.T) {
	now := time.Now().UTC()
	resource := &models.CaptureResource{SourceDatabase: "business"}
	sample := postgresSlotSample{SlotType: sql.NullString{String: "logical", Valid: true}, Plugin: sql.NullString{String: "pgoutput", Valid: true}, Database: sql.NullString{String: "business", Valid: true}, PublicationAllTables: sql.NullBool{Valid: true}, PublicationTables: 1, PublicationMatches: 1, Active: true, Unlimited: true, Retained: sql.NullString{String: "9007199254740993", Valid: true}}
	for _, risk := range []string{"reserved", "extended", "unreserved", "lost"} {
		sample.WALStatus = sql.NullString{String: risk, Valid: true}
		value := postgresSlotObservation(resource, sample, now, time.Minute)
		if value.Status != "available" || value.RiskState != risk || *value.RetainedWALBytes != "9007199254740993" || value.SafeWALSizeBytes != nil || !*value.WALKeepUnlimited {
			t.Fatalf("risk/sample %+v", value)
		}
	}
	sample.PublicationMatches = 0
	value := postgresSlotObservation(resource, sample, now, time.Minute)
	if value.Status != "identity_mismatch" || value.RetainedWALBytes != nil || value.SlotActive != nil {
		t.Fatal("foreign facts exposed")
	}
	sample.PublicationMatches = 1
	sample.Retained.String = "-1"
	if value = postgresSlotObservation(resource, sample, now, time.Minute); value.Status != "unavailable" || value.RetainedWALBytes != nil {
		t.Fatal("inconsistent WAL distance exposed")
	}
	sample.Retained.String = "0"
	sample.Database.String = "foreign"
	if value = postgresSlotObservation(resource, sample, now, time.Minute); value.Status != "identity_mismatch" {
		t.Fatal("foreign database matched")
	}
}
func TestPostgresTaskObservationFailureClearsOldFactsAndPublishesDelayIndependently(t *testing.T) {
	connected := true
	seconds := 0.125
	store := &fakeCaptureStore{resource: &models.CaptureResource{ID: 1, SourceType: models.CaptureSourcePostgreSQL, Status: models.CaptureStatusRunning, ResourceVersion: 1, PostgreSQLObservation: models.JSONMap{"status": "available", "retained_wal_bytes": "123"}}}
	connect := &fakeConnectControl{delay: &CaptureDelay{Status: "available", Connected: &connected, Seconds: &seconds}}
	s := &Supervisor{store: store, plans: fakePlanResolver{plan: &CapturePlan{}}, connect: connect, config: SupervisorConfig{SourceProbeTimeout: time.Second, MonitorInterval: time.Second}}
	if err := s.observePostgreSQLTask(context.Background(), store.resource); err != nil {
		t.Fatal(err)
	}
	summary := models.NewCaptureSummary(store.resource)
	if summary.PostgreSQLObservation.Status != "unavailable" || summary.PostgreSQLObservation.RetainedWALBytes != nil || summary.DelayObservation.DelaySeconds == nil || *summary.DelayObservation.DelaySeconds != seconds {
		t.Fatalf("cross-source failure %+v", summary)
	}
	connect.delay = nil
	if err := s.observePostgreSQLTask(context.Background(), store.resource); err != nil {
		t.Fatal(err)
	}
	if summary = models.NewCaptureSummary(store.resource); summary.DelayObservation.Status != "no_sample" || summary.DelayObservation.DelaySeconds != nil {
		t.Fatal("old delay reused")
	}
}
