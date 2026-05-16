package scantask

import (
	"fmt"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

const AutomaticTaskNamePrefix = "自动扫描"

func NewTaskFromUpsertRequest(tenantID, userID uint, req *models.ScanTaskUpsertRequest, now time.Time, nextRunAt *time.Time) *models.ScanTask {
	return &models.ScanTask{
		TenantID:    tenantID,
		EngineID:    req.EngineID,
		Name:        req.Name,
		Description: req.Description,
		Schedule:    req.Schedule,
		Enabled:     req.Enabled,
		Parameters:  TaskParameters(req.CatalogPaths, req.ScanDepth, req.Force),
		NextRunAt:   nextRunAt,
		CreatedBy:   userID,
		UpdatedBy:   userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func ApplyUpsertRequest(task *models.ScanTask, userID uint, req *models.ScanTaskUpsertRequest, now time.Time, nextRunAt *time.Time) {
	task.Name = req.Name
	task.Description = req.Description
	task.EngineID = req.EngineID
	task.Schedule = req.Schedule
	task.Enabled = req.Enabled
	task.Parameters = TaskParameters(req.CatalogPaths, req.ScanDepth, req.Force)
	task.NextRunAt = nextRunAt
	task.UpdatedBy = userID
	task.UpdatedAt = now
}

func AutomaticTaskName(resourceName string) string {
	return fmt.Sprintf("%s - %s", AutomaticTaskNamePrefix, resourceName)
}

func AutomaticTaskPattern() string {
	return AutomaticTaskNamePrefix + "%"
}

func AutomaticTaskParameters() models.JSONMap {
	return models.JSONMap{
		"scan_depth": "deep",
		"force":      false,
	}
}

func NewAutomaticTask(resource *commonModels.Engine, tenantID uint, cronExpr string) *models.ScanTask {
	return &models.ScanTask{
		TenantID:    tenantID,
		EngineID:    resource.ID,
		Name:        AutomaticTaskName(resource.Name),
		Description: "由存储引擎注册时自动创建",
		Schedule:    cronExpr,
		Enabled:     true,
		Parameters:  AutomaticTaskParameters(),
	}
}

func AutomaticTaskUpdates(resource *commonModels.Engine, cronExpr string, now time.Time) map[string]interface{} {
	return map[string]interface{}{
		"name":       AutomaticTaskName(resource.Name),
		"schedule":   cronExpr,
		"enabled":    resource.ScanConfig.Enabled,
		"parameters": AutomaticTaskParameters(),
		"updated_at": now,
	}
}

func ApplyAutomaticTaskUpdate(task models.ScanTask, resource *commonModels.Engine, cronExpr string) models.ScanTask {
	task.Name = AutomaticTaskName(resource.Name)
	task.Schedule = cronExpr
	task.Enabled = resource.ScanConfig.Enabled
	task.Parameters = AutomaticTaskParameters()
	return task
}
