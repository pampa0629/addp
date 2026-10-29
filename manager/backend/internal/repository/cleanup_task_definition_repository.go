package repository

import (
	"context"
	"encoding/json"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"gorm.io/gorm/clause"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// CleanupTaskDefinitionSpec registers one Manager TaskProvider task type with cleanup.
type CleanupTaskDefinitionSpec struct {
	TaskType string
	Table    string
}

// CleanupTaskDefinition is the common projection used by cleanup across Manager task tables.
type CleanupTaskDefinition struct {
	Version             uint64
	UpdatedAt           time.Time
	ID                  uint
	TenantID            uint
	TaskType            string
	Enabled             bool
	LastExecutionStatus *string
	BindingStatus       string
	BindingIssue        string
	Config              commonModels.JSONMap
	SourceEngineID      uint
	ItemID              uint
	ItemFingerprint     string
	Locator             string
	ResourceBindings    []CleanupTaskResourceBinding `gorm:"-"`
	CleanupReason       string                       `gorm:"-"`
}

type CleanupTaskResourceBinding struct {
	Role            string
	EngineID        uint
	ItemID          *uint
	ItemFingerprint string
	Locator         string
}

type CleanupTaskDefinitionRepository struct {
	db    *gorm.DB
	specs []CleanupTaskDefinitionSpec
}

type CleanupManagedArtifactSpec struct {
	TaskType       string
	Table          string
	BuildingStatus string
}

type CleanupManagedArtifact struct {
	ID              uint
	TenantID        uint
	TaskType        string
	SourceEngineID  uint
	ItemID          uint
	ItemFingerprint string
	Locator         string
	Status          string
	SizeBytes       int64
	SourceSizeBytes int64
}

type CleanupManagedArtifactRepository struct {
	db    *gorm.DB
	specs []CleanupManagedArtifactSpec
}

func NewCleanupManagedArtifactRepository(db *gorm.DB, specs []CleanupManagedArtifactSpec) *CleanupManagedArtifactRepository {
	return &CleanupManagedArtifactRepository{db: db, specs: append([]CleanupManagedArtifactSpec(nil), specs...)}
}

func (r *CleanupManagedArtifactRepository) List(ctx context.Context, tenantID uint) ([]CleanupManagedArtifact, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	artifacts := make([]CleanupManagedArtifact, 0)
	for _, spec := range r.specs {
		var rows []CleanupManagedArtifact
		query := r.db.WithContext(ctx).Table(spec.Table).Where("deleted_at IS NULL")
		if tenantID > 0 {
			query = query.Where("tenant_id = ?", tenantID)
		}
		if err := query.Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("list cleanup artifacts for %s: %w", spec.TaskType, err)
		}
		for index := range rows {
			rows[index].TaskType = spec.TaskType
		}
		artifacts = append(artifacts, rows...)
	}
	return artifacts, nil
}

func (r *CleanupManagedArtifactRepository) MarkMissingSource(ctx context.Context, artifact CleanupManagedArtifact) error {
	if r == nil || r.db == nil {
		return nil
	}
	spec, err := r.managedArtifactSpec(artifact.TaskType)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Table(spec.Table).
		Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", artifact.ID, artifact.TenantID).
		Updates(map[string]interface{}{
			"status": "deleted", "error_message": "resource reclaim logical cleanup: missing source", "updated_at": time.Now(),
		}).Error
}

func (r *CleanupManagedArtifactRepository) managedArtifactSpec(taskType string) (CleanupManagedArtifactSpec, error) {
	for _, spec := range r.specs {
		if spec.TaskType == taskType {
			return spec, nil
		}
	}
	return CleanupManagedArtifactSpec{}, fmt.Errorf("manager cleanup artifact type %q is not registered", taskType)
}

func NewCleanupTaskDefinitionRepository(db *gorm.DB, specs []CleanupTaskDefinitionSpec) *CleanupTaskDefinitionRepository {
	return &CleanupTaskDefinitionRepository{db: db, specs: append([]CleanupTaskDefinitionSpec(nil), specs...)}
}

func (r *CleanupTaskDefinitionRepository) List(ctx context.Context, tenantID uint) ([]CleanupTaskDefinition, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	definitions := make([]CleanupTaskDefinition, 0)
	for _, spec := range r.specs {
		if strings.TrimSpace(spec.TaskType) == "" || strings.TrimSpace(spec.Table) == "" {
			return nil, fmt.Errorf("manager cleanup task definition registration is incomplete")
		}
		var rows []CleanupTaskDefinition
		query := r.db.WithContext(ctx).Table(spec.Table).Where("deleted_at IS NULL")
		if spec.Table == "manager.task_definitions" {
			query = query.Where("task_type = ?", spec.TaskType)
		}
		if tenantID > 0 {
			query = query.Where("tenant_id = ?", tenantID)
		}
		if err := query.Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("list cleanup task definitions for %s: %w", spec.TaskType, err)
		}
		for index := range rows {
			rows[index].TaskType = spec.TaskType
			if spec.Table == "manager.task_definitions" {
				var bindings []CleanupTaskResourceBinding
				if err := r.db.WithContext(ctx).Table("manager.task_resource_bindings").
					Select("role, engine_id, item_id, item_fingerprint, locator").
					Where("task_definition_id = ? AND tenant_id = ?", rows[index].ID, rows[index].TenantID).
					Order("role, ordinal").Find(&bindings).Error; err != nil {
					return nil, fmt.Errorf("list cleanup task resource bindings for %s/%d: %w", spec.TaskType, rows[index].ID, err)
				}
				rows[index].ResourceBindings = bindings
			}
		}
		definitions = append(definitions, rows...)
	}
	return definitions, nil
}

func (r *CleanupTaskDefinitionRepository) MarkBindingMissing(ctx context.Context, definition CleanupTaskDefinition, reason string) error {
	if r == nil || r.db == nil {
		return nil
	}
	spec, err := r.specForTaskType(definition.TaskType)
	if err != nil {
		return err
	}
	issue := strings.TrimSpace(reason)
	if issue == "" {
		issue = models.TaskBindingIssueMissingSource
	}
	if issue != models.TaskBindingIssueMissingEngine && issue != models.TaskBindingIssueMissingSource {
		return fmt.Errorf("unsupported Manager task binding issue %q", issue)
	}
	query := r.db.WithContext(ctx).Table(spec.Table).
		Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", definition.ID, definition.TenantID)
	if spec.Table == "manager.task_definitions" {
		query = query.Where("task_type = ?", definition.TaskType)
	}
	return query.
		Updates(map[string]interface{}{
			"enabled":        false,
			"next_run_at":    nil,
			"binding_status": models.TaskBindingStatusMissing,
			"binding_issue":  issue,
			"updated_at":     time.Now(),
		}).Error
}

func (r *CleanupTaskDefinitionRepository) HardDelete(ctx context.Context, definition CleanupTaskDefinition) error {
	if r == nil || r.db == nil {
		return nil
	}
	spec, err := r.specForTaskType(definition.TaskType)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if spec.Table == "manager.task_definitions" {
			if err := tx.Table("manager.task_resource_bindings").
				Where("task_definition_id = ? AND tenant_id = ?", definition.ID, definition.TenantID).
				Delete(map[string]interface{}{}).Error; err != nil {
				return err
			}
		}
		query := tx.Table(spec.Table).Where("id = ? AND tenant_id = ?", definition.ID, definition.TenantID)
		if spec.Table == "manager.task_definitions" {
			query = query.Where("task_type = ?", definition.TaskType)
		}
		return query.Delete(map[string]interface{}{}).Error
	})
}

func (r *CleanupTaskDefinitionRepository) specForTaskType(taskType string) (CleanupTaskDefinitionSpec, error) {
	for _, spec := range r.specs {
		if spec.TaskType == taskType {
			return spec, nil
		}
	}
	return CleanupTaskDefinitionSpec{}, fmt.Errorf("manager cleanup task type %q is not registered", taskType)
}

// VerifyFrozen locks the exact definition and compares its full registered
// projection, including resource bindings. External authority was read earlier.
func (r *CleanupTaskDefinitionRepository) VerifyFrozen(ctx context.Context, tx *gorm.DB, expected CleanupTaskDefinition) error {
	spec, err := r.specForTaskType(expected.TaskType)
	if err != nil {
		return err
	}
	var locked CleanupTaskDefinition
	q := tx.WithContext(ctx).Table(spec.Table).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND deleted_at IS NULL", expected.ID, expected.TenantID)
	if spec.Table == "manager.task_definitions" {
		q = q.Where("task_type=?", expected.TaskType)
	}
	if err := q.Take(&locked).Error; err != nil {
		return err
	}
	rows, err := NewCleanupTaskDefinitionRepository(tx, []CleanupTaskDefinitionSpec{spec}).List(ctx, expected.TenantID)
	if err != nil {
		return err
	}
	expected.CleanupReason = ""
	for _, current := range rows {
		if current.ID == expected.ID {
			a, e := json.Marshal(expected)
			if e != nil {
				return e
			}
			b, e := json.Marshal(current)
			if e != nil {
				return e
			}
			if string(a) != string(b) {
				return ErrTaskDefinitionVersionConflict
			}
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

// ApplyFrozen is part of the caller's C lease/result transaction. Admission for
// task runs uses the same task row; queued or running executions block cleanup.
func (r *CleanupTaskDefinitionRepository) ApplyFrozen(ctx context.Context, tx *gorm.DB, expected CleanupTaskDefinition, physical bool) error {
	if err := r.VerifyFrozen(ctx, tx, expected); err != nil {
		return err
	}
	var active int64
	if err := tx.WithContext(ctx).Model(&commonExecution.TaskExecution{}).Where("tenant_id=? AND module='manager' AND task_type=? AND source_task_id=? AND status IN ?", expected.TenantID, expected.TaskType, fmt.Sprint(expected.ID), []string{"pending", "running"}).Count(&active).Error; err != nil {
		return err
	}
	if active != 0 {
		return fmt.Errorf("Manager cleanup task still active")
	}
	scoped := NewCleanupTaskDefinitionRepository(tx, r.specs)
	if physical {
		return scoped.HardDelete(ctx, expected)
	}
	spec, err := r.specForTaskType(expected.TaskType)
	if err != nil {
		return err
	}
	if spec.Table != "manager.task_definitions" {
		return tx.WithContext(ctx).Table(spec.Table).Where("id=? AND tenant_id=?", expected.ID, expected.TenantID).Updates(map[string]interface{}{"enabled": false, "next_run_at": nil, "updated_at": time.Now().UTC()}).Error
	}
	if err := scoped.MarkBindingMissing(ctx, expected, expected.CleanupReason); err != nil {
		return err
	}
	return tx.WithContext(ctx).Table(spec.Table).Where("id=? AND tenant_id=?", expected.ID, expected.TenantID).UpdateColumn("version", gorm.Expr("version + 1")).Error
}
