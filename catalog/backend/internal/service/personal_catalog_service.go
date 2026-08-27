package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	PersonalRelationResponsible = "responsible"
	PersonalRelationFavorite    = "favorite"
	PersonalRelationFollowing   = "following"
)

type EntryMarks struct {
	Favorite  bool `json:"favorite"`
	Following bool `json:"following"`
}

type PersonalCatalogService struct {
	db      *gorm.DB
	entries *EntryService
	now     func() time.Time
}

func NewPersonalCatalogService(db *gorm.DB, entries *EntryService) *PersonalCatalogService {
	return &PersonalCatalogService{db: db, entries: entries, now: time.Now}
}

func (s *PersonalCatalogService) List(ctx context.Context, tenantID, userID int64, access EntryAccess, relation string, page, pageSize int) (*EntryListResult, error) {
	if s == nil || s.db == nil || s.entries == nil || tenantID <= 0 || userID <= 0 || page < 1 || pageSize < 1 || pageSize > 200 ||
		!oneOf(relation, PersonalRelationResponsible, PersonalRelationFavorite, PersonalRelationFollowing) {
		return nil, ErrInvalidPersonalRelation
	}
	query := s.entries.visibleEntriesQuery(ctx, tenantID, access).
		Joins("JOIN catalog.source_bindings AS source ON source.catalog_entry_id = entries.id AND source.tenant_id = entries.tenant_id AND source.is_current = ?", true)
	if relation == PersonalRelationResponsible {
		query = query.Where(`EXISTS (SELECT 1 FROM catalog.responsibilities personal_responsibility
			WHERE personal_responsibility.tenant_id = entries.tenant_id
			AND personal_responsibility.catalog_entry_id = entries.id
			AND personal_responsibility.subject_type = 'user' AND personal_responsibility.subject_id = ?)`, userID)
	} else {
		query = query.Where(`EXISTS (SELECT 1 FROM catalog.entry_marks personal_mark
			WHERE personal_mark.tenant_id = entries.tenant_id
			AND personal_mark.catalog_entry_id = entries.id
			AND personal_mark.user_id = ? AND personal_mark.mark_type = ?)`, userID, relation)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count personal Catalog entries: %w", err)
	}
	type entryRow struct {
		models.Entry
		SourceStatus     string
		SourceIdentity   string
		ObservedSnapshot commonModels.JSONMap
	}
	var rows []entryRow
	if err := query.Select("entries.*, source.source_status, source.source_identity, source.observed_snapshot").
		Order("entries.updated_at DESC, entries.id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list personal Catalog entries: %w", err)
	}
	data := make([]EntrySummary, 0, len(rows))
	for _, row := range rows {
		name, _ := row.ObservedSnapshot["name"].(string)
		engineID, _ := numericInt64(row.ObservedSnapshot["engine_id"])
		if row.BusinessName != nil && strings.TrimSpace(*row.BusinessName) != "" {
			name = *row.BusinessName
		}
		data = append(data, EntrySummary{Entry: row.Entry, DisplayName: name, SourceStatus: row.SourceStatus,
			SourceIdentity: row.SourceIdentity, SourceEngineID: engineID})
	}
	return &EntryListResult{Data: data, Total: total, Page: page, PageSize: pageSize, TotalPages: totalPages(total, pageSize)}, nil
}

func (s *PersonalCatalogService) GetMarks(ctx context.Context, tenantID, userID int64, access EntryAccess, entryID uuid.UUID) (*EntryMarks, error) {
	if err := s.requireVisibleEntry(ctx, tenantID, userID, access, entryID); err != nil {
		return nil, err
	}
	var marks []models.EntryMark
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND catalog_entry_id = ?", tenantID, userID, entryID).Find(&marks).Error; err != nil {
		return nil, fmt.Errorf("read Catalog entry marks: %w", err)
	}
	result := &EntryMarks{}
	for _, mark := range marks {
		result.Favorite = result.Favorite || mark.MarkType == models.EntryMarkTypeFavorite
		result.Following = result.Following || mark.MarkType == models.EntryMarkTypeFollowing
	}
	return result, nil
}

func (s *PersonalCatalogService) ReplaceMarks(ctx context.Context, tenantID, userID int64, access EntryAccess, entryID uuid.UUID, marks EntryMarks) (*EntryMarks, error) {
	if err := s.requireVisibleEntry(ctx, tenantID, userID, access, entryID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			lockKey := fmt.Sprintf("catalog-entry-marks:%d:%d:%s", tenantID, userID, entryID)
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
				return fmt.Errorf("lock Catalog entry marks: %w", err)
			}
		}
		if err := tx.Where("tenant_id = ? AND user_id = ? AND catalog_entry_id = ?", tenantID, userID, entryID).Delete(&models.EntryMark{}).Error; err != nil {
			return fmt.Errorf("replace Catalog entry marks: %w", err)
		}
		rows := make([]models.EntryMark, 0, 2)
		if marks.Favorite {
			rows = append(rows, models.EntryMark{ID: uuid.New(), TenantID: tenantID, UserID: userID, CatalogEntryID: entryID, MarkType: models.EntryMarkTypeFavorite, CreatedAt: now})
		}
		if marks.Following {
			rows = append(rows, models.EntryMark{ID: uuid.New(), TenantID: tenantID, UserID: userID, CatalogEntryID: entryID, MarkType: models.EntryMarkTypeFollowing, CreatedAt: now})
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return fmt.Errorf("create Catalog entry marks: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &marks, nil
}

func (s *PersonalCatalogService) requireVisibleEntry(ctx context.Context, tenantID, userID int64, access EntryAccess, entryID uuid.UUID) error {
	if s == nil || s.db == nil || s.entries == nil || tenantID <= 0 || userID <= 0 || entryID == uuid.Nil {
		return ErrUserPrincipalRequired
	}
	var count int64
	if err := s.entries.visibleEntriesQuery(ctx, tenantID, access).Where("entries.id = ?", entryID).Count(&count).Error; err != nil {
		return fmt.Errorf("verify visible Catalog entry: %w", err)
	}
	if count != 1 {
		return ErrEntryNotFound
	}
	return nil
}
