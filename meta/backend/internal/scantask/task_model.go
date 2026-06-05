package scantask

import (
	"fmt"
	"strconv"
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
		Scope:       TaskScope(req.EngineID, req.Scope, req.CatalogPaths),
		Parameters:  TaskParameters(req.ScanDepth, req.Force),
		OwnerModule: "meta",
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
	task.Scope = TaskScope(req.EngineID, req.Scope, req.CatalogPaths)
	task.Parameters = TaskParameters(req.ScanDepth, req.Force)
	task.OwnerModule = "meta"
	task.OwnerRef = ""
	task.NextRunAt = nextRunAt
	task.UpdatedBy = userID
	task.UpdatedAt = now
}

func AutomaticTaskName(resourceName string) string {
	return fmt.Sprintf("%s - %s", AutomaticTaskNamePrefix, resourceName)
}

func AutomaticTaskOwnerRef(engineID uint) string {
	return "engine:" + strconv.FormatUint(uint64(engineID), 10)
}

func NewAutomaticTask(resource *commonModels.Engine, tenantID uint, cronExpr string) *models.ScanTask {
	return &models.ScanTask{
		TenantID:    tenantID,
		EngineID:    resource.ID,
		Name:        AutomaticTaskName(resource.Name),
		Description: "由存储引擎注册时自动创建",
		Schedule:    cronExpr,
		Enabled:     true,
		Scope:       EngineScope(resource.ID),
		Parameters:  AutomaticTaskParameters(),
		OwnerModule: "system",
		OwnerRef:    AutomaticTaskOwnerRef(resource.ID),
	}
}

func AutomaticTaskUpdates(resource *commonModels.Engine, cronExpr string, nextRunAt *time.Time, now time.Time) map[string]interface{} {
	updates := map[string]interface{}{
		"name":         AutomaticTaskName(resource.Name),
		"schedule":     cronExpr,
		"enabled":      resource.ScanConfig.Enabled,
		"scope":        EngineScope(resource.ID),
		"parameters":   AutomaticTaskParameters(),
		"owner_module": "system",
		"owner_ref":    AutomaticTaskOwnerRef(resource.ID),
		"updated_at":   now,
	}
	if nextRunAt != nil {
		updates["next_run_at"] = *nextRunAt
	} else {
		updates["next_run_at"] = nil
	}
	return updates
}

func ApplyAutomaticTaskUpdate(task models.ScanTask, resource *commonModels.Engine, cronExpr string) models.ScanTask {
	task.Name = AutomaticTaskName(resource.Name)
	task.Schedule = cronExpr
	task.Enabled = resource.ScanConfig.Enabled
	task.Scope = EngineScope(resource.ID)
	task.Parameters = AutomaticTaskParameters()
	task.OwnerModule = "system"
	task.OwnerRef = AutomaticTaskOwnerRef(resource.ID)
	return task
}

func TaskScope(engineID uint, explicit models.JSONMap, catalogPaths []string) models.JSONMap {
	if explicit != nil && len(explicit) > 0 {
		return explicit
	}
	if len(catalogPaths) > 0 {
		return models.JSONMap{
			"type":          "catalog_path",
			"engine_id":     engineID,
			"catalog_paths": catalogPaths,
		}
	}
	return EngineScope(engineID)
}

func EngineScope(engineID uint) models.JSONMap {
	return models.JSONMap{
		"type":      "engine",
		"engine_id": engineID,
	}
}
