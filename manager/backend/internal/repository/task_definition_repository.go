package repository

import (
	"context"
	"fmt"
	"strings"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

var managedQuickViewTaskTypes = []string{
	commonExecution.TaskTypeVectorTileCacheGeneration,
	commonExecution.TaskTypeVectorMaterializedViewGeneration,
	commonExecution.TaskTypeRasterCOGGeneration,
	commonExecution.TaskTypeModel3DGLBGeneration,
	commonExecution.TaskTypeModel3DTilesGeneration,
	commonExecution.TaskTypeGaussianSplatKSplatGeneration,
	commonExecution.TaskTypePointCloudCOPCGeneration,
}

var spatialBusinessTaskTypes = []string{
	commonExecution.TaskTypeVectorTileSetGeneration,
	commonExecution.TaskTypeRasterMosaicGeneration,
}

func ManagerDerivedTaskTypes() []string {
	result := append([]string{}, managedQuickViewTaskTypes...)
	return append(result, spatialBusinessTaskTypes...)
}

func ManagerDerivedTaskCategory(taskType string) string {
	for _, candidate := range managedQuickViewTaskTypes {
		if taskType == candidate {
			return models.TaskCategoryManagedQuickView
		}
	}
	for _, candidate := range spatialBusinessTaskTypes {
		if taskType == candidate {
			return models.TaskCategorySpatialBusiness
		}
	}
	return ""
}

func taskDefinitionScope(db *gorm.DB, taskType string) *gorm.DB {
	return db.Where("task_type = ?", taskType)
}

func createTaskDefinition(ctx context.Context, db *gorm.DB, taskType string, task *models.TaskDefinition) error {
	task.TaskType = taskType
	if task.Version == 0 {
		task.Version = 1
	}
	task.SemanticKey = taskSemanticKey(taskType, task.Config)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		return replaceTaskResourceBindings(tx, task)
	})
}

func updateTaskDefinition(ctx context.Context, db *gorm.DB, taskType string, task *models.TaskDefinition) error {
	task.TaskType = taskType
	task.Version++
	task.SemanticKey = taskSemanticKey(taskType, task.Config)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND tenant_id = ? AND task_type = ?", task.ID, task.TenantID, taskType).Save(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return replaceTaskResourceBindings(tx, task)
	})
}

func replaceTaskResourceBindings(tx *gorm.DB, task *models.TaskDefinition) error {
	if err := tx.Where("task_definition_id = ?", task.ID).Delete(&models.TaskResourceBinding{}).Error; err != nil {
		return err
	}
	bindings := projectTaskResourceBindings(task)
	if len(bindings) == 0 {
		return nil
	}
	return tx.Create(&bindings).Error
}

func projectTaskResourceBindings(task *models.TaskDefinition) []models.TaskResourceBinding {
	if task == nil {
		return nil
	}
	bindings := make([]models.TaskResourceBinding, 0, 2)
	add := func(role string, value interface{}, ordinal int) {
		m, ok := jsonMap(value)
		if !ok {
			return
		}
		engineID := firstUint(m, "source_engine_id", "target_engine_id", "engine_id")
		locator := firstString(m, "item_locator", "node_locator", "storage_locator", "locator")
		if engineID == 0 || locator == "" {
			return
		}
		var itemID *uint
		if id := firstUint(m, "item_id"); id > 0 {
			itemID = &id
		}
		bindings = append(bindings, models.TaskResourceBinding{
			TaskDefinitionID: task.ID,
			TenantID:         task.TenantID,
			Role:             role,
			EngineID:         engineID,
			Locator:          locator,
			ItemID:           itemID,
			ItemFingerprint:  firstString(m, "item_fingerprint"),
			Ordinal:          ordinal,
		})
	}

	if task.TaskType == commonExecution.TaskTypeVectorTileCacheGeneration ||
		task.TaskType == commonExecution.TaskTypeVectorMaterializedViewGeneration ||
		task.TaskType == commonExecution.TaskTypeRasterCOGGeneration {
		add(models.TaskResourceRoleSource, task.Config["target"], 0)
		return bindings
	}
	add(models.TaskResourceRoleSource, task.Config["source"], 0)
	add(models.TaskResourceRoleTarget, task.Config["target"], 0)
	return bindings
}

func taskSemanticKey(taskType string, config commonModels.JSONMap) string {
	source, _ := jsonMap(config["source"])
	target, _ := jsonMap(config["target"])
	switch taskType {
	case commonExecution.TaskTypeVectorTileCacheGeneration:
		return joinSemantic(firstString(target, "item_fingerprint"), firstString(config, "profile_hash"))
	case commonExecution.TaskTypeVectorMaterializedViewGeneration:
		geometry, _ := jsonMap(config["geometry"])
		return joinSemantic(firstString(target, "item_fingerprint"), firstString(geometry, "geometry_column"), fmt.Sprint(geometry["target_srid"]))
	case commonExecution.TaskTypeRasterCOGGeneration:
		return firstString(target, "item_fingerprint")
	case commonExecution.TaskTypeModel3DGLBGeneration,
		commonExecution.TaskTypeGaussianSplatKSplatGeneration,
		commonExecution.TaskTypePointCloudCOPCGeneration:
		return firstString(source, "item_fingerprint")
	case commonExecution.TaskTypeModel3DTilesGeneration:
		return joinSemantic(firstString(source, "item_fingerprint"), firstString(config, "target_format"))
	case commonExecution.TaskTypeVectorTileSetGeneration, commonExecution.TaskTypeRasterMosaicGeneration:
		return firstString(config, "semantic_hash")
	default:
		return ""
	}
}

func joinSemantic(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" && value != "<nil>" {
			clean = append(clean, value)
		}
	}
	return strings.Join(clean, ":")
}

func jsonMap(value interface{}) (commonModels.JSONMap, bool) {
	switch typed := value.(type) {
	case commonModels.JSONMap:
		return typed, true
	case map[string]interface{}:
		return commonModels.JSONMap(typed), true
	default:
		return nil, false
	}
}

func firstString(m commonModels.JSONMap, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstUint(m commonModels.JSONMap, keys ...string) uint {
	for _, key := range keys {
		switch value := m[key].(type) {
		case uint:
			return value
		case int:
			if value > 0 {
				return uint(value)
			}
		case int64:
			if value > 0 {
				return uint(value)
			}
		case float64:
			if value > 0 {
				return uint(value)
			}
		}
	}
	return 0
}

type TaskDefinitionFilter struct {
	TenantID uint
	TaskType string
	Category string
	Page     int
	PageSize int
}

type TaskDefinitionRepository struct{ db *gorm.DB }

func NewTaskDefinitionRepository(db *gorm.DB) *TaskDefinitionRepository {
	return &TaskDefinitionRepository{db: db}
}

func (r *TaskDefinitionRepository) List(ctx context.Context, filter TaskDefinitionFilter) ([]*models.TaskDefinition, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.TaskDefinition{}).Where("tenant_id = ?", filter.TenantID)
	if taskType := strings.TrimSpace(filter.TaskType); taskType != "" {
		query = query.Where("task_type = ?", taskType)
	} else {
		switch strings.TrimSpace(filter.Category) {
		case models.TaskCategoryManagedQuickView:
			query = query.Where("task_type IN ?", managedQuickViewTaskTypes)
		case models.TaskCategorySpatialBusiness:
			query = query.Where("task_type IN ?", spatialBusinessTaskTypes)
		default:
			query = query.Where("task_type IN ?", ManagerDerivedTaskTypes())
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var tasks []*models.TaskDefinition
	err := query.Order("updated_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error
	return tasks, total, err
}

func (r *TaskDefinitionRepository) Delete(ctx context.Context, tenantID uint, taskType string, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_definition_id = ? AND tenant_id = ?", id, tenantID).Delete(&models.TaskResourceBinding{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ? AND task_type = ?", id, tenantID, taskType).Delete(&models.TaskDefinition{}).Error
	})
}
