package capture

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"gorm.io/gorm"
)

func TestCaptureSupervisorStartPauseResumeStopLifecycle(t *testing.T) {
	store := &fakeCaptureStore{}
	connect := &fakeConnectControl{state: "RUNNING"}
	topics := &fakeTopicControl{}
	source := &fakeSourceResources{}
	plans := fakePlanResolver{plan: &CapturePlan{
		SourceType:     models.CaptureSourcePostgreSQL,
		SourceConnInfo: engineplugin.ConnectionInfo{"host": "source", "user": "postgres", "database": "business"},
		SourceEngineID: 12, SourceDatabase: "business", SourceSchema: "public", SourceTable: "orders",
		SourceIdentity: "addp://engine/12/path/public/orders?type=table", SourceConnectionFingerprint: "fingerprint",
	}}
	supervisor, err := NewSupervisor(store, plans, connect, topics, source, SupervisorConfig{
		TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second,
		StatusPollInterval: time.Millisecond, MonitorInterval: time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	task := &models.TransferTask{ID: 3, TenantID: 2, Name: "cdc"}
	resource, err := supervisor.Start(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if resource.Status != models.CaptureStatusRunning || !topics.created || !topics.accessCreated || connect.putConfig == nil {
		t.Fatalf("resource=%+v topic=%v config=%#v", resource, topics.created, connect.putConfig)
	}
	if source.probeCalls == 0 {
		t.Fatal("capture start did not probe the source database")
	}
	if err := supervisor.Pause(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if connect.pauseCalls != 0 {
		t.Fatalf("Pause called Kafka Connect pause %d times; CDC pause must keep capture running", connect.pauseCalls)
	}
	connect.state = "PAUSED"
	if err := supervisor.Resume(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if connect.resumeCalls != 1 {
		t.Fatalf("resume calls = %d", connect.resumeCalls)
	}
	if err := supervisor.Stop(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if store.resource.Status != models.CaptureStatusStopped || !connect.deleted || !topics.deleted || !topics.groupDeleted || !topics.accessDeleted || source.calls != 1 {
		t.Fatalf("cleanup incomplete: resource=%+v connect=%+v topics=%+v source=%+v", store.resource, connect, topics, source)
	}
	if err := supervisor.Stop(context.Background(), task); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

func TestCaptureSupervisorRefreshRequiresConnectorAndSourceHealth(t *testing.T) {
	store := &fakeCaptureStore{resource: &models.CaptureResource{
		ID: 1, TaskID: 3, TenantID: 2, SourceType: models.CaptureSourceOracle,
		SourceEngineID: 22, SourceDatabase: "FREEPDB1", SourceConnectionFingerprint: "fingerprint",
		ConnectorName: "connector", Status: models.CaptureStatusRunning, ResourceVersion: 1,
	}}
	plans := fakePlanResolver{plan: &CapturePlan{
		SourceType: models.CaptureSourceOracle, SourceEngineID: 22, SourceDatabase: "FREEPDB1",
		SourceConnectionFingerprint: "fingerprint", CDCConnInfo: engineplugin.ConnectionInfo{"user": "C##ADDP_CDC"},
	}}
	source := &fakeSourceResources{
		probeErr: errors.New("oracle unavailable"),
		recovery: &models.CaptureSourceRecovery{
			SchemaVersion: "capture.source_recovery/v1", Provider: "oracle", Health: "healthy", CapturePosition: "100", SampledAt: time.Now(),
		},
		transactions: &models.CaptureSourceTransactions{
			SchemaVersion: "capture.source_transactions/v1", Provider: "oracle", Status: "available", ActiveCount: 1, SampledAt: time.Now(),
		},
	}
	supervisor, err := NewSupervisor(store, plans, &fakeConnectControl{state: "RUNNING"}, &fakeTopicControl{}, source, SupervisorConfig{
		TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second,
		StatusPollInterval: time.Millisecond, MonitorInterval: time.Second, SourceProbeTimeout: time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Refresh(context.Background(), store.resource); err == nil {
		t.Fatal("Refresh() succeeded while source probe failed")
	}
	if store.resource.Status != models.CaptureStatusFailed || store.resource.SourceStatus != "OFFLINE" || store.resource.ConnectorStatus != "RUNNING" {
		t.Fatalf("resource after failed source probe = %+v", store.resource)
	}
	if recovery := models.NewCaptureSummary(store.resource).SourceRecovery; recovery == nil || recovery.Health != "unknown" {
		t.Fatalf("source failure recovery observation = %#v", recovery)
	}
	if transactions := models.NewCaptureSummary(store.resource).SourceTransactions; transactions == nil || transactions.Status != "unavailable" {
		t.Fatalf("source failure transaction observation = %#v", transactions)
	}
	source.probeErr = nil
	if _, err := supervisor.Refresh(context.Background(), store.resource); err != nil {
		t.Fatal(err)
	}
	if store.resource.Status != models.CaptureStatusRunning || store.resource.SourceStatus != "ONLINE" {
		t.Fatalf("resource after source recovery = %+v", store.resource)
	}
	if recovery := models.NewCaptureSummary(store.resource).SourceRecovery; recovery == nil || recovery.Health != "healthy" || recovery.CapturePosition != "100" {
		t.Fatalf("source recovery observation = %#v", recovery)
	}
	if transactions := models.NewCaptureSummary(store.resource).SourceTransactions; transactions == nil || transactions.Status != "available" || transactions.ActiveCount != 1 {
		t.Fatalf("source transaction observation = %#v", transactions)
	}
}

func TestCaptureSupervisorResumeLeavesFailurePublicationToSampling(t *testing.T) {
	store := &fakeCaptureStore{resource: &models.CaptureResource{
		ID: 1, TaskID: 3, TenantID: 2, SourceType: models.CaptureSourcePostgreSQL,
		SourceEngineID: 12, SourceDatabase: "business", SourceConnectionFingerprint: "fingerprint",
		ConnectorName: "connector", Status: models.CaptureStatusRunning, ResourceVersion: 1,
	}}
	connect := &fakeConnectControl{state: "FAILED"}
	supervisor, err := NewSupervisor(store, fakePlanResolver{plan: &CapturePlan{}}, connect, &fakeTopicControl{}, &fakeSourceResources{}, SupervisorConfig{
		TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second,
		StatusPollInterval: time.Millisecond, MonitorInterval: time.Second, SourceProbeTimeout: time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Resume(context.Background(), &models.TransferTask{ID: 3, TenantID: 2}); err == nil {
		t.Fatal("Resume() succeeded with an unhealthy connector")
	}
	if store.resource.Status != models.CaptureStatusRunning || store.resource.ResourceVersion != 1 || store.resource.ConnectorStatus != "" {
		t.Fatalf("Resume() published observations: %+v", store.resource)
	}
	if _, err := supervisor.Refresh(context.Background(), store.resource); err == nil {
		t.Fatal("sampling accepted an unhealthy connector")
	}
	if store.resource.Status != models.CaptureStatusFailed || store.resource.ConnectorStatus != "FAILED" {
		t.Fatalf("resource after connector failure = %+v", store.resource)
	}
}

func TestCaptureControlsCheckSourceWithoutPublishing(t *testing.T) {
	for _, action := range []string{"pause", "resume"} {
		t.Run(action, func(t *testing.T) {
			store := &fakeCaptureStore{resource: &models.CaptureResource{
				ID: 1, TaskID: 3, TenantID: 2, Generation: 1, ConnectorName: "owned",
				Status: models.CaptureStatusRunning, ResourceVersion: 1,
			}}
			source := &fakeSourceResources{probeErr: errors.New("source unavailable")}
			s, _ := NewSupervisor(store, fakePlanResolver{plan: &CapturePlan{}}, &fakeConnectControl{state: "RUNNING"}, &fakeTopicControl{}, source, SupervisorConfig{
				TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second,
				StatusPollInterval: time.Millisecond, MonitorInterval: time.Second,
			}, slog.Default())
			task := &models.TransferTask{ID: 3, TenantID: 2}
			control := s.Pause
			if action == "resume" {
				control = s.Resume
			}
			if err := control(context.Background(), task); !errors.Is(err, source.probeErr) {
				t.Fatalf("source check = %v", err)
			}
			if source.probeCalls != 1 || store.resource.ResourceVersion != 1 || store.resource.Status != models.CaptureStatusRunning {
				t.Fatalf("control published source health: calls=%d resource=%+v", source.probeCalls, store.resource)
			}
			if _, err := s.Refresh(context.Background(), store.resource); !errors.Is(err, source.probeErr) || store.resource.Status != models.CaptureStatusFailed {
				t.Fatalf("background source failure publication = %v, %+v", err, store.resource)
			}
		})
	}
}

func TestCaptureControlsRejectStopBeforeConnectorActions(t *testing.T) {
	for _, status := range []models.CaptureStatus{models.CaptureStatusCleaning, models.CaptureStatusCleanupFailed, models.CaptureStatusStopped} {
		store := &fakeCaptureStore{resource: &models.CaptureResource{ID: 1, TaskID: 3, TenantID: 2, Status: status}}
		connect := &fakeConnectControl{state: "PAUSED"}
		source := &fakeSourceResources{}
		s, _ := NewSupervisor(store, fakePlanResolver{plan: &CapturePlan{}}, connect, &fakeTopicControl{}, source, SupervisorConfig{
			TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second,
			StatusPollInterval: time.Millisecond, MonitorInterval: time.Second,
		}, slog.Default())
		for _, control := range []func(context.Context, *models.TransferTask) error{s.Pause, s.Resume} {
			if err := control(context.Background(), &models.TransferTask{ID: 3, TenantID: 2}); !errors.Is(err, repository.ErrCaptureTerminal) {
				t.Fatalf("control after %s = %v", status, err)
			}
		}
		if source.probeCalls != 0 || connect.resumeCalls != 0 || store.resource.Status != status {
			t.Fatalf("control touched terminal generation %s", status)
		}
	}
}

// The real repository returns detached snapshots and advances the stored CAS version.
type controlCaptureStore struct {
	*fakeCaptureStore
	mu      sync.Mutex
	updates int
}

func (f *controlCaptureStore) GetLatest(ctx context.Context, task, tenant uint) (*models.CaptureResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.fakeCaptureStore.GetLatest(ctx, task, tenant)
	if err != nil {
		return nil, err
	}
	copy := *r
	return &copy, nil
}

func (f *controlCaptureStore) Update(ctx context.Context, id uint, version uint64, fields map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	if err := f.fakeCaptureStore.Update(ctx, id, version, fields); err != nil {
		return err
	}
	f.resource.ResourceVersion++
	return nil
}

type controlProbeKey struct{}

type controlSourceProbe struct {
	fakeSourceResources
	entered chan struct{}
	release chan struct{}
}

func (s *controlSourceProbe) Probe(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	if ctx.Value(controlProbeKey{}) == true {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.fakeSourceResources.Probe(ctx, plan, resource)
}

func TestCaptureControlsKeepSamplingCASAndRecheckGeneration(t *testing.T) {
	for _, action := range []string{"pause", "resume"} {
		for _, change := range []string{"observation", "cleaning", "cleanup_failed", "stopped", "generation"} {
			t.Run(action+"/"+change, func(t *testing.T) {
				store := &controlCaptureStore{fakeCaptureStore: &fakeCaptureStore{resource: &models.CaptureResource{
					ID: 1, TaskID: 3, TenantID: 2, Generation: 1, SourceType: models.CaptureSourcePostgreSQL,
					ConnectorName: "owned", Status: models.CaptureStatusRunning, ResourceVersion: 103,
				}}}
				source := &controlSourceProbe{entered: make(chan struct{}), release: make(chan struct{})}
				s, err := NewSupervisor(store, fakePlanResolver{plan: &CapturePlan{}}, &fakeConnectControl{state: "RUNNING"}, &fakeTopicControl{}, source, SupervisorConfig{
					TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second,
					StatusPollInterval: time.Millisecond, MonitorInterval: time.Second, SourceProbeTimeout: 5 * time.Second,
				}, slog.Default())
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.WithValue(context.Background(), controlProbeKey{}, true)
				done := make(chan error, 1)
				go func() {
					task := &models.TransferTask{ID: 3, TenantID: 2}
					if action == "pause" {
						done <- s.Pause(ctx, task)
					} else {
						done <- s.Resume(ctx, task)
					}
				}()
				select {
				case <-source.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("control did not reach its read-only source probe")
				}
				// Publish a real Refresh result while the user probe holds version 103.
				latest, err := store.GetLatest(context.Background(), 3, 2)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.Refresh(context.Background(), latest); err != nil {
					t.Fatal(err)
				}
				store.mu.Lock()
				if change == "generation" {
					store.resource.ID++
					store.resource.Generation++
				} else if change != "observation" {
					store.resource.Status = models.CaptureStatus(change)
					store.resource.ResourceVersion++
				}
				store.mu.Unlock()
				close(source.release)
				if err := <-done; (err == nil) != (change == "observation") {
					t.Fatalf("control after %s: %v", change, err)
				}
				if store.updates != 1 {
					t.Fatalf("control competed with the sampling writer: %d writes", store.updates)
				}
				if store.resource.HealthObservation == nil || store.resource.ResourceVersion < 104 {
					t.Fatalf("sampling publication disappeared: %+v", store.resource)
				}
			})
		}
	}
}

func TestSourceProbeQueryUsesOracleDual(t *testing.T) {
	if got := sourceProbeQuery(models.CaptureSourceOracle); got != "SELECT 1 FROM DUAL" {
		t.Fatalf("Oracle probe query = %q", got)
	}
	if got := sourceProbeQuery(models.CaptureSourcePostgreSQL); got != "SELECT 1" {
		t.Fatalf("PostgreSQL probe query = %q", got)
	}
}

func TestCaptureSupervisorRejectsTerminalGeneration(t *testing.T) {
	store := &fakeCaptureStore{terminal: true}
	supervisor, _ := NewSupervisor(store, fakePlanResolver{plan: &CapturePlan{}}, &fakeConnectControl{}, &fakeTopicControl{}, &fakeSourceResources{}, SupervisorConfig{
		TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second,
		StatusPollInterval: time.Millisecond, MonitorInterval: time.Second,
	}, slog.Default())
	_, err := supervisor.Start(context.Background(), &models.TransferTask{ID: 1, TenantID: 1})
	if !errors.Is(err, repository.ErrCaptureTerminal) {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestCaptureSupervisorOwnsMySQLSchemaHistoryTopic(t *testing.T) {
	store := &fakeCaptureStore{}
	connect := &fakeConnectControl{state: "RUNNING"}
	topics := &fakeTopicControl{}
	plan := &CapturePlan{
		SourceType:     models.CaptureSourceMySQL,
		SourceConnInfo: engineplugin.ConnectionInfo{"host": "localhost", "user": "cdc", "password": "secret"},
		SourceEngineID: 13, SourceDatabase: "business", SourceTable: "orders",
		SourceIdentity: "addp://engine/13/path/business/orders?type=table", SourceConnectionFingerprint: "fingerprint",
	}
	supervisor, err := NewSupervisor(store, fakePlanResolver{plan: plan}, connect, topics, &fakeSourceResources{}, SupervisorConfig{
		TopicRetention: time.Hour, TopicReplication: 1, ConnectLoopbackHost: "host.docker.internal",
		ConnectBootstrapServers: "redpanda:29092", ConnectKafkaUsername: "connect", ConnectKafkaPassword: "secret",
		ConnectKafkaSecurityProtocol: "sasl_plaintext", ProvisioningTimeout: time.Second,
		StatusPollInterval: time.Millisecond, MonitorInterval: time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	task := &models.TransferTask{ID: 3, TenantID: 2}
	if _, err := supervisor.Start(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if len(topics.specs) != 2 || topics.specs[1].Name != "__addp_cdc_schema.2.3.1" ||
		topics.specs[1].CleanupPolicy != "delete" || topics.specs[1].RetentionMillis != -1 || !topics.schemaAccessCreated {
		t.Fatalf("MySQL topic provisioning = %#v, schema access=%t", topics.specs, topics.schemaAccessCreated)
	}
	if err := supervisor.Stop(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if topics.deletedTopics != 2 || !topics.schemaAccessDeleted {
		t.Fatalf("MySQL topic cleanup = %+v", topics)
	}
}

func TestCaptureSupervisorOwnsOracleSchemaHistoryTopic(t *testing.T) {
	store := &fakeCaptureStore{}
	topics := &fakeTopicControl{}
	plan := &CapturePlan{
		SourceType:     models.CaptureSourceOracle,
		CDCConnInfo:    engineplugin.ConnectionInfo{"host": "localhost", "port": 15210, "service_name": "FREEPDB1", "user": "C##ADDP_CDC", "password": "secret"},
		SourceEngineID: 22, SourceCDBName: "FREE", SourceDatabase: "FREEPDB1", SourceSchema: "BUSINESS", SourceTable: "CUSTOMERS",
		SourceIdentity: "addp://engine/22/path/BUSINESS/CUSTOMERS?type=table", SourceConnectionFingerprint: "fingerprint",
	}
	supervisor, err := NewSupervisor(store, fakePlanResolver{plan: plan}, &fakeConnectControl{state: "RUNNING"}, topics, &fakeSourceResources{}, SupervisorConfig{
		TopicRetention: time.Hour, TopicReplication: 1, ConnectLoopbackHost: "host.docker.internal",
		ConnectBootstrapServers: "redpanda:29092", ConnectKafkaUsername: "connect", ConnectKafkaPassword: "secret",
		ConnectKafkaSecurityProtocol: "sasl_plaintext", ProvisioningTimeout: time.Second,
		StatusPollInterval: time.Millisecond, MonitorInterval: time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	task := &models.TransferTask{ID: 3, TenantID: 2}
	if _, err := supervisor.Start(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if len(topics.specs) != 2 || topics.specs[1].Name != "__addp_cdc_schema.2.3.1" || !topics.schemaAccessCreated {
		t.Fatalf("Oracle topic provisioning = %#v", topics.specs)
	}
	if err := supervisor.Stop(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if topics.deletedTopics != 2 || !topics.schemaAccessDeleted {
		t.Fatalf("Oracle topic cleanup = %+v", topics)
	}
}

type fakeCaptureStore struct {
	reservationErr   error
	reservationCalls int
	committed        *int64
	committedErr     error
	resource         *models.CaptureResource
	terminal         bool
}

func (f *fakeCaptureStore) BeginGeneration(_ context.Context, identity repository.CaptureIdentity) (*models.CaptureResource, error) {
	if f.terminal {
		return nil, repository.ErrCaptureTerminal
	}
	if f.resource == nil {
		f.resource = &models.CaptureResource{
			ID: 1, TaskID: identity.TaskID, TenantID: identity.TenantID, Generation: 1,
			ConnectorName: "connector", TopicName: "topic", ConsumerGroup: "group", SourceType: identity.SourceType,
			SourceIdentity: identity.SourceIdentity, SourceConnectionFingerprint: identity.SourceConnectionFingerprint,
			SourceEngineID: identity.SourceEngineID, SourceDatabase: identity.SourceDatabase,
			SourceSchema: identity.SourceSchema, SourceTable: identity.SourceTable,
			Status: models.CaptureStatusProvisioning, ResourceVersion: 1,
		}
		switch identity.SourceType {
		case models.CaptureSourcePostgreSQL:
			f.resource.PostgreSQL = &models.PostgreSQLCaptureResource{CaptureResourceID: 1, SlotName: "slot", PublicationName: "publication", SlotOwned: true, PublicationOwned: true}
		case models.CaptureSourceMySQL:
			f.resource.MySQL = &models.MySQLCaptureResource{CaptureResourceID: 1, ConnectorServerID: 1, SchemaHistoryTopicName: "__addp_cdc_schema.2.3.1", SchemaHistoryTopicOwned: true}
		case models.CaptureSourceOracle:
			f.resource.Oracle = &models.OracleCaptureResource{CaptureResourceID: 1, SchemaHistoryTopicName: "__addp_cdc_schema.2.3.1", SchemaHistoryTopicOwned: true}
		}
	}
	return f.resource, nil
}

func (f *fakeCaptureStore) GetLatest(context.Context, uint, uint) (*models.CaptureResource, error) {
	if f.resource == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return f.resource, nil
}

func (f *fakeCaptureStore) ListObservable(context.Context, int) ([]models.CaptureResource, error) {
	return nil, nil
}

func (f *fakeCaptureStore) Update(_ context.Context, _ uint, expected uint64, fields map[string]interface{}) error {
	if f.resource.ResourceVersion != expected {
		return errors.New("version conflict")
	}
	f.apply(fields)
	return nil
}

func (f *fakeCaptureStore) ForceUpdate(_ context.Context, _ uint, fields map[string]interface{}) error {
	f.apply(fields)
	return nil
}

func (f *fakeCaptureStore) apply(fields map[string]interface{}) {
	if v, ok := fields["health_observation"].(models.JSONMap); ok {
		f.resource.HealthObservation = v
	}
	if v, ok := fields["postgresql_observation"].(models.JSONMap); ok {
		f.resource.PostgreSQLObservation = v
	}
	if v, ok := fields["delay_observation"].(models.JSONMap); ok {
		f.resource.DelayObservation = v
	}
	if value, ok := fields["buffer_observation"].(models.JSONMap); ok {
		f.resource.BufferObservation = value
	}
	if value, ok := fields["connector_error"].(string); ok {
		f.resource.ConnectorError = value
	}
	if status, ok := fields["status"].(models.CaptureStatus); ok {
		f.resource.Status = status
	}
	if value, ok := fields["topic_created"].(bool); ok {
		f.resource.TopicCreated = value
	}
	if value, ok := fields["connector_created"].(bool); ok {
		f.resource.ConnectorCreated = value
	}
	if value, ok := fields["connector_status"].(string); ok {
		f.resource.ConnectorStatus = value
	}
	if value, ok := fields["source_status"].(string); ok {
		f.resource.SourceStatus = value
	}
	if value, ok := fields["source_recovery"].(models.JSONMap); ok {
		f.resource.SourceRecovery = value
	}
	if value, ok := fields["source_transactions"].(models.JSONMap); ok {
		f.resource.SourceTransactions = value
	}
}

func (f *fakeCaptureStore) HasStopInitiatedGeneration(context.Context, uint, uint) (bool, error) {
	return f.terminal, nil
}

type fakePlanResolver struct{ plan *CapturePlan }

func (f fakePlanResolver) Resolve(context.Context, *models.TransferTask) (*CapturePlan, error) {
	return f.plan, nil
}
func (f fakePlanResolver) ResolveForCleanup(context.Context, *models.TransferTask) (*CapturePlan, error) {
	return f.plan, nil
}
func (f fakePlanResolver) ResolveForObservation(context.Context, *models.CaptureResource) (*CapturePlan, error) {
	return f.plan, nil
}

type fakeConnectControl struct {
	delay       *CaptureDelay
	state       string
	putConfig   map[string]string
	pauseCalls  int
	resumeCalls int
	deleted     bool
	offsets     *ConnectorOffsets
}

func (f *fakeConnectControl) PutConfig(_ context.Context, _ string, config map[string]string) error {
	f.putConfig = config
	if f.state == "" {
		f.state = "RUNNING"
	}
	return nil
}
func (f *fakeConnectControl) Status(context.Context, string) (*ConnectorStatus, error) {
	if f.deleted {
		return nil, ErrConnectorNotFound
	}
	return &ConnectorStatus{ConnectorState: f.state, TaskStates: []string{f.state}}, nil
}
func (f *fakeConnectControl) Offsets(context.Context, string) (*ConnectorOffsets, error) {
	if f.offsets != nil {
		return f.offsets, nil
	}
	return &ConnectorOffsets{Offsets: []ConnectorOffset{{Offset: map[string]json.RawMessage{"scn": json.RawMessage(`"1"`)}}}}, nil
}
func (f *fakeConnectControl) Pause(context.Context, string) error {
	f.pauseCalls++
	f.state = "PAUSED"
	return nil
}
func (f *fakeConnectControl) Resume(context.Context, string) error {
	f.resumeCalls++
	f.state = "RUNNING"
	return nil
}
func (f *fakeConnectControl) Delete(context.Context, string) error { f.deleted = true; return nil }

type fakeTopicControl struct {
	earliest, latest                                             int64
	bufferErr                                                    error
	created, accessCreated, deleted, groupDeleted, accessDeleted bool
	schemaAccessCreated, schemaAccessDeleted                     bool
	deletedTopics                                                int
	specs                                                        []TopicSpec
}

func (f *fakeTopicControl) EnsureTopic(_ context.Context, spec TopicSpec) error {
	f.created = true
	f.specs = append(f.specs, spec)
	return nil
}
func (f *fakeTopicControl) EnsureAccess(context.Context, string, string) error {
	f.accessCreated = true
	return nil
}
func (f *fakeTopicControl) EnsureSchemaHistoryAccess(context.Context, string) error {
	f.schemaAccessCreated = true
	return nil
}
func (f *fakeTopicControl) DeleteTopic(context.Context, string) error {
	f.deleted = true
	f.deletedTopics++
	return nil
}
func (f *fakeTopicControl) DeleteConsumerGroup(context.Context, string) error {
	f.groupDeleted = true
	return nil
}
func (f *fakeTopicControl) DeleteAccess(context.Context, string, string) error {
	f.accessDeleted = true
	return nil
}
func (f *fakeTopicControl) DeleteSchemaHistoryAccess(context.Context, string) error {
	f.schemaAccessDeleted = true
	return nil
}
func (f *fakeTopicControl) Close() {}

type fakeSourceResources struct {
	ensureErr       error
	ensureCalls     int
	calls           int
	probeCalls      int
	probeErr        error
	recovery        *models.CaptureSourceRecovery
	recoveryErr     error
	transactions    *models.CaptureSourceTransactions
	transactionsErr error
}

func (f *fakeSourceResources) EnsureOwnedResources(context.Context, *CapturePlan, *models.CaptureResource) error {
	f.ensureCalls++
	return f.ensureErr
}

func (f *fakeSourceResources) DropOwnedResources(context.Context, *CapturePlan, *models.CaptureResource) error {
	f.calls++
	return nil
}

func (f *fakeSourceResources) Probe(context.Context, *CapturePlan, *models.CaptureResource) error {
	f.probeCalls++
	return f.probeErr
}

func (f *fakeSourceResources) Observe(context.Context, *CapturePlan, *models.CaptureResource, *ConnectorOffsets, time.Time) (*SourceObservation, error) {
	return &SourceObservation{
		Recovery: f.recovery, RecoveryError: f.recoveryErr,
		Transactions: f.transactions, TransactionsError: f.transactionsErr,
	}, nil
}

func (f *fakeCaptureStore) RunCleanupOperation(ctx context.Context, _ *models.CaptureResource, _ string, _ any, invoke func(context.Context) error) error {
	return invoke(ctx)
}

func (f *fakeTopicControl) BufferOffsets(context.Context, string) (int64, int64, error) {
	return f.earliest, f.latest, f.bufferErr
}
func (f *fakeCaptureStore) BufferCommittedPosition(context.Context, *models.CaptureResource) (*int64, bool, error) {
	return f.committed, f.committed != nil, f.committedErr
}

func (f *fakeCaptureStore) ClaimPostgreSQLResources(_ context.Context, resource *models.CaptureResource) error {
	f.reservationCalls++
	if f.reservationErr != nil {
		return f.reservationErr
	}
	if resource.ResourceVersion != f.resource.ResourceVersion {
		return errors.New("version conflict")
	}
	return nil
}

func TestCaptureRejectsForeignReservationBeforeTopicOrConnectorEffects(t *testing.T) {
	store := &fakeCaptureStore{resource: &models.CaptureResource{ID: 1, TaskID: 3, TenantID: 2, SourceType: models.CaptureSourcePostgreSQL, ResourceVersion: 1, Status: models.CaptureStatusRunning, PostgreSQL: &models.PostgreSQLCaptureResource{SlotName: "reserved", PublicationName: "reserved_pub"}}}
	topics := &fakeTopicControl{}
	connect := &fakeConnectControl{state: "RUNNING"}
	s, err := NewSupervisor(store, fakePlanResolver{plan: &CapturePlan{}}, connect, topics, &fakeSourceResources{ensureErr: errors.New("foreign publication")}, SupervisorConfig{TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second, StatusPollInterval: time.Millisecond, MonitorInterval: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(context.Background(), &models.TransferTask{ID: 3, TenantID: 2}); err == nil {
		t.Fatal("foreign resource accepted")
	}
	if topics.created || connect.putConfig != nil || store.reservationCalls != 0 || store.resource.PostgreSQL.SlotOwned || store.resource.PostgreSQL.PublicationOwned {
		t.Fatal("foreign rejection claimed resources or invoked external creation")
	}
}

func TestCaptureReservationPersistenceFailurePreventsExternalCreation(t *testing.T) {
	store := &fakeCaptureStore{reservationErr: errors.New("ownership CAS rejected"), resource: &models.CaptureResource{ID: 1, TaskID: 3, TenantID: 2, SourceType: models.CaptureSourcePostgreSQL, ResourceVersion: 1, Status: models.CaptureStatusProvisioning, PostgreSQL: &models.PostgreSQLCaptureResource{SlotName: "reserved", PublicationName: "reserved_pub"}}}
	topics := &fakeTopicControl{}
	connect := &fakeConnectControl{state: "RUNNING"}
	s, err := NewSupervisor(store, fakePlanResolver{plan: &CapturePlan{}}, connect, topics, &fakeSourceResources{}, SupervisorConfig{TopicRetention: time.Hour, TopicReplication: 1, ProvisioningTimeout: time.Second, StatusPollInterval: time.Millisecond, MonitorInterval: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(context.Background(), &models.TransferTask{ID: 3, TenantID: 2}); err == nil {
		t.Fatal("unpersisted reservation accepted")
	}
	if topics.created || connect.putConfig != nil || store.reservationCalls != 1 || store.resource.PostgreSQL.SlotOwned || store.resource.PostgreSQL.PublicationOwned {
		t.Fatal("external creation preceded durable ownership")
	}
}

func (f *fakeConnectControl) Delay(context.Context, string) (*CaptureDelay, error) {
	if f.delay != nil {
		return f.delay, nil
	}
	return &CaptureDelay{Status: "no_sample"}, nil
}
