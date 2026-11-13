package capture

import (
	"context"
	"database/sql"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
	"io"
	"net"
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

// A paused database accepts TCP in the kernel but never completes PostgreSQL startup.
// QueryRowContext alone does not bound lib/pq's initial handshake.
func TestPostgresTaskObservationBoundsSilentStartupAndStillPublishesDelay(t *testing.T) {
	for _, sslMode := range []string{"disable", "require"} {
		t.Run(sslMode, func(t *testing.T) {

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			release, serverDone, handshakeReady := make(chan struct{}), make(chan struct{}), make(chan struct{})
			go func() {
				defer close(serverDone)
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				defer connection.Close()
				if _, err := io.ReadFull(connection, make([]byte, 8)); err != nil {
					return
				}
				if sslMode == "require" {
					if _, err := connection.Write([]byte{'S'}); err != nil {
						return
					}
					if _, err := io.ReadFull(connection, make([]byte, 5)); err != nil {
						return
					}
				}
				close(handshakeReady)
				<-release
			}()
			connected, seconds := true, 0.125
			store := &fakeCaptureStore{resource: &models.CaptureResource{ID: 1, SourceType: models.CaptureSourcePostgreSQL,
				Status: models.CaptureStatusRunning, ResourceVersion: 1, SourceDatabase: "unused",
				PostgreSQL: &models.PostgreSQLCaptureResource{SlotName: "owned", PublicationName: "owned", SlotOwned: true, PublicationOwned: true}}}
			connect := &fakeConnectControl{delay: &CaptureDelay{Status: "available", Connected: &connected, Seconds: &seconds}}
			plan := &CapturePlan{SourceConnInfo: engineplugin.ConnectionInfo{
				"host": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port, "user": "fixture", "database": "unused", "sslmode": sslMode}}
			s := &Supervisor{store: store, plans: fakePlanResolver{plan: plan}, connect: connect,
				config: SupervisorConfig{SourceProbeTimeout: 100 * time.Millisecond, MonitorInterval: time.Second}}
			result, clientDone := make(chan error, 1), make(chan struct{})
			defer func() { close(release); listener.Close(); <-serverDone; <-clientDone }()
			go func() {
				defer close(clientDone)
				result <- s.observePostgreSQLTask(context.Background(), store.resource)
			}()
			select {
			case err := <-result:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("source probe exceeded its deadline during PostgreSQL startup")
			}
			select {
			case <-handshakeReady:
			default:
				t.Fatal("probe did not reach the silent handshake")
			}
			summary := models.NewCaptureSummary(store.resource)
			if summary.PostgreSQLObservation.Status != "unavailable" || !summary.PostgreSQLObservation.ExpiresAt.After(time.Now()) ||
				summary.DelayObservation.Status != "available" || summary.DelayObservation.DelaySeconds == nil || *summary.DelayObservation.DelaySeconds != seconds {
				t.Fatalf("source handshake blocked independent fresh observations: %+v", summary)
			}
		})
	}
}
