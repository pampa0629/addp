package service

import (
	"context"
	"strings"

	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
)

func (s *AlertService) visibleAlertQuery(ctx context.Context, query *gorm.DB, tenantID int) *gorm.DB {
	if _, exists := execution.ReadScopesFromContext(ctx); !exists {
		return query
	}
	visible := execution.ApplyReadScopes(ctx, s.db.WithContext(ctx).Model(&execution.TaskExecution{}).Where("tenant_id = ?", tenantID)).Select("execution_id")
	return query.Where("execution_id IN (?)", visible)
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
	alert.Details = details
	alert.RuleName = execution.SafeDiagnosticText(alert.RuleName)
	return alert
}
