package scantask

import (
	"fmt"
	"strconv"
	"time"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

const AutomaticTaskNamePrefix = "自动扫描"
const DefaultEngineTaskScanDepth = scanflow.ScanDepthDeep

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

func NewEngineScanTask(tenantID, userID, engineID uint, engineName string, cronExpr string, scanDepth string, now time.Time, nextRunAt *time.Time) *models.ScanTask {
	return &models.ScanTask{
		TenantID:    tenantID,
		EngineID:    engineID,
		Name:        AutomaticTaskName(engineName),
		Description: "由 Console 注册引擎时创建",
		Schedule:    cronExpr,
		Enabled:     true,
		Scope:       EngineScope(engineID),
		Parameters:  EngineTaskParameters(scanDepth),
		OwnerModule: "system",
		OwnerRef:    AutomaticTaskOwnerRef(engineID),
		NextRunAt:   nextRunAt,
		CreatedBy:   userID,
		UpdatedBy:   userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func EngineScanTaskUpdates(userID uint, engineName string, cronExpr string, scanDepth string, nextRunAt *time.Time, now time.Time) map[string]interface{} {
	updates := map[string]interface{}{
		"name":         AutomaticTaskName(engineName),
		"schedule":     cronExpr,
		"enabled":      true,
		"parameters":   EngineTaskParameters(scanDepth),
		"owner_module": "system",
		"updated_by":   userID,
		"updated_at":   now,
	}
	if nextRunAt != nil {
		updates["next_run_at"] = *nextRunAt
	} else {
		updates["next_run_at"] = nil
	}
	return updates
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
