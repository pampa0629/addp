package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrAlertNotActive = errors.New("alert incident is not active")

var supportedAlertCodes = []string{
	"recovery_circuit_open", "recovery_half_open", "recovery_waiting", "recovery_ready",
	"retention_critical", "retention_degraded", "checkpoint_stalled", "diagnostics_error", "schema_change_blocked",
	"source_recovery_critical", "source_recovery_unavailable", "source_transactions_unavailable",
}

type observationSignal struct {
	Code     string
	Severity string
	Details  commonModels.JSONMap
}

type activeAlertSignal struct {
	TenantID      int
	Module        string
	TaskType      string
	SourceTaskID  string
	ExecutionID   string
	AlertRuleID   *uint
	RuleID        string
	RuleName      string
	RuleThreshold int
	Code          string
	Severity      string
	Details       commonModels.JSONMap
	Fingerprint   string
}

type AlertService struct {
	db                  *gorm.DB
	notificationService *NotificationService
}

func NewAlertService(db *gorm.DB, notificationService *NotificationService) *AlertService {
	return &AlertService{db: db, notificationService: notificationService}
}

type ListAlertsRequest struct {
	TenantID int
	Status   string
	Severity string
	Module   string
	Page     int
	PageSize int
}

type ListAlertsResponse struct {
	Data       []monitorModels.AlertIncident `json:"data"`
	Total      int64                         `json:"total"`
	Page       int                           `json:"page"`
	PageSize   int                           `json:"page_size"`
	TotalPages int                           `json:"total_pages"`
}

func (s *AlertService) Evaluate(ctx context.Context, now time.Time) error {
	continuousSignals, err := s.evaluateContinuousSignals(ctx, now)
	if err != nil {
		return err
	}
	ruleSignals, err := s.evaluateRuleSignals(ctx)
	if err != nil {
		return err
	}
	signals := append(continuousSignals, ruleSignals...)
	return s.reconcileSignals(ctx, signals, now)
}

func (s *AlertService) evaluateContinuousSignals(ctx context.Context, now time.Time) ([]activeAlertSignal, error) {
	latestIDs := s.db.WithContext(ctx).Table("common.task_executions").Select("MAX(id)").
		Where("module = ? AND task_type = ?", commonExecution.ModuleTransfer, commonExecution.TaskTypeSync).
		Where("source_task_id IS NOT NULL AND source_task_id <> ''").
		Where("parent_execution_id IS NULL").
		Group("tenant_id, module, task_type, source_task_id")
	var executions []commonExecution.TaskExecution
	err := s.db.WithContext(ctx).
		Where("id IN (?)", latestIDs).
		Order("id DESC").Find(&executions).Error
	if err != nil {
		return nil, err
	}
	signals := make([]activeAlertSignal, 0)
	for i := range executions {
		if executions[i].SourceTaskID == nil || !isContinuousExecution(executions[i]) {
			continue
		}
		for _, observation := range deriveObservationSignals(executions[i], now) {
			if executions[i].Status == commonExecution.ExecutionStatusFailed && observation.Code != "schema_change_blocked" {
				continue
			}
			if executions[i].Status != commonExecution.ExecutionStatusPending &&
				executions[i].Status != commonExecution.ExecutionStatusRunning &&
				executions[i].Status != commonExecution.ExecutionStatusFailed {
				continue
			}
			signals = append(signals, activeAlertSignal{
				TenantID: executions[i].TenantID, Module: executions[i].Module, TaskType: executions[i].TaskType,
				SourceTaskID: *executions[i].SourceTaskID, ExecutionID: executions[i].ExecutionID,
				Code: observation.Code, Severity: observation.Severity, Details: observation.Details,
				Fingerprint: alertFingerprint(executions[i], observation.Code),
			})
		}
	}
	return signals, nil
}

func (s *AlertService) evaluateRuleSignals(ctx context.Context) ([]activeAlertSignal, error) {
	var rules []monitorModels.AlertRule
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&rules).Error; err != nil {
		return nil, err
	}
	signals := make([]activeAlertSignal, 0, len(rules))
	for i := range rules {
		rule := rules[i]
		var latestExecution commonExecution.TaskExecution
		err := s.db.WithContext(ctx).
			Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?", rule.TenantID, rule.Module, rule.TaskType, rule.SourceTaskID).
			Where("parent_execution_id IS NULL").Order("id DESC").First(&latestExecution).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if isContinuousExecution(latestExecution) {
			continue
		}
		limit := 1
		if rule.RuleType == monitorModels.AlertRuleConsecutiveFailures {
			limit = rule.FailureThreshold
		}
		var executions []commonExecution.TaskExecution
		query := s.db.WithContext(ctx).
			Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?", rule.TenantID, rule.Module, rule.TaskType, rule.SourceTaskID).
			Where("parent_execution_id IS NULL").
			Where("status IN ?", []string{commonExecution.ExecutionStatusSuccess, commonExecution.ExecutionStatusFailed, commonExecution.ExecutionStatusTimeout}).
			Order("id DESC").Limit(limit)
		query = query.Where(nonContinuousExecutionCondition(s.db))
		if err := query.Find(&executions).Error; err != nil {
			return nil, err
		}
		signal, active := deriveRuleSignal(rule, executions)
		if active {
			signals = append(signals, signal)
		}
	}
	return signals, nil
}

func deriveRuleSignal(rule monitorModels.AlertRule, executions []commonExecution.TaskExecution) (activeAlertSignal, bool) {
	if len(executions) == 0 {
		return activeAlertSignal{}, false
	}
	latest := executions[0]
	active := false
	details := commonModels.JSONMap{"latest_status": latest.Status}
	switch rule.RuleType {
	case monitorModels.AlertRuleLastTerminalFailed:
		active = latest.Status == commonExecution.ExecutionStatusFailed
	case monitorModels.AlertRuleLastTerminalTimeout:
		active = latest.Status == commonExecution.ExecutionStatusTimeout
	case monitorModels.AlertRuleConsecutiveFailures:
		if len(executions) < rule.FailureThreshold {
			return activeAlertSignal{}, false
		}
		active = true
		for _, execution := range executions {
			if execution.Status == commonExecution.ExecutionStatusSuccess {
				active = false
				break
			}
		}
		details["failure_count"] = len(executions)
		details["failure_threshold"] = rule.FailureThreshold
	}
	if !active {
		return activeAlertSignal{}, false
	}
	ruleID := rule.ID
	return activeAlertSignal{
		TenantID: rule.TenantID, Module: rule.Module, TaskType: rule.TaskType,
		SourceTaskID: rule.SourceTaskID, ExecutionID: latest.ExecutionID,
		AlertRuleID: &ruleID, RuleID: rule.RuleID, RuleName: rule.Name, RuleThreshold: rule.FailureThreshold,
		Code: rule.RuleType, Severity: rule.Severity, Details: details,
		Fingerprint: ruleAlertFingerprint(rule),
	}, true
}

func (s *AlertService) reconcileSignals(ctx context.Context, signals []activeAlertSignal, now time.Time) error {
	sort.Slice(signals, func(i, j int) bool {
		if signals[i].AlertRuleID == nil {
			return signals[j].AlertRuleID != nil
		}
		if signals[j].AlertRuleID == nil {
			return false
		}
		return *signals[i].AlertRuleID < *signals[j].AlertRuleID
	})
	activeFingerprints := make([]string, 0, len(signals))
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, signal := range signals {
			if signal.AlertRuleID != nil {
				validated, active, err := validateRuleSignalTx(tx, signal)
				if err != nil {
					return err
				}
				if !active {
					continue
				}
				signal = validated
			}
			activeFingerprints = append(activeFingerprints, signal.Fingerprint)
			if err := s.upsertActiveAlert(tx, signal, now); err != nil {
				return err
			}
		}
		query := tx.Model(&monitorModels.AlertIncident{}).
			Where("status IN ?", []string{monitorModels.AlertStatusOpen, monitorModels.AlertStatusAcknowledged}).
			Where("alert_rule_id IS NOT NULL OR signal_code IN ?", supportedAlertCodes)
		if len(activeFingerprints) > 0 {
			query = query.Where("fingerprint NOT IN ?", activeFingerprints)
		}
		var incidents []monitorModels.AlertIncident
		if err := query.Find(&incidents).Error; err != nil {
			return err
		}
		for _, incident := range incidents {
			if err := s.resolveIncidentTx(tx, incident, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func validateRuleSignalTx(tx *gorm.DB, signal activeAlertSignal) (activeAlertSignal, bool, error) {
	var rule monitorModels.AlertRule
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&rule, *signal.AlertRuleID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return activeAlertSignal{}, false, nil
	}
	if err != nil {
		return activeAlertSignal{}, false, err
	}
	if !rule.Enabled || rule.RuleID != signal.RuleID || rule.TenantID != signal.TenantID ||
		rule.Module != signal.Module || rule.TaskType != signal.TaskType || rule.SourceTaskID != signal.SourceTaskID ||
		rule.RuleType != signal.Code || rule.FailureThreshold != signal.RuleThreshold {
		return activeAlertSignal{}, false, nil
	}
	signal.RuleName = rule.Name
	signal.Severity = rule.Severity
	signal.Fingerprint = ruleAlertFingerprint(rule)
	return signal, true, nil
}

func (s *AlertService) upsertActiveAlert(tx *gorm.DB, signal activeAlertSignal, now time.Time) error {
	result := tx.Exec(`INSERT INTO monitor.alert_incidents
		(tenant_id, module, task_type, source_task_id, execution_id, alert_rule_id, rule_id, rule_name, signal_code, fingerprint, severity, status, details, opened_at, last_observed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?, ?, ?, ?)
		ON CONFLICT (fingerprint) WHERE status IN ('open', 'acknowledged')
		DO NOTHING`,
		signal.TenantID, signal.Module, signal.TaskType, signal.SourceTaskID, signal.ExecutionID,
		signal.AlertRuleID, signal.RuleID, signal.RuleName, signal.Code, signal.Fingerprint, signal.Severity,
		signal.Details, now, now, now, now,
	)
	if result.Error != nil {
		return result.Error
	}
	var incident monitorModels.AlertIncident
	if err := tx.Where("fingerprint = ? AND status IN ?", signal.Fingerprint, []string{monitorModels.AlertStatusOpen, monitorModels.AlertStatusAcknowledged}).First(&incident).Error; err != nil {
		return err
	}
	if result.RowsAffected == 1 {
		if s.notificationService != nil {
			return s.notificationService.RecordAlertEventTx(tx, incident, monitorModels.AlertEventOpened, "", now)
		}
		return nil
	}

	previousSeverity := incident.Severity
	updates := map[string]interface{}{
		"execution_id": signal.ExecutionID, "rule_name": signal.RuleName, "severity": signal.Severity, "details": signal.Details,
		"last_observed_at": now, "updated_at": now,
	}
	if err := tx.Model(&monitorModels.AlertIncident{}).Where("id = ?", incident.ID).Updates(updates).Error; err != nil {
		return err
	}
	incident.ExecutionID = signal.ExecutionID
	incident.RuleName = signal.RuleName
	incident.Severity = signal.Severity
	incident.Details = signal.Details
	incident.LastObservedAt = now
	incident.UpdatedAt = now
	if previousSeverity == monitorModels.AlertSeverityWarning && signal.Severity == monitorModels.AlertSeverityCritical && s.notificationService != nil {
		return s.notificationService.RecordAlertEventTx(tx, incident, monitorModels.AlertEventEscalated, previousSeverity, now)
	}
	return nil
}

func (s *AlertService) resolveRuleIncidentsTx(tx *gorm.DB, alertRuleID uint, now time.Time) error {
	var incidents []monitorModels.AlertIncident
	if err := tx.Where("alert_rule_id = ? AND status IN ?", alertRuleID, []string{monitorModels.AlertStatusOpen, monitorModels.AlertStatusAcknowledged}).Find(&incidents).Error; err != nil {
		return err
	}
	for _, incident := range incidents {
		if err := s.resolveIncidentTx(tx, incident, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *AlertService) resolveIncidentTx(tx *gorm.DB, incident monitorModels.AlertIncident, now time.Time) error {
	result := tx.Model(&monitorModels.AlertIncident{}).
		Where("id = ? AND status IN ?", incident.ID, []string{monitorModels.AlertStatusOpen, monitorModels.AlertStatusAcknowledged}).
		Updates(map[string]interface{}{"status": monitorModels.AlertStatusResolved, "resolved_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return nil
	}
	incident.Status = monitorModels.AlertStatusResolved
	incident.ResolvedAt = &now
	incident.UpdatedAt = now
	if s.notificationService != nil {
		return s.notificationService.RecordAlertEventTx(tx, incident, monitorModels.AlertEventResolved, incident.Severity, now)
	}
	return nil
}

func (s *AlertService) List(ctx context.Context, req ListAlertsRequest) (*ListAlertsResponse, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 || req.PageSize > 100 {
		req.PageSize = 20
	}
	query := s.db.WithContext(ctx).Model(&monitorModels.AlertIncident{}).Where("tenant_id = ?", req.TenantID)
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}
	if req.Severity != "" {
		query = query.Where("severity = ?", req.Severity)
	}
	if req.Module != "" {
		query = query.Where("module = ?", req.Module)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var alerts []monitorModels.AlertIncident
	if err := query.Order("opened_at DESC, id DESC").Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize).Find(&alerts).Error; err != nil {
		return nil, err
	}
	return &ListAlertsResponse{Data: alerts, Total: total, Page: req.Page, PageSize: req.PageSize, TotalPages: int((total + int64(req.PageSize) - 1) / int64(req.PageSize))}, nil
}

func (s *AlertService) Acknowledge(ctx context.Context, id uint, tenantID int, actor string, now time.Time) (*monitorModels.AlertIncident, error) {
	result := s.db.WithContext(ctx).Model(&monitorModels.AlertIncident{}).
		Where("id = ? AND tenant_id = ? AND status = ?", id, tenantID, monitorModels.AlertStatusOpen).
		Updates(map[string]interface{}{"status": monitorModels.AlertStatusAcknowledged, "acknowledged_at": now, "acknowledged_by": actor, "updated_at": now})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrAlertNotActive
	}
	var alert monitorModels.AlertIncident
	return &alert, s.db.WithContext(ctx).First(&alert, "id = ? AND tenant_id = ?", id, tenantID).Error
}

func (s *AlertService) Suppress(ctx context.Context, id uint, tenantID int, until time.Time) (*monitorModels.AlertIncident, error) {
	result := s.db.WithContext(ctx).Model(&monitorModels.AlertIncident{}).
		Where("id = ? AND tenant_id = ? AND status IN ?", id, tenantID, []string{monitorModels.AlertStatusOpen, monitorModels.AlertStatusAcknowledged}).
		Updates(map[string]interface{}{"suppressed_until": until, "updated_at": time.Now()})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrAlertNotActive
	}
	var alert monitorModels.AlertIncident
	return &alert, s.db.WithContext(ctx).First(&alert, "id = ? AND tenant_id = ?", id, tenantID).Error
}

func deriveObservationSignals(execution commonExecution.TaskExecution, now time.Time) []observationSignal {
	metadata := map[string]interface{}(execution.Metadata)
	continuous, _ := metadata["continuous"].(map[string]interface{})
	diagnostics, _ := continuous["diagnostics"].(map[string]interface{})
	signals := make([]observationSignal, 0, 7)
	circuit, _ := metadata["recovery_circuit_state"].(string)
	notBefore, _ := metadata["recovery_not_before"].(string)
	if circuit == "open" {
		signals = append(signals, observationSignal{"recovery_circuit_open", monitorModels.AlertSeverityCritical, commonModels.JSONMap{"not_before": notBefore}})
	} else if circuit == "half_open" {
		signals = append(signals, observationSignal{"recovery_half_open", monitorModels.AlertSeverityWarning, commonModels.JSONMap{}})
	} else if execution.Status == commonExecution.ExecutionStatusPending && notBefore != "" {
		parsed, err := time.Parse(time.RFC3339Nano, notBefore)
		code := "recovery_ready"
		if err == nil && parsed.After(now) {
			code = "recovery_waiting"
		}
		signals = append(signals, observationSignal{code, monitorModels.AlertSeverityWarning, commonModels.JSONMap{"not_before": notBefore}})
	}
	if health, _ := diagnostics["health"].(string); health == "critical" {
		signals = append(signals, observationSignal{"retention_critical", monitorModels.AlertSeverityCritical, commonModels.JSONMap{"health": health}})
	} else if health == "degraded" {
		signals = append(signals, observationSignal{"retention_degraded", monitorModels.AlertSeverityWarning, commonModels.JSONMap{"health": health}})
	}
	if health, _ := diagnostics["checkpoint_health"].(string); health == "degraded" {
		signals = append(signals, observationSignal{"checkpoint_stalled", monitorModels.AlertSeverityWarning, commonModels.JSONMap{"health": health}})
	}
	if message, _ := diagnostics["error"].(string); message != "" {
		signals = append(signals, observationSignal{"diagnostics_error", monitorModels.AlertSeverityWarning, commonModels.JSONMap{"error": message}})
	}
	capture, _ := continuous["capture"].(map[string]interface{})
	generation := capture["generation"]
	sourceRecovery, _ := capture["source_recovery"].(map[string]interface{})
	if health, _ := sourceRecovery["health"].(string); health == "critical" {
		signals = append(signals, observationSignal{"source_recovery_critical", monitorModels.AlertSeverityCritical, captureObservationDetails(generation, sourceRecovery, []string{
			"schema_version", "provider", "health", "capture_position", "current_position", "earliest_available_position",
			"position_headroom", "earliest_available_at", "window_seconds", "fra_used_percent", "fra_reclaimable_percent", "sampled_at",
		})})
	} else if health == "unknown" {
		signals = append(signals, observationSignal{"source_recovery_unavailable", monitorModels.AlertSeverityWarning, captureObservationDetails(generation, sourceRecovery, []string{
			"schema_version", "provider", "health", "sampled_at",
		})})
	}
	sourceTransactions, _ := capture["source_transactions"].(map[string]interface{})
	if status, _ := sourceTransactions["status"].(string); status == "unavailable" {
		signals = append(signals, observationSignal{"source_transactions_unavailable", monitorModels.AlertSeverityWarning, captureObservationDetails(generation, sourceTransactions, []string{
			"schema_version", "provider", "status", "sampled_at",
		})})
	}
	schemaChange, _ := continuous["schema_change"].(map[string]interface{})
	if status, _ := schemaChange["status"].(string); status == "pending" {
		details := commonModels.JSONMap{}
		for _, key := range []string{"request_id", "generation", "from_revision", "to_revision", "detected_at", "scope", "source_partition", "source_offset", "missing_fields", "unexpected_fields", "incompatible_fields"} {
			if value, exists := schemaChange[key]; exists {
				details[key] = value
			}
		}
		signals = append(signals, observationSignal{"schema_change_blocked", monitorModels.AlertSeverityCritical, details})
	}
	return signals
}

func captureObservationDetails(generation interface{}, facts map[string]interface{}, allowed []string) commonModels.JSONMap {
	details := commonModels.JSONMap{}
	if generation != nil {
		details["generation"] = generation
	}
	for _, key := range allowed {
		if value, exists := facts[key]; exists {
			details[key] = value
		}
	}
	return details
}

func alertFingerprint(execution commonExecution.TaskExecution, code string) string {
	value := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s", execution.TenantID, execution.Module, execution.TaskType, *execution.SourceTaskID, code)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ruleAlertFingerprint(rule monitorModels.AlertRule) string {
	value := fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%s", rule.RuleID, rule.TenantID, rule.Module, rule.TaskType, rule.SourceTaskID)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
