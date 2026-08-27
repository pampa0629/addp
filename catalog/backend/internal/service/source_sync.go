package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	metaDataItemFeedName = "data_item_changes"
	metaChangeBatchSize  = 200
)

type MetaChangeSource interface {
	ListDataItemChanges(ctx context.Context, tenantID uint, afterCursor string, limit int) (*commonClient.MetaDataItemChangesResponse, error)
}

type TenantMetaChangeSource struct {
	client *commonClient.MetaClient
}

func NewTenantMetaChangeSource(client *commonClient.MetaClient) *TenantMetaChangeSource {
	return &TenantMetaChangeSource{client: client}
}

func (s *TenantMetaChangeSource) ListDataItemChanges(ctx context.Context, tenantID uint, afterCursor string, limit int) (*commonClient.MetaDataItemChangesResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("Meta change source is unavailable")
	}
	return s.client.WithTenantID(tenantID).ListDataItemChanges(ctx, afterCursor, limit)
}

type SourceSyncService struct {
	db     *gorm.DB
	source MetaChangeSource

	mu          sync.Mutex
	tenantLocks map[int64]*sync.Mutex
}

func NewSourceSyncService(db *gorm.DB, source MetaChangeSource) *SourceSyncService {
	return &SourceSyncService{db: db, source: source, tenantLocks: make(map[int64]*sync.Mutex)}
}

func (s *SourceSyncService) SourceName() string { return "Meta" }

func (s *SourceSyncService) SyncTenant(ctx context.Context, tenantID int64) error {
	if tenantID <= 0 {
		return fmt.Errorf("%w: tenant ID is required", ErrInvalidSourceChange)
	}
	lock := s.tenantLock(tenantID)
	lock.Lock()
	defer lock.Unlock()

	for {
		cursor, err := s.currentCursor(ctx, tenantID)
		if err != nil {
			return err
		}
		batch, err := s.source.ListDataItemChanges(ctx, uint(tenantID), cursor, metaChangeBatchSize)
		if err != nil {
			return fmt.Errorf("pull Meta DataItem changes for tenant %d: %w", tenantID, err)
		}
		if batch == nil || batch.SchemaVersion != "meta.data_item_changes/v1" {
			return fmt.Errorf("%w: unsupported Meta change schema", ErrInvalidSourceChange)
		}
		if err := s.applyBatch(ctx, tenantID, cursor, batch); err != nil {
			return err
		}
		if !batch.HasMore {
			return nil
		}
	}
}

func (s *SourceSyncService) tenantLock(tenantID int64) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lock := s.tenantLocks[tenantID]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	s.tenantLocks[tenantID] = lock
	return lock
}

func (s *SourceSyncService) currentCursor(ctx context.Context, tenantID int64) (string, error) {
	var checkpoint models.SourceCheckpoint
	err := s.db.WithContext(ctx).Where(
		"tenant_id = ? AND source_module = ? AND feed_name = ?",
		tenantID, models.SourceModuleMeta, metaDataItemFeedName,
	).First(&checkpoint).Error
	if err == nil {
		return checkpoint.Cursor, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return "", fmt.Errorf("read Catalog source checkpoint: %w", err)
	}
	return "", nil
}

func (s *SourceSyncService) applyBatch(ctx context.Context, tenantID int64, expectedCursor string, batch *commonClient.MetaDataItemChangesResponse) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		checkpoint, err := lockCheckpoint(tx, tenantID)
		if err != nil {
			return err
		}
		if checkpoint.Cursor != expectedCursor {
			return fmt.Errorf("Catalog source checkpoint changed concurrently")
		}
		for _, change := range batch.Changes {
			if err := applyMetaDataItemChange(tx, tenantID, change); err != nil {
				return err
			}
		}
		checkpoint.Cursor = batch.NextCursor
		if err := tx.Save(checkpoint).Error; err != nil {
			return fmt.Errorf("advance Catalog source checkpoint: %w", err)
		}
		return nil
	})
}

func lockCheckpoint(tx *gorm.DB, tenantID int64) (*models.SourceCheckpoint, error) {
	var checkpoint models.SourceCheckpoint
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND source_module = ? AND feed_name = ?",
		tenantID, models.SourceModuleMeta, metaDataItemFeedName,
	).First(&checkpoint).Error
	if err == nil {
		return &checkpoint, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("lock Catalog source checkpoint: %w", err)
	}
	checkpoint = models.SourceCheckpoint{
		TenantID: tenantID, SourceModule: models.SourceModuleMeta,
		FeedName: metaDataItemFeedName, Cursor: "",
	}
	if err := tx.Create(&checkpoint).Error; err != nil {
		return nil, fmt.Errorf("create Catalog source checkpoint: %w", err)
	}
	return &checkpoint, nil
}

func applyMetaDataItemChange(tx *gorm.DB, tenantID int64, change commonClient.MetaDataItemChange) error {
	identity := strings.TrimSpace(change.SourceIdentity)
	if identity == "" || len(change.SourceVersion) != 20 || (change.Operation != "upsert" && change.Operation != "missing") {
		return fmt.Errorf("%w: malformed Meta DataItem change", ErrInvalidSourceChange)
	}
	var binding models.SourceBinding
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND source_module = ? AND source_type = ? AND source_identity = ? AND is_current = ?",
		tenantID, models.SourceModuleMeta, models.SourceTypeDataItem, identity, true,
	).First(&binding).Error
	if err == gorm.ErrRecordNotFound {
		return createEntryFromMetaChange(tx, tenantID, change)
	}
	if err != nil {
		return fmt.Errorf("find Catalog source binding: %w", err)
	}
	if binding.SourceVersion >= change.SourceVersion {
		return nil
	}

	status := models.SourceStatusActive
	var missingAt *time.Time
	var missingReason *string
	if change.Operation == "missing" {
		status = models.SourceStatusMissing
		observedAt := change.ObservedAt.UTC()
		missingAt = &observedAt
		reason := "source_not_observed"
		missingReason = &reason
	}
	if err := tx.Model(&binding).Updates(map[string]interface{}{
		"source_status": status, "source_version": change.SourceVersion,
		"observed_snapshot": commonModels.JSONMap(change.Snapshot), "observed_at": change.ObservedAt,
		"missing_at": missingAt, "missing_reason": missingReason,
	}).Error; err != nil {
		return fmt.Errorf("update Catalog source binding: %w", err)
	}
	if err := syncComponents(tx, tenantID, binding.CatalogEntryID, change); err != nil {
		return err
	}
	if err := tx.Model(&models.Entry{}).Where("tenant_id = ? AND id = ?", tenantID, binding.CatalogEntryID).
		UpdateColumn("version", gorm.Expr("version + 1")).Error; err != nil {
		return fmt.Errorf("advance Catalog entry version: %w", err)
	}
	return enqueueProjection(tx, tenantID, binding.CatalogEntryID)
}

func createEntryFromMetaChange(tx *gorm.DB, tenantID int64, change commonClient.MetaDataItemChange) error {
	now := change.ObservedAt.UTC()
	entry := models.Entry{
		ID: uuid.New(), TenantID: tenantID, EntryType: models.EntryTypeDataItem,
		EntryStatus: models.EntryStatusActive, GovernanceStatus: models.GovernanceStatusDiscovered,
		Visibility: models.VisibilityInventory, Version: 1,
	}
	if err := tx.Create(&entry).Error; err != nil {
		return fmt.Errorf("create Catalog entry: %w", err)
	}
	status := models.SourceStatusActive
	var missingAt *time.Time
	var missingReason *string
	if change.Operation == "missing" {
		status = models.SourceStatusMissing
		missingAt = &now
		reason := "source_not_observed"
		missingReason = &reason
	}
	binding := models.SourceBinding{
		ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID,
		SourceModule: models.SourceModuleMeta, SourceType: models.SourceTypeDataItem,
		SourceIdentity: change.SourceIdentity, SourceStatus: status, SourceVersion: change.SourceVersion,
		IsCurrent: true, BoundAt: now, MissingAt: missingAt, MissingReason: missingReason,
		ObservedSnapshot: commonModels.JSONMap(change.Snapshot), ObservedAt: now,
	}
	if err := tx.Create(&binding).Error; err != nil {
		return fmt.Errorf("create Catalog source binding: %w", err)
	}
	if err := syncComponents(tx, tenantID, entry.ID, change); err != nil {
		return err
	}
	if err := tx.Create(&models.AuditEvent{
		ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID,
		EventType: "source_discovered", ActorType: "service_principal", ActorID: "addp-catalog",
		Details: commonModels.JSONMap{"source_module": models.SourceModuleMeta, "source_identity": change.SourceIdentity},
	}).Error; err != nil {
		return fmt.Errorf("create Catalog source audit: %w", err)
	}
	return enqueueProjection(tx, tenantID, entry.ID)
}

func syncComponents(tx *gorm.DB, tenantID int64, entryID uuid.UUID, change commonClient.MetaDataItemChange) error {
	if change.Operation == "missing" {
		if err := tx.Model(&models.Component{}).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID).
			Update("component_status", models.SourceStatusMissing).Error; err != nil {
			return fmt.Errorf("mark Catalog components missing: %w", err)
		}
		return nil
	}
	fields := datatype.FieldInfosFromPayload(change.Snapshot["fields"])
	activeKeys := make([]string, 0, len(fields))
	for index, field := range fields {
		key := strings.TrimSpace(field.Name)
		if key == "" {
			continue
		}
		activeKeys = append(activeKeys, key)
		var component models.Component
		err := tx.Where("tenant_id = ? AND catalog_entry_id = ? AND component_key = ?", tenantID, entryID, key).First(&component).Error
		payload := datatype.FieldInfoPayload([]datatype.FieldInfo{field})
		snapshot := commonModels.JSONMap{}
		if len(payload) == 1 {
			snapshot = commonModels.JSONMap(payload[0])
		}
		if err == gorm.ErrRecordNotFound {
			component = models.Component{
				ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entryID,
				ComponentKey: key, DisplayName: key, DataType: string(field.Type),
				ComponentStatus: models.SourceStatusActive, Ordinal: index, ObservedSnapshot: snapshot,
			}
			if err := tx.Create(&component).Error; err != nil {
				return fmt.Errorf("create Catalog component: %w", err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("find Catalog component: %w", err)
		}
		if err := tx.Model(&component).Updates(map[string]interface{}{
			"display_name": key, "data_type": string(field.Type), "component_status": models.SourceStatusActive,
			"ordinal": index, "observed_snapshot": snapshot,
		}).Error; err != nil {
			return fmt.Errorf("update Catalog component: %w", err)
		}
	}
	missingQuery := tx.Model(&models.Component{}).Where("tenant_id = ? AND catalog_entry_id = ?", tenantID, entryID)
	if len(activeKeys) > 0 {
		missingQuery = missingQuery.Where("component_key NOT IN ?", activeKeys)
	}
	if err := missingQuery.Update("component_status", models.SourceStatusMissing).Error; err != nil {
		return fmt.Errorf("mark absent Catalog components missing: %w", err)
	}
	return nil
}

func enqueueProjection(tx *gorm.DB, tenantID int64, entryID uuid.UUID) error {
	now := time.Now().UTC()
	if err := tx.Create(&models.ProjectionTask{
		TenantID: tenantID, CatalogEntryID: entryID, Projection: "catalog_entries",
		Status: "pending", AvailableAt: now,
	}).Error; err != nil {
		return fmt.Errorf("enqueue Catalog projection: %w", err)
	}
	return nil
}
