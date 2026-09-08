package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

// CleanupTaskDefinitionSpec registers one Manager TaskProvider task type with cleanup.
type CleanupTaskDefinitionSpec struct {
	TaskType string
	Table    string
}

// CleanupTaskDefinition is the common projection used by cleanup across Manager task tables.
type CleanupTaskDefinition struct {
	ID                  uint
	TenantID            uint
	TaskType            string
	Enabled             bool
	LastExecutionStatus *string
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

func (r *CleanupTaskDefinitionRepository) Disable(ctx context.Context, definition CleanupTaskDefinition, reason string) error {
	if r == nil || r.db == nil {
		return nil
	}
	spec, err := r.specForTaskType(definition.TaskType)
	if err != nil {
		return err
	}
	status := strings.TrimSpace(reason)
	if status == "" {
		status = "missing_source"
	}
	query := r.db.WithContext(ctx).Table(spec.Table).
		Where("id = ? AND tenant_id = ? AND deleted_at IS NULL AND enabled = ?", definition.ID, definition.TenantID, true)
	if spec.Table == "manager.task_definitions" {
		query = query.Where("task_type = ?", definition.TaskType)
	}
	return query.
		Updates(map[string]interface{}{
			"enabled":               false,
			"next_run_at":           nil,
			"last_execution_status": status,
			"updated_at":            time.Now(),
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
	query := r.db.WithContext(ctx).Table(spec.Table).Where("id = ? AND tenant_id = ?", definition.ID, definition.TenantID)
	if spec.Table == "manager.task_definitions" {
		query = query.Where("task_type = ?", definition.TaskType)
	}
	return query.Delete(map[string]interface{}{}).Error
}

func (r *CleanupTaskDefinitionRepository) specForTaskType(taskType string) (CleanupTaskDefinitionSpec, error) {
	for _, spec := range r.specs {
		if spec.TaskType == taskType {
			return spec, nil
		}
	}
	return CleanupTaskDefinitionSpec{}, fmt.Errorf("manager cleanup task type %q is not registered", taskType)
}
