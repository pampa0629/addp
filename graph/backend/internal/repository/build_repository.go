package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BuildRepository struct {
	db *gorm.DB
}

type BuildExecutionClaimMode string

const (
	BuildExecutionClaimRun   BuildExecutionClaimMode = "run"
	BuildExecutionClaimRerun BuildExecutionClaimMode = "rerun"
)

func NewBuildRepository(db *gorm.DB) *BuildRepository {
	return &BuildRepository{db: db}
}

// ============ BuildTask ============

func (r *BuildRepository) ListTasks(graphID, tenantID uint) ([]models.BuildTask, error) {
	var tasks []models.BuildTask
	err := r.db.Where("graph_id = ? AND tenant_id = ?", graphID, tenantID).
		Order("created_at DESC").Find(&tasks).Error
	if err == nil {
		for i := range tasks {
			if err = r.fillTaskExecution(&tasks[i]); err != nil {
				break
			}
		}
	}
	return tasks, err
}

func (r *BuildRepository) ListAllTasks(tenantID uint) ([]models.BuildTask, error) {
	var tasks []models.BuildTask
	err := r.db.Where("tenant_id = ?", tenantID).
		Order("created_at DESC").Find(&tasks).Error
	if err == nil {
		for i := range tasks {
			if err = r.fillTaskExecution(&tasks[i]); err != nil {
				break
			}
		}
	}
	return tasks, err
}

func (r *BuildRepository) GetTask(id, tenantID uint) (*models.BuildTask, error) {
	var task models.BuildTask
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&task).Error
	if err == nil {
		err = r.fillTaskExecution(&task)
	}
	return &task, err
}

func (r *BuildRepository) CreateTask(task *models.BuildTask) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, task.TenantID, false); err != nil {
			return err
		}
		if err := CheckGraphOpen(tx, task.GraphID, task.TenantID); err != nil {
			return err
		}
		return tx.Create(task).Error
	})
}

func (r *BuildRepository) EnqueueExecution(ctx context.Context, taskID, graphID, tenantID uint, execution *commonExecution.TaskExecution, mode BuildExecutionClaimMode, verified []models.FrozenBuildMaterial, engineVersion int64, expectedTarget models.KnowledgeGraph) (*models.BuildTask, []models.BuildMaterial, error) {
	var task models.BuildTask
	var materials []models.BuildMaterial
	if (execution.Source == commonExecution.ModuleGraph && execution.ParentExecutionID != nil) || (execution.Source == commonExecution.ModuleOrchestrator && execution.ParentExecutionID == nil) || (execution.Source != commonExecution.ModuleGraph && execution.Source != commonExecution.ModuleOrchestrator) {
		return nil, nil, commonAPI.ErrForbidden
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, tenantID, false); err != nil {
			return err
		}
		if err := CheckGraphOpen(tx, graphID, tenantID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).First(&task).Error; err != nil {
			return err
		}
		if task.GraphID != graphID {
			return fmt.Errorf("build task %d does not belong to graph %d", taskID, graphID)
		}

		sourceTaskID := commonExecution.NewSourceTaskIDFromUint(taskID)
		var activeCount int64
		if err := tx.Model(&commonExecution.TaskExecution{}).
			Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?",
				tenantID, commonExecution.ModuleGraph, commonExecution.TaskTypeKGBuild, *sourceTaskID,
				[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return fmt.Errorf("%w: graph build task %d already has an active execution", commonAPI.ErrConflict, taskID)
		}
		if task.Status == models.BuildStatusRunning || (task.Status == models.BuildStatusPending && task.ExecutionID != "") {
			return fmt.Errorf("%w: graph build task %d is active", commonAPI.ErrConflict, taskID)
		}

		var activeReviews int64
		if err := tx.Model(&models.ReviewItem{}).Where("task_id = ? AND tenant_id = ? AND active_command_id <> ''", taskID, tenantID).Count(&activeReviews).Error; err != nil {
			return err
		}
		if activeReviews != 0 {
			return commonAPI.ErrConflict
		}

		switch mode {
		case BuildExecutionClaimRun:
		case BuildExecutionClaimRerun:
			if task.Status != models.BuildStatusSuccess && task.Status != models.BuildStatusFailed &&
				task.Status != models.BuildStatusTimeout && task.Status != models.BuildStatusCancelled {
				return fmt.Errorf("graph build task %d cannot rerun from status %s", taskID, task.Status)
			}
			if err := tx.Model(&models.BuildMaterial{}).
				Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
				Updates(map[string]interface{}{
					"status": models.BuildMaterialStatusPending, "processed_chunks": 0, "total_chunks": 0,
					"error_message": "", "processed_at": nil,
				}).Error; err != nil {
				return err
			}
			if err := tx.Where("task_id = ? AND tenant_id = ? AND status = ?", taskID, tenantID, models.ReviewStatusPending).
				Delete(&models.ReviewItem{}).Error; err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported graph build execution claim mode %q", mode)
		}

		if err := tx.Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
			Order("created_at ASC, id ASC").Find(&materials).Error; err != nil {
			return err
		}
		if len(materials) == 0 {
			return fmt.Errorf("graph build task %d has no materials", taskID)
		}

		execution.SourceTaskID = sourceTaskID
		execution.SourceTaskName = &task.Name
		if len(verified) != len(materials) {
			return commonAPI.ErrConflict
		}
		for i, m := range materials {
			v := verified[i]
			if m.ID != v.Material.ID || m.FilePath != v.Material.FilePath || m.FileSize != v.Material.FileSize || m.ContentSHA256 != v.Material.ContentSHA256 || v.SHA256 == "" || (m.ContentSHA256 != "" && m.ContentSHA256 != v.SHA256) {
				return commonAPI.ErrConflict
			}
		}
		var kg models.KnowledgeGraph
		if err := tx.Where("id = ? AND tenant_id = ?", graphID, tenantID).First(&kg).Error; err != nil {
			return err
		}
		if kg.EngineID != expectedTarget.EngineID || kg.OntologyID != expectedTarget.OntologyID || kg.IndexManagement != expectedTarget.IndexManagement || !reflect.DeepEqual(kg.CatalogPath, expectedTarget.CatalogPath) {
			return commonAPI.ErrConflict
		}
		for i := range materials {
			verified[i].Material = materials[i]
		}
		ontology, err := NewOntologyRepository(tx).GetDetail(kg.OntologyID, tenantID)
		if err != nil {
			return err
		}
		frozenTask := task
		frozenTask.Materials = nil
		frozenTask.Graph = nil
		frozenTask.ExecutionID = execution.ExecutionID
		frozenTask.Status = models.BuildStatusPending
		frozenTask.StartedAt = nil
		frozenTask.CompletedAt = nil
		frozenTask.Stats = nil
		frozenTask.ErrorMessage = ""
		input := models.BuildInput{Schema: models.BuildInputSchema, Task: frozenTask, TenantID: tenantID, GraphID: graphID, EngineID: kg.EngineID, EngineVersion: engineVersion, CatalogPath: kg.CatalogPath, IndexManagement: kg.IndexManagement, Ontology: *ontology, Materials: verified}
		frozen, err := json.Marshal(input)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(frozen)
		execution.ExecutionConfig = commonModels.JSONMap{"graph_id": graphID, "input_schema": models.BuildInputSchema, "input_digest": hex.EncodeToString(digest[:])}
		if mode == BuildExecutionClaimRerun {
			execution.ExecutionConfig["predecessor_execution_id"] = task.ExecutionID
		}
		execution.ExecutionBoundary = commonExecution.ExecutionBoundaryBounded
		execution.MaxAttempts = 1
		if execution.ParentExecutionID != nil {
			if err := commonExecution.InheritOrchestratorActor(tx, execution); err != nil {
				return err
			}
		} else if err := commonExecution.ApplyUserOrigin(ctx, execution); err != nil {
			return err
		}
		if execution.ParentExecutionID != nil {
			var parent commonExecution.TaskExecution
			if err := tx.Where("execution_id = ? AND tenant_id = ?", *execution.ParentExecutionID, tenantID).First(&parent).Error; err != nil {
				return err
			}
			if err := commonExecution.InheritDisplayLanguage(execution, &parent); err != nil {
				return err
			}
		} else if err := commonExecution.FreezeDisplayLanguage(execution, commoni18n.Language(ctx)); err != nil {
			return err
		}

		if err := commonExecution.CreatePending(ctx, tx, execution); err != nil {
			return err
		}
		if err := tx.Create(&models.BuildExecutionControl{ExecutionID: execution.ExecutionID, TenantID: tenantID, GraphID: graphID, TaskID: taskID, Snapshot: string(frozen)}).Error; err != nil {
			return err
		}
		taskFields := map[string]interface{}{
			"execution_id": execution.ExecutionID, "status": models.BuildStatusPending,
			"started_at": nil, "completed_at": nil, "error_message": "",
		}
		if mode == BuildExecutionClaimRerun {
			taskFields["stats"] = []byte("{}")
		}
		if err := tx.Model(&task).Updates(taskFields).Error; err != nil {
			return err
		}
		task.ExecutionID = execution.ExecutionID
		task.Status = models.BuildStatusPending
		task.StartedAt = nil
		task.CompletedAt = nil
		task.ErrorMessage = ""
		if mode == BuildExecutionClaimRerun {
			task.Stats = []byte("{}")
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	var state interface{ SQLState() string }
	if errors.As(err, &state) && state.SQLState() == "40001" {
		err = fmt.Errorf("%w: graph enqueue snapshot changed", commonAPI.ErrConflict)
	}
	return &task, materials, err
}

// ============ BuildMaterial ============

func (r *BuildRepository) ListMaterials(taskID, tenantID uint) ([]models.BuildMaterial, error) {
	var materials []models.BuildMaterial
	err := r.db.Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
		Order("created_at ASC, id ASC").Find(&materials).Error
	return materials, err
}

func (r *BuildRepository) GetMaterial(id, tenantID uint) (*models.BuildMaterial, error) {
	var mat models.BuildMaterial
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&mat).Error
	return &mat, err
}

func (r *BuildRepository) UpdateMaterial(ctx context.Context, mat *models.BuildMaterial) error {
	return r.WithBuildLease(ctx, func(tx *gorm.DB, task *models.BuildTask, e *commonExecution.TaskExecution) error {
		if mat.TaskID != task.ID || mat.GraphID != task.GraphID || mat.TenantID != task.TenantID {
			return commonAPI.ErrConflict
		}
		result := tx.Model(&models.BuildMaterial{}).Where("id = ? AND task_id = ? AND graph_id = ? AND tenant_id = ?", mat.ID, task.ID, task.GraphID, task.TenantID).Updates(map[string]interface{}{"status": mat.Status, "total_chunks": mat.TotalChunks, "processed_chunks": mat.ProcessedChunks, "stats": mat.Stats, "error_message": mat.ErrorMessage, "processed_at": mat.ProcessedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonAPI.ErrConflict
		}
		return nil
	})
}

// ============ ReviewItem ============

type ReviewFilter struct {
	GraphID  uint
	TenantID uint
	TaskID   uint   // 0 表示不过滤
	ItemType string // "" 表示不过滤
	Status   string // "" 表示不过滤（默认返回 pending）
	Page     int
	PageSize int
}

func (r *BuildRepository) ListReviewItems(filter ReviewFilter) ([]models.ReviewItem, int64, error) {
	query := r.db.Where("graph_id = ? AND tenant_id = ?", filter.GraphID, filter.TenantID)
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if filter.ItemType != "" {
		query = query.Where("item_type = ?", filter.ItemType)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	} else {
		query = query.Where("status = ?", models.ReviewStatusPending)
	}

	var total int64
	query.Model(&models.ReviewItem{}).Count(&total)

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var items []models.ReviewItem
	err := query.Order("confidence ASC, created_at ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

func (r *BuildRepository) GetReviewItem(id, graphID, tenantID uint) (*models.ReviewItem, error) {
	var item models.ReviewItem
	err := r.db.Where("id = ? AND graph_id = ? AND tenant_id = ?", id, graphID, tenantID).First(&item).Error
	return &item, err
}

func (r *BuildRepository) CreateReviewItem(ctx context.Context, item *models.ReviewItem) error {
	return r.WithBuildLease(ctx, func(tx *gorm.DB, task *models.BuildTask, e *commonExecution.TaskExecution) error {
		if item.TaskID != task.ID || item.GraphID != task.GraphID || item.TenantID != task.TenantID {
			return commonAPI.ErrConflict
		}
		item.ExecutionID = e.ExecutionID
		return tx.Create(item).Error
	})
}

func (r *BuildRepository) UpdateReviewItem(item *models.ReviewItem) error {
	return r.db.Save(item).Error
}

func (r *BuildRepository) CountPendingReview(graphID, tenantID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.ReviewItem{}).
		Where("graph_id = ? AND tenant_id = ? AND status = ?", graphID, tenantID, models.ReviewStatusPending).
		Count(&count).Error
	return count, err
}
