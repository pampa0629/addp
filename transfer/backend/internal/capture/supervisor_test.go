package capture

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

func TestCaptureSupervisorResumeRecordsConnectorFailureFacts(t *testing.T) {
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
	if store.resource.Status != models.CaptureStatusFailed || store.resource.ConnectorStatus != "FAILED" {
		t.Fatalf("resource after connector failure = %+v", store.resource)
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
	resource *models.CaptureResource
	terminal bool
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
	return nil
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
