package capture

import (
	"context"
	"encoding/json"
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
	TopicRetention               time.Duration
	TopicRetentionBytes          int64
	TopicReplication             int16
	ConnectLoopbackHost          string
	ConnectBootstrapServers      string
	ConnectKafkaUsername         string
	ConnectKafkaPassword         string
	ConnectKafkaSecurityProtocol string
	ConnectKafkaSASLMechanism    string
	ConnectKafkaTLSCACertFile    string
	ProvisioningTimeout          time.Duration
	StatusPollInterval           time.Duration
	MonitorInterval              time.Duration
	SourceProbeTimeout           time.Duration
}

func (c SupervisorConfig) Validate() error {
	if c.TopicRetention <= 0 || c.TopicReplication <= 0 || c.ProvisioningTimeout <= 0 || c.StatusPollInterval <= 0 || c.MonitorInterval <= 0 || c.SourceProbeTimeout <= 0 {
		return fmt.Errorf("capture supervisor durations and replication factor must be greater than zero")
	}
	return nil
}

// Supervisor 是数据库 CDC 捕获资源的唯一 lifecycle owner。
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
	if config.SourceProbeTimeout == 0 {
		config.SourceProbeTimeout = 3 * time.Second
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
		TaskID: task.ID, TenantID: task.TenantID, SourceType: plan.SourceType, SourceIdentity: plan.SourceIdentity,
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
	if err := s.source.EnsureOwnedResources(ctx, plan, resource); err != nil {
		s.fail(ctx, resource, err)
		return nil, err
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
	if resource.SourceType == models.CaptureSourceMySQL || resource.SourceType == models.CaptureSourceOracle {
		historyTopic, owned := captureSchemaHistoryTopic(resource)
		if strings.TrimSpace(historyTopic) == "" || !owned {
			err := fmt.Errorf("database capture generation is missing owned schema history topic")
			s.fail(ctx, resource, err)
			return nil, err
		}
		if err := s.topics.EnsureTopic(ctx, TopicSpec{
			Name: historyTopic, Partitions: 1,
			ReplicationFactor: s.config.TopicReplication, CleanupPolicy: "delete", RetentionMillis: -1,
		}); err != nil {
			s.fail(ctx, resource, err)
			return nil, err
		}
		if err := s.topics.EnsureSchemaHistoryAccess(ctx, historyTopic); err != nil {
			s.fail(ctx, resource, err)
			return nil, err
		}
	}
	if err := s.advance(ctx, resource, map[string]interface{}{"status": models.CaptureStatusProvisioning, "topic_created": true, "connector_error": ""}); err != nil {
		return nil, err
	}
	connectorConfig, err := buildConnectorConfig(plan, resource, s.config)
	if err != nil {
		s.fail(ctx, resource, err)
		return nil, err
	}
	if err := s.connect.PutConfig(ctx, resource.ConnectorName, connectorConfig); err != nil {
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
	if err := s.probeSource(ctx, plan, resource); err != nil {
		s.failSource(ctx, resource, status, err)
		return nil, err
	}
	now := time.Now()
	fields := map[string]interface{}{
		"status": models.CaptureStatusRunning, "connector_status": status.ConnectorState,
		"connector_error": "", "source_status": "ONLINE", "source_error": "", "last_observed_at": now,
	}
	s.observeSource(ctx, plan, resource, now, fields)
	if err := s.advance(ctx, resource, fields); err != nil {
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
		s.failConnector(ctx, resource, nil, err)
		return err
	}
	if status.ConnectorState == "PAUSED" {
		if err := s.connect.Resume(ctx, resource.ConnectorName); err != nil {
			s.failConnector(ctx, resource, status, err)
			return err
		}
		status, err = s.waitRunning(ctx, resource.ConnectorName)
		if err != nil {
			s.failConnector(ctx, resource, status, err)
			return err
		}
	}
	_, err = s.Refresh(ctx, resource)
	return err
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
	var historyTopicErr, historyACLErr error
	if historyTopic, owned := captureSchemaHistoryTopic(resource); owned && strings.TrimSpace(historyTopic) != "" {
		historyTopicErr = s.topics.DeleteTopic(ctx, historyTopic)
		historyACLErr = s.topics.DeleteSchemaHistoryAccess(ctx, historyTopic)
	}
	cleanupErr := errors.Join(connectorErr, sourceErr, groupErr, topicErr, aclErr, historyTopicErr, historyACLErr)
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

func captureSchemaHistoryTopic(resource *models.CaptureResource) (string, bool) {
	if resource == nil {
		return "", false
	}
	switch resource.SourceType {
	case models.CaptureSourceMySQL:
		if resource.MySQL != nil {
			return resource.MySQL.SchemaHistoryTopicName, resource.MySQL.SchemaHistoryTopicOwned
		}
	case models.CaptureSourceOracle:
		if resource.Oracle != nil {
			return resource.Oracle.SchemaHistoryTopicName, resource.Oracle.SchemaHistoryTopicOwned
		}
	}
	return "", false
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
		s.failConnector(ctx, resource, nil, err)
		return nil, err
	}
	connectorError := strings.TrimSpace(status.Error)
	if !connectorHealthy(status) {
		if connectorError == "" {
			connectorError = fmt.Sprintf("connector=%s tasks=%v", status.ConnectorState, status.TaskStates)
		}
		err := fmt.Errorf("Kafka Connect connector is not healthy: %s", connectorError)
		s.failConnector(ctx, resource, status, err)
		return status, err
	}
	plan, err := s.plans.ResolveForObservation(ctx, resource)
	if err != nil {
		s.failSource(ctx, resource, status, err)
		return status, err
	}
	if err := s.probeSource(ctx, plan, resource); err != nil {
		s.failSource(ctx, resource, status, err)
		return status, err
	}
	if connectorError == "" {
		connectorError = ""
	} else {
		connectorError = fmt.Sprintf("connector=%s tasks=%v", status.ConnectorState, status.TaskStates)
	}
	now := time.Now()
	fields := map[string]interface{}{
		"status": models.CaptureStatusRunning, "connector_status": status.ConnectorState,
		"connector_error": connectorError, "source_status": "ONLINE", "source_error": "", "last_observed_at": now,
	}
	s.observeSource(ctx, plan, resource, now, fields)
	if err := s.advance(ctx, resource, fields); err != nil {
		return nil, err
	}
	return status, nil
}

func (s *Supervisor) observeSource(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource, sampledAt time.Time, fields map[string]interface{}) {
	if plan == nil || resource == nil || plan.SourceType != models.CaptureSourceOracle {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, s.config.SourceProbeTimeout)
	defer cancel()
	offsets, offsetsErr := s.connect.Offsets(probeCtx, resource.ConnectorName)
	observation, err := s.source.Observe(probeCtx, plan, resource, offsets, sampledAt)
	if err != nil {
		markSourceObservationsUnavailable(resource, err, sampledAt, fields)
		s.logger.Warn("capture source observation failed", "task_id", resource.TaskID, "provider", plan.SourceType, "error", err)
		return
	}
	if observation == nil {
		observation = &SourceObservation{}
	}
	if offsetsErr != nil {
		observation.Recovery = nil
		observation.RecoveryError = offsetsErr
	}
	writeSourceRecoveryObservation(s, resource, observation.Recovery, observation.RecoveryError, sampledAt, fields)
	writeSourceTransactionsObservation(s, resource, observation.Transactions, observation.TransactionsError, sampledAt, fields)
}

func writeSourceRecoveryObservation(s *Supervisor, resource *models.CaptureResource, observation *models.CaptureSourceRecovery, observationErr error, sampledAt time.Time, fields map[string]interface{}) {
	if observationErr == nil && observation != nil {
		fields["source_recovery"] = sourceObservationMap(observation)
		fields["source_recovery_error"] = ""
		return
	}
	if observationErr == nil {
		observationErr = fmt.Errorf("capture source recovery observation returned no facts")
	}
	fields["source_recovery"] = sourceObservationMap(&models.CaptureSourceRecovery{
		SchemaVersion: "capture.source_recovery/v1", Provider: string(resource.SourceType), Health: "unknown", SampledAt: sampledAt,
	})
	fields["source_recovery_error"] = observationErr.Error()
	s.logger.Warn("capture source recovery observation failed", "task_id", resource.TaskID, "provider", resource.SourceType, "error", observationErr)
}

func writeSourceTransactionsObservation(s *Supervisor, resource *models.CaptureResource, observation *models.CaptureSourceTransactions, observationErr error, sampledAt time.Time, fields map[string]interface{}) {
	if observationErr == nil && observation != nil {
		fields["source_transactions"] = sourceObservationMap(observation)
		fields["source_transactions_error"] = ""
		return
	}
	if observationErr == nil {
		observationErr = fmt.Errorf("capture source transaction observation returned no facts")
	}
	fields["source_transactions"] = sourceObservationMap(&models.CaptureSourceTransactions{
		SchemaVersion: "capture.source_transactions/v1", Provider: string(resource.SourceType), Status: "unavailable", SampledAt: sampledAt,
	})
	fields["source_transactions_error"] = observationErr.Error()
	s.logger.Warn("capture source transaction observation failed", "task_id", resource.TaskID, "provider", resource.SourceType, "error", observationErr)
}

func sourceObservationMap(observation interface{}) models.JSONMap {
	if observation == nil {
		return models.JSONMap{}
	}
	data, err := json.Marshal(observation)
	if err != nil {
		return models.JSONMap{}
	}
	var result models.JSONMap
	if err := json.Unmarshal(data, &result); err != nil {
		return models.JSONMap{}
	}
	return result
}

func (s *Supervisor) probeSource(ctx context.Context, plan *CapturePlan, resource *models.CaptureResource) error {
	probeCtx, cancel := context.WithTimeout(ctx, s.config.SourceProbeTimeout)
	defer cancel()
	return s.source.Probe(probeCtx, plan, resource)
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
	fields := map[string]interface{}{
		"status": models.CaptureStatusFailed, "connector_error": cause.Error(), "last_observed_at": time.Now(),
	}
	markSourceObservationsUnavailable(resource, cause, time.Now(), fields)
	if err := s.store.Update(ctx, resource.ID, resource.ResourceVersion, fields); err == nil {
		resource.ResourceVersion++
		resource.Status = models.CaptureStatusFailed
	}
}

func (s *Supervisor) failConnector(ctx context.Context, resource *models.CaptureResource, status *ConnectorStatus, cause error) {
	if resource == nil || cause == nil {
		return
	}
	connectorStatus := "UNKNOWN"
	if status != nil && strings.TrimSpace(status.ConnectorState) != "" {
		connectorStatus = status.ConnectorState
	}
	fields := map[string]interface{}{
		"status": models.CaptureStatusFailed, "connector_status": connectorStatus, "connector_error": cause.Error(),
		"last_observed_at": time.Now(),
	}
	markSourceObservationsUnavailable(resource, cause, time.Now(), fields)
	s.failWithFields(ctx, resource, fields)
}

func (s *Supervisor) failSource(ctx context.Context, resource *models.CaptureResource, status *ConnectorStatus, cause error) {
	if resource == nil || cause == nil {
		return
	}
	connectorStatus := "UNKNOWN"
	if status != nil && strings.TrimSpace(status.ConnectorState) != "" {
		connectorStatus = status.ConnectorState
	}
	fields := map[string]interface{}{
		"status": models.CaptureStatusFailed, "connector_status": connectorStatus, "connector_error": "",
		"source_status": "OFFLINE", "source_error": cause.Error(), "last_observed_at": time.Now(),
	}
	markSourceObservationsUnavailable(resource, cause, time.Now(), fields)
	s.failWithFields(ctx, resource, fields)
}

func markSourceObservationsUnavailable(resource *models.CaptureResource, cause error, sampledAt time.Time, fields map[string]interface{}) {
	if resource == nil || resource.SourceType != models.CaptureSourceOracle || cause == nil {
		return
	}
	fields["source_recovery"] = sourceObservationMap(&models.CaptureSourceRecovery{
		SchemaVersion: "capture.source_recovery/v1", Provider: string(resource.SourceType), Health: "unknown", SampledAt: sampledAt,
	})
	fields["source_recovery_error"] = cause.Error()
	fields["source_transactions"] = sourceObservationMap(&models.CaptureSourceTransactions{
		SchemaVersion: "capture.source_transactions/v1", Provider: string(resource.SourceType), Status: "unavailable", SampledAt: sampledAt,
	})
	fields["source_transactions_error"] = cause.Error()
}

func (s *Supervisor) failWithFields(ctx context.Context, resource *models.CaptureResource, fields map[string]interface{}) {
	if err := s.store.Update(ctx, resource.ID, resource.ResourceVersion, fields); err == nil {
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
