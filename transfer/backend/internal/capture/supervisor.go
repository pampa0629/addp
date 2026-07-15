package capture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"gorm.io/gorm"
)

type ResourceStore interface {
	BeginGeneration(ctx context.Context, identity repository.CaptureIdentity) (*models.CaptureResource, error)
	GetLatest(ctx context.Context, taskID, tenantID uint) (*models.CaptureResource, error)
	ListObservable(ctx context.Context, limit int) ([]models.CaptureResource, error)
	Update(ctx context.Context, id uint, expectedVersion uint64, fields map[string]interface{}) error
	HasStopInitiatedGeneration(ctx context.Context, taskID, tenantID uint) (bool, error)
}

type SupervisorConfig struct {
	TopicRetention      time.Duration
	TopicRetentionBytes int64
	TopicReplication    int16
	ConnectLoopbackHost string
	ProvisioningTimeout time.Duration
	StatusPollInterval  time.Duration
	MonitorInterval     time.Duration
}

func (c SupervisorConfig) Validate() error {
	if c.TopicRetention <= 0 || c.TopicReplication <= 0 || c.ProvisioningTimeout <= 0 || c.StatusPollInterval <= 0 || c.MonitorInterval <= 0 {
		return fmt.Errorf("capture supervisor durations and replication factor must be greater than zero")
	}
	return nil
}

// Supervisor 是 PostgreSQL CDC 捕获资源的唯一 lifecycle owner。
// pause 不调用 Kafka Connect pause：它只停止目标应用，connector 必须继续捕获。
type Supervisor struct {
	store   ResourceStore
	plans   PlanResolver
	connect ConnectControl
	topics  TopicControl
	source  SourceResourceControl
	config  SupervisorConfig
	logger  *slog.Logger
}

func NewSupervisor(store ResourceStore, plans PlanResolver, connect ConnectControl, topics TopicControl, source SourceResourceControl, config SupervisorConfig, logger *slog.Logger) (*Supervisor, error) {
	if store == nil || plans == nil || connect == nil || topics == nil || source == nil {
		return nil, fmt.Errorf("capture supervisor dependencies are required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{store: store, plans: plans, connect: connect, topics: topics, source: source, config: config, logger: logger}, nil
}

func (s *Supervisor) Start(ctx context.Context, task *models.TransferTask) (*models.CaptureResource, error) {
	plan, err := s.plans.Resolve(ctx, task)
	if err != nil {
		return nil, err
	}
	spatialInfo := models.JSONMap{}
	if payload := datatype.SpatialInfoPayload(plan.SourceSpatialInfo); len(payload) > 0 {
		spatialInfo = models.JSONMap(payload)
	}
	resource, err := s.store.BeginGeneration(ctx, repository.CaptureIdentity{
		TaskID: task.ID, TenantID: task.TenantID, SourceIdentity: plan.SourceIdentity,
		SourceConnectionFingerprint: plan.SourceConnectionFingerprint,
		SourceEngineID:              plan.SourceEngineID, SourceDatabase: plan.SourceDatabase,
		SourceSchema: plan.SourceSchema, SourceTable: plan.SourceTable,
		SourceSpatialInfo: spatialInfo,
	})
	if err != nil {
		return nil, err
	}
	if resource.Status == models.CaptureStatusRunning {
		if _, err := s.Refresh(ctx, resource); err == nil {
			return resource, nil
		}
	}
	if err := s.topics.EnsureTopic(ctx, TopicSpec{
		Name: resource.TopicName, Partitions: 1, ReplicationFactor: s.config.TopicReplication,
		RetentionMillis: s.config.TopicRetention.Milliseconds(), RetentionBytes: s.config.TopicRetentionBytes,
	}); err != nil {
		s.fail(ctx, resource, err)
		return nil, err
	}
	if err := s.topics.EnsureAccess(ctx, resource.TopicName, resource.ConsumerGroup); err != nil {
		s.fail(ctx, resource, err)
		return nil, err
	}
	if err := s.advance(ctx, resource, map[string]interface{}{"status": models.CaptureStatusProvisioning, "topic_created": true, "connector_error": ""}); err != nil {
		return nil, err
	}
	if err := s.connect.PutConfig(ctx, resource.ConnectorName, buildConnectorConfig(plan, resource, s.config.ConnectLoopbackHost)); err != nil {
		s.fail(ctx, resource, err)
		return nil, err
	}
	if err := s.advance(ctx, resource, map[string]interface{}{"connector_created": true}); err != nil {
		return nil, err
	}
	status, err := s.waitRunning(ctx, resource.ConnectorName)
	if err != nil {
		s.fail(ctx, resource, err)
		return nil, err
	}
	now := time.Now()
	if err := s.advance(ctx, resource, map[string]interface{}{
		"status": models.CaptureStatusRunning, "connector_status": status.ConnectorState,
		"connector_error": "", "last_observed_at": now,
	}); err != nil {
		return nil, err
	}
	return resource, nil
}

// Pause 只观测 connector；不得暂停捕获。
func (s *Supervisor) Pause(ctx context.Context, task *models.TransferTask) error {
	resource, err := s.store.GetLatest(ctx, task.ID, task.TenantID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if resource.Status == models.CaptureStatusStopped {
		return repository.ErrCaptureTerminal
	}
	_, err = s.Refresh(ctx, resource)
	return err
}

func (s *Supervisor) Resume(ctx context.Context, task *models.TransferTask) error {
	resource, err := s.store.GetLatest(ctx, task.ID, task.TenantID)
	if err != nil {
		return err
	}
	if resource.Status == models.CaptureStatusStopped {
		return repository.ErrCaptureTerminal
	}
	status, err := s.connect.Status(ctx, resource.ConnectorName)
	if err != nil {
		s.fail(ctx, resource, err)
		return err
	}
	if status.ConnectorState == "PAUSED" {
		if err := s.connect.Resume(ctx, resource.ConnectorName); err != nil {
			s.fail(ctx, resource, err)
			return err
		}
		status, err = s.waitRunning(ctx, resource.ConnectorName)
		if err != nil {
			s.fail(ctx, resource, err)
			return err
		}
	}
	if !connectorHealthy(status) {
		err := fmt.Errorf("Kafka Connect connector is not healthy: connector=%s tasks=%v", status.ConnectorState, status.TaskStates)
		s.fail(ctx, resource, err)
		return err
	}
	return s.advance(ctx, resource, map[string]interface{}{
		"status": models.CaptureStatusRunning, "connector_status": status.ConnectorState,
		"connector_error": "", "last_observed_at": time.Now(),
	})
}

func (s *Supervisor) Stop(ctx context.Context, task *models.TransferTask) error {
	resource, err := s.store.GetLatest(ctx, task.ID, task.TenantID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if resource.Status == models.CaptureStatusStopped {
		return nil
	}
	if err := s.advance(ctx, resource, map[string]interface{}{"status": models.CaptureStatusCleaning}); err != nil {
		return err
	}
	plan, planErr := s.plans.ResolveForCleanup(ctx, task)
	connectorErr := s.connect.Delete(ctx, resource.ConnectorName)
	if connectorErr == nil {
		connectorErr = s.waitDeleted(ctx, resource.ConnectorName)
	}
	var sourceErr error
	if planErr == nil {
		sourceErr = s.dropSourceResources(ctx, plan, resource)
	} else {
		sourceErr = planErr
	}
	groupErr := s.topics.DeleteConsumerGroup(ctx, resource.ConsumerGroup)
	topicErr := s.topics.DeleteTopic(ctx, resource.TopicName)
	aclErr := s.topics.DeleteAccess(ctx, resource.TopicName, resource.ConsumerGroup)
	cleanupErr := errors.Join(connectorErr, sourceErr, groupErr, topicErr, aclErr)
	if cleanupErr != nil {
		s.cleanupFail(ctx, resource, cleanupErr)
		return cleanupErr
	}
	now := time.Now()
	return s.advance(ctx, resource, map[string]interface{}{
		"status": models.CaptureStatusStopped, "connector_status": "DELETED", "connector_error": "",
		"connector_created": false, "topic_created": false, "last_observed_at": now, "stopped_at": now,
	})
}

func (s *Supervisor) dropSourceResources(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	deadline := time.NewTimer(s.config.ProvisioningTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(s.config.StatusPollInterval)
	defer ticker.Stop()
	for {
		err := s.source.DropOwnedResources(ctx, plan, resource)
		if err == nil || !errors.Is(err, ErrSourceCaptureResourceActive) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return err
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) HasStopInitiatedGeneration(ctx context.Context, taskID, tenantID uint) (bool, error) {
	return s.store.HasStopInitiatedGeneration(ctx, taskID, tenantID)
}

func (s *Supervisor) Get(ctx context.Context, taskID, tenantID uint) (*models.CaptureResource, error) {
	return s.store.GetLatest(ctx, taskID, tenantID)
}

func (s *Supervisor) Refresh(ctx context.Context, resource *models.CaptureResource) (*ConnectorStatus, error) {
	status, err := s.connect.Status(ctx, resource.ConnectorName)
	if err != nil {
		s.fail(ctx, resource, err)
		return nil, err
	}
	state := models.CaptureStatusFailed
	connectorError := strings.TrimSpace(status.Error)
	if connectorHealthy(status) {
		state = models.CaptureStatusRunning
	} else if connectorError == "" {
		connectorError = fmt.Sprintf("connector=%s tasks=%v", status.ConnectorState, status.TaskStates)
	}
	if err := s.advance(ctx, resource, map[string]interface{}{
		"status": state, "connector_status": status.ConnectorState,
		"connector_error": connectorError, "last_observed_at": time.Now(),
	}); err != nil {
		return nil, err
	}
	if state != models.CaptureStatusRunning {
		return status, fmt.Errorf("Kafka Connect connector is not healthy: %s", connectorError)
	}
	return status, nil
}

func (s *Supervisor) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.config.MonitorInterval)
	defer ticker.Stop()
	for {
		if err := s.refreshAll(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Error("capture status refresh failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) refreshAll(ctx context.Context) error {
	resources, err := s.store.ListObservable(ctx, 100)
	if err != nil {
		return err
	}
	for i := range resources {
		if _, err := s.Refresh(ctx, &resources[i]); err != nil {
			s.logger.Warn("capture connector is unhealthy", "task_id", resources[i].TaskID, "connector", resources[i].ConnectorName, "error", err)
		}
	}
	return nil
}

func (s *Supervisor) waitRunning(ctx context.Context, name string) (*ConnectorStatus, error) {
	deadline := time.NewTimer(s.config.ProvisioningTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(s.config.StatusPollInterval)
	defer ticker.Stop()
	for {
		status, err := s.connect.Status(ctx, name)
		if err == nil && connectorHealthy(status) {
			return status, nil
		}
		if err == nil && (status.ConnectorState == "FAILED" || containsState(status.TaskStates, "FAILED")) {
			return nil, fmt.Errorf("Kafka Connect connector failed: %s", status.Error)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("timed out waiting for Kafka Connect connector %q to run", name)
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) waitDeleted(ctx context.Context, name string) error {
	deadline := time.NewTimer(s.config.ProvisioningTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(s.config.StatusPollInterval)
	defer ticker.Stop()
	for {
		_, err := s.connect.Status(ctx, name)
		if errors.Is(err, ErrConnectorNotFound) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for Kafka Connect connector %q deletion", name)
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) advance(ctx context.Context, resource *models.CaptureResource, fields map[string]interface{}) error {
	if err := s.store.Update(ctx, resource.ID, resource.ResourceVersion, fields); err != nil {
		return err
	}
	resource.ResourceVersion++
	if status, ok := fields["status"].(models.CaptureStatus); ok {
		resource.Status = status
	}
	return nil
}

func (s *Supervisor) fail(ctx context.Context, resource *models.CaptureResource, cause error) {
	if resource == nil || cause == nil {
		return
	}
	if err := s.store.Update(ctx, resource.ID, resource.ResourceVersion, map[string]interface{}{
		"status": models.CaptureStatusFailed, "connector_error": cause.Error(), "last_observed_at": time.Now(),
	}); err == nil {
		resource.ResourceVersion++
		resource.Status = models.CaptureStatusFailed
	}
}

func (s *Supervisor) cleanupFail(ctx context.Context, resource *models.CaptureResource, cause error) {
	if resource == nil || cause == nil {
		return
	}
	if err := s.store.Update(ctx, resource.ID, resource.ResourceVersion, map[string]interface{}{
		"status": models.CaptureStatusCleanupFailed, "connector_error": cause.Error(), "last_observed_at": time.Now(),
	}); err == nil {
		resource.ResourceVersion++
		resource.Status = models.CaptureStatusCleanupFailed
	}
}

func connectorHealthy(status *ConnectorStatus) bool {
	if status == nil || status.ConnectorState != "RUNNING" || len(status.TaskStates) != 1 {
		return false
	}
	return status.TaskStates[0] == "RUNNING"
}

func containsState(states []string, expected string) bool {
	for _, state := range states {
		if state == expected {
			return true
		}
	}
	return false
}
