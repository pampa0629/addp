package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxCollectionEntries = 200

type CollectionAccess struct {
	UserID         int64
	ReadGroupIDs   []int64
	UpdateGroupIDs []int64
	EntryAccess    EntryAccess
}

type CollectionListFilter struct {
	ProjectGroupID int64
	Page           int
	PageSize       int
}

type CollectionListResult struct {
	Data       []models.Collection `json:"data"`
	Total      int64               `json:"total"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	TotalPages int                 `json:"total_pages"`
}

type CollectionDetail struct {
	models.Collection
	Entries []EntrySummary `json:"entries"`
}

type CollectionInput struct {
	ProjectGroupID int64
	Version        int64
	Name           string
	Description    string
	EntryIDs       []uuid.UUID
}

type CollectionProjectGroupAccess struct {
	ProjectGroupID int64
	RelationRole   string
	CanRead        bool
	CanUpdate      bool
}

type CollectionProjectGroupOption struct {
	ProjectGroupID int64  `json:"project_group_id,string" swaggertype:"string"`
	Name           string `json:"name"`
	Code           string `json:"code,omitempty"`
	Status         string `json:"status"`
	RelationRole   string `json:"relation_role"`
	CanRead        bool   `json:"can_read"`
	CanUpdate      bool   `json:"can_update"`
}

type CollectionProjectGroupList struct {
	Data []CollectionProjectGroupOption `json:"data"`
}

type CollectionService struct {
	db       *gorm.DB
	entries  *EntryService
	resolver SystemReferenceResolver
	now      func() time.Time
}

func NewCollectionService(db *gorm.DB, entries *EntryService) *CollectionService {
	return &CollectionService{db: db, entries: entries, now: time.Now}
}

func (s *CollectionService) WithSystemReferenceResolver(resolver SystemReferenceResolver) *CollectionService {
	s.resolver = resolver
	return s
}

func (s *CollectionService) ListProjectGroups(
	ctx context.Context,
	tenantID int64,
	accesses []CollectionProjectGroupAccess,
) (*CollectionProjectGroupList, error) {
	if s == nil || s.resolver == nil || tenantID <= 0 {
		return nil, ErrReferenceValidationUnavailable
	}
	if len(accesses) == 0 {
		return &CollectionProjectGroupList{Data: []CollectionProjectGroupOption{}}, nil
	}
	references := make([]commonClient.SystemCatalogReference, 0, len(accesses))
	for _, access := range accesses {
		if access.ProjectGroupID <= 0 || strings.TrimSpace(access.RelationRole) == "" || (!access.CanRead && !access.CanUpdate) {
			return nil, ErrReferenceValidationUnavailable
		}
		references = append(references, commonClient.SystemCatalogReference{SubjectType: "project_group", ID: access.ProjectGroupID})
	}
	resolved, err := s.resolver.ResolveSystemReferences(ctx, tenantID, references)
	if err != nil || len(resolved) != len(references) {
		return nil, fmt.Errorf("%w: resolve Project Group references", ErrReferenceValidationUnavailable)
	}
	data := make([]CollectionProjectGroupOption, 0, len(accesses))
	for index, result := range resolved {
		if result.SubjectType != "project_group" || result.ID != accesses[index].ProjectGroupID {
			return nil, ErrReferenceValidationUnavailable
		}
		if !result.Found || !result.Referenceable {
			continue
		}
		name := strings.TrimSpace(result.Name)
		if name == "" {
			return nil, ErrReferenceValidationUnavailable
		}
		data = append(data, CollectionProjectGroupOption{
			ProjectGroupID: result.ID, Name: name, Code: strings.TrimSpace(result.Code), Status: result.Status,
			RelationRole: accesses[index].RelationRole, CanRead: accesses[index].CanRead, CanUpdate: accesses[index].CanUpdate,
		})
	}
	return &CollectionProjectGroupList{Data: data}, nil
}

func (s *CollectionService) List(ctx context.Context, tenantID int64, access CollectionAccess, filter CollectionListFilter) (*CollectionListResult, error) {
	if s == nil || s.db == nil || tenantID <= 0 || access.UserID <= 0 || filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 200 || filter.ProjectGroupID < 0 {
		return nil, ErrInvalidCollection
	}
	allowed := canonicalPositiveIDs(access.ReadGroupIDs)
	if filter.ProjectGroupID > 0 {
		if !containsInt64(allowed, filter.ProjectGroupID) {
			return &CollectionListResult{Data: []models.Collection{}, Page: filter.Page, PageSize: filter.PageSize}, nil
		}
		allowed = []int64{filter.ProjectGroupID}
	}
	if len(allowed) == 0 {
		return &CollectionListResult{Data: []models.Collection{}, Page: filter.Page, PageSize: filter.PageSize}, nil
	}
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND project_group_id IN ?", tenantID, allowed)
	var total int64
	if err := query.Model(&models.Collection{}).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count Catalog collections: %w", err)
	}
	data := make([]models.Collection, 0)
	if err := query.Order("updated_at DESC, id ASC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&data).Error; err != nil {
		return nil, fmt.Errorf("list Catalog collections: %w", err)
	}
	return &CollectionListResult{Data: data, Total: total, Page: filter.Page, PageSize: filter.PageSize, TotalPages: totalPages(total, filter.PageSize)}, nil
}

func (s *CollectionService) Get(ctx context.Context, tenantID int64, access CollectionAccess, id uuid.UUID) (*CollectionDetail, error) {
	collection, err := s.getAccessibleCollection(ctx, tenantID, id, access.ReadGroupIDs)
	if err != nil {
		return nil, err
	}
	entries, err := s.listVisibleCollectionEntries(ctx, tenantID, access.EntryAccess, id)
	if err != nil {
		return nil, err
	}
	return &CollectionDetail{Collection: *collection, Entries: entries}, nil
}

func (s *CollectionService) Create(ctx context.Context, tenantID int64, access CollectionAccess, input CollectionInput) (*CollectionDetail, error) {
	name, description, entryIDs, err := normalizeCollectionInput(input)
	if err != nil || tenantID <= 0 || access.UserID <= 0 || input.ProjectGroupID <= 0 ||
		!containsInt64(access.ReadGroupIDs, input.ProjectGroupID) || !containsInt64(access.UpdateGroupIDs, input.ProjectGroupID) {
		return nil, ErrInvalidCollection
	}
	if err := s.validateVisibleEntries(ctx, tenantID, access.EntryAccess, entryIDs); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	collection := models.Collection{ID: uuid.New(), TenantID: tenantID, ProjectGroupID: input.ProjectGroupID, Name: name,
		Description: description, Version: 1, CreatedBy: access.UserID, CreatedAt: now, UpdatedAt: now}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&collection).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrCollectionNameConflict
			}
			return fmt.Errorf("create Catalog collection: %w", err)
		}
		if err := replaceCollectionEntries(tx, collection, access.UserID, entryIDs, now); err != nil {
			return err
		}
		return createCollectionAudit(tx, collection, access.UserID, "catalog.collection.created", len(entryIDs), now)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, CollectionAccess{UserID: access.UserID, ReadGroupIDs: []int64{input.ProjectGroupID}, EntryAccess: access.EntryAccess}, collection.ID)
}

func (s *CollectionService) Update(ctx context.Context, tenantID int64, access CollectionAccess, id uuid.UUID, input CollectionInput) (*CollectionDetail, error) {
	name, description, entryIDs, err := normalizeCollectionInput(input)
	if err != nil || tenantID <= 0 || access.UserID <= 0 || id == uuid.Nil || input.Version <= 0 {
		return nil, ErrInvalidCollection
	}
	current, err := s.getAccessibleCollection(ctx, tenantID, id, writableCollectionGroupIDs(access))
	if err != nil {
		return nil, err
	}
	if err := s.validateVisibleEntries(ctx, tenantID, access.EntryAccess, entryIDs); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked models.Collection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&locked).Error; err != nil {
			return ErrCollectionNotFound
		}
		if locked.ProjectGroupID != current.ProjectGroupID || locked.Version != input.Version {
			return ErrCollectionVersionConflict
		}
		result := tx.Model(&models.Collection{}).Where("tenant_id = ? AND id = ? AND version = ?", tenantID, id, input.Version).
			Updates(map[string]interface{}{"name": name, "description": description, "version": gorm.Expr("version + 1"), "updated_at": now})
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
				return ErrCollectionNameConflict
			}
			return fmt.Errorf("update Catalog collection: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrCollectionVersionConflict
		}
		locked.Name, locked.Description, locked.Version, locked.UpdatedAt = name, description, input.Version+1, now
		if err := replaceCollectionEntries(tx, locked, access.UserID, entryIDs, now); err != nil {
			return err
		}
		return createCollectionAudit(tx, locked, access.UserID, "catalog.collection.updated", len(entryIDs), now)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, CollectionAccess{UserID: access.UserID, ReadGroupIDs: []int64{current.ProjectGroupID}, EntryAccess: access.EntryAccess}, id)
}

func (s *CollectionService) Delete(ctx context.Context, tenantID int64, access CollectionAccess, id uuid.UUID, version int64) error {
	if tenantID <= 0 || access.UserID <= 0 || id == uuid.Nil || version <= 0 {
		return ErrInvalidCollection
	}
	if _, err := s.getAccessibleCollection(ctx, tenantID, id, writableCollectionGroupIDs(access)); err != nil {
		return err
	}
	now := s.now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked models.Collection
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&locked).Error; err != nil {
			return ErrCollectionNotFound
		}
		if locked.Version != version {
			return ErrCollectionVersionConflict
		}
		var memberCount int64
		if err := tx.Model(&models.CollectionEntry{}).Where("tenant_id = ? AND collection_id = ?", tenantID, id).Count(&memberCount).Error; err != nil {
			return fmt.Errorf("count Catalog collection entries: %w", err)
		}
		if err := createCollectionAudit(tx, locked, access.UserID, "catalog.collection.deleted", int(memberCount), now); err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND collection_id = ?", tenantID, id).Delete(&models.CollectionEntry{}).Error; err != nil {
			return fmt.Errorf("delete Catalog collection entries: %w", err)
		}
		if result := tx.Where("tenant_id = ? AND id = ? AND version = ?", tenantID, id, version).Delete(&models.Collection{}); result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return fmt.Errorf("delete Catalog collection: %w", result.Error)
			}
			return ErrCollectionVersionConflict
		}
		return nil
	})
}

func (s *CollectionService) getAccessibleCollection(ctx context.Context, tenantID int64, id uuid.UUID, allowedGroupIDs []int64) (*models.Collection, error) {
	if s == nil || s.db == nil || tenantID <= 0 || id == uuid.Nil || len(allowedGroupIDs) == 0 {
		return nil, ErrCollectionNotFound
	}
	var collection models.Collection
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ? AND project_group_id IN ?", tenantID, id, canonicalPositiveIDs(allowedGroupIDs)).First(&collection).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCollectionNotFound
		}
		return nil, fmt.Errorf("get Catalog collection: %w", err)
	}
	return &collection, nil
}

func (s *CollectionService) validateVisibleEntries(ctx context.Context, tenantID int64, access EntryAccess, entryIDs []uuid.UUID) error {
	if len(entryIDs) == 0 {
		return nil
	}
	var count int64
	if err := s.entries.visibleEntriesQuery(ctx, tenantID, access).Where("entries.id IN ?", entryIDs).Count(&count).Error; err != nil {
		return fmt.Errorf("validate Catalog collection entries: %w", err)
	}
	if count != int64(len(entryIDs)) {
		return ErrEntryNotFound
	}
	return nil
}

func (s *CollectionService) listVisibleCollectionEntries(ctx context.Context, tenantID int64, access EntryAccess, collectionID uuid.UUID) ([]EntrySummary, error) {
	type entryRow struct {
		models.Entry
		SourceStatus     string
		SourceIdentity   string
		ObservedSnapshot commonModels.JSONMap
	}
	var rows []entryRow
	err := s.entries.visibleEntriesQuery(ctx, tenantID, access).
		Joins("JOIN catalog.collection_entries AS member ON member.tenant_id = entries.tenant_id AND member.catalog_entry_id = entries.id AND member.collection_id = ?", collectionID).
		Joins("JOIN catalog.source_bindings AS source ON source.tenant_id = entries.tenant_id AND source.catalog_entry_id = entries.id AND source.is_current = ?", true).
		Select("entries.*, source.source_status, source.source_identity, source.observed_snapshot").
		Order("member.created_at ASC, member.id ASC").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list visible Catalog collection entries: %w", err)
	}
	result := make([]EntrySummary, 0, len(rows))
	for _, row := range rows {
		name, _ := row.ObservedSnapshot["name"].(string)
		engineID, _ := numericInt64(row.ObservedSnapshot["engine_id"])
		if row.BusinessName != nil && strings.TrimSpace(*row.BusinessName) != "" {
			name = *row.BusinessName
		}
		result = append(result, EntrySummary{Entry: row.Entry, DisplayName: name, SourceStatus: row.SourceStatus,
			SourceIdentity: row.SourceIdentity, SourceEngineID: engineID})
	}
	return result, nil
}

func normalizeCollectionInput(input CollectionInput) (string, string, []uuid.UUID, error) {
	name := strings.TrimSpace(input.Name)
	description := strings.TrimSpace(input.Description)
	if name == "" || len([]rune(name)) > 200 || len([]rune(description)) > 2000 || len(input.EntryIDs) > maxCollectionEntries {
		return "", "", nil, ErrInvalidCollection
	}
	seen := make(map[uuid.UUID]struct{}, len(input.EntryIDs))
	entryIDs := make([]uuid.UUID, 0, len(input.EntryIDs))
	for _, entryID := range input.EntryIDs {
		if entryID == uuid.Nil {
			return "", "", nil, ErrInvalidCollection
		}
		if _, exists := seen[entryID]; exists {
			return "", "", nil, ErrInvalidCollection
		}
		seen[entryID] = struct{}{}
		entryIDs = append(entryIDs, entryID)
	}
	return name, description, entryIDs, nil
}

func writableCollectionGroupIDs(access CollectionAccess) []int64 {
	readable := make(map[int64]struct{})
	for _, id := range canonicalPositiveIDs(access.ReadGroupIDs) {
		readable[id] = struct{}{}
	}
	result := make([]int64, 0, len(access.UpdateGroupIDs))
	for _, id := range canonicalPositiveIDs(access.UpdateGroupIDs) {
		if _, ok := readable[id]; ok {
			result = append(result, id)
		}
	}
	return result
}

func replaceCollectionEntries(tx *gorm.DB, collection models.Collection, actorID int64, entryIDs []uuid.UUID, now time.Time) error {
	if err := tx.Where("tenant_id = ? AND collection_id = ?", collection.TenantID, collection.ID).Delete(&models.CollectionEntry{}).Error; err != nil {
		return fmt.Errorf("replace Catalog collection entries: %w", err)
	}
	rows := make([]models.CollectionEntry, 0, len(entryIDs))
	for _, entryID := range entryIDs {
		rows = append(rows, models.CollectionEntry{ID: uuid.New(), TenantID: collection.TenantID, CollectionID: collection.ID,
			CatalogEntryID: entryID, AddedBy: actorID, CreatedAt: now})
	}
	if len(rows) > 0 {
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("create Catalog collection entries: %w", err)
		}
	}
	return nil
}

func createCollectionAudit(tx *gorm.DB, collection models.Collection, actorID int64, eventType string, entryCount int, now time.Time) error {
	if err := tx.Create(&models.CollectionAuditEvent{ID: uuid.New(), TenantID: collection.TenantID, CollectionID: collection.ID,
		EventType: eventType, ActorID: actorID, Details: commonModels.JSONMap{"project_group_id": collection.ProjectGroupID,
			"version": collection.Version, "entry_count": entryCount, "name": collection.Name}, CreatedAt: now}).Error; err != nil {
		return fmt.Errorf("audit Catalog collection: %w", err)
	}
	return nil
}

func canonicalPositiveIDs(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsInt64(values []int64, expected int64) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
