package capture

import (
	"context"
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
			ConnectorName: "connector", TopicName: "topic", ConsumerGroup: "group", SlotName: "slot", PublicationName: "publication",
			SourceIdentity: identity.SourceIdentity, SourceConnectionFingerprint: identity.SourceConnectionFingerprint,
			SourceEngineID: identity.SourceEngineID, SourceDatabase: identity.SourceDatabase,
			SourceSchema: identity.SourceSchema, SourceTable: identity.SourceTable,
			Status: models.CaptureStatusProvisioning, ResourceVersion: 1, SlotOwned: true, PublicationOwned: true,
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
}

func (f *fakeCaptureStore) HasTerminalGeneration(context.Context, uint, uint) (bool, error) {
	return f.terminal, nil
}

type fakePlanResolver struct{ plan *CapturePlan }

func (f fakePlanResolver) Resolve(context.Context, *models.TransferTask) (*CapturePlan, error) {
	return f.plan, nil
}
func (f fakePlanResolver) ResolveForCleanup(context.Context, *models.TransferTask) (*CapturePlan, error) {
	return f.plan, nil
}

type fakeConnectControl struct {
	state       string
	putConfig   map[string]string
	pauseCalls  int
	resumeCalls int
	deleted     bool
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
}

func (f *fakeTopicControl) EnsureTopic(context.Context, TopicSpec) error {
	f.created = true
	return nil
}
func (f *fakeTopicControl) EnsureAccess(context.Context, string, string) error {
	f.accessCreated = true
	return nil
}
func (f *fakeTopicControl) DeleteTopic(context.Context, string) error { f.deleted = true; return nil }
func (f *fakeTopicControl) DeleteConsumerGroup(context.Context, string) error {
	f.groupDeleted = true
	return nil
}
func (f *fakeTopicControl) DeleteAccess(context.Context, string, string) error {
	f.accessDeleted = true
	return nil
}
func (f *fakeTopicControl) Close() {}

type fakeSourceResources struct{ calls int }

func (f *fakeSourceResources) DropOwnedResources(context.Context, *CapturePlan, *models.CaptureResource) error {
	f.calls++
	return nil
}
