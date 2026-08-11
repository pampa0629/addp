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

var ErrCaptureTerminal = errors.New("database CDC task is permanently stopped")

type CaptureIdentity struct {
	TaskID                      uint
	TenantID                    uint
	SourceType                  models.CaptureSourceType
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
		err := tx.Preload("PostgreSQL").Preload("MySQL").Preload("Oracle").Where("task_id = ?", identity.TaskID).Order("generation DESC").First(&resource).Error
		if err == nil {
			if captureStopInitiated(resource.Status) {
				return ErrCaptureTerminal
			}
			if resource.SourceType != identity.SourceType || resource.SourceIdentity != identity.SourceIdentity || resource.SourceConnectionFingerprint != identity.SourceConnectionFingerprint || resource.SourceEngineID != identity.SourceEngineID ||
				resource.SourceDatabase != identity.SourceDatabase || resource.SourceSchema != identity.SourceSchema || resource.SourceTable != identity.SourceTable ||
				!captureSpatialInfoEqual(resource.SourceSpatialInfo, identity.SourceSpatialInfo) {
				return fmt.Errorf("capture source identity changed after generation creation")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if identity.SourceType != models.CaptureSourcePostgreSQL && identity.SourceType != models.CaptureSourceMySQL && identity.SourceType != models.CaptureSourceOracle {
			return fmt.Errorf("unsupported capture source type %q", identity.SourceType)
		}
		generation := uint64(1)
		resource = models.CaptureResource{
			TaskID: identity.TaskID, TenantID: identity.TenantID, Generation: generation,
			ConnectorName: captureConnectorName(identity.TenantID, identity.TaskID, generation),
			TopicName:     captureTopicName(identity.TenantID, identity.TaskID, generation),
			ConsumerGroup: captureConsumerGroup(identity.TenantID, identity.TaskID, generation),
			SourceType:    identity.SourceType, SourceIdentity: identity.SourceIdentity, SourceConnectionFingerprint: identity.SourceConnectionFingerprint,
			SourceEngineID: identity.SourceEngineID,
			SourceDatabase: identity.SourceDatabase, SourceSchema: identity.SourceSchema, SourceTable: identity.SourceTable,
			SourceSpatialInfo: identity.SourceSpatialInfo,
			Status:            models.CaptureStatusProvisioning, ResourceVersion: 1,
		}
		if err := tx.Create(&resource).Error; err != nil {
			return err
		}
		switch identity.SourceType {
		case models.CaptureSourcePostgreSQL:
			resource.PostgreSQL = &models.PostgreSQLCaptureResource{
				CaptureResourceID: resource.ID,
				SlotName:          captureSlotName(identity.TenantID, identity.TaskID, generation), PublicationName: capturePublicationName(identity.TenantID, identity.TaskID, generation),
				SlotOwned: true, PublicationOwned: true,
			}
			return tx.Create(resource.PostgreSQL).Error
		case models.CaptureSourceMySQL:
			serverID, err := mysqlConnectorServerID(resource.ID)
			if err != nil {
				return err
			}
			resource.MySQL = &models.MySQLCaptureResource{
				CaptureResourceID: resource.ID, ConnectorServerID: serverID,
				SchemaHistoryTopicName:  captureSchemaHistoryTopicName(identity.TenantID, identity.TaskID, generation),
				SchemaHistoryTopicOwned: true,
			}
			return tx.Create(resource.MySQL).Error
		case models.CaptureSourceOracle:
			resource.Oracle = &models.OracleCaptureResource{
				CaptureResourceID:       resource.ID,
				SchemaHistoryTopicName:  captureSchemaHistoryTopicName(identity.TenantID, identity.TaskID, generation),
				SchemaHistoryTopicOwned: true,
			}
			return tx.Create(resource.Oracle).Error
		default:
			return fmt.Errorf("unsupported capture source type %q", identity.SourceType)
		}
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
	err := r.db.WithContext(ctx).Preload("PostgreSQL").Preload("MySQL").Preload("Oracle").
		Where("task_id = ? AND tenant_id = ?", taskID, tenantID).Order("generation DESC").First(&resource).Error
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
		Preload("PostgreSQL").Preload("MySQL").Preload("Oracle").
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

func (r *CaptureRepository) HasStopInitiatedGeneration(ctx context.Context, taskID, tenantID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.CaptureResource{}).
		Where("task_id = ? AND tenant_id = ? AND status IN ?", taskID, tenantID, []models.CaptureStatus{
			models.CaptureStatusCleaning,
			models.CaptureStatusCleanupFailed,
			models.CaptureStatusStopped,
		}).Count(&count).Error
	return count > 0, err
}

func captureStopInitiated(status models.CaptureStatus) bool {
	return status == models.CaptureStatusCleaning || status == models.CaptureStatusCleanupFailed || status == models.CaptureStatusStopped
}

func captureConnectorName(tenantID, taskID uint, generation uint64) string {
	return fmt.Sprintf("addp-cdc-t%d-task%d-g%d", tenantID, taskID, generation)
}

func captureTopicName(tenantID, taskID uint, generation uint64) string {
	return fmt.Sprintf("__addp_cdc.%d.%d.%d", tenantID, taskID, generation)
}

func captureSchemaHistoryTopicName(tenantID, taskID uint, generation uint64) string {
	return fmt.Sprintf("__addp_cdc_schema.%d.%d.%d", tenantID, taskID, generation)
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

func mysqlConnectorServerID(captureResourceID uint) (uint32, error) {
	if captureResourceID == 0 || uint64(captureResourceID) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("capture resource ID %d cannot be represented as a MySQL connector server id", captureResourceID)
	}
	return uint32(captureResourceID), nil
}
