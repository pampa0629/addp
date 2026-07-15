package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCaptureTerminal = errors.New("PostgreSQL CDC task is permanently stopped")

type CaptureIdentity struct {
	TaskID                      uint
	TenantID                    uint
	SourceIdentity              string
	SourceConnectionFingerprint string
	SourceEngineID              uint
	SourceDatabase              string
	SourceSchema                string
	SourceTable                 string
	SourceSpatialInfo           models.JSONMap
}

type CaptureRepository struct {
	db *gorm.DB
}

func NewCaptureRepository(db *gorm.DB) *CaptureRepository {
	return &CaptureRepository{db: db}
}

// BeginGeneration 原子锁定 task 并创建或返回唯一 capture generation。
// failed/provisioning/running generation 都会被复用；stopped generation 是不可逆终态。
func (r *CaptureRepository) BeginGeneration(ctx context.Context, identity CaptureIdentity) (*models.CaptureResource, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("capture repository database is not configured")
	}
	var resource models.CaptureResource
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", identity.TaskID, identity.TenantID).First(&task).Error; err != nil {
			return err
		}
		err := tx.Where("task_id = ?", identity.TaskID).Order("generation DESC").First(&resource).Error
		if err == nil {
			if resource.Status == models.CaptureStatusStopped {
				return ErrCaptureTerminal
			}
			if resource.SourceIdentity != identity.SourceIdentity || resource.SourceConnectionFingerprint != identity.SourceConnectionFingerprint || resource.SourceEngineID != identity.SourceEngineID ||
				resource.SourceDatabase != identity.SourceDatabase || resource.SourceSchema != identity.SourceSchema || resource.SourceTable != identity.SourceTable ||
				!captureSpatialInfoEqual(resource.SourceSpatialInfo, identity.SourceSpatialInfo) {
				return fmt.Errorf("capture source identity changed after generation creation")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		generation := uint64(1)
		resource = models.CaptureResource{
			TaskID: identity.TaskID, TenantID: identity.TenantID, Generation: generation,
			ConnectorName:   captureConnectorName(identity.TenantID, identity.TaskID, generation),
			TopicName:       captureTopicName(identity.TenantID, identity.TaskID, generation),
			ConsumerGroup:   captureConsumerGroup(identity.TenantID, identity.TaskID, generation),
			SlotName:        captureSlotName(identity.TenantID, identity.TaskID, generation),
			PublicationName: capturePublicationName(identity.TenantID, identity.TaskID, generation),
			SourceIdentity:  identity.SourceIdentity, SourceConnectionFingerprint: identity.SourceConnectionFingerprint,
			SourceEngineID: identity.SourceEngineID,
			SourceDatabase: identity.SourceDatabase, SourceSchema: identity.SourceSchema, SourceTable: identity.SourceTable,
			SourceSpatialInfo: identity.SourceSpatialInfo,
			Status:            models.CaptureStatusProvisioning, SlotOwned: true, PublicationOwned: true, ResourceVersion: 1,
		}
		return tx.Create(&resource).Error
	})
	return &resource, err
}

func captureSpatialInfoEqual(left, right models.JSONMap) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func (r *CaptureRepository) GetLatest(ctx context.Context, taskID, tenantID uint) (*models.CaptureResource, error) {
	var resource models.CaptureResource
	err := r.db.WithContext(ctx).Where("task_id = ? AND tenant_id = ?", taskID, tenantID).Order("generation DESC").First(&resource).Error
	if err != nil {
		return nil, err
	}
	return &resource, nil
}

func (r *CaptureRepository) ListObservable(ctx context.Context, limit int) ([]models.CaptureResource, error) {
	if limit <= 0 {
		limit = 100
	}
	var resources []models.CaptureResource
	err := r.db.WithContext(ctx).
		Where("connector_created = ? AND status IN ?", true, []models.CaptureStatus{models.CaptureStatusRunning, models.CaptureStatusFailed}).
		Order("updated_at ASC").Limit(limit).Find(&resources).Error
	return resources, err
}

func (r *CaptureRepository) Update(ctx context.Context, id uint, expectedVersion uint64, fields map[string]interface{}) error {
	if fields == nil {
		fields = map[string]interface{}{}
	}
	fields["resource_version"] = gorm.Expr("resource_version + 1")
	fields["updated_at"] = time.Now()
	result := r.db.WithContext(ctx).Model(&models.CaptureResource{}).
		Where("id = ? AND resource_version = ?", id, expectedVersion).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("capture resource %d version conflict", id)
	}
	return nil
}

func (r *CaptureRepository) ForceUpdate(ctx context.Context, id uint, fields map[string]interface{}) error {
	if fields == nil {
		fields = map[string]interface{}{}
	}
	fields["resource_version"] = gorm.Expr("resource_version + 1")
	fields["updated_at"] = time.Now()
	return r.db.WithContext(ctx).Model(&models.CaptureResource{}).Where("id = ?", id).Updates(fields).Error
}

func (r *CaptureRepository) HasTerminalGeneration(ctx context.Context, taskID, tenantID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.CaptureResource{}).
		Where("task_id = ? AND tenant_id = ? AND status = ?", taskID, tenantID, models.CaptureStatusStopped).Count(&count).Error
	return count > 0, err
}

func captureConnectorName(tenantID, taskID uint, generation uint64) string {
	return fmt.Sprintf("addp-cdc-t%d-task%d-g%d", tenantID, taskID, generation)
}

func captureTopicName(tenantID, taskID uint, generation uint64) string {
	return fmt.Sprintf("__addp_cdc.%d.%d.%d", tenantID, taskID, generation)
}

func captureConsumerGroup(tenantID, taskID uint, generation uint64) string {
	return fmt.Sprintf("__addp_cdc_consumer.%d.%d.%d", tenantID, taskID, generation)
}

func captureSlotName(tenantID, taskID uint, generation uint64) string {
	return fmt.Sprintf("addp_cdc_t%d_task%d_g%d", tenantID, taskID, generation)
}

func capturePublicationName(tenantID, taskID uint, generation uint64) string {
	return fmt.Sprintf("addp_cdc_t%d_task%d_g%d_pub", tenantID, taskID, generation)
}
