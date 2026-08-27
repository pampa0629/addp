package repository

import (
	"errors"
	"time"

	"github.com/addp/workbench/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDataApplicationNotFound         = errors.New("data application not found")
	ErrDataApplicationVersionConflict  = errors.New("data application version conflict")
	ErrDataApplicationAlreadyPublished = errors.New("published data application cannot be deleted")
	ErrDataApplicationNotPublished     = errors.New("data application is not published")
)

type DataApplicationRepository struct{ db *gorm.DB }

func NewDataApplicationRepository(db *gorm.DB) *DataApplicationRepository {
	return &DataApplicationRepository{db: db}
}

func (r *DataApplicationRepository) List(tenantID, ownerUserID int64, offset, limit int) ([]models.DataApplication, int64, error) {
	query := r.db.Model(&models.DataApplication{}).Where("tenant_id = ? AND owner_user_id = ?", tenantID, ownerUserID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]models.DataApplication, 0)
	if err := query.Order("updated_at DESC, id ASC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *DataApplicationRepository) Get(tenantID, ownerUserID int64, id string) (*models.DataApplication, error) {
	var application models.DataApplication
	if err := r.db.Where("tenant_id = ? AND owner_user_id = ? AND id = ?", tenantID, ownerUserID, id).First(&application).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDataApplicationNotFound
		}
		return nil, err
	}
	return &application, nil
}

func (r *DataApplicationRepository) GetSourceViews(tenantID, ownerUserID int64, ids []string) ([]models.View, error) {
	views := make([]models.View, 0, len(ids))
	if err := r.db.Where("tenant_id = ? AND owner_user_id = ? AND id IN ?", tenantID, ownerUserID, ids).Find(&views).Error; err != nil {
		return nil, err
	}
	if len(views) != len(ids) {
		return nil, ErrViewNotFound
	}
	byID := make(map[string]models.View, len(views))
	for _, view := range views {
		byID[view.ID] = view
	}
	ordered := make([]models.View, 0, len(ids))
	for _, id := range ids {
		view, ok := byID[id]
		if !ok {
			return nil, ErrViewNotFound
		}
		ordered = append(ordered, view)
	}
	return ordered, nil
}

func (r *DataApplicationRepository) Create(application *models.DataApplication) error {
	return r.db.Create(application).Error
}

func (r *DataApplicationRepository) Update(application *models.DataApplication, expectedVersion int64) error {
	result := r.db.Model(&models.DataApplication{}).
		Where("tenant_id = ? AND owner_user_id = ? AND id = ? AND version = ?", application.TenantID, application.OwnerUserID, application.ID, expectedVersion).
		Updates(map[string]interface{}{
			"name": application.Name, "description": application.Description,
			"draft_snapshot": application.DraftSnapshot, "draft_content_hash": application.DraftContentHash,
			"version": gorm.Expr("version + 1"), "updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
	return r.resolveWriteResult(result, application.TenantID, application.OwnerUserID, application.ID)
}

func (r *DataApplicationRepository) Publish(tenantID, ownerUserID int64, id string, expectedVersion int64, publishedBy int64) (*models.DataApplicationRevision, error) {
	var revision models.DataApplicationRevision
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var application models.DataApplication
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND owner_user_id = ? AND id = ?", tenantID, ownerUserID, id).
			First(&application).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDataApplicationNotFound
			}
			return err
		}
		if application.Version != expectedVersion {
			return ErrDataApplicationVersionConflict
		}
		nextRevision := int64(1)
		if application.CurrentRevisionNumber != nil {
			nextRevision = *application.CurrentRevisionNumber + 1
		}
		now := time.Now().UTC()
		revision = models.DataApplicationRevision{
			ID: uuid.NewString(), ApplicationID: application.ID, TenantID: tenantID,
			RevisionNumber: nextRevision, Name: application.Name, Description: application.Description,
			Snapshot: append([]byte(nil), application.DraftSnapshot...), ContentHash: application.DraftContentHash,
			PublishedBy: publishedBy, PublishedAt: now,
		}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		result := tx.Model(&models.DataApplication{}).
			Where("tenant_id = ? AND owner_user_id = ? AND id = ? AND version = ?", tenantID, ownerUserID, id, expectedVersion).
			Updates(map[string]interface{}{
				"publication_status":      models.PublicationStatusPublished,
				"current_revision_number": nextRevision, "current_revision_hash": application.DraftContentHash,
				"version": gorm.Expr("version + 1"), "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDataApplicationVersionConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *DataApplicationRepository) Offline(tenantID, ownerUserID int64, id string, expectedVersion int64) error {
	result := r.db.Model(&models.DataApplication{}).
		Where("tenant_id = ? AND owner_user_id = ? AND id = ? AND version = ? AND publication_status = ? AND current_revision_number IS NOT NULL", tenantID, ownerUserID, id, expectedVersion, models.PublicationStatusPublished).
		Updates(map[string]interface{}{
			"publication_status": models.PublicationStatusOffline,
			"version":            gorm.Expr("version + 1"), "updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	application, err := r.Get(tenantID, ownerUserID, id)
	if err != nil {
		return err
	}
	if application.Version != expectedVersion {
		return ErrDataApplicationVersionConflict
	}
	return ErrDataApplicationNotPublished
}

func (r *DataApplicationRepository) Delete(tenantID, ownerUserID int64, id string, expectedVersion int64) error {
	result := r.db.Where("tenant_id = ? AND owner_user_id = ? AND id = ? AND version = ? AND current_revision_number IS NULL", tenantID, ownerUserID, id, expectedVersion).
		Delete(&models.DataApplication{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	application, err := r.Get(tenantID, ownerUserID, id)
	if err != nil {
		return err
	}
	if application.Version != expectedVersion {
		return ErrDataApplicationVersionConflict
	}
	return ErrDataApplicationAlreadyPublished
}

func (r *DataApplicationRepository) GetRuntime(tenantID, ownerUserID int64, id string) (*models.DataApplicationRevision, error) {
	application, err := r.Get(tenantID, ownerUserID, id)
	if err != nil {
		return nil, err
	}
	if application.PublicationStatus != models.PublicationStatusPublished || application.CurrentRevisionNumber == nil {
		return nil, ErrDataApplicationNotPublished
	}
	var revision models.DataApplicationRevision
	err = r.db.Where("tenant_id = ? AND application_id = ? AND revision_number = ?", tenantID, id, *application.CurrentRevisionNumber).
		First(&revision).Error
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

func (r *DataApplicationRepository) resolveWriteResult(result *gorm.DB, tenantID, ownerUserID int64, id string) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := r.db.Model(&models.DataApplication{}).Where("tenant_id = ? AND owner_user_id = ? AND id = ?", tenantID, ownerUserID, id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrDataApplicationNotFound
	}
	return ErrDataApplicationVersionConflict
}
