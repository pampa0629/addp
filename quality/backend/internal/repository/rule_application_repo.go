package repository

import (
	"context"
	"encoding/json"
	"fmt"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonRepository "github.com/addp/common/repository"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RuleApplicationRepository struct {
	db *gorm.DB
}

func NewRuleApplicationRepository(db *gorm.DB) *RuleApplicationRepository {
	return &RuleApplicationRepository{db: db}
}

type RuleApplicationListOptions struct {
	TenantID   int64
	EngineID   int64
	SchemaName string
	TableName  string
	Page       int
	PageSize   int
}

func (r *RuleApplicationRepository) List(opts RuleApplicationListOptions) ([]models.RuleApplication, int64, error) {
	var items []models.RuleApplication
	q := r.db.Where("tenant_id = ?", opts.TenantID)
	if opts.EngineID > 0 {
		q = q.Where("engine_id = ?", opts.EngineID)
	}
	if opts.SchemaName != "" {
		q = q.Where("schema_name = ?", opts.SchemaName)
	}
	if opts.TableName != "" {
		q = q.Where("table_name = ?", opts.TableName)
	}
	var total int64
	if err := q.Model(&models.RuleApplication{}).Count(&total).Error; err != nil {
		return nil, 0, commonRepository.WrapDBError(err)
	}
	page, pageSize := normalizePage(opts.Page, opts.PageSize)
	err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, commonRepository.WrapDBError(err)
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func (r *RuleApplicationRepository) Get(id, tenantID int64) (*models.RuleApplication, error) {
	var item models.RuleApplication
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	if err != nil {
		return nil, commonRepository.WrapDBError(err)
	}
	return &item, nil
}

func (r *RuleApplicationRepository) Create(item *models.RuleApplication) error {
	return commonRepository.WrapDBError(r.db.Create(item).Error)
}

func (r *RuleApplicationRepository) UpdateEnabled(id, tenantID, userID int64, enabled bool) error {
	result := r.db.Model(&models.RuleApplication{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"enabled": enabled, "updated_by": userID, "updated_at": gorm.Expr("CURRENT_TIMESTAMP")})
	if result.Error != nil {
		return commonRepository.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonAPI.ErrNotFound
	}
	return nil
}

func (r *RuleApplicationRepository) Delete(ctx context.Context, id, tenantID int64) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var application models.RuleApplication
		if err := tx.Where("id = ? AND tenant_id = ?", id, tenantID).First(&application).Error; err != nil {
			return err
		}
		// ClaimExecution locks the task before reading its rule snapshots. Keep
		// the same order here so deletion and trigger cannot cross each other.
		var tasks []models.CheckTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?", tenantID, application.EngineID, application.SchemaName, application.Table).
			Order("id ASC").
			Find(&tasks).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&application).Error; err != nil {
			return err
		}

		taskIDs := make([]int64, 0, len(tasks))
		for _, task := range tasks {
			taskIDs = append(taskIDs, task.ID)
		}
		var activeExecutions []commonExecution.TaskExecution
		if len(taskIDs) > 0 {
			sourceTaskIDs := make([]string, 0, len(taskIDs))
			for _, taskID := range taskIDs {
				sourceTaskIDs = append(sourceTaskIDs, fmt.Sprint(taskID))
			}
			if err := tx.Select("execution_id", "execution_config").
				Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id IN ? AND status IN ?", tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, sourceTaskIDs, []string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
				Find(&activeExecutions).Error; err != nil {
				return err
			}
		}
		for _, execution := range activeExecutions {
			references, err := executionReferencesRuleApplication(execution.ExecutionConfig, id)
			if err != nil {
				return fmt.Errorf("inspect quality execution %s rule snapshot: %w", execution.ExecutionID, err)
			}
			if references {
				return fmt.Errorf("%w: rule application %d has an active execution", commonAPI.ErrConflict, id)
			}
		}

		if err := tx.Where("tenant_id = ? AND rule_application_id = ?", tenantID, id).Delete(&models.Issue{}).Error; err != nil {
			return err
		}
		return tx.Delete(&application).Error
	}))
}

func executionReferencesRuleApplication(config map[string]interface{}, ruleApplicationID int64) (bool, error) {
	raw, ok := config["rule_applications"]
	if !ok {
		return false, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return false, err
	}
	var snapshots []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(data, &snapshots); err != nil {
		return false, err
	}
	for _, snapshot := range snapshots {
		if snapshot.ID == ruleApplicationID {
			return true, nil
		}
	}
	return false, nil
}
