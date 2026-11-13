package service

import (
	"context"
	"strings"

	commonclient "github.com/addp/common/client"

	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
)

func (s *AlertService) visibleAlertQuery(ctx context.Context, query *gorm.DB, tenantID int) *gorm.DB {
	scopes, exists := execution.ReadScopesFromContext(ctx)
	if !exists {
		return query
	}
	visible := execution.ApplyReadScopes(ctx, s.db.WithContext(ctx).Model(&execution.TaskExecution{}).Where("tenant_id = ?", tenantID)).Select("execution_id")
	taskScopes := make([]execution.ReadScope, 0, len(scopes))
	for _, scope := range scopes {
		taskScope := scope
		taskScope.Grants = nil
		for _, grant := range scope.Grants {
			if grant.TaskHistory {
				taskScope.Grants = append(taskScope.Grants, execution.ReadGrant{TaskType: grant.TaskType, TaskHistory: true})
			}
		}
		taskScopes = append(taskScopes, taskScope)
	}
	predicate, args := execution.ReadPredicate(execution.WithReadScopes(ctx, taskScopes))
	taskRisk := s.db.WithContext(ctx).Model(&monitorModels.AlertIncident{}).Where("tenant_id = ? AND signal_code IN ?", tenantID, cdcRiskCodes).Where(predicate, args...).Select("id")
	return query.Where("execution_id IN (?) OR id IN (?)", visible, taskRisk)
}

// Task risks use the same Owner TaskHistory grant even after execution retention.
func (s *AlertService) AuthorizeReadRequest(ctx context.Context, query *ExecutionQueryService, tenantID int, principalID int64, bearer, module string) (context.Context, error) {
	owners, err := query.repo.ListOwnerModules(ctx, tenantID)
	if err != nil {
		return nil, commonclient.ErrExecutionOwnerUnavailable
	}
	var riskOwners []string
	if err = s.db.WithContext(ctx).Model(&monitorModels.AlertIncident{}).Where("tenant_id = ? AND signal_code IN ?", tenantID, cdcRiskCodes).Distinct("module").Order("module").Pluck("module", &riskOwners).Error; err != nil {
		return nil, commonclient.ErrExecutionOwnerUnavailable
	}
	for _, owner := range riskOwners {
		found := false
		for _, existing := range owners {
			if owner == existing {
				found = true
				break
			}
		}
		if !found {
			owners = append(owners, owner)
		}
	}
	if module != "" {
		selected := []string{}
		for _, owner := range owners {
			if owner == module {
				selected = append(selected, owner)
			}
		}
		owners = selected
	}
	return query.authorizeReadOwners(ctx, tenantID, principalID, bearer, owners)
}

// Project alert evidence at the read boundary, including incidents recorded
// before the safe execution projection was introduced.
func safeAlertForRead(alert monitorModels.AlertIncident) monitorModels.AlertIncident {
	details := models.JSONMap{}
	for _, key := range strings.Fields("latest_status failure_count failure_threshold timeout_ms error health not_before generation schema_version provider status capture_position current_position earliest_available_position position_headroom earliest_available_at window_seconds fra_used_percent fra_reclaimable_percent sampled_at request_id from_revision to_revision detected_at scope source_partition source_offset missing_fields unexpected_fields incompatible_fields") {
		switch value := alert.Details[key].(type) {
		case string:
			if key == "error" {
				details[key] = execution.FailureCategory(models.JSONMap{"message": value})
			} else {
				details[key] = execution.SafeDiagnosticText(value)
			}
		case bool, int, int64, float64:
			details[key] = value
		case []interface{}:
			safe := []string{}
			for _, entry := range value {
				if text, ok := entry.(string); ok && len(safe) < 100 {
					safe = append(safe, execution.SafeDiagnosticText(text))
				}
			}
			details[key] = safe
		}
	}
	if alert.SignalCode == cdcRiskCodes[0] || alert.SignalCode == cdcRiskCodes[1] {
		reason, _ := alert.Details["reason"].(string)
		if reason == "slot_missing" || reason == "wal_lost" || reason == "buffer_position_lost" {
			details["reason"] = reason
		}
	}

	if alert.SignalCode == cdcRiskCodes[2] {
		reason, _ := alert.Details["reason"].(string)
		if reason == "connector_not_running" || reason == "task_not_running" || reason == "source_disconnected" {
			details["reason"] = reason
		}
		if since, ok := alert.Details["failed_since"].(string); ok {
			details["failed_since"] = execution.SafeDiagnosticText(since)
		}
		details["trigger_seconds"] = cdcCaptureTriggerSeconds
	}
	alert.Details = details
	alert.RuleName = execution.SafeDiagnosticText(alert.RuleName)
	return alert
}
