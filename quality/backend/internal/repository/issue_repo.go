package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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

func (r *IssueRepository) List(tenantID int64, status string, engineID int64, ownerDomainID *int64, page, pageSize int) ([]models.Issue, int64, error) {
	var items []models.Issue
	q := r.db.Where("tenant_id = ?", tenantID)
	q = filterOwnerDomain(q, ownerDomainID)
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
	if err := r.db.Where("tenant_id = ? AND issue_id = ?", tenantID, id).Order("id ASC").Find(&item.History).Error; err != nil {
		return nil, commonRepository.WrapDBError(err)
	}
	return &item, nil
}

func (r *IssueRepository) Create(item *models.Issue) error {
	return commonRepository.WrapDBError(r.db.Create(item).Error)
}

func (r *IssueRepository) UpdateStatus(ctx context.Context, id, tenantID, userID, version int64, status, note string) (*models.Issue, error) {
	note = strings.TrimSpace(note)
	if status != "resolved" && status != "accepted" {
		return nil, fmt.Errorf("%w: issue status must be resolved or accepted", commonAPI.ErrBadRequest)
	}
	if note == "" || utf8.RuneCountInString(note) > 4000 || version < 1 {
		return nil, fmt.Errorf("%w: positive version and bounded note required", commonAPI.ErrBadRequest)
	}
	var issue models.Issue
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&issue).Error; err != nil {
			return err
		}
		if issue.Version != version {
			return ErrVersionConflict
		}
		if issue.Status != "open" {
			return fmt.Errorf("%w: issue %d is already %s", commonAPI.ErrConflict, id, issue.Status)
		}
		var accepted json.RawMessage
		acceptedCount := int64(0)
		if status == "accepted" {
			var evidence models.FailureEvidence
			if err := json.Unmarshal(issue.Evidence, &evidence); err != nil || !completeEvidence(&evidence, issue.FailedCount) || issue.FailedCount == 0 || issue.TargetKey == nil || issue.RuleType == "row_count" {
				return fmt.Errorf("%w: complete record evidence required", commonAPI.ErrConflict)
			}
			accepted, _ = json.Marshal(evidence.Keys)
			acceptedCount = issue.FailedCount
		}
		now := time.Now().UTC()
		if err := tx.Create(&models.IssueAction{TenantID: tenantID, IssueID: issue.ID, PlanID: issue.PlanID, ExecutionID: issue.LastExecutionID, Action: status, ActorID: &userID, Note: note, AcceptedCount: acceptedCount, Evidence: issue.Evidence, CreatedAt: now}).Error; err != nil {
			return err
		}
		return tx.Model(&issue).Updates(map[string]interface{}{"version": gorm.Expr("version + 1"), "status": status, "resolved_at": now, "resolved_by": userID, "resolution_note": note, "accepted_keys": accepted, "accepted_count": acceptedCount, "pending_count": 0}).Error
	})
	if err != nil {
		return nil, commonRepository.WrapDBError(err)
	}
	return r.Get(id, tenantID)
}

func (r *IssueRepository) BatchCreate(items []models.Issue) error {
	if len(items) == 0 {
		return nil
	}
	return r.db.Create(&items).Error
}

// Reconcile applies one complete execution observation to the current issue
// projection. Identity includes the complete actual target scope.
func (r *IssueRepository) Reconcile(ctx context.Context, tenantID int64, executionID string, observations []models.IssueObservation, observedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, observation := range observations {
			if len(observation.TargetKey) != 64 {
				return fmt.Errorf("issue observation requires a target scope")
			}
			var issue models.Issue
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND plan_id=? AND target_key=? AND rule_key=?", tenantID, observation.PlanID, observation.TargetKey, observation.RuleKey).First(&issue).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			created := errors.Is(err, gorm.ErrRecordNotFound)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if observation.Passed {
					continue
				}
				issue = models.Issue{TenantID: tenantID, Version: 1, ExecutionID: executionID, LastExecutionID: executionID, PlanID: observation.PlanID, TargetKey: &observation.TargetKey, RuleKey: observation.RuleKey, RuleType: observation.RuleType, Status: "open"}
				insert := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "plan_id"}, {Name: "target_key"}, {Name: "rule_key"}}, DoNothing: true}).Create(&issue)
				if insert.Error != nil {
					return insert.Error
				}
				if insert.RowsAffected == 0 {
					issue = models.Issue{}
					if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND plan_id=? AND target_key=? AND rule_key=?", tenantID, observation.PlanID, observation.TargetKey, observation.RuleKey).First(&issue).Error; err != nil {
						return err
					}
					created = false
					if issue.LastExecutionID == executionID {
						continue
					}
				}
			} else if issue.LastExecutionID == executionID {
				continue
			}
			accepted := retainedAcceptance(issue, observation)
			status := "open"
			pending := observation.FailedCount - int64(len(accepted))
			if observation.Passed {
				status, pending = "resolved", 0
			} else if pending == 0 && observation.FailedCount > 0 {
				status = "accepted"
			}
			if observation.RuleType == "row_count" {
				pending = 0
			}
			detail, _ := json.Marshal(map[string]interface{}{"severity": observation.Severity, "message": observation.Message})
			evidence, _ := json.Marshal(observation.Evidence)
			acceptedJSON, _ := json.Marshal(accepted)
			reason := "not_observed"
			if observation.Evidence != nil {
				reason = observation.Evidence.Reason
			}
			updates := map[string]interface{}{
				"version": gorm.Expr("version + 1"), "evidence": evidence, "evidence_reason": reason,
				"accepted_keys": acceptedJSON, "accepted_count": len(accepted), "pending_count": pending,
				"last_execution_id": executionID, "rule_type": observation.RuleType,
				"severity": observation.Severity, "message": observation.Message, "column_name": observation.ColumnName,
				"table_name": observation.Table, "schema_name": observation.SchemaName, "engine_id": observation.EngineID,
				"failed_count": observation.FailedCount, "total_count": observation.TotalCount, "pass_rate": observation.PassRate,
				"detail": detail, "status": status,
				"owner_domain_id":  observation.OwnerDomainID,
				"last_observed_at": observedAt, "updated_at": observedAt,
			}
			if created {
				updates["version"] = 1
			}
			if status != issue.Status {
				updates["resolved_at"], updates["resolved_by"], updates["resolution_note"] = nil, nil, ""
				if status == "resolved" || status == "accepted" {
					updates["resolved_at"] = observedAt
				}
				if err := tx.Create(&models.IssueAction{TenantID: tenantID, IssueID: issue.ID, PlanID: issue.PlanID, ExecutionID: executionID, Action: status, AcceptedCount: int64(len(accepted)), CreatedAt: observedAt}).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&issue).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func completeEvidence(e *models.FailureEvidence, count int64) bool {
	if e == nil || e.Reason != "" || len(e.Scope) != 64 || int64(len(e.Keys)) != count || len(e.Keys) > models.MaxFailureKeys {
		return false
	}
	seen := map[string]bool{}
	for _, key := range e.Keys {
		if len(key) != 64 || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func retainedAcceptance(issue models.Issue, observation models.IssueObservation) []string {
	accepted := []string{}
	if observation.Passed || !completeEvidence(observation.Evidence, observation.FailedCount) {
		return accepted
	}
	var previous models.FailureEvidence
	var keys []string
	if json.Unmarshal(issue.Evidence, &previous) != nil || previous.Scope != observation.Evidence.Scope || json.Unmarshal(issue.AcceptedKeys, &keys) != nil {
		return accepted
	}
	current := map[string]bool{}
	for _, key := range observation.Evidence.Keys {
		current[key] = true
	}
	for _, key := range keys {
		if current[key] {
			accepted = append(accepted, key)
		}
	}
	return accepted
}
