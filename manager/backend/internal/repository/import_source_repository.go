package repository

import (
	"context"
	"errors"
	"github.com/addp/common/client"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type ImportSourceRepository struct{ db *gorm.DB }

func NewImportSourceRepository(db *gorm.DB) *ImportSourceRepository {
	return &ImportSourceRepository{db: db}
}
func (r *ImportSourceRepository) Create(ctx context.Context, source *models.ImportSource) error {
	return r.db.WithContext(ctx).Create(source).Error
}
func (r *ImportSourceRepository) BindTask(ctx context.Context, uploadUUID string, tenantID uint, taskID uint) error {
	if taskID == 0 {
		return errors.New("invalid import task")
	}
	result := r.db.WithContext(ctx).Model(&models.ImportSource{}).Where("upload_uuid = ? AND tenant_id = ? AND transfer_task_id IS NULL", uploadUUID, tenantID).Update("transfer_task_id", taskID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
func (r *ImportSourceRepository) Verify(ctx context.Context, tenantID uint, request client.ManagerImportSourceRequest) (*client.ManagerImportSource, error) {
	var source models.ImportSource
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND uploader_id = ? AND transfer_task_id = ? AND source_locator = ?", tenantID, request.PrincipalID, request.TaskID, request.SourceLocator).First(&source).Error
	if err != nil {
		return nil, err
	}
	return &client.ManagerImportSource{TenantID: source.TenantID, UploaderID: source.UploaderID, TaskID: *source.TransferTaskID, SourceLocator: source.SourceLocator}, nil
}
