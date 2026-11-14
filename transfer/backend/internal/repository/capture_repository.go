package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strconv"
	"strings"
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
		if err := RequireNoCleanupOperations(ctx, tx, identity.TenantID, identity.TaskID); err != nil {
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
		applyID, parseErr := uuid.Parse(task.ApplyIdentity)
		if parseErr != nil || applyID == uuid.Nil {
			return fmt.Errorf("capture requires a global task apply identity")
		}
		globalID := strings.ReplaceAll(applyID.String(), "-", "")
		generation := uint64(1)
		resource = models.CaptureResource{
			TaskID: identity.TaskID, TenantID: identity.TenantID, Generation: generation,
			ConnectorName: captureConnectorName(globalID, generation),
			TopicName:     captureTopicName(globalID, generation),
			ConsumerGroup: captureConsumerGroup(globalID, generation),
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
				SlotName:          captureSlotName(globalID, generation), PublicationName: capturePublicationName(globalID, generation),
				SlotOwned: false, PublicationOwned: false,
			}
			return tx.Create(resource.PostgreSQL).Error
		case models.CaptureSourceMySQL:
			serverID, err := mysqlConnectorServerID(resource.ID)
			if err != nil {
				return err
			}
			resource.MySQL = &models.MySQLCaptureResource{
				CaptureResourceID: resource.ID, ConnectorServerID: serverID,
				SchemaHistoryTopicName:  captureSchemaHistoryTopicName(globalID, generation),
				SchemaHistoryTopicOwned: true,
			}
			return tx.Create(resource.MySQL).Error
		case models.CaptureSourceOracle:
			resource.Oracle = &models.OracleCaptureResource{
				CaptureResourceID:       resource.ID,
				SchemaHistoryTopicName:  captureSchemaHistoryTopicName(globalID, generation),
				SchemaHistoryTopicOwned: true,
			}
			if len(identity.SourceSpatialInfo) > 0 {
				resource.Oracle.SpatialMirrorTableName = oracleSpatialMirrorTableName(globalID, generation)
				resource.Oracle.SpatialRowTriggerName = oracleSpatialRowTriggerName(globalID, generation)
				resource.Oracle.SpatialDDLGuardName = oracleSpatialDDLGuardName(globalID, generation)
				resource.Oracle.SpatialArtifactsOwned = true
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
	return MutateCleanup(ctx, r.db, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.CaptureResource{}).
			Where("id = ? AND resource_version = ?", id, expectedVersion).Updates(fields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("capture resource %d version conflict", id)
		}
		return nil
	})
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

func captureConnectorName(identity string, generation uint64) string {
	return "addp-cdc-" + identity + "-g" + strconv.FormatUint(generation, 36)
}
func captureTopicName(identity string, generation uint64) string {
	return "__addp_cdc." + identity + "." + strconv.FormatUint(generation, 36)
}
func captureSchemaHistoryTopicName(identity string, generation uint64) string {
	return "__addp_cdc_schema." + identity + "." + strconv.FormatUint(generation, 36)
}
func captureConsumerGroup(identity string, generation uint64) string {
	return "__addp_cdc_consumer." + identity + "." + strconv.FormatUint(generation, 36)
}
func captureSlotName(identity string, generation uint64) string {
	return "addp_cdc_" + identity + "_g" + strconv.FormatUint(generation, 36)
}
func capturePublicationName(identity string, generation uint64) string {
	return captureSlotName(identity, generation) + "_pub"
}
func oracleSpatialResourceName(role, identity string, generation uint64) string {
	sum := sha256.Sum256([]byte(captureTopicName(identity, generation)))
	return fmt.Sprintf("ADDP_%s_%x", role, sum[:10])
}
func oracleSpatialMirrorTableName(identity string, generation uint64) string {
	return oracleSpatialResourceName("M", identity, generation)
}
func oracleSpatialRowTriggerName(identity string, generation uint64) string {
	return oracleSpatialResourceName("R", identity, generation)
}
func oracleSpatialDDLGuardName(identity string, generation uint64) string {
	return oracleSpatialResourceName("D", identity, generation)
}

func mysqlConnectorServerID(captureResourceID uint) (uint32, error) {
	if captureResourceID == 0 || uint64(captureResourceID) > uint64(^uint32(0)) {
		return 0, fmt.Errorf("capture resource ID %d cannot be represented as a MySQL connector server id", captureResourceID)
	}
	return uint32(captureResourceID), nil
}

// BufferCommittedPosition reads only the current generation's source position.
func (r *CaptureRepository) BufferCommittedPosition(ctx context.Context, resource *models.CaptureResource) (*int64, bool, error) {
	var state models.SyncState
	err := r.db.WithContext(ctx).Where("task_id = ? AND source_identity = ? AND partition = ?", resource.TaskID, resource.SourceIdentity, "0").First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(state.Position) == 0 {
		return nil, false, nil
	}
	data, err := json.Marshal(state.Position)
	if err != nil {
		return nil, false, err
	}
	var position struct {
		Type      string            `json:"type"`
		Version   string            `json:"version"`
		Partition string            `json:"partition"`
		Values    map[string]string `json:"values"`
	}
	if err = json.Unmarshal(data, &position); err != nil {
		return nil, false, err
	}
	if position.Type != "kafka_offset" || position.Version != "v1" || position.Partition != "0" {
		return nil, false, fmt.Errorf("invalid CDC committed position")
	}
	offset, err := strconv.ParseInt(position.Values["next_offset"], 10, 64)
	if err != nil || offset < 0 {
		return nil, false, fmt.Errorf("invalid CDC committed offset")
	}
	return &offset, position.Values["initial_snapshot_completed"] == "true", nil
}

// ClaimPostgreSQLResources records verified namespace reservation before Connect can create objects.
func (r *CaptureRepository) ClaimPostgreSQLResources(ctx context.Context, resource *models.CaptureResource) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := NewCaptureRepository(tx).Update(ctx, resource.ID, resource.ResourceVersion, map[string]interface{}{}); err != nil {
			return err
		}
		result := tx.Model(&models.PostgreSQLCaptureResource{}).Where("capture_resource_id = ?", resource.ID).Updates(map[string]interface{}{"slot_owned": true, "publication_owned": true})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("PostgreSQL provider reservation is missing")
		}
		return nil
	})
}
