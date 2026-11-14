package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAlertRuleInvalid  = errors.New("invalid alert rule")
	ErrAlertRuleNotFound = errors.New("alert rule not found")
	ErrAlertRuleConflict = errors.New("alert rule name already exists")
)

type AlertRuleService struct {
	db                 *gorm.DB
	alertService       *AlertService
	taskProviderLister taskProviderLister
}

func NewAlertRuleService(db *gorm.DB, alertService *AlertService, taskProviderLister taskProviderLister) *AlertRuleService {
	return &AlertRuleService{db: db, alertService: alertService, taskProviderLister: taskProviderLister}
}

type AlertRuleRouteInput struct {
	Channel       string `json:"channel"`
	DestinationID uint   `json:"destination_id"`
}

type CreateAlertRuleInput struct {
	TenantID         int
	Name             string
	Module           string
	TaskType         string
	SourceTaskID     string
	SourceTaskName   string
	RuleType         string
	FailureThreshold int
	BacklogThreshold string
	Severity         string
	Enabled          bool
	Routes           []AlertRuleRouteInput
}

type UpdateAlertRuleInput struct {
	TenantID         int
	ID               uint
	Name             *string
	Module           *string
	TaskType         *string
	SourceTaskID     *string
	SourceTaskName   *string
	RuleType         *string
	FailureThreshold *int
	BacklogThreshold *string
	Severity         *string
	Enabled          *bool
	Routes           *[]AlertRuleRouteInput
}

type AlertRuleTarget struct {
	Module                string `json:"module"`
	TaskType              string `json:"task_type"`
	SourceTaskID          string `json:"source_task_id"`
	CaptureDelayAvailable bool   `json:"capture_delay_available,omitempty"`
	CDC                   bool   `json:"cdc,omitempty"`
	SourceTaskName        string `json:"source_task_name,omitempty"`
}

func (s *AlertRuleService) List(ctx context.Context, tenantID int) ([]monitorModels.AlertRule, error) {
	var rules []monitorModels.AlertRule
	err := s.db.WithContext(ctx).Preload("Routes").Where("tenant_id = ?", tenantID).
		Order("created_at DESC, id DESC").Find(&rules).Error
	return rules, err
}

func (s *AlertRuleService) ListTargets(ctx context.Context, tenantID int) ([]AlertRuleTarget, error) {
	activeTaskTypes, err := s.activeTaskTypes()
	if err != nil {
		return nil, err
	}
	latestIDs := s.db.WithContext(ctx).Table("common.task_executions").Select("MAX(id)").Where("tenant_id = ? AND source_task_id IS NOT NULL AND source_task_id <> ''", tenantID).Where("parent_execution_id IS NULL").Group("module, task_type, source_task_id")
	var executions []commonExecution.TaskExecution
	if err := s.db.WithContext(ctx).Where("id IN (?)", latestIDs).Order("module,task_type,source_task_id").Find(&executions).Error; err != nil {
		return nil, err
	}
	filtered := []AlertRuleTarget{}
	for _, e := range executions {
		if activeTaskTypes.Contains(e.Module, e.TaskType) && !isContinuousExecution(e) {
			name := ""
			if e.SourceTaskName != nil {
				name = *e.SourceTaskName
			}
			filtered = append(filtered, AlertRuleTarget{Module: e.Module, TaskType: e.TaskType, SourceTaskID: *e.SourceTaskID, SourceTaskName: name})
		}
	}
	if activeTaskTypes.Contains("transfer", "sync") && s.alertService != nil && s.alertService.cdcRiskReader != nil {
		rows, err := s.currentCDCTargets(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			taskID := strconv.FormatUint(uint64(row.TaskID), 10)
			current := filtered[:0]
			for _, target := range filtered {
				if target.Module != "transfer" || target.TaskType != "sync" || target.SourceTaskID != taskID {
					current = append(current, target)
				}
			}
			filtered = current
			filtered = append(filtered, AlertRuleTarget{Module: "transfer", TaskType: "sync", SourceTaskID: strconv.FormatUint(uint64(row.TaskID), 10), SourceTaskName: row.TaskName, CDC: true, CaptureDelayAvailable: row.SourceType == "postgresql"})
		}
	}
	return filtered, nil
}

func (s *AlertRuleService) Create(ctx context.Context, input CreateAlertRuleInput) (*monitorModels.AlertRule, error) {
	normalized, routes, err := s.normalizeCreateInput(s.db.WithContext(ctx), input)
	if err != nil {
		return nil, err
	}
	var created monitorModels.AlertRule
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "tenant_id"}, {Name: "name"}}, DoNothing: true,
		}).Create(&normalized)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrAlertRuleConflict
		}
		if err := s.replaceRoutesTx(tx, normalized.TenantID, normalized.ID, routes); err != nil {
			return err
		}
		return tx.Preload("Routes").First(&created, normalized.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *AlertRuleService) Update(ctx context.Context, input UpdateAlertRuleInput, now time.Time) (*monitorModels.AlertRule, error) {
	var before monitorModels.AlertRule
	if err := s.db.WithContext(ctx).Where("id=? AND tenant_id=?", input.ID, input.TenantID).First(&before).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAlertRuleNotFound
		}
		return nil, err
	}
	code := before.RuleType
	if input.RuleType != nil {
		code = *input.RuleType
	}
	var cdcTargets []commonClient.TransferCDCRiskItem
	if monitorModels.IsCDCPerformanceRule(code) && (input.Module != nil || input.TaskType != nil || input.SourceTaskID != nil || input.RuleType != nil || input.Enabled != nil && *input.Enabled && !before.Enabled) {
		var err error
		cdcTargets, err = s.currentCDCTargets(ctx, input.TenantID)
		if err != nil {
			return nil, err
		}
	}
	var updated monitorModels.AlertRule
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rule monitorModels.AlertRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", input.ID, input.TenantID).First(&rule).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAlertRuleNotFound
			}
			return err
		}
		if noAlertRuleUpdates(input) {
			return ErrAlertRuleInvalid
		}
		next, routes, routesChanged, err := s.normalizeUpdateInput(tx, rule, input, cdcTargets)
		if err != nil {
			return err
		}
		var duplicateCount int64
		if err := tx.Model(&monitorModels.AlertRule{}).
			Where("tenant_id = ? AND name = ? AND id <> ?", input.TenantID, next.Name, input.ID).
			Count(&duplicateCount).Error; err != nil {
			return err
		}
		if duplicateCount > 0 {
			return ErrAlertRuleConflict
		}
		semanticChanged := rule.Module != next.Module || rule.TaskType != next.TaskType ||
			rule.SourceTaskID != next.SourceTaskID || rule.RuleType != next.RuleType ||
			rule.BacklogThreshold != next.BacklogThreshold || rule.FailureThreshold != next.FailureThreshold || (rule.Enabled && !next.Enabled)
		if semanticChanged {
			next.CDCWindow = monitorModels.CDCPerformanceWindow{ResetAt: &now}
			if err := s.alertService.resolveRuleIncidentsTx(tx, rule.ID, now); err != nil {
				return err
			}
		}
		windowJSON, err := json.Marshal(next.CDCWindow)
		if err != nil {
			return err
		}
		updates := map[string]interface{}{
			"name": next.Name, "module": next.Module, "task_type": next.TaskType,
			"source_task_id": next.SourceTaskID, "source_task_name": next.SourceTaskName,
			"rule_type": next.RuleType, "failure_threshold": next.FailureThreshold, "backlog_threshold": next.BacklogThreshold, "cdc_window": string(windowJSON),
			"severity": next.Severity, "enabled": next.Enabled, "updated_at": now,
		}
		if err := tx.Model(&monitorModels.AlertRule{}).Where("id = ?", rule.ID).Updates(updates).Error; err != nil {
			return err
		}
		if routesChanged {
			if err := s.replaceRoutesTx(tx, input.TenantID, rule.ID, routes); err != nil {
				return err
			}
		}
		if err := tx.Preload("Routes").First(&updated, rule.ID).Error; err != nil {
			return err
		}
		if rule.Name != updated.Name {
			return tx.Model(&monitorModels.AlertIncident{}).
				Where("alert_rule_id = ? AND status IN ?", rule.ID, []string{monitorModels.AlertStatusOpen, monitorModels.AlertStatusAcknowledged}).
				Update("rule_name", updated.Name).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *AlertRuleService) Delete(ctx context.Context, tenantID int, id uint, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rule monitorModels.AlertRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", id, tenantID).First(&rule).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAlertRuleNotFound
			}
			return err
		}
		if err := s.alertService.resolveRuleIncidentsTx(tx, rule.ID, now); err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND alert_rule_id = ?", tenantID, rule.ID).
			Delete(&monitorModels.NotificationRoute{}).Error; err != nil {
			return err
		}
		return tx.Delete(&rule).Error
	})
}

func (s *AlertRuleService) normalizeCreateInput(db *gorm.DB, input CreateAlertRuleInput) (monitorModels.AlertRule, []AlertRuleRouteInput, error) {
	name, module, taskType, sourceTaskID, sourceTaskName, ruleType, threshold, severity, err := normalizeAlertRuleFields(
		input.Name, input.Module, input.TaskType, input.SourceTaskID, input.SourceTaskName,
		input.RuleType, input.FailureThreshold, input.Severity,
	)
	if err != nil {
		return monitorModels.AlertRule{}, nil, err
	}
	if err := s.validateTypedAlertRuleTarget(db, input.TenantID, module, taskType, sourceTaskID, ruleType); err != nil {
		return monitorModels.AlertRule{}, nil, err
	}
	if err := normalizeCDCBacklog(ruleType, input.BacklogThreshold); err != nil {
		return monitorModels.AlertRule{}, nil, err
	}
	routes, err := s.normalizeRoutes(db, input.TenantID, input.Routes)
	if err != nil {
		return monitorModels.AlertRule{}, nil, err
	}
	window := monitorModels.CDCPerformanceWindow{}
	if monitorModels.IsCDCPerformanceRule(ruleType) {
		now := time.Now().UTC()
		window.ResetAt = &now
	}
	return monitorModels.AlertRule{
		CDCWindow: window,
		RuleID:    uuid.NewString(), TenantID: input.TenantID, Name: name, Module: module,
		TaskType: taskType, SourceTaskID: sourceTaskID, SourceTaskName: sourceTaskName,
		BacklogThreshold: input.BacklogThreshold, RuleType: ruleType, FailureThreshold: threshold, Severity: severity, Enabled: input.Enabled,
	}, routes, nil
}

func (s *AlertRuleService) normalizeUpdateInput(db *gorm.DB, rule monitorModels.AlertRule, input UpdateAlertRuleInput, cdcTargets []commonClient.TransferCDCRiskItem) (monitorModels.AlertRule, []AlertRuleRouteInput, bool, error) {
	next := rule
	if input.BacklogThreshold != nil {
		next.BacklogThreshold = *input.BacklogThreshold
	}
	if input.Name != nil {
		next.Name = *input.Name
	}
	if input.Module != nil {
		next.Module = *input.Module
	}
	if input.TaskType != nil {
		next.TaskType = *input.TaskType
	}
	if input.SourceTaskID != nil {
		next.SourceTaskID = *input.SourceTaskID
	}
	if input.SourceTaskName != nil {
		next.SourceTaskName = *input.SourceTaskName
	}
	if input.RuleType != nil {
		next.RuleType = *input.RuleType
	}
	if input.FailureThreshold != nil {
		next.FailureThreshold = *input.FailureThreshold
	}
	if input.Severity != nil {
		next.Severity = *input.Severity
	}
	if input.Enabled != nil {
		next.Enabled = *input.Enabled
	}
	name, module, taskType, sourceTaskID, sourceTaskName, ruleType, threshold, severity, err := normalizeAlertRuleFields(
		next.Name, next.Module, next.TaskType, next.SourceTaskID, next.SourceTaskName,
		next.RuleType, next.FailureThreshold, next.Severity,
	)
	if err != nil {
		return monitorModels.AlertRule{}, nil, false, err
	}
	next.Name, next.Module, next.TaskType, next.SourceTaskID = name, module, taskType, sourceTaskID
	next.SourceTaskName, next.RuleType, next.FailureThreshold, next.Severity = sourceTaskName, ruleType, threshold, severity
	targetChanged := rule.Module != module || rule.TaskType != taskType || rule.SourceTaskID != sourceTaskID
	if err := normalizeCDCBacklog(ruleType, next.BacklogThreshold); err != nil {
		return monitorModels.AlertRule{}, nil, false, err
	}
	if targetChanged || rule.RuleType != ruleType || (!rule.Enabled && next.Enabled) {
		if monitorModels.IsCDCPerformanceRule(ruleType) {
			if !validCDCTarget(cdcTargets, sourceTaskID, ruleType) {
				return monitorModels.AlertRule{}, nil, false, ErrAlertRuleInvalid
			}
		} else if err := s.validateAlertRuleTarget(db, input.TenantID, module, taskType, sourceTaskID); err != nil {
			return monitorModels.AlertRule{}, nil, false, err
		}
	}
	if input.Routes == nil {
		return next, nil, false, nil
	}
	routes, err := s.normalizeRoutes(db, input.TenantID, *input.Routes)
	return next, routes, true, err
}

func (s *AlertRuleService) normalizeRoutes(db *gorm.DB, tenantID int, inputs []AlertRuleRouteInput) ([]AlertRuleRouteInput, error) {
	seen := make(map[string]struct{}, len(inputs))
	result := make([]AlertRuleRouteInput, 0, len(inputs))
	for _, route := range inputs {
		route.Channel = strings.TrimSpace(route.Channel)
		if route.DestinationID == 0 || (route.Channel != monitorModels.NotificationChannelWebhook && route.Channel != monitorModels.NotificationChannelEmail) {
			return nil, ErrAlertRuleInvalid
		}
		key := fmt.Sprintf("%s:%d", route.Channel, route.DestinationID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		var count int64
		var model interface{}
		if route.Channel == monitorModels.NotificationChannelWebhook {
			model = &monitorModels.WebhookDestination{}
		} else {
			model = &monitorModels.EmailDestination{}
		}
		if err := db.Model(model).Where("id = ? AND tenant_id = ?", route.DestinationID, tenantID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != 1 {
			return nil, ErrAlertRuleInvalid
		}
		result = append(result, route)
	}
	return result, nil
}

func (s *AlertRuleService) replaceRoutesTx(tx *gorm.DB, tenantID int, ruleID uint, routes []AlertRuleRouteInput) error {
	if err := tx.Where("tenant_id = ? AND alert_rule_id = ?", tenantID, ruleID).
		Delete(&monitorModels.NotificationRoute{}).Error; err != nil {
		return err
	}
	for _, route := range routes {
		if err := tx.Create(&monitorModels.NotificationRoute{
			TenantID: tenantID, AlertRuleID: ruleID, Channel: route.Channel, DestinationID: route.DestinationID,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func normalizeAlertRuleFields(name, module, taskType, sourceTaskID, sourceTaskName, ruleType string, threshold int, severity string) (string, string, string, string, string, string, int, string, error) {
	name, module, taskType = strings.TrimSpace(name), strings.TrimSpace(module), strings.TrimSpace(taskType)
	sourceTaskID, sourceTaskName = strings.TrimSpace(sourceTaskID), strings.TrimSpace(sourceTaskName)
	ruleType, severity = strings.TrimSpace(ruleType), strings.TrimSpace(severity)
	if name == "" || len(name) > 100 || module == "" || len(module) > 50 || taskType == "" || len(taskType) > 100 ||
		sourceTaskID == "" || len(sourceTaskID) > 255 || len(sourceTaskName) > 255 {
		return "", "", "", "", "", "", 0, "", ErrAlertRuleInvalid
	}
	if severity != monitorModels.AlertSeverityWarning && severity != monitorModels.AlertSeverityCritical {
		return "", "", "", "", "", "", 0, "", ErrAlertRuleInvalid
	}
	switch ruleType {
	case monitorModels.AlertRuleCDCStalled, monitorModels.AlertRuleCDCDelayed, monitorModels.AlertRuleCDCBacklog:
		if module != "transfer" || taskType != "sync" || severity != "warning" {
			return "", "", "", "", "", "", 0, "", ErrAlertRuleInvalid
		}
		threshold = 1
	case monitorModels.AlertRuleLastTerminalFailed, monitorModels.AlertRuleLastTerminalTimeout:
		threshold = 1
	case monitorModels.AlertRuleConsecutiveFailures:
		if threshold < 2 || threshold > 20 {
			return "", "", "", "", "", "", 0, "", ErrAlertRuleInvalid
		}
	default:
		return "", "", "", "", "", "", 0, "", ErrAlertRuleInvalid
	}
	return name, module, taskType, sourceTaskID, sourceTaskName, ruleType, threshold, severity, nil
}

func noAlertRuleUpdates(input UpdateAlertRuleInput) bool {
	return input.Name == nil && input.Module == nil && input.TaskType == nil && input.SourceTaskID == nil &&
		input.SourceTaskName == nil && input.RuleType == nil && input.BacklogThreshold == nil && input.FailureThreshold == nil &&
		input.Severity == nil && input.Enabled == nil && input.Routes == nil
}

func (s *AlertRuleService) validateAlertRuleTarget(db *gorm.DB, tenantID int, module, taskType, sourceTaskID string) error {
	activeTaskTypes, err := s.activeTaskTypes()
	if err != nil {
		return err
	}
	if !activeTaskTypes.Contains(module, taskType) {
		return ErrAlertRuleInvalid
	}
	var count int64
	latestID := db.Table("common.task_executions").Select("MAX(id)").
		Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?", tenantID, module, taskType, sourceTaskID).
		Where("parent_execution_id IS NULL")
	err = db.Table("common.task_executions").
		Where("id = (?)", latestID).Where(nonContinuousExecutionCondition(db)).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrAlertRuleInvalid
	}
	return nil
}

type activeTaskTypeSet map[string]struct{}

func (s *AlertRuleService) activeTaskTypes() (activeTaskTypeSet, error) {
	if s.taskProviderLister == nil {
		return nil, fmt.Errorf("task provider lister is required")
	}
	providers, err := s.taskProviderLister.ListTaskProviders()
	if err != nil {
		return nil, fmt.Errorf("list task providers: %w", err)
	}
	result := make(activeTaskTypeSet)
	for _, provider := range providers {
		if provider == nil || !provider.Enabled {
			continue
		}
		capabilities, err := parseActiveTaskProviderCapabilities(provider)
		if err != nil {
			return nil, err
		}
		for _, capability := range capabilities.TaskCapabilities {
			if !capability.Deprecated {
				result[result.Key(provider.ModuleName, capability.Type)] = struct{}{}
			}
		}
	}
	return result, nil
}

func parseActiveTaskProviderCapabilities(provider *commonModels.TaskProvider) (*taskprovider.Capabilities, error) {
	if provider.Capabilities == nil {
		return nil, fmt.Errorf("task provider %q capabilities are required", provider.ModuleName)
	}
	capabilities, err := taskprovider.ParseCapabilities(string(*provider.Capabilities))
	if err != nil {
		return nil, fmt.Errorf("parse task provider %q capabilities: %w", provider.ModuleName, err)
	}
	return capabilities, nil
}

func (s activeTaskTypeSet) Contains(module, taskType string) bool {
	_, ok := s[s.Key(module, taskType)]
	return ok
}

func (activeTaskTypeSet) Key(module, taskType string) string {
	return strings.TrimSpace(module) + "\x00" + strings.TrimSpace(taskType)
}

func nonContinuousExecutionCondition(db *gorm.DB) string {
	if db.Dialector.Name() == "postgres" {
		return "NOT (COALESCE(metadata, '{}'::jsonb) ? 'continuous')"
	}
	return "json_type(metadata, '$.continuous') IS NULL"
}

func isContinuousExecution(execution commonExecution.TaskExecution) bool {
	if execution.Metadata == nil {
		return false
	}
	_, hasContinuous := execution.Metadata["continuous"]
	return hasContinuous
}

func normalizeCDCBacklog(code, value string) error {
	if code != monitorModels.AlertRuleCDCBacklog {
		if value != "" {
			return ErrAlertRuleInvalid
		}
		return nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != value {
		return ErrAlertRuleInvalid
	}
	return nil
}
func (s *AlertRuleService) validateTypedAlertRuleTarget(db *gorm.DB, tenant int, module, taskType, taskID, code string) error {
	if !monitorModels.IsCDCPerformanceRule(code) {
		return s.validateAlertRuleTarget(db, tenant, module, taskType, taskID)
	}
	types, err := s.activeTaskTypes()
	if err != nil {
		return err
	}
	if !types.Contains(module, taskType) {
		return ErrAlertRuleInvalid
	}
	rows, err := s.currentCDCTargets(db.Statement.Context, tenant)
	if err != nil {
		return err
	}
	if validCDCTarget(rows, taskID, code) {
		return nil
	}
	return ErrAlertRuleInvalid
}

func (s *AlertRuleService) currentCDCTargets(ctx context.Context, tenant int) ([]commonClient.TransferCDCRiskItem, error) {
	if s.alertService == nil || s.alertService.cdcRiskReader == nil {
		return nil, commonClient.ErrExecutionOwnerUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	targets := []commonClient.TransferCDCRiskItem{}
	var after uint
	for {
		page, err := s.alertService.cdcRiskReader.ReadCDCRisks(ctx, after)
		if err != nil {
			return nil, commonClient.ErrExecutionOwnerUnavailable
		}
		for _, row := range page.Items {
			if row.TenantID == uint(tenant) && row.Generation > 0 && row.State != "closed" {
				targets = append(targets, row)
			}
		}
		if !page.HasMore {
			break
		}
		after = page.NextAfterID
	}
	return targets, nil
}

func validCDCTarget(rows []commonClient.TransferCDCRiskItem, taskID, code string) bool {
	for _, row := range rows {
		if strconv.FormatUint(uint64(row.TaskID), 10) == taskID && (code != monitorModels.AlertRuleCDCDelayed || row.SourceType == "postgresql") {
			return true
		}
	}
	return false
}
