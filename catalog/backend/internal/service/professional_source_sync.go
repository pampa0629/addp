package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	professionalCatalogFeedName = "catalog_resource_changes"
	professionalChangeBatchSize = 200
)

type ProfessionalResourceChange struct {
	SourceType, SourceIdentity, Operation, SourceVersion string
	ObservedAt                                           time.Time
	Snapshot                                             map[string]any
}

type ProfessionalChangeBatch struct {
	SchemaVersion string
	Changes       []ProfessionalResourceChange
	NextCursor    string
	HasMore       bool
}

type ProfessionalChangeSource interface {
	SourceModule() string
	SourceName() string
	SchemaVersion() string
	ListCatalogResourceChanges(context.Context, uint, string, int) (*ProfessionalChangeBatch, error)
}

type TenantModelChangeSource struct{ client *commonClient.ModelClient }

func NewTenantModelChangeSource(client *commonClient.ModelClient) ProfessionalChangeSource {
	return &TenantModelChangeSource{client: client}
}

func (*TenantModelChangeSource) SourceModule() string { return models.SourceModuleModel }
func (*TenantModelChangeSource) SourceName() string   { return "Model" }
func (*TenantModelChangeSource) SchemaVersion() string {
	return commonClient.ModelCatalogResourceChangesSchemaVersion
}

func (s *TenantModelChangeSource) ListCatalogResourceChanges(ctx context.Context, tenantID uint, cursor string, limit int) (*ProfessionalChangeBatch, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("Model change source is unavailable")
	}
	batch, err := s.client.WithTenantID(tenantID).ListCatalogResourceChanges(ctx, cursor, limit)
	if err != nil {
		return nil, err
	}
	changes := make([]ProfessionalResourceChange, 0, len(batch.Changes))
	for _, item := range batch.Changes {
		changes = append(changes, ProfessionalResourceChange{SourceType: item.SourceType, SourceIdentity: item.SourceIdentity, Operation: item.Operation, SourceVersion: item.SourceVersion, ObservedAt: item.ObservedAt, Snapshot: item.Snapshot})
	}
	return &ProfessionalChangeBatch{SchemaVersion: batch.SchemaVersion, Changes: changes, NextCursor: batch.NextCursor, HasMore: batch.HasMore}, nil
}

type TenantStandardChangeSource struct{ client *commonClient.StandardClient }

func NewTenantStandardChangeSource(client *commonClient.StandardClient) ProfessionalChangeSource {
	return &TenantStandardChangeSource{client: client}
}

type TenantServiceChangeSource struct{ client *commonClient.ServiceClient }

func NewTenantServiceChangeSource(client *commonClient.ServiceClient) ProfessionalChangeSource {
	return &TenantServiceChangeSource{client: client}
}

type TenantDevelopChangeSource struct{ client *commonClient.DevelopClient }

func NewTenantDevelopChangeSource(client *commonClient.DevelopClient) ProfessionalChangeSource {
	return &TenantDevelopChangeSource{client: client}
}

func (*TenantDevelopChangeSource) SourceModule() string { return models.SourceModuleDevelop }
func (*TenantDevelopChangeSource) SourceName() string   { return "Develop" }
func (*TenantDevelopChangeSource) SchemaVersion() string {
	return commonClient.DevelopCatalogResourceChangesSchemaVersion
}

func (s *TenantDevelopChangeSource) ListCatalogResourceChanges(ctx context.Context, tenantID uint, cursor string, limit int) (*ProfessionalChangeBatch, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("Develop change source is unavailable")
	}
	batch, err := s.client.WithTenantID(tenantID).ListCatalogResourceChanges(ctx, cursor, limit)
	if err != nil {
		return nil, err
	}
	changes := make([]ProfessionalResourceChange, 0, len(batch.Changes))
	for _, item := range batch.Changes {
		changes = append(changes, ProfessionalResourceChange{SourceType: item.SourceType, SourceIdentity: item.SourceIdentity, Operation: item.Operation, SourceVersion: item.SourceVersion, ObservedAt: item.ObservedAt, Snapshot: item.Snapshot})
	}
	return &ProfessionalChangeBatch{SchemaVersion: batch.SchemaVersion, Changes: changes, NextCursor: batch.NextCursor, HasMore: batch.HasMore}, nil
}

func (*TenantServiceChangeSource) SourceModule() string { return models.SourceModuleService }
func (*TenantServiceChangeSource) SourceName() string   { return "Service" }
func (*TenantServiceChangeSource) SchemaVersion() string {
	return commonClient.ServiceCatalogResourceChangesSchemaVersion
}

func (s *TenantServiceChangeSource) ListCatalogResourceChanges(ctx context.Context, tenantID uint, cursor string, limit int) (*ProfessionalChangeBatch, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("Service change source is unavailable")
	}
	batch, err := s.client.WithTenantID(tenantID).ListCatalogResourceChanges(ctx, cursor, limit)
	if err != nil {
		return nil, err
	}
	changes := make([]ProfessionalResourceChange, 0, len(batch.Changes))
	for _, item := range batch.Changes {
		changes = append(changes, ProfessionalResourceChange{SourceType: item.SourceType, SourceIdentity: item.SourceIdentity, Operation: item.Operation, SourceVersion: item.SourceVersion, ObservedAt: item.ObservedAt, Snapshot: item.Snapshot})
	}
	return &ProfessionalChangeBatch{SchemaVersion: batch.SchemaVersion, Changes: changes, NextCursor: batch.NextCursor, HasMore: batch.HasMore}, nil
}

func (*TenantStandardChangeSource) SourceModule() string { return models.SourceModuleStandard }
func (*TenantStandardChangeSource) SourceName() string   { return "Standard" }
func (*TenantStandardChangeSource) SchemaVersion() string {
	return commonClient.StandardCatalogResourceChangesSchemaVersion
}

func (s *TenantStandardChangeSource) ListCatalogResourceChanges(ctx context.Context, tenantID uint, cursor string, limit int) (*ProfessionalChangeBatch, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("Standard change source is unavailable")
	}
	batch, err := s.client.WithTenantID(tenantID).ListCatalogResourceChanges(ctx, cursor, limit)
	if err != nil {
		return nil, err
	}
	changes := make([]ProfessionalResourceChange, 0, len(batch.Changes))
	for _, item := range batch.Changes {
		changes = append(changes, ProfessionalResourceChange{SourceType: item.SourceType, SourceIdentity: item.SourceIdentity, Operation: item.Operation, SourceVersion: item.SourceVersion, ObservedAt: item.ObservedAt, Snapshot: item.Snapshot})
	}
	return &ProfessionalChangeBatch{SchemaVersion: batch.SchemaVersion, Changes: changes, NextCursor: batch.NextCursor, HasMore: batch.HasMore}, nil
}

type ProfessionalSourceSyncService struct {
	db     *gorm.DB
	source ProfessionalChangeSource
	mu     sync.Mutex
	locks  map[int64]*sync.Mutex
}

func NewProfessionalSourceSyncService(db *gorm.DB, source ProfessionalChangeSource) *ProfessionalSourceSyncService {
	return &ProfessionalSourceSyncService{db: db, source: source, locks: make(map[int64]*sync.Mutex)}
}

func (s *ProfessionalSourceSyncService) SourceName() string {
	if s == nil || s.source == nil {
		return "Professional"
	}
	return s.source.SourceName()
}

func (s *ProfessionalSourceSyncService) SyncTenant(ctx context.Context, tenantID int64) error {
	if tenantID <= 0 || s == nil || s.db == nil || s.source == nil {
		return fmt.Errorf("%w: professional source sync is unavailable", ErrInvalidSourceChange)
	}
	lock := s.tenantLock(tenantID)
	lock.Lock()
	defer lock.Unlock()
	for {
		cursor, err := sourceCheckpointCursor(ctx, s.db, tenantID, s.source.SourceModule(), professionalCatalogFeedName)
		if err != nil {
			return err
		}
		batch, err := s.source.ListCatalogResourceChanges(ctx, uint(tenantID), cursor, professionalChangeBatchSize)
		if err != nil {
			return fmt.Errorf("pull %s catalog changes for tenant %d: %w", s.source.SourceName(), tenantID, err)
		}
		if batch == nil || batch.SchemaVersion != s.source.SchemaVersion() {
			return fmt.Errorf("%w: unsupported %s change schema", ErrInvalidSourceChange, s.source.SourceName())
		}
		if err := s.applyBatch(ctx, tenantID, cursor, batch); err != nil {
			return err
		}
		if !batch.HasMore {
			return nil
		}
	}
}

func (s *ProfessionalSourceSyncService) tenantLock(tenantID int64) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lock := s.locks[tenantID]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	s.locks[tenantID] = lock
	return lock
}

func (s *ProfessionalSourceSyncService) applyBatch(ctx context.Context, tenantID int64, expectedCursor string, batch *ProfessionalChangeBatch) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		checkpoint, err := lockSourceCheckpoint(tx, tenantID, s.source.SourceModule(), professionalCatalogFeedName)
		if err != nil {
			return err
		}
		if checkpoint.Cursor != expectedCursor {
			return fmt.Errorf("Catalog %s source checkpoint changed concurrently", s.source.SourceName())
		}
		for _, change := range batch.Changes {
			if err := applyProfessionalResourceChange(tx, tenantID, s.source.SourceModule(), change); err != nil {
				return err
			}
		}
		checkpoint.Cursor = batch.NextCursor
		if err := tx.Save(checkpoint).Error; err != nil {
			return fmt.Errorf("advance Catalog %s source checkpoint: %w", s.source.SourceName(), err)
		}
		return nil
	})
}

func sourceCheckpointCursor(ctx context.Context, db *gorm.DB, tenantID int64, sourceModule, feedName string) (string, error) {
	var checkpoint models.SourceCheckpoint
	err := db.WithContext(ctx).Where("tenant_id = ? AND source_module = ? AND feed_name = ?", tenantID, sourceModule, feedName).First(&checkpoint).Error
	if err == nil {
		return checkpoint.Cursor, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("read Catalog source checkpoint: %w", err)
	}
	return "", nil
}

func lockSourceCheckpoint(tx *gorm.DB, tenantID int64, sourceModule, feedName string) (*models.SourceCheckpoint, error) {
	var checkpoint models.SourceCheckpoint
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND source_module = ? AND feed_name = ?", tenantID, sourceModule, feedName,
	).First(&checkpoint).Error
	if err == nil {
		return &checkpoint, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("lock Catalog source checkpoint: %w", err)
	}
	checkpoint = models.SourceCheckpoint{TenantID: tenantID, SourceModule: sourceModule, FeedName: feedName, Cursor: ""}
	if err := tx.Create(&checkpoint).Error; err != nil {
		return nil, fmt.Errorf("create Catalog source checkpoint: %w", err)
	}
	return &checkpoint, nil
}

func applyProfessionalResourceChange(tx *gorm.DB, tenantID int64, sourceModule string, change ProfessionalResourceChange) error {
	identity := strings.TrimSpace(change.SourceIdentity)
	identityID, identityErr := strconv.ParseInt(identity, 10, 64)
	entryType := professionalEntryType(sourceModule, change.SourceType)
	if identityErr != nil || identityID <= 0 || strconv.FormatInt(identityID, 10) != identity || entryType == "" ||
		len(change.SourceVersion) != 20 || (change.Operation != "upsert" && change.Operation != "missing") || change.ObservedAt.IsZero() || len(change.Snapshot) == 0 {
		return fmt.Errorf("%w: malformed %s catalog resource change", ErrInvalidSourceChange, sourceModule)
	}
	var binding models.SourceBinding
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"tenant_id = ? AND source_module = ? AND source_type = ? AND source_identity = ? AND is_current = ?",
		tenantID, sourceModule, change.SourceType, identity, true,
	).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return createEntryFromProfessionalChange(tx, tenantID, sourceModule, entryType, change)
	}
	if err != nil {
		return fmt.Errorf("find Catalog %s source binding: %w", sourceModule, err)
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
		reason := "source_deleted"
		missingReason = &reason
	}
	if err := tx.Model(&binding).Updates(map[string]any{
		"source_status": status, "source_version": change.SourceVersion,
		"observed_snapshot": commonModels.JSONMap(change.Snapshot), "observed_at": change.ObservedAt.UTC(),
		"missing_at": missingAt, "missing_reason": missingReason,
	}).Error; err != nil {
		return fmt.Errorf("update Catalog %s source binding: %w", sourceModule, err)
	}
	if err := tx.Model(&models.Entry{}).Where("tenant_id = ? AND id = ?", tenantID, binding.CatalogEntryID).
		UpdateColumn("version", gorm.Expr("version + 1")).Error; err != nil {
		return fmt.Errorf("advance Catalog entry version: %w", err)
	}
	return enqueueProjection(tx, tenantID, binding.CatalogEntryID)
}

func professionalEntryType(sourceModule, sourceType string) string {
	switch {
	case sourceModule == models.SourceModuleModel && sourceType == models.SourceTypeEntity:
		return models.EntryTypeBusinessEntity
	case sourceModule == models.SourceModuleModel && sourceType == models.SourceTypeLogicalTable:
		return models.EntryTypeLogicalModel
	case sourceModule == models.SourceModuleStandard && sourceType == models.SourceTypeMetric:
		return models.EntryTypeMetric
	case sourceModule == models.SourceModuleService && sourceType == models.SourceTypeQueryService:
		return models.EntryTypeDataService
	case sourceModule == models.SourceModuleDevelop && sourceType == models.SourceTypeDevTask:
		return models.EntryTypeDevelopmentArtifact
	default:
		return ""
	}
}

func createEntryFromProfessionalChange(tx *gorm.DB, tenantID int64, sourceModule, entryType string, change ProfessionalResourceChange) error {
	now := change.ObservedAt.UTC()
	entry := models.Entry{ID: uuid.New(), TenantID: tenantID, EntryType: entryType, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(&entry).Error; err != nil {
		return fmt.Errorf("create Catalog entry from %s: %w", sourceModule, err)
	}
	status := models.SourceStatusActive
	var missingAt *time.Time
	var missingReason *string
	if change.Operation == "missing" {
		status = models.SourceStatusMissing
		missingAt = &now
		reason := "source_deleted"
		missingReason = &reason
	}
	binding := models.SourceBinding{ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID,
		SourceModule: sourceModule, SourceType: change.SourceType, SourceIdentity: change.SourceIdentity,
		SourceStatus: status, SourceVersion: change.SourceVersion, IsCurrent: true, BoundAt: now,
		MissingAt: missingAt, MissingReason: missingReason, ObservedSnapshot: commonModels.JSONMap(change.Snapshot), ObservedAt: now}
	if err := tx.Create(&binding).Error; err != nil {
		return fmt.Errorf("create Catalog %s source binding: %w", sourceModule, err)
	}
	if err := tx.Create(&models.AuditEvent{ID: uuid.New(), TenantID: tenantID, CatalogEntryID: entry.ID,
		EventType: "source_discovered", ActorType: "service_principal", ActorID: "addp-catalog",
		Details: commonModels.JSONMap{"source_module": sourceModule, "source_type": change.SourceType, "source_identity": change.SourceIdentity}, CreatedAt: now}).Error; err != nil {
		return fmt.Errorf("create Catalog %s source audit: %w", sourceModule, err)
	}
	return enqueueProjection(tx, tenantID, entry.ID)
}
