package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonRepository "github.com/addp/common/repository"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type IssueRepository struct {
	db *gorm.DB
}

func NewIssueRepository(db *gorm.DB) *IssueRepository {
	return &IssueRepository{db: db}
}

func (r *IssueRepository) List(tenantID int64, status string, engineID int64, page, pageSize int) ([]models.Issue, int64, error) {
	var items []models.Issue
	q := r.db.Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if engineID > 0 {
		q = q.Where("engine_id = ?", engineID)
	}
	var total int64
	if err := q.Model(&models.Issue{}).Count(&total).Error; err != nil {
		return nil, 0, commonRepository.WrapDBError(err)
	}
	page, pageSize = normalizePage(page, pageSize)
	err := q.Order("updated_at desc, id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, commonRepository.WrapDBError(err)
}

func (r *IssueRepository) Get(id, tenantID int64) (*models.Issue, error) {
	var item models.Issue
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	if err != nil {
		return nil, commonRepository.WrapDBError(err)
	}
	return &item, nil
}

func (r *IssueRepository) Create(item *models.Issue) error {
	return commonRepository.WrapDBError(r.db.Create(item).Error)
}

func (r *IssueRepository) UpdateStatus(ctx context.Context, id, tenantID, userID int64, status, note string) error {
	note = strings.TrimSpace(note)
	if status != "resolved" && status != "ignored" {
		return fmt.Errorf("%w: issue status must be resolved or ignored", commonAPI.ErrBadRequest)
	}
	if note == "" {
		return fmt.Errorf("%w: issue resolution note is required", commonAPI.ErrBadRequest)
	}
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var issue models.Issue
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&issue).Error; err != nil {
			return err
		}
		if issue.Status != "open" {
			return fmt.Errorf("%w: issue %d is already %s", commonAPI.ErrConflict, id, issue.Status)
		}
		now := time.Now().UTC()
		return tx.Model(&issue).Updates(map[string]interface{}{"status": status, "resolved_at": now, "resolved_by": userID, "resolution_note": note}).Error
	}))
}

func (r *IssueRepository) BatchCreate(items []models.Issue) error {
	if len(items) == 0 {
		return nil
	}
	return r.db.Create(&items).Error
}

// Reconcile applies one complete execution observation to the current issue
// projection. It is idempotent on tenant + rule_application_id.
func (r *IssueRepository) Reconcile(ctx context.Context, tenantID int64, executionID string, observations []models.IssueObservation, observedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, observation := range observations {
			if observation.Passed {
				if err := tx.Model(&models.Issue{}).
					Where("tenant_id = ? AND rule_application_id = ? AND status = ?", tenantID, observation.RuleApplicationID, "open").
					Updates(map[string]interface{}{
						"status": "resolved", "resolved_at": observedAt, "last_execution_id": executionID,
						"last_observed_at": observedAt, "failed_count": observation.FailedCount, "total_count": observation.TotalCount,
						"pass_rate": observation.PassRate,
					}).Error; err != nil {
					return err
				}
				continue
			}

			detail, err := json.Marshal(map[string]interface{}{"severity": observation.Severity, "message": observation.Message})
			if err != nil {
				return err
			}
			issue := models.Issue{TenantID: tenantID, ExecutionID: executionID, LastExecutionID: executionID, RuleApplicationID: observation.RuleApplicationID, RuleType: observation.RuleType, Severity: observation.Severity, Message: observation.Message, ColumnName: observation.ColumnName, Table: observation.Table, SchemaName: observation.SchemaName, EngineID: observation.EngineID, FailedCount: observation.FailedCount, TotalCount: observation.TotalCount, PassRate: observation.PassRate, Detail: detail, Status: "open", LastObservedAt: &observedAt}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "tenant_id"}, {Name: "rule_application_id"}},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"last_execution_id": executionID, "rule_type": observation.RuleType,
					"severity": observation.Severity, "message": observation.Message, "column_name": observation.ColumnName,
					"table_name": observation.Table, "schema_name": observation.SchemaName, "engine_id": observation.EngineID,
					"failed_count": observation.FailedCount, "total_count": observation.TotalCount, "pass_rate": observation.PassRate,
					"detail": detail, "status": "open", "resolved_at": nil, "resolved_by": nil, "resolution_note": "",
					"last_observed_at": observedAt, "updated_at": observedAt,
				}),
			}).Create(&issue).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
